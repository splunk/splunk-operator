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

package v4

import (
	"bytes"
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func lifecycleTestInt64Pointer(value int64) *int64 {
	return &value
}

func lifecycleTestInt32Pointer(value int32) *int32 {
	return &value
}

func TestSearchHeadClusterLifecycleJSONRoundTripAndDeepCopy(t *testing.T) {
	now := metav1.Now()
	input := &SearchHeadCluster{
		Spec: SearchHeadClusterSpec{
			CommonSplunkSpec: CommonSplunkSpec{
				TerminationGracePeriodSeconds: lifecycleTestInt64Pointer(1200),
			},
			Replicas: 3,
			LifecyclePolicy: &SearchHeadClusterLifecyclePolicy{
				PodUpdateStrategy:             SearchHeadClusterPodUpdateStrategyRollingUpdate,
				DetentionTimeoutSeconds:       lifecycleTestInt64Pointer(179),
				SearchDrainTimeoutSeconds:     lifecycleTestInt64Pointer(180),
				CaptainTransferTimeoutSeconds: lifecycleTestInt64Pointer(181),
				PodStartupTimeoutSeconds:      lifecycleTestInt64Pointer(182),
				MemberRejoinTimeoutSeconds:    lifecycleTestInt64Pointer(1800),
			},
		},
		Status: SearchHeadClusterStatus{
			ImageUpgrade: &SearchHeadClusterImageUpgradeStatus{
				OperationID:                 "image-upgrade:example-search-head:revision-2",
				StatefulSetName:             "example-search-head",
				DesiredRevision:             "revision-2",
				SourceImage:                 "splunk/splunk:9.4.0",
				TargetImage:                 "splunk/splunk:10.0.0",
				TargetReplicas:              3,
				Phase:                       SearchHeadClusterImageUpgradePhaseRollingMembers,
				Reason:                      SearchHeadClusterImageUpgradeReasonMemberRecovered,
				Message:                     "ordinal 2 recovered",
				StartedAt:                   &now,
				PhaseStartedAt:              &now,
				LastTransitionTime:          &now,
				InitializationIntentAt:      &now,
				InitializationLastAttemptAt: &now,
				InitializationSucceededAt:   &now,
				InitializationAttemptCount:  1,
				CompletedOrdinals:           []int32{2},
				FinalizationIntentAt:        &now,
				FinalizationLastAttemptAt:   &now,
				FinalizationSucceededAt:     &now,
				FinalizationAttemptCount:    1,
				CompletedAt:                 &now,
			},
			LifecycleOperation: &SearchHeadClusterLifecycleOperationStatus{
				OperationID:                  "operation-1",
				Intent:                       SearchHeadClusterLifecycleIntentPodUpdate,
				DesiredRevision:              "revision-2",
				TargetPod:                    "example-search-head-2",
				TargetOrdinal:                lifecycleTestInt32Pointer(2),
				Stage:                        SearchHeadClusterLifecycleStageDrainingSearches,
				StartedAt:                    &now,
				StageStartedAt:               &now,
				LastTransitionTime:           &now,
				ReplacementPodObservedAt:     &now,
				CompletedOrdinals:            []int32{3},
				RetryCount:                   1,
				DetentionRequestedAt:         &now,
				DetentionRequestAttemptCount: 2,
				Reason:                       SearchHeadClusterLifecycleReasonSearchesActive,
				Message:                      "waiting for active searches to drain",
				Captain:                      "example-search-head-0",
				CaptainReady:                 true,
				ActiveHistoricalSearches:     2,
				ActiveRealtimeSearches:       1,
				LastSuccessfulSHCObservation: &now,
			},
		},
	}

	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded SearchHeadCluster
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	reencoded, err := json.Marshal(&decoded)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !bytes.Equal(encoded, reencoded) {
		t.Fatalf("JSON round trip changed representation:\ninput: %s\ndecoded: %s", encoded, reencoded)
	}
	if decoded.Spec.LifecyclePolicy == nil ||
		decoded.Status.ImageUpgrade == nil ||
		decoded.Status.ImageUpgrade.Phase != SearchHeadClusterImageUpgradePhaseRollingMembers ||
		decoded.Status.LifecycleOperation == nil ||
		decoded.Status.LifecycleOperation.Stage != SearchHeadClusterLifecycleStageDrainingSearches {
		t.Fatalf("JSON round trip lost lifecycle fields: %#v", decoded)
	}

	copied := input.DeepCopy()
	*copied.Spec.TerminationGracePeriodSeconds = 10
	*copied.Spec.LifecyclePolicy.DetentionTimeoutSeconds = 19
	*copied.Spec.LifecyclePolicy.SearchDrainTimeoutSeconds = 20
	*copied.Spec.LifecyclePolicy.PodStartupTimeoutSeconds = 30
	copied.Status.LifecycleOperation.ReplacementPodObservedAt = nil
	copied.Status.ImageUpgrade.CompletedOrdinals[0] = 1
	copied.Status.LifecycleOperation.CompletedOrdinals[0] = 1
	if *input.Spec.TerminationGracePeriodSeconds != 1200 ||
		*input.Spec.LifecyclePolicy.DetentionTimeoutSeconds != 179 ||
		*input.Spec.LifecyclePolicy.SearchDrainTimeoutSeconds != 180 ||
		*input.Spec.LifecyclePolicy.PodStartupTimeoutSeconds != 182 ||
		input.Status.LifecycleOperation.ReplacementPodObservedAt == nil ||
		input.Status.ImageUpgrade.CompletedOrdinals[0] != 2 ||
		input.Status.LifecycleOperation.CompletedOrdinals[0] != 3 {
		t.Fatal("DeepCopy shares lifecycle pointer or slice storage")
	}
}
