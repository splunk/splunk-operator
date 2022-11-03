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

package controller

import (
	"context"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestMergePodUpdates(t *testing.T) {
	ctx := context.TODO()
	var current, revised corev1.PodTemplateSpec
	name := "test-pod"
	matcher := func() bool { return false }

	podUpdateTester := func(param string) {
		if !MergePodUpdates(ctx, &current, &revised, name) {
			t.Errorf("MergePodUpdates() returned %t; want %t", false, true)
		}
		if !matcher() {
			t.Errorf("MergePodUpdates() to detect change: %s", param)
		}
		if MergePodUpdates(ctx, &current, &revised, name) {
			t.Errorf("MergePodUpdates() re-run returned %t; want %t", true, false)
		}
	}

	// should be no updates to merge if they are empty
	if MergePodUpdates(ctx, &current, &revised, name) {
		t.Errorf("MergePodUpdates() returned %t; want %t", true, false)
	}

	// check Affinity
	revised.Spec.Affinity = &corev1.Affinity{
		PodAffinity: &corev1.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{Namespaces: []string{"test"}},
			},
		},
	}
	matcher = func() bool { return current.Spec.Affinity == revised.Spec.Affinity }
	podUpdateTester("Affinity")

	// check SchedulerName
	revised.Spec.SchedulerName = "gp2"
	matcher = func() bool { return current.Spec.SchedulerName == revised.Spec.SchedulerName }
	podUpdateTester("SchedulerName")

	// check new Volume added
	revised.Spec.Volumes = []corev1.Volume{{Name: "new-volume-added"}}
	matcher = func() bool { return reflect.DeepEqual(current.Spec.Volumes, revised.Spec.Volumes) }
	podUpdateTester("Volume added")

	// check Volume updated
	current.Spec.Volumes = []corev1.Volume{{Name: "test-volume", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "secret1"}}}}
	revised.Spec.Volumes = []corev1.Volume{{Name: "test-volume", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "secret2"}}}}
	matcher = func() bool { return reflect.DeepEqual(current.Spec.Volumes, revised.Spec.Volumes) }
	podUpdateTester("Volume updated - secret name change")

	defaultMode := int32(440)
	current.Spec.Volumes = []corev1.Volume{{Name: "test-volume", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "secret1"}}}}
	revised.Spec.Volumes = []corev1.Volume{{Name: "test-volume", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "secret1", DefaultMode: &defaultMode}}}}
	matcher = func() bool { return reflect.DeepEqual(current.Spec.Volumes, revised.Spec.Volumes) }
	podUpdateTester("Volume updated - default mode chanage")

	// check container different Annotations
	revised.ObjectMeta.Annotations = map[string]string{"one": "two"}
	matcher = func() bool { return reflect.DeepEqual(current.ObjectMeta.Annotations, revised.ObjectMeta.Annotations) }
	podUpdateTester("Annotations")

	// check container different Labels
	revised.ObjectMeta.Labels = map[string]string{"one": "two"}
	matcher = func() bool { return reflect.DeepEqual(current.ObjectMeta.Labels, revised.ObjectMeta.Labels) }
	podUpdateTester("Labels")

	// check new init container added
	revised.Spec.InitContainers = []corev1.Container{{Image: "splunk/splunk:a.b.c"}}
	matcher = func() bool { return reflect.DeepEqual(current.Spec.InitContainers, revised.Spec.InitContainers) }
	podUpdateTester("InitContainer added")

	// check init container image modified
	revised.Spec.InitContainers = []corev1.Container{{Image: "splunk/splunk:a.b.d"}}
	matcher = func() bool { return reflect.DeepEqual(current.Spec.InitContainers, revised.Spec.InitContainers) }
	podUpdateTester("InitContainer image changed")

	// check new container added
	revised.Spec.Containers = []corev1.Container{{Image: "splunk/splunk"}}
	matcher = func() bool { return reflect.DeepEqual(current.Spec.Containers, revised.Spec.Containers) }
	podUpdateTester("Container added")

	// check container different Ports
	revised.Spec.Containers = []corev1.Container{{Image: "splunk/splunk"}}
	revised.Spec.Containers[0].Ports = []corev1.ContainerPort{{ContainerPort: 8000}}
	matcher = func() bool { return reflect.DeepEqual(current.Spec.Containers, revised.Spec.Containers) }
	podUpdateTester("Container Ports")

	// check container different VolumeMounts
	revised.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "mnt-splunk"}}
	matcher = func() bool { return reflect.DeepEqual(current.Spec.Containers, revised.Spec.Containers) }
	podUpdateTester("Container VolumeMounts")

	// check container different Resources
	revised.Spec.Containers[0].Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("0.25"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
	}
	matcher = func() bool { return reflect.DeepEqual(current.Spec.Containers, revised.Spec.Containers) }
	podUpdateTester("Container Resources")

	// check pod env update
	current.Spec.Containers[0].Env = append(current.Spec.Containers[0].Env, corev1.EnvVar{
		Name:  "SPLUNK_DEFAULTS_URL",
		Value: "defaults1.yaml",
	})
	revised.Spec.Containers[0].Env = append(revised.Spec.Containers[0].Env, corev1.EnvVar{
		Name:  "SPLUNK_DEFAULTS_URL",
		Value: "defaults2.yaml",
	})
	matcher = func() bool { return reflect.DeepEqual(current.Spec.Containers[0].Env, revised.Spec.Containers[0].Env) }
	podUpdateTester("Pod Env changed")

	// Check if the liveness probe initalDelay is updated
	revised.Spec.Containers[0].LivenessProbe = &corev1.Probe{InitialDelaySeconds: 5000}
	current.Spec.Containers[0].LivenessProbe = &corev1.Probe{InitialDelaySeconds: 10}
	matcher = func() bool {
		return current.Spec.Containers[0].LivenessProbe.InitialDelaySeconds == revised.Spec.Containers[0].LivenessProbe.InitialDelaySeconds
	}
	podUpdateTester("Pod LivenessProbe changed")

	// Check if the readiness probe initalDelay is updated
	revised.Spec.Containers[0].ReadinessProbe = &corev1.Probe{InitialDelaySeconds: 200}
	current.Spec.Containers[0].ReadinessProbe = &corev1.Probe{InitialDelaySeconds: 30}
	matcher = func() bool {
		return current.Spec.Containers[0].ReadinessProbe.InitialDelaySeconds == revised.Spec.Containers[0].ReadinessProbe.InitialDelaySeconds
	}
	podUpdateTester("Pod ReadinessProbe changed")

	// Check if the startup probe initalDelay is updated
	revised.Spec.Containers[0].StartupProbe = &corev1.Probe{InitialDelaySeconds: 200}
	current.Spec.Containers[0].StartupProbe = &corev1.Probe{InitialDelaySeconds: 30}
	matcher = func() bool {
		return current.Spec.Containers[0].StartupProbe.InitialDelaySeconds == revised.Spec.Containers[0].StartupProbe.InitialDelaySeconds
	}
	podUpdateTester("Pod ReadinessProbe changed")

	// check container removed
	revised.Spec.Containers = []corev1.Container{}
	matcher = func() bool { return reflect.DeepEqual(current.Spec.Containers, revised.Spec.Containers) }
	podUpdateTester("Container removed")
}

func TestMergeServiceSpecUpdates(t *testing.T) {
	ctx := context.TODO()
	var current, revised corev1.ServiceSpec
	name := "test-svc"
	matcher := func() bool { return false }

	svcUpdateTester := func(param string) {
		if !MergeServiceSpecUpdates(ctx, &current, &revised, name) {
			t.Errorf("MergeServiceSpecUpdates() returned %t; want %t", false, true)
		}
		if !matcher() {
			t.Errorf("MergeServiceSpecUpdates() to detect change: %s", param)
		}
		if MergeServiceSpecUpdates(ctx, &current, &revised, name) {
			t.Errorf("MergeServiceSpecUpdates() re-run returned %t; want %t", true, false)
		}
	}

	// should be no updates to merge if they are empty
	if MergeServiceSpecUpdates(ctx, &current, &revised, name) {
		t.Errorf("MergeServiceSpecUpdates() returned %t; want %t", true, false)
	}

	// check new Port added
	revised.Ports = []corev1.ServicePort{{Name: "new-port-added", Port: 32000}}
	matcher = func() bool { return reflect.DeepEqual(current.Ports, revised.Ports) }
	svcUpdateTester("Service Ports added")

	// check Port changed
	current.Ports = []corev1.ServicePort{{Name: "port-changed", Port: 32320}}
	revised.Ports = []corev1.ServicePort{{Name: "port-changed", Port: 32000}}
	matcher = func() bool { return reflect.DeepEqual(current.Ports, revised.Ports) }
	svcUpdateTester("Service Ports change")

	// new ExternalIPs
	revised.ExternalIPs = []string{"1.2.3.4"}
	matcher = func() bool { return reflect.DeepEqual(current.ExternalIPs, revised.ExternalIPs) }
	svcUpdateTester("Service ExternalIPs added")

	// updated ExternalIPs
	current.ExternalIPs = []string{"1.2.3.4"}
	revised.ExternalIPs = []string{"1.1.3.4"}
	matcher = func() bool { return reflect.DeepEqual(current.ExternalIPs, revised.ExternalIPs) }
	svcUpdateTester("Service ExternalIPs changed")

	// Type change
	current.Type = corev1.ServiceTypeClusterIP
	revised.Type = corev1.ServiceTypeNodePort
	matcher = func() bool { return current.Type == revised.Type }
	svcUpdateTester("Service Type changed")

	current.ExternalName = "splunk.example.com"
	revised.ExternalName = "splunk2.example.com"
	matcher = func() bool { return current.ExternalName == revised.ExternalName }
	svcUpdateTester("Service ExternalName changed")

	current.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
	revised.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeCluster
	matcher = func() bool { return current.ExternalTrafficPolicy == revised.ExternalTrafficPolicy }
	svcUpdateTester("Service ExternalTrafficPolicy changed")
}

func TestSortStatefulSetSlices(t *testing.T) {
	ctx := context.TODO()
	var unsorted, sorted corev1.PodSpec
	matcher := func() bool { return false }

	sortTester := func(sortSlice string) {
		SortStatefulSetSlices(ctx, &unsorted, sortSlice)
		if !matcher() {
			t.Errorf("SortStatefulSetSlices() didn't sort %s", sortSlice)
		}
	}

	// Check volume sorting
	unsorted.Volumes = []corev1.Volume{{Name: "bVolume"}, {Name: "aVolume"}}
	sorted.Volumes = []corev1.Volume{{Name: "aVolume"}, {Name: "bVolume"}}
	matcher = func() bool { return reflect.DeepEqual(sorted.Volumes, unsorted.Volumes) }
	sortTester("Volumes")

	// Check tolerations
	unsorted.Tolerations = []corev1.Toleration{{Key: "bKey"}, {Key: "aKey"}}
	sorted.Tolerations = []corev1.Toleration{{Key: "aKey"}, {Key: "bKey"}}
	matcher = func() bool { return reflect.DeepEqual(sorted.Tolerations, unsorted.Tolerations) }
	sortTester("Tolerations")

	// Create a container
	sorted.Containers = make([]corev1.Container, 1)
	unsorted.Containers = make([]corev1.Container, 1)

	// Check container port sorting
	unsorted.Containers[0].Ports = []corev1.ContainerPort{{ContainerPort: 8080}, {ContainerPort: 8000}}
	sorted.Containers[0].Ports = []corev1.ContainerPort{{ContainerPort: 8000}, {ContainerPort: 8080}}
	matcher = func() bool { return reflect.DeepEqual(sorted.Containers[0].Ports, unsorted.Containers[0].Ports) }
	sortTester("Container Ports")

	// Check volume mount sorting
	unsorted.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "bVolumeMount"}, {Name: "aVolumeMount"}}
	sorted.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "aVolumeMount"}, {Name: "bVolumeMount"}}
	matcher = func() bool {
		return reflect.DeepEqual(sorted.Containers[0].VolumeMounts, unsorted.Containers[0].VolumeMounts)
	}
	sortTester("Volume mounts")

	// Check env var sorting
	unsorted.Containers[0].Env = []corev1.EnvVar{{Name: "SPLUNK_ROLE", Value: "SPLUNK_INDEXER"}, {Name: "DECLARATIVE_ADMIN_PASSWORD", Value: "true"}}
	sorted.Containers[0].Env = []corev1.EnvVar{{Name: "DECLARATIVE_ADMIN_PASSWORD", Value: "true"}, {Name: "SPLUNK_ROLE", Value: "SPLUNK_INDEXER"}}
	matcher = func() bool {
		return reflect.DeepEqual(sorted.Containers[0].Env, unsorted.Containers[0].Env)
	}
	sortTester("Env variables")
}

func TestHasProbeChanged(t *testing.T) {
	var current, revised corev1.PodTemplateSpec
	revised.Spec.Containers = []corev1.Container{{Image: "splunk/splunk"}}
	revised.Spec.Containers[0].LivenessProbe = &corev1.Probe{InitialDelaySeconds: 120}

	current.Spec.Containers = []corev1.Container{{Image: "splunk/splunk"}}
	current.Spec.Containers[0].LivenessProbe = &corev1.Probe{InitialDelaySeconds: 100}

	// Check return is false when both probes are nil
	result := hasProbeChanged(nil, nil)
	if result {
		t.Errorf("Both probes nil. hasProbeChanged() returned %t; want %t", true, false)
	}

	// Check return is true when currentProbe is true and revisedProbe is not nil
	result = hasProbeChanged(nil, revised.Spec.Containers[0].LivenessProbe)
	if !result {
		t.Errorf("current Probe nil. hasProbeChanged() returned %t; want %t", false, true)
	}

	// Check return is true when current probe and revised probe InitialDelaySeconds is different
	result = hasProbeChanged(current.Spec.Containers[0].LivenessProbe, revised.Spec.Containers[0].LivenessProbe)
	if !result {
		t.Errorf("InitialDelaySeconds different. hasProbeChanged() returned %t; want %t", false, true)
	}

	// Check return is true when current probe and revised probe TimeoutSeconds is different
	current.Spec.Containers[0].LivenessProbe.InitialDelaySeconds = revised.Spec.Containers[0].LivenessProbe.InitialDelaySeconds
	current.Spec.Containers[0].LivenessProbe.TimeoutSeconds = 120
	revised.Spec.Containers[0].LivenessProbe.TimeoutSeconds = 100
	result = hasProbeChanged(current.Spec.Containers[0].LivenessProbe, revised.Spec.Containers[0].LivenessProbe)
	if !result {
		t.Errorf("TimoutSeconds different. hasProbeChanged() returned %t; want %t", false, true)
	}

	// Check return is true when current probe and revised probe PeriodSeconds is different
	current.Spec.Containers[0].LivenessProbe.TimeoutSeconds = revised.Spec.Containers[0].LivenessProbe.TimeoutSeconds
	current.Spec.Containers[0].LivenessProbe.PeriodSeconds = 120
	revised.Spec.Containers[0].LivenessProbe.PeriodSeconds = 100
	result = hasProbeChanged(current.Spec.Containers[0].LivenessProbe, revised.Spec.Containers[0].LivenessProbe)
	if !result {
		t.Errorf("PeriodSeconds different. hasProbeChanged() returned %t; want %t", false, true)
	}

	// Check return is true when current probe and revised probe FailureThreshold is different
	current.Spec.Containers[0].LivenessProbe.PeriodSeconds = revised.Spec.Containers[0].LivenessProbe.PeriodSeconds
	current.Spec.Containers[0].LivenessProbe.FailureThreshold = 120
	revised.Spec.Containers[0].LivenessProbe.FailureThreshold = 100
	result = hasProbeChanged(current.Spec.Containers[0].LivenessProbe, revised.Spec.Containers[0].LivenessProbe)
	if !result {
		t.Errorf("FailureThreshold different. hasProbeChanged() returned %t; want %t", false, true)
	}
}
