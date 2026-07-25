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
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ReplacementPolicy contains the time bounds used by the replacement
// decision engine.
type ReplacementPolicy struct {
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
	if operation.Stage == enterpriseApi.SearchHeadClusterLifecycleStageDrainingSearches &&
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
	if observation.ActiveHistoricalSearches > 0 || observation.ActiveRealtimeSearches > 0 {
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

	return Decision{Operation: operation}, false
}

func recordObservation(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	observation Observation,
) {
	operation.Captain = observation.Captain
	operation.CaptainReady = observation.CaptainReady
	operation.ActiveHistoricalSearches = observation.ActiveHistoricalSearches
	operation.ActiveRealtimeSearches = observation.ActiveRealtimeSearches
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
