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
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type pendingPreparer struct{}

func (pendingPreparer) Prepare(context.Context, mvutypes.Intent) (*platformv1alpha1.PostgresBlueGreenUpgradeStatus, error) {
	return nil, fmt.Errorf("%w: source publication is pending", mvutypes.ErrUpgradeFlowPending)
}

func TestAdvanceKeepsPreflightWhileSourcePreparationIsPending(t *testing.T) {
	outcome, err := New(mvutypes.Preflight, pendingPreparer{}, nil).Advance(t.Context(), mvutypes.Intent{AttemptID: "attempt-1"})
	require.NoError(t, err)
	assert.Equal(t, string(mvutypes.Preflight), outcome.Report.Phase)
	assert.Equal(t, mvutypes.ReasonUpgradeFlowPending, outcome.Report.Reason)
	assert.True(t, outcome.Report.Retry)
	require.NotNil(t, outcome.Report.Sleep, "successful waits must not accumulate failure backoff")
	assert.Equal(t, 1, *outcome.Report.Sleep)
}

type transientPreparer struct{}

func (transientPreparer) Prepare(context.Context, mvutypes.Intent) (*platformv1alpha1.PostgresBlueGreenUpgradeStatus, error) {
	return nil, errors.New("temporary Kubernetes API error")
}

func TestAdvanceKeepsPreflightForTransientPreparationError(t *testing.T) {
	outcome, err := New(mvutypes.Preflight, transientPreparer{}, nil).Advance(t.Context(), mvutypes.Intent{AttemptID: "attempt-1"})
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

	candidate := &countingCandidate{}
	outcome, err := New(mvutypes.Preflight, preparer, candidate).Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.Equal(t, mvutypes.ReasonUpgradeFlowPending, outcome.Report.Reason)
	assert.Equal(t, string(mvutypes.Upgrading), outcome.Report.Phase)
	assert.True(t, outcome.Report.Retry)
	assert.Zero(t, preparer.calls)
	assert.Equal(t, 1, candidate.calls)
}

type transientCandidate struct{}

func (transientCandidate) PrepareCandidate(context.Context, mvutypes.Intent) (*platformv1alpha1.PostgresBlueGreenUpgradeStatus, error) {
	return nil, errors.New("temporary Kubernetes API error")
}

func TestAdvanceKeepsUpgradingForTransientCandidateError(t *testing.T) {
	completedAt := metav1.Now()
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	phase := string(mvutypes.Upgrading)
	sourceVersion, targetVersion := "17", "18"
	intent := mvutypes.Intent{AttemptID: "attempt-1", Strategy: strategy, SourcePgVersion: sourceVersion, TargetPgVersion: targetVersion,
		State: []platformv1alpha1.PostgresMajorUpgradeStatus{{Phase: &phase, Strategy: &strategy, SourcePgVersion: &sourceVersion, TargetPgVersion: &targetVersion,
			BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt-1", SourcePreparation: &platformv1alpha1.BlueGreenSourcePreparationStatus{CompletedAt: &completedAt}}}},
	}
	outcome, err := New(mvutypes.Upgrading, &countingPreparer{}, transientCandidate{}).Advance(t.Context(), intent)
	require.EqualError(t, err, "temporary Kubernetes API error")
	assert.Equal(t, string(mvutypes.Upgrading), outcome.Report.Phase)
	assert.True(t, outcome.Report.Retry)
}

func TestSourcePreparationReceiptSurvivesCandidatePhaseTransition(t *testing.T) {
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	phase := string(mvutypes.Upgrading)
	completedAt := metav1.Now()
	assert.True(t, SourcePreparationComplete(mvutypes.Intent{
		Strategy: strategy, SourcePgVersion: "17", TargetPgVersion: "18", AttemptID: "attempt-1",
		State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
			Phase: &phase, Strategy: &strategy, SourcePgVersion: ptr("17"), TargetPgVersion: ptr("18"),
			BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
				AttemptID: "attempt-1", SourcePreparation: &platformv1alpha1.BlueGreenSourcePreparationStatus{CompletedAt: &completedAt},
			},
		}},
	}))
}

func TestAdvanceCertifiesOnlyCandidateWithCompleteReplicationProof(t *testing.T) {
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	phase := string(mvutypes.Upgrading)
	fingerprint := "source-config"
	schema := "schema"
	access := "access"
	ready := true
	now := metav1.Now()
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		AttemptID:                      "attempt-1",
		SourcePreparation:              &platformv1alpha1.BlueGreenSourcePreparationStatus{CompletedAt: &now},
		Blue:                           &platformv1alpha1.BlueGreenEnvironmentStatus{},
		Green:                          &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: "green-uid"}, AvailableAt: &now},
		SourceConfigurationFingerprint: &fingerprint,
		CredentialFingerprints: []platformv1alpha1.BlueGreenCredentialFingerprintStatus{{
			SecretName: "orders-admin", SecretUID: "secret-uid", Fingerprint: "fingerprint",
		}},
		CandidateEndpoints: &platformv1alpha1.BlueGreenCandidateEndpointsStatus{
			DirectService: &corev1.ObjectReference{Name: "orders-green-rw"},
		},
		DatabaseReplication: []platformv1alpha1.BlueGreenDatabaseReplicationStatus{{
			DatabaseName: "orders", SourceSchemaFingerprint: &schema, SourceAccessPolicyFingerprint: &access,
			TargetSchemaFingerprint: &schema, TargetAccessPolicyFingerprint: &access,
			SchemaImported: &ready, InitialCopyComplete: &ready,
			SubscriptionHealthy: &ready, SlotHealthy: &ready,
			ReplicaSlots: []platformv1alpha1.BlueGreenReplicaSlotStatus{{Valid: &ready}},
		}},
	}
	intent := mvutypes.Intent{Strategy: strategy, SourcePgVersion: "17", TargetPgVersion: "18", AttemptID: "attempt-1", State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
		Phase: &phase, Strategy: &strategy, SourcePgVersion: ptr("17"), TargetPgVersion: ptr("18"), BlueGreen: status,
	}}}

	outcome, err := New(mvutypes.Upgrading, &countingPreparer{}, candidateWithStatus{status: status}).Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.Equal(t, string(mvutypes.ReadyForSwitchover), outcome.Report.Phase)
	assert.Equal(t, mvutypes.ReasonCandidateReady, outcome.Report.Reason)
	assert.True(t, outcome.Report.Retry, "the ready candidate remains continuously verified")
}

func TestCandidateReadyRequiresEveryReplicationProof(t *testing.T) {
	ready := true
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{DatabaseReplication: []platformv1alpha1.BlueGreenDatabaseReplicationStatus{{
		SchemaImported: &ready, InitialCopyComplete: &ready, SubscriptionHealthy: &ready, SlotHealthy: &ready,
	}}}
	assert.False(t, CandidateReady(status), "an endpoint, availability timestamp, and source slot receipt are all required")
}

func TestCandidateReadyHandsOffToSwitchoverPort(t *testing.T) {
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	phase := string(mvutypes.Upgrading)
	now := metav1.Now()
	fingerprint, schema, access := "source-config", "schema", "access"
	ready := true
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		AttemptID:                      "attempt-1",
		SourcePreparation:              &platformv1alpha1.BlueGreenSourcePreparationStatus{CompletedAt: &now},
		Green:                          &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: "green-uid"}, AvailableAt: &now},
		CandidateEndpoints:             &platformv1alpha1.BlueGreenCandidateEndpointsStatus{DirectService: &corev1.ObjectReference{Name: "green-rw"}},
		SourceConfigurationFingerprint: &fingerprint,
		CredentialFingerprints:         []platformv1alpha1.BlueGreenCredentialFingerprintStatus{{SecretName: "admin", SecretUID: "secret-uid", Fingerprint: "credential"}},
		DatabaseReplication: []platformv1alpha1.BlueGreenDatabaseReplicationStatus{{
			DatabaseName: "orders", SourceSchemaFingerprint: &schema, SourceAccessPolicyFingerprint: &access,
			TargetSchemaFingerprint: &schema, TargetAccessPolicyFingerprint: &access, SchemaImported: &ready,
			InitialCopyComplete: &ready, SubscriptionHealthy: &ready, SlotHealthy: &ready,
			ReplicaSlots: []platformv1alpha1.BlueGreenReplicaSlotStatus{{Valid: &ready}},
		}},
	}
	intent := mvutypes.Intent{AttemptID: "attempt-1", Strategy: strategy, SourcePgVersion: "17", TargetPgVersion: "18", Generation: 8, SwitchoverAuthorized: true, SwitchoverTimeout: 90 * time.Second, State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
		Phase: &phase, Strategy: &strategy, SourcePgVersion: ptr("17"), TargetPgVersion: ptr("18"), BlueGreen: status,
	}}}
	switchover := &countingSwitchover{}
	outcome, err := New(mvutypes.Upgrading, nil, candidateWithStatus{status: status}, switchover).Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.Zero(t, switchover.calls, "gate consumption must persist before invoking the mutating switchover adapter")
	assert.Equal(t, string(mvutypes.SwitchingOver), outcome.Report.Phase)
	require.NotNil(t, outcome.BlueGreen.Gates.Switchover)
	assert.Equal(t, int64(8), outcome.BlueGreen.Gates.Switchover.Generation)
	assert.NotNil(t, outcome.BlueGreen.Gates.Switchover.ConsumedAt)
	require.NotNil(t, outcome.BlueGreen.Switchover)
	assert.Nil(t, outcome.BlueGreen.Switchover.LatchedTimeout, "control bootstrap precedes the bounded fencing interval")
	assert.Nil(t, outcome.BlueGreen.Switchover.StartedAt)
	assert.Nil(t, outcome.BlueGreen.Switchover.DeadlineAt)
	assert.True(t, SwitchoverStarted(mvutypes.Intent{AttemptID: intent.AttemptID, Strategy: strategy, SourcePgVersion: "17", TargetPgVersion: "18", State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
		Strategy: &strategy, SourcePgVersion: ptr("17"), TargetPgVersion: ptr("18"), BlueGreen: outcome.BlueGreen,
	}}}))
}

func TestCandidateReadyDoesNotConsumeSwitchoverGateWhenFalse(t *testing.T) {
	status := readyCandidateStatus()
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	phase := string(mvutypes.Upgrading)
	intent := mvutypes.Intent{AttemptID: "attempt-1", Strategy: strategy, SourcePgVersion: "17", TargetPgVersion: "18", State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
		Phase: &phase, Strategy: &strategy, SourcePgVersion: ptr("17"), TargetPgVersion: ptr("18"), BlueGreen: status,
	}}}
	switchover := &countingSwitchover{}
	outcome, err := New(mvutypes.Upgrading, nil, candidateWithStatus{status: status}, switchover).Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.Zero(t, switchover.calls)
	assert.Nil(t, outcome.BlueGreen.Switchover)
	assert.Nil(t, outcome.BlueGreen.Gates)
	assert.Equal(t, string(mvutypes.ReadyForSwitchover), outcome.Report.Phase)
}

func readyCandidateStatus() *platformv1alpha1.PostgresBlueGreenUpgradeStatus {
	now := metav1.Now()
	ready := true
	fingerprint, schema, access := "source-config", "schema", "access"
	return &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		AttemptID: "attempt-1", SourcePreparation: &platformv1alpha1.BlueGreenSourcePreparationStatus{CompletedAt: &now},
		Green:                          &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: "green-uid"}, AvailableAt: &now},
		CandidateEndpoints:             &platformv1alpha1.BlueGreenCandidateEndpointsStatus{DirectService: &corev1.ObjectReference{Name: "green-rw"}},
		SourceConfigurationFingerprint: &fingerprint,
		CredentialFingerprints:         []platformv1alpha1.BlueGreenCredentialFingerprintStatus{{SecretName: "admin", SecretUID: "secret-uid", Fingerprint: "credential"}},
		DatabaseReplication: []platformv1alpha1.BlueGreenDatabaseReplicationStatus{{
			DatabaseName: "orders", SourceSchemaFingerprint: &schema, SourceAccessPolicyFingerprint: &access,
			TargetSchemaFingerprint: &schema, TargetAccessPolicyFingerprint: &access, SchemaImported: &ready,
			InitialCopyComplete: &ready, SubscriptionHealthy: &ready, SlotHealthy: &ready,
			ReplicaSlots: []platformv1alpha1.BlueGreenReplicaSlotStatus{{Valid: &ready}},
		}},
	}
}

func TestStartedSwitchoverResumesWithoutCandidatePreparation(t *testing.T) {
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	phase := string(mvutypes.SwitchingOver)
	now := metav1.Now()
	intent := mvutypes.Intent{AttemptID: "attempt-1", Strategy: strategy, SourcePgVersion: "17", TargetPgVersion: "18", State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
		Phase: &phase, Strategy: &strategy, SourcePgVersion: ptr("17"), TargetPgVersion: ptr("18"),
		BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt-1", SourcePreparation: &platformv1alpha1.BlueGreenSourcePreparationStatus{CompletedAt: &now}, Switchover: &platformv1alpha1.BlueGreenSwitchoverStatus{StartedAt: &now}},
	}}}
	candidate := &countingCandidate{}
	switchover := &countingSwitchover{}
	outcome, err := New(mvutypes.SwitchingOver, nil, candidate, switchover).Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.Zero(t, candidate.calls)
	assert.Equal(t, 1, switchover.calls)
	assert.Equal(t, string(mvutypes.SwitchingOver), outcome.Report.Phase)
}

func TestTargetAvailableWaitsForRecoverabilityWithoutReplayingCandidate(t *testing.T) {
	status := readyCandidateStatus()
	now := metav1.Now()
	status.CommitReceipt = &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1, CommittedAt: &now, Target: status.Green.Ref.DeepCopy(), CanonicalConfigMap: &corev1.ObjectReference{Name: "access", UID: "access-uid"}}
	status.Switchover = &platformv1alpha1.BlueGreenSwitchoverStatus{StartedAt: &now, TargetAvailableAt: &now, EndpointPublicationEventRef: &corev1.ObjectReference{Name: "publication-event"}}
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	intent := mvutypes.Intent{AttemptID: status.AttemptID, Strategy: strategy, SourcePgVersion: "17", TargetPgVersion: "18", State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
		Phase: new(string(mvutypes.PostUpgradeBackup)), Strategy: &strategy,
		SourcePgVersion: new("17"), TargetPgVersion: new("18"), BlueGreen: status,
	}}}
	candidate, switchover := &countingCandidate{}, &countingSwitchover{}
	for range 2 {
		outcome, err := New(mvutypes.PostUpgradeBackup, nil, candidate, switchover).Advance(t.Context(), intent)
		require.NoError(t, err)
		assert.Equal(t, string(mvutypes.PostUpgradeBackup), outcome.Report.Phase)
		assert.True(t, outcome.Report.Retry)
		assert.Equal(t, status, outcome.BlueGreen)
	}
	assert.Zero(t, candidate.calls)
	assert.Equal(t, 2, switchover.calls, "the runtime continues canonical projection repair during recoverability handoff")
}

func TestTargetAvailabilityWithoutEventReceiptResumesSwitchover(t *testing.T) {
	status := readyCandidateStatus()
	now := metav1.Now()
	status.Switchover = &platformv1alpha1.BlueGreenSwitchoverStatus{StartedAt: &now, TargetAvailableAt: &now}
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	intent := mvutypes.Intent{AttemptID: status.AttemptID, Strategy: strategy, SourcePgVersion: "17", TargetPgVersion: "18", State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
		Phase: new(string(mvutypes.SwitchingOver)), Strategy: &strategy,
		SourcePgVersion: new("17"), TargetPgVersion: new("18"), BlueGreen: status,
	}}}
	candidate, switchover := &countingCandidate{}, &countingSwitchover{}
	_, err := New(mvutypes.SwitchingOver, nil, candidate, switchover).Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.Zero(t, candidate.calls)
	assert.Equal(t, 1, switchover.calls)
}

func TestRestoredSwitchoverRequiresNewRetryBeforeRelatchingDeadline(t *testing.T) {
	status := readyCandidateStatus()
	started := metav1.NewTime(time.Now().Add(-2 * time.Minute))
	restored := metav1.NewTime(time.Now().Add(-time.Minute))
	retryAt := metav1.Now()
	hba := []string{"host all all 10.0.0.0/8 scram-sha-256"}
	gate := &platformv1alpha1.BlueGreenGateConsumptionStatus{Generation: 3, ConsumedAt: &started}
	status.Gates = &platformv1alpha1.BlueGreenGateStatus{Switchover: gate}
	status.Switchover = &platformv1alpha1.BlueGreenSwitchoverStatus{
		Sequence: 1, StartedAt: &started, SourceFenceRestoredAt: &restored, TimedOutAt: &restored,
		LatchedTimeout: &metav1.Duration{Duration: time.Minute}, OriginalHBA: &hba,
		OriginalRoleLogins:       []platformv1alpha1.BlueGreenRoleLoginStatus{{RoleName: "writer", Login: true}},
		FinalBlueLSN:             new("0/100"),
		CandidateGuardsRemovedAt: &restored,
		ControlIdentity:          &platformv1alpha1.BlueGreenControlIdentityStatus{RoleName: "control", SecretRef: corev1.ObjectReference{Name: "control-secret"}},
	}
	intent := mvutypes.Intent{AttemptID: status.AttemptID, Generation: 9, SwitchoverTimeout: 90 * time.Second}
	_, err := beginSwitchover(status, intent)
	require.ErrorIs(t, err, mvutypes.ErrInvalidUpgradeIntent)
	intent.RetryRequestedAt = &retryAt
	outcome, err := beginSwitchover(status, intent)
	require.NoError(t, err)
	window := outcome.BlueGreen.Switchover
	assert.Equal(t, int64(2), window.Sequence)
	assert.Equal(t, &retryAt, window.RetryRequestedAt)
	assert.Nil(t, window.LatchedTimeout)
	assert.Nil(t, window.StartedAt)
	assert.Nil(t, window.DeadlineAt)
	assert.Nil(t, window.SourceFenceRestoredAt)
	assert.Nil(t, window.TimedOutAt)
	assert.Nil(t, window.FinalBlueLSN)
	assert.Equal(t, status.Switchover.CandidateGuardsRemovedAt, window.CandidateGuardsRemovedAt)
	assert.Equal(t, status.Switchover.ControlIdentity, window.ControlIdentity)
	assert.Equal(t, status.Switchover.OriginalHBA, window.OriginalHBA)
	assert.Equal(t, status.Switchover.OriginalRoleLogins, window.OriginalRoleLogins)
	assert.Equal(t, gate, outcome.BlueGreen.Gates.Switchover)
	assert.Equal(t, int64(1), status.Switchover.Sequence, "a new window must not mutate the persisted attempt snapshot")
	committed := status.DeepCopy()
	committed.CommitReceipt = &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1, CommittedAt: &restored}
	_, err = beginSwitchover(committed, intent)
	require.ErrorIs(t, err, mvutypes.ErrInvalidUpgradeIntent, "a receipt alone forbids another interruption window")
}

func TestExplicitRetryResumesRestoredAttemptWithoutCandidateReplay(t *testing.T) {
	status := readyCandidateStatus()
	started := metav1.NewTime(time.Now().Add(-2 * time.Minute))
	restored := metav1.NewTime(time.Now().Add(-time.Minute))
	retryAt := metav1.Now()
	status.Gates = &platformv1alpha1.BlueGreenGateStatus{Switchover: &platformv1alpha1.BlueGreenGateConsumptionStatus{Generation: 3, ConsumedAt: &started}}
	status.Switchover = &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1, StartedAt: &started, SourceFenceRestoredAt: &restored}
	strategy := mvutypes.MajorUpgradeFlowBlueGreen
	intent := mvutypes.Intent{AttemptID: status.AttemptID, Strategy: strategy, SourcePgVersion: "17", TargetPgVersion: "18",
		Generation: 9, RetryRequestedAt: &retryAt, SwitchoverAuthorized: false,
		State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
			Phase: new(string(mvutypes.Failed)), Strategy: &strategy,
			SourcePgVersion: new("17"), TargetPgVersion: new("18"), BlueGreen: status,
		}},
	}
	candidate, source := &countingCandidate{}, &countingPreparer{}
	outcome, err := New(mvutypes.Scheduled, source, candidate).Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.Equal(t, string(mvutypes.SwitchingOver), outcome.Report.Phase)
	assert.Equal(t, int64(2), outcome.BlueGreen.Switchover.Sequence)
	assert.Equal(t, status.Gates.Switchover, outcome.BlueGreen.Gates.Switchover)
	assert.Zero(t, candidate.calls)
	assert.Zero(t, source.calls)
	intent.RetryRequestedAt = nil
	outcome, err = New(mvutypes.Scheduled, source, candidate).Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.Equal(t, string(mvutypes.Failed), outcome.Report.Phase)
	assert.False(t, outcome.Report.Retry)
	assert.Zero(t, candidate.calls)
}

type countingPreparer struct{ calls int }

func (p *countingPreparer) Prepare(context.Context, mvutypes.Intent) (*platformv1alpha1.PostgresBlueGreenUpgradeStatus, error) {
	p.calls++
	return nil, nil
}

type countingCandidate struct{ calls int }

func (p *countingCandidate) PrepareCandidate(context.Context, mvutypes.Intent) (*platformv1alpha1.PostgresBlueGreenUpgradeStatus, error) {
	p.calls++
	return nil, nil
}

type candidateWithStatus struct {
	status *platformv1alpha1.PostgresBlueGreenUpgradeStatus
}

type countingSwitchover struct{ calls int }

func (s *countingSwitchover) AdvanceSwitchover(_ context.Context, _ mvutypes.Intent, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (Outcome, error) {
	s.calls++
	if status != nil && status.Switchover != nil && status.Switchover.TargetAvailableAt != nil && status.Switchover.EndpointPublicationEventRef != nil {
		return targetAvailableOutcome(status), nil
	}
	return Outcome{Report: reconciliationTypes.Report{Phase: string(mvutypes.SwitchingOver)}, BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt-1"}}, nil
}

func (p candidateWithStatus) PrepareCandidate(context.Context, mvutypes.Intent) (*platformv1alpha1.PostgresBlueGreenUpgradeStatus, error) {
	return p.status, nil
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
	_, err := New(mvutypes.Preflight, nil, nil).Advance(t.Context(), mvutypes.Intent{})
	require.ErrorIs(t, err, mvutypes.ErrInvalidUpgradeIntent)
	for _, phase := range []mvutypes.Status{mvutypes.Scheduled, mvutypes.PreUpgradeBackup} {
		outcome, err := New(phase, nil, nil).Advance(t.Context(), intent)
		require.NoError(t, err)
		assert.Equal(t, string(mvutypes.Preflight), outcome.Report.Phase)
		assert.True(t, outcome.Report.Retry)
	}
	_, err = New(mvutypes.Completed, nil, nil).Advance(t.Context(), intent)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	_, err = New(mvutypes.Preflight, nil, nil).Advance(t.Context(), intent)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
}

func TestAdvancePreservesStatusOnSuccessAndInvariantFailure(t *testing.T) {
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt-1"}
	intent := mvutypes.Intent{AttemptID: "attempt-1"}
	outcome, err := New(mvutypes.Preflight, resultPreparer{status: status}, nil).Advance(t.Context(), intent)
	require.NoError(t, err)
	assert.Same(t, status, outcome.BlueGreen)
	assert.Equal(t, mvutypes.ReasonPreflightCheckPassed, outcome.Report.Reason)
	assert.True(t, outcome.Report.Retry)

	invariant := fmt.Errorf("%w: unmanaged publication", mvutypes.ErrUpgradeFlowFailed)
	outcome, err = New(mvutypes.Preflight, resultPreparer{status: status, err: invariant}, nil).Advance(t.Context(), intent)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	assert.Same(t, status, outcome.BlueGreen)
	assert.Equal(t, string(mvutypes.Failed), outcome.Report.Phase)
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

// Target availability hands production over to the independent recoverability
// lifecycle. Keeping this state pending prevents the legacy pg_upgrade backup
// path from declaring a blue/green attempt Completed before replication teardown.
func targetAvailableOutcome(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) Outcome {
	return Outcome{Report: reconciliationTypes.Report{
		Name: mvutypes.UseCaseName, Phase: string(mvutypes.PostUpgradeBackup),
		Reason:  mvutypes.ReasonUpgradeFlowPending,
		Message: "Green is available; waiting for target-major recoverability and replication teardown.", Retry: true,
	}, BlueGreen: status}
}

func SwitchoverStarted(intent mvutypes.Intent) bool {
	return mvutypes.CutoverStageOfStatus(latestBlueGreenStatus(intent)).OwnsRecovery()
}

type failedReadSwitchover struct{ cause error }

func (s failedReadSwitchover) AdvanceSwitchover(context.Context, mvutypes.Intent, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (Outcome, error) {
	return Outcome{}, s.cause
}

func TestRuntimeReadFailureCannotRegressRecoveryPhase(t *testing.T) {
	now := metav1.Now()
	cause := errors.New("transient attempt read failure")
	for _, phase := range []mvutypes.Status{mvutypes.SwitchingOver, mvutypes.Scheduled, mvutypes.Failed} {
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/committed=%v", phase, committed), func(t *testing.T) {
				bg := readyCandidateStatus()
				bg.Switchover = &platformv1alpha1.BlueGreenSwitchoverStatus{StartedAt: &now}
				if committed {
					bg.CommitReceipt = &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1, CommittedAt: &now, Target: bg.Green.Ref.DeepCopy(), CanonicalConfigMap: &corev1.ObjectReference{Name: "access", UID: "access-uid"}}
				}
				intent := mvutypes.Intent{Strategy: mvutypes.MajorUpgradeFlowBlueGreen, AttemptID: bg.AttemptID, SourcePgVersion: "17", TargetPgVersion: "18"}
				intent.State = []platformv1alpha1.PostgresMajorUpgradeStatus{{Strategy: new(intent.Strategy), SourcePgVersion: new(intent.SourcePgVersion), TargetPgVersion: new(intent.TargetPgVersion), BlueGreen: bg}}
				candidate := &countingCandidate{}
				got, err := New(phase, nil, candidate, failedReadSwitchover{cause}).Advance(t.Context(), intent)
				require.ErrorIs(t, err, cause)
				require.NotNil(t, got.BlueGreen)
				assert.Equal(t, bg, got.BlueGreen)
				assert.Equal(t, string(mvutypes.SwitchingOver), got.Report.Phase)
				assert.True(t, got.Report.Retry)
				assert.Equal(t, 1, *got.Report.Sleep)
				assert.Zero(t, candidate.calls)
			})
		}
	}
}
