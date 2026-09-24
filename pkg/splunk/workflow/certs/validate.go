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

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/splunk/splunk-operator/pkg/logging"
)

// ValidateCertSecret fetches the named secret and checks it has one of two
// valid shapes:
//   - tls.crt and tls.key, with ca.crt optional (absent for ACME issuers).
//   - ca.crt only, with tls.crt and tls.key both absent. This shape is for
//     mounting a CA cert to trust an externally-managed TLS endpoint (e.g. a
//     postgres server's CA) without SOK managing a client cert/key for it.
//
// Returns the secret on success, a not-found error if missing, or a
// descriptive error if neither valid shape is present.
func ValidateCertSecret(ctx context.Context, c client.Client, namespace, secretName string) (*corev1.Secret, error) {
	logger := logging.FromContext(ctx).With("func", "ValidateCertSecret", "secret", secretName, "namespace", namespace)
	secret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: secretName}, secret); err != nil {
		if !k8serrors.IsNotFound(err) {
			logger.ErrorContext(ctx, "failed to fetch cert secret", "error", err)
		}
		return nil, err
	}

	_, hasCrt := secret.Data[CertTLSCRTKey]
	_, hasKey := secret.Data[CertTLSKeyKey]
	_, hasCA := secret.Data[CertCAKey]

	if hasCA && !hasCrt && !hasKey {
		return secret, nil
	}

	for _, key := range []string{CertTLSCRTKey, CertTLSKeyKey} {
		if _, ok := secret.Data[key]; !ok {
			certErr := &ErrCertSecretMalformed{Namespace: namespace, SecretName: secretName, MissingKey: key}
			return nil, splcommon.NewTerminalError(EventReasonCertSecretMalformed, certErr.Error(), certErr)
		}
	}
	return secret, nil
}
