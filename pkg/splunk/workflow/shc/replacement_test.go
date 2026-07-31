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
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNonCaptainReplacementRequiresDetentionAndDrain(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	operation := newTestOperation(now)
	observation := safeObservation(now)

	decision := EvaluateReplacement(operation, observation, testPolicy(), now)
	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget, ActionRequestDetention)

	observation.TargetMemberStatus = "ManualDetention"
	observation.ActiveHistoricalSearches = 2
	observation.ActiveRealtimeSearches = 1
	decision = EvaluateReplacement(decision.Operation, observation, testPolicy(), now.Add(time.Second))
	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageDrainingSearches, ActionNone)
	if decision.Operation.ActiveHistoricalSearches != 2 ||
		decision.Operation.ActiveRealtimeSearches != 1 {
		t.Fatalf("search counts = historical %d realtime %d, want 2 and 1",
			decision.Operation.ActiveHistoricalSearches,
			decision.Operation.ActiveRealtimeSearches,
		)
	}

	observation.ActiveHistoricalSearches = 0
	observation.ActiveRealtimeSearches = 0
	decision = EvaluateReplacement(decision.Operation, observation, testPolicy(), now.Add(2*time.Second))
	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement, ActionAuthorizeReplacement)
}

func TestReplacementWaitsForAllKVStoresBeforeDetention(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	operation := newTestOperation(now)
	observation := safeObservation(now)
	observation.KVStoreNotReadyMembers = []string{
		"example-search-head-1=starting",
	}

	decision := EvaluateReplacement(operation, observation, testPolicy(), now)

	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageValidatingCluster,
		ActionObserveCluster,
	)
	if decision.Operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonKVStoreNotReady {
		t.Fatalf(
			"reason = %q, want KVStoreNotReady",
			decision.Operation.Reason,
		)
	}
	if len(decision.Operation.KVStoreNotReadyMembers) != 1 {
		t.Fatalf(
			"KV Store status = %v, want one not-ready member",
			decision.Operation.KVStoreNotReadyMembers,
		)
	}
}

func TestReplacementWaitsForKVStoreObservationBeforeDetention(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	operation := newTestOperation(now)
	observation := safeObservation(now)
	observation.KVStoreObservationAvailable = false

	decision := EvaluateReplacement(operation, observation, testPolicy(), now)

	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageValidatingCluster,
		ActionObserveCluster,
	)
	if decision.Operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonObservationStale {
		t.Fatalf(
			"reason = %q, want ObservationStale",
			decision.Operation.Reason,
		)
	}
}

func TestDetainedTargetWaitsForReadyKVStore(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	operation := newTestOperation(now)
	operation.Stage = enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget
	stageStartedAt := metav1.NewTime(now)
	operation.StageStartedAt = &stageStartedAt
	observation := safeObservation(now)
	observation.TargetMemberStatus = "ManualDetention"
	observation.TargetKVStoreReady = false
	observation.KVStoreNotReadyMembers = []string{
		operation.TargetPod + "=starting",
	}

	decision := EvaluateReplacement(
		operation,
		observation,
		testPolicy(),
		now.Add(time.Second),
	)

	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget,
		ActionObserveCluster,
	)
	if decision.Operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonKVStoreNotReady {
		t.Fatalf(
			"reason = %q, want KVStoreNotReady",
			decision.Operation.Reason,
		)
	}
}

func TestDrainedTargetRechecksKVStoreBeforeAuthorization(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	operation := newTestOperation(now)
	operation.Stage = enterpriseApi.SearchHeadClusterLifecycleStageDrainingSearches
	stageStartedAt := metav1.NewTime(now)
	operation.StageStartedAt = &stageStartedAt
	observation := safeObservation(now)
	observation.TargetMemberStatus = "ManualDetention"
	observation.TargetKVStoreReady = false
	observation.KVStoreNotReadyMembers = []string{
		operation.TargetPod + "=starting",
	}

	decision := EvaluateReplacement(
		operation,
		observation,
		testPolicy(),
		now.Add(time.Second),
	)

	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageDrainingSearches,
		ActionObserveCluster,
	)
	if decision.Operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonKVStoreNotReady {
		t.Fatalf(
			"reason = %q, want KVStoreNotReady",
			decision.Operation.Reason,
		)
	}
}

func TestCaptainReplacementRequiresConfirmedTransfer(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	operation := newTestOperation(now)
	observation := safeObservation(now)
	observation.Captain = operation.TargetPod
	observation.CaptainTransferTarget = "example-search-head-1"
	observation.CaptainTransferTargetManagementURI = "https://example-search-head-1:8089"

	decision := EvaluateReplacement(operation, observation, testPolicy(), now)
	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget, ActionRequestDetention)

	observation.TargetMemberStatus = "ManualDetention"
	decision = EvaluateReplacement(decision.Operation, observation, testPolicy(), now.Add(time.Second))
	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageTransferringCaptain, ActionTransferCaptain)
	if decision.Action.Target != "example-search-head-1" {
		t.Fatalf("captain transfer target = %q, want example-search-head-1", decision.Action.Target)
	}
	if decision.Action.ManagementURI != "https://example-search-head-1:8089" {
		t.Fatalf("captain transfer management URI = %q, want https://example-search-head-1:8089", decision.Action.ManagementURI)
	}

	// Once the adapter records successful submission, the operation only
	// observes. A successful command response is not completion; a fresh
	// observation must report a different, ready captain.
	requestedAt := metav1.NewTime(now.Add(2 * time.Second))
	decision.Operation.CaptainTransferTarget = decision.Action.Target
	decision.Operation.CaptainTransferRequestedAt = &requestedAt
	decision = EvaluateReplacement(decision.Operation, observation, testPolicy(), now.Add(2*time.Second))
	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageTransferringCaptain, ActionObserveCluster)

	observation.Captain = "example-search-head-1"
	decision = EvaluateReplacement(decision.Operation, observation, testPolicy(), now.Add(3*time.Second))
	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement, ActionAuthorizeReplacement)
}

func TestSearchDrainTimeoutBlocksReplacement(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	operation := newTestOperation(now)
	observation := safeObservation(now)
	decision := EvaluateReplacement(operation, observation, testPolicy(), now)

	observation.TargetMemberStatus = "ManualDetention"
	observation.ActiveHistoricalSearches = 1
	decision = EvaluateReplacement(decision.Operation, observation, testPolicy(), now.Add(time.Second))
	decision = EvaluateReplacement(decision.Operation, observation, testPolicy(), now.Add(31*time.Second))

	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageBlocked, ActionNone)
	if decision.Operation.Reason != enterpriseApi.SearchHeadClusterLifecycleReasonSearchDrainTimedOut {
		t.Fatalf("reason = %q, want %q",
			decision.Operation.Reason,
			enterpriseApi.SearchHeadClusterLifecycleReasonSearchDrainTimedOut,
		)
	}
}

func TestDetentionTimeoutBlocksReplacementWithoutFreshObservation(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	operation := newTestOperation(now)
	operation.Stage = enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget
	stageStartedAt := metav1.NewTime(now)
	operation.StageStartedAt = &stageStartedAt

	decision := EvaluateReplacement(
		operation,
		Observation{},
		testPolicy(),
		now.Add(30*time.Second),
	)

	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageBlocked, ActionNone)
	if decision.Operation.Reason != enterpriseApi.SearchHeadClusterLifecycleReasonDetentionTimedOut {
		t.Fatalf("reason = %q, want %q",
			decision.Operation.Reason,
			enterpriseApi.SearchHeadClusterLifecycleReasonDetentionTimedOut,
		)
	}
	if decision.Operation.ReplacementAuthorizedAt != nil {
		t.Fatal("detention timeout must not authorize replacement")
	}
	if decision.Operation.SearchDrainContinuationToken != "" {
		t.Fatal("detention timeout must not issue a search-drain approval token")
	}
}

func TestRecordDetentionRequestAttemptPreservesFirstAttemptAndCountsRetries(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	operation := newTestOperation(now)
	operation.Stage = enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget

	first := RecordDetentionRequestAttempt(operation, now.Add(time.Second))
	second := RecordDetentionRequestAttempt(first, now.Add(2*time.Second))

	if first.DetentionRequestedAt == nil ||
		!first.DetentionRequestedAt.Time.Equal(now.Add(time.Second)) {
		t.Fatalf("first requestedAt = %v, want %v",
			first.DetentionRequestedAt,
			now.Add(time.Second),
		)
	}
	if second.DetentionRequestedAt == nil ||
		!second.DetentionRequestedAt.Equal(first.DetentionRequestedAt) {
		t.Fatalf("second requestedAt = %v, want preserved %v",
			second.DetentionRequestedAt,
			first.DetentionRequestedAt,
		)
	}
	if first.DetentionRequestAttemptCount != 1 ||
		second.DetentionRequestAttemptCount != 2 {
		t.Fatalf("attempt counts = %d then %d, want 1 then 2",
			first.DetentionRequestAttemptCount,
			second.DetentionRequestAttemptCount,
		)
	}
	if operation.DetentionRequestedAt != nil ||
		operation.DetentionRequestAttemptCount != 0 {
		t.Fatal("recording a detention request mutated the input operation")
	}
}

func TestReplacementAuthorizationRequiresCapturedPodIdentity(t *testing.T) {
	now := time.Date(2026, 7, 28, 6, 45, 0, 0, time.UTC)
	operation := newTestOperation(now)
	operation.Stage =
		enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement
	operation.TargetPodUID = "original-pod-uid"

	authorized, ok := RecordReplacementAuthorization(
		operation,
		"original-pod-uid",
		now.Add(time.Second),
	)
	if !ok || authorized.ReplacementAuthorizedAt == nil {
		t.Fatalf("intact target was not authorized: %#v", authorized)
	}
	if operation.ReplacementAuthorizedAt != nil {
		t.Fatal("replacement authorization mutated persisted input")
	}

	blocked, ok := RecordReplacementAuthorization(
		operation,
		"unplanned-replacement-uid",
		now.Add(2*time.Second),
	)
	if ok ||
		blocked.Stage !=
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked ||
		blocked.Reason !=
			enterpriseApi.SearchHeadClusterLifecycleReasonMemberIdentityMismatch ||
		blocked.ReplacementAuthorizedAt != nil {
		t.Fatalf("changed target identity was not blocked: %#v", blocked)
	}
}

func TestCaptainTransferTimeoutBlocksReplacement(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	operation := newTestOperation(now)
	observation := safeObservation(now)
	observation.Captain = operation.TargetPod
	observation.CaptainTransferTarget = "example-search-head-1"
	observation.CaptainTransferTargetManagementURI = "https://example-search-head-1:8089"

	decision := EvaluateReplacement(operation, observation, testPolicy(), now)
	observation.TargetMemberStatus = "ManualDetention"
	decision = EvaluateReplacement(decision.Operation, observation, testPolicy(), now.Add(time.Second))
	decision = EvaluateReplacement(decision.Operation, observation, testPolicy(), now.Add(32*time.Second))

	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageBlocked, ActionNone)
	if decision.Operation.Reason != enterpriseApi.SearchHeadClusterLifecycleReasonCaptainTransferTimedOut {
		t.Fatalf("reason = %q, want %q",
			decision.Operation.Reason,
			enterpriseApi.SearchHeadClusterLifecycleReasonCaptainTransferTimedOut,
		)
	}
}

func TestCaptainTransferTimeoutDoesNotRequireAvailableObservation(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	operation := newTestOperation(now)
	operation.Stage = enterpriseApi.SearchHeadClusterLifecycleStageTransferringCaptain
	stageStartedAt := metav1.NewTime(now)
	operation.StageStartedAt = &stageStartedAt

	decision := EvaluateReplacement(
		operation,
		Observation{},
		testPolicy(),
		now.Add(30*time.Second),
	)

	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageBlocked, ActionNone)
	if decision.Operation.Reason != enterpriseApi.SearchHeadClusterLifecycleReasonCaptainTransferTimedOut {
		t.Fatalf("reason = %q, want %q",
			decision.Operation.Reason,
			enterpriseApi.SearchHeadClusterLifecycleReasonCaptainTransferTimedOut,
		)
	}
}

func TestExpiredCaptainTransferAcceptsFreshSuccessfulObservation(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	operation := newTestOperation(now)
	operation.Stage =
		enterpriseApi.SearchHeadClusterLifecycleStageTransferringCaptain
	stageStartedAt := metav1.NewTime(now)
	operation.StageStartedAt = &stageStartedAt
	requestedAt := metav1.NewTime(now.Add(time.Second))
	operation.CaptainTransferRequestedAt = &requestedAt
	operation.CaptainTransferTarget = "example-search-head-1"

	observedAt := now.Add(31 * time.Second)
	observation := safeObservation(observedAt)
	observation.TargetMemberStatus = "ManualDetention"
	observation.Captain = "example-search-head-1"

	decision := EvaluateReplacement(
		operation,
		observation,
		testPolicy(),
		observedAt,
	)

	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement,
		ActionAuthorizeReplacement,
	)
}

func TestStaleObservationCannotAdvanceReplacement(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	operation := newTestOperation(now)
	observation := safeObservation(now)
	observation.Fresh = false

	decision := EvaluateReplacement(operation, observation, testPolicy(), now)

	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageValidatingCluster, ActionObserveCluster)
	if decision.Operation.Reason != enterpriseApi.SearchHeadClusterLifecycleReasonObservationStale {
		t.Fatalf("reason = %q, want %q",
			decision.Operation.Reason,
			enterpriseApi.SearchHeadClusterLifecycleReasonObservationStale,
		)
	}
}

func TestConflictingCaptainObservationBlocksReplacement(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	operation := newTestOperation(now)
	observation := safeObservation(now)
	observation.ConflictingCaptain = true

	decision := EvaluateReplacement(operation, observation, testPolicy(), now)

	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageBlocked, ActionNone)
	if decision.Operation.Reason != enterpriseApi.SearchHeadClusterLifecycleReasonConflictingCaptainObservation {
		t.Fatalf("reason = %q, want %q",
			decision.Operation.Reason,
			enterpriseApi.SearchHeadClusterLifecycleReasonConflictingCaptainObservation,
		)
	}
}

func TestPostTransferCaptainDisagreementWaitsForConvergence(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	operation := newTestOperation(now)
	operation.Stage = enterpriseApi.SearchHeadClusterLifecycleStageTransferringCaptain
	stageStartedAt := metav1.NewTime(now)
	operation.StageStartedAt = &stageStartedAt
	requestedAt := metav1.NewTime(now.Add(time.Second))
	operation.CaptainTransferRequestedAt = &requestedAt

	observation := safeObservation(now.Add(2 * time.Second))
	observation.Captain = operation.TargetPod
	observation.ConflictingCaptain = true

	decision := EvaluateReplacement(
		operation,
		observation,
		testPolicy(),
		now.Add(2*time.Second),
	)

	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageTransferringCaptain,
		ActionObserveCluster,
	)
	if decision.Operation.Reason != enterpriseApi.SearchHeadClusterLifecycleReasonCaptainTransferRequired {
		t.Fatalf("reason = %q, want %q",
			decision.Operation.Reason,
			enterpriseApi.SearchHeadClusterLifecycleReasonCaptainTransferRequired,
		)
	}

	decision = EvaluateReplacement(
		decision.Operation,
		observation,
		testPolicy(),
		now.Add(31*time.Second),
	)
	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageBlocked, ActionNone)
	if decision.Operation.Reason != enterpriseApi.SearchHeadClusterLifecycleReasonCaptainTransferTimedOut {
		t.Fatalf("reason = %q, want %q",
			decision.Operation.Reason,
			enterpriseApi.SearchHeadClusterLifecycleReasonCaptainTransferTimedOut,
		)
	}
}

func TestReplacementCannotAuthorizeWithoutPersistentMemberIdentity(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	operation := newTestOperation(now)
	observation := safeObservation(now)
	observation.TargetMemberID = ""
	observation.TargetMemberStatus = "ManualDetention"

	decision := EvaluateReplacement(
		operation,
		observation,
		testPolicy(),
		now,
	)

	assertDecision(
		t,
		decision,
		enterpriseApi.SearchHeadClusterLifecycleStageValidatingCluster,
		ActionObserveCluster,
	)
	if decision.Operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonClusterNotSafe {
		t.Fatalf(
			"reason = %q, want ClusterNotSafe",
			decision.Operation.Reason,
		)
	}
	if decision.Operation.TargetMemberID != "" {
		t.Fatalf(
			"missing persistent identity was recorded as %q",
			decision.Operation.TargetMemberID,
		)
	}
}

func TestScaleDownCompletesOnlyAfterRemovedOrdinalIsObservedGone(t *testing.T) {
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	target := int32(2)
	operation := StartReplacement(
		"scale-down:example-search-head-2",
		enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown,
		"revision-2",
		"example-search-head-2",
		target,
		now,
	)
	operation.Stage =
		enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement
	requestedAt := metav1.NewTime(now.Add(time.Second))
	operation.MembershipRemovalRequestedAt = &requestedAt

	decision := CompleteScaleDown(operation, 3, now.Add(2*time.Second))
	if decision.Stage !=
		enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement {
		t.Fatalf(
			"scale-down completed while target still existed: %q",
			decision.Stage,
		)
	}

	decision = CompleteScaleDown(
		decision,
		2,
		now.Add(3*time.Second),
	)
	if decision.Stage !=
		enterpriseApi.SearchHeadClusterLifecycleStageCompleted {
		t.Fatalf("scale-down stage = %q, want Completed", decision.Stage)
	}
	if decision.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonOperationCompleted {
		t.Fatalf(
			"scale-down reason = %q, want OperationCompleted",
			decision.Reason,
		)
	}
	if operation.Stage ==
		enterpriseApi.SearchHeadClusterLifecycleStageCompleted {
		t.Fatal("scale-down completion mutated persisted input")
	}
}

func TestScaleDownCancellationRequiresIntactMembershipAndTarget(t *testing.T) {
	now := time.Date(2026, 7, 28, 4, 35, 0, 0, time.UTC)
	target := int32(3)
	operation := StartReplacement(
		"scale-down:example-search-head-3",
		enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown,
		"",
		"example-search-head-3",
		target,
		now.Add(-time.Minute),
	)
	operation.Stage = enterpriseApi.SearchHeadClusterLifecycleStageBlocked

	cancelled, started := StartScaleDownCancellation(
		operation,
		4,
		4,
		now,
	)
	if !started {
		t.Fatal("restored replica intent did not start scale-down cancellation")
	}
	if cancelled.Stage !=
		enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery ||
		cancelled.Reason !=
			enterpriseApi.SearchHeadClusterLifecycleReasonScaleDownCancelled ||
		cancelled.MemberRejoinStartedAt == nil {
		t.Fatalf("scale-down cancellation = %#v", cancelled)
	}
	if operation.Stage != enterpriseApi.SearchHeadClusterLifecycleStageBlocked {
		t.Fatal("scale-down cancellation mutated persisted input")
	}

	resumed, started := StartScaleDownCancellation(cancelled, 4, 4, now)
	if started ||
		resumed.Stage !=
			enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery {
		t.Fatalf("durable cancellation did not resume idempotently: %#v", resumed)
	}

	removed := operation.DeepCopy()
	requestedAt := metav1.NewTime(now)
	removed.MembershipRemovalRequestedAt = &requestedAt
	notCancelled, started := StartScaleDownCancellation(removed, 4, 4, now)
	if started ||
		notCancelled.Stage !=
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked {
		t.Fatal("membership removal was incorrectly treated as cancellable")
	}

	notCancelled, started = StartScaleDownCancellation(operation, 3, 4, now)
	if started ||
		notCancelled.Stage !=
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked {
		t.Fatal("missing target ordinal was incorrectly treated as cancellable")
	}
}

func TestPodUpdateCancellationRequiresSupersededRevisionAndOriginalPod(t *testing.T) {
	now := time.Date(2026, 7, 28, 6, 45, 0, 0, time.UTC)
	target := int32(2)
	operation := StartReplacement(
		"PodUpdate:example-search-head-2:revision-2:2",
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		"revision-2",
		"example-search-head-2",
		target,
		now.Add(-time.Minute),
	)
	operation.Stage = enterpriseApi.SearchHeadClusterLifecycleStageBlocked
	operation.TargetPodUID = "original-pod-uid"

	cancelled, started := StartPodUpdateCancellation(
		operation,
		"revision-1",
		now,
	)
	if !started {
		t.Fatal("withdrawn Pod revision did not start cancellation")
	}
	if cancelled.Stage !=
		enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery ||
		cancelled.Reason !=
			enterpriseApi.SearchHeadClusterLifecycleReasonPodUpdateCancelled ||
		cancelled.MemberRejoinStartedAt == nil {
		t.Fatalf("Pod-update cancellation = %#v", cancelled)
	}
	if operation.Stage != enterpriseApi.SearchHeadClusterLifecycleStageBlocked {
		t.Fatal("Pod-update cancellation mutated persisted input")
	}

	resumed, started := StartPodUpdateCancellation(
		cancelled,
		"revision-1",
		now,
	)
	if started ||
		resumed.Stage !=
			enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery {
		t.Fatalf("durable cancellation did not resume idempotently: %#v", resumed)
	}

	authorized := operation.DeepCopy()
	authorizedAt := metav1.NewTime(now)
	authorized.ReplacementAuthorizedAt = &authorizedAt
	notCancelled, started := StartPodUpdateCancellation(
		authorized,
		"revision-1",
		now,
	)
	if started ||
		notCancelled.Stage !=
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked {
		t.Fatal("authorized replacement was incorrectly treated as cancellable")
	}

	notCancelled, started = StartPodUpdateCancellation(
		operation,
		"revision-2",
		now,
	)
	if started ||
		notCancelled.Stage !=
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked {
		t.Fatal("unchanged desired revision was incorrectly treated as cancellable")
	}

	identityMissing := operation.DeepCopy()
	identityMissing.TargetPodUID = ""
	notCancelled, started = StartPodUpdateCancellation(
		identityMissing,
		"revision-1",
		now,
	)
	if started ||
		notCancelled.Stage !=
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked {
		t.Fatal("operation without original Pod identity was incorrectly cancelled")
	}
}

func TestSearchDrainContinuationRequiresExactPostTimeoutApproval(t *testing.T) {
	now := time.Date(2026, 7, 28, 9, 30, 0, 0, time.UTC)
	operation := newTestOperation(now.Add(-time.Minute))
	operation.TargetPodUID = "original-pod-uid"
	operation.Stage =
		enterpriseApi.SearchHeadClusterLifecycleStageDrainingSearches
	stageStartedAt := metav1.NewTime(now.Add(-31 * time.Second))
	operation.StageStartedAt = &stageStartedAt
	observation := safeObservation(now)
	observation.TargetMemberStatus = "ManualDetention"
	observation.ActiveHistoricalSearches = 2
	observation.ActiveRealtimeSearches = 1

	blocked := EvaluateReplacement(
		operation,
		observation,
		testPolicy(),
		now,
	)
	if blocked.Operation.Stage !=
		enterpriseApi.SearchHeadClusterLifecycleStageBlocked ||
		blocked.Operation.Reason !=
			enterpriseApi.SearchHeadClusterLifecycleReasonSearchDrainTimedOut ||
		len(blocked.Operation.SearchDrainContinuationToken) != 64 {
		t.Fatalf("search-drain timeout = %#v", blocked.Operation)
	}
	if operation.SearchDrainContinuationToken != "" {
		t.Fatal("timeout evaluation mutated persisted input")
	}

	wrongToken := &enterpriseApi.SearchHeadClusterLifecycleApproval{
		OperationID: blocked.Operation.OperationID,
		Token:       "wrong-token",
		Action: enterpriseApi.
			SearchHeadClusterLifecycleApprovalActionContinueAfterSearchDrainTimeout,
	}
	notApproved, applied := ApplySearchDrainContinuationApproval(
		blocked.Operation,
		wrongToken,
		7,
		2,
		1,
		now.Add(time.Second),
	)
	if applied ||
		notApproved.Stage !=
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked {
		t.Fatalf("mismatched token applied: %#v", notApproved)
	}

	wrongOperation := *wrongToken
	wrongOperation.OperationID = "different-operation"
	wrongOperation.Token = blocked.Operation.SearchDrainContinuationToken
	notApproved, applied = ApplySearchDrainContinuationApproval(
		blocked.Operation,
		&wrongOperation,
		7,
		2,
		1,
		now.Add(time.Second),
	)
	if applied ||
		notApproved.Stage !=
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked {
		t.Fatalf("mismatched operation ID applied: %#v", notApproved)
	}

	approval := *wrongToken
	approval.Token = blocked.Operation.SearchDrainContinuationToken
	approved, applied := ApplySearchDrainContinuationApproval(
		blocked.Operation,
		&approval,
		7,
		2,
		1,
		now.Add(2*time.Second),
	)
	if !applied ||
		approved.Stage !=
			enterpriseApi.SearchHeadClusterLifecycleStageDrainingSearches ||
		approved.Reason !=
			enterpriseApi.
				SearchHeadClusterLifecycleReasonSearchDrainContinuationApproved ||
		approved.SearchDrainContinuationApprovedAt == nil ||
		approved.SearchDrainContinuationApprovalGeneration != 7 ||
		approved.ApprovedActiveHistoricalSearches != 2 ||
		approved.ApprovedActiveRealtimeSearches != 1 {
		t.Fatalf("approved continuation = %#v", approved)
	}
	if blocked.Operation.SearchDrainContinuationApprovedAt != nil {
		t.Fatal("approval mutated blocked input")
	}

	// Even after another full drain timeout, a persisted approval bypasses
	// only the search-count wait and still re-evaluates cluster/captain safety.
	observation.ObservedAt = now.Add(10 * time.Minute)
	continued := EvaluateReplacement(
		approved,
		observation,
		testPolicy(),
		now.Add(10*time.Minute),
	)
	assertDecision(
		t,
		continued,
		enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement,
		ActionAuthorizeReplacement,
	)

	duplicate, applied := ApplySearchDrainContinuationApproval(
		approved,
		&approval,
		8,
		2,
		1,
		now.Add(3*time.Second),
	)
	if applied ||
		duplicate.SearchDrainContinuationApprovalGeneration != 7 {
		t.Fatalf("approval was applied more than once: %#v", duplicate)
	}

	preTimeout := operation.DeepCopy()
	preTimeout.SearchDrainContinuationToken =
		blocked.Operation.SearchDrainContinuationToken
	preApproved, applied := ApplySearchDrainContinuationApproval(
		preTimeout,
		&approval,
		7,
		2,
		1,
		now.Add(time.Second),
	)
	if applied ||
		preApproved.Stage !=
			enterpriseApi.SearchHeadClusterLifecycleStageDrainingSearches {
		t.Fatalf("pre-timeout approval was accepted: %#v", preApproved)
	}
}

func TestCaptainChangeDuringDrainIsReobserved(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	operation := newTestOperation(now)
	observation := safeObservation(now)

	decision := EvaluateReplacement(operation, observation, testPolicy(), now)
	observation.TargetMemberStatus = "ManualDetention"
	observation.ActiveHistoricalSearches = 1
	decision = EvaluateReplacement(decision.Operation, observation, testPolicy(), now.Add(time.Second))

	observation.ActiveHistoricalSearches = 0
	observation.Captain = operation.TargetPod
	observation.CaptainTransferTarget = "example-search-head-1"
	observation.CaptainTransferTargetManagementURI = "https://example-search-head-1:8089"
	decision = EvaluateReplacement(decision.Operation, observation, testPolicy(), now.Add(2*time.Second))

	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageTransferringCaptain, ActionTransferCaptain)
}

func TestEvaluationDoesNotMutatePersistedInput(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	operation := newTestOperation(now)
	observation := safeObservation(now)

	decision := EvaluateReplacement(operation, observation, testPolicy(), now)

	if operation.Stage != enterpriseApi.SearchHeadClusterLifecycleStageValidatingCluster {
		t.Fatalf("input stage mutated to %q", operation.Stage)
	}
	if decision.Operation == operation {
		t.Fatal("decision reused input operation pointer")
	}
}

func TestStartAuthorizedPodUpdateRevisionRecoveryRecordsDurableBarrier(
	t *testing.T,
) {
	now := time.Date(2026, 7, 29, 18, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	operation.Stage =
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForScheduling
	operation.Reason =
		enterpriseApi.SearchHeadClusterLifecycleReasonPodUnschedulable
	operation.ReplacementPodUID = "failed-replacement-uid"
	observedAt := metav1.NewTime(now.Add(time.Second))
	operation.ReplacementPodObservedAt = &observedAt
	operation.MemberRejoinStartedAt = &observedAt

	recovery, started := StartAuthorizedPodUpdateRevisionRecovery(
		operation,
		"revision-1",
		now.Add(2*time.Second),
	)
	if !started {
		t.Fatal("authorized revision recovery was not started")
	}
	if operation.RecoveryRevision != "" ||
		operation.ReplacementPodUID != "failed-replacement-uid" {
		t.Fatalf("input operation was mutated: %#v", operation)
	}
	if recovery.DesiredRevision != "revision-2" ||
		recovery.RecoveryRevision != "revision-1" ||
		recovery.RevisionWithdrawalStartedAt == nil ||
		recovery.Stage !=
			enterpriseApi.SearchHeadClusterLifecycleStageWaitingForScheduling ||
		recovery.Reason !=
			enterpriseApi.
				SearchHeadClusterLifecycleReasonAuthorizedRevisionWithdrawn {
		t.Fatalf("revision recovery = %#v", recovery)
	}
	if recovery.ReplacementPodUID != "" ||
		recovery.ReplacementPodObservedAt != nil ||
		recovery.MemberRejoinStartedAt != nil {
		t.Fatalf("failed replacement identity was retained: %#v", recovery)
	}
}

func TestAuthorizedPodUpdateRevisionRecoveryRequiresAttributableFailure(
	t *testing.T,
) {
	now := time.Date(2026, 7, 29, 18, 0, 0, 0, time.UTC)
	operation := authorizedRecoveryOperation(now)
	operation.Stage =
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForContainer
	operation.Reason =
		enterpriseApi.SearchHeadClusterLifecycleReasonReplacementAuthorized

	if AuthorizedPodUpdateRevisionRecoveryEligible(operation) {
		t.Fatal("generic container startup wait was eligible for revision recovery")
	}
	recovery, started := StartAuthorizedPodUpdateRevisionRecovery(
		operation,
		"revision-1",
		now.Add(time.Second),
	)
	if started || recovery.RecoveryRevision != "" {
		t.Fatalf("unsafe revision recovery started: %#v", recovery)
	}

	operation.Reason =
		enterpriseApi.SearchHeadClusterLifecycleReasonImagePullFailed
	if !AuthorizedPodUpdateRevisionRecoveryEligible(operation) {
		t.Fatal("attributable image-pull failure was not eligible")
	}
}

func newTestOperation(now time.Time) *enterpriseApi.SearchHeadClusterLifecycleOperationStatus {
	return StartReplacement(
		"operation-1",
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		"revision-2",
		"example-search-head-2",
		2,
		now,
	)
}

func safeObservation(now time.Time) Observation {
	return Observation{
		ObservedAt:                  now,
		Available:                   true,
		Fresh:                       true,
		Initialized:                 true,
		MinPeersJoined:              true,
		Captain:                     "example-search-head-0",
		CaptainReady:                true,
		TargetMemberObserved:        true,
		TargetMemberID:              "member-guid-2",
		TargetMemberStatus:          "Up",
		TargetMemberRegistered:      true,
		KVStoreObservationRequired:  true,
		KVStoreObservationAvailable: true,
		TargetKVStoreReady:          true,
	}
}

func testPolicy() ReplacementPolicy {
	return ReplacementPolicy{
		DetentionTimeout:       30 * time.Second,
		SearchDrainTimeout:     30 * time.Second,
		CaptainTransferTimeout: 30 * time.Second,
	}
}

func assertDecision(
	t *testing.T,
	decision Decision,
	stage enterpriseApi.SearchHeadClusterLifecycleStage,
	action ActionType,
) {
	t.Helper()
	if decision.Operation == nil {
		t.Fatal("decision operation is nil")
	}
	if decision.Operation.Stage != stage {
		t.Fatalf("stage = %q, want %q", decision.Operation.Stage, stage)
	}
	if decision.Action.Type != action {
		t.Fatalf("action = %q, want %q", decision.Action.Type, action)
	}
}
