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

package majorversionupgradetypes

import (
	"testing"
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	reconciliationtypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func completionEvidence() *platformv1alpha1.PostgresBlueGreenUpgradeStatus {
	now := metav1.Now()
	green := corev1.ObjectReference{Name: "green", Namespace: "default", UID: "green-uid"}
	return &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt", Green: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: green},
		BlueGreenEndpointProtocolStatus: platformv1alpha1.BlueGreenEndpointProtocolStatus{CommitReceipt: &platformv1alpha1.BlueGreenCommitReceiptStatus{Target: &green, CommittedAt: &now, Sequence: 1, CanonicalConfigMap: &corev1.ObjectReference{Name: "access", UID: "access-uid"}}},
		Switchover:                      &platformv1alpha1.BlueGreenSwitchoverStatus{TargetAvailableAt: &now, EndpointPublicationEventRef: &corev1.ObjectReference{Name: "published"}},
		Completion:                      &platformv1alpha1.BlueGreenCompletionStatus{StartedAt: &now, SourceStoppedAt: &now},
		Recoverability:                  &platformv1alpha1.BlueGreenRecoverabilityStatus{TargetBackupName: new("target-backup"), BackupVerifiedAt: &now, WALArchiveBaselineVerifiedAt: &now},
	}
}

func TestRecoverableRequiresBackupAndWALBeforeTeardown(t *testing.T) {
	bg := completionEvidence()
	intent := Intent{Strategy: MajorUpgradeFlowBlueGreen, AttemptID: "attempt"}
	progress := Progress{Report: reconciliationtypes.Report{Phase: string(Recoverable), Retry: true}, BlueGreen: bg}
	require.NoError(t, ValidateBlueGreenProgress(intent, progress))
	assert.Equal(t, string(Recoverable), NormalizeCutoverProgress(progress).Report.Phase)
	bg.Recoverability.WALArchiveBaselineVerifiedAt = nil
	require.ErrorContains(t, ValidateBlueGreenProgress(intent, progress), "verified target backup and WAL")
	progress.Report.Phase = string(PostUpgradeBackup)
	bg.Completion.ReplicationRemovedAt = new(metav1.Now())
	require.ErrorContains(t, ValidateBlueGreenProgress(intent, progress), "verified target backup and WAL")
}

func TestCompletionRequiresOrderedCredentialRetirement(t *testing.T) {
	bg := completionEvidence()
	intent := Intent{Strategy: MajorUpgradeFlowBlueGreen, AttemptID: "attempt"}
	progress := Progress{Report: reconciliationtypes.Report{Phase: string(Completed)}, BlueGreen: bg}
	require.Error(t, ValidateBlueGreenProgress(intent, progress))
	bg.Completion.CredentialsRemovedAt = new(metav1.Now())
	require.ErrorContains(t, ValidateBlueGreenProgress(intent, progress), "precedes replication teardown")
	bg.Completion.ReplicationRemovedAt = new(metav1.Now())
	bg.Completion.SourceCredentialsRetiredAt = new(metav1.Now())
	require.NoError(t, ValidateBlueGreenProgress(intent, progress))
}

func TestStaleProgressCannotEraseRecoveryOrTeardownEvidence(t *testing.T) {
	previous := completionEvidence()
	previous.Completion.ReplicationRemovedAt = previous.Completion.SourceStoppedAt
	previous.Completion.SourceCredentialsRetiredAt = previous.Completion.SourceStoppedAt
	previous.Completion.CredentialsRemovedAt = previous.Completion.SourceStoppedAt
	for name, change := range map[string]func(*platformv1alpha1.PostgresBlueGreenUpgradeStatus){
		"completion removed":     func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.Completion = nil },
		"handoff start erased":   func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.Completion.StartedAt = nil },
		"source shutdown erased": func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.Completion.SourceStoppedAt = nil },
		"teardown erased":        func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.Completion.ReplicationRemovedAt = nil },
		"password proof erased": func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.Completion.SourceCredentialsRetiredAt = nil
		},
		"secret removal erased": func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.Completion.CredentialsRemovedAt = nil },
		"recovery proof erased": func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.Recoverability = nil },
		"backup proof erased":   func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.Recoverability.BackupVerifiedAt = nil },
		"WAL proof erased": func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.Recoverability.WALArchiveBaselineVerifiedAt = nil
		},
		"backup replaced": func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.Recoverability.TargetBackupName = new("another-backup")
		},
		"shutdown time rewritten": func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.Completion.SourceStoppedAt = new(metav1.NewTime(bg.Completion.SourceStoppedAt.Add(time.Second)))
		},
		"WAL time rewritten": func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.Recoverability.WALArchiveBaselineVerifiedAt = new(metav1.NewTime(bg.Recoverability.WALArchiveBaselineVerifiedAt.Add(time.Second)))
		},
	} {
		t.Run(name, func(t *testing.T) {
			next := previous.DeepCopy()
			change(next)
			require.Error(t, preserveCompletionReceipts(previous, next))
		})
	}
}

func TestCompletionReceiptsAllowNewProofsWithoutChangingOldOnes(t *testing.T) {
	previous := completionEvidence()
	previous.Recoverability = &platformv1alpha1.BlueGreenRecoverabilityStatus{}
	next := previous.DeepCopy()
	next.Recoverability = completionEvidence().Recoverability
	next.Completion.ReplicationRemovedAt = new(metav1.Now())
	require.NoError(t, preserveCompletionReceipts(previous, next))
	// An empty recorded name is immutable, though DeepDerivative ignores empty strings.
	previous.Recoverability.TargetBackupName = new("")
	require.Error(t, preserveCompletionReceipts(previous, next))
}
