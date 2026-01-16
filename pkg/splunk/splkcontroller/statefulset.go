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

	// Directional values for PreserveTotalCPUAnnotation
	// PreserveTotalCPUDown enables CPU-preserving scaling only when replicas decrease
	// (i.e., when CPU per pod increases)
	PreserveTotalCPUDown = "down"

	// PreserveTotalCPUUp enables CPU-preserving scaling only when replicas increase
	// (i.e., when CPU per pod decreases). NOTE: Scale-up is not yet supported.
	PreserveTotalCPUUp = "up"

	// PreserveTotalCPUBoth enables CPU-preserving scaling for both directions
	PreserveTotalCPUBoth = "both"

	// PreserveTotalCPUTrue is an alias for "both", providing backward compatibility
	PreserveTotalCPUTrue = "true"

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

	// CPUAwareTransitionStateAnnotation stores the complete state of a CPU-aware transition as JSON.
	// This annotation is automatically managed by the operator and should not be set manually.
	// The JSON structure includes original/target replicas, CPU per pod, and timestamps.
	CPUAwareTransitionStateAnnotation = "operator.splunk.com/cpu-aware-transition-state"

	// DefaultParallelPodUpdates is the default number of pods to update in parallel when the annotation is not set.
	DefaultParallelPodUpdates = 1
)

// CPUAwareTransitionState represents the complete state of a CPU-aware scaling transition.
// This struct is serialized to JSON and stored in the CPUAwareTransitionStateAnnotation.
type CPUAwareTransitionState struct {
	// OriginalReplicas is the number of replicas before the transition started
	OriginalReplicas int32 `json:"originalReplicas"`
	// TargetReplicas is the number of replicas after the transition completes
	TargetReplicas int32 `json:"targetReplicas"`
	// OriginalCPUMillis is the CPU request per pod (in millicores) before the transition
	OriginalCPUMillis int64 `json:"originalCPUMillis"`
	// TargetCPUMillis is the CPU request per pod (in millicores) after the transition
	TargetCPUMillis int64 `json:"targetCPUMillis"`
	// StartedAt is the timestamp when the transition started (RFC3339 format)
	StartedAt string `json:"startedAt"`
	// FinishedAt is the timestamp when the transition completed (RFC3339 format, empty if in progress)
	FinishedAt string `json:"finishedAt,omitempty"`
}

// UnifiedTransitionStateAnnotation stores the state of all concurrent transitions as JSON.
const UnifiedTransitionStateAnnotation = "operator.splunk.com/unified-transition-state"

// UnifiedTransitionStallTimeoutAnnotation allows users to configure the maximum time
// a unified transition can run before being considered stalled.
// Format: duration string (e.g., "30m", "1h")
// Default: 30 minutes
const UnifiedTransitionStallTimeoutAnnotation = "operator.splunk.com/unified-transition-stall-timeout"

// DefaultUnifiedTransitionStallTimeout is the default timeout for detecting stalled transitions.
const DefaultUnifiedTransitionStallTimeout = 30 * time.Minute

// MaxPodRecycleFailures is the maximum number of times a pod can fail recycling
// before being marked as permanently failed and skipped.
const MaxPodRecycleFailures = 3

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

// isPreserveTotalCPUEnabled checks if the CPU-preserving scaling annotation is enabled on the StatefulSet.
func isPreserveTotalCPUEnabled(statefulSet *appsv1.StatefulSet) bool {
	if statefulSet.Annotations == nil {
		return false
	}
	value, exists := statefulSet.Annotations[PreserveTotalCPUAnnotation]
	if !exists {
		return false
	}
	// Accept "true", "both", "down", or "up" as valid enabled values
	switch value {
	case PreserveTotalCPUTrue, PreserveTotalCPUBoth, PreserveTotalCPUDown, PreserveTotalCPUUp:
		return true
	default:
		return false
	}
}

// getReplicaScalingDirection determines the direction of replica scaling based on CPU changes.
// Returns "down" if newCPU > originalCPU (replicas will decrease to maintain total CPU).
// Returns "up" if newCPU < originalCPU (replicas would increase to maintain total CPU).
// Returns "" if CPU values are equal (no scaling needed).
func getReplicaScalingDirection(originalCPU, newCPU int64) string {
	if newCPU > originalCPU {
		return PreserveTotalCPUDown
	}
	if newCPU < originalCPU {
		return PreserveTotalCPUUp
	}
	return ""
}

// isCPUScalingAllowed checks if CPU-preserving scaling is allowed for the given direction.
// The annotation value can be:
// - "true" or "both": Allow scaling in both directions
// - "down": Allow only when replicas decrease (CPU per pod increases)
// - "up": Allow only when replicas increase (CPU per pod decreases)
// - Any other value or missing: Disabled (returns false)
func isCPUScalingAllowed(statefulSet *appsv1.StatefulSet, direction string) bool {
	if statefulSet.Annotations == nil {
		return false
	}
	value, exists := statefulSet.Annotations[PreserveTotalCPUAnnotation]
	if !exists {
		return false
	}

	// Normalize and check
	switch value {
	case PreserveTotalCPUTrue, PreserveTotalCPUBoth:
		return true
	case PreserveTotalCPUDown:
		return direction == PreserveTotalCPUDown
	case PreserveTotalCPUUp:
		return direction == PreserveTotalCPUUp
	default:
		return false
	}
}

// SyncCRReplicasFromCPUAwareTransition checks if CPU-aware scaling completed and the CR
// needs to be updated. Returns the target replicas if CR update is needed.
// This function does NOT remove the annotation - caller must do that after updating CR.
//
// It enforces that FinishedAt must be set before returning needsSync=true.
// This prevents the CR from being updated before the transition is actually complete,
// which could cause the annotation to be cleared prematurely.
//
// Returns:
// - (targetReplicas, true) if CR.Spec.Replicas should be updated to targetReplicas
// - (0, false) if no update needed (annotation absent, FinishedAt not set, or CR already matches)
func SyncCRReplicasFromCPUAwareTransition(statefulSet *appsv1.StatefulSet, crReplicas int32) (int32, bool) {
	if statefulSet.Annotations == nil {
		return 0, false
	}

	stateJSON, exists := statefulSet.Annotations[CPUAwareTransitionStateAnnotation]
	if !exists {
		return 0, false
	}

	var state CPUAwareTransitionState
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return 0, false
	}

	// CRITICAL: Require FinishedAt to be set before signaling CR update.
	// This ensures the transition is truly complete and prevents clearing
	// the annotation prematurely.
	if state.FinishedAt == "" {
		return 0, false
	}

	// Only signal update if:
	// 1. FinishedAt is set (transition is complete)
	// 2. StatefulSet has reached target
	// 3. CR doesn't match target yet
	if *statefulSet.Spec.Replicas == state.TargetReplicas && crReplicas != state.TargetReplicas {
		return state.TargetReplicas, true
	}

	return 0, false
}

// ClearCPUAwareTransitionAnnotation removes the CPUAwareTransitionStateAnnotation from the StatefulSet.
// Call this after successfully updating the CR's replicas.
func ClearCPUAwareTransitionAnnotation(ctx context.Context, c splcommon.ControllerClient,
	statefulSet *appsv1.StatefulSet) error {

	if statefulSet.Annotations == nil {
		return nil
	}
	if _, exists := statefulSet.Annotations[CPUAwareTransitionStateAnnotation]; !exists {
		return nil
	}

	delete(statefulSet.Annotations, CPUAwareTransitionStateAnnotation)
	return splutil.UpdateResource(ctx, c, statefulSet)
}

// getUnifiedTransitionState parses the UnifiedTransitionStateAnnotation and returns the state.
// If the new annotation is not present, it checks for the old CPUAwareTransitionStateAnnotation
// and migrates it to the new format for backward compatibility.
// Returns nil if no transition state is found.
// Returns true if the StatefulSet has a transition annotation AND the transition is finished.
func IsCPUPreservingScalingFinished(statefulSet *appsv1.StatefulSet) bool {
	if statefulSet.Annotations == nil {
		return false
	}

	stateJSON, exists := statefulSet.Annotations[CPUAwareTransitionStateAnnotation]
	if !exists {
		return false
	}

	var state CPUAwareTransitionState
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return false
	}

	// Transition is finished when FinishedAt timestamp is set
	return state.FinishedAt != ""
}

// checkCPUTransitionCompletion checks if: replicas == targetReplicas AND all pods [0..targetReplicas-1] have target CPU (new spec).
// Returns true if the transition is complete and ready to persist FinishedAt.
func checkCPUTransitionCompletion(
	ctx context.Context,
	c splcommon.ControllerClient,
	statefulSet *appsv1.StatefulSet,
	targetReplicas int32,
	targetCPUMillis int64,
) bool {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("checkCPUTransitionCompletion").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	replicas := *statefulSet.Spec.Replicas
	if replicas != targetReplicas {
		scopedLog.Info("Replicas not at target", "current", replicas, "target", targetReplicas)
		return false
	}

	// Check all pods [0, targetReplicas-1] have new spec
	for n := int32(0); n < targetReplicas; n++ {
		podName := fmt.Sprintf("%s-%d", statefulSet.GetName(), n)
		podNamespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: podName}
		var pod corev1.Pod
		if err := c.Get(ctx, podNamespacedName, &pod); err != nil {
			// Pod doesn't exist yet - not complete
			scopedLog.Info("Pod not found, transition not complete", "podName", podName)
			return false
		}
		if !hasNewSpec(&pod, targetCPUMillis) {
			scopedLog.Info("Pod does not have new spec", "podName", podName,
				"currentCPU", extractCPUFromPod(&pod), "targetCPU", targetCPUMillis)
			return false
		}
	}

	scopedLog.Info("All pods have new spec, transition complete",
		"targetReplicas", targetReplicas, "targetCPUMillis", targetCPUMillis)
	return true
}

// persistCPUTransitionFinished sets FinishedAt timestamp,
// marshal state, write CPUAwareTransitionStateAnnotation, and update the StatefulSet.
// Used by both handleCPUPreservingScaleUp and handleCPUPreservingScaleDown.
// Returns error if persistence fails.
func persistCPUTransitionFinished(
	ctx context.Context,
	c splcommon.ControllerClient,
	statefulSet *appsv1.StatefulSet,
	state *CPUAwareTransitionState,
) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("persistCPUTransitionFinished").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	// Set FinishedAt timestamp
	state.FinishedAt = time.Now().Format(time.RFC3339)

	// Marshal and persist
	updatedStateJSON, marshalErr := json.Marshal(state)
	if marshalErr != nil {
		scopedLog.Error(marshalErr, "Failed to marshal completed transition state")
		return marshalErr
	}

	statefulSet.Annotations[CPUAwareTransitionStateAnnotation] = string(updatedStateJSON)
	if updateErr := splutil.UpdateResource(ctx, c, statefulSet); updateErr != nil {
		scopedLog.Error(updateErr, "Failed to persist FinishedAt timestamp")
		return updateErr
	}

	scopedLog.Info("Transition completion persisted", "finishedAt", state.FinishedAt)
	return nil
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

// computeReadyCPUMetricsForScaleDown calculates CPU metrics for scale-down transitions.
// Pod population: READY pods only.
// It uses stored original/target CPU values from CPUAwareTransitionState.
func computeReadyCPUMetricsForScaleDown(
	ctx context.Context,
	c splcommon.ControllerClient,
	statefulSet *appsv1.StatefulSet,
	state CPUAwareTransitionState,
) (ScalingCPUMetrics, error) {
	scopedLog := log.FromContext(ctx)
	logger := scopedLog.WithName("computeReadyCPUMetricsForScaleDown").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	metrics := ScalingCPUMetrics{
		OriginalCPUPerPod: state.OriginalCPUMillis,
		TargetCPUPerPod:   state.TargetCPUMillis,
		OriginalTotalCPU:  int64(state.OriginalReplicas) * state.OriginalCPUMillis,
		TargetTotalCPU:    int64(state.TargetReplicas) * state.TargetCPUMillis,
	}

	// List all pods for this StatefulSet to get live CPU allocation
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

	for i := range podList.Items {
		pod := &podList.Items[i]

		if !isPodReady(pod) {
			continue
		}

		podCPU := extractCPUFromPod(pod)
		metrics.TotalReadyCPU += podCPU

		// Check if pod has new spec
		if hasNewSpec(pod, state.TargetCPUMillis) {
			metrics.NewSpecReadyPods++
			metrics.NewSpecReadyCPU += podCPU
		} else {
			metrics.OldSpecReadyPods++
			metrics.OldSpecReadyCPU += podCPU
		}
	}

	logger.Info("Computed CPU metrics for scale-down",
		"totalReadyCPU", metrics.TotalReadyCPU,
		"newSpecPods", metrics.NewSpecReadyPods,
		"newSpecCPU", metrics.NewSpecReadyCPU,
		"oldSpecPods", metrics.OldSpecReadyPods,
		"oldSpecCPU", metrics.OldSpecReadyCPU,
		"originalCPUPerPod", metrics.OriginalCPUPerPod,
		"targetCPUPerPod", metrics.TargetCPUPerPod,
		"originalTotalCPU", metrics.OriginalTotalCPU,
		"targetTotalCPU", metrics.TargetTotalCPU)

	return metrics, nil
}

// ScaleUpCPUMetrics tracks CPU allocation during scale-up transitions
// Unlike ScalingCPUMetrics which uses ready pods only, this includes all non-terminated pods
type ScaleUpCPUMetrics struct {
	TotalPodCPU      int64 // Total CPU of all non-terminated pods
	OldSpecPodCount  int32 // Number of non-terminated pods with old spec
	NewSpecPodCount  int32 // Number of non-terminated pods with new spec
	OldSpecReadyPods int32 // Number of READY pods with old spec (eligible for recycling)
}

// computeCPUCeiling calculates the CPU ceiling for scale-up transitions.
// The ceiling is the original total CPU plus a buffer based on parallelUpdates.
// This ensures we can make progress by adding new pods while staying within a reasonable CPU bound.
func computeCPUCeiling(state CPUAwareTransitionState, parallelUpdates int32) int64 {
	originalTotalCPU := int64(state.OriginalReplicas) * state.OriginalCPUMillis
	// Buffer allows adding up to parallelUpdates new pods without exceeding ceiling
	bufferCPU := int64(parallelUpdates) * state.TargetCPUMillis
	return originalTotalCPU + bufferCPU
}

// computeNonTerminatedCPUMetricsForScaleUp calculates CPU metrics for scale-up transitions.
// Pod population: all non-terminated pods.
// Unlike computeReadyCPUMetricsForScaleDown (READY pods only),
// this function counts ALL non-terminated pods because during scale-up
// we need to know the total CPU requests being made to the cluster.
func computeNonTerminatedCPUMetricsForScaleUp(
	ctx context.Context,
	c splcommon.ControllerClient,
	statefulSet *appsv1.StatefulSet,
	state CPUAwareTransitionState,
) (ScaleUpCPUMetrics, error) {
	scopedLog := log.FromContext(ctx)
	logger := scopedLog.WithName("computeNonTerminatedCPUMetricsForScaleUp").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	metrics := ScaleUpCPUMetrics{}

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

	for i := range podList.Items {
		pod := &podList.Items[i]

		// Skip terminated pods (Succeeded or Failed phase)
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}

		podCPU := extractCPUFromPod(pod)
		metrics.TotalPodCPU += podCPU

		// Check if pod has new spec (target CPU)
		if hasNewSpec(pod, state.TargetCPUMillis) {
			metrics.NewSpecPodCount++
		} else {
			metrics.OldSpecPodCount++
			// Track ready old-spec pods separately (eligible for recycling)
			if isPodReady(pod) {
				metrics.OldSpecReadyPods++
			}
		}
	}

	logger.Info("Computed CPU metrics for scale-up",
		"totalPodCPU", metrics.TotalPodCPU,
		"oldSpecPodCount", metrics.OldSpecPodCount,
		"newSpecPodCount", metrics.NewSpecPodCount,
		"oldSpecReadyPods", metrics.OldSpecReadyPods,
		"targetCPU", state.TargetCPUMillis,
		"originalCPU", state.OriginalCPUMillis)

	return metrics, nil
}

// DefaultStatefulSetPodManager is a simple StatefulSetPodManager that does nothing
type DefaultStatefulSetPodManager struct{}

// Update for DefaultStatefulSetPodManager handles all updates for a statefulset of standard pods.
func (mgr *DefaultStatefulSetPodManager) Update(ctx context.Context, client splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (enterpriseApi.Phase, error) {
	// Get eventPublisher from context
	var eventPublisher splcommon.K8EventPublisher
	if ep := ctx.Value(splcommon.EventPublisherKey); ep != nil {
		eventPublisher = ep.(splcommon.K8EventPublisher)
	}

	phase, err := ApplyStatefulSet(ctx, client, statefulSet, eventPublisher)

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

	// check for changes in StatefulSet-level metadata (labels and annotations)
	hasUpdates = hasUpdates || splcommon.MergeStatefulSetMetaUpdates(ctx, &current.ObjectMeta, &revised.ObjectMeta, current.GetName())

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

	// Detect if we need a unified transition for VCT migration and/or CPU changes
	// This replaces the legacy CPU-aware transition when VCT migration is also needed
	newCPU := getCPURequest(&revised.Spec.Template.Spec)
	needsCPUTransition := isPreserveTotalCPUEnabled(revised) && originalCPU != newCPU
	needsVCTMigration := vctResult.RequiresPVCMigration

	// Check if unified transition is already in progress
	existingUnifiedState, _ := getUnifiedTransitionState(revised)

	// If the existing unified state is a completed transition (migrated from old annotation),
	// treat it as if there's no active transition so we can start a new one
	if existingUnifiedState != nil && existingUnifiedState.FinishedAt != "" {
		scopedLog.Info("Ignoring completed unified state for new transition detection",
			"finishedAt", existingUnifiedState.FinishedAt)
		existingUnifiedState = nil
	}

	// Initialize unified transition when VCT migration is needed (with or without CPU change)
	// This uses the new unified transition system instead of legacy CPU-aware transition
	if needsVCTMigration && existingUnifiedState == nil {
		scopedLog.Info("Initializing unified transition for VCT migration",
			"needsCPUTransition", needsCPUTransition,
			"needsVCTMigration", needsVCTMigration)

		// Build unified state
		state := initUnifiedTransitionState(nil, nil)

		if needsCPUTransition {
			adjustedReplicas := calculateAdjustedReplicas(originalReplicas, originalCPU, newCPU)
			state.CPUChange = &CPUTransition{
				OriginalCPUMillis: originalCPU,
				TargetCPUMillis:   newCPU,
				OriginalReplicas:  originalReplicas,
				TargetReplicas:    adjustedReplicas,
			}
		}

		// Build VCT migration state
		expectedSC := make(map[string]string)
		expectedModes := make(map[string][]corev1.PersistentVolumeAccessMode)

		for _, change := range vctResult.PVCMigrationChanges {
			if change.NewStorageClass != "" {
				expectedSC[change.TemplateName] = change.NewStorageClass
			}
			if len(change.NewAccessModes) > 0 {
				expectedModes[change.TemplateName] = change.NewAccessModes
			}
		}

		state.VCTMigration = &VCTMigrationTransition{
			ExpectedStorageClasses: expectedSC,
			ExpectedAccessModes:    expectedModes,
		}

		// Persist state to annotation
		if err := persistUnifiedTransitionState(ctx, c, revised, state); err != nil {
			scopedLog.Error(err, "Failed to persist unified transition state")
			return enterpriseApi.PhaseError, err
		}

		if eventPublisher != nil {
			msg := "Started unified transition for VCT migration"
			if needsCPUTransition {
				msg += " with CPU-aware scaling"
			}
			eventPublisher.Normal(ctx, "UnifiedTransitionStarted", msg)
		}

		hasUpdates = true
	}

	// Apply CPU-aware scaling adjustments AFTER copying current to revised
	// Note: MergePodUpdates already merged the new template into current, so current now has the NEW CPU value
	// We compare the original CPU (before merge) with the new CPU (after merge) to detect changes
	// SKIP if unified transition was initialized above (it handles CPU changes too)
	if isPreserveTotalCPUEnabled(revised) && existingUnifiedState == nil && !needsVCTMigration {
		direction := getReplicaScalingDirection(originalCPU, newCPU)

		if direction != "" && isCPUScalingAllowed(revised, direction) {
			adjustedReplicas := calculateAdjustedReplicas(originalReplicas, originalCPU, newCPU)

			if adjustedReplicas != originalReplicas {
				scopedLog.Info("CPU-aware scaling detected. Will handle gradually with CPU constraints",
					"direction", direction,
					"originalCPU", originalCPU,
					"originalReplicas", originalReplicas,
					"currentTotalCPU", originalReplicas*int32(originalCPU),
					"newCPU", newCPU,
					"targetReplicas", adjustedReplicas,
					"targetTotalCPU", adjustedReplicas*int32(newCPU),
				)

				// Keep current replicas, will be adjusted gradually
				// Store complete transition state as JSON annotation
				if revised.Annotations == nil {
					revised.Annotations = make(map[string]string)
				}

				// Clear any completed transition annotation before creating new one
				if existingStateJSON, exists := revised.Annotations[CPUAwareTransitionStateAnnotation]; exists {
					var existingState CPUAwareTransitionState
					if parseErr := json.Unmarshal([]byte(existingStateJSON), &existingState); parseErr == nil {
						if existingState.FinishedAt != "" {
							scopedLog.Info("Clearing completed transition annotation",
								"previousFinishedAt", existingState.FinishedAt)

							delete(revised.Annotations, CPUAwareTransitionStateAnnotation)

							if clearErr := splutil.UpdateResource(ctx, c, revised); clearErr != nil {
								scopedLog.Error(clearErr, "Failed to clear completed transition annotation")
								return enterpriseApi.PhaseError, clearErr
							}

							// Re-fetch after clearing to ensure we have latest resource version
							if getErr := c.Get(ctx, namespacedName, revised); getErr != nil {
								scopedLog.Error(getErr, "Failed to re-fetch StatefulSet after clearing annotation")
								return enterpriseApi.PhaseError, getErr
							}
						}
					}
				}

				transitionState := CPUAwareTransitionState{
					OriginalReplicas:  originalReplicas,
					TargetReplicas:    adjustedReplicas,
					OriginalCPUMillis: originalCPU,
					TargetCPUMillis:   newCPU,
					StartedAt:         time.Now().Format(time.RFC3339),
				}
				stateJSON, jsonErr := json.Marshal(transitionState)
				if jsonErr != nil {
					scopedLog.Error(jsonErr, "Failed to marshal CPU-aware transition state")
					return enterpriseApi.PhaseError, jsonErr
				}
				revised.Annotations[CPUAwareTransitionStateAnnotation] = string(stateJSON)
				hasUpdates = true
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
		"replicas", replicas,
		"readyReplicas", readyReplicas,
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
					"elapsed", elapsed)
				if readyReplicas > 0 {
					return enterpriseApi.PhaseScalingUp, nil
				}
				return enterpriseApi.PhasePending, nil
			}
		}
	}
	// All current pods are ready (or timeout exceeded), proceed with scale up
	scopedLog.Info("Scaling replicas up")
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

// handleCPUPreservingScaleUp manages the gradual scale-up transition when CPU-preserving scaling is enabled.
// This function implements a ceiling-based algorithm to prevent CPU request spikes during scale-up.
//
// Algorithm (4 steps):
//  1. CHECK COMPLETION - If replicas == targetReplicas AND all pods [0, targetReplicas-1] have new spec
//  2. COMPUTE METRICS - Get totalPodCPU (all non-terminated pods), cpuCeiling
//  3. ADD NEW PODS - If under ceiling and below target replicas, add pods
//  4. RECYCLE OLD PODS - If ceiling prevents adding and old-spec pods exist, recycle to free capacity
//
// The CPU ceiling is: originalTotalCPU + (parallelUpdates × targetCPUPerPod)
// This ensures we never exceed the original total CPU by more than a small buffer.
//
// Returns: (phase, handled, error)
// - (phase, true, nil) if CPU-preserving scale-up is being handled (caller should return phase)
// - (PhaseReady, false, nil) if CPU-preserving scale-up is not applicable (caller should continue)
// - (PhaseError, true, error) if an error occurred
func handleCPUPreservingScaleUp(
	ctx context.Context,
	c splcommon.ControllerClient,
	statefulSet *appsv1.StatefulSet,
	mgr splcommon.StatefulSetPodManager,
	state CPUAwareTransitionState,
) (enterpriseApi.Phase, bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("handleCPUPreservingScaleUp").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	replicas := *statefulSet.Spec.Replicas
	targetReplicas := state.TargetReplicas
	parallelUpdates := getParallelPodUpdates(statefulSet)

	// Clamp parallelUpdates to at least 1 (safety)
	if parallelUpdates < 1 {
		parallelUpdates = 1
	}

	cpuCeiling := computeCPUCeiling(state, parallelUpdates)

	scopedLog.Info("CPU-aware scale-up active",
		"currentReplicas", replicas,
		"targetReplicas", targetReplicas,
		"originalCPUMillis", state.OriginalCPUMillis,
		"targetCPUMillis", state.TargetCPUMillis,
		"cpuCeiling", cpuCeiling,
		"parallelUpdates", parallelUpdates)

	// Step 1: CHECK COMPLETION
	// If replicas == targetReplicas AND all pods [0, targetReplicas-1] have new spec
	if replicas == targetReplicas && checkCPUTransitionCompletion(ctx, c, statefulSet, targetReplicas, state.TargetCPUMillis) {
		scopedLog.Info("CPU-aware scale-up complete, all pods have new spec",
			"finalReplicas", targetReplicas,
			"action", "caller should update CR replicas and remove annotation")

		if err := persistCPUTransitionFinished(ctx, c, statefulSet, &state); err != nil {
			return enterpriseApi.PhaseError, true, err
		}

		scopedLog.Info("Scale-up transition completion persisted", "finishedAt", state.FinishedAt)
		return enterpriseApi.PhaseReady, true, nil
	}

	// Step 2: COMPUTE LIVE CPU METRICS
	// Use all non-terminated pods (not just ready) to track total CPU requests
	metrics, metricsErr := computeNonTerminatedCPUMetricsForScaleUp(ctx, c, statefulSet, state)
	if metricsErr != nil {
		scopedLog.Error(metricsErr, "Unable to compute CPU metrics for scale-up")
		return enterpriseApi.PhaseError, true, metricsErr
	}

	scopedLog.Info("Scale-up CPU metrics computed",
		"totalPodCPU", metrics.TotalPodCPU,
		"cpuCeiling", cpuCeiling,
		"oldSpecPodCount", metrics.OldSpecPodCount,
		"newSpecPodCount", metrics.NewSpecPodCount,
		"oldSpecReadyPods", metrics.OldSpecReadyPods)

	// Step 3: ADD NEW PODS (if below target and under ceiling)
	if replicas < targetReplicas {
		availableRoom := cpuCeiling - metrics.TotalPodCPU
		targetCPUPerPod := state.TargetCPUMillis

		scopedLog.Info("Checking if we can add new pods",
			"availableRoom", availableRoom,
			"targetCPUPerPod", targetCPUPerPod,
			"currentReplicas", replicas,
			"targetReplicas", targetReplicas)

		if availableRoom >= targetCPUPerPod {
			// Calculate how many pods we can add
			podsCanAdd := availableRoom / targetCPUPerPod
			podsNeeded := int64(targetReplicas - replicas)
			if podsCanAdd > podsNeeded {
				podsCanAdd = podsNeeded
			}
			if podsCanAdd > int64(parallelUpdates) {
				podsCanAdd = int64(parallelUpdates)
			}

			if podsCanAdd > 0 {
				newReplicas := replicas + int32(podsCanAdd)
				scopedLog.Info("Adding new pods (under CPU ceiling)",
					"podsToAdd", podsCanAdd,
					"newReplicas", newReplicas,
					"availableRoom", availableRoom,
					"cpuCeiling", cpuCeiling,
					"totalPodCPU", metrics.TotalPodCPU)

				*statefulSet.Spec.Replicas = newReplicas
				updateErr := splutil.UpdateResource(ctx, c, statefulSet)
				if updateErr != nil {
					scopedLog.Error(updateErr, "Unable to update StatefulSet replicas for scale-up")
					return enterpriseApi.PhaseError, true, updateErr
				}
				return enterpriseApi.PhaseScalingUp, true, nil
			}
		}

		// Step 4: RECYCLE OLD-SPEC PODS to free capacity
		// We can only add more pods if we recycle old-spec pods (which have higher CPU)
		if metrics.OldSpecReadyPods > 0 {
			scopedLog.Info("CPU ceiling reached, need to recycle old-spec pods to free capacity",
				"availableRoom", availableRoom,
				"targetCPUPerPod", targetCPUPerPod,
				"oldSpecReadyPods", metrics.OldSpecReadyPods)

			// Find old-spec READY pods and recycle up to parallelUpdates
			recycledCount := int32(0)
			for n := int32(0); n < replicas && recycledCount < parallelUpdates; n++ {
				podName := fmt.Sprintf("%s-%d", statefulSet.GetName(), n)
				podNamespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: podName}
				var pod corev1.Pod
				podErr := c.Get(ctx, podNamespacedName, &pod)
				if podErr != nil {
					// Pod doesn't exist - skip
					continue
				}

				// Skip pods that are not ready (being recreated) or already have new spec
				if !isPodReady(&pod) || hasNewSpec(&pod, state.TargetCPUMillis) {
					continue
				}

				// Found an old-spec READY pod - recycle it
				scopedLog.Info("Recycling old-spec pod to free CPU capacity",
					"podName", podName,
					"podIndex", n,
					"oldCPU", extractCPUFromPod(&pod),
					"targetCPU", state.TargetCPUMillis)

				// Prepare for recycle
				ready, prepErr := mgr.PrepareRecycle(ctx, n)
				if prepErr != nil {
					scopedLog.Info("Unable to prepare pod for recycling, skipping for now",
						"podName", podName,
						"error", prepErr.Error())
					continue
				}

				recycledCount++
				if !ready {
					scopedLog.Info("Pod preparation in progress", "podName", podName)
					continue
				}

				// Delete the pod to trigger recreation with new spec
				preconditions := client.Preconditions{UID: &pod.ObjectMeta.UID, ResourceVersion: &pod.ObjectMeta.ResourceVersion}
				delErr := c.Delete(ctx, &pod, preconditions)
				if delErr != nil {
					scopedLog.Error(delErr, "Unable to delete Pod for recycling", "podName", podName)
					return enterpriseApi.PhaseError, true, delErr
				}

				scopedLog.Info("Recycled pod for CPU-aware scale-up",
					"podName", podName,
					"recycledThisCycle", recycledCount,
					"parallelUpdates", parallelUpdates)
			}

			if recycledCount > 0 {
				return enterpriseApi.PhaseUpdating, true, nil
			}
		}
	}

	// No action possible this cycle - waiting for pods to be recreated or scheduling
	scopedLog.Info("Waiting for scale-up progress (pods being created/recycled)")
	return enterpriseApi.PhaseUpdating, true, nil
}

// handleCPUPreservingScaleDown manages the gradual scale-down transition when CPU-preserving scaling is enabled.
// This function implements an interleaved recycle-and-balance algorithm that eliminates deadlock scenarios.
//
// Algorithm (4 steps):
//  1. CHECK COMPLETION - If replicas == targetReplicas AND all pods [0, targetReplicas-1] have new spec
//  2. COMPUTE METRICS - Get totalReadyCPU, originalTotalCPU, currentReplicas
//  3. BALANCE - If surplusCPU >= oldCPUPerPod, reduce replicas (return PhaseScalingDown)
//  4. RECYCLE - Recycle old-spec READY pods in [0, targetReplicas-1] up to parallelUpdates at a time
//
// Returns: (phase, handled, error)
// - (phase, true, nil) if CPU-preserving scale-down is being handled (caller should return phase)
// - (PhaseReady, false, nil) should never occur (caller already determined this is scale-down)
// - (PhaseError, true, error) if an error occurred
func handleCPUPreservingScaleDown(
	ctx context.Context,
	c splcommon.ControllerClient,
	statefulSet *appsv1.StatefulSet,
	mgr splcommon.StatefulSetPodManager,
	state CPUAwareTransitionState,
) (enterpriseApi.Phase, bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("handleCPUPreservingScaleDown").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	replicas := *statefulSet.Spec.Replicas
	readyReplicas := statefulSet.Status.ReadyReplicas
	targetReplicas := state.TargetReplicas

	scopedLog.Info("CPU-aware scale-down active",
		"currentReplicas", replicas,
		"targetReplicas", targetReplicas,
		"readyReplicas", readyReplicas,
		"originalCPUMillis", state.OriginalCPUMillis,
		"targetCPUMillis", state.TargetCPUMillis)

	// Step 1: CHECK COMPLETION using shared helper
	// If replicas == targetReplicas AND all pods [0, targetReplicas-1] have new spec
	if replicas == targetReplicas && checkCPUTransitionCompletion(ctx, c, statefulSet, targetReplicas, state.TargetCPUMillis) {
		scopedLog.Info("CPU-aware transition complete, all kept pods have new spec",
			"finalReplicas", targetReplicas,
			"action", "caller should update CR replicas and remove annotation")

		// Use shared helper to persist FinishedAt
		if err := persistCPUTransitionFinished(ctx, c, statefulSet, &state); err != nil {
			return enterpriseApi.PhaseError, true, err
		}

		scopedLog.Info("Transition completion persisted", "finishedAt", state.FinishedAt)
		return enterpriseApi.PhaseReady, true, nil
	}

	// Step 2: COMPUTE LIVE CPU METRICS
	// We use stored values from state for original/target CPU per pod,
	// but need to query live pods to get current ready CPU allocation
	metrics, metricsErr := computeReadyCPUMetricsForScaleDown(ctx, c, statefulSet, state)
	if metricsErr != nil {
		scopedLog.Error(metricsErr, "Unable to compute CPU metrics")
		return enterpriseApi.PhaseError, true, metricsErr
	}

	scopedLog.Info("CPU metrics computed",
		"totalReadyCPU", metrics.TotalReadyCPU,
		"originalTotalCPU", metrics.OriginalTotalCPU,
		"oldCPUPerPod", metrics.OriginalCPUPerPod,
		"newSpecPods", metrics.NewSpecReadyPods,
		"oldSpecPods", metrics.OldSpecReadyPods)

	// Step 3: BALANCE (if possible) - reduce replicas when new-spec pods provide surplus CPU
	// This happens BEFORE recycling to efficiently reduce replica count as soon as possible
	// surplusCPU measures the extra CPU capacity provided by new-spec pods compared to what they replaced
	surplusCPU := metrics.NewSpecReadyCPU - (int64(metrics.NewSpecReadyPods) * metrics.OriginalCPUPerPod)
	originalCPUPerPod := metrics.OriginalCPUPerPod

	if originalCPUPerPod > 0 && surplusCPU >= originalCPUPerPod && replicas > targetReplicas {
		// Calculate how many old pods can be safely deleted based on the surplus
		podsSafeToDelete := surplusCPU / originalCPUPerPod

		// Calculate the target replica count based on safe deletions from the original count
		calculatedTargetReplicas := state.OriginalReplicas - int32(podsSafeToDelete)

		// Never go below the final target replicas
		if calculatedTargetReplicas < targetReplicas {
			calculatedTargetReplicas = targetReplicas
		}

		if replicas > calculatedTargetReplicas {
			scopedLog.Info("Balancing CPU: reducing replicas based on new-spec surplus",
				"surplusCPU", surplusCPU,
				"originalCPUPerPod", originalCPUPerPod,
				"newSpecReadyPods", metrics.NewSpecReadyPods,
				"newSpecReadyCPU", metrics.NewSpecReadyCPU,
				"originalReplicas", state.OriginalReplicas,
				"podsSafeToDelete", podsSafeToDelete,
				"currentReplicas", replicas,
				"calculatedTargetReplicas", calculatedTargetReplicas)
			statefulSet.Spec.Replicas = &calculatedTargetReplicas
			updateErr := splutil.UpdateResource(ctx, c, statefulSet)
			if updateErr != nil {
				scopedLog.Error(updateErr, "Unable to update StatefulSet replicas for balancing")
				return enterpriseApi.PhaseError, true, updateErr
			}
			return enterpriseApi.PhaseScalingDown, true, nil
		}
	}

	// Step 4: RECYCLE OLD-SPEC KEPT PODS
	// Find old-spec pods in [0, targetReplicas-1] and recycle up to parallelUpdates at a time
	// Note: We don't wait for ALL pods to be ready first - that would block parallel recycling.
	// Instead, we skip pods that are not ready (they're being recreated from previous recycle).

	// Calculate CPU floor to enforce parallel update limit
	// Note: All old pods have the same CPU spec, so use OriginalCPUPerPod directly
	parallelUpdates := getParallelPodUpdates(statefulSet)
	minCPUFloor := metrics.OriginalTotalCPU - (int64(parallelUpdates) * metrics.OriginalCPUPerPod)

	scopedLog.Info("CPU floor calculated for parallel update enforcement",
		"minCPUFloor", minCPUFloor,
		"originalCPUPerPod", metrics.OriginalCPUPerPod,
		"parallelUpdates", parallelUpdates)

	// Track same-cycle recycle count
	recycledCount := int32(0)
	// Track running CPU total as pods are deleted within this cycle
	totalReadyCPU := metrics.TotalReadyCPU

	for n := int32(0); n < targetReplicas; n++ {
		podName := fmt.Sprintf("%s-%d", statefulSet.GetName(), n)
		podNamespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: podName}
		var pod corev1.Pod
		podErr := c.Get(ctx, podNamespacedName, &pod)
		if podErr != nil {
			// Pod doesn't exist yet - being recreated from previous recycle
			continue
		}

		// Skip pods that are not ready (being recreated) || already has new spec
		if !isPodReady(&pod) || hasNewSpec(&pod, state.TargetCPUMillis) {
			continue
		}

		// Found an old-spec READY pod that will be kept

		// SECONDARY CHECK: Enforce same-cycle limit (defense-in-depth)
		if recycledCount >= parallelUpdates {
			scopedLog.Info("Reached same-cycle recycle limit",
				"recycledCount", recycledCount,
				"parallelUpdates", parallelUpdates)
			break
		}

		// PRIMARY CHECK: Verify CPU floor won't be violated
		podCPU := extractCPUFromPod(&pod)
		afterRecycleCPU := totalReadyCPU - podCPU
		if afterRecycleCPU < minCPUFloor {
			// deficit represents how much CPU we would be short of the minimum floor
			deficit := minCPUFloor - afterRecycleCPU
			scopedLog.Info("Cannot recycle pod - would violate CPU floor",
				"podName", podName,
				"podCPU", podCPU,
				"runningTotalCPU", totalReadyCPU,
				"afterRecycleCPU", afterRecycleCPU,
				"minCPUFloor", minCPUFloor,
				"deficit", deficit)
			continue
		}

		scopedLog.Info("Pod eligible for recycling", "podName", podName)

		// All checks passed - proceed with PrepareRecycle
		ready, prepErr := mgr.PrepareRecycle(ctx, n)
		if prepErr != nil {
			// Don't stop the entire transition - log and skip this pod
			// It may be restarting for unrelated reasons (liveness probe, etc.)
			// We'll check it again in the next reconciliation cycle
			scopedLog.Info("Unable to prepare pod for recycling, skipping for now",
				"podName", podName,
				"error", prepErr.Error(),
				"action", "will retry in next reconciliation")
			continue
		}

		// we need to count this pod as recycled even if not ready yet
		// because PrepareRecycle may have initiated decommissioning which takes time and will be done in background
		recycledCount++
		if !ready {
			// Pod is being prepared for recycling (e.g., decommissioning) - count as pending
			// and continue to check other pods for parallel recycling
			scopedLog.Info("Pod preparation in progress, checking next pod", "podName", podName)
			continue
		}

		// Delete the pod to trigger recreation with new spec
		scopedLog.Info("Recycling pod for CPU-aware transition",
			"podName", podName,
			"podIndex", n,
			"oldCPU", extractCPUFromPod(&pod),
			"targetCPU", metrics.TargetCPUPerPod,
			"recycledThisCycle", recycledCount+1,
			"parallelUpdates", parallelUpdates)
		preconditions := client.Preconditions{UID: &pod.ObjectMeta.UID, ResourceVersion: &pod.ObjectMeta.ResourceVersion}
		delErr := c.Delete(ctx, &pod, preconditions)
		if delErr != nil {
			scopedLog.Error(delErr, "Unable to delete Pod for recycling", "podName", podName)
			return enterpriseApi.PhaseError, true, delErr
		}

		// Update running total after successful deletion
		// This ensures subsequent CPU floor checks in this cycle reflect the reduced capacity
		totalReadyCPU -= podCPU

		// Check if we've reached the parallel update limit
		if recycledCount >= parallelUpdates {
			scopedLog.Info("Reached parallel update limit for recycling",
				"recycledCount", recycledCount,
				"parallelUpdates", parallelUpdates,
				"totalReadyCPU", totalReadyCPU,
			)
			break
		}
	}

	if recycledCount > 0 {
		return enterpriseApi.PhaseUpdating, true, nil
	}

	// No pods to recycle and no balancing possible
	// This can happen when waiting for recycled pods to come back up
	scopedLog.Info("No old-spec pods found to recycle, continuing")
	return enterpriseApi.PhaseUpdating, true, nil
}

// handleCPUPreservingTransition is the main dispatcher for CPU-aware scaling transitions.
// It validates the transition state and delegates to the appropriate handler:
//   - handleCPUPreservingScaleUp: for scale-up (targetReplicas > currentReplicas)
//   - handleCPUPreservingScaleDown: for scale-down (targetReplicas < currentReplicas)
//
// When replicas == targetReplicas, the dispatcher runs the shared completion probe and
// persists FinishedAt if complete, ensuring both scale-up and scale-down have consistent
// completion handling.
//
// Returns: (phase, handled, error)
// - (phase, true, nil) if CPU-preserving transition is being handled (caller should return phase)
// - (PhaseReady, false, nil) if CPU-preserving transition is not applicable (caller should continue)
// - (PhaseError, true, error) if an error occurred
func handleCPUPreservingTransition(
	ctx context.Context,
	c splcommon.ControllerClient,
	statefulSet *appsv1.StatefulSet,
	mgr splcommon.StatefulSetPodManager,
	replicas int32,
) (enterpriseApi.Phase, bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("handleCPUPreservingTransition").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	// Check if CPU-preserving scaling is enabled
	if !isPreserveTotalCPUEnabled(statefulSet) {
		return enterpriseApi.PhaseReady, false, nil
	}

	// Check for transition state annotation
	transitionStateJSON := ""
	if statefulSet.Annotations != nil {
		transitionStateJSON = statefulSet.Annotations[CPUAwareTransitionStateAnnotation]
	}
	if transitionStateJSON == "" {
		return enterpriseApi.PhaseReady, false, nil
	}

	// Parse the transition state from JSON
	var transitionState CPUAwareTransitionState
	if parseErr := json.Unmarshal([]byte(transitionStateJSON), &transitionState); parseErr != nil {
		scopedLog.Error(parseErr, "Failed to parse CPU-aware transition state")
		return enterpriseApi.PhaseError, true, parseErr
	}

	// Short-circuit: if transition already completed (FinishedAt is set), skip all steps
	if transitionState.FinishedAt != "" {
		scopedLog.Info("CPU-aware transition already complete, skipping steps",
			"finishedAt", transitionState.FinishedAt,
			"targetReplicas", transitionState.TargetReplicas)
		return enterpriseApi.PhaseReady, true, nil
	}

	targetReplicas := transitionState.TargetReplicas

	// EXPLICIT COMPLETION CHECK: When replicas == targetReplicas, run shared completion probe.
	// This handles the edge case where we're at target replicas but need to check/persist completion.
	if replicas == targetReplicas {
		if checkCPUTransitionCompletion(ctx, c, statefulSet, targetReplicas, transitionState.TargetCPUMillis) {
			scopedLog.Info("CPU-aware transition complete at target replicas, persisting FinishedAt",
				"targetReplicas", targetReplicas)
			if err := persistCPUTransitionFinished(ctx, c, statefulSet, &transitionState); err != nil {
				return enterpriseApi.PhaseError, true, err
			}
			return enterpriseApi.PhaseReady, true, nil
		}
		// At target replicas but not all pods have new spec yet - need to continue recycling
		scopedLog.Info("At target replicas but completion check failed, continuing transition",
			"targetReplicas", targetReplicas)
	}

	if targetReplicas > replicas {
		// Scale-up case: dispatch to scale-up handler
		// This handles the gradual scale-up with CPU ceiling enforcement
		scopedLog.Info("Dispatching to CPU-aware scale-up handler",
			"currentReplicas", replicas,
			"targetReplicas", targetReplicas)
		return handleCPUPreservingScaleUp(ctx, c, statefulSet, mgr, transitionState)
	}

	// Scale-down case (targetReplicas <= replicas): dispatch to scale-down handler
	// This handles the gradual scale-down with CPU floor enforcement
	// Note: This also handles the replicas == targetReplicas case when completion check failed above
	scopedLog.Info("Dispatching to CPU-aware scale-down handler",
		"currentReplicas", replicas,
		"targetReplicas", targetReplicas)
	return handleCPUPreservingScaleDown(ctx, c, statefulSet, mgr, transitionState)
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

	// Get eventPublisher from context (if available)
	var eventPublisher splcommon.K8EventPublisher
	if ep := ctx.Value(splcommon.EventPublisherKey); ep != nil {
		eventPublisher = ep.(splcommon.K8EventPublisher)
	}

	// Handle unified transition (combines CPU-aware scaling + VCT migration)
	// This takes priority over the legacy handleCPUPreservingTransition
	if phase, handled, err := handleUnifiedTransition(ctx, c, statefulSet, mgr, eventPublisher); handled {
		return phase, err
	}

	// Handle CPU-preserving transition if enabled (legacy path for backward compatibility)
	if phase, handled, err := handleCPUPreservingTransition(ctx, c, statefulSet, mgr, replicas); handled {
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

	return enterpriseApi.PhaseReady, nil
}

// getScaleUpReadyWaitTimeout parses the ScaleUpReadyWaitTimeoutAnnotation from the StatefulSet
// and returns the configured timeout duration.
//
// Return values:
//   - If the annotation is missing, invalid format, or negative: returns 0 (immediate bypass, no wait)
//   - Otherwise: returns the parsed duration as-is (any valid Go duration is accepted)
//   - Use "-1" or any negative value to wait forever
//
// For CRs, users should use the `sts-only.operator.splunk.com/scale-up-ready-wait-timeout`
// annotation to prevent propagation to pod templates. The unprefixed key
// `operator.splunk.com/scale-up-ready-wait-timeout` is for direct StatefulSet annotation.
func getScaleUpReadyWaitTimeout(statefulSet *appsv1.StatefulSet) time.Duration {
	// defaultTimeout of 0 means "never wait" - scale up immediately without waiting
	// for existing pods to be ready. Use negative values (e.g., "-1") to wait forever.
	const defaultTimeout = time.Duration(0)

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
		// Invalid format, return default (no wait)
		return defaultTimeout
	}

	// Negative values mean "wait forever" - return as-is
	if timeout < 0 {
		return timeout
	}

	// Zero means immediate bypass, any positive duration is accepted as-is
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

// isPodFullyUpdated checks if a pod has ALL required updates applied.
// Returns true only if:
// - CPU spec matches target (if CPUChange is active)
// - All PVC storage classes match target (if VCTMigration is active)
// - All PVC access modes match target (if VCTMigration specifies access modes)

// canRecyclePodWithinCPUFloor checks if recycling a pod would violate the CPU floor constraint.
// The CPU floor ensures that total ready CPU never drops below the minimum required to maintain
// capacity during transitions.
//
// Returns true if:
// - state.CPUChange is nil (no CPU transition active, no floor constraint)
// - Recycling the pod would not drop total ready CPU below the floor
//
// Returns false if recycling would violate the CPU floor constraint.

// recyclePodForUnifiedTransition handles pod recycling for combined CPU + VCT transitions.
// Key insight: When recycling a pod, delete both the pod AND its PVCs if VCT migration is active.
// The StatefulSet controller will recreate the pod with new spec AND new PVCs.
//
// Error handling:
// - Pod deletion failures are logged and returned as errors (caller tracks failures)
// - PVC deletion failures due to finalizers are logged as warnings and do not block pod deletion
// - Stuck PVCs (deletion pending > 30 minutes) trigger warning events

// handleUnifiedTransition manages combined CPU-aware scaling and VCT migration transitions.
// This is the main entry point for unified transitions, replacing separate handlers.
//
// Key design principles:
// 1. Recycle each pod ONCE for ALL pending changes (CPU + VCT)
// 2. Handle replica scaling first (CPU ceiling/floor logic)
// 3. Then recycle pods that need updates
// 4. Respect parallelUpdates limit
// 5. Enforce CPU floor during transitions
// 6. Track failed pods and skip permanently failed ones after MaxPodRecycleFailures
// 7. Detect stalled transitions and publish warning events
//
// Returns: (phase, handled, error)
// - (phase, true, nil) if transition is being handled (caller should return phase)
// - (PhaseReady, false, nil) if no transition needed (caller should continue)
// - (PhaseError, true, error) if an error occurred
func handleUnifiedTransition(
	ctx context.Context,
	c splcommon.ControllerClient,
	statefulSet *appsv1.StatefulSet,
	mgr splcommon.StatefulSetPodManager,
	eventPublisher splcommon.K8EventPublisher,
) (enterpriseApi.Phase, bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("handleUnifiedTransition").WithValues(
		"name", statefulSet.GetName(),
		"namespace", statefulSet.GetNamespace())

	// Check for unified transition state annotation
	// IMPORTANT: Only handle transitions that were explicitly stored in the new format
	// OR have VCT migration. CPU-only transitions from the old format should be handled
	// by the legacy handleCPUPreservingTransition for backward compatibility.
	if statefulSet.Annotations == nil {
		return enterpriseApi.PhaseReady, false, nil
	}

	// Only process if the new unified annotation exists (not migrated from old format)
	stateJSON, hasNewAnnotation := statefulSet.Annotations[UnifiedTransitionStateAnnotation]
	if !hasNewAnnotation || stateJSON == "" {
		// No new format annotation - let legacy handler take care of it
		return enterpriseApi.PhaseReady, false, nil
	}

	state, err := getUnifiedTransitionState(statefulSet)
	if err != nil {
		scopedLog.Error(err, "Failed to get unified transition state")
		return enterpriseApi.PhaseError, true, err
	}

	if state == nil {
		// No transition in progress
		return enterpriseApi.PhaseReady, false, nil
	}

	// Already complete?
	if state.FinishedAt != "" {
		scopedLog.Info("Unified transition already complete, clearing state")
		if err := clearUnifiedTransitionState(ctx, c, statefulSet); err != nil {
			return enterpriseApi.PhaseError, true, err
		}
		return enterpriseApi.PhaseReady, true, nil
	}

	// ============================================================
	// CHECK FOR STALLED TRANSITION
	// ============================================================
	stallTimeout := getUnifiedTransitionStallTimeout(statefulSet)
	if isTransitionStalled(state, stallTimeout) {
		scopedLog.Info("Warning: Unified transition appears stalled",
			"startedAt", state.StartedAt,
			"stallTimeout", stallTimeout,
			"failedPods", len(state.FailedPods))
		if eventPublisher != nil {
			failedPodCount := 0
			if state.FailedPods != nil {
				failedPodCount = len(state.FailedPods)
			}
			eventPublisher.Warning(ctx, "UnifiedTransitionStalled",
				fmt.Sprintf("Unified transition has been running since %s (over %v). Failed pods: %d. Consider investigating or manually intervening.",
					state.StartedAt, stallTimeout, failedPodCount))
		}
		// Continue processing but log warnings - don't block entirely
	}

	parallelUpdates := getParallelPodUpdates(statefulSet)

	replicas := int32(1)
	if statefulSet.Spec.Replicas != nil {
		replicas = *statefulSet.Spec.Replicas
	}

	targetReplicas := replicas
	if state.CPUChange != nil {
		targetReplicas = state.CPUChange.TargetReplicas
	}

	scopedLog.Info("Processing unified transition",
		"currentReplicas", replicas,
		"targetReplicas", targetReplicas,
		"parallelUpdates", parallelUpdates,
		"hasCPUChange", state.CPUChange != nil,
		"hasVCTMigration", state.VCTMigration != nil,
		"failedPods", len(state.FailedPods))

	// ============================================================
	// STEP 1: Handle replica scaling first (if CPU change requires it)
	// ============================================================
	if state.CPUChange != nil {
		if targetReplicas > replicas {
			// Scale-up case: use CPU ceiling logic
			phase, err := handleUnifiedScaleUp(ctx, c, statefulSet, mgr, state, parallelUpdates, eventPublisher)
			if phase != enterpriseApi.PhaseReady {
				return phase, true, err
			}
		} else if targetReplicas < replicas {
			// Scale-down case: use CPU floor logic + delete PVCs
			phase, err := handleUnifiedScaleDown(ctx, c, statefulSet, mgr, state, parallelUpdates, eventPublisher)
			if phase != enterpriseApi.PhaseReady {
				return phase, true, err
			}
		}
	}

	// ============================================================
	// STEP 2: Recycle pods that need updates (CPU or VCT or both)
	// ============================================================
	recycledCount := int32(0)
	allPodsUpdated := true
	podsBeingRecycled := int32(0)
	stateModified := false // Track if state needs to be persisted

	for n := int32(0); n < targetReplicas && recycledCount < parallelUpdates; n++ {
		podName := fmt.Sprintf("%s-%d", statefulSet.GetName(), n)
		pod := &corev1.Pod{}
		podNamespacedName := types.NamespacedName{
			Namespace: statefulSet.GetNamespace(),
			Name:      podName,
		}

		// Skip permanently failed pods (continue with others)
		if isPodPermanentlyFailed(state, podName) {
			scopedLog.Info("Skipping permanently failed pod",
				"podName", podName,
				"failCount", state.FailedPods[podName].FailCount,
				"lastError", state.FailedPods[podName].LastError)
			continue
		}

		if err := c.Get(ctx, podNamespacedName, pod); err != nil {
			if k8serrors.IsNotFound(err) {
				// Pod being recreated by StatefulSet controller
				scopedLog.Info("Pod not found (being recreated)", "podName", podName)
				allPodsUpdated = false
				podsBeingRecycled++
				continue
			}
			return enterpriseApi.PhaseError, true, err
		}

		// Skip pods that aren't ready (being recycled)
		if !isPodReady(pod) {
			scopedLog.Info("Pod not ready (being recycled)", "podName", podName)
			allPodsUpdated = false
			podsBeingRecycled++
			continue
		}

		// Check if this pod is fully updated
		updated, err := isPodFullyUpdated(ctx, c, pod, statefulSet, state)
		if err != nil {
			return enterpriseApi.PhaseError, true, err
		}

		if updated {
			// This pod is done
			scopedLog.V(1).Info("Pod fully updated", "podName", podName)
			continue
		}

		allPodsUpdated = false

		// Enforce CPU floor if CPU transition is active
		if state.CPUChange != nil {
			if !canRecyclePodWithinCPUFloor(ctx, c, statefulSet, pod, state, parallelUpdates) {
				scopedLog.Info("Cannot recycle pod - would violate CPU floor", "podName", podName)
				continue
			}
		}

		// Recycle this pod (handles both CPU and VCT)
		err = recyclePodForUnifiedTransition(ctx, c, statefulSet, mgr, pod, n, state, eventPublisher)
		if err != nil {
			scopedLog.Error(err, "Failed to recycle pod", "podName", podName)

			// Track the failure in state
			permanentlyFailed := recordPodFailure(state, podName, err.Error())
			stateModified = true

			if permanentlyFailed {
				scopedLog.Info("Pod marked as permanently failed after max retries",
					"podName", podName,
					"maxRetries", MaxPodRecycleFailures)
				if eventPublisher != nil {
					eventPublisher.Warning(ctx, "PodRecycleFailed",
						fmt.Sprintf("Pod %s has failed recycling %d times and will be skipped: %v",
							podName, MaxPodRecycleFailures, err))
				}
			}

			// Continue with other pods
			continue
		}

		recycledCount++
	}

	// Persist state if we recorded any failures
	if stateModified {
		if err := persistUnifiedTransitionState(ctx, c, statefulSet, state); err != nil {
			scopedLog.Error(err, "Failed to persist updated transition state with failure info")
			// Don't fail the entire operation, continue
		}
	}

	// ============================================================
	// STEP 3: Check completion
	// ============================================================
	// Count permanently failed pods - if all non-failed pods are updated, consider complete
	permanentlyFailedCount := 0
	if state.FailedPods != nil {
		for _, failInfo := range state.FailedPods {
			if failInfo.FailCount >= MaxPodRecycleFailures {
				permanentlyFailedCount++
			}
		}
	}

	if allPodsUpdated && replicas == targetReplicas && podsBeingRecycled == 0 {
		scopedLog.Info("Unified transition complete",
			"permanentlyFailedPods", permanentlyFailedCount)
		state.FinishedAt = time.Now().Format(time.RFC3339)
		if err := persistUnifiedTransitionState(ctx, c, statefulSet, state); err != nil {
			return enterpriseApi.PhaseError, true, err
		}

		if eventPublisher != nil {
			msg := "Unified transition complete"
			if state.CPUChange != nil {
				msg += fmt.Sprintf(" - CPU: %dm->%dm, Replicas: %d->%d",
					state.CPUChange.OriginalCPUMillis, state.CPUChange.TargetCPUMillis,
					state.CPUChange.OriginalReplicas, state.CPUChange.TargetReplicas)
			}
			if state.VCTMigration != nil {
				msg += fmt.Sprintf(" - VCT: %d storage classes migrated",
					len(state.VCTMigration.ExpectedStorageClasses))
			}
			if permanentlyFailedCount > 0 {
				msg += fmt.Sprintf(" - WARNING: %d pods failed and were skipped", permanentlyFailedCount)
			}
			eventPublisher.Normal(ctx, "UnifiedTransitionComplete", msg)
		}

		return enterpriseApi.PhaseReady, true, nil
	}

	scopedLog.Info("Unified transition in progress",
		"recycledThisCycle", recycledCount,
		"podsBeingRecycled", podsBeingRecycled,
		"parallelUpdates", parallelUpdates,
		"permanentlyFailedPods", permanentlyFailedCount)

	return enterpriseApi.PhaseUpdating, true, nil
}

// handleUnifiedScaleUp handles scale-up during CPU-aware transitions with optional VCT migration.
// Uses CPU ceiling logic: add replicas while staying under the ceiling.
func handleUnifiedScaleUp(
	ctx context.Context,
	c splcommon.ControllerClient,
	statefulSet *appsv1.StatefulSet,
	mgr splcommon.StatefulSetPodManager,
	state *UnifiedTransitionState,
	parallelUpdates int32,
	eventPublisher splcommon.K8EventPublisher,
) (enterpriseApi.Phase, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("handleUnifiedScaleUp").WithValues(
		"name", statefulSet.GetName(),
		"namespace", statefulSet.GetNamespace())

	replicas := int32(1)
	if statefulSet.Spec.Replicas != nil {
		replicas = *statefulSet.Spec.Replicas
	}

	targetReplicas := state.CPUChange.TargetReplicas

	if replicas >= targetReplicas {
		// Scale-up complete
		return enterpriseApi.PhaseReady, nil
	}

	// Compute CPU ceiling: originalTotalCPU + buffer
	cpuCeiling := computeCPUCeiling(CPUAwareTransitionState{
		OriginalCPUMillis: state.CPUChange.OriginalCPUMillis,
		TargetCPUMillis:   state.CPUChange.TargetCPUMillis,
		OriginalReplicas:  state.CPUChange.OriginalReplicas,
		TargetReplicas:    state.CPUChange.TargetReplicas,
	}, parallelUpdates)

	// Compute current CPU from non-terminated pods
	cpuState := CPUAwareTransitionState{
		OriginalCPUMillis: state.CPUChange.OriginalCPUMillis,
		TargetCPUMillis:   state.CPUChange.TargetCPUMillis,
		OriginalReplicas:  state.CPUChange.OriginalReplicas,
		TargetReplicas:    state.CPUChange.TargetReplicas,
	}
	metrics, err := computeNonTerminatedCPUMetricsForScaleUp(ctx, c, statefulSet, cpuState)
	if err != nil {
		scopedLog.Error(err, "Failed to compute CPU metrics for scale-up")
		return enterpriseApi.PhaseError, err
	}

	// Calculate available room for new pods
	availableRoom := cpuCeiling - metrics.TotalPodCPU
	targetCPUPerPod := state.CPUChange.TargetCPUMillis

	scopedLog.Info("Scale-up CPU metrics",
		"currentReplicas", replicas,
		"targetReplicas", targetReplicas,
		"cpuCeiling", cpuCeiling,
		"totalPodCPU", metrics.TotalPodCPU,
		"availableRoom", availableRoom,
		"targetCPUPerPod", targetCPUPerPod)

	if availableRoom >= targetCPUPerPod {
		// Calculate how many pods we can add
		podsCanAdd := availableRoom / targetCPUPerPod
		podsNeeded := int64(targetReplicas - replicas)
		if podsCanAdd > podsNeeded {
			podsCanAdd = podsNeeded
		}
		if podsCanAdd > int64(parallelUpdates) {
			podsCanAdd = int64(parallelUpdates)
		}

		if podsCanAdd > 0 {
			newReplicas := replicas + int32(podsCanAdd)
			scopedLog.Info("Adding new pods (under CPU ceiling)",
				"podsToAdd", podsCanAdd,
				"newReplicas", newReplicas)

			*statefulSet.Spec.Replicas = newReplicas
			if err := splutil.UpdateResource(ctx, c, statefulSet); err != nil {
				scopedLog.Error(err, "Failed to update replicas for scale-up")
				return enterpriseApi.PhaseError, err
			}

			if eventPublisher != nil {
				eventPublisher.Normal(ctx, "ScalingUp",
					fmt.Sprintf("Scaling up from %d to %d replicas (target: %d) for unified transition",
						replicas, newReplicas, targetReplicas))
			}

			return enterpriseApi.PhaseScalingUp, nil
		}
	}

	// Cannot add more pods yet (need to recycle old-spec pods first)
	scopedLog.Info("Waiting to add more pods (recycling old-spec pods to free capacity)",
		"availableRoom", availableRoom,
		"targetCPUPerPod", targetCPUPerPod)

	return enterpriseApi.PhaseUpdating, nil
}

// handleUnifiedScaleDown handles scale-down during CPU-aware transitions with optional VCT migration.
// Uses CPU floor logic: ensure target pods have new spec before reducing replicas.
// Key: When deleting pods for scale-down, also delete their PVCs if VCT migration is active.
func handleUnifiedScaleDown(
	ctx context.Context,
	c splcommon.ControllerClient,
	statefulSet *appsv1.StatefulSet,
	mgr splcommon.StatefulSetPodManager,
	state *UnifiedTransitionState,
	parallelUpdates int32,
	eventPublisher splcommon.K8EventPublisher,
) (enterpriseApi.Phase, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("handleUnifiedScaleDown").WithValues(
		"name", statefulSet.GetName(),
		"namespace", statefulSet.GetNamespace())

	replicas := int32(1)
	if statefulSet.Spec.Replicas != nil {
		replicas = *statefulSet.Spec.Replicas
	}

	targetReplicas := state.CPUChange.TargetReplicas

	if replicas <= targetReplicas {
		// Scale-down complete
		return enterpriseApi.PhaseReady, nil
	}

	// First, ensure all remaining pods have new spec before we remove replicas
	// This maintains CPU floor during transition
	for n := int32(0); n < targetReplicas; n++ {
		podName := fmt.Sprintf("%s-%d", statefulSet.GetName(), n)
		pod := &corev1.Pod{}
		podNamespacedName := types.NamespacedName{
			Namespace: statefulSet.GetNamespace(),
			Name:      podName,
		}

		if err := c.Get(ctx, podNamespacedName, pod); err != nil {
			if k8serrors.IsNotFound(err) {
				// Pod being created
				scopedLog.Info("Pod being created, waiting", "podName", podName)
				return enterpriseApi.PhaseUpdating, nil
			}
			return enterpriseApi.PhaseError, err
		}

		if !isPodReady(pod) {
			// Pod not ready yet
			scopedLog.Info("Pod not ready, waiting", "podName", podName)
			return enterpriseApi.PhaseUpdating, nil
		}

		updated, err := isPodFullyUpdated(ctx, c, pod, statefulSet, state)
		if err != nil {
			return enterpriseApi.PhaseError, err
		}

		if !updated {
			// Need to wait for all target pods to be updated first
			scopedLog.Info("Waiting for pod to be updated before scale-down", "podName", podName)
			return enterpriseApi.PhaseUpdating, nil
		}
	}

	// All target pods have new spec - safe to reduce replicas
	// Calculate how many replicas to remove
	replicasToRemove := replicas - targetReplicas
	if replicasToRemove > parallelUpdates {
		replicasToRemove = parallelUpdates
	}

	// Delete PVCs for pods that will be removed (if VCT migration is active)
	if state.VCTMigration != nil {
		for n := replicas - replicasToRemove; n < replicas; n++ {
			podName := fmt.Sprintf("%s-%d", statefulSet.GetName(), n)

			// Delete PVCs for this pod
			for vctName := range state.VCTMigration.ExpectedStorageClasses {
				pvcName := fmt.Sprintf("%s-%s", vctName, podName)
				pvc := &corev1.PersistentVolumeClaim{}
				pvcNamespacedName := types.NamespacedName{
					Namespace: statefulSet.GetNamespace(),
					Name:      pvcName,
				}

				if err := c.Get(ctx, pvcNamespacedName, pvc); err != nil {
					if k8serrors.IsNotFound(err) {
						continue
					}
					return enterpriseApi.PhaseError, err
				}

				scopedLog.Info("Deleting PVC for scale-down", "pvcName", pvcName)
				if err := c.Delete(ctx, pvc); err != nil && !k8serrors.IsNotFound(err) {
					return enterpriseApi.PhaseError, err
				}

				if eventPublisher != nil {
					eventPublisher.Normal(ctx, "PVCDeleted",
						fmt.Sprintf("Deleted PVC %s during scale-down", pvcName))
				}
			}
		}
	}

	newReplicas := replicas - replicasToRemove

	scopedLog.Info("Scaling down for unified transition",
		"currentReplicas", replicas,
		"newReplicas", newReplicas,
		"targetReplicas", targetReplicas)

	// Update StatefulSet replicas
	*statefulSet.Spec.Replicas = newReplicas
	if err := splutil.UpdateResource(ctx, c, statefulSet); err != nil {
		scopedLog.Error(err, "Failed to update replicas for scale-down")
		return enterpriseApi.PhaseError, err
	}

	if eventPublisher != nil {
		eventPublisher.Normal(ctx, "ScalingDown",
			fmt.Sprintf("Scaling down from %d to %d replicas (target: %d) for unified transition",
				replicas, newReplicas, targetReplicas))
	}

	return enterpriseApi.PhaseScalingDown, nil
}
