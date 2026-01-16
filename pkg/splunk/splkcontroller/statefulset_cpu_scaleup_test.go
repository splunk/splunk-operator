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
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

// TestComputeCPUCeiling tests the computeCPUCeiling helper function
func TestComputeCPUCeiling(t *testing.T) {
	tests := []struct {
		name            string
		state           CPUAwareTransitionState
		parallelUpdates int32
		expectedCeiling int64
	}{
		{
			name: "basic scale-up 4x4CPU to 8x2CPU with parallelUpdates=1",
			state: CPUAwareTransitionState{
				OriginalReplicas:  4,
				TargetReplicas:    8,
				OriginalCPUMillis: 4000, // 4 CPU
				TargetCPUMillis:   2000, // 2 CPU
			},
			parallelUpdates: 1,
			// ceiling = 4*4000 + 1*2000 = 16000 + 2000 = 18000
			expectedCeiling: 18000,
		},
		{
			name: "scale-up 10x4CPU to 20x2CPU with parallelUpdates=3",
			state: CPUAwareTransitionState{
				OriginalReplicas:  10,
				TargetReplicas:    20,
				OriginalCPUMillis: 4000,
				TargetCPUMillis:   2000,
			},
			parallelUpdates: 3,
			// ceiling = 10*4000 + 3*2000 = 40000 + 6000 = 46000
			expectedCeiling: 46000,
		},
		{
			name: "scale-up with small CPU values",
			state: CPUAwareTransitionState{
				OriginalReplicas:  2,
				TargetReplicas:    4,
				OriginalCPUMillis: 500, // 500m
				TargetCPUMillis:   250, // 250m
			},
			parallelUpdates: 2,
			// ceiling = 2*500 + 2*250 = 1000 + 500 = 1500
			expectedCeiling: 1500,
		},
		{
			name: "parallelUpdates=0 should give minimal buffer",
			state: CPUAwareTransitionState{
				OriginalReplicas:  4,
				TargetReplicas:    8,
				OriginalCPUMillis: 2000,
				TargetCPUMillis:   1000,
			},
			parallelUpdates: 0,
			// ceiling = 4*2000 + 0*1000 = 8000
			expectedCeiling: 8000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := computeCPUCeiling(tt.state, tt.parallelUpdates)
			if result != tt.expectedCeiling {
				t.Errorf("computeCPUCeiling() = %d, expected %d", result, tt.expectedCeiling)
			}
		})
	}
}

// TestCPUAwareScaleUpDetection tests that scale-up transitions are detected and state is persisted
func TestCPUAwareScaleUpDetection(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Create current StatefulSet with 4 replicas, 4 CPU per pod (total: 16 CPU)
	var currentReplicas int32 = 4
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation: "up", // Enable scale-up only
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
	// Expected: target replicas should be 8 to maintain 16 CPU total
	var revisedReplicas int32 = 4
	revised := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation: "up",
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

	// For scale-up (4 replicas with 4 CPU -> 8 replicas with 2 CPU),
	// replicas should remain at 4 (gradual transition)
	expectedReplicas := int32(4)
	if *revised.Spec.Replicas != expectedReplicas {
		t.Errorf("Expected replicas to remain at %d for gradual scale-up, got %d", expectedReplicas, *revised.Spec.Replicas)
	}

	// Verify transition state annotation was set
	stateJSON, exists := revised.Annotations[CPUAwareTransitionStateAnnotation]
	if !exists {
		t.Error("Expected transition state annotation to be set for scale-up")
	} else {
		var state CPUAwareTransitionState
		if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
			t.Errorf("Failed to parse transition state JSON: %v", err)
		} else {
			if state.OriginalReplicas != 4 {
				t.Errorf("Expected original replicas to be 4, got %d", state.OriginalReplicas)
			}
			if state.TargetReplicas != 8 {
				t.Errorf("Expected target replicas to be 8, got %d", state.TargetReplicas)
			}
			if state.OriginalCPUMillis != 4000 {
				t.Errorf("Expected original CPU to be 4000, got %d", state.OriginalCPUMillis)
			}
			if state.TargetCPUMillis != 2000 {
				t.Errorf("Expected target CPU to be 2000, got %d", state.TargetCPUMillis)
			}
			if state.StartedAt == "" {
				t.Error("Expected StartedAt timestamp to be set")
			}
			if state.FinishedAt != "" {
				t.Error("Expected FinishedAt to be empty (transition in progress)")
			}
		}
	}
}

// TestCPUAwareScaleUpDirectionalControlUp tests that "up" only enables scale-up
func TestCPUAwareScaleUpDirectionalControlUp(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Create current StatefulSet with 4 replicas, 4 CPU per pod
	var currentReplicas int32 = 4
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation: "up", // Only enable scale-up
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

	// Create revised StatefulSet with 2 CPU per pod (should trigger scale-up)
	var revisedReplicas int32 = 4
	revised := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation: "up",
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

	phase, err := ApplyStatefulSet(ctx, c, revised, nil)
	if err != nil {
		t.Errorf("ApplyStatefulSet() failed: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("Expected PhaseUpdating, got %v", phase)
	}

	// Verify transition state annotation was set (scale-up should be enabled)
	if _, exists := revised.Annotations[CPUAwareTransitionStateAnnotation]; !exists {
		t.Error("Expected transition state annotation for scale-up with 'up' setting")
	}
}

// TestCPUAwareScaleUpDirectionalControlDown tests that "down" does NOT enable scale-up
func TestCPUAwareScaleUpDirectionalControlDown(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Create current StatefulSet with 4 replicas, 4 CPU per pod
	var currentReplicas int32 = 4
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation: "down", // Only enable scale-down
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

	// Create revised StatefulSet with 2 CPU per pod (would trigger scale-up if enabled)
	var revisedReplicas int32 = 4
	revised := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation: "down",
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

	phase, err := ApplyStatefulSet(ctx, c, revised, nil)
	if err != nil {
		t.Errorf("ApplyStatefulSet() failed: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("Expected PhaseUpdating, got %v", phase)
	}

	// Verify NO transition state annotation (scale-up should NOT be enabled with "down")
	if _, exists := revised.Annotations[CPUAwareTransitionStateAnnotation]; exists {
		t.Error("Expected NO transition state annotation for scale-up with 'down' setting")
	}
}

// TestCPUAwareScaleUpWithBoth tests that "both" enables scale-up
func TestCPUAwareScaleUpWithBoth(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Create current StatefulSet with 4 replicas, 4 CPU per pod
	var currentReplicas int32 = 4
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation: "both", // Enable both directions
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
	var revisedReplicas int32 = 4
	revised := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation: "both",
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

	phase, err := ApplyStatefulSet(ctx, c, revised, nil)
	if err != nil {
		t.Errorf("ApplyStatefulSet() failed: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("Expected PhaseUpdating, got %v", phase)
	}

	// Verify transition state annotation was set (scale-up enabled with "both")
	stateJSON, exists := revised.Annotations[CPUAwareTransitionStateAnnotation]
	if !exists {
		t.Error("Expected transition state annotation for scale-up with 'both' setting")
	} else {
		var state CPUAwareTransitionState
		if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
			t.Errorf("Failed to parse transition state: %v", err)
		} else if state.TargetReplicas != 8 {
			t.Errorf("Expected target replicas to be 8, got %d", state.TargetReplicas)
		}
	}
}

// TestCPUAwareScaleUpWithTrue tests that "true" enables scale-up (alias for both)
func TestCPUAwareScaleUpWithTrue(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Create current StatefulSet with 5 replicas, 4 CPU per pod
	var currentReplicas int32 = 5
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				PreserveTotalCPUAnnotation: "true", // Enable both directions
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
	// Target: 5 * 4 / 2 = 10 replicas
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

	phase, err := ApplyStatefulSet(ctx, c, revised, nil)
	if err != nil {
		t.Errorf("ApplyStatefulSet() failed: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("Expected PhaseUpdating, got %v", phase)
	}

	// Verify transition state annotation was set
	stateJSON, exists := revised.Annotations[CPUAwareTransitionStateAnnotation]
	if !exists {
		t.Error("Expected transition state annotation for scale-up with 'true' setting")
	} else {
		var state CPUAwareTransitionState
		if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
			t.Errorf("Failed to parse transition state: %v", err)
		} else if state.TargetReplicas != 10 {
			t.Errorf("Expected target replicas to be 10, got %d", state.TargetReplicas)
		}
	}
}

// TestIsCPUPreservingScalingFinishedForScaleUp tests that IsCPUPreservingScalingFinished works for scale-up
func TestIsCPUPreservingScalingFinishedForScaleUp(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expected    bool
	}{
		{
			name:        "no annotation",
			annotations: nil,
			expected:    false,
		},
		{
			name: "scale-up in progress (no FinishedAt)",
			annotations: map[string]string{
				CPUAwareTransitionStateAnnotation: `{"originalReplicas":4,"targetReplicas":8,"originalCPUMillis":4000,"targetCPUMillis":2000,"startedAt":"2026-01-12T00:00:00Z"}`,
			},
			expected: false,
		},
		{
			name: "scale-up complete (FinishedAt set)",
			annotations: map[string]string{
				CPUAwareTransitionStateAnnotation: `{"originalReplicas":4,"targetReplicas":8,"originalCPUMillis":4000,"targetCPUMillis":2000,"startedAt":"2026-01-12T00:00:00Z","finishedAt":"2026-01-12T00:10:00Z"}`,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sts := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: tt.annotations,
				},
			}
			result := IsCPUPreservingScalingFinished(sts)
			if result != tt.expected {
				t.Errorf("IsCPUPreservingScalingFinished() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// TestScaleUpCPUMetrics tests the ScaleUpCPUMetrics struct fields
func TestScaleUpCPUMetricsFields(t *testing.T) {
	metrics := ScaleUpCPUMetrics{
		TotalPodCPU:      16000,
		OldSpecPodCount:  4,
		NewSpecPodCount:  2,
		OldSpecReadyPods: 3,
	}

	if metrics.TotalPodCPU != 16000 {
		t.Errorf("Expected TotalPodCPU=16000, got %d", metrics.TotalPodCPU)
	}
	if metrics.OldSpecPodCount != 4 {
		t.Errorf("Expected OldSpecPodCount=4, got %d", metrics.OldSpecPodCount)
	}
	if metrics.NewSpecPodCount != 2 {
		t.Errorf("Expected NewSpecPodCount=2, got %d", metrics.NewSpecPodCount)
	}
	if metrics.OldSpecReadyPods != 3 {
		t.Errorf("Expected OldSpecReadyPods=3, got %d", metrics.OldSpecReadyPods)
	}
}
