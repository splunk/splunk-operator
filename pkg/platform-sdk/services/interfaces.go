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

// Package services provides the service layer implementations.
package services

import (
	"github.com/go-logr/logr"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/builders"
	builderspkg "github.com/splunk/splunk-operator/pkg/platform-sdk/services/builders"
	certificatepkg "github.com/splunk/splunk-operator/pkg/platform-sdk/services/certificate"
	configpkg "github.com/splunk/splunk-operator/pkg/platform-sdk/services/config"
	discoverypkg "github.com/splunk/splunk-operator/pkg/platform-sdk/services/discovery"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/services/interfaces"
	observabilitypkg "github.com/splunk/splunk-operator/pkg/platform-sdk/services/observability"
	secretpkg "github.com/splunk/splunk-operator/pkg/platform-sdk/services/secret"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Re-export interfaces for convenience
type ConfigResolver = interfaces.ConfigResolver
type CertificateResolver = interfaces.CertificateResolver
type SecretResolver = interfaces.SecretResolver
type DiscoveryService = interfaces.DiscoveryService
type ObservabilityService = interfaces.ObservabilityService
type BackupService = interfaces.BackupService

// Factory functions for services

func NewConfigResolver(client client.Client, config *api.RuntimeConfig, logger logr.Logger) ConfigResolver {
	return configpkg.NewResolver(client, config, logger)
}

func NewCertificateResolver(client client.Client, configResolver ConfigResolver, hasCertManager bool, config *api.RuntimeConfig, logger logr.Logger) CertificateResolver {
	return certificatepkg.NewResolver(client, configResolver, hasCertManager, config, logger)
}

func NewSecretResolver(client client.Client, configResolver ConfigResolver, config *api.RuntimeConfig, logger logr.Logger) SecretResolver {
	return secretpkg.NewResolver(client, configResolver, config, logger)
}

func NewDiscoveryService(client client.Client, config *api.RuntimeConfig, logger logr.Logger) DiscoveryService {
	return discoverypkg.NewService(client, config, logger)
}

func NewObservabilityService(client client.Client, configResolver ConfigResolver, config *api.RuntimeConfig, logger logr.Logger) ObservabilityService {
	return observabilitypkg.NewService(client, configResolver, config, logger)
}

func NewBackupService(client client.Client, config *api.RuntimeConfig, logger logr.Logger) BackupService {
	return &backupService{client: client, config: config, logger: logger}
}

// Factory functions for builders

func NewStatefulSetBuilder(namespace, ownerName string, observability ObservabilityService) builders.StatefulSetBuilder {
	return builderspkg.NewStatefulSetBuilder(namespace, ownerName, observability)
}

func NewServiceBuilder(namespace, ownerName string) builders.ServiceBuilder {
	return builderspkg.NewServiceBuilder(namespace, ownerName)
}

func NewConfigMapBuilder(namespace, ownerName string) builders.ConfigMapBuilder {
	return builderspkg.NewConfigMapBuilder(namespace, ownerName)
}

func NewPodBuilder(namespace, ownerName string) builders.PodBuilder {
	return &podBuilder{namespace: namespace, ownerName: ownerName}
}

func NewDeploymentBuilder(namespace, ownerName string, observability ObservabilityService) builders.DeploymentBuilder {
	return builderspkg.NewDeploymentBuilder(namespace, ownerName, observability)
}
