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

// Package certificate provides types for certificate resolution.
package certificate

import (
	"time"
)

// Binding describes what certificate you need.
//
// The SDK uses this to provision a TLS certificate via cert-manager
// (if available) or by generating a self-signed certificate.
//
// Example:
//
//	cert, err := rctx.ResolveCertificate(certificate.Binding{
//	    Name: "postgres-tls",
//	    DNSNames: []string{
//	        "postgres.default.svc",
//	        "postgres.default.svc.cluster.local",
//	        "*.postgres.default.svc.cluster.local",
//	    },
//	    Duration: ptrDuration(90 * 24 * time.Hour),
//	})
type Binding struct {
	// Name for the certificate (becomes the Certificate CR name and Secret name).
	// Required.
	Name string

	// Namespace where the certificate should be created.
	// If empty, defaults to the namespace from ReconcileContext.
	Namespace string

	// DNSNames to include in the certificate.
	// Required. Must have at least one DNS name.
	//
	// Example for a service:
	// - "my-service.my-namespace.svc"
	// - "my-service.my-namespace.svc.cluster.local"
	//
	// Example for a StatefulSet (include wildcard for pods):
	// - "*.my-service.my-namespace.svc.cluster.local"
	DNSNames []string

	// IPAddresses to include in the certificate (optional).
	// Use this if you need the certificate to be valid for specific IP addresses.
	IPAddresses []string

	// Duration before the certificate expires (optional).
	// If not specified, uses the default from PlatformConfig (typically 90 days).
	Duration *time.Duration

	// RenewBefore specifies how long before expiry to renew (optional).
	// If not specified, uses the default from PlatformConfig (typically 30 days).
	RenewBefore *time.Duration

	// Usages specifies the key usages for the certificate (optional).
	// If not specified, defaults to: digital signature, key encipherment, server auth
	Usages []string

	// IssuerRef overrides the certificate issuer from PlatformConfig (optional).
	// Only used if you want a specific issuer for this certificate.
	IssuerRef *IssuerRef

	// OwnerName is the name of the resource that owns this certificate (for cleanup).
	// Automatically set by ReconcileContext.
	OwnerName string

	// CRSpec is the certificate spec from the CR (for hierarchy resolution).
	// Automatically set by ReconcileContext if the CR has a certificate spec.
	CRSpec *Spec
}

// IssuerRef references a cert-manager Issuer or ClusterIssuer.
type IssuerRef struct {
	// Name of the Issuer or ClusterIssuer.
	Name string

	// Kind is either "Issuer" or "ClusterIssuer".
	// Defaults to "ClusterIssuer".
	Kind string

	// Group is the API group (defaults to "cert-manager.io").
	Group string
}

// Spec is the certificate specification from a CR.
// Feature controllers can include this in their CRs to allow users
// to override certificate settings.
type Spec struct {
	// IssuerRef specifies which cert-manager issuer to use.
	IssuerRef *IssuerRef

	// Duration for the certificate.
	Duration *time.Duration

	// RenewBefore specifies when to renew.
	RenewBefore *time.Duration

	// Usages for the certificate.
	Usages []string
}

// Ref is what you get back from ResolveCertificate - a reference to the certificate.
//
// The Ref tells you:
// - Where the certificate secret is (SecretName, Namespace)
// - Whether it's ready to use (Ready)
// - How it was provisioned (Provider: "cert-manager" or "self-signed")
// - Any error if it's not ready (Error)
//
// Example usage:
//
//	cert, err := rctx.ResolveCertificate(binding)
//	if err != nil {
//	    return ctrl.Result{}, err
//	}
//
//	if !cert.Ready {
//	    log.Info("Certificate not ready", "error", cert.Error)
//	    return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
//	}
//
//	// Use cert.SecretName in your StatefulSet
//	sts := rctx.BuildStatefulSet().
//	    WithCertificate(cert).
//	    Build()
type Ref struct {
	// SecretName is the Kubernetes secret containing the certificate.
	// The secret has type kubernetes.io/tls and contains:
	// - tls.crt: The certificate
	// - tls.key: The private key
	// - ca.crt: The CA certificate
	SecretName string

	// Namespace where the secret is located.
	Namespace string

	// Ready indicates if the certificate is issued and ready to use.
	//
	// Ready=true means the secret exists and contains a valid certificate.
	// Ready=false means the certificate is being issued (cert-manager is working on it)
	// or there's an error (check the Error field).
	//
	// When Ready=false, requeue after 10 seconds to check again.
	Ready bool

	// Provider indicates how this certificate was provisioned.
	// - "cert-manager": Provisioned by cert-manager
	// - "self-signed": Generated as a self-signed certificate
	Provider string

	// Error contains any error message if Ready is false.
	// Examples:
	// - "Certificate is being issued by cert-manager"
	// - "cert-manager Issuer 'enterprise-ca' not found"
	// - "Certificate failed issuance: rate limit exceeded"
	Error string

	// NotBefore is when the certificate becomes valid.
	NotBefore *time.Time

	// NotAfter is when the certificate expires.
	NotAfter *time.Time

	// RenewalTime is when the certificate will be renewed.
	RenewalTime *time.Time

	// MountPath is where the certificate should be mounted in the container.
	// If not specified, defaults to /etc/certs/<SecretName>
	//
	// For cert-manager compatibility, common paths are:
	// - /mnt/tls - for TLS certificates (tls.crt, tls.key, ca.crt)
	// - /mnt/ca-bundles - for CA bundle certificates
	MountPath string
}

// IsExpired returns true if the certificate has expired.
func (r *Ref) IsExpired() bool {
	if r.NotAfter == nil {
		return false
	}
	return time.Now().After(*r.NotAfter)
}

// IsNearExpiry returns true if the certificate is within the renewal window.
func (r *Ref) IsNearExpiry() bool {
	if r.RenewalTime == nil {
		return false
	}
	return time.Now().After(*r.RenewalTime)
}
