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

package internal

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/builders"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/services"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// registry is the service locator that creates, caches, and provides
// access to all SDK services.
//
// The registry uses lazy initialization with sync.Once to ensure each
// service is created exactly once and cached for reuse.
//
// Services can depend on each other (e.g., CertificateResolver depends
// on ConfigResolver). The registry handles dependency injection.
type registry struct {
	client client.Client
	config *api.RuntimeConfig
	logger logr.Logger

	// Service instances (created lazily)
	configResolver     services.ConfigResolver
	configResolverOnce sync.Once

	certificateResolver     services.CertificateResolver
	certificateResolverOnce sync.Once

	secretResolver     services.SecretResolver
	secretResolverOnce sync.Once

	discoveryService     services.DiscoveryService
	discoveryServiceOnce sync.Once

	observabilityService     services.ObservabilityService
	observabilityServiceOnce sync.Once

	backupService     services.BackupService
	backupServiceOnce sync.Once

	// Provider detection results (cached)
	hasCertManager     bool
	hasCertManagerOnce sync.Once

	hasExternalSecrets     bool
	hasExternalSecretsOnce sync.Once

	hasOTelCollector     bool
	hasOTelCollectorOnce sync.Once

	// Lifecycle
	started bool
	mu      sync.RWMutex
}

// newRegistry creates a new service registry.
func newRegistry(client client.Client, config *api.RuntimeConfig, logger logr.Logger) (*registry, error) {
	return &registry{
		client: client,
		config: config,
		logger: logger.WithName("registry"),
	}, nil
}

// Start initializes the registry and starts services.
func (r *registry) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return fmt.Errorf("registry already started")
	}

	r.logger.Info("Starting service registry")

	// Detect available providers
	r.detectProviders()

	r.logger.Info("Provider detection complete",
		"certManager", r.hasCertManager,
		"externalSecrets", r.hasExternalSecrets,
		"otelCollector", r.hasOTelCollector,
	)

	// Initialize config resolver (needed by other services)
	if err := r.initConfigResolver(ctx); err != nil {
		return fmt.Errorf("failed to initialize config resolver: %w", err)
	}

	r.started = true
	r.logger.Info("Service registry started")

	return nil
}

// Stop gracefully shuts down the registry.
func (r *registry) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started {
		return nil
	}

	r.logger.Info("Stopping service registry")

	// Stop services (if they implement lifecycle methods)
	// TODO: Add service lifecycle management

	r.started = false
	r.logger.Info("Service registry stopped")

	return nil
}

// ConfigResolver returns the config resolver service.
func (r *registry) ConfigResolver() services.ConfigResolver {
	r.configResolverOnce.Do(func() {
		r.logger.V(1).Info("Creating ConfigResolver")
		r.configResolver = services.NewConfigResolver(r.client, r.config, r.logger)
	})
	return r.configResolver
}

// CertificateResolver returns the certificate resolver service.
func (r *registry) CertificateResolver() services.CertificateResolver {
	r.certificateResolverOnce.Do(func() {
		r.logger.V(1).Info("Creating CertificateResolver")
		r.certificateResolver = services.NewCertificateResolver(
			r.client,
			r.ConfigResolver(), // Dependency injection
			r.HasCertManager(), // Provider detection
			r.config,
			r.logger,
		)
	})
	return r.certificateResolver
}

// SecretResolver returns the secret resolver service.
func (r *registry) SecretResolver() services.SecretResolver {
	r.secretResolverOnce.Do(func() {
		r.logger.V(1).Info("Creating SecretResolver")
		r.secretResolver = services.NewSecretResolver(
			r.client,
			r.ConfigResolver(), // Dependency injection
			r.config,
			r.logger,
		)
	})
	return r.secretResolver
}

// DiscoveryService returns the discovery service.
func (r *registry) DiscoveryService() services.DiscoveryService {
	r.discoveryServiceOnce.Do(func() {
		r.logger.V(1).Info("Creating DiscoveryService")
		r.discoveryService = services.NewDiscoveryService(
			r.client,
			r.config,
			r.logger,
		)
	})
	return r.discoveryService
}

// ObservabilityService returns the observability service.
func (r *registry) ObservabilityService() services.ObservabilityService {
	r.observabilityServiceOnce.Do(func() {
		r.logger.V(1).Info("Creating ObservabilityService")
		r.observabilityService = services.NewObservabilityService(
			r.client,
			r.ConfigResolver(), // Dependency injection
			r.config,
			r.logger,
		)
	})
	return r.observabilityService
}

// BackupService returns the backup service.
func (r *registry) BackupService() services.BackupService {
	r.backupServiceOnce.Do(func() {
		r.logger.V(1).Info("Creating BackupService")
		r.backupService = services.NewBackupService(
			r.client,
			r.config,
			r.logger,
		)
	})
	return r.backupService
}

// Builder factories

func (r *registry) StatefulSetBuilder(namespace, ownerName string) builders.StatefulSetBuilder {
	return services.NewStatefulSetBuilder(namespace, ownerName, r.ObservabilityService())
}

func (r *registry) ServiceBuilder(namespace, ownerName string) builders.ServiceBuilder {
	return services.NewServiceBuilder(namespace, ownerName)
}

func (r *registry) ConfigMapBuilder(namespace, ownerName string) builders.ConfigMapBuilder {
	return services.NewConfigMapBuilder(namespace, ownerName)
}

func (r *registry) PodBuilder(namespace, ownerName string) builders.PodBuilder {
	return services.NewPodBuilder(namespace, ownerName)
}

func (r *registry) DeploymentBuilder(namespace, ownerName string) builders.DeploymentBuilder {
	return services.NewDeploymentBuilder(namespace, ownerName, r.ObservabilityService())
}

// Provider detection

func (r *registry) HasCertManager() bool {
	r.hasCertManagerOnce.Do(func() {
		r.hasCertManager = r.detectCRD("cert-manager.io", "Certificate")
	})
	return r.hasCertManager
}

func (r *registry) HasExternalSecrets() bool {
	r.hasExternalSecretsOnce.Do(func() {
		r.hasExternalSecrets = r.detectCRD("external-secrets.io", "ExternalSecret")
	})
	return r.hasExternalSecrets
}

func (r *registry) HasOTelCollector() bool {
	r.hasOTelCollectorOnce.Do(func() {
		r.hasOTelCollector = r.detectCRD("opentelemetry.io", "OpenTelemetryCollector")
	})
	return r.hasOTelCollector
}

// detectProviders runs provider detection for all providers.
func (r *registry) detectProviders() {
	// Force detection by calling the methods
	_ = r.HasCertManager()
	_ = r.HasExternalSecrets()
	_ = r.HasOTelCollector()
}

// detectCRD checks if a CRD exists in the cluster.
func (r *registry) detectCRD(group, kind string) bool {
	_, err := r.client.RESTMapper().RESTMapping(
		schema.GroupKind{Group: group, Kind: kind},
	)
	return err == nil
}

// initConfigResolver initializes the config resolver on startup.
func (r *registry) initConfigResolver(ctx context.Context) error {
	resolver := r.ConfigResolver()
	if resolver == nil {
		return fmt.Errorf("config resolver not available")
	}

	// Start watches for configuration changes
	if err := resolver.StartWatches(ctx); err != nil {
		return fmt.Errorf("failed to start config watches: %w", err)
	}

	return nil
}
