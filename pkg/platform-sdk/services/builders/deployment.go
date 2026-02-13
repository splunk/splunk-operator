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

// DeploymentBuilder builds Deployment resources with a fluent API.
type DeploymentBuilder struct {
	namespace     string
	ownerName     string
	observability ObservabilityService
	ctx           context.Context

	// Deployment fields
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
	volumes            []corev1.Volume
	serviceAccountName string
	securityContext    *corev1.PodSecurityContext

	// SDK-managed resources
	certificates []*certificate.Ref
	secrets      []*secret.Ref
	configMaps   []string

	// Observability
	addObservability bool
}

// NewDeploymentBuilder creates a new DeploymentBuilder.
func NewDeploymentBuilder(namespace, ownerName string, observability ObservabilityService) *DeploymentBuilder {
	return &DeploymentBuilder{
		namespace:     namespace,
		ownerName:     ownerName,
		observability: observability,
		labels:        make(map[string]string),
		annotations:   make(map[string]string),
		replicas:      int32Ptr(1),
	}
}

// WithName sets the Deployment name.
func (b *DeploymentBuilder) WithName(name string) builders.DeploymentBuilder {
	b.name = name
	return b
}

// WithNamespace sets the namespace.
func (b *DeploymentBuilder) WithNamespace(namespace string) builders.DeploymentBuilder {
	b.namespace = namespace
	return b
}

// WithReplicas sets the replica count.
func (b *DeploymentBuilder) WithReplicas(replicas int32) builders.DeploymentBuilder {
	b.replicas = &replicas
	return b
}

// WithImage sets the container image.
func (b *DeploymentBuilder) WithImage(image string) builders.DeploymentBuilder {
	b.image = image
	return b
}

// WithPorts sets the container ports.
func (b *DeploymentBuilder) WithPorts(ports []corev1.ContainerPort) builders.DeploymentBuilder {
	b.ports = ports
	return b
}

// WithCommand sets the container command.
func (b *DeploymentBuilder) WithCommand(command []string) builders.DeploymentBuilder {
	b.command = command
	return b
}

// WithArgs sets the container args.
func (b *DeploymentBuilder) WithArgs(args []string) builders.DeploymentBuilder {
	b.args = args
	return b
}

// WithSecret adds a secret reference.
func (b *DeploymentBuilder) WithSecret(ref *secret.Ref) builders.DeploymentBuilder {
	b.secrets = append(b.secrets, ref)
	return b
}

// WithCertificate adds a certificate reference.
func (b *DeploymentBuilder) WithCertificate(ref *certificate.Ref) builders.DeploymentBuilder {
	b.certificates = append(b.certificates, ref)
	return b
}

// WithConfigMap adds a config map reference.
func (b *DeploymentBuilder) WithConfigMap(name string) builders.DeploymentBuilder {
	b.configMaps = append(b.configMaps, name)
	return b
}

// WithObservability enables observability annotations.
func (b *DeploymentBuilder) WithObservability() builders.DeploymentBuilder {
	b.addObservability = true
	return b
}

// WithEnv adds an environment variable.
func (b *DeploymentBuilder) WithEnv(env corev1.EnvVar) builders.DeploymentBuilder {
	b.env = append(b.env, env)
	return b
}

// WithEnvFrom adds an environment source.
func (b *DeploymentBuilder) WithEnvFrom(envFrom corev1.EnvFromSource) builders.DeploymentBuilder {
	b.envFrom = append(b.envFrom, envFrom)
	return b
}

// WithVolume adds a volume.
func (b *DeploymentBuilder) WithVolume(volume corev1.Volume) builders.DeploymentBuilder {
	b.volumes = append(b.volumes, volume)
	return b
}

// WithVolumeMount adds a volume mount.
func (b *DeploymentBuilder) WithVolumeMount(mount corev1.VolumeMount) builders.DeploymentBuilder {
	b.volumeMounts = append(b.volumeMounts, mount)
	return b
}

// WithResources sets resource requirements.
func (b *DeploymentBuilder) WithResources(resources corev1.ResourceRequirements) builders.DeploymentBuilder {
	b.resources = resources
	return b
}

// WithLabels sets labels.
func (b *DeploymentBuilder) WithLabels(labels map[string]string) builders.DeploymentBuilder {
	for k, v := range labels {
		b.labels[k] = v
	}
	return b
}

// WithAnnotations sets annotations.
func (b *DeploymentBuilder) WithAnnotations(annotations map[string]string) builders.DeploymentBuilder {
	for k, v := range annotations {
		b.annotations[k] = v
	}
	return b
}

// Build constructs the Deployment.
func (b *DeploymentBuilder) Build() (*appsv1.Deployment, error) {
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
		Name:         b.name,
		Image:        b.image,
		Ports:        b.ports,
		Command:      b.command,
		Args:         b.args,
		Env:          b.env,
		EnvFrom:      b.envFrom,
		VolumeMounts: volumeMounts,
		Resources:    b.resources,
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

	if b.securityContext != nil {
		podTemplate.Spec.SecurityContext = b.securityContext
	}

	// Build Deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        b.name,
			Namespace:   b.namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: b.replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: podTemplate,
		},
	}

	return deployment, nil
}

// buildLabels constructs the label map.
func (b *DeploymentBuilder) buildLabels() map[string]string {
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
func (b *DeploymentBuilder) buildAnnotations() map[string]string {
	annotations := make(map[string]string)

	// Merge user annotations
	for k, v := range b.annotations {
		annotations[k] = v
	}

	return annotations
}

// buildVolumes constructs volumes from SDK-managed resources.
func (b *DeploymentBuilder) buildVolumes() []corev1.Volume {
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
		volumes = append(volumes, corev1.Volume{
			Name: sec.SecretName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: sec.SecretName,
				},
			},
		})
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
func (b *DeploymentBuilder) buildVolumeMounts() []corev1.VolumeMount {
	mounts := make([]corev1.VolumeMount, 0, len(b.volumeMounts))

	// Add user-provided mounts
	mounts = append(mounts, b.volumeMounts...)

	// Add default mounts for certificates
	for i, cert := range b.certificates {
		volumeName := fmt.Sprintf("cert-%d", i)
		if cert.SecretName != "" {
			volumeName = cert.SecretName
		}

		mounts = append(mounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: fmt.Sprintf("/etc/certs/%s", volumeName),
			ReadOnly:  true,
		})
	}

	return mounts
}
