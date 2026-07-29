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
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ReplacementPolicy contains the time bounds used by the replacement
// decision engine.
type ReplacementPolicy struct {
	DetentionTimeout       time.Duration
	SearchDrainTimeout     time.Duration
	CaptainTransferTimeout time.Duration
}

// Observation is one authoritative point-in-time view of the SHC. Callers
// assemble it from Splunk's cluster and member endpoints before evaluating the
// workflow.
type Observation struct {
	ObservedAt                         time.Time
	Available                          bool
	Fresh                              bool
	ConflictingCaptain                 bool
	Initialized                        bool
	MinPeersJoined                     bool
	MaintenanceMode                    bool
	Captain                            string
	CaptainReady                       bool
	TargetMemberObserved               bool
	TargetMemberID                     string
	TargetMemberStatus                 string
	TargetMemberRegistered             bool
	KVStoreObservationRequired         bool
	KVStoreObservationAvailable        bool
	KVStoreNotReadyMembers             []string
	TargetKVStoreReady                 bool
	ActiveHistoricalSearches           int32
	ActiveRealtimeSearches             int32
	CaptainTransferTarget              string
	CaptainTransferTargetManagementURI string
}

// ActionType identifies a side effect that an adapter may perform after it
// has persisted the returned operation status.
type ActionType string

const (
	ActionNone                 ActionType = ""
	ActionObserveCluster       ActionType = "ObserveCluster"
	ActionRequestDetention     ActionType = "RequestDetention"
	ActionTransferCaptain      ActionType = "TransferCaptain"
	ActionAuthorizeReplacement ActionType = "AuthorizeReplacement"
	ActionReleaseDetention     ActionType = "ReleaseDetention"
)

// Action is a declarative request from the decision engine. The engine itself
// never calls Splunk and never mutates Kubernetes objects.
type Action struct {
	Type          ActionType
	Target        string
	ManagementURI string
}

// Decision is the next durable operation state and optional external action.
type Decision struct {
	Operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus
	Action    Action
}

// StartReplacement creates the durable initial state for one Pod replacement.
// The caller owns operation ID generation so an existing operation can be
// recovered without generating a new identity.
func StartReplacement(
	operationID string,
	intent enterpriseApi.SearchHeadClusterLifecycleIntent,
	desiredRevision string,
	targetPod string,
	targetOrdinal int32,
	now time.Time,
) *enterpriseApi.SearchHeadClusterLifecycleOperationStatus {
	timestamp := metav1.NewTime(now)
	return &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:        operationID,
		Intent:             intent,
		DesiredRevision:    desiredRevision,
		TargetPod:          targetPod,
		TargetOrdinal:      &targetOrdinal,
		Stage:              enterpriseApi.SearchHeadClusterLifecycleStageValidatingCluster,
		StartedAt:          &timestamp,
		StageStartedAt:     &timestamp,
		LastTransitionTime: &timestamp,
		Reason:             enterpriseApi.SearchHeadClusterLifecycleReasonOperationStarted,
		Message:            fmt.Sprintf("validating cluster before replacing %s", targetPod),
	}
}

// CompleteScaleDown records durable completion only after Kubernetes observes
// that the permanently removed ordinal is no longer part of the StatefulSet.
func CompleteScaleDown(
	current *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	observedReplicas int32,
	now time.Time,
) *enterpriseApi.SearchHeadClusterLifecycleOperationStatus {
	if current == nil {
		return nil
	}
	operation := current.DeepCopy()
	if operation.Intent != enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown ||
		operation.Stage !=
			enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement ||
		operation.TargetOrdinal == nil ||
		operation.MembershipRemovalRequestedAt == nil ||
		observedReplicas > *operation.TargetOrdinal {
		return operation
	}
	transition(
		operation,
		enterpriseApi.SearchHeadClusterLifecycleStageCompleted,
		enterpriseApi.SearchHeadClusterLifecycleReasonOperationCompleted,
		fmt.Sprintf(
			"%s was permanently removed from the Search Head Cluster",
			operation.TargetPod,
		),
		now,
	)
	return operation
}

// StartScaleDownCancellation converts a scale-down that no longer matches the
// desired replica count into a durable recovery operation. Cancellation is
// safe only while Kubernetes still observes the target ordinal and Splunk
// membership removal has not been requested. The returned boolean is true
// only for the first transition so the adapter can persist the new stage
// before releasing detention.
func StartScaleDownCancellation(
	current *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	observedReplicas int32,
	desiredReplicas int32,
	now time.Time,
) (*enterpriseApi.SearchHeadClusterLifecycleOperationStatus, bool) {
	if current == nil {
		return nil, false
	}
	operation := current.DeepCopy()
	if operation.Intent !=
		enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown ||
		operation.TargetOrdinal == nil ||
		operation.Stage == enterpriseApi.SearchHeadClusterLifecycleStageCompleted ||
		operation.MembershipRemovalRequestedAt != nil ||
		observedReplicas <= *operation.TargetOrdinal ||
		desiredReplicas <= *operation.TargetOrdinal {
		return operation, false
	}
	if operation.Stage ==
		enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery {
		return operation, false
	}

	startedAt := metav1.NewTime(now)
	operation.MemberRejoinStartedAt = &startedAt
	transition(
		operation,
		enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery,
		enterpriseApi.SearchHeadClusterLifecycleReasonScaleDownCancelled,
		fmt.Sprintf(
			"scale-down request was withdrawn; restoring %s to service",
			operation.TargetPod,
		),
		now,
	)
	return operation, true
}

// StartPodUpdateCancellation converts a Pod update whose desired revision was
// superseded or withdrawn into an in-place recovery operation. Cancellation is
// safe only before Kubernetes replacement was authorized and while the
// original target Pod identity is still recorded. The recovery workflow
// verifies that identity and releases detention before completing.
func StartPodUpdateCancellation(
	current *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	desiredRevision string,
	now time.Time,
) (*enterpriseApi.SearchHeadClusterLifecycleOperationStatus, bool) {
	if current == nil {
		return nil, false
	}
	operation := current.DeepCopy()
	if operation.Intent !=
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate ||
		operation.Stage == enterpriseApi.SearchHeadClusterLifecycleStageCompleted ||
		operation.Stage ==
			enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery ||
		operation.DesiredRevision == "" ||
		desiredRevision == "" ||
		operation.DesiredRevision == desiredRevision ||
		operation.ReplacementAuthorizedAt != nil ||
		operation.TargetPodUID == "" {
		return operation, false
	}

	startedAt := metav1.NewTime(now)
	operation.MemberRejoinStartedAt = &startedAt
	transition(
		operation,
		enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery,
		enterpriseApi.SearchHeadClusterLifecycleReasonPodUpdateCancelled,
		fmt.Sprintf(
			"Pod update revision %s was withdrawn or superseded; restoring %s to service before revision %s",
			operation.DesiredRevision,
			operation.TargetPod,
			desiredRevision,
		),
		now,
	)
	return operation, true
}

// AuthorizedPodUpdateRevisionRecoveryEligible reports whether an authorized
// replacement has reached a structured Kubernetes startup failure for which a
// changed desired template may safely request recovery at a known-good
// revision. It deliberately excludes generic startup waits and identity
// failures: those remain fail closed until their own policy is defined.
func AuthorizedPodUpdateRevisionRecoveryEligible(
	current *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
) bool {
	if current == nil ||
		current.Intent !=
			enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate ||
		current.TargetOrdinal == nil ||
		current.TargetPodUID == "" ||
		current.DesiredRevision == "" ||
		current.ReplacementAuthorizedAt == nil ||
		current.RecoveryRevision != "" {
		return false
	}

	switch current.Stage {
	case enterpriseApi.SearchHeadClusterLifecycleStageWaitingForScheduling:
		return current.Reason ==
			enterpriseApi.SearchHeadClusterLifecycleReasonPodUnschedulable
	case enterpriseApi.SearchHeadClusterLifecycleStageWaitingForStorage:
		return current.Reason ==
			enterpriseApi.SearchHeadClusterLifecycleReasonVolumeAttachmentPending
	case enterpriseApi.SearchHeadClusterLifecycleStageWaitingForContainer:
		return current.Reason ==
			enterpriseApi.SearchHeadClusterLifecycleReasonImagePullFailed
	case enterpriseApi.SearchHeadClusterLifecycleStageBlocked:
		switch current.Reason {
		case enterpriseApi.SearchHeadClusterLifecycleReasonPodStartupTimedOut,
			enterpriseApi.SearchHeadClusterLifecycleReasonImagePullFailed,
			enterpriseApi.SearchHeadClusterLifecycleReasonSplunkStartupFailed:
			return true
		}
	}
	return false
}

// StartAuthorizedPodUpdateRevisionRecovery records a persistence barrier before
// Kubernetes is asked to re-close the partition and restore the one unavailable
// target at the StatefulSet's last known-good revision. The original
// DesiredRevision and operation identity remain immutable audit evidence. A
// queued CR template is reconciled only after this recovery completes.
func StartAuthorizedPodUpdateRevisionRecovery(
	current *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	recoveryRevision string,
	now time.Time,
) (*enterpriseApi.SearchHeadClusterLifecycleOperationStatus, bool) {
	if !AuthorizedPodUpdateRevisionRecoveryEligible(current) ||
		recoveryRevision == "" ||
		recoveryRevision == current.DesiredRevision {
		if current == nil {
			return nil, false
		}
		return current.DeepCopy(), false
	}

	operation := current.DeepCopy()
	startedAt := metav1.NewTime(now)
	operation.RecoveryRevision = recoveryRevision
	operation.RevisionWithdrawalStartedAt = &startedAt
	operation.ReplacementPodUID = ""
	operation.ReplacementMemberID = ""
	operation.ReplacementPodObservedAt = nil
	operation.MemberRejoinStartedAt = nil
	operation.DetentionReleaseRequestedAt = nil
	transition(
		operation,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForScheduling,
		enterpriseApi.SearchHeadClusterLifecycleReasonAuthorizedRevisionWithdrawn,
		fmt.Sprintf(
			"authorized revision %s was withdrawn or superseded; recovering %s at last known-good revision %s before reconciling the queued template",
			operation.DesiredRevision,
			operation.TargetPod,
			recoveryRevision,
		),
		now,
	)
	return operation, true
}

// ApplySearchDrainContinuationApproval consumes one exact post-timeout
// approval. The operation ID and controller-issued token must both match the
// current fail-closed operation. The returned transition is a persistence
// barrier: cluster safety, detention, captaincy, and replacement authorization
// are re-evaluated on a later reconciliation.
func ApplySearchDrainContinuationApproval(
	current *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	approval *enterpriseApi.SearchHeadClusterLifecycleApproval,
	approvalGeneration int64,
	activeHistoricalSearches int32,
	activeRealtimeSearches int32,
	now time.Time,
) (*enterpriseApi.SearchHeadClusterLifecycleOperationStatus, bool) {
	if current == nil {
		return nil, false
	}
	operation := current.DeepCopy()
	if approval == nil ||
		approval.Action != enterpriseApi.
			SearchHeadClusterLifecycleApprovalActionContinueAfterSearchDrainTimeout ||
		approval.OperationID != operation.OperationID ||
		approval.Token == "" ||
		approval.Token != operation.SearchDrainContinuationToken ||
		operation.Stage != enterpriseApi.SearchHeadClusterLifecycleStageBlocked ||
		operation.Reason !=
			enterpriseApi.SearchHeadClusterLifecycleReasonSearchDrainTimedOut ||
		operation.SearchDrainContinuationApprovedAt != nil ||
		operation.ReplacementAuthorizedAt != nil ||
		operation.TargetPodUID == "" {
		return operation, false
	}

	approvedAt := metav1.NewTime(now)
	operation.SearchDrainContinuationApprovedAt = &approvedAt
	operation.SearchDrainContinuationApprovalGeneration = approvalGeneration
	operation.ApprovedActiveHistoricalSearches = activeHistoricalSearches
	operation.ApprovedActiveRealtimeSearches = activeRealtimeSearches
	operation.ActiveHistoricalSearches = activeHistoricalSearches
	operation.ActiveRealtimeSearches = activeRealtimeSearches
	transition(
		operation,
		enterpriseApi.SearchHeadClusterLifecycleStageDrainingSearches,
		enterpriseApi.
			SearchHeadClusterLifecycleReasonSearchDrainContinuationApproved,
		fmt.Sprintf(
			"approved continuation after search-drain timeout for %s: historical=%d realtime=%d; revalidating before replacement",
			operation.TargetPod,
			activeHistoricalSearches,
			activeRealtimeSearches,
		),
		now,
	)
	return operation, true
}

// EvaluateReplacement advances a durable replacement operation from an
// authoritative observation. The input operation is never mutated.
func EvaluateReplacement(
	current *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	observation Observation,
	policy ReplacementPolicy,
	now time.Time,
) Decision {
	if current == nil {
		return Decision{}
	}

	operation := current.DeepCopy()
	recordObservation(operation, observation)

	switch operation.Stage {
	case enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
		enterpriseApi.SearchHeadClusterLifecycleStageFailed,
		enterpriseApi.SearchHeadClusterLifecycleStageCompleted:
		return Decision{Operation: operation}
	}

	// Deadline enforcement must not depend on Splunk remaining observable. An
	// unavailable or unready captain is precisely when the controller must
	// continue aging an in-flight operation instead of waiting forever.
	if operation.Stage == enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget &&
		stageTimedOut(operation, policy.DetentionTimeout, now) {
		transition(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
			enterpriseApi.SearchHeadClusterLifecycleReasonDetentionTimedOut,
			fmt.Sprintf(
				"traffic withdrawal and manual detention were not confirmed for %s before the deadline",
				operation.TargetPod,
			),
			now,
		)
		return Decision{Operation: operation}
	}
	if operation.Stage == enterpriseApi.SearchHeadClusterLifecycleStageDrainingSearches &&
		operation.SearchDrainContinuationApprovedAt == nil &&
		stageTimedOut(operation, policy.SearchDrainTimeout, now) {
		transition(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
			enterpriseApi.SearchHeadClusterLifecycleReasonSearchDrainTimedOut,
			fmt.Sprintf("search drain timed out for %s: historical=%d realtime=%d",
				operation.TargetPod,
				observation.ActiveHistoricalSearches,
				observation.ActiveRealtimeSearches,
			),
			now,
		)
		operation.SearchDrainContinuationToken =
			newSearchDrainContinuationToken(operation)
		return Decision{Operation: operation}
	}
	if operation.Stage == enterpriseApi.SearchHeadClusterLifecycleStageTransferringCaptain &&
		stageTimedOut(operation, policy.CaptainTransferTimeout, now) {
		transition(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
			enterpriseApi.SearchHeadClusterLifecycleReasonCaptainTransferTimedOut,
			fmt.Sprintf("captain transfer away from %s timed out", operation.TargetPod),
			now,
		)
		return Decision{Operation: operation}
	}

	if decision, stop := validateObservation(operation, observation, now); stop {
		return decision
	}

	switch operation.Stage {
	case enterpriseApi.SearchHeadClusterLifecycleStageValidatingCluster:
		transition(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget,
			enterpriseApi.SearchHeadClusterLifecycleReasonDetentionRequested,
			fmt.Sprintf("requesting manual detention for %s", operation.TargetPod),
			now,
		)
		return Decision{
			Operation: operation,
			Action: Action{
				Type:   ActionRequestDetention,
				Target: operation.TargetPod,
			},
		}

	case enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget:
		if observation.TargetMemberStatus != "ManualDetention" {
			setReason(
				operation,
				enterpriseApi.SearchHeadClusterLifecycleReasonDetentionRequested,
				fmt.Sprintf("waiting for %s to enter manual detention", operation.TargetPod),
				now,
			)
			return Decision{
				Operation: operation,
				Action: Action{
					Type:   ActionRequestDetention,
					Target: operation.TargetPod,
				},
			}
		}

		transition(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleStageDrainingSearches,
			enterpriseApi.SearchHeadClusterLifecycleReasonSearchesActive,
			searchDrainMessage(observation),
			now,
		)
		return evaluateSearchDrain(operation, observation, policy, now)

	case enterpriseApi.SearchHeadClusterLifecycleStageDrainingSearches:
		if observation.TargetMemberStatus != "ManualDetention" {
			transition(
				operation,
				enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget,
				enterpriseApi.SearchHeadClusterLifecycleReasonDetentionRequested,
				fmt.Sprintf("%s is no longer in manual detention", operation.TargetPod),
				now,
			)
			return Decision{
				Operation: operation,
				Action: Action{
					Type:   ActionRequestDetention,
					Target: operation.TargetPod,
				},
			}
		}
		return evaluateSearchDrain(operation, observation, policy, now)

	case enterpriseApi.SearchHeadClusterLifecycleStageTransferringCaptain:
		return evaluateCaptainTransfer(operation, observation, policy, now)

	case enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement:
		return Decision{
			Operation: operation,
			Action: Action{
				Type:   ActionAuthorizeReplacement,
				Target: operation.TargetPod,
			},
		}
	}

	transition(
		operation,
		enterpriseApi.SearchHeadClusterLifecycleStageFailed,
		enterpriseApi.SearchHeadClusterLifecycleReasonClusterNotSafe,
		fmt.Sprintf("unsupported lifecycle stage %q", operation.Stage),
		now,
	)
	return Decision{Operation: operation}
}

func evaluateSearchDrain(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	observation Observation,
	policy ReplacementPolicy,
	now time.Time,
) Decision {
	if (observation.ActiveHistoricalSearches > 0 ||
		observation.ActiveRealtimeSearches > 0) &&
		operation.SearchDrainContinuationApprovedAt == nil {
		setReason(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleReasonSearchesActive,
			searchDrainMessage(observation),
			now,
		)
		return Decision{Operation: operation}
	}

	if observation.Captain == operation.TargetPod {
		transition(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleStageTransferringCaptain,
			enterpriseApi.SearchHeadClusterLifecycleReasonCaptainTransferRequired,
			fmt.Sprintf("%s is the active captain; captain transfer must be confirmed before replacement", operation.TargetPod),
			now,
		)
		return evaluateCaptainTransfer(operation, observation, policy, now)
	}

	return authorizeReplacement(operation, now)
}

func evaluateCaptainTransfer(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	observation Observation,
	policy ReplacementPolicy,
	now time.Time,
) Decision {
	if observation.Captain != operation.TargetPod {
		if !observation.CaptainReady {
			setReason(
				operation,
				enterpriseApi.SearchHeadClusterLifecycleReasonCaptainTransferRequired,
				fmt.Sprintf("waiting for new captain %s to become ready", observation.Captain),
				now,
			)
			return Decision{
				Operation: operation,
				Action:    Action{Type: ActionObserveCluster},
			}
		}
		return authorizeReplacement(operation, now)
	}

	if operation.CaptainTransferRequestedAt != nil {
		setReason(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleReasonCaptainTransferRequired,
			fmt.Sprintf("waiting to confirm captain transfer from %s to %s",
				operation.TargetPod,
				operation.CaptainTransferTarget,
			),
			now,
		)
		return Decision{
			Operation: operation,
			Action:    Action{Type: ActionObserveCluster},
		}
	}

	if observation.CaptainTransferTarget == "" ||
		observation.CaptainTransferTarget == operation.TargetPod ||
		observation.CaptainTransferTargetManagementURI == "" {
		setReason(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleReasonClusterNotSafe,
			"no eligible non-target captain candidate is available",
			now,
		)
		return Decision{
			Operation: operation,
			Action:    Action{Type: ActionObserveCluster},
		}
	}

	setReason(
		operation,
		enterpriseApi.SearchHeadClusterLifecycleReasonCaptainTransferRequired,
		fmt.Sprintf("requesting captain transfer from %s to %s", operation.TargetPod, observation.CaptainTransferTarget),
		now,
	)
	operation.CaptainTransferTarget = observation.CaptainTransferTarget
	return Decision{
		Operation: operation,
		Action: Action{
			Type:          ActionTransferCaptain,
			Target:        observation.CaptainTransferTarget,
			ManagementURI: observation.CaptainTransferTargetManagementURI,
		},
	}
}

func authorizeReplacement(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	now time.Time,
) Decision {
	transition(
		operation,
		enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement,
		enterpriseApi.SearchHeadClusterLifecycleReasonReplacementAuthorized,
		fmt.Sprintf("%s is detained, drained, and is not the active captain", operation.TargetPod),
		now,
	)
	return Decision{
		Operation: operation,
		Action: Action{
			Type:   ActionAuthorizeReplacement,
			Target: operation.TargetPod,
		},
	}
}

func validateObservation(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	observation Observation,
	now time.Time,
) (Decision, bool) {
	if !observation.Available || !observation.Fresh {
		setReason(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleReasonObservationStale,
			"a fresh authoritative SHC observation is required",
			now,
		)
		return Decision{
			Operation: operation,
			Action:    Action{Type: ActionObserveCluster},
		}, true
	}

	if observation.ConflictingCaptain {
		// A captain transfer is not observed atomically across Splunk's
		// captain-info and captain-members endpoints. After the transfer
		// request has been accepted, allow the two authoritative views to
		// converge while the existing captain-transfer deadline continues
		// to age. Replacement remains unauthorized until they agree.
		if operation.Stage == enterpriseApi.SearchHeadClusterLifecycleStageTransferringCaptain &&
			operation.CaptainTransferRequestedAt != nil {
			setReason(
				operation,
				enterpriseApi.SearchHeadClusterLifecycleReasonCaptainTransferRequired,
				"waiting for authoritative captain observations to converge after transfer request",
				now,
			)
			return Decision{
				Operation: operation,
				Action:    Action{Type: ActionObserveCluster},
			}, true
		}
		transition(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
			enterpriseApi.SearchHeadClusterLifecycleReasonConflictingCaptainObservation,
			"authoritative observations disagree on the active captain",
			now,
		)
		return Decision{Operation: operation}, true
	}

	if !observation.Initialized ||
		!observation.MinPeersJoined ||
		observation.MaintenanceMode ||
		observation.Captain == "" ||
		!observation.CaptainReady ||
		!observation.TargetMemberObserved ||
		!observation.TargetMemberRegistered ||
		observation.TargetMemberID == "" {
		setReason(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleReasonClusterNotSafe,
			"SHC is not safe for member replacement",
			now,
		)
		return Decision{
			Operation: operation,
			Action:    Action{Type: ActionObserveCluster},
		}, true
	}

	if operation.Stage ==
		enterpriseApi.SearchHeadClusterLifecycleStageValidatingCluster &&
		observation.KVStoreObservationRequired {
		if !observation.KVStoreObservationAvailable {
			setReason(
				operation,
				enterpriseApi.SearchHeadClusterLifecycleReasonObservationStale,
				"a fresh KV Store status observation is required for every Search Head member",
				now,
			)
			return Decision{
				Operation: operation,
				Action:    Action{Type: ActionObserveCluster},
			}, true
		}
		if len(observation.KVStoreNotReadyMembers) > 0 {
			setReason(
				operation,
				enterpriseApi.SearchHeadClusterLifecycleReasonKVStoreNotReady,
				fmt.Sprintf(
					"wait for every Search Head KV Store to report ready: %s",
					strings.Join(observation.KVStoreNotReadyMembers, ", "),
				),
				now,
			)
			return Decision{
				Operation: operation,
				Action:    Action{Type: ActionObserveCluster},
			}, true
		}
	}

	if targetKVStoreRequired(operation, observation) &&
		observation.KVStoreObservationRequired {
		if !observation.KVStoreObservationAvailable {
			setReason(
				operation,
				enterpriseApi.SearchHeadClusterLifecycleReasonObservationStale,
				fmt.Sprintf(
					"a fresh KV Store status observation is required for detained member %s",
					operation.TargetPod,
				),
				now,
			)
			return Decision{
				Operation: operation,
				Action:    Action{Type: ActionObserveCluster},
			}, true
		}
		if !observation.TargetKVStoreReady {
			setReason(
				operation,
				enterpriseApi.SearchHeadClusterLifecycleReasonKVStoreNotReady,
				fmt.Sprintf(
					"wait for detained member KV Store to report ready: %s",
					strings.Join(observation.KVStoreNotReadyMembers, ", "),
				),
				now,
			)
			return Decision{
				Operation: operation,
				Action:    Action{Type: ActionObserveCluster},
			}, true
		}
	}

	return Decision{Operation: operation}, false
}

func targetKVStoreRequired(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	observation Observation,
) bool {
	switch operation.Stage {
	case enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget:
		return observation.TargetMemberStatus == "ManualDetention"
	case enterpriseApi.SearchHeadClusterLifecycleStageDrainingSearches,
		enterpriseApi.SearchHeadClusterLifecycleStageTransferringCaptain,
		enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement:
		return true
	default:
		return false
	}
}

func recordObservation(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	observation Observation,
) {
	operation.Captain = observation.Captain
	operation.CaptainReady = observation.CaptainReady
	operation.ActiveHistoricalSearches = observation.ActiveHistoricalSearches
	operation.ActiveRealtimeSearches = observation.ActiveRealtimeSearches
	if observation.KVStoreObservationRequired &&
		observation.KVStoreObservationAvailable {
		operation.KVStoreNotReadyMembers = append(
			[]string(nil),
			observation.KVStoreNotReadyMembers...,
		)
		if !observation.ObservedAt.IsZero() {
			observedAt := metav1.NewTime(observation.ObservedAt)
			operation.LastSuccessfulKVStoreObservation = &observedAt
		}
	}
	if operation.TargetMemberID == "" && observation.TargetMemberID != "" {
		operation.TargetMemberID = observation.TargetMemberID
	}
	if observation.Available && observation.Fresh && !observation.ObservedAt.IsZero() {
		observedAt := metav1.NewTime(observation.ObservedAt)
		operation.LastSuccessfulSHCObservation = &observedAt
	}
}

func transition(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	stage enterpriseApi.SearchHeadClusterLifecycleStage,
	reason enterpriseApi.SearchHeadClusterLifecycleReason,
	message string,
	now time.Time,
) {
	timestamp := metav1.NewTime(now)
	operation.Stage = stage
	operation.StageStartedAt = &timestamp
	operation.LastTransitionTime = &timestamp
	operation.Reason = reason
	operation.Message = message
}

func setReason(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	reason enterpriseApi.SearchHeadClusterLifecycleReason,
	message string,
	now time.Time,
) {
	if operation.Reason == reason && operation.Message == message {
		return
	}
	timestamp := metav1.NewTime(now)
	operation.Reason = reason
	operation.Message = message
	operation.LastTransitionTime = &timestamp
}

// RecordDetentionRequestAttempt records an idempotent desired-state request
// while authoritative SHC observations remain responsible for confirming
// that the member entered manual detention.
func RecordDetentionRequestAttempt(
	current *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	now time.Time,
) *enterpriseApi.SearchHeadClusterLifecycleOperationStatus {
	if current == nil {
		return nil
	}
	operation := current.DeepCopy()
	if operation.DetentionRequestedAt == nil {
		requestedAt := metav1.NewTime(now)
		operation.DetentionRequestedAt = &requestedAt
	}
	operation.DetentionRequestAttemptCount++
	setReason(
		operation,
		enterpriseApi.SearchHeadClusterLifecycleReasonDetentionRequested,
		"manual detention was requested; waiting for authoritative confirmation",
		now,
	)
	return operation
}

// RecordReplacementAuthorization records the Kubernetes replacement boundary
// only when the Pod still has the identity captured before detention. A
// changed or missing UID is an unplanned replacement and blocks this planned
// operation instead of adopting the new Pod as its original target.
func RecordReplacementAuthorization(
	current *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	podUID string,
	now time.Time,
) (*enterpriseApi.SearchHeadClusterLifecycleOperationStatus, bool) {
	if current == nil {
		return nil, false
	}
	operation := current.DeepCopy()
	if podUID == "" ||
		(operation.TargetPodUID != "" &&
			operation.TargetPodUID != podUID) {
		transition(
			operation,
			enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
			enterpriseApi.SearchHeadClusterLifecycleReasonMemberIdentityMismatch,
			fmt.Sprintf(
				"target Pod %s changed from UID %q to %q before replacement authorization",
				operation.TargetPod,
				operation.TargetPodUID,
				podUID,
			),
			now,
		)
		return operation, false
	}
	operation.TargetPodUID = podUID
	if operation.ReplacementAuthorizedAt == nil {
		authorizedAt := metav1.NewTime(now)
		operation.ReplacementAuthorizedAt = &authorizedAt
	}
	return operation, true
}

func stageTimedOut(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	timeout time.Duration,
	now time.Time,
) bool {
	return timeout > 0 &&
		operation.StageStartedAt != nil &&
		!now.Before(operation.StageStartedAt.Add(timeout))
}

func searchDrainMessage(observation Observation) string {
	return fmt.Sprintf(
		"waiting for active searches to drain: historical=%d realtime=%d",
		observation.ActiveHistoricalSearches,
		observation.ActiveRealtimeSearches,
	)
}

func newSearchDrainContinuationToken(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
) string {
	blockedAt := ""
	if operation.StageStartedAt != nil {
		blockedAt = operation.StageStartedAt.Time.UTC().Format(time.RFC3339Nano)
	}
	return fmt.Sprintf(
		"%x",
		sha256.Sum256(
			[]byte(operation.OperationID+"\x00"+blockedAt),
		),
	)
}
