/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package core

import (
	"context"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGenerateConfigMap(t *testing.T) {
	scheme := newTestScheme()

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
			UID:       "cluster-uid",
		},
	}
	cnpgCluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
		},
		Status: cnpgv1.ClusterStatus{
			WriteService: "my-cluster-rw",
			ReadService:  "my-cluster-ro",
		},
	}

	t.Run("base endpoints without poolers", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		cm, err := generateConfigMap(context.Background(), c, scheme, cluster.DeepCopy(), cnpgCluster, "my-secret")

		require.NoError(t, err)
		assert.Equal(t, "my-cluster-configmap", cm.Name)
		assert.Equal(t, "default", cm.Namespace)
		assert.Equal(t, "my-cluster-rw.default.svc.cluster.local", cm.Data[pgconninfo.KeyClusterRWEndpoint])
		assert.Equal(t, "my-cluster-ro.default.svc.cluster.local", cm.Data[pgconninfo.KeyClusterROEndpoint])
		assert.Equal(t, "my-cluster-r.default.svc.cluster.local", cm.Data[pgconninfo.KeyClusterREndpoint])
		assert.Equal(t, pgconninfo.DefaultPort, cm.Data[pgconninfo.KeyDefaultClusterPort])
		assert.Equal(t, "postgres", cm.Data[configMapKeySuperUserName])
		assert.Equal(t, "my-secret", cm.Data[configMapKeySuperUserSecretRef])
		assert.NotContains(t, cm.Data, pgconninfo.KeyPoolerRWEndpoint)
		require.Len(t, cm.OwnerReferences, 1)
		assert.Equal(t, "cluster-uid", string(cm.OwnerReferences[0].UID))
	})

	t.Run("includes pooler endpoints when both poolers exist", func(t *testing.T) {
		rwPooler := &cnpgv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{Name: "my-cluster-pooler-rw", Namespace: "default"},
		}
		roPooler := &cnpgv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{Name: "my-cluster-pooler-ro", Namespace: "default"},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(rwPooler, roPooler).Build()
		cm, err := generateConfigMap(context.Background(), c, scheme, cluster.DeepCopy(), cnpgCluster, "my-secret")

		require.NoError(t, err)
		assert.Equal(t, "my-cluster-pooler-rw.default.svc.cluster.local", cm.Data[pgconninfo.KeyPoolerRWEndpoint])
		assert.Equal(t, "my-cluster-pooler-ro.default.svc.cluster.local", cm.Data[pgconninfo.KeyPoolerROEndpoint])
	})

	t.Run("uses existing configmap name from status", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		pg := cluster.DeepCopy()
		pg.Status.Resources = &enterprisev4.PostgresClusterResources{
			ConfigMapRef: &corev1.LocalObjectReference{Name: "custom-configmap"},
		}

		cm, err := generateConfigMap(context.Background(), c, scheme, pg, cnpgCluster, "my-secret")

		require.NoError(t, err)
		assert.Equal(t, "custom-configmap", cm.Name)
	})

	t.Run("includes CA metadata when available", func(t *testing.T) {
		caSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "my-server-ca", Namespace: "default"},
			Data:       map[string][]byte{"ca.crt": []byte("-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----\n")},
		}
		cnpg := cnpgCluster.DeepCopy()
		cnpg.Status.Certificates.ServerCASecret = "my-server-ca"
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(caSecret).Build()
		cm, err := generateConfigMap(t.Context(), c, scheme, cluster.DeepCopy(), cnpg, "my-secret")
		require.NoError(t, err)
		assert.Equal(t, "my-server-ca/"+defaultServerCACertKey, cm.Data[configMapKeyServerCASecretRef])
	})

	t.Run("omits CA metadata when CNPG has no CA secret set", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		cm, err := generateConfigMap(t.Context(), c, scheme, cluster.DeepCopy(), cnpgCluster, "my-secret")
		require.NoError(t, err)
		assert.NotContains(t, cm.Data, configMapKeyServerCASecretRef)
	})
}

func TestConfigMapConverge_RequeuesWhenCNPGPublishesCASecretButMetadataMissing(t *testing.T) {
	t.Parallel()

	// Arrange
	scheme := newTestScheme()

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status: enterprisev4.PostgresClusterStatus{
			Resources: &enterprisev4.PostgresClusterResources{
				ConfigMapRef: &corev1.LocalObjectReference{Name: "pg1-configmap"},
			},
		},
	}
	// ConfigMap lacks SERVER_CA_SECRET_REF — simulates CNPG having published CA but secret not yet materialized.
	existingCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-configmap", Namespace: "default"},
		Data: map[string]string{
			pgconninfo.KeyClusterRWEndpoint:  "pg1-rw.default.svc.cluster.local",
			pgconninfo.KeyClusterROEndpoint:  "pg1-ro.default.svc.cluster.local",
			pgconninfo.KeyClusterREndpoint:   "pg1-r.default.svc.cluster.local",
			pgconninfo.KeyDefaultClusterPort: pgconninfo.DefaultPort,
			configMapKeySuperUserSecretRef:   "pg1-secret",
		},
	}
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status: cnpgv1.ClusterStatus{
			Phase:        cnpgv1.PhaseHealthy,
			WriteService: "pg1-rw",
			ReadService:  "pg1-ro",
			Certificates: cnpgv1.CertificatesStatus{
				CertificatesConfiguration: cnpgv1.CertificatesConfiguration{
					ServerCASecret: "pg1-server-ca",
				},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingCM).Build()
	contracts := &reconcileContracts{CNPGCluster: cnpg, Secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret"}}}
	model := newConfigMapModel(c, scheme, noopEventEmitter{}, nil, cluster, contracts)

	// Act
	reconcileErr := model.Reconcile(t.Context())
	health, err := model.Observe(t.Context(), reconcileErr)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, pgcConstants.Provisioning, health.State)
	assert.Equal(t, reasonConfigMapFailed, health.Reason)
	assert.Equal(t, msgConfigMapCAMetadataPending, health.Message)
	assert.Equal(t, provisioningClusterPhase, health.Phase)
	assert.True(t, health.Result.RequeueAfter > 0)
}

func TestConfigMapModel_CheckContracts(t *testing.T) {
	scheme := newTestScheme()
	cluster := &enterprisev4.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"}}
	cnpg := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret"}}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	t.Run("returns errContractsNotReady when CNPGCluster is nil", func(t *testing.T) {
		contracts := &reconcileContracts{Secret: secret}
		model := newConfigMapModel(c, scheme, noopEventEmitter{}, nil, cluster, contracts)
		assert.ErrorIs(t, model.CheckContracts(), errContractsNotReady)
	})

	t.Run("returns errContractsNotReady when Secret is nil", func(t *testing.T) {
		contracts := &reconcileContracts{CNPGCluster: cnpg}
		model := newConfigMapModel(c, scheme, noopEventEmitter{}, nil, cluster, contracts)
		assert.ErrorIs(t, model.CheckContracts(), errContractsNotReady)
	})

	t.Run("returns nil when both contracts are satisfied", func(t *testing.T) {
		contracts := &reconcileContracts{CNPGCluster: cnpg, Secret: secret}
		model := newConfigMapModel(c, scheme, noopEventEmitter{}, nil, cluster, contracts)
		assert.NoError(t, model.CheckContracts())
	})
}
