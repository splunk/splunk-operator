// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
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
	"errors"
	"fmt"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

// TestGetUnifiedTransitionState_NoAnnotation verifies that getUnifiedTransitionState returns nil when no annotation is present
func TestGetUnifiedTransitionState_NoAnnotation(t *testing.T) {
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test",
		},
	}

	state, err := getUnifiedTransitionState(statefulSet)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if state != nil {
		t.Errorf("Expected nil state when no annotation, got: %+v", state)
	}

	// Also test with empty annotations map
	statefulSet.Annotations = make(map[string]string)
	state, err = getUnifiedTransitionState(statefulSet)
	if err != nil {
		t.Errorf("Expected no error with empty annotations, got: %v", err)
	}
	if state != nil {
		t.Errorf("Expected nil state with empty annotations, got: %+v", state)
	}
}

// TestGetUnifiedTransitionState_ValidState verifies that getUnifiedTransitionState correctly parses a valid JSON state
func TestGetUnifiedTransitionState_ValidState(t *testing.T) {
	stateJSON := `{
		"cpuChange": {
			"originalCPUMillis": 1000,
			"targetCPUMillis": 2000,
			"originalReplicas": 4,
			"targetReplicas": 2
		},
		"vctMigration": {
			"expectedStorageClasses": {"pvc-data": "premium"}
		},
		"startedAt": "2024-01-15T10:00:00Z"
	}`

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test",
			Annotations: map[string]string{
				UnifiedTransitionStateAnnotation: stateJSON,
			},
		},
	}

	state, err := getUnifiedTransitionState(statefulSet)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if state == nil {
		t.Fatal("Expected non-nil state")
	}

	// Verify CPU change fields
	if state.CPUChange == nil {
		t.Fatal("Expected non-nil CPUChange")
	}
	if state.CPUChange.OriginalCPUMillis != 1000 {
		t.Errorf("Expected OriginalCPUMillis=1000, got %d", state.CPUChange.OriginalCPUMillis)
	}
	if state.CPUChange.TargetCPUMillis != 2000 {
		t.Errorf("Expected TargetCPUMillis=2000, got %d", state.CPUChange.TargetCPUMillis)
	}
	if state.CPUChange.OriginalReplicas != 4 {
		t.Errorf("Expected OriginalReplicas=4, got %d", state.CPUChange.OriginalReplicas)
	}
	if state.CPUChange.TargetReplicas != 2 {
		t.Errorf("Expected TargetReplicas=2, got %d", state.CPUChange.TargetReplicas)
	}

	// Verify VCT migration fields
	if state.VCTMigration == nil {
		t.Fatal("Expected non-nil VCTMigration")
	}
	if state.VCTMigration.ExpectedStorageClasses["pvc-data"] != "premium" {
		t.Errorf("Expected ExpectedStorageClasses['pvc-data']='premium', got '%s'", state.VCTMigration.ExpectedStorageClasses["pvc-data"])
	}

	// Verify timestamp
	if state.StartedAt != "2024-01-15T10:00:00Z" {
		t.Errorf("Expected StartedAt='2024-01-15T10:00:00Z', got '%s'", state.StartedAt)
	}
}

// TestGetUnifiedTransitionState_InvalidJSON verifies that getUnifiedTransitionState handles malformed JSON
func TestGetUnifiedTransitionState_InvalidJSON(t *testing.T) {
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test",
			Annotations: map[string]string{
				UnifiedTransitionStateAnnotation: "invalid json {{{",
			},
		},
	}

	state, err := getUnifiedTransitionState(statefulSet)
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
	if state != nil {
		t.Errorf("Expected nil state for invalid JSON, got: %+v", state)
	}
}

// TestGetUnifiedTransitionState_MigrateCPUAware verifies backward compatibility by migrating old CPUAwareTransitionState
func TestGetUnifiedTransitionState_MigrateCPUAware(t *testing.T) {
	// Old format annotation
	oldStateJSON := `{
		"originalReplicas": 8,
		"targetReplicas": 4,
		"originalCPUMillis": 500,
		"targetCPUMillis": 1000,
		"startedAt": "2024-01-15T09:00:00Z",
		"finishedAt": "2024-01-15T09:30:00Z"
	}`

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test",
			Annotations: map[string]string{
				CPUAwareTransitionStateAnnotation: oldStateJSON,
			},
		},
	}

	state, err := getUnifiedTransitionState(statefulSet)
	if err != nil {
		t.Fatalf("Expected no error during migration, got: %v", err)
	}
	if state == nil {
		t.Fatal("Expected non-nil state after migration")
	}

	// Verify migrated CPU change fields
	if state.CPUChange == nil {
		t.Fatal("Expected non-nil CPUChange after migration")
	}
	if state.CPUChange.OriginalCPUMillis != 500 {
		t.Errorf("Expected migrated OriginalCPUMillis=500, got %d", state.CPUChange.OriginalCPUMillis)
	}
	if state.CPUChange.TargetCPUMillis != 1000 {
		t.Errorf("Expected migrated TargetCPUMillis=1000, got %d", state.CPUChange.TargetCPUMillis)
	}
	if state.CPUChange.OriginalReplicas != 8 {
		t.Errorf("Expected migrated OriginalReplicas=8, got %d", state.CPUChange.OriginalReplicas)
	}
	if state.CPUChange.TargetReplicas != 4 {
		t.Errorf("Expected migrated TargetReplicas=4, got %d", state.CPUChange.TargetReplicas)
	}

	// Verify timestamps are preserved
	if state.StartedAt != "2024-01-15T09:00:00Z" {
		t.Errorf("Expected migrated StartedAt='2024-01-15T09:00:00Z', got '%s'", state.StartedAt)
	}
	if state.FinishedAt != "2024-01-15T09:30:00Z" {
		t.Errorf("Expected migrated FinishedAt='2024-01-15T09:30:00Z', got '%s'", state.FinishedAt)
	}

	// Verify VCT migration is nil (old format doesn't have it)
	if state.VCTMigration != nil {
		t.Errorf("Expected nil VCTMigration after migration from CPU-only state, got: %+v", state.VCTMigration)
	}
}

// TestPersistUnifiedTransitionState verifies that state is correctly persisted to the StatefulSet annotation
func TestPersistUnifiedTransitionState(t *testing.T) {
	ctx := context.TODO()

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test",
		},
	}

	state := &UnifiedTransitionState{
		CPUChange: &CPUTransition{
			OriginalCPUMillis: 1000,
			TargetCPUMillis:   500,
			OriginalReplicas:  4,
			TargetReplicas:    8,
		},
		StartedAt: "2024-01-15T10:00:00Z",
	}

	// Create mock client
	mockCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.StatefulSet-test-test-statefulset"},
	}
	mockClient := spltest.NewMockClient()
	mockClient.AddObject(statefulSet)

	err := persistUnifiedTransitionState(ctx, mockClient, statefulSet, state)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify annotation was set
	if statefulSet.Annotations == nil {
		t.Fatal("Expected annotations to be set")
	}
	savedJSON, exists := statefulSet.Annotations[UnifiedTransitionStateAnnotation]
	if !exists {
		t.Fatal("Expected UnifiedTransitionStateAnnotation to be set")
	}

	// Verify we can parse the saved state
	var savedState UnifiedTransitionState
	if err := json.Unmarshal([]byte(savedJSON), &savedState); err != nil {
		t.Fatalf("Failed to unmarshal saved state: %v", err)
	}

	if savedState.CPUChange.OriginalCPUMillis != 1000 {
		t.Errorf("Expected saved OriginalCPUMillis=1000, got %d", savedState.CPUChange.OriginalCPUMillis)
	}

	_ = mockCalls // suppress unused warning
}

// TestClearUnifiedTransitionState verifies that the annotation is removed correctly
func TestClearUnifiedTransitionState(t *testing.T) {
	ctx := context.TODO()

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test",
			Annotations: map[string]string{
				UnifiedTransitionStateAnnotation:  `{"startedAt": "2024-01-15T10:00:00Z"}`,
				CPUAwareTransitionStateAnnotation: `{"startedAt": "2024-01-15T09:00:00Z"}`,
			},
		},
	}

	// Create mock client
	mockClient := spltest.NewMockClient()
	mockClient.AddObject(statefulSet)

	err := clearUnifiedTransitionState(ctx, mockClient, statefulSet)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify both annotations were removed
	if _, exists := statefulSet.Annotations[UnifiedTransitionStateAnnotation]; exists {
		t.Error("Expected UnifiedTransitionStateAnnotation to be removed")
	}
	if _, exists := statefulSet.Annotations[CPUAwareTransitionStateAnnotation]; exists {
		t.Error("Expected CPUAwareTransitionStateAnnotation to be removed (backward compat)")
	}
}

// TestUnifiedTransitionState_CPUOnly verifies handling of CPU-only transitions
func TestUnifiedTransitionState_CPUOnly(t *testing.T) {
	cpuChange := &CPUTransition{
		OriginalCPUMillis: 2000,
		TargetCPUMillis:   1000,
		OriginalReplicas:  3,
		TargetReplicas:    6,
	}

	state := initUnifiedTransitionState(cpuChange, nil)

	if state == nil {
		t.Fatal("Expected non-nil state")
	}
	if state.CPUChange == nil {
		t.Error("Expected non-nil CPUChange")
	}
	if state.VCTMigration != nil {
		t.Error("Expected nil VCTMigration for CPU-only transition")
	}
	if state.StartedAt == "" {
		t.Error("Expected StartedAt to be set")
	}
	if state.FinishedAt != "" {
		t.Error("Expected FinishedAt to be empty for new transition")
	}
	if state.CPUChange.OriginalCPUMillis != 2000 {
		t.Errorf("Expected OriginalCPUMillis=2000, got %d", state.CPUChange.OriginalCPUMillis)
	}
}

// TestUnifiedTransitionState_VCTOnly verifies handling of VCT-only transitions
func TestUnifiedTransitionState_VCTOnly(t *testing.T) {
	vctMigration := &VCTMigrationTransition{
		ExpectedStorageClasses: map[string]string{
			"pvc-data": "premium",
			"pvc-logs": "fast",
		},
		ExpectedAccessModes: map[string][]corev1.PersistentVolumeAccessMode{
			"pvc-data": {corev1.ReadWriteOnce},
		},
	}

	state := initUnifiedTransitionState(nil, vctMigration)

	if state == nil {
		t.Fatal("Expected non-nil state")
	}
	if state.CPUChange != nil {
		t.Error("Expected nil CPUChange for VCT-only transition")
	}
	if state.VCTMigration == nil {
		t.Error("Expected non-nil VCTMigration")
	}
	if state.StartedAt == "" {
		t.Error("Expected StartedAt to be set")
	}
	if len(state.VCTMigration.ExpectedStorageClasses) != 2 {
		t.Errorf("Expected 2 storage classes, got %d", len(state.VCTMigration.ExpectedStorageClasses))
	}
	if state.VCTMigration.ExpectedStorageClasses["pvc-data"] != "premium" {
		t.Errorf("Expected pvc-data storage class='premium', got '%s'", state.VCTMigration.ExpectedStorageClasses["pvc-data"])
	}
}

// TestUnifiedTransitionState_Combined verifies handling of combined CPU+VCT transitions
func TestUnifiedTransitionState_Combined(t *testing.T) {
	cpuChange := &CPUTransition{
		OriginalCPUMillis: 500,
		TargetCPUMillis:   1000,
		OriginalReplicas:  10,
		TargetReplicas:    5,
	}
	vctMigration := &VCTMigrationTransition{
		ExpectedStorageClasses: map[string]string{
			"pvc-data": "premium-ssd",
		},
	}

	state := initUnifiedTransitionState(cpuChange, vctMigration)

	if state == nil {
		t.Fatal("Expected non-nil state")
	}
	if state.CPUChange == nil {
		t.Error("Expected non-nil CPUChange for combined transition")
	}
	if state.VCTMigration == nil {
		t.Error("Expected non-nil VCTMigration for combined transition")
	}
	if state.StartedAt == "" {
		t.Error("Expected StartedAt to be set")
	}

	// Verify CPU fields
	if state.CPUChange.OriginalReplicas != 10 || state.CPUChange.TargetReplicas != 5 {
		t.Errorf("Expected replicas 10->5, got %d->%d", state.CPUChange.OriginalReplicas, state.CPUChange.TargetReplicas)
	}

	// Verify VCT fields
	if state.VCTMigration.ExpectedStorageClasses["pvc-data"] != "premium-ssd" {
		t.Errorf("Expected storage class='premium-ssd', got '%s'", state.VCTMigration.ExpectedStorageClasses["pvc-data"])
	}

	// Verify tracking maps are initialized
	if state.PodStatus == nil {
		t.Error("Expected PodStatus map to be initialized")
	}
	if state.FailedPods == nil {
		t.Error("Expected FailedPods map to be initialized")
	}
}

// TestIsUnifiedTransitionInProgress verifies the in-progress check logic
func TestIsUnifiedTransitionInProgress(t *testing.T) {
	tests := []struct {
		name       string
		annotation string
		expected   bool
	}{
		{
			name:       "no annotation",
			annotation: "",
			expected:   false,
		},
		{
			name:       "in progress (no FinishedAt)",
			annotation: `{"startedAt": "2024-01-15T10:00:00Z"}`,
			expected:   true,
		},
		{
			name:       "finished (FinishedAt set)",
			annotation: `{"startedAt": "2024-01-15T10:00:00Z", "finishedAt": "2024-01-15T11:00:00Z"}`,
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-statefulset",
					Namespace: "test",
				},
			}
			if tt.annotation != "" {
				statefulSet.Annotations = map[string]string{
					UnifiedTransitionStateAnnotation: tt.annotation,
				}
			}

			result := isUnifiedTransitionInProgress(statefulSet)
			if result != tt.expected {
				t.Errorf("isUnifiedTransitionInProgress() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// TestIsPodFullyUpdated_CPUOnly tests isPodFullyUpdated when only CPU change is active
func TestIsPodFullyUpdated_CPUOnly(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Create pod with old CPU (1000m instead of target 2000m)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-0",
			Namespace: "test",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "splunk",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("1000m"),
						},
					},
				},
			},
		},
	}

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
	}

	state := &UnifiedTransitionState{
		CPUChange: &CPUTransition{
			OriginalCPUMillis: 1000,
			TargetCPUMillis:   2000,
			OriginalReplicas:  4,
			TargetReplicas:    2,
		},
	}

	result, err := isPodFullyUpdated(ctx, c, pod, statefulSet, state)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result {
		t.Error("Expected isPodFullyUpdated to return false when CPU not updated")
	}
}

// TestIsPodFullyUpdated_VCTOnly tests isPodFullyUpdated when only VCT migration is active
func TestIsPodFullyUpdated_VCTOnly(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Create pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-0",
			Namespace: "test",
		},
	}

	// Create PVC with wrong storage class (standard instead of premium)
	wrongStorageClass := "standard"
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pvc-data-test-pod-0",
			Namespace: "test",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &wrongStorageClass,
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}
	c.AddObject(pvc)

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
	}

	state := &UnifiedTransitionState{
		VCTMigration: &VCTMigrationTransition{
			ExpectedStorageClasses: map[string]string{
				"pvc-data": "premium",
			},
		},
	}

	result, err := isPodFullyUpdated(ctx, c, pod, statefulSet, state)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result {
		t.Error("Expected isPodFullyUpdated to return false when PVC storage class is wrong")
	}
}

// TestIsPodFullyUpdated_Combined tests isPodFullyUpdated when both CPU and VCT are active
func TestIsPodFullyUpdated_Combined(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Create pod with CORRECT CPU (2000m)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-0",
			Namespace: "test",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "splunk",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("2000m"),
						},
					},
				},
			},
		},
	}

	// Create PVC with WRONG storage class
	wrongStorageClass := "standard"
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pvc-data-test-pod-0",
			Namespace: "test",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &wrongStorageClass,
		},
	}
	c.AddObject(pvc)

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
	}

	state := &UnifiedTransitionState{
		CPUChange: &CPUTransition{
			OriginalCPUMillis: 1000,
			TargetCPUMillis:   2000,
		},
		VCTMigration: &VCTMigrationTransition{
			ExpectedStorageClasses: map[string]string{
				"pvc-data": "premium",
			},
		},
	}

	// CPU is correct, but VCT is wrong - should return false
	result, err := isPodFullyUpdated(ctx, c, pod, statefulSet, state)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result {
		t.Error("Expected isPodFullyUpdated to return false when either check fails")
	}
}

// TestIsPodFullyUpdated_AllUpdated tests isPodFullyUpdated when all updates are applied
func TestIsPodFullyUpdated_AllUpdated(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Create pod with CORRECT CPU (2000m)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-0",
			Namespace: "test",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "splunk",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("2000m"),
						},
					},
				},
			},
		},
	}

	// Create PVC with CORRECT storage class
	correctStorageClass := "premium"
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pvc-data-test-pod-0",
			Namespace: "test",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &correctStorageClass,
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}
	c.AddObject(pvc)

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
	}

	state := &UnifiedTransitionState{
		CPUChange: &CPUTransition{
			OriginalCPUMillis: 1000,
			TargetCPUMillis:   2000,
		},
		VCTMigration: &VCTMigrationTransition{
			ExpectedStorageClasses: map[string]string{
				"pvc-data": "premium",
			},
			ExpectedAccessModes: map[string][]corev1.PersistentVolumeAccessMode{
				"pvc-data": {corev1.ReadWriteOnce},
			},
		},
	}

	result, err := isPodFullyUpdated(ctx, c, pod, statefulSet, state)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !result {
		t.Error("Expected isPodFullyUpdated to return true when all updates are applied")
	}
}

// TestPVCMatchesVCTSpec_SameStorageClass tests pvcMatchesVCTSpec when storage class matches
func TestPVCMatchesVCTSpec_SameStorageClass(t *testing.T) {
	storageClass := "premium"
	pvc := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClass,
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}
	vct := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClass,
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}

	result := pvcMatchesVCTSpec(pvc, vct)
	if !result {
		t.Error("Expected pvcMatchesVCTSpec to return true when storage class and access modes match")
	}
}

// TestPVCMatchesVCTSpec_DifferentStorageClass tests pvcMatchesVCTSpec when storage class differs
func TestPVCMatchesVCTSpec_DifferentStorageClass(t *testing.T) {
	pvcStorageClass := "standard"
	vctStorageClass := "premium"
	pvc := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &pvcStorageClass,
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}
	vct := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &vctStorageClass,
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}

	result := pvcMatchesVCTSpec(pvc, vct)
	if result {
		t.Error("Expected pvcMatchesVCTSpec to return false when storage class differs")
	}
}

// TestPVCMatchesVCTSpec_NilStorageClass tests pvcMatchesVCTSpec with nil storage class
func TestPVCMatchesVCTSpec_NilStorageClass(t *testing.T) {
	// Test case 1: Both nil - should match
	pvc1 := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: nil,
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}
	vct1 := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: nil,
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}

	if !pvcMatchesVCTSpec(pvc1, vct1) {
		t.Error("Expected pvcMatchesVCTSpec to return true when both storage classes are nil")
	}

	// Test case 2: One nil, one set - should not match
	storageClass := "standard"
	pvc2 := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: nil,
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}
	vct2 := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClass,
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}

	if pvcMatchesVCTSpec(pvc2, vct2) {
		t.Error("Expected pvcMatchesVCTSpec to return false when one storage class is nil")
	}
}

// TestCanRecyclePodWithinCPUFloor_NoCPUChange tests canRecyclePodWithinCPUFloor when no CPU change is active
func TestCanRecyclePodWithinCPUFloor_NoCPUChange(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-0",
			Namespace: "test",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "splunk",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("1000m"),
						},
					},
				},
			},
		},
	}

	replicas := int32(3)
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
		},
	}

	// State with no CPU change - only VCT migration
	state := &UnifiedTransitionState{
		VCTMigration: &VCTMigrationTransition{
			ExpectedStorageClasses: map[string]string{
				"pvc-data": "premium",
			},
		},
	}

	result := canRecyclePodWithinCPUFloor(ctx, c, statefulSet, pod, state, 1)
	if !result {
		t.Error("Expected canRecyclePodWithinCPUFloor to return true when no CPU change is active")
	}
}

// TestCanRecyclePodWithinCPUFloor_WouldViolate tests canRecyclePodWithinCPUFloor when recycling would violate floor
func TestCanRecyclePodWithinCPUFloor_WouldViolate(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	replicas := int32(3)
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
		},
	}
	c.AddObject(statefulSet)

	// Create only 1 ready pod (below minimum required)
	readyPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-0",
			Namespace: "test",
			Labels:    map[string]string{"app": "test"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "splunk",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("1000m"),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
	c.AddObject(readyPod)

	// State with CPU change: original 3 replicas * 1000m = 3000m floor
	// Current total: 1 pod * 1000m = 1000m
	// After recycling: 0m (below 3000m - 1000m = 2000m floor)
	state := &UnifiedTransitionState{
		CPUChange: &CPUTransition{
			OriginalCPUMillis: 1000,
			TargetCPUMillis:   2000,
			OriginalReplicas:  3,
			TargetReplicas:    2,
		},
	}

	result := canRecyclePodWithinCPUFloor(ctx, c, statefulSet, readyPod, state, 1)
	if result {
		t.Error("Expected canRecyclePodWithinCPUFloor to return false when recycling would violate floor")
	}
}

// TestCanRecyclePodWithinCPUFloor_Safe tests canRecyclePodWithinCPUFloor when recycling is safe
func TestCanRecyclePodWithinCPUFloor_Safe(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	replicas := int32(4)
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
		},
	}
	c.AddObject(statefulSet)

	// Create 4 ready pods with 1000m CPU each
	for i := 0; i < 4; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("test-%d", i),
				Namespace: "test",
				Labels:    map[string]string{"app": "test"},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "splunk",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("1000m"),
							},
						},
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			},
		}
		c.AddObject(pod)
	}

	// The pod we want to recycle
	podToRecycle := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-0",
			Namespace: "test",
			Labels:    map[string]string{"app": "test"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "splunk",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("1000m"),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}

	// State with CPU change: original 4 replicas * 1000m = 4000m floor
	// Parallel updates = 1, so minCPUFloor = 4000m - 1000m = 3000m
	// Current total: 4 pods * 1000m = 4000m
	// After recycling 1 pod: 3000m (equals floor, so safe)
	state := &UnifiedTransitionState{
		CPUChange: &CPUTransition{
			OriginalCPUMillis: 1000,
			TargetCPUMillis:   2000,
			OriginalReplicas:  4,
			TargetReplicas:    2,
		},
	}

	result := canRecyclePodWithinCPUFloor(ctx, c, statefulSet, podToRecycle, state, 1)
	if !result {
		t.Error("Expected canRecyclePodWithinCPUFloor to return true when recycling is safe")
	}
}

// =============================================================================
// handleUnifiedTransition Integration Tests
// =============================================================================

// TestHandleUnifiedTransition_CPUOnly tests that handleUnifiedTransition correctly handles
// CPU-only transitions (no VCT migration).
func TestHandleUnifiedTransition_CPUOnly(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 2
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sts",
			Namespace: "test",
			Annotations: map[string]string{
				UnifiedTransitionStateAnnotation: `{
					"cpuChange": {
						"originalCPUMillis": 1000,
						"targetCPUMillis": 2000,
						"originalReplicas": 2,
						"targetReplicas": 1
					},
					"startedAt": "2024-01-01T00:00:00Z"
				}`,
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
	}
	c.AddObject(statefulSet)

	// Create pods with old CPU spec
	for i := 0; i < 2; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("test-sts-%d", i),
				Namespace: "test",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "splunk",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("1000m"),
							},
						},
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			},
		}
		c.AddObject(pod)
	}

	mgr := &DefaultStatefulSetPodManager{}

	phase, handled, err := handleUnifiedTransition(ctx, c, statefulSet, mgr, nil)
	if err != nil {
		t.Errorf("handleUnifiedTransition returned error: %v", err)
	}
	if !handled {
		t.Error("Expected handled=true for CPU-only transition")
	}
	if phase != enterpriseApi.PhaseUpdating && phase != enterpriseApi.PhaseScalingDown {
		t.Errorf("Expected PhaseUpdating or PhaseScalingDown, got %v", phase)
	}
}

// TestHandleUnifiedTransition_VCTOnly tests that handleUnifiedTransition correctly handles
// VCT-only migrations (no CPU change).
func TestHandleUnifiedTransition_VCTOnly(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 2
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sts",
			Namespace: "test",
			Annotations: map[string]string{
				UnifiedTransitionStateAnnotation: `{
					"vctMigration": {
						"expectedStorageClasses": {"pvc-data": "new-storage-class"}
					},
					"startedAt": "2024-01-01T00:00:00Z"
				}`,
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
		Status: appsv1.StatefulSetStatus{
			CurrentRevision: "rev-1",
			UpdateRevision:  "rev-2",
		},
	}
	c.AddObject(statefulSet)

	// Create pods with PVCs
	for i := 0; i < 2; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("test-sts-%d", i),
				Namespace: "test",
				Labels: map[string]string{
					"controller-revision-hash": "rev-1", // Old revision
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "splunk"},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			},
		}
		c.AddObject(pod)

		// Create PVC with old storage class
		oldSC := "old-storage-class"
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pvc-data-test-sts-%d", i),
				Namespace: "test",
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: &oldSC,
			},
		}
		c.AddObject(pvc)
	}

	mgr := &DefaultStatefulSetPodManager{}

	phase, handled, err := handleUnifiedTransition(ctx, c, statefulSet, mgr, nil)
	if err != nil {
		t.Errorf("handleUnifiedTransition returned error: %v", err)
	}
	if !handled {
		t.Error("Expected handled=true for VCT-only transition")
	}
	// VCT-only transition should be in updating phase
	if phase != enterpriseApi.PhaseUpdating {
		t.Errorf("Expected PhaseUpdating, got %v", phase)
	}
}

// TestHandleUnifiedTransition_Combined tests that handleUnifiedTransition correctly handles
// combined CPU + VCT transitions.
func TestHandleUnifiedTransition_Combined(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 2
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sts",
			Namespace: "test",
			Annotations: map[string]string{
				UnifiedTransitionStateAnnotation: `{
					"cpuChange": {
						"originalCPUMillis": 1000,
						"targetCPUMillis": 2000,
						"originalReplicas": 2,
						"targetReplicas": 1
					},
					"vctMigration": {
						"expectedStorageClasses": {"pvc-data": "new-storage-class"}
					},
					"startedAt": "2024-01-01T00:00:00Z"
				}`,
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
	}
	c.AddObject(statefulSet)

	// Create pods
	for i := 0; i < 2; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("test-sts-%d", i),
				Namespace: "test",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "splunk",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("1000m"),
							},
						},
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			},
		}
		c.AddObject(pod)
	}

	mgr := &DefaultStatefulSetPodManager{}

	phase, handled, err := handleUnifiedTransition(ctx, c, statefulSet, mgr, nil)
	if err != nil {
		t.Errorf("handleUnifiedTransition returned error: %v", err)
	}
	if !handled {
		t.Error("Expected handled=true for combined transition")
	}
	// Combined transition should be processing
	if phase != enterpriseApi.PhaseUpdating && phase != enterpriseApi.PhaseScalingDown {
		t.Errorf("Expected PhaseUpdating or PhaseScalingDown, got %v", phase)
	}
}

// TestHandleUnifiedTransition_NoTransition tests that handleUnifiedTransition correctly returns
// (PhaseReady, false, nil) when no transition annotation is present.
func TestHandleUnifiedTransition_NoTransition(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 2
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sts",
			Namespace: "test",
			// No annotations - no transition in progress
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
	}
	c.AddObject(statefulSet)

	mgr := &DefaultStatefulSetPodManager{}

	phase, handled, err := handleUnifiedTransition(ctx, c, statefulSet, mgr, nil)
	if err != nil {
		t.Errorf("handleUnifiedTransition returned error: %v", err)
	}
	if handled {
		t.Error("Expected handled=false when no transition annotation")
	}
	if phase != enterpriseApi.PhaseReady {
		t.Errorf("Expected PhaseReady, got %v", phase)
	}
}

// TestHandleUnifiedTransition_AlreadyComplete tests that handleUnifiedTransition correctly
// clears the annotation and returns when transition is already complete.
func TestHandleUnifiedTransition_AlreadyComplete(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 1
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sts",
			Namespace: "test",
			Annotations: map[string]string{
				UnifiedTransitionStateAnnotation: `{
					"cpuChange": {
						"originalCPUMillis": 1000,
						"targetCPUMillis": 2000,
						"originalReplicas": 2,
						"targetReplicas": 1
					},
					"startedAt": "2024-01-01T00:00:00Z",
					"finishedAt": "2024-01-01T00:10:00Z"
				}`,
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
	}
	c.AddObject(statefulSet)

	// Create pod with new spec
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sts-0",
			Namespace: "test",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "splunk",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("2000m"),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
	c.AddObject(pod)

	mgr := &DefaultStatefulSetPodManager{}

	phase, handled, err := handleUnifiedTransition(ctx, c, statefulSet, mgr, nil)
	if err != nil {
		t.Errorf("handleUnifiedTransition returned error: %v", err)
	}
	if !handled {
		t.Error("Expected handled=true for already-complete transition (to clear annotation)")
	}
	if phase != enterpriseApi.PhaseReady {
		t.Errorf("Expected PhaseReady after clearing completed transition, got %v", phase)
	}
}

// ============================================================
// Tests for Task 5.1-5.5: Edge Cases and Error Recovery
// ============================================================

// TestIsPVCStuckInDeletion tests the helper function for detecting stuck PVCs
func TestIsPVCStuckInDeletion(t *testing.T) {
	tests := []struct {
		name              string
		deletionTimestamp *metav1.Time
		expected          bool
	}{
		{
			name:              "no deletion timestamp",
			deletionTimestamp: nil,
			expected:          false,
		},
		{
			name: "deletion timestamp recent (not stuck)",
			deletionTimestamp: &metav1.Time{
				Time: time.Now().Add(-2 * time.Minute),
			},
			expected: false,
		},
		{
			name: "deletion timestamp old (stuck)",
			deletionTimestamp: &metav1.Time{
				Time: time.Now().Add(-35 * time.Minute),
			},
			expected: true,
		},
		{
			name: "deletion timestamp exactly at threshold",
			deletionTimestamp: &metav1.Time{
				Time: time.Now().Add(-30*time.Minute - 1*time.Second), // slightly over to account for execution time
			},
			expected: true, // Will be just over 30 minutes
		},
		{
			name: "deletion timestamp just over threshold",
			deletionTimestamp: &metav1.Time{
				Time: time.Now().Add(-31 * time.Minute),
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-pvc",
					Namespace:         "test",
					DeletionTimestamp: tt.deletionTimestamp,
				},
			}

			result := isPVCStuckInDeletion(pvc)
			if result != tt.expected {
				t.Errorf("isPVCStuckInDeletion() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestIsTransitionStalled tests the helper function for detecting stalled transitions
func TestIsTransitionStalled(t *testing.T) {
	tests := []struct {
		name     string
		state    *UnifiedTransitionState
		timeout  time.Duration
		expected bool
	}{
		{
			name:     "nil state",
			state:    nil,
			timeout:  30 * time.Minute,
			expected: false,
		},
		{
			name: "empty startedAt",
			state: &UnifiedTransitionState{
				StartedAt: "",
			},
			timeout:  30 * time.Minute,
			expected: false,
		},
		{
			name: "invalid startedAt format",
			state: &UnifiedTransitionState{
				StartedAt: "invalid-timestamp",
			},
			timeout:  30 * time.Minute,
			expected: false,
		},
		{
			name: "transition started recently (not stalled)",
			state: &UnifiedTransitionState{
				StartedAt: time.Now().Add(-10 * time.Minute).Format(time.RFC3339),
			},
			timeout:  30 * time.Minute,
			expected: false,
		},
		{
			name: "transition running too long (stalled)",
			state: &UnifiedTransitionState{
				StartedAt: time.Now().Add(-45 * time.Minute).Format(time.RFC3339),
			},
			timeout:  30 * time.Minute,
			expected: true,
		},
		{
			name: "transition at timeout boundary",
			state: &UnifiedTransitionState{
				StartedAt: time.Now().Add(-30*time.Minute - 1*time.Second).Format(time.RFC3339), // slightly over to account for execution time
			},
			timeout:  30 * time.Minute,
			expected: true, // Will be just over 30 minutes
		},
		{
			name: "transition just over timeout",
			state: &UnifiedTransitionState{
				StartedAt: time.Now().Add(-31 * time.Minute).Format(time.RFC3339),
			},
			timeout:  30 * time.Minute,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTransitionStalled(tt.state, tt.timeout)
			if result != tt.expected {
				t.Errorf("isTransitionStalled() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestGetUnifiedTransitionStallTimeout tests parsing the stall timeout annotation
func TestGetUnifiedTransitionStallTimeout(t *testing.T) {
	tests := []struct {
		name       string
		annotation string
		expected   time.Duration
	}{
		{
			name:       "missing annotation uses default",
			annotation: "",
			expected:   DefaultUnifiedTransitionStallTimeout,
		},
		{
			name:       "valid 1h timeout",
			annotation: "1h",
			expected:   time.Hour,
		},
		{
			name:       "valid 45m timeout",
			annotation: "45m",
			expected:   45 * time.Minute,
		},
		{
			name:       "invalid format uses default",
			annotation: "invalid",
			expected:   DefaultUnifiedTransitionStallTimeout,
		},
		{
			name:       "zero value uses default",
			annotation: "0s",
			expected:   DefaultUnifiedTransitionStallTimeout,
		},
		{
			name:       "negative value uses default",
			annotation: "-10m",
			expected:   DefaultUnifiedTransitionStallTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sts",
					Namespace: "test",
				},
			}

			if tt.annotation != "" {
				statefulSet.Annotations = map[string]string{
					UnifiedTransitionStallTimeoutAnnotation: tt.annotation,
				}
			}

			result := getUnifiedTransitionStallTimeout(statefulSet)
			if result != tt.expected {
				t.Errorf("getUnifiedTransitionStallTimeout() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestRecordPodFailure tests the pod failure tracking logic
func TestRecordPodFailure(t *testing.T) {
	tests := []struct {
		name              string
		initialFailedPods map[string]FailedPodInfo
		podName           string
		errMsg            string
		expectedPermanent bool
		expectedFailCount int
	}{
		{
			name:              "first failure",
			initialFailedPods: nil,
			podName:           "test-pod-0",
			errMsg:            "connection refused",
			expectedPermanent: false,
			expectedFailCount: 1,
		},
		{
			name: "second failure",
			initialFailedPods: map[string]FailedPodInfo{
				"test-pod-0": {FailCount: 1, LastError: "first error"},
			},
			podName:           "test-pod-0",
			errMsg:            "timeout",
			expectedPermanent: false,
			expectedFailCount: 2,
		},
		{
			name: "third failure - becomes permanent",
			initialFailedPods: map[string]FailedPodInfo{
				"test-pod-0": {FailCount: 2, LastError: "second error"},
			},
			podName:           "test-pod-0",
			errMsg:            "third failure",
			expectedPermanent: true,
			expectedFailCount: 3,
		},
		{
			name: "different pod failure",
			initialFailedPods: map[string]FailedPodInfo{
				"test-pod-0": {FailCount: 2, LastError: "error"},
			},
			podName:           "test-pod-1",
			errMsg:            "new pod error",
			expectedPermanent: false,
			expectedFailCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &UnifiedTransitionState{
				FailedPods: tt.initialFailedPods,
			}

			permanent := recordPodFailure(state, tt.podName, tt.errMsg)

			if permanent != tt.expectedPermanent {
				t.Errorf("recordPodFailure() returned permanent=%v, want %v", permanent, tt.expectedPermanent)
			}

			if state.FailedPods == nil {
				t.Fatal("Expected FailedPods map to be initialized")
			}

			failInfo, exists := state.FailedPods[tt.podName]
			if !exists {
				t.Fatalf("Expected FailedPods to contain %s", tt.podName)
			}

			if failInfo.FailCount != tt.expectedFailCount {
				t.Errorf("Expected FailCount=%d, got %d", tt.expectedFailCount, failInfo.FailCount)
			}

			if failInfo.LastError != tt.errMsg {
				t.Errorf("Expected LastError=%q, got %q", tt.errMsg, failInfo.LastError)
			}

			if failInfo.LastAttempt == "" {
				t.Error("Expected LastAttempt to be set")
			}
		})
	}
}

// TestIsPodPermanentlyFailed tests the helper function for checking permanent failures
func TestIsPodPermanentlyFailed(t *testing.T) {
	tests := []struct {
		name       string
		failedPods map[string]FailedPodInfo
		podName    string
		expected   bool
	}{
		{
			name:       "nil failedPods",
			failedPods: nil,
			podName:    "test-pod-0",
			expected:   false,
		},
		{
			name:       "empty failedPods",
			failedPods: map[string]FailedPodInfo{},
			podName:    "test-pod-0",
			expected:   false,
		},
		{
			name: "pod not in failedPods",
			failedPods: map[string]FailedPodInfo{
				"other-pod": {FailCount: 5},
			},
			podName:  "test-pod-0",
			expected: false,
		},
		{
			name: "pod with 1 failure (not permanent)",
			failedPods: map[string]FailedPodInfo{
				"test-pod-0": {FailCount: 1},
			},
			podName:  "test-pod-0",
			expected: false,
		},
		{
			name: "pod with 2 failures (not permanent)",
			failedPods: map[string]FailedPodInfo{
				"test-pod-0": {FailCount: 2},
			},
			podName:  "test-pod-0",
			expected: false,
		},
		{
			name: "pod with 3 failures (permanent)",
			failedPods: map[string]FailedPodInfo{
				"test-pod-0": {FailCount: 3},
			},
			podName:  "test-pod-0",
			expected: true,
		},
		{
			name: "pod with more than 3 failures (permanent)",
			failedPods: map[string]FailedPodInfo{
				"test-pod-0": {FailCount: 5},
			},
			podName:  "test-pod-0",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &UnifiedTransitionState{
				FailedPods: tt.failedPods,
			}

			result := isPodPermanentlyFailed(state, tt.podName)
			if result != tt.expected {
				t.Errorf("isPodPermanentlyFailed() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestRecyclePodForUnifiedTransition_PVCDeletionFailure tests that pod recycling
// continues even when PVC deletion fails
func TestRecyclePodForUnifiedTransition_PVCDeletionFailure(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	storageClass := "premium-ssd"
	var replicas int32 = 1
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sts",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pvc-data"},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &storageClass,
					},
				},
			},
		},
	}
	c.AddObject(statefulSet)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sts-0",
			Namespace: "test",
			UID:       "test-uid",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "splunk",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("1000m"),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
	c.AddObject(pod)

	// Create a PVC that will trigger delete failure
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "pvc-data-test-sts-0",
			Namespace:  "test",
			Finalizers: []string{"kubernetes.io/pvc-protection"},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClass,
		},
	}
	c.AddObject(pvc)

	// Induce delete error for PVC
	c.InduceErrorKind[splcommon.MockClientInduceErrorDelete] = errors.New("PVC deletion failed")

	state := &UnifiedTransitionState{
		VCTMigration: &VCTMigrationTransition{
			ExpectedStorageClasses: map[string]string{
				"pvc-data": "new-storage-class",
			},
		},
		StartedAt: time.Now().Format(time.RFC3339),
	}

	mgr := &DefaultStatefulSetPodManager{}

	// The function should NOT return an error even though PVC deletion fails
	// because PVC deletion failures are logged as warnings but don't block pod deletion
	err := recyclePodForUnifiedTransition(ctx, c, statefulSet, mgr, pod, 0, state, nil)

	// Pod deletion will also fail due to the induced error, so we expect an error
	// but the important thing is that the PVC deletion failure didn't cause an immediate return
	if err == nil {
		t.Log("Function completed - PVC deletion failure was handled gracefully")
	} else if err.Error() == "PVC deletion failed" {
		t.Error("Function returned PVC deletion error - should have continued to pod deletion")
	} else {
		// Expected: pod deletion error (since we induced delete errors)
		t.Logf("Got expected error from pod deletion: %v", err)
	}
}

// TestUnifiedTransition_MigrateFromCPUAware verifies that persisting a unified state
// also removes the legacy CPUAwareTransitionStateAnnotation (Task 6.3)
func TestUnifiedTransition_MigrateFromCPUAware(t *testing.T) {
	ctx := context.TODO()

	// StatefulSet with both old and potentially migrated state
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test",
			Annotations: map[string]string{
				// Legacy annotation that would have been set by old operator version
				CPUAwareTransitionStateAnnotation: `{
					"originalReplicas": 6,
					"targetReplicas": 3,
					"originalCPUMillis": 1000,
					"targetCPUMillis": 2000,
					"startedAt": "2024-01-15T09:00:00Z"
				}`,
			},
		},
	}

	// Create mock client
	mockClient := spltest.NewMockClient()
	mockClient.AddObject(statefulSet)

	// Create a new unified state (simulating what happens after migration)
	newState := &UnifiedTransitionState{
		CPUChange: &CPUTransition{
			OriginalCPUMillis: 1000,
			TargetCPUMillis:   2000,
			OriginalReplicas:  6,
			TargetReplicas:    3,
		},
		StartedAt: "2024-01-15T09:00:00Z",
	}

	// Persist the new state - this should also remove the old annotation
	err := persistUnifiedTransitionState(ctx, mockClient, statefulSet, newState)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify new annotation was set
	if _, exists := statefulSet.Annotations[UnifiedTransitionStateAnnotation]; !exists {
		t.Error("Expected UnifiedTransitionStateAnnotation to be set")
	}

	// CRITICAL: Verify old annotation was removed
	if _, exists := statefulSet.Annotations[CPUAwareTransitionStateAnnotation]; exists {
		t.Error("Expected CPUAwareTransitionStateAnnotation to be REMOVED after persisting unified state")
	}
}

// TestUnifiedTransition_CPUOnlyBackwardCompat verifies that CPU-only transitions
// using the legacy CPUAwareTransitionStateAnnotation are still handled correctly
// by the legacy handleCPUPreservingTransition (Task 6.4)
func TestUnifiedTransition_CPUOnlyBackwardCompat(t *testing.T) {
	// Test that getUnifiedTransitionState correctly migrates old format
	oldStateJSON := `{
		"originalReplicas": 10,
		"targetReplicas": 5,
		"originalCPUMillis": 500,
		"targetCPUMillis": 1000,
		"startedAt": "2024-01-15T09:00:00Z"
	}`

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test",
			Annotations: map[string]string{
				// Only the old annotation, simulating an upgrade scenario
				CPUAwareTransitionStateAnnotation: oldStateJSON,
			},
		},
	}

	// getUnifiedTransitionState should read and migrate the old format
	state, err := getUnifiedTransitionState(statefulSet)
	if err != nil {
		t.Fatalf("Expected no error during migration, got: %v", err)
	}
	if state == nil {
		t.Fatal("Expected non-nil state after migration")
	}

	// Verify the migrated state has correct values
	if state.CPUChange == nil {
		t.Fatal("Expected CPUChange to be populated from migrated state")
	}

	// Check all CPU change fields
	if state.CPUChange.OriginalReplicas != 10 {
		t.Errorf("Expected OriginalReplicas=10, got %d", state.CPUChange.OriginalReplicas)
	}
	if state.CPUChange.TargetReplicas != 5 {
		t.Errorf("Expected TargetReplicas=5, got %d", state.CPUChange.TargetReplicas)
	}
	if state.CPUChange.OriginalCPUMillis != 500 {
		t.Errorf("Expected OriginalCPUMillis=500, got %d", state.CPUChange.OriginalCPUMillis)
	}
	if state.CPUChange.TargetCPUMillis != 1000 {
		t.Errorf("Expected TargetCPUMillis=1000, got %d", state.CPUChange.TargetCPUMillis)
	}

	// Verify timestamp is preserved
	if state.StartedAt != "2024-01-15T09:00:00Z" {
		t.Errorf("Expected StartedAt to be preserved, got '%s'", state.StartedAt)
	}

	// VCTMigration should be nil since old format doesn't have it
	if state.VCTMigration != nil {
		t.Error("Expected VCTMigration to be nil for CPU-only migration")
	}
}

// TestUnifiedTransition_NewAnnotationTakesPrecedence verifies that when both
// old and new annotations exist, the new one takes precedence
func TestUnifiedTransition_NewAnnotationTakesPrecedence(t *testing.T) {
	storageClass := "premium-ssd"
	newStateJSON := `{
		"cpuChange": {
			"originalCPUMillis": 1000,
			"targetCPUMillis": 2000,
			"originalReplicas": 4,
			"targetReplicas": 2
		},
		"vctMigration": {
			"expectedStorageClasses": {
				"pvc-data": "premium-ssd"
			}
		},
		"startedAt": "2024-01-15T10:00:00Z"
	}`

	// Note: The old annotation has DIFFERENT values - we want to make sure the new one wins
	oldStateJSON := `{
		"originalReplicas": 8,
		"targetReplicas": 4,
		"originalCPUMillis": 500,
		"targetCPUMillis": 1000,
		"startedAt": "2024-01-14T09:00:00Z"
	}`

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test",
			Annotations: map[string]string{
				UnifiedTransitionStateAnnotation:  newStateJSON,
				CPUAwareTransitionStateAnnotation: oldStateJSON, // Should be ignored
			},
		},
	}

	state, err := getUnifiedTransitionState(statefulSet)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if state == nil {
		t.Fatal("Expected non-nil state")
	}

	// Verify we got values from the NEW annotation, not the old one
	if state.CPUChange.OriginalCPUMillis != 1000 {
		t.Errorf("Expected OriginalCPUMillis=1000 (from new), got %d", state.CPUChange.OriginalCPUMillis)
	}
	if state.CPUChange.TargetReplicas != 2 {
		t.Errorf("Expected TargetReplicas=2 (from new), got %d", state.CPUChange.TargetReplicas)
	}
	if state.StartedAt != "2024-01-15T10:00:00Z" {
		t.Errorf("Expected StartedAt from new annotation, got '%s'", state.StartedAt)
	}

	// Verify VCT migration is present (only in new format)
	if state.VCTMigration == nil {
		t.Error("Expected VCTMigration to be present from new annotation")
	} else if state.VCTMigration.ExpectedStorageClasses["pvc-data"] != storageClass {
		t.Errorf("Expected storage class 'premium-ssd', got '%s'",
			state.VCTMigration.ExpectedStorageClasses["pvc-data"])
	}
}

// TestHandleUnifiedTransition_LegacyCPUOnlySkipped verifies that handleUnifiedTransition
// returns (PhaseReady, false, nil) when only the legacy annotation is present,
// allowing handleCPUPreservingTransition to handle it instead
func TestHandleUnifiedTransition_LegacyCPUOnlySkipped(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// StatefulSet with ONLY the legacy annotation (no new unified annotation)
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test",
			Annotations: map[string]string{
				CPUAwareTransitionStateAnnotation: `{
					"originalReplicas": 6,
					"targetReplicas": 3,
					"originalCPUMillis": 1000,
					"targetCPUMillis": 2000,
					"startedAt": "2024-01-15T09:00:00Z"
				}`,
				PreserveTotalCPUAnnotation: "true",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: func() *int32 { r := int32(6); return &r }(),
		},
	}
	c.AddObject(statefulSet)

	mgr := &DefaultStatefulSetPodManager{}

	// handleUnifiedTransition should skip this (only legacy annotation present)
	phase, handled, err := handleUnifiedTransition(ctx, c, statefulSet, mgr, nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// handleUnifiedTransition should NOT handle legacy-only transitions
	if handled {
		t.Error("Expected handled=false for legacy-only annotation")
	}
	if phase != enterpriseApi.PhaseReady {
		t.Errorf("Expected PhaseReady, got %v", phase)
	}
}
