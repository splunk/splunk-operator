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

package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	// seed random number generator for splunk secret generation
	rand.Seed(time.Now().UnixNano())
}

// AsOwner returns an object to use for Kubernetes resource ownership references.
func AsOwner(cr MetaObject) metav1.OwnerReference {
	trueVar := true

	return metav1.OwnerReference{
		APIVersion: cr.GetObjectKind().GroupVersionKind().GroupVersion().String(),
		Kind:       cr.GetObjectKind().GroupVersionKind().Kind,
		Name:       cr.GetObjectMeta().GetName(),
		UID:        cr.GetObjectMeta().GetUID(),
		Controller: &trueVar,
	}
}

// AppendParentMeta appends parent's metadata to a child
func AppendParentMeta(child, parent metav1.Object) {
	// append labels from parent
	for k, v := range parent.GetLabels() {
		child.GetLabels()[k] = v
	}

	// append annotations from parent
	for k, v := range parent.GetAnnotations() {
		// ignore Annotations set by kubectl
		if !strings.HasPrefix(k, "kubectl.kubernetes.io/") {
			child.GetAnnotations()[k] = v
		}
	}
}

// ParseResourceQuantity parses and returns a resource quantity from a string.
func ParseResourceQuantity(str string, useIfEmpty string) (resource.Quantity, error) {
	var result resource.Quantity

	if str == "" {
		if useIfEmpty != "" {
			result = resource.MustParse(useIfEmpty)
		}
	} else {
		var err error
		result, err = resource.ParseQuantity(str)
		if err != nil {
			return result, fmt.Errorf("Invalid resource quantity \"%s\": %s", str, err)
		}
	}

	return result, nil
}

// GetServiceFQDN returns the fully qualified domain name for a Kubernetes service.
func GetServiceFQDN(namespace string, name string) string {
	clusterDomain := os.Getenv("CLUSTER_DOMAIN")
	if clusterDomain == "" {
		clusterDomain = "cluster.local"
	}
	return fmt.Sprintf(
		"%s.%s.svc.%s",
		name, namespace, clusterDomain,
	)
}

// GenerateSecret returns a randomly generated sequence of text that is n bytes in length.
func GenerateSecret(secretBytes string, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = secretBytes[rand.Int63()%int64(len(secretBytes))]
	}
	return b
}

// SortContainerPorts returns a sorted list of Kubernetes ContainerPorts.
func SortContainerPorts(ports []corev1.ContainerPort) []corev1.ContainerPort {
	sorted := make([]corev1.ContainerPort, len(ports))
	copy(sorted, ports)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ContainerPort < sorted[j].ContainerPort })
	return sorted
}

// SortServicePorts returns a sorted list of Kubernetes ServicePorts.
func SortServicePorts(ports []corev1.ServicePort) []corev1.ServicePort {
	sorted := make([]corev1.ServicePort, len(ports))
	copy(sorted, ports)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Port < sorted[j].Port })
	return sorted
}

// CompareContainerPorts is a generic comparer of two Kubernetes ContainerPorts.
// It returns true if there are material differences between them, or false otherwise.
// TODO: could use refactoring; lots of boilerplate copy-pasta here
func CompareContainerPorts(a []corev1.ContainerPort, b []corev1.ContainerPort) bool {
	return sortAndCompareSlices(a, b, "ContainerPort")
}

// CompareServicePorts is a generic comparer of two Kubernetes ServicePorts.
// It returns true if there are material differences between them, or false otherwise.
// TODO: could use refactoring; lots of boilerplate copy-pasta here
func CompareServicePorts(a []corev1.ServicePort, b []corev1.ServicePort) bool {
	return sortAndCompareSlices(a, b, "Port")
}

// CompareEnvs is a generic comparer of two Kubernetes Env variables.
// It returns true if there are material differences between them, or false otherwise.
func CompareEnvs(a []corev1.EnvVar, b []corev1.EnvVar) bool {
	return sortAndCompareSlices(a, b, "Name")
}

// CompareTolerations compares the 2 list of tolerations
func CompareTolerations(a []corev1.Toleration, b []corev1.Toleration) bool {
	return sortAndCompareSlices(a, b, "Key")
}

// CompareVolumes is a generic comparer of two Kubernetes Volumes.
// It returns true if there are material differences between them, or false otherwise.
func CompareVolumes(a []corev1.Volume, b []corev1.Volume) bool {
	return sortAndCompareSlices(a, b, "Name")
}

// CompareVolumeMounts is a generic comparer of two Kubernetes VolumeMounts.
// It returns true if there are material differences between them, or false otherwise.
func CompareVolumeMounts(a []corev1.VolumeMount, b []corev1.VolumeMount) bool {
	return sortAndCompareSlices(a, b, "Name")
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

// CompareSortedStrings returns true if there are differences between the two sorted lists of strings, or false otherwise.
func CompareSortedStrings(a []string, b []string) bool {
	if len(a) != len(b) {
		return true
	}

	sort.Strings(a)
	sort.Strings(b)

	if !reflect.DeepEqual(a, b) {
		return true
	}

	return false
}

// GetIstioAnnotations returns a map of istio annotations for a pod template
func GetIstioAnnotations(ports []corev1.ContainerPort) map[string]string {
	// list of ports within the deployments that we want istio to leave alone
	excludeOutboundPorts := []int32{8089, 8191, 9997, 7777, 9000, 17000, 17500, 19000}

	// calculate outbound port exclusions
	excludeOutboundPortsLookup := make(map[int32]bool)
	excludeOutboundPortsBuf := bytes.NewBufferString("")
	for idx := range excludeOutboundPorts {
		if excludeOutboundPortsBuf.Len() > 0 {
			fmt.Fprint(excludeOutboundPortsBuf, ",")
		}
		fmt.Fprintf(excludeOutboundPortsBuf, "%d", excludeOutboundPorts[idx])
		excludeOutboundPortsLookup[excludeOutboundPorts[idx]] = true
	}

	// calculate inbound port inclusions
	includeInboundPortsBuf := bytes.NewBufferString("")
	sortedPorts := SortContainerPorts(ports)
	for idx := range sortedPorts {
		_, skip := excludeOutboundPortsLookup[sortedPorts[idx].ContainerPort]
		if !skip {
			if includeInboundPortsBuf.Len() > 0 {
				fmt.Fprint(includeInboundPortsBuf, ",")
			}
			fmt.Fprintf(includeInboundPortsBuf, "%d", sortedPorts[idx].ContainerPort)
		}
	}

	return map[string]string{
		"traffic.sidecar.istio.io/excludeOutboundPorts": excludeOutboundPortsBuf.String(),
		"traffic.sidecar.istio.io/includeInboundPorts":  includeInboundPortsBuf.String(),
	}
}

// GetLabels returns a map of labels to use for managed components.
func GetLabels(component, name, identifier string) map[string]string {
	// see https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels
	return map[string]string{
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  component,
		"app.kubernetes.io/name":       name,
		"app.kubernetes.io/part-of":    fmt.Sprintf("splunk-%s-%s", identifier, component),
		"app.kubernetes.io/instance":   fmt.Sprintf("splunk-%s-%s", identifier, name),
	}
}

// AppendPodAntiAffinity appends a Kubernetes Affinity object to include anti-affinity for pods of the same type, and returns the result.
func AppendPodAntiAffinity(affinity *corev1.Affinity, identifier string, typeLabel string) *corev1.Affinity {
	if affinity == nil {
		affinity = &corev1.Affinity{}
	} else {
		affinity = affinity.DeepCopy()
	}

	if affinity.PodAntiAffinity == nil {
		affinity.PodAntiAffinity = &corev1.PodAntiAffinity{}
	}

	// prepare match expressions to match select labels
	matchExpressions := []metav1.LabelSelectorRequirement{
		{
			Key:      "app.kubernetes.io/instance",
			Operator: metav1.LabelSelectorOpIn,
			Values:   []string{fmt.Sprintf("splunk-%s-%s", identifier, typeLabel)},
		},
	}

	affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution = append(
		affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution,
		corev1.WeightedPodAffinityTerm{
			Weight: 100,
			PodAffinityTerm: corev1.PodAffinityTerm{
				LabelSelector: &metav1.LabelSelector{
					MatchExpressions: matchExpressions,
				},
				TopologyKey: "kubernetes.io/hostname",
			},
		},
	)

	return affinity
}

// ValidateImagePullPolicy checks validity of the ImagePullPolicy spec parameter, and returns error if it is invalid.
func ValidateImagePullPolicy(imagePullPolicy *string) error {
	// ImagePullPolicy
	if *imagePullPolicy == "" {
		*imagePullPolicy = os.Getenv("IMAGE_PULL_POLICY")
	}
	switch *imagePullPolicy {
	case "":
		*imagePullPolicy = "IfNotPresent"
		break
	case "Always":
		break
	case "IfNotPresent":
		break
	default:
		return fmt.Errorf("ImagePullPolicy must be one of \"Always\" or \"IfNotPresent\"; value=\"%s\"", *imagePullPolicy)
	}
	return nil
}

// ValidateResources checks resource requests and limits and sets defaults if not provided
func ValidateResources(resources *corev1.ResourceRequirements, defaults corev1.ResourceRequirements) {
	// check for nil maps
	if resources.Requests == nil {
		resources.Requests = make(corev1.ResourceList)
	}
	if resources.Limits == nil {
		resources.Limits = make(corev1.ResourceList)
	}

	// if not given, use default cpu requests
	_, ok := resources.Requests[corev1.ResourceCPU]
	if !ok {
		resources.Requests[corev1.ResourceCPU] = defaults.Requests[corev1.ResourceCPU]
	}

	// if not given, use default memory requests
	_, ok = resources.Requests[corev1.ResourceMemory]
	if !ok {
		resources.Requests[corev1.ResourceMemory] = defaults.Requests[corev1.ResourceMemory]
	}

	// if not given, use default cpu limits
	_, ok = resources.Limits[corev1.ResourceCPU]
	if !ok {
		resources.Limits[corev1.ResourceCPU] = defaults.Limits[corev1.ResourceCPU]
	}

	// if not given, use default memory limits
	_, ok = resources.Limits[corev1.ResourceMemory]
	if !ok {
		resources.Limits[corev1.ResourceMemory] = defaults.Limits[corev1.ResourceMemory]
	}
}

// ValidateSpec checks validity and makes default updates to a Spec, and returns error if something is wrong.
func ValidateSpec(spec *Spec, defaultResources corev1.ResourceRequirements) error {
	// make sure SchedulerName is not empty
	if spec.SchedulerName == "" {
		spec.SchedulerName = "default-scheduler"
	}

	// set default values for service template
	setServiceTemplateDefaults(spec)

	// if not provided, set default resource requests and limits
	ValidateResources(&spec.Resources, defaultResources)

	return ValidateImagePullPolicy(&spec.ImagePullPolicy)
}

// setServiceTemplateDefaults sets default values for service templates
func setServiceTemplateDefaults(spec *Spec) {
	if spec.ServiceTemplate.Spec.Ports != nil {
		for idx := range spec.ServiceTemplate.Spec.Ports {
			var p *corev1.ServicePort = &spec.ServiceTemplate.Spec.Ports[idx]
			if p.Protocol == "" {
				p.Protocol = corev1.ProtocolTCP
			}

			if p.TargetPort.IntValue() == 0 {
				p.TargetPort.IntVal = p.Port
			}
		}
	}

	if spec.ServiceTemplate.Spec.Type == "" {
		spec.ServiceTemplate.Spec.Type = corev1.ServiceTypeClusterIP
	}
}

// sortAndCompareSlices sorts and compare the slices for equality. Return true if NOT equal. False otherwise
func sortAndCompareSlices(a interface{}, b interface{}, keyName string) bool {
	aType := reflect.TypeOf(a)
	bType := reflect.TypeOf(b)

	if aType.Kind() != reflect.Slice || bType.Kind() != reflect.Slice {
		panic(fmt.Sprintf("SortAndCompareSlices can only be used on slices: Kind(a)=%v, Kind(b)=%v", aType.Kind(), bType.Kind()))
	}

	if aType.Elem() != bType.Elem() {
		panic(fmt.Sprintf("SortAndCompareSlides can only be used on slices on the same type: Elem(a)=%v, Elem(b)=%v", aType.Elem(), bType.Elem()))
	}

	_, found := aType.Elem().FieldByName(keyName)
	if !found {
		panic(fmt.Sprintf("SortAndCompareSlides cannot find the specified key name '%s' to sort on", keyName))
	}

	aValue := reflect.ValueOf(a)
	bValue := reflect.ValueOf(b)
	if aValue.Len() != bValue.Len() {
		return true
	}

	sortFunc := func(s interface{}, i, j int) bool {
		sValue := reflect.ValueOf(s)

		val1 := sValue.Index(i).FieldByName(keyName)
		val2 := sValue.Index(j).FieldByName(keyName)

		switch val1.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return val1.Int() < val2.Int()
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return val1.Uint() < val2.Uint()
		case reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
			return val1.Float() < val2.Float()
		case reflect.String:
			return val1.String() < val2.String()
		default:
			panic(fmt.Sprintf("SortAndCompareSlides can only sort on keyName of int, uint, float or string type: Kind(%s)=%v", keyName, val1.Kind()))
		}
	}

	sort.Slice(a, func(i, j int) bool {
		return sortFunc(a, i, j)
	})

	sort.Slice(b, func(i, j int) bool {
		return sortFunc(b, i, j)
	})

	return !reflect.DeepEqual(a, b)
}
