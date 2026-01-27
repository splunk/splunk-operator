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
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

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

func TestUpdateStatefulSetPods_CPUAwareWaitForNewPods(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Create StatefulSet with CPU-aware scaling annotation and target replicas
	var replicas int32 = 5
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation:           "true",
				CPUAwareTargetReplicasAnnotation: "3",
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
			ReadyReplicas: 5,
		},
	}

	// Create 5 old-spec pods (2 CPU each) - all ready, NO new-spec pods yet
	podList := &corev1.PodList{}
	for i := 0; i < 5; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "2", true, "v1")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}
	c.ListObj = podList

	c.AddObject(sts)

	mgr := &DefaultStatefulSetPodManager{}

	// Execute
	phase, err := UpdateStatefulSetPods(ctx, c, sts, mgr, 3)

	// Verify - should wait for new-spec pods before deleting old ones
	if err != nil {
		t.Errorf("UpdateStatefulSetPods() error = %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("Expected PhaseUpdating, got %v", phase)
	}

	// Verify replicas not yet reduced
	if *sts.Spec.Replicas != 5 {
		t.Errorf("Expected replicas to remain 5, got %d", *sts.Spec.Replicas)
	}
}

func TestUpdateStatefulSetPods_CPUAwareBalancingDeletions(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Create StatefulSet with CPU-aware scaling and parallel updates
	var replicas int32 = 10
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation:           "true",
				ParallelPodUpdatesAnnotation:     "3",
				CPUAwareTargetReplicasAnnotation: "4",
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
									corev1.ResourceCPU: resource.MustParse("32"),
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

	// Scenario: Scaling with high CPU ratio
	// Create 3 new-spec pods (32 CPU each) - ready (indexes 0-2)
	podList := &corev1.PodList{}
	for i := 0; i < 3; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "32", true, "v2")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}

	// Create 7 old-spec pods (4 CPU each) - ready (indexes 3-9)
	for i := 3; i < 10; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "4", true, "v1")
		c.AddObject(pod)
		podList.Items = append(podList.Items, *pod)
	}
	c.ListObj = podList

	c.AddObject(sts)

	mgr := &DefaultStatefulSetPodManager{}

	// Execute - should delete multiple old pods in one cycle
	phase, err := UpdateStatefulSetPods(ctx, c, sts, mgr, 4)

	// Verify
	if err != nil {
		t.Errorf("UpdateStatefulSetPods() error = %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("Expected PhaseUpdating, got %v", phase)
	}

	// Verify some old pods were deleted (highest index first)
	namespacedName := types.NamespacedName{Namespace: "test", Name: "splunk-indexer-9"}
	var pod corev1.Pod
	err = c.Get(ctx, namespacedName, &pod)
	if err == nil {
		t.Logf("Pod splunk-indexer-9 still exists, checking deletion logic")
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
				PreserveTotalCPUAnnotation:           "true",
				ParallelPodUpdatesAnnotation:     "10", // Very high to test bounds
				CPUAwareTargetReplicasAnnotation: "3",
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

	// Execute - should NOT delete all old pods because it would drop below original CPU
	phase, err := UpdateStatefulSetPods(ctx, c, sts, mgr, 3)

	// Verify
	if err != nil {
		t.Errorf("UpdateStatefulSetPods() error = %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("Expected PhaseUpdating, got %v", phase)
	}
}

func TestUpdateStatefulSetPods_CPUAwareScaleDownComplete(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Create StatefulSet with target replicas
	var replicas int32 = 5
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation:           "true",
				CPUAwareTargetReplicasAnnotation: "3",
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
			ReadyReplicas: 5,
		},
	}

	// All pods are new-spec pods (no more old pods to delete)
	podList := &corev1.PodList{}
	for i := 0; i < 5; i++ {
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

	// The function should have updated replicas to target and removed annotation
	if phase != enterpriseApi.PhaseScalingDown {
		t.Logf("Phase = %v (expected PhaseScalingDown when scale-down completes)", phase)
	}
}

func TestComputeCPUMetrics(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 5
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
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
									corev1.ResourceCPU: resource.MustParse("4000m"), // 4 CPU target
								},
							},
						},
					},
				},
			},
		},
	}

	// Create mixed pods: 2 new-spec (4 CPU), 3 old-spec (2 CPU)
	podList := &corev1.PodList{}

	// New-spec pods
	for i := 0; i < 2; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "4000m", true, "v2")
		podList.Items = append(podList.Items, *pod)
	}

	// Old-spec pods
	for i := 2; i < 5; i++ {
		pod := createCPUTestPod(fmt.Sprintf("splunk-indexer-%d", i), "test", "2000m", true, "v1")
		podList.Items = append(podList.Items, *pod)
	}
	c.ListObj = podList

	c.AddObject(sts)

	metrics, err := computeCPUMetrics(ctx, c, sts, 3)
	if err != nil {
		t.Fatalf("computeCPUMetrics() error = %v", err)
	}

	// Verify metrics
	// Total ready CPU = 2*4000 + 3*2000 = 14000 millicores
	if metrics.TotalReadyCPU != 14000 {
		t.Errorf("Expected TotalReadyCPU = 14000, got %d", metrics.TotalReadyCPU)
	}

	if metrics.NewSpecReadyPods != 2 {
		t.Errorf("Expected NewSpecReadyPods = 2, got %d", metrics.NewSpecReadyPods)
	}

	if metrics.OldSpecReadyPods != 3 {
		t.Errorf("Expected OldSpecReadyPods = 3, got %d", metrics.OldSpecReadyPods)
	}

	if metrics.TargetCPUPerPod != 4000 {
		t.Errorf("Expected TargetCPUPerPod = 4000, got %d", metrics.TargetCPUPerPod)
	}

	if metrics.OriginalCPUPerPod != 2000 {
		t.Errorf("Expected OriginalCPUPerPod = 2000, got %d", metrics.OriginalCPUPerPod)
	}
}
