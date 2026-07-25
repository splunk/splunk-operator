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

	// Kubernetes readiness is only the boundary to SHC rejoin validation.
	replacement.PodReady = true
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
	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery, ActionObserveCluster)

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
			"unscheduled Pod started Splunk rejoin timer at %v",
			decision.Operation.MemberRejoinStartedAt,
		)
	}
}

func TestRecoveryAttributesStoragePendingReplacementToKubernetes(t *testing.T) {
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	observation := RecoveryObservation{
		PodExists:      true,
		PodUID:         "new-pod-uid",
		PodRevision:    "revision-2",
		PodScheduled:   true,
		StoragePending: true,
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
			"storage-pending Pod started Splunk rejoin timer at %v",
			decision.Operation.MemberRejoinStartedAt,
		)
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

func TestRecoveryClassifiesImagePullFailure(t *testing.T) {
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	observation := recoveredPodObservation()
	observation.PodReady = false
	observation.ImagePullFailed = true

	decision := EvaluateRecovery(operation, observation, testRecoveryPolicy(), now.Add(time.Second))

	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageBlocked, ActionNone)
	if decision.Operation.Reason != enterpriseApi.SearchHeadClusterLifecycleReasonImagePullFailed {
		t.Fatalf("reason = %q, want ImagePullFailed", decision.Operation.Reason)
	}
	if decision.Operation.MemberRejoinStartedAt != nil {
		t.Fatalf(
			"image-pull failure started Splunk rejoin timer at %v",
			decision.Operation.MemberRejoinStartedAt,
		)
	}
}

func TestRecoveryWaitsForRecoverableContainerStartupFailure(t *testing.T) {
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	observation := recoveredPodObservation()
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
			"container startup failure started Splunk rejoin timer at %v",
			decision.Operation.MemberRejoinStartedAt,
		)
	}
}

func TestRecoveryBlocksTerminalContainerStartupFailure(t *testing.T) {
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	observation := recoveredPodObservation()
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
		MemberRejoinTimeout: 5 * time.Minute,
	}
}
