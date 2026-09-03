// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	"context"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/splunk/splunk-operator/pkg/splunk/resources"
)

func noahClusterForSHCTest(namespace, name, authSecretName string) *enterpriseApi.NoahCluster {
	return &enterpriseApi.NoahCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: enterpriseApi.NoahClusterSpec{
			Endpoint:      "http://noah.example:8080",
			Tenant:        "linus-dev",
			AuthSecretRef: corev1.LocalObjectReference{Name: authSecretName},
		},
	}
}

// applySearchHeadClusterNoah builds identity-aware deployer and search-head
// StatefulSets: SPLUNK_NOAH_ENABLED, the headless service name, and the
// cluster domain are present on both, and each StatefulSet keeps its own
// distinct name/labels so deployer and member identities cannot be confused.
func TestApplySearchHeadClusterNoahCreatesIdentityAwareStatefulSets(t *testing.T) {
	t.Setenv(resources.ClusterDomainEnvName, "corp.example")

	ctx := context.Background()
	client := spltest.NewMockClient()
	client.AddObject(noahClusterForSHCTest("test", "noah", "noah-auth"))
	client.AddObject(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noah-auth", Namespace: "test"},
		Data:       map[string][]byte{noahAuthSecretKey: []byte(t.Name())},
	})

	cr := &enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "enterprise.splunk.com/v4",
			Kind:       "SearchHeadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "shc",
			Namespace: "test",
			UID:       types.UID("shc-uid"),
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			Replicas: 3,
			NoahClusterRef: &corev1.LocalObjectReference{
				Name: "noah",
			},
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{Image: "splunk/splunk:latest"},
			},
		},
	}
	setVolumeDefaults(&cr.Spec.CommonSplunkSpec)

	searchHeadPhase, deployerPhase, searchHeadStatefulSet, err := applySearchHeadClusterNoah(ctx, client, cr)
	require.NoError(t, err)
	assert.NotEmpty(t, searchHeadPhase)
	assert.NotEmpty(t, deployerPhase)
	require.NotNil(t, searchHeadStatefulSet)

	deployerStatefulSet := &appsv1.StatefulSet{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{
		Name:      GetSplunkStatefulsetName(SplunkDeployer, cr.GetName()),
		Namespace: cr.GetNamespace(),
	}, deployerStatefulSet))

	assert.NotEqual(t, searchHeadStatefulSet.Name, deployerStatefulSet.Name, "deployer and member StatefulSets must have distinct names")

	for _, ss := range []*appsv1.StatefulSet{searchHeadStatefulSet, deployerStatefulSet} {
		env := make(map[string]corev1.EnvVar)
		for _, item := range ss.Spec.Template.Spec.Containers[0].Env {
			env[item.Name] = item
		}
		assert.Equal(t, "true", env[resources.NoahEnabledEnvName].Value, "%s must have Noah pod identity", ss.Name)
		assert.Equal(t, ss.Spec.ServiceName, env[resources.NoahHeadlessServiceEnvName].Value, "%s advertised identity must derive from its own headless service, not Pod IP", ss.Name)
		assert.Equal(t, "corp.example", env[resources.ClusterDomainEnvName].Value)
		require.NotNil(t, env[resources.PodNameEnvName].ValueFrom, "%s identity must survive Pod IP changes via the downward API, not a literal IP", ss.Name)
		require.NotNil(t, env[resources.PodNamespaceEnvName].ValueFrom)
	}
}

// A failure while removing owner references during deletion must abort
// immediately rather than fall through to CheckForDeletion, which removes
// finalizers once its own callbacks succeed. Swallowing the error here would
// let the finalizer disappear while owner references are still orphaned,
// with nothing left to retry the cleanup.
func TestApplySearchHeadClusterNoah_DeletionAbortsOnOwnerReferenceCleanupError(t *testing.T) {
	ctx := context.Background()
	client := spltest.NewMockClient()
	// The namespace-scoped secret is intentionally not seeded: it is the
	// first resource DeleteOwnerReferencesForResources touches, so its
	// absence reproduces a cleanup failure without a bespoke fake client.

	deletionTimestamp := metav1.Now()
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "shc",
			Namespace:         "test",
			DeletionTimestamp: &deletionTimestamp,
			Finalizers:        []string{"enterprise.splunk.com/delete-pvc"},
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			Replicas: 3,
			NoahClusterRef: &corev1.LocalObjectReference{
				Name: "noah",
			},
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{Image: "splunk/splunk:latest"},
			},
		},
	}
	setVolumeDefaults(&cr.Spec.CommonSplunkSpec)

	_, err := ApplySearchHeadClusterNoah(ctx, client, cr)
	require.Error(t, err)
	assert.Contains(t, cr.GetFinalizers(), "enterprise.splunk.com/delete-pvc", "finalizer must survive an owner-reference cleanup failure so deletion can retry")
}

// When the referenced NoahCluster does not exist yet, the reconcile must
// requeue as PhasePending rather than error — the CR may simply not have
// been created yet.
func TestApplySearchHeadClusterNoah_PendingWhenNoahClusterMissing(t *testing.T) {
	ctx := context.Background()
	client := spltest.NewMockClient()

	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "shc", Namespace: "test"},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			NoahClusterRef: &corev1.LocalObjectReference{Name: "missing-noah-cluster"},
		},
	}

	searchHeadPhase, deployerPhase, statefulSet, err := applySearchHeadClusterNoah(ctx, client, cr)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhasePending, searchHeadPhase)
	assert.Equal(t, enterpriseApi.PhasePending, deployerPhase)
	assert.Nil(t, statefulSet)
}
