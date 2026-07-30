// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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

package splkcontroller

import (
	"context"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

// MergePodUpdates looks for material differences between a Pod's current
// config and a revised config. It merges material changes from revised to
// current. This enables us to minimize updates. It returns true if there
// are material differences between them, or false otherwise.
func MergePodUpdates(ctx context.Context, current *corev1.PodTemplateSpec, revised *corev1.PodTemplateSpec, name string) bool {
	result := MergePodSpecUpdates(ctx, &current.Spec, &revised.Spec, name)
	if MergePodMetaUpdates(ctx, &current.ObjectMeta, &revised.ObjectMeta, name) {
		result = true
	}
	return result
}

// MergePodMetaUpdates looks for material differences between a Pod's current
// meta data and a revised meta data. It merges material changes from revised to
// current. This enables us to minimize updates. It returns true if there
// are material differences between them, or false otherwise.
func MergePodMetaUpdates(ctx context.Context, current *metav1.ObjectMeta, revised *metav1.ObjectMeta, name string) bool {
	scopedLog := logging.FromContext(ctx).With("func", "MergePodMetaUpdates", "name", name)
	result := false

	// check Annotations
	if !reflect.DeepEqual(current.Annotations, revised.Annotations) {
		scopedLog.InfoContext(ctx, "container Annotations differ", "current", current.Annotations, "revised", revised.Annotations)
		current.Annotations = revised.Annotations
		result = true
	}

	// check Labels
	if !reflect.DeepEqual(current.Labels, revised.Labels) {
		scopedLog.InfoContext(ctx, "container Labels differ", "current", current.Labels, "revised", revised.Labels)
		current.Labels = revised.Labels
		result = true
	}

	return result
}

// MergePodSpecUpdates looks for material differences between a Pod's current
// desired spec and a revised spec. It merges material changes from revised to
// current. This enables us to minimize updates. It returns true if there
// are material differences between them, or false otherwise.
func MergePodSpecUpdates(ctx context.Context, current *corev1.PodSpec, revised *corev1.PodSpec, name string) bool {
	scopedLog := logging.FromContext(ctx).With("func", "MergePodUpdates", "name", name)
	result := false

	// check for changes in ServiceAccount
	if splcommon.CompareByMarshall(current.ServiceAccountName, revised.ServiceAccountName) {
		scopedLog.InfoContext(ctx, "pod service account differs",
			"current", current.ServiceAccountName,
			"revised", revised.ServiceAccountName)
		current.ServiceAccountName = revised.ServiceAccountName
		result = true
	}

	// check for changes in Affinity
	if splcommon.CompareByMarshall(current.Affinity, revised.Affinity) {
		scopedLog.InfoContext(ctx, "pod Affinity differs",
			"current", current.Affinity,
			"revised", revised.Affinity)
		current.Affinity = revised.Affinity
		result = true
	}

	// check for changes in Tolerations
	if splcommon.CompareTolerations(current.Tolerations, revised.Tolerations) {
		scopedLog.InfoContext(ctx, "pod Tolerations differs",
			"current", current.Tolerations,
			"revised", revised.Tolerations)
		current.Tolerations = revised.Tolerations
		result = true
	}

	// check for changes in TopologySpreadConstraint
	if splcommon.CompareTopologySpreadConstraints(current.TopologySpreadConstraints, revised.TopologySpreadConstraints) {
		scopedLog.InfoContext(ctx, "pod TopologySpreadConstraint differs",
			"current", current.TopologySpreadConstraints,
			"revised", revised.TopologySpreadConstraints)
		current.TopologySpreadConstraints = revised.TopologySpreadConstraints
		result = true
	}

	// check for changes in ImagePullSecrets
	if splcommon.CompareImagePullSecrets(current.ImagePullSecrets, revised.ImagePullSecrets) {
		scopedLog.InfoContext(ctx, "pod ImagePullSecrets differs",
			"current", current.ImagePullSecrets,
			"revised", revised.ImagePullSecrets)
		current.ImagePullSecrets = revised.ImagePullSecrets
		result = true
	}

	// check for changes in SchedulerName
	if current.SchedulerName != revised.SchedulerName {
		scopedLog.InfoContext(ctx, "pod SchedulerName differs",
			"current", current.SchedulerName,
			"revised", revised.SchedulerName)
		current.SchedulerName = revised.SchedulerName
		result = true
	}

	// Check for changes in Volumes
	if splcommon.CompareVolumes(current.Volumes, revised.Volumes) {
		scopedLog.InfoContext(ctx, "pod Volumes differ",
			"current", current.Volumes,
			"revised", revised.Volumes)
		current.Volumes = revised.Volumes
		result = true
	}

	// Check for changes in Init containers
	if len(current.InitContainers) != len(revised.InitContainers) {
		scopedLog.InfoContext(ctx, "pod init containers  differ",
			"current", len(current.InitContainers),
			"revised", len(revised.InitContainers))
		current.InitContainers = revised.InitContainers
		result = true
	} else {
		for idx := range current.InitContainers {
			// check Image
			if current.InitContainers[idx].Image != revised.InitContainers[idx].Image {
				scopedLog.InfoContext(ctx, "init Container Images differ",
					"current", current.InitContainers[idx].Image,
					"revised", revised.InitContainers[idx].Image)
				current.InitContainers[idx].Image = revised.InitContainers[idx].Image
				result = true
			}
		}
	}

	// check for changes in container images; assume that the ordering is same for pods with > 1 container
	if len(current.Containers) != len(revised.Containers) {
		scopedLog.InfoContext(ctx, "pod Container counts differ",
			"current", len(current.Containers),
			"revised", len(revised.Containers))
		current.Containers = revised.Containers
		result = true
	} else {
		for idx := range current.Containers {
			// check Image
			if current.Containers[idx].Image != revised.Containers[idx].Image {
				scopedLog.InfoContext(ctx, "pod Container Images differ",
					"current", current.Containers[idx].Image,
					"revised", revised.Containers[idx].Image)
				current.Containers[idx].Image = revised.Containers[idx].Image
				result = true
			}

			// check Ports
			if splcommon.CompareContainerPorts(current.Containers[idx].Ports, revised.Containers[idx].Ports) {
				scopedLog.InfoContext(ctx, "pod Container Ports differ",
					"current", current.Containers[idx].Ports,
					"revised", revised.Containers[idx].Ports)
				current.Containers[idx].Ports = revised.Containers[idx].Ports
				result = true
			}

			// check VolumeMounts
			if splcommon.CompareVolumeMounts(current.Containers[idx].VolumeMounts, revised.Containers[idx].VolumeMounts) {
				scopedLog.InfoContext(ctx, "pod Container VolumeMounts differ",
					"current", current.Containers[idx].VolumeMounts,
					"revised", revised.Containers[idx].VolumeMounts)
				current.Containers[idx].VolumeMounts = revised.Containers[idx].VolumeMounts
				result = true
			}

			// check Resources
			if splcommon.CompareByMarshall(&current.Containers[idx].Resources, &revised.Containers[idx].Resources) {
				scopedLog.InfoContext(ctx, "pod Container Resources differ",
					"current", current.Containers[idx].Resources,
					"revised", revised.Containers[idx].Resources)
				current.Containers[idx].Resources = revised.Containers[idx].Resources
				result = true
			}

			// check Env
			if splcommon.CompareEnvs(current.Containers[idx].Env, revised.Containers[idx].Env) {
				scopedLog.InfoContext(ctx, "pod Container Envs differ",
					"current", current.Containers[idx].Env,
					"revised", revised.Containers[idx].Env)
				current.Containers[idx].Env = revised.Containers[idx].Env
				result = true
			}

			// check probes
			if hasProbeChanged(current.Containers[idx].LivenessProbe, revised.Containers[idx].LivenessProbe) {
				scopedLog.InfoContext(ctx, "pod Container Liveness Probe differ",
					"current", current.Containers[idx].LivenessProbe,
					"revised", revised.Containers[idx].LivenessProbe)
				current.Containers[idx].LivenessProbe = revised.Containers[idx].LivenessProbe
				result = true
			}

			if hasProbeChanged(current.Containers[idx].ReadinessProbe, revised.Containers[idx].ReadinessProbe) {
				scopedLog.InfoContext(ctx, "pod Container ReadinessProbe Probe differ",
					"current", current.Containers[idx].ReadinessProbe,
					"revised", revised.Containers[idx].ReadinessProbe)
				current.Containers[idx].ReadinessProbe = revised.Containers[idx].ReadinessProbe
				result = true
			}

			if hasProbeChanged(current.Containers[idx].StartupProbe, revised.Containers[idx].StartupProbe) {
				scopedLog.InfoContext(ctx, "pod Container StartupProbe Probe differ",
					"current", current.Containers[idx].StartupProbe,
					"revised", revised.Containers[idx].StartupProbe)
				current.Containers[idx].StartupProbe = revised.Containers[idx].StartupProbe
				result = true
			}
		}
	}

	return result
}

// SortStatefulSetSlices sorts required slices in a statefulSet
func SortStatefulSetSlices(ctx context.Context, current *corev1.PodSpec, name string) error {
	scopedLog := logging.FromContext(ctx).With("func", "SortStatefulSetSlices", "name", name)

	// Sort tolerations
	splcommon.SortSlice(current.Tolerations, splcommon.SortFieldKey)

	// Sort TopologySpreadConstraints
	splcommon.SortSlice(current.TopologySpreadConstraints, splcommon.SortFieldTopologyKey)

	// Sort volumes
	splcommon.SortSlice(current.Volumes, splcommon.SortFieldName)

	// Sort ImagePullSecrets
	splcommon.SortSlice(current.ImagePullSecrets, splcommon.SortFieldName)

	// Sort slices inside container specs
	for idx := range current.Containers {
		// Sort container ports
		splcommon.SortSlice(current.Containers[idx].Ports, splcommon.SortFieldContainerPort)

		// Sort VolumeMounts
		splcommon.SortSlice(current.Containers[idx].VolumeMounts, splcommon.SortFieldName)

		// Sort env variables
		splcommon.SortSlice(current.Containers[idx].Env, splcommon.SortFieldName)
	}
	scopedLog.InfoContext(ctx, "successfully sorted slices in statefulSet")

	return nil
}

// MergeServiceSpecUpdates merges the current and revised spec of the service object
func MergeServiceSpecUpdates(ctx context.Context, current *corev1.ServiceSpec, revised *corev1.ServiceSpec, name string) bool {
	scopedLog := logging.FromContext(ctx).With("func", "MergeServiceSpecUpdates", "name", name)
	result := false

	// check service Type. An empty revised.Type means the controller did not
	// explicitly set one; Kubernetes defaults it to ClusterIP server-side.
	// Treat the empty value as ClusterIP so we (1) avoid an endless
	// reconcile->update->watch loop against the API-server-defaulted ClusterIP,
	// while (2) still driving a previously customized Service (e.g. LoadBalancer
	// or NodePort set via spec.serviceTemplate) back to the default ClusterIP
	// when the override is removed from the CR.
	currentType := current.Type
	if currentType == "" {
		currentType = corev1.ServiceTypeClusterIP
	}
	revisedType := revised.Type
	if revisedType == "" {
		revisedType = corev1.ServiceTypeClusterIP
	}
	if currentType != revisedType {
		scopedLog.InfoContext(ctx, "service Type differs",
			"current", current.Type,
			"revised", revisedType)
		current.Type = revisedType
		result = true
	}

	if current.ExternalName != revised.ExternalName {
		scopedLog.InfoContext(ctx, "external Name differs",
			"current", current.ExternalName,
			"revised", revised.ExternalName)
		current.ExternalName = revised.ExternalName
		result = true
	}

	if current.ExternalTrafficPolicy != revised.ExternalTrafficPolicy {
		scopedLog.InfoContext(ctx, "external Traffic Policy differs",
			"current", current.ExternalTrafficPolicy,
			"revised", revised.ExternalTrafficPolicy)
		current.ExternalTrafficPolicy = revised.ExternalTrafficPolicy
		result = true
	}

	if current.PublishNotReadyAddresses != revised.PublishNotReadyAddresses {
		scopedLog.InfoContext(ctx, "publish Not Ready Addresses differs",
			"current", current.PublishNotReadyAddresses,
			"revised", revised.PublishNotReadyAddresses)
		current.PublishNotReadyAddresses = revised.PublishNotReadyAddresses
		result = true
	}

	if splcommon.CompareSortedStrings(current.ExternalIPs, revised.ExternalIPs) {
		scopedLog.InfoContext(ctx, "external IPs differs",
			"current", current.ExternalIPs,
			"revised", revised.ExternalIPs)
		current.ExternalIPs = revised.ExternalIPs
		result = true
	}

	// check for changes in Ports
	if splcommon.CompareServicePorts(current.Ports, revised.Ports) {
		scopedLog.InfoContext(ctx, "service Ports differs",
			"current", current.Ports,
			"revised", revised.Ports)
		current.Ports = revised.Ports
		result = true
	}

	return result
}

// hasProbeChanged checks for changes in given current probe
func hasProbeChanged(currentProbe *corev1.Probe, revisedProbe *corev1.Probe) bool {
	if currentProbe == nil {
		return revisedProbe != nil
	}
	if currentProbe.InitialDelaySeconds != revisedProbe.InitialDelaySeconds {
		return true
	}
	if currentProbe.TimeoutSeconds != revisedProbe.TimeoutSeconds {
		return true
	}
	if currentProbe.PeriodSeconds != revisedProbe.PeriodSeconds {
		return true
	}
	if currentProbe.FailureThreshold != revisedProbe.FailureThreshold {
		return true
	}
	return false
}
