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

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ReasonPodTerminalFailure is the machine-readable reason used when a pod is
	// stuck in a non-recoverable terminal state that requires manual remediation.
	ReasonPodTerminalFailure = "PodTerminalFailure"
)

// DefaultStatefulSetPodManager is a simple StatefulSetPodManager that does nothing
type DefaultStatefulSetPodManager struct{}

// statefulSetPodUpdateReadinessManager is an optional contract for a
// StatefulSetPodManager that deliberately makes one Pod not ready before an
// Operator-owned OnDelete replacement. Implementations must fail closed and
// allow progress only when the not-ready state is part of the active,
// persisted Pod update operation.
type statefulSetPodUpdateReadinessManager interface {
	CanProceedWithPodUpdateDespiteNotReadyReplicas(
		context.Context,
		*appsv1.StatefulSet,
		int32,
	) (bool, error)
}

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

// terminalPodWaitingReasons is the set of container Waiting.Reason values that
// indicate a non-recoverable pod failure. A container stuck in one of these states
// will never start without external intervention (correcting the image reference,
// creating the missing Secret/ConfigMap, etc.).
var terminalPodWaitingReasons = map[string]bool{
	"ErrImagePull":               true, // image pull failed (bad credentials, unreachable registry)
	"ImagePullBackOff":           true, // kubelet backing off after repeated pull failures
	"InvalidImageName":           true, // image reference is syntactically malformed
	"ErrInvalidImage":            true, // image reference resolves but is not a valid image
	"CreateContainerConfigError": true, // env-var or volume references a missing ConfigMap/Secret key
	"CreateContainerError":       true, // OCI runtime cannot create container (missing device, seccomp profile, etc.)
	"RunContainerError":          true, // OCI runtime cannot run container (invalid entrypoint, missing binary)
}

// checkPodsForTerminalFailures lists pods belonging to statefulSet and returns a
// descriptive error if any container or init-container is stuck in a terminal
// waiting state. Returns nil if all pods appear healthy or if the List call
// fails (treated as transient so it does not interrupt reconciliation).
func checkPodsForTerminalFailures(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet) error {
	if statefulSet.Spec.Selector == nil {
		return nil
	}
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(statefulSet.GetNamespace()),
		client.MatchingLabels(statefulSet.Spec.Selector.MatchLabels),
	}
	if err := c.List(ctx, podList, listOpts...); err != nil {
		return nil // transient API error; don't interrupt reconciliation
	}
	for i := range podList.Items {
		pod := &podList.Items[i]
		for _, cs := range pod.Status.InitContainerStatuses {
			if cs.State.Waiting != nil && terminalPodWaitingReasons[cs.State.Waiting.Reason] {
				return fmt.Errorf("pod %s init-container %q in terminal state %s: %s",
					pod.Name, cs.Name, cs.State.Waiting.Reason, cs.State.Waiting.Message)
			}
		}
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Waiting != nil && terminalPodWaitingReasons[cs.State.Waiting.Reason] {
				return fmt.Errorf("pod %s container %q in terminal state %s: %s",
					pod.Name, cs.Name, cs.State.Waiting.Reason, cs.State.Waiting.Message)
			}
		}
	}
	return nil
}

// CheckPodsForTerminalFailures is the exported form of checkPodsForTerminalFailures.
// It returns a splcommon.TerminalError (which satisfies errors.Is(err, reconcile.TerminalError(nil)))
// if any pod belonging to statefulSet is stuck in a non-recoverable container waiting state,
// or nil otherwise. Callers only need to propagate the returned error.
func CheckPodsForTerminalFailures(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet) error {
	if termErr := checkPodsForTerminalFailures(ctx, c, statefulSet); termErr != nil {
		return splcommon.NewTerminalError(ReasonPodTerminalFailure, "Pod stuck in terminal state — manual fix required", termErr)
	}
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

	// Canonicalize the desired Pod template before computing or persisting any
	// material update. The slice comparison helpers sort equal-length slices
	// in place, but return early when lengths differ. Without this step, adding
	// an environment variable can persist an unsorted template revision; a
	// later strategy-only partition update then persists the sorted order as a
	// second, unintended Pod revision after rollout authorization.
	if err := SortStatefulSetSlices(
		ctx,
		&revised.Spec.Template.Spec,
		revised.GetObjectMeta().GetName(),
	); err != nil {
		return enterpriseApi.PhaseError, err
	}

	// check for changes in Pod template
	desiredUpdateStrategy := revised.Spec.UpdateStrategy
	hasUpdateStrategyChanges := !reflect.DeepEqual(
		current.Spec.UpdateStrategy,
		desiredUpdateStrategy,
	)
	hasUpdates := MergePodUpdates(ctx, &current.Spec.Template, &revised.Spec.Template, current.GetObjectMeta().GetName())
	*revised = current // caller expects that object passed represents latest state
	if hasUpdateStrategyChanges {
		revised.Spec.UpdateStrategy = desiredUpdateStrategy
	}

	// only update if there are material differences, as determined by comparison function
	if hasUpdates || hasUpdateStrategyChanges {
		// Persist the desired Pod template and replacement policy together. OnDelete
		// retains Operator-owned replacement. A partitioned RollingUpdate makes only
		// ordinals authorized by its partition eligible for Kubernetes replacement.
		// Replicas remain managed below by UpdateStatefulSetPods.

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

// UpdateStatefulSetPods manages scaling and config updates for StatefulSets
func UpdateStatefulSetPods(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, mgr splcommon.StatefulSetPodManager, desiredReplicas int32) (enterpriseApi.Phase, error) {
	scopedLog := logging.FromContext(ctx).With("func", "UpdateStatefulSetPods",
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	// Re-fetch the StatefulSet to ensure we have the latest status, especially UpdateRevision.
	// This addresses a race condition where the StatefulSet controller may not have updated
	// Status.UpdateRevision yet after a spec change was applied. Without this re-fetch,
	// we might incorrectly report PhaseReady when pods actually need to be recycled.
	namespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: statefulSet.GetName()}
	err := c.Get(ctx, namespacedName, statefulSet)
	if err != nil {
		scopedLog.ErrorContext(ctx, "unable to re-fetch StatefulSet for latest status", "error", err)
		return enterpriseApi.PhaseError, err
	}

	// wait for all replicas ready
	replicas := *statefulSet.Spec.Replicas
	readyReplicas := statefulSet.Status.ReadyReplicas
	canProceedWithPodUpdate := false
	if readyReplicas < replicas {
		if readinessManager, ok := mgr.(statefulSetPodUpdateReadinessManager); ok {
			canProceedWithPodUpdate, err =
				readinessManager.CanProceedWithPodUpdateDespiteNotReadyReplicas(
					ctx,
					statefulSet,
					desiredReplicas,
				)
			if err != nil {
				scopedLog.ErrorContext(
					ctx,
					"unable to validate intentional Pod readiness withdrawal",
					"error",
					err,
				)
				return enterpriseApi.PhaseError, err
			}
		}
		if canProceedWithPodUpdate {
			scopedLog.InfoContext(
				ctx,
				"continuing Operator-owned Pod update after verified readiness withdrawal",
				"readyReplicas",
				readyReplicas,
				"replicas",
				replicas,
			)
		} else {
			scopedLog.InfoContext(ctx, "waiting for pods to become ready")
		}
		// Detect terminal container states (wrong image, inaccessible registry, missing
		// ConfigMap/Secret key) that will never self-heal. Surface them as PhaseError
		// immediately rather than waiting for the full reconcile timeout.
		if termErr := checkPodsForTerminalFailures(ctx, c, statefulSet); termErr != nil {
			scopedLog.ErrorContext(ctx, "terminal pod failure detected; setting PhaseError", "error", termErr)
			return enterpriseApi.PhaseError, splcommon.NewTerminalError(ReasonPodTerminalFailure, "Pod stuck in terminal state — manual fix required", termErr)
		}
		if !canProceedWithPodUpdate {
			if readyReplicas > 0 {
				return enterpriseApi.PhaseScalingUp, nil
			}
			return enterpriseApi.PhasePending, nil
		}
	} else if readyReplicas > replicas {
		scopedLog.InfoContext(ctx, "waiting for scale down to complete")
		return enterpriseApi.PhaseScalingDown, nil
	}

	// readyReplicas == replicas

	// check for scaling up
	if readyReplicas < desiredReplicas && !canProceedWithPodUpdate {
		// scale up StatefulSet to match desiredReplicas
		scopedLog.InfoContext(ctx, "scaling replicas up", "replicas", desiredReplicas)
		*statefulSet.Spec.Replicas = desiredReplicas
		return enterpriseApi.PhaseScalingUp, splutil.UpdateResource(ctx, c, statefulSet)
	}

	// check for scaling down
	if readyReplicas > desiredReplicas {
		// prepare pod for removal via scale down
		n := readyReplicas - 1
		podName := fmt.Sprintf("%s-%d", statefulSet.GetName(), n)
		ready, err := mgr.PrepareScaleDown(ctx, n)
		if err != nil {
			scopedLog.ErrorContext(ctx, "unable to decommission Pod", "podName", podName, "error", err)
			return enterpriseApi.PhaseError, err
		}
		if !ready {
			// wait until pod quarantine has completed before deleting it
			return enterpriseApi.PhaseScalingDown, nil
		}

		// scale down statefulset to terminate pod
		scopedLog.InfoContext(ctx, "scaling replicas down", "replicas", n)
		*statefulSet.Spec.Replicas = n
		err = splutil.UpdateResource(ctx, c, statefulSet)
		if err != nil {
			scopedLog.ErrorContext(ctx, "scale down update failed for StatefulSet", "error", err)
			return enterpriseApi.PhaseError, err
		}

		// delete PVCs used by the pod so that a future scale up will have clean state
		for _, vol := range statefulSet.Spec.VolumeClaimTemplates {
			namespacedName := types.NamespacedName{
				Namespace: vol.ObjectMeta.Namespace,
				Name:      fmt.Sprintf("%s-%s", vol.ObjectMeta.Name, podName),
			}
			var pvc corev1.PersistentVolumeClaim
			err := c.Get(ctx, namespacedName, &pvc)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					continue
				}
				scopedLog.ErrorContext(ctx, "unable to find PVC for deletion", "pvcName", pvc.ObjectMeta.Name, "error", err)
				return enterpriseApi.PhaseError, err
			}
			scopedLog.InfoContext(ctx, "deleting PVC", "pvcName", pvc.ObjectMeta.Name)
			err = c.Delete(ctx, &pvc)
			if err != nil {
				scopedLog.ErrorContext(ctx, "unable to delete PVC", "pvcName", pvc.ObjectMeta.Name, "error", err)
				return enterpriseApi.PhaseError, err
			}
		}

		return enterpriseApi.PhaseScalingDown, nil
	}

	// ready and no StatefulSet scaling is required
	// readyReplicas == desiredReplicas

	// check existing pods for desired updates
	podTraversalReplicas := readyReplicas
	if canProceedWithPodUpdate {
		// The one intentionally withdrawn target still exists and must remain
		// part of the highest-to-lowest OnDelete traversal.
		podTraversalReplicas = replicas
	}
	for n := podTraversalReplicas - 1; n >= 0; n-- {
		// get Pod
		podName := fmt.Sprintf("%s-%d", statefulSet.GetName(), n)
		namespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: podName}
		var pod corev1.Pod
		err := c.Get(ctx, namespacedName, &pod)
		if err != nil {
			scopedLog.ErrorContext(ctx, "unable to find Pod", "podName", podName, "error", err)
			return enterpriseApi.PhaseError, err
		}
		if pod.Status.Phase != corev1.PodRunning || len(pod.Status.ContainerStatuses) == 0 || !pod.Status.ContainerStatuses[0].Ready {
			if termErr := checkPodsForTerminalFailures(ctx, c, statefulSet); termErr != nil {
				scopedLog.ErrorContext(ctx, "terminal pod failure detected during update; setting PhaseError", "error", termErr)
				return enterpriseApi.PhaseError, splcommon.NewTerminalError(ReasonPodTerminalFailure, "Pod stuck in terminal state — manual fix required", termErr)
			}
			scopedLog.ErrorContext(ctx, "waiting for Pod to become ready", "podName", podName, "error", err)
			return enterpriseApi.PhaseUpdating, err
		}

		// terminate pod if it has pending updates; k8s will start a new one with revised template
		if statefulSet.Status.UpdateRevision != "" && statefulSet.Status.UpdateRevision != pod.GetLabels()["controller-revision-hash"] {
			// pod needs to be updated; first, prepare it to be recycled
			ready, err := mgr.PrepareRecycle(ctx, n)
			if err != nil {
				scopedLog.ErrorContext(ctx, "unable to prepare Pod for recycling", "podName", podName, "error", err)
				return enterpriseApi.PhaseError, err
			}
			if !ready {
				// wait until pod quarantine has completed before deleting it
				return enterpriseApi.PhaseUpdating, nil
			}

			// deleting pod will cause StatefulSet controller to create a new one with latest template
			scopedLog.InfoContext(ctx, "recycling Pod for updates", "podName", podName,
				"statefulSetRevision", statefulSet.Status.UpdateRevision,
				"podRevision", pod.GetLabels()["controller-revision-hash"])
			preconditions := client.Preconditions{UID: &pod.ObjectMeta.UID, ResourceVersion: &pod.ObjectMeta.ResourceVersion}
			err = c.Delete(context.Background(), &pod, preconditions)
			if err != nil {
				scopedLog.ErrorContext(ctx, "unable to delete Pod", "podName", podName, "error", err)
				return enterpriseApi.PhaseError, err
			}

			// only delete one at a time
			return enterpriseApi.PhaseUpdating, nil
		}

		// check if pod was previously prepared for recycling; if so, complete
		complete, err := mgr.FinishRecycle(ctx, n)
		if err != nil {
			scopedLog.ErrorContext(ctx, "unable to complete recycling of pod", "podName", podName, "error", err)
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
	scopedLog.InfoContext(ctx, "all pods are ready")

	// Finalize rolling upgrade process
	// It uses first pod to get a client
	err = mgr.FinishUpgrade(ctx, 0)
	if err != nil {
		scopedLog.ErrorContext(ctx, "unable to finalize rolling upgrade process", "error", err)
		return enterpriseApi.PhaseError, err
	}

	scopedLog.InfoContext(ctx, "statefulset - Phase Ready")

	return enterpriseApi.PhaseReady, nil
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
	scopedLog := logging.FromContext(ctx).With("func", "RemoveUnwantedOwnerRefSs", "statefulSet", namespacedName)

	scopedLog.InfoContext(ctx, "removing unwanted owner references on CR deletion")

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
	scopedLog := logging.FromContext(ctx).With("func", "isScalingUp", "name", cr.GetName(), "namespace", cr.GetNamespace())

	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: name}
	current, err := GetStatefulSetByName(ctx, client, namespacedName)
	if err != nil {
		scopedLog.ErrorContext(ctx, "unable to get current stateful set", "name", namespacedName, "error", err)
		return enterpriseApi.StatefulSetNotScaling, err
	}

	if *current.Spec.Replicas < desiredReplicas {
		return enterpriseApi.StatefulSetScalingUp, nil
	} else if *current.Spec.Replicas > desiredReplicas {
		return enterpriseApi.StatefulSetScalingDown, nil
	}

	return enterpriseApi.StatefulSetNotScaling, nil
}
