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
	"slices"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BlueGreenCancelRequestedInSpec reads the current cancellation gate.
func BlueGreenCancelRequestedInSpec(spec *platformv1alpha1.PostgresClusterSpec) bool {
	return spec != nil && spec.PostgresMajorUpgradeConfig != nil && spec.PostgresMajorUpgradeConfig.BlueGreen != nil &&
		spec.PostgresMajorUpgradeConfig.BlueGreen.Cancel != nil && *spec.PostgresMajorUpgradeConfig.BlueGreen.Cancel
}

// BlueGreenCancellationPending permits an existing attempt to consume cancel
// before ordinary class/configuration prerequisites can block recovery.
func BlueGreenCancellationPending(spec *platformv1alpha1.PostgresClusterSpec, entry platformv1alpha1.PostgresMajorUpgradeStatus) bool {
	return BlueGreenCancelRequestedInSpec(spec) && entry.BlueGreen != nil && entry.BlueGreen.AttemptID != "" &&
		entry.Strategy != nil && *entry.Strategy == MajorUpgradeFlowBlueGreen &&
		(entry.Phase == nil || *entry.Phase != string(Completed) && *entry.Phase != string(Cancelled)) &&
		!CutoverStageOfStatus(entry.BlueGreen).Committed()
}

// BlueGreenCancellationRequested reports whether cancellation is latched.
func BlueGreenCancellationRequested(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
	return status != nil && status.Gates != nil && status.Gates.Cancellation != nil &&
		status.Gates.Cancellation.RequestedAt != nil
}

// BlueGreenCancelling reports a latched cancellation that still owns the
// attempt. A commit receipt means only fail-forward recovery remains.
func BlueGreenCancelling(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
	return BlueGreenCancellationRequested(status) && status.CommitReceipt == nil
}

// BlueGreenCancellationApplies reports whether this pass must cancel: a
// latched cancellation always resumes, and a new request applies only to an
// attempt that has neither committed nor reached a terminal phase.
func BlueGreenCancellationApplies(intent Intent) bool {
	if intent.Strategy != MajorUpgradeFlowBlueGreen {
		return false
	}
	for i := len(intent.State) - 1; i >= 0; i-- {
		entry := intent.State[i]
		if !MatchesIntent(entry, intent) {
			continue
		}
		if BlueGreenCancelling(entry.BlueGreen) {
			return true
		}
		terminal := entry.Phase != nil && slices.Contains([]Status{Completed, Cancelled}, Status(*entry.Phase))
		return intent.CancelRequested && !terminal && !CutoverStageOfStatus(entry.BlueGreen).Committed()
	}
	return intent.CancelRequested
}

// BlueGreenGateConditions projects whether the cancel, retry, and cleanup
// gates can be consumed, and whether cleanup is required, from the attempt's
// durable state. The conditions explain availability; gates express permission.
func BlueGreenGateConditions(entry platformv1alpha1.PostgresMajorUpgradeStatus) []metav1.Condition {
	gate := gateState{entry: entry, committed: CutoverStageOfStatus(entry.BlueGreen).Committed(),
		cancelling: BlueGreenCancellationRequested(entry.BlueGreen)}
	return []metav1.Condition{gate.cancellation(), gate.retry(), gate.cleanup(), gate.cleanupRequired()}
}

type gateState struct {
	entry      platformv1alpha1.PostgresMajorUpgradeStatus
	committed  bool
	cancelling bool
}

func (g gateState) cancellation() metav1.Condition {
	switch {
	case g.committed:
		return condition(ConditionCancellationAvailable, false, ReasonCommittedFailForward, messageCommittedFailForward)
	case g.cancelling:
		return condition(ConditionCancellationAvailable, false, ReasonCancellationRequested, messageCancellationRequested)
	case g.phaseIs(Completed, Cancelled):
		return condition(ConditionCancellationAvailable, false, ReasonAttemptTerminal, messageAttemptTerminal)
	}
	return condition(ConditionCancellationAvailable, true, ReasonBeforeEndpointCommit, messageCancellationAvailable)
}

func (g gateState) retry() metav1.Condition {
	switch {
	case g.committed:
		return condition(ConditionRetryAvailable, false, ReasonCommittedFailForward, messageCommittedFailForward)
	case g.cancelling:
		return condition(ConditionRetryAvailable, false, ReasonCancellationRequested, messageCancellationRequested)
	case BlueGreenRebuildRequired(g.entry.BlueGreen):
		return condition(ConditionRetryAvailable, false, ReasonCandidateRebuildRequired, reportMessageCandidateRebuildRequired)
	case !g.phaseIs(Failed):
		return condition(ConditionRetryAvailable, false, ReasonNoTerminalFailure, messageNoTerminalFailure)
	}
	return condition(ConditionRetryAvailable, true, ReasonRetryAnnotationRequired, messageRetryAvailable)
}

func (g gateState) cleanup() metav1.Condition {
	status := g.entry.BlueGreen
	switch {
	case IsBlueGreenAttemptCleaned(g.entry):
		return condition(ConditionCleanupAvailable, false, ReasonCleaned, messageCleaned)
	case g.phaseIs(Cancelled) && g.cancelling && status.Gates.Cancellation.CompletedAt != nil:
		return condition(ConditionCleanupAvailable, true, ReasonCancelledEnvironmentRetained, messageCancelledEnvironmentRetained)
	case g.phaseIs(Completed) && status != nil && status.Recoverability != nil &&
		status.Recoverability.BackupVerifiedAt != nil && status.Recoverability.WALArchiveBaselineVerifiedAt != nil:
		return condition(ConditionCleanupAvailable, true, ReasonCompletedEnvironmentRetained, messageCompletedEnvironmentRetained)
	}
	return condition(ConditionCleanupAvailable, false, ReasonCleanupNotReady, messageCleanupNotReady)
}

// cleanupRequired reports a terminal attempt whose non-authoritative
// environment remains until cleanup.
func (g gateState) cleanupRequired() metav1.Condition {
	status := g.entry.BlueGreen
	retained := status != nil && !IsBlueGreenAttemptCleaned(g.entry) &&
		(g.phaseIs(Cancelled) && status.Green != nil || g.phaseIs(Completed) && status.Blue != nil)
	if retained {
		return condition(ConditionCleanupRequired, true, ReasonEnvironmentRetained, messageEnvironmentRetained)
	}
	return condition(ConditionCleanupRequired, false, ReasonNoRetainedEnvironment, messageNoRetainedEnvironment)
}

func (g gateState) phaseIs(phases ...Status) bool {
	return g.entry.Phase != nil && slices.Contains(phases, Status(*g.entry.Phase))
}

func condition(conditionType string, available bool, reason, message string) metav1.Condition {
	status := metav1.ConditionFalse
	if available {
		status = metav1.ConditionTrue
	}
	return NewBlueGreenCondition(conditionType, status, reason, message)
}
