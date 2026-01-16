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
	"fmt"
	"reflect"
	"time"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// This file contains logic for handling StatefulSet transitions when updating
// Volume Claim Templates (VCT) or CPU resources. This includes:
// - VCT comparison and migration
// - CPU-aware scaling transitions
// - Unified transitions (combined CPU + VCT changes)

// VCTStorageChange represents a storage change for a volume claim template
type VCTStorageChange struct {
	TemplateName string
	OldSize      resource.Quantity
	NewSize      resource.Quantity
}

// VCTChange represents a change to a VolumeClaimTemplate that requires PVC migration
type VCTChange struct {
	TemplateName    string                              // Name of the VCT
	ChangeType      string                              // "storage-class" or "access-modes"
	OldStorageClass string                              // Previous storage class name
	NewStorageClass string                              // New storage class name
	OldAccessModes  []corev1.PersistentVolumeAccessMode // Previous access modes
	NewAccessModes  []corev1.PersistentVolumeAccessMode // New access modes
}

// VCTCompareResult holds the result of comparing volume claim templates
type VCTCompareResult struct {
	RequiresRecreate     bool               // True if StatefulSet needs to be recreated
	StorageExpansions    []VCTStorageChange // Storage expansions that can be done in-place
	RecreateReason       string             // Reason why recreation is needed
	RequiresPVCMigration bool               // True when storage class or access modes change
	PVCMigrationChanges  []VCTChange        // List of VCT changes requiring migration
}

// CompareVolumeClaimTemplates compares volume claim templates between current and revised StatefulSets
// Returns a VCTCompareResult indicating what changes are needed
func CompareVolumeClaimTemplates(current, revised *appsv1.StatefulSet) VCTCompareResult {
	result := VCTCompareResult{
		RequiresRecreate:  false,
		StorageExpansions: []VCTStorageChange{},
	}

	currentVCTs := current.Spec.VolumeClaimTemplates
	revisedVCTs := revised.Spec.VolumeClaimTemplates

	// Build map of current VCTs by name
	currentVCTMap := make(map[string]corev1.PersistentVolumeClaim)
	for _, vct := range currentVCTs {
		currentVCTMap[vct.Name] = vct
	}

	// Build map of revised VCTs by name
	revisedVCTMap := make(map[string]corev1.PersistentVolumeClaim)
	for _, vct := range revisedVCTs {
		revisedVCTMap[vct.Name] = vct
	}

	// Check for removed VCTs (requires recreate)
	for name := range currentVCTMap {
		if _, exists := revisedVCTMap[name]; !exists {
			result.RequiresRecreate = true
			result.RecreateReason = fmt.Sprintf("VolumeClaimTemplate '%s' was removed", name)
			return result
		}
	}

	// Check for added VCTs (requires recreate)
	for name := range revisedVCTMap {
		if _, exists := currentVCTMap[name]; !exists {
			result.RequiresRecreate = true
			result.RecreateReason = fmt.Sprintf("VolumeClaimTemplate '%s' was added", name)
			return result
		}
	}

	// Compare each VCT
	for name, currentVCT := range currentVCTMap {
		revisedVCT := revisedVCTMap[name]

		// Check storage class change (use rolling migration)
		currentSC := ""
		if currentVCT.Spec.StorageClassName != nil {
			currentSC = *currentVCT.Spec.StorageClassName
		}
		revisedSC := ""
		if revisedVCT.Spec.StorageClassName != nil {
			revisedSC = *revisedVCT.Spec.StorageClassName
		}
		if currentSC != revisedSC {
			// Storage class change - use rolling migration instead of recreate
			result.RequiresPVCMigration = true
			result.PVCMigrationChanges = append(result.PVCMigrationChanges, VCTChange{
				TemplateName:    name,
				ChangeType:      "storage-class",
				OldStorageClass: currentSC,
				NewStorageClass: revisedSC,
			})
		}

		// Check access modes change (use rolling migration)
		if !reflect.DeepEqual(currentVCT.Spec.AccessModes, revisedVCT.Spec.AccessModes) {
			// Access modes change - use rolling migration instead of recreate
			result.RequiresPVCMigration = true
			result.PVCMigrationChanges = append(result.PVCMigrationChanges, VCTChange{
				TemplateName:   name,
				ChangeType:     "access-modes",
				OldAccessModes: currentVCT.Spec.AccessModes,
				NewAccessModes: revisedVCT.Spec.AccessModes,
			})
		}

		// Check storage size change
		currentSize := currentVCT.Spec.Resources.Requests[corev1.ResourceStorage]
		revisedSize := revisedVCT.Spec.Resources.Requests[corev1.ResourceStorage]

		if !currentSize.Equal(revisedSize) {
			// Storage size changed
			if revisedSize.Cmp(currentSize) < 0 {
				// Storage decrease requested - not supported
				result.RequiresRecreate = true
				result.RecreateReason = fmt.Sprintf("Storage decrease requested for VolumeClaimTemplate '%s' from %s to %s (not supported)", name, currentSize.String(), revisedSize.String())
				return result
			}
			// Storage increase - can potentially be done in-place
			result.StorageExpansions = append(result.StorageExpansions, VCTStorageChange{
				TemplateName: name,
				OldSize:      currentSize,
				NewSize:      revisedSize,
			})
		}
	}

	return result
}

// ExpandPVCStorage expands the storage of existing PVCs to match the new VCT size
// This is called when storage expansion is detected and the storage class supports volume expansion
func ExpandPVCStorage(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, changes []VCTStorageChange, eventPublisher splcommon.K8EventPublisher) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ExpandPVCStorage").WithValues(
		"name", statefulSet.GetName(),
		"namespace", statefulSet.GetNamespace())

	// Get all pods for this StatefulSet to find their PVCs
	replicas := int32(1)
	if statefulSet.Spec.Replicas != nil {
		replicas = *statefulSet.Spec.Replicas
	}

	for _, change := range changes {
		scopedLog.Info("Expanding PVC storage",
			"template", change.TemplateName,
			"oldSize", change.OldSize.String(),
			"newSize", change.NewSize.String())

		// Expand each PVC for this template
		for i := int32(0); i < replicas; i++ {
			pvcName := fmt.Sprintf("%s-%s-%d", change.TemplateName, statefulSet.GetName(), i)

			// Get the existing PVC
			pvc := &corev1.PersistentVolumeClaim{}
			namespacedName := types.NamespacedName{
				Namespace: statefulSet.GetNamespace(),
				Name:      pvcName,
			}

			err := c.Get(ctx, namespacedName, pvc)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					// PVC doesn't exist yet (replica not created), skip
					scopedLog.Info("PVC not found, skipping", "pvc", pvcName)
					continue
				}
				scopedLog.Error(err, "Failed to get PVC", "pvc", pvcName)
				return err
			}

			// Check if expansion is needed
			currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
			if currentSize.Cmp(change.NewSize) >= 0 {
				// PVC is already at or above the requested size
				scopedLog.Info("PVC already at requested size", "pvc", pvcName, "currentSize", currentSize.String())
				continue
			}

			// Update PVC storage request
			pvc.Spec.Resources.Requests[corev1.ResourceStorage] = change.NewSize

			err = splutil.UpdateResource(ctx, c, pvc)
			if err != nil {
				scopedLog.Error(err, "Failed to expand PVC", "pvc", pvcName)
				if eventPublisher != nil {
					eventPublisher.Warning(ctx, "PVCExpansionFailed", fmt.Sprintf("Failed to expand PVC %s: %v", pvcName, err))
				}
				return err
			}

			scopedLog.Info("Successfully requested PVC expansion", "pvc", pvcName, "newSize", change.NewSize.String())
			if eventPublisher != nil {
				eventPublisher.Normal(ctx, "PVCExpansionRequested", fmt.Sprintf("Requested PVC %s expansion to %s", pvcName, change.NewSize.String()))
			}
		}
	}

	return nil
}

// PVC stuck threshold - if deletion has been pending for more than 30 minutes.
// This threshold accounts for pod decommissioning time (which can take 15+ minutes)
// plus PVC deletion time, since PVCs cannot be deleted until the pod is fully removed.
const stuckThreshold = 30 * time.Minute

// isPVCStuckInDeletion checks if a PVC has a deletion timestamp but is not being removed.
// A PVC is considered stuck if it has been marked for deletion for more than 30 minutes.
// This can happen when finalizers prevent deletion or the storage backend is slow.
// The threshold is set to accommodate pod decommissioning time before PVC deletion can complete.
func isPVCStuckInDeletion(pvc *corev1.PersistentVolumeClaim) bool {
	if pvc.DeletionTimestamp == nil {
		return false
	}
	return time.Since(pvc.DeletionTimestamp.Time) > stuckThreshold
}

// pvcMatchesVCTSpec checks if an existing PVC matches the desired VCT specification.
// Compares storage class and access modes, handling nil storage class names properly.
func pvcMatchesVCTSpec(pvc *corev1.PersistentVolumeClaim, vct *corev1.PersistentVolumeClaim) bool {
	// Compare storage class names (handling nil)
	pvcStorageClass := ""
	if pvc.Spec.StorageClassName != nil {
		pvcStorageClass = *pvc.Spec.StorageClassName
	}
	vctStorageClass := ""
	if vct.Spec.StorageClassName != nil {
		vctStorageClass = *vct.Spec.StorageClassName
	}
	if pvcStorageClass != vctStorageClass {
		return false
	}

	// Compare access modes
	if !reflect.DeepEqual(pvc.Spec.AccessModes, vct.Spec.AccessModes) {
		return false
	}

	return true
}

// UnifiedTransitionState tracks ALL concurrent transitions in a single annotation.
// This enables recycling each pod ONCE for ALL pending changes (CPU + VCT).
type UnifiedTransitionState struct {
	// CPUChange tracks CPU-aware scaling transition (optional, nil if no CPU change)
	CPUChange *CPUTransition `json:"cpuChange,omitempty"`

	// VCTMigration tracks VCT migration transition (optional, nil if no VCT change)
	VCTMigration *VCTMigrationTransition `json:"vctMigration,omitempty"`

	// StartedAt is the timestamp when the transition started (RFC3339 format)
	StartedAt string `json:"startedAt"`

	// FinishedAt is the timestamp when the transition completed (RFC3339 format, empty if in progress)
	FinishedAt string `json:"finishedAt,omitempty"`

	// PodStatus tracks per-pod completion for large clusters
	PodStatus map[string]PodTransitionStatus `json:"podStatus,omitempty"`

	// FailedPods tracks pods that failed during transition
	FailedPods map[string]FailedPodInfo `json:"failedPods,omitempty"`
}

// CPUTransition tracks CPU-aware scaling parameters
type CPUTransition struct {
	OriginalCPUMillis int64 `json:"originalCPUMillis"`
	TargetCPUMillis   int64 `json:"targetCPUMillis"`
	OriginalReplicas  int32 `json:"originalReplicas"`
	TargetReplicas    int32 `json:"targetReplicas"`
}

// VCTMigrationTransition tracks VCT migration parameters
type VCTMigrationTransition struct {
	ExpectedStorageClasses map[string]string                              `json:"expectedStorageClasses"`
	ExpectedAccessModes    map[string][]corev1.PersistentVolumeAccessMode `json:"expectedAccessModes,omitempty"`
	PodsNeedingMigration   []int32                                        `json:"podsNeedingMigration,omitempty"`
}

// PodTransitionStatus tracks completion status for a single pod
type PodTransitionStatus struct {
	CPUUpdated bool   `json:"cpuUpdated,omitempty"`
	PVCUpdated bool   `json:"pvcUpdated,omitempty"`
	UpdatedAt  string `json:"updatedAt,omitempty"`
}

// FailedPodInfo captures details about a failed pod transition
type FailedPodInfo struct {
	LastError   string `json:"lastError"`
	FailCount   int    `json:"failCount"`
	LastAttempt string `json:"lastAttempt"`
}

// isTransitionStalled checks if the unified transition has been running too long.
// Returns true if the transition has exceeded the configured or default timeout (30 minutes).
// This helps detect and handle transitions that may be stuck due to external issues.
// Note: The timeout is configurable via annotation and should account for pod decommissioning time.
func isTransitionStalled(state *UnifiedTransitionState, timeout time.Duration) bool {
	if state == nil || state.StartedAt == "" {
		return false
	}

	startTime, err := time.Parse(time.RFC3339, state.StartedAt)
	if err != nil {
		// Cannot parse start time, consider it not stalled
		return false
	}

	return time.Since(startTime) > timeout
}

// getUnifiedTransitionStallTimeout parses the stall timeout from the StatefulSet annotation.
// Returns the configured timeout or DefaultUnifiedTransitionStallTimeout if not set or invalid.
func getUnifiedTransitionStallTimeout(statefulSet *appsv1.StatefulSet) time.Duration {
	if statefulSet.Annotations == nil {
		return DefaultUnifiedTransitionStallTimeout
	}

	timeoutStr, exists := statefulSet.Annotations[UnifiedTransitionStallTimeoutAnnotation]
	if !exists || timeoutStr == "" {
		return DefaultUnifiedTransitionStallTimeout
	}

	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil || timeout <= 0 {
		return DefaultUnifiedTransitionStallTimeout
	}

	return timeout
}

// recordPodFailure records a pod recycling failure in the transition state.
// Returns true if the pod has exceeded MaxPodRecycleFailures and should be skipped.
func recordPodFailure(state *UnifiedTransitionState, podName string, errMsg string) bool {
	if state.FailedPods == nil {
		state.FailedPods = make(map[string]FailedPodInfo)
	}

	failInfo, exists := state.FailedPods[podName]
	if !exists {
		failInfo = FailedPodInfo{}
	}

	failInfo.FailCount++
	failInfo.LastError = errMsg
	failInfo.LastAttempt = time.Now().Format(time.RFC3339)
	state.FailedPods[podName] = failInfo

	return failInfo.FailCount >= MaxPodRecycleFailures
}

// isPodPermanentlyFailed checks if a pod has exceeded the maximum failure count.
func isPodPermanentlyFailed(state *UnifiedTransitionState, podName string) bool {
	if state.FailedPods == nil {
		return false
	}

	failInfo, exists := state.FailedPods[podName]
	if !exists {
		return false
	}

	return failInfo.FailCount >= MaxPodRecycleFailures
}

// getUnifiedTransitionState parses the UnifiedTransitionStateAnnotation and returns the state.
// If the new annotation is not present, it checks for the old CPUAwareTransitionStateAnnotation
// and migrates it to the new format for backward compatibility.
// Returns nil if no transition state is found.
func getUnifiedTransitionState(statefulSet *appsv1.StatefulSet) (*UnifiedTransitionState, error) {
	if statefulSet.Annotations == nil {
		return nil, nil
	}

	// Check for new annotation first
	stateJSON, exists := statefulSet.Annotations[UnifiedTransitionStateAnnotation]
	if exists && stateJSON != "" {
		var state UnifiedTransitionState
		if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
			return nil, fmt.Errorf("failed to unmarshal unified transition state: %w", err)
		}
		return &state, nil
	}

	// Fall back to old annotation (backward compatibility)
	return migrateFromCPUAwareTransitionState(statefulSet)
}

// migrateFromCPUAwareTransitionState converts old CPUAwareTransitionState to UnifiedTransitionState.
// This ensures backward compatibility during operator upgrades.
func migrateFromCPUAwareTransitionState(statefulSet *appsv1.StatefulSet) (*UnifiedTransitionState, error) {
	if statefulSet.Annotations == nil {
		return nil, nil
	}

	oldStateJSON, exists := statefulSet.Annotations[CPUAwareTransitionStateAnnotation]
	if !exists || oldStateJSON == "" {
		return nil, nil
	}

	var oldState CPUAwareTransitionState
	if err := json.Unmarshal([]byte(oldStateJSON), &oldState); err != nil {
		return nil, fmt.Errorf("failed to unmarshal CPU-aware transition state: %w", err)
	}

	// Convert to new format
	newState := &UnifiedTransitionState{
		StartedAt:  oldState.StartedAt,
		FinishedAt: oldState.FinishedAt,
		CPUChange: &CPUTransition{
			OriginalCPUMillis: oldState.OriginalCPUMillis,
			TargetCPUMillis:   oldState.TargetCPUMillis,
			OriginalReplicas:  oldState.OriginalReplicas,
			TargetReplicas:    oldState.TargetReplicas,
		},
	}

	return newState, nil
}

// persistUnifiedTransitionState marshals the state and updates the StatefulSet annotation.
// It also removes the legacy CPUAwareTransitionStateAnnotation if present, completing the
// migration from the old format to the new unified format.
func persistUnifiedTransitionState(ctx context.Context, c splcommon.ControllerClient,
	statefulSet *appsv1.StatefulSet, state *UnifiedTransitionState) error {

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("persistUnifiedTransitionState").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	stateJSON, err := json.Marshal(state)
	if err != nil {
		scopedLog.Error(err, "Failed to marshal unified transition state")
		return err
	}

	if statefulSet.Annotations == nil {
		statefulSet.Annotations = make(map[string]string)
	}
	statefulSet.Annotations[UnifiedTransitionStateAnnotation] = string(stateJSON)

	// Remove legacy annotation if present (migration cleanup)
	// This ensures we don't have both old and new annotations simultaneously
	if _, hasOldAnnotation := statefulSet.Annotations[CPUAwareTransitionStateAnnotation]; hasOldAnnotation {
		scopedLog.Info("Removing legacy CPUAwareTransitionStateAnnotation during migration")
		delete(statefulSet.Annotations, CPUAwareTransitionStateAnnotation)
	}

	if err := splutil.UpdateResource(ctx, c, statefulSet); err != nil {
		scopedLog.Error(err, "Failed to persist unified transition state")
		return err
	}

	scopedLog.Info("Persisted unified transition state", "startedAt", state.StartedAt)
	return nil
}

// clearUnifiedTransitionState removes the UnifiedTransitionStateAnnotation from the StatefulSet.
func clearUnifiedTransitionState(ctx context.Context, c splcommon.ControllerClient,
	statefulSet *appsv1.StatefulSet) error {

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("clearUnifiedTransitionState").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	if statefulSet.Annotations == nil {
		return nil
	}

	// Remove both new and old annotations (in case old annotation still exists)
	_, hasNewAnnotation := statefulSet.Annotations[UnifiedTransitionStateAnnotation]
	_, hasOldAnnotation := statefulSet.Annotations[CPUAwareTransitionStateAnnotation]

	if !hasNewAnnotation && !hasOldAnnotation {
		return nil
	}

	delete(statefulSet.Annotations, UnifiedTransitionStateAnnotation)
	delete(statefulSet.Annotations, CPUAwareTransitionStateAnnotation)

	if err := splutil.UpdateResource(ctx, c, statefulSet); err != nil {
		scopedLog.Error(err, "Failed to clear unified transition state")
		return err
	}

	scopedLog.Info("Cleared unified transition state")
	return nil
}

// isUnifiedTransitionInProgress checks if any transition is active.
// Returns true if a transition annotation exists and has not finished.
func isUnifiedTransitionInProgress(statefulSet *appsv1.StatefulSet) bool {
	state, err := getUnifiedTransitionState(statefulSet)
	if err != nil || state == nil {
		return false
	}
	// Transition is in progress if FinishedAt is not set
	return state.FinishedAt == ""
}

// initUnifiedTransitionState creates an initial UnifiedTransitionState with the given transitions.
// Either cpuChange or vctMigration (or both) should be provided.
func initUnifiedTransitionState(cpuChange *CPUTransition, vctMigration *VCTMigrationTransition) *UnifiedTransitionState {
	return &UnifiedTransitionState{
		CPUChange:    cpuChange,
		VCTMigration: vctMigration,
		StartedAt:    time.Now().Format(time.RFC3339),
		PodStatus:    make(map[string]PodTransitionStatus),
		FailedPods:   make(map[string]FailedPodInfo),
	}
}

// isPodFullyUpdated checks if a pod has ALL required updates applied.
// Returns true only if:
// - CPU spec matches target (if CPUChange is active)
// - All PVC storage classes match target (if VCTMigration is active)
// - All PVC access modes match target (if VCTMigration specifies access modes)
func isPodFullyUpdated(
	ctx context.Context,
	c splcommon.ControllerClient,
	pod *corev1.Pod,
	statefulSet *appsv1.StatefulSet,
	state *UnifiedTransitionState,
) (bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("isPodFullyUpdated").WithValues(
		"podName", pod.GetName(),
		"namespace", pod.GetNamespace())

	// Check CPU update if CPUChange is active
	if state.CPUChange != nil {
		if !hasNewSpec(pod, state.CPUChange.TargetCPUMillis) {
			currentCPU := extractCPUFromPod(pod)
			scopedLog.Info("Pod CPU not updated",
				"currentCPU", currentCPU,
				"targetCPU", state.CPUChange.TargetCPUMillis)
			return false, nil
		}
	}

	// Check VCT migration if active
	if state.VCTMigration != nil {
		for vctName, expectedStorageClass := range state.VCTMigration.ExpectedStorageClasses {
			// Build PVC name: {vctName}-{podName}
			pvcName := fmt.Sprintf("%s-%s", vctName, pod.GetName())
			pvcNamespacedName := types.NamespacedName{
				Namespace: pod.GetNamespace(),
				Name:      pvcName,
			}

			var pvc corev1.PersistentVolumeClaim
			if err := c.Get(ctx, pvcNamespacedName, &pvc); err != nil {
				if k8serrors.IsNotFound(err) {
					// PVC doesn't exist yet - not updated
					scopedLog.Info("PVC not found, pod not fully updated",
						"pvcName", pvcName)
					return false, nil
				}
				scopedLog.Error(err, "Failed to get PVC", "pvcName", pvcName)
				return false, err
			}

			// Compare storage class
			pvcStorageClass := ""
			if pvc.Spec.StorageClassName != nil {
				pvcStorageClass = *pvc.Spec.StorageClassName
			}
			if pvcStorageClass != expectedStorageClass {
				scopedLog.Info("PVC storage class not updated",
					"pvcName", pvcName,
					"currentStorageClass", pvcStorageClass,
					"expectedStorageClass", expectedStorageClass)
				return false, nil
			}

			// Check access modes if specified
			if state.VCTMigration.ExpectedAccessModes != nil {
				expectedAccessModes, hasExpectedModes := state.VCTMigration.ExpectedAccessModes[vctName]
				if hasExpectedModes {
					if !reflect.DeepEqual(pvc.Spec.AccessModes, expectedAccessModes) {
						scopedLog.Info("PVC access modes not updated",
							"pvcName", pvcName,
							"currentAccessModes", pvc.Spec.AccessModes,
							"expectedAccessModes", expectedAccessModes)
						return false, nil
					}
				}
			}
		}
	}

	scopedLog.Info("Pod is fully updated")
	return true, nil
}

// recyclePodForUnifiedTransition handles pod recycling for combined CPU + VCT transitions.
// Key insight: When recycling a pod, delete both the pod AND its PVCs if VCT migration is active.
// The StatefulSet controller will recreate the pod with new spec AND new PVCs.
//
// Error handling:
// - Pod deletion failures are logged and returned as errors (caller tracks failures)
// - PVC deletion failures due to finalizers are logged as warnings and do not block pod deletion
// - Stuck PVCs (deletion pending > 5 minutes) trigger warning events
func recyclePodForUnifiedTransition(
	ctx context.Context,
	c splcommon.ControllerClient,
	statefulSet *appsv1.StatefulSet,
	mgr splcommon.StatefulSetPodManager,
	pod *corev1.Pod,
	podIndex int32,
	state *UnifiedTransitionState,
	eventPublisher splcommon.K8EventPublisher,
) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("recyclePodForUnifiedTransition").WithValues(
		"podName", pod.GetName(),
		"namespace", pod.GetNamespace(),
		"hasCPUChange", state.CPUChange != nil,
		"hasVCTMigration", state.VCTMigration != nil)

	// Step 1: Prepare for recycle (decommission Splunk peer)
	ready, err := mgr.PrepareRecycle(ctx, podIndex)
	if err != nil {
		scopedLog.Error(err, "Failed to prepare pod for recycle")
		return err
	}
	if !ready {
		scopedLog.Info("Pod decommissioning in progress, will retry")
		return nil // Will retry next reconcile
	}

	scopedLog.Info("Pod decommissioning complete, proceeding with recycle")

	// Step 2: Delete PVCs first (if VCT migration is active)
	// PVC deletion failures do NOT block pod deletion - we log warnings and continue
	if state.VCTMigration != nil {
		for vctName := range state.VCTMigration.ExpectedStorageClasses {
			pvcName := fmt.Sprintf("%s-%s", vctName, pod.GetName())
			pvc := &corev1.PersistentVolumeClaim{}
			pvcNamespacedName := types.NamespacedName{
				Namespace: statefulSet.GetNamespace(),
				Name:      pvcName,
			}

			if err := c.Get(ctx, pvcNamespacedName, pvc); err != nil {
				if k8serrors.IsNotFound(err) {
					scopedLog.Info("PVC already deleted", "pvcName", pvcName)
					continue
				}
				// Log warning but continue - PVC may be in a transient state
				scopedLog.Info("Warning: Failed to get PVC, continuing with pod deletion",
					"pvcName", pvcName, "error", err.Error())
				continue
			}

			// Check if PVC is stuck in deletion
			if isPVCStuckInDeletion(pvc) {
				scopedLog.Info("Warning: PVC stuck in deletion",
					"pvcName", pvcName,
					"deletionTimestamp", pvc.DeletionTimestamp.Time,
					"finalizers", pvc.Finalizers)
				if eventPublisher != nil {
					eventPublisher.Warning(ctx, "PVCStuckInDeletion",
						fmt.Sprintf("PVC %s has been pending deletion for over 20 minutes. Finalizers: %v",
							pvcName, pvc.Finalizers))
				}
				// Continue anyway - pod recreation will handle this eventually
				continue
			}

			oldSC := "<nil>"
			if pvc.Spec.StorageClassName != nil {
				oldSC = *pvc.Spec.StorageClassName
			}

			scopedLog.Info("Deleting PVC for VCT migration",
				"pvcName", pvcName,
				"oldStorageClass", oldSC,
				"newStorageClass", state.VCTMigration.ExpectedStorageClasses[vctName])

			if err := c.Delete(ctx, pvc); err != nil {
				if k8serrors.IsNotFound(err) {
					// Already deleted, continue
					continue
				}
				// PVC deletion failed - log warning but don't block pod deletion
				// This handles cases like finalizers preventing deletion
				scopedLog.Info("Warning: Failed to delete PVC, continuing with pod deletion",
					"pvcName", pvcName, "error", err.Error())
				if eventPublisher != nil {
					eventPublisher.Warning(ctx, "PVCDeletionFailed",
						fmt.Sprintf("Failed to delete PVC %s: %v. Pod deletion will proceed.",
							pvcName, err))
				}
				continue
			}

			if eventPublisher != nil {
				eventPublisher.Normal(ctx, "PVCDeleted",
					fmt.Sprintf("Deleted PVC %s for storage class migration (old: %s, new: %s)",
						pvcName, oldSC, state.VCTMigration.ExpectedStorageClasses[vctName]))
			}
		}
	}

	// Step 3: Delete pod (StatefulSet controller will recreate with new spec)
	// New PVCs will be created with new storage class from updated VCT
	scopedLog.Info("Deleting pod for unified transition")

	preconditions := client.Preconditions{
		UID:             &pod.ObjectMeta.UID,
		ResourceVersion: &pod.ObjectMeta.ResourceVersion,
	}
	if err := c.Delete(ctx, pod, preconditions); err != nil && !k8serrors.IsNotFound(err) {
		scopedLog.Error(err, "Failed to delete pod")
		// Return error so caller can track this failure
		return fmt.Errorf("failed to delete pod %s: %w", pod.GetName(), err)
	}

	if eventPublisher != nil {
		msg := fmt.Sprintf("Deleted pod %s for unified transition", pod.GetName())
		if state.CPUChange != nil {
			msg += fmt.Sprintf(" (CPU: %dm->%dm)",
				state.CPUChange.OriginalCPUMillis, state.CPUChange.TargetCPUMillis)
		}
		if state.VCTMigration != nil {
			msg += fmt.Sprintf(" (VCT: %d storage classes)", len(state.VCTMigration.ExpectedStorageClasses))
		}
		eventPublisher.Normal(ctx, "PodRecycled", msg)
	}

	return nil
}

// canRecyclePodWithinCPUFloor checks if recycling a pod would violate the CPU floor constraint.
// The CPU floor ensures that total ready CPU never drops below the minimum required to maintain
// capacity during transitions.
//
// Returns true if:
// - state.CPUChange is nil (no CPU transition active, no floor constraint)
// - Recycling the pod would not drop total ready CPU below the floor
//
// Returns false if recycling would violate the CPU floor constraint.
func canRecyclePodWithinCPUFloor(
	ctx context.Context,
	c splcommon.ControllerClient,
	statefulSet *appsv1.StatefulSet,
	pod *corev1.Pod,
	state *UnifiedTransitionState,
	parallelUpdates int32,
) bool {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("canRecyclePodWithinCPUFloor").WithValues(
		"podName", pod.GetName(),
		"namespace", pod.GetNamespace())

	// If no CPU change is active, there's no CPU floor constraint
	if state.CPUChange == nil {
		scopedLog.Info("No CPU change active, recycling allowed")
		return true
	}

	// Calculate CPU floor: originalCPUMillis * originalReplicas - buffer for parallel updates
	cpuFloor := state.CPUChange.OriginalCPUMillis * int64(state.CPUChange.OriginalReplicas)
	cpuBuffer := int64(parallelUpdates) * state.CPUChange.OriginalCPUMillis
	minCPUFloor := cpuFloor - cpuBuffer

	// Compute current total ready CPU
	cpuState := CPUAwareTransitionState{
		OriginalCPUMillis: state.CPUChange.OriginalCPUMillis,
		TargetCPUMillis:   state.CPUChange.TargetCPUMillis,
		OriginalReplicas:  state.CPUChange.OriginalReplicas,
		TargetReplicas:    state.CPUChange.TargetReplicas,
	}
	metrics, err := computeReadyCPUMetricsForScaleDown(ctx, c, statefulSet, cpuState)
	if err != nil {
		scopedLog.Error(err, "Failed to compute CPU metrics, disallowing recycle")
		return false
	}

	// Calculate CPU after recycling this pod
	podCPU := extractCPUFromPod(pod)
	cpuAfterRecycle := metrics.TotalReadyCPU - podCPU

	scopedLog.Info("Checking CPU floor constraint",
		"totalReadyCPU", metrics.TotalReadyCPU,
		"podCPU", podCPU,
		"cpuAfterRecycle", cpuAfterRecycle,
		"minCPUFloor", minCPUFloor)

	if cpuAfterRecycle < minCPUFloor {
		scopedLog.Info("Recycling would violate CPU floor",
			"deficit", minCPUFloor-cpuAfterRecycle)
		return false
	}

	return true
}
