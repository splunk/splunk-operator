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

package resources

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
)

func TestAsOwner(t *testing.T) {
	cr := enterprisev1.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	got := AsOwner(&cr)

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

	if !*got.Controller {
		t.Errorf("AsOwner().Controller = %t; want %t", *got.Controller, true)
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
				"c": "d",
			},
			Annotations: map[string]string{
				"three": "four",
			},
		},
	}

	AppendParentMeta(child.GetObjectMeta(), parent.GetObjectMeta())

	// check Labels
	want := map[string]string{
		"a": "b",
		"c": "d",
	}
	if !reflect.DeepEqual(child.GetLabels(), want) {
		t.Errorf("AppendParentMeta() child Labels=%v; want %v", child.GetLabels(), want)
	}

	// check Annotations
	want = map[string]string{
		"one":   "two",
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
	test := func(secretBytes string, n int) {
		results := [][]byte{}

		// get 10 results
		for i := 0; i < 10; i++ {
			results = append(results, GenerateSecret(secretBytes, n))

			// ensure its length is correct
			if len(results[i]) != n {
				t.Errorf("GenerateSecret(\"%s\",%d) len = %d; want %d", secretBytes, 10, len(results[i]), n)
			}

			// ensure it only includes allowed bytes
			for _, c := range results[i] {
				if bytes.IndexByte([]byte(secretBytes), c) == -1 {
					t.Errorf("GenerateSecret(\"%s\",%d) returned invalid byte: %c", secretBytes, 10, c)
				}
			}

			// ensure each result is unique
			for x := i; x > 0; x-- {
				if bytes.Compare(results[x-1], results[i]) == 0 {
					t.Errorf("GenerateSecret(\"%s\",%d) returned two identical values: %s", secretBytes, n, string(results[i]))
				}
			}
		}
	}

	test("ABCDEF01234567890", 10)
	test("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", 10)
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
		{ContainerPort: 9000}, {ContainerPort: 8000}, {ContainerPort: 80},
	}
	want = map[string]string{
		"traffic.sidecar.istio.io/excludeOutboundPorts": "8089,8191,9997,7777,9000,17000,17500,19000",
		"traffic.sidecar.istio.io/includeInboundPorts":  "80,8000",
	}
	test()

	ports = []corev1.ContainerPort{
		{ContainerPort: 9000}, {ContainerPort: 8089}, {ContainerPort: 7777}, {ContainerPort: 17500}, {ContainerPort: 8191},
	}
	want = map[string]string{
		"traffic.sidecar.istio.io/excludeOutboundPorts": "8089,8191,9997,7777,9000,17000,17500,19000",
		"traffic.sidecar.istio.io/includeInboundPorts":  "",
	}
	test()
}

func TestGetLabels(t *testing.T) {
	test := func(component, name, identifier string, want map[string]string) {
		got := GetLabels(component, name, identifier)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("GetLabels(\"%s\",\"%s\",\"%s\") = %v; want %v", component, name, identifier, got, want)
		}
	}

	test("indexer", "cluster-master", "t1", map[string]string{
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "indexer",
		"app.kubernetes.io/name":       "cluster-master",
		"app.kubernetes.io/part-of":    "splunk-t1-indexer",
		"app.kubernetes.io/instance":   "splunk-t1-cluster-master",
	})
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

func TestValidateCommonSpec(t *testing.T) {
	spec := enterprisev1.CommonSpec{}
	defaultResources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("0.1"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("4"),
			corev1.ResourceMemory: resource.MustParse("8Gi"),
		},
	}

	test := func(pullPolicy, scheduler string) {
		err := ValidateCommonSpec(&spec, defaultResources)
		if err != nil {
			t.Errorf("ValidateCommonSpec() returned %v; want nil", err)
		}
		if spec.ImagePullPolicy != pullPolicy {
			t.Errorf("ValidateCommonSpec() ImagePullPolicy = %s; want %s", spec.ImagePullPolicy, pullPolicy)
		}
		if spec.SchedulerName != scheduler {
			t.Errorf("ValidateCommonSpec() SchedulerName = %s; want %s", spec.SchedulerName, scheduler)
		}
		if !reflect.DeepEqual(spec.Resources, defaultResources) {
			t.Errorf("ValidateCommonSpec() Resources = %v; want %v", spec.Resources, defaultResources)
		}
	}

	test("IfNotPresent", "default-scheduler")

	spec.ImagePullPolicy = "Always"
	spec.SchedulerName = "blah"
	test("Always", "blah")

	spec.ImagePullPolicy = "IfNotPresent"
	test("IfNotPresent", "blah")

	spec.ImagePullPolicy = "Invalid"
	err := ValidateCommonSpec(&spec, defaultResources)
	if err == nil {
		t.Error("ValidateCommonSpec() returned nil; want ERROR")
	}
}
