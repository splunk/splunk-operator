// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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
	"reflect"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	//stdlog "log"
	//"github.com/go-logr/stdr"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

// kubernetes logger used by splunk.reconcile package
var log = logf.Log.WithName("splunk.reconcile")

// simple stdout logger, used for debugging
//var log = stdr.New(stdlog.New(os.Stderr, "", stdlog.LstdFlags|stdlog.Lshortfile)).WithName("splunk.reconcile")

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
	if splcommon.CompareByMarshall(current.Affinity, revised.Affinity) {
		scopedLog.Info("Pod Affinity differs",
			"current", current.Affinity,
			"revised", revised.Affinity)
		current.Affinity = revised.Affinity
		result = true
	}

	if splcommon.CompareTolerations(current.Tolerations, revised.Tolerations) {
		scopedLog.Info("Pod Tolerations differs",
			"current", current.Tolerations,
			"revised", revised.Tolerations)
		current.Tolerations = revised.Tolerations
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

	// Check for changes in Volumes
	if splcommon.CompareVolumes(current.Volumes, revised.Volumes) {
		scopedLog.Info("Pod Volumes differ",
			"current", current.Volumes,
			"revised", revised.Volumes)
		current.Volumes = revised.Volumes
		result = true
	}

	// Check for changes in Init containers
	if len(current.InitContainers) != len(revised.InitContainers) {
		scopedLog.Info("Pod init containers  differ",
			"current", len(current.InitContainers),
			"revised", len(revised.InitContainers))
		current.InitContainers = revised.InitContainers
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
			if splcommon.CompareContainerPorts(current.Containers[idx].Ports, revised.Containers[idx].Ports) {
				scopedLog.Info("Pod Container Ports differ",
					"current", current.Containers[idx].Ports,
					"revised", revised.Containers[idx].Ports)
				current.Containers[idx].Ports = revised.Containers[idx].Ports
				result = true
			}

			// check VolumeMounts
			if splcommon.CompareVolumeMounts(current.Containers[idx].VolumeMounts, revised.Containers[idx].VolumeMounts) {
				scopedLog.Info("Pod Container VolumeMounts differ",
					"current", current.Containers[idx].VolumeMounts,
					"revised", revised.Containers[idx].VolumeMounts)
				current.Containers[idx].VolumeMounts = revised.Containers[idx].VolumeMounts
				result = true
			}

			// check Resources
			if splcommon.CompareByMarshall(&current.Containers[idx].Resources, &revised.Containers[idx].Resources) {
				scopedLog.Info("Pod Container Resources differ",
					"current", current.Containers[idx].Resources,
					"revised", revised.Containers[idx].Resources)
				current.Containers[idx].Resources = revised.Containers[idx].Resources
				result = true
			}

			// check Env
			if splcommon.CompareEnvs(current.Containers[idx].Env, revised.Containers[idx].Env) {
				scopedLog.Info("Pod Container Envs differ",
					"current", current.Containers[idx].Env,
					"revised", revised.Containers[idx].Env)
				current.Containers[idx].Env = revised.Containers[idx].Env
				result = true
			}
		}
	}

	return result
}

// SortStatefulSetSlices sorts required slices in a statefulSet
func SortStatefulSetSlices(current *corev1.PodSpec, name string) error {
	scopedLog := log.WithName("SortStatefulSetSlices").WithValues("name", name)

	// Sort tolerations
	splcommon.SortSlice(current.Tolerations, splcommon.SortFieldKey)

	// Sort volumes
	splcommon.SortSlice(current.Volumes, splcommon.SortFieldName)

	// Sort slices inside container specs
	for idx := range current.Containers {
		// Sort container ports
		splcommon.SortSlice(current.Containers[idx].Ports, splcommon.SortFieldContainerPort)

		// Sort VolumeMounts
		splcommon.SortSlice(current.Containers[idx].VolumeMounts, splcommon.SortFieldName)

		// Sort env variables
		splcommon.SortSlice(current.Containers[idx].Env, splcommon.SortFieldName)
	}
	scopedLog.Info("Successfully sorted slices in statefulSet")

	return nil
}

// MergeServiceSpecUpdates merges the current and revised spec of the service object
func MergeServiceSpecUpdates(current *corev1.ServiceSpec, revised *corev1.ServiceSpec, name string) bool {
	scopedLog := log.WithName("MergeServiceSpecUpdates").WithValues("name", name)
	result := false

	// check service Type
	if current.Type != revised.Type {
		scopedLog.Info("Service Type differs",
			"current", current.Type,
			"revised", revised.Type)
		current.Type = revised.Type
		result = true
	}

	if current.ExternalName != revised.ExternalName {
		scopedLog.Info("External Name differs",
			"current", current.ExternalName,
			"revised", revised.ExternalName)
		current.ExternalName = revised.ExternalName
		result = true
	}

	if current.ExternalTrafficPolicy != revised.ExternalTrafficPolicy {
		scopedLog.Info("External Traffic Policy differs",
			"current", current.ExternalTrafficPolicy,
			"revised", revised.ExternalTrafficPolicy)
		current.ExternalTrafficPolicy = revised.ExternalTrafficPolicy
		result = true
	}

	if splcommon.CompareSortedStrings(current.ExternalIPs, revised.ExternalIPs) {
		scopedLog.Info("External IPs differs",
			"current", current.ExternalIPs,
			"revised", revised.ExternalIPs)
		current.ExternalIPs = revised.ExternalIPs
		result = true
	}

	// check for changes in Ports
	if splcommon.CompareServicePorts(current.Ports, revised.Ports) {
		scopedLog.Info("Service Ports differs",
			"current", current.Ports,
			"revised", revised.Ports)
		current.Ports = revised.Ports
		result = true
	}

	return result
}
