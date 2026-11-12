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

package bluegreen

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type fakeCanceller struct {
	committed bool
	done      map[string]bool
	errs      map[string]error
	calls     []string
	run       func(context.Context, string) (bool, error)
}

func newFakeCanceller() *fakeCanceller {
	return &fakeCanceller{done: map[string]bool{"restore": true, "replication": true, "credentials": true, "fence": true}, errs: map[string]error{}}
}

func (c *fakeCanceller) step(ctx context.Context, name string) (bool, error) {
	c.calls = append(c.calls, name)
	if c.run != nil {
		return c.run(ctx, name)
	}
	return c.done[name], c.errs[name]
}

func (c *fakeCanceller) CommitObserved(context.Context, string) (bool, error) {
	c.calls = append(c.calls, "observe")
	return c.committed, c.errs["observe"]
}
func (c *fakeCanceller) RestoreSource(ctx context.Context, _ mvutypes.Intent, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	return c.step(ctx, "restore")
}
func (c *fakeCanceller) RemoveReplication(ctx context.Context, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	return c.step(ctx, "replication")
}
func (c *fakeCanceller) RemoveCredentials(ctx context.Context, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	return c.step(ctx, "credentials")
}
func (c *fakeCanceller) FenceCandidate(ctx context.Context, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	return c.step(ctx, "fence")
}

func cancellationIntent(phase mvutypes.Status, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) mvutypes.Intent {
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	intent := mvutypes.Intent{AttemptID: "attempt-1", Strategy: strategy, SourcePgVersion: "17", TargetPgVersion: "18", Generation: 3, CancelRequested: true}
	if status != nil {
		status.AttemptID = "attempt-1"
		intent.State = []platformv1alpha1.PostgresMajorUpgradeStatus{{Phase: new(string(phase)), Strategy: &strategy,
			SourcePgVersion: ptr("17"), TargetPgVersion: ptr("18"), BlueGreen: status}}
	}
	return intent
}

// persist records the outcome as the next pass's durable state.
func persist(intent mvutypes.Intent, outcome Outcome) mvutypes.Intent {
	next := intent
	entry := *intent.State[0].DeepCopy()
	entry.Phase = new(outcome.Report.Phase)
	entry.BlueGreen = outcome.BlueGreen.DeepCopy()
	next.State = []platformv1alpha1.PostgresMajorUpgradeStatus{entry}
	return next
}

func TestCancelTakesPriorityOverUnconsumedSwitchoverGate(t *testing.T) {
	intent := cancellationIntent(mvutypes.ReadyForSwitchover, readyCandidateStatus())
	intent.SwitchoverAuthorized = true
	candidate, switchover, canceller := &countingCandidate{}, &countingSwitchover{}, newFakeCanceller()

	outcome, err := New(mvutypes.ReadyForSwitchover, &countingPreparer{}, candidate, switchover).WithCanceller(canceller).Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.Equal(t, string(mvutypes.Cancelling), outcome.Report.Phase)
	require.True(t, mvutypes.BlueGreenCancellationRequested(outcome.BlueGreen))
	assert.True(t, outcome.Report.Retry)
	assert.Zero(t, candidate.calls)
	assert.Zero(t, switchover.calls)
	assert.True(t, outcome.BlueGreen.Gates.Switchover == nil, "cancellation must not consume the switchover gate")
}

func TestCancellationCompletesOneStepPerPassInOrder(t *testing.T) {
	intent := cancellationIntent(mvutypes.Upgrading, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{})
	canceller := newFakeCanceller()
	flow := New(mvutypes.Upgrading, &countingPreparer{}, &countingCandidate{}, nil).WithCanceller(canceller)

	var phases []string
	for range 6 {
		outcome, err := flow.Advance(t.Context(), intent)
		require.NoError(t, err)
		phases = append(phases, outcome.Report.Phase)
		intent = persist(intent, outcome)
		intent.CancelRequested = false // a latched cancellation ignores the cleared gate
	}
	assert.Equal(t, []string{"Cancelling", "Cancelling", "Cancelling", "Cancelling", "Cancelling", "Cancelled"}, phases)
	assert.Equal(t, []string{"observe", "observe", "restore", "observe", "replication", "observe", "credentials", "observe", "fence", "observe"}, canceller.calls, "no completed step repeats")
	cancellation := intent.State[0].BlueGreen.Gates.Cancellation
	for name, receipt := range map[string]*metav1.Time{"restored": cancellation.SourceAccessRestoredAt, "replication": cancellation.ReplicationRemovedAt,
		"credentials": cancellation.CredentialsRemovedAt, "fence": cancellation.GreenFencedAt, "completed": cancellation.CompletedAt} {
		assert.NotNil(t, receipt, name)
	}
}

func TestCancellationStepWaitsAndRetriesWithoutAdvancing(t *testing.T) {
	now := metav1.Now()
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{Gates: &platformv1alpha1.BlueGreenGateStatus{
		Cancellation: &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: &now, SourceAccessRestoredAt: &now}}}
	canceller := newFakeCanceller()
	canceller.done["replication"] = false
	flow := New(mvutypes.Cancelling, nil, nil, nil).WithCanceller(canceller)

	outcome, err := flow.Advance(t.Context(), cancellationIntent(mvutypes.Cancelling, status))
	require.NoError(t, err)
	assert.Equal(t, string(mvutypes.Cancelling), outcome.Report.Phase)
	assert.Equal(t, mvutypes.ReasonCancellationInProgress, outcome.Report.Reason)
	assert.Nil(t, outcome.BlueGreen.Gates.Cancellation.ReplicationRemovedAt)

	blocked := errors.New("subscription finalizer failed")
	canceller.errs["replication"] = blocked
	outcome, err = flow.Advance(t.Context(), cancellationIntent(mvutypes.Cancelling, status))
	require.ErrorIs(t, err, blocked)
	assert.Equal(t, string(mvutypes.Cancelling), outcome.Report.Phase, "cancellation never fails the attempt")
	assert.Equal(t, mvutypes.ReasonCancellationBlocked, outcome.Report.Reason)
	assert.True(t, outcome.Report.Retry)
	assert.Equal(t, []string{"observe", "replication", "observe", "replication"}, canceller.calls)
}

func TestCancellationIsIgnoredAfterCommit(t *testing.T) {
	t.Run("observed canonical commit", func(t *testing.T) {
		intent := cancellationIntent(mvutypes.SwitchingOver, readyCandidateStatus())
		now := metav1.Now()
		intent.State[0].BlueGreen.Gates = &platformv1alpha1.BlueGreenGateStatus{Switchover: &platformv1alpha1.BlueGreenGateConsumptionStatus{Generation: 3, ConsumedAt: &now}}
		intent.State[0].BlueGreen.Switchover = &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1, StartedAt: &now}
		canceller, switchover := newFakeCanceller(), &countingSwitchover{}
		canceller.committed = true
		outcome, err := New(mvutypes.SwitchingOver, nil, nil, switchover).WithCanceller(canceller).Advance(t.Context(), intent)
		require.NoError(t, err)
		assert.Equal(t, 1, switchover.calls, "a committed attempt fails forward")
		assert.False(t, mvutypes.BlueGreenCancellationRequested(outcome.BlueGreen))
	})
	t.Run("durable receipt", func(t *testing.T) {
		status := readyCandidateStatus()
		status.CommitReceipt = &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1}
		canceller, switchover := newFakeCanceller(), &countingSwitchover{}
		_, err := New(mvutypes.SwitchingOver, nil, nil, switchover).WithCanceller(canceller).Advance(t.Context(), cancellationIntent(mvutypes.SwitchingOver, status))
		require.NoError(t, err)
		assert.Empty(t, canceller.calls, "a durable receipt needs no canonical observation")
		assert.Equal(t, 1, switchover.calls)
	})
	t.Run("receipt after a latched request", func(t *testing.T) {
		status := readyCandidateStatus()
		status.Gates = &platformv1alpha1.BlueGreenGateStatus{Cancellation: &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: new(metav1.Now())}}
		status.CommitReceipt = &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1}
		canceller, switchover := newFakeCanceller(), &countingSwitchover{}
		_, err := New(mvutypes.SwitchingOver, nil, nil, switchover).WithCanceller(canceller).Advance(t.Context(), cancellationIntent(mvutypes.SwitchingOver, status))
		require.NoError(t, err)
		assert.Empty(t, canceller.calls, "a commit receipt overrides the latch, as in the cutover stage")
		assert.Equal(t, 1, switchover.calls)
	})
	t.Run("terminal phase", func(t *testing.T) {
		for _, phase := range []mvutypes.Status{mvutypes.Completed, mvutypes.Cancelled} {
			assert.False(t, mvutypes.BlueGreenCancellationApplies(cancellationIntent(phase, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{})), phase)
		}
	})
}

func TestCancellationFromEveryPrecommitPhase(t *testing.T) {
	now := metav1.Now()
	restoredWindow := readyCandidateStatus()
	restoredWindow.Switchover = &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1, StartedAt: &now, SourceFenceRestoredAt: &now}
	restoredWindow.Gates = &platformv1alpha1.BlueGreenGateStatus{Switchover: &platformv1alpha1.BlueGreenGateConsumptionStatus{Generation: 3, ConsumedAt: &now}}
	openWindow := restoredWindow.DeepCopy()
	openWindow.Switchover.SourceFenceRestoredAt = nil
	for _, test := range []struct {
		phase  mvutypes.Status
		status *platformv1alpha1.PostgresBlueGreenUpgradeStatus
	}{
		{mvutypes.Scheduled, nil},
		{mvutypes.PreUpgradeBackup, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{}},
		{mvutypes.Preflight, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{}},
		{mvutypes.Upgrading, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{SourcePreparation: &platformv1alpha1.BlueGreenSourcePreparationStatus{CompletedAt: &now}}},
		{mvutypes.ReadyForSwitchover, readyCandidateStatus()},
		{mvutypes.SwitchingOver, openWindow},
		{mvutypes.Failed, restoredWindow},
		{mvutypes.Failed, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{RebuildRequired: ptr("slot invalidated")}},
	} {
		t.Run(string(test.phase), func(t *testing.T) {
			canceller, switchover := newFakeCanceller(), &countingSwitchover{}
			outcome, err := New(test.phase, &countingPreparer{}, &countingCandidate{}, switchover).WithCanceller(canceller).
				Advance(t.Context(), cancellationIntent(test.phase, test.status.DeepCopy()))
			require.NoError(t, err)
			assert.Equal(t, string(mvutypes.Cancelling), outcome.Report.Phase)
			assert.True(t, mvutypes.BlueGreenCancellationRequested(outcome.BlueGreen))
			assert.Zero(t, switchover.calls)
		})
	}
}

func TestCancellationRequiresItsPort(t *testing.T) {
	_, err := New(mvutypes.Upgrading, nil, nil, nil).Advance(t.Context(), cancellationIntent(mvutypes.Upgrading, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{}))
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
}

func TestCancellationRechecksLateCommitBeforeEveryStep(t *testing.T) {
	status := readyCandidateStatus()
	now := metav1.Now()
	status.Gates = &platformv1alpha1.BlueGreenGateStatus{Cancellation: &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: &now}}
	intent := cancellationIntent(mvutypes.Cancelling, status)
	canceller, switchover := newFakeCanceller(), &countingSwitchover{}
	canceller.committed = true
	_, err := New(mvutypes.Cancelling, nil, nil, switchover).WithCanceller(canceller).Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.Equal(t, 1, switchover.calls)
	assert.Equal(t, []string{"observe"}, canceller.calls, "no cancellation work after a late commit")
}

func TestCancellationBoundsEachStepAndRetriesWithoutReceipt(t *testing.T) {
	for _, step := range []string{"restore", "replication", "credentials", "fence"} {
		t.Run(step, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				now := metav1.Now()
				cancellation := &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: &now}
				receipts := map[string]**metav1.Time{"restore": &cancellation.SourceAccessRestoredAt, "replication": &cancellation.ReplicationRemovedAt, "credentials": &cancellation.CredentialsRemovedAt, "fence": &cancellation.GreenFencedAt}
				for _, previous := range []string{"restore", "replication", "credentials", "fence"} {
					if previous == step {
						break
					}
					*receipts[previous] = &now
				}
				status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{Gates: &platformv1alpha1.BlueGreenGateStatus{Cancellation: cancellation}}
				intent := cancellationIntent(mvutypes.Cancelling, status)
				canceller := newFakeCanceller()
				canceller.run = func(ctx context.Context, name string) (bool, error) {
					require.Equal(t, step, name)
					deadline, ok := ctx.Deadline()
					require.True(t, ok)
					assert.Equal(t, cancellationStepTimeout, time.Until(deadline))
					<-ctx.Done()
					return false, ctx.Err()
				}
				flow := New(mvutypes.Cancelling, nil, nil, nil).WithCanceller(canceller)
				start := time.Now()
				outcome, err := flow.Advance(t.Context(), intent)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				assert.Equal(t, cancellationStepTimeout, time.Since(start))
				assert.Nil(t, *receipts[step])
				assert.Equal(t, mvutypes.ReasonCancellationBlocked, outcome.Report.Reason)
				canceller.run = nil
				outcome, err = flow.Advance(t.Context(), persist(intent, outcome))
				require.NoError(t, err)
				recovered := outcome.BlueGreen.Gates.Cancellation
				saved := map[string]*metav1.Time{"restore": recovered.SourceAccessRestoredAt, "replication": recovered.ReplicationRemovedAt, "credentials": recovered.CredentialsRemovedAt, "fence": recovered.GreenFencedAt}
				assert.NotNil(t, saved[step], "the retry confirms only this step")
			})
		})
	}
}
