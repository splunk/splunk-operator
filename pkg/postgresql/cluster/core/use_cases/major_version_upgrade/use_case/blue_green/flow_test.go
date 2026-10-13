/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
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

type pendingPreparer struct{}

func (pendingPreparer) Prepare(context.Context, mvutypes.Intent) (*platformv1alpha1.PostgresBlueGreenUpgradeStatus, error) {
	return nil, fmt.Errorf("%w: source publication is pending", mvutypes.ErrUpgradeFlowPending)
}

func TestAdvanceKeepsPreflightWhileSourcePreparationIsPending(t *testing.T) {
	outcome, err := New(mvutypes.Preflight, pendingPreparer{}).Advance(t.Context(), mvutypes.Intent{AttemptID: "attempt-1"})
	require.NoError(t, err)
	assert.Equal(t, string(mvutypes.Preflight), outcome.Report.Phase)
	assert.Equal(t, mvutypes.ReasonUpgradeFlowPending, outcome.Report.Reason)
	assert.True(t, outcome.Report.Retry)
}

type transientPreparer struct{}

func (transientPreparer) Prepare(context.Context, mvutypes.Intent) (*platformv1alpha1.PostgresBlueGreenUpgradeStatus, error) {
	return nil, errors.New("temporary Kubernetes API error")
}

func TestAdvanceKeepsPreflightForTransientPreparationError(t *testing.T) {
	outcome, err := New(mvutypes.Preflight, transientPreparer{}).Advance(t.Context(), mvutypes.Intent{AttemptID: "attempt-1"})
	require.EqualError(t, err, "temporary Kubernetes API error")
	assert.Equal(t, string(mvutypes.Preflight), outcome.Report.Phase)
	assert.Equal(t, mvutypes.ReasonUnknownMajorUpgradeError, outcome.Report.Reason)
	assert.Contains(t, outcome.Report.Message, err.Error())
	assert.True(t, outcome.Report.Retry)
}

func TestAdvanceDoesNotRepeatCompletedSourcePreparation(t *testing.T) {
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	phase := string(mvutypes.Preflight)
	completedAt := metav1.Now()
	baseline := "frozen-source"
	preparer := &countingPreparer{}
	intent := mvutypes.Intent{
		Strategy:        strategy,
		SourcePgVersion: "17",
		TargetPgVersion: "18",
		AttemptID:       "attempt-1",
		State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
			Phase:           &phase,
			Strategy:        &strategy,
			SourcePgVersion: ptr("17"),
			TargetPgVersion: ptr("18"),
			Conditions:      []metav1.Condition{{Type: "Progressing", Reason: "LaterStageUpdatedCondition"}},
			BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
				AttemptID:                      "attempt-1",
				SourceConfigurationFingerprint: &baseline,
				SourcePreparation: &platformv1alpha1.BlueGreenSourcePreparationStatus{
					CompletedAt: &completedAt,
				},
			},
		}},
	}

	outcome, err := New(mvutypes.Preflight, preparer).Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.Equal(t, mvutypes.ReasonPreflightCheckPassed, outcome.Report.Reason)
	assert.False(t, outcome.Report.Retry)
	assert.Zero(t, preparer.calls)
}

type countingPreparer struct{ calls int }

func (p *countingPreparer) Prepare(context.Context, mvutypes.Intent) (*platformv1alpha1.PostgresBlueGreenUpgradeStatus, error) {
	p.calls++
	return nil, nil
}

func ptr(value string) *string { return &value }

type resultPreparer struct {
	status *platformv1alpha1.PostgresBlueGreenUpgradeStatus
	err    error
}

func (p resultPreparer) Prepare(context.Context, mvutypes.Intent) (*platformv1alpha1.PostgresBlueGreenUpgradeStatus, error) {
	return p.status, p.err
}

func TestAdvanceValidatesPhaseAndPreparer(t *testing.T) {
	intent := mvutypes.Intent{AttemptID: "attempt-1"}
	_, err := New(mvutypes.Preflight, nil).Advance(t.Context(), mvutypes.Intent{})
	require.ErrorIs(t, err, mvutypes.ErrInvalidUpgradeIntent)
	for _, phase := range []mvutypes.Status{mvutypes.Scheduled, mvutypes.PreUpgradeBackup} {
		outcome, err := New(phase, nil).Advance(t.Context(), intent)
		require.NoError(t, err)
		assert.Equal(t, string(mvutypes.Preflight), outcome.Report.Phase)
		assert.True(t, outcome.Report.Retry)
	}
	_, err = New(mvutypes.Completed, nil).Advance(t.Context(), intent)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	_, err = New(mvutypes.Preflight, nil).Advance(t.Context(), intent)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
}

func TestAdvancePreservesStatusOnSuccessAndInvariantFailure(t *testing.T) {
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt-1"}
	intent := mvutypes.Intent{AttemptID: "attempt-1"}
	outcome, err := New(mvutypes.Preflight, resultPreparer{status: status}).Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.Same(t, status, outcome.BlueGreen)
	assert.Equal(t, mvutypes.ReasonPreflightCheckPassed, outcome.Report.Reason)
	assert.True(t, outcome.Report.Retry)

	invariant := fmt.Errorf("%w: unmanaged publication", mvutypes.ErrUpgradeFlowFailed)
	outcome, err = New(mvutypes.Preflight, resultPreparer{status: status, err: invariant}).Advance(t.Context(), intent)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	assert.Same(t, status, outcome.BlueGreen)
	assert.Empty(t, outcome.Report.Phase)
}

func TestSourcePreparationCompleteRequiresMatchingReceipt(t *testing.T) {
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	phase := string(mvutypes.Preflight)
	intent := mvutypes.Intent{Strategy: strategy, AttemptID: "attempt-1", SourcePgVersion: "17", TargetPgVersion: "18"}
	assert.False(t, SourcePreparationComplete(intent))
	intent.State = []platformv1alpha1.PostgresMajorUpgradeStatus{{
		Phase: &phase, Strategy: &strategy, SourcePgVersion: ptr("17"), TargetPgVersion: ptr("18"),
		BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt-1", SourcePreparation: &platformv1alpha1.BlueGreenSourcePreparationStatus{}},
	}}
	assert.False(t, SourcePreparationComplete(intent))
	completedAt := metav1.Now()
	intent.State[0].BlueGreen.SourcePreparation.CompletedAt = &completedAt
	assert.True(t, SourcePreparationComplete(intent))
}
