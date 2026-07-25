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
	"math"
	"slices"
	"sort"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SHCImageUpgradeOrdinalAction describes whether observing a recovered member
// requires a durable status write.
type SHCImageUpgradeOrdinalAction string

const (
	SHCImageUpgradeOrdinalWait    SHCImageUpgradeOrdinalAction = "Wait"
	SHCImageUpgradeOrdinalPersist SHCImageUpgradeOrdinalAction = "Persist"
	SHCImageUpgradeOrdinalBlock   SHCImageUpgradeOrdinalAction = "Block"
)

// SHCImageUpgradeOrdinalDecision never aliases the input operation.
type SHCImageUpgradeOrdinalDecision struct {
	Action    SHCImageUpgradeOrdinalAction
	Operation *enterpriseApi.SearchHeadClusterImageUpgradeStatus
	Reason    enterpriseApi.SearchHeadClusterImageUpgradeReason
	Message   string
}

// RecordSHCImageUpgradeCompletedOrdinal adds one fully recovered member to the
// bounded, unique completed set. Re-observing an ordinal is timestamp-stable.
func RecordSHCImageUpgradeCompletedOrdinal(
	current *enterpriseApi.SearchHeadClusterImageUpgradeStatus,
	ordinal int32,
	now time.Time,
) SHCImageUpgradeOrdinalDecision {
	if current == nil {
		return SHCImageUpgradeOrdinalDecision{
			Action:  SHCImageUpgradeOrdinalBlock,
			Reason:  enterpriseApi.SearchHeadClusterImageUpgradeReasonClusterNotReady,
			Message: "cannot record a member without a durable image-upgrade workflow",
		}
	}
	operation := current.DeepCopy()
	if slices.Contains(operation.CompletedOrdinals, ordinal) {
		return ordinalDecision(
			SHCImageUpgradeOrdinalWait,
			operation,
		)
	}
	if operation.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseRollingMembers ||
		operation.InitializationSucceededAt == nil ||
		ordinal < 0 ||
		ordinal >= operation.TargetReplicas {
		return SHCImageUpgradeOrdinalDecision{
			Action:    SHCImageUpgradeOrdinalBlock,
			Operation: operation,
			Reason: enterpriseApi.
				SearchHeadClusterImageUpgradeReasonMemberLifecycleBlocked,
			Message: "refusing to record an invalid or premature member recovery",
		}
	}

	operation.CompletedOrdinals = append(
		operation.CompletedOrdinals,
		ordinal,
	)
	sort.Slice(operation.CompletedOrdinals, func(i, j int) bool {
		return operation.CompletedOrdinals[i] <
			operation.CompletedOrdinals[j]
	})
	timestamp := metav1.NewTime(now)
	operation.Reason =
		enterpriseApi.SearchHeadClusterImageUpgradeReasonMemberRecovered
	operation.Message = fmt.Sprintf(
		"Search Head ordinal %d completed image-upgrade recovery",
		ordinal,
	)
	operation.LastTransitionTime = &timestamp
	return ordinalDecision(
		SHCImageUpgradeOrdinalPersist,
		operation,
	)
}

func ordinalDecision(
	action SHCImageUpgradeOrdinalAction,
	operation *enterpriseApi.SearchHeadClusterImageUpgradeStatus,
) SHCImageUpgradeOrdinalDecision {
	return SHCImageUpgradeOrdinalDecision{
		Action:    action,
		Operation: operation,
		Reason:    operation.Reason,
		Message:   operation.Message,
	}
}

// SHCImageUpgradeFinalizationPod is the bounded Kubernetes and SHC
// observation required before finalization.
type SHCImageUpgradeFinalizationPod struct {
	Ordinal          int32
	Exists           bool
	Ready            bool
	Deleting         bool
	Revision         string
	Image            string
	MemberRegistered bool
	MemberStatus     string
}

// SHCImageUpgradeFinalizationInput contains the complete finalization gate.
type SHCImageUpgradeFinalizationInput struct {
	Current                     *enterpriseApi.SearchHeadClusterImageUpgradeStatus
	Pods                        []SHCImageUpgradeFinalizationPod
	StatefulSetReplicas         int32
	StatefulSetPartition        int32
	StatefulSetCurrentRevision  string
	StatefulSetUpdateRevision   string
	StatefulSetTargetImage      string
	LatestMemberLifecycleDone   bool
	Initialized                 bool
	MinPeersJoined              bool
	CaptainReady                bool
	CoordinationOwned           bool
	ConflictingPlannedOperation bool
	ManagementTargetEligible    bool
	Now                         time.Time
}

// SHCImageUpgradeFinalizationAction is the only action an adapter may take
// after evaluating finalization.
type SHCImageUpgradeFinalizationAction string

const (
	SHCImageUpgradeFinalizationWait SHCImageUpgradeFinalizationAction = "Wait"
	// SHCImageUpgradeFinalizationPersist requires a status write and a
	// reconcile boundary. No endpoint call or Ready projection is allowed in
	// the same reconciliation.
	SHCImageUpgradeFinalizationPersist SHCImageUpgradeFinalizationAction = "Persist"
	SHCImageUpgradeFinalizationCall    SHCImageUpgradeFinalizationAction = "Call"
	// SHCImageUpgradeFinalizationFinished is returned only after a persisted
	// Completed phase is observed.
	SHCImageUpgradeFinalizationFinished SHCImageUpgradeFinalizationAction = "Finished"
	SHCImageUpgradeFinalizationBlock    SHCImageUpgradeFinalizationAction = "Block"
)

// SHCImageUpgradeFinalizationDecision never aliases Current.
type SHCImageUpgradeFinalizationDecision struct {
	Action    SHCImageUpgradeFinalizationAction
	Operation *enterpriseApi.SearchHeadClusterImageUpgradeStatus
	Reason    enterpriseApi.SearchHeadClusterImageUpgradeReason
	Message   string
}

// EvaluateSHCImageUpgradeFinalization enforces persistence barriers between
// member recovery, finalization intent, endpoint success, and completion.
func EvaluateSHCImageUpgradeFinalization(
	input SHCImageUpgradeFinalizationInput,
) SHCImageUpgradeFinalizationDecision {
	if input.Current == nil {
		return finalizationDecisionWithoutOperation(
			SHCImageUpgradeFinalizationWait,
			enterpriseApi.SearchHeadClusterImageUpgradeReasonClusterNotReady,
			"wait for a durable Search Head Cluster image-upgrade workflow",
		)
	}
	operation := input.Current.DeepCopy()
	if operation.Phase ==
		enterpriseApi.SearchHeadClusterImageUpgradePhaseCompleted {
		return finalizationDecision(
			SHCImageUpgradeFinalizationFinished,
			operation,
		)
	}
	if operation.Phase ==
		enterpriseApi.SearchHeadClusterImageUpgradePhaseBlocked ||
		operation.Phase ==
			enterpriseApi.SearchHeadClusterImageUpgradePhaseFailed {
		return finalizationDecision(
			SHCImageUpgradeFinalizationBlock,
			operation,
		)
	}
	if operation.Phase ==
		enterpriseApi.SearchHeadClusterImageUpgradePhaseFinalizing &&
		operation.FinalizationSucceededAt != nil {
		completeImageUpgradeFinalization(operation, input.Now)
		return finalizationDecision(
			SHCImageUpgradeFinalizationPersist,
			operation,
		)
	}
	if input.ConflictingPlannedOperation {
		blockImageUpgrade(
			operation,
			enterpriseApi.SearchHeadClusterImageUpgradeReasonConflictingPlannedOperation,
			"another planned operation conflicts with image-upgrade finalization",
			input.Now,
		)
		return finalizationDecision(
			SHCImageUpgradeFinalizationBlock,
			operation,
		)
	}
	if !input.CoordinationOwned {
		return finalizationWaitForOperation(
			operation,
			"wait to recover durable image-upgrade lifecycle coordination",
		)
	}

	switch operation.Phase {
	case enterpriseApi.SearchHeadClusterImageUpgradePhaseRollingMembers:
		if !shcImageUpgradeFinalizationEligible(input, operation) {
			return finalizationWaitForOperation(
				operation,
				"wait for every Kubernetes and Search Head recovery gate before finalization",
			)
		}
		timestamp := metav1.NewTime(input.Now)
		operation.Phase =
			enterpriseApi.SearchHeadClusterImageUpgradePhasePendingFinalization
		operation.Reason =
			enterpriseApi.SearchHeadClusterImageUpgradeReasonAllMembersRecovered
		operation.Message =
			"all Search Head image-upgrade members recovered"
		operation.PhaseStartedAt = &timestamp
		operation.LastTransitionTime = &timestamp
		return finalizationDecision(
			SHCImageUpgradeFinalizationPersist,
			operation,
		)

	case enterpriseApi.SearchHeadClusterImageUpgradePhasePendingFinalization:
		if !shcImageUpgradeFinalizationEligible(input, operation) {
			return finalizationWaitForOperation(
				operation,
				"finalization eligibility was lost before intent was recorded",
			)
		}
		recordFinalizationIntent(operation, input.Now)
		return finalizationDecision(
			SHCImageUpgradeFinalizationPersist,
			operation,
		)

	case enterpriseApi.SearchHeadClusterImageUpgradePhaseFinalizing:
		if operation.FinalizationIntentAt == nil {
			recordFinalizationIntent(operation, input.Now)
			return finalizationDecision(
				SHCImageUpgradeFinalizationPersist,
				operation,
			)
		}
		if !shcImageUpgradeFinalizationEligible(input, operation) {
			return finalizationWaitForOperation(
				operation,
				"wait to restore finalization eligibility before retrying",
			)
		}
		if !input.ManagementTargetEligible {
			return finalizationWaitForOperation(
				operation,
				"wait for an eligible Search Head finalization target",
			)
		}
		return finalizationDecision(
			SHCImageUpgradeFinalizationCall,
			operation,
		)

	default:
		return finalizationDecision(
			SHCImageUpgradeFinalizationWait,
			operation,
		)
	}
}

// RecordSHCImageUpgradeFinalizationAttempt creates the status to persist after
// one authorized upgrade-finalize request. Endpoint error text is excluded.
func RecordSHCImageUpgradeFinalizationAttempt(
	current *enterpriseApi.SearchHeadClusterImageUpgradeStatus,
	succeeded bool,
	now time.Time,
) SHCImageUpgradeFinalizationDecision {
	if current == nil {
		return finalizationDecisionWithoutOperation(
			SHCImageUpgradeFinalizationBlock,
			enterpriseApi.SearchHeadClusterImageUpgradeReasonClusterNotReady,
			"cannot record finalization without a durable image-upgrade workflow",
		)
	}
	operation := current.DeepCopy()
	if operation.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseFinalizing ||
		operation.FinalizationIntentAt == nil {
		return finalizationDecision(
			SHCImageUpgradeFinalizationBlock,
			operation,
		)
	}
	if operation.FinalizationSucceededAt != nil {
		return finalizationDecision(
			SHCImageUpgradeFinalizationWait,
			operation,
		)
	}

	timestamp := metav1.NewTime(now)
	operation.FinalizationLastAttemptAt = &timestamp
	if operation.FinalizationAttemptCount < math.MaxInt32 {
		operation.FinalizationAttemptCount++
	}
	operation.LastTransitionTime = &timestamp
	if succeeded {
		operation.FinalizationSucceededAt = &timestamp
		operation.Reason =
			enterpriseApi.SearchHeadClusterImageUpgradeReasonFinalizationSucceeded
		operation.Message =
			"Search Head Cluster image-upgrade finalization request succeeded"
	} else {
		operation.Reason =
			enterpriseApi.SearchHeadClusterImageUpgradeReasonFinalizationRetrying
		operation.Message =
			"Search Head Cluster image-upgrade finalization request will be retried"
	}
	return finalizationDecision(
		SHCImageUpgradeFinalizationPersist,
		operation,
	)
}

func shcImageUpgradeFinalizationEligible(
	input SHCImageUpgradeFinalizationInput,
	operation *enterpriseApi.SearchHeadClusterImageUpgradeStatus,
) bool {
	if operation.TargetReplicas <= 0 ||
		operation.DesiredRevision == "" ||
		operation.TargetImage == "" ||
		operation.InitializationSucceededAt == nil ||
		input.StatefulSetReplicas != operation.TargetReplicas ||
		input.StatefulSetPartition != operation.TargetReplicas ||
		input.StatefulSetCurrentRevision != operation.DesiredRevision ||
		input.StatefulSetUpdateRevision != operation.DesiredRevision ||
		input.StatefulSetTargetImage != operation.TargetImage ||
		!input.LatestMemberLifecycleDone ||
		!input.Initialized ||
		!input.MinPeersJoined ||
		!input.CaptainReady ||
		!completedImageUpgradeOrdinals(
			operation.CompletedOrdinals,
			operation.TargetReplicas,
		) {
		return false
	}

	pods := append([]SHCImageUpgradeFinalizationPod(nil), input.Pods...)
	sort.Slice(pods, func(i, j int) bool {
		return pods[i].Ordinal < pods[j].Ordinal
	})
	if len(pods) != int(operation.TargetReplicas) {
		return false
	}
	for ordinal, pod := range pods {
		if pod.Ordinal != int32(ordinal) ||
			!pod.Exists ||
			!pod.Ready ||
			pod.Deleting ||
			pod.Revision != operation.DesiredRevision ||
			pod.Image != operation.TargetImage ||
			!pod.MemberRegistered ||
			pod.MemberStatus != "Up" {
			return false
		}
	}
	return true
}

func completeImageUpgradeFinalization(
	operation *enterpriseApi.SearchHeadClusterImageUpgradeStatus,
	now time.Time,
) {
	timestamp := metav1.NewTime(now)
	operation.Phase =
		enterpriseApi.SearchHeadClusterImageUpgradePhaseCompleted
	operation.Reason =
		enterpriseApi.SearchHeadClusterImageUpgradeReasonOperationCompleted
	operation.Message =
		"Search Head Cluster image-upgrade workflow completed"
	operation.CompletedAt = &timestamp
	operation.PhaseStartedAt = &timestamp
	operation.LastTransitionTime = &timestamp
}

func completedImageUpgradeOrdinals(
	ordinals []int32,
	replicas int32,
) bool {
	if len(ordinals) != int(replicas) {
		return false
	}
	completed := append([]int32(nil), ordinals...)
	sort.Slice(completed, func(i, j int) bool {
		return completed[i] < completed[j]
	})
	for ordinal, completedOrdinal := range completed {
		if completedOrdinal != int32(ordinal) {
			return false
		}
	}
	return true
}

func recordFinalizationIntent(
	operation *enterpriseApi.SearchHeadClusterImageUpgradeStatus,
	now time.Time,
) {
	timestamp := metav1.NewTime(now)
	operation.Phase =
		enterpriseApi.SearchHeadClusterImageUpgradePhaseFinalizing
	operation.Reason =
		enterpriseApi.SearchHeadClusterImageUpgradeReasonFinalizationIntentRecorded
	operation.Message =
		"recorded Search Head Cluster image-upgrade finalization intent"
	operation.FinalizationIntentAt = &timestamp
	operation.PhaseStartedAt = &timestamp
	operation.LastTransitionTime = &timestamp
}

func finalizationWaitForOperation(
	operation *enterpriseApi.SearchHeadClusterImageUpgradeStatus,
	message string,
) SHCImageUpgradeFinalizationDecision {
	return SHCImageUpgradeFinalizationDecision{
		Action:    SHCImageUpgradeFinalizationWait,
		Operation: operation,
		Reason:    enterpriseApi.SearchHeadClusterImageUpgradeReasonClusterNotReady,
		Message:   message,
	}
}

func finalizationDecision(
	action SHCImageUpgradeFinalizationAction,
	operation *enterpriseApi.SearchHeadClusterImageUpgradeStatus,
) SHCImageUpgradeFinalizationDecision {
	return SHCImageUpgradeFinalizationDecision{
		Action:    action,
		Operation: operation,
		Reason:    operation.Reason,
		Message:   operation.Message,
	}
}

func finalizationDecisionWithoutOperation(
	action SHCImageUpgradeFinalizationAction,
	reason enterpriseApi.SearchHeadClusterImageUpgradeReason,
	message string,
) SHCImageUpgradeFinalizationDecision {
	return SHCImageUpgradeFinalizationDecision{
		Action:  action,
		Reason:  reason,
		Message: message,
	}
}
