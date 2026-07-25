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

func TestCaptainReplacementRequiresConfirmedTransfer(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	operation := newTestOperation(now)
	observation := safeObservation(now)
	observation.Captain = operation.TargetPod
	observation.CaptainTransferTarget = "example-search-head-1"

	decision := EvaluateReplacement(operation, observation, testPolicy(), now)
	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget, ActionRequestDetention)

	observation.TargetMemberStatus = "ManualDetention"
	decision = EvaluateReplacement(decision.Operation, observation, testPolicy(), now.Add(time.Second))
	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageTransferringCaptain, ActionTransferCaptain)
	if decision.Action.Target != "example-search-head-1" {
		t.Fatalf("captain transfer target = %q, want example-search-head-1", decision.Action.Target)
	}

	// A successful command response is not represented as completion. The
	// operation remains in transfer until a fresh observation reports a
	// different, ready captain.
	decision = EvaluateReplacement(decision.Operation, observation, testPolicy(), now.Add(2*time.Second))
	assertDecision(t, decision, enterpriseApi.SearchHeadClusterLifecycleStageTransferringCaptain, ActionTransferCaptain)

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

func TestCaptainTransferTimeoutBlocksReplacement(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	operation := newTestOperation(now)
	observation := safeObservation(now)
	observation.Captain = operation.TargetPod
	observation.CaptainTransferTarget = "example-search-head-1"

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
		ObservedAt:             now,
		Available:              true,
		Fresh:                  true,
		Initialized:            true,
		MinPeersJoined:         true,
		Captain:                "example-search-head-0",
		CaptainReady:           true,
		TargetMemberObserved:   true,
		TargetMemberStatus:     "Up",
		TargetMemberRegistered: true,
	}
}

func testPolicy() ReplacementPolicy {
	return ReplacementPolicy{
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
