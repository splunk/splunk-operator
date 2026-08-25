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
package core

import (
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/recoverytypes"
)

// RecoveryBackend is the secondary (driven) port through which the operator asks
// the target provisioner whether it can execute a provider-neutral RecoveryPlan.
// It exists to keep provisioner-specific capability rules — e.g. which recovery
// target kinds a source supports, or that an object-store source needs the class
// to define an object store — out of core as domain logic. The concrete adapter
// (infrastructure/cnpg) encodes CNPG's capabilities; another provisioner could
// accept source/target combinations CNPG rejects. Defined here, next to its
// consumer (ValidateRecoveryCapabilities), while the neutral value objects it
// exchanges live in the leaf recoverytypes package, mirroring BackupBackend.
type RecoveryBackend interface {
	// ValidatePlan reports every way the backend cannot execute plan. An empty
	// result means the backend accepts the plan. It is a pure capability check —
	// no cluster state is read — so it is safe to call at admission time.
	ValidatePlan(plan recoverytypes.RecoveryPlan) []recoverytypes.CapabilityViolation
}

// deriveRecoveryPlan builds the provider-neutral RecoveryPlan from the immutable
// bootstrapFrom spec and the referenced class. It returns (plan, true) only for a
// well-formed single-source restore request; it returns (_, false) when there is
// no bootstrapFrom or the source is malformed (exactly-one-source is enforced by
// the structural checks in validateBootstrapFrom, so a malformed request has
// already produced an actionable error and needs no capability check).
func deriveRecoveryPlan(class *platformv1alpha1.PostgresClusterClass, cluster *platformv1alpha1.PostgresCluster) (recoverytypes.RecoveryPlan, bool) {
	b := cluster.Spec.BootstrapFrom
	if b == nil {
		return recoverytypes.RecoveryPlan{}, false
	}

	var source recoverytypes.SourceKind
	switch {
	case b.VolumeSnapshot != nil && b.ObjectStorage == nil:
		if b.VolumeSnapshot.WalArchive != nil {
			source = recoverytypes.SourceVolumeSnapshotWithWAL
		} else {
			source = recoverytypes.SourceVolumeSnapshot
		}
	case b.ObjectStorage != nil && b.VolumeSnapshot == nil:
		source = recoverytypes.SourceObjectStorage
	default:
		// Zero or both sources: structural validation owns this error.
		return recoverytypes.RecoveryPlan{}, false
	}

	plan := recoverytypes.RecoveryPlan{
		Source:                   source,
		ClassProvidesObjectStore: classProvidesObjectStore(class),
	}
	if b.RecoveryTarget != nil {
		plan.HasTarget = true
		plan.TargetKind = recoverytypes.TargetKind(b.RecoveryTarget.Type)
	}
	return plan, true
}

// classProvidesObjectStore reports whether the referenced class defines an object
// store the backend can resolve WAL/base-backup access from. Core reports the
// fact; the backend decides whether a given plan requires it.
func classProvidesObjectStore(class *platformv1alpha1.PostgresClusterClass) bool {
	return class.Spec.CNPG != nil &&
		class.Spec.CNPG.Backup != nil &&
		class.Spec.CNPG.Backup.BarmanObjectStore != nil
}

// ValidateRecoveryCapabilities routes the provider-specific capability rules for a
// recovery request through the RecoveryBackend port and maps any violations onto
// ConfigValidationErrors. It is a no-op when there is no (well-formed) restore
// request. Structural and PostgreSQL value-format checks are not delegated — they
// are provisioner-independent and stay in validateBootstrapFrom. Both composition
// roots that gate recovery (the admission webhook and the reconciler) call this
// with a concrete backend, mirroring how BackupBackend is injected at runtime.
func ValidateRecoveryCapabilities(backend RecoveryBackend, class *platformv1alpha1.PostgresClusterClass, cluster *platformv1alpha1.PostgresCluster) []ConfigValidationError {
	if backend == nil {
		return nil
	}
	plan, ok := deriveRecoveryPlan(class, cluster)
	if !ok {
		return nil
	}
	violations := backend.ValidatePlan(plan)
	if len(violations) == 0 {
		return nil
	}
	errs := make([]ConfigValidationError, 0, len(violations))
	for _, v := range violations {
		errs = append(errs, ConfigValidationError{Field: v.Field, Message: v.Message})
	}
	return errs
}
