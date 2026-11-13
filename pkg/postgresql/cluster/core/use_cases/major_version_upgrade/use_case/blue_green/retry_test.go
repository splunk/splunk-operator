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
	"errors"
	"fmt"
	"testing"
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// restoredWindowIntent is a restored window with a newer retry request.
func restoredWindowIntent(fenced bool) mvutypes.Intent {
	status := readyCandidateStatus()
	started := metav1.NewTime(time.Now().Add(-2 * time.Minute))
	restored := metav1.NewTime(time.Now().Add(-time.Minute))
	status.Gates = &platformv1alpha1.BlueGreenGateStatus{Switchover: &platformv1alpha1.BlueGreenGateConsumptionStatus{Generation: 3, ConsumedAt: &started}}
	status.Switchover = &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1, SourceFenceRestoredAt: &restored}
	if fenced {
		status.Switchover.StartedAt = &started
	}
	intent := cancellationIntent(mvutypes.Failed, status)
	intent.CancelRequested = false
	intent.RetryRequestedAt = new(metav1.Now())
	return intent
}

func TestTimedOutWindowWaitsForCatchUpBeforeAnotherWindow(t *testing.T) {
	intent := restoredWindowIntent(true)
	switchover, candidate := &countingSwitchover{notReady: true}, &countingCandidate{}
	flow := New(mvutypes.Scheduled, &countingPreparer{}, candidate, switchover)

	outcome, err := flow.Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.Equal(t, string(mvutypes.Upgrading), outcome.Report.Phase, "green returns to catch-up")
	assert.True(t, outcome.Report.Retry)
	assert.Equal(t, int64(1), outcome.BlueGreen.Switchover.Sequence, "no window opens before revalidation")
	assert.Zero(t, candidate.calls, "a fenced green is never re-prepared through ordinary access")

	switchover.notReady = false
	outcome, err = flow.Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.Equal(t, string(mvutypes.SwitchingOver), outcome.Report.Phase)
	assert.Equal(t, int64(2), outcome.BlueGreen.Switchover.Sequence)
	assert.Equal(t, intent.RetryRequestedAt, outcome.BlueGreen.Switchover.RetryRequestedAt)
	assert.Equal(t, 2, switchover.revalidations)
	assert.Zero(t, switchover.calls)
}

func TestUnfencedRestoredWindowReusesOrdinaryCandidatePreparation(t *testing.T) {
	intent := restoredWindowIntent(false)
	switchover := &countingSwitchover{}
	ready := intent.State[0].BlueGreen.DeepCopy()
	outcome, err := New(mvutypes.Scheduled, &countingPreparer{}, candidateWithStatus{status: ready}, switchover).Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.Equal(t, string(mvutypes.SwitchingOver), outcome.Report.Phase)
	assert.Zero(t, switchover.revalidations)

	ready.Green.AvailableAt = nil
	outcome, err = New(mvutypes.Scheduled, &countingPreparer{}, candidateWithStatus{status: ready}, switchover).Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.Equal(t, string(mvutypes.Upgrading), outcome.Report.Phase, "a candidate that is no longer ready waits")
}

func TestRevalidationFailureClassification(t *testing.T) {
	t.Run("integrity", func(t *testing.T) {
		invalid := fmt.Errorf("%w: frozen catalog changed", mvutypes.ErrCandidateInvalid)
		outcome, err := New(mvutypes.Scheduled, nil, nil, &countingSwitchover{revalidateErr: invalid}).Advance(t.Context(), restoredWindowIntent(true))
		require.ErrorIs(t, err, mvutypes.ErrCandidateInvalid)
		require.NotNil(t, outcome.BlueGreen.RebuildRequired)
		assert.Equal(t, string(mvutypes.Failed), outcome.Report.Phase)
		assert.Equal(t, mvutypes.ReasonCandidateRebuildRequired, outcome.Report.Reason)
	})
	t.Run("transient", func(t *testing.T) {
		outcome, err := New(mvutypes.Scheduled, nil, nil, &countingSwitchover{revalidateErr: errors.New("connection reset")}).Advance(t.Context(), restoredWindowIntent(true))
		require.Error(t, err)
		assert.True(t, outcome.Report.Retry)
		assert.Nil(t, outcome.BlueGreen.RebuildRequired)
	})
}

func TestRebuildRequiredRejectsRetryOfRestoredWindow(t *testing.T) {
	intent := restoredWindowIntent(true)
	intent.State[0].BlueGreen.RebuildRequired = ptr("slot invalidated")
	switchover := &countingSwitchover{}
	outcome, err := New(mvutypes.Scheduled, nil, nil, switchover).Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.Equal(t, string(mvutypes.Failed), outcome.Report.Phase)
	assert.Equal(t, mvutypes.ReasonCandidateRebuildRequired, outcome.Report.Reason)
	assert.False(t, outcome.Report.Retry)
	assert.Zero(t, switchover.revalidations)
	assert.Equal(t, int64(1), outcome.BlueGreen.Switchover.Sequence)
}

func TestPartialControlBootstrapUsesRevalidationBeforeAnyWindow(t *testing.T) {
	intent := restoredWindowIntent(false)
	intent.State[0].BlueGreen.Switchover.ControlIdentity = &platformv1alpha1.BlueGreenControlIdentityStatus{RoleName: "control"}
	candidate, switchover := &countingCandidate{}, &countingSwitchover{notReady: true}
	outcome, err := New(mvutypes.Scheduled, nil, candidate, switchover).Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.Equal(t, string(mvutypes.Upgrading), outcome.Report.Phase)
	assert.Equal(t, int64(1), outcome.BlueGreen.Switchover.Sequence)
	assert.Zero(t, candidate.calls, "ordinary preflight must not inspect an owned partial SQL bootstrap")
	assert.Equal(t, 1, switchover.revalidations)
	switchover.notReady = false
	outcome, err = New(mvutypes.Upgrading, nil, candidate, switchover).Advance(t.Context(), persist(intent, outcome))
	require.NoError(t, err)
	assert.Equal(t, int64(2), outcome.BlueGreen.Switchover.Sequence)
	assert.Zero(t, candidate.calls)
}
