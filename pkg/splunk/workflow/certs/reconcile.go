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
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
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
}

// ReconcileCerts processes all certs for a CR and returns a CertMountConfig
// describing volumes, mounts, and rotation annotations to inject into the pod template.
//
// Phase 1 — mount-only: no auto-generation. Missing secrets are skipped with a
// warning log. Auto-generation via cert-manager is Phase 2.
//
// Sources (processed in order; user-declared wins on duplicate secretName):
//  1. Operator-driven: secret names from CertificateRequester.Certificates() (if cr
//     implements the interface). Always mounted as-is at /mnt/tls/<secretName>/.
//  2. User-declared: entries from certsGetter.GetCertEntries(). When role is set,
//     mounted at the fixed Ansible path /mnt/tls/splunk-<role>-tls-cert/.
//     When role is unset, mounted as-is at /mnt/tls/<secretName>/.
//
// Returns nil, nil in two distinct cases:
//   - No certs configured (normal): neither spec.certs[] nor CertificateRequester
//     return any entries. The caller should proceed without injecting any mounts.
//   - All secrets missing (transient): certs are declared but none of the referenced
//     Secrets exist yet. Each missing secret is logged as a warning. This is treated
//     as a transient state rather than an error because Phase 2 will auto-generate
//     missing secrets; the reconcile will retry once the secrets appear.
func ReconcileCerts(ctx context.Context, c client.Client, cr client.Object, userEntries []CertEntry) (*CertMountConfig, error) {
	logger := log.FromContext(ctx)
	ns := cr.GetNamespace()
	config := &CertMountConfig{
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
					logger.Info("operator-driven cert secret not found, skipping mount", "secret", secretName)
					continue
				}
				return nil, fmt.Errorf("reconciling operator-driven cert %s: %w", secretName, err)
			}
			addCertMount(config, secretName, asIsMountPath(secretName), certHash(secret))
			seen[secretName] = true
		}
	}

	// --- Source 2: user-declared ---
	// Missing secret → warning + skip (Phase 2 will auto-generate).
	// User-declared replaces any operator-driven entry for the same secretName.
	for _, entry := range userEntries {
		secret, err := ValidateCertSecret(ctx, c, ns, entry.SecretName)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logger.Info("user-declared cert secret not found, skipping mount (Phase 2 will auto-generate)", "secret", entry.SecretName)
				if seen[entry.SecretName] {
					removeCertMount(config, entry.SecretName)
					delete(seen, entry.SecretName)
				}
				continue
			}
			return nil, fmt.Errorf("reconciling user-declared cert %s: %w", entry.SecretName, err)
		}

		mountPath := asIsMountPath(entry.SecretName)
		if entry.Role != "" {
			mountPath = fmt.Sprintf(RoleMountFmt, entry.Role)
		}

		if seen[entry.SecretName] {
			removeCertMount(config, entry.SecretName)
		}
		addCertMount(config, entry.SecretName, mountPath, certHash(secret))
		seen[entry.SecretName] = true
	}

	if len(config.Volumes) == 0 {
		return nil, nil
	}
	return config, nil
}

// addCertMount appends one volume, one volumeMount, and one annotation for the given secret.
func addCertMount(config *CertMountConfig, secretName, mountPath, hash string) {
	volName := volumeName(secretName)
	// DefaultMode must be set explicitly to match what Kubernetes stores after creation
	// (corev1.SecretVolumeSourceDefaultMode = 0644 = 420). Without it the operator's
	// MergePodUpdates sees a perpetual diff and keeps updating the StatefulSet.
	defaultMode := corev1.SecretVolumeSourceDefaultMode
	config.Volumes = append(config.Volumes, corev1.Volume{
		Name: volName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  secretName,
				DefaultMode: &defaultMode,
			},
		},
	})
	config.VolumeMounts = append(config.VolumeMounts, corev1.VolumeMount{
		Name:      volName,
		MountPath: mountPath,
		ReadOnly:  true,
	})
	config.Annotations[certRevAnnotKey(secretName)] = hash
}

// removeCertMount removes the volume, volumeMount, and annotation for secretName.
func removeCertMount(config *CertMountConfig, secretName string) {
	volName := volumeName(secretName)
	annotKey := certRevAnnotKey(secretName)

	filtered := config.Volumes[:0]
	for _, v := range config.Volumes {
		if v.Name != volName {
			filtered = append(filtered, v)
		}
	}
	config.Volumes = filtered

	filteredMounts := config.VolumeMounts[:0]
	for _, m := range config.VolumeMounts {
		if m.Name != volName {
			filteredMounts = append(filteredMounts, m)
		}
	}
	config.VolumeMounts = filteredMounts

	delete(config.Annotations, annotKey)
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
