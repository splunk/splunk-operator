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

package majorupgradeadapter

import (
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCleanupIntentSurvivesMatchingMajorsAndMissingProvider(t *testing.T) {
	for _, phase := range []mvutypes.Status{mvutypes.Completed, mvutypes.Cancelled} {
		t.Run(string(phase), func(t *testing.T) {
			c, parent, store := initialAttemptStoreFixture(t)
			parent.Spec.PostgresMajorUpgradeConfig.Allow = new(false)
			parent.Spec.PostgresMajorUpgradeConfig.BlueGreen = &platformv1alpha1.PostgresBlueGreenUpgradeConfig{Cleanup: new(true)}
			require.NoError(t, c.Update(t.Context(), parent))
			parent.Status.CurrentPgVersion = "18"
			parent.Status.ProvisionerRef = &corev1.ObjectReference{Name: "vanished", UID: "vanished-uid"}
			parent.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{
				Phase: new(string(phase)), Strategy: new(mvutypes.MajorUpgradeFlowBlueGreen), SourcePgVersion: new("17"), TargetPgVersion: new("18"),
				BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "terminal-attempt"},
			}}
			require.NoError(t, c.Status().Update(t.Context(), parent))
			intent, enabled, err := store.ReadMajorUpgradeIntent(t.Context())
			require.NoError(t, err)
			require.True(t, enabled)
			assert.True(t, intent.RequiresBlueGreenCleanup)
			assert.Equal(t, "17", intent.SourcePgVersion)
			assert.Equal(t, "18", intent.TargetPgVersion)
			assert.Equal(t, "terminal-attempt", intent.AttemptID)
			assert.Equal(t, parent.Generation, intent.Generation)

			now := metav1.Now()
			parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Gates = &platformv1alpha1.BlueGreenGateStatus{Cleanup: &platformv1alpha1.BlueGreenGateConsumptionStatus{Generation: parent.Generation, ConsumedAt: &now}}
			require.NoError(t, c.Status().Update(t.Context(), parent))
			parent.Spec.PostgresMajorUpgradeConfig = nil
			parent.Spec.PostgresVersion = nil
			require.NoError(t, c.Update(t.Context(), parent))
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
			resumed, enabled, err := store.ReadMajorUpgradeIntent(t.Context())
			require.NoError(t, err)
			require.True(t, enabled)
			assert.True(t, resumed.RequiresBlueGreenCleanup)
			assert.Equal(t, intent.AttemptID, resumed.AttemptID)
			assert.Equal(t, intent.SourcePgVersion, resumed.SourcePgVersion)
		})
	}
}

func TestCleanupPermissionIsRecheckedBeforeConsumption(t *testing.T) {
	for _, change := range []string{"revoked", "generation changed"} {
		t.Run(change, func(t *testing.T) {
			c, parent, store := initialAttemptStoreFixture(t)
			parent.Spec.PostgresMajorUpgradeConfig.BlueGreen = &platformv1alpha1.PostgresBlueGreenUpgradeConfig{Cleanup: new(true)}
			require.NoError(t, c.Update(t.Context(), parent))
			now := metav1.Now()
			bg := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "cancelled", Gates: &platformv1alpha1.BlueGreenGateStatus{
				Cancellation: &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: &now, SourceAccessRestoredAt: &now, ReplicationRemovedAt: &now, CredentialsRemovedAt: &now, GreenFencedAt: &now, CompletedAt: &now},
			}}
			parent.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{Phase: new("Cancelled"), Strategy: new(mvutypes.MajorUpgradeFlowBlueGreen), SourcePgVersion: new("17"), TargetPgVersion: new("18"), BlueGreen: bg}}
			require.NoError(t, c.Status().Update(t.Context(), parent))
			intent, enabled, err := store.ReadMajorUpgradeIntent(t.Context())
			require.NoError(t, err)
			require.True(t, enabled)
			bg = intent.State[0].BlueGreen.DeepCopy()
			bg.Gates.Cleanup = &platformv1alpha1.BlueGreenGateConsumptionStatus{Generation: intent.Generation, ConsumedAt: &now}
			bg.Cleanup = &platformv1alpha1.BlueGreenCleanupStatus{State: platformv1alpha1.BlueGreenCleanupStateCleaning, StartedAt: &now, RetirementGeneration: new(int64(1))}
			progress := mvutypes.Progress{BlueGreen: bg}
			progress.Report.Phase = string(mvutypes.Cancelled)
			if change == "revoked" {
				parent.Spec.PostgresMajorUpgradeConfig.BlueGreen.Cleanup = new(false)
			} else {
				parent.Generation++
			}
			require.NoError(t, c.Update(t.Context(), parent))
			_, err = store.SaveMajorUpgradeProgress(t.Context(), intent, progress)
			require.ErrorContains(t, err, "permission changed")
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
			assert.Nil(t, parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Gates.Cleanup)
		})
	}
}
