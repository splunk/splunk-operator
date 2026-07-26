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
	"fmt"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestRollingUpdateControllerStartsDurablePreparationWithoutDeletingPod(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil {
		t.Fatalf("update RollingUpdate StatefulSet: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("phase = %q, want %q", phase, enterpriseApi.PhaseUpdating)
	}
	operation := mgr.cr.Status.LifecycleOperation
	if operation == nil ||
		operation.TargetOrdinal == nil ||
		*operation.TargetOrdinal != 2 ||
		operation.DesiredRevision != "revision-2" ||
		operation.Intent != enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate {
		t.Fatalf("lifecycle operation = %#v, want durable preparation for ordinal 2", operation)
	}
	assertNoRollingUpdatePodDelete(t, client)
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("unexpected Kubernetes update before authorization: %v", client.Calls["Update"])
	}
}

func TestRollingUpdateControllerAdvancesOnlyAfterPersistedAuthorization(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	target := int32(2)
	authorizedAt := metav1.Now()
	mgr.cr.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:             "pod-update-2",
		Intent:                  enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		DesiredRevision:         "revision-2",
		TargetPod:               statefulSet.GetName() + "-2",
		TargetOrdinal:           &target,
		Stage:                   enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement,
		ReplacementAuthorizedAt: &authorizedAt,
	}

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil {
		t.Fatalf("update RollingUpdate StatefulSet: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("phase = %q, want %q", phase, enterpriseApi.PhaseUpdating)
	}
	if len(client.Calls["Update"]) != 1 {
		t.Fatalf("Kubernetes updates = %d, want one partition update", len(client.Calls["Update"]))
	}
	assertNoRollingUpdatePodDelete(t, client)

	stored := &appsv1.StatefulSet{}
	if err := client.Get(context.Background(), types.NamespacedName{
		Namespace: statefulSet.GetNamespace(),
		Name:      statefulSet.GetName(),
	}, stored); err != nil {
		t.Fatalf("get StatefulSet: %v", err)
	}
	if stored.Spec.UpdateStrategy.RollingUpdate == nil ||
		stored.Spec.UpdateStrategy.RollingUpdate.Partition == nil ||
		*stored.Spec.UpdateStrategy.RollingUpdate.Partition != target {
		t.Fatalf("stored strategy = %#v, want partition %d",
			stored.Spec.UpdateStrategy, target)
	}
}

func TestRollingUpdateControllerWaitsForKubernetesWithoutDeletingPod(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		2,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	target := int32(2)
	authorizedAt := metav1.Now()
	mgr.cr.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:             "pod-update-2",
		Intent:                  enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		DesiredRevision:         "revision-2",
		TargetPod:               statefulSet.GetName() + "-2",
		TargetOrdinal:           &target,
		Stage:                   enterpriseApi.SearchHeadClusterLifecycleStageWaitingForTermination,
		ReplacementAuthorizedAt: &authorizedAt,
	}

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil {
		t.Fatalf("update RollingUpdate StatefulSet: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("phase = %q, want %q", phase, enterpriseApi.PhaseUpdating)
	}
	assertNoRollingUpdatePodDelete(t, client)
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("unexpected update while waiting for Kubernetes: %v", client.Calls["Update"])
	}
}

func TestRollingUpdateControllerReportsStableRevisionReady(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-2",
		"revision-2",
		[]string{"revision-2", "revision-2", "revision-2"},
	)

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil {
		t.Fatalf("update RollingUpdate StatefulSet: %v", err)
	}
	if phase != enterpriseApi.PhaseReady {
		t.Fatalf("phase = %q, want %q", phase, enterpriseApi.PhaseReady)
	}
	assertNoRollingUpdatePodDelete(t, client)
}

func TestLifecycleRecoveryWaitsForRollingPartitionAuthorization(t *testing.T) {
	target := int32(2)
	partition := int32(3)
	operation := &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		Intent:        enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		TargetOrdinal: &target,
		TargetPodUID:  "original-pod-uid",
		Stage:         enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement,
	}
	statefulSet := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
					Partition: &partition,
				},
			},
		},
	}

	if lifecycleRecoveryActiveForStatefulSet(statefulSet, operation) {
		t.Fatal("recovery became active before the partition authorized replacement")
	}

	partition = target
	if !lifecycleRecoveryActiveForStatefulSet(statefulSet, operation) {
		t.Fatal("recovery did not become active after the partition authorized replacement")
	}
}

func TestLifecycleRecoveryPreservesOnDeleteOrdering(t *testing.T) {
	target := int32(2)
	operation := &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		Intent:        enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		TargetOrdinal: &target,
		TargetPodUID:  "original-pod-uid",
		Stage:         enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement,
	}
	statefulSet := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.OnDeleteStatefulSetStrategyType,
			},
		},
	}

	if !lifecycleRecoveryActiveForStatefulSet(statefulSet, operation) {
		t.Fatal("OnDelete recovery ordering changed")
	}
}

func rollingUpdateControllerFixture(
	t *testing.T,
	partition int32,
	currentRevision string,
	updateRevision string,
	podRevisions []string,
) (*searchHeadClusterPodManager, *appsv1.StatefulSet, *spltest.MockClient) {
	t.Helper()
	replicas := int32(len(podRevisions))
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			Replicas: replicas,
			LifecyclePolicy: &enterpriseApi.SearchHeadClusterLifecyclePolicy{
				PodUpdateStrategy: enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate,
			},
		},
	}
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(SplunkSearchHead, cr.GetName()),
			Namespace: cr.GetNamespace(),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
					Partition: &partition,
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:        replicas,
			ReadyReplicas:   replicas,
			CurrentRevision: currentRevision,
			UpdateRevision:  updateRevision,
		},
	}

	client := spltest.NewMockClient()
	ctx := context.Background()
	if err := client.Create(ctx, statefulSet); err != nil {
		t.Fatalf("create StatefulSet: %v", err)
	}
	for ordinal, revision := range podRevisions {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", statefulSet.GetName(), ordinal),
				Namespace: statefulSet.GetNamespace(),
				Labels: map[string]string{
					"controller-revision-hash": revision,
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}
		if err := client.Create(ctx, pod); err != nil {
			t.Fatalf("create Pod %d: %v", ordinal, err)
		}
	}
	client.ResetCalls()

	mgr := &searchHeadClusterPodManager{
		c:           client,
		cr:          cr,
		statefulSet: statefulSet,
	}
	return mgr, statefulSet, client
}

func assertNoRollingUpdatePodDelete(t *testing.T, client *spltest.MockClient) {
	t.Helper()
	if len(client.Calls["Delete"]) != 0 {
		t.Fatalf("RollingUpdate controller called Delete: %v", client.Calls["Delete"])
	}
}
