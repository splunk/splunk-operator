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
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

// SHCRolloutAction identifies the only mutations or external work the rollout
// adapter may perform.
type SHCRolloutAction string

const (
	SHCRolloutActionNone          SHCRolloutAction = ""
	SHCRolloutActionPrepareTarget SHCRolloutAction = "PrepareTarget"
	SHCRolloutActionSetPartition  SHCRolloutAction = "SetPartition"
	SHCRolloutActionWait          SHCRolloutAction = "Wait"
	SHCRolloutActionComplete      SHCRolloutAction = "Complete"
	SHCRolloutActionBlock         SHCRolloutAction = "Block"
)

// SHCRolloutReason is a bounded explanation of a partition decision.
type SHCRolloutReason string

const (
	SHCRolloutReasonStable                        SHCRolloutReason = "Stable"
	SHCRolloutReasonPaused                        SHCRolloutReason = "Paused"
	SHCRolloutReasonInitialFormationPending       SHCRolloutReason = "InitialFormationPending"
	SHCRolloutReasonCaptainUnavailable            SHCRolloutReason = "CaptainUnavailable"
	SHCRolloutReasonRollbackPending               SHCRolloutReason = "RollbackPending"
	SHCRolloutReasonScaleUpMemberPending          SHCRolloutReason = "ScaleUpMemberPending"
	SHCRolloutReasonWaitingForRevision            SHCRolloutReason = "WaitingForRevision"
	SHCRolloutReasonPrepareTarget                 SHCRolloutReason = "PrepareTarget"
	SHCRolloutReasonPartitionAdvanceAuthorized    SHCRolloutReason = "PartitionAdvanceAuthorized"
	SHCRolloutReasonWaitingForKubernetes          SHCRolloutReason = "WaitingForKubernetes"
	SHCRolloutReasonWaitingForRecovery            SHCRolloutReason = "WaitingForRecovery"
	SHCRolloutReasonTooManyUnavailable            SHCRolloutReason = "TooManyUnavailable"
	SHCRolloutReasonExistingUnavailablePod        SHCRolloutReason = "ExistingUnavailablePod"
	SHCRolloutReasonMemberRecoveryPending         SHCRolloutReason = "MemberRecoveryPending"
	SHCRolloutReasonOutOfOrderRevision            SHCRolloutReason = "OutOfOrderRevision"
	SHCRolloutReasonConflictingLifecycleOperation SHCRolloutReason = "ConflictingLifecycleOperation"
	SHCRolloutReasonLifecycleBlocked              SHCRolloutReason = "LifecycleBlocked"
	SHCRolloutReasonInvalidState                  SHCRolloutReason = "InvalidState"
)

// SHCRolloutPod is the coordinator's bounded Kubernetes observation for one
// StatefulSet ordinal.
type SHCRolloutPod struct {
	Ordinal  int32
	Exists   bool
	Ready    bool
	Deleting bool
	Revision string
	Image    string
	// MemberRegistered and MemberStatus are the current Splunk observation
	// for this ordinal. Kubernetes readiness alone cannot prove that an
	// unplanned replacement has recovered into the SHC.
	MemberRegistered bool
	MemberStatus     string
}

// SHCRolloutLifecycle is the durable lifecycle observation consumed by the
// partition coordinator. ReplacementAuthorized must represent an
// authorization already persisted by an earlier reconciliation.
type SHCRolloutLifecycle struct {
	TargetOrdinal         *int32
	Stage                 enterpriseApi.SearchHeadClusterLifecycleStage
	ReplacementAuthorized bool
}

// SHCRolloutState is a point-in-time view of StatefulSet and lifecycle state.
type SHCRolloutState struct {
	Replicas        int32
	Partition       int32
	CurrentRevision string
	UpdateRevision  string
	Paused          bool
	// ScaleUpFromReplicas identifies the last stable replica boundary while
	// Kubernetes creates additive ordinals from a scale-up-induced revision.
	// Those ordinals were not replacements and therefore have no destructive
	// lifecycle authorization.
	ScaleUpFromReplicas *int32
	Pods                []SHCRolloutPod
	Lifecycle           SHCRolloutLifecycle
}

// SHCRolloutDecision is a pure coordinator result. DesiredPartition is set
// only for SHCRolloutActionSetPartition.
type SHCRolloutDecision struct {
	Action           SHCRolloutAction
	Reason           SHCRolloutReason
	Message          string
	TargetOrdinal    *int32
	DesiredPartition *int32
}

// EvaluateSHCRollout coordinates a reverse-ordinal, partition-gated rollout.
// It never mutates a StatefulSet and never deletes a Pod.
func EvaluateSHCRollout(state SHCRolloutState) SHCRolloutDecision {
	pods, decision := validateSHCRolloutState(state)
	if decision != nil {
		return *decision
	}

	if state.Paused {
		return waitSHCRollout(
			SHCRolloutReasonPaused,
			"rollout is paused; partition remains unchanged",
			nil,
		)
	}
	if state.UpdateRevision == "" {
		return waitSHCRollout(
			SHCRolloutReasonWaitingForRevision,
			"StatefulSet update revision has not been observed",
			nil,
		)
	}

	if allSHCPodsAtRevision(pods, state.UpdateRevision) {
		for ordinal := int32(0); ordinal < state.Replicas; ordinal++ {
			pod := pods[ordinal]
			if !shcRolloutPodAvailable(pod) {
				reason := SHCRolloutReasonWaitingForKubernetes
				message := fmt.Sprintf(
					"Pod ordinal %d is not stably ready in Kubernetes",
					ordinal,
				)
				if shcRolloutPodKubernetesReady(pod) {
					reason = SHCRolloutReasonWaitingForRecovery
					message = fmt.Sprintf(
						"Pod ordinal %d is Kubernetes-ready but has not recovered as a registered Up SHC member",
						ordinal,
					)
				}
				return waitSHCRollout(
					reason,
					message,
					ordinalPointer(ordinal),
				)
			}
		}
		noRolloutInProgress := state.Partition == state.Replicas &&
			state.CurrentRevision == state.UpdateRevision
		rolloutRecovered := state.Lifecycle.TargetOrdinal != nil &&
			*state.Lifecycle.TargetOrdinal == state.Partition &&
			state.Lifecycle.ReplacementAuthorized &&
			lifecycleCompletedForOrdinal(
				state.Lifecycle,
				*state.Lifecycle.TargetOrdinal,
			)
		if noRolloutInProgress || rolloutRecovered || state.Replicas == 0 {
			return SHCRolloutDecision{
				Action:  SHCRolloutActionComplete,
				Reason:  SHCRolloutReasonStable,
				Message: "all StatefulSet Pods are ready at the update revision",
			}
		}
	}

	// A lifecycle operation deliberately withdraws its target from service
	// before the StatefulSet partition is lowered. Do not mistake that owned
	// unavailability for an unrelated disruption, or the durable operation
	// cannot advance beyond detention. Every other unavailable Pod remains a
	// safety block.
	unavailable := int32(0)
	unavailableOrdinal := int32(-1)
	for ordinal := int32(0); ordinal < state.Replicas; ordinal++ {
		pod := pods[ordinal]
		if !shcRolloutPodAvailable(pod) &&
			!lifecycleOwnsUnavailableOrdinal(state.Lifecycle, ordinal) {
			unavailable++
			unavailableOrdinal = ordinal
		}
	}
	if unavailable > 1 {
		return blockSHCRollout(
			SHCRolloutReasonTooManyUnavailable,
			fmt.Sprintf("%d Pods are unavailable; refusing partition advancement", unavailable),
			nil,
		)
	}
	if unavailable > 0 && state.Partition == state.Replicas {
		reason := SHCRolloutReasonExistingUnavailablePod
		message := fmt.Sprintf(
			"Pod ordinal %d is already unavailable; refusing a new planned disruption",
			unavailableOrdinal,
		)
		if shcRolloutPodKubernetesReady(pods[unavailableOrdinal]) {
			reason = SHCRolloutReasonMemberRecoveryPending
			message = fmt.Sprintf(
				"Pod ordinal %d is Kubernetes-ready but has not recovered as a registered Up SHC member",
				unavailableOrdinal,
			)
		}
		return blockSHCRollout(
			reason,
			message,
			ordinalPointer(unavailableOrdinal),
		)
	}

	if state.Partition < state.Replicas {
		activeOrdinal := state.Partition
		activeOrdinalIsScaleUpAddition :=
			state.ScaleUpFromReplicas != nil &&
				activeOrdinal >= *state.ScaleUpFromReplicas
		for ordinal := activeOrdinal + 1; ordinal < state.Replicas; ordinal++ {
			pod := pods[ordinal]
			if !shcRolloutPodAvailable(pod) ||
				pod.Revision != state.UpdateRevision {
				return blockSHCRollout(
					SHCRolloutReasonOutOfOrderRevision,
					fmt.Sprintf("higher ordinal %d is not recovered at update revision", ordinal),
					ordinalPointer(ordinal),
				)
			}
		}
		// When a Pod-template change is withdrawn, Kubernetes can reuse the
		// original ControllerRevision. CurrentRevision and UpdateRevision then
		// become equal before every superseded Pod has rolled back. Untouched
		// lower ordinals already matching that revision are correct, not
		// out-of-order; the durable partition/lifecycle record still controls
		// which mismatching ordinal may be prepared next.
		restoringExistingRevision :=
			state.CurrentRevision == state.UpdateRevision
		for ordinal := int32(0); ordinal < activeOrdinal; ordinal++ {
			if !restoringExistingRevision &&
				pods[ordinal].Revision == state.UpdateRevision {
				return blockSHCRollout(
					SHCRolloutReasonOutOfOrderRevision,
					fmt.Sprintf("lower ordinal %d changed before partition authorization", ordinal),
					ordinalPointer(ordinal),
				)
			}
		}

		activePod := pods[activeOrdinal]
		activePodRecoveredByKubernetes := activePod.Exists &&
			activePod.Revision == state.UpdateRevision &&
			activePod.Ready &&
			!activePod.Deleting
		if !activePodRecoveredByKubernetes {
			if activeOrdinalIsScaleUpAddition {
				return waitSHCRollout(
					SHCRolloutReasonScaleUpMemberPending,
					fmt.Sprintf(
						"waiting for additive scale-up ordinal %d to become ready",
						activeOrdinal,
					),
					ordinalPointer(activeOrdinal),
				)
			}
			if !lifecycleTargetsOrdinal(state.Lifecycle, activeOrdinal) {
				return blockSHCRollout(
					SHCRolloutReasonConflictingLifecycleOperation,
					fmt.Sprintf("partition %d has no matching durable lifecycle target", state.Partition),
					ordinalPointer(activeOrdinal),
				)
			}
			if lifecycleBlocked(state.Lifecycle) {
				return blockSHCRollout(
					SHCRolloutReasonLifecycleBlocked,
					fmt.Sprintf("lifecycle operation for ordinal %d is %s", activeOrdinal, state.Lifecycle.Stage),
					ordinalPointer(activeOrdinal),
				)
			}
			if !state.Lifecycle.ReplacementAuthorized {
				return blockSHCRollout(
					SHCRolloutReasonConflictingLifecycleOperation,
					fmt.Sprintf("partition %d was lowered before durable replacement authorization", state.Partition),
					ordinalPointer(activeOrdinal),
				)
			}
			return waitSHCRollout(
				SHCRolloutReasonWaitingForKubernetes,
				fmt.Sprintf("waiting for Kubernetes to replace ordinal %d", activeOrdinal),
				ordinalPointer(activeOrdinal),
			)
		}

		if lifecycleTargetsOrdinal(state.Lifecycle, activeOrdinal) {
			if lifecycleBlocked(state.Lifecycle) {
				return blockSHCRollout(
					SHCRolloutReasonLifecycleBlocked,
					fmt.Sprintf("lifecycle operation for ordinal %d is %s", activeOrdinal, state.Lifecycle.Stage),
					ordinalPointer(activeOrdinal),
				)
			}
			if !state.Lifecycle.ReplacementAuthorized {
				return blockSHCRollout(
					SHCRolloutReasonConflictingLifecycleOperation,
					fmt.Sprintf("ordinal %d reached the update revision without durable replacement authorization", activeOrdinal),
					ordinalPointer(activeOrdinal),
				)
			}
			if !lifecycleCompletedForOrdinal(state.Lifecycle, activeOrdinal) {
				return waitSHCRollout(
					SHCRolloutReasonWaitingForRecovery,
					fmt.Sprintf("ordinal %d is locally ready but SHC recovery is incomplete", activeOrdinal),
					ordinalPointer(activeOrdinal),
				)
			}
		} else {
			nextOrdinal := activeOrdinal - 1
			preparingNextOrdinal := activeOrdinal > 0 &&
				lifecycleTargetsOrdinal(state.Lifecycle, nextOrdinal)
			if !preparingNextOrdinal {
				if activeOrdinalIsScaleUpAddition {
					if activeOrdinal == 0 {
						return SHCRolloutDecision{
							Action:  SHCRolloutActionComplete,
							Reason:  SHCRolloutReasonStable,
							Message: "additive scale-up ordinal recovered",
						}
					}
					return SHCRolloutDecision{
						Action:        SHCRolloutActionPrepareTarget,
						Reason:        SHCRolloutReasonPrepareTarget,
						Message:       fmt.Sprintf("prepare ordinal %d after additive scale-up ordinal %d recovered", nextOrdinal, activeOrdinal),
						TargetOrdinal: ordinalPointer(nextOrdinal),
					}
				}
				return blockSHCRollout(
					SHCRolloutReasonConflictingLifecycleOperation,
					fmt.Sprintf("recovered ordinal %d has no completed lifecycle record or next-ordinal preparation", activeOrdinal),
					ordinalPointer(activeOrdinal),
				)
			}
		}
		if unavailable > 0 {
			reason := SHCRolloutReasonExistingUnavailablePod
			message := fmt.Sprintf(
				"unrelated Pod ordinal %d is unavailable; refusing the next planned disruption",
				unavailableOrdinal,
			)
			if shcRolloutPodKubernetesReady(pods[unavailableOrdinal]) {
				reason = SHCRolloutReasonMemberRecoveryPending
				message = fmt.Sprintf(
					"unrelated Pod ordinal %d is Kubernetes-ready but has not recovered as a registered Up SHC member",
					unavailableOrdinal,
				)
			}
			return blockSHCRollout(
				reason,
				message,
				ordinalPointer(unavailableOrdinal),
			)
		}
	}

	if state.Partition == 0 {
		return waitSHCRollout(
			SHCRolloutReasonWaitingForRevision,
			"partition is zero but StatefulSet revisions have not converged",
			ordinalPointer(0),
		)
	}

	target := state.Partition - 1
	targetPod := pods[target]
	if !shcRolloutPodAvailable(targetPod) &&
		!lifecycleOwnsUnavailableOrdinal(state.Lifecycle, target) {
		return blockSHCRollout(
			SHCRolloutReasonOutOfOrderRevision,
			fmt.Sprintf("next target ordinal %d is not stably ready in Kubernetes and the SHC before preparation", target),
			ordinalPointer(target),
		)
	}
	if targetPod.Revision == state.UpdateRevision {
		return blockSHCRollout(
			SHCRolloutReasonOutOfOrderRevision,
			fmt.Sprintf("next target ordinal %d already has the update revision", target),
			ordinalPointer(target),
		)
	}

	if lifecycleTargetsOrdinal(state.Lifecycle, target) {
		if lifecycleBlocked(state.Lifecycle) {
			return blockSHCRollout(
				SHCRolloutReasonLifecycleBlocked,
				fmt.Sprintf("lifecycle preparation for ordinal %d is %s", target, state.Lifecycle.Stage),
				ordinalPointer(target),
			)
		}
		if state.Lifecycle.ReplacementAuthorized {
			return SHCRolloutDecision{
				Action:           SHCRolloutActionSetPartition,
				Reason:           SHCRolloutReasonPartitionAdvanceAuthorized,
				Message:          fmt.Sprintf("lower partition from %d to %d", state.Partition, target),
				TargetOrdinal:    ordinalPointer(target),
				DesiredPartition: ordinalPointer(target),
			}
		}
	} else if lifecycleInProgress(state.Lifecycle) {
		return blockSHCRollout(
			SHCRolloutReasonConflictingLifecycleOperation,
			fmt.Sprintf("lifecycle target does not match next rollout ordinal %d", target),
			ordinalPointer(target),
		)
	}

	return SHCRolloutDecision{
		Action:        SHCRolloutActionPrepareTarget,
		Reason:        SHCRolloutReasonPrepareTarget,
		Message:       fmt.Sprintf("prepare ordinal %d before lowering partition", target),
		TargetOrdinal: ordinalPointer(target),
	}
}

func shcRolloutPodKubernetesReady(pod SHCRolloutPod) bool {
	return pod.Exists && pod.Ready && !pod.Deleting
}

func shcRolloutPodAvailable(pod SHCRolloutPod) bool {
	return shcRolloutPodKubernetesReady(pod) &&
		pod.MemberRegistered &&
		pod.MemberStatus == "Up"
}

func validateSHCRolloutState(
	state SHCRolloutState,
) (map[int32]SHCRolloutPod, *SHCRolloutDecision) {
	if state.Replicas < 0 ||
		state.Partition < 0 ||
		state.Partition > state.Replicas {
		decision := blockSHCRollout(
			SHCRolloutReasonInvalidState,
			fmt.Sprintf("invalid replicas=%d partition=%d", state.Replicas, state.Partition),
			nil,
		)
		return nil, &decision
	}
	pods := make(map[int32]SHCRolloutPod, state.Replicas)
	for _, pod := range state.Pods {
		if pod.Ordinal < 0 || pod.Ordinal >= state.Replicas {
			decision := blockSHCRollout(
				SHCRolloutReasonInvalidState,
				fmt.Sprintf("Pod ordinal %d is outside replica range", pod.Ordinal),
				ordinalPointer(pod.Ordinal),
			)
			return nil, &decision
		}
		if _, exists := pods[pod.Ordinal]; exists {
			decision := blockSHCRollout(
				SHCRolloutReasonInvalidState,
				fmt.Sprintf("duplicate observation for Pod ordinal %d", pod.Ordinal),
				ordinalPointer(pod.Ordinal),
			)
			return nil, &decision
		}
		pods[pod.Ordinal] = pod
	}
	for ordinal := int32(0); ordinal < state.Replicas; ordinal++ {
		if _, exists := pods[ordinal]; !exists {
			pods[ordinal] = SHCRolloutPod{Ordinal: ordinal}
		}
	}
	return pods, nil
}

func allSHCPodsAtRevision(pods map[int32]SHCRolloutPod, revision string) bool {
	for _, pod := range pods {
		if !pod.Exists || pod.Revision != revision {
			return false
		}
	}
	return true
}

func lifecycleTargetsOrdinal(lifecycle SHCRolloutLifecycle, ordinal int32) bool {
	return lifecycle.TargetOrdinal != nil && *lifecycle.TargetOrdinal == ordinal
}

func lifecycleOwnsUnavailableOrdinal(
	lifecycle SHCRolloutLifecycle,
	ordinal int32,
) bool {
	return lifecycleTargetsOrdinal(lifecycle, ordinal) &&
		lifecycle.Stage != "" &&
		lifecycle.Stage != enterpriseApi.SearchHeadClusterLifecycleStageCompleted
}

func lifecycleCompletedForOrdinal(lifecycle SHCRolloutLifecycle, ordinal int32) bool {
	return lifecycleTargetsOrdinal(lifecycle, ordinal) &&
		lifecycle.Stage == enterpriseApi.SearchHeadClusterLifecycleStageCompleted
}

func lifecycleBlocked(lifecycle SHCRolloutLifecycle) bool {
	return lifecycle.Stage == enterpriseApi.SearchHeadClusterLifecycleStageBlocked ||
		lifecycle.Stage == enterpriseApi.SearchHeadClusterLifecycleStageFailed
}

func lifecycleInProgress(lifecycle SHCRolloutLifecycle) bool {
	return lifecycle.TargetOrdinal != nil &&
		lifecycle.Stage != enterpriseApi.SearchHeadClusterLifecycleStageCompleted
}

func waitSHCRollout(
	reason SHCRolloutReason,
	message string,
	target *int32,
) SHCRolloutDecision {
	return SHCRolloutDecision{
		Action:        SHCRolloutActionWait,
		Reason:        reason,
		Message:       message,
		TargetOrdinal: target,
	}
}

func blockSHCRollout(
	reason SHCRolloutReason,
	message string,
	target *int32,
) SHCRolloutDecision {
	return SHCRolloutDecision{
		Action:        SHCRolloutActionBlock,
		Reason:        reason,
		Message:       message,
		TargetOrdinal: target,
	}
}

func ordinalPointer(ordinal int32) *int32 {
	return &ordinal
}
