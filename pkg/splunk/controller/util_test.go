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

package controller

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestMergePodUpdates(t *testing.T) {
	var current, revised corev1.PodTemplateSpec
	name := "test-pod"
	matcher := func() bool { return false }

	podUpdateTester := func(param string) {
		if !MergePodUpdates(&current, &revised, name) {
			t.Errorf("MergePodUpdates() returned %t; want %t", false, true)
		}
		if !matcher() {
			t.Errorf("MergePodUpdates() to detect change: %s", param)
		}
		if MergePodUpdates(&current, &revised, name) {
			t.Errorf("MergePodUpdates() re-run returned %t; want %t", true, false)
		}
	}

	// should be no updates to merge if they are empty
	if MergePodUpdates(&current, &revised, name) {
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

	// check new container added
	revised.Spec.Containers = []corev1.Container{{Image: "splunk/splunk"}}
	matcher = func() bool { return reflect.DeepEqual(current.Spec.Containers, revised.Spec.Containers) }
	podUpdateTester("Container added")

	// check container different Image
	revised.Spec.Containers = []corev1.Container{{Image: "splunk/spark"}}
	matcher = func() bool { return reflect.DeepEqual(current.Spec.Containers, revised.Spec.Containers) }
	podUpdateTester("Container Image")

	// check container different Ports
	revised.Spec.Containers[0].Ports = []corev1.ContainerPort{{ContainerPort: 8000}}
	matcher = func() bool { return reflect.DeepEqual(current.Spec.Containers, revised.Spec.Containers) }
	podUpdateTester("Container Ports")

	// check container different VolumeMounts
	revised.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "mnt-spark"}}
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

	// check container removed
	revised.Spec.Containers = []corev1.Container{}
	matcher = func() bool { return reflect.DeepEqual(current.Spec.Containers, revised.Spec.Containers) }
	podUpdateTester("Container removed")
}

func TestMergeServiceSpecUpdates(t *testing.T) {
	var current, revised corev1.ServiceSpec
	name := "test-svc"
	matcher := func() bool { return false }

	svcUpdateTester := func(param string) {
		if !MergeServiceSpecUpdates(&current, &revised, name) {
			t.Errorf("MergeServiceSpecUpdates() returned %t; want %t", false, true)
		}
		if !matcher() {
			t.Errorf("MergeServiceSpecUpdates() to detect change: %s", param)
		}
		if MergeServiceSpecUpdates(&current, &revised, name) {
			t.Errorf("MergeServiceSpecUpdates() re-run returned %t; want %t", true, false)
		}
	}

	// should be no updates to merge if they are empty
	if MergeServiceSpecUpdates(&current, &revised, name) {
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
