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

import enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

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
	if appContext.IsDeploymentInProgress {
		return true
	}
	switch appContext.BundlePushStatus.BundlePushStage {
	case enterpriseApi.BundlePushPending, enterpriseApi.BundlePushInProgress:
		return true
	default:
		return false
	}
}
