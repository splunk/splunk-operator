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

package certs

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	cmapi "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/logging"
	certlib "github.com/splunk/splunk-operator/pkg/splunk/client/certmanager"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

// CertEntry is a CR-agnostic description of a single cert to mount.
// Enterprise reconcilers convert their CRD-specific CertSpec into this type
// so that workflow/certs remains decoupled from CRD API packages.
type CertEntry struct {
	// SecretName is the Kubernetes Secret to mount.
	SecretName string
	// Role is the Ansible-processed role (e.g. "server", "input").
	// When non-empty the cert is mounted at the fixed Ansible path
	// /mnt/tls/splunk-<role>-tls-cert/. When empty it is mounted as-is.
	Role string
	// IssuerRef selects the cert-manager Issuer/ClusterIssuer to use when
	// auto-generating this cert. It must reference an Issuer/ClusterIssuer
	// that already exists in the cluster — required for auto-generation to
	// succeed.
	IssuerRef *certlib.IssuerRef
	// DNSNames are the DNS SANs for the generated cert. Empty means the
	// caller-supplied entry has no explicit SANs; ReconcileCerts falls back
	// to autoDNSNames.
	DNSNames []string
	// Duration is the requested validity period of the generated cert. Nil
	// means cert-manager's own default applies.
	Duration *metav1.Duration
	// RenewBefore is how long before expiry cert-manager should renew the
	// generated cert. Nil means cert-manager's own default applies.
	RenewBefore *metav1.Duration
	// RotationPolicy controls private key reuse/regeneration on renewal.
	// Empty means cert-manager's own default applies.
	RotationPolicy cmapi.PrivateKeyRotationPolicy
}

// ReconcileCerts processes all certs for a CR and returns a CertMountConfig
// describing volumes, mounts, and rotation annotations to inject into the pod template.
//
// Sources (processed in order; user-declared wins on duplicate secretName):
//  1. Operator-driven: secret names from CertificateRequester.Certificates() (if cr
//     implements the interface). Always mounted as-is at /mnt/tls/<secretName>/.
//     Mount-only — the operator never auto-generates these.
//  2. User-declared: entries from certsGetter.GetCertEntries(). When role is set,
//     mounted at the fixed Ansible path /mnt/tls/splunk-<role>-tls-cert/.
//     When role is unset, mounted as-is at /mnt/tls/<secretName>/. When the
//     referenced secret does not exist, the operator auto-generates it via
//     pkg/splunk/client/certmanager.EnsureCertificate (cert-manager), owned by
//     cr — unless the CertManagerCertGeneration feature gate is disabled, in
//     which case reconciliation fails with ErrCertGenerationDisabled instead.
//     The entry's IssuerRef must name an Issuer/ClusterIssuer that already
//     exists — the operator never creates one, and reconciliation fails
//     with an error if it is missing or unset. Reconciliation also fails with
//     an error (not a silent skip) if cert-manager itself is not installed,
//     since there is no watch to retry once it later is.
//
// Returns nil, nil in two distinct cases:
//   - No certs configured (normal): neither spec.certs[] nor CertificateRequester
//     return any entries. The caller should proceed without injecting any mounts.
//   - All secrets missing (transient): certs are declared but none of the referenced
//     Secrets exist yet — cert-manager is still issuing them. The reconcile will
//     retry once the secrets appear (a watch on cert Secrets triggers the next
//     reconcile; see watch.go).
func ReconcileCerts(ctx context.Context, c client.Client, cr client.Object, userEntries []CertEntry) (*CertMountConfig, error) {
	if !config.DefaultMutableFeatureGate.Enabled(config.CertManagement) {
		return nil, nil
	}

	ns := cr.GetNamespace()
	logger := logging.FromContext(ctx).With("func", "ReconcileCerts", "namespace", ns, "name", cr.GetName())
	mountConfig := &CertMountConfig{
		Annotations: make(map[string]string),
	}

	// seen tracks secretNames already mounted; user-declared replaces operator-driven.
	seen := make(map[string]bool)

	// --- Source 1: operator-driven (CertificateRequester) ---
	// Mount-only: missing secret → warning + skip. No auto-generation.
	if requester, ok := cr.(CertificateRequester); ok {
		for _, secretName := range requester.Certificates() {
			if seen[secretName] {
				continue
			}
			secret, err := ValidateCertSecret(ctx, c, ns, secretName)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					logger.WarnContext(ctx, "operator-driven cert secret not found, skipping mount; it may not be ready yet", "secret", secretName, "error", err)
					continue
				}
				logger.ErrorContext(ctx, "failed to validate operator-driven cert secret", "secret", secretName, "error", err)
				var te *splcommon.TerminalError
				if errors.As(err, &te) {
					return nil, err
				}
				return nil, fmt.Errorf("reconciling operator-driven cert %s: %w", secretName, err)
			}
			if err := checkSecretOwnership(cr, secret); err != nil {
				logger.ErrorContext(ctx, "operator-driven cert secret was auto-generated for a different CR", "secret", secretName, "error", err)
				return nil, err
			}
			addCertMount(mountConfig, secretName, asIsMountPath(secretName), certHash(secret))
			seen[secretName] = true
		}
	}

	// --- Source 2: user-declared ---
	// Missing secret → auto-generate via cert-manager, then requeue
	// until cert-manager populates the secret.
	// User-declared replaces any operator-driven entry for the same secretName.
	for _, entry := range userEntries {
		secret, err := ValidateCertSecret(ctx, c, ns, entry.SecretName)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				if seen[entry.SecretName] {
					removeCertMount(mountConfig, entry.SecretName)
					delete(seen, entry.SecretName)
				}

				if !config.DefaultMutableFeatureGate.Enabled(config.CertManagerCertGeneration) {
					logger.ErrorContext(ctx, "user-declared cert secret not found and cert generation is disabled", "secret", entry.SecretName)
					return nil, fmt.Errorf("auto-generating user-declared cert %s: %w", entry.SecretName, ErrCertGenerationDisabled)
				}

				opts := []certlib.CertOption{
					certlib.WithOwner(cr),
					certlib.WithSecretAnnotations(map[string]string{CertGeneratedForUIDAnnotation: string(cr.GetUID())}),
				}
				if entry.IssuerRef != nil {
					opts = append(opts, certlib.WithIssuerRef(*entry.IssuerRef))
				}
				if len(entry.DNSNames) > 0 {
					opts = append(opts, certlib.WithDNSNames(entry.DNSNames))
				}
				if entry.Duration != nil {
					opts = append(opts, certlib.WithDuration(*entry.Duration))
				}
				if entry.RenewBefore != nil {
					opts = append(opts, certlib.WithRenewBefore(*entry.RenewBefore))
				}
				if entry.RotationPolicy != "" {
					opts = append(opts, certlib.WithRotationPolicy(entry.RotationPolicy))
				}

				ensureErr := certlib.EnsureCertificate(ctx, c, entry.SecretName, ns, opts...)
				switch {
				case ensureErr == nil, errors.Is(ensureErr, certlib.ErrCertificateNotReady):
					// The Certificate was just created (or already exists) but
					// cert-manager hasn't populated its Secret yet. Surface this as
					// a reconcile error rather than creating the StatefulSet/pod
					// without the cert volume — the CertSecretMapper watch (see
					// watch.go) triggers an immediate reconcile once the Secret
					// appears, so this costs one extra reconcile cycle, not a
					// backoff-bound delay.
					logger.InfoContext(ctx, "user-declared cert generated, waiting for secret", "secret", entry.SecretName)
					return nil, fmt.Errorf("auto-generating user-declared cert %s: %w", entry.SecretName, certlib.ErrCertificateNotReady)
				case errors.Is(ensureErr, certlib.ErrCertManagerNotInstalled):
					// Surfaced as a reconcile error (not skipped) so the controller
					// keeps requeuing: there is no watch that fires when cert-manager
					// is installed later, so silently continuing here would let the CR
					// settle into Ready without the requested cert ever mounted.
					logger.ErrorContext(ctx, "user-declared cert secret not found and cert-manager is not installed", "secret", entry.SecretName, "error", ensureErr)
					return nil, fmt.Errorf("auto-generating user-declared cert %s: %w", entry.SecretName, ensureErr)
				default:
					// Includes ErrIssuerRefRequired, ErrIssuerNotFound, and
					// ErrIssuerNotReady: all are CR/cluster misconfigurations, not
					// transient states, so they surface as reconcile errors rather
					// than being swallowed into a requeue.
					logger.ErrorContext(ctx, "failed to auto-generate user-declared cert", "secret", entry.SecretName, "error", ensureErr)
					return nil, fmt.Errorf("auto-generating user-declared cert %s: %w", entry.SecretName, ensureErr)
				}
			}
			logger.ErrorContext(ctx, "failed to validate user-declared cert secret", "secret", entry.SecretName, "error", err)
			var te *splcommon.TerminalError
			if errors.As(err, &te) {
				return nil, err
			}
			return nil, fmt.Errorf("reconciling user-declared cert %s: %w", entry.SecretName, err)
		}
		if err := checkSecretOwnership(cr, secret); err != nil {
			logger.ErrorContext(ctx, "user-declared cert secret was auto-generated for a different CR", "secret", entry.SecretName, "error", err)
			return nil, err
		}

		mountPath := asIsMountPath(entry.SecretName)
		if entry.Role != "" {
			mountPath = fmt.Sprintf(RoleMountFmt, entry.Role)
		}

		if seen[entry.SecretName] {
			removeCertMount(mountConfig, entry.SecretName)
		}
		addCertMount(mountConfig, entry.SecretName, mountPath, certHash(secret))
		seen[entry.SecretName] = true
	}

	if len(mountConfig.Volumes) == 0 {
		return nil, nil
	}
	return mountConfig, nil
}

// checkSecretOwnership rejects mounting secret if it was auto-generated by
// the operator for a different CR. Auto-generated secrets carry
// CertGeneratedForUIDAnnotation set to the requesting CR's UID (stamped via
// cert-manager's SecretTemplate — see certlib.WithSecretAnnotations); its DNS
// SANs are scoped to that one CR, so mounting it into another CR would silently
// present a certificate whose SANs don't match the consuming workload.
// Customer-provided secrets never carry this annotation and are therefore
// always allowed to be shared across CRs.
func checkSecretOwnership(cr client.Object, secret *corev1.Secret) error {
	generatedFor, ok := secret.Annotations[CertGeneratedForUIDAnnotation]
	if !ok || generatedFor == string(cr.GetUID()) {
		return nil
	}
	msg := fmt.Sprintf("secret %s/%s was auto-generated for a different CR and cannot be reused", secret.Namespace, secret.Name)
	return splcommon.NewTerminalError(EventReasonCertSecretWrongOwner, msg, errors.New(msg))
}

// addCertMount appends one volume, one volumeMount, and one annotation for the given secret.
func addCertMount(mountConfig *CertMountConfig, secretName, mountPath, hash string) {
	volName := volumeName(secretName)
	// DefaultMode must be set explicitly to match what Kubernetes stores after creation
	// (corev1.SecretVolumeSourceDefaultMode = 0644 = 420). Without it the operator's
	// MergePodUpdates sees a perpetual diff and keeps updating the StatefulSet.
	defaultMode := corev1.SecretVolumeSourceDefaultMode
	mountConfig.Volumes = append(mountConfig.Volumes, corev1.Volume{
		Name: volName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  secretName,
				DefaultMode: &defaultMode,
			},
		},
	})
	mountConfig.VolumeMounts = append(mountConfig.VolumeMounts, corev1.VolumeMount{
		Name:      volName,
		MountPath: mountPath,
		ReadOnly:  true,
	})
	mountConfig.Annotations[certRevAnnotKey(secretName)] = hash
}

// removeCertMount removes the volume, volumeMount, and annotation for secretName.
func removeCertMount(mountConfig *CertMountConfig, secretName string) {
	volName := volumeName(secretName)
	annotKey := certRevAnnotKey(secretName)

	filtered := mountConfig.Volumes[:0]
	for _, v := range mountConfig.Volumes {
		if v.Name != volName {
			filtered = append(filtered, v)
		}
	}
	mountConfig.Volumes = filtered

	filteredMounts := mountConfig.VolumeMounts[:0]
	for _, m := range mountConfig.VolumeMounts {
		if m.Name != volName {
			filteredMounts = append(filteredMounts, m)
		}
	}
	mountConfig.VolumeMounts = filteredMounts

	delete(mountConfig.Annotations, annotKey)
}

// volumeName returns a Kubernetes-safe volume name derived from a Secret name.
// Kubernetes volume names must be a DNS label: lowercase alphanumeric and hyphens,
// max 63 chars. A stable 8-char hash suffix prevents collisions between names that
// normalize identically (e.g. "foo.bar" vs "foo-bar") or share the same prefix.
func volumeName(secretName string) string {
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte(secretName)))[:8]
	safe := strings.ReplaceAll(secretName, ".", "-")
	// "cert-" (5) + safe + "-" (1) + sum (8) must fit in 63 chars → safe ≤ 49.
	const maxSafe = 63 - 5 - 1 - 8
	if len(safe) > maxSafe {
		safe = safe[:maxSafe]
	}
	return "cert-" + safe + "-" + sum
}

// certRevAnnotKey returns the pod-template annotation key for cert rotation detection.
// Kubernetes annotation names (the part after the "/") are limited to 63 characters.
// The prefix "enterprise.splunk.com/cert-rev-" is 31 chars, leaving 32 for the secret
// name. For longer names a stable 8-char hash suffix is used to stay within the limit
// while remaining collision-safe.
func certRevAnnotKey(secretName string) string {
	// prefix "enterprise.splunk.com/" (22) + name part ≤ 63 → name part ≤ 63.
	// name part = "cert-rev-" (9) + secretName; max secretName = 63 - 9 = 54.
	const maxName = 63 - 9 // 54
	if len(secretName) <= maxName {
		return fmt.Sprintf(CertRevAnnotFmt, secretName)
	}
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte(secretName)))[:8]
	return fmt.Sprintf(CertRevAnnotFmt, secretName[:maxName-9]+"-"+sum)
}

func asIsMountPath(secretName string) string {
	return CertMountRoot + "/" + secretName
}

// certHash returns SHA-256(tls.crt + tls.key + ca.crt) as a hex string for rotation
// detection. ca.crt is optional (absent for ACME issuers) and skipped when missing.
func certHash(secret *corev1.Secret) string {
	h := sha256.New()
	h.Write(secret.Data[CertTLSCRTKey])
	h.Write(secret.Data[CertTLSKeyKey])
	if ca, ok := secret.Data[CertCAKey]; ok {
		h.Write(ca)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
