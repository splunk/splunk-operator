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

package majorupgradeadapter

import (
	"errors"
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationtypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestReportOnlyProgressPreservesDurableCutoverState(t *testing.T) {
	now := metav1.Now()
	for _, test := range []struct {
		name string
		bg   platformv1alpha1.PostgresBlueGreenUpgradeStatus
	}{
		{"source preparation", platformv1alpha1.PostgresBlueGreenUpgradeStatus{SourcePreparation: &platformv1alpha1.BlueGreenSourcePreparationStatus{CompletedAt: &now}, SourceConfigurationFingerprint: new("baseline")}},
		{"consumed gate", platformv1alpha1.PostgresBlueGreenUpgradeStatus{Gates: &platformv1alpha1.BlueGreenGateStatus{Switchover: &platformv1alpha1.BlueGreenGateConsumptionStatus{Generation: 1, ConsumedAt: &now}}}},
		{"interruption window", platformv1alpha1.PostgresBlueGreenUpgradeStatus{Switchover: &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1, StartedAt: &now}}},
		{"commit receipt", platformv1alpha1.PostgresBlueGreenUpgradeStatus{CommitReceipt: &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 2}}},
		{"canonical recovery", platformv1alpha1.PostgresBlueGreenUpgradeStatus{
			CommitReceipt:       &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 2, CanonicalConfigMap: &corev1.ObjectReference{Name: "canonical", Namespace: "ns", UID: "canonical-uid"}},
			EndpointPreparation: &platformv1alpha1.BlueGreenEndpointPreparationStatus{EndpointFingerprint: new("routing")},
			CanonicalRecovery:   &platformv1alpha1.BlueGreenCanonicalRecoveryStatus{CommitSequence: 2, ConfigMap: corev1.ObjectReference{Name: "canonical", Namespace: "ns", UID: "recovered-uid"}, EndpointFingerprint: "routing", RecoveredAt: &now},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.bg.AttemptID = "attempt-1"
			original := test.bg.DeepCopy()
			intent := mvutypes.Intent{Strategy: mvutypes.MajorUpgradeFlowBlueGreen, AttemptID: test.bg.AttemptID, SourcePgVersion: "17", TargetPgVersion: "18"}
			intent.State = []platformv1alpha1.PostgresMajorUpgradeStatus{{Strategy: &intent.Strategy, SourcePgVersion: &intent.SourcePgVersion, TargetPgVersion: &intent.TargetPgVersion, BlueGreen: &test.bg}}
			store := &recordingStateStore{fakeStateStore: fakeStateStore{entries: intent.State}}
			cause := errors.New("original cutover probe failed")
			adapter := NewMajorUpgradeStateStore(store)
			err := progressError(adapter.SaveMajorUpgradeProgress(t.Context(), intent, mvutypes.Progress{Report: reconciliationtypes.Report{Name: mvutypes.UseCaseName, Phase: string(mvutypes.Failed), Message: cause.Error()}}))
			require.NoError(t, err)
			require.Len(t, store.saved, 1)
			phase := mvutypes.Failed
			if mvutypes.CutoverStageOfStatus(&test.bg).OwnsRecovery() {
				phase = mvutypes.SwitchingOver
			}
			require.Equal(t, string(phase), *store.saved[0].Phase)
			require.Contains(t, store.saved[0].Conditions[0].Message, cause.Error())
			require.Equal(t, original, store.saved[0].BlueGreen)
			require.Equal(t, original, intent.State[0].BlueGreen)
			store.saved = nil
			replacementErr := progressError(adapter.SaveMajorUpgradeProgress(t.Context(), intent, mvutypes.Progress{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: intent.AttemptID}}))
			if test.bg.CommitReceipt != nil {
				require.NoError(t, replacementErr, "coordinator-owned state always comes from the fresh snapshot")
				require.Equal(t, original, store.saved[0].BlueGreen)
			} else {
				require.Error(t, replacementErr, "runtime-owned durable state cannot be discarded")
				require.Empty(t, store.saved)
			}

		})
	}
}
