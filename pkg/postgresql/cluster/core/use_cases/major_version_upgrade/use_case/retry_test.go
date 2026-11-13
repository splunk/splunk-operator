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

package majorversionupgrade

import (
	"testing"
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func failedBlueGreenIntent(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) mvutypes.Intent {
	intent := blueGreenEntryIntent(mvutypes.Failed, status)
	intent.CancelRequested = false
	failedAt := metav1.NewTime(time.Unix(1_000, 0))
	intent.State[0].Conditions = []metav1.Condition{{Type: mvutypes.ConditionMajorUpgradeTerminalFailure, Status: metav1.ConditionTrue, LastTransitionTime: failedAt}}
	intent.RetryRequestedAt = new(metav1.NewTime(failedAt.Add(time.Second)))
	return intent
}

func TestBlueGreenRetryResumesOnlyReusableAttempts(t *testing.T) {
	now := metav1.Now()
	for _, test := range []struct {
		name   string
		status *platformv1alpha1.PostgresBlueGreenUpgradeStatus
		retry  bool
	}{
		{"reusable", &platformv1alpha1.PostgresBlueGreenUpgradeStatus{}, true},
		{"rebuild required", &platformv1alpha1.PostgresBlueGreenUpgradeStatus{RebuildRequired: new("schema drift")}, false},
		{"cancellation latched", &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
			Gates: &platformv1alpha1.BlueGreenGateStatus{Cancellation: &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: &now}}}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			intent := failedBlueGreenIntent(test.status)
			scheduled, err := newTestUseCase(intent).Schedule(t.Context())
			require.NoError(t, err)
			assert.Equal(t, test.retry, scheduled)
			if test.name == "cancellation latched" {
				assert.Equal(t, mvutypes.Cancelling, phaseForIntent(intent.State, intent), "a latched cancellation resumes instead of retrying")
				return
			}
			expected := mvutypes.Failed
			if test.retry {
				expected = mvutypes.Scheduled
			}
			assert.Equal(t, expected, phaseForIntent(intent.State, intent))
		})
	}
}
