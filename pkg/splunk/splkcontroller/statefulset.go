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
	"fmt"
	"math"
	"reflect"
	"strconv"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// ScaleUpReadyWaitTimeoutAnnotation is the user-facing annotation that allows users to configure
	// the timeout for waiting for pods to become ready during scale-up operations.
	// This annotation can be set on the CR and will propagate to the StatefulSet.
	// Expected format: duration string (e.g., "10m", "5m30s", "0s")
	// Default behavior (if not set): wait indefinitely for pods to become ready
	// Setting to "0s" will skip waiting and proceed immediately with scale-up
	ScaleUpReadyWaitTimeoutAnnotation = "operator.splunk.com/scale-up-ready-wait-timeout"

	// ScaleUpWaitStartedAnnotation is an internal annotation used by the operator to track
	// when the waiting period for pod readiness started during scale-up operations.
	// This annotation is automatically managed by the operator and should not be set manually.
	// Expected format: RFC3339 timestamp (e.g., "2006-01-02T15:04:05Z07:00")
	ScaleUpWaitStartedAnnotation = "operator.splunk.com/scale-up-wait-started"

	// PreserveTotalCPUAnnotation is the annotation key to enable CPU-preserving scaling.
	// When set on a StatefulSet, this annotation enables the operator to automatically
	// adjust replicas to maintain the same total CPU allocation when CPU requests per pod change.
	// Example: If set to "true" and pods scale from 4x2CPU to 8x1CPU, total CPU (8) is preserved.
	// This is useful for license-based or cost-optimized deployments where total resource
	// allocation should remain constant regardless of individual pod sizing.
	PreserveTotalCPUAnnotation = "operator.splunk.com/preserve-total-cpu"

	// ParallelPodUpdatesAnnotation is the annotation key to specify the number of pods that can be updated in parallel.
	// When set on a StatefulSet, this annotation controls how many pods can be deleted/recycled simultaneously
	// during rolling updates. This can significantly speed up large cluster updates.
	//
	// The annotation accepts either:
	//   - A floating-point value <= 1.0: Interpreted as a percentage of total replicas
	//     Example: "0.25" means 25% of pods can be updated in parallel
	//   - A value > 1.0: Interpreted as an absolute number of pods
	//     Example: "3" allows up to 3 pods to be updated at once
	//
	// If the annotation is missing or invalid, the default value of 1 is used (sequential updates).
	// Valid range: 1 to total number of replicas. Values outside this range are clamped.
	ParallelPodUpdatesAnnotation = "operator.splunk.com/parallel-pod-updates"

	// CPUAwareTargetReplicasAnnotation stores the target replica count during CPU-aware scale-down.
	// This annotation is automatically managed by the operator and should not be set manually.
	CPUAwareTargetReplicasAnnotation = "operator.splunk.com/cpu-aware-target-replicas"

	// DefaultParallelPodUpdates is the default number of pods to update in parallel when the annotation is not set.
	DefaultParallelPodUpdates = 1
)

// ScalingCPUMetrics tracks CPU allocation across old and new spec pods during transitions
type ScalingCPUMetrics struct {
	TotalReadyCPU     int64 // Total CPU of all ready pods
	NewSpecReadyPods  int32 // Number of ready pods with new spec
	NewSpecReadyCPU   int64 // Total CPU of ready pods with new spec
	OldSpecReadyPods  int32 // Number of ready pods with old spec
	OldSpecReadyCPU   int64 // Total CPU of ready pods with old spec
	OriginalTotalCPU  int64 // Original total CPU before transition
	TargetTotalCPU    int64 // Target total CPU after transition
	TargetCPUPerPod   int64 // CPU per pod in target spec
	OriginalCPUPerPod int64 // CPU per pod in original spec
}

// VCTStorageChange represents a storage change for a volume claim template
type VCTStorageChange struct {
	TemplateName string
	OldSize      resource.Quantity
	NewSize      resource.Quantity
}

// VCTCompareResult holds the result of comparing volume claim templates
type VCTCompareResult struct {
	RequiresRecreate  bool               // True if StatefulSet needs to be recreated
	StorageExpansions []VCTStorageChange // Storage expansions that can be done in-place
	RecreateReason    string             // Reason why recreation is needed
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

		// Check storage class change (requires recreate)
		currentSC := ""
		if currentVCT.Spec.StorageClassName != nil {
			currentSC = *currentVCT.Spec.StorageClassName
		}
		revisedSC := ""
		if revisedVCT.Spec.StorageClassName != nil {
			revisedSC = *revisedVCT.Spec.StorageClassName
		}
		if currentSC != revisedSC {
			result.RequiresRecreate = true
			result.RecreateReason = fmt.Sprintf("StorageClassName changed for VolumeClaimTemplate '%s' from '%s' to '%s'", name, currentSC, revisedSC)
			return result
		}

		// Check access modes change (requires recreate)
		if !reflect.DeepEqual(currentVCT.Spec.AccessModes, revisedVCT.Spec.AccessModes) {
			result.RequiresRecreate = true
			result.RecreateReason = fmt.Sprintf("AccessModes changed for VolumeClaimTemplate '%s'", name)
			return result
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

// isPreserveTotalCPUEnabled checks if the CPU-preserving scaling annotation is enabled on the StatefulSet.
func isPreserveTotalCPUEnabled(statefulSet *appsv1.StatefulSet) bool {
	if statefulSet.Annotations == nil {
		return false
	}
	value, exists := statefulSet.Annotations[PreserveTotalCPUAnnotation]
	return exists && value == "true"
}

// getCPURequest extracts the CPU request from a pod template spec.
// Returns the CPU millicores (e.g., "2" CPU = 2000 millicores) or 0 if not found.
func getCPURequest(podSpec *corev1.PodSpec) int64 {
	if podSpec == nil || len(podSpec.Containers) == 0 {
		return 0
	}
	// Use the first container's CPU request as the reference
	cpuRequest := podSpec.Containers[0].Resources.Requests[corev1.ResourceCPU]
	return cpuRequest.MilliValue()
}

// calculateAdjustedReplicas calculates the new replica count to maintain total CPU when per-pod CPU changes.
// Formula: newReplicas = (currentReplicas * currentCPUPerPod) / newCPUPerPod
// Returns the adjusted replica count, rounded up to ensure we don't under-provision.
func calculateAdjustedReplicas(currentReplicas int32, currentCPUPerPod, newCPUPerPod int64) int32 {
	if newCPUPerPod == 0 {
		return currentReplicas // Avoid division by zero
	}
	totalCPU := currentReplicas * int32(currentCPUPerPod)
	adjustedReplicas := (totalCPU + int32(newCPUPerPod) - 1) / int32(newCPUPerPod) // Ceiling division
	if adjustedReplicas < 1 {
		return 1 // Ensure at least 1 replica
	}
	return adjustedReplicas
}

// getParallelPodUpdates extracts and validates the parallel pod updates setting from StatefulSet annotations.
// Returns the number of pods that can be updated in parallel during rolling updates.
//
// The annotation accepts either:
//   - A floating-point value < 1.0: Interpreted as a percentage of total replicas
//     Example: "0.25" means 25% of pods can be updated in parallel
//   - A value >= 1.0: Interpreted as an absolute number of pods
//     Example: "3" or "3.0" allows up to 3 pods to be updated at once
//
// If the annotation is missing, invalid, or out of range, returns DefaultParallelPodUpdates (1).
// The returned value is clamped between 1 and the total number of replicas.
func getParallelPodUpdates(statefulSet *appsv1.StatefulSet) int32 {
	if statefulSet.Annotations == nil {
		return DefaultParallelPodUpdates
	}

	value, exists := statefulSet.Annotations[ParallelPodUpdatesAnnotation]
	if !exists || value == "" {
		return DefaultParallelPodUpdates
	}

	// Parse the annotation value as float64
	floatValue, err := strconv.ParseFloat(value, 64)
	if err != nil || floatValue <= 0 {
		return DefaultParallelPodUpdates
	}

	var parallelUpdates int32
	totalReplicas := int32(1)
	if statefulSet.Spec.Replicas != nil {
		totalReplicas = *statefulSet.Spec.Replicas
	}

	if floatValue < 1.0 {
		// Percentage mode: value is a fraction of total replicas
		// e.g., 0.25 means 25% of replicas
		calculated := float64(totalReplicas) * floatValue
		parallelUpdates = int32(math.Ceil(calculated))
	} else {
		// Absolute mode: value is the exact number of pods
		// e.g., 1.0, 2.5, 3 all treated as absolute values
		parallelUpdates = int32(math.Round(floatValue))
	}

	// Clamp to reasonable bounds: at least 1, at most total replicas
	if parallelUpdates < 1 {
		return 1
	}
	if parallelUpdates > totalReplicas {
		return totalReplicas
	}

	return parallelUpdates
}

// isPodReady checks if a pod is in Ready condition
func isPodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// extractCPUFromPod extracts CPU millicores from a running pod
func extractCPUFromPod(pod *corev1.Pod) int64 {
	if len(pod.Spec.Containers) == 0 {
		return 0
	}
	// Use first container's CPU request
	cpuRequest := pod.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU]
	return cpuRequest.MilliValue()
}

// hasNewSpec checks if a pod has the new spec (compares CPU)
func hasNewSpec(pod *corev1.Pod, targetCPU int64) bool {
	podCPU := extractCPUFromPod(pod)
	return podCPU == targetCPU
}

// computeCPUMetrics calculates CPU metrics for all ready pods
func computeCPUMetrics(
	ctx context.Context,
	c splcommon.ControllerClient,
	statefulSet *appsv1.StatefulSet,
	targetReplicas int32,
) (ScalingCPUMetrics, error) {
	scopedLog := log.FromContext(ctx)
	logger := scopedLog.WithName("computeCPUMetrics").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	metrics := ScalingCPUMetrics{}

	// Get target CPU from statefulSet spec (after merge)
	metrics.TargetCPUPerPod = getCPURequest(&statefulSet.Spec.Template.Spec)

	// Get original CPU - need to detect it from existing pods
	metrics.OriginalCPUPerPod = metrics.TargetCPUPerPod // Default assumption

	// List all pods for this StatefulSet
	selector, err := metav1.LabelSelectorAsSelector(statefulSet.Spec.Selector)
	if err != nil {
		return metrics, err
	}

	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(statefulSet.GetNamespace()),
		client.MatchingLabelsSelector{Selector: selector},
	}

	err = c.List(ctx, podList, listOpts...)
	if err != nil {
		return metrics, err
	}

	// Track first old-spec CPU we find
	firstOldCPU := int64(0)

	for i := range podList.Items {
		pod := &podList.Items[i]

		if !isPodReady(pod) {
			continue
		}

		podCPU := extractCPUFromPod(pod)
		metrics.TotalReadyCPU += podCPU

		// Check if pod has new spec
		if hasNewSpec(pod, metrics.TargetCPUPerPod) {
			metrics.NewSpecReadyPods++
			metrics.NewSpecReadyCPU += podCPU
		} else {
			metrics.OldSpecReadyPods++
			metrics.OldSpecReadyCPU += podCPU
			if firstOldCPU == 0 {
				firstOldCPU = podCPU
			}
		}
	}

	// Set original CPU per pod from first old pod we found
	if firstOldCPU > 0 {
		metrics.OriginalCPUPerPod = firstOldCPU
	}

	// Calculate totals
	currentReplicas := *statefulSet.Spec.Replicas
	metrics.OriginalTotalCPU = int64(currentReplicas) * metrics.OriginalCPUPerPod
	metrics.TargetTotalCPU = int64(targetReplicas) * metrics.TargetCPUPerPod

	logger.Info("Computed CPU metrics",
		"totalReadyCPU", metrics.TotalReadyCPU,
		"newSpecPods", metrics.NewSpecReadyPods,
		"newSpecCPU", metrics.NewSpecReadyCPU,
		"oldSpecPods", metrics.OldSpecReadyPods,
		"oldSpecCPU", metrics.OldSpecReadyCPU,
		"originalCPUPerPod", metrics.OriginalCPUPerPod,
		"targetCPUPerPod", metrics.TargetCPUPerPod)

	return metrics, nil
}

// DefaultStatefulSetPodManager is a simple StatefulSetPodManager that does nothing
type DefaultStatefulSetPodManager struct{}

// Update for DefaultStatefulSetPodManager handles all updates for a statefulset of standard pods.
// If CPU-preserving scaling is enabled (PreserveTotalCPUAnnotation="true"), this function will adjust
// desiredReplicas to maintain total CPU allocation when per-pod CPU requests change.
func (mgr *DefaultStatefulSetPodManager) Update(ctx context.Context, client splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (enterpriseApi.Phase, error) {
	// Get eventPublisher from context
	var eventPublisher splcommon.K8EventPublisher
	if ep := ctx.Value(splcommon.EventPublisherKey); ep != nil {
		eventPublisher = ep.(splcommon.K8EventPublisher)
	}

	phase, err := ApplyStatefulSet(ctx, client, statefulSet, eventPublisher)

	// If CPU scaling is enabled and ApplyStatefulSet modified replicas due to CPU changes,
	// use the new calculated replicas instead of the original desiredReplicas
	if isPreserveTotalCPUEnabled(statefulSet) {
		desiredReplicas = *statefulSet.Spec.Replicas
	}

	if err == nil && phase == enterpriseApi.PhaseReady {
		phase, err = UpdateStatefulSetPods(ctx, client, statefulSet, mgr, desiredReplicas)
	}
	return phase, err
}

// PrepareScaleDown for DefaultStatefulSetPodManager does nothing and returns true
func (mgr *DefaultStatefulSetPodManager) PrepareScaleDown(ctx context.Context, n int32) (bool, error) {
	return true, nil
}

// PrepareRecycle for DefaultStatefulSetPodManager does nothing and returns true
func (mgr *DefaultStatefulSetPodManager) PrepareRecycle(ctx context.Context, n int32) (bool, error) {
	return true, nil
}

// FinishRecycle for DefaultStatefulSetPodManager does nothing and returns false
func (mgr *DefaultStatefulSetPodManager) FinishRecycle(ctx context.Context, n int32) (bool, error) {
	return true, nil
}

func (mgr *DefaultStatefulSetPodManager) FinishUpgrade(ctx context.Context, n int32) error {
	return nil
}

// ApplyStatefulSet creates or updates a Kubernetes StatefulSet
// It intelligently handles different types of changes:
// - VolumeClaimTemplate changes: Delete + Recreate with orphan cascade (preserves pods and PVCs)
// - Label/Annotation changes: In-place update
// - Pod template changes: In-place update
// - No changes: No operation
func ApplyStatefulSet(ctx context.Context, c splcommon.ControllerClient, revised *appsv1.StatefulSet, eventPublisher splcommon.K8EventPublisher) (enterpriseApi.Phase, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyStatefulSet").WithValues(
		"name", revised.GetObjectMeta().GetName(),
		"namespace", revised.GetObjectMeta().GetNamespace())

	namespacedName := types.NamespacedName{Namespace: revised.GetNamespace(), Name: revised.GetName()}
	var current appsv1.StatefulSet

	err := c.Get(ctx, namespacedName, &current)
	if err != nil {
		// In every reconcile, the statefulSet spec created by the operator is compared
		// against the one stored in etcd. While comparing the two specs, for the fields
		// represented by slices(ports, volume mounts etc..) the order of the elements is
		// important i.e any change in order followed by an update of statefulSet will cause
		// a change in the UpdatedRevision field in the StatefulSpec. This inturn triggers
		// a pod recycle unnecessarily. To avoid the same, sort the slices during the
		// statefulSet creation.
		// Note: During the update scenario below, MergePodUpdates takes care of sorting.
		SortStatefulSetSlices(ctx, &revised.Spec.Template.Spec, revised.GetObjectMeta().GetName())

		// no StatefulSet exists -> just create a new one
		err = splutil.CreateResource(ctx, c, revised)
		return enterpriseApi.PhasePending, err
	}

	// found an existing StatefulSet

	// Save original CPU value before MergePodUpdates modifies current
	originalCPU := getCPURequest(&current.Spec.Template.Spec)
	originalReplicas := *current.Spec.Replicas

	// check for changes in Pod template
	hasUpdates := MergePodUpdates(ctx, &current.Spec.Template, &revised.Spec.Template, current.GetObjectMeta().GetName())

	// Compare VolumeClaimTemplates to detect changes
	vctResult := CompareVolumeClaimTemplates(&current, revised)

	if vctResult.RequiresRecreate {
		// VCT changes require StatefulSet recreation (delete and recreate)
		scopedLog.Info("VolumeClaimTemplate changes require StatefulSet recreation",
			"reason", vctResult.RecreateReason)
		if eventPublisher != nil {
			eventPublisher.Warning(ctx, "VCTRecreateRequired", fmt.Sprintf("StatefulSet will be recreated: %s", vctResult.RecreateReason))
		}

		// Delete the existing StatefulSet with orphan propagation (keeps pods and PVCs)
		err = splutil.DeleteResource(ctx, c, &current, client.PropagationPolicy(metav1.DeletePropagationOrphan))
		if err != nil {
			scopedLog.Error(err, "Failed to delete StatefulSet for VCT update")
			return enterpriseApi.PhaseError, err
		}

		scopedLog.Info("Deleted StatefulSet for VCT recreation, will recreate on next reconcile")
		if eventPublisher != nil {
			eventPublisher.Normal(ctx, "VCTRecreateInProgress", "StatefulSet deleted for VCT update, will recreate on next reconcile")
		}

		// Return to trigger reconcile which will recreate the StatefulSet
		return enterpriseApi.PhasePending, nil
	}

	// Handle storage expansions if any
	if len(vctResult.StorageExpansions) > 0 {
		scopedLog.Info("Storage expansions detected, attempting PVC expansion",
			"expansions", len(vctResult.StorageExpansions))

		err = ExpandPVCStorage(ctx, c, &current, vctResult.StorageExpansions, eventPublisher)
		if err != nil {
			scopedLog.Error(err, "Failed to expand PVC storage")
			// Don't fail the reconcile, log the error and continue
			// The storage expansion might fail due to storage class not supporting expansion
		}
	}

	*revised = current // caller expects that object passed represents latest state

	// Apply CPU-aware scaling adjustments AFTER copying current to revised
	// Note: MergePodUpdates already merged the new template into current, so current now has the NEW CPU value
	// We compare the original CPU (before merge) with the new CPU (after merge) to detect changes
	if isPreserveTotalCPUEnabled(revised) {
		newCPU := getCPURequest(&revised.Spec.Template.Spec) // revised now has the merged template from current

		// Only adjust replicas if CPU request actually changed and both are non-zero
		if originalCPU > 0 && newCPU > 0 && originalCPU != newCPU {
			adjustedReplicas := calculateAdjustedReplicas(originalReplicas, originalCPU, newCPU)

			if adjustedReplicas != originalReplicas {
				scopedLog.Info("CPU-aware scaling detected",
					"originalReplicas", originalReplicas,
					"originalCPU", originalCPU,
					"newCPU", newCPU,
					"targetReplicas", adjustedReplicas,
					"currentTotalCPU", originalReplicas*int32(originalCPU))

				// For scale-down (fewer replicas), do NOT update replicas here
				// Let UpdateStatefulSetPods handle gradual scale-down with CPU bounds
				// For scale-up (more replicas), safe to increase immediately
				if adjustedReplicas > originalReplicas {
					scopedLog.Info("CPU-aware scale-up: increasing replicas immediately",
						"from", originalReplicas, "to", adjustedReplicas)
					revised.Spec.Replicas = &adjustedReplicas
					hasUpdates = true
				} else {
					scopedLog.Info("CPU-aware scale-down: will handle gradually with CPU bounds",
						"currentReplicas", originalReplicas, "targetReplicas", adjustedReplicas)
					// Keep current replicas, will be reduced gradually
					// Store target in annotation for UpdateStatefulSetPods to use
					if revised.Annotations == nil {
						revised.Annotations = make(map[string]string)
					}
					revised.Annotations[CPUAwareTargetReplicasAnnotation] = fmt.Sprintf("%d", adjustedReplicas)
					hasUpdates = true
				}
			}
		}
	}

	if hasUpdates {
		// only update if there are material differences, as determined by comparison function
		// this updates the desired state template, but doesn't actually modify any pods
		// because we use an "OnUpdate" strategy https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#update-strategies
		// note also that this ignores Replicas, which is handled below by UpdateStatefulSetPods

		err = splutil.UpdateResource(ctx, c, revised)
		if err != nil {
			return enterpriseApi.PhaseUpdating, err
		}
		// always pass the latest resource back to caller
		err = c.Get(ctx, namespacedName, revised)
		return enterpriseApi.PhaseUpdating, err
	}

	// scaling and pod updates are handled by UpdateStatefulSetPods
	return enterpriseApi.PhaseReady, nil
}

// handleScaleDown manages the scale-down operation for a StatefulSet by safely removing pods.
//
// The function handles scale-down through a careful sequence of steps:
//  1. Identifies the highest-numbered pod to remove (following StatefulSet ordering conventions)
//  2. Calls mgr.PrepareScaleDown to initiate cleanup, regardless of pod state
//     (The pod manager implementation decides what cleanup is needed based on actual pod state)
//  3. Waits for PrepareScaleDown to complete before proceeding with pod termination
//  4. Updates the StatefulSet replica count to terminate the pod
//  5. Deletes associated PVCs to ensure clean state for potential future scale-ups
//
// This approach is designed to ensure proper cleanup in all scenarios, including edge cases (but do happen in practice) where:
// - Pods are deleted manually outside of the operator
// - Pods are in unexpected or transitional states
// - The Cluster Manager still has references to peers that no longer exist
//
// This function returns PhaseScalingDown when operation is in progress, PhaseError on failure,
// and throws error if there is any error encountered during the scale-down process
func handleScaleDown(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, mgr splcommon.StatefulSetPodManager, replicas int32, desiredReplicas int32) (enterpriseApi.Phase, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("handleScaleDown").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"desiredReplicas", desiredReplicas,
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	// prepare pod for removal via scale down (highest-numbered pod per StatefulSet convention)
	n := replicas - 1
	podName := fmt.Sprintf("%s-%d", statefulSet.GetName(), n)

	// always call PrepareScaleDown to ensure proper cleanup regardless of pod state.
	// This handles edge cases where pods are deleted manually or in unexpected states,
	// preventing zombie peers in the Cluster Manager. The pod manager implementation
	// will decide if actual cleanup is needed based on the pod's current state.
	ready, err := mgr.PrepareScaleDown(ctx, n)
	if err != nil {
		scopedLog.Error(err, "Unable to prepare Pod for scale down", "podName", podName)
		return enterpriseApi.PhaseError, err
	}
	if !ready {
		// wait until pod preparation has completed before deleting it
		return enterpriseApi.PhaseScalingDown, nil
	}

	// scale down statefulset to terminate pod
	scopedLog.Info("Scaling replicas down", "replicas", n)
	*statefulSet.Spec.Replicas = n
	err = splutil.UpdateResource(ctx, c, statefulSet)
	if err != nil {
		scopedLog.Error(err, "Scale down update failed for StatefulSet")
		return enterpriseApi.PhaseError, err
	}

	// delete PVCs used by the pod so that a future scale up will have clean state
	for _, vol := range statefulSet.Spec.VolumeClaimTemplates {
		// VolumeClaimTemplate's namespace is typically empty (inherits from StatefulSet),
		// so we need to fall back to the StatefulSet's namespace when building PVC names
		pvcNamespace := vol.ObjectMeta.Namespace
		if pvcNamespace == "" {
			pvcNamespace = statefulSet.GetNamespace()
		}
		namespacedName := types.NamespacedName{
			Namespace: pvcNamespace,
			Name:      fmt.Sprintf("%s-%s", vol.ObjectMeta.Name, podName),
		}
		var pvc corev1.PersistentVolumeClaim
		err := c.Get(ctx, namespacedName, &pvc)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				// PVC doesn't exist, nothing to delete
				scopedLog.Info("PVC not found, skipping deletion", "pvcName", namespacedName.Name)
				continue
			}
			scopedLog.Error(err, "Unable to find PVC for deletion", "pvcName", namespacedName.Name)
			return enterpriseApi.PhaseError, err
		}
		scopedLog.Info("Deleting PVC", "pvcName", pvc.ObjectMeta.Name)
		err = c.Delete(ctx, &pvc)
		if err != nil {
			scopedLog.Error(err, "Unable to delete PVC", "pvcName", pvc.ObjectMeta.Name)
			return enterpriseApi.PhaseError, err
		}
	}

	return enterpriseApi.PhaseScalingDown, nil
}

// handleScaleUp manages the scale-up operation for a StatefulSet
//
// This function also implements a configurable timeout mechanism that allows users to control
// how long the operator waits for existing pods to become ready before scaling up.
// The timeout can be configured via the ScaleUpReadyWaitTimeoutAnnotation on the CR/StatefulSet.
//
// Behavior:
//   - Early return if no scale-up is needed (readyReplicas >= desiredReplicas)
//   - Waits for all current pods to be ready before scaling up (if readyReplicas < replicas)
//     Respects configurable timeout using getScaleUpReadyWaitTimeout()
//   - Tracks wait start time using setScaleUpWaitStarted() to enable timeout calculation
//   - Setting timeout to 0 bypasses the wait entirely and proceeds immediately
//   - Proceeds with scale-up after timeout expires even if not all pods are ready
//   - Clears wait timestamp after successful scale-up via clearScaleUpWaitStarted()
//
// The timeout mechanism prevents indefinite waiting when pods fail to become ready,
// allowing the operator to make forward progress while maintaining the principle of
// waiting for stability during normal operations.
//
// This function returns PhasePending when waiting for initial pods, PhaseScalingUp when actively scaling,
// and throws error if there is any error encountered during the scale-up process
func handleScaleUp(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, replicas int32, readyReplicas int32, desiredReplicas int32) (enterpriseApi.Phase, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("handleScaleUp").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"desiredReplicas", desiredReplicas,
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	if readyReplicas >= desiredReplicas {
		// No scale-up needed
		return enterpriseApi.PhaseReady, nil
	}

	// Before scaling up, wait for all current pods to be ready
	if readyReplicas < replicas {
		// Get the configured timeout for waiting
		timeout := getScaleUpReadyWaitTimeout(statefulSet)

		// If timeout is negative, wait forever (no timeout bypass)
		if timeout < 0 {
			// Check if we have a wait start time (used to log once per scale-up)
			_, hasStartTime := getScaleUpWaitStarted(statefulSet)

			if !hasStartTime {
				// First time waiting, record the start time and log informative message
				scopedLog.Info("Waiting for all pods to become ready before scaling up (no timeout configured). Set annotation 'operator.splunk.com/scale-up-ready-wait-timeout' to proceed with scale-up after a specified duration.")
				err := setScaleUpWaitStarted(ctx, c, statefulSet)
				if err != nil {
					scopedLog.Error(err, "Failed to set scale-up wait start time")
					return enterpriseApi.PhaseError, err
				}
			}
			// Continue waiting indefinitely
			if readyReplicas > 0 {
				return enterpriseApi.PhaseScalingUp, nil
			}
			return enterpriseApi.PhasePending, nil
		}

		// If timeout is 0, bypass the wait and proceed immediately with scale-up
		if timeout == 0 {
			scopedLog.Info("Timeout set to 0, bypassing wait for pods to be ready")
			// Jump to scale-up logic below
		} else {
			// Check if we have a wait start time
			startTime, hasStartTime := getScaleUpWaitStarted(statefulSet)

			if !hasStartTime {
				// First time waiting, record the start time
				scopedLog.Info("Starting to wait for pods to become ready before scaling up")
				err := setScaleUpWaitStarted(ctx, c, statefulSet)
				if err != nil {
					scopedLog.Error(err, "Failed to set scale-up wait start time")
					return enterpriseApi.PhaseError, err
				}
				// Return to continue waiting in next reconcile
				if readyReplicas > 0 {
					return enterpriseApi.PhaseScalingUp, nil
				}
				return enterpriseApi.PhasePending, nil
			}

			// We have a start time, check if timeout has been exceeded
			elapsed := time.Since(startTime)
			if elapsed > timeout {
				// Timeout exceeded, proceed with scale-up despite not all pods being ready
				notReadyCount := replicas - readyReplicas
				scopedLog.Info("Proceeding with scale-up after timeout",
					"timeout", timeout,
					"elapsed", elapsed,
					"notReadyCount", notReadyCount)
				// Jump to scale-up logic below
			} else {
				// Still within timeout window, continue waiting
				scopedLog.Info("Waiting for pods to become ready before scaling up",
					"timeout", timeout,
					"elapsed", elapsed,
					"readyReplicas", readyReplicas,
					"replicas", replicas)
				if readyReplicas > 0 {
					return enterpriseApi.PhaseScalingUp, nil
				}
				return enterpriseApi.PhasePending, nil
			}
		}
	}
	// All current pods are ready (or timeout exceeded), proceed with scale up
	scopedLog.Info("Scaling replicas up", "replicas", desiredReplicas)
	*statefulSet.Spec.Replicas = desiredReplicas
	err := splutil.UpdateResource(ctx, c, statefulSet)
	if err != nil {
		return enterpriseApi.PhaseScalingUp, err
	}
	// Clear the scale-up wait timestamp after successful scale-up
	// Return error to trigger requeue and prevent stale annotations
	if err := clearScaleUpWaitStarted(ctx, c, statefulSet); err != nil {
		scopedLog.Error(err, "Failed to clear scale-up wait timestamp")
		return enterpriseApi.PhaseScalingUp, err
	}
	return enterpriseApi.PhaseScalingUp, nil
}

// handleCPUPreservingTransition manages the gradual pod transition when CPU-preserving scaling is enabled.
// It ensures total CPU allocation stays above the original level while transitioning to pods with different CPU specs.
//
// The function handles the following scenarios:
// 1. Waits for at least one new-spec pod to become ready before deleting old pods
// 2. Calculates how many old pods can be safely deleted while maintaining CPU bounds
// 3. Properly decommissions pods via PrepareScaleDown before deletion
// 4. Updates StatefulSet replicas once all old pods are deleted
//
// Returns:
// - (phase, true, nil) if CPU-preserving transition is being handled (caller should return phase)
// - (PhaseReady, false, nil) if CPU-preserving transition is not applicable (caller should continue)
// - (PhaseError, true, error) if an error occurred
func handleCPUPreservingTransition(
	ctx context.Context,
	c splcommon.ControllerClient,
	statefulSet *appsv1.StatefulSet,
	mgr splcommon.StatefulSetPodManager,
	replicas int32,
	readyReplicas int32,
) (enterpriseApi.Phase, bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("handleCPUPreservingTransition").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	// Check if CPU-preserving scaling is enabled
	if !isPreserveTotalCPUEnabled(statefulSet) {
		return enterpriseApi.PhaseReady, false, nil
	}

	// Check for target replicas annotation
	targetReplicasStr := ""
	if statefulSet.Annotations != nil {
		targetReplicasStr = statefulSet.Annotations[CPUAwareTargetReplicasAnnotation]
	}
	if targetReplicasStr == "" {
		return enterpriseApi.PhaseReady, false, nil
	}

	targetReplicas, parseErr := strconv.ParseInt(targetReplicasStr, 10, 32)
	if parseErr != nil || int32(targetReplicas) >= replicas {
		return enterpriseApi.PhaseReady, false, nil
	}

	scopedLog.Info("CPU-aware scale-down active",
		"currentReplicas", replicas,
		"targetReplicas", targetReplicas,
		"readyReplicas", readyReplicas)

	// Compute CPU metrics
	metrics, metricsErr := computeCPUMetrics(ctx, c, statefulSet, int32(targetReplicas))
	if metricsErr != nil {
		scopedLog.Error(metricsErr, "Unable to compute CPU metrics")
		return enterpriseApi.PhaseError, true, metricsErr
	}

	// Need at least one new-spec pod ready before deleting old pods
	if metrics.NewSpecReadyPods == 0 {
		scopedLog.Info("Waiting for new-spec pods to become ready before CPU-aware scale-down",
			"oldSpecPods", metrics.OldSpecReadyPods)
		return enterpriseApi.PhaseUpdating, true, nil
	}

	// Calculate how many old pods can be safely deleted
	maxParallelUpdates := getParallelPodUpdates(statefulSet)
	oldPodsPerNewPod := int64(1)
	if metrics.OriginalCPUPerPod > 0 {
		oldPodsPerNewPod = metrics.TargetCPUPerPod / metrics.OriginalCPUPerPod
		if oldPodsPerNewPod > 1 {
			oldPodsPerNewPod-- // Safety margin
		}
		if oldPodsPerNewPod < 1 {
			oldPodsPerNewPod = 1
		}
	}

	maxBalancingDeletes := int32(maxParallelUpdates) * int32(oldPodsPerNewPod)

	scopedLog.Info("CPU-aware balancing calculation",
		"newSpecReadyPods", metrics.NewSpecReadyPods,
		"oldPodsPerNewPod", oldPodsPerNewPod,
		"maxBalancingDeletes", maxBalancingDeletes,
		"totalReadyCPU", metrics.TotalReadyCPU,
		"originalTotalCPU", metrics.OriginalTotalCPU)

	// Find old-spec pods to delete (from highest index downward)
	podsToDelete := []string{}
	cpuAfterDeletion := metrics.TotalReadyCPU

	for n := readyReplicas - 1; n >= int32(targetReplicas) && int32(len(podsToDelete)) < maxBalancingDeletes; n-- {
		podName := fmt.Sprintf("%s-%d", statefulSet.GetName(), n)
		podNamespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: podName}
		var pod corev1.Pod
		podErr := c.Get(ctx, podNamespacedName, &pod)
		if podErr != nil {
			continue
		}

		if !isPodReady(&pod) {
			continue
		}

		// Only delete old-spec pods
		if hasNewSpec(&pod, metrics.TargetCPUPerPod) {
			continue // Skip new-spec pods
		}

		podCPU := extractCPUFromPod(&pod)
		newTotalCPU := cpuAfterDeletion - podCPU

		// Ensure we stay above original CPU
		if newTotalCPU < metrics.OriginalTotalCPU {
			scopedLog.Info("CPU bounds reached, stopping balancing deletions",
				"currentCPU", cpuAfterDeletion,
				"wouldBe", newTotalCPU,
				"originalCPU", metrics.OriginalTotalCPU)
			break
		}

		podsToDelete = append(podsToDelete, podName)
		cpuAfterDeletion = newTotalCPU
	}

	// Delete the selected pods (with proper decommissioning)
	if len(podsToDelete) > 0 {
		scopedLog.Info("Executing CPU-aware balancing deletions",
			"podsToDelete", len(podsToDelete),
			"currentCPU", metrics.TotalReadyCPU,
			"afterDeletionCPU", cpuAfterDeletion,
			"originalCPU", metrics.OriginalTotalCPU)

		for _, podName := range podsToDelete {
			podNamespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: podName}
			var pod corev1.Pod
			podErr := c.Get(ctx, podNamespacedName, &pod)
			if podErr != nil {
				continue
			}

			// Extract pod index for PrepareScaleDown
			var podIndex int32
			fmt.Sscanf(podName, statefulSet.GetName()+"-%d", &podIndex)

			// Call PrepareScaleDown to properly decommission the pod
			// (drain data, remove from cluster manager, etc.)
			ready, prepErr := mgr.PrepareScaleDown(ctx, podIndex)
			if prepErr != nil {
				scopedLog.Error(prepErr, "Unable to prepare Pod for CPU-aware scale down", "podName", podName)
				return enterpriseApi.PhaseError, true, prepErr
			}
			if !ready {
				// Wait until pod preparation has completed before deleting it
				scopedLog.Info("Waiting for pod decommissioning before CPU-aware deletion", "podName", podName)
				return enterpriseApi.PhaseUpdating, true, nil
			}

			// Now safe to delete the pod
			preconditions := client.Preconditions{UID: &pod.ObjectMeta.UID, ResourceVersion: &pod.ObjectMeta.ResourceVersion}
			delErr := c.Delete(ctx, &pod, preconditions)
			if delErr != nil {
				scopedLog.Error(delErr, "Unable to delete Pod for balancing", "podName", podName)
			} else {
				scopedLog.Info("Deleted pod for CPU balancing", "podName", podName, "podCPU", extractCPUFromPod(&pod))
			}
		}

		return enterpriseApi.PhaseUpdating, true, nil
	}

	// All old pods deleted, update StatefulSet replicas to target
	if metrics.OldSpecReadyPods == 0 && replicas > int32(targetReplicas) {
		scopedLog.Info("All old pods deleted, updating StatefulSet replicas to target",
			"currentReplicas", replicas, "targetReplicas", targetReplicas)
		targetReplicasInt32 := int32(targetReplicas)
		statefulSet.Spec.Replicas = &targetReplicasInt32
		updateErr := splutil.UpdateResource(ctx, c, statefulSet)
		if updateErr != nil {
			scopedLog.Error(updateErr, "Unable to update StatefulSet replicas")
			return enterpriseApi.PhaseError, true, updateErr
		}

		// Remove the target replicas annotation
		delete(statefulSet.Annotations, CPUAwareTargetReplicasAnnotation)
		updateErr = splutil.UpdateResource(ctx, c, statefulSet)
		if updateErr != nil {
			scopedLog.Error(updateErr, "Unable to remove CPU-aware target annotation")
		}

		scopedLog.Info("CPU-aware scale-down complete", "finalReplicas", targetReplicas)
		return enterpriseApi.PhaseScalingDown, true, nil
	}

	// Continue to next reconcile for more deletions or final replica update
	return enterpriseApi.PhaseUpdating, true, nil
}

// UpdateStatefulSetPods manages scaling and config updates for StatefulSets.
// The function implements careful ordering of operations:
// 1. Prioritize scale-down operations (removes pods even if not all are ready)
// 2. Wait for current pods to be ready before scaling up (ensures stability), or bypass wait if timeout exceeded
// 3. Handle pod updates for revision changes after scaling is complete
// This ordering ensures stable operations and prevents cascading issues during scaling.
func UpdateStatefulSetPods(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, mgr splcommon.StatefulSetPodManager, desiredReplicas int32) (enterpriseApi.Phase, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("UpdateStatefulSetPods").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	replicas := *statefulSet.Spec.Replicas
	readyReplicas := statefulSet.Status.ReadyReplicas

	// Re-fetch the StatefulSet to ensure we have the latest status, especially UpdateRevision.
	// This addresses a race condition where the StatefulSet controller may not have updated
	// Status.UpdateRevision yet after a spec change was applied. Without this re-fetch,
	// we might incorrectly report PhaseReady when pods actually need to be recycled.
	namespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: statefulSet.GetName()}
	err := c.Get(ctx, namespacedName, statefulSet)
	if err != nil {
		scopedLog.Error(err, "Unable to re-fetch StatefulSet for latest status")
		return enterpriseApi.PhaseError, err
	}

	// Handle CPU-preserving transition if enabled
	if phase, handled, err := handleCPUPreservingTransition(ctx, c, statefulSet, mgr, replicas, readyReplicas); handled {
		return phase, err
	}

	// check for scaling down - prioritize scale-down operations
	// Check StatefulSet spec replicas (not readyReplicas) to handle cases where replicas > desiredReplicas but readyReplicas < desiredReplicas
	if replicas > desiredReplicas {
		return handleScaleDown(ctx, c, statefulSet, mgr, replicas, desiredReplicas)
	}

	// check for scaling up
	if readyReplicas < desiredReplicas {
		return handleScaleUp(ctx, c, statefulSet, replicas, readyReplicas, desiredReplicas)
	}

	// readyReplicas == desiredReplicas
	// wait for all replicas to be ready
	if readyReplicas < replicas {
		scopedLog.Info("Waiting for pods to become ready")
		if readyReplicas > 0 {
			return enterpriseApi.PhaseScalingUp, nil
		}
		return enterpriseApi.PhasePending, nil
	} else if readyReplicas > replicas {
		scopedLog.Info("Waiting for scale down to complete")
		return enterpriseApi.PhaseScalingDown, nil
	}

	// readyReplicas == replicas
	// readyReplicas == desiredReplicas
	// ready and no StatefulSet scaling is required

	// Clear the scale-up wait timestamp now that all pods are ready and scaling is complete
	// Return error to trigger requeue and prevent stale annotations
	if err := clearScaleUpWaitStarted(ctx, c, statefulSet); err != nil {
		scopedLog.Error(err, "Failed to clear scale-up wait timestamp")
		return enterpriseApi.PhaseReady, err
	}

	// check existing pods for desired updates
	phase, err := CheckStatefulSetPodsForUpdates(ctx, c, statefulSet, mgr, readyReplicas)
	if phase != enterpriseApi.PhaseReady || err != nil {
		return phase, err
	}

	// Remove unwanted owner references
	err = splutil.RemoveUnwantedSecrets(ctx, c, statefulSet.GetName(), statefulSet.GetNamespace())
	if err != nil {
		return enterpriseApi.PhaseReady, err
	}

	// all is good!
	scopedLog.Info("All pods are ready")

	// Finalize rolling upgrade process
	// It uses first pod to get a client
	err = mgr.FinishUpgrade(ctx, 0)
	if err != nil {
		scopedLog.Error(err, "Unable to finalize rolling upgrade process")
		return enterpriseApi.PhaseError, err
	}

	scopedLog.Info("Statefulset - Phase Ready")

	return enterpriseApi.PhaseReady, nil
}

// CheckStatefulSetPodsForUpdates checks existing pods for desired updates and handles recycling if needed.
// This function iterates through all pods in reverse order (highest index first) and:
// - Verifies each pod exists and is ready
// - Compares pod revision with StatefulSet UpdateRevision
// - Initiates controlled pod recycling (PrepareRecycle -> Delete -> FinishRecycle)
// - Supports parallel pod updates via annotation (default: 1 pod at a time)
// Returns PhaseUpdating while updates are in progress, PhaseReady when all pods are current.
func CheckStatefulSetPodsForUpdates(ctx context.Context,
	c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet,
	mgr splcommon.StatefulSetPodManager, readyReplicas int32,
) (enterpriseApi.Phase, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("CheckStatefulSetPodsForUpdates").WithValues("name", statefulSet.GetName(), "namespace", statefulSet.GetNamespace())

	// Get the maximum number of pods to update in parallel
	maxParallelUpdates := getParallelPodUpdates(statefulSet)
	podsDeletedThisCycle := int32(0)

	scopedLog.Info("Checking pods for updates", "maxParallelUpdates", maxParallelUpdates)

	for n := readyReplicas - 1; n >= 0; n-- {
		// get Pod
		podName := fmt.Sprintf("%s-%d", statefulSet.GetName(), n)
		namespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: podName}
		var pod corev1.Pod
		err := c.Get(ctx, namespacedName, &pod)
		if err != nil {
			scopedLog.Error(err, "Unable to find Pod", "podName", podName)
			return enterpriseApi.PhaseError, err
		}
		if pod.Status.Phase != corev1.PodRunning || len(pod.Status.ContainerStatuses) == 0 || !pod.Status.ContainerStatuses[0].Ready {
			scopedLog.Info("Waiting for Pod to become ready", "podName", podName)
			return enterpriseApi.PhaseUpdating, nil
		}

		// terminate pod if it has pending updates; k8s will start a new one with revised template
		if statefulSet.Status.UpdateRevision != "" && statefulSet.Status.UpdateRevision != pod.GetLabels()["controller-revision-hash"] {
			// pod needs to be updated; first, prepare it to be recycled
			ready, err := mgr.PrepareRecycle(ctx, n)
			if err != nil {
				scopedLog.Error(err, "Unable to prepare Pod for recycling", "podName", podName)
				return enterpriseApi.PhaseError, err
			}
			if !ready {
				// wait until pod quarantine has completed before deleting it
				return enterpriseApi.PhaseUpdating, nil
			}

			// deleting pod will cause StatefulSet controller to create a new one with revised template
			scopedLog.Info("Recycling Pod for updates", "podName", podName,
				"statefulSetRevision", statefulSet.Status.UpdateRevision,
				"podRevision", pod.GetLabels()["controller-revision-hash"])
			preconditions := client.Preconditions{UID: &pod.ObjectMeta.UID, ResourceVersion: &pod.ObjectMeta.ResourceVersion}
			err = c.Delete(ctx, &pod, preconditions)
			if err != nil {
				scopedLog.Error(err, "Unable to delete Pod", "podName", podName)
				return enterpriseApi.PhaseError, err
			}

			// Track number of pods deleted in this cycle
			podsDeletedThisCycle++

			// Check if we've reached the parallel update limit
			if podsDeletedThisCycle >= maxParallelUpdates {
				scopedLog.Info("Reached parallel update limit, waiting for next reconcile",
					"podsDeleted", podsDeletedThisCycle,
					"maxParallel", maxParallelUpdates)
				return enterpriseApi.PhaseUpdating, nil
			}

			// Continue to next pod for parallel updates
			continue
		}

		// check if pod was previously prepared for recycling; if so, complete
		complete, err := mgr.FinishRecycle(ctx, n)
		if err != nil {
			scopedLog.Error(err, "Unable to complete recycling of pod", "podName", podName)
			return enterpriseApi.PhaseError, err
		}
		if !complete {
			// return and wait until next reconcile to let things settle down
			return enterpriseApi.PhaseUpdating, nil
		}
	}

	// If we deleted any pods this cycle, return PhaseUpdating to wait for them to be recreated
	if podsDeletedThisCycle > 0 {
		scopedLog.Info("Pods deleted this cycle, waiting for recreation",
			"podsDeleted", podsDeletedThisCycle)
		return enterpriseApi.PhaseUpdating, nil
	}

	// Phase 2: CPU-preserving balancing deletions (only if no update deletions occurred)
	// This handles deleting extra old-spec pods to reclaim CPU resources
	if isPreserveTotalCPUEnabled(statefulSet) {
		// Check if there's a target replicas annotation
		targetReplicasStr := ""
		if statefulSet.Annotations != nil {
			targetReplicasStr = statefulSet.Annotations[CPUAwareTargetReplicasAnnotation]
		}

		if targetReplicasStr != "" {
			targetReplicas, parseErr := strconv.ParseInt(targetReplicasStr, 10, 32)
			if parseErr == nil && int32(targetReplicas) < readyReplicas {
				// We have a target for scale-down, compute metrics
				metrics, compErr := computeCPUMetrics(ctx, c, statefulSet, int32(targetReplicas))
				if compErr == nil && metrics.NewSpecReadyPods > 0 {
					// Calculate balancing deletions
					oldPodsPerNewPod := int64(1)
					if metrics.OriginalCPUPerPod > 0 {
						oldPodsPerNewPod = metrics.TargetCPUPerPod / metrics.OriginalCPUPerPod
						if oldPodsPerNewPod > 1 {
							oldPodsPerNewPod-- // Safety margin
						}
						if oldPodsPerNewPod < 1 {
							oldPodsPerNewPod = 1
						}
					}

					maxBalancingDeletes := int32(maxParallelUpdates) * int32(oldPodsPerNewPod)

					scopedLog.Info("Checking for CPU-aware balancing deletions",
						"maxBalancingDeletes", maxBalancingDeletes,
						"oldSpecPods", metrics.OldSpecReadyPods,
						"newSpecPods", metrics.NewSpecReadyPods)

					// Find old-spec pods beyond target replicas to delete
					cpuAfterDeletion := metrics.TotalReadyCPU

					for n := readyReplicas - 1; n >= int32(targetReplicas); n-- {
						podName := fmt.Sprintf("%s-%d", statefulSet.GetName(), n)
						podNamespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: podName}
						var pod corev1.Pod
						podErr := c.Get(ctx, podNamespacedName, &pod)
						if podErr != nil {
							continue
						}

						if !isPodReady(&pod) {
							continue
						}

						// Only delete old-spec pods
						if hasNewSpec(&pod, metrics.TargetCPUPerPod) {
							continue
						}

						podCPU := extractCPUFromPod(&pod)
						newTotalCPU := cpuAfterDeletion - podCPU

						// Check CPU bounds
						if newTotalCPU < metrics.OriginalTotalCPU {
							scopedLog.Info("CPU bounds reached during balancing in CheckStatefulSetPodsForUpdates",
								"currentCPU", cpuAfterDeletion,
								"wouldBe", newTotalCPU,
								"minRequired", metrics.OriginalTotalCPU)
							break
						}

						// Call PrepareScaleDown to properly decommission the pod
						ready, prepErr := mgr.PrepareScaleDown(ctx, n)
						if prepErr != nil {
							scopedLog.Error(prepErr, "Unable to prepare Pod for CPU-aware scale down", "podName", podName)
							return enterpriseApi.PhaseError, prepErr
						}
						if !ready {
							// Wait until pod preparation has completed
							scopedLog.Info("Waiting for pod decommissioning before CPU-aware deletion", "podName", podName)
							return enterpriseApi.PhaseUpdating, nil
						}

						// Delete this pod
						scopedLog.Info("CPU-aware balancing deletion from CheckStatefulSetPodsForUpdates",
							"podName", podName, "podCPU", podCPU)
						preconditions := client.Preconditions{UID: &pod.ObjectMeta.UID, ResourceVersion: &pod.ObjectMeta.ResourceVersion}
						delErr := c.Delete(ctx, &pod, preconditions)
						if delErr != nil {
							scopedLog.Error(delErr, "Unable to delete Pod for balancing", "podName", podName)
							return enterpriseApi.PhaseError, delErr
						}

						cpuAfterDeletion = newTotalCPU

						// Only delete one pod at a time due to PrepareScaleDown requirement
						// (we need to wait for decommissioning to complete)
						scopedLog.Info("Balancing deletion completed, waiting for next reconcile",
							"podName", podName,
							"cpuBefore", metrics.TotalReadyCPU,
							"cpuAfter", cpuAfterDeletion)
						return enterpriseApi.PhaseUpdating, nil
					}
				}
			}
		}
	}

	return enterpriseApi.PhaseReady, nil
}

// getScaleUpReadyWaitTimeout parses the ScaleUpReadyWaitTimeoutAnnotation from the StatefulSet
// and returns the configured timeout duration. If the annotation is missing, invalid, or negative,
// it returns the default timeout of 10 minutes. A special case is "0s" or "0" which returns 0
// to indicate immediate bypass of the wait.
//
// Note that we also validate and timeout is within acceptable range (30s to 24h) to prevent:
// - Excessively short timeouts that could cause instability
// - Excessively long timeouts that could delay operations indefinitely
func getScaleUpReadyWaitTimeout(statefulSet *appsv1.StatefulSet) time.Duration {
	const (
		// defaultTimeout of -1 means "wait forever" - this is the original behavior
		// before the breaking change. A sentinel value of -1 indicates indefinite wait.
		defaultTimeout = time.Duration(-1)
		minTimeout     = 30 * time.Second
		maxTimeout     = 24 * time.Hour
	)

	if statefulSet.Annotations == nil {
		return defaultTimeout
	}

	timeoutStr, exists := statefulSet.Annotations[ScaleUpReadyWaitTimeoutAnnotation]
	if !exists {
		return defaultTimeout
	}

	// Parse the duration string
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		// Invalid format, return default (wait forever)
		return defaultTimeout
	}

	// Handle negative values - treat as "wait forever" (same as default)
	if timeout < 0 {
		return defaultTimeout
	}

	// Special case: 0 means immediate bypass - allow with warning
	if timeout == 0 {
		// Use a simple logger without context since this is a pure function
		// The caller will have appropriate context logging
		return timeout
	}

	// Validate timeout is within acceptable range
	if timeout < minTimeout {
		// Timeout too short - could cause instability, use default (wait forever)
		return defaultTimeout
	}

	if timeout > maxTimeout {
		// Timeout too long - cap at maxTimeout rather than waiting forever
		return maxTimeout
	}

	// Valid timeout in acceptable range
	return timeout
}

// getScaleUpWaitStarted retrieves and parses the ScaleUpWaitStartedAnnotation timestamp
// from the StatefulSet. Returns the parsed time and true if found and valid, otherwise
// returns zero time and false.
func getScaleUpWaitStarted(statefulSet *appsv1.StatefulSet) (time.Time, bool) {
	if statefulSet.Annotations == nil {
		return time.Time{}, false
	}

	timestampStr, exists := statefulSet.Annotations[ScaleUpWaitStartedAnnotation]
	if !exists {
		return time.Time{}, false
	}

	// Parse RFC3339 timestamp
	timestamp, err := time.Parse(time.RFC3339, timestampStr)
	if err != nil {
		// Invalid format
		return time.Time{}, false
	}

	return timestamp, true
}

// setScaleUpWaitStarted sets the ScaleUpWaitStartedAnnotation to the current time on the StatefulSet.
// This marks the beginning of the wait period for pod readiness during scale-up operations.
// After updating, it re-fetches the StatefulSet to prevent stale data issues in subsequent operations.
func setScaleUpWaitStarted(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("setScaleUpWaitStarted").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	// Initialize annotations map if nil
	if statefulSet.Annotations == nil {
		statefulSet.Annotations = make(map[string]string)
	}

	// Set the current time in RFC3339 format
	currentTime := time.Now().Format(time.RFC3339)
	statefulSet.Annotations[ScaleUpWaitStartedAnnotation] = currentTime

	scopedLog.Info("Setting scale-up wait started timestamp", "timestamp", currentTime)

	// Update the StatefulSet
	err := splutil.UpdateResource(ctx, c, statefulSet)
	if err != nil {
		scopedLog.Error(err, "Failed to update StatefulSet with wait started annotation")
		return err
	}

	// Re-fetch the StatefulSet to ensure we have the latest version from etcd.
	// This prevents race conditions where subsequent operations might work with stale data,
	// particularly important when the annotation is checked immediately after being set.
	namespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: statefulSet.GetName()}
	err = c.Get(ctx, namespacedName, statefulSet)
	if err != nil {
		scopedLog.Error(err, "Failed to re-fetch StatefulSet after setting wait started annotation")
		return err
	}

	return nil
}

// clearScaleUpWaitStarted removes the ScaleUpWaitStartedAnnotation from the StatefulSet.
// This is called when the wait period is complete or when scale-up operations finish.
// After updating, it re-fetches the StatefulSet to prevent stale data issues in subsequent operations.
func clearScaleUpWaitStarted(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("clearScaleUpWaitStarted").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	// Check if annotations exist and the annotation is present
	if statefulSet.Annotations == nil {
		// Nothing to clear
		return nil
	}

	if _, exists := statefulSet.Annotations[ScaleUpWaitStartedAnnotation]; !exists {
		// Annotation doesn't exist, nothing to do
		return nil
	}

	scopedLog.Info("Clearing scale-up wait started timestamp")

	// Remove the annotation
	delete(statefulSet.Annotations, ScaleUpWaitStartedAnnotation)

	// Update the StatefulSet
	err := splutil.UpdateResource(ctx, c, statefulSet)
	if err != nil {
		scopedLog.Error(err, "Failed to update StatefulSet to clear wait started annotation")
		return err
	}

	// Re-fetch the StatefulSet to ensure we have the latest version from etcd.
	// This prevents race conditions where subsequent operations might work with stale data,
	// particularly important in reconciliation loops where the StatefulSet state is checked frequently.
	namespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: statefulSet.GetName()}
	err = c.Get(ctx, namespacedName, statefulSet)
	if err != nil {
		scopedLog.Error(err, "Failed to re-fetch StatefulSet after clearing wait started annotation")
		return err
	}

	return nil
}

// SetStatefulSetOwnerRef sets owner references for statefulset
func SetStatefulSetOwnerRef(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, namespacedName types.NamespacedName) error {

	statefulset, err := GetStatefulSetByName(ctx, client, namespacedName)
	if err != nil {
		return err
	}

	currentOwnerRef := statefulset.GetOwnerReferences()
	// Check if owner ref exists
	for i := 0; i < len(currentOwnerRef); i++ {
		if reflect.DeepEqual(currentOwnerRef[i].UID, cr.GetUID()) {
			return nil
		}
	}

	// Owner ref doesn't exist, update statefulset with owner references
	statefulset.SetOwnerReferences(append(statefulset.GetOwnerReferences(), splcommon.AsOwner(cr, false)))

	// Update owner reference if needed
	err = splutil.UpdateResource(ctx, client, statefulset)
	return err
}

// RemoveUnwantedOwnerRefSs removes all the unwanted owner references for statefulset except the CR it belongs to
func RemoveUnwantedOwnerRefSs(ctx context.Context, client splcommon.ControllerClient, namespacedName types.NamespacedName, cr splcommon.MetaObject) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("RemoveUnwantedOwnerRefSs").WithValues("statefulSet", namespacedName)

	scopedLog.Info("Removing unwanted owner references on CR deletion")

	// Get statefulSet
	statefulset, err := GetStatefulSetByName(ctx, client, namespacedName)
	if err != nil {
		return err
	}

	// Configure statefulSet with only the CR's owner reference
	crOwnerRef := make([]metav1.OwnerReference, 0)
	statefulset.SetOwnerReferences(append(crOwnerRef, splcommon.AsOwner(cr, true)))

	// Update statefulSet
	err = splutil.UpdateResource(ctx, client, statefulset)
	if err != nil {
		return err
	}

	return err
}

// GetStatefulSetByName retrieves current statefulset
func GetStatefulSetByName(ctx context.Context, c splcommon.ControllerClient, namespacedName types.NamespacedName) (*appsv1.StatefulSet, error) {
	var statefulset appsv1.StatefulSet

	err := c.Get(ctx, namespacedName, &statefulset)
	if err != nil {
		// Didn't find it
		return nil, err
	}

	return &statefulset, nil
}

// DeleteReferencesToAutomatedMCIfExists deletes the automated MC sts. This is when customer migrates from automated MC to MC CRD
// Check if MC CR is not the owner of the MC statefulset then delete that Statefulset
func DeleteReferencesToAutomatedMCIfExists(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, namespacedName types.NamespacedName) error {
	statefulset, err := GetStatefulSetByName(ctx, client, namespacedName)
	if err != nil {
		// if MC Sts doesn't exist return nil, may have been deleted by other CR
		return nil
	}
	//2. Retrieve all the owners of the MC statefulset
	currentOwnersRef := statefulset.GetOwnerReferences()
	//3. if Multiple owners OR if current CR is the owner of the MC statefulset then delete the MC statefulset
	if len(currentOwnersRef) > 1 || (len(currentOwnersRef) == 1 && isCurrentCROwner(cr, currentOwnersRef)) {
		err := splutil.DeleteResource(ctx, client, statefulset)
		if err != nil {
			return err
		}

		//delete corresponding mc configmap
		configmap, err := GetConfigMap(ctx, client, namespacedName)
		if k8serrors.IsNotFound(err) {
			return nil
		} else if err != nil {
			return err
		}
		err = splutil.DeleteResource(ctx, client, configmap)
		return err
	}

	return nil
}

// isCurrentCROwner returns true if current CR is the ONLY owner of the automated MC
func isCurrentCROwner(cr splcommon.MetaObject, currentOwners []metav1.OwnerReference) bool {
	// adding extra verification as unit test cases fails since fakeclient do not set UID
	return reflect.DeepEqual(currentOwners[0].UID, cr.GetUID()) &&
		(currentOwners[0].Kind == cr.GetObjectKind().GroupVersionKind().Kind) &&
		(currentOwners[0].Name == cr.GetName())
}

// IsStatefulSetScalingUpOrDown checks if we are currently scaling up or down
func IsStatefulSetScalingUpOrDown(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, name string, desiredReplicas int32) (enterpriseApi.StatefulSetScalingType, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("isScalingUp").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: name}
	current, err := GetStatefulSetByName(ctx, client, namespacedName)
	if err != nil {
		scopedLog.Error(err, "Unable to get current stateful set", "name", namespacedName)
		return enterpriseApi.StatefulSetNotScaling, err
	}

	if *current.Spec.Replicas < desiredReplicas {
		return enterpriseApi.StatefulSetScalingUp, nil
	} else if *current.Spec.Replicas > desiredReplicas {
		return enterpriseApi.StatefulSetScalingDown, nil
	}

	return enterpriseApi.StatefulSetNotScaling, nil
}
