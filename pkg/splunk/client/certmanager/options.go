// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package certmanager

import (
	cmapi "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// defaultUsages are the key usages requested on an auto-generated Certificate
// when the caller does not override them via WithUsages.
var defaultUsages = []cmapi.KeyUsage{cmapi.UsageServerAuth, cmapi.UsageClientAuth}

// IssuerRef identifies a cert-manager Issuer or ClusterIssuer that must
// already exist in the cluster. Kind must be "Issuer" (the default, when
// empty) or "ClusterIssuer".
type IssuerRef struct {
	Name  string
	Kind  string
	Group string
}

// certConfig accumulates optional settings for EnsureCertificate.
type certConfig struct {
	issuerRef         *IssuerRef
	dnsNames          []string
	commonName        string
	usages            []cmapi.KeyUsage
	owner             metav1.Object
	ownerReference    *metav1.OwnerReference
	existingGuard     func(*cmapi.Certificate) error
	duration          *metav1.Duration
	renewBefore       *metav1.Duration
	rotationPolicy    cmapi.PrivateKeyRotationPolicy
	secretAnnotations map[string]string
	secretLabels      map[string]string
	labels            map[string]string
	annotations       map[string]string
}

// CertOption configures optional behavior of EnsureCertificate.
type CertOption func(*certConfig)

// WithIssuerRef selects the cert-manager Issuer or ClusterIssuer that
// EnsureCertificate must find already present in the cluster.
func WithIssuerRef(ref IssuerRef) CertOption {
	return func(c *certConfig) {
		c.issuerRef = &ref
	}
}

// WithDNSNames sets explicit DNS SANs instead of auto-derived ones.
func WithDNSNames(names []string) CertOption {
	return func(c *certConfig) {
		c.dnsNames = names
	}
}

// WithCommonName sets an explicit Subject CommonName on the generated
// certificate, overriding EnsureCertificate's defaultCommonName.
func WithCommonName(name string) CertOption {
	return func(c *certConfig) {
		c.commonName = name
	}
}

// WithUsages overrides the default [ServerAuth, ClientAuth] key usages.
func WithUsages(usages []cmapi.KeyUsage) CertOption {
	return func(c *certConfig) {
		c.usages = usages
	}
}

// WithOwner sets an ownerReference on the per-secret Certificate CR so it is
// garbage-collected when owner is deleted.
func WithOwner(owner metav1.Object) CertOption {
	return func(c *certConfig) {
		c.owner = owner
	}
}

// WithOwnerReference sets a controller owner reference without requiring the
// caller to pass its full Kubernetes object through the certificate boundary.
func WithOwnerReference(owner metav1.OwnerReference) CertOption {
	return func(c *certConfig) { c.ownerReference = &owner }
}

// WithExistingCertificateGuard verifies that an existing Certificate is safe
// to update. The guard runs inside CreateOrUpdate's mutation callback, after
// its read and before any Certificate fields are changed.
func WithExistingCertificateGuard(guard func(*cmapi.Certificate) error) CertOption {
	return func(c *certConfig) { c.existingGuard = guard }
}

// WithDuration sets the requested validity period of the generated
// certificate. When unset, cert-manager applies its own default.
func WithDuration(d metav1.Duration) CertOption {
	return func(c *certConfig) {
		c.duration = &d
	}
}

// WithRenewBefore sets how long before expiry cert-manager should renew the
// generated certificate. When unset, cert-manager applies its own default.
func WithRenewBefore(d metav1.Duration) CertOption {
	return func(c *certConfig) {
		c.renewBefore = &d
	}
}

// WithRotationPolicy sets the private key rotation policy ("Never" or
// "Always") applied on renewal. When unset, cert-manager applies its own
// default.
func WithRotationPolicy(policy cmapi.PrivateKeyRotationPolicy) CertOption {
	return func(c *certConfig) {
		c.rotationPolicy = policy
	}
}

// WithSecretAnnotations stamps the given annotations onto the Secret
// cert-manager creates/renews for this Certificate, via
// CertificateSpec.SecretTemplate. Unlike WithOwner (which sets an
// ownerReference on the Certificate CR, not the Secret), this is the only
// documented hook cert-manager gives callers to attach metadata to the
// Secret itself, since cert-manager — not the caller — creates and
// continually reconciles that Secret.
func WithSecretAnnotations(annotations map[string]string) CertOption {
	return func(c *certConfig) {
		c.secretAnnotations = annotations
	}
}

// WithSecretLabels stamps labels onto the Secret cert-manager creates and
// renews through CertificateSpec.SecretTemplate.
func WithSecretLabels(labels map[string]string) CertOption {
	return func(c *certConfig) { c.secretLabels = labels }
}

// WithLabels stamps labels on the Certificate resource itself.
func WithLabels(labels map[string]string) CertOption {
	return func(c *certConfig) { c.labels = labels }
}

// WithAnnotations stamps annotations on the Certificate resource itself.
func WithAnnotations(annotations map[string]string) CertOption {
	return func(c *certConfig) { c.annotations = annotations }
}
