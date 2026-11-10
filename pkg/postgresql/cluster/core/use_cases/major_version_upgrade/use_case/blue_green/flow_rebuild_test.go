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
	"fmt"
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type resultCandidate struct {
	status *platformv1alpha1.PostgresBlueGreenUpgradeStatus
	err    error
}

func (p resultCandidate) PrepareCandidate(context.Context, mvutypes.Intent) (*platformv1alpha1.PostgresBlueGreenUpgradeStatus, error) {
	return p.status, p.err
}

func preparedCandidateIntent() mvutypes.Intent {
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	phase := string(mvutypes.Upgrading)
	completedAt := metav1.Now()
	return mvutypes.Intent{AttemptID: "attempt-1", Strategy: strategy, SourcePgVersion: "17", TargetPgVersion: "18",
		State: []platformv1alpha1.PostgresMajorUpgradeStatus{{Phase: &phase, Strategy: &strategy, SourcePgVersion: ptr("17"), TargetPgVersion: ptr("18"),
			BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt-1",
				SourcePreparation: &platformv1alpha1.BlueGreenSourcePreparationStatus{CompletedAt: &completedAt}}}},
	}
}

func TestPreparationClassifiesOnlyCandidateInvalidFailuresForRebuild(t *testing.T) {
	invalid := fmt.Errorf("%w: schema changed after preflight", mvutypes.ErrCandidateInvalid)
	for _, test := range []struct {
		name    string
		err     error
		rebuild bool
	}{
		{name: "candidate invalid", err: invalid, rebuild: true},
		{name: "joined candidate invalid", err: errors.Join(errors.New("context"), invalid), rebuild: true},
		{name: "ordinary terminal", err: fmt.Errorf("%w: unsupported extension", mvutypes.ErrUpgradeFlowFailed)},
		{name: "invalid intent", err: mvutypes.ErrInvalidUpgradeIntent},
	} {
		t.Run(test.name, func(t *testing.T) {
			status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt-1"}
			outcome, err := New(mvutypes.Preflight, resultPreparer{status: status, err: test.err}, nil, nil).Advance(t.Context(), mvutypes.Intent{AttemptID: "attempt-1"})
			require.ErrorIs(t, err, test.err)
			require.NotNil(t, outcome.BlueGreen)
			assert.Nil(t, status.RebuildRequired, "the adapter's status must not be mutated in place")
			if !test.rebuild {
				assert.Same(t, status, outcome.BlueGreen)
				assert.Nil(t, outcome.BlueGreen.RebuildRequired)
				return
			}
			require.NotNil(t, outcome.BlueGreen.RebuildRequired)
			assert.Equal(t, test.err.Error(), *outcome.BlueGreen.RebuildRequired)
		})
	}
}

func TestCandidatePreparationPersistsRebuildClassification(t *testing.T) {
	invalid := fmt.Errorf("%w: blue logical slot is invalidated", mvutypes.ErrCandidateInvalid)

	outcome, err := New(mvutypes.Upgrading, &countingPreparer{}, resultCandidate{err: invalid}, nil).Advance(t.Context(), preparedCandidateIntent())
	require.ErrorIs(t, err, mvutypes.ErrCandidateInvalid)
	assert.Equal(t, string(mvutypes.Failed), outcome.Report.Phase, "rebuild classification must take the terminal branch, not automatic retry")
	assert.Equal(t, mvutypes.ReasonCandidateRebuildRequired, outcome.Report.Reason)
	require.NotNil(t, outcome.BlueGreen, "a nil adapter status falls back to the durable attempt")
	assert.NotNil(t, outcome.BlueGreen.SourcePreparation, "the durable receipt must survive classification")
	require.NotNil(t, outcome.BlueGreen.RebuildRequired)
	assert.Contains(t, *outcome.BlueGreen.RebuildRequired, "logical slot is invalidated")
}

func TestCandidatePreparationKeepsEarlierRebuildReason(t *testing.T) {
	intent := preparedCandidateIntent()
	intent.State[0].BlueGreen.RebuildRequired = ptr("first reason")
	invalid := fmt.Errorf("%w: second reason", mvutypes.ErrCandidateInvalid)

	outcome, err := New(mvutypes.Upgrading, &countingPreparer{}, resultCandidate{err: invalid}, nil).Advance(t.Context(), intent)
	require.ErrorIs(t, err, mvutypes.ErrCandidateInvalid)
	require.NotNil(t, outcome.BlueGreen.RebuildRequired)
	assert.Equal(t, "first reason", *outcome.BlueGreen.RebuildRequired)
}

func TestTransientCandidateFailureIsNotClassifiedForRebuild(t *testing.T) {
	outcome, err := New(mvutypes.Upgrading, &countingPreparer{}, resultCandidate{err: fmt.Errorf("%w: waiting", mvutypes.ErrUpgradeFlowPending)}, nil).Advance(t.Context(), preparedCandidateIntent())
	require.NoError(t, err)
	if outcome.BlueGreen != nil {
		assert.Nil(t, outcome.BlueGreen.RebuildRequired)
	}
}
