/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package majorversionupgradetypes

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/assert"
)

func TestNewBlueGreenCondition(t *testing.T) {
	condition := NewBlueGreenCondition(
		ConditionReadyForSwitchover,
		metav1.ConditionTrue,
		"CandidateHealthy",
		"candidate is ready",
	)

	assert.Equal(t, ConditionReadyForSwitchover, condition.Type)
	assert.Equal(t, metav1.ConditionTrue, condition.Status)
	assert.Equal(t, "CandidateHealthy", condition.Reason)
	assert.Equal(t, "candidate is ready", condition.Message)
	assert.False(t, condition.LastTransitionTime.IsZero())
}
