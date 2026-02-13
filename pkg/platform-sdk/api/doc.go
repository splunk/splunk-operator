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

/*
Package api defines the public interfaces for the Platform SDK.

The Platform SDK provides stable interfaces for Kubernetes controllers to:
- Resolve TLS certificates via cert-manager or self-signed fallback
- Validate secrets from External Secrets Operator or Kubernetes
- Discover Splunk instances and other services dynamically
- Build Kubernetes resources with best practices

# Quick Start

Create a Runtime instance in your controller:

	func (r *MyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	    runtime, err := sdk.NewRuntime(mgr.GetClient(),
	        sdk.WithClusterScoped(),
	        sdk.WithLogger(log),
	    )
	    if err != nil {
	        return err
	    }

	    if err := runtime.Start(context.Background()); err != nil {
	        return err
	    }

	    r.sdkRuntime = runtime
	    return ctrl.NewControllerManagedBy(mgr).For(&v1.MyResource{}).Complete(r)
	}

Use the SDK in your Reconcile method:

	func (r *MyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	    rctx := r.sdkRuntime.NewReconcileContext(ctx, req.Namespace, req.Name)

	    // Resolve a certificate
	    cert, err := rctx.ResolveCertificate(certificate.Binding{
	        Name: "my-tls",
	        DNSNames: []string{"my-service.default.svc"},
	    })
	    if err != nil || !cert.Ready {
	        return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	    }

	    // Resolve a secret
	    secret, err := rctx.ResolveSecret(secret.Binding{
	        Name: "my-credentials",
	        Keys: []string{"username", "password"},
	    })
	    if err != nil || !secret.Ready {
	        return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	    }

	    // Build a StatefulSet
	    sts, err := rctx.BuildStatefulSet().
	        WithName("my-app").
	        WithImage("my-app:latest").
	        WithSecret(secret).
	        WithCertificate(cert).
	        WithObservability().
	        Build()
	    if err != nil {
	        return ctrl.Result{}, err
	    }

	    return ctrl.Result{}, r.Create(ctx, sts)
	}

# Package Structure

	api/                    - Public interfaces (this package)
	  certificate/          - Certificate types
	  secret/               - Secret types
	  discovery/            - Service discovery types
	  builders/             - Resource builder interfaces
	internal/               - Internal implementations (not exported)
	services/               - Service layer (resolvers, discovery, etc.)
	providers/              - Provider implementations (cert-manager, ESO, etc.)
	pkg/                    - Shared utilities (cache, registry, etc.)

# Design Patterns

The SDK uses several key design patterns:

Factory Pattern: Runtime creates ReconcileContext, ReconcileContext creates Builders
Builder Pattern: Fluent API with method chaining for resource construction
Provider Pattern: Pluggable implementations (CertManagerProvider, SelfSignedProvider)
Singleton Pattern: Runtime singleton per operator
Dependency Injection: Constructor and context injection

# Key Concepts

Resource Resolution: The SDK uses a "Ready" pattern. Methods return a Ref with a Ready field.
Ready=true means the resource is available. Ready=false means "come back later" (requeue).

Configuration Hierarchy: CR spec > TenantConfig > PlatformConfig > Built-in defaults.
The SDK automatically resolves configuration from multiple sources.

Service Discovery: The SDK caches discovery results for 30 seconds and performs health checks.

Observability: The SDK automatically adds Prometheus annotations when requested.

# Error Handling

The SDK distinguishes between fatal errors and "not ready yet":

	resource, err := rctx.ResolveSomething(binding)
	if err != nil {
	    // Fatal error - API is down, SDK is broken, etc.
	    return ctrl.Result{}, err
	}

	if !resource.Ready {
	    // NOT an error! Just not ready yet. Check resource.Error for details.
	    log.Info("Resource pending", "reason", resource.Error)
	    return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Resource is ready, use it

# Testing

The SDK interfaces are designed for testing with mocks:

	type MockRuntime struct {
	    Certificates map[string]*certificate.Ref
	    Secrets      map[string]*secret.Ref
	}

	func (m *MockRuntime) NewReconcileContext(ctx, namespace, name) ReconcileContext {
	    return &MockReconcileContext{runtime: m, namespace: namespace, name: name}
	}

See the SDK documentation for complete examples and patterns.
*/
package api
