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

package config

// ResolvedCertConfig is the resolved certificate configuration.
type ResolvedCertConfig struct {
	// Provider is the certificate provider ("cert-manager", "self-signed", or "user-provided").
	Provider string

	// NamingPattern is the pattern for certificate secret names.
	// Example: "${service}-${instance}-tls"
	NamingPattern string

	// UserProvidedNamespace is the namespace where user-provided certificates are located.
	// Only used when Provider is "user-provided".
	UserProvidedNamespace string

	// IssuerRef for cert-manager (nil if not using cert-manager).
	IssuerRef *IssuerRef

	// Duration in seconds.
	Duration int64

	// RenewBefore in seconds.
	RenewBefore int64

	// Usages for the certificate.
	Usages []string
}

// IssuerRef references a cert-manager Issuer or ClusterIssuer.
type IssuerRef struct {
	Name  string
	Kind  string
	Group string
}

// ResolvedSecretConfig is the resolved secret configuration.
type ResolvedSecretConfig struct {
	// Provider is the secret provider.
	Provider string

	// GenerateFallback indicates whether to generate fallback secrets.
	GenerateFallback bool

	// VersioningEnabled indicates if versioning is enabled for Splunk secrets.
	VersioningEnabled bool

	// VersionsToKeep is how many versions to keep.
	VersionsToKeep int

	// CSI configuration (only set when Provider is "csi").
	CSIDriver        string // CSI driver name
	CSIProvider      string // CSI provider (vault, aws, azure, gcp)
	CSINamingPattern string // Pattern for SecretProviderClass names
	CSIMountPath     string // Default mount path for CSI secrets
	CSIVaultAddress  string // Vault address (if using Vault)
	CSIVaultRole     string // Vault role (if using Vault)
	CSIAWSRegion     string // AWS region (if using AWS)
}

// ResolvedObservabilityConfig is the resolved observability configuration.
type ResolvedObservabilityConfig struct {
	// Enabled indicates if observability is enabled.
	Enabled bool

	// Provider is the observability provider.
	Provider string

	// OTelCollectorMode is the deployment mode for OTel Collector.
	OTelCollectorMode string

	// PrometheusPort for metrics.
	PrometheusPort int

	// PrometheusPath for metrics.
	PrometheusPath string

	// SamplingRate for traces.
	SamplingRate float64
}
