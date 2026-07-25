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
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SHCImageUpgradeInitializationAction is the only action an adapter may take
// after evaluating the durable initialization state.
type SHCImageUpgradeInitializationAction string

const (
	SHCImageUpgradeInitializationWait SHCImageUpgradeInitializationAction = "Wait"
	// SHCImageUpgradeInitializationPersist requires a status write and a
	// reconcile boundary. No Splunk or member-lifecycle action is allowed in
	// the same reconciliation.
	SHCImageUpgradeInitializationPersist SHCImageUpgradeInitializationAction = "Persist"
	// SHCImageUpgradeInitializationCall permits one upgrade-init request. It
	// is returned only after persisted initialization intent is observed.
	SHCImageUpgradeInitializationCall SHCImageUpgradeInitializationAction = "Call"
	// SHCImageUpgradeInitializationAllowMembers permits the per-member
	// lifecycle to proceed. It is returned only for persisted RollingMembers.
	SHCImageUpgradeInitializationAllowMembers SHCImageUpgradeInitializationAction = "AllowMembers"
	SHCImageUpgradeInitializationBlock        SHCImageUpgradeInitializationAction = "Block"
)

// SHCImageUpgradeInitializationInput contains the bounded observations needed
// before an upgrade-init request or member lifecycle can be authorized.
type SHCImageUpgradeInitializationInput struct {
	Current                     *enterpriseApi.SearchHeadClusterImageUpgradeStatus
	CoordinationOwned           bool
	ConflictingPlannedOperation bool
	ManagementTargetEligible    bool
	Now                         time.Time
}

// SHCImageUpgradeInitializationDecision is side-effect free. Operation is a
// copy and never aliases Current.
type SHCImageUpgradeInitializationDecision struct {
	Action    SHCImageUpgradeInitializationAction
	Operation *enterpriseApi.SearchHeadClusterImageUpgradeStatus
	Reason    enterpriseApi.SearchHeadClusterImageUpgradeReason
	Message   string
}

// EvaluateSHCImageUpgradeInitialization enforces a reconcile boundary between
// recording workflow identity, recording initialization intent, calling
// upgrade-init, recording success, and allowing the first member lifecycle.
func EvaluateSHCImageUpgradeInitialization(
	input SHCImageUpgradeInitializationInput,
) SHCImageUpgradeInitializationDecision {
	if input.Current == nil {
		return initializationDecisionWithoutOperation(
			SHCImageUpgradeInitializationWait,
			enterpriseApi.SearchHeadClusterImageUpgradeReasonClusterNotReady,
			"wait for a durable Search Head Cluster image-upgrade workflow",
		)
	}

	operation := input.Current.DeepCopy()
	if operation.Phase == enterpriseApi.SearchHeadClusterImageUpgradePhaseBlocked ||
		operation.Phase == enterpriseApi.SearchHeadClusterImageUpgradePhaseFailed {
		return initializationDecision(
			SHCImageUpgradeInitializationBlock,
			operation,
		)
	}
	if input.ConflictingPlannedOperation {
		blockImageUpgrade(
			operation,
			enterpriseApi.SearchHeadClusterImageUpgradeReasonConflictingPlannedOperation,
			"another planned operation owns Search Head Cluster lifecycle coordination",
			input.Now,
		)
		return initializationDecision(
			SHCImageUpgradeInitializationBlock,
			operation,
		)
	}

	switch operation.Phase {
	case enterpriseApi.SearchHeadClusterImageUpgradePhasePendingInitialization:
		if !input.CoordinationOwned {
			return initializationWaitForOperation(
				operation,
				"wait to acquire durable image-upgrade lifecycle coordination",
			)
		}
		recordInitializationIntent(operation, input.Now)
		return initializationDecision(
			SHCImageUpgradeInitializationPersist,
			operation,
		)

	case enterpriseApi.SearchHeadClusterImageUpgradePhaseInitializing:
		if !input.CoordinationOwned {
			return initializationWaitForOperation(
				operation,
				"wait to recover durable image-upgrade lifecycle coordination",
			)
		}
		if operation.InitializationIntentAt == nil {
			recordInitializationIntent(operation, input.Now)
			return initializationDecision(
				SHCImageUpgradeInitializationPersist,
				operation,
			)
		}
		if operation.InitializationSucceededAt != nil {
			timestamp := metav1.NewTime(input.Now)
			operation.Phase =
				enterpriseApi.SearchHeadClusterImageUpgradePhaseRollingMembers
			operation.Reason =
				enterpriseApi.SearchHeadClusterImageUpgradeReasonInitializationSucceeded
			operation.Message =
				"persisted Search Head Cluster image-upgrade initialization success"
			operation.PhaseStartedAt = &timestamp
			operation.LastTransitionTime = &timestamp
			return initializationDecision(
				SHCImageUpgradeInitializationPersist,
				operation,
			)
		}
		if !input.ManagementTargetEligible {
			return initializationWaitForOperation(
				operation,
				"wait for an eligible Search Head management target and service-ready captain",
			)
		}
		return initializationDecision(
			SHCImageUpgradeInitializationCall,
			operation,
		)

	case enterpriseApi.SearchHeadClusterImageUpgradePhaseRollingMembers:
		return initializationDecision(
			SHCImageUpgradeInitializationAllowMembers,
			operation,
		)

	default:
		return initializationDecision(
			SHCImageUpgradeInitializationWait,
			operation,
		)
	}
}

// RecordSHCImageUpgradeInitializationAttempt creates the status to persist
// after one authorized upgrade-init request. Endpoint error text is
// intentionally not accepted so status remains bounded and redacted.
func RecordSHCImageUpgradeInitializationAttempt(
	current *enterpriseApi.SearchHeadClusterImageUpgradeStatus,
	succeeded bool,
	now time.Time,
) SHCImageUpgradeInitializationDecision {
	if current == nil {
		return initializationDecisionWithoutOperation(
			SHCImageUpgradeInitializationBlock,
			enterpriseApi.SearchHeadClusterImageUpgradeReasonClusterNotReady,
			"cannot record initialization without a durable image-upgrade workflow",
		)
	}

	operation := current.DeepCopy()
	if operation.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseInitializing ||
		operation.InitializationIntentAt == nil {
		return initializationDecision(
			SHCImageUpgradeInitializationBlock,
			operation,
		)
	}
	if operation.InitializationSucceededAt != nil {
		return initializationDecision(
			SHCImageUpgradeInitializationWait,
			operation,
		)
	}

	timestamp := metav1.NewTime(now)
	operation.InitializationLastAttemptAt = &timestamp
	if operation.InitializationAttemptCount < math.MaxInt32 {
		operation.InitializationAttemptCount++
	}
	operation.LastTransitionTime = &timestamp
	if succeeded {
		operation.InitializationSucceededAt = &timestamp
		operation.Reason =
			enterpriseApi.SearchHeadClusterImageUpgradeReasonInitializationSucceeded
		operation.Message =
			"Search Head Cluster image-upgrade initialization request succeeded"
	} else {
		operation.Reason =
			enterpriseApi.SearchHeadClusterImageUpgradeReasonInitializationRetrying
		operation.Message =
			"Search Head Cluster image-upgrade initialization request will be retried"
	}
	return initializationDecision(
		SHCImageUpgradeInitializationPersist,
		operation,
	)
}

func recordInitializationIntent(
	operation *enterpriseApi.SearchHeadClusterImageUpgradeStatus,
	now time.Time,
) {
	timestamp := metav1.NewTime(now)
	operation.Phase =
		enterpriseApi.SearchHeadClusterImageUpgradePhaseInitializing
	operation.Reason =
		enterpriseApi.SearchHeadClusterImageUpgradeReasonInitializationIntentRecorded
	operation.Message =
		"recorded Search Head Cluster image-upgrade initialization intent"
	operation.InitializationIntentAt = &timestamp
	operation.PhaseStartedAt = &timestamp
	operation.LastTransitionTime = &timestamp
}

func initializationWaitForOperation(
	operation *enterpriseApi.SearchHeadClusterImageUpgradeStatus,
	message string,
) SHCImageUpgradeInitializationDecision {
	return SHCImageUpgradeInitializationDecision{
		Action:    SHCImageUpgradeInitializationWait,
		Operation: operation,
		Reason:    enterpriseApi.SearchHeadClusterImageUpgradeReasonClusterNotReady,
		Message:   message,
	}
}

func initializationDecision(
	action SHCImageUpgradeInitializationAction,
	operation *enterpriseApi.SearchHeadClusterImageUpgradeStatus,
) SHCImageUpgradeInitializationDecision {
	return SHCImageUpgradeInitializationDecision{
		Action:    action,
		Operation: operation,
		Reason:    operation.Reason,
		Message:   operation.Message,
	}
}

func initializationDecisionWithoutOperation(
	action SHCImageUpgradeInitializationAction,
	reason enterpriseApi.SearchHeadClusterImageUpgradeReason,
	message string,
) SHCImageUpgradeInitializationDecision {
	return SHCImageUpgradeInitializationDecision{
		Action:  action,
		Reason:  reason,
		Message: message,
	}
}
