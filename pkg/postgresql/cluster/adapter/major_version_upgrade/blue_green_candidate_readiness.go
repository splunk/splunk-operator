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

package majorupgradeadapter

import (
	"fmt"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	clustercnpg "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/cnpg"
)

const candidatePluginStartupGracePeriod = 5 * time.Minute

// candidateBlockingError lets CNPG retry transient plugin hook failures during
// green startup, without changing error policy for an authoritative cluster.
// The API creation time survives retries and operator restarts; a broken plugin
// cannot reset its grace period or be mistaken for a ready candidate.
func candidateBlockingError(candidate *cnpgv1.Cluster, now time.Time) error {
	if candidate != nil && candidate.Status.Phase == cnpgv1.PhaseFailurePlugin {
		if !candidate.CreationTimestamp.IsZero() && now.Before(candidate.CreationTimestamp.Add(candidatePluginStartupGracePeriod)) {
			return fmt.Errorf("%w: %s", mvutypes.ErrCandidatePluginPending, candidate.Status.PhaseReason)
		}
		return fmt.Errorf("%w: %s", mvutypes.ErrCandidatePluginTimedOut, candidate.Status.PhaseReason)
	}
	if err := clustercnpg.ClusterBlockingError(candidate); err != nil {
		return fmt.Errorf("%w: green CNPG Cluster is blocked: %v", mvutypes.ErrUpgradeFlowFailed, err)
	}
	return nil
}
