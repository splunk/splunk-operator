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

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PhaseAndConditions holds both the phase and derived conditions for a Splunk CR status update.
// This struct ensures that Phase and Conditions are always updated atomically.
type PhaseAndConditions struct {
	Phase      enterpriseApi.Phase
	Conditions []metav1.Condition
}

// SetPhaseAndConditions atomically sets both Phase and Conditions to ensure consistency.
// It derives the appropriate conditions from the given phase and paused state.
// The message parameter is used to provide additional context in the condition messages.
func SetPhaseAndConditions(phase enterpriseApi.Phase, isPaused bool, message string) PhaseAndConditions {
	conditions := deriveConditionsFromPhase(phase, isPaused, message)
	return PhaseAndConditions{
		Phase:      phase,
		Conditions: conditions,
	}
}

// deriveConditionsFromPhase derives Kubernetes-standard conditions from the given Phase.
// This ensures conditions are always consistent with the phase.
func deriveConditionsFromPhase(phase enterpriseApi.Phase, isPaused bool, message string) []metav1.Condition {
	now := metav1.NewTime(time.Now())
	conditions := make([]metav1.Condition, 0, 3)

	// Ready condition
	readyCondition := metav1.Condition{
		Type:               string(enterpriseApi.ConditionReady),
		LastTransitionTime: now,
		ObservedGeneration: 0, // Will be set by the caller if needed
	}

	// Progressing condition
	progressingCondition := metav1.Condition{
		Type:               string(enterpriseApi.ConditionProgressing),
		LastTransitionTime: now,
		ObservedGeneration: 0,
	}

	// Paused condition
	pausedCondition := metav1.Condition{
		Type:               string(enterpriseApi.ConditionPaused),
		LastTransitionTime: now,
		ObservedGeneration: 0,
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

	// Derive Ready and Progressing conditions from Phase
	switch phase {
	case enterpriseApi.PhaseReady:
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = string(enterpriseApi.ReasonAllReplicasReady)
		readyCondition.Message = "All replicas are ready"
		if message != "" {
			readyCondition.Message = message
		}

		progressingCondition.Status = metav1.ConditionFalse
		progressingCondition.Reason = string(enterpriseApi.ReasonStable)
		progressingCondition.Message = "Resource is stable"

	case enterpriseApi.PhasePending:
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = string(enterpriseApi.ReasonPending)
		readyCondition.Message = "Resource is pending initialization"
		if message != "" {
			readyCondition.Message = message
		}

		progressingCondition.Status = metav1.ConditionTrue
		progressingCondition.Reason = string(enterpriseApi.ReasonUpdating)
		progressingCondition.Message = "Resource is being initialized"

	case enterpriseApi.PhaseUpdating:
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = string(enterpriseApi.ReasonReplicasNotReady)
		readyCondition.Message = "Resource is updating"
		if message != "" {
			readyCondition.Message = message
		}

		progressingCondition.Status = metav1.ConditionTrue
		progressingCondition.Reason = string(enterpriseApi.ReasonUpdating)
		progressingCondition.Message = "Resource is being updated"

	case enterpriseApi.PhaseScalingUp:
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = string(enterpriseApi.ReasonReplicasNotReady)
		readyCondition.Message = "Resource is scaling up"
		if message != "" {
			readyCondition.Message = message
		}

		progressingCondition.Status = metav1.ConditionTrue
		progressingCondition.Reason = string(enterpriseApi.ReasonScalingUp)
		progressingCondition.Message = "Resource is scaling up"

	case enterpriseApi.PhaseScalingDown:
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = string(enterpriseApi.ReasonReplicasNotReady)
		readyCondition.Message = "Resource is scaling down"
		if message != "" {
			readyCondition.Message = message
		}

		progressingCondition.Status = metav1.ConditionTrue
		progressingCondition.Reason = string(enterpriseApi.ReasonScalingDown)
		progressingCondition.Message = "Resource is scaling down"

	case enterpriseApi.PhaseTerminating:
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = string(enterpriseApi.ReasonTerminating)
		readyCondition.Message = "Resource is being terminated"
		if message != "" {
			readyCondition.Message = message
		}

		progressingCondition.Status = metav1.ConditionTrue
		progressingCondition.Reason = string(enterpriseApi.ReasonTerminating)
		progressingCondition.Message = "Resource is being terminated"

	case enterpriseApi.PhaseError:
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = string(enterpriseApi.ReasonReconcileFailed)
		readyCondition.Message = "Reconciliation failed"
		if message != "" {
			readyCondition.Message = message
		}

		progressingCondition.Status = metav1.ConditionFalse
		progressingCondition.Reason = string(enterpriseApi.ReasonReconcileFailed)
		progressingCondition.Message = "Reconciliation failed"

	default:
		// Unknown phase - treat as not ready, not progressing
		readyCondition.Status = metav1.ConditionUnknown
		readyCondition.Reason = "Unknown"
		readyCondition.Message = "Unknown phase"

		progressingCondition.Status = metav1.ConditionUnknown
		progressingCondition.Reason = "Unknown"
		progressingCondition.Message = "Unknown phase"
	}

	conditions = append(conditions, readyCondition, progressingCondition, pausedCondition)
	return conditions
}

// UpdateCondition updates or adds a condition in the conditions slice.
// If a condition with the same type already exists, it is updated.
// If not, the new condition is appended.
func UpdateCondition(conditions []metav1.Condition, newCondition metav1.Condition) []metav1.Condition {
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

// IsConditionFalse returns true if the condition with the given type has status False.
func IsConditionFalse(conditions []metav1.Condition, conditionType enterpriseApi.ConditionType) bool {
	condition := GetCondition(conditions, conditionType)
	return condition != nil && condition.Status == metav1.ConditionFalse
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
