// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateServingCertificateExtendedKeyUsage(t *testing.T) {
	const hostname = "service.example.test"
	unknown := []asn1.ObjectIdentifier{{1, 3, 6, 1, 4, 1, 55555, 1}}
	tests := []struct {
		name      string
		known     []x509.ExtKeyUsage
		unknown   []asn1.ObjectIdentifier
		wantError bool
	}{
		{name: "unrestricted"},
		{name: "server authentication", known: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}},
		{name: "any usage", known: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}},
		{name: "client authentication only", known: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, wantError: true},
		{name: "unknown usage only", unknown: unknown, wantError: true},
		{name: "server authentication with unknown usage", known: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, unknown: unknown},
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	privateKey, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now()
			template := &x509.Certificate{
				SerialNumber: big.NewInt(1),
				NotBefore:    now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
				DNSNames: []string{hostname}, KeyUsage: x509.KeyUsageDigitalSignature,
				ExtKeyUsage: tt.known, UnknownExtKeyUsage: tt.unknown,
			}
			der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
			require.NoError(t, err)
			leaf, err := x509.ParseCertificate(der)
			require.NoError(t, err)
			require.Equal(t, tt.known, leaf.ExtKeyUsage)
			require.Equal(t, tt.unknown, leaf.UnknownExtKeyUsage)
			certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
			err = ValidateServingCertificate(certificatePEM, keyPEM, hostname)
			if tt.wantError {
				require.ErrorContains(t, err, "server authentication")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
