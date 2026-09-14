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
package adapter

import (
	"context"
	"errors"
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	dbclusterreadiness "github.com/splunk/splunk-operator/pkg/postgresql/database/core/components/clusterreadiness"
	pgcnpg "github.com/splunk/splunk-operator/pkg/postgresql/shared/cnpg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestClusterReaderTranslatesFacts(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	ready := "Ready"
	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "primary", Namespace: "dbs"},
		Status: platformv1alpha1.PostgresClusterStatus{
			Phase:          &ready,
			ProvisionerRef: &corev1.ObjectReference{APIVersion: "postgresql.cnpg.io/v1", Kind: "Cluster", Name: "primary-cnpg", Namespace: "dbs"},
			Conditions:     []metav1.Condition{{Type: pgcnpg.ClusterReadyCondition, Reason: pgcnpg.ClusterReadyReasonFailingOver}},
			ManagedRolesStatus: &platformv1alpha1.ManagedRolesStatus{
				Reconciled: []string{"orders_admin"},
			},
			ConnectionPoolerStatus: &platformv1alpha1.ConnectionPoolerStatus{
				Enabled:          true,
				ReadWriteEnabled: true,
			},
			Resources: &platformv1alpha1.PostgresClusterResources{
				SuperUserSecretRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "primary-superuser"},
					Key:                  "password",
				},
			},
			CustomMetricsStatus: &platformv1alpha1.CustomMetricsStatus{
				DatabaseContributions: []platformv1alpha1.DatabaseCustomMetricsStatus{{DatabaseName: "orders"}},
			},
		},
	}

	facts, err := NewClusterReader(fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()).Read(t.Context(), "dbs", "primary")
	require.NoError(t, err)
	assert.Equal(t, dbclusterreadiness.LifecycleReady, facts.Lifecycle)
	require.NotNil(t, facts.Provider)
	assert.Equal(t, dbclusterreadiness.ProviderCNPG, facts.Provider.Kind)
	assert.Equal(t, "primary-cnpg", facts.Provider.Name)
	assert.Equal(t, dbclusterreadiness.RecoveryInProgress, facts.Recovery)
	require.NotNil(t, facts.ManagedRolesStatus)
	assert.Equal(t, []string{"orders_admin"}, facts.ManagedRolesStatus.Reconciled)
	require.NotNil(t, facts.ConnectionPoolerStatus)
	assert.True(t, facts.ConnectionPoolerStatus.ReadWriteEnabled)
	require.NotNil(t, facts.SuperUserSecretRef)
	assert.Equal(t, "primary-superuser", facts.SuperUserSecretRef.Name)
	require.NotNil(t, facts.CustomMetricsStatus)
	assert.Equal(t, "orders", facts.CustomMetricsStatus.DatabaseContributions[0].DatabaseName)
}

func TestClusterReaderClassifiesRecoveryConditions(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))

	tests := []struct {
		name       string
		conditions []metav1.Condition
		want       dbclusterreadiness.Recovery
	}{
		{
			name: "no cluster ready condition is not recovery",
			want: dbclusterreadiness.RecoveryNone,
		},
		{
			name: "unrelated cluster ready reason is not recovery",
			conditions: []metav1.Condition{{
				Type: pgcnpg.ClusterReadyCondition, Reason: "CNPGClusterProvisioning",
			}},
			want: dbclusterreadiness.RecoveryNone,
		},
		{
			name: "recovery is in progress",
			conditions: []metav1.Condition{{
				Type: pgcnpg.ClusterReadyCondition, Reason: pgcnpg.ClusterReadyReasonRecovery,
			}},
			want: dbclusterreadiness.RecoveryInProgress,
		},
		{
			name: "failover is in progress",
			conditions: []metav1.Condition{{
				Type: pgcnpg.ClusterReadyCondition, Reason: pgcnpg.ClusterReadyReasonFailingOver,
			}},
			want: dbclusterreadiness.RecoveryInProgress,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "primary", Namespace: "dbs"},
				Status:     platformv1alpha1.PostgresClusterStatus{Conditions: tt.conditions},
			}

			facts, err := NewClusterReader(fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()).Read(t.Context(), "dbs", "primary")
			require.NoError(t, err)
			assert.Equal(t, tt.want, facts.Recovery)
		})
	}
}

func TestClusterReaderMapsNotFoundAndPreservesTransientErrors(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))

	t.Run("not found", func(t *testing.T) {
		_, err := NewClusterReader(fake.NewClientBuilder().WithScheme(scheme).Build()).Read(t.Context(), "dbs", "missing")
		assert.ErrorIs(t, err, dbclusterreadiness.ErrClusterNotFound)
		assert.True(t, apierrors.IsNotFound(err))
	})

	t.Run("transient", func(t *testing.T) {
		transient := errors.New("apiserver unavailable")
		reader := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
				return transient
			},
		}).Build()
		_, err := NewClusterReader(reader).Read(t.Context(), "dbs", "primary")
		assert.ErrorIs(t, err, transient)
	})
}
