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
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

func TestSHCPodRolloutActiveFailsClosed(t *testing.T) {
	tests := []struct {
		name      string
		operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus
		want      bool
	}{
		{name: "no operation"},
		{
			name: "scale down is not Pod rollout",
			operation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
				Intent: enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown,
				Stage:  enterpriseApi.SearchHeadClusterLifecycleStageDrainingSearches,
			},
		},
		{
			name: "completed Pod rollout",
			operation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
				Intent: enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
				Stage:  enterpriseApi.SearchHeadClusterLifecycleStageCompleted,
			},
		},
		{
			name: "draining Pod rollout",
			operation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
				Intent: enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
				Stage:  enterpriseApi.SearchHeadClusterLifecycleStageDrainingSearches,
			},
			want: true,
		},
		{
			name: "blocked Pod rollout",
			operation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
				Intent: enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
				Stage:  enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
			},
			want: true,
		},
		{
			name: "failed Pod rollout",
			operation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
				Intent: enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
				Stage:  enterpriseApi.SearchHeadClusterLifecycleStageFailed,
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shcPodRolloutActive(test.operation); got != test.want {
				t.Fatalf("shcPodRolloutActive() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestSHCAppFrameworkWorkActive(t *testing.T) {
	tests := []struct {
		name       string
		appContext *enterpriseApi.AppDeploymentContext
		want       bool
	}{
		{name: "nil"},
		{name: "empty", appContext: &enterpriseApi.AppDeploymentContext{}},
		{
			name: "deployment in progress",
			appContext: &enterpriseApi.AppDeploymentContext{
				IsDeploymentInProgress: true,
			},
			want: true,
		},
		{
			name: "bundle pending",
			appContext: &enterpriseApi.AppDeploymentContext{
				BundlePushStatus: enterpriseApi.BundlePushTracker{
					BundlePushStage: enterpriseApi.BundlePushPending,
				},
			},
			want: true,
		},
		{
			name: "bundle in progress",
			appContext: &enterpriseApi.AppDeploymentContext{
				BundlePushStatus: enterpriseApi.BundlePushTracker{
					BundlePushStage: enterpriseApi.BundlePushInProgress,
				},
			},
			want: true,
		},
		{
			name: "bundle complete",
			appContext: &enterpriseApi.AppDeploymentContext{
				BundlePushStatus: enterpriseApi.BundlePushTracker{
					BundlePushStage: enterpriseApi.BundlePushComplete,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shcAppFrameworkWorkActive(test.appContext); got != test.want {
				t.Fatalf(
					"shcAppFrameworkWorkActive() = %t, want %t",
					got,
					test.want,
				)
			}
		})
	}
}
