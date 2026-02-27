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

package api

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/builders"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/certificate"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/discovery"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/secret"
	"k8s.io/client-go/tools/record"
)

// ReconcileContext provides SDK capabilities for a single reconciliation.
//
// ReconcileContext is a lightweight, short-lived object that knows about
// the specific resource being reconciled (its namespace and name) and
// provides convenient access to all SDK capabilities.
//
// Usage:
//
//	func (r *MyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
//	    // Create a context for this reconciliation
//	    rctx := r.sdkRuntime.NewReconcileContext(ctx, req.Namespace, req.Name)
//
//	    // Resolve a certificate
//	    cert, err := rctx.ResolveCertificate(certificate.Binding{
//	        Name: "my-tls",
//	        DNSNames: []string{"my-service.default.svc"},
//	    })
//	    if err != nil || !cert.Ready {
//	        return ctrl.Result{RequeueAfter: 10 * time.Second}, err
//	    }
//
//	    // Resolve a secret
//	    secret, err := rctx.ResolveSecret(secret.Binding{
//	        Name: "my-credentials",
//	        Keys: []string{"username", "password"},
//	    })
//	    if err != nil || !secret.Ready {
//	        return ctrl.Result{RequeueAfter: 10 * time.Second}, err
//	    }
//
//	    // Build a StatefulSet
//	    sts, err := rctx.BuildStatefulSet().
//	        WithName("my-app").
//	        WithSecret(secret).
//	        WithCertificate(cert).
//	        Build()
//	    if err != nil {
//	        return ctrl.Result{}, err
//	    }
//
//	    return ctrl.Result{}, nil
//	}
type ReconcileContext interface {
	// Context information

	// Namespace returns the namespace of the resource being reconciled.
	Namespace() string

	// Name returns the name of the resource being reconciled.
	Name() string

	// Context returns the Go context for this reconciliation.
	Context() context.Context

	// Logger returns a logger configured with resource context.
	// The logger includes namespace and name fields automatically.
	Logger() logr.Logger

	// EventRecorder returns an event recorder for emitting Kubernetes events.
	// Use this to record state changes that should be visible via kubectl describe.
	//
	// Example:
	//   rctx.EventRecorder().Event(obj, corev1.EventTypeNormal, "CertificateReady", "Certificate has been provisioned")
	EventRecorder() record.EventRecorder

	// Certificate resolution

	// ResolveCertificate requests a TLS certificate with the specified DNS names.
	//
	// The SDK automatically:
	// - Checks if cert-manager is installed and uses it if available
	// - Falls back to self-signed certificates if cert-manager is not available
	// - Creates Certificate CRs (for cert-manager) or Secrets (for self-signed)
	// - Watches for certificate readiness
	// - Returns a Ref with the secret name and Ready status
	//
	// Returns:
	// - Ref with Ready=true when the certificate is issued and ready
	// - Ref with Ready=false when the certificate is being issued (requeue after 10s)
	// - Error if there's a fatal problem (API unreachable, invalid configuration)
	//
	// Example:
	//
	//	cert, err := rctx.ResolveCertificate(certificate.Binding{
	//	    Name: "postgres-tls",
	//	    DNSNames: []string{"postgres.default.svc"},
	//	})
	//	if err != nil {
	//	    return ctrl.Result{}, err
	//	}
	//	if !cert.Ready {
	//	    return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	//	}
	ResolveCertificate(binding certificate.Binding) (*certificate.Ref, error)

	// Secret resolution

	// ResolveSecret validates that a Kubernetes secret exists and has required keys.
	//
	// The SDK does NOT create ExternalSecret CRs. That's the admin's job via kubectl/GitOps.
	// The SDK only validates the resulting Kubernetes secret exists.
	//
	// For Splunk secrets (SecretTypeSplunk), the SDK manages versioned secrets
	// (splunk-standalone-secret-v1, v2, v3) for safe rotation.
	//
	// Returns:
	// - Ref with Ready=true when the secret exists and has all required keys
	// - Ref with Ready=false when the secret doesn't exist or is missing keys
	// - Error if there's a fatal problem (API unreachable, invalid configuration)
	//
	// Example:
	//
	//	secret, err := rctx.ResolveSecret(secret.Binding{
	//	    Name: "postgres-credentials",
	//	    Keys: []string{"username", "password"},
	//	})
	//	if err != nil {
	//	    return ctrl.Result{}, err
	//	}
	//	if !secret.Ready {
	//	    return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	//	}
	ResolveSecret(binding secret.Binding) (*secret.Ref, error)

	// Service discovery

	// DiscoverSplunk finds Splunk instances (Standalone, SearchHeadCluster, IndexerCluster, etc.).
	//
	// The SDK searches for:
	// - SOK-managed Splunk CRs (in Kubernetes)
	// - ExternalSplunkCluster CRs (pointing to Splunk on VMs/external systems)
	//
	// Results are sorted by health (healthy endpoints first) and cached for 30 seconds.
	//
	// Example:
	//
	//	endpoints, err := rctx.DiscoverSplunk(discovery.SplunkSelector{
	//	    Type: discovery.SplunkTypeSearchHeadCluster,
	//	    IncludeExternal: true,
	//	})
	//	if err != nil {
	//	    return ctrl.Result{}, err
	//	}
	//	if len(endpoints) == 0 {
	//	    return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	//	}
	//	splunkURL := endpoints[0].URL
	DiscoverSplunk(selector discovery.SplunkSelector) ([]discovery.SplunkEndpoint, error)

	// Discover finds generic Kubernetes services by labels.
	//
	// Use this for discovering services that aren't Splunk instances
	// (databases, message queues, etc.).
	//
	// Example:
	//
	//	endpoints, err := rctx.Discover(discovery.Selector{
	//	    Labels: map[string]string{
	//	        "app": "postgresql",
	//	    },
	//	})
	//	if err != nil {
	//	    return ctrl.Result{}, err
	//	}
	Discover(selector discovery.Selector) ([]discovery.Endpoint, error)

	// Configuration resolution

	// ResolveConfig reads configuration with proper hierarchy.
	//
	// Hierarchy: CR spec > TenantConfig > PlatformConfig > Built-in defaults
	//
	// Key examples: "certificates.issuer", "secrets.store", "observability.enabled"
	//
	// Most of the time, you don't need to call this directly. The SDK's
	// ResolveCertificate and ResolveSecret methods handle configuration internally.
	ResolveConfig(key string) (interface{}, error)

	// Resource builders

	// BuildStatefulSet creates a fluent builder for StatefulSets.
	//
	// The builder automatically:
	// - Mounts secrets at /mnt/splunk-secrets
	// - Mounts certificates at /mnt/tls and /mnt/ca-bundle
	// - Adds observability annotations (Prometheus)
	// - Sets owner references for garbage collection
	// - Applies SDK conventions for labels and selectors
	//
	// Example:
	//
	//	sts, err := rctx.BuildStatefulSet().
	//	    WithName("postgres").
	//	    WithReplicas(1).
	//	    WithImage("postgres:15").
	//	    WithSecret(secret).
	//	    WithCertificate(cert).
	//	    WithObservability().
	//	    Build()
	BuildStatefulSet() builders.StatefulSetBuilder

	// BuildService creates a fluent builder for Services.
	//
	// The builder automatically:
	// - Adds discovery labels for DiscoveryService
	// - Sets owner references
	// - Applies SDK conventions
	BuildService() builders.ServiceBuilder

	// BuildConfigMap creates a fluent builder for ConfigMaps.
	BuildConfigMap() builders.ConfigMapBuilder

	// BuildPod creates a fluent builder for Pod specifications.
	// Useful for building pod specs that are shared across resources.
	BuildPod() builders.PodBuilder

	// BuildDeployment creates a fluent builder for Deployments.
	// Use this for stateless workloads.
	BuildDeployment() builders.DeploymentBuilder
}
