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

package cnpg

import (
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/recoverytypes"
)

// recoveryBackend implements the core RecoveryBackend port for the CNPG
// provisioner. It encodes CNPG's recovery capabilities so those provider-specific
// rules live in the infrastructure layer instead of leaking into core as domain
// logic. It satisfies the port structurally, so this package never imports core.
type recoveryBackend struct{}

// NewRecoveryBackend returns a CNPG recovery backend that validates recovery plans
// against CloudNativePG's capabilities. Callers bind it to the core.RecoveryBackend
// port at the injection site. It holds no client: capability validation is a pure
// function of the plan, so the same instance is safe to share between the admission
// webhook and the reconciler.
func NewRecoveryBackend() recoveryBackend {
	return recoveryBackend{}
}

// ValidatePlan reports every way CNPG cannot execute the given recovery plan.
func (recoveryBackend) ValidatePlan(plan recoverytypes.RecoveryPlan) []recoverytypes.CapabilityViolation {
	var violations []recoverytypes.CapabilityViolation

	// An object-store source auto-selects the base backup from the archive, and CNPG only auto-detects
	// it for time/lsn targets. With xid/name/immediate there is no way to pin a backupID, so CNPG falls
	// back to the latest backup, which may precede the target and silently start recovery from the
	// wrong base. Reject those until backupID selection is supported. A volume-snapshot base is
	// unambiguous, so this restriction applies only to the object-store source.
	if plan.Source == recoverytypes.SourceObjectStorage && plan.HasTarget {
		switch plan.TargetKind {
		case recoverytypes.TargetXID, recoverytypes.TargetName, recoverytypes.TargetImmediate:
			violations = append(violations, recoverytypes.CapabilityViolation{
				Field:   "spec.bootstrapFrom.recoveryTarget",
				Message: "target types xid, name, and immediate are not supported for an objectStorage source (CNPG can only auto-select the base backup for types time or lsn); use type time or lsn, or restore from a volumeSnapshot base",
			})
		}
	}

	// Any source that reads WAL (and, for objectStorage, the base backup) from an object store needs
	// the class to define the object store so CNPG can resolve the bucket path and credentials.
	if plan.Source.ReadsObjectStore() && !plan.ClassProvidesObjectStore {
		field := "spec.bootstrapFrom.objectStorage"
		if plan.Source == recoverytypes.SourceVolumeSnapshotWithWAL {
			field = "spec.bootstrapFrom.volumeSnapshot.walArchive"
		}
		violations = append(violations, recoverytypes.CapabilityViolation{
			Field:   field,
			Message: "restoring from an object storage archive requires cnpg.backup.barmanObjectStore to be configured in the referenced PostgresClusterClass",
		})
	}

	return violations
}
