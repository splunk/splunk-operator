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
func ReconcileStatefulSetPods(c ControllerClient, statefulSet *appsv1.StatefulSet, readyReplicas, desiredReplicas int32,
	removeReadyPod func(ControllerClient, *appsv1.StatefulSet) (enterprisev1.ResourcePhase, error)) (enterprisev1.ResourcePhase, error) {

	scopedLog := log.WithName("ReconcileStatefulSetPods").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	// inline function used to handle scale down
	scaleDown := func(replicas int32) error {
		scopedLog.Info(fmt.Sprintf("Scaling replicas down to %d", replicas))
		*statefulSet.Spec.Replicas = replicas
		return UpdateResource(c, statefulSet)
	}

	// check for scaling down
	if readyReplicas > desiredReplicas {
		if removeReadyPod != nil {
			return removeReadyPod(c, statefulSet)
		}
		return enterprisev1.PhaseScalingDown, scaleDown(readyReplicas - 1)
	}

	// check if replicas are not yet ready
	if readyReplicas < desiredReplicas {
		if *statefulSet.Spec.Replicas < desiredReplicas {
			// scale up StatefulSet to match desiredReplicas
			scopedLog.Info(fmt.Sprintf("Scaling replicas up to %d", desiredReplicas))
			*statefulSet.Spec.Replicas = desiredReplicas
			return enterprisev1.PhaseScalingUp, UpdateResource(c, statefulSet)
		}

		if statefulSet.Status.UpdatedReplicas < statefulSet.Status.Replicas {
			scopedLog.Info("Waiting for updates to complete")
			return enterprisev1.PhaseUpdating, nil
		}

		scopedLog.Info("Waiting for pods to become ready")
		if readyReplicas > 0 {
			return enterprisev1.PhaseScalingUp, nil
		}
		return enterprisev1.PhasePending, nil
	}

	// readyReplicas == desiredReplicas

	// check if we have extra pods in statefulset
	if *statefulSet.Spec.Replicas != desiredReplicas {
		// scale down StatefulSet to match readyReplicas
		return enterprisev1.PhaseScalingDown, scaleDown(readyReplicas)
	}

	// ready and no StatefulSet scaling required

	// check existing pods for desired updates
	for n := *statefulSet.Spec.Replicas; n > 0; n-- {
		// get Pod
		podName := fmt.Sprintf("%s-%d", statefulSet.GetName(), n-1)
		namespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: podName}
		var pod corev1.Pod
		err := c.Get(context.TODO(), namespacedName, &pod)
		if err != nil {
			scopedLog.Error(err, "Unable to find Pod", "podName", podName)
			return enterprisev1.PhaseError, err
		}

		// terminate pod if it has pending updates; k8s will start a new one with revised template
		if statefulSet.Status.UpdateRevision != "" && statefulSet.Status.UpdateRevision != pod.GetLabels()["controller-revision-hash"] {
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
	}

	// all is good!
	scopedLog.Info("All pods are ready")
	return enterprisev1.PhaseReady, nil
}
