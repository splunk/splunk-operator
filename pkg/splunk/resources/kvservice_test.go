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

package resources

import (
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestGetStandardK8sServiceForKVServiceDeployment(t *testing.T) {
	cr := &enterpriseApi.KVService{
		TypeMeta: metav1.TypeMeta{
			Kind: "KVService",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
			Labels: map[string]string{
				"parent": "true",
			},
			Annotations: map[string]string{
				"note": "from-parent",
			},
		},
	}
	cr.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "enterprise.splunk.com",
		Version: "v4",
		Kind:    "KVService",
	})

	spec := &enterpriseApi.KVServiceSpec{
		CommonWorkloadSpec: enterpriseApi.CommonWorkloadSpec{
			Spec: enterpriseApi.Spec{
				ServiceTemplate: corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"existing": "label",
						},
						Annotations: map[string]string{
							"existing-annotation": "true",
						},
					},
				},
			},
		},
	}

	service := GetKVServiceService(cr, spec)
	if service.Name != splcommon.GetSplunkServiceName(splcommon.SplunkKVService, cr.GetName(), false) {
		t.Errorf("service name=%s; want %s", service.Name, splcommon.GetSplunkServiceName(splcommon.SplunkKVService, cr.GetName(), false))
	}
	if service.Namespace != cr.GetNamespace() {
		t.Errorf("service namespace=%s; want %s", service.Namespace, cr.GetNamespace())
	}
	if service.Spec.Selector["app.kubernetes.io/instance"] != "splunk-stack1-kvservice" {
		t.Errorf("service selector instance=%s; want %s", service.Spec.Selector["app.kubernetes.io/instance"], "splunk-stack1-kvservice")
	}
	if service.Labels["parent"] != "true" {
		t.Errorf("service label parent missing")
	}
	if service.Labels["existing"] != "label" {
		t.Errorf("service label existing missing")
	}
	if service.Annotations["note"] != "from-parent" {
		t.Errorf("service annotation note missing")
	}
	if service.Annotations["existing-annotation"] != "true" {
		t.Errorf("service annotation existing-annotation missing")
	}
	if len(service.OwnerReferences) != 1 {
		t.Errorf("owner references=%d; want 1", len(service.OwnerReferences))
	}
	foundPort := false
	for _, port := range service.Spec.Ports {
		if port.Port == 8066 {
			foundPort = true
			break
		}
	}
	if !foundPort {
		t.Errorf("service port 8066 not found")
	}
}

func TestKVServicePodSettings(t *testing.T) {
	cr := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "test", Annotations: map[string]string{"sidecar.istio.io/inject": "true", "parent": "kept"}}}
	cr.Spec.PodAnnotations = map[string]string{"sidecar.istio.io/inject": "false", "custom": "value"}
	cr.Spec.LivenessInitialDelaySeconds = 90
	cr.Spec.ReadinessInitialDelaySeconds = 75
	cr.Spec.LivenessProbe = &enterpriseApi.Probe{TimeoutSeconds: 9, PeriodSeconds: 12, FailureThreshold: 8}
	cr.Spec.ReadinessProbe = &enterpriseApi.Probe{InitialDelaySeconds: 65, TimeoutSeconds: 7, PeriodSeconds: 10, FailureThreshold: 6}
	deployment := NewKVServiceDeployment(cr)
	annotations := deployment.Spec.Template.Annotations
	if annotations["sidecar.istio.io/inject"] != "false" || annotations["parent"] != "kept" || annotations["custom"] != "value" {
		t.Errorf("pod annotation precedence is incorrect: %v", annotations)
	}
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		t.Fatal("deployment containers empty")
	}
	container := deployment.Spec.Template.Spec.Containers[0]
	liveness, readiness := container.LivenessProbe, container.ReadinessProbe
	if liveness == nil || readiness == nil {
		t.Fatal("liveness and readiness probes must be present")
	}
	if liveness.InitialDelaySeconds != 90 || liveness.TimeoutSeconds != 9 || liveness.PeriodSeconds != 12 || liveness.FailureThreshold != 8 {
		t.Errorf("liveness settings ignored: %+v", liveness)
	}
	if readiness.InitialDelaySeconds != 65 || readiness.TimeoutSeconds != 7 || readiness.PeriodSeconds != 10 || readiness.FailureThreshold != 6 {
		t.Errorf("readiness settings ignored: %+v", readiness)
	}
	if container.StartupProbe != nil {
		t.Errorf("KVService must not configure a startup probe: %+v", container.StartupProbe)
	}
	for i, probe := range []*corev1.Probe{liveness, readiness} {
		path := []string{"/service/liveness", "/service/readiness"}[i]
		if probe.HTTPGet == nil || probe.HTTPGet.Path != path || probe.HTTPGet.Port.IntValue() != 8066 || probe.Exec != nil || probe.SuccessThreshold != 1 {
			t.Errorf("HTTP health endpoint lost: %+v", probe)
		}
	}
}

func TestKVServiceDeploymentVolumesPortsAndSecurity(t *testing.T) {
	cr := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "test"}}
	mode := int32(0400)
	cr.Spec.Volumes = []corev1.Volume{
		{Name: "db-tls", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "db-tls"}}},
		{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "config"}, DefaultMode: &mode}}},
	}
	cr.Spec.ExtraEnv = []corev1.EnvVar{{Name: "KVSERVICE_DB_PASSWORD", ValueFrom: &corev1.EnvVarSource{
		SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "postgres"}, Key: "password"},
	}}}
	cr.Spec.Resources.Requests = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")}
	cr.Spec.ServiceTemplate.Spec.Ports = []corev1.ServicePort{{Name: "metrics", Port: 9090, TargetPort: intstr.FromInt(9091), Protocol: corev1.ProtocolTCP}}
	SetKVServiceVolumeDefaults(&cr.Spec)
	assert.Equal(t, corev1.SecretVolumeSourceDefaultMode, *cr.Spec.Volumes[0].Secret.DefaultMode)
	assert.Equal(t, mode, *cr.Spec.Volumes[1].ConfigMap.DefaultMode, "explicit volume permissions must be preserved")
	before := cr.DeepCopy()
	deployment := NewKVServiceDeployment(cr)
	pod := &deployment.Spec.Template.Spec
	require.Len(t, pod.Containers, 1)
	container := &pod.Containers[0]
	assert.Equal(t, cr.Spec.ExtraEnv, container.Env)
	assert.Equal(t, cr.Spec.Resources, container.Resources)
	assert.Contains(t, container.Ports, corev1.ContainerPort{Name: "http-splunkv", ContainerPort: splcommon.KVServicePort, Protocol: corev1.ProtocolTCP})
	assert.Contains(t, container.Ports, corev1.ContainerPort{Name: "metrics", ContainerPort: 9091, Protocol: corev1.ProtocolTCP})
	assert.Equal(t, cr.Spec.Volumes, pod.Volumes)
	assert.Contains(t, container.VolumeMounts, corev1.VolumeMount{Name: "db-tls", MountPath: "/mnt/db-tls"})
	require.NotNil(t, pod.SecurityContext)
	assert.Equal(t, int64(41812), *pod.SecurityContext.RunAsUser)
	assert.Equal(t, int64(41812), *pod.SecurityContext.FSGroup)
	require.NotNil(t, container.SecurityContext)
	assert.True(t, *container.SecurityContext.RunAsNonRoot)
	assert.False(t, *container.SecurityContext.AllowPrivilegeEscalation)
	assert.False(t, *container.SecurityContext.Privileged)
	assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, container.SecurityContext.SeccompProfile.Type)
	assert.Contains(t, container.SecurityContext.Capabilities.Drop, corev1.Capability("ALL"))
	assert.Equal(t, before, cr, "building the Deployment must not mutate the CR")
	container.Env[0].ValueFrom.SecretKeyRef.Name = "changed"
	assert.Equal(t, "postgres", cr.Spec.ExtraEnv[0].ValueFrom.SecretKeyRef.Name)
	pod.Volumes[0].Secret.SecretName = "changed"
	assert.Equal(t, "db-tls", cr.Spec.Volumes[0].Secret.SecretName)
}
