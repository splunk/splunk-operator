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

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/stretchr/testify/assert"
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
