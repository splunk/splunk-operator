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

// retryableFailureReasons are reasons attached to retryable reports that signal
// an obstacle blocking forward progress (a prerequisite is missing, state could
// not be read/written, or an unexpected error occurred) rather than healthy
// in-flight progress. Reports carrying these reasons surface as a
// MajorUpgradeRetryableFailure condition so operators can see the upgrade is
// stuck-but-retrying; every other retryable report is normal progress and maps
// to MajorUpgradeProgressing.
//
// "Still running" style waits (ReasonUpgradeFlowPending) are intentionally
// excluded: they are healthy progress, not a failure.
var retryableFailureReasons = map[string]struct{}{
	ReasonStateLoadFailed:           {},
	ReasonStatusPersistConflict:     {},
	ReasonBackupStatusMissing:       {},
	ReasonRollbackCapabilityFailed:  {},
	ReasonPreUpgradeBackupNotReady:  {},
	ReasonPostUpgradeBackupNotReady: {},
	ReasonUnknownMajorUpgradeError:  {},
}

// IsRetryableFailureReason reports whether a retryable report's reason
// represents a blocking obstacle (MajorUpgradeRetryableFailure) rather than
// healthy progress (MajorUpgradeProgressing).
func IsRetryableFailureReason(reason string) bool {
	_, ok := retryableFailureReasons[reason]
	return ok
}
