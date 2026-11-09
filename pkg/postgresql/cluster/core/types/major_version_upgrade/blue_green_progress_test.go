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

package majorversionupgradetypes

import (
	"math"
	"testing"
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateBlueGreenProgressKeepsSwitchoverCommitBoundaryMonotonic(t *testing.T) {
	started := metav1.Now()
	deadline := metav1.NewTime(started.Add(time.Minute))
	committed := metav1.NewTime(deadline.Add(-time.Second))
	finalLSN := "0/2A000000"
	verified := metav1.NewTime(committed.Add(time.Second))
	fenceApplied := metav1.NewTime(started.Add(time.Second))
	fenceVerified := metav1.NewTime(fenceApplied.Add(time.Second))
	drainCompleted := metav1.NewTime(fenceVerified.Add(time.Second))
	hba := []string{"host all all 0.0.0.0/0 scram-sha-256"}
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		AttemptID: "attempt-1",
		Gates:     &platformv1alpha1.BlueGreenGateStatus{Switchover: &platformv1alpha1.BlueGreenGateConsumptionStatus{Generation: 8, ConsumedAt: &started}},
		CommitReceipt: &platformv1alpha1.BlueGreenCommitReceiptStatus{
			Sequence: 1, CommittedAt: &committed,
			CanonicalConfigMap: &corev1.ObjectReference{Name: "cluster-access", UID: "canonical-uid"},
			Target:             &corev1.ObjectReference{Name: "green", UID: "green-uid"},
		},
		Switchover: &platformv1alpha1.BlueGreenSwitchoverStatus{
			Sequence: 1, RetryRequestedAt: &started,
			ControlIdentity: &platformv1alpha1.BlueGreenControlIdentityStatus{RoleName: "control", SecretRef: corev1.ObjectReference{Name: "control-secret", UID: "control-uid"}, PreparedAt: &started},
			LatchedTimeout:  &metav1.Duration{Duration: time.Minute}, StartedAt: &started, DeadlineAt: &deadline,
			FenceAppliedAt: &fenceApplied, FenceVerifiedAt: &fenceVerified, DrainCompletedAt: &drainCompleted,
			OriginalHBA: &hba, OriginalRoleLogins: []platformv1alpha1.BlueGreenRoleLoginStatus{{RoleName: "orders_rw", Login: true}},
			GreenOriginalHBA: &hba, GreenOriginalRoleLogins: []platformv1alpha1.BlueGreenRoleLoginStatus{{RoleName: "orders_rw", Login: true}},
			CandidateGuardsRemovedAt: &verified, FinalParityVerifiedAt: &verified,
			EndpointPublicationEventRef: &corev1.ObjectReference{Name: "published", UID: "event-uid"},
			FinalBlueLSN:                &finalLSN,
			SequenceSynchronization:     &platformv1alpha1.BlueGreenSequenceSynchronizationStatus{CompletedAt: &committed, VerifiedAt: &verified},
		},
	}
	intent := Intent{Strategy: MajorUpgradeFlowBlueGreen, SourcePgVersion: "17", TargetPgVersion: "18", AttemptID: "attempt-1", State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
		Strategy: new(MajorUpgradeFlowBlueGreen), SourcePgVersion: new("17"), TargetPgVersion: new("18"), BlueGreen: status,
	}}}

	for _, tc := range []struct {
		name   string
		mutate func(*platformv1alpha1.PostgresBlueGreenUpgradeStatus)
	}{
		{"remove start", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.Switchover.StartedAt = nil }},
		{"remove commit receipt", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.CommitReceipt = nil }},
		{"rewrite commit time", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.CommitReceipt.CommittedAt = &deadline }},
		{"remove consumed gate", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.Gates.Switchover = nil }},
		{"remove control identity", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.Switchover.ControlIdentity = nil }},
		{"remove parity proof", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.Switchover.FinalParityVerifiedAt = nil }},
		{"remove guard removal proof", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.Switchover.CandidateGuardsRemovedAt = nil
		}},
		{"remove publication Event receipt", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.Switchover.EndpointPublicationEventRef = nil
		}},
		{"remove consumed retry", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.Switchover.RetryRequestedAt = nil }},
		{"change green HBA snapshot", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.Switchover.GreenOriginalHBA = new([]string{"host all all all trust"})
		}},
		{"change green role snapshot", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.Switchover.GreenOriginalRoleLogins = []platformv1alpha1.BlueGreenRoleLoginStatus{{RoleName: "orders_rw", Login: false}}
		}},
		{"rewrite commit sequence", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.CommitReceipt.Sequence++ }},
		{"rewrite consumed gate generation", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.Gates.Switchover.Generation++ }},
		{"change latched timeout", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.Switchover.LatchedTimeout.Duration += time.Second
		}},
		{"change deadline", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			later := metav1.NewTime(deadline.Add(time.Second))
			bg.Switchover.DeadlineAt = &later
		}},
		{"change fence milestone", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			later := metav1.NewTime(fenceApplied.Add(time.Second))
			bg.Switchover.FenceAppliedAt = &later
		}},
		{"change original HBA", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.Switchover.OriginalHBA = &[]string{"host all all 127.0.0.1/32 trust"}
		}},
		{"change original role login", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.Switchover.OriginalRoleLogins[0].Login = false
		}},
		{"change final LSN", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.Switchover.FinalBlueLSN = new("0/2A000001")
		}},
		{"remove sequence receipt", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.Switchover.SequenceSynchronization = nil }},
		{"rewrite canonical receipt", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.CommitReceipt.CanonicalConfigMap.UID = "recreated"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proposed := status.DeepCopy()
			tc.mutate(proposed)
			err := ValidateBlueGreenProgress(intent, Progress{BlueGreen: proposed})
			assert.Error(t, err)
		})
	}
	assert.NoError(t, ValidateBlueGreenProgress(intent, Progress{BlueGreen: status.DeepCopy()}))
}

func TestSwitchoverWindowCanAdvanceOnlyAfterRestorationAndNewRetry(t *testing.T) {
	started := metav1.NewTime(time.Now().Add(-2 * time.Minute))
	restored := metav1.NewTime(time.Now().Add(-time.Minute))
	retryAt := metav1.Now()
	current := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt", Switchover: &platformv1alpha1.BlueGreenSwitchoverStatus{
		Sequence: 1, StartedAt: &started, SourceFenceRestoredAt: &restored,
		CandidateGuardsRemovedAt: &restored,
		OriginalHBA:              new([]string{"host all all 10.0.0.0/8 scram-sha-256"}),
		ControlIdentity:          &platformv1alpha1.BlueGreenControlIdentityStatus{RoleName: "control", SecretRef: corev1.ObjectReference{Name: "control-secret", UID: "control-uid"}},
	}}
	valid := current.DeepCopy()
	valid.Switchover = &platformv1alpha1.BlueGreenSwitchoverStatus{
		Sequence: 2, RetryRequestedAt: &retryAt,
		OriginalHBA: current.Switchover.OriginalHBA, ControlIdentity: current.Switchover.ControlIdentity,
		CandidateGuardsRemovedAt: current.Switchover.CandidateGuardsRemovedAt,
	}
	require.NoError(t, validateExistingBlueGreenProgress("attempt", current, valid))
	for _, test := range []struct {
		name   string
		change func(*platformv1alpha1.PostgresBlueGreenUpgradeStatus)
	}{
		{"no new retry", func(next *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { next.Switchover.RetryRequestedAt = nil }},
		{"old retry", func(next *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			next.Switchover.RetryRequestedAt = &started
		}},
		{"skipped sequence", func(next *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { next.Switchover.Sequence = 3 }},
		{"reused final LSN", func(next *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			next.Switchover.FinalBlueLSN = new("0/100")
		}},
		{"reused failure", func(next *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			next.Switchover.Failure = new("previous interruption failed")
		}},
		{"reused parity receipt", func(next *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			next.Switchover.FinalParityVerifiedAt = &restored
		}},
		{"lost original HBA", func(next *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { next.Switchover.OriginalHBA = nil }},
		{"lost control identity", func(next *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { next.Switchover.ControlIdentity = nil }},
		{"lost guard removal receipt", func(next *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			next.Switchover.CandidateGuardsRemovedAt = nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			next := valid.DeepCopy()
			test.change(next)
			assert.Error(t, validateExistingBlueGreenProgress("attempt", current, next))
		})
	}
	unrestored := current.DeepCopy()
	unrestored.Switchover.SourceFenceRestoredAt = nil
	assert.Error(t, validateExistingBlueGreenProgress("attempt", unrestored, valid))
	committed := current.DeepCopy()
	committed.CommitReceipt = &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1}
	assert.Error(t, validateExistingBlueGreenProgress("attempt", committed, valid))
	consumedRetry := current.DeepCopy()
	consumedRetry.Switchover.RetryRequestedAt = &retryAt
	assert.Error(t, validateExistingBlueGreenProgress("attempt", consumedRetry, valid))
	exhausted := current.DeepCopy()
	exhausted.Switchover.Sequence = math.MaxInt64
	wrapped := valid.DeepCopy()
	wrapped.Switchover.Sequence = math.MinInt64
	assert.Error(t, validateExistingBlueGreenProgress("attempt", exhausted, wrapped))
}

func TestCanonicalRecoveryCannotChangeCommitIdentity(t *testing.T) {
	now := metav1.Now()
	current := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt",
		CommitReceipt:       &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 5, CanonicalConfigMap: &corev1.ObjectReference{Name: "access", Namespace: "default", UID: "original"}},
		EndpointPreparation: &platformv1alpha1.BlueGreenEndpointPreparationStatus{EndpointFingerprint: new("routing")},
		CanonicalRecovery: &platformv1alpha1.BlueGreenCanonicalRecoveryStatus{
			CommitSequence: 5, ConfigMap: corev1.ObjectReference{Name: "access", Namespace: "default", UID: "recreated-1"}, EndpointFingerprint: "routing", RecoveredAt: &now,
		},
	}
	valid := current.DeepCopy()
	valid.CanonicalRecovery = &platformv1alpha1.BlueGreenCanonicalRecoveryStatus{
		CommitSequence: 5, ConfigMap: corev1.ObjectReference{Name: "access", Namespace: "default", UID: "recreated-2"}, EndpointFingerprint: "routing", RecoveredAt: &now,
	}
	require.NoError(t, validateExistingBlueGreenProgress("attempt", current, valid))
	invalid := current.DeepCopy()
	invalid.CanonicalRecovery = nil
	assert.Error(t, validateExistingBlueGreenProgress("attempt", current, invalid))
	invalid = current.DeepCopy()
	invalid.CommitReceipt.Sequence++
	assert.Error(t, validateExistingBlueGreenProgress("attempt", current, invalid))
	invalid = current.DeepCopy()
	invalid.CanonicalRecovery = &platformv1alpha1.BlueGreenCanonicalRecoveryStatus{CommitSequence: 6, ConfigMap: current.CanonicalRecovery.ConfigMap, EndpointFingerprint: "routing", RecoveredAt: &now}
	assert.Error(t, validateExistingBlueGreenProgress("attempt", current, invalid))
}

func TestValidateBlueGreenProgressRejectsAttemptAndCommitReceiptRegression(t *testing.T) {
	source := "17"
	target := "18"
	strategy := MajorUpgradeFlowBlueGreen
	intent := Intent{
		Strategy:        strategy,
		SourcePgVersion: source,
		TargetPgVersion: target,
		AttemptID:       "attempt-1",
		State: []platformv1alpha1.PostgresMajorUpgradeStatus{{
			Strategy:        &strategy,
			SourcePgVersion: &source,
			TargetPgVersion: &target,
			BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
				AttemptID:     "attempt-1",
				CommitReceipt: &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 2},
			},
		}},
	}

	assert.Error(t, ValidateBlueGreenProgress(intent, Progress{
		BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "other-attempt"},
	}))
	assert.Error(t, ValidateBlueGreenProgress(intent, Progress{
		BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
			AttemptID:     "attempt-1",
			CommitReceipt: &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1},
		},
	}))
	assert.NoError(t, ValidateBlueGreenProgress(intent, Progress{
		BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
			AttemptID:     "attempt-1",
			CommitReceipt: &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 2},
		},
	}))
}

func TestValidateBlueGreenProgressKeepsSourcePreparationReceiptWithBaseline(t *testing.T) {
	source, target := "17", "18"
	strategy := MajorUpgradeFlowBlueGreen
	completedAt := metav1.Now()
	baseline := "frozen-source"
	intent := Intent{Strategy: strategy, SourcePgVersion: source, TargetPgVersion: target, AttemptID: "attempt-1"}
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		AttemptID:                      intent.AttemptID,
		SourceConfigurationFingerprint: &baseline,
		SourcePreparation:              &platformv1alpha1.BlueGreenSourcePreparationStatus{CompletedAt: &completedAt},
	}
	progress := Progress{BlueGreen: status}
	require.NoError(t, ValidateBlueGreenProgress(intent, progress))
	intent.State = []platformv1alpha1.PostgresMajorUpgradeStatus{{
		Strategy: new(strategy), SourcePgVersion: new(source), TargetPgVersion: new(target), BlueGreen: status.DeepCopy(),
	}}
	assert.NoError(t, ValidateBlueGreenProgress(intent, Progress{}), "a report without a new blue/green status preserves the receipt")

	withoutReceipt := status.DeepCopy()
	withoutReceipt.SourcePreparation = nil
	assert.Error(t, ValidateBlueGreenProgress(intent, Progress{BlueGreen: withoutReceipt}))
	changedReceipt := status.DeepCopy()
	later := metav1.NewTime(completedAt.Add(time.Second))
	changedReceipt.SourcePreparation.CompletedAt = &later
	assert.Error(t, ValidateBlueGreenProgress(intent, Progress{BlueGreen: changedReceipt}))
	changedBaseline := status.DeepCopy()
	changed := "changed-source"
	changedBaseline.SourceConfigurationFingerprint = &changed
	assert.Error(t, ValidateBlueGreenProgress(intent, Progress{BlueGreen: changedBaseline}))

	status.SourceConfigurationFingerprint = nil
	assert.Error(t, ValidateBlueGreenProgress(Intent{Strategy: strategy, AttemptID: intent.AttemptID}, Progress{BlueGreen: status}), "a new receipt requires its baseline")
}
