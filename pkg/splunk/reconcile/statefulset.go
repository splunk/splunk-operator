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

package reconcile

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
)

// StatefulSetPodManager is used to manage the pods within a StatefulSet
type StatefulSetPodManager interface {
	// Update handles all updates for a statefulset and all of its pods
	Update(ControllerClient, *appsv1.StatefulSet, int32) (enterprisev1.ResourcePhase, error)

	// PrepareScaleDown prepares pod to be removed via scale down event; it returns true when ready
	PrepareScaleDown(int32) (bool, error)

	// PrepareRecycle prepares pod to be recycled for updates; it returns true when ready
	PrepareRecycle(int32) (bool, error)

	// FinishRecycle completes recycle event for pod and returns true, or returns false if nothing to do
	FinishRecycle(int32) (bool, error)
}

// DefaultStatefulSetPodManager is a simple StatefulSetPodManager that does nothing
type DefaultStatefulSetPodManager struct{}

// Update for DefaultStatefulSetPodManager handles all updates for a statefulset of standard pods
func (mgr *DefaultStatefulSetPodManager) Update(client ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (enterprisev1.ResourcePhase, error) {
	phase, err := ApplyStatefulSet(client, statefulSet)
	if err == nil && phase == enterprisev1.PhaseReady {
		phase, err = UpdateStatefulSetPods(client, statefulSet, mgr, desiredReplicas)
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
func ApplyStatefulSet(c ControllerClient, revised *appsv1.StatefulSet) (enterprisev1.ResourcePhase, error) {
	namespacedName := types.NamespacedName{Namespace: revised.GetNamespace(), Name: revised.GetName()}
	var current appsv1.StatefulSet

	err := c.Get(context.TODO(), namespacedName, &current)
	if err != nil {
		// no StatefulSet exists -> just create a new one
		err = CreateResource(c, revised)
		return enterprisev1.PhasePending, err
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
		return enterprisev1.PhaseUpdating, UpdateResource(c, revised)
	}

	// scaling and pod updates are handled by UpdateStatefulSetPods
	return enterprisev1.PhaseReady, nil
}

// UpdateStatefulSetPods manages scaling and config updates for StatefulSets
func UpdateStatefulSetPods(c ControllerClient, statefulSet *appsv1.StatefulSet, mgr StatefulSetPodManager, desiredReplicas int32) (enterprisev1.ResourcePhase, error) {

	scopedLog := log.WithName("UpdateStatefulSetPods").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	// wait for all replicas ready
	replicas := *statefulSet.Spec.Replicas
	readyReplicas := statefulSet.Status.ReadyReplicas
	if readyReplicas < replicas {
		scopedLog.Info("Waiting for pods to become ready")
		if readyReplicas > 0 {
			return enterprisev1.PhaseScalingUp, nil
		}
		return enterprisev1.PhasePending, nil
	} else if readyReplicas > replicas {
		scopedLog.Info("Waiting for scale down to complete")
		return enterprisev1.PhaseScalingDown, nil
	}

	// readyReplicas == replicas

	// check for scaling up
	if readyReplicas < desiredReplicas {
		// scale up StatefulSet to match desiredReplicas
		scopedLog.Info("Scaling replicas up", "replicas", desiredReplicas)
		*statefulSet.Spec.Replicas = desiredReplicas
		return enterprisev1.PhaseScalingUp, UpdateResource(c, statefulSet)
	}

	// check for scaling down
	if readyReplicas > desiredReplicas {
		// prepare pod for removal via scale down
		n := readyReplicas - 1
		podName := fmt.Sprintf("%s-%d", statefulSet.GetName(), n)
		ready, err := mgr.PrepareScaleDown(n)
		if err != nil {
			scopedLog.Error(err, "Unable to decommission Pod", "podName", podName)
			return enterprisev1.PhaseError, err
		}
		if !ready {
			// wait until pod quarantine has completed before deleting it
			return enterprisev1.PhaseScalingDown, nil
		}

		// scale down statefulset to terminate pod
		scopedLog.Info("Scaling replicas down", "replicas", n)
		*statefulSet.Spec.Replicas = n
		err = UpdateResource(c, statefulSet)
		if err != nil {
			scopedLog.Error(err, "Scale down update failed for StatefulSet")
			return enterprisev1.PhaseError, err
		}

		// delete PVCs used by the pod so that a future scale up will have clean state
		for _, vol := range []string{"pvc-etc", "pvc-var"} {
			namespacedName := types.NamespacedName{
				Namespace: statefulSet.GetNamespace(),
				Name:      fmt.Sprintf("%s-%s", vol, podName),
			}
			var pvc corev1.PersistentVolumeClaim
			err := c.Get(context.TODO(), namespacedName, &pvc)
			if err != nil {
				scopedLog.Error(err, "Unable to find PVC for deletion", "pvcName", pvc.ObjectMeta.Name)
				return enterprisev1.PhaseError, err
			}
			log.Info("Deleting PVC", "pvcName", pvc.ObjectMeta.Name)
			err = c.Delete(context.Background(), &pvc)
			if err != nil {
				scopedLog.Error(err, "Unable to delete PVC", "pvcName", pvc.ObjectMeta.Name)
				return enterprisev1.PhaseError, err
			}
		}

		return enterprisev1.PhaseScalingDown, nil
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
			return enterprisev1.PhaseError, err
		}
		if pod.Status.Phase != corev1.PodRunning || len(pod.Status.ContainerStatuses) == 0 || pod.Status.ContainerStatuses[0].Ready != true {
			scopedLog.Error(err, "Waiting for Pod to become ready", "podName", podName)
			return enterprisev1.PhaseUpdating, err
		}

		// terminate pod if it has pending updates; k8s will start a new one with revised template
		if statefulSet.Status.UpdateRevision != "" && statefulSet.Status.UpdateRevision != pod.GetLabels()["controller-revision-hash"] {
			// pod needs to be updated; first, prepare it to be recycled
			ready, err := mgr.PrepareRecycle(n)
			if err != nil {
				scopedLog.Error(err, "Unable to prepare Pod for recycling", "podName", podName)
				return enterprisev1.PhaseError, err
			}
			if !ready {
				// wait until pod quarantine has completed before deleting it
				return enterprisev1.PhaseUpdating, nil
			}

			// deleting pod will cause StatefulSet controller to create a new one with latest template
			scopedLog.Info("Recycling Pod for updates", "podName", podName,
				"statefulSetRevision", statefulSet.Status.UpdateRevision,
				"podRevision", pod.GetLabels()["controller-revision-hash"])
			preconditions := client.Preconditions{UID: &pod.ObjectMeta.UID, ResourceVersion: &pod.ObjectMeta.ResourceVersion}
			err = c.Delete(context.Background(), &pod, preconditions)
			if err != nil {
				scopedLog.Error(err, "Unable to delete Pod", "podName", podName)
				return enterprisev1.PhaseError, err
			}

			// only delete one at a time
			return enterprisev1.PhaseUpdating, nil
		}

		// check if pod was previously prepared for recycling; if so, complete
		complete, err := mgr.FinishRecycle(n)
		if err != nil {
			scopedLog.Error(err, "Unable to complete recycling of pod", "podName", podName)
			return enterprisev1.PhaseError, err
		}
		if !complete {
			// return and wait until next reconcile to let things settle down
			return enterprisev1.PhaseUpdating, nil
		}
	}

	// all is good!
	scopedLog.Info("All pods are ready")
	return enterprisev1.PhaseReady, nil
}
