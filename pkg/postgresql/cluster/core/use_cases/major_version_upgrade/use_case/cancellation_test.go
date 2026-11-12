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
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	bluegreen "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/blue_green"
	pgupgradeflow "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/pg_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// completingCanceller completes every step and records the step names.
type completingCanceller struct{ calls []string }

func (c *completingCanceller) CommitObserved(context.Context, string) (bool, error) {
	c.calls = append(c.calls, "observe")
	return false, nil
}
func (c *completingCanceller) RestoreSource(context.Context, mvutypes.Intent, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	c.calls = append(c.calls, "restore")
	return true, nil
}
func (c *completingCanceller) RemoveReplication(context.Context, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	c.calls = append(c.calls, "replication")
	return true, nil
}
func (c *completingCanceller) RemoveCredentials(context.Context, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	c.calls = append(c.calls, "credentials")
	return true, nil
}
func (c *completingCanceller) FenceCandidate(context.Context, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	c.calls = append(c.calls, "fence")
	return true, nil
}

func blueGreenEntryIntent(phase mvutypes.Status, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) mvutypes.Intent {
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	status.AttemptID = "attempt"
	return mvutypes.Intent{Strategy: strategy, SourcePgVersion: "17", TargetPgVersion: "18", AttemptID: "attempt", CancelRequested: true,
		State: []platformv1alpha1.PostgresMajorUpgradeStatus{{Strategy: &strategy, SourcePgVersion: new("17"), TargetPgVersion: new("18"),
			Phase: new(string(phase)), BlueGreen: status}}}
}

func TestCancelledAttemptIsTerminal(t *testing.T) {
	now := metav1.Now()
	intent := blueGreenEntryIntent(mvutypes.Cancelled, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		Gates: &platformv1alpha1.BlueGreenGateStatus{Cancellation: &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: &now, CompletedAt: &now}}})
	useCase := newTestUseCase(intent)
	scheduled, err := useCase.Schedule(t.Context())
	require.NoError(t, err)
	assert.False(t, scheduled)
	assert.Empty(t, useCase.BlocksComponents(), "blue's ordinary reconciliation resumes after cancellation")
}

func TestCancelRequestActivatesTerminalPrecommitFailure(t *testing.T) {
	intent := blueGreenEntryIntent(mvutypes.Failed, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{RebuildRequired: new("slot invalidated")})
	useCase := newTestUseCase(intent)
	scheduled, err := useCase.Schedule(t.Context())
	require.NoError(t, err)
	assert.True(t, scheduled, "a terminal failure can still be cancelled")
	assert.Equal(t, mvutypes.Cancelling, phaseForIntent(intent.State, intent))

	intent.CancelRequested = false
	scheduled, err = newTestUseCase(intent).Schedule(t.Context())
	require.NoError(t, err)
	assert.False(t, scheduled)
}

func TestCancellationBypassesBackupAndReadinessPrerequisites(t *testing.T) {
	intent := blueGreenEntryIntent(mvutypes.PreUpgradeBackup, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{})
	store := &blueGreenProgressStore{intent: intent}
	canceller := &completingCanceller{}
	backup := &fakeBackupProvider{}
	useCase := NewMajorUpgradeUseCase(readinessStore{majorUpgradeInfoStoreFunc: fakeInfoStore(intent), ready: false}, backup, nil,
		pgupgradeflow.NoopNotifier(), &fakeBlueGreenPreparer{}, nil, nil).WithBlueGreenCanceller(canceller)
	require.NoError(t, useCase.Prerequisites(t.Context()), "an unready source must not block cancellation")

	useCase = NewMajorUpgradeUseCase(store, backup, nil, pgupgradeflow.NoopNotifier(), &fakeBlueGreenPreparer{}, nil, nil).WithBlueGreenCanceller(canceller)
	scheduled, err := useCase.Schedule(t.Context())
	require.NoError(t, err)
	require.True(t, scheduled)
	report, err := useCase.Act(t.Context())
	require.NoError(t, err)
	assert.Equal(t, string(mvutypes.Cancelling), report.Phase)
	assert.Nil(t, store.progress.Baseline, "cancellation must not create a pre-upgrade backup")
	assert.True(t, mvutypes.BlueGreenCancellationRequested(store.progress.BlueGreen))
}

func TestCommittedAttemptIgnoresCancelRequest(t *testing.T) {
	intent := blueGreenEntryIntent(mvutypes.SwitchingOver, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		CommitReceipt: &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1}})
	assert.False(t, mvutypes.BlueGreenCancellationApplies(intent))
	intent.State[0].Phase = new(string(mvutypes.Completed))
	assert.False(t, mvutypes.BlueGreenCancellationApplies(intent))
}

// TestCancellationSurvivesLostStatusWrites replays every pass with its status
// write lost, then with it persisted, and checks the order of durable steps.
func TestCancellationSurvivesLostStatusWrites(t *testing.T) {
	now := metav1.Now()
	intent := blueGreenEntryIntent(mvutypes.SwitchingOver, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		SourcePreparation: &platformv1alpha1.BlueGreenSourcePreparationStatus{CompletedAt: &now},
		Gates:             &platformv1alpha1.BlueGreenGateStatus{Switchover: &platformv1alpha1.BlueGreenGateConsumptionStatus{Generation: 2, ConsumedAt: &now}},
		Switchover:        &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1, StartedAt: &now},
	})
	canceller := &completingCanceller{}
	flow := bluegreen.New(mvutypes.SwitchingOver, nil, nil, nil).WithCanceller(canceller)
	var phases []string
	for range 6 {
		_, err := flow.Advance(t.Context(), intent) // lost write: the next pass sees the old state
		require.NoError(t, err)
		outcome, err := flow.Advance(t.Context(), intent)
		require.NoError(t, err)
		phases = append(phases, outcome.Report.Phase)
		entry := *intent.State[0].DeepCopy()
		entry.Phase, entry.BlueGreen = new(outcome.Report.Phase), outcome.BlueGreen.DeepCopy()
		intent.State = []platformv1alpha1.PostgresMajorUpgradeStatus{entry}
	}
	assert.Equal(t, []string{"Cancelling", "Cancelling", "Cancelling", "Cancelling", "Cancelling", "Cancelled"}, phases)
	assert.Equal(t, []string{
		"observe", "observe", // replay the request latch
		"observe", "restore", "observe", "restore",
		"observe", "replication", "observe", "replication",
		"observe", "credentials", "observe", "credentials",
		"observe", "fence", "observe", "fence",
		"observe", "observe", // replay completion
	}, canceller.calls,
		"a lost write repeats only the idempotent step it reported")
}
