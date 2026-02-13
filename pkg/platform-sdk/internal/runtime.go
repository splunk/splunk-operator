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

// Package internal contains internal implementations not exported to users.
package internal

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// runtime implements the api.Runtime interface.
//
// The runtime is the main SDK coordinator that:
// - Creates and manages the service registry
// - Initializes all services on Start()
// - Provides ReconcileContext instances on demand
// - Manages lifecycle (Start/Stop)
type runtime struct {
	client client.Client
	config *api.RuntimeConfig
	logger logr.Logger

	// registry holds all services (resolvers, providers, discovery, etc.)
	registry *registry

	// started indicates if the runtime has been started
	started bool
	mu      sync.RWMutex

	// stopCh is used to signal shutdown
	stopCh chan struct{}
}

// NewRuntime creates a new runtime implementation.
func NewRuntime(client client.Client, config *api.RuntimeConfig) (api.Runtime, error) {
	if client == nil {
		return nil, fmt.Errorf("client cannot be nil")
	}

	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	r := &runtime{
		client: client,
		config: config,
		logger: config.Logger,
		stopCh: make(chan struct{}),
	}

	// Create the service registry
	registry, err := newRegistry(client, config, r.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry: %w", err)
	}
	r.registry = registry

	return r, nil
}

// NewReconcileContext creates a context for a single reconciliation.
func (r *runtime) NewReconcileContext(ctx context.Context, namespace, name string) api.ReconcileContext {
	logger := r.logger.WithValues("namespace", namespace, "name", name)

	return &reconcileContext{
		ctx:       ctx,
		namespace: namespace,
		name:      name,
		runtime:   r,
		registry:  r.registry,
		logger:    logger,
	}
}

// Start initializes the SDK runtime.
func (r *runtime) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return fmt.Errorf("runtime already started")
	}

	r.logger.Info("Starting Platform SDK runtime",
		"clusterScoped", r.config.ClusterScoped,
		"namespace", r.config.Namespace,
		"dryRun", r.config.DryRun,
	)

	// Start the registry (initializes services, starts watches)
	if err := r.registry.Start(ctx); err != nil {
		return fmt.Errorf("failed to start registry: %w", err)
	}

	r.started = true
	r.logger.Info("Platform SDK runtime started successfully")

	return nil
}

// Stop gracefully shuts down the SDK.
func (r *runtime) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started {
		return nil
	}

	r.logger.Info("Stopping Platform SDK runtime")

	// Signal shutdown
	close(r.stopCh)

	// Stop the registry (stops watches, closes connections)
	if err := r.registry.Stop(); err != nil {
		return fmt.Errorf("failed to stop registry: %w", err)
	}

	r.started = false
	r.logger.Info("Platform SDK runtime stopped successfully")

	return nil
}

// GetClient returns the Kubernetes client.
func (r *runtime) GetClient() client.Client {
	return r.client
}

// GetLogger returns the logger.
func (r *runtime) GetLogger() logr.Logger {
	return r.logger
}

// GetEventRecorder returns the event recorder.
func (r *runtime) GetEventRecorder() record.EventRecorder {
	return r.config.EventRecorder
}
