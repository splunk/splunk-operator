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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
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
	cr.Spec.StartupProbe = &enterpriseApi.Probe{InitialDelaySeconds: 60, TimeoutSeconds: 5, PeriodSeconds: 10, FailureThreshold: 20}
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
	if liveness.InitialDelaySeconds != 30 || liveness.TimeoutSeconds != 1 || liveness.PeriodSeconds != 5 || liveness.FailureThreshold != 3 {
		t.Errorf("fixed liveness settings overridden: %+v", liveness)
	}
	if readiness.InitialDelaySeconds != 30 || readiness.TimeoutSeconds != 1 || readiness.PeriodSeconds != 5 || readiness.FailureThreshold != 3 {
		t.Errorf("fixed readiness settings overridden: %+v", readiness)
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
