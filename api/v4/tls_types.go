/*
Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TLS and TrustBundle types shared by all CRDs.

type TLSProvider string
type TLSConditionType string

const (
	TLSProviderSecret TLSProvider = "Secret"
	TLSProviderCSI    TLSProvider = "CSI"
)

const (
	TLSReady            TLSConditionType = "TLSReady"
	TLSRotatePending    TLSConditionType = "TLSRotatePending"
	TLSTrustBundleReady TLSConditionType = "TLSTrustBundleReady"
	TLSTrackingLimited  TLSConditionType = "TLSTrackingLimited" // CSI without a trackable Secret
)

type TLSSecretRef struct {
	// Secret with keys: tls.crt, tls.key, ca.crt (optional)
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

type TLSCSI struct {
	// cert-manager CSI driver attributes
	// +kubebuilder:validation:Enum=Issuer;ClusterIssuer
	IssuerRefKind string `json:"issuerRefKind"`
	// +kubebuilder:validation:MinLength=1
	IssuerRefName string `json:"issuerRefName"`
	// DNS SANs for the leaf
	// +optional
	DNSNames []string `json:"dnsNames,omitempty"`
	// Durations parsed by cert-manager (example "2160h")
	// +optional
	Duration string `json:"duration,omitempty"`
	// +optional
	RenewBefore string `json:"renewBefore,omitempty"`
	// +kubebuilder:validation:Enum=rsa;ecdsa
	// +optional
	KeyAlgorithm string `json:"keyAlgorithm,omitempty"`
	// +optional
	KeySize int `json:"keySize,omitempty"`
}

type TrustBundle struct {
	// Optional trust-manager bundle Secret containing the CA bundle
	// +optional
	SecretName string `json:"secretName,omitempty"`
	// +optional
	Key string `json:"key,omitempty"` // default: "ca-bundle.crt"
}

// TLSConfig controls how certs/keys/CA are presented to Splunk.
// All content is copied/symlinked into CanonicalDir under $SPLUNK_HOME.
type TLSConfig struct {
	// +kubebuilder:validation:Enum=Secret;CSI
	Provider TLSProvider `json:"provider"`
	// +optional
	SecretRef *TLSSecretRef `json:"secretRef,omitempty"`
	// +optional
	CSI *TLSCSI `json:"csi,omitempty"`
	// Canonical destination inside $SPLUNK_HOME, defaults to /opt/splunk/etc/auth/tls
	// +optional
	CanonicalDir string `json:"canonicalDir,omitempty"`
	// Optional Trust Bundle mounted as a Secret produced by trust-manager
	// +optional
	TrustBundle *TrustBundle `json:"trustBundle,omitempty"`

	// KVEncryptedKey enables building a separate PEM for KV store
    // that contains an AES-256 encrypted private key, and writes
    // [kvstore] sslPassword + serverCert in server.conf accordingly.
    // Defaults to disabled for simplicity and reliability.
    KVEncryptedKey *KVEncryptedKeySpec `json:"kvEncryptedKey,omitempty"`
}

type KVEncryptedKeySpec struct {
    // Enabled toggles the feature on or off. Default: false.
    Enabled bool `json:"enabled"`

    // PasswordSecretRef, if set, provides the passphrase (utf-8, no newline)
    // to encrypt the key. If omitted, we will auto-generate a random
    // base64 passphrase at runtime inside the pod.
    PasswordSecretRef *corev1.SecretKeySelector `json:"passwordSecretRef,omitempty"`

    // BundleFile is the filename (under canonicalDir) for the KV bundle PEM.
    // Default: "kvstore.pem"
    BundleFile string `json:"bundleFile,omitempty"`
}

type TLSCondition struct {
	Type               TLSConditionType       `json:"type"`
	Status             corev1.ConditionStatus `json:"status"`
	Reason             string                 `json:"reason,omitempty"`
	Message            string                 `json:"message,omitempty"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime,omitempty"`
}

type TLSObserved struct {
	// Where we sourced material from - e.g. "Secret/<name>" or "CSI:Issuer/Name"
	Source string `json:"source,omitempty"`
	// Hash over cert material we observe
	// - for Secret: sha256 over tls.crt, tls.key, ca.crt if present
	// - for CSI: sha256 over CSI attributes (best-effort)
	Hash string `json:"hash,omitempty"`

	// Leaf certificate facts if we can parse tls.crt
	LeafSHA256   string       `json:"leafSHA256,omitempty"`
	NotBefore    *metav1.Time `json:"notBefore,omitempty"`
	NotAfter     *metav1.Time `json:"notAfter,omitempty"`
	SerialNumber string       `json:"serialNumber,omitempty"`

	// Secret resourceVersion if provider=Secret
	SecretResourceVersion string `json:"secretResourceVersion,omitempty"`
}

// +kubebuilder:object:generate=true
type TLSStatus struct {
	Provider            TLSProvider    `json:"provider,omitempty"`
	CanonicalDir        string         `json:"canonicalDir,omitempty"`
	Observed            TLSObserved    `json:"observed,omitempty"`
	TrustBundleHash     string         `json:"trustBundleHash,omitempty"`
	PreTasksHash        string         `json:"preTasksHash,omitempty"`        // NEW
	PodTemplateChecksum string         `json:"podTemplateChecksum,omitempty"` // NEW
	Conditions          []TLSCondition `json:"conditions,omitempty"`
	LastObserved        metav1.Time    `json:"lastObserved,omitempty"`
}
