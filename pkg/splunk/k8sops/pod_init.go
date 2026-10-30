// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package k8sops

import (
	"cmp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Compare in execution order. Kubernetes defaults otherwise make a stored
// initializer differ from the same desired initializer on every reconcile.
func initContainersEqual(current, desired []corev1.Container) bool {
	if len(current) != len(desired) {
		return false
	}
	for i := range current {
		if !apiequality.Semantic.DeepEqual(defaultedInitContainer(&current[i]), defaultedInitContainer(&desired[i])) {
			return false
		}
	}
	return true
}

// Normalize comparison copies only. Preserve explicit values, and leave the
// caller's template untouched. These defaults match Kubernetes container defaults.
func defaultedInitContainer(container *corev1.Container) *corev1.Container {
	result := container.DeepCopy()
	if result.ImagePullPolicy == "" {
		result.ImagePullPolicy = corev1.PullIfNotPresent
		// A registry port is not a tag. Inspect only the last name component,
		// before any digest. Digest-only references do not imply the latest tag.
		name, _, hasDigest := strings.Cut(result.Image, "@")
		if _, component, hasPath := strings.CutLast(name, "/"); hasPath {
			name = component
		}
		_, tag, hasTag := strings.CutLast(name, ":")
		if tag == "latest" || (name != "" && !hasTag && !hasDigest) {
			result.ImagePullPolicy = corev1.PullAlways
		}
	}
	if result.TerminationMessagePath == "" {
		result.TerminationMessagePath = corev1.TerminationMessagePathDefault
	}
	if result.TerminationMessagePolicy == "" {
		result.TerminationMessagePolicy = corev1.TerminationMessageReadFile
	}
	for i := range result.Ports {
		result.Ports[i].Protocol = cmp.Or(result.Ports[i].Protocol, corev1.ProtocolTCP)
	}
	for i := range result.Env {
		if source := result.Env[i].ValueFrom; source != nil {
			if source.FieldRef != nil {
				source.FieldRef.APIVersion = cmp.Or(source.FieldRef.APIVersion, "v1")
			}
			if source.FileKeyRef != nil && source.FileKeyRef.Optional == nil {
				source.FileKeyRef.Optional = new(false)
			}
		}
	}
	for _, quantities := range []corev1.ResourceList{result.Resources.Limits, result.Resources.Requests} {
		for name, quantity := range quantities {
			quantity.RoundUp(resource.Milli)
			quantities[name] = quantity
		}
	}
	for _, probe := range []*corev1.Probe{result.LivenessProbe, result.ReadinessProbe, result.StartupProbe} {
		if probe == nil {
			continue
		}
		probe.TimeoutSeconds = cmp.Or(probe.TimeoutSeconds, 1)
		probe.PeriodSeconds = cmp.Or(probe.PeriodSeconds, 10)
		probe.SuccessThreshold = cmp.Or(probe.SuccessThreshold, 1)
		probe.FailureThreshold = cmp.Or(probe.FailureThreshold, 3)
		defaultInitHTTPAction(probe.HTTPGet)
		if probe.GRPC != nil && probe.GRPC.Service == nil {
			probe.GRPC.Service = new("")
		}
	}
	if result.Lifecycle != nil {
		for _, handler := range []*corev1.LifecycleHandler{result.Lifecycle.PostStart, result.Lifecycle.PreStop} {
			if handler != nil {
				defaultInitHTTPAction(handler.HTTPGet)
			}
		}
	}
	return result
}

func defaultInitHTTPAction(action *corev1.HTTPGetAction) {
	if action != nil {
		action.Path = cmp.Or(action.Path, "/")
		action.Scheme = cmp.Or(action.Scheme, corev1.URISchemeHTTP)
	}
}
