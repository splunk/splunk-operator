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

package controller

import (
	"context"
	"fmt"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"reflect"

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

	// wait for all replicas ready
	replicas := *statefulSet.Spec.Replicas
	readyReplicas := statefulSet.Status.ReadyReplicas
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
				Namespace: vol.ObjectMeta.Namespace,
				Name:      fmt.Sprintf("%s-%s", vol.ObjectMeta.Name, podName),
			}
			var pvc corev1.PersistentVolumeClaim
			err := c.Get(ctx, namespacedName, &pvc)
			if err != nil {
				scopedLog.Error(err, "Unable to find PVC for deletion", "pvcName", pvc.ObjectMeta.Name)
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

	// ready and no StatefulSet scaling is required
	// readyReplicas == desiredReplicas

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
		if pod.Status.Phase != corev1.PodRunning || len(pod.Status.ContainerStatuses) == 0 || pod.Status.ContainerStatuses[0].Ready != true {
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
	if err != nil {
		return err
	}

	return nil
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

//isCurrentCROwner returns true if current CR is the ONLY owner of the automated MC
func isCurrentCROwner(cr splcommon.MetaObject, currentOwners []metav1.OwnerReference) bool {
	return reflect.DeepEqual(currentOwners[0].UID, cr.GetUID())
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
