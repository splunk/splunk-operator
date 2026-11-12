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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBlueGreenGateConditions(t *testing.T) {
	now := metav1.Now()
	requested := &platformv1alpha1.BlueGreenGateStatus{Cancellation: &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: &now}}
	cancelled := &platformv1alpha1.BlueGreenGateStatus{Cancellation: &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: &now, CompletedAt: &now}}
	receipt := &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1}
	recoverable := &platformv1alpha1.BlueGreenRecoverabilityStatus{BackupVerifiedAt: &now, WALArchiveBaselineVerifiedAt: &now}

	type want struct {
		status metav1.ConditionStatus
		reason string
	}
	for _, test := range []struct {
		name                     string
		phase                    Status
		status                   *platformv1alpha1.PostgresBlueGreenUpgradeStatus
		cancellation, retry, cln want
	}{
		{
			name: "preparing", phase: Upgrading, status: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{},
			cancellation: want{metav1.ConditionTrue, ReasonBeforeEndpointCommit},
			retry:        want{metav1.ConditionFalse, ReasonNoTerminalFailure},
			cln:          want{metav1.ConditionFalse, ReasonCleanupNotReady},
		},
		{
			name: "no blue/green status yet", phase: Preflight,
			cancellation: want{metav1.ConditionTrue, ReasonBeforeEndpointCommit},
			retry:        want{metav1.ConditionFalse, ReasonNoTerminalFailure},
			cln:          want{metav1.ConditionFalse, ReasonCleanupNotReady},
		},
		{
			name: "reusable failure", phase: Failed, status: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{},
			cancellation: want{metav1.ConditionTrue, ReasonBeforeEndpointCommit},
			retry:        want{metav1.ConditionTrue, ReasonRetryAnnotationRequired},
			cln:          want{metav1.ConditionFalse, ReasonCleanupNotReady},
		},
		{
			name: "failure requiring rebuild", phase: Failed, status: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{RebuildRequired: new("drift")},
			cancellation: want{metav1.ConditionTrue, ReasonBeforeEndpointCommit},
			retry:        want{metav1.ConditionFalse, ReasonCandidateRebuildRequired},
			cln:          want{metav1.ConditionFalse, ReasonCleanupNotReady},
		},
		{
			name: "cancelling", phase: Cancelling, status: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{Gates: requested, RebuildRequired: new("drift")},
			cancellation: want{metav1.ConditionFalse, ReasonCancellationRequested},
			retry:        want{metav1.ConditionFalse, ReasonCancellationRequested},
			cln:          want{metav1.ConditionFalse, ReasonCleanupNotReady},
		},
		{
			name: "cancelled", phase: Cancelled, status: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{Gates: cancelled},
			cancellation: want{metav1.ConditionFalse, ReasonCancellationRequested},
			retry:        want{metav1.ConditionFalse, ReasonCancellationRequested},
			cln:          want{metav1.ConditionTrue, ReasonCancelledEnvironmentRetained},
		},
		{
			name: "cancelled phase without completion receipt", phase: Cancelled, status: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{Gates: requested},
			cancellation: want{metav1.ConditionFalse, ReasonCancellationRequested},
			retry:        want{metav1.ConditionFalse, ReasonCancellationRequested},
			cln:          want{metav1.ConditionFalse, ReasonCleanupNotReady},
		},
		{
			name: "committed switchover", phase: SwitchingOver, status: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{CommitReceipt: receipt},
			cancellation: want{metav1.ConditionFalse, ReasonCommittedFailForward},
			retry:        want{metav1.ConditionFalse, ReasonCommittedFailForward},
			cln:          want{metav1.ConditionFalse, ReasonCleanupNotReady},
		},
		{
			name: "completed before recoverability", phase: Completed, status: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{CommitReceipt: receipt},
			cancellation: want{metav1.ConditionFalse, ReasonCommittedFailForward},
			retry:        want{metav1.ConditionFalse, ReasonCommittedFailForward},
			cln:          want{metav1.ConditionFalse, ReasonCleanupNotReady},
		},
		{
			name: "completed and recoverable", phase: Completed, status: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{CommitReceipt: receipt, Recoverability: recoverable},
			cancellation: want{metav1.ConditionFalse, ReasonCommittedFailForward},
			retry:        want{metav1.ConditionFalse, ReasonCommittedFailForward},
			cln:          want{metav1.ConditionTrue, ReasonCompletedEnvironmentRetained},
		},
		{
			name: "cleaned", phase: Completed, status: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{CommitReceipt: receipt, Recoverability: recoverable,
				Cleanup: &platformv1alpha1.BlueGreenCleanupStatus{State: platformv1alpha1.BlueGreenCleanupStateCleaned}},
			cancellation: want{metav1.ConditionFalse, ReasonCommittedFailForward},
			retry:        want{metav1.ConditionFalse, ReasonCommittedFailForward},
			cln:          want{metav1.ConditionFalse, ReasonCleaned},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			conditions := BlueGreenGateConditions(platformv1alpha1.PostgresMajorUpgradeStatus{Phase: new(string(test.phase)), BlueGreen: test.status})
			require.Len(t, conditions, 4)
			for conditionType, expected := range map[string]want{
				ConditionCancellationAvailable: test.cancellation,
				ConditionRetryAvailable:        test.retry,
				ConditionCleanupAvailable:      test.cln,
			} {
				condition := meta.FindStatusCondition(conditions, conditionType)
				require.NotNil(t, condition, conditionType)
				assert.Equal(t, expected.status, condition.Status, conditionType)
				assert.Equal(t, expected.reason, condition.Reason, conditionType)
				assert.NotEmpty(t, condition.Message, conditionType)
			}
		})
	}
}

func TestCleanupRequiredWhileEnvironmentIsRetained(t *testing.T) {
	environment := &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "env"}}
	for _, test := range []struct {
		name     string
		phase    Status
		status   *platformv1alpha1.PostgresBlueGreenUpgradeStatus
		required bool
	}{
		{"cancelled with green", Cancelled, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{Green: environment}, true},
		{"cancelled before green", Cancelled, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{}, false},
		{"completed with blue", Completed, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{Blue: environment}, true},
		{"cleaned", Completed, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{Blue: environment,
			Cleanup: &platformv1alpha1.BlueGreenCleanupStatus{State: platformv1alpha1.BlueGreenCleanupStateCleaned}}, false},
		{"in progress", Upgrading, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{Blue: environment, Green: environment}, false},
		{"no blue/green status", Cancelled, nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			conditions := BlueGreenGateConditions(platformv1alpha1.PostgresMajorUpgradeStatus{Phase: new(string(test.phase)), BlueGreen: test.status})
			condition := meta.FindStatusCondition(conditions, ConditionCleanupRequired)
			require.NotNil(t, condition)
			assert.Equal(t, test.required, condition.Status == metav1.ConditionTrue)
		})
	}
}

func TestCancellationStepsAreWriteOnceAndLatched(t *testing.T) {
	requested := metav1.NewTime(time.Unix(1_000, 0))
	restored := metav1.NewTime(requested.Add(time.Second))
	current := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt", Gates: &platformv1alpha1.BlueGreenGateStatus{
		Cancellation: &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: &requested, SourceAccessRestoredAt: &restored},
	}}
	next := current.DeepCopy()
	removed := metav1.NewTime(restored.Add(time.Second))
	next.Gates.Cancellation.ReplicationRemovedAt = &removed
	require.NoError(t, validateExistingBlueGreenProgress("attempt", current, next))

	for _, test := range []struct {
		name   string
		change func(*platformv1alpha1.BlueGreenGateStatus)
	}{
		{"removed receipt", func(gates *platformv1alpha1.BlueGreenGateStatus) { gates.Cancellation = nil }},
		{"cleared request", func(gates *platformv1alpha1.BlueGreenGateStatus) { gates.Cancellation.RequestedAt = nil }},
		{"moved request", func(gates *platformv1alpha1.BlueGreenGateStatus) {
			gates.Cancellation.RequestedAt = new(metav1.NewTime(requested.Add(time.Hour)))
		}},
		{"cleared restoration", func(gates *platformv1alpha1.BlueGreenGateStatus) { gates.Cancellation.SourceAccessRestoredAt = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := current.DeepCopy()
			test.change(changed.Gates)
			assert.Error(t, validateExistingBlueGreenProgress("attempt", current, changed))
		})
	}
}

func TestCancellationCannotStartOrCompleteAfterCommit(t *testing.T) {
	now := metav1.Now()
	committed := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt", CommitReceipt: &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1}}
	requested := committed.DeepCopy()
	requested.Gates = &platformv1alpha1.BlueGreenGateStatus{Cancellation: &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: &now}}
	assert.Error(t, validateExistingBlueGreenProgress("attempt", committed, requested))

	completed := requested.DeepCopy()
	completed.Gates.Cancellation.CompletedAt = &now
	assert.Error(t, validateCancellationReceipt("attempt", completed))
}

func TestCancellationStepsRequireLatchedRequest(t *testing.T) {
	now := metav1.Now()
	for name, cancellation := range map[string]*platformv1alpha1.BlueGreenCancellationStatus{
		"source access":  {SourceAccessRestoredAt: &now},
		"replication":    {ReplicationRemovedAt: &now},
		"credentials":    {CredentialsRemovedAt: &now},
		"green fence":    {GreenFencedAt: &now},
		"completion":     {CompletedAt: &now},
		"latched result": {RequestedAt: &now, CompletedAt: &now},
	} {
		t.Run(name, func(t *testing.T) {
			intent := Intent{Strategy: MajorUpgradeFlowBlueGreen, AttemptID: "attempt"}
			err := ValidateBlueGreenProgress(intent, Progress{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
				AttemptID: "attempt", Gates: &platformv1alpha1.BlueGreenGateStatus{Cancellation: cancellation},
			}})
			if cancellation.RequestedAt != nil {
				assert.NoError(t, err)
				return
			}
			assert.Error(t, err)
		})
	}
}
