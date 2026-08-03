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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ValidateCertSecret fetches the named secret and checks it contains the
// required keys tls.crt and tls.key. ca.crt is optional (absent for ACME issuers).
// Returns the secret on success, a not-found error if missing, or a descriptive
// error if a required key is absent.
func ValidateCertSecret(ctx context.Context, c client.Client, namespace, secretName string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: secretName}, secret); err != nil {
		return nil, err
	}
	for _, key := range []string{CertTLSCRTKey, CertTLSKeyKey} {
		if _, ok := secret.Data[key]; !ok {
			certErr := &ErrCertSecretMalformed{Namespace: namespace, SecretName: secretName, MissingKey: key}
			return nil, splcommon.NewTerminalError(EventReasonCertSecretMalformed, certErr.Error(), certErr)
		}
	}
	return secret, nil
}
