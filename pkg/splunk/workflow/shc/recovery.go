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
	"fmt"
	"slices"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RecoveryPolicy contains the time bounds for Pod termination and complete
// member recovery.
type RecoveryPolicy struct {
	TerminationTimeout  time.Duration
	MemberRejoinTimeout time.Duration
}

// RecoveryObservation separates Kubernetes Pod recovery from the stronger
// Splunk member recovery contract.
type RecoveryObservation struct {
	PodExists                bool
	PodUID                   string
	PodDeleting              bool
	PodScheduled             bool
	PodUnschedulable         bool
	StoragePending           bool
	ImagePullFailed          bool
	ContainerStartupFailed   bool
	ContainerFailureTerminal bool
	PodReady                 bool
	PodRevision              string
	MemberObserved           bool
	MemberStatus             string
	MemberRegistered         bool
	CaptainMemberObserved    bool
	CaptainMemberID          string
	CaptainMemberStatus      string
	CaptainReady             bool
	AuthoritativeCaptain     bool
}

// EvaluateRecovery advances an authorized Pod replacement through Kubernetes
// recovery, SHC rejoin, detention release, and durable completion.
func EvaluateRecovery(
	current *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	observation RecoveryObservation,
	policy RecoveryPolicy,
	now time.Time,
) Decision {
	if current == nil {
		return Decision{}
	}
	operation := current.DeepCopy()
	if operation.Stage == enterpriseApi.SearchHeadClusterLifecycleStageBlocked ||
		operation.Stage == enterpriseApi.SearchHeadClusterLifecycleStageFailed ||
		operation.Stage == enterpriseApi.SearchHeadClusterLifecycleStageCompleted {
		return Decision{Operation: operation}
	}

	if recoveryTimedOut(operation, policy.MemberRejoinTimeout, now) {
		transition(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
			enterpriseApi.SearchHeadClusterLifecycleReasonMemberRejoinTimedOut,
			recoveryTimeoutMessage(operation, observation),
			now,
		)
		return Decision{Operation: operation}
	}

	switch operation.Stage {
	case enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement:
		return classifyPodRecovery(operation, observation, policy, now)

	case enterpriseApi.SearchHeadClusterLifecycleStageWaitingForTermination:
		if observation.PodExists && observation.PodUID == operation.TargetPodUID {
			if stageTimedOut(operation, policy.TerminationTimeout, now) {
				transition(
					operation,
					enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
					enterpriseApi.SearchHeadClusterLifecycleReasonPodTerminationTimedOut,
					fmt.Sprintf("original Pod %s did not terminate within its budget", operation.TargetPod),
					now,
				)
			}
			return Decision{Operation: operation}
		}
		return classifyPodRecovery(operation, observation, policy, now)

	case enterpriseApi.SearchHeadClusterLifecycleStageWaitingForScheduling,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForStorage,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForContainer:
		return classifyPodRecovery(operation, observation, policy, now)

	case enterpriseApi.SearchHeadClusterLifecycleStageWaitingForMemberRejoin:
		if decision, stop := validateReplacementIdentity(operation, observation, now); stop {
			return decision
		}
		return evaluateMemberRejoin(operation, observation, now)

	case enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery:
		if decision, stop := validateReplacementIdentity(operation, observation, now); stop {
			return decision
		}
		return validateRecoveredMember(operation, observation, now)
	}

	return Decision{Operation: operation}
}

func classifyPodRecovery(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	observation RecoveryObservation,
	policy RecoveryPolicy,
	now time.Time,
) Decision {
	if !observation.PodExists {
		transitionIfNeeded(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleStageWaitingForScheduling,
			enterpriseApi.SearchHeadClusterLifecycleReasonReplacementAuthorized,
			fmt.Sprintf("waiting for replacement Pod %s to be created", operation.TargetPod),
			now,
		)
		return Decision{Operation: operation}
	}

	if observation.PodUID == operation.TargetPodUID {
		transitionIfNeeded(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleStageWaitingForTermination,
			enterpriseApi.SearchHeadClusterLifecycleReasonReplacementAuthorized,
			fmt.Sprintf("waiting for original Pod %s to terminate", operation.TargetPod),
			now,
		)
		if stageTimedOut(operation, policy.TerminationTimeout, now) {
			transition(
				operation,
				enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
				enterpriseApi.SearchHeadClusterLifecycleReasonPodTerminationTimedOut,
				fmt.Sprintf("original Pod %s did not terminate within its budget", operation.TargetPod),
				now,
			)
		}
		return Decision{Operation: operation}
	}

	operation.ReplacementPodUID = observation.PodUID
	if observation.PodRevision != operation.DesiredRevision {
		transition(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
			enterpriseApi.SearchHeadClusterLifecycleReasonPodRevisionMismatch,
			fmt.Sprintf("replacement Pod revision %q does not match desired revision %q",
				observation.PodRevision,
				operation.DesiredRevision,
			),
			now,
		)
		return Decision{Operation: operation}
	}

	if !observation.PodScheduled {
		reason := enterpriseApi.SearchHeadClusterLifecycleReasonReplacementAuthorized
		message := fmt.Sprintf("waiting for replacement Pod %s to be scheduled", operation.TargetPod)
		if observation.PodUnschedulable {
			reason = enterpriseApi.SearchHeadClusterLifecycleReasonPodUnschedulable
			message = fmt.Sprintf("replacement Pod %s is unschedulable", operation.TargetPod)
		}
		transitionIfNeeded(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleStageWaitingForScheduling,
			reason,
			message,
			now,
		)
		return Decision{Operation: operation}
	}

	if observation.StoragePending {
		transitionIfNeeded(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleStageWaitingForStorage,
			enterpriseApi.SearchHeadClusterLifecycleReasonVolumeAttachmentPending,
			fmt.Sprintf("replacement Pod %s is waiting for storage", operation.TargetPod),
			now,
		)
		return Decision{Operation: operation}
	}

	if observation.ImagePullFailed {
		transition(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
			enterpriseApi.SearchHeadClusterLifecycleReasonImagePullFailed,
			fmt.Sprintf("replacement Pod %s cannot pull its image", operation.TargetPod),
			now,
		)
		return Decision{Operation: operation}
	}

	if observation.ContainerFailureTerminal {
		transition(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
			enterpriseApi.SearchHeadClusterLifecycleReasonSplunkStartupFailed,
			fmt.Sprintf("replacement Pod %s has a terminal container startup failure", operation.TargetPod),
			now,
		)
		return Decision{Operation: operation}
	}

	if !observation.PodReady {
		reason := enterpriseApi.SearchHeadClusterLifecycleReasonReplacementAuthorized
		message := fmt.Sprintf("waiting for containers in replacement Pod %s", operation.TargetPod)
		if observation.ContainerStartupFailed {
			reason = enterpriseApi.SearchHeadClusterLifecycleReasonSplunkStartupFailed
			message = fmt.Sprintf("splunkd has not started in replacement Pod %s", operation.TargetPod)
		}
		transitionIfNeeded(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleStageWaitingForContainer,
			reason,
			message,
			now,
		)
		return Decision{Operation: operation}
	}

	transitionIfNeeded(
		operation,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForMemberRejoin,
		enterpriseApi.SearchHeadClusterLifecycleReasonMemberNotRegistered,
		fmt.Sprintf("replacement Pod %s is locally ready; waiting for SHC rejoin", operation.TargetPod),
		now,
	)
	if operation.MemberRejoinStartedAt == nil {
		startedAt := metav1.NewTime(now)
		operation.MemberRejoinStartedAt = &startedAt
	}
	if decision, stop := validateReplacementIdentity(operation, observation, now); stop {
		return decision
	}
	return evaluateMemberRejoin(operation, observation, now)
}

func recoveryTimeoutMessage(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	observation RecoveryObservation,
) string {
	memberStatusAccepted := observation.MemberStatus == "Up" ||
		observation.MemberStatus == "ManualDetention"
	captainMemberStatusAccepted := observation.CaptainMemberStatus == "Up" ||
		observation.CaptainMemberStatus == "ManualDetention"
	return fmt.Sprintf(
		"member recovery timed out in stage %s: podExists=%t podScheduled=%t podReady=%t memberObserved=%t memberRegistered=%t memberStatusAccepted=%t authoritativeCaptain=%t captainReady=%t captainMemberObserved=%t captainMemberStatusAccepted=%t",
		operation.Stage,
		observation.PodExists,
		observation.PodScheduled,
		observation.PodReady,
		observation.MemberObserved,
		observation.MemberRegistered,
		memberStatusAccepted,
		observation.AuthoritativeCaptain,
		observation.CaptainReady,
		observation.CaptainMemberObserved,
		captainMemberStatusAccepted,
	)
}

func validateReplacementIdentity(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	observation RecoveryObservation,
	now time.Time,
) (Decision, bool) {
	if !observation.MemberObserved || !observation.CaptainMemberObserved {
		return Decision{}, false
	}
	if operation.TargetMemberID == "" {
		transition(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
			enterpriseApi.SearchHeadClusterLifecycleReasonMemberIdentityMismatch,
			"retained member identity was not captured before replacement",
			now,
		)
		return Decision{Operation: operation}, true
	}
	if observation.CaptainMemberID == "" {
		transition(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
			enterpriseApi.SearchHeadClusterLifecycleReasonMemberIdentityMismatch,
			"replacement member identity is missing from the captain view",
			now,
		)
		return Decision{Operation: operation}, true
	}
	if observation.CaptainMemberID != operation.TargetMemberID {
		transition(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
			enterpriseApi.SearchHeadClusterLifecycleReasonMemberIdentityMismatch,
			fmt.Sprintf("replacement member identity does not match retained identity %s", operation.TargetMemberID),
			now,
		)
		return Decision{Operation: operation}, true
	}
	operation.ReplacementMemberID = observation.CaptainMemberID
	return Decision{}, false
}

func evaluateMemberRejoin(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	observation RecoveryObservation,
	now time.Time,
) Decision {
	if !observation.AuthoritativeCaptain || !observation.CaptainReady {
		setReason(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleReasonClusterNotSafe,
			"waiting for one authoritative service-ready captain",
			now,
		)
		return Decision{Operation: operation, Action: Action{Type: ActionObserveCluster}}
	}
	if !observation.MemberObserved ||
		!observation.CaptainMemberObserved ||
		!observation.MemberRegistered {
		setReason(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleReasonMemberNotRegistered,
			fmt.Sprintf("waiting for %s to register in both member and captain views", operation.TargetPod),
			now,
		)
		return Decision{Operation: operation, Action: Action{Type: ActionObserveCluster}}
	}
	if observation.MemberStatus != "Up" && observation.MemberStatus != "ManualDetention" {
		setReason(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleReasonMemberNotUp,
			fmt.Sprintf("member view reports status %q", observation.MemberStatus),
			now,
		)
		return Decision{Operation: operation, Action: Action{Type: ActionObserveCluster}}
	}
	if observation.CaptainMemberStatus != "Up" &&
		observation.CaptainMemberStatus != "ManualDetention" {
		setReason(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleReasonMemberSynchronizationPending,
			fmt.Sprintf("captain member view reports status %q", observation.CaptainMemberStatus),
			now,
		)
		return Decision{Operation: operation, Action: Action{Type: ActionObserveCluster}}
	}

	transition(
		operation,
		enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery,
		enterpriseApi.SearchHeadClusterLifecycleReasonRecoveryValidated,
		fmt.Sprintf("%s retained its identity and rejoined; detention release is required", operation.TargetPod),
		now,
	)
	return Decision{
		Operation: operation,
		Action: Action{
			Type:   ActionReleaseDetention,
			Target: operation.TargetPod,
		},
	}
}

func validateRecoveredMember(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	observation RecoveryObservation,
	now time.Time,
) Decision {
	if observation.MemberStatus == "ManualDetention" ||
		observation.CaptainMemberStatus == "ManualDetention" {
		if operation.DetentionReleaseRequestedAt != nil {
			setReason(
				operation,
				enterpriseApi.SearchHeadClusterLifecycleReasonRecoveryValidated,
				"detention release was requested; waiting for member and captain views to report Up",
				now,
			)
			return Decision{Operation: operation, Action: Action{Type: ActionObserveCluster}}
		}
		return Decision{
			Operation: operation,
			Action: Action{
				Type:   ActionReleaseDetention,
				Target: operation.TargetPod,
			},
		}
	}

	if !observation.AuthoritativeCaptain ||
		!observation.CaptainReady ||
		!observation.MemberObserved ||
		!observation.CaptainMemberObserved ||
		!observation.MemberRegistered ||
		observation.MemberStatus != "Up" ||
		observation.CaptainMemberStatus != "Up" {
		setReason(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleReasonMemberSynchronizationPending,
			"waiting for registered Up status in both member and captain views",
			now,
		)
		return Decision{Operation: operation, Action: Action{Type: ActionObserveCluster}}
	}

	transition(
		operation,
		enterpriseApi.SearchHeadClusterLifecycleStageCompleted,
		enterpriseApi.SearchHeadClusterLifecycleReasonOperationCompleted,
		fmt.Sprintf("%s replacement and SHC recovery completed", operation.TargetPod),
		now,
	)
	if operation.TargetOrdinal != nil &&
		!slices.Contains(operation.CompletedOrdinals, *operation.TargetOrdinal) {
		operation.CompletedOrdinals = append(operation.CompletedOrdinals, *operation.TargetOrdinal)
	}
	return Decision{Operation: operation}
}

func recoveryTimedOut(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	timeout time.Duration,
	now time.Time,
) bool {
	return timeout > 0 &&
		operation.MemberRejoinStartedAt != nil &&
		!now.Before(operation.MemberRejoinStartedAt.Add(timeout))
}

func transitionIfNeeded(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	stage enterpriseApi.SearchHeadClusterLifecycleStage,
	reason enterpriseApi.SearchHeadClusterLifecycleReason,
	message string,
	now time.Time,
) {
	if operation.Stage == stage {
		setReason(operation, reason, message, now)
		return
	}
	transition(operation, stage, reason, message, now)
}
