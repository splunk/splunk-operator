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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

const (
	NoahEnabledEnvName         = "SPLUNK_NOAH_ENABLED"
	NoahHeadlessServiceEnvName = "SPLUNK_HEADLESS_SERVICE_NAME"
	ClusterDomainEnvName       = "CLUSTER_DOMAIN"
	PodNameEnvName             = "POD_NAME"
	PodNamespaceEnvName        = "POD_NAMESPACE"

	noahCacheWarmDecommissionCommand  = `touch "$SPLUNK_HOME/var/run/splunk/decommission_for_cache_warming"`
	noahTerminationGracePeriodSeconds = int64(15 * 60)
)

// WithNoahPodIdentity supplies the StatefulSet identity inputs consumed by the
// Noah-aware Splunk image. Pod name and namespace come from the Downward API;
// the governing Service and cluster domain are stable for every ordinal.
//
// The option is intentionally opt-in so classic workloads do not receive a
// Noah image contract or an unnecessary pod-template update.
func WithNoahPodIdentity(clusterDomain string) StatefulSetOption {
	if clusterDomain == "" {
		clusterDomain = "cluster.local"
	}

	return func(statefulSet *appsv1.StatefulSet) {
		identityEnv := []corev1.EnvVar{
			{Name: NoahEnabledEnvName, Value: "true"},
			{Name: NoahHeadlessServiceEnvName, Value: statefulSet.Spec.ServiceName},
			{Name: ClusterDomainEnvName, Value: clusterDomain},
			{
				Name: PodNameEnvName,
				ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  "metadata.name",
				}},
			},
			{
				Name: PodNamespaceEnvName,
				ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  "metadata.namespace",
				}},
			},
		}

		for i := range statefulSet.Spec.Template.Spec.Containers {
			container := &statefulSet.Spec.Template.Spec.Containers[i]
			if container.Name != "splunk" {
				continue
			}
			container.Env = upsertEnvVars(container.Env, identityEnv)
		}
	}
}

// WithNoahCacheWarmDecommission arms Splunk's cache-warm decommission before
// Kubernetes starts normal container termination.
func WithNoahCacheWarmDecommission() StatefulSetOption {
	return func(statefulSet *appsv1.StatefulSet) {
		statefulSet.Spec.Template.Spec.TerminationGracePeriodSeconds = new(noahTerminationGracePeriodSeconds)

		for i := range statefulSet.Spec.Template.Spec.Containers {
			container := &statefulSet.Spec.Template.Spec.Containers[i]
			if container.Name != "splunk" {
				continue
			}
			if container.Lifecycle == nil {
				container.Lifecycle = &corev1.Lifecycle{}
			}
			container.Lifecycle.PreStop = &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{Command: []string{"/bin/sh", "-c", noahCacheWarmDecommissionCommand}},
			}
		}
	}
}

func upsertEnvVars(existing, desired []corev1.EnvVar) []corev1.EnvVar {
	replacements := make(map[string]corev1.EnvVar, len(desired))
	for _, env := range desired {
		replacements[env.Name] = env
	}

	result := make([]corev1.EnvVar, 0, len(existing)+len(desired))
	seen := make(map[string]bool, len(desired))
	for _, env := range existing {
		replacement, managed := replacements[env.Name]
		if !managed {
			result = append(result, env)
			continue
		}
		if seen[env.Name] {
			continue
		}
		result = append(result, replacement)
		seen[env.Name] = true
	}

	for _, env := range desired {
		if seen[env.Name] {
			continue
		}
		result = append(result, env)
	}
	return result
}
