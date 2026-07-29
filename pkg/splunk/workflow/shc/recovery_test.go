// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package shc

import (
	"strings"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRecoverySeparatesPodReadinessFromSHCCompletion(t *testing.T) {
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	policy := testRecoveryPolicy()

	decision := EvaluateRecovery(operation, RecoveryObservation{
		PodExists: true,
		PodUID:    "old-pod-uid",
	}, policy, now.Add(time.Second))
	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageWaitingForTermination, ActionNone)

	decision = EvaluateRecovery(decision.Operation, RecoveryObservation{}, policy, now.Add(2*time.Second))
	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageWaitingForScheduling, ActionNone)

	replacement := RecoveryObservation{
		PodExists:    true,
		PodUID:       "new-pod-uid",
		PodScheduled: true,
		PodRevision:  "revision-2",
	}
	decision = EvaluateRecovery(decision.Operation, replacement, policy, now.Add(3*time.Second))
	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageWaitingForContainer, ActionNone)

	// Container readiness is only the boundary to SHC rejoin validation.
	replacement.ContainersReady = true
	decision = EvaluateRecovery(decision.Operation, replacement, policy, now.Add(4*time.Second))
	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageWaitingForMemberRejoin, ActionObserveCluster)

	replacement.MemberObserved = true
	replacement.MemberRegistered = true
	replacement.MemberStatus = "ManualDetention"
	replacement.CaptainMemberObserved = true
	replacement.CaptainMemberID = "member-guid-2"
	replacement.CaptainMemberStatus = "ManualDetention"
	replacement.CaptainReady = true
	replacement.AuthoritativeCaptain = true
	decision = EvaluateRecovery(decision.Operation, replacement, policy, now.Add(5*time.Second))
	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery, ActionReleaseDetention)

	// The adapter persists the validating stage before releasing detention.
	decision = EvaluateRecovery(decision.Operation, replacement, policy, now.Add(6*time.Second))
	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery, ActionReleaseDetention)
	releasedAt := metav1.NewTime(now.Add(6 * time.Second))
	decision.Operation.DetentionReleaseRequestedAt = &releasedAt

	// A submitted release is not completion until both Splunk views report Up.
	decision = EvaluateRecovery(decision.Operation, replacement, policy, now.Add(7*time.Second))
	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery, ActionReleaseDetention)
	if decision.Operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonDetentionReleasePending {
		t.Fatalf(
			"reason = %q, want DetentionReleasePending",
			decision.Operation.Reason,
		)
	}

	replacement.MemberStatus = "Up"
	replacement.CaptainMemberStatus = "Up"
	decision = EvaluateRecovery(decision.Operation, replacement, policy, now.Add(8*time.Second))
	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageCompleted, ActionNone)
	if len(decision.Operation.CompletedOrdinals) != 1 ||
		decision.Operation.CompletedOrdinals[0] != 2 {
		t.Fatalf("completed ordinals = %v, want [2]", decision.Operation.CompletedOrdinals)
	}
	if decision.Operation.ReplacementPodUID != "new-pod-uid" {
		t.Fatalf("replacement Pod UID = %q, want new-pod-uid", decision.Operation.ReplacementPodUID)
	}
	if decision.Operation.ReplacementMemberID != "member-guid-2" {
		t.Fatalf("replacement member ID = %q, want member-guid-2", decision.Operation.ReplacementMemberID)
	}
}

func TestScaleDownCancellationReleasesDetentionWithoutReplacement(t *testing.T) {
	now := time.Date(2026, 7, 28, 4, 35, 0, 0, time.UTC)
	target := int32(3)
	operation := &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:   "ScaleDown:example-search-head-3:",
		Intent:        enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown,
		TargetPod:     "example-search-head-3",
		TargetOrdinal: &target,
		Stage: enterpriseApi.
			SearchHeadClusterLifecycleStageValidatingRecovery,
		Reason: enterpriseApi.
			SearchHeadClusterLifecycleReasonScaleDownCancelled,
	}
	startedAt := metav1.NewTime(now)
	operation.MemberRejoinStartedAt = &startedAt
	observation := recoveredPodObservation()
	observation.PodUID = "original-pod-uid"
	observation.MemberStatus = "ManualDetention"
	observation.CaptainMemberStatus = "ManualDetention"

	decision := EvaluateRecovery(
		operation,
		observation,
		testRecoveryPolicy(),
		now.Add(time.Second),
	)
	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery,
		ActionReleaseDetention,
	)
	if decision.Operation.TargetPodUID != "original-pod-uid" {
		t.Fatalf(
			"cancelled scale-down Pod UID = %q, want original-pod-uid",
			decision.Operation.TargetPodUID,
		)
	}
	decision.Operation = RecordDetentionReleaseAttempt(
		decision.Operation,
		now.Add(2*time.Second),
	)

	observation.MemberStatus = "Up"
	observation.CaptainMemberStatus = "Up"
	decision = EvaluateRecovery(
		decision.Operation,
		observation,
		testRecoveryPolicy(),
		now.Add(3*time.Second),
	)
	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageCompleted,
		ActionNone,
	)
	if len(decision.Operation.CompletedOrdinals) != 0 {
		t.Fatalf(
			"cancelled scale down recorded replacement ordinals %v",
			decision.Operation.CompletedOrdinals,
		)
	}
	if !strings.Contains(
		decision.Operation.Message,
		"scale-down cancellation completed",
	) {
		t.Fatalf(
			"completion message = %q, want cancellation evidence",
			decision.Operation.Message,
		)
	}
}

func TestPodUpdateCancellationRestoresOriginalPodWithoutCompletingRevision(t *testing.T) {
	now := time.Date(2026, 7, 28, 6, 45, 0, 0, time.UTC)
	target := int32(2)
	startedAt := metav1.NewTime(now)
	operation := &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:     "PodUpdate:example-search-head-2:revision-2:2",
		Intent:          enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		DesiredRevision: "revision-2",
		TargetPod:       "example-search-head-2",
		TargetOrdinal:   &target,
		TargetPodUID:    "original-pod-uid",
		TargetMemberID:  "member-guid-2",
		Stage: enterpriseApi.
			SearchHeadClusterLifecycleStageValidatingRecovery,
		Reason: enterpriseApi.
			SearchHeadClusterLifecycleReasonPodUpdateCancelled,
		ActiveHistoricalSearches: 2,
		ActiveRealtimeSearches:   1,
		MemberRejoinStartedAt:    &startedAt,
	}
	observation := recoveredPodObservation()
	observation.PodUID = "original-pod-uid"
	observation.MemberStatus = "ManualDetention"
	observation.CaptainMemberStatus = "ManualDetention"
	observation.ActiveHistoricalSearches = 1
	observation.ActiveRealtimeSearches = 1

	decision := EvaluateRecovery(
		operation,
		observation,
		testRecoveryPolicy(),
		now.Add(time.Second),
	)
	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery,
		ActionReleaseDetention,
	)
	if decision.Operation.ActiveHistoricalSearches != 1 ||
		decision.Operation.ActiveRealtimeSearches != 1 {
		t.Fatalf(
			"recovery search counts = historical %d realtime %d, want current 1 and 1",
			decision.Operation.ActiveHistoricalSearches,
			decision.Operation.ActiveRealtimeSearches,
		)
	}
	decision.Operation = RecordDetentionReleaseAttempt(
		decision.Operation,
		now.Add(2*time.Second),
	)

	observation.MemberStatus = "Up"
	observation.CaptainMemberStatus = "Up"
	observation.ActiveHistoricalSearches = 0
	observation.ActiveRealtimeSearches = 0
	decision = EvaluateRecovery(
		decision.Operation,
		observation,
		testRecoveryPolicy(),
		now.Add(3*time.Second),
	)
	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageCompleted,
		ActionNone,
	)
	if len(decision.Operation.CompletedOrdinals) != 0 {
		t.Fatalf(
			"cancelled Pod update recorded completed revision ordinals %v",
			decision.Operation.CompletedOrdinals,
		)
	}
	if !strings.Contains(
		decision.Operation.Message,
		"Pod-update cancellation completed",
	) {
		t.Fatalf(
			"completion message = %q, want Pod-update cancellation evidence",
			decision.Operation.Message,
		)
	}
	if decision.Operation.ActiveHistoricalSearches != 0 ||
		decision.Operation.ActiveRealtimeSearches != 0 {
		t.Fatalf(
			"completed recovery retained stale search counts: historical %d realtime %d",
			decision.Operation.ActiveHistoricalSearches,
			decision.Operation.ActiveRealtimeSearches,
		)
	}
}

func TestScaleDownCancellationBlocksWhenOriginalPodIsNotIntact(t *testing.T) {
	now := time.Date(2026, 7, 28, 4, 35, 0, 0, time.UTC)
	target := int32(3)
	newOperation := func() *enterpriseApi.SearchHeadClusterLifecycleOperationStatus {
		startedAt := metav1.NewTime(now)
		return &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
			OperationID:           "ScaleDown:example-search-head-3:",
			Intent:                enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown,
			TargetPod:             "example-search-head-3",
			TargetOrdinal:         &target,
			TargetPodUID:          "original-pod-uid",
			Stage:                 enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery,
			Reason:                enterpriseApi.SearchHeadClusterLifecycleReasonScaleDownCancelled,
			MemberRejoinStartedAt: &startedAt,
		}
	}

	tests := []struct {
		name        string
		observation RecoveryObservation
	}{
		{
			name:        "missing Pod",
			observation: RecoveryObservation{},
		},
		{
			name: "terminating Pod",
			observation: RecoveryObservation{
				PodExists:   true,
				PodUID:      "original-pod-uid",
				PodDeleting: true,
			},
		},
		{
			name: "changed Pod identity",
			observation: RecoveryObservation{
				PodExists: true,
				PodUID:    "replacement-pod-uid",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := EvaluateRecovery(
				newOperation(),
				test.observation,
				testRecoveryPolicy(),
				now.Add(time.Second),
			)
			assertDecision(
				t,
				decision,
				enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
				ActionNone,
			)
		})
	}
}

func TestRecoveryClassifiesDetentionReleaseTimeout(t *testing.T) {
	now := time.Date(2026, 7, 27, 18, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	operation.Stage =
		enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery
	rejoinStartedAt := metav1.NewTime(now)
	releaseRequestedAt := metav1.NewTime(now.Add(5 * time.Second))
	operation.MemberRejoinStartedAt = &rejoinStartedAt
	operation.DetentionReleaseRequestedAt = &releaseRequestedAt

	observation := recoveredPodObservation()
	observation.MemberStatus = "ManualDetention"
	observation.CaptainMemberStatus = "ManualDetention"
	policy := testRecoveryPolicy()
	policy.MemberRejoinTimeout = 30 * time.Second

	decision := EvaluateRecovery(
		operation,
		observation,
		policy,
		now.Add(30*time.Second),
	)

	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
		ActionNone,
	)
	if decision.Operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonDetentionReleaseTimedOut {
		t.Fatalf(
			"reason = %q, want DetentionReleaseTimedOut",
			decision.Operation.Reason,
		)
	}
}

func TestRecoveryUsesContainerReadinessBeforeSHCServingGate(t *testing.T) {
	now := time.Date(2026, 7, 26, 23, 55, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	policy := testRecoveryPolicy()

	decision := EvaluateRecovery(operation, RecoveryObservation{
		PodExists: true,
		PodUID:    "old-pod-uid",
	}, policy, now.Add(time.Second))
	decision = EvaluateRecovery(
		decision.Operation,
		RecoveryObservation{},
		policy,
		now.Add(2*time.Second),
	)
	replacement := RecoveryObservation{
		PodExists:    true,
		PodUID:       "new-pod-uid",
		PodScheduled: true,
		PodRevision:  "revision-2",
	}
	decision = EvaluateRecovery(
		decision.Operation,
		replacement,
		policy,
		now.Add(3*time.Second),
	)
	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForContainer,
		ActionNone,
	)

	// The Operator-managed SHC serving gate keeps PodReady false until SHC
	// recovery completes. Kubernetes ContainersReady must start rejoin
	// validation without waiting on that gate.
	replacement.ContainersReady = true
	decision = EvaluateRecovery(
		decision.Operation,
		replacement,
		policy,
		now.Add(4*time.Second),
	)
	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForMemberRejoin,
		ActionObserveCluster,
	)
	if decision.Operation.MemberRejoinStartedAt == nil {
		t.Fatal("member rejoin timer was not started after containers became ready")
	}
}

func TestRecoveryBlocksChangedPersistentMemberIdentity(t *testing.T) {
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	observation := recoveredPodObservation()
	observation.CaptainMemberID = "different-member-guid"

	decision := EvaluateRecovery(operation, observation, testRecoveryPolicy(), now.Add(time.Second))

	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageBlocked, ActionNone)
	if decision.Operation.Reason != enterpriseApi.SearchHeadClusterLifecycleReasonMemberIdentityMismatch {
		t.Fatalf("reason = %q, want MemberIdentityMismatch", decision.Operation.Reason)
	}
}

func TestRecoveryBlocksMissingPersistentMemberIdentity(t *testing.T) {
	tests := []struct {
		name                string
		targetMemberID      string
		replacementMemberID string
		wantMessageFragment string
	}{
		{
			name:                "original identity was not captured",
			replacementMemberID: "member-guid-2",
			wantMessageFragment: "retained member identity was not captured",
		},
		{
			name:                "replacement identity is missing",
			targetMemberID:      "member-guid-2",
			wantMessageFragment: "replacement member identity is missing",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
			operation := authorizedRecoveryOperation(now)
			operation.TargetMemberID = test.targetMemberID
			observation := recoveredPodObservation()
			observation.CaptainMemberID = test.replacementMemberID

			decision := EvaluateRecovery(
				operation,
				observation,
				testRecoveryPolicy(),
				now.Add(time.Second),
			)

			assertDecision(
				t,
				decision,
				enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
				ActionNone,
			)
			if decision.Operation.Reason !=
				enterpriseApi.SearchHeadClusterLifecycleReasonMemberIdentityMismatch {
				t.Fatalf(
					"reason = %q, want MemberIdentityMismatch",
					decision.Operation.Reason,
				)
			}
			if !strings.Contains(
				decision.Operation.Message,
				test.wantMessageFragment,
			) {
				t.Fatalf(
					"message = %q, want fragment %q",
					decision.Operation.Message,
					test.wantMessageFragment,
				)
			}
			if decision.Operation.ReplacementMemberID != "" {
				t.Fatalf(
					"missing identity accepted replacement ID %q",
					decision.Operation.ReplacementMemberID,
				)
			}
		})
	}
}

func TestRecoveryBlocksWrongStatefulSetRevision(t *testing.T) {
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	observation := recoveredPodObservation()
	observation.PodRevision = "revision-1"

	decision := EvaluateRecovery(operation, observation, testRecoveryPolicy(), now.Add(time.Second))

	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageBlocked, ActionNone)
	if decision.Operation.Reason != enterpriseApi.SearchHeadClusterLifecycleReasonPodRevisionMismatch {
		t.Fatalf("reason = %q, want PodRevisionMismatch", decision.Operation.Reason)
	}
}

func TestRecoveryAttributesUnschedulableReplacementToKubernetes(t *testing.T) {
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	observation := RecoveryObservation{
		PodExists:        true,
		PodUID:           "new-pod-uid",
		PodRevision:      "revision-2",
		PodUnschedulable: true,
	}

	decision := EvaluateRecovery(
		operation,
		observation,
		testRecoveryPolicy(),
		now.Add(time.Second),
	)

	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForScheduling,
		ActionNone,
	)
	if decision.Operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonPodUnschedulable {
		t.Fatalf(
			"reason = %q, want PodUnschedulable",
			decision.Operation.Reason,
		)
	}
	if decision.Operation.MemberRejoinStartedAt != nil {
		t.Fatalf(
			"unscheduled replacement started member rejoin at %v",
			decision.Operation.MemberRejoinStartedAt,
		)
	}
	if decision.Operation.ReplacementPodObservedAt == nil {
		t.Fatal("unscheduled replacement did not start the Pod startup timer")
	}
}

func TestRecoveryAttributesStoragePendingReplacementToKubernetes(t *testing.T) {
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	observation := RecoveryObservation{
		PodExists:                         true,
		PodUID:                            "new-pod-uid",
		PodRevision:                       "revision-2",
		PodScheduled:                      true,
		PodReadyToStartContainersObserved: true,
		StoragePending:                    true,
	}

	decision := EvaluateRecovery(
		operation,
		observation,
		testRecoveryPolicy(),
		now.Add(time.Second),
	)

	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForStorage,
		ActionNone,
	)
	if decision.Operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonVolumeAttachmentPending {
		t.Fatalf(
			"reason = %q, want VolumeAttachmentPending",
			decision.Operation.Reason,
		)
	}
	if decision.Operation.MemberRejoinStartedAt != nil {
		t.Fatalf(
			"storage-pending replacement started member rejoin at %v",
			decision.Operation.MemberRejoinStartedAt,
		)
	}
	if decision.Operation.ReplacementPodObservedAt == nil {
		t.Fatal("storage-pending replacement did not start the Pod startup timer")
	}
}

func TestRecoveryAttributesGenericPodInfrastructurePendingToKubernetes(t *testing.T) {
	now := time.Date(2026, 7, 29, 13, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	observation := RecoveryObservation{
		PodExists:                         true,
		PodUID:                            "new-pod-uid",
		PodRevision:                       "revision-2",
		PodScheduled:                      true,
		PodReadyToStartContainersObserved: true,
	}

	decision := EvaluateRecovery(
		operation,
		observation,
		testRecoveryPolicy(),
		now.Add(time.Second),
	)

	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForPodInfrastructure,
		ActionNone,
	)
	if decision.Operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonPodInfrastructurePending {
		t.Fatalf(
			"reason = %q, want PodInfrastructurePending",
			decision.Operation.Reason,
		)
	}
	if decision.Operation.MemberRejoinStartedAt != nil {
		t.Fatalf(
			"Pod-infrastructure wait started member rejoin at %v",
			decision.Operation.MemberRejoinStartedAt,
		)
	}
	if decision.Operation.ReplacementPodObservedAt == nil {
		t.Fatal("Pod-infrastructure wait did not start the Pod startup timer")
	}
}

func TestRecoveryBlocksPodInfrastructureWaitAfterStartupBudget(t *testing.T) {
	now := time.Date(2026, 7, 29, 14, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	observation := RecoveryObservation{
		PodExists:                         true,
		PodUID:                            "new-pod-uid",
		PodRevision:                       "revision-2",
		PodScheduled:                      true,
		PodReadyToStartContainersObserved: true,
	}
	policy := testRecoveryPolicy()
	policy.PodStartupTimeout = 30 * time.Second

	decision := EvaluateRecovery(
		operation,
		observation,
		policy,
		now.Add(time.Second),
	)
	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForPodInfrastructure,
		ActionNone,
	)

	decision = EvaluateRecovery(
		decision.Operation,
		observation,
		policy,
		decision.Operation.ReplacementPodObservedAt.Add(policy.PodStartupTimeout),
	)
	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
		ActionNone,
	)
	if decision.Operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonPodStartupTimedOut {
		t.Fatalf(
			"reason = %q, want PodStartupTimedOut",
			decision.Operation.Reason,
		)
	}
	for _, fragment := range []string{
		"stage WaitingForPodInfrastructure",
		"podReadyToStartContainersObserved=true",
		"podReadyToStartContainers=false",
	} {
		if !strings.Contains(decision.Operation.Message, fragment) {
			t.Fatalf(
				"timeout message = %q, want fragment %q",
				decision.Operation.Message,
				fragment,
			)
		}
	}
}

func TestRecoveryTimeoutContinuesWithoutPodOrSplunkObservation(t *testing.T) {
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	operation.Stage = enterpriseApi.SearchHeadClusterLifecycleStageWaitingForMemberRejoin
	rejoinStartedAt := metav1.NewTime(now)
	operation.MemberRejoinStartedAt = &rejoinStartedAt
	policy := testRecoveryPolicy()
	policy.MemberRejoinTimeout = 30 * time.Second

	decision := EvaluateRecovery(operation, RecoveryObservation{}, policy, now.Add(30*time.Second))

	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageBlocked, ActionNone)
	if decision.Operation.Reason != enterpriseApi.SearchHeadClusterLifecycleReasonMemberRejoinTimedOut {
		t.Fatalf("reason = %q, want MemberRejoinTimedOut", decision.Operation.Reason)
	}
}

func TestRecoveryTimeoutRecordsBoundedGateSnapshot(t *testing.T) {
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	operation.Stage =
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForMemberRejoin
	rejoinStartedAt := metav1.NewTime(now)
	operation.MemberRejoinStartedAt = &rejoinStartedAt
	observation := recoveredPodObservation()
	observation.CaptainMemberObserved = false
	observation.CaptainMemberID = ""
	observation.CaptainMemberStatus = ""
	policy := testRecoveryPolicy()
	policy.MemberRejoinTimeout = 30 * time.Second

	decision := EvaluateRecovery(
		operation,
		observation,
		policy,
		now.Add(30*time.Second),
	)

	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
		ActionNone,
	)
	if decision.Operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonMemberRejoinTimedOut {
		t.Fatalf(
			"reason = %q, want MemberRejoinTimedOut",
			decision.Operation.Reason,
		)
	}
	for _, fragment := range []string{
		"podExists=true",
		"podScheduled=true",
		"podReady=true",
		"memberObserved=true",
		"memberRegistered=true",
		"memberStatusAccepted=true",
		"authoritativeCaptain=true",
		"captainReady=true",
		"captainMemberObserved=false",
		"captainMemberStatusAccepted=false",
	} {
		if !strings.Contains(decision.Operation.Message, fragment) {
			t.Fatalf(
				"timeout message = %q, want fragment %q",
				decision.Operation.Message,
				fragment,
			)
		}
	}
	if len(decision.Operation.Message) > 512 {
		t.Fatalf(
			"timeout snapshot is not bounded: %d bytes",
			len(decision.Operation.Message),
		)
	}
	if decision.Operation.TargetPod != "example-search-head-2" ||
		decision.Operation.TargetMemberID != "member-guid-2" {
		t.Fatalf(
			"timeout lost target identity: pod %q, member %q",
			decision.Operation.TargetPod,
			decision.Operation.TargetMemberID,
		)
	}
}

func TestRecoveryWaitsForRetryableImagePullFailure(t *testing.T) {
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	observation := recoveredPodObservation()
	observation.ContainersReady = false
	observation.PodReady = false
	observation.ImagePullFailed = true

	decision := EvaluateRecovery(operation, observation, testRecoveryPolicy(), now.Add(time.Second))

	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForContainer,
		ActionNone,
	)
	if decision.Operation.Reason != enterpriseApi.SearchHeadClusterLifecycleReasonImagePullFailed {
		t.Fatalf("reason = %q, want ImagePullFailed", decision.Operation.Reason)
	}
	if decision.Operation.MemberRejoinStartedAt != nil {
		t.Fatalf(
			"image-pull failure started Splunk rejoin timer at %v",
			decision.Operation.MemberRejoinStartedAt,
		)
	}
	if decision.Operation.ReplacementPodObservedAt == nil {
		t.Fatal("image-pull failure did not start the Pod startup timer")
	}
}

func TestRecoveryBlocksRetryableImagePullFailureAfterBudget(t *testing.T) {
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	observation := recoveredPodObservation()
	observation.ContainersReady = false
	observation.PodReady = false
	observation.ImagePullFailed = true
	policy := testRecoveryPolicy()
	policy.PodStartupTimeout = 30 * time.Second

	decision := EvaluateRecovery(
		operation,
		observation,
		policy,
		now.Add(time.Second),
	)
	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForContainer,
		ActionNone,
	)

	decision = EvaluateRecovery(
		decision.Operation,
		observation,
		policy,
		decision.Operation.ReplacementPodObservedAt.Add(policy.PodStartupTimeout),
	)
	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
		ActionNone,
	)
	if decision.Operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonImagePullFailed {
		t.Fatalf(
			"reason = %q, want ImagePullFailed",
			decision.Operation.Reason,
		)
	}
	if !strings.Contains(decision.Operation.Message, "imagePullFailed=true") {
		t.Fatalf(
			"timeout message = %q, want image-pull attribution",
			decision.Operation.Message,
		)
	}
}

func TestRecoveryBlocksTerminalImagePullFailureImmediately(t *testing.T) {
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	observation := recoveredPodObservation()
	observation.ContainersReady = false
	observation.PodReady = false
	observation.ImagePullFailed = true
	observation.ImagePullFailureTerminal = true

	decision := EvaluateRecovery(
		operation,
		observation,
		testRecoveryPolicy(),
		now.Add(time.Second),
	)

	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
		ActionNone,
	)
	if decision.Operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonImagePullFailed {
		t.Fatalf(
			"reason = %q, want ImagePullFailed",
			decision.Operation.Reason,
		)
	}
	if decision.Operation.ReplacementPodObservedAt == nil {
		t.Fatal("terminal image-pull failure did not record replacement observation")
	}
}

func TestRecoveryWaitsForRecoverableContainerStartupFailure(t *testing.T) {
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	observation := recoveredPodObservation()
	observation.ContainersReady = false
	observation.PodReady = false
	observation.ContainerStartupFailed = true

	decision := EvaluateRecovery(
		operation,
		observation,
		testRecoveryPolicy(),
		now.Add(time.Second),
	)

	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForContainer,
		ActionNone,
	)
	if decision.Operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonSplunkStartupFailed {
		t.Fatalf(
			"reason = %q, want SplunkStartupFailed",
			decision.Operation.Reason,
		)
	}
	if decision.Operation.MemberRejoinStartedAt != nil {
		t.Fatalf(
			"container startup failure started member rejoin at %v",
			decision.Operation.MemberRejoinStartedAt,
		)
	}
	if decision.Operation.ReplacementPodObservedAt == nil {
		t.Fatal("container startup failure did not start the Pod startup timer")
	}
}

func TestRecoveryBlocksRecoverableContainerStartupFailureAfterBudget(t *testing.T) {
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	observation := recoveredPodObservation()
	observation.ContainersReady = false
	observation.PodReady = false
	observation.ContainerStartupFailed = true
	policy := testRecoveryPolicy()
	policy.PodStartupTimeout = 30 * time.Second

	decision := EvaluateRecovery(
		operation,
		observation,
		policy,
		now.Add(time.Second),
	)
	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForContainer,
		ActionNone,
	)
	if decision.Operation.ReplacementPodObservedAt == nil {
		t.Fatal("container startup failure did not start the Pod startup timer")
	}

	decision = EvaluateRecovery(
		decision.Operation,
		observation,
		policy,
		decision.Operation.ReplacementPodObservedAt.Add(policy.PodStartupTimeout),
	)
	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
		ActionNone,
	)
	if decision.Operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonSplunkStartupFailed {
		t.Fatalf(
			"reason = %q, want SplunkStartupFailed",
			decision.Operation.Reason,
		)
	}
	if !strings.Contains(
		decision.Operation.Message,
		"stage WaitingForContainer",
	) {
		t.Fatalf(
			"timeout message = %q, want container-stage attribution",
			decision.Operation.Message,
		)
	}
}

func TestRecoveryBackfillsReplacementStartupBudgetAfterOperatorUpgrade(t *testing.T) {
	now := time.Date(2026, 7, 24, 14, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now.Add(-2 * time.Hour))
	operation.Stage =
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForContainer
	stageStartedAt := metav1.NewTime(now.Add(-time.Hour))
	operation.StageStartedAt = &stageStartedAt
	operation.ReplacementPodUID = "new-pod-uid"
	observation := recoveredPodObservation()
	observation.ContainersReady = false
	observation.PodReady = false
	observation.ContainerStartupFailed = true
	policy := testRecoveryPolicy()
	policy.PodStartupTimeout = 30 * time.Minute

	decision := EvaluateRecovery(operation, observation, policy, now)

	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
		ActionNone,
	)
	if decision.Operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonSplunkStartupFailed {
		t.Fatalf(
			"reason = %q, want SplunkStartupFailed",
			decision.Operation.Reason,
		)
	}
	if decision.Operation.ReplacementPodObservedAt == nil ||
		!decision.Operation.ReplacementPodObservedAt.Equal(&stageStartedAt) {
		t.Fatalf(
			"backfilled startup time = %v, want %v",
			decision.Operation.ReplacementPodObservedAt,
			stageStartedAt,
		)
	}
}

func TestRecoveryBlocksTerminalContainerStartupFailure(t *testing.T) {
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	observation := recoveredPodObservation()
	observation.ContainersReady = false
	observation.PodReady = false
	observation.ContainerStartupFailed = true
	observation.ContainerFailureTerminal = true

	decision := EvaluateRecovery(
		operation,
		observation,
		testRecoveryPolicy(),
		now.Add(time.Second),
	)

	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
		ActionNone,
	)
	if decision.Operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonSplunkStartupFailed {
		t.Fatalf(
			"reason = %q, want SplunkStartupFailed",
			decision.Operation.Reason,
		)
	}
	if decision.Operation.MemberRejoinStartedAt != nil {
		t.Fatalf(
			"terminal startup failure started Splunk rejoin timer at %v",
			decision.Operation.MemberRejoinStartedAt,
		)
	}
}

func TestRecoveryWaitsWhenCaptainDoesNotObserveReplacementMember(t *testing.T) {
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	observation := recoveredPodObservation()
	observation.CaptainMemberObserved = false
	observation.CaptainMemberID = ""
	observation.CaptainMemberStatus = ""

	decision := EvaluateRecovery(
		operation,
		observation,
		testRecoveryPolicy(),
		now.Add(time.Second),
	)

	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForMemberRejoin,
		ActionObserveCluster,
	)
	if decision.Operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonMemberNotRegistered {
		t.Fatalf(
			"reason = %q, want MemberNotRegistered",
			decision.Operation.Reason,
		)
	}
	if decision.Operation.MemberRejoinStartedAt == nil {
		t.Fatal("member rejoin timer was not started")
	}
	startedAt := decision.Operation.MemberRejoinStartedAt.DeepCopy()

	decision = EvaluateRecovery(
		decision.Operation,
		observation,
		testRecoveryPolicy(),
		now.Add(2*time.Second),
	)
	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForMemberRejoin,
		ActionObserveCluster,
	)
	if decision.Operation.TargetOrdinal == nil ||
		*decision.Operation.TargetOrdinal != 2 {
		t.Fatalf(
			"waiting recovery changed target ordinal: %#v",
			decision.Operation.TargetOrdinal,
		)
	}
	if decision.Operation.MemberRejoinStartedAt == nil ||
		!decision.Operation.MemberRejoinStartedAt.Equal(startedAt) {
		t.Fatalf(
			"member rejoin timer changed from %v to %v",
			startedAt,
			decision.Operation.MemberRejoinStartedAt,
		)
	}

	policy := testRecoveryPolicy()
	policy.MemberRejoinTimeout = 30 * time.Second
	decision = EvaluateRecovery(
		decision.Operation,
		observation,
		policy,
		startedAt.Add(30*time.Second),
	)
	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
		ActionNone,
	)
	if decision.Operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonMemberRejoinTimedOut {
		t.Fatalf(
			"reason = %q, want MemberRejoinTimedOut",
			decision.Operation.Reason,
		)
	}
}

func TestRecoveryWaitsForUpStatusInMemberAndCaptainViews(t *testing.T) {
	tests := []struct {
		name          string
		memberStatus  string
		captainStatus string
		wantReason    enterpriseApi.SearchHeadClusterLifecycleReason
	}{
		{
			name:          "member view not up",
			memberStatus:  "Joining",
			captainStatus: "Up",
			wantReason:    enterpriseApi.SearchHeadClusterLifecycleReasonMemberNotUp,
		},
		{
			name:          "captain view not synchronized",
			memberStatus:  "Up",
			captainStatus: "Joining",
			wantReason: enterpriseApi.
				SearchHeadClusterLifecycleReasonMemberSynchronizationPending,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
			operation := authorizedRecoveryOperation(now)
			observation := recoveredPodObservation()
			observation.MemberStatus = test.memberStatus
			observation.CaptainMemberStatus = test.captainStatus

			decision := EvaluateRecovery(
				operation,
				observation,
				testRecoveryPolicy(),
				now.Add(time.Second),
			)

			assertDecision(
				t,
				decision,
				enterpriseApi.SearchHeadClusterLifecycleStageWaitingForMemberRejoin,
				ActionObserveCluster,
			)
			if decision.Operation.Reason != test.wantReason {
				t.Fatalf(
					"reason = %q, want %q",
					decision.Operation.Reason,
					test.wantReason,
				)
			}
			if decision.Operation.DetentionReleaseRequestedAt != nil {
				t.Fatalf(
					"non-Up member requested detention release at %v",
					decision.Operation.DetentionReleaseRequestedAt,
				)
			}
			if len(decision.Operation.CompletedOrdinals) != 0 {
				t.Fatalf(
					"non-Up member completed ordinals %v",
					decision.Operation.CompletedOrdinals,
				)
			}
		})
	}
}

func TestRecoveryBlocksSuspectedConsensusCatchupWithoutDestructiveAction(t *testing.T) {
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	operation.Stage =
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForMemberRejoin
	rejoinStartedAt := metav1.NewTime(now)
	operation.MemberRejoinStartedAt = &rejoinStartedAt
	observation := recoveredPodObservation()
	observation.MemberStatus = "Up"
	observation.CaptainMemberStatus = "Joining"
	policy := testRecoveryPolicy()
	policy.MemberRejoinTimeout = 30 * time.Second

	decision := EvaluateRecovery(
		operation,
		observation,
		policy,
		now.Add(29*time.Second),
	)

	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForMemberRejoin,
		ActionObserveCluster,
	)
	if decision.Operation.Reason != enterpriseApi.
		SearchHeadClusterLifecycleReasonMemberSynchronizationPending {
		t.Fatalf(
			"reason = %q, want MemberSynchronizationPending",
			decision.Operation.Reason,
		)
	}
	if decision.Operation.ReplacementMemberID != "member-guid-2" {
		t.Fatalf(
			"replacement identity = %q, want member-guid-2",
			decision.Operation.ReplacementMemberID,
		)
	}

	decision = EvaluateRecovery(
		decision.Operation,
		observation,
		policy,
		now.Add(30*time.Second),
	)
	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
		ActionNone,
	)
	if decision.Operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonMemberRejoinTimedOut {
		t.Fatalf(
			"reason = %q, want MemberRejoinTimedOut",
			decision.Operation.Reason,
		)
	}
	for _, fragment := range []string{
		"memberObserved=true",
		"memberStatusAccepted=true",
		"captainMemberObserved=true",
		"captainMemberStatusAccepted=false",
	} {
		if !strings.Contains(decision.Operation.Message, fragment) {
			t.Fatalf(
				"timeout message = %q, want fragment %q",
				decision.Operation.Message,
				fragment,
			)
		}
	}
	if decision.Operation.TargetMemberID != "member-guid-2" ||
		decision.Operation.ReplacementMemberID != "member-guid-2" {
		t.Fatalf(
			"timeout lost identity evidence: target %q, replacement %q",
			decision.Operation.TargetMemberID,
			decision.Operation.ReplacementMemberID,
		)
	}
	if decision.Operation.DetentionReleaseRequestedAt != nil ||
		len(decision.Operation.CompletedOrdinals) != 0 {
		t.Fatalf(
			"blocked catch-up changed recovery state: release=%v completed=%v",
			decision.Operation.DetentionReleaseRequestedAt,
			decision.Operation.CompletedOrdinals,
		)
	}
}

func TestRecoverySeparatesPodReadinessFromCaptainSynchronization(t *testing.T) {
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	observation := recoveredPodObservation()
	observation.ContainersReady = true
	observation.MemberStatus = "Up"
	observation.CaptainMemberStatus = "Syncing"

	decision := EvaluateRecovery(
		operation,
		observation,
		testRecoveryPolicy(),
		now.Add(time.Second),
	)

	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForMemberRejoin,
		ActionObserveCluster,
	)
	if decision.Operation.Reason != enterpriseApi.
		SearchHeadClusterLifecycleReasonMemberSynchronizationPending {
		t.Fatalf(
			"reason = %q, want MemberSynchronizationPending",
			decision.Operation.Reason,
		)
	}
	if decision.Operation.DetentionReleaseRequestedAt != nil ||
		len(decision.Operation.CompletedOrdinals) != 0 {
		t.Fatalf(
			"local readiness bypassed synchronization gate: release=%v completed=%v",
			decision.Operation.DetentionReleaseRequestedAt,
			decision.Operation.CompletedOrdinals,
		)
	}
	if decision.Operation.MemberRejoinStartedAt == nil {
		t.Fatal("synchronization wait did not start the rejoin budget")
	}

	observation.CaptainMemberStatus = "Up"
	decision = EvaluateRecovery(
		decision.Operation,
		observation,
		testRecoveryPolicy(),
		now.Add(2*time.Second),
	)
	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery,
		ActionReleaseDetention,
	)
	if decision.Operation.ReplacementMemberID != "member-guid-2" {
		t.Fatalf(
			"synchronized replacement identity = %q, want member-guid-2",
			decision.Operation.ReplacementMemberID,
		)
	}
}

func TestRecoveryRestoresWithdrawnAuthorizationAtKnownGoodRevision(
	t *testing.T,
) {
	now := time.Date(2026, 7, 29, 18, 30, 0, 0, time.UTC)
	failed := authorizedRecoveryOperation(now)
	failed.Stage =
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForScheduling
	failed.Reason =
		enterpriseApi.SearchHeadClusterLifecycleReasonPodUnschedulable
	operation, started := StartAuthorizedPodUpdateRevisionRecovery(
		failed,
		"revision-1",
		now.Add(time.Second),
	)
	if !started {
		t.Fatal("authorized revision recovery was not started")
	}

	decision := EvaluateRecovery(
		operation,
		RecoveryObservation{
			PodExists:   true,
			PodUID:      "failed-replacement-uid",
			PodRevision: "revision-2",
		},
		testRecoveryPolicy(),
		now.Add(2*time.Second),
	)
	if decision.Operation.Stage !=
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForScheduling ||
		decision.Operation.Reason !=
			enterpriseApi.
				SearchHeadClusterLifecycleReasonAuthorizedRevisionWithdrawn ||
		decision.Operation.ReplacementPodUID != "" {
		t.Fatalf("superseded Pod recovery decision = %#v", decision.Operation)
	}

	recovered := recoveredPodObservation()
	recovered.PodRevision = "revision-1"
	decision = EvaluateRecovery(
		decision.Operation,
		recovered,
		testRecoveryPolicy(),
		now.Add(3*time.Second),
	)
	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery,
		ActionReleaseDetention,
	)
	decision.Operation = RecordDetentionReleaseAttempt(
		decision.Operation,
		now.Add(4*time.Second),
	)
	decision = EvaluateRecovery(
		decision.Operation,
		recovered,
		testRecoveryPolicy(),
		now.Add(5*time.Second),
	)
	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageCompleted,
		ActionNone,
	)
	if len(decision.Operation.CompletedOrdinals) != 0 {
		t.Fatalf(
			"recovery at known-good revision marked failed desired revision complete: %v",
			decision.Operation.CompletedOrdinals,
		)
	}
}

func authorizedRecoveryOperation(now time.Time) *enterpriseApi.SearchHeadClusterLifecycleOperationStatus {
	operation := StartReplacement(
		"operation-1",
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		"revision-2",
		"example-search-head-2",
		2,
		now,
	)
	operation.Stage = enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement
	operation.TargetPodUID = "old-pod-uid"
	operation.TargetMemberID = "member-guid-2"
	authorizedAt := metav1.NewTime(now)
	operation.ReplacementAuthorizedAt = &authorizedAt
	return operation
}

func recoveredPodObservation() RecoveryObservation {
	return RecoveryObservation{
		PodExists:             true,
		PodUID:                "new-pod-uid",
		PodScheduled:          true,
		ContainersReady:       true,
		PodReady:              true,
		PodRevision:           "revision-2",
		MemberObserved:        true,
		MemberStatus:          "Up",
		MemberRegistered:      true,
		CaptainMemberObserved: true,
		CaptainMemberID:       "member-guid-2",
		CaptainMemberStatus:   "Up",
		CaptainReady:          true,
		AuthoritativeCaptain:  true,
	}
}

func testRecoveryPolicy() RecoveryPolicy {
	return RecoveryPolicy{
		TerminationTimeout:  20 * time.Second,
		PodStartupTimeout:   5 * time.Minute,
		MemberRejoinTimeout: 5 * time.Minute,
	}
}
