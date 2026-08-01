// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package controller

import (
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPreparePausedStatusInitializesAndIsIdempotent(t *testing.T) {
	phase := enterpriseApi.Phase("")
	observedGeneration := int64(0)
	var conditions []metav1.Condition

	if !preparePausedStatus(&phase, &observedGeneration, &conditions, 3, true) {
		t.Fatal("preparePausedStatus() changed = false, want true for an empty status")
	}
	if phase != enterpriseApi.PhasePending {
		t.Fatalf("phase = %q, want %q", phase, enterpriseApi.PhasePending)
	}
	if observedGeneration != 3 {
		t.Fatalf("observedGeneration = %d, want 3", observedGeneration)
	}
	paused := meta.FindStatusCondition(conditions, string(enterpriseApi.ConditionPaused))
	if paused == nil || paused.Status != metav1.ConditionTrue {
		t.Fatalf("Paused condition = %#v, want True", paused)
	}
	progressing := meta.FindStatusCondition(conditions, string(enterpriseApi.ConditionProgressing))
	if progressing == nil || progressing.Status != metav1.ConditionFalse || progressing.Reason != string(enterpriseApi.ReasonPausedByAnnotation) {
		t.Fatalf("Progressing condition = %#v, want False/%s", progressing, enterpriseApi.ReasonPausedByAnnotation)
	}

	if preparePausedStatus(&phase, &observedGeneration, &conditions, 3, true) {
		t.Fatal("preparePausedStatus() changed = true, want false for an identical paused status")
	}

	if !preparePausedStatus(&phase, &observedGeneration, &conditions, 3, false) {
		t.Fatal("preparePausedStatus() changed = false, want true when unpausing")
	}
	if common.IsPaused(conditions) {
		t.Fatal("Paused condition remained True after unpausing")
	}
	if preparePausedStatus(&phase, &observedGeneration, &conditions, 3, false) {
		t.Fatal("preparePausedStatus() changed = true, want false for an identical unpaused status")
	}
}

func TestPreparePausedStatusPreservesExistingPhase(t *testing.T) {
	phase := enterpriseApi.PhaseReady
	observedGeneration := int64(5)
	conditions := common.SetPhaseAndConditions(nil, common.PhaseConditionInput{
		Phase:      phase,
		Generation: observedGeneration,
	}).Conditions

	if !preparePausedStatus(&phase, &observedGeneration, &conditions, 5, true) {
		t.Fatal("preparePausedStatus() changed = false, want true when pausing")
	}
	if phase != enterpriseApi.PhaseReady {
		t.Fatalf("phase = %q, want existing phase %q", phase, enterpriseApi.PhaseReady)
	}
	ready := meta.FindStatusCondition(conditions, string(enterpriseApi.ConditionReady))
	if ready == nil || ready.Status != metav1.ConditionTrue {
		t.Fatalf("Ready condition = %#v, want True", ready)
	}
}
