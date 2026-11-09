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
	"fmt"
	"math"
	"reflect"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ValidateBlueGreenProgress defines the domain policy for replacing durable
// attempt state. Attempt identity, consumed gates, snapshots, and completed
// receipts are monotonic; a new interruption window requires source restoration
// and a newer explicit retry. Persistence adapters call this policy before I/O.
// A nil BlueGreen payload is report-only and preserves the current attempt.
func ValidateBlueGreenProgress(intent Intent, progress Progress) error {
	if intent.Strategy != MajorUpgradeFlowBlueGreen {
		return nil
	}
	if intent.AttemptID == "" {
		return fmt.Errorf("blue/green major upgrade progress requires an attempt ID")
	}
	if progress.BlueGreen != nil && progress.BlueGreen.AttemptID != intent.AttemptID {
		return fmt.Errorf("blue/green progress attempt ID %q does not match intent attempt ID %q", progress.BlueGreen.AttemptID, intent.AttemptID)
	}
	if err := validateSourcePreparationReceipt(intent.AttemptID, progress.BlueGreen); err != nil {
		return err
	}

	for i := len(intent.State) - 1; i >= 0; i-- {
		current := intent.State[i]
		if !MatchesIntent(current, intent) || current.BlueGreen == nil {
			continue
		}
		return validateExistingBlueGreenProgress(intent.AttemptID, current.BlueGreen, progress.BlueGreen)
	}
	return nil
}

func validateSourcePreparationReceipt(attemptID string, proposed *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	if proposed != nil && proposed.SourcePreparation != nil &&
		(proposed.SourcePreparation.CompletedAt == nil || proposed.SourceConfigurationFingerprint == nil) {
		return fmt.Errorf("blue/green source-preparation receipt requires completion time and configuration baseline for attempt %q", attemptID)
	}
	return nil
}

func validateExistingBlueGreenProgress(attemptID string, current, proposed *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	// An omitted payload is a report-only update, not a replacement of the
	// durable attempt state. Persistence preserves it unchanged.
	if proposed == nil {
		return nil
	}
	if previous := current.SourcePreparation; previous != nil && previous.CompletedAt != nil {
		receipt := proposed.SourcePreparation
		if receipt == nil || receipt.CompletedAt == nil || !previous.CompletedAt.Time.Equal(receipt.CompletedAt.Time) {
			return fmt.Errorf("blue/green progress cannot change source-preparation receipt for attempt %q", attemptID)
		}
		if current.SourceConfigurationFingerprint == nil || proposed.SourceConfigurationFingerprint == nil ||
			*current.SourceConfigurationFingerprint != *proposed.SourceConfigurationFingerprint {
			return fmt.Errorf("blue/green progress cannot change source configuration baseline for attempt %q", attemptID)
		}
	}
	if current.CommitReceipt != nil {
		if proposed.CommitReceipt == nil {
			return fmt.Errorf("blue/green progress cannot remove endpoint commit receipt for attempt %q", attemptID)
		}
		if proposed.CommitReceipt.Sequence != current.CommitReceipt.Sequence ||
			!sameObjectReference(current.CommitReceipt.CanonicalConfigMap, proposed.CommitReceipt.CanonicalConfigMap) ||
			!sameObjectReference(current.CommitReceipt.Target, proposed.CommitReceipt.Target) ||
			!sameOptionalTime(current.CommitReceipt.CommittedAt, proposed.CommitReceipt.CommittedAt) {
			return fmt.Errorf("blue/green progress cannot rewrite endpoint commit receipt for attempt %q", attemptID)
		}
	}
	if err := validateCanonicalRecovery(attemptID, current, proposed); err != nil {
		return err
	}
	if current.Gates != nil && current.Gates.Switchover != nil {
		if proposed.Gates == nil || proposed.Gates.Switchover == nil ||
			current.Gates.Switchover.Generation != proposed.Gates.Switchover.Generation ||
			!sameOptionalTime(current.Gates.Switchover.ConsumedAt, proposed.Gates.Switchover.ConsumedAt) {
			return fmt.Errorf("blue/green progress cannot change consumed switchover gate for attempt %q", attemptID)
		}
	}
	if current.Switchover != nil {
		if proposed.Switchover == nil {
			return fmt.Errorf("blue/green progress cannot remove switchover state for attempt %q", attemptID)
		}
		previous, next := current.Switchover, proposed.Switchover
		if err := validateSwitchoverSnapshots(attemptID, previous, next); err != nil {
			return err
		}
		if proposed.CommitReceipt != nil && next.SourceFenceRestoredAt != nil {
			return fmt.Errorf("blue/green progress cannot restore blue after endpoint commit for attempt %q", attemptID)
		}
		if previous.Sequence != next.Sequence {
			if proposed.CommitReceipt != nil {
				return fmt.Errorf("blue/green progress cannot start another interruption window after endpoint commit for attempt %q", attemptID)
			}
			return validateNextSwitchoverWindow(attemptID, current, next)
		}
		if previous.RetryRequestedAt != nil && !sameOptionalTime(previous.RetryRequestedAt, next.RetryRequestedAt) {
			return fmt.Errorf("blue/green progress cannot change consumed retry request for attempt %q", attemptID)
		}
		if previous.LatchedTimeout != nil && (next.LatchedTimeout == nil || previous.LatchedTimeout.Duration != next.LatchedTimeout.Duration) {
			return fmt.Errorf("blue/green progress cannot change the latched switchover timeout for attempt %q", attemptID)
		}
		for _, milestone := range SwitchoverMilestones() {
			oldTime, newTime := milestone.Time(previous), milestone.Time(next)
			if previousTimeSet(oldTime) && !sameOptionalTime(oldTime, newTime) {
				return fmt.Errorf("blue/green progress cannot change switchover %s for attempt %q", milestone.Name, attemptID)
			}
		}

		if previous.EndpointPublicationEventRef != nil && !sameObjectReference(previous.EndpointPublicationEventRef, next.EndpointPublicationEventRef) {
			return fmt.Errorf("blue/green progress cannot change endpoint publication Event receipt for attempt %q", attemptID)
		}
		if previous.FinalBlueLSN != nil && (next.FinalBlueLSN == nil || *previous.FinalBlueLSN != *next.FinalBlueLSN) {
			return fmt.Errorf("blue/green progress cannot change final blue LSN for attempt %q", attemptID)
		}
		if previous.SequenceSynchronization != nil {
			if next.SequenceSynchronization == nil {
				return fmt.Errorf("blue/green progress cannot remove sequence synchronization receipt for attempt %q", attemptID)
			}
			for _, milestone := range []struct {
				name string
				old  *metav1.Time
				new  *metav1.Time
			}{
				{"completion time", previous.SequenceSynchronization.CompletedAt, next.SequenceSynchronization.CompletedAt},
				{"verification time", previous.SequenceSynchronization.VerifiedAt, next.SequenceSynchronization.VerifiedAt},
			} {
				if previousTimeSet(milestone.old) && !sameOptionalTime(milestone.old, milestone.new) {
					return fmt.Errorf("blue/green progress cannot change sequence synchronization %s for attempt %q", milestone.name, attemptID)
				}
			}
		}
	}
	return nil
}

func validateNextSwitchoverWindow(attemptID string, current *platformv1alpha1.PostgresBlueGreenUpgradeStatus, next *platformv1alpha1.BlueGreenSwitchoverStatus) error {
	previous := current.Switchover
	if !isNextSwitchoverSequence(previous.Sequence, next.Sequence) || current.CommitReceipt != nil ||
		!isValidRetryTiming(previous, next) ||
		!sameCutoverAccessStatus(previous.BlueGreenCutoverAccessStatus, next.BlueGreenCutoverAccessStatus) {
		return fmt.Errorf("blue/green progress requires verified blue restoration and a newer retry for another switchover window in attempt %q", attemptID)
	}
	pristine := platformv1alpha1.BlueGreenSwitchoverWindow{Sequence: next.Sequence, RetryRequestedAt: next.RetryRequestedAt}
	if !reflect.DeepEqual(next.BlueGreenSwitchoverWindow, pristine) {
		return fmt.Errorf("blue/green progress cannot reuse interruption proofs in a new switchover window for attempt %q", attemptID)
	}

	return nil
}

func isNextSwitchoverSequence(previous, next int64) bool {
	return previous != math.MaxInt64 && next == previous+1
}

func isValidRetryTiming(previous, next *platformv1alpha1.BlueGreenSwitchoverStatus) bool {
	return previous.SourceFenceRestoredAt != nil && next.RetryRequestedAt != nil &&
		next.RetryRequestedAt.After(previous.SourceFenceRestoredAt.Time) &&
		(previous.RetryRequestedAt == nil || next.RetryRequestedAt.After(previous.RetryRequestedAt.Time))
}

func sameCutoverAccessStatus(previous, next platformv1alpha1.BlueGreenCutoverAccessStatus) bool {
	return reflect.DeepEqual(previous, next)
}

func validateSwitchoverSnapshots(attemptID string, previous, next *platformv1alpha1.BlueGreenSwitchoverStatus) error {
	if previous.GreenConfigurationFingerprint != nil && (next.GreenConfigurationFingerprint == nil || *previous.GreenConfigurationFingerprint != *next.GreenConfigurationFingerprint) {
		return fmt.Errorf("blue/green progress cannot change green configuration baseline for attempt %q", attemptID)
	}
	if len(previous.GreenCredentialFingerprints) > 0 {
		if len(previous.GreenCredentialFingerprints) != len(next.GreenCredentialFingerprints) {
			return fmt.Errorf("blue/green progress cannot change green credential inventory for attempt %q", attemptID)
		}
		for i, old := range previous.GreenCredentialFingerprints {
			candidate := next.GreenCredentialFingerprints[i]
			if old.SecretName != candidate.SecretName || old.SecretUID != candidate.SecretUID || old.Fingerprint != candidate.Fingerprint || !sameOptionalTime(old.CapturedAt, candidate.CapturedAt) {
				return fmt.Errorf("blue/green progress cannot change green credential baseline for attempt %q", attemptID)
			}
		}
	}
	for _, snapshot := range []struct {
		previous *[]string
		next     *[]string
	}{{previous.OriginalHBA, next.OriginalHBA}, {previous.GreenOriginalHBA, next.GreenOriginalHBA}} {
		if snapshot.previous != nil && (snapshot.next == nil || !reflect.DeepEqual(*snapshot.previous, *snapshot.next)) {
			return fmt.Errorf("blue/green progress cannot change original HBA snapshot for attempt %q", attemptID)
		}
	}
	if len(previous.OriginalRoleLogins) > 0 && !reflect.DeepEqual(previous.OriginalRoleLogins, next.OriginalRoleLogins) ||
		len(previous.GreenOriginalRoleLogins) > 0 && !reflect.DeepEqual(previous.GreenOriginalRoleLogins, next.GreenOriginalRoleLogins) {
		return fmt.Errorf("blue/green progress cannot change original role login snapshot for attempt %q", attemptID)
	}
	if identity := previous.ControlIdentity; identity != nil {
		if next.ControlIdentity == nil || identity.RoleName != next.ControlIdentity.RoleName ||
			!sameObjectReference(&identity.SecretRef, &next.ControlIdentity.SecretRef) ||
			identity.PreparedAt != nil && !sameOptionalTime(identity.PreparedAt, next.ControlIdentity.PreparedAt) {
			return fmt.Errorf("blue/green progress cannot change control identity for attempt %q", attemptID)
		}
	}
	return nil
}

func validateCanonicalRecovery(attemptID string, current, proposed *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	if current.CanonicalRecovery == nil && (proposed == nil || proposed.CanonicalRecovery == nil) {
		return nil
	}
	if proposed == nil || proposed.CanonicalRecovery == nil || proposed.CommitReceipt == nil || current.CommitReceipt == nil {
		return fmt.Errorf("blue/green progress cannot remove canonical recovery proof for attempt %q", attemptID)
	}
	recovery, receipt := proposed.CanonicalRecovery, proposed.CommitReceipt
	if recovery.CommitSequence != receipt.Sequence || receipt.CanonicalConfigMap == nil || recovery.ConfigMap.Name != receipt.CanonicalConfigMap.Name ||
		recovery.ConfigMap.Namespace != receipt.CanonicalConfigMap.Namespace || recovery.ConfigMap.UID == "" ||
		recovery.EndpointFingerprint == "" || recovery.RecoveredAt == nil || proposed.EndpointPreparation == nil ||
		proposed.EndpointPreparation.EndpointFingerprint == nil || recovery.EndpointFingerprint != *proposed.EndpointPreparation.EndpointFingerprint {
		return fmt.Errorf("blue/green progress has invalid canonical recovery proof for attempt %q", attemptID)
	}
	if previous := current.CanonicalRecovery; previous != nil {
		if previous.ConfigMap.UID == recovery.ConfigMap.UID && !reflect.DeepEqual(previous, recovery) ||
			previous.CommitSequence != recovery.CommitSequence || previous.EndpointFingerprint != recovery.EndpointFingerprint ||
			previous.RecoveredAt != nil && recovery.RecoveredAt.Before(previous.RecoveredAt) {
			return fmt.Errorf("blue/green progress cannot rewrite canonical recovery proof for attempt %q", attemptID)
		}
	}
	return nil
}

func previousTimeSet(value *metav1.Time) bool { return value != nil && !value.IsZero() }

func sameOptionalTime(left, right *metav1.Time) bool {
	if left == nil || left.IsZero() {
		return right == nil || right.IsZero()
	}
	return right != nil && left.Equal(right)
}

func sameObjectReference(left, right *corev1.ObjectReference) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
