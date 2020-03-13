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

package deploy

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
)

// StatefulSetPodManager is used to manage the pods within a StatefulSet
type StatefulSetPodManager interface {
	// Decommision pod and return true if complete
	Decommission(n int32) (bool, error)

	// Quarantine pod and return true if complete
	Quarantine(n int32) (bool, error)

	// ReleaseQuarantine will release a quarantine and return true, if active; it returns false if none active
	ReleaseQuarantine(n int32) (bool, error)
}

// DefaultStatefulSetPodManager is a simple StatefulSetPodManager that does nothing
type DefaultStatefulSetPodManager struct{}

// Decommission for DefaultStatefulSetPodManager does nothing and returns true
func (mgr *DefaultStatefulSetPodManager) Decommission(n int32) (bool, error) {
	return true, nil
}

// Quarantine for DefaultStatefulSetPodManager does nothing and returns true
func (mgr *DefaultStatefulSetPodManager) Quarantine(n int32) (bool, error) {
	return true, nil
}

// ReleaseQuarantine for DefaultStatefulSetPodManager does nothing and returns false
func (mgr *DefaultStatefulSetPodManager) ReleaseQuarantine(n int32) (bool, error) {
	return false, nil
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
		// note also that this ignores Replicas, which is handled below by ReconcileStatefulSetPods
		return enterprisev1.PhaseUpdating, UpdateResource(c, revised)
	}

	// scaling and pod updates are handled by ReconcileStatefulSetPods
	return enterprisev1.PhaseReady, nil
}

// ReconcileStatefulSetPods manages scaling and config updates for StatefulSets
func ReconcileStatefulSetPods(c ControllerClient, statefulSet *appsv1.StatefulSet, mgr StatefulSetPodManager, desiredReplicas int32) (enterprisev1.ResourcePhase, error) {

	scopedLog := log.WithName("ReconcileStatefulSetPods").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	// wait for all replicas ready
	replicas := statefulSet.Status.Replicas
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
		// decommission pod to prepare for removal
		n := readyReplicas - 1
		complete, err := mgr.Decommission(n)
		if err != nil {
			podName := fmt.Sprintf("%s-%d", statefulSet.GetName(), n)
			scopedLog.Error(err, "Unable to decommission Pod", "podName", podName)
			return enterprisev1.PhaseError, err
		}
		if !complete {
			// wait until pod quarantine has completed before deleting it
			return enterprisev1.PhaseScalingDown, nil
		}

		// scale down statefulset to terminate pod
		scopedLog.Info("Scaling replicas down", "replicas", n)
		*statefulSet.Spec.Replicas = n
		return enterprisev1.PhaseScalingDown, UpdateResource(c, statefulSet)
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

		// terminate pod if it has pending updates; k8s will start a new one with revised template
		if statefulSet.Status.UpdateRevision != "" && statefulSet.Status.UpdateRevision != pod.GetLabels()["controller-revision-hash"] {
			// pod needs to be updated; first, quarantine it to prepare for restart
			complete, err := mgr.Quarantine(n)
			if err != nil {
				scopedLog.Error(err, "Unable to quarantine Pod", "podName", podName)
				return enterprisev1.PhaseError, err
			}
			if !complete {
				// wait until pod quarantine has completed before deleting it
				return enterprisev1.PhaseUpdating, nil
			}

			// deleting pod will cause StatefulSet controller to create a new one with latest template
			scopedLog.Info("Recycling Pod for updates", "podName", podName,
				"statefulSetRevision", statefulSet.Status.CurrentRevision,
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

		// check if pod was previously quarantined; if so, it's ok to release it
		released, err := mgr.ReleaseQuarantine(n)
		if err != nil {
			scopedLog.Error(err, "Unable to release Pod from quarantine", "podName", podName)
			return enterprisev1.PhaseError, err
		}
		if released {
			// if pod was released, return and wait until next reconcile to let things settle down
			return enterprisev1.PhaseUpdating, nil
		}
	}

	// all is good!
	scopedLog.Info("All pods are ready")
	return enterprisev1.PhaseReady, nil
}
