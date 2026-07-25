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
	"reflect"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSHCImageUpgradeCompletedOrdinalIsUniqueAndStable(t *testing.T) {
	now := time.Date(2026, 7, 25, 16, 0, 0, 0, time.UTC)
	operation := rollingImageUpgrade(now.Add(-time.Hour))

	first := RecordSHCImageUpgradeCompletedOrdinal(operation, 2, now)
	assertOrdinalAction(t, first, SHCImageUpgradeOrdinalPersist)
	if !reflect.DeepEqual(first.Operation.CompletedOrdinals, []int32{2}) ||
		first.Operation.LastTransitionTime == nil {
		t.Fatalf("first completed ordinal = %#v", first.Operation)
	}
	firstTransition := first.Operation.LastTransitionTime.DeepCopy()
	if len(operation.CompletedOrdinals) != 0 {
		t.Fatal("ordinal projection mutated persisted input")
	}

	duplicate := RecordSHCImageUpgradeCompletedOrdinal(
		first.Operation,
		2,
		now.Add(time.Minute),
	)
	assertOrdinalAction(t, duplicate, SHCImageUpgradeOrdinalWait)
	if !reflect.DeepEqual(duplicate.Operation.CompletedOrdinals, []int32{2}) ||
		!duplicate.Operation.LastTransitionTime.Equal(firstTransition) {
		t.Fatalf("duplicate ordinal changed status: %#v", duplicate.Operation)
	}

	second := RecordSHCImageUpgradeCompletedOrdinal(
		duplicate.Operation,
		1,
		now.Add(2*time.Minute),
	)
	third := RecordSHCImageUpgradeCompletedOrdinal(
		second.Operation,
		0,
		now.Add(3*time.Minute),
	)
	if !reflect.DeepEqual(
		third.Operation.CompletedOrdinals,
		[]int32{0, 1, 2},
	) {
		t.Fatalf("completed ordinals = %v", third.Operation.CompletedOrdinals)
	}
}

func TestSHCImageUpgradeCompletedOrdinalRejectsPrematureOrInvalidObservation(t *testing.T) {
	now := time.Date(2026, 7, 25, 16, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		current *enterpriseApi.SearchHeadClusterImageUpgradeStatus
		ordinal int32
	}{
		{name: "missing operation", ordinal: 0},
		{
			name:    "negative ordinal",
			current: rollingImageUpgrade(now),
			ordinal: -1,
		},
		{
			name:    "ordinal outside captured replicas",
			current: rollingImageUpgrade(now),
			ordinal: 3,
		},
		{
			name: "initialization success missing",
			current: func() *enterpriseApi.SearchHeadClusterImageUpgradeStatus {
				operation := rollingImageUpgrade(now)
				operation.InitializationSucceededAt = nil
				return operation
			}(),
			ordinal: 2,
		},
		{
			name: "wrong phase",
			current: func() *enterpriseApi.SearchHeadClusterImageUpgradeStatus {
				operation := rollingImageUpgrade(now)
				operation.Phase =
					enterpriseApi.SearchHeadClusterImageUpgradePhaseInitializing
				return operation
			}(),
			ordinal: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := RecordSHCImageUpgradeCompletedOrdinal(
				test.current,
				test.ordinal,
				now,
			)
			assertOrdinalAction(t, decision, SHCImageUpgradeOrdinalBlock)
			if decision.Operation != nil &&
				len(decision.Operation.CompletedOrdinals) != 0 {
				t.Fatalf("invalid observation changed ordinals: %#v", decision)
			}
		})
	}
}

func TestSHCImageUpgradeFinalizationRequiresEveryOrdinalAndPartitionReset(t *testing.T) {
	input := eligibleFinalizationInput()
	input.Current.CompletedOrdinals = []int32{1, 2}

	missing := EvaluateSHCImageUpgradeFinalization(input)
	assertFinalizationAction(t, missing, SHCImageUpgradeFinalizationWait)
	if missing.Operation.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseRollingMembers {
		t.Fatalf("missing ordinal advanced phase: %#v", missing.Operation)
	}

	input.Current.CompletedOrdinals = []int32{0, 1, 2}
	input.StatefulSetPartition = 0
	partition := EvaluateSHCImageUpgradeFinalization(input)
	assertFinalizationAction(t, partition, SHCImageUpgradeFinalizationWait)
	if partition.Operation.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseRollingMembers {
		t.Fatalf("partition reset not observed but phase advanced: %#v", partition)
	}
}

func TestSHCImageUpgradeFinalizationPersistenceBarriersAndRetry(t *testing.T) {
	input := eligibleFinalizationInput()

	eligible := EvaluateSHCImageUpgradeFinalization(input)
	assertFinalizationAction(t, eligible, SHCImageUpgradeFinalizationPersist)
	if eligible.Operation.Phase != enterpriseApi.
		SearchHeadClusterImageUpgradePhasePendingFinalization ||
		eligible.Operation.FinalizationIntentAt != nil ||
		eligible.Operation.FinalizationAttemptCount != 0 {
		t.Fatalf("eligibility barrier = %#v", eligible.Operation)
	}
	if input.Current.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseRollingMembers {
		t.Fatal("eligibility transition mutated persisted input")
	}

	input.Current = eligible.Operation
	input.Now = input.Now.Add(time.Second)
	intent := EvaluateSHCImageUpgradeFinalization(input)
	assertFinalizationAction(t, intent, SHCImageUpgradeFinalizationPersist)
	if intent.Operation.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseFinalizing ||
		intent.Operation.FinalizationIntentAt == nil ||
		intent.Operation.FinalizationAttemptCount != 0 {
		t.Fatalf("intent barrier = %#v", intent.Operation)
	}

	input.Current = intent.Operation
	input.Now = input.Now.Add(time.Second)
	call := EvaluateSHCImageUpgradeFinalization(input)
	assertFinalizationAction(t, call, SHCImageUpgradeFinalizationCall)

	failed := RecordSHCImageUpgradeFinalizationAttempt(
		call.Operation,
		false,
		input.Now.Add(time.Second),
	)
	assertFinalizationAction(t, failed, SHCImageUpgradeFinalizationPersist)
	if failed.Operation.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseFinalizing ||
		failed.Operation.FinalizationAttemptCount != 1 ||
		failed.Operation.FinalizationLastAttemptAt == nil ||
		failed.Operation.FinalizationSucceededAt != nil ||
		failed.Operation.Reason != enterpriseApi.
			SearchHeadClusterImageUpgradeReasonFinalizationRetrying {
		t.Fatalf("failed finalization = %#v", failed.Operation)
	}

	input.Current = failed.Operation
	input.Now = input.Now.Add(2 * time.Second)
	retry := EvaluateSHCImageUpgradeFinalization(input)
	assertFinalizationAction(t, retry, SHCImageUpgradeFinalizationCall)

	succeeded := RecordSHCImageUpgradeFinalizationAttempt(
		retry.Operation,
		true,
		input.Now.Add(time.Second),
	)
	assertFinalizationAction(t, succeeded, SHCImageUpgradeFinalizationPersist)
	if succeeded.Operation.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseFinalizing ||
		succeeded.Operation.FinalizationSucceededAt == nil ||
		succeeded.Operation.FinalizationAttemptCount != 2 ||
		succeeded.Operation.CompletedAt != nil {
		t.Fatalf("successful finalization barrier = %#v", succeeded.Operation)
	}

	input.Current = succeeded.Operation
	input.ManagementTargetEligible = false
	input.CoordinationOwned = false
	input.ConflictingPlannedOperation = true
	input.Pods = nil
	input.Now = input.Now.Add(2 * time.Second)
	completed := EvaluateSHCImageUpgradeFinalization(input)
	assertFinalizationAction(t, completed, SHCImageUpgradeFinalizationPersist)
	if completed.Operation.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseCompleted ||
		completed.Operation.CompletedAt == nil {
		t.Fatalf("completion barrier = %#v", completed.Operation)
	}

	input.Current = completed.Operation
	input.Now = input.Now.Add(time.Second)
	finished := EvaluateSHCImageUpgradeFinalization(input)
	assertFinalizationAction(t, finished, SHCImageUpgradeFinalizationFinished)
}

func TestSHCImageUpgradeFinalizationRevalidatesEveryRecoveryGate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SHCImageUpgradeFinalizationInput)
	}{
		{
			name: "replica count",
			mutate: func(input *SHCImageUpgradeFinalizationInput) {
				input.StatefulSetReplicas = 2
			},
		},
		{
			name: "initialization success",
			mutate: func(input *SHCImageUpgradeFinalizationInput) {
				input.Current.InitializationSucceededAt = nil
			},
		},
		{
			name: "current revision",
			mutate: func(input *SHCImageUpgradeFinalizationInput) {
				input.StatefulSetCurrentRevision = "revision-1"
			},
		},
		{
			name: "update revision",
			mutate: func(input *SHCImageUpgradeFinalizationInput) {
				input.StatefulSetUpdateRevision = "revision-3"
			},
		},
		{
			name: "target image",
			mutate: func(input *SHCImageUpgradeFinalizationInput) {
				input.StatefulSetTargetImage = "splunk/splunk:10.1.0"
			},
		},
		{
			name: "latest lifecycle",
			mutate: func(input *SHCImageUpgradeFinalizationInput) {
				input.LatestMemberLifecycleDone = false
			},
		},
		{
			name: "initialized",
			mutate: func(input *SHCImageUpgradeFinalizationInput) {
				input.Initialized = false
			},
		},
		{
			name: "minimum peers",
			mutate: func(input *SHCImageUpgradeFinalizationInput) {
				input.MinPeersJoined = false
			},
		},
		{
			name: "captain ready",
			mutate: func(input *SHCImageUpgradeFinalizationInput) {
				input.CaptainReady = false
			},
		},
		{
			name: "Pod missing",
			mutate: func(input *SHCImageUpgradeFinalizationInput) {
				input.Pods[2].Exists = false
			},
		},
		{
			name: "Pod terminating",
			mutate: func(input *SHCImageUpgradeFinalizationInput) {
				input.Pods[2].Deleting = true
			},
		},
		{
			name: "Pod not ready",
			mutate: func(input *SHCImageUpgradeFinalizationInput) {
				input.Pods[2].Ready = false
			},
		},
		{
			name: "Pod revision",
			mutate: func(input *SHCImageUpgradeFinalizationInput) {
				input.Pods[2].Revision = "revision-1"
			},
		},
		{
			name: "Pod image",
			mutate: func(input *SHCImageUpgradeFinalizationInput) {
				input.Pods[2].Image = "splunk/splunk:9.4.0"
			},
		},
		{
			name: "member registration",
			mutate: func(input *SHCImageUpgradeFinalizationInput) {
				input.Pods[2].MemberRegistered = false
			},
		},
		{
			name: "member status",
			mutate: func(input *SHCImageUpgradeFinalizationInput) {
				input.Pods[2].MemberStatus = "ManualDetention"
			},
		},
		{
			name: "coordination ownership",
			mutate: func(input *SHCImageUpgradeFinalizationInput) {
				input.CoordinationOwned = false
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := eligibleFinalizationInput()
			test.mutate(&input)

			decision := EvaluateSHCImageUpgradeFinalization(input)

			assertFinalizationAction(t, decision, SHCImageUpgradeFinalizationWait)
			if decision.Operation.Phase != enterpriseApi.
				SearchHeadClusterImageUpgradePhaseRollingMembers {
				t.Fatalf("failed gate advanced phase: %#v", decision)
			}
		})
	}
}

func TestSHCImageUpgradeFinalizationBlocksConflictingOperation(t *testing.T) {
	input := eligibleFinalizationInput()
	input.ConflictingPlannedOperation = true

	decision := EvaluateSHCImageUpgradeFinalization(input)

	assertFinalizationAction(t, decision, SHCImageUpgradeFinalizationBlock)
	if decision.Operation.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseBlocked ||
		decision.Operation.Reason != enterpriseApi.
			SearchHeadClusterImageUpgradeReasonConflictingPlannedOperation {
		t.Fatalf("conflicting finalization = %#v", decision)
	}
	if input.Current.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseRollingMembers {
		t.Fatal("conflict transition mutated persisted input")
	}
}

func TestPersistedSHCImageUpgradeFinalizationSuccessIsNotAttemptedAgain(t *testing.T) {
	input := eligibleFinalizationInput()
	input.Current.Phase =
		enterpriseApi.SearchHeadClusterImageUpgradePhaseFinalizing
	intentAt := input.Now.Add(-time.Minute)
	input.Current.FinalizationIntentAt = timePointer(intentAt)
	succeeded := RecordSHCImageUpgradeFinalizationAttempt(
		input.Current,
		true,
		input.Now,
	)

	replay := RecordSHCImageUpgradeFinalizationAttempt(
		succeeded.Operation,
		true,
		input.Now.Add(time.Minute),
	)
	assertFinalizationAction(t, replay, SHCImageUpgradeFinalizationWait)
	if replay.Operation.FinalizationAttemptCount != 1 ||
		!replay.Operation.FinalizationSucceededAt.Equal(
			succeeded.Operation.FinalizationSucceededAt,
		) {
		t.Fatalf("replayed finalization changed evidence: %#v", replay)
	}
}

func TestSHCImageUpgradeFinalizationRequiresEligibleTargetForCall(t *testing.T) {
	input := eligibleFinalizationInput()
	input.Current.Phase =
		enterpriseApi.SearchHeadClusterImageUpgradePhaseFinalizing
	input.Current.FinalizationIntentAt = timePointer(input.Now.Add(-time.Minute))
	input.ManagementTargetEligible = false

	decision := EvaluateSHCImageUpgradeFinalization(input)

	assertFinalizationAction(t, decision, SHCImageUpgradeFinalizationWait)
	if decision.Operation.FinalizationAttemptCount != 0 ||
		decision.Operation.FinalizationLastAttemptAt != nil {
		t.Fatalf("target wait recorded finalization attempt: %#v", decision)
	}
}

func TestSHCImageUpgradeFinalizationRejectsAttemptBeforePersistedIntent(t *testing.T) {
	input := eligibleFinalizationInput()
	input.Current.Phase =
		enterpriseApi.SearchHeadClusterImageUpgradePhasePendingFinalization

	decision := RecordSHCImageUpgradeFinalizationAttempt(
		input.Current,
		true,
		input.Now,
	)

	assertFinalizationAction(t, decision, SHCImageUpgradeFinalizationBlock)
	if decision.Operation.FinalizationAttemptCount != 0 ||
		decision.Operation.FinalizationSucceededAt != nil ||
		decision.Operation.Phase != enterpriseApi.
			SearchHeadClusterImageUpgradePhasePendingFinalization {
		t.Fatalf("pre-intent attempt changed operation: %#v", decision)
	}
}

func TestSHCImageUpgradeFinalizationAttemptCountIsBounded(t *testing.T) {
	input := eligibleFinalizationInput()
	input.Current.Phase =
		enterpriseApi.SearchHeadClusterImageUpgradePhaseFinalizing
	input.Current.FinalizationIntentAt = timePointer(input.Now.Add(-time.Minute))
	input.Current.FinalizationAttemptCount = math.MaxInt32

	decision := RecordSHCImageUpgradeFinalizationAttempt(
		input.Current,
		false,
		input.Now,
	)

	if decision.Operation.FinalizationAttemptCount != math.MaxInt32 {
		t.Fatalf(
			"attempt count = %d, want %d",
			decision.Operation.FinalizationAttemptCount,
			math.MaxInt32,
		)
	}
}

func rollingImageUpgrade(
	now time.Time,
) *enterpriseApi.SearchHeadClusterImageUpgradeStatus {
	succeededAt := timePointer(now.Add(-time.Minute))
	return &enterpriseApi.SearchHeadClusterImageUpgradeStatus{
		OperationID:     "image-upgrade:splunk-example-search-head:revision-2",
		StatefulSetName: "splunk-example-search-head",
		DesiredRevision: "revision-2",
		SourceImage:     "splunk/splunk:9.4.0",
		TargetImage:     "splunk/splunk:10.0.0",
		TargetReplicas:  3,
		Phase: enterpriseApi.
			SearchHeadClusterImageUpgradePhaseRollingMembers,
		Reason: enterpriseApi.
			SearchHeadClusterImageUpgradeReasonInitializationSucceeded,
		InitializationSucceededAt: succeededAt,
		StartedAt:                 timePointer(now.Add(-time.Hour)),
		PhaseStartedAt:            timePointer(now.Add(-time.Minute)),
		LastTransitionTime:        timePointer(now.Add(-time.Minute)),
	}
}

func eligibleFinalizationInput() SHCImageUpgradeFinalizationInput {
	now := time.Date(2026, 7, 25, 17, 0, 0, 0, time.UTC)
	operation := rollingImageUpgrade(now.Add(-time.Minute))
	operation.CompletedOrdinals = []int32{0, 1, 2}
	return SHCImageUpgradeFinalizationInput{
		Current:                    operation,
		StatefulSetReplicas:        3,
		StatefulSetPartition:       3,
		StatefulSetCurrentRevision: "revision-2",
		StatefulSetUpdateRevision:  "revision-2",
		StatefulSetTargetImage:     "splunk/splunk:10.0.0",
		LatestMemberLifecycleDone:  true,
		Initialized:                true,
		MinPeersJoined:             true,
		CaptainReady:               true,
		CoordinationOwned:          true,
		ManagementTargetEligible:   true,
		Now:                        now,
		Pods: []SHCImageUpgradeFinalizationPod{
			finalizationPod(0),
			finalizationPod(1),
			finalizationPod(2),
		},
	}
}

func finalizationPod(ordinal int32) SHCImageUpgradeFinalizationPod {
	return SHCImageUpgradeFinalizationPod{
		Ordinal:          ordinal,
		Exists:           true,
		Ready:            true,
		Revision:         "revision-2",
		Image:            "splunk/splunk:10.0.0",
		MemberRegistered: true,
		MemberStatus:     "Up",
	}
}

func timePointer(value time.Time) *metav1.Time {
	timestamp := metav1.NewTime(value)
	return &timestamp
}

func assertOrdinalAction(
	t *testing.T,
	decision SHCImageUpgradeOrdinalDecision,
	action SHCImageUpgradeOrdinalAction,
) {
	t.Helper()
	if decision.Action != action {
		t.Fatalf("action = %q, want %q: %#v", decision.Action, action, decision)
	}
}

func assertFinalizationAction(
	t *testing.T,
	decision SHCImageUpgradeFinalizationDecision,
	action SHCImageUpgradeFinalizationAction,
) {
	t.Helper()
	if decision.Action != action {
		t.Fatalf("action = %q, want %q: %#v", decision.Action, action, decision)
	}
	if decision.Operation != nil && decision.Operation.OperationID == "" {
		t.Fatalf("decision lost operation identity: %#v", decision)
	}
}
