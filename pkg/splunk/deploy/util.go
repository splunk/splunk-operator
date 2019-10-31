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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/splunk/splunk-operator/pkg/splunk/resources"
)

// The ResourceObject type implements methods of runtime.Object and GetObjectMeta()
type ResourceObject interface {
	runtime.Object
	GetObjectMeta() metav1.Object
}

// CreateResource creates a new Kubernetes resource using the REST API.
func CreateResource(client client.Client, obj ResourceObject) error {
	err := client.Create(context.TODO(), obj)

	if err != nil && !errors.IsAlreadyExists(err) {
		log.Printf("Failed to create object : %v", err)
		return err
	}

	log.Printf("Created %s in namespace %s\n", obj.GetObjectMeta().GetName(), obj.GetObjectMeta().GetNamespace())

	return nil
}

// UpdateResource updates an existing Kubernetes resource using the REST API.
func UpdateResource(client client.Client, obj ResourceObject) error {
	err := client.Update(context.TODO(), obj)

	if err != nil && !errors.IsAlreadyExists(err) {
		log.Printf("Failed to update object : %v", err)
		return err
	}

	log.Printf("Updated %s in namespace %s\n", obj.GetObjectMeta().GetName(), obj.GetObjectMeta().GetNamespace())

	return nil
}

// ApplyConfigMap creates or updates a Kubernetes ConfigMap
func ApplyConfigMap(client client.Client, configMap *corev1.ConfigMap) error {

	var oldConfigMap corev1.ConfigMap
	namespacedName := types.NamespacedName{
		Namespace: configMap.Namespace,
		Name:      configMap.Name,
	}

	err := client.Get(context.TODO(), namespacedName, &oldConfigMap)
	if err == nil {
		// found existing ConfigMap: do nothing
		log.Printf("Found existing ConfigMap %s in namespace %s\n", configMap.GetObjectMeta().GetName(), configMap.GetObjectMeta().GetNamespace())
	} else {
		err = CreateResource(client, configMap)
	}

	return err
}

// ApplySecret creates or updates a Kubernetes Secret
func ApplySecret(client client.Client, secret *corev1.Secret) error {

	var oldSecret corev1.Secret
	namespacedName := types.NamespacedName{
		Namespace: secret.Namespace,
		Name:      secret.Name,
	}

	err := client.Get(context.TODO(), namespacedName, &oldSecret)
	if err == nil {
		// found existing Secret: do nothing
		log.Printf("Found existing Secret %s in namespace %s\n", secret.GetObjectMeta().GetName(), secret.GetObjectMeta().GetNamespace())
	} else {
		err = CreateResource(client, secret)
	}

	return err
}

// ApplyService creates or updates a Kubernetes Service
func ApplyService(client client.Client, service *corev1.Service) error {

	var oldService corev1.Service
	namespacedName := types.NamespacedName{
		Namespace: service.Namespace,
		Name:      service.Name,
	}

	err := client.Get(context.TODO(), namespacedName, &oldService)
	if err == nil {
		// found existing Service: do nothing
		log.Printf("Found existing Service %s in namespace %s\n", service.GetObjectMeta().GetName(), service.GetObjectMeta().GetNamespace())
	} else {
		err = CreateResource(client, service)
	}

	return err
}

// MergePodUpdates looks for material differences between a Pod's current
// config and a revised config. It merges material changes from revised to
// current. This enables us to minimize updates. It returns true if there
// are material differences between them, or false otherwise.
func MergePodUpdates(current *corev1.PodTemplateSpec, revised *corev1.PodTemplateSpec) bool {
	result := false

	// check for changes in Affinity
	if resources.CompareByMarshall(current.Spec.Affinity, revised.Spec.Affinity) {
		log.Printf("Affinity differs for %s: \"%v\" != \"%v\"",
			current.GetObjectMeta().GetName(), current.Spec.Affinity,
			revised.Spec.Affinity)
		current.Spec.Affinity = revised.Spec.Affinity
		result = true
	}

	// check for changes in SchedulerName
	if resources.CompareByMarshall(current.Spec.SchedulerName, revised.Spec.SchedulerName) {
		log.Printf("SchedulerName differs for %s: \"%v\" != \"%v\"",
			current.GetObjectMeta().GetName(), current.Spec.SchedulerName,
			revised.Spec.SchedulerName)
		current.Spec.SchedulerName = revised.Spec.SchedulerName
		result = true
	}

	// check for changes in container images; assume that the ordering is same for pods with > 1 container
	if len(current.Spec.Containers) != len(revised.Spec.Containers) {
		log.Printf("Container counts differ for %s: %d != %d",
			current.GetObjectMeta().GetName(), len(current.Spec.Containers),
			len(revised.Spec.Containers))
		current.Spec.Containers = revised.Spec.Containers
		result = true
	} else {
		for idx := range current.Spec.Containers {
			// check Image
			if current.Spec.Containers[idx].Image != revised.Spec.Containers[idx].Image {
				log.Printf("Container Images differ for %s: \"%s\" != \"%s\"",
					current.GetObjectMeta().GetName(), current.Spec.Containers[idx].Image,
					revised.Spec.Containers[idx].Image)
				current.Spec.Containers[idx].Image = revised.Spec.Containers[idx].Image
				result = true
			}

			// check Ports
			if resources.ComparePorts(current.Spec.Containers[idx].Ports, revised.Spec.Containers[idx].Ports) {
				log.Printf("Container Ports differ for %s: \"%v\" != \"%v\"",
					current.GetObjectMeta().GetName(), current.Spec.Containers[idx].Ports,
					revised.Spec.Containers[idx].Ports)
				current.Spec.Containers[idx].Ports = revised.Spec.Containers[idx].Ports
				result = true
			}

			// check VolumeMounts
			if resources.CompareVolumeMounts(current.Spec.Containers[idx].VolumeMounts, revised.Spec.Containers[idx].VolumeMounts) {
				log.Printf("Container VolumeMounts differ for %s: \"%v\" != \"%v\"",
					current.GetObjectMeta().GetName(), current.Spec.Containers[idx].VolumeMounts,
					revised.Spec.Containers[idx].VolumeMounts)
				current.Spec.Containers[idx].VolumeMounts = revised.Spec.Containers[idx].VolumeMounts
				result = true
			}

			// check Resources
			if resources.CompareByMarshall(&current.Spec.Containers[idx].Resources, &revised.Spec.Containers[idx].Resources) {
				log.Printf("Container Resources differ for %s: \"%v\" != \"%v\"",
					current.GetObjectMeta().GetName(), current.Spec.Containers[idx].Resources,
					revised.Spec.Containers[idx].Resources)
				current.Spec.Containers[idx].Resources = revised.Spec.Containers[idx].Resources
				result = true
			}
		}
	}

	return result
}
