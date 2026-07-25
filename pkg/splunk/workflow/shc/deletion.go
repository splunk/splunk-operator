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

package shc

import (
	"fmt"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StartClusterDeletion records whole-cluster deletion as a distinct lifecycle
// intent. Complete CR deletion is Kubernetes resource finalization, not a Pod
// recycle or a permanent single-member scale-down.
func StartClusterDeletion(
	current *enterpriseApi.SearchHeadClusterLifecycleOperationStatus,
	clusterName string,
	now time.Time,
) *enterpriseApi.SearchHeadClusterLifecycleOperationStatus {
	if current != nil &&
		current.Intent == enterpriseApi.SearchHeadClusterLifecycleIntentClusterDeletion {
		return current.DeepCopy()
	}

	timestamp := metav1.NewTime(now)
	return &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:        fmt.Sprintf("ClusterDeletion:%s", clusterName),
		Intent:             enterpriseApi.SearchHeadClusterLifecycleIntentClusterDeletion,
		Stage:              enterpriseApi.SearchHeadClusterLifecycleStageFinalizingClusterDeletion,
		StartedAt:          &timestamp,
		StageStartedAt:     &timestamp,
		LastTransitionTime: &timestamp,
		Reason:             enterpriseApi.SearchHeadClusterLifecycleReasonClusterDeletionRequested,
		Message:            "finalizing complete Search Head Cluster deletion without per-member recycle or consensus removal",
	}
}
