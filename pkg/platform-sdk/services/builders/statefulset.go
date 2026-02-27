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

package builders

import (
	"context"
	"fmt"

	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/builders"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/certificate"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/secret"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ObservabilityService interface to avoid import cycle.
type ObservabilityService interface {
	ShouldAddObservability(ctx context.Context, namespace string) (bool, error)
	GetObservabilityAnnotations(ctx context.Context, namespace string) (map[string]string, error)
}

// StatefulSetBuilder builds StatefulSet resources with a fluent API.
type StatefulSetBuilder struct {
	namespace     string
	ownerName     string
	observability ObservabilityService
	ctx           context.Context

	// StatefulSet fields
	name        string
	replicas    *int32
	labels      map[string]string
	annotations map[string]string

	// Container fields
	image        string
	ports        []corev1.ContainerPort
	command      []string
	args         []string
	env          []corev1.EnvVar
	envFrom      []corev1.EnvFromSource
	volumeMounts []corev1.VolumeMount
	resources    corev1.ResourceRequirements

	// Pod fields
	volumes                       []corev1.Volume
	serviceAccountName            string
	podSecurityContext            *corev1.PodSecurityContext
	affinity                      *corev1.Affinity
	terminationGracePeriodSeconds *int64
	imagePullPolicy               corev1.PullPolicy
	priorityClassName             string

	// Container security and probes
	containerSecurityContext *corev1.SecurityContext
	livenessProbe            *corev1.Probe
	readinessProbe           *corev1.Probe
	startupProbe             *corev1.Probe

	// StatefulSet-specific
	serviceName          string
	volumeClaimTemplates []corev1.PersistentVolumeClaim
	updateStrategy       *appsv1.StatefulSetUpdateStrategy
	pvcRetentionPolicy   *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy

	// SDK-managed resources
	certificates []*certificate.Ref
	secrets      []*secret.Ref
	configMaps   []string

	// Observability
	addObservability bool
}

// NewStatefulSetBuilder creates a new StatefulSetBuilder.
func NewStatefulSetBuilder(namespace, ownerName string, observability ObservabilityService) *StatefulSetBuilder {
	return &StatefulSetBuilder{
		namespace:     namespace,
		ownerName:     ownerName,
		observability: observability,
		labels:        make(map[string]string),
		annotations:   make(map[string]string),
		replicas:      int32Ptr(1),
	}
}

// WithName sets the StatefulSet name.
func (b *StatefulSetBuilder) WithName(name string) builders.StatefulSetBuilder {
	b.name = name
	return b
}

// WithNamespace sets the namespace.
func (b *StatefulSetBuilder) WithNamespace(namespace string) builders.StatefulSetBuilder {
	b.namespace = namespace
	return b
}

// WithReplicas sets the replica count.
func (b *StatefulSetBuilder) WithReplicas(replicas int32) builders.StatefulSetBuilder {
	b.replicas = &replicas
	return b
}

// WithImage sets the container image.
func (b *StatefulSetBuilder) WithImage(image string) builders.StatefulSetBuilder {
	b.image = image
	return b
}

// WithPorts sets the container ports.
func (b *StatefulSetBuilder) WithPorts(ports []corev1.ContainerPort) builders.StatefulSetBuilder {
	b.ports = ports
	return b
}

// WithCommand sets the container command.
func (b *StatefulSetBuilder) WithCommand(command []string) builders.StatefulSetBuilder {
	b.command = command
	return b
}

// WithArgs sets the container args.
func (b *StatefulSetBuilder) WithArgs(args []string) builders.StatefulSetBuilder {
	b.args = args
	return b
}

// WithSecret adds a secret reference.
func (b *StatefulSetBuilder) WithSecret(ref *secret.Ref) builders.StatefulSetBuilder {
	b.secrets = append(b.secrets, ref)
	return b
}

// WithCertificate adds a certificate reference.
func (b *StatefulSetBuilder) WithCertificate(ref *certificate.Ref) builders.StatefulSetBuilder {
	b.certificates = append(b.certificates, ref)
	return b
}

// WithConfigMap adds a config map reference.
func (b *StatefulSetBuilder) WithConfigMap(name string) builders.StatefulSetBuilder {
	b.configMaps = append(b.configMaps, name)
	return b
}

// WithObservability enables observability annotations.
func (b *StatefulSetBuilder) WithObservability() builders.StatefulSetBuilder {
	b.addObservability = true
	return b
}

// WithEnv adds an environment variable.
func (b *StatefulSetBuilder) WithEnv(env corev1.EnvVar) builders.StatefulSetBuilder {
	b.env = append(b.env, env)
	return b
}

// WithEnvFrom adds an environment source.
func (b *StatefulSetBuilder) WithEnvFrom(envFrom corev1.EnvFromSource) builders.StatefulSetBuilder {
	b.envFrom = append(b.envFrom, envFrom)
	return b
}

// WithVolume adds a volume.
func (b *StatefulSetBuilder) WithVolume(volume corev1.Volume) builders.StatefulSetBuilder {
	b.volumes = append(b.volumes, volume)
	return b
}

// WithVolumeMount adds a volume mount.
func (b *StatefulSetBuilder) WithVolumeMount(mount corev1.VolumeMount) builders.StatefulSetBuilder {
	b.volumeMounts = append(b.volumeMounts, mount)
	return b
}

// WithResources sets resource requirements.
func (b *StatefulSetBuilder) WithResources(resources corev1.ResourceRequirements) builders.StatefulSetBuilder {
	b.resources = resources
	return b
}

// WithLabels sets labels.
func (b *StatefulSetBuilder) WithLabels(labels map[string]string) builders.StatefulSetBuilder {
	for k, v := range labels {
		b.labels[k] = v
	}
	return b
}

// WithAnnotations sets annotations.
func (b *StatefulSetBuilder) WithAnnotations(annotations map[string]string) builders.StatefulSetBuilder {
	for k, v := range annotations {
		b.annotations[k] = v
	}
	return b
}

// WithPodSecurityContext sets the pod security context.
func (b *StatefulSetBuilder) WithPodSecurityContext(psc *corev1.PodSecurityContext) builders.StatefulSetBuilder {
	b.podSecurityContext = psc
	return b
}

// WithSecurityContext sets the container security context.
func (b *StatefulSetBuilder) WithSecurityContext(sc *corev1.SecurityContext) builders.StatefulSetBuilder {
	b.containerSecurityContext = sc
	return b
}

// WithAffinity sets pod affinity rules.
func (b *StatefulSetBuilder) WithAffinity(affinity *corev1.Affinity) builders.StatefulSetBuilder {
	b.affinity = affinity
	return b
}

// WithLivenessProbe sets the liveness probe.
func (b *StatefulSetBuilder) WithLivenessProbe(probe *corev1.Probe) builders.StatefulSetBuilder {
	b.livenessProbe = probe
	return b
}

// WithReadinessProbe sets the readiness probe.
func (b *StatefulSetBuilder) WithReadinessProbe(probe *corev1.Probe) builders.StatefulSetBuilder {
	b.readinessProbe = probe
	return b
}

// WithStartupProbe sets the startup probe.
func (b *StatefulSetBuilder) WithStartupProbe(probe *corev1.Probe) builders.StatefulSetBuilder {
	b.startupProbe = probe
	return b
}

// WithServiceName sets the StatefulSet service name.
func (b *StatefulSetBuilder) WithServiceName(name string) builders.StatefulSetBuilder {
	b.serviceName = name
	return b
}

// WithVolumeClaimTemplate adds a volume claim template.
func (b *StatefulSetBuilder) WithVolumeClaimTemplate(pvc corev1.PersistentVolumeClaim) builders.StatefulSetBuilder {
	b.volumeClaimTemplates = append(b.volumeClaimTemplates, pvc)
	return b
}

// WithUpdateStrategy sets the update strategy.
func (b *StatefulSetBuilder) WithUpdateStrategy(strategy appsv1.StatefulSetUpdateStrategy) builders.StatefulSetBuilder {
	b.updateStrategy = &strategy
	return b
}

// WithPVCRetentionPolicy sets the PVC retention policy.
func (b *StatefulSetBuilder) WithPVCRetentionPolicy(policy *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy) builders.StatefulSetBuilder {
	b.pvcRetentionPolicy = policy
	return b
}

// WithImagePullPolicy sets the image pull policy.
func (b *StatefulSetBuilder) WithImagePullPolicy(policy corev1.PullPolicy) builders.StatefulSetBuilder {
	b.imagePullPolicy = policy
	return b
}

// WithTerminationGracePeriodSeconds sets the termination grace period.
func (b *StatefulSetBuilder) WithTerminationGracePeriodSeconds(seconds int64) builders.StatefulSetBuilder {
	b.terminationGracePeriodSeconds = &seconds
	return b
}

// WithPriorityClassName sets the priority class name.
func (b *StatefulSetBuilder) WithPriorityClassName(className string) builders.StatefulSetBuilder {
	b.priorityClassName = className
	return b
}

// Build constructs the StatefulSet.
func (b *StatefulSetBuilder) Build() (*appsv1.StatefulSet, error) {
	if b.name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if b.image == "" {
		return nil, fmt.Errorf("image is required")
	}

	// Add default labels
	labels := b.buildLabels()
	annotations := b.buildAnnotations()

	// Add observability annotations if requested
	if b.addObservability && b.observability != nil && b.ctx != nil {
		obsAnnotations, err := b.observability.GetObservabilityAnnotations(b.ctx, b.namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to get observability annotations: %w", err)
		}
		for k, v := range obsAnnotations {
			annotations[k] = v
		}
	}

	// Add volumes for SDK-managed resources
	volumes := b.buildVolumes()
	volumeMounts := b.buildVolumeMounts()

	// Build container
	container := corev1.Container{
		Name:            b.name,
		Image:           b.image,
		ImagePullPolicy: b.imagePullPolicy,
		Ports:           b.ports,
		Command:         b.command,
		Args:            b.args,
		Env:             b.env,
		EnvFrom:         b.envFrom,
		VolumeMounts:    volumeMounts,
		Resources:       b.resources,
		SecurityContext: b.containerSecurityContext,
		LivenessProbe:   b.livenessProbe,
		ReadinessProbe:  b.readinessProbe,
		StartupProbe:    b.startupProbe,
	}

	// Build pod template
	podTemplate := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{container},
			Volumes:    volumes,
		},
	}

	if b.serviceAccountName != "" {
		podTemplate.Spec.ServiceAccountName = b.serviceAccountName
	}

	if b.podSecurityContext != nil {
		podTemplate.Spec.SecurityContext = b.podSecurityContext
	}

	if b.affinity != nil {
		podTemplate.Spec.Affinity = b.affinity
	}

	if b.terminationGracePeriodSeconds != nil {
		podTemplate.Spec.TerminationGracePeriodSeconds = b.terminationGracePeriodSeconds
	}

	if b.priorityClassName != "" {
		podTemplate.Spec.PriorityClassName = b.priorityClassName
	}

	// Build StatefulSet
	serviceName := b.serviceName
	if serviceName == "" {
		serviceName = b.name
	}

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        b.name,
			Namespace:   b.namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: b.replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template:             podTemplate,
			ServiceName:          serviceName,
			VolumeClaimTemplates: b.volumeClaimTemplates,
			PodManagementPolicy:  appsv1.ParallelPodManagement,
		},
	}

	// Set optional fields
	if b.updateStrategy != nil {
		statefulSet.Spec.UpdateStrategy = *b.updateStrategy
	}

	if b.pvcRetentionPolicy != nil {
		statefulSet.Spec.PersistentVolumeClaimRetentionPolicy = b.pvcRetentionPolicy
	}

	return statefulSet, nil
}

// buildLabels constructs the label map.
func (b *StatefulSetBuilder) buildLabels() map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/name":       b.name,
		"app.kubernetes.io/instance":   b.ownerName,
		"app.kubernetes.io/managed-by": "splunk-operator",
	}

	// Merge user labels
	for k, v := range b.labels {
		labels[k] = v
	}

	return labels
}

// buildAnnotations constructs the annotation map.
func (b *StatefulSetBuilder) buildAnnotations() map[string]string {
	annotations := make(map[string]string)

	// Merge user annotations
	for k, v := range b.annotations {
		annotations[k] = v
	}

	return annotations
}

// buildVolumes constructs volumes from SDK-managed resources.
func (b *StatefulSetBuilder) buildVolumes() []corev1.Volume {
	volumes := make([]corev1.Volume, 0, len(b.volumes))

	// Add user-provided volumes
	volumes = append(volumes, b.volumes...)

	// Add certificate volumes
	for i, cert := range b.certificates {
		volumeName := fmt.Sprintf("cert-%d", i)
		if cert.SecretName != "" {
			volumeName = cert.SecretName
		}

		volumes = append(volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cert.SecretName,
				},
			},
		})
	}

	// Add secret volumes
	for _, sec := range b.secrets {
		// Check if this is a CSI secret
		if sec.CSI != nil {
			// Create CSI volume
			volumes = append(volumes, corev1.Volume{
				Name: sec.SecretName,
				VolumeSource: corev1.VolumeSource{
					CSI: &corev1.CSIVolumeSource{
						Driver:   sec.CSI.Driver,
						ReadOnly: boolPtr(true),
						VolumeAttributes: map[string]string{
							"secretProviderClass": sec.CSI.ProviderClass,
						},
					},
				},
			})
		} else {
			// Create K8s Secret volume
			volumes = append(volumes, corev1.Volume{
				Name: sec.SecretName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: sec.SecretName,
					},
				},
			})
		}
	}

	// Add config map volumes
	for _, cm := range b.configMaps {
		volumes = append(volumes, corev1.Volume{
			Name: cm,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cm,
					},
				},
			},
		})
	}

	return volumes
}

// buildVolumeMounts constructs volume mounts.
func (b *StatefulSetBuilder) buildVolumeMounts() []corev1.VolumeMount {
	mounts := make([]corev1.VolumeMount, 0, len(b.volumeMounts))

	// Add user-provided mounts
	mounts = append(mounts, b.volumeMounts...)

	// Add default mounts for certificates
	for i, cert := range b.certificates {
		volumeName := fmt.Sprintf("cert-%d", i)
		if cert.SecretName != "" {
			volumeName = cert.SecretName
		}

		// Use custom mount path if specified, otherwise default to /etc/certs/<volumeName>
		mountPath := cert.MountPath
		if mountPath == "" {
			mountPath = fmt.Sprintf("/etc/certs/%s", volumeName)
		}

		mounts = append(mounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: mountPath,
			ReadOnly:  true,
		})
	}

	// Add mounts for secrets - support both CSI and K8s Secrets
	for _, sec := range b.secrets {
		if sec.CSI != nil {
			// CSI secret - use CSI-specific mount path
			mounts = append(mounts, corev1.VolumeMount{
				Name:      sec.SecretName,
				MountPath: sec.CSI.MountPath,
				ReadOnly:  true,
			})
		}
		// Note: K8s Secret volumes don't need explicit mounts here
		// They are referenced via envFrom in the container spec
	}

	return mounts
}

// int32Ptr returns a pointer to an int32 value.
func int32Ptr(i int32) *int32 {
	return &i
}

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool {
	return &b
}
