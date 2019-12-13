// Copyright (c) 2018-2019 Splunk Inc. All rights reserved.
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
	"log"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
)

// ApplySplunkStatefulSet creates or updates a Kubernetes StatefulSet for a given type of Splunk Enterprise instance (indexers or search heads).
func ApplySplunkStatefulSet(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType enterprise.InstanceType, replicas int, envVariables []corev1.EnvVar) error {

	statefulSet, err := enterprise.GetSplunkStatefulSet(cr, instanceType, replicas, envVariables)
	if err != nil {
		return err
	}

	return ApplyStatefulSet(client, statefulSet)
}

// ApplyStatefulSet creates or updates a Kubernetes StatefulSet
func ApplyStatefulSet(client client.Client, statefulSet *appsv1.StatefulSet) error {

	var current appsv1.StatefulSet
	namespacedName := types.NamespacedName{
		Namespace: statefulSet.Namespace,
		Name:      statefulSet.Name,
	}

	err := client.Get(context.TODO(), namespacedName, &current)
	if err == nil {
		// found existing StatefulSet
		if MergeStatefulSetUpdates(&current, statefulSet) {
			// only update if there are material differences, as determined by comparison function
			err = UpdateResource(client, &current)
		} else {
			log.Printf("No changes for StatefulSet %s in namespace %s\n", statefulSet.GetObjectMeta().GetName(), statefulSet.GetObjectMeta().GetNamespace())
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
	result := false

	// check for change in Replicas count
	if *current.Spec.Replicas != *revised.Spec.Replicas {
		log.Printf("Replicas differ for %s: %d != %d",
			current.GetObjectMeta().GetName(), *current.Spec.Replicas, *revised.Spec.Replicas)
		current.Spec.Replicas = revised.Spec.Replicas
		result = true
	}

	// check for changes in Pod template
	if MergePodUpdates(&current.Spec.Template, &revised.Spec.Template, current.GetObjectMeta().GetName()) {
		result = true
	}

	return result
}
