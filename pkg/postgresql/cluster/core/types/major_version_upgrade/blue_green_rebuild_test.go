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
	"errors"
	"fmt"
	"strings"
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCandidateInvalidKeepsTerminalClassification(t *testing.T) {
	err := fmt.Errorf("%w: credential Secret changed", ErrCandidateInvalid)
	assert.True(t, IsTerminalFlowFailure(err))
	assert.True(t, IsTerminalFlowFailure(errors.Join(errors.New("context"), err)))
	assert.True(t, IsTerminalFlowFailure(fmt.Errorf("%w: unsupported extension", ErrUpgradeFlowFailed)))
	assert.False(t, IsTerminalFlowFailure(fmt.Errorf("%w: waiting", ErrUpgradeFlowPending)))
	assert.False(t, IsTerminalFlowFailure(nil))
	assert.False(t, errors.Is(err, ErrUpgradeFlowFailed), "the rebuild sentinel is independent of ErrUpgradeFlowFailed")
	assert.False(t, errors.Is(fmt.Errorf("%w: unsupported extension", ErrUpgradeFlowFailed), ErrCandidateInvalid))

	report := ReportFromError(err)
	assert.Equal(t, string(Failed), report.Phase)
	assert.Equal(t, ReasonCandidateRebuildRequired, report.Reason)
	assert.Equal(t, reportMessageCandidateRebuildRequired, report.Message)
	assert.False(t, report.Retry)
}

func TestAsCandidateInvalidClassifiesOnlyTerminalFailures(t *testing.T) {
	terminal := fmt.Errorf("%w: source role %q is not declared", ErrUpgradeFlowFailed, "intruder")
	classified := AsCandidateInvalid(terminal)
	require.ErrorIs(t, classified, ErrCandidateInvalid)
	require.ErrorIs(t, classified, terminal, "the original evidence is preserved")

	already := fmt.Errorf("%w: drift", ErrCandidateInvalid)
	assert.Same(t, already, AsCandidateInvalid(already))
	pending := fmt.Errorf("%w: waiting", ErrUpgradeFlowPending)
	assert.Same(t, pending, AsCandidateInvalid(pending), "transient errors stay retryable")
	transient := errors.New("connection reset")
	assert.Same(t, transient, AsCandidateInvalid(transient))
	assert.NoError(t, AsCandidateInvalid(nil))
}

func TestMarkBlueGreenRebuildRequired(t *testing.T) {
	invalid := fmt.Errorf("%w: slot invalidated", ErrCandidateInvalid)

	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{}
	require.True(t, MarkBlueGreenRebuildRequired(status, invalid))
	require.NotNil(t, status.RebuildRequired)
	assert.Equal(t, invalid.Error(), *status.RebuildRequired)
	assert.False(t, MarkBlueGreenRebuildRequired(status, fmt.Errorf("%w: later", ErrCandidateInvalid)), "the first reason is write-once")
	assert.Equal(t, invalid.Error(), *status.RebuildRequired)

	assert.False(t, MarkBlueGreenRebuildRequired(nil, invalid))
	ordinary := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{}
	assert.False(t, MarkBlueGreenRebuildRequired(ordinary, fmt.Errorf("%w: unsupported", ErrUpgradeFlowFailed)))
	assert.Nil(t, ordinary.RebuildRequired)
	committed := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{CommitReceipt: &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1}}
	assert.False(t, MarkBlueGreenRebuildRequired(committed, invalid), "committed attempts fail forward")
	assert.Nil(t, committed.RebuildRequired)
}

func TestMarkBlueGreenRebuildRequiredBoundsTheReason(t *testing.T) {
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{}
	require.True(t, MarkBlueGreenRebuildRequired(status, fmt.Errorf("%w: %s", ErrCandidateInvalid, strings.Repeat("x", 4096))))
	assert.LessOrEqual(t, len(*status.RebuildRequired), maxRebuildReasonLength)
	assert.True(t, strings.HasSuffix(*status.RebuildRequired, "…"))
}

func TestCutoverStageCommitted(t *testing.T) {
	now := metav1.Now()
	green := &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", UID: "green-uid"}}
	assert.False(t, CutoverStageOfStatus(nil).Committed())
	assert.False(t, CutoverStageOfStatus(&platformv1alpha1.PostgresBlueGreenUpgradeStatus{Switchover: &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1, StartedAt: &now}}).Committed())
	assert.True(t, CutoverStageOfStatus(&platformv1alpha1.PostgresBlueGreenUpgradeStatus{CommitReceipt: &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1}}).Committed())
	handedOff := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{Green: green,
		CommitReceipt: &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1, CommittedAt: &now, Target: &green.Ref, CanonicalConfigMap: &corev1.ObjectReference{Name: "access", UID: "cm"}},
		Switchover:    &platformv1alpha1.BlueGreenSwitchoverStatus{TargetAvailableAt: &now, EndpointPublicationEventRef: &corev1.ObjectReference{Name: "event"}}}
	assert.True(t, CutoverStageOfStatus(handedOff).Committed())
}

func TestRebuildClassificationIsWriteOnceAndPrecommitOnly(t *testing.T) {
	current := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt"}
	classified := current.DeepCopy()
	classified.RebuildRequired = new("slot invalidated")
	require.NoError(t, validateExistingBlueGreenProgress("attempt", current, classified))
	require.NoError(t, validateExistingBlueGreenProgress("attempt", classified, classified.DeepCopy()))

	removed := classified.DeepCopy()
	removed.RebuildRequired = nil
	assert.Error(t, validateExistingBlueGreenProgress("attempt", classified, removed))
	rewritten := classified.DeepCopy()
	rewritten.RebuildRequired = new("another reason")
	assert.Error(t, validateExistingBlueGreenProgress("attempt", classified, rewritten))

	committed := current.DeepCopy()
	committed.CommitReceipt = &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1}
	committedClassified := committed.DeepCopy()
	committedClassified.RebuildRequired = new("late drift")
	assert.Error(t, validateExistingBlueGreenProgress("attempt", committed, committedClassified))
}
