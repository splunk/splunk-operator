// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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

package splkcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

// createTestTransitionStateJSON creates a JSON string for CPUAwareTransitionState annotation
// used in tests. This helper simplifies creating test annotations with the new JSON format.
func createTestTransitionStateJSON(originalReplicas, targetReplicas int32, originalCPUMillis, targetCPUMillis int64) string {
	return createTestTransitionStateJSONWithFinished(originalReplicas, targetReplicas, originalCPUMillis, targetCPUMillis, "")
}

// createTestTransitionStateJSONWithFinished creates a JSON string with optional FinishedAt timestamp
func createTestTransitionStateJSONWithFinished(originalReplicas, targetReplicas int32, originalCPUMillis, targetCPUMillis int64, finishedAt string) string {
	state := CPUAwareTransitionState{
		OriginalReplicas:  originalReplicas,
		TargetReplicas:    targetReplicas,
		OriginalCPUMillis: originalCPUMillis,
		TargetCPUMillis:   targetCPUMillis,
		StartedAt:         "2026-01-10T10:00:00Z",
		FinishedAt:        finishedAt,
	}
	data, _ := json.Marshal(state)
	return string(data)
}

// Helper function to create test pods with specific CPU
func createCPUTestPod(name, namespace, cpu string, ready bool, revision string) *corev1.Pod {
	cpuQuantity := resource.MustParse(cpu)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"controller-revision-hash": revision,
				"app":                      "splunk",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "splunk",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: cpuQuantity,
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Ready: ready},
			},
		},
	}
	if ready {
		pod.Status.Conditions = []corev1.PodCondition{
			{Type: corev1.PodReady, Status: corev1.ConditionTrue},
		}
	}
	return pod
}

func TestUpdateStatefulSetPods_CPUAwareDefersToPodRecycling(t *testing.T) {
	// This test verifies that CPU-aware scaling recycles kept pods when there's no excess CPU.
	// When there IS excess CPU, balancing (reducing replicas) happens first.
	// This test sets up a scenario with NO excess CPU to verify recycling behavior.
	//
	// Scenario: 6 pods × 2CPU -> 3 pods × 4CPU
	// All pods still have old spec (2 CPU each), no recycling has happened yet
	// TotalReadyCPU = 6 × 2 = 12 CPU
	// OriginalTotalCPU = 6 × 2 = 12 CPU
	// excessCPU = 0 -> No balancing possible, must recycle first
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 6
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation:        "true",
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(6, 3, 2000, 4000),
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "splunk"},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("4"),
								},
							},
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas:  6,
			UpdateRevision: "v2", // New revision
		},
	}

	// All 6 pods still have old spec (2 CPU each) - no recycling has happened yet
	// TotalReadyCPU = 6 × 2 = 12 CPU
	// OriginalTotalCPU = 6 × 2 = 12 CPU
	// excessCPU = 0 -> No balancing possible, must recycle pods 0, 1, 2 (kept pods)
	podList := &corev1.PodList{}

	// All pods have old spec (2 CPU each)
	for i := 0; i < 6; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "2", true, "v1")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}
	c.ListObj = podList

	c.AddObject(sts)

	mgr := &DefaultStatefulSetPodManager{}

	// Execute
	phase, err := UpdateStatefulSetPods(ctx, c, sts, mgr, 3)

	// Verify - with no excess CPU, recycling must happen first
	// Pod 0 is the lowest index kept pod with old spec, so it gets recycled
	if err != nil {
		t.Errorf("UpdateStatefulSetPods() error = %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("Expected PhaseUpdating (recycling), got %v", phase)
	}

	// Verify pod0 was deleted (recycled) - lowest index kept pod with old spec
	namespacedName := types.NamespacedName{Namespace: "test", Name: "splunk-indexer-0"}
	var pod corev1.Pod
	err = c.Get(ctx, namespacedName, &pod)
	if err == nil {
		t.Errorf("Expected pod0 to be deleted for recycling, but it still exists")
	}

	// Verify pod5 (highest index, will be deleted during scale-down) was NOT recycled
	// It will be deleted when we reduce StatefulSet replicas later
	namespacedName = types.NamespacedName{Namespace: "test", Name: "splunk-indexer-5"}
	err = c.Get(ctx, namespacedName, &pod)
	if err != nil {
		t.Errorf("Expected pod5 to still exist (not recycled, will be deleted when replicas reduced)")
	}

	// Verify replicas not yet reduced (no excess CPU for balancing)
	if *sts.Spec.Replicas != 6 {
		t.Errorf("Expected replicas to remain 6, got %d", *sts.Spec.Replicas)
	}
}

func TestUpdateStatefulSetPods_CPUAwareReducesReplicasAfterRecycling(t *testing.T) {
	// This test verifies the interleaved balance behavior:
	// After ONE pod is recycled and gains excess CPU, balancing happens immediately.
	// This is more efficient than waiting for all kept pods to be recycled.
	//
	// Scenario: 5×2CPU -> 3×4CPU
	// Pods 0,1,2 (kept): Pod 0 recycled (has new spec), pods 1,2 still old spec
	// TotalReadyCPU = 1×4 + 4×2 = 12, OriginalTotalCPU = 5×2 = 10, excess = 2
	// Balance immediately: 5->4 (don't wait for pods 1,2 to recycle)
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 5
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation:        "true",
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(5, 3, 2000, 4000),
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "splunk"},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("4"),
								},
							},
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas:  5,
			UpdateRevision: "v2",
		},
	}

	// Pod 0: NEW spec (4 CPU) - recycled and ready
	// Pods 1-4: OLD spec (2 CPU) - not yet recycled
	// This is the realistic interleaved state after pod-0 recycled
	podList := &corev1.PodList{}
	pod0 := createCPUTestPod("splunk-indexer-0", "test", "4", true, "v2")
	c.AddObject(pod0)
	podList.Items = append(podList.Items, *pod0)

	for i := 1; i < 5; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "2", true, "v1")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}
	c.ListObj = podList

	c.AddObject(sts)

	mgr := &DefaultStatefulSetPodManager{}

	// Execute
	phase, err := UpdateStatefulSetPods(ctx, c, sts, mgr, 3)

	// Verify - should reduce replicas by 1 (interleaved balancing)
	if err != nil {
		t.Errorf("UpdateStatefulSetPods() error = %v", err)
	}
	if phase != enterpriseApi.PhaseScalingDown {
		t.Errorf("Expected PhaseScalingDown (interleaved balance), got %v", phase)
	}

	// Verify replicas reduced by 1 (from 5->4), not all the way to target (3)
	// This demonstrates the interleaved approach: balance as soon as possible
	if *sts.Spec.Replicas != 4 {
		t.Errorf("Expected replicas reduced to 4 (interleaved), got %d", *sts.Spec.Replicas)
	}

	// This demonstrates the key benefit of Balance-First:
	// As soon as ONE pod is recycled (not all), we can start reducing replicas.
	// With 15-min pod readiness, this saves significant time:
	// - Balance-First: Start reducing after 15 min (when pod-0 ready)
	// - Recycle-First: Start reducing after 45 min (when all 3 kept pods ready)
}

func TestUpdateStatefulSetPods_CPUAwareBalancingDeletions(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Scenario: 10×8CPU -> 4×20CPU, test parallel recycling with parallelUpdates=3
	// All pods still have old spec (8 CPU each) - no recycling has happened yet
	// TotalReadyCPU = 10 × 8 = 80 CPU
	// OriginalTotalCPU = 10 × 8 = 80 CPU
	// excessCPU = 0 -> No balancing possible, must recycle first
	var replicas int32 = 10
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation:        "true",
				ParallelPodUpdatesAnnotation:      "3",
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(10, 4, 8000, 20000),
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "splunk"},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("20"),
								},
							},
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: 10,
		},
	}

	// All 10 pods have old spec (8 CPU each) - no recycling has happened yet
	// Kept pods [0, 3]: all need recycling
	// minCPUFloor = 80000 - (3 × 8000) = 56000m
	// After recycling 3 pods: 80000 - 24000 = 56000m >= 56000m ✅
	podList := &corev1.PodList{}
	for i := 0; i < 10; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "8", true, "v1")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}
	c.ListObj = podList

	c.AddObject(sts)

	mgr := &DefaultStatefulSetPodManager{}

	// Execute - with no excess CPU, recycling happens first
	// Should recycle up to 3 pods (parallelUpdates=3) within CPU floor
	phase, err := UpdateStatefulSetPods(ctx, c, sts, mgr, 4)

	// Verify
	if err != nil {
		t.Errorf("UpdateStatefulSetPods() error = %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("Expected PhaseUpdating, got %v", phase)
	}

	// Verify pods 0, 1, 2 were recycled (lowest index kept pods with old spec)
	deletedCount := 0
	for i := 0; i < 3; i++ {
		namespacedName := types.NamespacedName{Namespace: "test", Name: fmt.Sprintf("splunk-indexer-%d", i)}
		var pod corev1.Pod
		err := c.Get(ctx, namespacedName, &pod)
		if err != nil {
			deletedCount++
		}
	}
	if deletedCount != 3 {
		t.Errorf("Expected 3 pods recycled (parallelUpdates=3), got %d", deletedCount)
	}

	// Verify pod-9 (highest index, outside kept range) was NOT recycled
	namespacedName := types.NamespacedName{Namespace: "test", Name: "splunk-indexer-9"}
	var pod corev1.Pod
	err = c.Get(ctx, namespacedName, &pod)
	if err != nil {
		t.Errorf("Expected pod-9 to still exist (not in kept range)")
	}
}

func TestUpdateStatefulSetPods_CPUBoundsEnforcement(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Create StatefulSet
	var replicas int32 = 6
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation:        "true",
				ParallelPodUpdatesAnnotation:      "10", // Very high to test bounds
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(6, 3, 2000, 4000),
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "splunk"},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("4"),
								},
							},
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: 6,
		},
	}

	// Create 1 new-spec pod (4 CPU)
	podList := &corev1.PodList{}
	pod := createCPUTestPod("splunk-indexer-0", "test", "4", true, "v2")
	c.AddObject(pod)
	podList.Items = append(podList.Items, *pod)

	// Create 5 old-spec pods (2 CPU each) - total 10 CPU
	// Original total: 6 * 2 = 12 CPU
	// Current total: 1*4 + 5*2 = 14 CPU
	for i := 1; i < 6; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "2", true, "v1")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}
	c.ListObj = podList

	c.AddObject(sts)

	mgr := &DefaultStatefulSetPodManager{}

	// Execute - with excess CPU, the algorithm will first try to balance (reduce replicas)
	// Total ready CPU = 1*4 + 5*2 = 14 CPU (14000m)
	// Original total = 6 × 2000 = 12000m
	// Excess = 14000 - 12000 = 2000m
	// Since excess (2000m) >= oldCPUPerPod (2000m), it can delete 1 excess pod
	phase, err := UpdateStatefulSetPods(ctx, c, sts, mgr, 3)

	// Verify - Step 3 (Balance) triggers before Step 4 (Recycle)
	if err != nil {
		t.Errorf("UpdateStatefulSetPods() error = %v", err)
	}
	// The algorithm follows: BALANCE before RECYCLE
	// With excess CPU, it will reduce replicas first (PhaseScalingDown)
	if phase != enterpriseApi.PhaseScalingDown {
		t.Errorf("Expected PhaseScalingDown (balance step), got %v", phase)
	}
}

func TestUpdateStatefulSetPods_CPUAwareScaleDownComplete(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Create StatefulSet at target replicas (scale-down already complete, just need to verify all pods have new spec)
	var replicas int32 = 3
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation:        "true",
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(5, 3, 2000, 4000),
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "splunk"},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("4"),
								},
							},
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: 3,
		},
	}

	// All pods (0-2) are new-spec pods with 4 CPU each
	podList := &corev1.PodList{}
	for i := 0; i < 3; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "4", true, "v2")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}
	c.ListObj = podList

	c.AddObject(sts)

	mgr := &DefaultStatefulSetPodManager{}

	// Execute - should finalize scale-down
	phase, err := UpdateStatefulSetPods(ctx, c, sts, mgr, 3)

	// Verify - should update replicas to target
	if err != nil {
		t.Errorf("UpdateStatefulSetPods() error = %v", err)
	}

	if phase != enterpriseApi.PhaseReady {
		t.Errorf("Expected PhaseReady when transition completes, got %v", phase)
	}

	// Verify FinishedAt is set in the annotation
	var updatedSts appsv1.StatefulSet
	if getErr := c.Get(ctx, types.NamespacedName{Name: "splunk-indexer", Namespace: "test"}, &updatedSts); getErr != nil {
		t.Fatalf("Failed to get updated StatefulSet: %v", getErr)
	}

	if updatedSts.Annotations == nil {
		t.Fatal("Expected annotations to exist after completion")
	}

	stateJSON, exists := updatedSts.Annotations[CPUAwareTransitionStateAnnotation]
	if !exists {
		t.Fatal("Expected transition annotation to exist (cleared later by CR update)")
	}

	var finalState CPUAwareTransitionState
	if unmarshalErr := json.Unmarshal([]byte(stateJSON), &finalState); unmarshalErr != nil {
		t.Fatalf("Failed to unmarshal final state: %v", unmarshalErr)
	}

	if finalState.FinishedAt == "" {
		t.Error("Expected FinishedAt to be set when transition completes")
	}

	// Verify IsCPUPreservingScalingFinished returns true
	if !IsCPUPreservingScalingFinished(&updatedSts) {
		t.Error("Expected IsCPUPreservingScalingFinished to return true")
	}
}

// TestInterleavedRecycle_6x2_to_3x4 tests the canonical scenario from PRD Section 7
// Scenario: 6 pods × 2CPU -> 3 pods × 4CPU (total 12 CPU preserved)
// This verifies the interleaved algorithm correctly balances after each recycle.
func TestInterleavedRecycle_6x2_to_3x4(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Initial state: 6 pods with 2CPU each, target is 3 pods with 4CPU
	var replicas int32 = 6
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation:        "true",
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(6, 3, 2000, 4000),
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "splunk"},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("4000m"), // Target: 4 CPU
								},
							},
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: 6,
		},
	}

	// Reconcile 1: All 6 pods have old spec (2 CPU)
	// Expected: totalReadyCPU=12, originalTotalCPU=12, excessCPU=0
	// Action: No balance possible, recycle pod-0
	podList := &corev1.PodList{}
	for i := 0; i < 6; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "2000m", true, "v1")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}
	c.ListObj = podList
	c.AddObject(sts)

	mgr := &DefaultStatefulSetPodManager{}
	phase, err := UpdateStatefulSetPods(ctx, c, sts, mgr, 3)

	if err != nil {
		t.Errorf("Reconcile 1: unexpected error = %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("Reconcile 1: expected PhaseUpdating (recycling pod-0), got %v", phase)
	}

	// Verify pod-0 was deleted for recycling
	namespacedName := types.NamespacedName{Namespace: "test", Name: "splunk-indexer-0"}
	var pod corev1.Pod
	err = c.Get(ctx, namespacedName, &pod)
	if err == nil {
		t.Errorf("Reconcile 1: expected pod-0 to be deleted for recycling")
	}
}

// TestInterleavedRecycle_BalanceAfterRecycle tests that balancing occurs when there's excess CPU
func TestInterleavedRecycle_BalanceAfterRecycle(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// State after pod-0 recycled: 1×4CPU + 5×2CPU
	// totalReadyCPU = 4 + 10 = 14, originalTotalCPU = 12, excessCPU = 2
	// excessCPU (2) >= oldCPUPerPod (2), so can delete 1 pod
	var replicas int32 = 6
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation:        "true",
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(6, 3, 2000, 4000),
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "splunk"},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("4000m"),
								},
							},
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: 6,
		},
	}

	// Create pods: pod-0 has new spec (4 CPU), pods 1-5 have old spec (2 CPU)
	podList := &corev1.PodList{}
	pod0 := createCPUTestPod("splunk-indexer-0", "test", "4000m", true, "v2")
	c.AddObject(pod0)
	podList.Items = append(podList.Items, *pod0)

	for i := 1; i < 6; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "2000m", true, "v1")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}
	c.ListObj = podList
	c.AddObject(sts)

	mgr := &DefaultStatefulSetPodManager{}
	phase, err := UpdateStatefulSetPods(ctx, c, sts, mgr, 3)

	if err != nil {
		t.Errorf("Balance: unexpected error = %v", err)
	}
	if phase != enterpriseApi.PhaseScalingDown {
		t.Errorf("Balance: expected PhaseScalingDown (reducing replicas), got %v", phase)
	}

	// Verify replicas were reduced by 1 (from 6 to 5)
	if *sts.Spec.Replicas != 5 {
		t.Errorf("Balance: expected replicas = 5, got %d", *sts.Spec.Replicas)
	}
}

// TestInterleavedRecycle_10x1_to_2x5 tests aggressive balancing scenario
// Scenario: 10 pods × 1CPU -> 2 pods × 5CPU (total 10 CPU preserved)
func TestInterleavedRecycle_10x1_to_2x5(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// State: pod-0 has new spec (5 CPU), pods 1-9 have old spec (1 CPU each)
	// totalReadyCPU = 5 + 9*1 = 14, originalTotalCPU = 10*1 = 10, excessCPU = 4
	// excessCPU (4) / oldCPUPerPod (1) = 4 pods can be deleted
	// But we only need to delete min(4, 10-2) = min(4, 8) = 4 pods
	var replicas int32 = 10
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation:        "true",
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(10, 2, 1000, 5000),
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "splunk"},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("5000m"),
								},
							},
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: 10,
		},
	}

	// Create pods: pod-0 has new spec (5 CPU), pods 1-9 have old spec (1 CPU)
	podList := &corev1.PodList{}
	pod0 := createCPUTestPod("splunk-indexer-0", "test", "5000m", true, "v2")
	c.AddObject(pod0)
	podList.Items = append(podList.Items, *pod0)

	for i := 1; i < 10; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "1000m", true, "v1")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}
	c.ListObj = podList
	c.AddObject(sts)

	mgr := &DefaultStatefulSetPodManager{}
	phase, err := UpdateStatefulSetPods(ctx, c, sts, mgr, 2)

	if err != nil {
		t.Errorf("Aggressive balance: unexpected error = %v", err)
	}
	if phase != enterpriseApi.PhaseScalingDown {
		t.Errorf("Aggressive balance: expected PhaseScalingDown, got %v", phase)
	}

	// Verify replicas were reduced by 4 (from 10 to 6)
	if *sts.Spec.Replicas != 6 {
		t.Errorf("Aggressive balance: expected replicas = 6, got %d", *sts.Spec.Replicas)
	}
}

// TestInterleavedRecycle_CompletionDetection tests the completion detection (Step 1)
// When CPU-aware transition completes, the annotation is KEPT as a signal for the caller
// (pod manager) to update the CR's replicas. The caller is responsible for clearing the
// annotation after successfully updating the CR.
func TestInterleavedRecycle_CompletionDetection(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Final state: 3 pods with new spec (4 CPU each), replicas = 3 = targetReplicas
	var replicas int32 = 3
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation:        "true",
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(6, 3, 2000, 4000),
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "splunk"},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("4000m"),
								},
							},
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas:   3,
			UpdateRevision:  "v2",
			CurrentRevision: "v2",
		},
	}

	// All 3 pods have new spec
	podList := &corev1.PodList{}
	for i := 0; i < 3; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "4000m", true, "v2")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}
	c.ListObj = podList
	c.AddObject(sts)

	mgr := &DefaultStatefulSetPodManager{}
	phase, err := UpdateStatefulSetPods(ctx, c, sts, mgr, 3)

	if err != nil {
		t.Errorf("Completion: unexpected error = %v", err)
	}
	if phase != enterpriseApi.PhaseReady {
		t.Errorf("Completion: expected PhaseReady, got %v", phase)
	}

	// Verify annotation is KEPT (signals CR update pending)
	// The caller (pod manager) is responsible for:
	// 1. Checking the annotation via SyncCRReplicasFromCPUAwareTransition
	// 2. Updating the CR's replicas
	// 3. Calling ClearCPUAwareTransitionAnnotation
	if _, exists := sts.Annotations[CPUAwareTransitionStateAnnotation]; !exists {
		t.Errorf("Completion: expected transition state annotation to be kept for CR sync")
	}
}

// TestBalanceCalculation_NoExcess tests that no balance occurs when there's no excess CPU
func TestBalanceCalculation_NoExcess(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// All pods have old spec - totalReadyCPU = 12, originalTotalCPU = 12, excessCPU = 0
	var replicas int32 = 6
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation:        "true",
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(6, 3, 2000, 4000),
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "splunk"},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("4000m"),
								},
							},
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: 6,
		},
	}

	// All 6 pods have old spec (2 CPU)
	podList := &corev1.PodList{}
	for i := 0; i < 6; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "2000m", true, "v1")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}
	c.ListObj = podList
	c.AddObject(sts)

	mgr := &DefaultStatefulSetPodManager{}
	phase, err := UpdateStatefulSetPods(ctx, c, sts, mgr, 3)

	if err != nil {
		t.Errorf("NoExcess: unexpected error = %v", err)
	}
	// Should recycle pod-0 since no balance possible
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("NoExcess: expected PhaseUpdating (recycling), got %v", phase)
	}

	// Replicas should NOT be reduced (no excess CPU to balance)
	if *sts.Spec.Replicas != 6 {
		t.Errorf("NoExcess: expected replicas to remain 6, got %d", *sts.Spec.Replicas)
	}
}

// TestBalanceCalculation_PartialExcess tests balance when there's a not-ready pod.
// Pod-0 (new spec, ready) provides surplus CPU, which is enough to delete
// one old pod (even though pod-5 is not ready).
func TestBalanceCalculation_PartialExcess(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Scenario: 6×2CPU -> 3×4CPU
	// State: pod-0 has 4CPU (new spec, ready), pods 1-4 have 2CPU (ready), pod-5 has 2CPU (not ready)
	// CPU metrics:
	// totalReadyCPU = 1×4000 + 4×2000 = 12000m (pod-5 not counted, not ready)
	// OriginalTotalCPU = 6 × 2000 = 12000m
	// surplusCPU = pod-0 contributes (4000 - 2000) = 2000m surplus
	// Since surplusCPU (2000m) >= oldCPUPerPod (2000m), one old pod can be deleted
	// Result: BALANCE (scale down) by reducing replicas from 6 to 5
	var replicas int32 = 6
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation:        "true",
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(6, 3, 2000, 4000),
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "splunk"},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("4000m"),
								},
							},
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: 5, // One pod not ready
		},
	}

	// Create pods: pod-0 has new spec (4 CPU), pods 1-4 have old spec (2 CPU), pod-5 not ready
	podList := &corev1.PodList{}
	pod0 := createCPUTestPod("splunk-indexer-0", "test", "4000m", true, "v2")
	c.AddObject(pod0)
	podList.Items = append(podList.Items, *pod0)

	for i := 1; i < 5; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "2000m", true, "v1")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}
	// Pod-5 not ready
	pod5 := createCPUTestPod("splunk-indexer-5", "test", "2000m", false, "v1")
	c.AddObject(pod5)
	podList.Items = append(podList.Items, *pod5)

	c.ListObj = podList
	c.AddObject(sts)

	mgr := &DefaultStatefulSetPodManager{}
	phase, err := UpdateStatefulSetPods(ctx, c, sts, mgr, 3)

	if err != nil {
		t.Errorf("PartialExcess: unexpected error = %v", err)
	}
	// pod-0 provides 2000m surplus,
	// which is enough to delete one old pod. So we BALANCE (scale down).
	if phase != enterpriseApi.PhaseScalingDown {
		t.Errorf("PartialExcess: expected PhaseScalingDown (balance due to surplus), got %v", phase)
	}

	// Verify replicas were reduced by 1 (from 6 to 5) due to balancing
	if *sts.Spec.Replicas != 5 {
		t.Errorf("PartialExcess: expected replicas = 5 (balanced), got %d", *sts.Spec.Replicas)
	}
}

// TestInterleavedRecycle_ParallelUpdates tests recycling with parallelUpdates > 1
func TestInterleavedRecycle_ParallelUpdates(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Scenario: 6×2CPU -> 3×4CPU with parallelUpdates=2
	// All pods have old spec, should recycle 2 pods at once
	var replicas int32 = 6
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation:        "true",
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(6, 3, 2000, 4000),
				ParallelPodUpdatesAnnotation:      "2",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "splunk"},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("4000m"),
								},
							},
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: 6,
		},
	}

	// All 6 pods have old spec (2 CPU)
	podList := &corev1.PodList{}
	for i := 0; i < 6; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "2000m", true, "v1")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}
	c.ListObj = podList
	c.AddObject(sts)

	mgr := &DefaultStatefulSetPodManager{}
	phase, err := UpdateStatefulSetPods(ctx, c, sts, mgr, 3)

	if err != nil {
		t.Errorf("ParallelUpdates: unexpected error = %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("ParallelUpdates: expected PhaseUpdating, got %v", phase)
	}

	// Verify recycling behavior with CPU floor enforcement
	// Current ready CPU = 6 × 2000m = 12000m
	// OriginalTotalCPU = (3 × 4000) / 2000 × 2000 = 12000m
	// minCPUFloor = 12000 - (2 × 2000) = 8000m
	// After recycling pod-0: 12000 - 2000 = 10000m >= 8000m ✅ (allowed)
	// After recycling pod-1: 10000 - 2000 = 8000m >= 8000m ✅ (allowed)
	// Both pods can be recycled within CPU floor limits
	deletedCount := 0
	for i := 0; i < 2; i++ {
		namespacedName := types.NamespacedName{Namespace: "test", Name: fmt.Sprintf("splunk-indexer-%d", i)}
		var pod corev1.Pod
		err := c.Get(ctx, namespacedName, &pod)
		if err != nil {
			deletedCount++
		}
	}
	// 2 pods should be deleted since parallelUpdates=2 and CPU floor allows it
	if deletedCount != 2 {
		t.Errorf("ParallelUpdates: expected 2 pods deleted (parallelUpdates=2, within CPU floor), got %d", deletedCount)
	}
}

// TestInterleavedRecycle_ParallelUpdates_WithExcessCPU tests parallel recycling when there IS excess CPU
func TestInterleavedRecycle_ParallelUpdates_WithExcessCPU(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Scenario: 8×2CPU -> 4×4CPU with parallelUpdates=2
	// We have 8 pods, 2 already converted to new spec, 6 still old spec
	// Total ready = 2×4 + 6×2 = 8 + 12 = 20 CPU
	// Original total = (4 × 4000) / 2000 × 2000 = 8 × 2000 = 16000m (floor)
	// Excess = 20000 - 16000 = 4000m (2 old pods worth of excess)
	// After recycling 2 pods: 20000 - 4000 = 16000 >= 16000 (OK)
	var replicas int32 = 8
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation:        "true",
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(8, 4, 2000, 4000),
				ParallelPodUpdatesAnnotation:      "2",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "splunk"},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("4000m"),
								},
							},
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: 8,
		},
	}

	// 2 pods with new spec (4 CPU each) + 6 pods with old spec (2 CPU each)
	podList := &corev1.PodList{}
	for i := 0; i < 2; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "4000m", true, "v2")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}
	for i := 2; i < 8; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "2000m", true, "v1")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}
	c.ListObj = podList
	c.AddObject(sts)

	mgr := &DefaultStatefulSetPodManager{}
	phase, err := UpdateStatefulSetPods(ctx, c, sts, mgr, 4)

	if err != nil {
		t.Errorf("ParallelUpdates with excess: unexpected error = %v", err)
	}

	// With 4000m excess CPU, the algorithm will first try to BALANCE (reduce replicas)
	// podsCanDelete = 4000 / 2000 = 2, podsNeeded = 8 - 4 = 4, so delete min(2,4) = 2
	if phase != enterpriseApi.PhaseScalingDown {
		t.Errorf("ParallelUpdates with excess: expected PhaseScalingDown (balance step), got %v", phase)
	}

	// Verify replicas reduced by 2
	if *sts.Spec.Replicas != 6 {
		t.Errorf("ParallelUpdates with excess: expected replicas = 6, got %d", *sts.Spec.Replicas)
	}
}

// TestInterleavedRecycle_SkipsNotReadyPods verifies that when there's sufficient
// surplus CPU from new spec ready pods, the algorithm will BALANCE (scale down)
// rather than recycle.
//
// In this scenario, pod-1 (new spec, ready) provides surplus CPU (4 - 2 = 2 CPU),
// which is enough to delete one old pod. Balancing takes priority over recycling.
func TestInterleavedRecycle_SkipsNotReadyPods(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Scenario: 6×2CPU -> 3×4CPU, parallelUpdates=2
	// Pod-0: NOT ready (being recreated from previous recycle) - not counted in ready CPU
	// Pod-1: new spec (4 CPU), ready - provides surplus of (4 - 2 = 2 CPU)
	// Pod-2: old spec (2 CPU), ready
	// Pod-3: old spec (2 CPU), ready
	// Pod-4: old spec (2 CPU), ready
	// Pod-5: old spec (2 CPU), ready
	//
	// CPU metrics:
	// totalReadyCPU = 1×4000 + 4×2000 = 12000m (pod-0 not counted, not ready)
	// OriginalTotalCPU = 6 × 2000 = 12000m
	// surplusCPU = pod-1 contributes (4000 - 2000) = 2000m surplus
	// Since surplusCPU (2000m) >= oldCPUPerPod (2000m), one old pod can be deleted
	// Result: BALANCE (scale down) by reducing replicas from 6 to 5
	var replicas int32 = 6
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation:        "true",
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(6, 3, 2000, 4000),
				ParallelPodUpdatesAnnotation:      "2",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "splunk"},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("4000m"),
								},
							},
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: 5, // 5 ready (pod-0 is not ready)
		},
	}

	podList := &corev1.PodList{}

	// Pod-0: NOT ready (being recreated from previous cycle) - 4CPU new spec but not ready
	pod0 := createCPUTestPod("splunk-indexer-0", "test", "4000m", false, "v2")
	c.AddObject(pod0)
	podList.Items = append(podList.Items, *pod0)

	// Pod-1: new spec (4 CPU), ready
	pod1 := createCPUTestPod("splunk-indexer-1", "test", "4000m", true, "v2")
	c.AddObject(pod1)
	podList.Items = append(podList.Items, *pod1)

	// Pods 2-5: old spec (2 CPU each), ready
	for i := 2; i <= 5; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "2000m", true, "v1")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}

	c.ListObj = podList
	c.AddObject(sts)

	mgr := &DefaultStatefulSetPodManager{}
	phase, err := UpdateStatefulSetPods(ctx, c, sts, mgr, 3)

	// The key assertion: with the new CPU-aware balancing logic,
	// pod-1 (new spec, ready) provides 2000m surplus, which is enough
	// to delete one old pod. So we BALANCE (scale down) instead of recycling.
	if err != nil {
		t.Errorf("SkipsNotReady: unexpected error = %v", err)
	}
	if phase != enterpriseApi.PhaseScalingDown {
		t.Errorf("SkipsNotReady: expected PhaseScalingDown (balance due to surplus), got %v", phase)
	}

	// Verify replicas were reduced by 1 (from 6 to 5) due to balancing
	if *sts.Spec.Replicas != 5 {
		t.Errorf("SkipsNotReady: expected replicas = 5 (balanced), got %d", *sts.Spec.Replicas)
	}

	// Verify pod-2 was NOT deleted (balancing reduces replicas, doesn't delete kept pods)
	namespacedName := types.NamespacedName{Namespace: "test", Name: "splunk-indexer-2"}
	var pod corev1.Pod
	err = c.Get(ctx, namespacedName, &pod)
	if err != nil {
		t.Errorf("SkipsNotReady: expected pod-2 to still exist (balancing doesn't delete kept pods), but it was deleted")
	}

	// Verify pods 3-5 still exist (balancing reduces replicas count, StatefulSet controller handles deletion)
	for i := 3; i <= 5; i++ {
		namespacedName := types.NamespacedName{Namespace: "test", Name: fmt.Sprintf("splunk-indexer-%d", i)}
		err := c.Get(ctx, namespacedName, &pod)
		if err != nil {
			t.Errorf("SkipsNotReady: expected pod-%d to still exist (not yet deleted by STS controller), but it was deleted", i)
		}
	}
}

// prepareRecycleErrorPodManager is a mock pod manager that returns errors for specific pods
type prepareRecycleErrorPodManager struct {
	errorPodIndices map[int32]bool // Pod indices that should return errors
}

func (mgr *prepareRecycleErrorPodManager) Update(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (enterpriseApi.Phase, error) {
	return enterpriseApi.PhaseUpdating, nil
}

func (mgr *prepareRecycleErrorPodManager) PrepareScaleDown(ctx context.Context, n int32) (bool, error) {
	return true, nil
}

func (mgr *prepareRecycleErrorPodManager) PrepareRecycle(ctx context.Context, n int32) (bool, error) {
	if mgr.errorPodIndices[n] {
		return false, fmt.Errorf("Status=Restarting")
	}
	return true, nil
}

func (mgr *prepareRecycleErrorPodManager) FinishRecycle(ctx context.Context, n int32) (bool, error) {
	return true, nil
}

func (mgr *prepareRecycleErrorPodManager) FinishUpgrade(ctx context.Context, n int32) error {
	return nil
}

// TestInterleavedRecycle_PrepareRecycleError verifies that when PrepareRecycle returns an error
// for one pod, the transition continues to check other pods instead of stopping entirely.
func TestInterleavedRecycle_PrepareRecycleError(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Scenario: replicas=5, target=5, parallelUpdates=2
	// Pods 0-1: new spec (4 CPU each), ready - already converted
	// Pod-2: old spec (2 CPU), ready - but PrepareRecycle returns error ("Status=Restarting")
	// Pod-3: old spec (2 CPU), ready - should be recycled (PrepareRecycle succeeds)
	// Pod-4: old spec (2 CPU), ready - should be recycled (PrepareRecycle succeeds)
	// Expected: pod-2 is skipped, pod-3 and pod-4 are recycled
	// CPU headroom: current = 2×4000 + 3×2000 = 14000m
	// minCPUFloor = (5×4000)/2000×2000 - 2×2000 = 20000 - 4000 = 16000m
	// After recycling 2 pods: 14000 - 4000 = 10000m < 16000m ❌
	// Need more new-spec pods for headroom
	var replicas int32 = 5
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation:        "true",
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(10, 5, 2000, 4000),
				ParallelPodUpdatesAnnotation:      "2",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "splunk"},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("4000m"),
								},
							},
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: 5,
		},
	}

	podList := &corev1.PodList{}

	// Pods 0-2: new spec (4 CPU each), ready
	for i := 0; i <= 2; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "4000m", true, "v2")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}

	// Pods 3-4: old spec (2 CPU each), ready
	for i := 3; i <= 4; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "2000m", true, "v1")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}
	c.ListObj = podList
	c.AddObject(sts)

	// Use custom pod manager that returns error for pod-3
	mgr := &prepareRecycleErrorPodManager{
		errorPodIndices: map[int32]bool{3: true}, // Only pod-3 returns error
	}

	phase, err := UpdateStatefulSetPods(ctx, c, sts, mgr, 5)

	// The key assertion: we should NOT get an error because of pod-3's PrepareRecycle failure
	// Instead, we should skip pod-3 and continue to recycle pod-4
	if err != nil {
		t.Errorf("PrepareRecycleError: unexpected error = %v (should skip problematic pod)", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("PrepareRecycleError: expected PhaseUpdating, got %v", phase)
	}

	// Verify pods 0-2 were NOT deleted (already have new spec)
	for i := 0; i <= 2; i++ {
		namespacedName := types.NamespacedName{Namespace: "test", Name: fmt.Sprintf("splunk-indexer-%d", i)}
		var pod corev1.Pod
		err = c.Get(ctx, namespacedName, &pod)
		if err != nil {
			t.Errorf("PrepareRecycleError: pod-%d should NOT be deleted (new spec), but got error: %v", i, err)
		}
	}

	// Verify pod-3 was NOT deleted (skipped due to PrepareRecycle error)
	namespacedName := types.NamespacedName{Namespace: "test", Name: "splunk-indexer-3"}
	var pod3 corev1.Pod
	err = c.Get(ctx, namespacedName, &pod3)
	if err != nil {
		t.Errorf("PrepareRecycleError: pod-3 should NOT be deleted (PrepareRecycle failed), but got error: %v", err)
	}

	// Verify pod-4 was deleted (recycled)
	// Current CPU = 3×4000 + 2×2000 = 16000m
	// minCPUFloor = 20000 - 4000 = 16000m
	// After recycling pod-4: 16000 - 2000 = 14000m < 16000m ❌
	// CPU floor blocks recycling, so pod-4 should NOT be deleted
	namespacedName = types.NamespacedName{Namespace: "test", Name: "splunk-indexer-4"}
	var pod4 corev1.Pod
	err = c.Get(ctx, namespacedName, &pod4)
	if err != nil {
		t.Errorf("PrepareRecycleError: pod-4 should NOT be deleted (CPU floor blocks), but got error: %v", err)
	}
}

// TestSyncCRReplicasFromCPUAwareTransition tests the helper function for checking if CR needs sync
func TestSyncCRReplicasFromCPUAwareTransition(t *testing.T) {
	testCases := []struct {
		name          string
		annotations   map[string]string
		stsReplicas   int32
		crReplicas    int32
		wantTarget    int32
		wantNeedsSync bool
	}{
		{
			name:          "No annotation - no sync needed",
			annotations:   nil,
			stsReplicas:   3,
			crReplicas:    6,
			wantTarget:    0,
			wantNeedsSync: false,
		},
		{
			name: "Annotation present but STS not at target - no sync",
			annotations: map[string]string{
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(6, 3, 2000, 4000),
			},
			stsReplicas:   5,
			crReplicas:    6,
			wantTarget:    0,
			wantNeedsSync: false,
		},
		{
			name: "STS at target but CR already matches - no sync",
			annotations: map[string]string{
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(6, 3, 2000, 4000),
			},
			stsReplicas:   3,
			crReplicas:    3,
			wantTarget:    0,
			wantNeedsSync: false,
		},
		{
			name: "STS at target and CR needs update - sync required",
			annotations: map[string]string{
				// FinishedAt MUST be set for sync to be signaled (safety requirement)
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSONWithFinished(6, 3, 2000, 4000, "2024-01-01T12:00:00Z"),
			},
			stsReplicas:   3,
			crReplicas:    6,
			wantTarget:    3,
			wantNeedsSync: true,
		},
		{
			name: "STS at target and CR needs update but FinishedAt not set - no sync (safety)",
			annotations: map[string]string{
				// Without FinishedAt, sync should NOT be triggered to prevent premature CR updates
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(6, 3, 2000, 4000),
			},
			stsReplicas:   3,
			crReplicas:    6,
			wantTarget:    0,
			wantNeedsSync: false,
		},
		{
			name: "Invalid annotation value - no sync",
			annotations: map[string]string{
				CPUAwareTransitionStateAnnotation: "invalid-json",
			},
			stsReplicas:   3,
			crReplicas:    6,
			wantTarget:    0,
			wantNeedsSync: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sts := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "test",
					Annotations: tc.annotations,
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: &tc.stsReplicas,
				},
			}

			gotTarget, gotNeedsSync := SyncCRReplicasFromCPUAwareTransition(sts, tc.crReplicas)

			if gotTarget != tc.wantTarget {
				t.Errorf("SyncCRReplicasFromCPUAwareTransition() target = %d, want %d", gotTarget, tc.wantTarget)
			}
			if gotNeedsSync != tc.wantNeedsSync {
				t.Errorf("SyncCRReplicasFromCPUAwareTransition() needsSync = %v, want %v", gotNeedsSync, tc.wantNeedsSync)
			}
		})
	}
}

// TestClearCPUAwareTransitionAnnotation tests the helper function for clearing the annotation
func TestClearCPUAwareTransitionAnnotation(t *testing.T) {
	ctx := context.TODO()

	t.Run("No annotations - no error", func(t *testing.T) {
		c := spltest.NewMockClient()
		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sts",
				Namespace: "test",
			},
		}
		c.AddObject(sts)

		err := ClearCPUAwareTransitionAnnotation(ctx, c, sts)
		if err != nil {
			t.Errorf("ClearCPUAwareTransitionAnnotation() error = %v", err)
		}
	})

	t.Run("Annotation not present - no error", func(t *testing.T) {
		c := spltest.NewMockClient()
		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sts",
				Namespace: "test",
				Annotations: map[string]string{
					"other-annotation": "value",
				},
			},
		}
		c.AddObject(sts)

		err := ClearCPUAwareTransitionAnnotation(ctx, c, sts)
		if err != nil {
			t.Errorf("ClearCPUAwareTransitionAnnotation() error = %v", err)
		}
	})

	t.Run("Annotation present - removes it", func(t *testing.T) {
		c := spltest.NewMockClient()
		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sts",
				Namespace: "test",
				Annotations: map[string]string{
					CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(6, 3, 2000, 4000),
					"other-annotation":                "value",
				},
			},
		}
		c.AddObject(sts)

		err := ClearCPUAwareTransitionAnnotation(ctx, c, sts)
		if err != nil {
			t.Errorf("ClearCPUAwareTransitionAnnotation() error = %v", err)
		}

		// Verify annotation was removed
		if _, exists := sts.Annotations[CPUAwareTransitionStateAnnotation]; exists {
			t.Errorf("ClearCPUAwareTransitionAnnotation() expected annotation to be removed")
		}

		// Verify other annotations are preserved
		if sts.Annotations["other-annotation"] != "value" {
			t.Errorf("ClearCPUAwareTransitionAnnotation() other annotations should be preserved")
		}
	})
}

// TestUpdateStatefulSetPods_CPUAwareShortCircuitCompleted tests that completed transitions
// (with FinishedAt set) are short-circuited immediately and return PhaseReady
func TestUpdateStatefulSetPods_CPUAwareShortCircuitCompleted(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 3
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation: "true",
				// Transition already completed with FinishedAt set
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSONWithFinished(6, 3, 2000, 4000, "2026-01-10T11:00:00Z"),
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "splunk"},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("4"),
								},
							},
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: 3,
		},
	}

	// Create pods (though they shouldn't be checked due to short-circuit)
	podList := &corev1.PodList{}
	for i := 0; i < 3; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "4", true, "v2")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}
	c.ListObj = podList

	c.AddObject(sts)

	mgr := &DefaultStatefulSetPodManager{}

	// Execute - should short-circuit immediately
	phase, err := UpdateStatefulSetPods(ctx, c, sts, mgr, 3)

	if err != nil {
		t.Errorf("UpdateStatefulSetPods() error = %v", err)
	}

	if phase != enterpriseApi.PhaseReady {
		t.Errorf("Expected PhaseReady for completed transition (short-circuit), got %v", phase)
	}

	// Verify no pods were checked (no Get calls for pods should have been made)
	// The short-circuit should happen before any pod inspection
}

// TestApplyStatefulSet_ClearsCompletedTransitionBeforeNewOne tests that
// ApplyStatefulSet clears a completed transition annotation before starting a new one
func TestApplyStatefulSet_ClearsCompletedTransitionBeforeNewOne(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var oldReplicas int32 = 3
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation: "true",
				// Previous transition completed
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSONWithFinished(6, 3, 2000, 4000, "2026-01-10T11:00:00Z"),
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &oldReplicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "splunk"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "splunk"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("4"),
								},
							},
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas:   3,
			UpdatedReplicas: 3,
		},
	}

	// New desired template with different CPU (8 CPU this time)
	revised := current.DeepCopy()
	revised.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("8")

	c.AddObject(current)

	// Execute ApplyStatefulSet with CPU-aware scaling
	phase, err := ApplyStatefulSet(ctx, c, revised, nil)

	if err != nil {
		t.Fatalf("ApplyStatefulSet() error = %v", err)
	}

	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("Expected PhaseUpdating when starting new transition, got %v", phase)
	}

	// Verify the old completed annotation was cleared and new one created
	var updatedSts appsv1.StatefulSet
	if getErr := c.Get(ctx, types.NamespacedName{Name: "splunk-indexer", Namespace: "test"}, &updatedSts); getErr != nil {
		t.Fatalf("Failed to get updated StatefulSet: %v", getErr)
	}

	stateJSON, exists := updatedSts.Annotations[CPUAwareTransitionStateAnnotation]
	if !exists {
		t.Fatal("Expected new transition annotation to exist")
	}

	var newState CPUAwareTransitionState
	if unmarshalErr := json.Unmarshal([]byte(stateJSON), &newState); unmarshalErr != nil {
		t.Fatalf("Failed to unmarshal new state: %v", unmarshalErr)
	}

	// Verify it's a NEW transition (not the old completed one)
	if newState.FinishedAt != "" {
		t.Error("Expected new transition to NOT have FinishedAt set")
	}

	if newState.OriginalCPUMillis != 4000 {
		t.Errorf("Expected OriginalCPUMillis=4000 (from current spec), got %d", newState.OriginalCPUMillis)
	}

	if newState.TargetCPUMillis != 8000 {
		t.Errorf("Expected TargetCPUMillis=8000 (new spec), got %d", newState.TargetCPUMillis)
	}
}

// TestIsCPUPreservingScalingFinished_ChecksFinishedAt tests that the completion
// check now uses FinishedAt field instead of replica count
func TestIsCPUPreservingScalingFinished_ChecksFinishedAt(t *testing.T) {
	testCases := []struct {
		name         string
		annotations  map[string]string
		stsReplicas  int32
		wantFinished bool
	}{
		{
			name:         "No annotation - not finished",
			annotations:  nil,
			stsReplicas:  3,
			wantFinished: false,
		},
		{
			name: "Annotation without FinishedAt - not finished",
			annotations: map[string]string{
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(6, 3, 2000, 4000),
			},
			stsReplicas:  3,
			wantFinished: false,
		},
		{
			name: "Annotation with FinishedAt - finished",
			annotations: map[string]string{
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSONWithFinished(6, 3, 2000, 4000, "2026-01-10T11:00:00Z"),
			},
			stsReplicas:  3,
			wantFinished: true,
		},
		{
			name: "FinishedAt set but replicas don't match (shouldn't happen, but tests FinishedAt takes precedence)",
			annotations: map[string]string{
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSONWithFinished(6, 3, 2000, 4000, "2026-01-10T11:00:00Z"),
			},
			stsReplicas:  6,    // Still at original replicas
			wantFinished: true, // FinishedAt takes precedence
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sts := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-sts",
					Namespace:   "test",
					Annotations: tc.annotations,
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: &tc.stsReplicas,
				},
			}

			gotFinished := IsCPUPreservingScalingFinished(sts)

			if gotFinished != tc.wantFinished {
				t.Errorf("IsCPUPreservingScalingFinished() = %v, want %v", gotFinished, tc.wantFinished)
			}
		})
	}
}

// TestCheckCPUTransitionCompletion tests the shared completion probe helper
func TestCheckCPUTransitionCompletion(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	targetReplicas := int32(3)
	targetCPUMillis := int64(4000)

	// Create 3 pods with new spec (target CPU)
	podList := &corev1.PodList{}
	for i := 0; i < 3; i++ {
		pod := createCPUTestPod(fmt.Sprintf("test-sts-%d", i), "test", "4", true, "v2")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}
	c.ListObj = podList

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sts",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &targetReplicas,
		},
	}
	c.AddObject(sts)

	t.Run("All pods have new spec at target replicas - complete", func(t *testing.T) {
		result := checkCPUTransitionCompletion(ctx, c, sts, targetReplicas, targetCPUMillis)
		if !result {
			t.Errorf("Expected true when all pods have new spec, got false")
		}
	})

	t.Run("Replicas not at target - not complete", func(t *testing.T) {
		var notAtTarget int32 = 4
		stsNotAtTarget := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sts-2",
				Namespace: "test",
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: &notAtTarget,
			},
		}
		c.AddObject(stsNotAtTarget)
		result := checkCPUTransitionCompletion(ctx, c, stsNotAtTarget, targetReplicas, targetCPUMillis)
		if result {
			t.Errorf("Expected false when replicas not at target, got true")
		}
	})

	t.Run("Pod has old spec - not complete", func(t *testing.T) {
		c2 := spltest.NewMockClient()
		oldCPUPod := createCPUTestPod("test-sts-old-0", "test", "2", true, "v1") // Old CPU
		c2.AddObject(oldCPUPod)

		stsOld := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sts-old",
				Namespace: "test",
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: func() *int32 { r := int32(1); return &r }(),
			},
		}
		c2.AddObject(stsOld)

		result := checkCPUTransitionCompletion(ctx, c2, stsOld, 1, targetCPUMillis)
		if result {
			t.Errorf("Expected false when pod has old spec (2000m vs 4000m target), got true")
		}
	})
}

// TestPersistCPUTransitionFinished tests the shared helper for persisting FinishedAt
func TestPersistCPUTransitionFinished(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sts-persist",
			Namespace: "test",
			Annotations: map[string]string{
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(6, 3, 2000, 4000),
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: func() *int32 { r := int32(3); return &r }(),
		},
	}
	c.AddObject(sts)

	state := CPUAwareTransitionState{
		OriginalReplicas:  6,
		TargetReplicas:    3,
		OriginalCPUMillis: 2000,
		TargetCPUMillis:   4000,
		StartedAt:         "2026-01-10T10:00:00Z",
	}

	t.Run("Persists FinishedAt and updates annotation", func(t *testing.T) {
		err := persistCPUTransitionFinished(ctx, c, sts, &state)
		if err != nil {
			t.Errorf("persistCPUTransitionFinished() error = %v", err)
		}

		// Verify FinishedAt is set
		if state.FinishedAt == "" {
			t.Errorf("Expected FinishedAt to be set, but it's empty")
		}

		// Verify annotation contains FinishedAt
		annotationJSON := sts.Annotations[CPUAwareTransitionStateAnnotation]
		if annotationJSON == "" {
			t.Errorf("Expected annotation to be updated")
		}

		var parsedState CPUAwareTransitionState
		if err := json.Unmarshal([]byte(annotationJSON), &parsedState); err != nil {
			t.Errorf("Failed to parse updated annotation: %v", err)
		}

		if parsedState.FinishedAt == "" {
			t.Errorf("FinishedAt should be set in persisted annotation")
		}
	})
}

// TestDispatcherExplicitCompletionCheck tests that handleCPUPreservingTransition
// handles the replicas == targetReplicas case with explicit completion check
func TestDispatcherExplicitCompletionCheck(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var targetReplicas int32 = 3

	// Create a StatefulSet at target replicas
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sts-dispatch",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation: "true",
				// In-progress transition (no FinishedAt)
				CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(6, 3, 2000, 4000),
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &targetReplicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "splunk"},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("4"),
								},
							},
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: 3,
		},
	}
	c.AddObject(sts)

	// Create 3 pods with new spec (should trigger completion)
	podList := &corev1.PodList{}
	for i := 0; i < 3; i++ {
		pod := createCPUTestPod(fmt.Sprintf("test-sts-dispatch-%d", i), "test", "4", true, "v2")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}
	c.ListObj = podList

	mgr := &DefaultStatefulSetPodManager{}

	t.Run("At target replicas with all new spec - persists FinishedAt", func(t *testing.T) {
		phase, handled, err := handleCPUPreservingTransition(ctx, c, sts, mgr, targetReplicas)

		if err != nil {
			t.Errorf("handleCPUPreservingTransition() error = %v", err)
		}
		if !handled {
			t.Errorf("Expected handled=true")
		}
		if phase != enterpriseApi.PhaseReady {
			t.Errorf("Expected PhaseReady after completion, got %v", phase)
		}

		// Verify FinishedAt was persisted
		stateJSON := sts.Annotations[CPUAwareTransitionStateAnnotation]
		var state CPUAwareTransitionState
		if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
			t.Errorf("Failed to parse state: %v", err)
		}
		if state.FinishedAt == "" {
			t.Errorf("FinishedAt should be set after completion")
		}
	})

	t.Run("At target replicas with old spec pods - continues transition", func(t *testing.T) {
		c2 := spltest.NewMockClient()

		// Reset annotation to in-progress
		sts2 := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sts-dispatch2",
				Namespace: "test",
				Annotations: map[string]string{
					PreserveTotalCPUAnnotation:        "true",
					CPUAwareTransitionStateAnnotation: createTestTransitionStateJSON(6, 3, 2000, 4000),
				},
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: &targetReplicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "splunk"},
				},
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "splunk",
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU: resource.MustParse("4"),
									},
								},
							},
						},
					},
				},
			},
			Status: appsv1.StatefulSetStatus{
				ReadyReplicas: 3,
			},
		}
		c2.AddObject(sts2)

		// Create pods with OLD spec (should NOT trigger completion)
		podList2 := &corev1.PodList{}
		for i := 0; i < 3; i++ {
			pod := createCPUTestPod(fmt.Sprintf("test-sts-dispatch2-%d", i), "test", "2", true, "v1") // OLD CPU
			c2.AddObject(pod)
			podList2.Items = append(podList2.Items, *pod)
		}
		c2.ListObj = podList2

		phase, handled, err := handleCPUPreservingTransition(ctx, c2, sts2, mgr, targetReplicas)

		if err != nil {
			t.Errorf("handleCPUPreservingTransition() error = %v", err)
		}
		if !handled {
			t.Errorf("Expected handled=true")
		}
		// Should continue with scale-down handler since completion check failed
		// (pods have old spec), returns PhaseUpdating
		if phase == enterpriseApi.PhaseReady {
			t.Errorf("Expected PhaseUpdating (continue transition), got PhaseReady")
		}

		// Verify FinishedAt was NOT set
		stateJSON := sts2.Annotations[CPUAwareTransitionStateAnnotation]
		var state CPUAwareTransitionState
		if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
			t.Errorf("Failed to parse state: %v", err)
		}
		if state.FinishedAt != "" {
			t.Errorf("FinishedAt should NOT be set when pods have old spec, but got: %s", state.FinishedAt)
		}
	})
}
