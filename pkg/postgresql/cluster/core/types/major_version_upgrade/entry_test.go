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
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHasActiveBlueGreenAttemptRequiresSourceOwnership(t *testing.T) {
	placeholder := platformv1alpha1.PostgresMajorUpgradeStatus{
		BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "scheduled-only"},
	}
	owned := platformv1alpha1.PostgresMajorUpgradeStatus{
		BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
			AttemptID: "source-owned",
			Blue:      &platformv1alpha1.BlueGreenEnvironmentStatus{},
		},
	}

	assert.False(t, HasActiveBlueGreenAttempt([]platformv1alpha1.PostgresMajorUpgradeStatus{placeholder}))
	assert.True(t, HasActiveBlueGreenAttempt([]platformv1alpha1.PostgresMajorUpgradeStatus{placeholder, owned}))
}

func TestRetryMustBeNewerThanConsumedValue(t *testing.T) {
	consumed := time.Date(2026, 11, 12, 12, 0, 5, 123, time.UTC)
	for _, test := range []struct {
		name    string
		request time.Time
		receipt string
		allowed bool
	}{
		{"same value", consumed, consumed.Format(time.RFC3339Nano), false},
		{"same instant in another zone", consumed.In(time.FixedZone("ahead", 3600)), consumed.Format(time.RFC3339Nano), false},
		{"older value", consumed.Add(-time.Nanosecond), consumed.Format(time.RFC3339Nano), false},
		{"newer value", consumed.Add(time.Nanosecond), consumed.Format(time.RFC3339Nano), true},
		{"unreadable receipt", consumed.Add(time.Second), "malformed", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			entry := platformv1alpha1.PostgresMajorUpgradeStatus{Conditions: []metav1.Condition{
				{Type: ConditionMajorUpgradeTerminalFailure, LastTransitionTime: metav1.NewTime(consumed.Add(-time.Minute))},
				{Type: ConditionMajorUpgradeRetryConsumed, Status: metav1.ConditionTrue, Message: test.receipt},
			}}
			request := metav1.NewTime(test.request)
			assert.Equal(t, test.allowed, RetryRequestedAfterTerminalFailure(&request, entry))
		})
	}
}
