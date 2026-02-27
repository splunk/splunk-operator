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

// Package config implements configuration resolution with hierarchical merging.
package config

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	platformv4 "github.com/splunk/splunk-operator/api/platform/v4"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/config"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Resolver implements ConfigResolver interface.
//
// The resolver reads PlatformConfig and TenantConfig CRs and implements
// hierarchical merge logic:
//
//	CR spec > TenantConfig > PlatformConfig > Built-in defaults
//
// Configuration is cached per namespace to avoid repeated API calls.
// Watches detect changes and invalidate caches automatically.
type Resolver struct {
	client client.Client
	config *api.RuntimeConfig
	logger logr.Logger

	// Cache for resolved configurations per namespace
	cache      map[string]*cachedConfig
	cacheMu    sync.RWMutex
	cacheValid bool

	// Watches
	watcherStarted bool
	watcherMu      sync.Mutex
}

// cachedConfig holds resolved configuration for a namespace.
type cachedConfig struct {
	platformConfig *config.PlatformConfig
	tenantConfig   *config.TenantConfig
}

// NewResolver creates a new ConfigResolver.
func NewResolver(client client.Client, cfg *api.RuntimeConfig, logger logr.Logger) *Resolver {
	return &Resolver{
		client:     client,
		config:     cfg,
		logger:     logger.WithName("config-resolver"),
		cache:      make(map[string]*cachedConfig),
		cacheValid: false,
	}
}

// StartWatches starts watching for configuration changes.
func (r *Resolver) StartWatches(ctx context.Context) error {
	r.watcherMu.Lock()
	defer r.watcherMu.Unlock()

	if r.watcherStarted {
		return nil
	}

	r.logger.Info("Starting configuration watches")

	// TODO: Implement actual watches using controller-runtime
	// For now, we'll just mark as started and rely on periodic reconciliation
	// to pick up changes. In a full implementation, we'd use:
	// - Watch PlatformConfig CRs
	// - Watch TenantConfig CRs in relevant namespaces
	// - Invalidate cache on changes

	r.watcherStarted = true
	r.logger.Info("Configuration watches started")

	return nil
}

// ResolveConfig reads configuration with proper hierarchy.
func (r *Resolver) ResolveConfig(ctx context.Context, key string, namespace string) (interface{}, error) {
	// Get cached or fetch configuration
	_, err := r.getConfig(ctx, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get configuration: %w", err)
	}

	// Parse the key and return the appropriate value
	// For now, returning nil as we'll implement specific resolve methods
	return nil, fmt.Errorf("generic config resolution not yet implemented, use specific methods")
}

// GetPlatformConfig retrieves the PlatformConfig for a namespace.
func (r *Resolver) GetPlatformConfig(ctx context.Context, namespace string) (*config.PlatformConfig, error) {
	cfg, err := r.getConfig(ctx, namespace)
	if err != nil {
		return nil, err
	}
	return cfg.platformConfig, nil
}

// GetTenantConfig retrieves the TenantConfig for a namespace.
func (r *Resolver) GetTenantConfig(ctx context.Context, namespace string) (*config.TenantConfig, error) {
	if !r.config.ClusterScoped {
		// In namespace-scoped mode, only return config for our namespace
		if namespace != r.config.Namespace {
			return nil, fmt.Errorf("namespace %s not accessible in namespace-scoped mode", namespace)
		}
	}

	cfg, err := r.getConfig(ctx, namespace)
	if err != nil {
		return nil, err
	}
	return cfg.tenantConfig, nil
}

// ResolveCertificateConfig resolves certificate configuration with hierarchy.
func (r *Resolver) ResolveCertificateConfig(ctx context.Context, namespace string) (*ResolvedCertConfig, error) {
	cfg, err := r.getConfig(ctx, namespace)
	if err != nil {
		return nil, err
	}

	resolved := &ResolvedCertConfig{
		// Layer 1: Built-in defaults
		Provider:    "self-signed",
		Duration:    90 * 24 * 3600, // 90 days in seconds
		RenewBefore: 30 * 24 * 3600, // 30 days in seconds
		Usages:      []string{"digital signature", "key encipherment", "server auth"},
	}

	// Layer 2: PlatformConfig (cluster-wide)
	if cfg.platformConfig != nil {
		if cfg.platformConfig.Spec.Certificates.Provider != "" {
			resolved.Provider = cfg.platformConfig.Spec.Certificates.Provider
		}
		if cfg.platformConfig.Spec.Certificates.IssuerRef != nil {
			resolved.IssuerRef = &IssuerRef{
				Name:  cfg.platformConfig.Spec.Certificates.IssuerRef.Name,
				Kind:  cfg.platformConfig.Spec.Certificates.IssuerRef.Kind,
				Group: cfg.platformConfig.Spec.Certificates.IssuerRef.Group,
			}
		}
		if cfg.platformConfig.Spec.Certificates.Duration != nil {
			resolved.Duration = int64(cfg.platformConfig.Spec.Certificates.GetCertificateDuration().Seconds())
		}
		if cfg.platformConfig.Spec.Certificates.RenewBefore != nil {
			resolved.RenewBefore = int64(cfg.platformConfig.Spec.Certificates.GetCertificateRenewBefore().Seconds())
		}
		if len(cfg.platformConfig.Spec.Certificates.Usages) > 0 {
			resolved.Usages = cfg.platformConfig.Spec.Certificates.Usages
		}
	}

	// Layer 3: TenantConfig (namespace-specific, cluster-scoped mode only)
	if r.config.ClusterScoped && cfg.tenantConfig != nil {
		if cfg.tenantConfig.Spec.Certificates.Provider != "" {
			resolved.Provider = cfg.tenantConfig.Spec.Certificates.Provider
		}
		if cfg.tenantConfig.Spec.Certificates.IssuerRef != nil {
			resolved.IssuerRef = &IssuerRef{
				Name:  cfg.tenantConfig.Spec.Certificates.IssuerRef.Name,
				Kind:  cfg.tenantConfig.Spec.Certificates.IssuerRef.Kind,
				Group: cfg.tenantConfig.Spec.Certificates.IssuerRef.Group,
			}
		}
		if cfg.tenantConfig.Spec.Certificates.Duration != nil {
			resolved.Duration = int64(cfg.tenantConfig.Spec.Certificates.GetCertificateDuration().Seconds())
		}
		if cfg.tenantConfig.Spec.Certificates.RenewBefore != nil {
			resolved.RenewBefore = int64(cfg.tenantConfig.Spec.Certificates.GetCertificateRenewBefore().Seconds())
		}
		if len(cfg.tenantConfig.Spec.Certificates.Usages) > 0 {
			resolved.Usages = cfg.tenantConfig.Spec.Certificates.Usages
		}
	}

	// Validate issuer ref for cert-manager
	if resolved.Provider == "cert-manager" && resolved.IssuerRef == nil {
		return nil, fmt.Errorf("cert-manager provider requires issuerRef in configuration")
	}

	return resolved, nil
}

// ResolveSecretConfig resolves secret configuration with hierarchy.
func (r *Resolver) ResolveSecretConfig(ctx context.Context, namespace string) (*ResolvedSecretConfig, error) {
	cfg, err := r.getConfig(ctx, namespace)
	if err != nil {
		return nil, err
	}

	resolved := &ResolvedSecretConfig{
		// Layer 1: Built-in defaults
		Provider:          "kubernetes",
		GenerateFallback:  false,
		VersioningEnabled: true,
		VersionsToKeep:    3,
		// CSI defaults
		CSIDriver:        "secrets-store.csi.k8s.io",
		CSINamingPattern: "${service}-${instance}-secrets",
		CSIMountPath:     "/mnt/secrets",
	}

	// Layer 2: PlatformConfig
	if cfg.platformConfig != nil {
		if cfg.platformConfig.Spec.Secrets.Provider != "" {
			resolved.Provider = cfg.platformConfig.Spec.Secrets.Provider
		}
		resolved.GenerateFallback = cfg.platformConfig.Spec.Secrets.GenerateFallback
		resolved.VersioningEnabled = cfg.platformConfig.Spec.Secrets.VersioningEnabled
		if cfg.platformConfig.Spec.Secrets.VersionsToKeep > 0 {
			resolved.VersionsToKeep = cfg.platformConfig.Spec.Secrets.GetVersionsToKeep()
		}

		// CSI configuration
		if cfg.platformConfig.Spec.Secrets.CSI != nil {
			csiConfig := cfg.platformConfig.Spec.Secrets.CSI

			// Driver
			if csiConfig.Driver != "" {
				resolved.CSIDriver = csiConfig.Driver
			}

			// Provider
			if csiConfig.DefaultProvider != "" {
				resolved.CSIProvider = csiConfig.DefaultProvider
			}

			// Naming pattern
			if csiConfig.Naming != nil && csiConfig.Naming.Pattern != "" {
				resolved.CSINamingPattern = csiConfig.Naming.Pattern
			}

			// Mount path
			if csiConfig.MountPath != "" {
				resolved.CSIMountPath = csiConfig.MountPath
			}

			// Vault configuration
			if csiConfig.Vault != nil {
				if csiConfig.Vault.Address != "" {
					resolved.CSIVaultAddress = csiConfig.Vault.Address
				}
				if csiConfig.Vault.Role != "" {
					resolved.CSIVaultRole = csiConfig.Vault.Role
				}
			}

			// AWS configuration
			if csiConfig.AWS != nil {
				if csiConfig.AWS.Region != "" {
					resolved.CSIAWSRegion = csiConfig.AWS.Region
				}
			}
		}
	}

	// Layer 3: TenantConfig
	if r.config.ClusterScoped && cfg.tenantConfig != nil {
		if cfg.tenantConfig.Spec.Secrets.Provider != "" {
			resolved.Provider = cfg.tenantConfig.Spec.Secrets.Provider
		}
		resolved.GenerateFallback = cfg.tenantConfig.Spec.Secrets.GenerateFallback
		resolved.VersioningEnabled = cfg.tenantConfig.Spec.Secrets.VersioningEnabled
		if cfg.tenantConfig.Spec.Secrets.VersionsToKeep > 0 {
			resolved.VersionsToKeep = cfg.tenantConfig.Spec.Secrets.GetVersionsToKeep()
		}

		// TenantConfig CSI overrides (if needed)
		if cfg.tenantConfig.Spec.Secrets.CSI != nil {
			csiConfig := cfg.tenantConfig.Spec.Secrets.CSI

			if csiConfig.Driver != "" {
				resolved.CSIDriver = csiConfig.Driver
			}
			if csiConfig.DefaultProvider != "" {
				resolved.CSIProvider = csiConfig.DefaultProvider
			}
			if csiConfig.Naming != nil && csiConfig.Naming.Pattern != "" {
				resolved.CSINamingPattern = csiConfig.Naming.Pattern
			}
			if csiConfig.MountPath != "" {
				resolved.CSIMountPath = csiConfig.MountPath
			}
		}
	}

	return resolved, nil
}

// ResolveObservabilityConfig resolves observability configuration with hierarchy.
func (r *Resolver) ResolveObservabilityConfig(ctx context.Context, namespace string) (*ResolvedObservabilityConfig, error) {
	cfg, err := r.getConfig(ctx, namespace)
	if err != nil {
		return nil, err
	}

	resolved := &ResolvedObservabilityConfig{
		// Layer 1: Built-in defaults
		Enabled:           true,
		Provider:          "opentelemetry",
		OTelCollectorMode: "daemonset",
		PrometheusPort:    9090,
		PrometheusPath:    "/metrics",
		SamplingRate:      0.1,
	}

	// Layer 2: PlatformConfig
	if cfg.platformConfig != nil {
		resolved.Enabled = cfg.platformConfig.Spec.Observability.Enabled
		if cfg.platformConfig.Spec.Observability.Provider != "" {
			resolved.Provider = cfg.platformConfig.Spec.Observability.Provider
		}
		if cfg.platformConfig.Spec.Observability.OTelCollectorMode != "" {
			resolved.OTelCollectorMode = cfg.platformConfig.Spec.Observability.OTelCollectorMode
		}
		if cfg.platformConfig.Spec.Observability.PrometheusAnnotations.Port > 0 {
			resolved.PrometheusPort = cfg.platformConfig.Spec.Observability.PrometheusAnnotations.Port
		}
		if cfg.platformConfig.Spec.Observability.PrometheusAnnotations.Path != "" {
			resolved.PrometheusPath = cfg.platformConfig.Spec.Observability.PrometheusAnnotations.Path
		}
		if cfg.platformConfig.Spec.Observability.SamplingRate > 0 {
			resolved.SamplingRate = cfg.platformConfig.Spec.Observability.SamplingRate
		}
	}

	// Layer 3: TenantConfig
	if r.config.ClusterScoped && cfg.tenantConfig != nil {
		resolved.Enabled = cfg.tenantConfig.Spec.Observability.Enabled
		if cfg.tenantConfig.Spec.Observability.Provider != "" {
			resolved.Provider = cfg.tenantConfig.Spec.Observability.Provider
		}
		if cfg.tenantConfig.Spec.Observability.OTelCollectorMode != "" {
			resolved.OTelCollectorMode = cfg.tenantConfig.Spec.Observability.OTelCollectorMode
		}
		if cfg.tenantConfig.Spec.Observability.PrometheusAnnotations.Port > 0 {
			resolved.PrometheusPort = cfg.tenantConfig.Spec.Observability.PrometheusAnnotations.Port
		}
		if cfg.tenantConfig.Spec.Observability.PrometheusAnnotations.Path != "" {
			resolved.PrometheusPath = cfg.tenantConfig.Spec.Observability.PrometheusAnnotations.Path
		}
		if cfg.tenantConfig.Spec.Observability.SamplingRate > 0 {
			resolved.SamplingRate = cfg.tenantConfig.Spec.Observability.SamplingRate
		}
	}

	return resolved, nil
}

// getConfig retrieves and caches configuration for a namespace.
func (r *Resolver) getConfig(ctx context.Context, namespace string) (*cachedConfig, error) {
	// Check cache first
	r.cacheMu.RLock()
	if r.cacheValid {
		if cfg, ok := r.cache[namespace]; ok {
			r.cacheMu.RUnlock()
			return cfg, nil
		}
	}
	r.cacheMu.RUnlock()

	// Cache miss or invalid, fetch from API
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	// Double-check after acquiring write lock
	if r.cacheValid {
		if cfg, ok := r.cache[namespace]; ok {
			return cfg, nil
		}
	}

	cfg := &cachedConfig{}

	// Fetch PlatformConfig (cluster-scoped, single instance)
	platformConfig, err := r.fetchPlatformConfig(ctx)
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to fetch PlatformConfig: %w", err)
	}
	cfg.platformConfig = platformConfig

	// Fetch TenantConfig (namespace-scoped)
	tenantConfig, err := r.fetchTenantConfig(ctx, namespace)
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to fetch TenantConfig: %w", err)
	}
	cfg.tenantConfig = tenantConfig

	// Update cache
	r.cache[namespace] = cfg
	r.cacheValid = true

	r.logger.V(1).Info("Configuration cached",
		"namespace", namespace,
		"hasPlatformConfig", cfg.platformConfig != nil,
		"hasTenantConfig", cfg.tenantConfig != nil,
	)

	return cfg, nil
}

// fetchPlatformConfig fetches the PlatformConfig from the cluster.
func (r *Resolver) fetchPlatformConfig(ctx context.Context) (*config.PlatformConfig, error) {
	// By convention, we look for a PlatformConfig named "default"
	// Fetch using the API type (platformv4.PlatformConfig)
	apiPlatformConfig := &platformv4.PlatformConfig{}
	err := r.client.Get(ctx, types.NamespacedName{
		Name: "default",
	}, apiPlatformConfig)

	if err != nil {
		if apierrors.IsNotFound(err) {
			r.logger.V(1).Info("PlatformConfig not found, using defaults")
			return nil, nil
		}
		return nil, err
	}

	// Convert from API type to internal SDK type
	platformConfig := config.FromAPIType(apiPlatformConfig)
	return platformConfig, nil
}

// fetchTenantConfig fetches the TenantConfig for a namespace.
func (r *Resolver) fetchTenantConfig(ctx context.Context, namespace string) (*config.TenantConfig, error) {
	if namespace == "" {
		return nil, nil
	}

	// By convention, we look for a TenantConfig named "default" in the namespace
	// Fetch using the API type (platformv4.TenantConfig)
	apiTenantConfig := &platformv4.TenantConfig{}
	err := r.client.Get(ctx, types.NamespacedName{
		Name:      "default",
		Namespace: namespace,
	}, apiTenantConfig)

	if err != nil {
		if apierrors.IsNotFound(err) {
			r.logger.V(1).Info("TenantConfig not found for namespace", "namespace", namespace)
			return nil, nil
		}
		return nil, err
	}

	// Convert from API type to internal SDK type
	tenantConfig := config.TenantConfigFromAPIType(apiTenantConfig)
	return tenantConfig, nil
}

// InvalidateCache invalidates the configuration cache.
// Call this when configuration changes are detected.
func (r *Resolver) InvalidateCache() {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	r.cacheValid = false
	r.cache = make(map[string]*cachedConfig)

	r.logger.Info("Configuration cache invalidated")
}

// InvalidateNamespace invalidates cache for a specific namespace.
func (r *Resolver) InvalidateNamespace(namespace string) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	delete(r.cache, namespace)

	r.logger.V(1).Info("Configuration cache invalidated for namespace", "namespace", namespace)
}
