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

// Package sdk provides the Platform SDK for Kubernetes controllers.
//
// The Platform SDK simplifies controller development by providing:
// - Certificate resolution via cert-manager or self-signed fallback
// - Secret validation with External Secrets Operator integration
// - Service discovery with health checking and caching
// - Resource builders with best practices built-in
//
// See the api package for the public interfaces and usage examples.
package sdk

import (
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/internal"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// NewRuntime creates a new SDK Runtime instance.
//
// The Runtime is the main entry point for the Platform SDK. Create one
// instance per controller and reuse it across reconciliations.
//
// Example:
//
//	runtime, err := sdk.NewRuntime(mgr.GetClient(),
//	    sdk.WithClusterScoped(),
//	    sdk.WithLogger(log),
//	)
//	if err != nil {
//	    return err
//	}
//
//	if err := runtime.Start(ctx); err != nil {
//	    return err
//	}
//
// The Runtime manages:
// - Service lifecycle (resolvers, providers, discovery)
// - Configuration watching (PlatformConfig, TenantConfig)
// - Provider detection (cert-manager, ESO, OTel)
// - Resource caching and connection pooling
func NewRuntime(client client.Client, opts ...api.RuntimeOption) (api.Runtime, error) {
	// Build configuration from options
	config := &api.RuntimeConfig{
		ClusterScoped: true, // Default to cluster-scoped
		Logger:        log.Log.WithName("platform-sdk"),
		MetricsPort:   8383,
	}

	for _, opt := range opts {
		opt(config)
	}

	// Create the runtime implementation
	return internal.NewRuntime(client, config)
}

// Re-export option functions for convenience
var (
	WithClusterScoped = api.WithClusterScoped
	WithNamespace     = api.WithNamespace
	WithLogger        = api.WithLogger
	WithDryRun        = api.WithDryRun
	WithMetrics       = api.WithMetrics
	WithEventRecorder = api.WithEventRecorder
)
