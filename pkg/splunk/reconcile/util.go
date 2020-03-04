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
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/splunk/splunk-operator/pkg/splunk/resources"
)

// logger used by splunk.deploy package
var log = logf.Log.WithName("splunk.deploy")

// The ResourceObject type implements methods of runtime.Object and GetObjectMeta()
type ResourceObject interface {
	runtime.Object
	GetObjectMeta() metav1.Object
}

// The ControllerClient interfaces implements methods of the Kubernetes controller-runtime client
type ControllerClient interface {
	client.Client
}

// CreateResource creates a new Kubernetes resource using the REST API.
func CreateResource(client ControllerClient, obj ResourceObject) error {
	scopedLog := log.WithName("CreateResource").WithValues(
		"name", obj.GetObjectMeta().GetName(),
		"namespace", obj.GetObjectMeta().GetNamespace())
	err := client.Create(context.TODO(), obj)

	if err != nil && !errors.IsAlreadyExists(err) {
		scopedLog.Error(err, "Failed to create resource")
		return err
	}

	scopedLog.Info("Created resource")

	return nil
}

// UpdateResource updates an existing Kubernetes resource using the REST API.
func UpdateResource(client ControllerClient, obj ResourceObject) error {
	scopedLog := log.WithName("UpdateResource").WithValues(
		"name", obj.GetObjectMeta().GetName(),
		"namespace", obj.GetObjectMeta().GetNamespace())
	err := client.Update(context.TODO(), obj)

	if err != nil && !errors.IsAlreadyExists(err) {
		scopedLog.Error(err, "Failed to update resource")
		return err
	}

	scopedLog.Info("Updated resource")

	return nil
}

// MergePodUpdates looks for material differences between a Pod's current
// config and a revised config. It merges material changes from revised to
// current. This enables us to minimize updates. It returns true if there
// are material differences between them, or false otherwise.
func MergePodUpdates(current *corev1.PodTemplateSpec, revised *corev1.PodTemplateSpec, name string) bool {
	result := MergePodSpecUpdates(&current.Spec, &revised.Spec, name)
	if MergePodMetaUpdates(&current.ObjectMeta, &revised.ObjectMeta, name) {
		result = true
	}
	return result
}

// MergePodMetaUpdates looks for material differences between a Pod's current
// meta data and a revised meta data. It merges material changes from revised to
// current. This enables us to minimize updates. It returns true if there
// are material differences between them, or false otherwise.
func MergePodMetaUpdates(current *metav1.ObjectMeta, revised *metav1.ObjectMeta, name string) bool {
	scopedLog := log.WithName("MergePodMetaUpdates").WithValues("name", name)
	result := false

	// check Annotations
	if !reflect.DeepEqual(current.Annotations, revised.Annotations) {
		scopedLog.Info("Container Annotations differ", "current", current.Annotations, "revised", revised.Annotations)
		current.Annotations = revised.Annotations
		result = true
	}

	// check Labels
	if !reflect.DeepEqual(current.Labels, revised.Labels) {
		scopedLog.Info("Container Labels differ", "current", current.Labels, "revised", revised.Labels)
		current.Labels = revised.Labels
		result = true
	}

	return result
}

// MergePodSpecUpdates looks for material differences between a Pod's current
// desired spec and a revised spec. It merges material changes from revised to
// current. This enables us to minimize updates. It returns true if there
// are material differences between them, or false otherwise.
func MergePodSpecUpdates(current *corev1.PodSpec, revised *corev1.PodSpec, name string) bool {
	scopedLog := log.WithName("MergePodUpdates").WithValues("name", name)
	result := false

	// check for changes in Affinity
	if resources.CompareByMarshall(current.Affinity, revised.Affinity) {
		scopedLog.Info("Pod Affinity differs",
			"current", current.Affinity,
			"revised", revised.Affinity)
		current.Affinity = revised.Affinity
		result = true
	}

	// check for changes in SchedulerName
	if current.SchedulerName != revised.SchedulerName {
		scopedLog.Info("Pod SchedulerName differs",
			"current", current.SchedulerName,
			"revised", revised.SchedulerName)
		current.SchedulerName = revised.SchedulerName
		result = true
	}

	// check for changes in container images; assume that the ordering is same for pods with > 1 container
	if len(current.Containers) != len(revised.Containers) {
		scopedLog.Info("Pod Container counts differ",
			"current", len(current.Containers),
			"revised", len(revised.Containers))
		current.Containers = revised.Containers
		result = true
	} else {
		for idx := range current.Containers {
			// check Image
			if current.Containers[idx].Image != revised.Containers[idx].Image {
				scopedLog.Info("Pod Container Images differ",
					"current", current.Containers[idx].Image,
					"revised", revised.Containers[idx].Image)
				current.Containers[idx].Image = revised.Containers[idx].Image
				result = true
			}

			// check Ports
			if resources.CompareContainerPorts(current.Containers[idx].Ports, revised.Containers[idx].Ports) {
				scopedLog.Info("Pod Container Ports differ",
					"current", current.Containers[idx].Ports,
					"revised", revised.Containers[idx].Ports)
				current.Containers[idx].Ports = revised.Containers[idx].Ports
				result = true
			}

			// check VolumeMounts
			if resources.CompareVolumeMounts(current.Containers[idx].VolumeMounts, revised.Containers[idx].VolumeMounts) {
				scopedLog.Info("Pod Container VolumeMounts differ",
					"current", current.Containers[idx].VolumeMounts,
					"revised", revised.Containers[idx].VolumeMounts)
				current.Containers[idx].VolumeMounts = revised.Containers[idx].VolumeMounts
				result = true
			}

			// check Resources
			if resources.CompareByMarshall(&current.Containers[idx].Resources, &revised.Containers[idx].Resources) {
				scopedLog.Info("Pod Container Resources differ",
					"current", current.Containers[idx].Resources,
					"revised", revised.Containers[idx].Resources)
				current.Containers[idx].Resources = revised.Containers[idx].Resources
				result = true
			}
		}
	}

	return result
}
