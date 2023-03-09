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

package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAsOwner(t *testing.T) {
	cr := TestResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	boolValues := [2]bool{true, false}

	for b := range boolValues {
		got := AsOwner(&cr, boolValues[b])

		if got.APIVersion != cr.TypeMeta.APIVersion {
			t.Errorf("AsOwner().APIVersion = %s; want %s", got.APIVersion, cr.TypeMeta.APIVersion)
		}

		if got.Kind != cr.TypeMeta.Kind {
			t.Errorf("AsOwner().Kind = %s; want %s", got.Kind, cr.TypeMeta.Kind)
		}

		if got.Name != cr.Name {
			t.Errorf("AsOwner().Name = %s; want %s", got.Name, cr.Name)
		}

		if got.UID != cr.UID {
			t.Errorf("AsOwner().UID = %s; want %s", got.UID, cr.UID)
		}

		if *got.Controller != boolValues[b] {
			t.Errorf("AsOwner().Controller = %t; want %t", *got.Controller, true)
		}
	}
}

func TestAppendParentMeta(t *testing.T) {
	parent := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"a": "b",
			},
			Annotations: map[string]string{
				"one": "two",
				"kubectl.kubernetes.io/last-applied-configuration": "foobar",
			},
		},
	}
	child := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"a": "z",
				"c": "d",
			},
			Annotations: map[string]string{
				"three": "four",
				"one":   "ten",
			},
		},
	}

	AppendParentMeta(child.GetObjectMeta(), parent.GetObjectMeta())

	// check Labels
	want := map[string]string{
		"a": "z",
		"c": "d",
	}
	if !reflect.DeepEqual(child.GetLabels(), want) {
		t.Errorf("AppendParentMeta() child Labels=%v; want %v", child.GetLabels(), want)
	}

	// check Annotations
	want = map[string]string{
		"one":   "ten",
		"three": "four",
	}
	if !reflect.DeepEqual(child.GetAnnotations(), want) {
		t.Errorf("AppendParentMeta() child Annotations=%v; want %v", child.GetAnnotations(), want)
	}
}

func TestParseResourceQuantity(t *testing.T) {
	resourceQuantityTester := func(t *testing.T, str string, defaultStr string, want int64) {
		q, err := ParseResourceQuantity(str, defaultStr)
		if err != nil {
			t.Errorf("ParseResourceQuantity(\"%s\",\"%s\") error: %v", str, defaultStr, err)
		}

		got, success := q.AsInt64()
		if !success {
			t.Errorf("ParseResourceQuantity(\"%s\",\"%s\") returned false", str, defaultStr)
		}
		if got != want {
			t.Errorf("ParseResourceQuantity(\"%s\",\"%s\") = %d; want %d", str, defaultStr, got, want)
		}
	}

	resourceQuantityTester(t, "1Gi", "", 1073741824)
	resourceQuantityTester(t, "4", "", 4)
	resourceQuantityTester(t, "", "1Gi", 1073741824)

	_, err := ParseResourceQuantity("13rf1", "")
	if err == nil {
		t.Errorf("ParseResourceQuantity(\"13rf1\",\"\") returned nil; want error")
	}
}

func TestGetServiceFQDN(t *testing.T) {
	test := func(namespace string, name string, want string) {
		got := GetServiceFQDN(namespace, name)
		if got != want {
			t.Errorf("GetServiceFQDN() = %s; want %s", got, want)
		}
	}

	test("test", "t1", "t1.test.svc.cluster.local")

	os.Setenv("CLUSTER_DOMAIN", "example.com")
	test("test", "t2", "t2.test.svc.example.com")
}

func TestGenerateSecret(t *testing.T) {
	test := func(SecretBytes string, n int) {
		results := [][]byte{}

		// get 10 results
		for i := 0; i < 10; i++ {
			results = append(results, GenerateSecret(SecretBytes, n))

			// ensure its length is correct
			if len(results[i]) != n {
				t.Errorf("GenerateSecret(\"%s\",%d) len = %d; want %d", SecretBytes, 10, len(results[i]), n)
			}

			// ensure it only includes allowed bytes
			for _, c := range results[i] {
				if bytes.IndexByte([]byte(SecretBytes), c) == -1 {
					t.Errorf("GenerateSecret(\"%s\",%d) returned invalid byte: %c", SecretBytes, 10, c)
				}
			}

			// ensure each result is unique
			for x := i; x > 0; x-- {
				if bytes.Equal(results[x-1], results[i]) {
					t.Errorf("GenerateSecret(\"%s\",%d) returned two identical values: %s", SecretBytes, n, string(results[i]))
				}
			}
		}
	}

	test("ABCDEF01234567890", 10)
	test("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", 10)
}

func TestSortContainerPorts(t *testing.T) {
	var ports []corev1.ContainerPort
	var want []corev1.ContainerPort

	test := func() {
		got := SortContainerPorts(ports)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("SortContainerPorts() got %v; want %v", got, want)
		}
	}

	ports = []corev1.ContainerPort{
		{ContainerPort: 100},
		{ContainerPort: 200},
		{ContainerPort: 3000},
	}
	want = ports
	test()

	ports = []corev1.ContainerPort{
		{ContainerPort: 3000},
		{ContainerPort: 100},
		{ContainerPort: 200},
	}
	want = []corev1.ContainerPort{
		{ContainerPort: 100},
		{ContainerPort: 200},
		{ContainerPort: 3000},
	}
	test()
}

func TestSortServicePorts(t *testing.T) {
	var ports []corev1.ServicePort
	var want []corev1.ServicePort

	test := func() {
		got := SortServicePorts(ports)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("SortServicePorts() got %v; want %v", got, want)
		}
	}

	ports = []corev1.ServicePort{
		{Port: 100},
		{Port: 200},
		{Port: 3000},
	}
	want = ports
	test()

	ports = []corev1.ServicePort{
		{Port: 3000},
		{Port: 100},
		{Port: 200},
	}
	want = []corev1.ServicePort{
		{Port: 100},
		{Port: 200},
		{Port: 3000},
	}
	test()
}

func compareTester(t *testing.T, method string, f func() bool, a interface{}, b interface{}, want bool) {
	got := f()
	if got != want {
		aBytes, errA := json.Marshal(a)
		bBytes, errB := json.Marshal(b)
		cmp := "=="
		if want {
			cmp = "!="
		}
		if errA == nil && errB == nil {
			t.Errorf("%s() failed: %s %s %s", method, string(aBytes), cmp, string(bBytes))
		} else {
			t.Errorf("%s() failed: %v %s %v", method, a, cmp, b)
		}
	}
}

func TestCompareContainerPorts(t *testing.T) {
	var a []corev1.ContainerPort
	var b []corev1.ContainerPort

	test := func(want bool) {
		f := func() bool {
			return CompareContainerPorts(a, b)
		}
		compareTester(t, "CompareContainerPorts", f, a, b, want)
	}

	test(false)

	var nullPort corev1.ContainerPort
	httpPort := corev1.ContainerPort{
		Name:          "http",
		ContainerPort: 80,
		Protocol:      "TCP",
	}
	splunkWebPort := corev1.ContainerPort{
		Name:          "splunk",
		ContainerPort: 8000,
		Protocol:      "TCP",
	}
	s2sPort := corev1.ContainerPort{
		Name:          "s2s",
		ContainerPort: 9997,
		Protocol:      "TCP",
	}

	a = []corev1.ContainerPort{nullPort, nullPort}
	b = []corev1.ContainerPort{nullPort, nullPort}
	test(false)

	a = []corev1.ContainerPort{nullPort, nullPort}
	b = []corev1.ContainerPort{nullPort, nullPort, nullPort}
	test(true)

	a = []corev1.ContainerPort{httpPort, splunkWebPort}
	b = []corev1.ContainerPort{httpPort, splunkWebPort}
	test(false)

	a = []corev1.ContainerPort{httpPort, s2sPort, splunkWebPort}
	b = []corev1.ContainerPort{s2sPort, splunkWebPort, httpPort}
	test(false)

	a = []corev1.ContainerPort{httpPort, s2sPort}
	b = []corev1.ContainerPort{s2sPort, splunkWebPort}
	test(true)

	a = []corev1.ContainerPort{s2sPort}
	b = []corev1.ContainerPort{s2sPort, splunkWebPort}
	test(true)
}

func TestCompareServicePorts(t *testing.T) {
	var a []corev1.ServicePort
	var b []corev1.ServicePort

	test := func(want bool) {
		f := func() bool {
			return CompareServicePorts(a, b)
		}
		compareTester(t, "CompareServicePorts", f, a, b, want)
	}

	test(false)

	var nullPort corev1.ServicePort
	httpPort := corev1.ServicePort{
		Name:     "http",
		Port:     80,
		Protocol: "TCP",
	}
	splunkWebPort := corev1.ServicePort{
		Name:     "splunk",
		Port:     8000,
		Protocol: "TCP",
	}
	s2sPort := corev1.ServicePort{
		Name:     "s2s",
		Port:     9997,
		Protocol: "TCP",
	}

	a = []corev1.ServicePort{nullPort, nullPort}
	b = []corev1.ServicePort{nullPort, nullPort}
	test(false)

	a = []corev1.ServicePort{nullPort, nullPort}
	b = []corev1.ServicePort{nullPort, nullPort, nullPort}
	test(true)

	a = []corev1.ServicePort{httpPort, splunkWebPort}
	b = []corev1.ServicePort{httpPort, splunkWebPort}
	test(false)

	a = []corev1.ServicePort{httpPort, s2sPort, splunkWebPort}
	b = []corev1.ServicePort{s2sPort, splunkWebPort, httpPort}
	test(false)

	a = []corev1.ServicePort{httpPort, s2sPort}
	b = []corev1.ServicePort{s2sPort, splunkWebPort}
	test(true)

	a = []corev1.ServicePort{s2sPort}
	b = []corev1.ServicePort{s2sPort, splunkWebPort}
	test(true)
}

func TestCompareEnvs(t *testing.T) {
	var a []corev1.EnvVar
	var b []corev1.EnvVar

	test := func(want bool) {
		f := func() bool {
			return CompareEnvs(a, b)
		}
		compareTester(t, "CompareEnvs", f, a, b, want)
	}

	test(false)

	aEnv := corev1.EnvVar{
		Name:  "A",
		Value: "a",
	}
	bEnv := corev1.EnvVar{
		Name:  "B",
		Value: "b",
	}
	cEnv := corev1.EnvVar{
		Name:  "C",
		Value: "c",
	}

	a = []corev1.EnvVar{aEnv, bEnv}
	b = []corev1.EnvVar{aEnv, bEnv}
	test(false)

	a = []corev1.EnvVar{aEnv, cEnv, bEnv}
	b = []corev1.EnvVar{cEnv, bEnv, aEnv}
	test(false)

	a = []corev1.EnvVar{aEnv, cEnv}
	b = []corev1.EnvVar{cEnv, bEnv}
	test(true)

	a = []corev1.EnvVar{aEnv, cEnv}
	b = []corev1.EnvVar{cEnv}
	test(true)
}

func TestCompareVolumeMounts(t *testing.T) {
	var a []corev1.VolumeMount
	var b []corev1.VolumeMount

	test := func(want bool) {
		f := func() bool {
			return CompareVolumeMounts(a, b)
		}
		compareTester(t, "CompareVolumeMounts", f, a, b, want)
	}

	test(false)

	var nullVolume corev1.VolumeMount
	varVolume := corev1.VolumeMount{
		Name:      "mnt-var",
		MountPath: "/opt/splunk/var",
	}
	etcVolume := corev1.VolumeMount{
		Name:      "mnt-etc",
		MountPath: "/opt/splunk/etc",
	}
	secretVolume := corev1.VolumeMount{
		Name:      "mnt-secrets",
		MountPath: "/mnt/secrets",
	}

	a = []corev1.VolumeMount{nullVolume, nullVolume}
	b = []corev1.VolumeMount{nullVolume, nullVolume}
	test(false)

	a = []corev1.VolumeMount{nullVolume, nullVolume}
	b = []corev1.VolumeMount{nullVolume, nullVolume, nullVolume}
	test(true)

	a = []corev1.VolumeMount{varVolume, etcVolume}
	b = []corev1.VolumeMount{varVolume, etcVolume}
	test(false)

	a = []corev1.VolumeMount{varVolume, secretVolume, etcVolume}
	b = []corev1.VolumeMount{secretVolume, etcVolume, varVolume}
	test(false)

	a = []corev1.VolumeMount{varVolume, secretVolume}
	b = []corev1.VolumeMount{secretVolume, etcVolume}
	test(true)
}

func TestCompareByMarshall(t *testing.T) {
	var a corev1.ResourceRequirements
	var b corev1.ResourceRequirements

	test := func(want bool) {
		f := func() bool {
			return CompareByMarshall(a, b)
		}
		compareTester(t, "CompareByMarshall", f, a, b, want)
	}

	test(false)

	low := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("0.1"),
		corev1.ResourceMemory: resource.MustParse("512Mi"),
	}
	medium := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("8"),
		corev1.ResourceMemory: resource.MustParse("4Gi"),
	}
	high := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("32"),
		corev1.ResourceMemory: resource.MustParse("32Gi"),
	}

	a = corev1.ResourceRequirements{Requests: low, Limits: high}
	b = corev1.ResourceRequirements{Requests: low, Limits: high}
	test(false)

	a = corev1.ResourceRequirements{Requests: medium, Limits: high}
	b = corev1.ResourceRequirements{Requests: low, Limits: high}
	test(true)
}

func TestCompareSortedStrings(t *testing.T) {
	var a []string
	var b []string

	test := func(want bool) {
		f := func() bool {
			return CompareSortedStrings(a, b)
		}
		compareTester(t, "CompareSortedStrings", f, a, b, want)
	}

	test(false)

	ip1 := "192.168.2.1"
	ip2 := "192.168.2.100"
	ip3 := "192.168.10.1"

	a = []string{ip1, ip2}
	b = []string{ip1, ip2}
	test(false)

	a = []string{ip1, ip3, ip2}
	b = []string{ip3, ip2, ip1}
	test(false)

	a = []string{ip1, ip3}
	b = []string{ip3, ip2}
	test(true)

	a = []string{ip1, ip3}
	b = []string{ip3}
	test(true)
}

func TestGetIstioAnnotations(t *testing.T) {
	var ports []corev1.ContainerPort
	var want map[string]string

	test := func() {
		got := GetIstioAnnotations(ports)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("GetIstioAnnotations() = %v; want %v", got, want)
		}
	}

	ports = []corev1.ContainerPort{
		{ContainerPort: 8000}, {ContainerPort: 80},
	}
	want = map[string]string{
		"traffic.sidecar.istio.io/excludeOutboundPorts": "8089,8191,9997",
		"traffic.sidecar.istio.io/includeInboundPorts":  "80,8000",
	}
	test()

	ports = []corev1.ContainerPort{
		{ContainerPort: 8089}, {ContainerPort: 8191},
	}
	want = map[string]string{
		"traffic.sidecar.istio.io/excludeOutboundPorts": "8089,8191,9997",
		"traffic.sidecar.istio.io/includeInboundPorts":  "",
	}
	test()
}

func TestGetLabels(t *testing.T) {
	test := func(component, name, instanceIdentifier string, partOfIdentifier string, want map[string]string) {
		got, _ := GetLabels(component, name, instanceIdentifier, partOfIdentifier, make([]string, 0))
		if !reflect.DeepEqual(got, want) {
			t.Errorf("GetLabels(\"%s\",\"%s\",\"%s\",\"%s\") = %v; want %v", component, name, instanceIdentifier, partOfIdentifier, got, want)
		}
	}

	test("indexer", "cluster-manager", "t1", "t1", map[string]string{
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "indexer",
		"app.kubernetes.io/name":       "cluster-manager",
		"app.kubernetes.io/part-of":    "splunk-t1-indexer",
		"app.kubernetes.io/instance":   "splunk-t1-cluster-manager",
	})

	// Multipart IndexerCluster - selector of indexer service for main part
	test("indexer", "indexer", "", "cluster1", map[string]string{
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "indexer",
		"app.kubernetes.io/name":       "indexer",
		"app.kubernetes.io/part-of":    "splunk-cluster1-indexer",
	})

	// Multipart IndexerCluster - labels of child IndexerCluster part
	test("indexer", "indexer", "site1", "cluster1", map[string]string{
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "indexer",
		"app.kubernetes.io/name":       "indexer",
		"app.kubernetes.io/part-of":    "splunk-cluster1-indexer",
		"app.kubernetes.io/instance":   "splunk-site1-indexer",
	})

	testNew := func(component, name, instanceIdentifier string, partOfIdentifier string, selectFew []string, want map[string]string, expectedErr string) {
		got, err := GetLabels(component, name, instanceIdentifier, partOfIdentifier, selectFew)
		if err != nil && expectedErr != err.Error() {
			t.Errorf("GetLabels(\"%s\",\"%s\",\"%s\",\"%s\") expected Error %s, got error %s", component, name, instanceIdentifier, partOfIdentifier, expectedErr, err.Error())
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("GetLabels(\"%s\",\"%s\",\"%s\",\"%s\") = %v; want %v", component, name, instanceIdentifier, partOfIdentifier, got, want)
		}
	}

	// Test all labels using selectFew option
	selectAll := []string{"manager", "component", "name", "partof", "instance"}
	testNew("indexer", "cluster-manager", "t1", "t1", selectAll, map[string]string{
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "indexer",
		"app.kubernetes.io/name":       "cluster-manager",
		"app.kubernetes.io/part-of":    "splunk-t1-indexer",
		"app.kubernetes.io/instance":   "splunk-t1-cluster-manager",
	}, "")

	// Test a few labels using selectFew option
	selectFewPartial := []string{"manager", "component"}
	testNew("indexer", "cluster-manager", "t1", "t1", selectFewPartial, map[string]string{
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "indexer",
	}, "")

	// Test incorrect label
	selectFewIncorrect := []string{"randomvalue"}
	testNew("indexer", "cluster-manager", "t1", "t1", selectFewIncorrect, map[string]string{}, "Incorrect label type randomvalue")
}

func TestAppendPodAffinity(t *testing.T) {
	var affinity corev1.Affinity
	identifier := "test1"
	typeLabel := "indexer"

	test := func(want corev1.Affinity) {
		got := AppendPodAntiAffinity(&affinity, identifier, typeLabel)
		f := func() bool {
			return CompareByMarshall(got, want)
		}
		compareTester(t, "AppendPodAntiAffinity()", f, got, want, false)
	}

	wantAppended := corev1.WeightedPodAffinityTerm{
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
	}

	test(corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
				wantAppended,
			},
		},
	})

	affinity = corev1.Affinity{
		PodAffinity: &corev1.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{Namespaces: []string{"test"}},
			},
		},
		PodAntiAffinity: &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
				wantAppended,
			},
		},
	}
	test(corev1.Affinity{
		PodAffinity: &corev1.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{Namespaces: []string{"test"}},
			},
		},
		PodAntiAffinity: &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
				wantAppended, wantAppended,
			},
		},
	})
}

func TestCompareTolerations(t *testing.T) {
	var a []corev1.Toleration
	var b []corev1.Toleration

	test := func(want bool) {
		f := func() bool {
			return CompareTolerations(a, b)
		}
		compareTester(t, "CompareTolerations", f, a, b, want)
	}

	// No change
	test(false)
	var nullToleration corev1.Toleration

	toleration1 := corev1.Toleration{
		Key:      "key1",
		Operator: corev1.TolerationOpEqual,
		Value:    "value1",
		Effect:   corev1.TaintEffectNoSchedule,
	}
	toleration2 := corev1.Toleration{
		Key:      "key1",
		Operator: corev1.TolerationOpEqual,
		Value:    "value1",
		Effect:   corev1.TaintEffectNoSchedule,
	}

	// No change
	a = []corev1.Toleration{nullToleration, nullToleration}
	b = []corev1.Toleration{nullToleration, nullToleration}
	test(false)

	// No change
	a = []corev1.Toleration{toleration1}
	b = []corev1.Toleration{toleration2}
	test(false)

	// Change effect
	var toleration corev1.Toleration

	toleration = toleration2
	toleration.Effect = corev1.TaintEffectNoExecute
	a = []corev1.Toleration{toleration}
	b = []corev1.Toleration{toleration1}
	test(true)

	// Change operator
	toleration = toleration2
	toleration.Operator = corev1.TolerationOpExists
	a = []corev1.Toleration{toleration}
	b = []corev1.Toleration{toleration1}
	test(true)

	// Change value
	toleration = toleration2
	toleration.Value = "newValue"
	a = []corev1.Toleration{toleration}
	b = []corev1.Toleration{toleration1}
	test(true)

}

func TestCompareTopologySpreadConstraints(t *testing.T) {
	var a []corev1.TopologySpreadConstraint
	var b []corev1.TopologySpreadConstraint

	test := func(want bool) {
		f := func() bool {
			return CompareTopologySpreadConstraints(a, b)
		}
		compareTester(t, "CompareTopologySpreadConstraints", f, a, b, want)
	}

	// No change
	test(false)

	var nullTopologySpreadConstraint corev1.TopologySpreadConstraint
	topologySpreadConstraint1 := corev1.TopologySpreadConstraint{
		MaxSkew:           1,
		TopologyKey:       "key1",
		WhenUnsatisfiable: "key2",
		//LabelSelector: <object>
		//MatchLabelKeys: <list> # optional; alpha since v1.25
		//NodeAffinityPolicy: [Honor|Ignore] # optional; beta since v1.26
		//NodeTaintsPolicy:
	}
	topologySpreadConstraint2 := corev1.TopologySpreadConstraint{
		MaxSkew:           1,
		TopologyKey:       "key1",
		WhenUnsatisfiable: "key2",
		//LabelSelector: <object>
		//MatchLabelKeys: <list> # optional; alpha since v1.25
		//NodeAffinityPolicy: [Honor|Ignore] # optional; beta since v1.26
		//NodeTaintsPolicy:
	}

	// No change
	a = []corev1.TopologySpreadConstraint{nullTopologySpreadConstraint, nullTopologySpreadConstraint}
	b = []corev1.TopologySpreadConstraint{nullTopologySpreadConstraint, nullTopologySpreadConstraint}
	test(false)

	// No change
	a = []corev1.TopologySpreadConstraint{topologySpreadConstraint1}
	b = []corev1.TopologySpreadConstraint{topologySpreadConstraint2}
	test(false)

	// Change maxSkew
	var topologySpreadConstraint corev1.TopologySpreadConstraint

	topologySpreadConstraint = topologySpreadConstraint2
	topologySpreadConstraint.MaxSkew = 1
	a = []corev1.TopologySpreadConstraint{topologySpreadConstraint}
	b = []corev1.TopologySpreadConstraint{topologySpreadConstraint1}
	test(false)

	// Change topologyKey
	topologySpreadConstraint = topologySpreadConstraint2
	topologySpreadConstraint.TopologyKey = ""
	a = []corev1.TopologySpreadConstraint{topologySpreadConstraint}
	b = []corev1.TopologySpreadConstraint{topologySpreadConstraint1}
	test(true)

	// Change labelSelector
	topologySpreadConstraint = topologySpreadConstraint2
	topologySpreadConstraint.LabelSelector = &metav1.LabelSelector{}
	a = []corev1.TopologySpreadConstraint{topologySpreadConstraint}
	b = []corev1.TopologySpreadConstraint{topologySpreadConstraint1}
	test(true)

	// Change matchLabelKeys
	topologySpreadConstraint = topologySpreadConstraint2
	topologySpreadConstraint.MatchLabelKeys = []string{"newValue"}
	a = []corev1.TopologySpreadConstraint{topologySpreadConstraint}
	b = []corev1.TopologySpreadConstraint{topologySpreadConstraint1}
	test(true)

	/*
		// Change nodeAffinityPolicy
		topologySpreadConstraint = topologySpreadConstraint2
		topologySpreadConstraint.NodeAffinityPolicy = &corev1.NodeInclusionPolicy{}
		a = []corev1.TopologySpreadConstraint{topologySpreadConstraint}
		b = []corev1.TopologySpreadConstraint{topologySpreadConstraint1}
		test(true)

		// Change nodeTaintsPolicy
		topologySpreadConstraint = topologySpreadConstraint2
		topologySpreadConstraint.NodeTaintsPolicy = &corev1.NodeInclusionPolicy{}
		a = []corev1.TopologySpreadConstraint{topologySpreadConstraint}
		b = []corev1.TopologySpreadConstraint{topologySpreadConstraint1}
		test(true)
	*/
}

func TestCompareVolumes(t *testing.T) {
	var a []corev1.Volume
	var b []corev1.Volume

	test := func(want bool) {
		f := func() bool {
			return CompareVolumes(a, b)
		}
		compareTester(t, "CompareVolumes", f, a, b, want)
	}

	// No change
	test(false)

	var nullVolume corev1.Volume

	defaultMode := int32(440)
	secret1Volume := corev1.Volume{Name: "test-volume", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "secret1"}}}
	secret2Volume := corev1.Volume{Name: "test-volume", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "secret2"}}}
	secret3Volume := corev1.Volume{Name: "test-volume", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "secret2", DefaultMode: &defaultMode}}}
	secret4Volume := corev1.Volume{Name: "test-volume", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "secret2", DefaultMode: &defaultMode}}}

	// No change
	a = []corev1.Volume{nullVolume, nullVolume}
	b = []corev1.Volume{nullVolume, nullVolume}
	test(false)

	// Change - new volume
	a = []corev1.Volume{nullVolume, nullVolume}
	b = []corev1.Volume{nullVolume, nullVolume, nullVolume}
	test(true)

	// No change
	a = []corev1.Volume{secret1Volume}
	b = []corev1.Volume{secret1Volume}
	test(false)

	// Change - new volume
	a = []corev1.Volume{secret1Volume}
	b = []corev1.Volume{secret1Volume, secret2Volume}
	test(true)

	// Change - new default mode
	a = []corev1.Volume{secret2Volume}
	b = []corev1.Volume{secret3Volume}
	test(true)

	// No change
	a = []corev1.Volume{secret3Volume}
	b = []corev1.Volume{secret4Volume}
	test(false)
}

func TestSortAndCompareSlices(t *testing.T) {

	// Test panic cases
	var done sync.WaitGroup

	var deferFunc = func() {
		if r := recover(); r == nil {
			t.Errorf("Expect code panic when comparing slices")
		}
		done.Done()
	}

	done.Add(1)
	go func() {
		var a corev1.ServicePort
		var b corev1.ContainerPort
		defer deferFunc()
		sortAndCompareSlices(a, b, "Name")
	}()

	done.Add(1)
	go func() {
		var a []corev1.ServicePort
		var b []corev1.ContainerPort

		defer deferFunc()
		sortAndCompareSlices(a, b, "Name")
	}()
	done.Add(1)
	go func() {
		defer deferFunc()
		a := []corev1.ServicePort{{Name: "http", Port: 80}}
		b := []corev1.ServicePort{{Name: "http", Port: 80}}
		sortAndCompareSlices(a, b, "SortFieldNameDoesNotExist")
	}()
	done.Wait()

	// Test inequality
	var a []corev1.ServicePort
	var b []corev1.ServicePort
	a = []corev1.ServicePort{{Name: "http", Port: 80}, {Name: "https", Port: 443}}
	b = []corev1.ServicePort{{Name: "http", Port: 81}, {Name: "https", Port: 443}}
	if !sortAndCompareSlices(a, b, "Name") {
		t.Errorf("Expect 2 slices to be not equal - (%v, %v)", a, b)
	}

	a = []corev1.ServicePort{{Name: "http", Port: 80}, {Name: "https", Port: 443}}
	b = []corev1.ServicePort{{Name: "http", Port: 80}, {Name: "https", Port: 443}, {Name: "ssh", Port: 22}}
	if !sortAndCompareSlices(a, b, "Name") {
		t.Errorf("Expect 2 slices to be not equal - (%v, %v)", a, b)
	}

	// Test equality
	a = []corev1.ServicePort{{Name: "http", Port: 80}, {Name: "https", Port: 443}}
	b = []corev1.ServicePort{{Name: "http", Port: 80}, {Name: "https", Port: 443}}
	if sortAndCompareSlices(a, b, "Name") {
		t.Errorf("Expect 2 slices to be equal - (%v, %v)", a, b)
	}

	a = []corev1.ServicePort{{Name: "ssh", Port: 22}, {Name: "https", Port: 443}, {Name: "http", Port: 80}}
	b = []corev1.ServicePort{{Name: "http", Port: 80}, {Name: "https", Port: 443}, {Name: "ssh", Port: 22}}
	if sortAndCompareSlices(a, b, "Name") {
		t.Errorf("Expect 2 slices to be equal - (%v, %v)", a, b)
	}
}
