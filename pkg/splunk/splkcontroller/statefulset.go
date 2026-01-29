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

// UpdateStatefulSetPods manages scaling and config updates for StatefulSets
func UpdateStatefulSetPods(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, mgr splcommon.StatefulSetPodManager, desiredReplicas int32) (enterpriseApi.Phase, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("UpdateStatefulSetPods").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	replicas := *statefulSet.Spec.Replicas
	readyReplicas := statefulSet.Status.ReadyReplicas

	// CRITICAL: Check for scaling FIRST before waiting for pods to be ready
	// This ensures we detect when CR spec changes (e.g., replicas: 3 -> 2)
	scopedLog.Info("UpdateStatefulSetPods called",
		"currentReplicas", replicas,
		"desiredReplicas", desiredReplicas,
		"readyReplicas", readyReplicas)

	// check for scaling up
	if readyReplicas < desiredReplicas {
		// scale up StatefulSet to match desiredReplicas
		scopedLog.Info("Scaling replicas up", "replicas", desiredReplicas)
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
			scopedLog.Error(err, "Unable to decommission Pod", "podName", podName)
			return enterpriseApi.PhaseError, err
		}
		if !ready {
			// wait until pod quarantine has completed before deleting it
			return enterpriseApi.PhaseScalingDown, nil
		}

		// V3 FIX #1: Mark pods with scale-down intent BEFORE scaling down
		// This ensures the finalizer handler can reliably detect scale-down vs restart
		// Inline implementation to avoid import cycle (enterprise -> splkcontroller -> enterprise)
		err = markPodForScaleDown(ctx, c, statefulSet, n)
		if err != nil {
			scopedLog.Error(err, "Failed to mark pod for scale-down", "newReplicas", n)
			// Don't fail - fall back to ordinal comparison in finalizer
		}

		// scale down statefulset to terminate pod
		scopedLog.Info("Scaling replicas down", "replicas", n)
		*statefulSet.Spec.Replicas = n
		err = splutil.UpdateResource(ctx, c, statefulSet)
		if err != nil {
			scopedLog.Error(err, "Scale down update failed for StatefulSet")
			return enterpriseApi.PhaseError, err
		}

		// V3 FIX #3: PVC deletion removed - handled by finalizer synchronously
		// The pod finalizer will delete PVCs before allowing pod termination
		// This ensures PVCs are always deleted even if operator crashes

		return enterpriseApi.PhaseScalingDown, nil
	}

	// No scaling needed: readyReplicas == desiredReplicas
	// But we need to wait for StatefulSet to stabilize at the desired count

	// Wait for StatefulSet.Spec.Replicas to match desiredReplicas (should be updated now)
	// and wait for all desired pods to be ready
	if readyReplicas < desiredReplicas {
		scopedLog.Info("Waiting for pods to become ready during scale-up",
			"ready", readyReplicas,
			"desired", desiredReplicas)
		return enterpriseApi.PhaseScalingUp, nil
	}

	if readyReplicas > desiredReplicas {
		scopedLog.Info("Waiting for scale-down to complete",
			"ready", readyReplicas,
			"desired", desiredReplicas)
		return enterpriseApi.PhaseScalingDown, nil
	}

	// readyReplicas == desiredReplicas - all pods are ready

	// Check if using RollingUpdate strategy
	// With RollingUpdate, Kubernetes automatically handles pod updates + preStop hooks + finalizers handle cleanup
	if statefulSet.Spec.UpdateStrategy.Type == appsv1.RollingUpdateStatefulSetStrategyType {
		scopedLog.Info("RollingUpdate strategy detected - letting Kubernetes handle pod updates")

		// Check if update is in progress
		if statefulSet.Status.UpdatedReplicas < statefulSet.Status.Replicas {
			scopedLog.Info("RollingUpdate in progress",
				"updated", statefulSet.Status.UpdatedReplicas,
				"total", statefulSet.Status.Replicas)
			return enterpriseApi.PhaseUpdating, nil
		}

		// All pods updated, call FinishUpgrade for post-upgrade tasks
		err := mgr.FinishUpgrade(ctx, 0)
		if err != nil {
			scopedLog.Error(err, "Unable to finalize rolling upgrade process")
			return enterpriseApi.PhaseError, err
		}

		return enterpriseApi.PhaseReady, nil
	}

	// For OnDelete strategy, continue with manual pod management
	scopedLog.Info("OnDelete strategy detected - using manual pod management")

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
			err = c.Delete(context.Background(), &pod, preconditions)
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
	err := splutil.RemoveUnwantedSecrets(ctx, c, statefulSet.GetName(), statefulSet.GetNamespace())
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

// markPodForScaleDown updates the intent annotation on the pod that will be deleted
// This is called before scaling down to mark the pod with scale-down intent
// Inline version to avoid import cycle with enterprise package
func markPodForScaleDown(ctx context.Context, c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, newReplicas int32) error {
	scopedLog := log.FromContext(ctx).WithName("markPodForScaleDown")

	// Mark the pod that will be deleted (ordinal = newReplicas)
	podName := fmt.Sprintf("%s-%d", statefulSet.Name, newReplicas)
	pod := &corev1.Pod{}
	err := c.Get(ctx, types.NamespacedName{
		Name:      podName,
		Namespace: statefulSet.Namespace,
	}, pod)

	if err != nil {
		if k8serrors.IsNotFound(err) {
			scopedLog.Info("Pod already deleted, skipping", "pod", podName)
			return nil
		}
		return fmt.Errorf("failed to get pod %s: %w", podName, err)
	}

	// Update intent annotation
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}

	// Only update if annotation is different
	if pod.Annotations["splunk.com/pod-intent"] != "scale-down" {
		pod.Annotations["splunk.com/pod-intent"] = "scale-down"
		scopedLog.Info("Marking pod for scale-down", "pod", podName)

		if err := c.Update(ctx, pod); err != nil {
			return fmt.Errorf("failed to update pod %s annotation: %w", podName, err)
		}
	}

	return nil
}
