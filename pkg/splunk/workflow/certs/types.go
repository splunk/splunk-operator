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
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// ErrCertSecretMalformed is returned when a cert secret exists but is missing
// a required key (tls.crt or tls.key). This is a user misconfiguration that
// will not self-heal without manual remediation.
type ErrCertSecretMalformed struct {
	Namespace  string
	SecretName string
	MissingKey string
}

func (e *ErrCertSecretMalformed) Error() string {
	return fmt.Sprintf("cert secret %s/%s is missing required key %q", e.Namespace, e.SecretName, e.MissingKey)
}

// ErrCertGenerationDisabled is returned by ReconcileCerts when a user-declared
// cert's secret does not exist but the CertManagerCertGeneration feature gate
// is disabled, so the operator will not auto-generate it via cert-manager.
var ErrCertGenerationDisabled = errors.New("certificate generation is disabled")

// CertificateRequester is an optional interface implemented by CR types whose
// controllers need to inject cert references derived from other CR fields
// (e.g. a Postgres CA secret from a databaseRef). These are mount-only —
// the operator never auto-generates them.
type CertificateRequester interface {
	// Certificates returns secret names the controller derives from other CR fields.
	// These are merged with spec.certs[] during reconciliation.
	Certificates() []string
}

// CertMountConfig carries all cert volumes, mounts, and annotations for one CR.
// Returned by ReconcileCerts and injected into the pod template via InjectCertMounts.
type CertMountConfig struct {
	Volumes      []corev1.Volume
	VolumeMounts []corev1.VolumeMount
	// Annotations maps "certRev/<secretName>" to SHA-256(tls.crt+tls.key) for rotation detection.
	Annotations map[string]string
}
