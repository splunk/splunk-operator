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

package kvservice

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/stretchr/testify/require"
)

// Generate real key pairs so serving-certificate and rotation tests exercise
// the same PEM contract as a user-provided Secret.
func servingCertificateData(t *testing.T, name, namespace string, modify ...func(*x509.Certificate)) map[string][]byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)
	template := &x509.Certificate{SerialNumber: serial,
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames: []string{splcommon.GetSplunkServiceName(splcommon.SplunkKVService, name, false) + "." + namespace}}
	for _, apply := range modify {
		apply(template)
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	privateKey, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return map[string][]byte{"tls.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), "tls.key": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey})}
}
