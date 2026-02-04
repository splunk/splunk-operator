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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func TestGetParallelPodUpdates(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		replicas    int32
		expected    int32
	}{
		{
			name:        "annotation missing",
			annotations: nil,
			replicas:    10,
			expected:    1, // DefaultParallelPodUpdates
		},
		{
			name:        "annotation empty",
			annotations: map[string]string{ParallelPodUpdatesAnnotation: ""},
			replicas:    10,
			expected:    1,
		},
		{
			name:        "annotation set to 1 (absolute)",
			annotations: map[string]string{ParallelPodUpdatesAnnotation: "1"},
			replicas:    10,
			expected:    1,
		},
		{
			name:        "annotation set to 3 (absolute)",
			annotations: map[string]string{ParallelPodUpdatesAnnotation: "3"},
			replicas:    10,
			expected:    3,
		},
		{
			name:        "annotation set to 10 (absolute)",
			annotations: map[string]string{ParallelPodUpdatesAnnotation: "10"},
			replicas:    10,
			expected:    10,
		},
		{
			name:        "annotation exceeds replicas (absolute)",
			annotations: map[string]string{ParallelPodUpdatesAnnotation: "20"},
			replicas:    10,
			expected:    10, // Clamped to replica count
		},
		{
			name:        "annotation invalid - non-numeric",
			annotations: map[string]string{ParallelPodUpdatesAnnotation: "abc"},
			replicas:    10,
			expected:    1,
		},
		{
			name:        "annotation invalid - negative",
			annotations: map[string]string{ParallelPodUpdatesAnnotation: "-5"},
			replicas:    10,
			expected:    1,
		},
		{
			name:        "annotation invalid - zero",
			annotations: map[string]string{ParallelPodUpdatesAnnotation: "0"},
			replicas:    10,
			expected:    1,
		},
		// Floating-point percentage mode tests
		{
			name:        "percentage mode - 0.25 (25%)",
			annotations: map[string]string{ParallelPodUpdatesAnnotation: "0.25"},
			replicas:    10,
			expected:    3, // ceil(10 * 0.25) = ceil(2.5) = 3
		},
		{
			name:        "percentage mode - 0.5 (50%)",
			annotations: map[string]string{ParallelPodUpdatesAnnotation: "0.5"},
			replicas:    10,
			expected:    5, // ceil(10 * 0.5) = 5
		},
		{
			name:        "percentage mode - 0.1 (10%)",
			annotations: map[string]string{ParallelPodUpdatesAnnotation: "0.1"},
			replicas:    10,
			expected:    1, // ceil(10 * 0.1) = 1
		},
		{
			name:        "absolute mode - 1.0 treated as 1 pod",
			annotations: map[string]string{ParallelPodUpdatesAnnotation: "1.0"},
			replicas:    10,
			expected:    1, // 1.0 >= 1.0 so absolute mode, round(1.0) = 1
		},
		{
			name:        "percentage mode - small cluster",
			annotations: map[string]string{ParallelPodUpdatesAnnotation: "0.5"},
			replicas:    3,
			expected:    2, // ceil(3 * 0.5) = ceil(1.5) = 2
		},
		{
			name:        "percentage mode - very small percentage",
			annotations: map[string]string{ParallelPodUpdatesAnnotation: "0.01"},
			replicas:    100,
			expected:    1, // ceil(100 * 0.01) = 1
		},
		{
			name:        "absolute mode - 2.5 rounds to 3",
			annotations: map[string]string{ParallelPodUpdatesAnnotation: "2.5"},
			replicas:    10,
			expected:    3, // round(2.5) = 3
		},
		{
			name:        "absolute mode - 5.0",
			annotations: map[string]string{ParallelPodUpdatesAnnotation: "5.0"},
			replicas:    10,
			expected:    5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sts := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: tt.annotations,
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: &tt.replicas,
				},
			}
			result := getParallelPodUpdates(sts)
			if result != tt.expected {
				t.Errorf("getParallelPodUpdates() = %d, expected %d", result, tt.expected)
			}
		})
	}
}

func TestParallelPodUpdatesInCheckStatefulSetPodsForUpdates(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 5
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				ParallelPodUpdatesAnnotation: "3", // Update 3 pods in parallel
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
		Status: appsv1.StatefulSetStatus{
			UpdateRevision: "revision-2",
		},
	}

	// Create 5 pods with old revision that need updating
	for i := 0; i < 5; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", statefulSet.Name, i),
				Namespace: statefulSet.Namespace,
				Labels: map[string]string{
					"controller-revision-hash": "revision-1", // Old revision
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Ready: true},
				},
			},
		}
		c.AddObject(pod)
	}

	mgr := &DefaultStatefulSetPodManager{}

	// First reconcile: should delete 3 pods (parallel limit)
	phase, err := CheckStatefulSetPodsForUpdates(ctx, c, statefulSet, mgr, replicas)
	if err != nil {
		t.Errorf("CheckStatefulSetPodsForUpdates returned unexpected error: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("CheckStatefulSetPodsForUpdates() phase = %v, expected PhaseUpdating", phase)
	}

	// Verify exactly 3 pods were deleted
	deleteCalls := c.Calls["Delete"]
	if len(deleteCalls) != 3 {
		t.Errorf("Expected 3 pod deletions, got %d", len(deleteCalls))
	}
}

func TestParallelPodUpdatesSequentialMode(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 5
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			// No annotation - should default to 1
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
		Status: appsv1.StatefulSetStatus{
			UpdateRevision: "revision-2",
		},
	}

	// Create 5 pods with old revision
	for i := 0; i < 5; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", statefulSet.Name, i),
				Namespace: statefulSet.Namespace,
				Labels: map[string]string{
					"controller-revision-hash": "revision-1",
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Ready: true},
				},
			},
		}
		c.AddObject(pod)
	}

	mgr := &DefaultStatefulSetPodManager{}

	// First reconcile: should delete only 1 pod (default behavior)
	phase, err := CheckStatefulSetPodsForUpdates(ctx, c, statefulSet, mgr, replicas)
	if err != nil {
		t.Errorf("CheckStatefulSetPodsForUpdates returned unexpected error: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("CheckStatefulSetPodsForUpdates() phase = %v, expected PhaseUpdating", phase)
	}

	// Verify exactly 1 pod was deleted (sequential mode)
	deleteCalls := c.Calls["Delete"]
	if len(deleteCalls) != 1 {
		t.Errorf("Expected 1 pod deletion in sequential mode, got %d", len(deleteCalls))
	}
}

func TestParallelPodUpdatesPercentageMode(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 10
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				ParallelPodUpdatesAnnotation: "0.3", // 30% = 3 pods
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
		Status: appsv1.StatefulSetStatus{
			UpdateRevision: "revision-2",
		},
	}

	// Create 10 pods all needing updates
	for i := 0; i < 10; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", statefulSet.Name, i),
				Namespace: statefulSet.Namespace,
				Labels: map[string]string{
					"controller-revision-hash": "revision-1",
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Ready: true},
				},
			},
		}
		c.AddObject(pod)
	}

	mgr := &DefaultStatefulSetPodManager{}

	// First reconcile: should delete 3 pods (30% of 10)
	phase, err := CheckStatefulSetPodsForUpdates(ctx, c, statefulSet, mgr, replicas)
	if err != nil {
		t.Errorf("CheckStatefulSetPodsForUpdates returned unexpected error: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("CheckStatefulSetPodsForUpdates() phase = %v, expected PhaseUpdating", phase)
	}

	// Verify 3 pods were deleted
	deleteCalls := c.Calls["Delete"]
	if len(deleteCalls) != 3 {
		t.Errorf("Expected 3 pod deletions (30%% of 10), got %d", len(deleteCalls))
	}
}

func TestParallelPodUpdatesAllPodsNeedUpdate(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 10
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				ParallelPodUpdatesAnnotation: "5", // Update 5 pods in parallel
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
		Status: appsv1.StatefulSetStatus{
			UpdateRevision: "revision-2",
		},
	}

	// Create 10 pods all needing updates
	for i := 0; i < 10; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", statefulSet.Name, i),
				Namespace: statefulSet.Namespace,
				Labels: map[string]string{
					"controller-revision-hash": "revision-1",
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Ready: true},
				},
			},
		}
		c.AddObject(pod)
	}

	mgr := &DefaultStatefulSetPodManager{}

	// First reconcile: should delete 5 pods
	phase, err := CheckStatefulSetPodsForUpdates(ctx, c, statefulSet, mgr, replicas)
	if err != nil {
		t.Errorf("CheckStatefulSetPodsForUpdates returned unexpected error: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("CheckStatefulSetPodsForUpdates() phase = %v, expected PhaseUpdating", phase)
	}

	// Verify 5 pods were deleted in first cycle
	deleteCalls := c.Calls["Delete"]
	if len(deleteCalls) != 5 {
		t.Errorf("Expected 5 pod deletions in first cycle, got %d", len(deleteCalls))
	}
}

func TestParallelPodUpdatesNoPodsNeedUpdate(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 5
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				ParallelPodUpdatesAnnotation: "3",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
		Status: appsv1.StatefulSetStatus{
			UpdateRevision: "revision-2",
		},
	}

	// Create 5 pods all with correct revision (no updates needed)
	for i := 0; i < 5; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", statefulSet.Name, i),
				Namespace: statefulSet.Namespace,
				Labels: map[string]string{
					"controller-revision-hash": "revision-2", // Current revision
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Ready: true},
				},
			},
		}
		c.AddObject(pod)
	}

	mgr := &DefaultStatefulSetPodManager{}

	// Should return PhaseReady since all pods are up to date
	phase, err := CheckStatefulSetPodsForUpdates(ctx, c, statefulSet, mgr, replicas)
	if err != nil {
		t.Errorf("CheckStatefulSetPodsForUpdates returned unexpected error: %v", err)
	}
	if phase != enterpriseApi.PhaseReady {
		t.Errorf("CheckStatefulSetPodsForUpdates() phase = %v, expected PhaseReady", phase)
	}

	// Verify no pods were deleted
	deleteCalls := c.Calls["Delete"]
	if len(deleteCalls) != 0 {
		t.Errorf("Expected 0 pod deletions, got %d", len(deleteCalls))
	}
}

func TestParallelPodUpdatesPartialUpdates(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 5
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				ParallelPodUpdatesAnnotation: "3",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
		Status: appsv1.StatefulSetStatus{
			UpdateRevision: "revision-2",
		},
	}

	// Create 5 pods: 2 need updates, 3 are current
	for i := 0; i < 5; i++ {
		revision := "revision-2" // Current
		if i < 2 {
			revision = "revision-1" // Old - needs update
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", statefulSet.Name, i),
				Namespace: statefulSet.Namespace,
				Labels: map[string]string{
					"controller-revision-hash": revision,
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Ready: true},
				},
			},
		}
		c.AddObject(pod)
	}

	mgr := &DefaultStatefulSetPodManager{}

	// Should delete only the 2 pods that need updates (less than parallel limit of 3)
	phase, err := CheckStatefulSetPodsForUpdates(ctx, c, statefulSet, mgr, replicas)
	if err != nil {
		t.Errorf("CheckStatefulSetPodsForUpdates returned unexpected error: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("CheckStatefulSetPodsForUpdates() phase = %v, expected PhaseUpdating", phase)
	}

	// Verify exactly 2 pods were deleted (not hitting the limit of 3)
	deleteCalls := c.Calls["Delete"]
	if len(deleteCalls) != 2 {
		t.Errorf("Expected 2 pod deletions, got %d", len(deleteCalls))
	}
}

func TestParallelPodUpdatesAllAtOnce(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 5
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				ParallelPodUpdatesAnnotation: "5", // Absolute 5 pods - all pods
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
		Status: appsv1.StatefulSetStatus{
			UpdateRevision: "revision-2",
		},
	}

	// Create 5 pods all needing updates
	for i := 0; i < 5; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", statefulSet.Name, i),
				Namespace: statefulSet.Namespace,
				Labels: map[string]string{
					"controller-revision-hash": "revision-1",
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Ready: true},
				},
			},
		}
		c.AddObject(pod)
	}

	mgr := &DefaultStatefulSetPodManager{}

	// Should delete all 5 pods
	phase, err := CheckStatefulSetPodsForUpdates(ctx, c, statefulSet, mgr, replicas)
	if err != nil {
		t.Errorf("CheckStatefulSetPodsForUpdates returned unexpected error: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("CheckStatefulSetPodsForUpdates() phase = %v, expected PhaseUpdating", phase)
	}

	// Verify all 5 pods were deleted
	deleteCalls := c.Calls["Delete"]
	if len(deleteCalls) != 5 {
		t.Errorf("Expected 5 pod deletions, got %d", len(deleteCalls))
	}
}

func TestParallelPodUpdatesHighPercentage(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 10
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-indexer",
			Namespace: "test",
			Annotations: map[string]string{
				ParallelPodUpdatesAnnotation: "0.99", // 99% - nearly all pods
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
		Status: appsv1.StatefulSetStatus{
			UpdateRevision: "revision-2",
		},
	}

	// Create 10 pods all needing updates
	for i := 0; i < 10; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", statefulSet.Name, i),
				Namespace: statefulSet.Namespace,
				Labels: map[string]string{
					"controller-revision-hash": "revision-1",
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Ready: true},
				},
			},
		}
		c.AddObject(pod)
	}

	mgr := &DefaultStatefulSetPodManager{}

	// Should delete 10 pods (ceil(10 * 0.99) = ceil(9.9) = 10)
	phase, err := CheckStatefulSetPodsForUpdates(ctx, c, statefulSet, mgr, replicas)
	if err != nil {
		t.Errorf("CheckStatefulSetPodsForUpdates returned unexpected error: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("CheckStatefulSetPodsForUpdates() phase = %v, expected PhaseUpdating", phase)
	}

	// Verify all 10 pods were deleted
	deleteCalls := c.Calls["Delete"]
	if len(deleteCalls) != 10 {
		t.Errorf("Expected 10 pod deletions (99%%), got %d", len(deleteCalls))
	}
}
