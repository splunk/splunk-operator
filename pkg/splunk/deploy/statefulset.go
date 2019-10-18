// Copyright (c) 2018-2019 Splunk Inc. All rights reserved.
// Use of this source code is governed by an Apache 2 style
// license that can be found in the LICENSE file.

package deploy

import (
	"bytes"
	"log"
	"context"
	"sort"
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	"github.com/splunk/splunk-operator/pkg/splunk/spark"
)

// ApplySplunkStatefulSet creates or updates a Kubernetes StatefulSet for a given type of Splunk Enterprise instance (indexers or search heads).
func ApplySplunkStatefulSet(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType enterprise.SplunkInstanceType, replicas int, envVariables []corev1.EnvVar) error {

	statefulSet, err := enterprise.GetSplunkStatefulSet(cr, instanceType, replicas, envVariables)
	if err != nil {
		return err
	}

	return ApplyStatefulSet(client, statefulSet)
}

// ApplySparkStatefulSet creates or updates a Kubernetes StatefulSet for a given type of Spark instance (master or workers).
func ApplySparkStatefulSet(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType spark.SparkInstanceType, replicas int, envVariables []corev1.EnvVar, containerPorts []corev1.ContainerPort) error {

	statefulSet, err := spark.GetSparkStatefulSet(cr, instanceType, replicas, envVariables, containerPorts)
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
		Name: statefulSet.Name,
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
		log.Printf("StatefulSet %s Replicas differ: %d != %d",
			current.GetObjectMeta().GetName(), *current.Spec.Replicas, *revised.Spec.Replicas)
		current.Spec.Replicas = revised.Spec.Replicas
		result = true
	}

	// check for changes in Affinity
	if CompareByMarshall(&current.Spec.Template.Spec.Affinity, &revised.Spec.Template.Spec.Affinity) {
		log.Printf("StatefulSet %s Affinity differs: \"%v\" != \"%v\"",
			current.GetObjectMeta().GetName(), current.Spec.Template.Spec.Affinity,
			revised.Spec.Template.Spec.Affinity)
		current.Spec.Template.Spec.Affinity = revised.Spec.Template.Spec.Affinity
		result = true
	}

	// check for changes in SchedulerName
	if CompareByMarshall(&current.Spec.Template.Spec.SchedulerName, &revised.Spec.Template.Spec.SchedulerName) {
		log.Printf("StatefulSet %s SchedulerName differs: \"%v\" != \"%v\"",
			current.GetObjectMeta().GetName(), current.Spec.Template.Spec.SchedulerName,
			revised.Spec.Template.Spec.SchedulerName)
		current.Spec.Template.Spec.SchedulerName = revised.Spec.Template.Spec.SchedulerName
		result = true
	}

	// check for changes in container images; assume that the ordering is same for pods with > 1 container
	if len(current.Spec.Template.Spec.Containers) != len(current.Spec.Template.Spec.Containers) {
		log.Printf("StatefulSet %s Container counts differ: %d != %d",
			current.GetObjectMeta().GetName(), len(current.Spec.Template.Spec.Containers),
			len(revised.Spec.Template.Spec.Containers))
		current.Spec.Template.Spec.Containers = revised.Spec.Template.Spec.Containers
		result = true
	}
	for idx := range current.Spec.Template.Spec.Containers {
		// check Image
		if current.Spec.Template.Spec.Containers[idx].Image != revised.Spec.Template.Spec.Containers[idx].Image {
			log.Printf("StatefulSet %s Container Images differ: \"%s\" != \"%s\"",
				current.GetObjectMeta().GetName(), current.Spec.Template.Spec.Containers[idx].Image,
				revised.Spec.Template.Spec.Containers[idx].Image)
			current.Spec.Template.Spec.Containers[idx].Image = revised.Spec.Template.Spec.Containers[idx].Image
			result = true
		}

		// check Ports
		if ComparePorts(current.Spec.Template.Spec.Containers[idx].Ports, revised.Spec.Template.Spec.Containers[idx].Ports) {
			log.Printf("StatefulSet %s Container Ports differ: \"%v\" != \"%v\"",
				current.GetObjectMeta().GetName(), current.Spec.Template.Spec.Containers[idx].Ports,
				revised.Spec.Template.Spec.Containers[idx].Ports)
			current.Spec.Template.Spec.Containers[idx].Ports = revised.Spec.Template.Spec.Containers[idx].Ports
			result = true
		}

		// check VolumeMounts
		if CompareVolumeMounts(current.Spec.Template.Spec.Containers[idx].VolumeMounts, revised.Spec.Template.Spec.Containers[idx].VolumeMounts) {
			log.Printf("StatefulSet %s Container VolumeMounts differ: \"%v\" != \"%v\"",
				current.GetObjectMeta().GetName(), current.Spec.Template.Spec.Containers[idx].VolumeMounts,
				revised.Spec.Template.Spec.Containers[idx].VolumeMounts)
			current.Spec.Template.Spec.Containers[idx].VolumeMounts = revised.Spec.Template.Spec.Containers[idx].VolumeMounts
			result = true
		}

		// check Resources
		if CompareByMarshall(&current.Spec.Template.Spec.Containers[idx].Resources, &revised.Spec.Template.Spec.Containers[idx].Resources) {
			log.Printf("StatefulSet %s Container Resources differ: \"%v\" != \"%v\"",
				current.GetObjectMeta().GetName(), current.Spec.Template.Spec.Containers[idx].Resources,
				revised.Spec.Template.Spec.Containers[idx].Resources)
			current.Spec.Template.Spec.Containers[idx].Resources = revised.Spec.Template.Spec.Containers[idx].Resources
			result = true
		}
	}

	return result
}

// ComparePorts is a generic comparer of two Kubernetes ContainerPorts.
// It returns true if there are material differences between them, or false otherwise.
// TODO: could use refactoring; lots of boilerplate copy-pasta here
func ComparePorts(a []corev1.ContainerPort, b []corev1.ContainerPort) bool {
	// first, check for short-circuit opportunity
	if len(a) != len(b) {
		return true
	}

	// make sorted copy of a
	aSorted := make([]corev1.ContainerPort, len(a))
	copy(aSorted, a)
	sort.Slice(aSorted, func(i, j int) bool { return aSorted[i].ContainerPort < aSorted[j].ContainerPort })

	// make sorted copy of b
	bSorted := make([]corev1.ContainerPort, len(b))
	copy(bSorted, b)
	sort.Slice(bSorted, func(i, j int) bool { return bSorted[i].ContainerPort < bSorted[j].ContainerPort })

	// iterate elements, checking for differences
	for n := range aSorted {
		if aSorted[n] != bSorted[n] {
			return true
		}
	}

	return false
}

// CompareEnv is a generic comparer of two Kubernetes Env variables.
// It returns true if there are material differences between them, or false otherwise.
func CompareEnvs(a []corev1.EnvVar, b []corev1.EnvVar) bool {
	// first, check for short-circuit opportunity
	if len(a) != len(b) {
		return true
	}

	// make sorted copy of a
	aSorted := make([]corev1.EnvVar, len(a))
	copy(aSorted, a)
	sort.Slice(aSorted, func(i, j int) bool { return aSorted[i].Name < aSorted[j].Name })

	// make sorted copy of b
	bSorted := make([]corev1.EnvVar, len(b))
	copy(bSorted, b)
	sort.Slice(bSorted, func(i, j int) bool { return bSorted[i].Name < bSorted[j].Name })

	// iterate elements, checking for differences
	for n := range aSorted {
		if aSorted[n] != bSorted[n] {
			return true
		}
	}

	return false
}

// CompareVolumeMounts is a generic comparer of two Kubernetes VolumeMounts.
// It returns true if there are material differences between them, or false otherwise.
func CompareVolumeMounts(a []corev1.VolumeMount, b []corev1.VolumeMount) bool {
	// first, check for short-circuit opportunity
	if len(a) != len(b) {
		return true
	}

	// make sorted copy of a
	aSorted := make([]corev1.VolumeMount, len(a))
	copy(aSorted, a)
	sort.Slice(aSorted, func(i, j int) bool { return aSorted[i].Name < aSorted[j].Name })

	// make sorted copy of b
	bSorted := make([]corev1.VolumeMount, len(b))
	copy(bSorted, b)
	sort.Slice(bSorted, func(i, j int) bool { return bSorted[i].Name < bSorted[j].Name })

	// iterate elements, checking for differences
	for n := range aSorted {
		if aSorted[n] != bSorted[n] {
			return true
		}
	}

	return false
}

// CompareByMarshall compares two Kubernetes objects by marshalling them to JSON.
// It returns true if there are differences between the two marshalled values, or false otherwise.
func CompareByMarshall(a interface{}, b interface{}) bool {
	aBytes, err := json.Marshal(a)
	if err != nil {
		return true
	}

	bBytes, err := json.Marshal(b)
	if err != nil {
		return true
	}

	if bytes.Compare(aBytes, bBytes) != 0 {
		return true
	}

	return false
}
