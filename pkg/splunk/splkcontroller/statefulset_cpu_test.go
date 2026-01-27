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
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func parseQuantity(s string) resource.Quantity {
	q, _ := resource.ParseQuantity(s)
	return q
}

func TestIsKeepTotalCPUEnabled(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expected    bool
	}{
		{
			name:        "annotation enabled",
			annotations: map[string]string{PreserveTotalCPUAnnotation: "true"},
			expected:    true,
		},
		{
			name:        "annotation disabled",
			annotations: map[string]string{PreserveTotalCPUAnnotation: "false"},
			expected:    false,
		},
		{
			name:        "annotation missing",
			annotations: map[string]string{},
			expected:    false,
		},
		{
			name:        "nil annotations",
			annotations: nil,
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sts := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: tt.annotations,
				},
			}
			result := isPreserveTotalCPUEnabled(sts)
			if result != tt.expected {
				t.Errorf("isPreserveTotalCPUEnabled() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestGetCPURequest(t *testing.T) {
	tests := []struct {
		name     string
		podSpec  *corev1.PodSpec
		expected int64
	}{
		{
			name: "CPU request present",
			podSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: parseQuantity("2"),
							},
						},
					},
				},
			},
			expected: 2000, // 2 CPU = 2000 millicores
		},
		{
			name: "CPU request in millicores",
			podSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: parseQuantity("500m"),
							},
						},
					},
				},
			},
			expected: 500,
		},
		{
			name: "no CPU request",
			podSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{},
						},
					},
				},
			},
			expected: 0,
		},
		{
			name:     "nil podSpec",
			podSpec:  nil,
			expected: 0,
		},
		{
			name: "no containers",
			podSpec: &corev1.PodSpec{
				Containers: []corev1.Container{},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getCPURequest(tt.podSpec)
			if result != tt.expected {
				t.Errorf("getCPURequest() = %d, expected %d", result, tt.expected)
			}
		})
	}
}

func TestCalculateAdjustedReplicas(t *testing.T) {
	tests := []struct {
		name             string
		currentReplicas  int32
		currentCPUPerPod int64
		newCPUPerPod     int64
		expected         int32
	}{
		{
			name:             "double CPU per pod - halve replicas",
			currentReplicas:  10,
			currentCPUPerPod: 2000, // 2 CPU
			newCPUPerPod:     4000, // 4 CPU
			expected:         5,    // 10 * 2 / 4 = 5
		},
		{
			name:             "halve CPU per pod - double replicas",
			currentReplicas:  5,
			currentCPUPerPod: 4000, // 4 CPU
			newCPUPerPod:     2000, // 2 CPU
			expected:         10,   // 5 * 4 / 2 = 10
		},
		{
			name:             "no change in CPU",
			currentReplicas:  8,
			currentCPUPerPod: 3000,
			newCPUPerPod:     3000,
			expected:         8,
		},
		{
			name:             "round up to avoid under-provisioning",
			currentReplicas:  10,
			currentCPUPerPod: 5000, // 50 total CPU
			newCPUPerPod:     7000, // 50 / 7 = 7.14... rounds up to 8
			expected:         8,
		},
		{
			name:             "zero new CPU (safety check)",
			currentReplicas:  10,
			currentCPUPerPod: 2000,
			newCPUPerPod:     0,
			expected:         10, // Should return current replicas to avoid division by zero
		},
		{
			name:             "result would be zero - ensure at least 1",
			currentReplicas:  1,
			currentCPUPerPod: 500,  // 0.5 CPU
			newCPUPerPod:     5000, // 5 CPU
			expected:         1,    // Max(1, 500/5000) = 1
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateAdjustedReplicas(tt.currentReplicas, tt.currentCPUPerPod, tt.newCPUPerPod)
			if result != tt.expected {
				t.Errorf("calculateAdjustedReplicas(%d, %d, %d) = %d, expected %d",
					tt.currentReplicas, tt.currentCPUPerPod, tt.newCPUPerPod, result, tt.expected)
			}
		})
	}
}

func TestCPUAwareScalingInApplyStatefulSet(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Create current StatefulSet with 10 replicas, 2 CPU per pod
	var currentReplicas int32 = 10
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation: "true",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &currentReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: parseQuantity("2"),
								},
							},
						},
					},
				},
			},
		},
	}
	c.AddObject(current)

	// Create revised StatefulSet with 4 CPU per pod
	// Expected: replicas should be adjusted to 5 to maintain total 20 CPU
	var revisedReplicas int32 = 10 // Original desired replicas
	revised := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation: "true",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &revisedReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: parseQuantity("4"),
								},
							},
						},
					},
				},
			},
		},
	}

	// Apply the StatefulSet
	phase, err := ApplyStatefulSet(ctx, c, revised, nil)
	if err != nil {
		t.Errorf("ApplyStatefulSet() failed: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("Expected PhaseUpdating, got %v", phase)
	}

	// For scale-down (10 replicas with 2 CPU -> 5 replicas with 4 CPU),
	// the replicas should remain at 10 and target should be stored in annotation
	expectedReplicas := int32(10) // Current replicas preserved for gradual scale-down
	if *revised.Spec.Replicas != expectedReplicas {
		t.Errorf("Expected replicas to be %d, got %d", expectedReplicas, *revised.Spec.Replicas)
	}

	// Verify target annotation was set
	if revised.Annotations[CPUAwareTargetReplicasAnnotation] != "5" {
		t.Errorf("Expected target annotation to be '5', got '%s'", revised.Annotations[CPUAwareTargetReplicasAnnotation])
	}
}

func TestCPUAwareScalingDisabled(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Create current StatefulSet without CPU scaling annotation
	var currentReplicas int32 = 10
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			// No PreserveTotalCPUAnnotation
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &currentReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: parseQuantity("2"),
								},
							},
						},
					},
				},
			},
		},
	}
	c.AddObject(current)

	// Create revised StatefulSet with 4 CPU per pod but no annotation
	var revisedReplicas int32 = 10
	revised := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &revisedReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: parseQuantity("4"),
								},
							},
						},
					},
				},
			},
		},
	}

	// Apply the StatefulSet
	phase, err := ApplyStatefulSet(ctx, c, revised, nil)
	if err != nil {
		t.Errorf("ApplyStatefulSet() failed: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("Expected PhaseUpdating, got %v", phase)
	}

	// Verify replicas were NOT adjusted (annotation not set)
	if *revised.Spec.Replicas != revisedReplicas {
		t.Errorf("Expected replicas to remain %d, got %d", revisedReplicas, *revised.Spec.Replicas)
	}
}

func TestCPUAwareScalingScaleUp(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Create current StatefulSet with 5 replicas, 4 CPU per pod (total: 20 CPU)
	var currentReplicas int32 = 5
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation: "true",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &currentReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: parseQuantity("4"),
								},
							},
						},
					},
				},
			},
		},
	}
	c.AddObject(current)

	// Create revised StatefulSet with 2 CPU per pod
	// Expected: replicas should be adjusted to 10 to maintain total 20 CPU
	var revisedReplicas int32 = 5
	revised := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation: "true",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &revisedReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: parseQuantity("2"),
								},
							},
						},
					},
				},
			},
		},
	}

	// Apply the StatefulSet
	phase, err := ApplyStatefulSet(ctx, c, revised, nil)
	if err != nil {
		t.Errorf("ApplyStatefulSet() failed: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("Expected PhaseUpdating, got %v", phase)
	}

	// For scale-up (5 replicas with 4 CPU -> 10 replicas with 2 CPU),
	// the replicas should be increased immediately
	expectedReplicas := int32(10) // 5 * 4 / 2 = 10
	if *revised.Spec.Replicas != expectedReplicas {
		t.Errorf("Expected replicas to be adjusted to %d, got %d", expectedReplicas, *revised.Spec.Replicas)
	}

	// Verify no target annotation was set (immediate scale-up)
	if _, exists := revised.Annotations[CPUAwareTargetReplicasAnnotation]; exists {
		t.Errorf("Expected no target annotation for scale-up, but found one")
	}
}

func TestIsPodReady(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected bool
	}{
		{
			name: "pod ready",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			},
			expected: true,
		},
		{
			name: "pod not ready",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionFalse},
					},
				},
			},
			expected: false,
		},
		{
			name: "no ready condition",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPodReady(tt.pod)
			if result != tt.expected {
				t.Errorf("isPodReady() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestExtractCPUFromPod(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected int64
	}{
		{
			name: "pod with CPU request",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: parseQuantity("2"),
								},
							},
						},
					},
				},
			},
			expected: 2000,
		},
		{
			name: "pod without containers",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{},
				},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractCPUFromPod(tt.pod)
			if result != tt.expected {
				t.Errorf("extractCPUFromPod() = %d, expected %d", result, tt.expected)
			}
		})
	}
}

func TestHasNewSpec(t *testing.T) {
	tests := []struct {
		name      string
		pod       *corev1.Pod
		targetCPU int64
		expected  bool
	}{
		{
			name: "pod has new spec",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: parseQuantity("4"),
								},
							},
						},
					},
				},
			},
			targetCPU: 4000,
			expected:  true,
		},
		{
			name: "pod has old spec",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: parseQuantity("2"),
								},
							},
						},
					},
				},
			},
			targetCPU: 4000,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasNewSpec(tt.pod, tt.targetCPU)
			if result != tt.expected {
				t.Errorf("hasNewSpec() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
