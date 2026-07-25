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
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

func TestSHCRolloutRequiresPreparationBeforePartitionAdvance(t *testing.T) {
	state := pendingSHCRolloutState()

	decision := EvaluateSHCRollout(state)
	assertSHCRolloutDecision(t, decision, SHCRolloutActionPrepareTarget, SHCRolloutReasonPrepareTarget, 2)

	state.Lifecycle = lifecycleForOrdinal(
		2,
		enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement,
		false,
	)
	decision = EvaluateSHCRollout(state)
	assertSHCRolloutDecision(t, decision, SHCRolloutActionPrepareTarget, SHCRolloutReasonPrepareTarget, 2)

	state.Lifecycle.ReplacementAuthorized = true
	decision = EvaluateSHCRollout(state)
	assertSHCRolloutDecision(
		t,
		decision,
		SHCRolloutActionSetPartition,
		SHCRolloutReasonPartitionAdvanceAuthorized,
		2,
	)
	if decision.DesiredPartition == nil || *decision.DesiredPartition != 2 {
		t.Fatalf("desired partition = %v, want 2", decision.DesiredPartition)
	}
}

func TestSHCRolloutWaitsForKubernetesThenSHCRecovery(t *testing.T) {
	state := pendingSHCRolloutState()
	state.Partition = 2
	state.Lifecycle = lifecycleForOrdinal(
		2,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForTermination,
		true,
	)

	decision := EvaluateSHCRollout(state)
	assertSHCRolloutDecision(t, decision, SHCRolloutActionWait, SHCRolloutReasonWaitingForKubernetes, 2)

	state.Pods[2].Revision = state.UpdateRevision
	state.Pods[2].Ready = true
	state.Lifecycle.Stage = enterpriseApi.SearchHeadClusterLifecycleStageWaitingForMemberRejoin
	decision = EvaluateSHCRollout(state)
	assertSHCRolloutDecision(t, decision, SHCRolloutActionWait, SHCRolloutReasonWaitingForRecovery, 2)

	state.Lifecycle.Stage = enterpriseApi.SearchHeadClusterLifecycleStageCompleted
	decision = EvaluateSHCRollout(state)
	assertSHCRolloutDecision(t, decision, SHCRolloutActionPrepareTarget, SHCRolloutReasonPrepareTarget, 1)
}

func TestSHCRolloutCompletesInReverseOrdinalOrder(t *testing.T) {
	state := pendingSHCRolloutState()
	for target := int32(2); target >= 0; target-- {
		state.Partition = target + 1
		state.Lifecycle = lifecycleForOrdinal(
			target,
			enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement,
			true,
		)
		decision := EvaluateSHCRollout(state)
		assertSHCRolloutDecision(
			t,
			decision,
			SHCRolloutActionSetPartition,
			SHCRolloutReasonPartitionAdvanceAuthorized,
			target,
		)

		state.Partition = target
		state.Pods[target].Revision = state.UpdateRevision
		state.Pods[target].Ready = true
		state.Lifecycle.Stage = enterpriseApi.SearchHeadClusterLifecycleStageCompleted
	}
	state.CurrentRevision = state.UpdateRevision

	decision := EvaluateSHCRollout(state)
	if decision.Action != SHCRolloutActionComplete {
		t.Fatalf("action = %q reason = %q, want Complete", decision.Action, decision.Reason)
	}
}

func TestSHCRolloutBlocksOutOfOrderLowerOrdinalRevision(t *testing.T) {
	state := pendingSHCRolloutState()
	state.Partition = 2
	state.Pods[2].Revision = state.UpdateRevision
	state.Pods[0].Revision = state.UpdateRevision
	state.Lifecycle = lifecycleForOrdinal(
		2,
		enterpriseApi.SearchHeadClusterLifecycleStageCompleted,
		true,
	)

	decision := EvaluateSHCRollout(state)

	assertSHCRolloutDecision(t, decision, SHCRolloutActionBlock, SHCRolloutReasonOutOfOrderRevision, 0)
}

func TestSHCRolloutBlocksMoreThanOneUnavailablePod(t *testing.T) {
	state := pendingSHCRolloutState()
	state.Pods[2].Ready = false
	state.Pods[1].Exists = false

	decision := EvaluateSHCRollout(state)

	if decision.Action != SHCRolloutActionBlock ||
		decision.Reason != SHCRolloutReasonTooManyUnavailable {
		t.Fatalf("decision = %#v, want TooManyUnavailable block", decision)
	}
}

func TestSHCRolloutPauseNeverChangesPartition(t *testing.T) {
	state := pendingSHCRolloutState()
	state.Paused = true

	decision := EvaluateSHCRollout(state)

	if decision.Action != SHCRolloutActionWait ||
		decision.Reason != SHCRolloutReasonPaused ||
		decision.DesiredPartition != nil {
		t.Fatalf("decision = %#v, want paused wait without partition", decision)
	}
}

func TestSHCRolloutBlocksPartitionWithoutMatchingAuthorization(t *testing.T) {
	state := pendingSHCRolloutState()
	state.Partition = 2
	state.Lifecycle = lifecycleForOrdinal(
		1,
		enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement,
		true,
	)

	decision := EvaluateSHCRollout(state)

	assertSHCRolloutDecision(
		t,
		decision,
		SHCRolloutActionBlock,
		SHCRolloutReasonConflictingLifecycleOperation,
		2,
	)
}

func TestSHCRolloutBlocksLifecycleFailure(t *testing.T) {
	state := pendingSHCRolloutState()
	state.Lifecycle = lifecycleForOrdinal(
		2,
		enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
		false,
	)

	decision := EvaluateSHCRollout(state)

	assertSHCRolloutDecision(t, decision, SHCRolloutActionBlock, SHCRolloutReasonLifecycleBlocked, 2)
}

func TestSHCRolloutStableWithoutTemplateChange(t *testing.T) {
	state := pendingSHCRolloutState()
	state.CurrentRevision = state.UpdateRevision
	for i := range state.Pods {
		state.Pods[i].Revision = state.UpdateRevision
	}

	decision := EvaluateSHCRollout(state)

	if decision.Action != SHCRolloutActionComplete ||
		decision.Reason != SHCRolloutReasonStable {
		t.Fatalf("decision = %#v, want stable completion", decision)
	}
}

func TestSHCRolloutDoesNotCompleteBeforeFinalSHCRecovery(t *testing.T) {
	state := pendingSHCRolloutState()
	state.Partition = 0
	state.CurrentRevision = state.UpdateRevision
	for i := range state.Pods {
		state.Pods[i].Revision = state.UpdateRevision
	}
	state.Lifecycle = lifecycleForOrdinal(
		0,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForMemberRejoin,
		true,
	)

	decision := EvaluateSHCRollout(state)

	assertSHCRolloutDecision(
		t,
		decision,
		SHCRolloutActionWait,
		SHCRolloutReasonWaitingForRecovery,
		0,
	)
}

func pendingSHCRolloutState() SHCRolloutState {
	return SHCRolloutState{
		Replicas:        3,
		Partition:       3,
		CurrentRevision: "revision-1",
		UpdateRevision:  "revision-2",
		Pods: []SHCRolloutPod{
			{Ordinal: 0, Exists: true, Ready: true, Revision: "revision-1"},
			{Ordinal: 1, Exists: true, Ready: true, Revision: "revision-1"},
			{Ordinal: 2, Exists: true, Ready: true, Revision: "revision-1"},
		},
	}
}

func lifecycleForOrdinal(
	ordinal int32,
	stage enterpriseApi.SearchHeadClusterLifecycleStage,
	authorized bool,
) SHCRolloutLifecycle {
	return SHCRolloutLifecycle{
		TargetOrdinal:         ordinalPointer(ordinal),
		Stage:                 stage,
		ReplacementAuthorized: authorized,
	}
}

func assertSHCRolloutDecision(
	t *testing.T,
	decision SHCRolloutDecision,
	action SHCRolloutAction,
	reason SHCRolloutReason,
	target int32,
) {
	t.Helper()
	if decision.Action != action || decision.Reason != reason {
		t.Fatalf("decision = %#v, want action=%q reason=%q", decision, action, reason)
	}
	if decision.TargetOrdinal == nil || *decision.TargetOrdinal != target {
		t.Fatalf("target = %v, want %d", decision.TargetOrdinal, target)
	}
}
