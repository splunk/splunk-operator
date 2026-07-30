// Copyright (c) 2026 Splunk Inc. All rights reserved.
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
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Splunk's SHC functional setup waits an additional 60 seconds after all
// members return Up and the captain service becomes ready. Preserve the same
// uninterrupted stabilization contract before advancing a startup stage.
const searchHeadInitialFormationStabilizationPeriod = time.Minute

func normalizedSearchHeadInitialFormationStage(
	stage enterpriseApi.SearchHeadClusterInitialFormationStage,
) enterpriseApi.SearchHeadClusterInitialFormationStage {
	if stage == "" {
		return enterpriseApi.SearchHeadClusterInitialFormationStageClusterFormation
	}
	return stage
}

func (mgr *searchHeadClusterPodManager) initialFormationObservationStable(
	desiredReplicas int32,
) bool {
	if mgr == nil ||
		mgr.cr == nil ||
		!mgr.initialFormationContainersReady ||
		mgr.cr.Status.DeployerPhase != enterpriseApi.PhaseReady ||
		!mgr.cr.Status.CaptainMembersObserved ||
		mgr.cr.Status.CaptainRollingRestart ||
		!mgr.cr.Status.Initialized ||
		!mgr.cr.Status.MinPeersJoined ||
		!mgr.cr.Status.CaptainReady ||
		mgr.cr.Status.MaintenanceMode ||
		mgr.cr.Status.Captain == "" ||
		int32(len(mgr.cr.Status.Members)) != desiredReplicas {
		return false
	}

	for i := range mgr.cr.Status.Members {
		member := mgr.cr.Status.Members[i]
		if member.Status != "Up" ||
			member.CaptainStatus != "Up" ||
			!member.Registered ||
			member.AdvertiseRestartRequired ||
			(member.RestartState != "" &&
				member.RestartState != "NoRestart") {
			return false
		}
	}
	return true
}

func (mgr *searchHeadClusterPodManager) initialFormationStableFor(
	now time.Time,
	desiredReplicas int32,
) bool {
	if !mgr.initialFormationObservationStable(desiredReplicas) {
		mgr.cr.Status.InitialFormationStableSince = nil
		return false
	}
	if mgr.cr.Status.InitialFormationStableSince == nil {
		stableSince := metav1.NewTime(now)
		mgr.cr.Status.InitialFormationStableSince = &stableSince
		return false
	}
	return !now.Before(
		mgr.cr.Status.InitialFormationStableSince.Time.Add(
			searchHeadInitialFormationStabilizationPeriod,
		),
	)
}

// reconcileInitialFormationStage advances only after the current Splunk
// topology has remained continuously healthy. Bundle-owning stages are
// advanced by ApplySearchHeadCluster after the corresponding deployer work is
// accepted or completed.
func (mgr *searchHeadClusterPodManager) reconcileInitialFormationStage(
	desiredReplicas int32,
) {
	if !mgr.searchHeadInitialFormationPending() {
		return
	}

	stage := normalizedSearchHeadInitialFormationStage(
		mgr.cr.Status.InitialFormationStage,
	)
	mgr.cr.Status.InitialFormationStage = stage

	switch stage {
	case enterpriseApi.SearchHeadClusterInitialFormationStageClusterFormation,
		enterpriseApi.SearchHeadClusterInitialFormationStageTelemetryApplied,
		enterpriseApi.SearchHeadClusterInitialFormationStageFinalStabilization:
	default:
		mgr.cr.Status.InitialFormationStableSince = nil
		return
	}

	if !mgr.initialFormationStableFor(
		searchHeadClusterLifecycleNow(),
		desiredReplicas,
	) {
		return
	}

	mgr.cr.Status.InitialFormationStableSince = nil
	switch stage {
	case enterpriseApi.SearchHeadClusterInitialFormationStageClusterFormation:
		mgr.cr.Status.InitialFormationStage =
			enterpriseApi.SearchHeadClusterInitialFormationStageTelemetryPending
	case enterpriseApi.SearchHeadClusterInitialFormationStageTelemetryApplied:
		if len(mgr.cr.Spec.AppFrameworkConfig.AppSources) == 0 {
			mgr.cr.Status.InitialFormationStage =
				enterpriseApi.SearchHeadClusterInitialFormationStageComplete
			return
		}
		mgr.cr.Status.InitialFormationStage =
			enterpriseApi.SearchHeadClusterInitialFormationStageAppFrameworkPending
	case enterpriseApi.SearchHeadClusterInitialFormationStageFinalStabilization:
		mgr.cr.Status.InitialFormationStage =
			enterpriseApi.SearchHeadClusterInitialFormationStageComplete
	}
}

func searchHeadInitialFormationAppFrameworkSettled(
	cr *enterpriseApi.SearchHeadCluster,
) bool {
	if cr == nil || cr.Status.AppContext.IsDeploymentInProgress {
		return false
	}
	switch cr.Status.AppContext.BundlePushStatus.BundlePushStage {
	case enterpriseApi.BundlePushUninitialized,
		enterpriseApi.BundlePushComplete:
		return true
	default:
		return false
	}
}

func searchHeadCanRunInitialAppFramework(
	cr *enterpriseApi.SearchHeadCluster,
) bool {
	return cr != nil &&
		cr.Status.LastStableReplicas == nil &&
		normalizedSearchHeadInitialFormationStage(
			cr.Status.InitialFormationStage,
		) ==
			enterpriseApi.SearchHeadClusterInitialFormationStageAppFrameworkPending
}
