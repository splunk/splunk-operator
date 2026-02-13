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

// Package certificate implements certificate resolution with multiple providers.
package certificate

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/certificate"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/services/config"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/services/interfaces"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Resolver implements certificate resolution with provider selection.
type Resolver struct {
	client         client.Client
	configResolver interfaces.ConfigResolver
	config         *api.RuntimeConfig
	logger         logr.Logger

	// Providers
	certManagerProvider Provider
	selfSignedProvider  Provider

	// Provider detection
	hasCertManager bool
}

// Provider is the interface for certificate providers.
type Provider interface {
	EnsureCertificate(ctx context.Context, req Request) (*certificate.Ref, error)
	Name() string
}

// Request contains the details for certificate provisioning.
type Request struct {
	Name        string
	Namespace   string
	DNSNames    []string
	IPAddresses []string
	Duration    int64
	RenewBefore int64
	Usages      []string
	IssuerRef   *config.IssuerRef
}

// NewResolver creates a new certificate resolver.
func NewResolver(
	client client.Client,
	configResolver interfaces.ConfigResolver,
	hasCertManager bool,
	cfg *api.RuntimeConfig,
	logger logr.Logger,
) *Resolver {
	r := &Resolver{
		client:         client,
		configResolver: configResolver,
		config:         cfg,
		logger:         logger.WithName("certificate-resolver"),
		hasCertManager: hasCertManager,
	}

	r.certManagerProvider = NewCertManagerProvider(client, logger)
	r.selfSignedProvider = NewSelfSignedProvider(client, logger)

	return r
}

// Resolve resolves a certificate for a resource.
func (r *Resolver) Resolve(ctx context.Context, binding certificate.Binding) (*certificate.Ref, error) {
	if binding.Name == "" {
		return nil, fmt.Errorf("certificate name is required")
	}
	if len(binding.DNSNames) == 0 {
		return nil, fmt.Errorf("at least one DNS name is required")
	}

	certConfig, err := r.configResolver.ResolveCertificateConfig(ctx, binding.Namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve certificate configuration: %w", err)
	}

	provider, err := r.selectProvider(certConfig)
	if err != nil {
		return nil, err
	}

	req := Request{
		Name:        binding.Name,
		Namespace:   binding.Namespace,
		DNSNames:    binding.DNSNames,
		IPAddresses: binding.IPAddresses,
		Duration:    certConfig.Duration,
		RenewBefore: certConfig.RenewBefore,
		Usages:      certConfig.Usages,
		IssuerRef:   certConfig.IssuerRef,
	}

	if binding.Duration != nil {
		req.Duration = int64(binding.Duration.Seconds())
	}
	if binding.RenewBefore != nil {
		req.RenewBefore = int64(binding.RenewBefore.Seconds())
	}

	return provider.EnsureCertificate(ctx, req)
}

// selectProvider selects the appropriate certificate provider.
func (r *Resolver) selectProvider(certConfig *config.ResolvedCertConfig) (Provider, error) {
	switch certConfig.Provider {
	case "cert-manager":
		if !r.hasCertManager {
			return r.selfSignedProvider, nil
		}
		if certConfig.IssuerRef == nil {
			return nil, fmt.Errorf("cert-manager provider requires issuerRef configuration")
		}
		return r.certManagerProvider, nil
	case "self-signed":
		return r.selfSignedProvider, nil
	default:
		return r.selfSignedProvider, nil
	}
}
