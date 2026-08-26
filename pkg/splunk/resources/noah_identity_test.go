// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package resources_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/splunk/splunk-operator/pkg/splunk/resources"
)

func TestWithNoahPodIdentity_InjectsStableInputs(t *testing.T) {
	statefulSet := makeNoahStatefulSet()
	resources.WithNoahPodIdentity("")(statefulSet)

	env := envByName(statefulSet.Spec.Template.Spec.Containers[0].Env)
	assert.Equal(t, "true", env[resources.NoahEnabledEnvName].Value)
	assert.Equal(t, statefulSet.Spec.ServiceName, env[resources.NoahHeadlessServiceEnvName].Value)
	assert.Equal(t, "cluster.local", env[resources.ClusterDomainEnvName].Value)
	assert.Equal(t, "preserved", env["UNMANAGED"].Value)

	for envName, fieldPath := range map[string]string{
		resources.PodNameEnvName:      "metadata.name",
		resources.PodNamespaceEnvName: "metadata.namespace",
	} {
		fieldRef := env[envName].ValueFrom
		require.NotNil(t, fieldRef)
		require.NotNil(t, fieldRef.FieldRef)
		assert.Equal(t, "v1", fieldRef.FieldRef.APIVersion)
		assert.Equal(t, fieldPath, fieldRef.FieldRef.FieldPath)
	}
}

func TestWithNoahPodIdentity_UsesCustomClusterDomain(t *testing.T) {
	statefulSet := makeNoahStatefulSet()
	resources.WithNoahPodIdentity("corp.example")(statefulSet)

	env := envByName(statefulSet.Spec.Template.Spec.Containers[0].Env)
	assert.Equal(t, "corp.example", env[resources.ClusterDomainEnvName].Value)
}

func TestWithNoahPodIdentity_PreservesAdvertisedHostAcrossPodReplacement(t *testing.T) {
	statefulSet := makeNoahStatefulSet()
	statefulSet.Name = "splunk-main-indexer"
	resources.WithNoahPodIdentity("corp.example")(statefulSet)

	namespace := strings.Repeat("n", 63)
	require.Len(t, namespace, 63)
	identities := make([]string, 0, 2)

	for _, ordinal := range []int{0, 2} {
		podName := fmt.Sprintf("%s-%d", statefulSet.Name, ordinal)
		original := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
				UID:       types.UID(fmt.Sprintf("original-%d", ordinal)),
			},
			Status: corev1.PodStatus{PodIP: fmt.Sprintf("10.0.0.%d", ordinal+1)},
		}
		replacement := original.DeepCopy()
		replacement.UID = types.UID(fmt.Sprintf("replacement-%d", ordinal))
		replacement.Status.PodIP = fmt.Sprintf("10.0.1.%d", ordinal+1)

		require.NotEqual(t, original.UID, replacement.UID)
		require.NotEqual(t, original.Status.PodIP, replacement.Status.PodIP)

		identity := noahAdvertisedHost(t, statefulSet, original)
		assert.Equal(t, identity, noahAdvertisedHost(t, statefulSet, replacement))
		assert.Equal(t, fmt.Sprintf("%s.%s.%s.svc.corp.example", podName, statefulSet.Spec.ServiceName, namespace), identity)
		identities = append(identities, identity)
	}

	assert.NotEqual(t, identities[0], identities[1], "different ordinals must have different identities")
}

func TestWithNoahPodIdentity_LeavesSidecarsUnchanged(t *testing.T) {
	statefulSet := makeNoahStatefulSet()
	want := statefulSet.Spec.Template.Spec.Containers[1].DeepCopy()
	resources.WithNoahPodIdentity("")(statefulSet)

	assert.Equal(t, want, &statefulSet.Spec.Template.Spec.Containers[1])
}

func TestWithNoahPodIdentity_IsIdempotentAndReplacesManagedValues(t *testing.T) {
	statefulSet := makeNoahStatefulSet()
	splunk := &statefulSet.Spec.Template.Spec.Containers[0]
	splunk.Env = append(
		splunk.Env,
		corev1.EnvVar{Name: resources.NoahEnabledEnvName, Value: "false"},
		corev1.EnvVar{Name: resources.NoahEnabledEnvName, Value: "duplicate"},
		corev1.EnvVar{Name: resources.NoahHeadlessServiceEnvName, Value: "stale-service"},
	)

	option := resources.WithNoahPodIdentity("")
	option(statefulSet)
	want := statefulSet.DeepCopy()
	option(statefulSet)

	assert.Equal(t, want, statefulSet)
	counts := make(map[string]int)
	for _, env := range statefulSet.Spec.Template.Spec.Containers[0].Env {
		counts[env.Name]++
	}
	for _, name := range []string{
		resources.NoahEnabledEnvName,
		resources.NoahHeadlessServiceEnvName,
		resources.ClusterDomainEnvName,
		resources.PodNameEnvName,
		resources.PodNamespaceEnvName,
	} {
		assert.Equal(t, 1, counts[name], name)
	}

	env := envByName(statefulSet.Spec.Template.Spec.Containers[0].Env)
	assert.Equal(t, "true", env[resources.NoahEnabledEnvName].Value)
	assert.Equal(t, statefulSet.Spec.ServiceName, env[resources.NoahHeadlessServiceEnvName].Value)
}

func TestWithNoahPodIdentity_NoSplunkContainerIsNoop(t *testing.T) {
	statefulSet := makeNoahStatefulSet()
	statefulSet.Spec.Template.Spec.Containers = statefulSet.Spec.Template.Spec.Containers[1:]
	want := statefulSet.DeepCopy()

	resources.WithNoahPodIdentity("")(statefulSet)

	assert.Equal(t, want, statefulSet)
}

func envByName(env []corev1.EnvVar) map[string]corev1.EnvVar {
	result := make(map[string]corev1.EnvVar, len(env))
	for _, item := range env {
		result[item.Name] = item
	}
	return result
}

func makeNoahStatefulSet() *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			ServiceName: "splunk-main-indexer-headless",
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "splunk", Env: []corev1.EnvVar{{Name: "UNMANAGED", Value: "preserved"}}},
						{Name: "sidecar", Env: []corev1.EnvVar{{Name: "SIDECAR", Value: "unchanged"}}},
					},
				},
			},
		},
	}
}

func noahAdvertisedHost(t *testing.T, statefulSet *appsv1.StatefulSet, pod *corev1.Pod) string {
	t.Helper()
	env := envByName(statefulSet.Spec.Template.Spec.Containers[0].Env)
	resolve := func(name string) string {
		value, ok := env[name]
		require.True(t, ok, "missing environment variable %s", name)
		if value.ValueFrom == nil {
			return value.Value
		}
		require.NotNil(t, value.ValueFrom.FieldRef, "environment variable %s must use fieldRef", name)
		switch value.ValueFrom.FieldRef.FieldPath {
		case "metadata.name":
			return pod.Name
		case "metadata.namespace":
			return pod.Namespace
		default:
			t.Fatalf("unsupported fieldRef %q for environment variable %s", value.ValueFrom.FieldRef.FieldPath, name)
			return ""
		}
	}

	return fmt.Sprintf("%s.%s.%s.svc.%s",
		resolve(resources.PodNameEnvName),
		resolve(resources.NoahHeadlessServiceEnvName),
		resolve(resources.PodNamespaceEnvName),
		resolve(resources.ClusterDomainEnvName),
	)
}
