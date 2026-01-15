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
	"reflect"
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
)

// DefaultStatefulSetPodManager is a simple StatefulSetPodManager that does nothing
type DefaultStatefulSetPodManager struct{}

// Update for DefaultStatefulSetPodManager handles all updates for a statefulset of standard pods
func (mgr *DefaultStatefulSetPodManager) Update(ctx context.Context, client splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (enterpriseApi.Phase, error) {
	phase, err := ApplyStatefulSet(ctx, client, statefulSet)
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
func ApplyStatefulSet(ctx context.Context, c splcommon.ControllerClient, revised *appsv1.StatefulSet) (enterpriseApi.Phase, error) {
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

	// check for changes in Pod template
	hasUpdates := MergePodUpdates(ctx, &current.Spec.Template, &revised.Spec.Template, current.GetObjectMeta().GetName())

	// check for changes in StatefulSet-level metadata (labels and annotations)
	hasUpdates = hasUpdates || splcommon.MergeStatefulSetMetaUpdates(ctx, &current.ObjectMeta, &revised.ObjectMeta, current.GetName())

	*revised = current // caller expects that object passed represents latest state

	// only update if there are material differences, as determined by comparison function
	if hasUpdates {
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
		namespacedName := types.NamespacedName{
			Namespace: statefulSet.GetNamespace(),
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
			scopedLog.Error(err, "Waiting for Pod to become ready", "podName", podName)
			return enterpriseApi.PhaseUpdating, err
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

			// deleting pod will cause StatefulSet controller to create a new one with latest template
			scopedLog.Info("Recycling Pod for updates", "podName", podName,
				"statefulSetRevision", statefulSet.Status.UpdateRevision,
				"podRevision", pod.GetLabels()["controller-revision-hash"])
			preconditions := client.Preconditions{UID: &pod.ObjectMeta.UID, ResourceVersion: &pod.ObjectMeta.ResourceVersion}
			err = c.Delete(ctx, &pod, preconditions)
			if err != nil {
				scopedLog.Error(err, "Unable to delete Pod", "podName", podName)
				return enterpriseApi.PhaseError, err
			}

			// only delete one at a time
			return enterpriseApi.PhaseUpdating, nil
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
