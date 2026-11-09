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
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgendpointprotocol "github.com/splunk/splunk-operator/pkg/postgresql/shared/endpointprotocol"
)

// HasRetainedSourceFenceProof defines when absent blue instances can rely on
// the durable precommit fence instead of blocking green's fail-forward recovery.
// Adapters separately verify live provider identity and the exact access overlay.
func HasRetainedSourceFenceProof(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, attemptID, controlRole string) bool {
	if status == nil || status.AttemptID == "" || status.AttemptID != attemptID || !pgendpointprotocol.CommitReceiptTargetsGreen(status) {
		return false
	}
	w := status.Switchover
	return w != nil && w.ControlIdentity != nil && w.ControlIdentity.RoleName == controlRole &&
		w.ControlIdentity.PreparedAt != nil && w.FenceVerifiedAt != nil && w.DrainCompletedAt != nil &&
		w.FinalBlueLSN != nil && *w.FinalBlueLSN != "" && status.CommitReceipt.CommittedAt != nil
}
