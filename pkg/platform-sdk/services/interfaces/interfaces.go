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

// Package interfaces defines the service interfaces to avoid import cycles.
package interfaces

import (
	"context"

	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/certificate"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/discovery"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/secret"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/services/config"
)

// ConfigResolver reads platform and tenant configuration with proper hierarchy.
type ConfigResolver interface {
	// StartWatches starts watching for configuration changes.
	StartWatches(ctx context.Context) error

	// ResolveConfig reads configuration with proper hierarchy.
	// Hierarchy: CR spec > TenantConfig > PlatformConfig > Built-in defaults
	ResolveConfig(ctx context.Context, key string, namespace string) (interface{}, error)

	// ResolveCertificateConfig resolves certificate configuration.
	ResolveCertificateConfig(ctx context.Context, namespace string) (*config.ResolvedCertConfig, error)

	// ResolveSecretConfig resolves secret configuration.
	ResolveSecretConfig(ctx context.Context, namespace string) (*config.ResolvedSecretConfig, error)

	// ResolveObservabilityConfig resolves observability configuration.
	ResolveObservabilityConfig(ctx context.Context, namespace string) (*config.ResolvedObservabilityConfig, error)
}

// CertificateResolver orchestrates certificate provisioning.
type CertificateResolver interface {
	// Resolve resolves a certificate for a resource.
	Resolve(ctx context.Context, binding certificate.Binding) (*certificate.Ref, error)
}

// SecretResolver validates that secrets exist and manages versioning.
type SecretResolver interface {
	// Resolve validates that a secret exists.
	Resolve(ctx context.Context, binding secret.Binding) (*secret.Ref, error)
}

// DiscoveryService finds services in Kubernetes and external systems.
type DiscoveryService interface {
	// DiscoverSplunk finds Splunk instances.
	DiscoverSplunk(ctx context.Context, selector discovery.SplunkSelector) ([]discovery.SplunkEndpoint, error)

	// Discover finds generic Kubernetes services.
	Discover(ctx context.Context, selector discovery.Selector) ([]discovery.Endpoint, error)
}

// ObservabilityService adds monitoring capabilities.
type ObservabilityService interface {
	// ShouldAddObservability checks if observability should be added.
	ShouldAddObservability(ctx context.Context, namespace string) (bool, error)

	// GetObservabilityAnnotations returns the observability annotations.
	GetObservabilityAnnotations(ctx context.Context, namespace string) (map[string]string, error)
}

// BackupService manages backup and restore operations.
type BackupService interface {
	// TODO: Define backup service interface
}
