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
	"slices"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

// BlueGreenCleanupRequestedInSpec reads the current cleanup permission.
func BlueGreenCleanupRequestedInSpec(spec *platformv1alpha1.PostgresClusterSpec) bool {
	return spec != nil && spec.PostgresMajorUpgradeConfig != nil && spec.PostgresMajorUpgradeConfig.BlueGreen != nil &&
		ptr.Deref(spec.PostgresMajorUpgradeConfig.BlueGreen.Cleanup, false)
}

// BlueGreenCleanupPending selects terminal retirement before ordinary prerequisites.
func BlueGreenCleanupPending(spec *platformv1alpha1.PostgresClusterSpec, entry platformv1alpha1.PostgresMajorUpgradeStatus) bool {
	bg := entry.BlueGreen
	if bg == nil || bg.AttemptID == "" || IsBlueGreenAttemptCleaned(entry) || entry.Strategy == nil ||
		*entry.Strategy != MajorUpgradeFlowBlueGreen || entry.Phase == nil ||
		!slices.Contains([]Status{Completed, Cancelled}, Status(*entry.Phase)) {
		return false
	}
	if bg.Gates != nil && bg.Gates.Cleanup != nil || bg.Cleanup != nil &&
		(bg.Cleanup.State == platformv1alpha1.BlueGreenCleanupStateCleaning || bg.Cleanup.State == platformv1alpha1.BlueGreenCleanupStateCleanupFailed) {
		return true
	}
	return BlueGreenCleanupRequestedInSpec(spec)
}

// ClusterCleanupPending selects only the latest terminal attempt.
func ClusterCleanupPending(cluster *platformv1alpha1.PostgresCluster) bool {
	if cluster == nil {
		return false
	}
	entry := LatestAttempt(cluster.Status.PostgresMajorUpgradeStatus)
	return entry != nil && BlueGreenCleanupPending(&cluster.Spec, *entry)
}

// BlueGreenCleanupEligible requires terminal production safety before retirement.
func BlueGreenCleanupEligible(entry platformv1alpha1.PostgresMajorUpgradeStatus) bool {
	bg := entry.BlueGreen
	if bg == nil || bg.AttemptID == "" || entry.Phase == nil || entry.Strategy == nil || *entry.Strategy != MajorUpgradeFlowBlueGreen {
		return false
	}
	switch Status(*entry.Phase) {
	case Completed:
		c := bg.Completion
		return CutoverStageOfStatus(bg) == CutoverHandedOff && BlueGreenRecoverable(bg) && c != nil &&
			c.StartedAt != nil && c.SourceStoppedAt != nil && c.ReplicationRemovedAt != nil &&
			c.SourceCredentialsRetiredAt != nil && c.CredentialsRemovedAt != nil
	case Cancelled:
		if bg.Gates == nil || bg.Gates.Cancellation == nil || CutoverStageOfStatus(bg).Committed() {
			return false
		}
		c := bg.Gates.Cancellation
		return c.RequestedAt != nil && c.SourceAccessRestoredAt != nil && c.ReplicationRemovedAt != nil &&
			c.CredentialsRemovedAt != nil && c.GreenFencedAt != nil && c.CompletedAt != nil
	}
	return false
}

// BlueGreenRetirementEnvironments selects authoritative and disposable identities.
func BlueGreenRetirementEnvironments(entry platformv1alpha1.PostgresMajorUpgradeStatus) (*corev1.ObjectReference, *corev1.ObjectReference, error) {
	if !BlueGreenCleanupEligible(entry) {
		return nil, nil, fmt.Errorf("blue/green cleanup requires completed production or cancellation receipts")
	}
	authority, retired := entry.BlueGreen.Blue, entry.BlueGreen.Green
	if Status(*entry.Phase) == Completed {
		authority, retired = entry.BlueGreen.Green, entry.BlueGreen.Blue
		if retired == nil {
			return nil, nil, fmt.Errorf("completed blue/green cleanup requires the source identity")
		}
	}
	if retired == nil {
		if authority == nil {
			return nil, nil, nil // Cancellation may precede source/candidate observation.
		}
		return authority.Ref.DeepCopy(), nil, nil
	}
	if authority == nil || !retirementReferenceValid(authority.Ref) {
		return nil, nil, fmt.Errorf("blue/green cleanup requires the authoritative environment identity")
	}
	if !retirementReferenceValid(retired.Ref) || retired.Ref.UID == authority.Ref.UID ||
		retired.Ref.Namespace == authority.Ref.Namespace && retired.Ref.Name == authority.Ref.Name {
		return nil, nil, fmt.Errorf("blue/green retirement must differ from the authoritative environment")
	}
	return authority.Ref.DeepCopy(), retired.Ref.DeepCopy(), nil
}

// RetirementAcknowledged checks a child proof against the complete frozen request.
func RetirementAcknowledged(parentUID types.UID, attemptID string, cleanup *platformv1alpha1.BlueGreenCleanupStatus,
	row platformv1alpha1.BlueGreenRetirementAcknowledgementStatus, child *platformv1alpha1.PostgresDatabase) bool {
	if parentUID == "" || attemptID == "" || cleanup == nil || cleanup.EnvironmentRef == nil ||
		cleanup.RetirementGeneration == nil || *cleanup.RetirementGeneration < 1 || child == nil ||
		row.DatabaseName == "" || row.PostgresDatabaseUID == "" || string(child.UID) != row.PostgresDatabaseUID || child.Name != row.PostgresDatabaseName ||
		child.Namespace != cleanup.EnvironmentRef.Namespace || row.PostgresDatabaseGeneration < 1 ||
		child.Generation != row.PostgresDatabaseGeneration || row.PostgresDatabaseAbsentAt != nil {
		return false
	}
	proof := child.Status.EnvironmentRetirement
	if proof == nil || proof.ParentUID != parentUID || proof.AttemptID != attemptID ||
		proof.RetirementGeneration != *cleanup.RetirementGeneration || proof.ObservedDatabaseGeneration != child.Generation ||
		!sameRetirementReference(proof.EnvironmentRef, *cleanup.EnvironmentRef) {
		return false
	}
	var found bool
	for _, database := range proof.Databases {
		if database.DatabaseName == row.DatabaseName {
			if found || database.AcknowledgedAt == nil || !sameRetirementResources(row.Resources, database.Resources) {
				return false
			}
			found = true
		}
	}
	return found
}

// validateCleanupProgress requires a latched, terminal, identity-bound retirement.
func validateCleanupProgress(intent Intent, progress Progress) error {
	bg := progress.BlueGreen
	if bg == nil || bg.Cleanup == nil && (bg.Gates == nil || bg.Gates.Cleanup == nil) {
		return nil
	}
	c := bg.Cleanup
	if c != nil && c.State == platformv1alpha1.BlueGreenCleanupStateRetained && (bg.Gates == nil || bg.Gates.Cleanup == nil) &&
		c.StartedAt == nil && c.CompletedAt == nil && c.EnvironmentRef == nil && c.RetirementGeneration == nil && len(c.RetirementAcknowledgements) == 0 && len(c.RetiredResources) == 0 {
		return nil
	}
	if c == nil || bg.Gates == nil || bg.Gates.Cleanup == nil || bg.Gates.Cleanup.Generation < 1 ||
		bg.Gates.Cleanup.ConsumedAt == nil || c.StartedAt == nil || c.RetirementGeneration == nil || *c.RetirementGeneration < 1 {
		return fmt.Errorf("blue/green cleanup requires a consumed gate, start time, and retirement generation")
	}
	entry := platformv1alpha1.PostgresMajorUpgradeStatus{Phase: new(progress.Report.Phase), Strategy: &intent.Strategy, BlueGreen: bg}
	_, environment, err := BlueGreenRetirementEnvironments(entry)
	if err != nil {
		return err
	}
	if !sameObjectReference(environment, c.EnvironmentRef) || environment == nil && len(c.RetirementAcknowledgements) != 0 {
		return fmt.Errorf("blue/green cleanup cannot retire another environment")
	}
	if !distinctRetirementResources(c.RetiredResources) {
		return fmt.Errorf("retired resources require distinct exact identities")
	}
	if c.CompletedAt != nil && c.State != platformv1alpha1.BlueGreenCleanupStateCleaned ||
		c.State == platformv1alpha1.BlueGreenCleanupStateCleaned && c.CompletedAt == nil {
		return fmt.Errorf("blue/green cleanup completion requires Cleaned and a completion time")
	}
	if !slices.Contains([]platformv1alpha1.BlueGreenCleanupState{
		platformv1alpha1.BlueGreenCleanupStateCleaning, platformv1alpha1.BlueGreenCleanupStateCleanupFailed, platformv1alpha1.BlueGreenCleanupStateCleaned,
	}, c.State) || c.State == platformv1alpha1.BlueGreenCleanupStateCleaned && c.Failure != nil {
		return fmt.Errorf("blue/green cleanup cannot retain a consumed gate or complete with a failure")
	}
	participants := make(map[[3]string]bool, len(c.RetirementAcknowledgements))
	for _, row := range c.RetirementAcknowledgements {
		key := [3]string{row.PostgresDatabaseName, row.PostgresDatabaseUID, row.DatabaseName}
		if row.PostgresDatabaseName == "" || row.PostgresDatabaseUID == "" || row.DatabaseName == "" ||
			row.PostgresDatabaseGeneration < 0 || row.AcknowledgedAt != nil &&
			(row.PostgresDatabaseGeneration == 0 || row.PostgresDatabaseAbsentAt != nil) ||
			participants[key] || !distinctRetirementResources(row.Resources) {
			return fmt.Errorf("blue/green retirement requires distinct exact resource and child identities")
		}
		participants[key] = true
		for _, ref := range row.Resources {
			if environment == nil || ref.Namespace != environment.Namespace || ref.APIVersion != environment.APIVersion || ref.Kind != "Database" {
				return fmt.Errorf("retirement resources must be Databases in the retired environment's namespace")
			}
		}
		if c.State == platformv1alpha1.BlueGreenCleanupStateCleaned && row.AcknowledgedAt == nil && row.PostgresDatabaseAbsentAt == nil {
			return fmt.Errorf("blue/green cleanup requires every retirement acknowledgement")
		}
	}
	return nil
}

// preserveCleanupProgress freezes authorization and each retirement revision.
func preserveCleanupProgress(previous, next *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	if previous.Gates != nil && previous.Gates.Cleanup != nil &&
		(next.Gates == nil || !equality.Semantic.DeepEqual(previous.Gates.Cleanup, next.Gates.Cleanup)) {
		return fmt.Errorf("blue/green progress cannot change consumed cleanup permission")
	}
	old, proposed := previous.Cleanup, next.Cleanup
	if old != nil && old.State == platformv1alpha1.BlueGreenCleanupStateCleaned && !equality.Semantic.DeepEqual(old, proposed) {
		return fmt.Errorf("blue/green progress cannot rewrite completed cleanup")
	}
	if old == nil || old.StartedAt == nil {
		return nil
	}
	if proposed != nil {
		for _, ref := range old.RetiredResources {
			if !slices.ContainsFunc(proposed.RetiredResources, func(next corev1.ObjectReference) bool { return sameRetirementReference(ref, next) }) {
				return fmt.Errorf("blue/green progress cannot discard retired resource identities")
			}
		}
	}
	if proposed == nil || !sameOptionalTime(old.StartedAt, proposed.StartedAt) ||
		!sameObjectReference(old.EnvironmentRef, proposed.EnvironmentRef) || old.RetirementGeneration == nil ||
		proposed.RetirementGeneration == nil || *proposed.RetirementGeneration < *old.RetirementGeneration {
		return fmt.Errorf("blue/green progress cannot remove or replace cleanup identity")
	}
	if ptr.Equal(old.RetirementGeneration, proposed.RetirementGeneration) {
		if !equality.Semantic.DeepDerivative(old.RetirementAcknowledgements, proposed.RetirementAcknowledgements) ||
			!equality.Semantic.DeepEqual(RetirementRoster(old), RetirementRoster(proposed)) {
			return fmt.Errorf("blue/green retirement inventory requires a new generation")
		}
	} else if slices.ContainsFunc(proposed.RetirementAcknowledgements, func(row platformv1alpha1.BlueGreenRetirementAcknowledgementStatus) bool {
		return row.AcknowledgedAt != nil || row.PostgresDatabaseAbsentAt != nil
	}) {
		return fmt.Errorf("a new retirement generation must clear previous proofs")
	}
	return nil
}

// RetirementRoster excludes receipts when comparing the frozen participant set.
func RetirementRoster(cleanup *platformv1alpha1.BlueGreenCleanupStatus) []platformv1alpha1.BlueGreenRetirementAcknowledgementStatus {
	if cleanup == nil {
		return nil
	}
	rows := slices.Clone(cleanup.RetirementAcknowledgements)
	for i := range rows {
		rows[i].AcknowledgedAt = nil
		rows[i].PostgresDatabaseAbsentAt = nil
	}
	return rows
}

func retirementReferenceValid(ref corev1.ObjectReference) bool {
	return ref.APIVersion != "" && ref.Kind != "" && ref.Namespace != "" && ref.Name != "" && ref.UID != ""
}

func sameRetirementReference(left, right corev1.ObjectReference) bool {
	return retirementReferenceValid(left) && retirementReferenceValid(right) && left.APIVersion == right.APIVersion &&
		left.Kind == right.Kind && left.Namespace == right.Namespace && left.Name == right.Name && left.UID == right.UID
}

// distinctRetirementResources requires complete identities without duplicate entries.
func distinctRetirementResources(refs []corev1.ObjectReference) bool {
	return sameRetirementResources(refs, refs)
}

// sameRetirementResources compares identity sets independently of order and resource version.
func sameRetirementResources(left, right []corev1.ObjectReference) bool {
	if len(left) != len(right) {
		return false
	}
	identities := make(map[corev1.ObjectReference]struct{}, len(left))
	for _, ref := range left {
		if !retirementReferenceValid(ref) {
			return false
		}
		ref.ResourceVersion, ref.FieldPath = "", ""
		identities[ref] = struct{}{}
	}
	if len(identities) != len(left) {
		return false
	}
	for _, ref := range right {
		ref.ResourceVersion, ref.FieldPath = "", ""
		if _, ok := identities[ref]; !ok {
			return false
		}
		delete(identities, ref)
	}
	return len(identities) == 0
}
