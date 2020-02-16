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

// ApplyStatefulSet creates or updates a Kubernetes StatefulSet
func ApplyStatefulSet(client ControllerClient, statefulSet *appsv1.StatefulSet) error {
	scopedLog := log.WithName("ApplyStatefulSet").WithValues(
		"name", statefulSet.GetObjectMeta().GetName(),
		"namespace", statefulSet.GetObjectMeta().GetNamespace())

	namespacedName := types.NamespacedName{Namespace: statefulSet.GetNamespace(), Name: statefulSet.GetName()}
	var current appsv1.StatefulSet

	err := client.Get(context.TODO(), namespacedName, &current)
	if err == nil {
		// found existing StatefulSet
		if MergeStatefulSetUpdates(&current, statefulSet) {
			// only update if there are material differences, as determined by comparison function
			err = UpdateResource(client, &current)
		} else {
			scopedLog.Info("No changes for StatefulSet")
		}
	} else {
		err = CreateResource(client, statefulSet)
	}

	return err
}

// MergeStatefulSetUpdates looks for material differences between a
// StatefulSet's current config and a revised config. It merges material
// changes from revised to current. This enables us to minimize updates.
// It returns true if there are material differences between them, or false otherwise.
func MergeStatefulSetUpdates(current *appsv1.StatefulSet, revised *appsv1.StatefulSet) bool {
	scopedLog := log.WithName("MergeStatefulSetUpdates").WithValues(
		"name", current.GetObjectMeta().GetName(),
		"namespace", current.GetObjectMeta().GetNamespace())
	result := false

	// check for change in Replicas count
	if current.Spec.Replicas != nil && revised.Spec.Replicas != nil && *current.Spec.Replicas != *revised.Spec.Replicas {
		scopedLog.Info("StatefulSet Replicas differ",
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
