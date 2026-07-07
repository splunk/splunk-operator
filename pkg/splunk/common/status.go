// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package common

import (
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PhaseAndConditions holds both the phase and derived conditions for a Splunk CR status update.
// This struct ensures that Phase and Conditions are always updated atomically.
type PhaseAndConditions struct {
	Phase      enterpriseApi.Phase
	Conditions []metav1.Condition
}

// PhaseConditionInput carries the desired-state inputs for SetPhaseAndConditions.
type PhaseConditionInput struct {
	Phase      enterpriseApi.Phase
	IsPaused   bool
	Message    string
	Generation int64
	IsStalled  bool
}

// SetPhaseAndConditions atomically sets Phase and Conditions derived from the given phase.
// If existingConditions is provided, LastTransitionTime is preserved when the condition status hasn't changed.
// If existingConditions is nil, new conditions are created with the current time.
func SetPhaseAndConditions(existingConditions []metav1.Condition, in PhaseConditionInput) PhaseAndConditions {
	conditions := deriveConditionsFromPhase(existingConditions, in)
	return PhaseAndConditions{
		Phase:      in.Phase,
		Conditions: conditions,
	}
}

// deriveConditionsFromPhase derives Kubernetes-standard conditions from the given Phase.
// If existingConditions is provided, LastTransitionTime is preserved when status hasn't changed.
// This ensures conditions are always consistent with the phase while maintaining proper transition tracking.
func deriveConditionsFromPhase(existingConditions []metav1.Condition, in PhaseConditionInput) []metav1.Condition {
	phase, isPaused, message, generation, isStalled := in.Phase, in.IsPaused, in.Message, in.Generation, in.IsStalled
	// Stalled is only meaningful when reconciliation has failed; clear it for all other phases
	// so that Ready=True and Stalled=True can never coexist.
	if phase != enterpriseApi.PhaseError {
		isStalled = false
	}
	now := metav1.NewTime(time.Now())
	conditions := make([]metav1.Condition, 0, 3)

	// Helper to get existing condition's LastTransitionTime if status matches
	getTransitionTime := func(condType string, newStatus metav1.ConditionStatus) metav1.Time {
		for _, c := range existingConditions {
			if c.Type == condType {
				if c.Status == newStatus {
					// Status unchanged - preserve original transition time
					return c.LastTransitionTime
				}
				// Status changed - use current time
				return now
			}
		}
		// Condition not found - this is a new condition
		return now
	}

	// Ready condition
	readyCondition := metav1.Condition{
		Type:               string(enterpriseApi.ConditionReady),
		ObservedGeneration: generation,
	}

	// Progressing condition
	progressingCondition := metav1.Condition{
		Type:               string(enterpriseApi.ConditionProgressing),
		ObservedGeneration: generation,
	}

	// Paused condition
	pausedCondition := metav1.Condition{
		Type:               string(enterpriseApi.ConditionPaused),
		ObservedGeneration: generation,
	}

	// Set Paused condition based on isPaused flag
	if isPaused {
		pausedCondition.Status = metav1.ConditionTrue
		pausedCondition.Reason = string(enterpriseApi.ReasonPausedByAnnotation)
		pausedCondition.Message = "Reconciliation is paused via annotation"
	} else {
		pausedCondition.Status = metav1.ConditionFalse
		pausedCondition.Reason = string(enterpriseApi.ReasonNotPaused)
		pausedCondition.Message = "Reconciliation is not paused"
	}
	pausedCondition.LastTransitionTime = getTransitionTime(pausedCondition.Type, pausedCondition.Status)

	// Stalled condition
	stalledCondition := metav1.Condition{
		Type:               string(enterpriseApi.ConditionStalled),
		ObservedGeneration: generation,
	}

	// Set Stalled condition based on isStalled flag
	if isStalled {
		stalledCondition.Status = metav1.ConditionTrue
		stalledCondition.Reason = string(enterpriseApi.ReasonStalled)
	} else {
		stalledCondition.Status = metav1.ConditionFalse
		stalledCondition.Reason = string(enterpriseApi.ReasonNotStalled)
		stalledCondition.Message = ""
	}
	stalledCondition.LastTransitionTime = getTransitionTime(stalledCondition.Type, stalledCondition.Status)

	// Derive Ready and Progressing conditions from Phase
	switch phase {
	case enterpriseApi.PhaseReady:
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = string(enterpriseApi.ReasonAllReplicasReady)
		readyCondition.Message = "All replicas are ready"
		if message != "" {
			readyCondition.Message = message
		}
		readyCondition.LastTransitionTime = getTransitionTime(readyCondition.Type, readyCondition.Status)

		progressingCondition.Status = metav1.ConditionFalse
		progressingCondition.Reason = string(enterpriseApi.ReasonStable)
		progressingCondition.Message = "Resource is stable"
		progressingCondition.LastTransitionTime = getTransitionTime(progressingCondition.Type, progressingCondition.Status)
	case enterpriseApi.PhasePending:
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = string(enterpriseApi.ReasonReplicasNotReady)
		readyCondition.Message = "Resource is pending initialization"
		if message != "" {
			readyCondition.Message = message
		}
		readyCondition.LastTransitionTime = getTransitionTime(readyCondition.Type, readyCondition.Status)

		progressingCondition.Status = metav1.ConditionTrue
		progressingCondition.Reason = string(enterpriseApi.ReasonScaling)
		progressingCondition.Message = "Resource is being initialized"
		progressingCondition.LastTransitionTime = getTransitionTime(progressingCondition.Type, progressingCondition.Status)

	case enterpriseApi.PhaseUpdating:
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = string(enterpriseApi.ReasonReplicasNotReady)
		readyCondition.Message = "Resource is being updated"
		if message != "" {
			readyCondition.Message = message
		}
		readyCondition.LastTransitionTime = getTransitionTime(readyCondition.Type, readyCondition.Status)

		progressingCondition.Status = metav1.ConditionTrue
		progressingCondition.Reason = string(enterpriseApi.ReasonUpgrading)
		progressingCondition.Message = "Resource is being updated"
		progressingCondition.LastTransitionTime = getTransitionTime(progressingCondition.Type, progressingCondition.Status)

	case enterpriseApi.PhaseScalingUp, enterpriseApi.PhaseScalingDown:
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = string(enterpriseApi.ReasonReplicasNotReady)
		if phase == enterpriseApi.PhaseScalingUp {
			readyCondition.Message = "Resource is scaling up"
		} else {
			readyCondition.Message = "Resource is scaling down"
		}
		if message != "" {
			readyCondition.Message = message
		}
		readyCondition.LastTransitionTime = getTransitionTime(readyCondition.Type, readyCondition.Status)

		progressingCondition.Status = metav1.ConditionTrue
		progressingCondition.Reason = string(enterpriseApi.ReasonScaling)
		if phase == enterpriseApi.PhaseScalingUp {
			progressingCondition.Message = "Resource is scaling up"
		} else {
			progressingCondition.Message = "Resource is scaling down"
		}
		progressingCondition.LastTransitionTime = getTransitionTime(progressingCondition.Type, progressingCondition.Status)

	case enterpriseApi.PhaseTerminating:
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = string(enterpriseApi.ReasonReplicasNotReady)
		readyCondition.Message = "Resource is being terminated"
		if message != "" {
			readyCondition.Message = message
		}
		readyCondition.LastTransitionTime = getTransitionTime(readyCondition.Type, readyCondition.Status)

		progressingCondition.Status = metav1.ConditionTrue
		progressingCondition.Reason = string(enterpriseApi.ReasonScaling)
		progressingCondition.Message = "Resource is being terminated"
		progressingCondition.LastTransitionTime = getTransitionTime(progressingCondition.Type, progressingCondition.Status)

	case enterpriseApi.PhaseError:
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = string(enterpriseApi.ReasonReconcileFailed)
		readyCondition.Message = "Reconciliation failed"
		if message != "" {
			readyCondition.Message = message
		}
		readyCondition.LastTransitionTime = getTransitionTime(readyCondition.Type, readyCondition.Status)

		progressingCondition.Status = metav1.ConditionFalse
		progressingCondition.Reason = string(enterpriseApi.ReasonReconcileFailed)
		progressingCondition.Message = "Reconciliation failed"
		if message != "" {
			progressingCondition.Message = message
		}
		progressingCondition.LastTransitionTime = getTransitionTime(progressingCondition.Type, progressingCondition.Status)

		if isStalled {
			stalledCondition.Message = "Reconciliation failed"
			if message != "" {
				stalledCondition.Message = message
			}
		}
		stalledCondition.LastTransitionTime = getTransitionTime(stalledCondition.Type, stalledCondition.Status)

	default:
		// Unknown phase - treat as not ready, not progressing
		readyCondition.Status = metav1.ConditionUnknown
		readyCondition.Reason = "Unknown"
		readyCondition.Message = "Unknown phase"
		readyCondition.LastTransitionTime = getTransitionTime(readyCondition.Type, readyCondition.Status)

		progressingCondition.Status = metav1.ConditionUnknown
		progressingCondition.Reason = "Unknown"
		progressingCondition.Message = "Unknown phase"
		progressingCondition.LastTransitionTime = getTransitionTime(progressingCondition.Type, progressingCondition.Status)

		if isStalled {
			stalledCondition.Message = "Unknown phase"
			stalledCondition.LastTransitionTime = getTransitionTime(stalledCondition.Type, stalledCondition.Status)
		}
	}

	conditions = append(conditions, readyCondition, progressingCondition, pausedCondition, stalledCondition)
	return conditions
}

// UpsertCondition updates or adds a condition in the conditions slice.
// If a condition with the same type already exists, it is updated.
// If not, the new condition is appended.
func UpsertCondition(conditions []metav1.Condition, newCondition metav1.Condition) []metav1.Condition {
	for i, c := range conditions {
		if c.Type == newCondition.Type {
			// Only update LastTransitionTime if status changed
			if c.Status != newCondition.Status {
				newCondition.LastTransitionTime = metav1.NewTime(time.Now())
			} else {
				newCondition.LastTransitionTime = c.LastTransitionTime
			}
			conditions[i] = newCondition
			return conditions
		}
	}
	// Condition not found, append it
	newCondition.LastTransitionTime = metav1.NewTime(time.Now())
	return append(conditions, newCondition)
}

// GetCondition returns the condition with the given type from the conditions slice.
// Returns nil if the condition is not found.
func GetCondition(conditions []metav1.Condition, conditionType enterpriseApi.ConditionType) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == string(conditionType) {
			return &conditions[i]
		}
	}
	return nil
}

// IsConditionTrue returns true if the condition with the given type has status True.
func IsConditionTrue(conditions []metav1.Condition, conditionType enterpriseApi.ConditionType) bool {
	condition := GetCondition(conditions, conditionType)
	return condition != nil && condition.Status == metav1.ConditionTrue
}

// IsReady returns true if the Ready condition is True.
func IsReady(conditions []metav1.Condition) bool {
	return IsConditionTrue(conditions, enterpriseApi.ConditionReady)
}

// IsProgressing returns true if the Progressing condition is True.
func IsProgressing(conditions []metav1.Condition) bool {
	return IsConditionTrue(conditions, enterpriseApi.ConditionProgressing)
}

// IsPaused returns true if the Paused condition is True.
func IsPaused(conditions []metav1.Condition) bool {
	return IsConditionTrue(conditions, enterpriseApi.ConditionPaused)
}

// IsStalled returns true if the Stalled condition is True.
func IsStalled(conditions []metav1.Condition) bool {
	return IsConditionTrue(conditions, enterpriseApi.ConditionStalled)
}
