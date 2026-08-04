// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

func shcPodRolloutActive(
	operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
) bool {
	return operation != nil &&
		operation.Intent == enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate &&
		operation.Stage != enterpriseApi.SearchHeadClusterLifecycleStageCompleted
}

func shcImageUpgradeActive(
	operation *enterpriseApi.SearchHeadClusterImageUpgradeStatus,
) bool {
	return operation != nil &&
		operation.Phase !=
			enterpriseApi.SearchHeadClusterImageUpgradePhaseCompleted
}

func shcAppFrameworkWorkActive(
	appContext *enterpriseApi.AppDeploymentContext,
) bool {
	if appContext == nil {
		return false
	}
	switch appContext.BundlePushStatus.BundlePushStage {
	case enterpriseApi.BundlePushPending, enterpriseApi.BundlePushInProgress:
		return true
	}

	// IsDeploymentInProgress is also used as a transient lock while the remote
	// repository is being polled. It therefore cannot, by itself, distinguish
	// active deployment work from an empty or read-only poll. Phase-3 app
	// records retain the legacy DeployStatusPending value after a cluster-scoped
	// bundle reaches install-complete, so the durable phase status is the
	// completion boundary for pending or in-progress records.
	for _, appSource := range appContext.AppsSrcDeployStatus {
		for _, app := range appSource.AppDeploymentInfoList {
			switch app.DeployStatus {
			case enterpriseApi.DeployStatusPending,
				enterpriseApi.DeployStatusInProgress:
				if app.PhaseInfo.Phase == enterpriseApi.PhaseInstall &&
					app.PhaseInfo.Status ==
						enterpriseApi.AppPkgInstallComplete {
					continue
				}
				switch app.PhaseInfo.Status {
				case enterpriseApi.AppPkgDownloadError,
					enterpriseApi.AppPkgPodCopyError,
					enterpriseApi.AppPkgInstallError:
					// App Framework retains terminal error records for
					// diagnostics. They do not own an unrelated Pod rollout.
					continue
				}
				return true
			}
		}
	}

	return false
}

// shcDeployerUpdateDeferred keeps a new Deployer Pod-template replacement
// from starting while another established-SHC owner is using the disruption
// slot. Initial formation and compatibility-mode clusters retain their legacy
// ordering. An already-started Deployer replacement is detected separately
// and must resume instead of yielding to a later owner.
func shcDeployerUpdateDeferred(
	cr *enterpriseApi.SearchHeadCluster,
) (bool, string) {
	if cr == nil ||
		!searchHeadClusterLifecycleEnabled() ||
		cr.Status.LastStableReplicas == nil {
		return false, ""
	}
	if shcAppFrameworkWorkActive(&cr.Status.AppContext) {
		return true, "AppFrameworkOperationActive"
	}
	if shcPodRolloutActive(cr.Status.LifecycleOperation) {
		return true, "SearchHeadLifecycleActive"
	}
	return false, ""
}

// shcDeployerReconcilePhase preserves a fail-closed wait when the Kubernetes
// observation at the start of reconciliation proved that the Deployer had not
// converged. The generic manager can report Ready while a new StatefulSet
// generation or update revision has not reached its status yet; one later
// observation must prove convergence before Search Head Pods may change.
func shcDeployerReconcilePhase(
	managerPhase enterpriseApi.Phase,
	observedActive bool,
) enterpriseApi.Phase {
	if observedActive && managerPhase == enterpriseApi.PhaseReady {
		return enterpriseApi.PhaseUpdating
	}
	return managerPhase
}

// shcAppFrameworkKubernetesRestartEnabled identifies the fully qualified
// runtime contract for converting an App Framework restart requirement into a
// StatefulSet revision. Compatibility-mode OnDelete clusters, disabled feature
// gates, and initial formation continue to use Splunk's supported bundle-owned
// restart path.
func shcAppFrameworkKubernetesRestartEnabled(
	cr *enterpriseApi.SearchHeadCluster,
) (bool, error) {
	if cr == nil ||
		!searchHeadClusterLifecycleEnabled() ||
		searchHeadCanRunInitialAppFramework(cr) {
		return false, nil
	}
	policy, err := ResolveSearchHeadClusterLifecyclePolicy(&cr.Spec)
	if err != nil {
		return false, err
	}
	return policy.PodUpdateStrategy ==
		enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate, nil
}

// shcAppFrameworkRestartObservationPending identifies a completed operational
// bundle whose restart requirement has not yet been read from the authoritative
// captain-members endpoint. The observation revision is durable so a bundle
// that requires no restart does not cause an unbounded REST poll.
func shcAppFrameworkRestartObservationPending(
	cr *enterpriseApi.SearchHeadCluster,
) (bool, error) {
	enabled, err := shcAppFrameworkKubernetesRestartEnabled(cr)
	if err != nil || !enabled {
		return false, err
	}
	return cr.Status.AppContext.BundlePushStatus.BundlePushStage ==
		enterpriseApi.BundlePushComplete &&
		cr.Status.AppFrameworkBundleRevision != "" &&
		cr.Status.AppFrameworkRestartObservedRevision !=
			cr.Status.AppFrameworkBundleRevision, nil
}

// validateSHCAppFrameworkRestartBaseline prevents the controller from
// attributing an already-pending member restart to a newly sent App Framework
// bundle. The post-send restart observation is meaningful only from a clean
// member baseline.
func validateSHCAppFrameworkRestartBaseline(
	cr *enterpriseApi.SearchHeadCluster,
) error {
	if cr == nil {
		return fmt.Errorf("SHC App Framework restart baseline requires a SearchHeadCluster")
	}
	for i := range cr.Status.Members {
		member := cr.Status.Members[i]
		if member.AdvertiseRestartRequired {
			return fmt.Errorf(
				"SHC member %s already advertises restart-required before App Framework bundle send",
				member.Name,
			)
		}
		if member.RestartState != "" && member.RestartState != "NoRestart" {
			return fmt.Errorf(
				"SHC member %s has restart state %q before App Framework bundle send",
				member.Name,
				member.RestartState,
			)
		}
	}
	return nil
}
