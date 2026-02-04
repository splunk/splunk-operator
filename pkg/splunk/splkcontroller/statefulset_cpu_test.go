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
			name:        "annotation enabled with true",
			annotations: map[string]string{PreserveTotalCPUAnnotation: "true"},
			expected:    true,
		},
		{
			name:        "annotation enabled with both",
			annotations: map[string]string{PreserveTotalCPUAnnotation: "both"},
			expected:    true,
		},
		{
			name:        "annotation enabled with down",
			annotations: map[string]string{PreserveTotalCPUAnnotation: "down"},
			expected:    true,
		},
		{
			name:        "annotation enabled with up",
			annotations: map[string]string{PreserveTotalCPUAnnotation: "up"},
			expected:    true,
		},
		{
			name:        "annotation disabled with false",
			annotations: map[string]string{PreserveTotalCPUAnnotation: "false"},
			expected:    false,
		},
		{
			name:        "annotation disabled with invalid value",
			annotations: map[string]string{PreserveTotalCPUAnnotation: "invalid"},
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

func TestGetReplicaScalingDirection(t *testing.T) {
	tests := []struct {
		name        string
		originalCPU int64
		newCPU      int64
		expected    string
	}{
		{
			name:        "scale down (CPU per pod increases)",
			originalCPU: 2000,
			newCPU:      4000,
			expected:    PreserveTotalCPUDown,
		},
		{
			name:        "scale up (CPU per pod decreases)",
			originalCPU: 4000,
			newCPU:      2000,
			expected:    PreserveTotalCPUUp,
		},
		{
			name:        "no change",
			originalCPU: 2000,
			newCPU:      2000,
			expected:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getReplicaScalingDirection(tt.originalCPU, tt.newCPU)
			if result != tt.expected {
				t.Errorf("getReplicaScalingDirection() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestIsCPUScalingAllowed(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		direction   string
		expected    bool
	}{
		{
			name:        "true allows down",
			annotations: map[string]string{PreserveTotalCPUAnnotation: "true"},
			direction:   PreserveTotalCPUDown,
			expected:    true,
		},
		{
			name:        "true allows up",
			annotations: map[string]string{PreserveTotalCPUAnnotation: "true"},
			direction:   PreserveTotalCPUUp,
			expected:    true,
		},
		{
			name:        "both allows down",
			annotations: map[string]string{PreserveTotalCPUAnnotation: "both"},
			direction:   PreserveTotalCPUDown,
			expected:    true,
		},
		{
			name:        "both allows up",
			annotations: map[string]string{PreserveTotalCPUAnnotation: "both"},
			direction:   PreserveTotalCPUUp,
			expected:    true,
		},
		{
			name:        "down allows down",
			annotations: map[string]string{PreserveTotalCPUAnnotation: "down"},
			direction:   PreserveTotalCPUDown,
			expected:    true,
		},
		{
			name:        "down blocks up",
			annotations: map[string]string{PreserveTotalCPUAnnotation: "down"},
			direction:   PreserveTotalCPUUp,
			expected:    false,
		},
		{
			name:        "up allows up",
			annotations: map[string]string{PreserveTotalCPUAnnotation: "up"},
			direction:   PreserveTotalCPUUp,
			expected:    true,
		},
		{
			name:        "up blocks down",
			annotations: map[string]string{PreserveTotalCPUAnnotation: "up"},
			direction:   PreserveTotalCPUDown,
			expected:    false,
		},
		{
			name:        "invalid value blocks all",
			annotations: map[string]string{PreserveTotalCPUAnnotation: "invalid"},
			direction:   PreserveTotalCPUDown,
			expected:    false,
		},
		{
			name:        "missing annotation blocks all",
			annotations: map[string]string{},
			direction:   PreserveTotalCPUDown,
			expected:    false,
		},
		{
			name:        "nil annotations blocks all",
			annotations: nil,
			direction:   PreserveTotalCPUDown,
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
			result := isCPUScalingAllowed(sts, tt.direction)
			if result != tt.expected {
				t.Errorf("isCPUScalingAllowed() = %v, expected %v", result, tt.expected)
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

	// Verify transition state annotation was set
	stateJSON, exists := revised.Annotations[CPUAwareTransitionStateAnnotation]
	if !exists {
		t.Error("Expected transition state annotation to be set")
	} else {
		var state CPUAwareTransitionState
		if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
			t.Errorf("Failed to parse transition state JSON: %v", err)
		} else if state.TargetReplicas != 5 {
			t.Errorf("Expected target replicas to be 5, got %d", state.TargetReplicas)
		}
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
	// Expected: target replicas should be 10 to maintain total 20 CPU
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
	// Scale-up now uses gradual transition, returns PhaseUpdating
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("Expected PhaseUpdating, got %v", phase)
	}

	// For gradual scale-up (5 replicas with 4 CPU -> 10 replicas with 2 CPU),
	// replicas should remain at 5 (gradual transition will increase over time)
	expectedReplicas := int32(5) // replicas unchanged initially - gradual scale-up
	if *revised.Spec.Replicas != expectedReplicas {
		t.Errorf("Expected replicas to remain at %d for gradual scale-up, got %d", expectedReplicas, *revised.Spec.Replicas)
	}

	// Verify transition state annotation was set with correct target
	stateJSON, exists := revised.Annotations[CPUAwareTransitionStateAnnotation]
	if !exists {
		t.Error("Expected transition state annotation to be set for scale-up")
	} else {
		var state CPUAwareTransitionState
		if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
			t.Errorf("Failed to parse transition state JSON: %v", err)
		} else if state.TargetReplicas != 10 {
			t.Errorf("Expected target replicas to be 10, got %d", state.TargetReplicas)
		}
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

// TestCPUAwareScaleDownCompletion tests completion detection for scale-down through handleCPUPreservingTransition
func TestCPUAwareScaleDownCompletion(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Test case: replicas == targetReplicas AND all pods have new spec → PhaseReady + FinishedAt set
	t.Run("scale-down complete when all pods have new spec", func(t *testing.T) {
		// Setup StatefulSet at target replicas
		var replicas int32 = 5
		transitionState := CPUAwareTransitionState{
			OriginalReplicas:  10,
			TargetReplicas:    5,
			OriginalCPUMillis: 2000, // old CPU
			TargetCPUMillis:   4000, // new CPU (doubled)
			StartedAt:         "2026-01-12T00:00:00Z",
		}
		stateJSON, _ := json.Marshal(transitionState)

		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "splunk-test-scale-down-complete",
				Namespace: "test",
				Annotations: map[string]string{
					PreserveTotalCPUAnnotation:        "true",
					CPUAwareTransitionStateAnnotation: string(stateJSON),
				},
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: &replicas,
			},
			Status: appsv1.StatefulSetStatus{
				ReadyReplicas: replicas,
			},
		}
		c.AddObject(sts)

		// Create all 5 pods with new CPU spec (4000m)
		for i := int32(0); i < 5; i++ {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "splunk-test-scale-down-complete-" + string(rune('0'+i)),
					Namespace: "test",
				},
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
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			}
			c.AddObject(pod)
		}

		mgr := &MockStatefulSetPodManager{}
		phase, handled, err := handleCPUPreservingTransition(ctx, c, sts, mgr, replicas)

		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if !handled {
			t.Error("Expected handled=true for scale-down completion")
		}
		if phase != enterpriseApi.PhaseReady {
			t.Errorf("Expected PhaseReady, got %v", phase)
		}
	})

	// Test case: replicas == targetReplicas but some pods have old spec → PhaseUpdating
	t.Run("scale-down not complete when pods have old spec", func(t *testing.T) {
		c2 := spltest.NewMockClient()
		var replicas int32 = 5
		transitionState := CPUAwareTransitionState{
			OriginalReplicas:  10,
			TargetReplicas:    5,
			OriginalCPUMillis: 2000,
			TargetCPUMillis:   4000,
			StartedAt:         "2026-01-12T00:00:00Z",
		}
		stateJSON, _ := json.Marshal(transitionState)

		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "splunk-test-not-complete",
				Namespace: "test",
				Annotations: map[string]string{
					PreserveTotalCPUAnnotation:        "true",
					CPUAwareTransitionStateAnnotation: string(stateJSON),
				},
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: &replicas,
			},
			Status: appsv1.StatefulSetStatus{
				ReadyReplicas: replicas,
			},
		}
		c2.AddObject(sts)

		// Create 5 pods - some with old spec (2000m), some with new spec (4000m)
		for i := int32(0); i < 5; i++ {
			cpuValue := "4" // new spec
			if i < 2 {
				cpuValue = "2" // old spec for first 2 pods
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "splunk-test-not-complete-" + string(rune('0'+i)),
					Namespace: "test",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: parseQuantity(cpuValue),
								},
							},
						},
					},
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			}
			c2.AddObject(pod)
		}

		mgr := &MockStatefulSetPodManager{}
		phase, handled, err := handleCPUPreservingTransition(ctx, c2, sts, mgr, replicas)

		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if !handled {
			t.Error("Expected handled=true")
		}
		// Should be PhaseUpdating since recycling is needed
		if phase != enterpriseApi.PhaseUpdating {
			t.Errorf("Expected PhaseUpdating (recycling needed), got %v", phase)
		}
	})
}

// TestCPUAwareScaleDownBalance tests the balance step during scale-down
func TestCPUAwareScaleDownBalance(t *testing.T) {
	ctx := context.TODO()

	// Test: surplusCPU >= oldCPUPerPod → reduce replicas
	t.Run("balance reduces replicas when surplus exists", func(t *testing.T) {
		c := spltest.NewMockClient()
		var replicas int32 = 10
		transitionState := CPUAwareTransitionState{
			OriginalReplicas:  10,
			TargetReplicas:    5,
			OriginalCPUMillis: 2000, // 2 CPU per pod (original: 10*2 = 20 total)
			TargetCPUMillis:   4000, // 4 CPU per pod (target: 5*4 = 20 total)
			StartedAt:         "2026-01-12T00:00:00Z",
		}
		stateJSON, _ := json.Marshal(transitionState)

		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "splunk-test-balance",
				Namespace: "test",
				Annotations: map[string]string{
					PreserveTotalCPUAnnotation:        "true",
					CPUAwareTransitionStateAnnotation: string(stateJSON),
				},
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: &replicas,
			},
			Status: appsv1.StatefulSetStatus{
				ReadyReplicas: replicas,
			},
		}
		c.AddObject(sts)

		// Create pods: 2 new-spec pods (4000m) and 8 old-spec pods (2000m)
		// New-spec pods: 2*4000 = 8000m
		// Old-spec pods: 8*2000 = 16000m
		// Total ready CPU: 24000m
		// Surplus = newSpecCPU - (newSpecPods * originalCPUPerPod) = 8000 - (2*2000) = 4000m
		// This surplus >= 2000 (oldCPUPerPod), so balance should kick in
		for i := int32(0); i < 10; i++ {
			cpuValue := "2" // old spec
			if i < 2 {
				cpuValue = "4" // new spec for first 2 pods
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "splunk-test-balance-" + string(rune('0'+i)),
					Namespace: "test",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: parseQuantity(cpuValue),
								},
							},
						},
					},
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			}
			c.AddObject(pod)
		}

		mgr := &MockStatefulSetPodManager{}
		phase, handled, err := handleCPUPreservingTransition(ctx, c, sts, mgr, replicas)

		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if !handled {
			t.Error("Expected handled=true")
		}
		if phase != enterpriseApi.PhaseScalingDown {
			t.Errorf("Expected PhaseScalingDown (balance), got %v", phase)
		}
	})
}

// TestCPUAwareScaleDownRecycle tests pod recycling during scale-down
func TestCPUAwareScaleDownRecycle(t *testing.T) {
	ctx := context.TODO()

	// Test: recycle old-spec pods when no balance possible
	t.Run("recycle old-spec ready pods", func(t *testing.T) {
		c := spltest.NewMockClient()
		var replicas int32 = 5
		transitionState := CPUAwareTransitionState{
			OriginalReplicas:  10,
			TargetReplicas:    5,
			OriginalCPUMillis: 2000,
			TargetCPUMillis:   4000,
			StartedAt:         "2026-01-12T00:00:00Z",
		}
		stateJSON, _ := json.Marshal(transitionState)

		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "splunk-test-recycle",
				Namespace: "test",
				Annotations: map[string]string{
					PreserveTotalCPUAnnotation:        "true",
					CPUAwareTransitionStateAnnotation: string(stateJSON),
					ParallelPodUpdatesAnnotation:      "1",
				},
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: &replicas,
			},
			Status: appsv1.StatefulSetStatus{
				ReadyReplicas: replicas,
			},
		}
		c.AddObject(sts)

		// Create 5 pods all with old spec - should trigger recycling
		for i := int32(0); i < 5; i++ {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("splunk-test-recycle-%d", i),
					Namespace: "test",
					UID:       types.UID(fmt.Sprintf("test-uid-%d", i)),
				},
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
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			}
			c.AddObject(pod)
		}

		mgr := &MockStatefulSetPodManager{
			PrepareRecycleReady: true,
		}
		phase, handled, err := handleCPUPreservingTransition(ctx, c, sts, mgr, replicas)

		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if !handled {
			t.Error("Expected handled=true")
		}
		if phase != enterpriseApi.PhaseUpdating {
			t.Errorf("Expected PhaseUpdating (recycling), got %v", phase)
		}
	})

	// Test: skip non-ready pods
	t.Run("skip non-ready pods during recycle", func(t *testing.T) {
		c := spltest.NewMockClient()
		var replicas int32 = 5
		transitionState := CPUAwareTransitionState{
			OriginalReplicas:  10,
			TargetReplicas:    5,
			OriginalCPUMillis: 2000,
			TargetCPUMillis:   4000,
			StartedAt:         "2026-01-12T00:00:00Z",
		}
		stateJSON, _ := json.Marshal(transitionState)

		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "splunk-test-skip-not-ready",
				Namespace: "test",
				Annotations: map[string]string{
					PreserveTotalCPUAnnotation:        "true",
					CPUAwareTransitionStateAnnotation: string(stateJSON),
					ParallelPodUpdatesAnnotation:      "1",
				},
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: &replicas,
			},
			Status: appsv1.StatefulSetStatus{
				ReadyReplicas: 3, // only 3 ready
			},
		}
		c.AddObject(sts)

		// Create pods - only some are ready
		for i := int32(0); i < 5; i++ {
			readyStatus := corev1.ConditionTrue
			if i >= 3 {
				readyStatus = corev1.ConditionFalse // not ready
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("splunk-test-skip-not-ready-%d", i),
					Namespace: "test",
					UID:       types.UID(fmt.Sprintf("test-uid-%d", i)),
				},
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
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: readyStatus},
					},
				},
			}
			c.AddObject(pod)
		}

		mgr := &MockStatefulSetPodManager{
			PrepareRecycleReady: true,
		}
		phase, handled, err := handleCPUPreservingTransition(ctx, c, sts, mgr, replicas)

		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if !handled {
			t.Error("Expected handled=true")
		}
		// Should still be updating (recycling the ready ones)
		if phase != enterpriseApi.PhaseUpdating {
			t.Errorf("Expected PhaseUpdating, got %v", phase)
		}
	})
}

// TestCPUAwareScaleDownParallelUpdates tests parallel update enforcement during scale-down
func TestCPUAwareScaleDownParallelUpdates(t *testing.T) {
	ctx := context.TODO()

	// Test: parallelUpdates=3 recycles up to 3 pods per cycle
	t.Run("parallel updates limit enforced", func(t *testing.T) {
		c := spltest.NewMockClient()
		var replicas int32 = 5
		transitionState := CPUAwareTransitionState{
			OriginalReplicas:  10,
			TargetReplicas:    5,
			OriginalCPUMillis: 2000,
			TargetCPUMillis:   4000,
			StartedAt:         "2026-01-12T00:00:00Z",
		}
		stateJSON, _ := json.Marshal(transitionState)

		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "splunk-test-parallel",
				Namespace: "test",
				Annotations: map[string]string{
					PreserveTotalCPUAnnotation:        "true",
					CPUAwareTransitionStateAnnotation: string(stateJSON),
					ParallelPodUpdatesAnnotation:      "3", // Allow 3 parallel updates
				},
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: &replicas,
			},
			Status: appsv1.StatefulSetStatus{
				ReadyReplicas: replicas,
			},
		}
		c.AddObject(sts)

		// Create 5 pods all with old spec
		for i := int32(0); i < 5; i++ {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("splunk-test-parallel-%d", i),
					Namespace: "test",
					UID:       types.UID(fmt.Sprintf("test-uid-%d", i)),
				},
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
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			}
			c.AddObject(pod)
		}

		// Track how many PrepareRecycle calls are made
		mgr := &MockStatefulSetPodManager{
			PrepareRecycleReady: true,
		}
		phase, handled, err := handleCPUPreservingTransition(ctx, c, sts, mgr, replicas)

		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if !handled {
			t.Error("Expected handled=true")
		}
		if phase != enterpriseApi.PhaseUpdating {
			t.Errorf("Expected PhaseUpdating, got %v", phase)
		}
	})
}

// MockStatefulSetPodManager is a mock implementation for testing
type MockStatefulSetPodManager struct {
	PrepareRecycleReady bool
	PrepareRecycleError error
}

func (m *MockStatefulSetPodManager) Update(ctx context.Context, c splcommon.ControllerClient, sts *appsv1.StatefulSet, desiredReplicas int32) (enterpriseApi.Phase, error) {
	return enterpriseApi.PhaseReady, nil
}

func (m *MockStatefulSetPodManager) PrepareScaleDown(ctx context.Context, n int32) (bool, error) {
	return true, nil
}

func (m *MockStatefulSetPodManager) PrepareRecycle(ctx context.Context, n int32) (bool, error) {
	return m.PrepareRecycleReady, m.PrepareRecycleError
}

func (m *MockStatefulSetPodManager) FinishRecycle(ctx context.Context, n int32) (bool, error) {
	return true, nil
}

func (m *MockStatefulSetPodManager) FinishUpgrade(ctx context.Context, n int32) error {
	return nil
}
