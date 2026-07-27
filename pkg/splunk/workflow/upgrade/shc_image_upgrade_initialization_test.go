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

package upgrade

import (
	"math"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

func TestSHCImageUpgradeInitializationRequiresPersistenceBarriers(t *testing.T) {
	now := time.Date(2026, 7, 25, 13, 0, 0, 0, time.UTC)
	operation := pendingImageUpgrade(now.Add(-time.Minute))

	intent := EvaluateSHCImageUpgradeInitialization(
		SHCImageUpgradeInitializationInput{
			Current:                  operation,
			CoordinationOwned:        true,
			ManagementTargetEligible: true,
			KVStoreReady:             true,
			Now:                      now,
		},
	)
	assertInitializationAction(
		t,
		intent,
		SHCImageUpgradeInitializationPersist,
	)
	if intent.Operation.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseInitializing ||
		intent.Operation.InitializationIntentAt == nil ||
		intent.Operation.InitializationAttemptCount != 0 ||
		intent.Operation.InitializationLastAttemptAt != nil {
		t.Fatalf("intent operation = %#v", intent.Operation)
	}
	if operation.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhasePendingInitialization {
		t.Fatal("intent transition mutated persisted input")
	}

	call := EvaluateSHCImageUpgradeInitialization(
		SHCImageUpgradeInitializationInput{
			Current:                  intent.Operation,
			CoordinationOwned:        true,
			ManagementTargetEligible: true,
			KVStoreReady:             true,
			Now:                      now.Add(time.Second),
		},
	)
	assertInitializationAction(t, call, SHCImageUpgradeInitializationCall)

	success := RecordSHCImageUpgradeInitializationAttempt(
		call.Operation,
		true,
		now.Add(2*time.Second),
	)
	assertInitializationAction(
		t,
		success,
		SHCImageUpgradeInitializationPersist,
	)
	if success.Operation.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseInitializing ||
		success.Operation.InitializationSucceededAt == nil ||
		success.Operation.InitializationAttemptCount != 1 {
		t.Fatalf("success operation = %#v", success.Operation)
	}

	advance := EvaluateSHCImageUpgradeInitialization(
		SHCImageUpgradeInitializationInput{
			Current:                  success.Operation,
			CoordinationOwned:        true,
			ManagementTargetEligible: true,
			KVStoreReady:             true,
			Now:                      now.Add(3 * time.Second),
		},
	)
	assertInitializationAction(
		t,
		advance,
		SHCImageUpgradeInitializationPersist,
	)
	if advance.Operation.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseRollingMembers {
		t.Fatalf("advance operation = %#v", advance.Operation)
	}

	allow := EvaluateSHCImageUpgradeInitialization(
		SHCImageUpgradeInitializationInput{
			Current:                  advance.Operation,
			CoordinationOwned:        true,
			ManagementTargetEligible: true,
			KVStoreReady:             true,
			Now:                      now.Add(4 * time.Second),
		},
	)
	assertInitializationAction(
		t,
		allow,
		SHCImageUpgradeInitializationAllowMembers,
	)
}

func TestSHCImageUpgradeInitializationWaitsForCoordinationBeforeIntent(t *testing.T) {
	now := time.Date(2026, 7, 25, 13, 0, 0, 0, time.UTC)
	operation := pendingImageUpgrade(now.Add(-time.Minute))

	decision := EvaluateSHCImageUpgradeInitialization(
		SHCImageUpgradeInitializationInput{
			Current:                  operation,
			ManagementTargetEligible: true,
			Now:                      now,
		},
	)

	assertInitializationAction(
		t,
		decision,
		SHCImageUpgradeInitializationWait,
	)
	if decision.Operation.InitializationIntentAt != nil ||
		decision.Operation.Phase !=
			enterpriseApi.SearchHeadClusterImageUpgradePhasePendingInitialization {
		t.Fatalf("coordination wait changed operation: %#v", decision.Operation)
	}
}

func TestSHCImageUpgradeInitializationWaitsToRecoverCoordinationAfterIntent(t *testing.T) {
	now := time.Date(2026, 7, 25, 13, 0, 0, 0, time.UTC)
	operation := initializingImageUpgrade(now.Add(-time.Minute))

	decision := EvaluateSHCImageUpgradeInitialization(
		SHCImageUpgradeInitializationInput{
			Current:                  operation,
			ManagementTargetEligible: true,
			Now:                      now,
		},
	)

	assertInitializationAction(
		t,
		decision,
		SHCImageUpgradeInitializationWait,
	)
	if decision.Operation.InitializationAttemptCount != 0 ||
		decision.Operation.InitializationLastAttemptAt != nil {
		t.Fatalf("coordination recovery wait recorded an attempt: %#v", decision)
	}
}

func TestSHCImageUpgradeInitializationBlocksConflictingOwner(t *testing.T) {
	now := time.Date(2026, 7, 25, 13, 0, 0, 0, time.UTC)
	operation := pendingImageUpgrade(now.Add(-time.Minute))

	decision := EvaluateSHCImageUpgradeInitialization(
		SHCImageUpgradeInitializationInput{
			Current:                     operation,
			CoordinationOwned:           true,
			ConflictingPlannedOperation: true,
			ManagementTargetEligible:    true,
			Now:                         now,
		},
	)

	assertInitializationAction(
		t,
		decision,
		SHCImageUpgradeInitializationBlock,
	)
	if decision.Operation.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseBlocked ||
		decision.Reason != enterpriseApi.
			SearchHeadClusterImageUpgradeReasonConflictingPlannedOperation {
		t.Fatalf("conflict decision = %#v", decision)
	}
	if operation.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhasePendingInitialization {
		t.Fatal("conflict transition mutated persisted input")
	}
}

func TestSHCImageUpgradeInitializationRequiresEligibleTargetForCall(t *testing.T) {
	now := time.Date(2026, 7, 25, 13, 0, 0, 0, time.UTC)
	operation := initializingImageUpgrade(now.Add(-time.Minute))

	decision := EvaluateSHCImageUpgradeInitialization(
		SHCImageUpgradeInitializationInput{
			Current:           operation,
			CoordinationOwned: true,
			Now:               now,
		},
	)

	assertInitializationAction(
		t,
		decision,
		SHCImageUpgradeInitializationWait,
	)
	if decision.Operation.InitializationAttemptCount != 0 ||
		decision.Operation.InitializationLastAttemptAt != nil {
		t.Fatalf("target wait recorded an attempt: %#v", decision.Operation)
	}
}

func TestSHCImageUpgradeInitializationWaitsForKVStorePreflight(t *testing.T) {
	now := time.Date(2026, 7, 25, 13, 0, 0, 0, time.UTC)
	operation := initializingImageUpgrade(now.Add(-time.Minute))

	decision := EvaluateSHCImageUpgradeInitialization(
		SHCImageUpgradeInitializationInput{
			Current:                  operation,
			CoordinationOwned:        true,
			ManagementTargetEligible: true,
			KVStoreReady:             false,
			KVStoreMessage: "wait for every Search Head KV Store to report " +
				"ready: example-search-head-1=starting",
			Now: now,
		},
	)

	assertInitializationAction(
		t,
		decision,
		SHCImageUpgradeInitializationWait,
	)
	if decision.Reason !=
		enterpriseApi.SearchHeadClusterImageUpgradeReasonClusterNotReady ||
		decision.Message !=
			"wait for every Search Head KV Store to report ready: example-search-head-1=starting" {
		t.Fatalf("KV Store preflight decision = %#v", decision)
	}
	if decision.Operation.InitializationAttemptCount != 0 ||
		decision.Operation.InitializationLastAttemptAt != nil {
		t.Fatalf(
			"KV Store preflight wait recorded an endpoint attempt: %#v",
			decision.Operation,
		)
	}
}

func TestSHCImageUpgradeInitializationFailureRemainsRetryable(t *testing.T) {
	now := time.Date(2026, 7, 25, 13, 0, 0, 0, time.UTC)
	operation := initializingImageUpgrade(now.Add(-time.Minute))

	failed := RecordSHCImageUpgradeInitializationAttempt(operation, false, now)

	assertInitializationAction(
		t,
		failed,
		SHCImageUpgradeInitializationPersist,
	)
	if failed.Operation.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseInitializing ||
		failed.Operation.Reason != enterpriseApi.
			SearchHeadClusterImageUpgradeReasonInitializationRetrying ||
		failed.Operation.InitializationAttemptCount != 1 ||
		failed.Operation.InitializationLastAttemptAt == nil ||
		failed.Operation.InitializationSucceededAt != nil {
		t.Fatalf("failed attempt = %#v", failed.Operation)
	}

	retry := EvaluateSHCImageUpgradeInitialization(
		SHCImageUpgradeInitializationInput{
			Current:                  failed.Operation,
			CoordinationOwned:        true,
			ManagementTargetEligible: true,
			KVStoreReady:             true,
			Now:                      now.Add(time.Second),
		},
	)
	assertInitializationAction(t, retry, SHCImageUpgradeInitializationCall)
}

func TestSHCImageUpgradeInitializationRejectsAttemptBeforePersistedIntent(t *testing.T) {
	now := time.Date(2026, 7, 25, 13, 0, 0, 0, time.UTC)
	operation := pendingImageUpgrade(now.Add(-time.Minute))

	decision := RecordSHCImageUpgradeInitializationAttempt(
		operation,
		true,
		now,
	)

	assertInitializationAction(
		t,
		decision,
		SHCImageUpgradeInitializationBlock,
	)
	if decision.Operation.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhasePendingInitialization ||
		decision.Operation.InitializationAttemptCount != 0 ||
		decision.Operation.InitializationSucceededAt != nil {
		t.Fatalf("pre-intent attempt changed operation: %#v", decision)
	}
}

func TestPersistedSHCImageUpgradeInitializationSuccessIsNotCalledAgain(t *testing.T) {
	now := time.Date(2026, 7, 25, 13, 0, 0, 0, time.UTC)
	operation := initializingImageUpgrade(now.Add(-time.Minute))
	succeeded := RecordSHCImageUpgradeInitializationAttempt(operation, true, now)

	replayedAttempt := RecordSHCImageUpgradeInitializationAttempt(
		succeeded.Operation,
		true,
		now.Add(time.Second),
	)
	assertInitializationAction(
		t,
		replayedAttempt,
		SHCImageUpgradeInitializationWait,
	)
	if replayedAttempt.Operation.InitializationAttemptCount != 1 ||
		!replayedAttempt.Operation.InitializationSucceededAt.Equal(
			succeeded.Operation.InitializationSucceededAt,
		) {
		t.Fatalf("replayed success changed attempt evidence: %#v", replayedAttempt)
	}

	resume := EvaluateSHCImageUpgradeInitialization(
		SHCImageUpgradeInitializationInput{
			Current:                  succeeded.Operation,
			CoordinationOwned:        true,
			ManagementTargetEligible: true,
			Now:                      now.Add(2 * time.Second),
		},
	)
	assertInitializationAction(
		t,
		resume,
		SHCImageUpgradeInitializationPersist,
	)
}

func TestSHCImageUpgradeInitializationRepairsMissingIntentWithoutCalling(t *testing.T) {
	now := time.Date(2026, 7, 25, 13, 0, 0, 0, time.UTC)
	operation := pendingImageUpgrade(now.Add(-time.Minute))
	operation.Phase =
		enterpriseApi.SearchHeadClusterImageUpgradePhaseInitializing

	decision := EvaluateSHCImageUpgradeInitialization(
		SHCImageUpgradeInitializationInput{
			Current:                  operation,
			CoordinationOwned:        true,
			ManagementTargetEligible: true,
			Now:                      now,
		},
	)

	assertInitializationAction(
		t,
		decision,
		SHCImageUpgradeInitializationPersist,
	)
	if decision.Operation.InitializationIntentAt == nil {
		t.Fatalf("missing intent was not repaired: %#v", decision.Operation)
	}
}

func TestSHCImageUpgradeInitializationAttemptCountIsBounded(t *testing.T) {
	now := time.Date(2026, 7, 25, 13, 0, 0, 0, time.UTC)
	operation := initializingImageUpgrade(now.Add(-time.Minute))
	operation.InitializationAttemptCount = math.MaxInt32

	decision := RecordSHCImageUpgradeInitializationAttempt(
		operation,
		false,
		now,
	)

	if decision.Operation.InitializationAttemptCount != math.MaxInt32 {
		t.Fatalf(
			"attempt count = %d, want bounded %d",
			decision.Operation.InitializationAttemptCount,
			math.MaxInt32,
		)
	}
}

func TestSHCImageUpgradeInitializationDoesNotAuthorizeOtherPhases(t *testing.T) {
	now := time.Date(2026, 7, 25, 13, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		phase  enterpriseApi.SearchHeadClusterImageUpgradePhase
		action SHCImageUpgradeInitializationAction
	}{
		{
			name:   "pending finalization",
			phase:  enterpriseApi.SearchHeadClusterImageUpgradePhasePendingFinalization,
			action: SHCImageUpgradeInitializationWait,
		},
		{
			name:   "completed",
			phase:  enterpriseApi.SearchHeadClusterImageUpgradePhaseCompleted,
			action: SHCImageUpgradeInitializationWait,
		},
		{
			name:   "blocked",
			phase:  enterpriseApi.SearchHeadClusterImageUpgradePhaseBlocked,
			action: SHCImageUpgradeInitializationBlock,
		},
		{
			name:   "failed",
			phase:  enterpriseApi.SearchHeadClusterImageUpgradePhaseFailed,
			action: SHCImageUpgradeInitializationBlock,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := initializingImageUpgrade(now.Add(-time.Minute))
			operation.Phase = test.phase

			decision := EvaluateSHCImageUpgradeInitialization(
				SHCImageUpgradeInitializationInput{
					Current:                  operation,
					CoordinationOwned:        true,
					ManagementTargetEligible: true,
					Now:                      now,
				},
			)

			assertInitializationAction(t, decision, test.action)
		})
	}
}

func pendingImageUpgrade(
	startedAt time.Time,
) *enterpriseApi.SearchHeadClusterImageUpgradeStatus {
	input := supportedImageUpgradeInput()
	input.Now = startedAt
	return recordedImageUpgrade(input)
}

func initializingImageUpgrade(
	intentAt time.Time,
) *enterpriseApi.SearchHeadClusterImageUpgradeStatus {
	operation := pendingImageUpgrade(intentAt.Add(-time.Minute))
	decision := EvaluateSHCImageUpgradeInitialization(
		SHCImageUpgradeInitializationInput{
			Current:           operation,
			CoordinationOwned: true,
			Now:               intentAt,
		},
	)
	return decision.Operation
}

func assertInitializationAction(
	t *testing.T,
	decision SHCImageUpgradeInitializationDecision,
	action SHCImageUpgradeInitializationAction,
) {
	t.Helper()
	if decision.Action != action {
		t.Fatalf("action = %q, want %q: %#v", decision.Action, action, decision)
	}
	if decision.Operation != nil && decision.Operation.OperationID == "" {
		t.Fatalf("decision lost operation identity: %#v", decision)
	}
}
