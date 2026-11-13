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
	"fmt"
	"testing"
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationtypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	bluegreen "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/blue_green"
	pgupgradeflow "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/pg_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type matrixSwitchover struct{ advances, revalidations int }

func (s *matrixSwitchover) AdvanceSwitchover(_ context.Context, _ mvutypes.Intent, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bluegreen.Outcome, error) {
	s.advances++
	phase := mvutypes.SwitchingOver
	if status != nil && status.Switchover != nil && status.Switchover.TargetAvailableAt != nil {
		phase = mvutypes.PostUpgradeBackup
	}
	return bluegreen.Outcome{BlueGreen: status, Report: reconciliationtypes.Report{Name: mvutypes.UseCaseName, Phase: string(phase), Retry: true}}, nil
}

func (s *matrixSwitchover) RevalidateCandidate(context.Context, mvutypes.Intent, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	s.revalidations++
	return true, nil
}

// matrixStatus builds one durable attempt state named by the transition table.
func matrixStatus(state string) (mvutypes.Status, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
	at := func(offset time.Duration) *metav1.Time { return new(metav1.NewTime(time.Unix(10_000, 0).Add(offset))) }
	prepared := &platformv1alpha1.BlueGreenSourcePreparationStatus{CompletedAt: at(0)}
	consumed := &platformv1alpha1.BlueGreenGateStatus{Switchover: &platformv1alpha1.BlueGreenGateConsumptionStatus{Generation: 2, ConsumedAt: at(time.Second)}}
	green := &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: "green-uid"}}
	receipt := &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1, Target: &green.Ref, CommittedAt: at(3 * time.Second),
		CanonicalConfigMap: &corev1.ObjectReference{Name: "access", UID: "canonical-uid"}}
	switch state {
	case "preflight":
		return mvutypes.Preflight, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{}
	case "candidate":
		return mvutypes.Upgrading, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{SourcePreparation: prepared}
	case "open window":
		return mvutypes.SwitchingOver, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{SourcePreparation: prepared, Green: green, Gates: consumed,
			Switchover: &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1, StartedAt: at(2 * time.Second)}}
	case "committed":
		return mvutypes.SwitchingOver, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{SourcePreparation: prepared, Green: green, Gates: consumed, CommitReceipt: receipt,
			Switchover: &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1, StartedAt: at(2 * time.Second)}}
	case "target available":
		return mvutypes.PostUpgradeBackup, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{SourcePreparation: prepared, Green: green, Gates: consumed, CommitReceipt: receipt,
			Switchover: &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1, StartedAt: at(2 * time.Second),
				TargetAvailableAt: at(4 * time.Second), EndpointPublicationEventRef: &corev1.ObjectReference{Name: "event"}}}
	case "restored window":
		return mvutypes.Failed, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{SourcePreparation: prepared, Green: green, Gates: consumed,
			Switchover: &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1, StartedAt: at(2 * time.Second), SourceFenceRestoredAt: at(3 * time.Second)}}
	case "candidate failure":
		return mvutypes.Failed, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{SourcePreparation: prepared}
	case "rebuild required":
		return mvutypes.Failed, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{SourcePreparation: prepared, RebuildRequired: new("slot invalidated")}
	case "cancelling":
		return mvutypes.Cancelling, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{SourcePreparation: prepared,
			Gates: &platformv1alpha1.BlueGreenGateStatus{Cancellation: &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: at(time.Second)}}}
	case "cancelled":
		return mvutypes.Cancelled, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{SourcePreparation: prepared, Green: green,
			Gates: &platformv1alpha1.BlueGreenGateStatus{Cancellation: &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: at(time.Second), CompletedAt: at(2 * time.Second)}}}
	case "completed":
		return mvutypes.Completed, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{SourcePreparation: prepared, CommitReceipt: receipt}
	}
	panic("unknown matrix state " + state)
}

// TestBlueGreenTransitionMatrix runs every lifecycle state against every user
// action: none, a cancel request, and a newer retry timestamp.
func TestBlueGreenTransitionMatrix(t *testing.T) {
	type want struct {
		phase                   mvutypes.Status // "" when the use case is not scheduled
		advances, revalidations int
	}
	notScheduled := want{}
	states := []struct {
		name                string
		none, cancel, retry want
	}{
		{"preflight", want{phase: mvutypes.Preflight}, want{phase: mvutypes.Cancelling}, want{phase: mvutypes.Preflight}},
		{"candidate", want{phase: mvutypes.Upgrading}, want{phase: mvutypes.Cancelling}, want{phase: mvutypes.Upgrading}},
		{"open window", want{phase: mvutypes.SwitchingOver, advances: 1}, want{phase: mvutypes.Cancelling}, want{phase: mvutypes.SwitchingOver, advances: 1}},
		{"committed", want{phase: mvutypes.SwitchingOver, advances: 1}, want{phase: mvutypes.SwitchingOver, advances: 1}, want{phase: mvutypes.SwitchingOver, advances: 1}},
		{"target available", want{phase: mvutypes.PostUpgradeBackup, advances: 1}, want{phase: mvutypes.PostUpgradeBackup, advances: 1}, want{phase: mvutypes.PostUpgradeBackup, advances: 1}},
		{"restored window", notScheduled, want{phase: mvutypes.Cancelling}, want{phase: mvutypes.SwitchingOver, revalidations: 1}},
		{"candidate failure", notScheduled, want{phase: mvutypes.Cancelling}, want{phase: mvutypes.Preflight}},
		{"rebuild required", notScheduled, want{phase: mvutypes.Cancelling}, notScheduled},
		{"cancelling", want{phase: mvutypes.Cancelling}, want{phase: mvutypes.Cancelling}, want{phase: mvutypes.Cancelling}},
		{"cancelled", notScheduled, notScheduled, notScheduled},
		{"completed", notScheduled, notScheduled, notScheduled},
	}
	for _, state := range states {
		for action, expected := range map[string]want{"none": state.none, "cancel": state.cancel, "retry": state.retry} {
			t.Run(fmt.Sprintf("%s/%s", state.name, action), func(t *testing.T) {
				phase, status := matrixStatus(state.name)
				intent := blueGreenEntryIntent(phase, status)
				intent.CancelRequested = action == "cancel"
				intent.Generation = 2
				if phase == mvutypes.Failed {
					intent.State[0].Conditions = []metav1.Condition{{Type: mvutypes.ConditionMajorUpgradeTerminalFailure, Status: metav1.ConditionTrue,
						LastTransitionTime: metav1.NewTime(time.Unix(10_005, 0))}}
				}
				if action == "retry" {
					intent.RetryRequestedAt = new(metav1.NewTime(time.Unix(10_010, 0)))
				}
				store := &blueGreenProgressStore{intent: intent}
				switchover, candidate := &matrixSwitchover{}, &fakeBlueGreenCandidatePreparer{}
				useCase := NewMajorUpgradeUseCase(store, &fakeBackupProvider{status: &mvutypes.BackupInfo{BackupName: "pre"}}, nil, pgupgradeflow.NoopNotifier(),
					&fakeBlueGreenPreparer{err: mvutypes.ErrUpgradeFlowPending}, candidate, switchover).WithBlueGreenCanceller(&completingCanceller{})

				scheduled, err := useCase.Schedule(t.Context())
				require.NoError(t, err)
				require.Equal(t, expected.phase != "", scheduled)
				if !scheduled {
					return
				}
				report, _ := useCase.Act(t.Context())
				assert.Equal(t, string(expected.phase), report.Phase)
				assert.Equal(t, expected.advances, switchover.advances)
				assert.Equal(t, expected.revalidations, switchover.revalidations)
				if expected.phase == mvutypes.Cancelling {
					assert.Zero(t, candidate.calls, "a cancelling attempt never prepares the candidate")
				} else {
					assert.False(t, mvutypes.BlueGreenCancellationRequested(store.progress.BlueGreen), "a committed or uncancelled attempt latches no request")
				}
			})
		}
	}
}
