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

package services

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/builders"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/certificate"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/secret"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Service stubs (will be implemented in subsequent commits)

type certificateResolver struct {
	client         client.Client
	configResolver ConfigResolver
	config         *api.RuntimeConfig
	logger         logr.Logger
}

func (r *certificateResolver) Resolve(ctx context.Context, binding certificate.Binding) (*certificate.Ref, error) {
	return nil, fmt.Errorf("not yet implemented")
}

type secretResolver struct {
	client         client.Client
	configResolver ConfigResolver
	config         *api.RuntimeConfig
	logger         logr.Logger
}

func (r *secretResolver) Resolve(ctx context.Context, binding secret.Binding) (*secret.Ref, error) {
	return nil, fmt.Errorf("not yet implemented")
}

type backupService struct {
	client client.Client
	config *api.RuntimeConfig
	logger logr.Logger
}

// Builder stubs

type podBuilder struct {
	namespace string
	ownerName string
}

func (b *podBuilder) WithContainers(containers []corev1.Container) builders.PodBuilder { return b }
func (b *podBuilder) WithInitContainers(initContainers []corev1.Container) builders.PodBuilder {
	return b
}
func (b *podBuilder) WithVolumes(volumes []corev1.Volume) builders.PodBuilder              { return b }
func (b *podBuilder) WithServiceAccountName(serviceAccountName string) builders.PodBuilder { return b }
func (b *podBuilder) WithSecurityContext(securityContext *corev1.PodSecurityContext) builders.PodBuilder {
	return b
}
func (b *podBuilder) WithAffinity(affinity *corev1.Affinity) builders.PodBuilder          { return b }
func (b *podBuilder) WithTolerations(tolerations []corev1.Toleration) builders.PodBuilder { return b }
func (b *podBuilder) WithNodeSelector(nodeSelector map[string]string) builders.PodBuilder { return b }
func (b *podBuilder) WithLabels(labels map[string]string) builders.PodBuilder             { return b }
func (b *podBuilder) WithAnnotations(annotations map[string]string) builders.PodBuilder   { return b }
func (b *podBuilder) Build() (*corev1.PodSpec, error)                                     { return nil, fmt.Errorf("not yet implemented") }
