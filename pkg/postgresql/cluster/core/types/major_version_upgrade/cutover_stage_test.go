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
	"encoding/json" // Exercise the serializer used by Kubernetes clients.
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
	"time"
)

func committedStageFixture() *platformv1alpha1.PostgresBlueGreenUpgradeStatus {
	now := metav1.NewTime(time.Unix(100, 0).UTC())
	return &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt", Green: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: "green-uid"}}, CommitReceipt: &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1, CommittedAt: &now, Target: &corev1.ObjectReference{Name: "green", UID: "green-uid"}, CanonicalConfigMap: &corev1.ObjectReference{Name: "access", UID: "canonical-uid"}}, Switchover: &platformv1alpha1.BlueGreenSwitchoverStatus{StartedAt: &now}}
}

func TestCutoverStagesSelectRecoveryConservatively(t *testing.T) {
	now := metav1.Now()
	for _, tc := range []struct {
		name   string
		change func(*platformv1alpha1.PostgresBlueGreenUpgradeStatus)
		want   CutoverStage
	}{
		{"preparation", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.CommitReceipt = nil; bg.Switchover = nil }, CutoverNone},
		{"consumed before start", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.CommitReceipt = nil
			bg.Switchover = nil
			bg.Gates = &platformv1alpha1.BlueGreenGateStatus{Switchover: &platformv1alpha1.BlueGreenGateConsumptionStatus{ConsumedAt: &now}}
		}, CutoverAuthorized},
		{"window before start", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.CommitReceipt = nil
			bg.Switchover = &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1}
		}, CutoverAuthorized},
		{"interruption", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.CommitReceipt = nil }, CutoverInterrupting},
		{"restored", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.CommitReceipt = nil
			bg.Switchover.SourceFenceRestoredAt = &now
		}, CutoverRestored},
		{"receipt overrides restoration", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.Switchover.SourceFenceRestoredAt = &now }, CutoverCommitted},
		{"receipt without window", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.Switchover = nil }, CutoverCommitted},
		{"available event pending", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.Switchover.TargetAvailableAt = &now }, CutoverTargetAvailable},
		{"handoff", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.Switchover.TargetAvailableAt = &now
			bg.Switchover.EndpointPublicationEventRef = &corev1.ObjectReference{Name: "event", UID: "event-uid"}
		}, CutoverHandedOff},
		{"availability without receipt", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.CommitReceipt = nil
			bg.Switchover.TargetAvailableAt = &now
		}, CutoverInterrupting},
		{"replacement target cannot hand off", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
			bg.Green.Ref.UID = "replacement"
			bg.Switchover.TargetAvailableAt = &now
			bg.Switchover.EndpointPublicationEventRef = &corev1.ObjectReference{Name: "event"}
		}, CutoverCommitted},
		{"compacted", func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) { bg.HistoryCompacted = true }, CutoverNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bg := committedStageFixture()
			tc.change(bg)
			assert.Equal(t, tc.want, CutoverStageOfStatus(bg))
		})
	}
	for _, phase := range []Status{Completed, Cancelled} {
		assert.Equal(t, CutoverNone, CutoverStageOf(platformv1alpha1.PostgresMajorUpgradeStatus{Phase: new(string(phase)), BlueGreen: committedStageFixture()}))
	}
	cleaned := committedStageFixture()
	cleaned.Cleanup = &platformv1alpha1.BlueGreenCleanupStatus{State: platformv1alpha1.BlueGreenCleanupStateCleaned}
	assert.Equal(t, CutoverNone, CutoverStageOf(platformv1alpha1.PostgresMajorUpgradeStatus{BlueGreen: cleaned}))
	assert.Equal(t, CutoverNone, CutoverStageOfStatus(nil))
}

func TestRecoveryPhaseSurvivesGenericErrorMapping(t *testing.T) {
	for _, phase := range []Status{Scheduled, Failed, Upgrading} {
		bg := committedStageFixture()
		bg.CommitReceipt = nil
		p := Progress{BlueGreen: bg}
		p.Report.Phase = string(phase)
		p.Report.Reason = "ProbeFailed"
		p.Report.Message = "original failure"
		got := NormalizeCutoverProgress(p)
		assert.Equal(t, string(SwitchingOver), got.Report.Phase)
		assert.Equal(t, p.Report.Message, got.Report.Message)
		assert.Equal(t, p.Report.Reason, got.Report.Reason)
		assert.True(t, got.Report.Retry)
		require.NotNil(t, got.Report.Sleep)
		assert.Equal(t, 1, *got.Report.Sleep)
		bg.Switchover.SourceFenceRestoredAt = new(metav1.Now())
		p.Report.Phase = string(Failed)
		assert.Equal(t, string(Failed), NormalizeCutoverProgress(p).Report.Phase)
	}
}

func TestEndpointOwnerGroupingPreservesWireAndDeepCopy(t *testing.T) {
	bg := committedStageFixture()
	bg.EndpointIntentRevision = 4
	bg.EndpointPreparation = &platformv1alpha1.BlueGreenEndpointPreparationStatus{Generation: 4}
	bg.EndpointPublication = &platformv1alpha1.BlueGreenEndpointPublicationStatus{Generation: 5}
	bg.CanonicalRecovery = &platformv1alpha1.BlueGreenCanonicalRecoveryStatus{CommitSequence: 1}
	raw, err := json.Marshal(bg)
	require.NoError(t, err)
	var wire map[string]any
	require.NoError(t, json.Unmarshal(raw, &wire))
	for _, key := range []string{"endpointIntentRevision", "endpointPreparation", "endpointPublication", "commitReceipt", "canonicalRecovery"} {
		assert.Contains(t, wire, key)
	}
	assert.NotContains(t, wire, "BlueGreenEndpointProtocolStatus")
	assert.NotContains(t, wire, "endpoint")
	var restored platformv1alpha1.PostgresBlueGreenUpgradeStatus
	require.NoError(t, json.Unmarshal(raw, &restored))
	roundTrip, err := json.Marshal(&restored)
	require.NoError(t, err)
	assert.JSONEq(t, string(raw), string(roundTrip))
	assert.True(t, bg.CommitReceipt.CommittedAt.Equal(restored.CommitReceipt.CommittedAt))
	copy := bg.DeepCopy()
	copy.EndpointPreparation.Generation = 99
	copy.CommitReceipt.Target.UID = "changed"
	assert.Equal(t, int64(4), bg.EndpointPreparation.Generation)
	assert.Equal(t, corev1.ObjectReference{Name: "green", UID: "green-uid"}, *bg.CommitReceipt.Target)
}
