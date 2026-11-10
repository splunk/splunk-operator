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

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
)

// maxRebuildReasonLength matches the API's MaxLength for rebuildRequired.
const maxRebuildReasonLength = 1024

// BlueGreenRebuildRequired reports whether the attempt can no longer be
// reused by a retry.
func BlueGreenRebuildRequired(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
	return status != nil && status.RebuildRequired != nil
}

// MarkBlueGreenRebuildRequired durably classifies a pre-commit attempt as
// unusable when cause is ErrCandidateInvalid. It never classifies a committed
// attempt, which can only fail forward, and never rewrites an earlier reason.
// It reports whether status changed.
func MarkBlueGreenRebuildRequired(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, cause error) bool {
	if status == nil || !errors.Is(cause, ErrCandidateInvalid) || CutoverStageOfStatus(status).Committed() || BlueGreenRebuildRequired(status) {
		return false
	}
	reason := cause.Error()
	if len(reason) > maxRebuildReasonLength {
		reason = reason[:maxRebuildReasonLength-len("…")] + "…"
	}
	status.RebuildRequired = &reason
	return true
}
