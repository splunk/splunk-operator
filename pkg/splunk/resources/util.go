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

const (
	secretBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ01234567890%()*+,-./<=>?@^_|~"
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
func GenerateSecret(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = secretBytes[rand.Int63()%int64(len(secretBytes))]
	}
	return b
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

// CompareEnvs is a generic comparer of two Kubernetes Env variables.
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
