// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
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
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

// DefaultStatefulSetPodManager is a simple StatefulSetPodManager that does nothing
type DefaultStatefulSetPodManager struct{}

// Update for DefaultStatefulSetPodManager handles all updates for a statefulset of standard pods
func (mgr *DefaultStatefulSetPodManager) Update(client splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (splcommon.Phase, error) {
	phase, err := ApplyStatefulSet(client, statefulSet)
	skipRecheckUpdate := false
	if err == nil && phase == splcommon.PhaseReady {
		phase, err = UpdateStatefulSetPods(client, statefulSet, mgr, desiredReplicas, &skipRecheckUpdate)
	}
	return phase, err
}

// PrepareScaleDown for DefaultStatefulSetPodManager does nothing and returns true
func (mgr *DefaultStatefulSetPodManager) PrepareScaleDown(n int32) (bool, error) {
	return true, nil
}

// PrepareRecycle for DefaultStatefulSetPodManager does nothing and returns true
func (mgr *DefaultStatefulSetPodManager) PrepareRecycle(n int32) (bool, error) {
	return true, nil
}

// FinishRecycle for DefaultStatefulSetPodManager does nothing and returns false
func (mgr *DefaultStatefulSetPodManager) FinishRecycle(n int32) (bool, error) {
	return true, nil
}

// ApplyStatefulSet creates or updates a Kubernetes StatefulSet
func ApplyStatefulSet(c splcommon.ControllerClient, revised *appsv1.StatefulSet) (splcommon.Phase, error) {
	namespacedName := types.NamespacedName{Namespace: revised.GetNamespace(), Name: revised.GetName()}
	var current appsv1.StatefulSet

	err := c.Get(context.TODO(), namespacedName, &current)
	if err != nil {
		// no StatefulSet exists -> just create a new one
		err = splutil.CreateResource(c, revised)
		return splcommon.PhasePending, err
	}

	// found an existing StatefulSet

	// check for changes in Pod template
	hasUpdates := MergePodUpdates(&current.Spec.Template, &revised.Spec.Template, current.GetObjectMeta().GetName())
	*revised = current // caller expects that object passed represents latest state

	// only update if there are material differences, as determined by comparison function
	if hasUpdates {
		// this updates the desired state template, but doesn't actually modify any pods
		// because we use an "OnUpdate" strategy https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#update-strategies
		// note also that this ignores Replicas, which is handled below by UpdateStatefulSetPods
		return splcommon.PhaseUpdating, splutil.UpdateResource(c, revised)
	}

	// scaling and pod updates are handled by UpdateStatefulSetPods
	return splcommon.PhaseReady, nil
}

// UpdatePodRevisionHash updates the controller-revision-hash label on pods.
func UpdatePodRevisionHash(c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, readyReplicas int32) error {
	scopedLog := log.WithName("updatePodRevisionHash").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	namespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: statefulSet.GetName()}
	var current appsv1.StatefulSet

	err := c.Get(context.TODO(), namespacedName, &current)
	if err != nil {
		scopedLog.Error(err, "Unable to Get statefulset", "statefulset", statefulSet.GetName())
	}

	for n := readyReplicas - 1; n >= 0; n-- {
		// get Pod
		podName := fmt.Sprintf("%s-%d", current.GetName(), n)
		namespacedName := types.NamespacedName{Namespace: current.GetNamespace(), Name: podName}
		var pod corev1.Pod
		err := c.Get(context.TODO(), namespacedName, &pod)
		if err != nil {
			scopedLog.Error(err, "Unable to find Pod", "podName", podName)
			return err
		}
		if pod.Status.Phase != corev1.PodRunning || len(pod.Status.ContainerStatuses) == 0 || pod.Status.ContainerStatuses[0].Ready != true {
			scopedLog.Error(err, "Waiting for Pod to become ready", "podName", podName)
			return err
		}

		labels := pod.GetLabels()
		revisionHash := labels["controller-revision-hash"]
		// update pod controller revision hash
		if current.Status.UpdateRevision != "" && current.Status.UpdateRevision != revisionHash {
			// update controller-revision-hash label for pod with statefulset update revision
			labels["controller-revision-hash"] = current.Status.UpdateRevision
			pod.SetLabels(labels)
			err = splutil.UpdateResource(c, &pod)
			if err != nil {
				scopedLog.Error(err, "Unable to update controller-revision-hash label for pod", "podName", podName)
				return err
			}
			scopedLog.Info("Updated controller-revision-hash label for pod", "podName", podName, "revision", current.Status.UpdateRevision)
		}
	}

	scopedLog.Info("Updated controller-revision-hash label for all pods")
	return nil
}

// isRevisionUpdateSuccessful checks if current revision is different from updated revision
func isRevisionUpdateSuccessful(c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet) bool {
	scopedLog := log.WithName("isUpdateSuccessful").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	namespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: statefulSet.GetName()}
	var current appsv1.StatefulSet

	err := c.Get(context.TODO(), namespacedName, &current)
	if err != nil {
		scopedLog.Error(err, "Unable to Get statefulset", "statefulset", statefulSet.GetName())
		return false
	}

	// Check if current revision is different from update revision
	if current.Status.CurrentRevision == current.Status.UpdateRevision {
		scopedLog.Error(err, "Statefulset UpdateRevision not updated yet")
		return false
	}

	return true
}

// checkAndUpdatePodRevision updates the pod revision hash labels on pods if statefulset update was successful
func checkAndUpdatePodRevision(c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, readyReplicas int32, skipRecheckUpdate *bool) (bool, error) {
	scopedLog := log.WithName("UpdateStatefulSetPods").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())
	var err error
	if !isRevisionUpdateSuccessful(c, statefulSet) {
		scopedLog.Error(err, "Statefulset not updated yet")
		*skipRecheckUpdate = false
		return false, err
	}
	// update the controller-revision-hash label on pods to
	// to avoid unnecessary recycle of pods
	err = UpdatePodRevisionHash(c, statefulSet, readyReplicas)
	if err != nil {
		scopedLog.Error(err, "Unable to update pod-revision-hash for the pods")
		*skipRecheckUpdate = false
		return false, err
	}
	*skipRecheckUpdate = true
	return true, nil
}

// UpdateStatefulSetPods manages scaling and config updates for StatefulSets
func UpdateStatefulSetPods(c splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, mgr splcommon.StatefulSetPodManager, desiredReplicas int32, skipRecheckUpdate *bool) (splcommon.Phase, error) {
	scopedLog := log.WithName("UpdateStatefulSetPods").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	// wait for all replicas ready
	replicas := *statefulSet.Spec.Replicas
	readyReplicas := statefulSet.Status.ReadyReplicas
	if readyReplicas < replicas {
		scopedLog.Info("Waiting for pods to become ready")
		if readyReplicas > 0 {
			if !*skipRecheckUpdate {
				ret, err := checkAndUpdatePodRevision(c, statefulSet, readyReplicas, skipRecheckUpdate)
				if !ret || err != nil {
					scopedLog.Error(err, "Unable to update pod-revision-hash for the pods")
					return splcommon.PhaseError, err
				}
			}
			return splcommon.PhaseScalingUp, nil
		}
		return splcommon.PhasePending, nil
	} else if readyReplicas > replicas {
		scopedLog.Info("Waiting for scale down to complete")
		if !*skipRecheckUpdate {
			ret, err := checkAndUpdatePodRevision(c, statefulSet, readyReplicas-1, skipRecheckUpdate)
			if !ret || err != nil {
				scopedLog.Error(err, "Unable to update pod-revision-hash for the pods")
				return splcommon.PhaseError, err
			}
		}
		return splcommon.PhaseScalingDown, nil
	}

	// readyReplicas == replicas

	// check for scaling up
	if readyReplicas < desiredReplicas {
		// scale up StatefulSet to match desiredReplicas
		scopedLog.Info("Scaling replicas up", "replicas", desiredReplicas)
		*statefulSet.Spec.Replicas = desiredReplicas
		err := splutil.UpdateResource(c, statefulSet)
		if err != nil {
			scopedLog.Error(err, "Unable to update statefulset")
			return splcommon.PhaseError, err
		}

		// Check if the revision was updated successfully for Statefulset.
		// It so can happen that it may take few seconds for the update to be
		// reflected in the resource. In that case, just return from here and
		// check the status back in the next reconcile loop.
		var ret bool
		ret, err = checkAndUpdatePodRevision(c, statefulSet, readyReplicas, skipRecheckUpdate)
		if !ret || err != nil {
			scopedLog.Error(err, "Unable to update pod-revision-hash for the pods")
			return splcommon.PhaseError, err
		}
		return splcommon.PhaseScalingUp, err
	}

	// check for scaling down
	if readyReplicas > desiredReplicas {
		// prepare pod for removal via scale down
		n := readyReplicas - 1
		podName := fmt.Sprintf("%s-%d", statefulSet.GetName(), n)
		ready, err := mgr.PrepareScaleDown(n)
		if err != nil {
			scopedLog.Error(err, "Unable to decommission Pod", "podName", podName)
			return splcommon.PhaseError, err
		}
		if !ready {
			// wait until pod quarantine has completed before deleting it
			return splcommon.PhaseScalingDown, nil
		}

		// scale down statefulset to terminate pod
		scopedLog.Info("Scaling replicas down", "replicas", n)
		*statefulSet.Spec.Replicas = n
		err = splutil.UpdateResource(c, statefulSet)
		if err != nil {
			scopedLog.Error(err, "Scale down update failed for StatefulSet")
			return splcommon.PhaseError, err
		}

		// Check if the revision was updated successfully for Statefulset.
		// It so can happen that it may take few seconds for the update to be
		// reflected in the resource. In that case, just return from here and
		// check the status back in the next reconcile loop.
		var ret bool
		ret, err = checkAndUpdatePodRevision(c, statefulSet, readyReplicas-1, skipRecheckUpdate)
		if !ret || err != nil {
			scopedLog.Error(err, "Unable to update pod-revision-hash for the pods")
			return splcommon.PhaseError, err
		}

		// delete PVCs used by the pod so that a future scale up will have clean state
		for _, vol := range statefulSet.Spec.VolumeClaimTemplates {
			namespacedName := types.NamespacedName{
				Namespace: vol.ObjectMeta.Namespace,
				Name:      fmt.Sprintf("%s-%s", vol.ObjectMeta.Name, podName),
			}
			var pvc corev1.PersistentVolumeClaim
			err := c.Get(context.TODO(), namespacedName, &pvc)
			if err != nil {
				scopedLog.Error(err, "Unable to find PVC for deletion", "pvcName", pvc.ObjectMeta.Name)
				return splcommon.PhaseError, err
			}
			log.Info("Deleting PVC", "pvcName", pvc.ObjectMeta.Name)
			err = c.Delete(context.Background(), &pvc)
			if err != nil {
				scopedLog.Error(err, "Unable to delete PVC", "pvcName", pvc.ObjectMeta.Name)
				return splcommon.PhaseError, err
			}
		}

		return splcommon.PhaseScalingDown, nil
	}

	// ready and no StatefulSet scaling is required
	// readyReplicas == desiredReplicas

	// check existing pods for desired updates
	for n := readyReplicas - 1; n >= 0; n-- {
		// get Pod
		podName := fmt.Sprintf("%s-%d", statefulSet.GetName(), n)
		namespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: podName}
		var pod corev1.Pod
		err := c.Get(context.TODO(), namespacedName, &pod)
		if err != nil {
			scopedLog.Error(err, "Unable to find Pod", "podName", podName)
			return splcommon.PhaseError, err
		}
		if pod.Status.Phase != corev1.PodRunning || len(pod.Status.ContainerStatuses) == 0 || pod.Status.ContainerStatuses[0].Ready != true {
			scopedLog.Error(err, "Waiting for Pod to become ready", "podName", podName)
			return splcommon.PhaseUpdating, err
		}

		// terminate pod if it has pending updates; k8s will start a new one with revised template
		if statefulSet.Status.UpdateRevision != "" && statefulSet.Status.UpdateRevision != pod.GetLabels()["controller-revision-hash"] {
			// pod needs to be updated; first, prepare it to be recycled
			ready, err := mgr.PrepareRecycle(n)
			if err != nil {
				scopedLog.Error(err, "Unable to prepare Pod for recycling", "podName", podName)
				return splcommon.PhaseError, err
			}
			if !ready {
				// wait until pod quarantine has completed before deleting it
				return splcommon.PhaseUpdating, nil
			}

			// deleting pod will cause StatefulSet controller to create a new one with latest template
			scopedLog.Info("Recycling Pod for updates", "podName", podName,
				"statefulSetRevision", statefulSet.Status.UpdateRevision,
				"podRevision", pod.GetLabels()["controller-revision-hash"])
			preconditions := client.Preconditions{UID: &pod.ObjectMeta.UID, ResourceVersion: &pod.ObjectMeta.ResourceVersion}
			err = c.Delete(context.Background(), &pod, preconditions)
			if err != nil {
				scopedLog.Error(err, "Unable to delete Pod", "podName", podName)
				return splcommon.PhaseError, err
			}

			// only delete one at a time
			return splcommon.PhaseUpdating, nil
		}

		// check if pod was previously prepared for recycling; if so, complete
		complete, err := mgr.FinishRecycle(n)
		if err != nil {
			scopedLog.Error(err, "Unable to complete recycling of pod", "podName", podName)
			return splcommon.PhaseError, err
		}
		if !complete {
			// return and wait until next reconcile to let things settle down
			return splcommon.PhaseUpdating, nil
		}
	}

	// Remove unwanted owner references
	err := splutil.RemoveUnwantedSecrets(c, statefulSet.GetName(), statefulSet.GetNamespace())
	if err != nil {
		return splcommon.PhaseReady, err
	}

	// all is good!
	scopedLog.Info("All pods are ready")
	return splcommon.PhaseReady, nil
}

// SetStatefulSetOwnerRef sets owner references for statefulset
func SetStatefulSetOwnerRef(client splcommon.ControllerClient, cr splcommon.MetaObject, namespacedName types.NamespacedName) error {

	statefulset, err := GetStatefulSetByName(client, namespacedName)
	if err != nil {
		return err
	}

	currentOwnerRef := statefulset.GetOwnerReferences()
	// Check if owner ref exists
	for i := 0; i < len(currentOwnerRef); i++ {
		if reflect.DeepEqual(currentOwnerRef[i], splcommon.AsOwner(cr, false)) {
			return nil
		}
	}

	// Owner ref doesn't exist, update statefulset with owner references
	statefulset.SetOwnerReferences(append(statefulset.GetOwnerReferences(), splcommon.AsOwner(cr, false)))

	// Update owner reference if needed
	err = splutil.UpdateResource(client, statefulset)
	if err != nil {
		return err
	}

	return nil
}

// GetStatefulSetByName retrieves current statefulset
func GetStatefulSetByName(c splcommon.ControllerClient, namespacedName types.NamespacedName) (*appsv1.StatefulSet, error) {
	var statefulset appsv1.StatefulSet

	err := c.Get(context.TODO(), namespacedName, &statefulset)
	if err != nil {
		// Didn't find it
		return nil, err
	}

	return &statefulset, nil
}
