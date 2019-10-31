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
	"github.com/splunk/splunk-operator/pkg/splunk/spark"
)

// ApplySparkDeployment creates or updates a Kubernetes Deployment for a given type of Spark instance.
func ApplySparkDeployment(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType spark.InstanceType, replicas int, envVariables []corev1.EnvVar, ports []corev1.ContainerPort) error {

	deployment, err := spark.GetSparkDeployment(cr, instanceType, replicas, envVariables, ports)
	if err != nil {
		return err
	}

	return ApplyDeployment(client, deployment)
}

// ApplyDeployment creates or updates a Kubernetes Deployment
func ApplyDeployment(client client.Client, deployment *appsv1.Deployment) error {

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
			err = UpdateResource(client, deployment)
		} else {
			log.Printf("No changes for Deployment %s in namespace %s\n", deployment.GetObjectMeta().GetName(), deployment.GetObjectMeta().GetNamespace())
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
	result := false

	// check for change in Replicas count
	if *current.Spec.Replicas != *revised.Spec.Replicas {
		log.Printf("Replicas differ for %s: %d != %d",
			current.GetObjectMeta().GetName(), *current.Spec.Replicas, *revised.Spec.Replicas)
		current.Spec.Replicas = revised.Spec.Replicas
		result = true
	}

	// check for changes in Pod template
	if MergePodUpdates(&current.Spec.Template, &revised.Spec.Template) {
		result = true
	}

	return result
}
