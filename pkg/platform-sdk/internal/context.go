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

package internal

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/builders"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/certificate"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/discovery"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/secret"
	"k8s.io/client-go/tools/record"
)

// reconcileContext implements the api.ReconcileContext interface.
//
// ReconcileContext is a lightweight wrapper that:
// - Knows about the specific resource being reconciled (namespace, name)
// - Provides a logger with resource context
// - Delegates to the registry's services for actual work
// - Provides a clean, convenient API for controllers
type reconcileContext struct {
	ctx       context.Context
	namespace string
	name      string
	runtime   *runtime
	registry  *registry
	logger    logr.Logger
}

// Context information

func (rc *reconcileContext) Namespace() string {
	return rc.namespace
}

func (rc *reconcileContext) Name() string {
	return rc.name
}

func (rc *reconcileContext) Context() context.Context {
	return rc.ctx
}

func (rc *reconcileContext) Logger() logr.Logger {
	return rc.logger
}

func (rc *reconcileContext) EventRecorder() record.EventRecorder {
	return rc.runtime.GetEventRecorder()
}

// Certificate resolution

func (rc *reconcileContext) ResolveCertificate(binding certificate.Binding) (*certificate.Ref, error) {
	// Fill in context if not provided
	if binding.Namespace == "" {
		binding.Namespace = rc.namespace
	}
	if binding.OwnerName == "" {
		binding.OwnerName = rc.name
	}

	// Get the certificate resolver from the registry
	resolver := rc.registry.CertificateResolver()
	if resolver == nil {
		return nil, fmt.Errorf("certificate resolver not available")
	}

	// Delegate to the resolver
	return resolver.Resolve(rc.ctx, binding)
}

// Secret resolution

func (rc *reconcileContext) ResolveSecret(binding secret.Binding) (*secret.Ref, error) {
	// Fill in context if not provided
	if binding.Namespace == "" {
		binding.Namespace = rc.namespace
	}
	if binding.OwnerName == "" {
		binding.OwnerName = rc.name
	}

	// Get the secret resolver from the registry
	resolver := rc.registry.SecretResolver()
	if resolver == nil {
		return nil, fmt.Errorf("secret resolver not available")
	}

	// Delegate to the resolver
	return resolver.Resolve(rc.ctx, binding)
}

// Service discovery

func (rc *reconcileContext) DiscoverSplunk(selector discovery.SplunkSelector) ([]discovery.SplunkEndpoint, error) {
	// Fill in context if not provided
	if selector.Namespace == "" && !rc.runtime.config.ClusterScoped {
		selector.Namespace = rc.namespace
	}

	// Get the discovery service from the registry
	discoveryService := rc.registry.DiscoveryService()
	if discoveryService == nil {
		return nil, fmt.Errorf("discovery service not available")
	}

	// Delegate to the discovery service
	return discoveryService.DiscoverSplunk(rc.ctx, selector)
}

func (rc *reconcileContext) Discover(selector discovery.Selector) ([]discovery.Endpoint, error) {
	// Fill in context if not provided
	if selector.Namespace == "" && !rc.runtime.config.ClusterScoped {
		selector.Namespace = rc.namespace
	}

	// Get the discovery service from the registry
	discoveryService := rc.registry.DiscoveryService()
	if discoveryService == nil {
		return nil, fmt.Errorf("discovery service not available")
	}

	// Delegate to the discovery service
	return discoveryService.Discover(rc.ctx, selector)
}

// Configuration resolution

func (rc *reconcileContext) ResolveConfig(key string) (interface{}, error) {
	// Get the config resolver from the registry
	resolver := rc.registry.ConfigResolver()
	if resolver == nil {
		return nil, fmt.Errorf("config resolver not available")
	}

	// Delegate to the resolver
	return resolver.ResolveConfig(rc.ctx, key, rc.namespace)
}

// Resource builders

func (rc *reconcileContext) BuildStatefulSet() builders.StatefulSetBuilder {
	return rc.registry.StatefulSetBuilder(rc.namespace, rc.name)
}

func (rc *reconcileContext) BuildService() builders.ServiceBuilder {
	return rc.registry.ServiceBuilder(rc.namespace, rc.name)
}

func (rc *reconcileContext) BuildConfigMap() builders.ConfigMapBuilder {
	return rc.registry.ConfigMapBuilder(rc.namespace, rc.name)
}

func (rc *reconcileContext) BuildPod() builders.PodBuilder {
	return rc.registry.PodBuilder(rc.namespace, rc.name)
}

func (rc *reconcileContext) BuildDeployment() builders.DeploymentBuilder {
	return rc.registry.DeploymentBuilder(rc.namespace, rc.name)
}
