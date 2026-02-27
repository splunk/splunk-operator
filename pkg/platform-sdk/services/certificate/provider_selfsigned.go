// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//  http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package certificate

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"

	"github.com/go-logr/logr"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/certificate"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SelfSignedProvider implements self-signed certificate generation.
//
// The provider:
// - Generates RSA key pairs
// - Creates self-signed X.509 certificates
// - Stores certificates in Kubernetes secrets
// - Returns Ready=true immediately (no external dependency)
type SelfSignedProvider struct {
	client client.Client
	logger logr.Logger
}

// NewSelfSignedProvider creates a new self-signed certificate provider.
func NewSelfSignedProvider(client client.Client, logger logr.Logger) *SelfSignedProvider {
	return &SelfSignedProvider{
		client: client,
		logger: logger.WithName("selfsigned-provider"),
	}
}

// Name returns the provider name.
func (p *SelfSignedProvider) Name() string {
	return "self-signed"
}

// EnsureCertificate ensures a self-signed certificate exists.
func (p *SelfSignedProvider) EnsureCertificate(ctx context.Context, req Request) (*certificate.Ref, error) {
	// Check if secret already exists
	secret := &corev1.Secret{}
	err := p.client.Get(ctx, types.NamespacedName{
		Name:      req.Name,
		Namespace: req.Namespace,
	}, secret)

	if err == nil {
		// Secret exists, validate it's a TLS secret with required keys
		if secret.Type != corev1.SecretTypeTLS {
			return nil, fmt.Errorf("existing secret %s is not a TLS secret", req.Name)
		}

		if _, hasCert := secret.Data["tls.crt"]; !hasCert {
			return nil, fmt.Errorf("existing secret %s is missing tls.crt", req.Name)
		}
		if _, hasKey := secret.Data["tls.key"]; !hasKey {
			return nil, fmt.Errorf("existing secret %s is missing tls.key", req.Name)
		}

		// Parse certificate to get expiry info
		certPEM := secret.Data["tls.crt"]
		block, _ := pem.Decode(certPEM)
		if block != nil {
			cert, err := x509.ParseCertificate(block.Bytes)
			if err == nil {
				ref := &certificate.Ref{
					SecretName: req.Name,
					Namespace:  req.Namespace,
					Ready:      true,
					Provider:   "self-signed",
					NotBefore:  &cert.NotBefore,
					NotAfter:   &cert.NotAfter,
				}

				// Calculate renewal time (30 days before expiry by default)
				renewDuration := time.Duration(req.RenewBefore) * time.Second
				renewalTime := cert.NotAfter.Add(-renewDuration)
				ref.RenewalTime = &renewalTime

				p.logger.V(1).Info("Using existing self-signed certificate",
					"name", req.Name,
					"namespace", req.Namespace,
					"notAfter", cert.NotAfter,
				)

				return ref, nil
			}
		}

		// Fall through to return basic ref if we couldn't parse
		p.logger.V(1).Info("Using existing certificate (couldn't parse for details)",
			"name", req.Name,
			"namespace", req.Namespace,
		)

		return &certificate.Ref{
			SecretName: req.Name,
			Namespace:  req.Namespace,
			Ready:      true,
			Provider:   "self-signed",
		}, nil
	}

	if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to get secret: %w", err)
	}

	// Secret doesn't exist, generate new certificate
	return p.generateCertificate(ctx, req)
}

// generateCertificate generates a new self-signed certificate.
func (p *SelfSignedProvider) generateCertificate(ctx context.Context, req Request) (*certificate.Ref, error) {
	p.logger.Info("Generating self-signed certificate",
		"name", req.Name,
		"namespace", req.Namespace,
		"dnsNames", req.DNSNames,
	)

	// Generate RSA key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA key: %w", err)
	}

	// Create certificate template
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(time.Duration(req.Duration) * time.Second)

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Splunk Platform SDK"},
			CommonName:   req.Name,
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              req.DNSNames,
	}

	// Add IP addresses if specified
	for _, ipStr := range req.IPAddresses {
		if ip := net.ParseIP(ipStr); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		}
	}

	// Create self-signed certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	// Encode certificate to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	// Encode private key to PEM
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	// Create Kubernetes secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: req.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by":  "platform-sdk",
				"platform.splunk.com/component": "certificate",
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": certPEM,
			"tls.key": keyPEM,
			"ca.crt":  certPEM, // Self-signed, so CA is same as cert
		},
	}

	err = p.client.Create(ctx, secret)
	if err != nil {
		return nil, fmt.Errorf("failed to create secret: %w", err)
	}

	p.logger.Info("Self-signed certificate generated successfully",
		"name", req.Name,
		"namespace", req.Namespace,
		"notBefore", notBefore,
		"notAfter", notAfter,
	)

	// Calculate renewal time
	renewDuration := time.Duration(req.RenewBefore) * time.Second
	renewalTime := notAfter.Add(-renewDuration)

	return &certificate.Ref{
		SecretName:  req.Name,
		Namespace:   req.Namespace,
		Ready:       true,
		Provider:    "self-signed",
		NotBefore:   &notBefore,
		NotAfter:    &notAfter,
		RenewalTime: &renewalTime,
	}, nil
}
