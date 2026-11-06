/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package endpointpublication

import (
	"math"
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/require"
)

func TestSuccessiveCommitsRetainSequenceAcrossCleanedAndPrunedHistory(t *testing.T) {
	for _, history := range []string{"cleaned summary", "pruned history"} {
		t.Run(history, func(t *testing.T) {
			first, store, key, _ := preparedRecoveryFixture(t)
			require.NoError(t, first.CommitCanonical(t.Context(), key, "attempt"))
			require.Equal(t, int64(1), store.parent.Status.BlueGreenCommitSequence)
			cleaned := *store.parent.Status.PostgresMajorUpgradeStatus[0].DeepCopy()
			cleaned.Phase = new(string(mvutypes.Completed))
			// Cleanup compacts the first attempt to an identity/cleanup summary,
			// omitting its detailed receipt. Older summaries can be pruned too.
			cleaned.BlueGreen = &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt", HistoryCompacted: true, Cleanup: &platformv1alpha1.BlueGreenCleanupStatus{State: platformv1alpha1.BlueGreenCleanupStateCleaned}}

			second, next, nextKey, _ := preparedRecoveryFixture(t)
			next.parent.Status.BlueGreenCommitSequence = store.parent.Status.BlueGreenCommitSequence
			currentBG(next).AttemptID = "second-attempt"
			next.children[0].Status.EndpointPreparation.AttemptID = "second-attempt"
			if history == "cleaned summary" {
				next.parent.Status.PostgresMajorUpgradeStatus = append([]platformv1alpha1.PostgresMajorUpgradeStatus{cleaned}, next.parent.Status.PostgresMajorUpgradeStatus...)
			}
			require.NoError(t, second.CommitCanonical(t.Context(), nextKey, "second-attempt"))
			require.Equal(t, int64(2), next.parent.Status.PostgresMajorUpgradeStatus[len(next.parent.Status.PostgresMajorUpgradeStatus)-1].BlueGreen.CommitReceipt.Sequence)
			require.Equal(t, int64(2), next.parent.Status.BlueGreenCommitSequence)
			require.NoError(t, second.CommitCanonical(t.Context(), nextKey, "second-attempt"))
			require.Equal(t, int64(2), next.parent.Status.BlueGreenCommitSequence, "retrying the same commit must not consume another sequence")
		})
	}
}

func TestCommitSequenceIncludesExistingReceiptsAndRejectsExhaustionBeforeCommit(t *testing.T) {
	t.Run("existing receipt", func(t *testing.T) {
		coordinator, store, key, _ := preparedRecoveryFixture(t)
		store.parent.Status.BlueGreenCommitSequence = 3
		store.parent.Status.PostgresMajorUpgradeStatus = append([]platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{CommitReceipt: &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 7}}}}, store.parent.Status.PostgresMajorUpgradeStatus...)
		require.NoError(t, coordinator.CommitCanonical(t.Context(), key, "attempt"))
		require.Equal(t, int64(8), store.parent.Status.PostgresMajorUpgradeStatus[len(store.parent.Status.PostgresMajorUpgradeStatus)-1].BlueGreen.CommitReceipt.Sequence)
		require.Equal(t, int64(8), store.parent.Status.BlueGreenCommitSequence)
	})
	t.Run("exhausted counter", func(t *testing.T) {
		coordinator, store, key, _ := preparedRecoveryFixture(t)
		store.parent.Status.BlueGreenCommitSequence = math.MaxInt64
		before := store.configMaps["cluster-config"].DeepCopy()
		require.ErrorContains(t, coordinator.CommitCanonical(t.Context(), key, "attempt"), "sequence is exhausted")
		require.Equal(t, before, store.configMaps["cluster-config"])
		require.Nil(t, currentBG(store).CommitReceipt)
	})
}

func TestCanonicalRecoveryRestoresCommitSequenceHighWaterMark(t *testing.T) {
	coordinator, store, key, _ := committedRecoveryFixture(t)
	store.parent.Status.BlueGreenCommitSequence = 0
	require.NoError(t, coordinator.RecoverCanonical(t.Context(), key, "attempt"))
	require.Equal(t, currentBG(store).CommitReceipt.Sequence, store.parent.Status.BlueGreenCommitSequence)
}
