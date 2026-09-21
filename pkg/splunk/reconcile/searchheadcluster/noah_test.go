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

package searchheadcluster

import (
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	reconcileutil "github.com/splunk/splunk-operator/pkg/splunk/reconcile"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	configworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/config"
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

// applySearchHeadClusterNoah never creates a deployer — this is unconditional,
// with no spec field controlling it. The search-head StatefulSet is still
// identity-aware — SPLUNK_NOAH_ENABLED,
// the headless service name, and the cluster domain are all present — and
// still gets SPLUNK_DEPLOYER_URL despite there being no deployer, repointed
// at 127.0.0.1 instead of the (nonexistent) deployer's: splunk-ansible's
// splunk_search_head role gates running `splunk init shcluster-config` (the
// command that actually enables search head clustering) on that variable
// being merely non-empty, never on it being reachable
// (roles/splunk_search_head/tasks/main.yml checks 'deployer_url' in splunk
// and splunk.deployer_url). Omitting the env var entirely left the search
// head permanently unclustered — a real regression an earlier version of
// this change introduced while over-applying a Codex review finding.
// Pointing it at a Kubernetes Service — the deleted deployer's, or even the
// search head's own — is not viable either: search_head_clustering.yml
// opens with a wait_for_splunk_instance check against deployer_url that
// burns a fixed ~6.5-minute retries*delay budget regardless of how the
// connection fails, and a not-yet-ready pod's own Service has zero ready
// endpoints (Kubernetes only routes to pods that already passed their own
// readiness probe), so both exceeded the pod's startup-probe deadline and
// restarted the container in a loop that never finished provisioning
// (live-verified 2026-09-10). 127.0.0.1 resolves that check almost
// instantly instead, since it talks to the already-running local splunkd
// directly, sidestepping Service routing entirely.
func TestApplySearchHeadClusterNoahCreatesIdentityAwareStatefulSets(t *testing.T) {
	t.Setenv(resources.ClusterDomainEnvName, "corp.example")

	ctx := t.Context()
	client := spltest.NewMockClient()
	client.AddObject(noahClusterForSHCTest("test", "noah", "noah-auth"))
	client.AddObject(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noah-auth", Namespace: "test"},
		Data:       map[string][]byte{configworkflow.NoahAuthSecretKey: []byte(t.Name())},
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
	resources.SetVolumeDefaults(&cr.Spec.CommonSplunkSpec)

	dependency := reconcileutil.ResolveNoahDependency(ctx, client, cr, &cr.Status.Conditions, cr.Spec.NoahClusterRef)
	require.NotNil(t, dependency.Runtime)
	require.NoError(t, dependency.ReconcileErr)
	searchHeadPhase, deployerPhase, searchHeadStatefulSet, err := applySearchHeadClusterNoah(ctx, client, cr, dependency.Runtime)
	require.NoError(t, err)
	assert.NotEmpty(t, searchHeadPhase)
	assert.Equal(t, enterpriseApi.PhaseReady, deployerPhase, "no deployer is ever deployed, so its phase is a fixed constant")
	require.NotNil(t, searchHeadStatefulSet)

	deployerStatefulSet := &appsv1.StatefulSet{}
	err = client.Get(ctx, types.NamespacedName{
		Name:      splutil.GetSplunkStatefulsetName(SplunkDeployer, cr.GetName()),
		Namespace: cr.GetNamespace(),
	}, deployerStatefulSet)
	assert.Error(t, err, "Noah must never create a deployer StatefulSet")

	env := make(map[string]corev1.EnvVar)
	for _, item := range searchHeadStatefulSet.Spec.Template.Spec.Containers[0].Env {
		env[item.Name] = item
	}
	assert.Equal(t, "true", env[resources.NoahEnabledEnvName].Value, "search-head must have Noah pod identity")
	assert.Equal(t, searchHeadStatefulSet.Spec.ServiceName, env[resources.NoahHeadlessServiceEnvName].Value, "advertised identity must derive from the search-head's own headless service, not Pod IP")
	assert.Equal(t, "corp.example", env[resources.ClusterDomainEnvName].Value)
	require.NotNil(t, env[resources.PodNameEnvName].ValueFrom, "identity must survive Pod IP changes via the downward API, not a literal IP")
	require.NotNil(t, env[resources.PodNamespaceEnvName].ValueFrom)
	assert.Equal(t, "127.0.0.1", env["SPLUNK_DEPLOYER_URL"].Value,
		"SPLUNK_DEPLOYER_URL must point at the local splunkd, not a Kubernetes Service (the deleted deployer's or even the search-head's own), so splunk-ansible's bootstrap check resolves instantly instead of hitting zero ready endpoints and burning its full retry budget")
}

// A failure while removing owner references during deletion must abort
// immediately rather than fall through to CheckForDeletion, which removes
// finalizers once its own callbacks succeed. Swallowing the error here would
// let the finalizer disappear while owner references are still orphaned,
// with nothing left to retry the cleanup.
func TestApplySearchHeadClusterNoah_DeletionAbortsOnOwnerReferenceCleanupError(t *testing.T) {
	ctx := t.Context()
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
	resources.SetVolumeDefaults(&cr.Spec.CommonSplunkSpec)

	_, err := ApplySearchHeadClusterNoah(ctx, client, cr)
	require.Error(t, err)
	assert.Contains(t, cr.GetFinalizers(), "enterprise.splunk.com/delete-pvc", "finalizer must survive an owner-reference cleanup failure so deletion can retry")
}

// When the referenced NoahCluster does not exist yet, the reconcile must
// requeue as PhasePending rather than error — the CR may simply not have
// been created yet.
func TestApplySearchHeadClusterNoah_PendingWhenNoahClusterMissing(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	client := spltest.NewMockClient()
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "shc", Namespace: "test", Generation: 4},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			Replicas:       3,
			NoahClusterRef: &corev1.LocalObjectReference{Name: "missing-noah-cluster"},
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{Image: "splunk/splunk:latest"},
			},
		},
	}
	resources.SetVolumeDefaults(&cr.Spec.CommonSplunkSpec)
	require.NoError(t, client.Create(t.Context(), cr.DeepCopy()))

	result, err := ApplySearchHeadClusterNoah(t.Context(), client, cr)

	require.NoError(t, err)
	assert.True(t, result.Requeue)
	assert.Equal(t, enterpriseApi.PhasePending, cr.Status.Phase)
	assert.Equal(t, enterpriseApi.PhaseReady, cr.Status.DeployerPhase,
		"no deployer is ever deployed on the Noah path, so a search-head-side Noah dependency failure must not make status.deployerPhase falsely report Pending for a resource that was never even attempted")
	condition := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionReady)
	require.NotNil(t, condition)
	assert.Contains(t, condition.Message, "missing-noah-cluster")

	dependency := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahDependencyResolved)
	require.NotNil(t, dependency)
	assert.Equal(t, metav1.ConditionUnknown, dependency.Status)
	assert.Equal(t, string(enterpriseApi.ReasonNoahDependencyMissing), dependency.Reason)
	assert.Equal(t, int64(4), dependency.ObservedGeneration)
	assert.Contains(t, dependency.Message, "missing-noah-cluster")
	assert.Nil(t, splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahPeersReady),
		"search heads do not register as Noah peers")
}

func TestApplySearchHeadClusterNoah_ReportsUnknownDependencyReadFailure(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	client := spltest.NewMockClient()
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "shc", Namespace: "test", Generation: 3},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			Replicas:       3,
			NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{Image: "splunk/splunk:latest"},
			},
		},
	}
	resources.SetVolumeDefaults(&cr.Spec.CommonSplunkSpec)
	require.NoError(t, client.Create(t.Context(), cr.DeepCopy()))
	client.InduceErrorKind[splcommon.MockClientInduceErrorGet] = assert.AnError

	_, err := ApplySearchHeadClusterNoah(t.Context(), client, cr)
	require.Error(t, err)
	assert.Equal(t, enterpriseApi.PhaseError, cr.Status.Phase)

	condition := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahDependencyResolved)
	require.NotNil(t, condition)
	assert.Equal(t, metav1.ConditionUnknown, condition.Status)
	assert.Equal(t, string(enterpriseApi.ReasonNoahDependencyUnknown), condition.Reason)
	ready := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionReady)
	require.NotNil(t, ready)
	assert.Equal(t, "Reconciliation failed", ready.Message)
	for _, condition := range cr.Status.Conditions {
		assert.NotEmpty(t, condition.Type)
	}
}

func TestApplySearchHeadClusterNoah_ReportsResolvedDependency(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	client := spltest.NewMockClient()
	client.AddObject(noahClusterForSHCTest("test", "noah", "noah-auth"))
	client.AddObject(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noah-auth", Namespace: "test"},
		Data:       map[string][]byte{configworkflow.NoahAuthSecretKey: []byte(t.Name())},
	})
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "shc", Namespace: "test", Generation: 9},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			Replicas:       3,
			NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{Image: "splunk/splunk:latest"},
			},
		},
	}
	resources.SetVolumeDefaults(&cr.Spec.CommonSplunkSpec)
	require.NoError(t, client.Create(t.Context(), cr.DeepCopy()))

	result, err := ApplySearchHeadClusterNoah(t.Context(), client, cr)
	require.NoError(t, err)
	assert.True(t, result.Requeue)

	condition := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahDependencyResolved)
	require.NotNil(t, condition)
	assert.Equal(t, metav1.ConditionTrue, condition.Status)
	assert.Equal(t, string(enterpriseApi.ReasonNoahDependencyResolved), condition.Reason)
	assert.Equal(t, int64(9), condition.ObservedGeneration)
}

func TestApplySearchHeadClusterNoah_PendingWhenAuthSecretMissing(t *testing.T) {
	client := spltest.NewMockClient()
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "shc", Namespace: "test"},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
		},
	}
	require.NoError(t, client.Create(t.Context(), &enterpriseApi.NoahCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "noah", Namespace: cr.Namespace},
		Spec: enterpriseApi.NoahClusterSpec{
			Endpoint:      "https://noah.test.svc",
			Tenant:        "tenant",
			AuthSecretRef: corev1.LocalObjectReference{Name: "missing-auth"},
		},
	}))
	client.ResetCalls()

	dependency := reconcileutil.ResolveNoahDependency(t.Context(), client, cr, &cr.Status.Conditions, cr.Spec.NoahClusterRef)

	assert.Equal(t, enterpriseApi.PhasePending, dependency.Phase)
	assert.NoError(t, dependency.ReconcileErr)
	assert.Contains(t, dependency.Message, "missing-auth")
	assert.Nil(t, dependency.Runtime, "an unresolved dependency must not yield a runtime to reconcile with")
	assert.Empty(t, client.Calls["Create"], "missing Noah dependencies must not partially create workload resources")
}

func TestApplySearchHeadClusterNoah_ValidatesRuntimeBeforeCreatingResources(t *testing.T) {
	client := spltest.NewMockClient()
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "shc", Namespace: "test"},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
		},
	}
	require.NoError(t, client.Create(t.Context(), &enterpriseApi.NoahCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "noah", Namespace: cr.Namespace},
		Spec: enterpriseApi.NoahClusterSpec{
			Endpoint:      "https://noah.test.svc",
			Tenant:        "tenant",
			AuthSecretRef: corev1.LocalObjectReference{Name: "noah-auth"},
		},
	}))
	require.NoError(t, client.Create(t.Context(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noah-auth", Namespace: cr.Namespace},
		Data:       map[string][]byte{"wrong-key": []byte("unit-test-noah-key")},
	}))
	client.ResetCalls()

	dependency := reconcileutil.ResolveNoahDependency(t.Context(), client, cr, &cr.Status.Conditions, cr.Spec.NoahClusterRef)

	assert.Nil(t, dependency.Runtime, "an invalid credential must not yield a runtime to reconcile with")
	require.Error(t, dependency.ReconcileErr)
	_, terminal := splcommon.TerminalMessage(dependency.ReconcileErr)
	assert.True(t, terminal)
	reason, _ := splcommon.TerminalReason(dependency.ReconcileErr)
	assert.Equal(t, splcommon.EventReasonNoahConfigurationInvalid, reason)
	condition := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahDependencyResolved)
	require.NotNil(t, condition)
	assert.Equal(t, string(enterpriseApi.ReasonNoahConfigurationInvalid), condition.Reason)
	assert.Contains(t, dependency.Message, configworkflow.NoahAuthSecretKey)
	assert.Empty(t, client.Calls["Create"], "invalid Noah configuration must fail before creating workload resources")
}
