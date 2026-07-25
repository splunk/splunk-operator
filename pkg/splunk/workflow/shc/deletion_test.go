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
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

func TestClusterDeletionSupersedesMemberLifecycleIntent(t *testing.T) {
	now := time.Date(2026, 7, 25, 11, 0, 0, 0, time.UTC)
	target := int32(2)
	current := StartReplacement(
		"PodUpdate:splunk-example-search-head-2:revision-2",
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		"revision-2",
		"splunk-example-search-head-2",
		target,
		now.Add(-time.Minute),
	)

	operation := StartClusterDeletion(current, "example", now)
	if operation.Intent !=
		enterpriseApi.SearchHeadClusterLifecycleIntentClusterDeletion {
		t.Fatalf("intent = %q, want ClusterDeletion", operation.Intent)
	}
	if operation.Stage !=
		enterpriseApi.SearchHeadClusterLifecycleStageFinalizingClusterDeletion {
		t.Fatalf("stage = %q, want FinalizingClusterDeletion", operation.Stage)
	}
	if operation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonClusterDeletionRequested {
		t.Fatalf("reason = %q, want ClusterDeletionRequested", operation.Reason)
	}
	if operation.TargetPod != "" || operation.TargetOrdinal != nil ||
		operation.MembershipRemovalRequestedAt != nil {
		t.Fatalf(
			"cluster deletion retained per-member state: %#v",
			operation,
		)
	}
}

func TestClusterDeletionIntentIsStableAcrossReconciliation(t *testing.T) {
	now := time.Date(2026, 7, 25, 11, 0, 0, 0, time.UTC)
	operation := StartClusterDeletion(nil, "example", now)

	resumed := StartClusterDeletion(operation, "example", now.Add(time.Minute))
	if !resumed.StartedAt.Equal(operation.StartedAt) ||
		!resumed.StageStartedAt.Equal(operation.StageStartedAt) {
		t.Fatalf(
			"cluster deletion timestamps changed across resume: before=%#v after=%#v",
			operation,
			resumed,
		)
	}
	if resumed == operation {
		t.Fatal("resumed operation aliases the persisted status")
	}
}
