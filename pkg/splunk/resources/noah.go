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
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

const (
	NoahEnabledEnvName           = "SPLUNK_NOAH_ENABLED"
	NoahAdvertisedAddressEnvName = "SPLUNK_NOAH_ADVERTISED_ADDR"
	HeadlessServiceEnvName       = "SPLUNK_HEADLESS_SERVICE_NAME"
	ClusterDomainEnvName         = "CLUSTER_DOMAIN"
	PodNameEnvName               = "POD_NAME"
	PodNamespaceEnvName          = "POD_NAMESPACE"

	noahCacheWarmDecommissionCommand  = `touch "$SPLUNK_HOME/var/run/splunk/decommission_for_cache_warming"`
	noahManagementPort                = 8089
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
			{Name: HeadlessServiceEnvName, Value: statefulSet.Spec.ServiceName},
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
			container.Env = replaceAndAppendEnvVars(container.Env, identityEnv)
		}
	}
}

// WithNoahIndexerIdentity supplies the stable advertised management address
// consumed by a Noah indexer. The Downward API variables are added first so
// Kubernetes expands them in SPLUNK_NOAH_ADVERTISED_ADDR.
func WithNoahIndexerIdentity(clusterDomain string) StatefulSetOption {
	if clusterDomain == "" {
		clusterDomain = "cluster.local"
	}
	podIdentity := WithNoahPodIdentity(clusterDomain)

	return func(statefulSet *appsv1.StatefulSet) {
		podIdentity(statefulSet)
		advertisedAddress := corev1.EnvVar{
			Name: NoahAdvertisedAddressEnvName,
			Value: fmt.Sprintf(
				"https://$(%s).%s.$(%s).svc.%s:%d",
				PodNameEnvName,
				statefulSet.Spec.ServiceName,
				PodNamespaceEnvName,
				clusterDomain,
				noahManagementPort,
			),
		}

		for i := range statefulSet.Spec.Template.Spec.Containers {
			container := &statefulSet.Spec.Template.Spec.Containers[i]
			if container.Name != "splunk" {
				continue
			}
			container.Env = replaceAndAppendEnvVars(container.Env, []corev1.EnvVar{advertisedAddress})
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

func replaceAndAppendEnvVars(existing, desired []corev1.EnvVar) []corev1.EnvVar {
	names := make(map[string]struct{}, len(desired))
	for _, env := range desired {
		names[env.Name] = struct{}{}
	}

	result := make([]corev1.EnvVar, 0, len(existing)+len(desired))
	for _, env := range existing {
		if _, replace := names[env.Name]; !replace {
			result = append(result, env)
		}
	}

	return append(result, desired...)
}
