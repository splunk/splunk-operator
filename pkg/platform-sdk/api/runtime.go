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

// Package api defines the public interfaces for the Platform SDK.
// These interfaces provide stable contracts for feature controllers to use
// certificates, secrets, service discovery, and resource builders.
package api

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Runtime is the main SDK entry point.
// Create one instance per controller and reuse it across reconciliations.
//
// The Runtime manages the lifecycle of all SDK services (resolvers, providers,
// discovery, observability) and provides ReconcileContext objects for each
// reconciliation.
//
// Usage:
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
//	// Use runtime across reconciliations
//	rctx := runtime.NewReconcileContext(ctx, namespace, name)
type Runtime interface {
	// NewReconcileContext creates a context for a single reconciliation.
	// Call this at the start of each Reconcile() invocation.
	//
	// The ReconcileContext is lightweight and short-lived - one per reconciliation.
	// It provides access to all SDK capabilities with the resource's context.
	NewReconcileContext(ctx context.Context, namespace, name string) ReconcileContext

	// Start initializes the SDK runtime.
	// Call this in controller's SetupWithManager or main().
	//
	// Start performs:
	// - Initializes all services (resolvers, providers, discovery)
	// - Starts background watchers for configuration changes
	// - Validates cluster capabilities (cert-manager, ESO, etc.)
	Start(ctx context.Context) error

	// Stop gracefully shuts down the SDK.
	// Call this on controller shutdown.
	//
	// Stop ensures:
	// - All watches are stopped
	// - Connections are closed
	// - Background goroutines are terminated
	Stop() error

	// GetClient returns the Kubernetes client used by the SDK.
	GetClient() client.Client

	// GetLogger returns the logger configured for the SDK.
	GetLogger() logr.Logger

	// GetEventRecorder returns the event recorder for emitting Kubernetes events.
	GetEventRecorder() record.EventRecorder
}

// RuntimeOption configures the Runtime during creation.
type RuntimeOption func(*RuntimeConfig)

// RuntimeConfig holds configuration for the Runtime.
type RuntimeConfig struct {
	// ClusterScoped indicates if the operator watches all namespaces.
	// If false, the operator is namespace-scoped.
	ClusterScoped bool

	// Namespace is the namespace the operator is scoped to (if not cluster-scoped).
	Namespace string

	// Logger for SDK operations.
	Logger logr.Logger

	// EventRecorder for emitting Kubernetes events.
	// Used to record state changes like certificate provisioning, secret rotation, etc.
	EventRecorder record.EventRecorder

	// DryRun mode for testing (no actual changes to cluster).
	DryRun bool

	// EnableMetrics enables Prometheus metrics for SDK operations.
	EnableMetrics bool

	// MetricsPort is the port for Prometheus metrics (default: 8383).
	MetricsPort int
}

// WithClusterScoped configures the Runtime for cluster-scoped operation.
// The operator can watch and manage resources across all namespaces.
func WithClusterScoped() RuntimeOption {
	return func(cfg *RuntimeConfig) {
		cfg.ClusterScoped = true
	}
}

// WithNamespace configures the Runtime for namespace-scoped operation.
// The operator can only watch and manage resources in the specified namespace.
func WithNamespace(namespace string) RuntimeOption {
	return func(cfg *RuntimeConfig) {
		cfg.ClusterScoped = false
		cfg.Namespace = namespace
	}
}

// WithLogger sets the logger for SDK operations.
func WithLogger(logger logr.Logger) RuntimeOption {
	return func(cfg *RuntimeConfig) {
		cfg.Logger = logger
	}
}

// WithDryRun enables dry-run mode for testing.
// In dry-run mode, the SDK doesn't make actual changes to the cluster.
func WithDryRun(dryRun bool) RuntimeOption {
	return func(cfg *RuntimeConfig) {
		cfg.DryRun = dryRun
	}
}

// WithMetrics enables Prometheus metrics for SDK operations.
func WithMetrics(enabled bool, port int) RuntimeOption {
	return func(cfg *RuntimeConfig) {
		cfg.EnableMetrics = enabled
		cfg.MetricsPort = port
	}
}

// WithEventRecorder sets the event recorder for emitting Kubernetes events.
// Events are used to record state changes visible via kubectl describe.
//
// Usage:
//
//	recorder := mgr.GetEventRecorderFor("splunk-operator")
//	runtime, err := sdk.NewRuntime(mgr.GetClient(),
//	    sdk.WithEventRecorder(recorder),
//	)
func WithEventRecorder(recorder record.EventRecorder) RuntimeOption {
	return func(cfg *RuntimeConfig) {
		cfg.EventRecorder = recorder
	}
}
