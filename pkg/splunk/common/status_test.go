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
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSetPhaseAndConditions_AllPhases(t *testing.T) {
	generation := int64(1)
	tests := []struct {
		name           string
		phase          enterpriseApi.Phase
		isPaused       bool
		message        string
		expectedReady  metav1.ConditionStatus
		expectedReason string
	}{
		{
			name:           "PhaseReady",
			phase:          enterpriseApi.PhaseReady,
			isPaused:       false,
			message:        "",
			expectedReady:  metav1.ConditionTrue,
			expectedReason: string(enterpriseApi.ReasonAllReplicasReady),
		},
		{
			name:           "PhasePending",
			phase:          enterpriseApi.PhasePending,
			isPaused:       false,
			message:        "",
			expectedReady:  metav1.ConditionFalse,
			expectedReason: string(enterpriseApi.ReasonReplicasNotReady),
		},
		{
			name:           "PhaseUpdating",
			phase:          enterpriseApi.PhaseUpdating,
			isPaused:       false,
			message:        "",
			expectedReady:  metav1.ConditionFalse,
			expectedReason: string(enterpriseApi.ReasonReplicasNotReady),
		},
		{
			name:           "PhaseScalingUp",
			phase:          enterpriseApi.PhaseScalingUp,
			isPaused:       false,
			message:        "",
			expectedReady:  metav1.ConditionFalse,
			expectedReason: string(enterpriseApi.ReasonReplicasNotReady),
		},
		{
			name:           "PhaseScalingDown",
			phase:          enterpriseApi.PhaseScalingDown,
			isPaused:       false,
			message:        "",
			expectedReady:  metav1.ConditionFalse,
			expectedReason: string(enterpriseApi.ReasonReplicasNotReady),
		},
		{
			name:           "PhaseTerminating",
			phase:          enterpriseApi.PhaseTerminating,
			isPaused:       false,
			message:        "",
			expectedReady:  metav1.ConditionFalse,
			expectedReason: string(enterpriseApi.ReasonReplicasNotReady),
		},
		{
			name:           "PhaseError",
			phase:          enterpriseApi.PhaseError,
			isPaused:       false,
			message:        "",
			expectedReady:  metav1.ConditionFalse,
			expectedReason: string(enterpriseApi.ReasonReconcileFailed),
		},
		{
			name:           "PhaseReady with custom message",
			phase:          enterpriseApi.PhaseReady,
			isPaused:       false,
			message:        "Custom ready message",
			expectedReady:  metav1.ConditionTrue,
			expectedReason: string(enterpriseApi.ReasonAllReplicasReady),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SetPhaseAndConditions(nil, tt.phase, tt.isPaused, tt.message, generation)

			if result.Phase != tt.phase {
				t.Errorf("SetPhaseAndConditions() Phase = %v, want %v", result.Phase, tt.phase)
			}

			if len(result.Conditions) != 3 {
				t.Errorf("SetPhaseAndConditions() Conditions count = %v, want 3", len(result.Conditions))
			}

			readyCondition := GetCondition(result.Conditions, enterpriseApi.ConditionReady)
			if readyCondition == nil {
				t.Errorf("SetPhaseAndConditions() Ready condition not found")
				return
			}

			if readyCondition.Status != tt.expectedReady {
				t.Errorf("SetPhaseAndConditions() Ready.Status = %v, want %v", readyCondition.Status, tt.expectedReady)
			}

			if readyCondition.Reason != tt.expectedReason {
				t.Errorf("SetPhaseAndConditions() Ready.Reason = %v, want %v", readyCondition.Reason, tt.expectedReason)
			}

			if tt.message != "" && readyCondition.Message != tt.message {
				t.Errorf("SetPhaseAndConditions() Ready.Message = %v, want %v", readyCondition.Message, tt.message)
			}

			if readyCondition.ObservedGeneration != generation {
				t.Errorf("SetPhaseAndConditions() ObservedGeneration = %v, want %v", readyCondition.ObservedGeneration, generation)
			}
		})
	}
}

func TestSetPhaseAndConditions_Paused(t *testing.T) {
	result := SetPhaseAndConditions(nil, enterpriseApi.PhaseReady, true, "", 1)

	pausedCondition := GetCondition(result.Conditions, enterpriseApi.ConditionPaused)
	if pausedCondition == nil {
		t.Errorf("SetPhaseAndConditions() Paused condition not found")
		return
	}

	if pausedCondition.Status != metav1.ConditionTrue {
		t.Errorf("SetPhaseAndConditions() Paused.Status = %v, want %v", pausedCondition.Status, metav1.ConditionTrue)
	}

	if pausedCondition.Reason != string(enterpriseApi.ReasonPausedByAnnotation) {
		t.Errorf("SetPhaseAndConditions() Paused.Reason = %v, want %v", pausedCondition.Reason, enterpriseApi.ReasonPausedByAnnotation)
	}
}

func TestSetPhaseAndConditions_NotPaused(t *testing.T) {
	result := SetPhaseAndConditions(nil, enterpriseApi.PhaseReady, false, "", 1)

	pausedCondition := GetCondition(result.Conditions, enterpriseApi.ConditionPaused)
	if pausedCondition == nil {
		t.Errorf("SetPhaseAndConditions() Paused condition not found")
		return
	}

	if pausedCondition.Status != metav1.ConditionFalse {
		t.Errorf("SetPhaseAndConditions() Paused.Status = %v, want %v", pausedCondition.Status, metav1.ConditionFalse)
	}

	if pausedCondition.Reason != string(enterpriseApi.ReasonNotPaused) {
		t.Errorf("SetPhaseAndConditions() Paused.Reason = %v, want %v", pausedCondition.Reason, enterpriseApi.ReasonNotPaused)
	}
}

func TestSetPhaseAndConditions_Progressing(t *testing.T) {
	tests := []struct {
		name                string
		phase               enterpriseApi.Phase
		expectedProgressing metav1.ConditionStatus
		expectedReason      string
	}{
		{
			name:                "PhaseReady - not progressing",
			phase:               enterpriseApi.PhaseReady,
			expectedProgressing: metav1.ConditionFalse,
			expectedReason:      string(enterpriseApi.ReasonStable),
		},
		{
			name:                "PhasePending - progressing",
			phase:               enterpriseApi.PhasePending,
			expectedProgressing: metav1.ConditionTrue,
			expectedReason:      string(enterpriseApi.ReasonScaling),
		},
		{
			name:                "PhaseUpdating - progressing",
			phase:               enterpriseApi.PhaseUpdating,
			expectedProgressing: metav1.ConditionTrue,
			expectedReason:      string(enterpriseApi.ReasonUpgrading),
		},
		{
			name:                "PhaseScalingUp - progressing",
			phase:               enterpriseApi.PhaseScalingUp,
			expectedProgressing: metav1.ConditionTrue,
			expectedReason:      string(enterpriseApi.ReasonScaling),
		},
		{
			name:                "PhaseScalingDown - progressing",
			phase:               enterpriseApi.PhaseScalingDown,
			expectedProgressing: metav1.ConditionTrue,
			expectedReason:      string(enterpriseApi.ReasonScaling),
		},
		{
			name:                "PhaseTerminating - progressing",
			phase:               enterpriseApi.PhaseTerminating,
			expectedProgressing: metav1.ConditionTrue,
			expectedReason:      string(enterpriseApi.ReasonScaling),
		},
		{
			name:                "PhaseError - not progressing",
			phase:               enterpriseApi.PhaseError,
			expectedProgressing: metav1.ConditionFalse,
			expectedReason:      string(enterpriseApi.ReasonReconcileFailed),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SetPhaseAndConditions(nil, tt.phase, false, "", 1)

			progressingCondition := GetCondition(result.Conditions, enterpriseApi.ConditionProgressing)
			if progressingCondition == nil {
				t.Errorf("SetPhaseAndConditions() Progressing condition not found")
				return
			}

			if progressingCondition.Status != tt.expectedProgressing {
				t.Errorf("SetPhaseAndConditions() Progressing.Status = %v, want %v", progressingCondition.Status, tt.expectedProgressing)
			}

			if progressingCondition.Reason != tt.expectedReason {
				t.Errorf("SetPhaseAndConditions() Progressing.Reason = %v, want %v", progressingCondition.Reason, tt.expectedReason)
			}
		})
	}
}

func TestUpsertCondition(t *testing.T) {
	now := metav1.Now()
	existingConditions := []metav1.Condition{
		{
			Type:               string(enterpriseApi.ConditionReady),
			Status:             metav1.ConditionTrue,
			Reason:             string(enterpriseApi.ReasonAllReplicasReady),
			Message:            "All replicas are ready",
			LastTransitionTime: now,
		},
	}

	// Test updating existing condition
	newCondition := metav1.Condition{
		Type:    string(enterpriseApi.ConditionReady),
		Status:  metav1.ConditionFalse,
		Reason:  string(enterpriseApi.ReasonReplicasNotReady),
		Message: "Replicas not ready",
	}

	updatedConditions := UpsertCondition(existingConditions, newCondition)

	if len(updatedConditions) != 1 {
		t.Errorf("UpsertCondition() should not add new condition when updating existing, got %d conditions", len(updatedConditions))
	}

	readyCondition := GetCondition(updatedConditions, enterpriseApi.ConditionReady)
	if readyCondition.Status != metav1.ConditionFalse {
		t.Errorf("UpsertCondition() Status = %v, want %v", readyCondition.Status, metav1.ConditionFalse)
	}

	// Test adding new condition
	progressingCondition := metav1.Condition{
		Type:    string(enterpriseApi.ConditionProgressing),
		Status:  metav1.ConditionTrue,
		Reason:  string(enterpriseApi.ReasonUpgrading),
		Message: "Updating",
	}

	updatedConditions = UpsertCondition(updatedConditions, progressingCondition)

	if len(updatedConditions) != 2 {
		t.Errorf("UpsertCondition() should add new condition, got %d conditions", len(updatedConditions))
	}
}

func TestGetCondition(t *testing.T) {
	conditions := []metav1.Condition{
		{
			Type:   string(enterpriseApi.ConditionReady),
			Status: metav1.ConditionTrue,
		},
		{
			Type:   string(enterpriseApi.ConditionProgressing),
			Status: metav1.ConditionFalse,
		},
	}

	// Test finding existing condition
	readyCondition := GetCondition(conditions, enterpriseApi.ConditionReady)
	if readyCondition == nil {
		t.Errorf("GetCondition() should find Ready condition")
	}

	// Test not finding non-existing condition
	pausedCondition := GetCondition(conditions, enterpriseApi.ConditionPaused)
	if pausedCondition != nil {
		t.Errorf("GetCondition() should not find Paused condition")
	}
}

func TestIsConditionTrue(t *testing.T) {
	conditions := []metav1.Condition{
		{
			Type:   string(enterpriseApi.ConditionReady),
			Status: metav1.ConditionTrue,
		},
		{
			Type:   string(enterpriseApi.ConditionProgressing),
			Status: metav1.ConditionFalse,
		},
	}

	if !IsConditionTrue(conditions, enterpriseApi.ConditionReady) {
		t.Errorf("IsConditionTrue() should return true for Ready condition")
	}

	if IsConditionTrue(conditions, enterpriseApi.ConditionProgressing) {
		t.Errorf("IsConditionTrue() should return false for Progressing condition")
	}

	if IsConditionTrue(conditions, enterpriseApi.ConditionPaused) {
		t.Errorf("IsConditionTrue() should return false for non-existing condition")
	}
}

func TestIsReady(t *testing.T) {
	readyConditions := []metav1.Condition{
		{
			Type:   string(enterpriseApi.ConditionReady),
			Status: metav1.ConditionTrue,
		},
	}

	notReadyConditions := []metav1.Condition{
		{
			Type:   string(enterpriseApi.ConditionReady),
			Status: metav1.ConditionFalse,
		},
	}

	if !IsReady(readyConditions) {
		t.Errorf("IsReady() should return true when Ready condition is True")
	}

	if IsReady(notReadyConditions) {
		t.Errorf("IsReady() should return false when Ready condition is False")
	}

	if IsReady([]metav1.Condition{}) {
		t.Errorf("IsReady() should return false when no conditions exist")
	}
}

func TestIsProgressing(t *testing.T) {
	progressingConditions := []metav1.Condition{
		{
			Type:   string(enterpriseApi.ConditionProgressing),
			Status: metav1.ConditionTrue,
		},
	}

	notProgressingConditions := []metav1.Condition{
		{
			Type:   string(enterpriseApi.ConditionProgressing),
			Status: metav1.ConditionFalse,
		},
	}

	if !IsProgressing(progressingConditions) {
		t.Errorf("IsProgressing() should return true when Progressing condition is True")
	}

	if IsProgressing(notProgressingConditions) {
		t.Errorf("IsProgressing() should return false when Progressing condition is False")
	}
}

func TestIsPaused(t *testing.T) {
	pausedConditions := []metav1.Condition{
		{
			Type:   string(enterpriseApi.ConditionPaused),
			Status: metav1.ConditionTrue,
		},
	}

	notPausedConditions := []metav1.Condition{
		{
			Type:   string(enterpriseApi.ConditionPaused),
			Status: metav1.ConditionFalse,
		},
	}

	if !IsPaused(pausedConditions) {
		t.Errorf("IsPaused() should return true when Paused condition is True")
	}

	if IsPaused(notPausedConditions) {
		t.Errorf("IsPaused() should return false when Paused condition is False")
	}
}

func TestSetPhaseAndConditions_TransitionTimePreservation(t *testing.T) {
	generation := int64(5)
	oldTime := metav1.NewTime(time.Now().Add(-1 * time.Hour))

	// Create existing conditions with old transition time
	existingConditions := []metav1.Condition{
		{
			Type:               string(enterpriseApi.ConditionReady),
			Status:             metav1.ConditionTrue,
			Reason:             string(enterpriseApi.ReasonAllReplicasReady),
			Message:            "All replicas are ready",
			LastTransitionTime: oldTime,
			ObservedGeneration: generation - 1,
		},
		{
			Type:               string(enterpriseApi.ConditionProgressing),
			Status:             metav1.ConditionFalse,
			Reason:             string(enterpriseApi.ReasonStable),
			Message:            "Resource is stable",
			LastTransitionTime: oldTime,
			ObservedGeneration: generation - 1,
		},
		{
			Type:               string(enterpriseApi.ConditionPaused),
			Status:             metav1.ConditionFalse,
			Reason:             string(enterpriseApi.ReasonNotPaused),
			Message:            "Reconciliation is not paused",
			LastTransitionTime: oldTime,
			ObservedGeneration: generation - 1,
		},
	}

	// Test 1: Status unchanged - LastTransitionTime should be preserved
	result := SetPhaseAndConditions(existingConditions, enterpriseApi.PhaseReady, false, "", generation)

	readyCondition := GetCondition(result.Conditions, enterpriseApi.ConditionReady)
	if readyCondition == nil {
		t.Fatal("Ready condition not found")
	}

	if !readyCondition.LastTransitionTime.Equal(&oldTime) {
		t.Errorf("LastTransitionTime should be preserved when status unchanged, got %v, want %v",
			readyCondition.LastTransitionTime, oldTime)
	}

	if readyCondition.ObservedGeneration != generation {
		t.Errorf("ObservedGeneration should be updated, got %v, want %v",
			readyCondition.ObservedGeneration, generation)
	}

	// Test 2: Status changed - LastTransitionTime should be updated
	result = SetPhaseAndConditions(existingConditions, enterpriseApi.PhaseError, false, "Error occurred", generation)

	readyCondition = GetCondition(result.Conditions, enterpriseApi.ConditionReady)
	if readyCondition == nil {
		t.Fatal("Ready condition not found after phase change")
	}

	if readyCondition.Status != metav1.ConditionFalse {
		t.Errorf("Ready status should be False for PhaseError, got %v", readyCondition.Status)
	}

	if readyCondition.LastTransitionTime.Equal(&oldTime) {
		t.Error("LastTransitionTime should be updated when status changes")
	}

	// Test 3: Paused status change - LastTransitionTime should be updated
	result = SetPhaseAndConditions(existingConditions, enterpriseApi.PhaseReady, true, "", generation)

	pausedCondition := GetCondition(result.Conditions, enterpriseApi.ConditionPaused)
	if pausedCondition == nil {
		t.Fatal("Paused condition not found")
	}

	if pausedCondition.Status != metav1.ConditionTrue {
		t.Errorf("Paused status should be True when isPaused=true, got %v", pausedCondition.Status)
	}

	if pausedCondition.LastTransitionTime.Equal(&oldTime) {
		t.Error("Paused LastTransitionTime should be updated when status changes from False to True")
	}

	// Test 4: Empty existing conditions - should create new conditions with current time
	result = SetPhaseAndConditions(nil, enterpriseApi.PhaseReady, false, "", generation)

	if len(result.Conditions) != 3 {
		t.Errorf("Should create 3 conditions, got %d", len(result.Conditions))
	}

	readyCondition = GetCondition(result.Conditions, enterpriseApi.ConditionReady)
	if readyCondition.LastTransitionTime.IsZero() {
		t.Error("LastTransitionTime should not be zero for new conditions")
	}
}
