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
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// preparePausedStatus derives the status fields shared by v4 Splunk resources.
// A newly created, already-paused resource has no phase yet, so Pending is used
// as the schema-valid representation of work that has not started. The return
// value tells callers whether a status write is necessary.
func preparePausedStatus(
	phase *enterpriseApi.Phase,
	observedGeneration *int64,
	conditions *[]metav1.Condition,
	generation int64,
	isPaused bool,
) bool {
	desiredPhase := *phase
	if desiredPhase == "" {
		desiredPhase = enterpriseApi.PhasePending
	}
	desired := splcommon.SetPhaseAndConditions(*conditions, splcommon.PhaseConditionInput{
		Phase:      desiredPhase,
		IsPaused:   isPaused,
		Generation: generation,
	})

	changed := *phase != desired.Phase ||
		*observedGeneration != generation ||
		!apiequality.Semantic.DeepEqual(*conditions, desired.Conditions)
	if !changed {
		return false
	}

	*phase = desired.Phase
	*observedGeneration = generation
	*conditions = desired.Conditions
	return true
}
