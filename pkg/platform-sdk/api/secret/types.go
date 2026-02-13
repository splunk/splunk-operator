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

// Package secret provides types for secret resolution.
package secret

// Binding describes what secret you need.
//
// The SDK validates that a Kubernetes secret exists with the required keys.
// The SDK does NOT create ExternalSecret CRs - that's the admin's job via kubectl/GitOps.
//
// Example (generic secret):
//
//	secret, err := rctx.ResolveSecret(secret.Binding{
//	    Name: "postgres-credentials",
//	    Type: secret.SecretTypeGeneric,
//	    Keys: []string{"username", "password"},
//	})
//
// Example (Splunk secret with versioning):
//
//	secret, err := rctx.ResolveSecret(secret.Binding{
//	    Name: "splunk-standalone",
//	    Type: secret.SecretTypeSplunk,
//	    Keys: []string{
//	        "password",
//	        "hec_token",
//	        "pass4SymmKey",
//	    },
//	})
type Binding struct {
	// Name of the Kubernetes secret.
	// Required.
	Name string

	// Namespace where the secret is located.
	// If empty, defaults to the namespace from ReconcileContext.
	Namespace string

	// Type of secret (affects validation and versioning behavior).
	// Required.
	Type SecretType

	// Keys that must exist in the secret.
	// Required. Must have at least one key.
	//
	// The SDK validates that all keys are present in the secret.Data.
	Keys []string

	// Service is the service type for pattern-based naming (CSI).
	// Examples: "standalone", "clustermanager", "searchhead"
	Service string

	// Instance is the instance name for pattern-based naming (CSI).
	// Examples: "my-splunk", "cm", "sh1"
	Instance string

	// CSI configuration for CSI-based secret mounting.
	// If set, uses CSI driver instead of Kubernetes Secret objects.
	CSI *CSIConfig

	// OwnerName is the name of the resource that owns this secret (for cleanup).
	// Automatically set by ReconcileContext.
	OwnerName string

	// CRSpec is the secret spec from the CR (for hierarchy resolution).
	// Automatically set by ReconcileContext if the CR has a secret spec.
	CRSpec *Spec
}

// CSIConfig specifies CSI-based secret mounting configuration.
type CSIConfig struct {
	// Provider is the CSI provider to use.
	// Values: "vault", "aws", "azure", "gcp"
	Provider string

	// ProviderClass is the name of the SecretProviderClass CRD.
	// If not specified, SDK constructs from pattern.
	ProviderClass string

	// MountPath is where to mount secrets in the pod.
	// Default: /mnt/secrets/<name>
	MountPath string

	// SyncToKubernetesSecret creates a K8s Secret for compatibility.
	// Default: false (secrets only mounted via CSI)
	SyncToKubernetesSecret bool
}

// SecretType indicates what kind of secret this is.
type SecretType string

const (
	// SecretTypeGeneric is a generic key-value secret.
	// Use this for database credentials, API tokens, etc.
	//
	// The SDK validates the secret exists and has the required keys.
	// No versioning is applied.
	SecretTypeGeneric SecretType = "generic"

	// SecretTypeSplunk is a Splunk-specific secret with versioning support.
	// Use this for Splunk secrets (admin password, HEC token, cluster keys, etc.).
	//
	// The SDK:
	// - Creates versioned secrets (splunk-standalone-secret-v1, v2, v3)
	// - Manages rotation when the source secret changes
	// - Keeps last 3 versions for rollback
	// - Includes default.yml with TLS and token configuration
	SecretTypeSplunk SecretType = "splunk"

	// SecretTypeTLS is a TLS secret (same as kubernetes.io/tls).
	// Use this when you need to reference an existing TLS secret.
	//
	// The SDK validates the secret exists and has: tls.crt, tls.key, ca.crt
	SecretTypeTLS SecretType = "tls"

	// SecretTypeOpaque is an opaque secret (same as Opaque type).
	// Use this when you need to reference an existing opaque secret.
	SecretTypeOpaque SecretType = "opaque"
)

// Spec is the secret specification from a CR.
// Feature controllers can include this in their CRs to allow users
// to override secret settings.
type Spec struct {
	// SecretRef references an existing Kubernetes secret.
	SecretRef *SecretRef

	// Generate indicates whether to generate a fallback secret if not found.
	// Only applicable for development environments.
	Generate bool
}

// SecretRef references a Kubernetes secret.
type SecretRef struct {
	// Name of the secret.
	Name string

	// Namespace of the secret (optional, defaults to resource namespace).
	Namespace string
}

// Ref is what you get back from ResolveSecret - a reference to the secret.
//
// The Ref tells you:
// - Where the secret is (SecretName, Namespace)
// - Whether it's ready to use (Ready)
// - What keys it contains (Keys)
// - How it was created (Provider: "external-secrets", "kubernetes", or "generated")
// - Version (for Splunk secrets)
// - Any error if it's not ready (Error)
//
// Example usage:
//
//	secret, err := rctx.ResolveSecret(binding)
//	if err != nil {
//	    return ctrl.Result{}, err
//	}
//
//	if !secret.Ready {
//	    log.Info("Secret not ready", "error", secret.Error)
//	    return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
//	}
//
//	// Use secret.SecretName in your StatefulSet
//	sts := rctx.BuildStatefulSet().
//	    WithSecret(secret).
//	    Build()
type Ref struct {
	// SecretName is the Kubernetes secret name.
	SecretName string

	// Namespace where the secret is located.
	Namespace string

	// Keys that exist in the secret.
	// This is the intersection of requested keys and actual keys in the secret.
	Keys []string

	// Ready indicates if the secret exists and has all required keys.
	//
	// Ready=true means the secret exists and contains all requested keys.
	// Ready=false means:
	// - The secret doesn't exist (admin needs to create ExternalSecret or manual secret)
	// - The secret exists but is missing required keys
	// - ExternalSecret exists but ESO hasn't synced yet
	//
	// When Ready=false, requeue after 10 seconds to check again.
	Ready bool

	// Provider indicates where this secret came from.
	// - "external-secrets": Synced by External Secrets Operator from Vault/AWS/Azure/GCP
	// - "kubernetes": Created manually by admin via kubectl
	// - "generated": Generated by SDK as fallback (development only)
	Provider string

	// Version for Splunk secrets (e.g., v1, v2, v3).
	// Only set for SecretTypeSplunk.
	Version *int

	// Error contains any error message if Ready is false.
	// Examples:
	// - "secret not found"
	// - "missing required keys: password"
	// - "ExternalSecret status not synced"
	Error string

	// SourceSecretName is the source secret for versioned Splunk secrets.
	// Example: "splunk-default-secret" is the source for "splunk-standalone-secret-v1"
	// Only set for SecretTypeSplunk.
	SourceSecretName string

	// CSI contains CSI mounting information if using CSI secrets.
	// Only set when Provider starts with "csi-".
	CSI *CSIInfo
}

// CSIInfo provides CSI mounting information for secrets.
type CSIInfo struct {
	// ProviderClass is the name of the SecretProviderClass CRD.
	ProviderClass string

	// Driver is the CSI driver name.
	// Usually: "secrets-store.csi.k8s.io"
	Driver string

	// MountPath is where secrets will be mounted in the pod.
	// Example: /mnt/secrets/my-app-secrets
	MountPath string

	// Files are the expected files that will be available after mounting.
	// These correspond to the Keys from the Binding.
	// Example: ["username", "password"]
	Files []string
}

// HasKey returns true if the secret has the specified key.
func (r *Ref) HasKey(key string) bool {
	for _, k := range r.Keys {
		if k == key {
			return true
		}
	}
	return false
}

// HasAllKeys returns true if the secret has all the specified keys.
func (r *Ref) HasAllKeys(keys []string) bool {
	for _, key := range keys {
		if !r.HasKey(key) {
			return false
		}
	}
	return true
}
