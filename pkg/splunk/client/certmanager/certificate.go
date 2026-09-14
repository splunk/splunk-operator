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
	"context"
	"errors"
	"fmt"

	cmapi "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/splunk/splunk-operator/pkg/logging"
)

//+kubebuilder:rbac:groups=cert-manager.io,resources=issuers;clusterissuers,verbs=get;list;watch
//+kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete

// ErrCertManagerNotInstalled is returned by EnsureCertificate when the
// cert-manager Certificate CRD is not registered in the cluster.
var ErrCertManagerNotInstalled = errors.New("cert-manager is not installed in this cluster")

// ErrCertificateNotReady is returned by EnsureCertificate when the
// Certificate CR exists but cert-manager has not yet populated its secret.
// Callers should emit a Warning event and requeue.
var ErrCertificateNotReady = errors.New("the certificate resource exists, but cert-manager has not yet populated the secret with the certificate data.")

// ErrIssuerRefRequired is returned by EnsureCertificate when no issuerRef was
// supplied via WithIssuerRef. The operator never bootstraps an issuer on the
// caller's behalf, so an explicit reference is mandatory.
var ErrIssuerRefRequired = errors.New("issuerRef is required to auto-generate a certificate")

// ErrIssuerNotFound is returned by EnsureCertificate when the referenced
// Issuer/ClusterIssuer does not exist in the cluster. The operator never
// creates issuers — the admin must create it before auto-generation can
// proceed.
var ErrIssuerNotFound = errors.New("referenced issuer does not exist")

// ErrIssuerNotReady is returned by EnsureCertificate when the referenced
// Issuer/ClusterIssuer exists but has not reported a Ready condition of
// True (e.g. a misconfigured ACME/CA backend). The operator never repairs
// issuer configuration — the admin must fix it before auto-generation can
// proceed.
var ErrIssuerNotReady = errors.New("referenced issuer is not ready")

// defaultCommonName is applied to every auto-generated Certificate that does
// not set an explicit CommonName via WithCommonName. It keeps the leaf's
// Subject (and, for a self-signed issuer, its Issuer DN too, since Issuer ==
// Subject when self-signed) from being entirely empty: some curl/OpenSSL
// builds abort a TLS handshake against a certificate whose issuer DN has no
// fields set at all, even though the chain and SAN match are otherwise valid.
// A single fixed value (rather than deriving one per-cert from DNSNames) is
// deliberate: TLS clients ignore CommonName whenever a SAN is present (RFC
// 6125 §6.4.4), so all it needs to do is be non-empty and safely under
// cert-manager's 64-byte CommonName limit — Splunk's own service/pod FQDNs
// routinely exceed that limit and would otherwise have to be truncated.
const defaultCommonName = "splunk-operator-generated-cert"

// certificateName derives the cert-manager Certificate CR name from the
// target secret name. Certificate and Secret share a 1:1 relationship in
// this library, so reusing the secret name keeps the mapping obvious.
func certificateName(secretName string) string {
	return secretName
}

// EnsureCertificate ensures a cert-manager Certificate CR exists that will
// populate secretName in namespace. Callers must supply an issuerRef via
// WithIssuerRef — the operator never creates or bootstraps an Issuer on the
// caller's behalf, so the referenced Issuer/ClusterIssuer must already exist
// in the cluster. Callers must also supply DNS names via WithDNSNames since
// this package has no knowledge of CR-specific service/pod naming.
//
// WithOwner sets a controller ownerReference on the per-secret Certificate CR
// (using c.Scheme() to resolve its GVK).
//
// EnsureCertificate reconciles the Certificate CR's spec on every call: if it
// does not exist it is created, and if it already exists its spec is patched
// to match the desired state (e.g. DNS SANs changing as replicas scale).
// cert-manager itself remains responsible for rotation and renewal of the
// issued certificate within that spec.
//
// Returns ErrCertManagerNotInstalled if the CRD is absent, ErrIssuerRefRequired
// if no issuerRef was supplied, ErrIssuerNotFound if the referenced
// Issuer/ClusterIssuer does not exist, or ErrCertificateNotReady if the
// Certificate CR was just created or updated (or already existed unchanged)
// but its secret has not yet been populated with the current spec.
func EnsureCertificate(ctx context.Context, c client.Client, secretName, namespace string, opts ...CertOption) error {
	logger := logging.FromContext(ctx).With("func", "EnsureCertificate", "secret", secretName, "namespace", namespace)

	installed, err := CertManagerInstalled(c.RESTMapper())
	if err != nil {
		logger.ErrorContext(ctx, "failed to probe cert-manager installation", "error", err)
		return fmt.Errorf("probing cert-manager installation: %w", err)
	}
	if !installed {
		logger.WarnContext(ctx, "cert-manager is not installed, cannot auto-generate certificate")
		return ErrCertManagerNotInstalled
	}

	cfg := &certConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.issuerRef == nil {
		logger.ErrorContext(ctx, "no issuerRef supplied for auto-generated certificate", "error", ErrIssuerRefRequired)
		return ErrIssuerRefRequired
	}
	issuerRef, err := resolveIssuerRef(ctx, c, namespace, *cfg.issuerRef)
	if err != nil {
		return err
	}

	usages := cfg.usages
	if len(usages) == 0 {
		usages = defaultUsages
	}

	commonName := cfg.commonName
	if commonName == "" {
		commonName = defaultCommonName
	}

	certName := certificateName(secretName)
	desiredSpec := cmapi.CertificateSpec{
		SecretName:  secretName,
		IssuerRef:   issuerRef,
		CommonName:  commonName,
		DNSNames:    cfg.dnsNames,
		Usages:      usages,
		Duration:    cfg.duration,
		RenewBefore: cfg.renewBefore,
	}
	if cfg.rotationPolicy != "" {
		desiredSpec.PrivateKey = &cmapi.CertificatePrivateKey{RotationPolicy: cfg.rotationPolicy}
	}
	if len(cfg.secretAnnotations) > 0 {
		desiredSpec.SecretTemplate = &cmapi.CertificateSecretTemplate{Annotations: cfg.secretAnnotations}
	}

	certObj := &cmapi.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: namespace,
		},
	}
	result, err := controllerutil.CreateOrUpdate(ctx, c, certObj, func() error {
		certObj.Spec = desiredSpec
		if cfg.owner != nil {
			if err := controllerutil.SetControllerReference(cfg.owner, certObj, c.Scheme()); err != nil {
				return fmt.Errorf("setting owner reference on certificate %s: %w", certName, err)
			}
		}
		return nil
	})
	if err != nil {
		logger.ErrorContext(ctx, "failed to reconcile certificate", "certificate", certName, "error", err)
		return fmt.Errorf("reconciling certificate %s: %w", certName, err)
	}

	switch result {
	case controllerutil.OperationResultCreated:
		logger.InfoContext(ctx, "created certificate", "certificate", certName, "issuer", issuerRef.Name, "issuerKind", issuerRef.Kind)
		return ErrCertificateNotReady
	case controllerutil.OperationResultUpdated:
		logger.InfoContext(ctx, "updated certificate spec", "certificate", certName, "issuer", issuerRef.Name, "issuerKind", issuerRef.Kind)
		return ErrCertificateNotReady
	default:
		logger.InfoContext(ctx, "certificate unchanged, checking readiness", "certificate", certName)
		return checkCertificateReady(ctx, c, certName, namespace)
	}
}

// resolveIssuerRef verifies that the Issuer or ClusterIssuer named by ref
// exists in the cluster and has a Ready condition of True, returning
// ErrIssuerNotFound or ErrIssuerNotReady otherwise. The operator never
// creates or repairs issuers on the caller's behalf — the admin owns their
// lifecycle entirely.
func resolveIssuerRef(ctx context.Context, c client.Client, namespace string, ref IssuerRef) (cmmeta.IssuerReference, error) {
	kind := ref.Kind
	if kind == "" {
		kind = cmapi.IssuerKind
	}

	var conditions []cmapi.IssuerCondition
	var err error
	switch kind {
	case cmapi.ClusterIssuerKind:
		clusterIssuer := &cmapi.ClusterIssuer{}
		err = c.Get(ctx, types.NamespacedName{Name: ref.Name}, clusterIssuer)
		conditions = clusterIssuer.Status.Conditions
	default:
		issuer := &cmapi.Issuer{}
		err = c.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: namespace}, issuer)
		conditions = issuer.Status.Conditions
	}

	logger := logging.FromContext(ctx).With("func", "resolveIssuerRef", "kind", kind, "issuer", ref.Name, "namespace", namespace)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.ErrorContext(ctx, "referenced issuer not found", "error", err)
			return cmmeta.IssuerReference{}, fmt.Errorf("%w: %s %q in namespace %q", ErrIssuerNotFound, kind, ref.Name, namespace)
		}
		logger.ErrorContext(ctx, "failed to check for issuer", "error", err)
		return cmmeta.IssuerReference{}, fmt.Errorf("checking for issuer %s %q: %w", kind, ref.Name, err)
	}

	ready := false
	for _, cond := range conditions {
		if cond.Type == cmapi.IssuerConditionReady && cond.Status == cmmeta.ConditionTrue {
			ready = true
			break
		}
	}
	if !ready {
		logger.ErrorContext(ctx, "referenced issuer is not ready")
		return cmmeta.IssuerReference{}, fmt.Errorf("%w: %s %q in namespace %q", ErrIssuerNotReady, kind, ref.Name, namespace)
	}

	return cmmeta.IssuerReference{Name: ref.Name, Kind: kind}, nil
}

// checkCertificateReady fetches the existing Certificate CR and reports
// whether cert-manager has marked it Ready (i.e. its secret is populated).
func checkCertificateReady(ctx context.Context, c client.Client, name, namespace string) error {
	logger := logging.FromContext(ctx).With("func", "checkCertificateReady", "certificate", name, "namespace", namespace)
	existing := &cmapi.Certificate{}
	if err := c.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, existing); err != nil {
		logger.ErrorContext(ctx, "failed to fetch certificate for readiness check", "error", err)
		return fmt.Errorf("fetching certificate %s: %w", name, err)
	}
	for _, cond := range existing.Status.Conditions {
		if cond.Type == cmapi.CertificateConditionReady && cond.Status == "True" {
			logger.InfoContext(ctx, "certificate is ready")
			return nil
		}
	}
	logger.InfoContext(ctx, "certificate not yet ready")
	return ErrCertificateNotReady
}

// CertManagerInstalled reports whether the cert-manager Certificate CRD is
// registered in the cluster, via the given RESTMapper. A no-match (CRD
// absent) is reported as (false, nil) so callers can warn-and-requeue
// instead of crashing; any other discovery error is returned so the caller
// can distinguish "not installed" from a transient discovery failure.
//
// Mirrors the probe pattern used for the optional barman-cloud ObjectStore
// CRD in internal/controller/platform/postgrescluster_controller.go.
func CertManagerInstalled(mapper meta.RESTMapper) (bool, error) {
	gvk := cmapi.SchemeGroupVersion.WithKind(cmapi.CertificateKind)
	_, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err == nil {
		return true, nil
	}
	if meta.IsNoMatchError(err) {
		return false, nil
	}
	return false, err
}
