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
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
)

func TestRollingUpdateControllerContinuesOwnedTargetWithdrawal(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	target := int32(2)
	mgr.cr.Status.LifecycleOperation =
		&enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
			OperationID:     "PodUpdate:splunk-stack1-search-head-2:revision-2",
			Intent:          enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
			DesiredRevision: "revision-2",
			TargetPod:       statefulSet.GetName() + "-2",
			TargetOrdinal:   &target,
			Stage: enterpriseApi.
				SearchHeadClusterLifecycleStageDetainingTarget,
		}

	targetPod := &corev1.Pod{}
	if err := client.Get(context.Background(), types.NamespacedName{
		Namespace: statefulSet.GetNamespace(),
		Name:      statefulSet.GetName() + "-2",
	}, targetPod); err != nil {
		t.Fatalf("get lifecycle target Pod: %v", err)
	}
	targetPod.UID = types.UID("owned-target-pod-uid")
	for index := range targetPod.Status.Conditions {
		if targetPod.Status.Conditions[index].Type == corev1.PodReady {
			targetPod.Status.Conditions[index].Status = corev1.ConditionFalse
		}
	}
	if err := client.Update(context.Background(), targetPod); err != nil {
		t.Fatalf("mark lifecycle target unavailable: %v", err)
	}
	client.ResetCalls()

	oldGetMembers := getSearchHeadCaptainMembers
	oldRequestDetention := requestSearchHeadDetention
	t.Cleanup(func() {
		getSearchHeadCaptainMembers = oldGetMembers
		requestSearchHeadDetention = oldRequestDetention
	})
	getSearchHeadCaptainMembers = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) (map[string]splclient.SearchHeadCaptainMemberInfo, error) {
		return map[string]splclient.SearchHeadCaptainMemberInfo{
			statefulSet.GetName() + "-0": {
				Identifier:    "member-guid-0",
				Label:         statefulSet.GetName() + "-0",
				Status:        "Up",
				Captain:       true,
				ManagementURI: "https://search-head-0:8089",
			},
			statefulSet.GetName() + "-1": {
				Identifier:    "member-guid-1",
				Label:         statefulSet.GetName() + "-1",
				Status:        "Up",
				ManagementURI: "https://search-head-1:8089",
			},
			statefulSet.GetName() + "-2": {
				Identifier:    "member-guid-2",
				Label:         statefulSet.GetName() + "-2",
				Status:        "Up",
				ManagementURI: "https://search-head-2:8089",
			},
		}, nil
	}
	detentionCalls := 0
	requestSearchHeadDetention = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) error {
		detentionCalls++
		return nil
	}

	// First persist the original Pod identity. The owned-unavailability path
	// must not perform detention in the same reconciliation.
	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil {
		t.Fatalf("continue owned lifecycle target: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("phase = %q, want %q", phase, enterpriseApi.PhaseUpdating)
	}
	if detentionCalls != 0 {
		t.Fatalf("detention calls during identity barrier = %d, want 0", detentionCalls)
	}
	if mgr.cr.Status.LifecycleOperation.TargetPodUID != "owned-target-pod-uid" {
		t.Fatalf(
			"owned lifecycle target Pod UID = %q, want durable fixture identity",
			mgr.cr.Status.LifecycleOperation.TargetPodUID,
		)
	}
	assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, 3)
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("identity barrier changed Kubernetes state: %v", client.Calls["Update"])
	}
	assertNoRollingUpdatePodDelete(t, client)

	phase, err = mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil {
		t.Fatalf("continue owned lifecycle target after identity barrier: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("phase = %q, want %q", phase, enterpriseApi.PhaseUpdating)
	}
	if detentionCalls != 1 {
		t.Fatalf("detention calls after identity barrier = %d, want 1", detentionCalls)
	}
	assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, 3)
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("owned target withdrawal changed Kubernetes state: %v", client.Calls["Update"])
	}
	assertNoRollingUpdatePodDelete(t, client)
}
