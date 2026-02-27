/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v4

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PlatformConfigSpec defines the desired state of PlatformConfig.
type PlatformConfigSpec struct {
	// Secrets configuration for secret management.
	// +optional
	Secrets SecretConfig `json:"secrets,omitempty"`

	// Certificates configuration for TLS certificate management.
	// +optional
	Certificates CertificatesConfig `json:"certificates,omitempty"`
}

// SecretConfig configures secret management.
type SecretConfig struct {
	// Provider specifies the secret provider to use.
	// Valid values: "kubernetes", "external-secrets", "csi"
	// Default: "kubernetes"
	// +optional
	// +kubebuilder:validation:Enum=kubernetes;external-secrets;csi
	Provider string `json:"provider,omitempty"`

	// GenerateFallback indicates whether to generate fallback secrets.
	// Only for development environments.
	// Default: false
	// +optional
	GenerateFallback bool `json:"generateFallback,omitempty"`

	// VersioningEnabled enables versioned secrets for Splunk resources.
	// Default: true
	// +optional
	VersioningEnabled bool `json:"versioningEnabled,omitempty"`

	// VersionsToKeep is how many versions to keep.
	// Default: 3
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	VersionsToKeep int `json:"versionsToKeep,omitempty"`

	// CSI configures CSI-based secret management.
	// Only used when Provider is "csi".
	// +optional
	CSI *CSISecretConfig `json:"csi,omitempty"`
}

// CertificatesConfig configures TLS certificate management.
type CertificatesConfig struct {
	// Provider specifies the certificate provider to use.
	// Valid values: "user-provided", "cert-manager"
	// Default: "cert-manager"
	// +optional
	// +kubebuilder:validation:Enum=user-provided;cert-manager
	Provider string `json:"provider,omitempty"`

	// Naming configures certificate naming pattern.
	// Used to construct certificate secret names for each service instance.
	// +optional
	Naming *CertificateNamingConfig `json:"naming,omitempty"`

	// UserProvided configures user-provided certificates.
	// Only used when Provider is "user-provided".
	// +optional
	UserProvided *UserProvidedCertificates `json:"userProvided,omitempty"`

	// CertManager configures cert-manager integration.
	// Only used when Provider is "cert-manager".
	// +optional
	CertManager *CertManagerConfig `json:"certManager,omitempty"`
}

// CertificateNamingConfig configures certificate naming pattern.
type CertificateNamingConfig struct {
	// Pattern is the naming pattern for certificate secrets.
	// Supports variable substitution:
	// - ${namespace}: Kubernetes namespace
	// - ${service}: Service type (e.g., "standalone", "clustermanager")
	// - ${instance}: Instance name (e.g., "my-splunk")
	// Example: "${service}-${instance}-tls" -> "standalone-my-splunk-tls"
	// Default: "${service}-${instance}-tls"
	// +optional
	// +kubebuilder:default="${service}-${instance}-tls"
	Pattern string `json:"pattern,omitempty"`
}

// UserProvidedCertificates specifies user-provided certificate configuration.
type UserProvidedCertificates struct {
	// Pattern overrides the global naming pattern for user-provided certificates.
	// If not specified, uses the pattern from CertificatesConfig.Naming.
	// +optional
	Pattern string `json:"pattern,omitempty"`

	// Namespace is the namespace where the certificate secrets are located.
	// If not specified, uses the same namespace as the resource.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// CertManagerConfig configures cert-manager integration.
type CertManagerConfig struct {
	// Enabled indicates whether cert-manager integration is enabled.
	// Default: true
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// IssuerName is the name of the cert-manager Issuer or ClusterIssuer.
	// +optional
	IssuerName string `json:"issuerName,omitempty"`

	// IssuerKind is the kind of issuer (Issuer or ClusterIssuer).
	// Default: "ClusterIssuer"
	// +optional
	// +kubebuilder:validation:Enum=Issuer;ClusterIssuer
	IssuerKind string `json:"issuerKind,omitempty"`
}

// CSISecretConfig configures CSI-based secret management.
type CSISecretConfig struct {
	// Driver is the CSI driver to use.
	// Default: "secrets-store.csi.k8s.io"
	// +optional
	// +kubebuilder:default="secrets-store.csi.k8s.io"
	Driver string `json:"driver,omitempty"`

	// DefaultProvider is the default CSI provider.
	// Valid values: "vault", "aws", "azure", "gcp"
	// +optional
	// +kubebuilder:validation:Enum=vault;aws;azure;gcp
	DefaultProvider string `json:"defaultProvider,omitempty"`

	// Naming configures SecretProviderClass naming pattern.
	// +optional
	Naming *SecretNamingConfig `json:"naming,omitempty"`

	// MountPath is the default mount path for CSI secrets.
	// Default: "/mnt/secrets"
	// +optional
	// +kubebuilder:default="/mnt/secrets"
	MountPath string `json:"mountPath,omitempty"`

	// Vault configures Vault-specific settings.
	// Only used when DefaultProvider is "vault".
	// +optional
	Vault *VaultConfig `json:"vault,omitempty"`

	// AWS configures AWS Secrets Manager settings.
	// Only used when DefaultProvider is "aws".
	// +optional
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
	// +optional
	// +kubebuilder:default="${service}-${instance}-secrets"
	Pattern string `json:"pattern,omitempty"`
}

// VaultConfig configures Vault CSI provider.
type VaultConfig struct {
	// Address is the Vault server address.
	// Example: "https://vault.company.com"
	// +optional
	Address string `json:"address,omitempty"`

	// Role is the Vault role to use for authentication.
	// +optional
	Role string `json:"role,omitempty"`
}

// AWSSecretsConfig configures AWS Secrets Manager CSI provider.
type AWSSecretsConfig struct {
	// Region is the AWS region.
	// Example: "us-west-2"
	// +optional
	Region string `json:"region,omitempty"`
}

// PlatformConfigStatus defines the observed state of PlatformConfig.
type PlatformConfigStatus struct {
	// Conditions represent the latest available observations of the config's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:storageversion

// PlatformConfig is the Schema for the platformconfigs API.
type PlatformConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PlatformConfigSpec   `json:"spec,omitempty"`
	Status PlatformConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PlatformConfigList contains a list of PlatformConfig.
type PlatformConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PlatformConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PlatformConfig{}, &PlatformConfigList{})
}
