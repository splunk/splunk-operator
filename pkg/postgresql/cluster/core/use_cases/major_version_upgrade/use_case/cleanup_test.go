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
	"context"
	"errors"
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationtypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	bluegreen "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/blue_green"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cleanupFunc func(context.Context, mvutypes.Intent, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bluegreen.Outcome, error)

func (f cleanupFunc) AdvanceCleanup(ctx context.Context, intent mvutypes.Intent) (bluegreen.Outcome, error) {
	return f(ctx, intent, intent.State[len(intent.State)-1].BlueGreen.DeepCopy())
}

func TestCleanupRunsWithoutPreparationAndPreservesProductionOutcome(t *testing.T) {
	for _, phase := range []mvutypes.Status{mvutypes.Completed, mvutypes.Cancelled} {
		for _, fails := range []bool{false, true} {
			name := string(phase)
			if fails {
				name += " transient failure"
			}
			t.Run(name, func(t *testing.T) {
				intent := mvutypes.Intent{Strategy: mvutypes.MajorUpgradeFlowBlueGreen, AttemptID: "attempt", SourcePgVersion: "17", TargetPgVersion: "18", RequiresBlueGreenCleanup: true}
				intent.State = []platformv1alpha1.PostgresMajorUpgradeStatus{{Strategy: &intent.Strategy, Phase: new(string(phase)), SourcePgVersion: &intent.SourcePgVersion, TargetPgVersion: &intent.TargetPgVersion, BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: intent.AttemptID}}}
				store := &blueGreenProgressStore{intent: intent}
				cause := errors.New("temporary API failure")
				calls := 0
				useCase := NewCleanupUseCase(store, cleanupFunc(func(ctx context.Context, got mvutypes.Intent, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bluegreen.Outcome, error) {
					calls++
					assert.Equal(t, intent.AttemptID, got.AttemptID)
					outcome := bluegreen.Outcome{Report: reconciliationtypes.Report{Phase: string(mvutypes.Failed), Retry: true}, BlueGreen: bg}
					if fails {
						return outcome, cause
					}
					return outcome, nil
				}), nil)
				require.NoError(t, useCase.Prerequisites(t.Context()))
				scheduled, err := useCase.Schedule(t.Context())
				require.NoError(t, err)
				require.True(t, scheduled)
				assert.Empty(t, useCase.BlocksComponents())
				report, err := useCase.Act(t.Context())
				if fails {
					require.ErrorIs(t, err, cause)
				} else {
					require.NoError(t, err)
				}
				assert.Equal(t, 1, calls)
				assert.Equal(t, string(phase), report.Phase)
				assert.Equal(t, string(phase), store.progress.Report.Phase)
				assert.True(t, report.Retry)
				require.NotNil(t, report.Sleep)
				assert.Equal(t, intent.AttemptID, store.progress.BlueGreen.AttemptID)
			})
		}
	}
}

func TestCleanupSelectionStaysDormantUntilRuntimeIsWired(t *testing.T) {
	useCase := NewCleanupUseCase(fakeInfoStore(mvutypes.Intent{RequiresBlueGreenCleanup: true}), nil, nil)
	scheduled, err := useCase.Schedule(t.Context())
	require.NoError(t, err)
	assert.False(t, scheduled)
	assert.Empty(t, useCase.BlocksComponents())
}

func TestCleanupEventsOnlyDescribeStateTransitions(t *testing.T) {
	intent := mvutypes.Intent{AttemptID: "attempt", Strategy: mvutypes.MajorUpgradeFlowBlueGreen, SourcePgVersion: "17", TargetPgVersion: "18", RequiresBlueGreenCleanup: true}
	intent.State = []platformv1alpha1.PostgresMajorUpgradeStatus{{SourcePgVersion: &intent.SourcePgVersion, TargetPgVersion: &intent.TargetPgVersion, Strategy: &intent.Strategy, BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt"}}}
	notifier := &fakeNotifier{}
	useCase := &CleanupUseCase{notifier: notifier}
	for _, state := range []platformv1alpha1.BlueGreenCleanupState{platformv1alpha1.BlueGreenCleanupStateCleaning, platformv1alpha1.BlueGreenCleanupStateCleanupFailed, platformv1alpha1.BlueGreenCleanupStateCleaned} {
		progress := mvutypes.Progress{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{Cleanup: &platformv1alpha1.BlueGreenCleanupStatus{State: state}}}
		useCase.emitCleanupEvent(intent, progress)
		intent.State[0].BlueGreen.Cleanup = progress.BlueGreen.Cleanup.DeepCopy()
		useCase.emitCleanupEvent(intent, progress)
	}
	assert.Equal(t, []string{"CleanupStarted", "CleanupCompleted"}, notifier.informs)
	assert.Equal(t, []string{"CleanupFailed"}, notifier.warnings)
}
