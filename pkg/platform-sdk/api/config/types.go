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

// Package config provides configuration types for the Platform SDK.
package config

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PlatformConfig defines cluster-wide defaults for the Platform SDK.
//
// The platform admin creates one PlatformConfig to set defaults for
// certificate issuers, secret stores, observability, etc.
//
// NOTE: This is the internal SDK type. The API type is in api/platform/v4.
// This type is used for conversion and internal SDK operations.
type PlatformConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PlatformConfigSpec   `json:"spec,omitempty"`
	Status PlatformConfigStatus `json:"status,omitempty"`
}

// PlatformConfigSpec defines the desired configuration for the platform.
type PlatformConfigSpec struct {
	// Certificates configuration for cert-manager integration.
	Certificates CertificateConfig `json:"certificates,omitempty"`

	// Secrets configuration for secret management.
	Secrets SecretConfig `json:"secrets,omitempty"`

	// Observability configuration for monitoring and tracing.
	Observability ObservabilityConfig `json:"observability,omitempty"`

	// Discovery configuration for service discovery.
	Discovery DiscoveryConfig `json:"discovery,omitempty"`

	// Backup configuration for backup and restore.
	Backup BackupConfig `json:"backup,omitempty"`
}

// PlatformConfigStatus defines the observed state of PlatformConfig.
type PlatformConfigStatus struct {
	// Conditions represent the latest available observations of the config's state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// TenantConfig defines namespace-specific overrides for the Platform SDK.
//
// Tenant admins create TenantConfigs to override PlatformConfig defaults
// for their namespace. This enables multi-tenancy with different settings
// per tenant.
//
// NOTE: This is the internal SDK type. The API type should be in api/platform/v4.
// This type is used for conversion and internal SDK operations.
type TenantConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TenantConfigSpec   `json:"spec,omitempty"`
	Status TenantConfigStatus `json:"status,omitempty"`
}

// TenantConfigSpec defines the desired configuration for a tenant.
type TenantConfigSpec struct {
	// Certificates configuration overrides.
	Certificates CertificateConfig `json:"certificates,omitempty"`

	// Secrets configuration overrides.
	Secrets SecretConfig `json:"secrets,omitempty"`

	// Observability configuration overrides.
	Observability ObservabilityConfig `json:"observability,omitempty"`

	// Discovery configuration overrides.
	Discovery DiscoveryConfig `json:"discovery,omitempty"`

	// Backup configuration overrides.
	Backup BackupConfig `json:"backup,omitempty"`
}

// TenantConfigStatus defines the observed state of TenantConfig.
type TenantConfigStatus struct {
	// Conditions represent the latest available observations of the config's state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// CertificateConfig configures certificate management.
type CertificateConfig struct {
	// Provider specifies the certificate provider to use.
	// Valid values: "cert-manager", "self-signed"
	// Default: "self-signed"
	Provider string `json:"provider,omitempty"`

	// IssuerRef references the cert-manager Issuer or ClusterIssuer.
	// Only used when Provider is "cert-manager".
	IssuerRef *IssuerRef `json:"issuerRef,omitempty"`

	// Duration is the default certificate duration.
	// Default: 2160h (90 days)
	Duration *metav1.Duration `json:"duration,omitempty"`

	// RenewBefore is when to renew before expiry.
	// Default: 720h (30 days)
	RenewBefore *metav1.Duration `json:"renewBefore,omitempty"`

	// Usages are the default key usages.
	// Default: ["digital signature", "key encipherment", "server auth"]
	Usages []string `json:"usages,omitempty"`
}

// IssuerRef references a cert-manager Issuer or ClusterIssuer.
type IssuerRef struct {
	// Name of the Issuer or ClusterIssuer.
	Name string `json:"name"`

	// Kind is "Issuer" or "ClusterIssuer".
	// Default: "ClusterIssuer"
	Kind string `json:"kind,omitempty"`

	// Group is the API group.
	// Default: "cert-manager.io"
	Group string `json:"group,omitempty"`
}

// SecretConfig configures secret management.
type SecretConfig struct {
	// Provider specifies the secret provider to use.
	// Valid values: "external-secrets", "kubernetes", "csi"
	// Default: "kubernetes"
	Provider string `json:"provider,omitempty"`

	// GenerateFallback indicates whether to generate fallback secrets.
	// Only for development environments.
	// Default: false
	GenerateFallback bool `json:"generateFallback,omitempty"`

	// VersioningEnabled enables versioned secrets for Splunk resources.
	// Default: true
	VersioningEnabled bool `json:"versioningEnabled,omitempty"`

	// VersionsToKeep is how many versions to keep.
	// Default: 3
	VersionsToKeep int `json:"versionsToKeep,omitempty"`

	// CSI configures CSI-based secret management.
	// Only used when Provider is "csi".
	CSI *CSISecretConfig `json:"csi,omitempty"`
}

// CSISecretConfig configures CSI-based secret management.
type CSISecretConfig struct {
	// Driver is the CSI driver to use.
	// Default: "secrets-store.csi.k8s.io"
	Driver string `json:"driver,omitempty"`

	// DefaultProvider is the default CSI provider.
	// Valid values: "vault", "aws", "azure", "gcp"
	DefaultProvider string `json:"defaultProvider,omitempty"`

	// Naming configures SecretProviderClass naming pattern.
	Naming *SecretNamingConfig `json:"naming,omitempty"`

	// MountPath is the default mount path for CSI secrets.
	// Default: "/mnt/secrets"
	MountPath string `json:"mountPath,omitempty"`

	// Vault configures Vault-specific settings.
	// Only used when DefaultProvider is "vault".
	Vault *VaultConfig `json:"vault,omitempty"`

	// AWS configures AWS Secrets Manager settings.
	// Only used when DefaultProvider is "aws".
	AWS *AWSSecretsConfig `json:"aws,omitempty"`
}

// SecretNamingConfig configures secret naming pattern for CSI.
type SecretNamingConfig struct {
	// Pattern is the naming pattern for SecretProviderClass resources.
	// Supports variable substitution:
	// - ${namespace}: Kubernetes namespace
	// - ${service}: Service type (e.g., "standalone", "clustermanager")
	// - ${instance}: Instance name (e.g., "my-splunk")
	// Example: "${service}-${instance}-secrets" -> "standalone-my-splunk-secrets"
	// Default: "${service}-${instance}-secrets"
	Pattern string `json:"pattern,omitempty"`
}

// VaultConfig configures Vault CSI provider.
type VaultConfig struct {
	// Address is the Vault server address.
	// Example: "https://vault.company.com"
	Address string `json:"address,omitempty"`

	// Role is the Vault role to use for authentication.
	Role string `json:"role,omitempty"`
}

// AWSSecretsConfig configures AWS Secrets Manager CSI provider.
type AWSSecretsConfig struct {
	// Region is the AWS region.
	// Example: "us-west-2"
	Region string `json:"region,omitempty"`
}

// ObservabilityConfig configures observability integration.
type ObservabilityConfig struct {
	// Enabled indicates if observability is enabled.
	// Default: true
	Enabled bool `json:"enabled,omitempty"`

	// Provider specifies the observability provider.
	// Valid values: "opentelemetry", "prometheus", "datadog"
	// Default: "opentelemetry"
	Provider string `json:"provider,omitempty"`

	// OTelCollectorMode specifies the OTel Collector deployment mode.
	// Valid values: "sidecar", "daemonset", "deployment"
	// Default: "daemonset"
	OTelCollectorMode string `json:"otelCollectorMode,omitempty"`

	// PrometheusAnnotations configures Prometheus scraping.
	PrometheusAnnotations PrometheusAnnotations `json:"prometheusAnnotations,omitempty"`

	// SamplingRate for traces (0.0 to 1.0).
	// Default: 0.1 (10%)
	// +kubebuilder:validation:Type=number
	// +kubebuilder:validation:Format=double
	SamplingRate float64 `json:"samplingRate,omitempty"`
}

// PrometheusAnnotations configures Prometheus scraping annotations.
type PrometheusAnnotations struct {
	// Scrape indicates if Prometheus should scrape this pod.
	// Default: true
	Scrape bool `json:"scrape,omitempty"`

	// Port for Prometheus metrics.
	// Default: 9090
	Port int `json:"port,omitempty"`

	// Path for Prometheus metrics.
	// Default: /metrics
	Path string `json:"path,omitempty"`
}

// DiscoveryConfig configures service discovery.
type DiscoveryConfig struct {
	// Enabled indicates if service discovery is enabled.
	// Default: true
	Enabled bool `json:"enabled,omitempty"`

	// CacheTTL is the cache TTL for discovery results.
	// Default: 30s
	CacheTTL *metav1.Duration `json:"cacheTTL,omitempty"`

	// HealthCheckEnabled indicates if health checks are enabled.
	// Default: true
	HealthCheckEnabled bool `json:"healthCheckEnabled,omitempty"`

	// HealthCheckTimeout is the timeout for health checks.
	// Default: 5s
	HealthCheckTimeout *metav1.Duration `json:"healthCheckTimeout,omitempty"`
}

// BackupConfig configures backup and restore.
type BackupConfig struct {
	// Enabled indicates if backup is enabled.
	// Default: false
	Enabled bool `json:"enabled,omitempty"`

	// Provider specifies the backup storage provider.
	// Valid values: "s3", "azure", "gcs", "nfs"
	Provider string `json:"provider,omitempty"`

	// Bucket or container name for backups.
	Bucket string `json:"bucket,omitempty"`

	// Retention policy for backups.
	Retention RetentionPolicy `json:"retention,omitempty"`
}

// RetentionPolicy defines backup retention.
type RetentionPolicy struct {
	// KeepLast is the number of most recent backups to keep.
	// Default: 5
	KeepLast int `json:"keepLast,omitempty"`

	// KeepDaily is the number of daily backups to keep.
	// Default: 7
	KeepDaily int `json:"keepDaily,omitempty"`

	// KeepWeekly is the number of weekly backups to keep.
	// Default: 4
	KeepWeekly int `json:"keepWeekly,omitempty"`

	// KeepMonthly is the number of monthly backups to keep.
	// Default: 12
	KeepMonthly int `json:"keepMonthly,omitempty"`
}

// PlatformConfigList contains a list of PlatformConfig.
// +kubebuilder:object:root=true
type PlatformConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PlatformConfig `json:"items"`
}

// TenantConfigList contains a list of TenantConfig.
// +kubebuilder:object:root=true
type TenantConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TenantConfig `json:"items"`
}

// Helper functions for default values

// GetCertificateDuration returns the certificate duration with defaults.
func (c *CertificateConfig) GetCertificateDuration() time.Duration {
	if c.Duration != nil {
		return c.Duration.Duration
	}
	return 90 * 24 * time.Hour // 90 days
}

// GetCertificateRenewBefore returns the renew before duration with defaults.
func (c *CertificateConfig) GetCertificateRenewBefore() time.Duration {
	if c.RenewBefore != nil {
		return c.RenewBefore.Duration
	}
	return 30 * 24 * time.Hour // 30 days
}

// GetVersionsToKeep returns the versions to keep with defaults.
func (s *SecretConfig) GetVersionsToKeep() int {
	if s.VersionsToKeep > 0 {
		return s.VersionsToKeep
	}
	return 3
}

// GetCacheTTL returns the cache TTL with defaults.
func (d *DiscoveryConfig) GetCacheTTL() time.Duration {
	if d.CacheTTL != nil {
		return d.CacheTTL.Duration
	}
	return 30 * time.Second
}

// GetHealthCheckTimeout returns the health check timeout with defaults.
func (d *DiscoveryConfig) GetHealthCheckTimeout() time.Duration {
	if d.HealthCheckTimeout != nil {
		return d.HealthCheckTimeout.Duration
	}
	return 5 * time.Second
}

func init() {
	SchemeBuilder.Register(&PlatformConfig{}, &PlatformConfigList{})
	SchemeBuilder.Register(&TenantConfig{}, &TenantConfigList{})
}
