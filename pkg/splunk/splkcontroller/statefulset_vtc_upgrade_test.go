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
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCompareVolumeClaimTemplates_NoChanges(t *testing.T) {
	storageClass := "standard"
	current := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pvc-data"},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &storageClass,
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("10Gi"),
							},
						},
					},
				},
			},
		},
	}
	revised := current.DeepCopy()

	result := CompareVolumeClaimTemplates(current, revised)

	if result.RequiresRecreate {
		t.Errorf("Expected no recreate required, got RequiresRecreate=true with reason: %s", result.RecreateReason)
	}
	if len(result.StorageExpansions) != 0 {
		t.Errorf("Expected no storage expansions, got %d", len(result.StorageExpansions))
	}
}

func TestCompareVolumeClaimTemplates_StorageExpansion(t *testing.T) {
	storageClass := "standard"
	current := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pvc-data"},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &storageClass,
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("10Gi"),
							},
						},
					},
				},
			},
		},
	}
	revised := current.DeepCopy()
	revised.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("20Gi")

	result := CompareVolumeClaimTemplates(current, revised)

	if result.RequiresRecreate {
		t.Errorf("Expected no recreate for storage expansion, got RequiresRecreate=true with reason: %s", result.RecreateReason)
	}
	if len(result.StorageExpansions) != 1 {
		t.Errorf("Expected 1 storage expansion, got %d", len(result.StorageExpansions))
	}
	if len(result.StorageExpansions) > 0 {
		expansion := result.StorageExpansions[0]
		if expansion.TemplateName != "pvc-data" {
			t.Errorf("Expected template name 'pvc-data', got '%s'", expansion.TemplateName)
		}
		if expansion.OldSize.String() != "10Gi" {
			t.Errorf("Expected old size '10Gi', got '%s'", expansion.OldSize.String())
		}
		if expansion.NewSize.String() != "20Gi" {
			t.Errorf("Expected new size '20Gi', got '%s'", expansion.NewSize.String())
		}
	}
}

func TestCompareVolumeClaimTemplates_StorageDecrease(t *testing.T) {
	storageClass := "standard"
	current := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pvc-data"},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &storageClass,
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("20Gi"),
							},
						},
					},
				},
			},
		},
	}
	revised := current.DeepCopy()
	revised.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("10Gi")

	result := CompareVolumeClaimTemplates(current, revised)

	if !result.RequiresRecreate {
		t.Error("Expected RequiresRecreate=true for storage decrease")
	}
	if result.RecreateReason == "" {
		t.Error("Expected a reason for storage decrease recreate")
	}
}

func TestCompareVolumeClaimTemplates_StorageClassChange(t *testing.T) {
	storageClass1 := "standard"
	storageClass2 := "premium"
	current := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pvc-data"},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &storageClass1,
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("10Gi"),
							},
						},
					},
				},
			},
		},
	}
	revised := current.DeepCopy()
	revised.Spec.VolumeClaimTemplates[0].Spec.StorageClassName = &storageClass2

	result := CompareVolumeClaimTemplates(current, revised)

	// Storage class change should set RequiresPVCMigration, NOT RequiresRecreate
	if result.RequiresRecreate {
		t.Errorf("Expected RequiresRecreate=false for storage class change, but got RequiresRecreate=true with reason: %s", result.RecreateReason)
	}
	if !result.RequiresPVCMigration {
		t.Error("Expected RequiresPVCMigration=true for storage class change")
	}
	if len(result.PVCMigrationChanges) != 1 {
		t.Errorf("Expected 1 PVC migration change, got %d", len(result.PVCMigrationChanges))
	}
	if len(result.PVCMigrationChanges) > 0 {
		change := result.PVCMigrationChanges[0]
		if change.TemplateName != "pvc-data" {
			t.Errorf("Expected template name 'pvc-data', got '%s'", change.TemplateName)
		}
		if change.ChangeType != "storage-class" {
			t.Errorf("Expected change type 'storage-class', got '%s'", change.ChangeType)
		}
		if change.OldStorageClass != "standard" {
			t.Errorf("Expected old storage class 'standard', got '%s'", change.OldStorageClass)
		}
		if change.NewStorageClass != "premium" {
			t.Errorf("Expected new storage class 'premium', got '%s'", change.NewStorageClass)
		}
	}
}

func TestCompareVolumeClaimTemplates_VCTAdded(t *testing.T) {
	storageClass := "standard"
	current := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pvc-data"},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &storageClass,
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("10Gi"),
							},
						},
					},
				},
			},
		},
	}
	revised := current.DeepCopy()
	revised.Spec.VolumeClaimTemplates = append(revised.Spec.VolumeClaimTemplates, corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "pvc-logs"},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClass,
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("5Gi"),
				},
			},
		},
	})

	result := CompareVolumeClaimTemplates(current, revised)

	if !result.RequiresRecreate {
		t.Error("Expected RequiresRecreate=true for VCT addition")
	}
	if result.RecreateReason == "" {
		t.Error("Expected a reason for VCT addition recreate")
	}
}

func TestCompareVolumeClaimTemplates_VCTRemoved(t *testing.T) {
	storageClass := "standard"
	current := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pvc-data"},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &storageClass,
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("10Gi"),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pvc-logs"},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &storageClass,
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("5Gi"),
							},
						},
					},
				},
			},
		},
	}
	revised := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pvc-data"},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &storageClass,
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("10Gi"),
							},
						},
					},
				},
			},
		},
	}

	result := CompareVolumeClaimTemplates(current, revised)

	if !result.RequiresRecreate {
		t.Error("Expected RequiresRecreate=true for VCT removal")
	}
	if result.RecreateReason == "" {
		t.Error("Expected a reason for VCT removal recreate")
	}
}

func TestCompareVolumeClaimTemplates_AccessModesChange(t *testing.T) {
	storageClass := "standard"
	current := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pvc-data"},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &storageClass,
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("10Gi"),
							},
						},
					},
				},
			},
		},
	}
	revised := current.DeepCopy()
	revised.Spec.VolumeClaimTemplates[0].Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}

	result := CompareVolumeClaimTemplates(current, revised)

	// Access modes change should set RequiresPVCMigration, NOT RequiresRecreate
	if result.RequiresRecreate {
		t.Errorf("Expected RequiresRecreate=false for access modes change, but got RequiresRecreate=true with reason: %s", result.RecreateReason)
	}
	if !result.RequiresPVCMigration {
		t.Error("Expected RequiresPVCMigration=true for access modes change")
	}
	if len(result.PVCMigrationChanges) != 1 {
		t.Errorf("Expected 1 PVC migration change, got %d", len(result.PVCMigrationChanges))
	}
	if len(result.PVCMigrationChanges) > 0 {
		change := result.PVCMigrationChanges[0]
		if change.TemplateName != "pvc-data" {
			t.Errorf("Expected template name 'pvc-data', got '%s'", change.TemplateName)
		}
		if change.ChangeType != "access-modes" {
			t.Errorf("Expected change type 'access-modes', got '%s'", change.ChangeType)
		}
		if len(change.OldAccessModes) != 1 || change.OldAccessModes[0] != corev1.ReadWriteOnce {
			t.Errorf("Expected old access modes [ReadWriteOnce], got %v", change.OldAccessModes)
		}
		if len(change.NewAccessModes) != 1 || change.NewAccessModes[0] != corev1.ReadWriteMany {
			t.Errorf("Expected new access modes [ReadWriteMany], got %v", change.NewAccessModes)
		}
	}
}

// TestCompareVolumeClaimTemplates_StorageClassChange_Migration verifies that storage class
// changes set RequiresPVCMigration (not RequiresRecreate) with proper VCTChange details
func TestCompareVolumeClaimTemplates_StorageClassChange_Migration(t *testing.T) {
	oldSC := "standard"
	newSC := "premium-ssd"
	current := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "splunk-data"},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &oldSC,
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("100Gi"),
							},
						},
					},
				},
			},
		},
	}
	revised := current.DeepCopy()
	revised.Spec.VolumeClaimTemplates[0].Spec.StorageClassName = &newSC

	result := CompareVolumeClaimTemplates(current, revised)

	// Verify migration flags
	if result.RequiresRecreate {
		t.Errorf("Storage class change should NOT require recreate, got RequiresRecreate=true with reason: %s", result.RecreateReason)
	}
	if !result.RequiresPVCMigration {
		t.Error("Storage class change should set RequiresPVCMigration=true")
	}

	// Verify change details
	if len(result.PVCMigrationChanges) != 1 {
		t.Fatalf("Expected 1 PVC migration change, got %d", len(result.PVCMigrationChanges))
	}
	change := result.PVCMigrationChanges[0]
	if change.TemplateName != "splunk-data" {
		t.Errorf("Expected template name 'splunk-data', got '%s'", change.TemplateName)
	}
	if change.ChangeType != "storage-class" {
		t.Errorf("Expected change type 'storage-class', got '%s'", change.ChangeType)
	}
	if change.OldStorageClass != "standard" {
		t.Errorf("Expected old storage class 'standard', got '%s'", change.OldStorageClass)
	}
	if change.NewStorageClass != "premium-ssd" {
		t.Errorf("Expected new storage class 'premium-ssd', got '%s'", change.NewStorageClass)
	}
}

// TestCompareVolumeClaimTemplates_AccessModesChange_Migration verifies that access modes
// changes set RequiresPVCMigration (not RequiresRecreate) with proper VCTChange details
func TestCompareVolumeClaimTemplates_AccessModesChange_Migration(t *testing.T) {
	storageClass := "standard"
	current := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "splunk-logs"},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &storageClass,
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("50Gi"),
							},
						},
					},
				},
			},
		},
	}
	revised := current.DeepCopy()
	revised.Spec.VolumeClaimTemplates[0].Spec.AccessModes = []corev1.PersistentVolumeAccessMode{
		corev1.ReadWriteMany,
	}

	result := CompareVolumeClaimTemplates(current, revised)

	// Verify migration flags
	if result.RequiresRecreate {
		t.Errorf("Access modes change should NOT require recreate, got RequiresRecreate=true with reason: %s", result.RecreateReason)
	}
	if !result.RequiresPVCMigration {
		t.Error("Access modes change should set RequiresPVCMigration=true")
	}

	// Verify change details
	if len(result.PVCMigrationChanges) != 1 {
		t.Fatalf("Expected 1 PVC migration change, got %d", len(result.PVCMigrationChanges))
	}
	change := result.PVCMigrationChanges[0]
	if change.TemplateName != "splunk-logs" {
		t.Errorf("Expected template name 'splunk-logs', got '%s'", change.TemplateName)
	}
	if change.ChangeType != "access-modes" {
		t.Errorf("Expected change type 'access-modes', got '%s'", change.ChangeType)
	}
	if len(change.OldAccessModes) != 1 || change.OldAccessModes[0] != corev1.ReadWriteOnce {
		t.Errorf("Expected old access modes [ReadWriteOnce], got %v", change.OldAccessModes)
	}
	if len(change.NewAccessModes) != 1 || change.NewAccessModes[0] != corev1.ReadWriteMany {
		t.Errorf("Expected new access modes [ReadWriteMany], got %v", change.NewAccessModes)
	}
}

// TestCompareVolumeClaimTemplates_MultipleChanges verifies that multiple VCT changes
// are all captured in PVCMigrationChanges (storage class + access modes on different VCTs)
func TestCompareVolumeClaimTemplates_MultipleChanges(t *testing.T) {
	oldSC := "standard"
	newSC := "premium"
	current := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pvc-data"},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &oldSC,
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("100Gi"),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pvc-logs"},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &oldSC,
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("50Gi"),
							},
						},
					},
				},
			},
		},
	}
	revised := current.DeepCopy()
	// Change storage class on pvc-data
	revised.Spec.VolumeClaimTemplates[0].Spec.StorageClassName = &newSC
	// Change access modes on pvc-logs
	revised.Spec.VolumeClaimTemplates[1].Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}

	result := CompareVolumeClaimTemplates(current, revised)

	// Verify migration flags
	if result.RequiresRecreate {
		t.Errorf("Multiple migration changes should NOT require recreate, got RequiresRecreate=true with reason: %s", result.RecreateReason)
	}
	if !result.RequiresPVCMigration {
		t.Error("Multiple changes should set RequiresPVCMigration=true")
	}

	// Verify all changes are captured
	if len(result.PVCMigrationChanges) != 2 {
		t.Fatalf("Expected 2 PVC migration changes, got %d", len(result.PVCMigrationChanges))
	}

	// Check that we have both types of changes (order may vary due to map iteration)
	hasStorageClassChange := false
	hasAccessModesChange := false
	for _, change := range result.PVCMigrationChanges {
		if change.ChangeType == "storage-class" && change.TemplateName == "pvc-data" {
			hasStorageClassChange = true
			if change.OldStorageClass != "standard" || change.NewStorageClass != "premium" {
				t.Errorf("Storage class change has wrong values: old='%s', new='%s'", change.OldStorageClass, change.NewStorageClass)
			}
		}
		if change.ChangeType == "access-modes" && change.TemplateName == "pvc-logs" {
			hasAccessModesChange = true
			if len(change.OldAccessModes) != 1 || change.OldAccessModes[0] != corev1.ReadWriteOnce {
				t.Errorf("Access modes change has wrong old values: %v", change.OldAccessModes)
			}
			if len(change.NewAccessModes) != 1 || change.NewAccessModes[0] != corev1.ReadWriteMany {
				t.Errorf("Access modes change has wrong new values: %v", change.NewAccessModes)
			}
		}
	}
	if !hasStorageClassChange {
		t.Error("Missing storage-class change for pvc-data")
	}
	if !hasAccessModesChange {
		t.Error("Missing access-modes change for pvc-logs")
	}
}

// TestCompareVolumeClaimTemplates_BothStorageClassAndAccessModes verifies that when both
// storage class AND access modes change on the SAME VCT, two separate VCTChange entries are created
func TestCompareVolumeClaimTemplates_BothStorageClassAndAccessModes(t *testing.T) {
	oldSC := "standard"
	newSC := "premium"
	current := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pvc-data"},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &oldSC,
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("100Gi"),
							},
						},
					},
				},
			},
		},
	}
	revised := current.DeepCopy()
	// Change both storage class and access modes on same VCT
	revised.Spec.VolumeClaimTemplates[0].Spec.StorageClassName = &newSC
	revised.Spec.VolumeClaimTemplates[0].Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}

	result := CompareVolumeClaimTemplates(current, revised)

	// Verify migration flags
	if result.RequiresRecreate {
		t.Errorf("Combined changes should NOT require recreate, got RequiresRecreate=true with reason: %s", result.RecreateReason)
	}
	if !result.RequiresPVCMigration {
		t.Error("Combined changes should set RequiresPVCMigration=true")
	}

	// Verify both changes are captured separately
	if len(result.PVCMigrationChanges) != 2 {
		t.Fatalf("Expected 2 PVC migration changes (storage-class + access-modes), got %d", len(result.PVCMigrationChanges))
	}

	hasStorageClassChange := false
	hasAccessModesChange := false
	for _, change := range result.PVCMigrationChanges {
		if change.ChangeType == "storage-class" {
			hasStorageClassChange = true
		}
		if change.ChangeType == "access-modes" {
			hasAccessModesChange = true
		}
	}
	if !hasStorageClassChange || !hasAccessModesChange {
		t.Errorf("Expected both storage-class and access-modes changes, got hasStorageClass=%v, hasAccessModes=%v", hasStorageClassChange, hasAccessModesChange)
	}
}
