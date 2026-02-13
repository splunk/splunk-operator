// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//  http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package builders provides fluent builders for Kubernetes resources.
package builders

import (
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/certificate"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/secret"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// StatefulSetBuilder builds a StatefulSet with SDK integrations.
//
// The builder automatically:
// - Mounts secrets at /mnt/splunk-secrets
// - Mounts certificates at /mnt/tls and /mnt/ca-bundle
// - Adds observability annotations (Prometheus)
// - Sets owner references for garbage collection
// - Applies SDK conventions for labels and selectors
//
// Example:
//
//	sts, err := rctx.BuildStatefulSet().
//	    WithName("postgres").
//	    WithNamespace("default").
//	    WithReplicas(1).
//	    WithImage("postgres:15").
//	    WithSecret(secret).
//	    WithCertificate(cert).
//	    WithObservability().
//	    Build()
type StatefulSetBuilder interface {
	WithName(name string) StatefulSetBuilder
	WithNamespace(namespace string) StatefulSetBuilder
	WithReplicas(replicas int32) StatefulSetBuilder
	WithImage(image string) StatefulSetBuilder
	WithPorts(ports []corev1.ContainerPort) StatefulSetBuilder
	WithCommand(command []string) StatefulSetBuilder
	WithArgs(args []string) StatefulSetBuilder

	// Automatically mounts secrets and certificates
	WithSecret(ref *secret.Ref) StatefulSetBuilder
	WithCertificate(ref *certificate.Ref) StatefulSetBuilder
	WithConfigMap(name string) StatefulSetBuilder

	// Adds observability annotations
	WithObservability() StatefulSetBuilder

	// Environment variables
	WithEnv(env corev1.EnvVar) StatefulSetBuilder
	WithEnvFrom(envFrom corev1.EnvFromSource) StatefulSetBuilder

	// Additional customization
	WithVolume(volume corev1.Volume) StatefulSetBuilder
	WithVolumeMount(mount corev1.VolumeMount) StatefulSetBuilder
	WithResources(resources corev1.ResourceRequirements) StatefulSetBuilder
	WithLabels(labels map[string]string) StatefulSetBuilder
	WithAnnotations(annotations map[string]string) StatefulSetBuilder

	// Security contexts
	WithPodSecurityContext(psc *corev1.PodSecurityContext) StatefulSetBuilder
	WithSecurityContext(sc *corev1.SecurityContext) StatefulSetBuilder

	// Probes
	WithLivenessProbe(probe *corev1.Probe) StatefulSetBuilder
	WithReadinessProbe(probe *corev1.Probe) StatefulSetBuilder
	WithStartupProbe(probe *corev1.Probe) StatefulSetBuilder

	// Scheduling
	WithAffinity(affinity *corev1.Affinity) StatefulSetBuilder

	// StatefulSet-specific
	WithServiceName(name string) StatefulSetBuilder
	WithVolumeClaimTemplate(pvc corev1.PersistentVolumeClaim) StatefulSetBuilder
	WithUpdateStrategy(strategy appsv1.StatefulSetUpdateStrategy) StatefulSetBuilder
	WithPVCRetentionPolicy(policy *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy) StatefulSetBuilder
	WithImagePullPolicy(policy corev1.PullPolicy) StatefulSetBuilder
	WithTerminationGracePeriodSeconds(seconds int64) StatefulSetBuilder
	WithPriorityClassName(className string) StatefulSetBuilder

	// Build the final StatefulSet
	Build() (*appsv1.StatefulSet, error)
}

// ServiceBuilder builds a Service with SDK integrations.
type ServiceBuilder interface {
	WithName(name string) ServiceBuilder
	WithNamespace(namespace string) ServiceBuilder
	WithType(serviceType corev1.ServiceType) ServiceBuilder
	WithPorts(ports []corev1.ServicePort) ServiceBuilder
	WithSelector(selector map[string]string) ServiceBuilder
	WithLabels(labels map[string]string) ServiceBuilder
	WithAnnotations(annotations map[string]string) ServiceBuilder

	// Adds discovery labels for DiscoveryService
	WithDiscoveryLabels() ServiceBuilder

	Build() (*corev1.Service, error)
}

// ConfigMapBuilder builds a ConfigMap with SDK integrations.
type ConfigMapBuilder interface {
	WithName(name string) ConfigMapBuilder
	WithNamespace(namespace string) ConfigMapBuilder
	WithData(data map[string]string) ConfigMapBuilder
	WithBinaryData(binaryData map[string][]byte) ConfigMapBuilder
	WithLabels(labels map[string]string) ConfigMapBuilder
	WithAnnotations(annotations map[string]string) ConfigMapBuilder

	Build() (*corev1.ConfigMap, error)
}

// PodBuilder builds Pod specifications.
// Useful for building pod specs that are shared across resources.
type PodBuilder interface {
	WithContainers(containers []corev1.Container) PodBuilder
	WithInitContainers(initContainers []corev1.Container) PodBuilder
	WithVolumes(volumes []corev1.Volume) PodBuilder
	WithServiceAccountName(serviceAccountName string) PodBuilder
	WithSecurityContext(securityContext *corev1.PodSecurityContext) PodBuilder
	WithAffinity(affinity *corev1.Affinity) PodBuilder
	WithTolerations(tolerations []corev1.Toleration) PodBuilder
	WithNodeSelector(nodeSelector map[string]string) PodBuilder
	WithLabels(labels map[string]string) PodBuilder
	WithAnnotations(annotations map[string]string) PodBuilder

	Build() (*corev1.PodSpec, error)
}

// DeploymentBuilder builds a Deployment with SDK integrations.
type DeploymentBuilder interface {
	WithName(name string) DeploymentBuilder
	WithNamespace(namespace string) DeploymentBuilder
	WithReplicas(replicas int32) DeploymentBuilder
	WithImage(image string) DeploymentBuilder
	WithPorts(ports []corev1.ContainerPort) DeploymentBuilder
	WithCommand(command []string) DeploymentBuilder
	WithArgs(args []string) DeploymentBuilder

	// Automatically mounts secrets and certificates
	WithSecret(ref *secret.Ref) DeploymentBuilder
	WithCertificate(ref *certificate.Ref) DeploymentBuilder
	WithConfigMap(name string) DeploymentBuilder

	// Adds observability annotations
	WithObservability() DeploymentBuilder

	// Environment variables
	WithEnv(env corev1.EnvVar) DeploymentBuilder
	WithEnvFrom(envFrom corev1.EnvFromSource) DeploymentBuilder

	// Additional customization
	WithVolume(volume corev1.Volume) DeploymentBuilder
	WithVolumeMount(mount corev1.VolumeMount) DeploymentBuilder
	WithResources(resources corev1.ResourceRequirements) DeploymentBuilder
	WithLabels(labels map[string]string) DeploymentBuilder
	WithAnnotations(annotations map[string]string) DeploymentBuilder

	Build() (*appsv1.Deployment, error)
}
