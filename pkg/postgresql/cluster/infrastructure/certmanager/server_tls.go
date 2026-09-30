/*
Copyright 2026.

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

// Package certmanager adapts cert-manager to the PostgreSQL server TLS port.
package certmanager

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"slices"
	"strings"

	cmapi "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	tlsport "github.com/splunk/splunk-operator/pkg/postgresql/cluster/ports/tls"
	certclient "github.com/splunk/splunk-operator/pkg/splunk/client/certmanager"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	retainedPoolerSANsAnnotation = "enterprise.splunk.com/retained-pooler-sans"
)

// Manager implements the server TLS port using cert-manager Certificate objects.
type Manager struct {
	client client.Client
}

func NewManager(c client.Client) *Manager { return &Manager{client: c} }

func (m *Manager) Reconcile(ctx context.Context, request tlsport.Request) error {
	identity := request.Identity
	if identity.ClusterName == "" || identity.Namespace == "" || identity.CertificateName == "" || identity.ServerTLSSecret == "" || identity.ServerCASecret == "" || request.IssuerRef.Name == "" {
		return fmt.Errorf("%w: resolved TLS request is incomplete", tlsport.ErrConfiguration)
	}
	labels := identity.Labels()
	if err := m.ensureIssuerIsSupported(ctx, identity.Namespace, request.IssuerRef); err != nil {
		return fmt.Errorf("%w: %v", tlsport.ErrConfiguration, err)
	}
	options := []certclient.CertOption{
		certclient.WithIssuerRef(certclient.IssuerRef{Name: request.IssuerRef.Name, Kind: request.IssuerRef.Kind, Group: request.IssuerRef.Group}),
		certclient.WithDNSNames(request.DNSNames),
		certclient.WithUsages(keyUsages(request.Usages)),
		certclient.WithOwnerReference(identity.OwnerReference),
		certclient.WithLabels(labels),
		certclient.WithSecretLabels(labels),
		certclient.WithExistingCertificateGuard(func(certificate *cmapi.Certificate) error {
			if !hasLabels(certificate.Labels, labels) || !controlledByIdentity(certificate, identity) {
				return fmt.Errorf("Certificate %s/%s is not managed by this PostgresCluster", identity.Namespace, identity.CertificateName)
			}
			return nil
		}),
	}
	if len(request.RetainedPoolerDNSNames) > 0 {
		options = append(options, certclient.WithAnnotations(map[string]string{retainedPoolerSANsAnnotation: strings.Join(request.RetainedPoolerDNSNames, ",")}))
	}
	if request.Duration != nil {
		options = append(options, certclient.WithDuration(*request.Duration))
	}
	if request.RenewBefore != nil {
		options = append(options, certclient.WithRenewBefore(*request.RenewBefore))
	}
	if err := certclient.EnsureCertificate(ctx, m.client, identity.ServerTLSSecret, identity.Namespace, options...); err != nil {
		switch {
		case errors.Is(err, certclient.ErrCertManagerNotInstalled):
			return tlsport.ErrCertManagerNotInstalled
		case errors.Is(err, certclient.ErrCertificateNotReady), errors.Is(err, certclient.ErrIssuerNotReady), errors.Is(err, certclient.ErrIssuerNotFound):
			return fmt.Errorf("%w: %v", tlsport.ErrCertificatePending, err)
		default:
			return fmt.Errorf("%w: %v", tlsport.ErrConfiguration, err)
		}
	}

	leaf := &corev1.Secret{}
	if err := m.client.Get(ctx, client.ObjectKey{Namespace: identity.Namespace, Name: identity.ServerTLSSecret}, leaf); err != nil {
		return fmt.Errorf("%w: leaf secret is unavailable", tlsport.ErrCertificatePending)
	}
	if leaf.Type != corev1.SecretTypeTLS || len(leaf.Data[corev1.TLSCertKey]) == 0 || len(leaf.Data[corev1.TLSPrivateKeyKey]) == 0 || len(leaf.Data["ca.crt"]) == 0 {
		return fmt.Errorf("%w: leaf Secret must be tls type with tls.crt, tls.key, and ca.crt", tlsport.ErrInvalidMaterial)
	}
	if err := validateDNSNames(leaf.Data[corev1.TLSCertKey], request.DNSNames); err != nil {
		return fmt.Errorf("%w: %v", tlsport.ErrInvalidMaterial, err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(leaf.Data["ca.crt"]) {
		return fmt.Errorf("%w: ca.crt is not PEM certificate data", tlsport.ErrInvalidMaterial)
	}
	ca := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: identity.ServerCASecret, Namespace: identity.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, m.client, ca, func() error {
		if ca.GetResourceVersion() != "" && !hasLabels(ca.Labels, labels) {
			return fmt.Errorf("Secret %s/%s is not managed by this PostgresCluster", identity.Namespace, identity.ServerCASecret)
		}
		ca.Type = corev1.SecretTypeOpaque
		ca.Data = map[string][]byte{"ca.crt": leaf.Data["ca.crt"]}
		ca.Labels = labels
		return nil
	}); err != nil {
		return fmt.Errorf("%w: copying server CA Secret: %v", tlsport.ErrConfiguration, err)
	}
	return nil
}

func (m *Manager) ExistingCertificateDNSNames(ctx context.Context, identity tlsport.Identity) ([]string, error) {
	certificate := &cmapi.Certificate{}
	err := m.client.Get(ctx, client.ObjectKey{Namespace: identity.Namespace, Name: identity.CertificateName}, certificate)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		if apimeta.IsNoMatchError(err) {
			return nil, tlsport.ErrCertManagerNotInstalled
		}
		return nil, fmt.Errorf("getting existing server Certificate: %w", err)
	}
	return append([]string(nil), certificate.Spec.DNSNames...), nil
}

func (m *Manager) Finalize(ctx context.Context, request tlsport.FinalizeRequest) error {
	if request.Retain {
		return m.retain(ctx, request.Identity)
	}
	if !request.DeleteCASecret {
		return nil
	}
	return m.cleanup(ctx, request.Identity)
}

func (m *Manager) cleanup(ctx context.Context, identity tlsport.Identity) error {
	secret := &corev1.Secret{}
	if err := m.client.Get(ctx, client.ObjectKey{Name: identity.ServerCASecret, Namespace: identity.Namespace}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("getting copied server CA Secret: %w", err)
	}
	if !hasLabels(secret.Labels, identity.Labels()) {
		return nil
	}
	if err := m.client.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting copied server CA Secret: %w", err)
	}
	return nil
}

// Retain removes the PostgresCluster owner reference from a verified managed
// Certificate. This is required when retaining the CNPG Cluster: cert-manager
// must continue renewing the leaf Secret still referenced by that survivor.
func (m *Manager) retain(ctx context.Context, identity tlsport.Identity) error {
	certificate := &cmapi.Certificate{}
	if err := m.client.Get(ctx, client.ObjectKey{Namespace: identity.Namespace, Name: identity.CertificateName}, certificate); err != nil {
		if apierrors.IsNotFound(err) || apimeta.IsNoMatchError(err) {
			return nil
		}
		return fmt.Errorf("getting server Certificate: %w", err)
	}
	if !hasLabels(certificate.Labels, identity.Labels()) {
		return fmt.Errorf("Certificate %s/%s is not managed by this PostgresCluster", identity.Namespace, certificate.Name)
	}
	controller := metav1.GetControllerOf(certificate)
	if controller == nil {
		return nil
	}
	if !controlledByIdentity(certificate, identity) {
		return fmt.Errorf("Certificate %s/%s is not managed by this PostgresCluster", identity.Namespace, certificate.Name)
	}
	owners := certificate.GetOwnerReferences()
	retained := make([]metav1.OwnerReference, 0, len(owners)-1)
	for _, owner := range owners {
		if owner.UID == identity.ClusterUID && owner.Kind == "PostgresCluster" && owner.APIVersion == "platform.splunk.com/v1alpha1" {
			continue
		}
		retained = append(retained, owner)
	}
	if len(retained) == len(owners) {
		return nil
	}
	certificate.SetOwnerReferences(retained)
	if err := m.client.Update(ctx, certificate); err != nil {
		return fmt.Errorf("orphaning server Certificate: %w", err)
	}
	return nil
}

func keyUsages(usages []string) []cmapi.KeyUsage {
	result := make([]cmapi.KeyUsage, 0, len(usages))
	for _, usage := range usages {
		result = append(result, cmapi.KeyUsage(usage))
	}
	return result
}

func (m *Manager) ensureIssuerIsSupported(ctx context.Context, namespace string, issuerRef tlsport.IssuerReference) error {
	if issuerRef.Group != "" && issuerRef.Group != "cert-manager.io" {
		return fmt.Errorf("issuer group %q is not supported", issuerRef.Group)
	}
	if issuerRef.Kind == "ClusterIssuer" {
		issuer := &cmapi.ClusterIssuer{}
		if err := m.client.Get(ctx, client.ObjectKey{Name: issuerRef.Name}, issuer); err != nil {
			return fmt.Errorf("getting ClusterIssuer: %w", err)
		}
		if issuer.Spec.ACME != nil {
			return errors.New("ACME issuers are not supported for PostgreSQL server certificates")
		}
		return nil
	}
	issuer := &cmapi.Issuer{}
	if err := m.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: issuerRef.Name}, issuer); err != nil {
		return fmt.Errorf("getting Issuer: %w", err)
	}
	if issuer.Spec.ACME != nil {
		return errors.New("ACME issuers are not supported for PostgreSQL server certificates")
	}
	return nil
}

func hasLabels(actual, expected map[string]string) bool {
	for key, value := range expected {
		if actual[key] != value {
			return false
		}
	}
	return true
}

func controlledByIdentity(obj metav1.Object, identity tlsport.Identity) bool {
	owner := metav1.GetControllerOf(obj)
	return owner != nil && owner.UID == identity.ClusterUID && owner.Kind == "PostgresCluster" && owner.APIVersion == "platform.splunk.com/v1alpha1"
}

func validateDNSNames(certPEM []byte, required []string) error {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return errors.New("tls.crt is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return errors.New("tls.crt is not a certificate")
	}
	for _, dnsName := range required {
		if !slices.Contains(cert.DNSNames, dnsName) {
			return errors.New("tls.crt does not contain the required DNS names")
		}
	}
	if len(cert.ExtKeyUsage) > 0 &&
		!slices.Contains(cert.ExtKeyUsage, x509.ExtKeyUsageServerAuth) &&
		!slices.Contains(cert.ExtKeyUsage, x509.ExtKeyUsageAny) {
		return errors.New("tls.crt is not valid for server authentication")
	}
	return nil
}
