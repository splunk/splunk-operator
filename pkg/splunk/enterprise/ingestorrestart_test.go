// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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
	"fmt"
	"log/slog"
	"reflect"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func newRestartTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = enterpriseApi.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	_ = policyv1.AddToScheme(s)
	return s
}

func newRestartTestSTS(crName, namespace, currentRevision, updateRevision string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(SplunkIngestor, crName),
			Namespace: namespace,
		},
		Status: appsv1.StatefulSetStatus{
			CurrentRevision: currentRevision,
			UpdateRevision:  updateRevision,
		},
	}
}

func newRestartTestCR(name, namespace string, replicas int32) *enterpriseApi.IngestorCluster {
	return &enterpriseApi.IngestorCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       enterpriseApi.IngestorClusterSpec{Replicas: replicas},
	}
}

// newRestartTestPod returns a running+ready pod. Pass a non-nil deletionTimestamp to simulate termination.
func newRestartTestPod(name, namespace, crName string, deletionTimestamp *metav1.Time) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/instance": "splunk-" + crName + "-ingestor",
				"controller-revision-hash":   "rev-1",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
	if deletionTimestamp != nil {
		pod.Finalizers = []string{"test/keep"}
		pod.DeletionTimestamp = deletionTimestamp
	}
	return pod
}

// withChecker overrides MakeRestartRequiredChecker for the duration of a test and restores it on cleanup.
func withChecker(t *testing.T, checker RestartRequiredChecker) {
	t.Helper()
	orig := MakeRestartRequiredChecker
	MakeRestartRequiredChecker = func(_ splcommon.ControllerClient, _ *enterpriseApi.IngestorCluster) RestartRequiredChecker {
		return checker
	}
	t.Cleanup(func() { MakeRestartRequiredChecker = orig })
}

// findCondition is a test helper that looks up a condition by type.
func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

// TestRunRollingEviction_EvictsAllCandidatesUntilPDBBlocks verifies that all candidates are
// evicted in ordinal order when the PDB allows it, and the result carries a retry interval.
func TestRunRollingEviction_EvictsAllCandidatesUntilPDBBlocks(t *testing.T) {
	scheme := newRestartTestScheme()
	cr := newRestartTestCR("test", "ns", 3)
	pod0 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 0), "ns", "test", nil)
	pod1 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 1), "ns", "test", nil)
	pod2 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 2), "ns", "test", nil)

	withChecker(t, func(_ context.Context, _ int32) (bool, error) { return true, nil })

	var evicted []string
	c := newFakeClientBuilder(scheme).
		WithObjects(cr, newRestartTestSTS("test", "ns", "rev-1", "rev-1"), pod0, pod1, pod2).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceCreate: func(_ context.Context, _ client.Client, _ string, obj client.Object, _ client.Object, _ ...client.SubResourceCreateOption) error {
				evicted = append(evicted, obj.GetName())
				return nil
			},
		}).
		Build()

	result, err := RunRollingEviction(context.Background(), c, cr, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []string{
		GetSplunkStatefulsetPodName(SplunkIngestor, "test", 0),
		GetSplunkStatefulsetPodName(SplunkIngestor, "test", 1),
		GetSplunkStatefulsetPodName(SplunkIngestor, "test", 2),
	}
	if !reflect.DeepEqual(evicted, expected) {
		t.Errorf("expected all pods evicted in order %v, got %v", expected, evicted)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected non-zero RequeueAfter")
	}
}

// TestRunRollingEviction_StopsAtPDBBlock verifies that eviction stops as soon as the PDB
// blocks, leaving remaining candidates unevicted.
func TestRunRollingEviction_StopsAtPDBBlock(t *testing.T) {
	scheme := newRestartTestScheme()
	cr := newRestartTestCR("test", "ns", 3)
	pod0 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 0), "ns", "test", nil)
	pod1 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 1), "ns", "test", nil)
	pod2 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 2), "ns", "test", nil)

	withChecker(t, func(_ context.Context, _ int32) (bool, error) { return true, nil })

	var evicted []string
	c := newFakeClientBuilder(scheme).
		WithObjects(cr, newRestartTestSTS("test", "ns", "rev-1", "rev-1"), pod0, pod1, pod2).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceCreate: func(_ context.Context, _ client.Client, _ string, obj client.Object, _ client.Object, _ ...client.SubResourceCreateOption) error {
				// Allow pod-0, block pod-1 with PDB.
				if obj.GetName() == GetSplunkStatefulsetPodName(SplunkIngestor, "test", 1) {
					return k8serrors.NewTooManyRequests("pdb", 0)
				}
				evicted = append(evicted, obj.GetName())
				return nil
			},
		}).
		Build()

	result, err := RunRollingEviction(context.Background(), c, cr, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(evicted) != 1 || evicted[0] != GetSplunkStatefulsetPodName(SplunkIngestor, "test", 0) {
		t.Errorf("expected only pod-0 evicted before PDB block, got %v", evicted)
	}
	if result.RequeueAfter != pdbRetryInterval {
		t.Errorf("expected pdbRetryInterval (%v), got %v", pdbRetryInterval, result.RequeueAfter)
	}
	cond := findCondition(cr.Status.Conditions, string(enterpriseApi.ConditionRestarting))
	if cond == nil || cond.Reason != string(enterpriseApi.ReasonRestartBlockedByPDB) {
		t.Errorf("expected RestartBlockedByPDB condition, got %+v", cond)
	}
}

// TestRunRollingEviction_PDB429ReturnsLongerRequeue verifies that a PDB block sets the condition
// and returns pdbRetryInterval.
func TestRunRollingEviction_PDB429ReturnsLongerRequeue(t *testing.T) {
	scheme := newRestartTestScheme()
	cr := newRestartTestCR("test", "ns", 2)
	pod0 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 0), "ns", "test", nil)
	pod1 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 1), "ns", "test", nil)

	withChecker(t, func(_ context.Context, _ int32) (bool, error) { return true, nil })

	c := newFakeClientBuilder(scheme).
		WithObjects(cr, newRestartTestSTS("test", "ns", "rev-1", "rev-1"), pod0, pod1).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceCreate: func(_ context.Context, _ client.Client, _ string, _ client.Object, _ client.Object, _ ...client.SubResourceCreateOption) error {
				return k8serrors.NewTooManyRequests("pdb", 0)
			},
		}).
		Build()

	result, err := RunRollingEviction(context.Background(), c, cr, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != pdbRetryInterval {
		t.Errorf("expected pdbRetryInterval (%v), got %v", pdbRetryInterval, result.RequeueAfter)
	}
	cond := findCondition(cr.Status.Conditions, string(enterpriseApi.ConditionRestarting))
	if cond == nil || cond.Reason != string(enterpriseApi.ReasonRestartBlockedByPDB) {
		t.Errorf("expected RestartBlockedByPDB condition, got %+v", cond)
	}
}

// TestRunRollingEviction_NoneNeedRestart verifies poll interval and no evictions when no pods need restart.
func TestRunRollingEviction_NoneNeedRestart(t *testing.T) {
	scheme := newRestartTestScheme()
	cr := newRestartTestCR("test", "ns", 2)
	pod0 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 0), "ns", "test", nil)
	pod1 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 1), "ns", "test", nil)

	withChecker(t, func(_ context.Context, _ int32) (bool, error) { return false, nil })

	evicted := 0
	c := newFakeClientBuilder(scheme).
		WithObjects(cr, newRestartTestSTS("test", "ns", "rev-1", "rev-1"), pod0, pod1).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceCreate: func(_ context.Context, _ client.Client, _ string, _ client.Object, _ client.Object, _ ...client.SubResourceCreateOption) error {
				evicted++
				return nil
			},
		}).
		Build()

	result, err := RunRollingEviction(context.Background(), c, cr, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != restartPollInterval {
		t.Errorf("expected restartPollInterval (%v), got %v", restartPollInterval, result.RequeueAfter)
	}
	if evicted != 0 {
		t.Errorf("expected no evictions, got %d", evicted)
	}
}

// TestRunRollingEviction_CheckerErrorContinues verifies that a checker error for one pod is
// skipped and others are still evaluated.
func TestRunRollingEviction_CheckerErrorContinues(t *testing.T) {
	scheme := newRestartTestScheme()
	cr := newRestartTestCR("test", "ns", 3)
	pod0 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 0), "ns", "test", nil)
	pod1 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 1), "ns", "test", nil)
	pod2 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 2), "ns", "test", nil)

	// Ordinal 0 errors, ordinal 1 needs restart, ordinal 2 does not.
	withChecker(t, func(_ context.Context, n int32) (bool, error) {
		switch n {
		case 0:
			return false, fmt.Errorf("API error")
		case 1:
			return true, nil
		default:
			return false, nil
		}
	})

	var evicted []string
	c := newFakeClientBuilder(scheme).
		WithObjects(cr, newRestartTestSTS("test", "ns", "rev-1", "rev-1"), pod0, pod1, pod2).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceCreate: func(_ context.Context, _ client.Client, _ string, obj client.Object, _ client.Object, _ ...client.SubResourceCreateOption) error {
				evicted = append(evicted, obj.GetName())
				return nil
			},
		}).
		Build()

	_, err := RunRollingEviction(context.Background(), c, cr, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(evicted) != 1 || evicted[0] != GetSplunkStatefulsetPodName(SplunkIngestor, "test", 1) {
		t.Errorf("expected pod-1 evicted, got %v", evicted)
	}
}

// TestRunRollingEviction_AllCheckersFail verifies RestartCheckIncomplete condition and retry interval.
func TestRunRollingEviction_AllCheckersFail(t *testing.T) {
	scheme := newRestartTestScheme()
	cr := newRestartTestCR("test", "ns", 2)
	pod0 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 0), "ns", "test", nil)
	pod1 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 1), "ns", "test", nil)

	withChecker(t, func(_ context.Context, _ int32) (bool, error) { return false, fmt.Errorf("unreachable") })

	c := newFakeClientBuilder(scheme).WithObjects(cr, newRestartTestSTS("test", "ns", "rev-1", "rev-1"), pod0, pod1).Build()

	result, err := RunRollingEviction(context.Background(), c, cr, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != restartRetryInterval {
		t.Errorf("expected restartRetryInterval, got %v", result.RequeueAfter)
	}
	cond := findCondition(cr.Status.Conditions, string(enterpriseApi.ConditionRestarting))
	if cond == nil || cond.Reason != string(enterpriseApi.ReasonRestartCheckIncomplete) {
		t.Errorf("expected RestartCheckIncomplete condition, got %+v", cond)
	}
}

// TestRunRollingEviction_PartialCheckFailureNoCandidate verifies RestartCheckIncomplete condition.
func TestRunRollingEviction_PartialCheckFailureNoCandidate(t *testing.T) {
	scheme := newRestartTestScheme()
	cr := newRestartTestCR("test", "ns", 2)
	pod0 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 0), "ns", "test", nil)
	pod1 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 1), "ns", "test", nil)

	// Ordinal 0 errors, ordinal 1 reports no restart — no candidate but one failure.
	withChecker(t, func(_ context.Context, n int32) (bool, error) {
		if n == 0 {
			return false, fmt.Errorf("unreachable")
		}
		return false, nil
	})

	c := newFakeClientBuilder(scheme).WithObjects(cr, newRestartTestSTS("test", "ns", "rev-1", "rev-1"), pod0, pod1).Build()

	result, err := RunRollingEviction(context.Background(), c, cr, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != restartRetryInterval {
		t.Errorf("expected restartRetryInterval, got %v", result.RequeueAfter)
	}
	cond := findCondition(cr.Status.Conditions, string(enterpriseApi.ConditionRestarting))
	if cond == nil || cond.Reason != string(enterpriseApi.ReasonRestartCheckIncomplete) {
		t.Errorf("expected RestartCheckIncomplete condition, got %+v", cond)
	}
}

// TestRunRollingEviction_NonPDB429PropagatesError verifies that a non-429 eviction error
// is propagated as a reconcile error.
func TestRunRollingEviction_NonPDB429PropagatesError(t *testing.T) {
	scheme := newRestartTestScheme()
	cr := newRestartTestCR("test", "ns", 2)
	pod0 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 0), "ns", "test", nil)
	pod1 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 1), "ns", "test", nil)

	withChecker(t, func(_ context.Context, _ int32) (bool, error) { return true, nil })

	c := newFakeClientBuilder(scheme).
		WithObjects(cr, newRestartTestSTS("test", "ns", "rev-1", "rev-1"), pod0, pod1).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceCreate: func(_ context.Context, _ client.Client, _ string, _ client.Object, _ client.Object, _ ...client.SubResourceCreateOption) error {
				return fmt.Errorf("eviction API unavailable")
			},
		}).
		Build()

	result, err := RunRollingEviction(context.Background(), c, cr, slog.Default())
	if err == nil {
		t.Fatal("expected non-429 eviction error to be propagated")
	}
	if result.RequeueAfter != restartRetryInterval {
		t.Errorf("expected restartRetryInterval, got %v", result.RequeueAfter)
	}
}

// TestRunRollingEviction_AlreadyTerminatingSkipped verifies that a 404 or 409 eviction error
// (pod already terminating or gone) is treated as success and eviction continues to the next candidate.
func TestRunRollingEviction_AlreadyTerminatingSkipped(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"NotFound", k8serrors.NewNotFound(policyv1.Resource("pods"), "splunk-test-ingestor-0")},
		{"Conflict", k8serrors.NewConflict(policyv1.Resource("pods"), "splunk-test-ingestor-0", fmt.Errorf("already deleting"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scheme := newRestartTestScheme()
			cr := newRestartTestCR("test", "ns", 2)
			pod0 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 0), "ns", "test", nil)
			pod1 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 1), "ns", "test", nil)

			withChecker(t, func(_ context.Context, _ int32) (bool, error) { return true, nil })

			evictErr := tc.err
			var evicted []string
			c := newFakeClientBuilder(scheme).
				WithObjects(cr, newRestartTestSTS("test", "ns", "rev-1", "rev-1"), pod0, pod1).
				WithInterceptorFuncs(interceptor.Funcs{
					SubResourceCreate: func(_ context.Context, _ client.Client, _ string, obj client.Object, _ client.Object, _ ...client.SubResourceCreateOption) error {
						if obj.GetName() == GetSplunkStatefulsetPodName(SplunkIngestor, "test", 0) {
							return evictErr
						}
						evicted = append(evicted, obj.GetName())
						return nil
					},
				}).
				Build()

			_, err := RunRollingEviction(context.Background(), c, cr, slog.Default())
			if err != nil {
				t.Fatalf("expected no error for %s, got %v", tc.name, err)
			}
			expected := []string{GetSplunkStatefulsetPodName(SplunkIngestor, "test", 1)}
			if !reflect.DeepEqual(evicted, expected) {
				t.Errorf("expected %v evicted, got %v", expected, evicted)
			}
		})
	}
}

// TestRunRollingEviction_TerminatingPodCheckerFails verifies that a terminating pod's
// restart_required check fails (Splunk REST is down), counts as failedChecks, and the
// remaining ready pods that need restart are still evicted.
func TestRunRollingEviction_TerminatingPodCheckerFails(t *testing.T) {
	scheme := newRestartTestScheme()
	cr := newRestartTestCR("test", "ns", 3)
	now := metav1.Now()
	pod0 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 0), "ns", "test", nil)
	pod1 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 1), "ns", "test", nil)
	pod2 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 2), "ns", "test", &now)

	// pod-2 (terminating) fails the check; pod-0 and pod-1 need restart.
	withChecker(t, func(_ context.Context, n int32) (bool, error) {
		if n == 2 {
			return false, fmt.Errorf("connection refused")
		}
		return true, nil
	})

	var evicted []string
	c := newFakeClientBuilder(scheme).
		WithObjects(cr, newRestartTestSTS("test", "ns", "rev-1", "rev-1"), pod0, pod1, pod2).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceCreate: func(_ context.Context, _ client.Client, _ string, obj client.Object, _ client.Object, _ ...client.SubResourceCreateOption) error {
				evicted = append(evicted, obj.GetName())
				return nil
			},
		}).
		Build()

	_, err := RunRollingEviction(context.Background(), c, cr, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// pod-2 check failed (terminating); pod-0 and pod-1 are evicted.
	expected := []string{
		GetSplunkStatefulsetPodName(SplunkIngestor, "test", 0),
		GetSplunkStatefulsetPodName(SplunkIngestor, "test", 1),
	}
	if !reflect.DeepEqual(evicted, expected) {
		t.Errorf("expected %v evicted, got %v", expected, evicted)
	}
}

// TestRunRollingEviction_MarkCompleteWhenPreviouslyActive verifies the Restarting condition
// transitions to False/RollingRestartComplete when no pods need restart and it was previously active.
func TestRunRollingEviction_MarkCompleteWhenPreviouslyActive(t *testing.T) {
	scheme := newRestartTestScheme()
	cr := newRestartTestCR("test", "ns", 2)
	cr.Status.Conditions = []metav1.Condition{
		{
			Type:   string(enterpriseApi.ConditionRestarting),
			Status: metav1.ConditionTrue,
			Reason: string(enterpriseApi.ReasonRollingRestartInProgress),
		},
	}
	pod0 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 0), "ns", "test", nil)
	pod1 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 1), "ns", "test", nil)

	withChecker(t, func(_ context.Context, _ int32) (bool, error) { return false, nil })

	c := newFakeClientBuilder(scheme).WithObjects(cr, newRestartTestSTS("test", "ns", "rev-1", "rev-1"), pod0, pod1).Build()

	_, err := RunRollingEviction(context.Background(), c, cr, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cond := findCondition(cr.Status.Conditions, string(enterpriseApi.ConditionRestarting))
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != string(enterpriseApi.ReasonRollingRestartComplete) {
		t.Errorf("expected RollingRestartComplete condition, got %+v", cond)
	}
}

// TestRunRollingEviction_DefersWhenRolloutInProgress verifies that eviction is deferred
// when UpdateRevision != CurrentRevision (StatefulSet spec change in progress).
func TestRunRollingEviction_DefersWhenRolloutInProgress(t *testing.T) {
	scheme := newRestartTestScheme()
	cr := newRestartTestCR("test", "ns", 2)
	pod0 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 0), "ns", "test", nil)
	pod1 := newRestartTestPod(GetSplunkStatefulsetPodName(SplunkIngestor, "test", 1), "ns", "test", nil)

	withChecker(t, func(_ context.Context, _ int32) (bool, error) { return true, nil })

	evicted := 0
	c := newFakeClientBuilder(scheme).
		WithObjects(cr, newRestartTestSTS("test", "ns", "rev-1", "rev-2"), pod0, pod1).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceCreate: func(_ context.Context, _ client.Client, _ string, _ client.Object, _ client.Object, _ ...client.SubResourceCreateOption) error {
				evicted++
				return nil
			},
		}).
		Build()

	result, err := RunRollingEviction(context.Background(), c, cr, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evicted != 0 {
		t.Errorf("expected no evictions during rollout, got %d", evicted)
	}
	if result.RequeueAfter != restartRetryInterval {
		t.Errorf("expected restartRetryInterval (%v), got %v", restartRetryInterval, result.RequeueAfter)
	}
}
