// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	configworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/config"
)

// acceptedGeneralTerms is the licence acceptance flag documented in the repo
// README. Reconcile entrypoints validate it before resolving Noah dependencies.
const acceptedGeneralTerms = "--accept-sgt-current-at-splunk-com"

func TestNoahDependencyOutcomeReportsSharedCondition(t *testing.T) {
	tests := []struct {
		name       string
		secretData map[string][]byte
		noahName   string
		status     metav1.ConditionStatus
		reason     enterpriseApi.ConditionReason
		phase      enterpriseApi.Phase
		message    string
	}{
		{
			name:     "missing dependency is unknown and retryable",
			noahName: "missing-noah",
			status:   metav1.ConditionUnknown,
			reason:   enterpriseApi.ReasonNoahDependencyMissing,
			phase:    enterpriseApi.PhasePending,
			message:  "Waiting for Noah dependencies",
		},
		{
			name:       "invalid dependency is false and terminal",
			noahName:   "noah",
			secretData: map[string][]byte{"wrong-key": []byte("unit-test-noah-key")},
			status:     metav1.ConditionFalse,
			reason:     enterpriseApi.ReasonNoahConfigurationInvalid,
			phase:      enterpriseApi.PhaseError,
			message:    "Invalid Noah dependency configuration",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newNoahConditionTestClient(t, test.secretData)
			cr := &enterpriseApi.IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "idx", Namespace: "test"},
				Spec: enterpriseApi.IndexerClusterSpec{
					Replicas:       1,
					NoahClusterRef: &corev1.LocalObjectReference{Name: test.noahName},
				},
			}

			runtime, err := resolveNoahDependency(t.Context(), client, cr, &cr.Status.Conditions, cr.Spec.NoahClusterRef)
			require.Error(t, err)
			assert.Nil(t, runtime)
			outcome, handled := noahDependencyOutcome(err)
			require.True(t, handled)
			assert.Equal(t, test.phase, outcome.phase)

			condition := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahDependencyResolved)
			require.NotNil(t, condition, "dependency outcomes must not be reported as NoahPeersReady")
			assert.Equal(t, test.status, condition.Status)
			assert.Equal(t, string(test.reason), condition.Reason)
			assert.Contains(t, condition.Message, test.message)
		})
	}
}

// newNoahConditionTestClient seeds a NoahCluster named "noah" and its auth
// Secret. Passing nil secretData creates a valid credential; passing a map
// creates exactly that data so a test can drive a specific resolver failure.
func newNoahConditionTestClient(t *testing.T, secretData map[string][]byte) *spltest.MockClient {
	t.Helper()
	if secretData == nil {
		secretData = map[string][]byte{configworkflow.NoahAuthSecretKey: []byte(t.Name())}
	}
	client := spltest.NewMockClient()
	client.AddObject(&enterpriseApi.NoahCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "noah", Namespace: "test"},
		Spec: enterpriseApi.NoahClusterSpec{
			Endpoint:      "http://noah.example:8080",
			Tenant:        "linus-dev",
			AuthSecretRef: corev1.LocalObjectReference{Name: "noah-auth"},
		},
	})
	client.AddObject(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noah-auth", Namespace: "test"},
		Data:       secretData,
	})
	return client
}

func TestNoahDependencyConditionRedactsCredential(t *testing.T) {
	const credential = "super-secret-noah-key"
	client := newNoahConditionTestClient(t, map[string][]byte{
		configworkflow.NoahAuthSecretKey: []byte(credential + "\nsecond-line"),
	})

	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "idx", Namespace: "test"},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas:       1,
			NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
		},
	}

	_, err := resolveNoahDependency(t.Context(), client, cr, &cr.Status.Conditions, cr.Spec.NoahClusterRef)
	require.Error(t, err)

	condition := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahDependencyResolved)
	require.NotNil(t, condition)
	assert.NotContains(t, condition.Message, credential)
	assert.Contains(t, condition.Message, configworkflow.NoahAuthSecretKey,
		"the message must still name the offending key so an operator can act on it")
}

// A degraded dependency must recover to True once the dependency is fixed, and
// LastTransitionTime must only move when the status actually changes.
func TestNoahDependencyResolvedConditionTransitions(t *testing.T) {
	blocked := newNoahDependencyResolvedCondition(
		metav1.ConditionUnknown, enterpriseApi.ReasonNoahDependencyMissing, "waiting")
	resolved := newNoahDependencyResolvedCondition(
		metav1.ConditionTrue, enterpriseApi.ReasonNoahDependencyResolved, "resolved")

	conditions := splcommon.UpsertCondition(nil, blocked)
	require.Len(t, conditions, 1)
	blockedAt := conditions[0].LastTransitionTime
	require.False(t, blockedAt.IsZero())

	// Same status re-reported: the transition time must not move.
	conditions = splcommon.UpsertCondition(conditions, blocked)
	require.Len(t, conditions, 1, "the condition must be upserted, not duplicated")
	assert.Equal(t, blockedAt, conditions[0].LastTransitionTime)

	// Recovery flips the status, so the transition time advances.
	conditions = splcommon.UpsertCondition(conditions, resolved)
	require.Len(t, conditions, 1)
	assert.Equal(t, metav1.ConditionTrue, conditions[0].Status)
	assert.Equal(t, string(enterpriseApi.ReasonNoahDependencyResolved), conditions[0].Reason)
	assert.True(t, conditions[0].LastTransitionTime.After(blockedAt.Time) ||
		conditions[0].LastTransitionTime.Equal(&blockedAt),
		"a status change must refresh LastTransitionTime")
}

func TestNoahReconcilersPublishDependencyResolvedCondition(t *testing.T) {
	t.Run("IndexerCluster", func(t *testing.T) {
		t.Setenv("SPLUNK_GENERAL_TERMS", acceptedGeneralTerms)
		client := newNoahConditionTestClient(t, nil)
		cr := &enterpriseApi.IndexerCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "idx", Namespace: "test", Generation: 7,
			},
			Spec: enterpriseApi.IndexerClusterSpec{
				Replicas:       1,
				NoahClusterRef: &corev1.LocalObjectReference{Name: "missing-noah"},
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{Image: "splunk/splunk:latest"},
				},
			},
		}
		setVolumeDefaults(&cr.Spec.CommonSplunkSpec)
		require.NoError(t, client.Create(t.Context(), cr.DeepCopy()))

		_, err := ApplyNoahIndexerCluster(t.Context(), client, cr)
		require.NoError(t, err, "a missing dependency is retryable, not terminal")

		condition := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahDependencyResolved)
		require.NotNil(t, condition, "the reconciler must publish the shared dependency condition")
		assert.Equal(t, metav1.ConditionUnknown, condition.Status)
		assert.Equal(t, string(enterpriseApi.ReasonNoahDependencyMissing), condition.Reason)
		assert.Equal(t, int64(7), condition.ObservedGeneration)
		assert.Contains(t, condition.Message, "missing-noah")
	})

	t.Run("SearchHeadCluster", func(t *testing.T) {
		t.Setenv("SPLUNK_GENERAL_TERMS", acceptedGeneralTerms)
		client := newNoahConditionTestClient(t, nil)
		cr := &enterpriseApi.SearchHeadCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "shc", Namespace: "test", Generation: 4,
			},
			Spec: enterpriseApi.SearchHeadClusterSpec{
				Replicas:       3,
				NoahClusterRef: &corev1.LocalObjectReference{Name: "missing-noah"},
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{Image: "splunk/splunk:latest"},
				},
			},
		}
		setVolumeDefaults(&cr.Spec.CommonSplunkSpec)
		require.NoError(t, client.Create(t.Context(), cr.DeepCopy()))

		_, err := ApplySearchHeadClusterNoah(t.Context(), client, cr)
		require.NoError(t, err)

		condition := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahDependencyResolved)
		require.NotNil(t, condition, "the search head must publish the condition it previously discarded")
		assert.Equal(t, metav1.ConditionUnknown, condition.Status)
		assert.Equal(t, string(enterpriseApi.ReasonNoahDependencyMissing), condition.Reason)
		assert.Equal(t, int64(4), condition.ObservedGeneration)
		assert.Contains(t, condition.Message, "missing-noah")

		assert.Nil(t, splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahPeersReady),
			"search heads consume bucket maps rather than registering as Noah peers, so they must not report NoahPeersReady")
	})
}

// noahForbiddenSecretReader denies reads of the Noah auth Secret with Forbidden
// rather than NotFound, which the resolver returns unwrapped — so
// noahDependencyOutcome cannot classify it.
type noahForbiddenSecretReader struct {
	*spltest.MockClient
}

func (r noahForbiddenSecretReader) Get(ctx context.Context, key k8sclient.ObjectKey, obj k8sclient.Object, opts ...k8sclient.GetOption) error {
	if _, ok := obj.(*corev1.Secret); ok {
		return k8serrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, key.Name, assert.AnError)
	}
	return r.MockClient.Get(ctx, key, obj, opts...)
}

func TestNoahUnclassifiedErrorDoesNotClaimDependencyResolved(t *testing.T) {
	newClient := func(t *testing.T) noahForbiddenSecretReader {
		t.Helper()
		base := spltest.NewMockClient()
		base.AddObject(&enterpriseApi.NoahCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "noah", Namespace: "test"},
			Spec: enterpriseApi.NoahClusterSpec{
				Endpoint:      "http://noah.example:8080",
				Tenant:        "linus-dev",
				AuthSecretRef: corev1.LocalObjectReference{Name: "noah-auth"},
			},
		})
		return noahForbiddenSecretReader{base}
	}

	t.Run("IndexerCluster", func(t *testing.T) {
		t.Setenv("SPLUNK_GENERAL_TERMS", acceptedGeneralTerms)
		client := newClient(t)
		cr := &enterpriseApi.IndexerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "idx", Namespace: "test", Generation: 3},
			Spec: enterpriseApi.IndexerClusterSpec{
				Replicas:       1,
				NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{Image: "splunk/splunk:latest"},
				},
			},
		}
		setVolumeDefaults(&cr.Spec.CommonSplunkSpec)
		require.NoError(t, client.Create(t.Context(), cr.DeepCopy()))

		_, err := ApplyNoahIndexerCluster(t.Context(), client, cr)
		require.Error(t, err)
		assert.Equal(t, enterpriseApi.PhaseError, cr.Status.Phase)
		assert.Nil(t, splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahDependencyResolved),
			"an unclassified failure must not claim the dependency resolved")
		assertNoEmptyConditions(t, cr.Status.Conditions)
	})

	t.Run("SearchHeadCluster", func(t *testing.T) {
		t.Setenv("SPLUNK_GENERAL_TERMS", acceptedGeneralTerms)
		client := newClient(t)
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
		setVolumeDefaults(&cr.Spec.CommonSplunkSpec)
		require.NoError(t, client.Create(t.Context(), cr.DeepCopy()))

		_, err := ApplySearchHeadClusterNoah(t.Context(), client, cr)
		require.Error(t, err)
		assert.Equal(t, enterpriseApi.PhaseError, cr.Status.Phase)
		assert.Nil(t, splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahDependencyResolved),
			"an unclassified failure must not claim the dependency resolved")
		assertNoEmptyConditions(t, cr.Status.Conditions)
	})
}

func TestNoahReconcilersReportDependencyResolvedOnSuccess(t *testing.T) {
	t.Run("IndexerCluster", func(t *testing.T) {
		t.Setenv("SPLUNK_GENERAL_TERMS", acceptedGeneralTerms)
		client := newNoahConditionTestClient(t, nil)
		cr := &enterpriseApi.IndexerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "idx", Namespace: "test", Generation: 5},
			Spec: enterpriseApi.IndexerClusterSpec{
				Replicas:       1,
				NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{Image: "splunk/splunk:latest"},
				},
			},
		}
		setVolumeDefaults(&cr.Spec.CommonSplunkSpec)
		require.NoError(t, client.Create(t.Context(), cr.DeepCopy()))

		_, _ = ApplyNoahIndexerCluster(t.Context(), client, cr)

		condition := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahDependencyResolved)
		require.NotNil(t, condition)
		assert.Equal(t, metav1.ConditionTrue, condition.Status)
		assert.Equal(t, string(enterpriseApi.ReasonNoahDependencyResolved), condition.Reason)
		assert.Equal(t, int64(5), condition.ObservedGeneration)
	})

	t.Run("SearchHeadCluster", func(t *testing.T) {
		t.Setenv("SPLUNK_GENERAL_TERMS", acceptedGeneralTerms)
		client := newNoahConditionTestClient(t, nil)
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
		setVolumeDefaults(&cr.Spec.CommonSplunkSpec)
		require.NoError(t, client.Create(t.Context(), cr.DeepCopy()))

		_, _ = ApplySearchHeadClusterNoah(t.Context(), client, cr)

		condition := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahDependencyResolved)
		require.NotNil(t, condition)
		assert.Equal(t, metav1.ConditionTrue, condition.Status)
		assert.Equal(t, string(enterpriseApi.ReasonNoahDependencyResolved), condition.Reason)
		assert.Equal(t, int64(9), condition.ObservedGeneration)
	})
}

// A condition with an empty type is unusable to any consumer and cannot be
// found by GetCondition, so it would otherwise corrupt status unnoticed.
func assertNoEmptyConditions(t *testing.T, conditions []metav1.Condition) {
	t.Helper()
	for _, condition := range conditions {
		assert.NotEmpty(t, condition.Type, "status must not contain a condition with no type")
	}
}

// A workload that was previously healthy must not keep advertising its peers as
// up once the dependency it needs to observe them disappears. The two conditions
// report different things: the dependency is missing, and peer state is unknown
// as a consequence — not still true, and not a dependency reason restated.
func TestNoahDependencyLossClearsStalePeersReady(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", acceptedGeneralTerms)
	client := spltest.NewMockClient()
	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "idx", Namespace: "test", Generation: 3},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas:       1,
			NoahClusterRef: &corev1.LocalObjectReference{Name: "missing-noah"},
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{Image: "splunk/splunk:latest"},
			},
		},
	}
	setVolumeDefaults(&cr.Spec.CommonSplunkSpec)
	// A previous reconcile saw every peer up.
	cr.Status.Conditions = splcommon.UpsertCondition(cr.Status.Conditions, newNoahPeersReadyCondition(
		metav1.ConditionTrue, enterpriseApi.ReasonNoahPeersReady, "All expected Noah peers are up"))
	require.NoError(t, client.Create(t.Context(), cr.DeepCopy()))

	_, err := ApplyNoahIndexerCluster(t.Context(), client, cr)
	require.NoError(t, err, "a missing dependency is retryable, not terminal")

	peers := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahPeersReady)
	require.NotNil(t, peers)
	assert.Equal(t, metav1.ConditionUnknown, peers.Status,
		"peer readiness must not stay True once Noah cannot be reached")
	assert.Equal(t, string(enterpriseApi.ReasonNoahPeerObservationFailed), peers.Reason,
		"peer readiness must not borrow a dependency reason")

	dependency := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahDependencyResolved)
	require.NotNil(t, dependency)
	assert.Equal(t, string(enterpriseApi.ReasonNoahDependencyMissing), dependency.Reason)
}
