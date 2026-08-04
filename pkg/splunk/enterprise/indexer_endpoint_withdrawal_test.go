// Copyright (c) 2026 Splunk Inc. All rights reserved.

package enterprise

import (
	"context"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIndexerEndpointWithdrawalFailsClosed(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	mgr, _, pods := indexerLifecycleFixture(t)
	target := pods[2]
	target.Status.Conditions[0].Status = corev1.ConditionFalse

	ready := true
	setIndexerEndpointSliceForWithdrawal(mgr, target, &ready)
	withdrawn, err := mgr.indexerEndpointWithdrawalObserved(
		context.Background(),
		target,
	)
	require.NoError(t, err)
	require.False(t, withdrawn)

	setIndexerEndpointSliceForWithdrawal(mgr, target, nil)
	withdrawn, err = mgr.indexerEndpointWithdrawalObserved(
		context.Background(),
		target,
	)
	require.NoError(t, err)
	require.False(t, withdrawn)

	ready = false
	setIndexerEndpointSliceForWithdrawal(mgr, target, &ready)
	withdrawn, err = mgr.indexerEndpointWithdrawalObserved(
		context.Background(),
		target,
	)
	require.NoError(t, err)
	require.True(t, withdrawn)

	target.Status.Conditions[0].Status = corev1.ConditionTrue
	withdrawn, err = mgr.indexerEndpointWithdrawalObserved(
		context.Background(),
		target,
	)
	require.NoError(t, err)
	require.False(t, withdrawn)
}

func TestIndexerEndpointWithdrawalPersistsDelayAndInvalidation(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	mgr, _, pods := indexerLifecycleFixture(t)
	target := pods[2]
	target.Status.Conditions[0].Status = corev1.ConditionFalse
	now := metav1.Now()
	delaySeconds := int64(30)
	mgr.cr.Spec.LifecyclePolicy = &enterpriseApi.IndexerClusterLifecyclePolicy{
		EndpointWithdrawalDelaySeconds: &delaySeconds,
	}
	mgr.cr.Status.PodUpdate = &enterpriseApi.IndexerClusterPodUpdateStatus{
		OperationID:        "operation",
		Stage:              enterpriseApi.IndexerClusterPodUpdateStageWithdrawingReadiness,
		TargetPod:          target.Name,
		TargetPodUID:       string(target.UID),
		TargetOrdinal:      2,
		SourceRevision:     "old",
		DesiredRevision:    "new",
		StartedAt:          &now,
		StageStartedAt:     &now,
		LastTransitionTime: &now,
	}

	ready := false
	setIndexerEndpointSliceForWithdrawal(mgr, target, &ready)
	elapsed, err := mgr.indexerEndpointWithdrawalDelayElapsed(
		context.Background(),
		target,
	)
	require.NoError(t, err)
	require.False(t, elapsed)
	operation := mgr.cr.Status.PodUpdate
	require.NotNil(t, operation.EndpointWithdrawalObservedAt)
	require.NotNil(t, operation.EndpointWithdrawalDeadline)
	require.Equal(
		t,
		30*time.Second,
		operation.EndpointWithdrawalDeadline.Sub(
			operation.EndpointWithdrawalObservedAt.Time,
		),
	)
	require.Equal(t, string(target.UID), operation.EndpointWithdrawalPodUID)
	require.Equal(t, int64(1), operation.EndpointWithdrawalSequence)

	elapsed, err = mgr.indexerEndpointWithdrawalDelayElapsed(
		context.Background(),
		target,
	)
	require.NoError(t, err)
	require.False(t, elapsed)
	require.Equal(
		t,
		"IndexerEndpointWithdrawalPropagationPending",
		operation.Reason,
	)

	ready = true
	setIndexerEndpointSliceForWithdrawal(mgr, target, &ready)
	elapsed, err = mgr.indexerEndpointWithdrawalDelayElapsed(
		context.Background(),
		target,
	)
	require.NoError(t, err)
	require.False(t, elapsed)
	require.Equal(t, int64(1), operation.EndpointWithdrawalInvalidatedSequence)

	ready = false
	setIndexerEndpointSliceForWithdrawal(mgr, target, &ready)
	elapsed, err = mgr.indexerEndpointWithdrawalDelayElapsed(
		context.Background(),
		target,
	)
	require.NoError(t, err)
	require.False(t, elapsed)
	require.Equal(t, int64(2), operation.EndpointWithdrawalSequence)

	observedAt := metav1.NewTime(time.Now().Add(-31 * time.Second))
	deadline := metav1.NewTime(time.Now().Add(-time.Second))
	operation.EndpointWithdrawalObservedAt = &observedAt
	operation.EndpointWithdrawalDeadline = &deadline
	elapsed, err = mgr.indexerEndpointWithdrawalDelayElapsed(
		context.Background(),
		target,
	)
	require.NoError(t, err)
	require.True(t, elapsed)
}

func TestIndexerEndpointWithdrawalPreservesEffectiveDeadline(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	mgr, _, pods := indexerLifecycleFixture(t)
	target := pods[2]
	target.Status.Conditions[0].Status = corev1.ConditionFalse
	now := metav1.NewTime(
		time.Date(2026, 8, 4, 13, 40, 32, 0, time.UTC),
	)
	delaySeconds := int64(30)
	mgr.cr.Spec.LifecyclePolicy = &enterpriseApi.IndexerClusterLifecyclePolicy{
		EndpointWithdrawalDelaySeconds: &delaySeconds,
	}
	mgr.cr.Status.PodUpdate = &enterpriseApi.IndexerClusterPodUpdateStatus{
		OperationID:        "operation",
		Stage:              enterpriseApi.IndexerClusterPodUpdateStageWithdrawingReadiness,
		TargetPod:          target.Name,
		TargetPodUID:       string(target.UID),
		TargetOrdinal:      2,
		SourceRevision:     "old",
		DesiredRevision:    "new",
		StartedAt:          &now,
		StageStartedAt:     &now,
		LastTransitionTime: &now,
	}

	oldNow := indexerEndpointWithdrawalNow
	t.Cleanup(func() {
		indexerEndpointWithdrawalNow = oldNow
	})
	currentTime := now.Time
	indexerEndpointWithdrawalNow = func() time.Time {
		return currentTime
	}
	ready := false
	setIndexerEndpointSliceForWithdrawal(mgr, target, &ready)

	elapsed, err := mgr.indexerEndpointWithdrawalDelayElapsed(
		context.Background(),
		target,
	)
	require.NoError(t, err)
	require.False(t, elapsed)
	originalObservedAt := mgr.cr.Status.PodUpdate.EndpointWithdrawalObservedAt.DeepCopy()
	originalDeadline := mgr.cr.Status.PodUpdate.EndpointWithdrawalDeadline.DeepCopy()

	// Simulate a controller restart after the customer changes the policy.
	// The effective deadline for this already-observed sequence is immutable.
	delaySeconds = 1
	currentTime = currentTime.Add(5 * time.Second)
	elapsed, err = mgr.indexerEndpointWithdrawalDelayElapsed(
		context.Background(),
		target,
	)
	require.NoError(t, err)
	require.False(t, elapsed)
	require.Equal(
		t,
		originalObservedAt,
		mgr.cr.Status.PodUpdate.EndpointWithdrawalObservedAt,
	)
	require.Equal(
		t,
		originalDeadline,
		mgr.cr.Status.PodUpdate.EndpointWithdrawalDeadline,
	)
}

func TestIndexerEndpointWithdrawalRejectsReplacementUID(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	mgr, _, pods := indexerLifecycleFixture(t)
	target := pods[2]
	now := metav1.Now()
	mgr.cr.Status.PodUpdate = &enterpriseApi.IndexerClusterPodUpdateStatus{
		OperationID:        "operation",
		Stage:              enterpriseApi.IndexerClusterPodUpdateStageWithdrawingReadiness,
		TargetPod:          target.Name,
		TargetPodUID:       string(target.UID),
		TargetOrdinal:      2,
		SourceRevision:     "old",
		DesiredRevision:    "new",
		StartedAt:          &now,
		StageStartedAt:     &now,
		LastTransitionTime: &now,
	}
	target.UID = "replacement-uid"
	target.Status.Conditions[0].Status = corev1.ConditionFalse

	elapsed, err := mgr.ensureIndexerEndpointWithdrawalBarrier(
		context.Background(),
	)
	require.ErrorContains(t, err, "does not match target")
	require.False(t, elapsed)
}

func TestIndexerControlledStateRecoveryWaitsForEndpointBarrier(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	mgr, _, pods := indexerLifecycleFixture(t)
	target := pods[2]
	target.Status.Conditions[0].Status = corev1.ConditionFalse
	target.Status.ContainerStatuses[0].Ready = false
	require.NoError(t, mgr.c.Update(context.Background(), target))
	now := metav1.NewTime(
		time.Date(2026, 8, 4, 13, 40, 32, 0, time.UTC),
	)
	mgr.cr.Status.PodUpdate = &enterpriseApi.IndexerClusterPodUpdateStatus{
		OperationID:        "operation",
		Stage:              enterpriseApi.IndexerClusterPodUpdateStageWithdrawingReadiness,
		TargetPod:          target.Name,
		TargetPodUID:       string(target.UID),
		TargetOrdinal:      2,
		SourceRevision:     "old",
		DesiredRevision:    "new",
		StartedAt:          &now,
		StageStartedAt:     &now,
		LastTransitionTime: &now,
	}
	mgr.cr.Status.Peers[2].Status = "Decommissioning"

	oldNow := indexerEndpointWithdrawalNow
	t.Cleanup(func() {
		indexerEndpointWithdrawalNow = oldNow
	})
	currentTime := now.Time
	indexerEndpointWithdrawalNow = func() time.Time {
		return currentTime
	}
	ready := false
	setIndexerEndpointSliceForWithdrawal(mgr, target, &ready)

	complete, err := mgr.PrepareRecycle(context.Background(), 2)
	require.NoError(t, err)
	require.False(t, complete)
	require.Nil(t, mgr.cr.Status.PodUpdate.DecommissionRequestedAt)
	require.Equal(
		t,
		enterpriseApi.IndexerClusterPodUpdateStageWithdrawingReadiness,
		mgr.cr.Status.PodUpdate.Stage,
	)

	currentTime = currentTime.Add(31 * time.Second)
	complete, err = mgr.PrepareRecycle(context.Background(), 2)
	require.NoError(t, err)
	require.False(t, complete)
	require.NotNil(t, mgr.cr.Status.PodUpdate.DecommissionRequestedAt)
	require.True(t, mgr.cr.Status.PodUpdate.ObservedDecommissioning)
	require.Equal(
		t,
		enterpriseApi.IndexerClusterPodUpdateStageDecommissioning,
		mgr.cr.Status.PodUpdate.Stage,
	)
}

func TestIndexerDecommissionWaitsForEndpointPropagationDelay(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	mgr, _, pods := indexerLifecycleFixture(t)
	target := pods[2]
	target.Status.Conditions[0].Status = corev1.ConditionFalse
	target.Status.ContainerStatuses[0].Ready = false
	require.NoError(t, mgr.c.Update(context.Background(), target))
	now := metav1.Now()
	delaySeconds := int64(1)
	mgr.cr.Spec.LifecyclePolicy = &enterpriseApi.IndexerClusterLifecyclePolicy{
		EndpointWithdrawalDelaySeconds: &delaySeconds,
	}
	mgr.cr.Status.PodUpdate = &enterpriseApi.IndexerClusterPodUpdateStatus{
		OperationID:        "operation",
		Stage:              enterpriseApi.IndexerClusterPodUpdateStageWithdrawingReadiness,
		TargetPod:          target.Name,
		TargetPodUID:       string(target.UID),
		TargetOrdinal:      2,
		SourceRevision:     "old",
		DesiredRevision:    "new",
		StartedAt:          &now,
		StageStartedAt:     &now,
		LastTransitionTime: &now,
	}

	decommissionCalls := 0
	oldRequest := requestIndexerPeerDecommission
	t.Cleanup(func() {
		requestIndexerPeerDecommission = oldRequest
	})
	requestIndexerPeerDecommission = func(
		_ context.Context,
		_ *indexerClusterPodManager,
		_ int32,
		_ bool,
	) error {
		decommissionCalls++
		return nil
	}

	ready := true
	setIndexerEndpointSliceForWithdrawal(mgr, target, &ready)
	complete, err := mgr.PrepareRecycle(context.Background(), 2)
	require.NoError(t, err)
	require.False(t, complete)
	require.Zero(t, decommissionCalls)
	require.Nil(t, mgr.cr.Status.PodUpdate.DecommissionRequestedAt)

	ready = false
	setIndexerEndpointSliceForWithdrawal(mgr, target, &ready)
	complete, err = mgr.PrepareRecycle(context.Background(), 2)
	require.NoError(t, err)
	require.False(t, complete)
	require.Zero(t, decommissionCalls)
	require.NotNil(t, mgr.cr.Status.PodUpdate.EndpointWithdrawalObservedAt)

	observedAt := metav1.NewTime(time.Now().Add(-2 * time.Second))
	deadline := metav1.NewTime(time.Now().Add(-time.Second))
	mgr.cr.Status.PodUpdate.EndpointWithdrawalObservedAt = &observedAt
	mgr.cr.Status.PodUpdate.EndpointWithdrawalDeadline = &deadline
	complete, err = mgr.PrepareRecycle(context.Background(), 2)
	require.NoError(t, err)
	require.False(t, complete)
	require.Equal(t, 1, decommissionCalls)
	require.NotNil(t, mgr.cr.Status.PodUpdate.DecommissionRequestedAt)
	require.Equal(
		t,
		enterpriseApi.IndexerClusterPodUpdateStageDecommissioning,
		mgr.cr.Status.PodUpdate.Stage,
	)
}

func setIndexerEndpointSliceForWithdrawal(
	mgr *indexerClusterPodManager,
	pod *corev1.Pod,
	ready *bool,
) {
	mgr.c.(*spltest.MockClient).ListObj = &discoveryv1.EndpointSliceList{
		Items: []discoveryv1.EndpointSlice{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: pod.Namespace,
				Labels: map[string]string{
					discoveryv1.LabelServiceName: splcommon.GetSplunkServiceName(
						SplunkIndexer,
						mgr.cr.GetName(),
						false,
					),
				},
			},
			Endpoints: []discoveryv1.Endpoint{{
				TargetRef: &corev1.ObjectReference{
					Kind:      "Pod",
					Namespace: pod.Namespace,
					Name:      pod.Name,
					UID:       pod.UID,
				},
				Conditions: discoveryv1.EndpointConditions{Ready: ready},
			}},
		}},
	}
}
