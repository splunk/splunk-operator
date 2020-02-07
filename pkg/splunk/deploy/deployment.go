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

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ApplyDeployment creates or updates a Kubernetes Deployment
func ApplyDeployment(client ControllerClient, deployment *appsv1.Deployment) error {
	scopedLog := log.WithName("ApplyDeployment").WithValues(
		"name", deployment.GetObjectMeta().GetName(),
		"namespace", deployment.GetObjectMeta().GetNamespace())

	var current appsv1.Deployment
	namespacedName := types.NamespacedName{
		Namespace: deployment.Namespace,
		Name:      deployment.Name,
	}

	err := client.Get(context.TODO(), namespacedName, &current)
	if err == nil {
		// found existing Deployment
		if MergeDeploymentUpdates(&current, deployment) {
			// only update if there are material differences, as determined by comparison function
			err = UpdateResource(client, &current)
		} else {
			scopedLog.Info("No changes for Deployment")
		}
	} else {
		err = CreateResource(client, deployment)
	}

	return err
}

// MergeDeploymentUpdates looks for material differences between a
// Deployment's current config and a revised config. It merges material
// changes from revised to current. This enables us to minimize updates.
// It returns true if there are material differences between them, or false otherwise.
func MergeDeploymentUpdates(current *appsv1.Deployment, revised *appsv1.Deployment) bool {
	scopedLog := log.WithName("MergeDeploymentUpdates").WithValues(
		"name", current.GetObjectMeta().GetName(),
		"namespace", current.GetObjectMeta().GetNamespace())
	result := false

	// check for change in Replicas count
	if *current.Spec.Replicas != *revised.Spec.Replicas {
		scopedLog.Info("Deployment Replicas differ",
			"current", *current.Spec.Replicas,
			"revised", *revised.Spec.Replicas)
		current.Spec.Replicas = revised.Spec.Replicas
		result = true
	}

	// check for changes in Pod template
	if MergePodUpdates(&current.Spec.Template, &revised.Spec.Template, current.GetObjectMeta().GetName()) {
		result = true
	}

	return result
}
