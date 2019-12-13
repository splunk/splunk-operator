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

package resources

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha1"
)

func init() {
	// seed random number generator for splunk secret generation
	rand.Seed(time.Now().UnixNano())
}

// AsOwner returns an object to use for Kubernetes resource ownership references.
func AsOwner(cr *v1alpha1.SplunkEnterprise) metav1.OwnerReference {
	trueVar := true

	return metav1.OwnerReference{
		APIVersion: cr.TypeMeta.APIVersion,
		Kind:       cr.TypeMeta.Kind,
		Name:       cr.Name,
		UID:        cr.UID,
		Controller: &trueVar,
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

// SortPorts returns a sorted list of Kubernetes ContainerPorts.
func SortPorts(ports []corev1.ContainerPort) []corev1.ContainerPort {
	sorted := make([]corev1.ContainerPort, len(ports))
	copy(sorted, ports)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ContainerPort < sorted[j].ContainerPort })
	return sorted
}

// ComparePorts is a generic comparer of two Kubernetes ContainerPorts.
// It returns true if there are material differences between them, or false otherwise.
// TODO: could use refactoring; lots of boilerplate copy-pasta here
func ComparePorts(a []corev1.ContainerPort, b []corev1.ContainerPort) bool {
	// first, check for short-circuit opportunity
	if len(a) != len(b) {
		return true
	}

	// make sorted copies of a and b
	aSorted := SortPorts(a)
	bSorted := SortPorts(b)

	// iterate elements, checking for differences
	for n := range aSorted {
		if aSorted[n] != bSorted[n] {
			return true
		}
	}

	return false
}

// SortEnvs returns a sorted list of Kubernetes EnvVar.
func SortEnvs(envvars []corev1.EnvVar) []corev1.EnvVar {
	sorted := make([]corev1.EnvVar, len(envvars))
	copy(sorted, envvars)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	return sorted
}

// CompareEnvs is a generic comparer of two Kubernetes Env variables.
// It returns true if there are material differences between them, or false otherwise.
func CompareEnvs(a []corev1.EnvVar, b []corev1.EnvVar) bool {
	// first, check for short-circuit opportunity
	if len(a) != len(b) {
		return true
	}

	// make sorted copies of a and b
	aSorted := SortEnvs(a)
	bSorted := SortEnvs(b)

	// iterate elements, checking for differences
	for n := range aSorted {
		if aSorted[n] != bSorted[n] {
			return true
		}
	}

	return false
}

// SortVolumeMounts returns a sorted list of Kubernetes VolumeMounts.
func SortVolumeMounts(mounts []corev1.VolumeMount) []corev1.VolumeMount {
	sorted := make([]corev1.VolumeMount, len(mounts))
	copy(sorted, mounts)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	return sorted
}

// CompareVolumeMounts is a generic comparer of two Kubernetes VolumeMounts.
// It returns true if there are material differences between them, or false otherwise.
func CompareVolumeMounts(a []corev1.VolumeMount, b []corev1.VolumeMount) bool {
	// first, check for short-circuit opportunity
	if len(a) != len(b) {
		return true
	}

	// make sorted copies of a and b
	aSorted := SortVolumeMounts(a)
	bSorted := SortVolumeMounts(b)

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
	sortedPorts := SortPorts(ports)
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
func GetLabels(identifier string, typeLabel string, isSelector bool) map[string]string {
	result := map[string]string{
		"app":  "splunk",
		"for":  identifier,
		"type": typeLabel,
	}

	if isSelector {
		return result
	}

	// see https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels
	result["app.kubernetes.io/name"] = fmt.Sprintf("splunk-%s", identifier)
	result["app.kubernetes.io/part-of"] = "splunk"
	result["app.kubernetes.io/managed-by"] = "splunk-operator"
	result["app.kubernetes.io/instance"] = fmt.Sprintf("splunk-%s-%s", identifier, typeLabel)

	return result
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

	affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution = append(
		affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution,
		corev1.WeightedPodAffinityTerm{
			Weight: 100,
			PodAffinityTerm: corev1.PodAffinityTerm{
				LabelSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "app.kubernetes.io/instance",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{fmt.Sprintf("splunk-%s-%s", identifier, typeLabel)},
						},
					},
				},
				TopologyKey: "kubernetes.io/hostname",
			},
		},
	)

	return affinity
}
