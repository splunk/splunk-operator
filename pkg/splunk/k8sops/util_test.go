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

package k8sops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestHasProbeChanged(t *testing.T) {
	var current, revised corev1.PodTemplateSpec
	revised.Spec.Containers = []corev1.Container{{Image: "splunk/splunk"}}
	revised.Spec.Containers[0].LivenessProbe = &corev1.Probe{InitialDelaySeconds: 120}

	current.Spec.Containers = []corev1.Container{{Image: "splunk/splunk"}}
	current.Spec.Containers[0].LivenessProbe = &corev1.Probe{InitialDelaySeconds: 100}

	// Check return is false when both probes are nil
	result := hasProbeChanged(nil, nil)
	if result {
		t.Errorf("Both probes nil. hasProbeChanged() returned %t; want %t", true, false)
	}

	// Check return is true when currentProbe is true and revisedProbe is not nil
	result = hasProbeChanged(nil, revised.Spec.Containers[0].LivenessProbe)
	if !result {
		t.Errorf("current Probe nil. hasProbeChanged() returned %t; want %t", false, true)
	}

	// Check return is true when current probe and revised probe InitialDelaySeconds is different
	result = hasProbeChanged(current.Spec.Containers[0].LivenessProbe, revised.Spec.Containers[0].LivenessProbe)
	if !result {
		t.Errorf("InitialDelaySeconds different. hasProbeChanged() returned %t; want %t", false, true)
	}

	// Check return is true when current probe and revised probe TimeoutSeconds is different
	current.Spec.Containers[0].LivenessProbe.InitialDelaySeconds = revised.Spec.Containers[0].LivenessProbe.InitialDelaySeconds
	current.Spec.Containers[0].LivenessProbe.TimeoutSeconds = 120
	revised.Spec.Containers[0].LivenessProbe.TimeoutSeconds = 100
	result = hasProbeChanged(current.Spec.Containers[0].LivenessProbe, revised.Spec.Containers[0].LivenessProbe)
	if !result {
		t.Errorf("TimoutSeconds different. hasProbeChanged() returned %t; want %t", false, true)
	}

	// Check return is true when current probe and revised probe PeriodSeconds is different
	current.Spec.Containers[0].LivenessProbe.TimeoutSeconds = revised.Spec.Containers[0].LivenessProbe.TimeoutSeconds
	current.Spec.Containers[0].LivenessProbe.PeriodSeconds = 120
	revised.Spec.Containers[0].LivenessProbe.PeriodSeconds = 100
	result = hasProbeChanged(current.Spec.Containers[0].LivenessProbe, revised.Spec.Containers[0].LivenessProbe)
	if !result {
		t.Errorf("PeriodSeconds different. hasProbeChanged() returned %t; want %t", false, true)
	}

	// Check return is true when current probe and revised probe FailureThreshold is different
	current.Spec.Containers[0].LivenessProbe.PeriodSeconds = revised.Spec.Containers[0].LivenessProbe.PeriodSeconds
	current.Spec.Containers[0].LivenessProbe.FailureThreshold = 120
	revised.Spec.Containers[0].LivenessProbe.FailureThreshold = 100
	result = hasProbeChanged(current.Spec.Containers[0].LivenessProbe, revised.Spec.Containers[0].LivenessProbe)
	if !result {
		t.Errorf("FailureThreshold different. hasProbeChanged() returned %t; want %t", false, true)
	}
}

func TestHasProbeChangedHandler(t *testing.T) {
	httpProbe := func(scheme corev1.URIScheme, path string) *corev1.Probe {
		return &corev1.Probe{HTTPGet: &corev1.HTTPGetAction{Scheme: scheme, Path: path}}
	}
	tests := []struct {
		name             string
		current, revised *corev1.Probe
		want             bool
	}{
		{name: "removed probe", current: httpProbe(corev1.URISchemeHTTP, "/"), want: true},
		{name: "enable TLS", current: httpProbe(corev1.URISchemeHTTP, "/health"), revised: httpProbe(corev1.URISchemeHTTPS, "/health"), want: true},
		{name: "disable TLS", current: httpProbe(corev1.URISchemeHTTPS, "/health"), revised: httpProbe(corev1.URISchemeHTTP, "/health"), want: true},
		{name: "changed path", current: httpProbe(corev1.URISchemeHTTP, "/old"), revised: httpProbe(corev1.URISchemeHTTP, "/new"), want: true},
		{name: "default scheme", current: httpProbe(corev1.URISchemeHTTP, "/health"), revised: httpProbe("", "/health")},
		{name: "default path", current: httpProbe(corev1.URISchemeHTTP, "/"), revised: httpProbe(corev1.URISchemeHTTP, "")},
		{name: "changed exec", current: &corev1.Probe{Exec: &corev1.ExecAction{Command: []string{"old"}}}, revised: &corev1.Probe{Exec: &corev1.ExecAction{Command: []string{"new"}}}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalCurrent, originalRevised := tt.current.DeepCopy(), tt.revised.DeepCopy()
			assert.Equal(t, tt.want, hasProbeChanged(tt.current, tt.revised))
			assert.Equal(t, originalCurrent, tt.current, "comparison must not mutate current probe")
			assert.Equal(t, originalRevised, tt.revised, "comparison must not mutate desired probe")
		})
	}
}
