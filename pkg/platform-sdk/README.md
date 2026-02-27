# Platform SDK

The Platform SDK provides a unified, high-level API for building Splunk Operator controllers. It abstracts common patterns like certificate management, secret rotation, service discovery, and resource construction.

## Table of Contents

- [Quick Start](#quick-start)
- [Architecture](#architecture)
- [Core Concepts](#core-concepts)
- [Services](#services)
- [Builders](#builders)
- [Configuration](#configuration)
- [Logging and Events](#logging-and-events)
- [Examples](#examples)

## Quick Start

### Setup in Controller

```go
import (
    "github.com/splunk/splunk-operator/pkg/platform-sdk"
    "github.com/splunk/splunk-operator/pkg/platform-sdk/api"
)

type MyReconciler struct {
    client.Client
    sdkRuntime api.Runtime
}

func (r *MyReconciler) SetupWithManager(mgr ctrl.Manager) error {
    // Create SDK runtime
    recorder := mgr.GetEventRecorderFor("splunk-operator")
    sdkRuntime, err := sdk.NewRuntime(
        mgr.GetClient(),
        sdk.WithClusterScoped(),
        sdk.WithLogger(log),
        sdk.WithEventRecorder(recorder),
    )
    if err != nil {
        return err
    }

    // Start the SDK
    if err := sdkRuntime.Start(context.Background()); err != nil {
        return err
    }

    r.sdkRuntime = sdkRuntime

    return ctrl.NewControllerManagedBy(mgr).
        For(&enterprisev4.Standalone{}).
        Complete(r)
}
```

### Usage in Reconcile Loop

```go
func (r *MyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // Create reconcile context
    rctx := r.sdkRuntime.NewReconcileContext(ctx, req.Namespace, req.Name)

    // Get the CR
    cr := &enterprisev4.Standalone{}
    if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // Resolve certificate
    cert, err := rctx.ResolveCertificate(certificate.Binding{
        Name:     "splunk-tls",
        DNSNames: []string{
            fmt.Sprintf("%s.%s.svc", cr.Name, cr.Namespace),
            fmt.Sprintf("%s.%s.svc.cluster.local", cr.Name, cr.Namespace),
        },
    })
    if err != nil {
        return ctrl.Result{}, err
    }
    if !cert.Ready {
        rctx.Logger().Info("Certificate not ready, requeueing")
        return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
    }

    // Resolve secret
    secretRef, err := rctx.ResolveSecret(secret.Binding{
        Name: "splunk-credentials",
        Type: secret.TypeSplunk,
        Keys: []string{"password", "hec_token"},
    })
    if err != nil {
        return ctrl.Result{}, err
    }

    // Build StatefulSet
    sts, err := rctx.BuildStatefulSet().
        WithName(cr.Name).
        WithReplicas(cr.Spec.Replicas).
        WithImage(cr.Spec.Image).
        WithPorts([]corev1.ContainerPort{
            {Name: "web", ContainerPort: 8000},
            {Name: "mgmt", ContainerPort: 8089},
        }).
        WithCertificate(cert).
        WithSecret(secretRef).
        WithObservability().
        Build()
    if err != nil {
        return ctrl.Result{}, err
    }

    // Apply the StatefulSet
    if err := r.Client.Patch(ctx, sts, client.Apply,
        client.ForceOwnership, client.FieldOwner("splunk-operator")); err != nil {
        return ctrl.Result{}, err
    }

    // Record event
    rctx.EventRecorder().Event(cr, corev1.EventTypeNormal,
        api.EventReasonCertificateReady,
        fmt.Sprintf("Certificate %s is ready", cert.SecretName))

    return ctrl.Result{}, nil
}
```

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Controller Layer                          │
│  (Standalone, ClusterManager, SearchHead controllers)       │
└───────────────────────┬─────────────────────────────────────┘
                        │
                        │ Uses
                        ▼
┌─────────────────────────────────────────────────────────────┐
│                   Platform SDK                               │
├─────────────────────────────────────────────────────────────┤
│  Runtime (Singleton per operator)                           │
│    │                                                          │
│    ├─► ReconcileContext (Per reconciliation)                │
│    │                                                          │
│    └─► Service Registry                                      │
│          ├─► ConfigResolver                                  │
│          ├─► CertificateResolver ─► [cert-manager|self-sign]│
│          ├─► SecretResolver       ─► [ESO|Kubernetes]       │
│          ├─► DiscoveryService                                │
│          ├─► ObservabilityService                            │
│          └─► Builders (StatefulSet, Service, etc.)          │
└─────────────────────────────────────────────────────────────┘
                        │
                        │ Reads
                        ▼
┌─────────────────────────────────────────────────────────────┐
│              Configuration Layer                             │
│  PlatformConfig (cluster) + TenantConfig (namespace)        │
└─────────────────────────────────────────────────────────────┘
```

## Core Concepts

### Runtime

The `Runtime` is the main SDK entry point. Create **one instance per operator** and reuse it across all reconciliations.

- Manages lifecycle of all services
- Starts background watchers for configuration
- Detects cluster capabilities (cert-manager, ESO, OTel)

### ReconcileContext

A `ReconcileContext` is a lightweight, short-lived object created **once per reconciliation**. It:

- Knows about the specific resource being reconciled
- Provides a logger with namespace/name context
- Provides access to all SDK services and builders
- Includes EventRecorder for emitting Kubernetes events

### Service Registry

The registry creates and caches services with lazy initialization:

- **ConfigResolver**: Hierarchical configuration resolution
- **CertificateResolver**: Certificate provisioning
- **SecretResolver**: Secret validation and versioning
- **DiscoveryService**: Service discovery
- **ObservabilityService**: Monitoring annotations

## Services

### ConfigResolver

Resolves configuration with 4-layer hierarchy:

1. Built-in defaults (hardcoded)
2. PlatformConfig (cluster-scoped)
3. TenantConfig (namespace-scoped)
4. CR spec (per-resource)

```go
// Get resolved certificate config
certConfig, err := configResolver.ResolveCertificateConfig(ctx, namespace)

// Check provider
if certConfig.Provider == "cert-manager" {
    // Use cert-manager
}
```

### CertificateResolver

Provisions TLS certificates with automatic provider selection:

```go
cert, err := rctx.ResolveCertificate(certificate.Binding{
    Name:     "my-tls",
    DNSNames: []string{"my-service.default.svc"},
    Duration: 90 * 24 * time.Hour,
})

if !cert.Ready {
    return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

// Use cert.SecretName in pod spec
```

**Providers:**
- **cert-manager**: Creates Certificate CR, watches for ready status
- **self-signed**: Generates RSA 2048-bit key and X.509 cert, stores in Secret

### SecretResolver

Validates secrets and manages versioning for Splunk secrets:

```go
secretRef, err := rctx.ResolveSecret(secret.Binding{
    Name: "splunk-credentials",
    Type: secret.TypeSplunk,
    Keys: []string{"password", "hec_token"},
})

// secretRef.SecretName might be "splunk-credentials-v3" (versioned)
```

**Versioning:**
- Source: `splunk-{namespace}-secret` (admin creates)
- Versioned: `{base}-v1`, `{base}-v2`, `{base}-v3` (SDK manages)
- Automatic rotation on content change
- Keeps last 3 versions for rollback

### DiscoveryService

Discovers Splunk instances and Kubernetes services:

```go
// Find all IndexerClusters
endpoints, err := rctx.DiscoverSplunk(discovery.SplunkSelector{
    Type:      discovery.SplunkTypeIndexerCluster,
    Namespace: "splunk-prod",
})

for _, ep := range endpoints {
    log.Info("Found indexer", "url", ep.URL, "ready", ep.Health.Healthy)
}

// Generic service discovery
services, err := rctx.Discover(discovery.Selector{
    Labels: map[string]string{"app": "splunk"},
})
```

### ObservabilityService

Adds monitoring and telemetry annotations:

```go
// Check if observability is enabled
enabled, err := observability.ShouldAddObservability(ctx, namespace)

// Get annotations for pod spec
annotations, err := observability.GetObservabilityAnnotations(ctx, namespace)
// Returns: {"prometheus.io/scrape": "true", "prometheus.io/port": "9090", ...}
```

## Builders

All builders use a fluent API pattern:

### StatefulSetBuilder

```go
sts, err := rctx.BuildStatefulSet().
    WithName("splunk-indexer").
    WithReplicas(3).
    WithImage("splunk/splunk:9.1.0").
    WithPorts([]corev1.ContainerPort{
        {Name: "web", ContainerPort: 8000},
        {Name: "mgmt", ContainerPort: 8089},
        {Name: "s2s", ContainerPort: 9997},
    }).
    WithCertificate(certRef).      // Auto-creates volume & mount
    WithSecret(secretRef).          // Auto-creates volume
    WithConfigMap("app-config").    // Auto-creates volume
    WithObservability().            // Adds Prometheus annotations
    WithEnv(corev1.EnvVar{
        Name: "SPLUNK_ROLE",
        Value: "splunk_indexer",
    }).
    WithResources(corev1.ResourceRequirements{
        Requests: corev1.ResourceList{
            corev1.ResourceCPU:    resource.MustParse("2"),
            corev1.ResourceMemory: resource.MustParse("8Gi"),
        },
    }).
    Build()
```

### ServiceBuilder

```go
svc, err := rctx.BuildService().
    WithName("splunk-indexer").
    WithType(corev1.ServiceTypeClusterIP).
    WithPorts([]corev1.ServicePort{
        {Name: "web", Port: 8000, TargetPort: intstr.FromInt(8000)},
        {Name: "mgmt", Port: 8089, TargetPort: intstr.FromInt(8089)},
    }).
    WithDiscoveryLabels().  // Adds splunk.com/discoverable=true
    Build()
```

### ConfigMapBuilder

```go
cm, err := rctx.BuildConfigMap().
    WithName("app-config").
    WithData(map[string]string{
        "server.conf": serverConfContent,
        "inputs.conf": inputsConfContent,
    }).
    Build()
```

### DeploymentBuilder

```go
deploy, err := rctx.BuildDeployment().
    WithName("splunk-forwarder").
    WithReplicas(2).
    WithImage("splunk/universalforwarder:9.1.0").
    WithPorts([]corev1.ContainerPort{
        {Name: "mgmt", ContainerPort: 8089},
    }).
    WithCertificate(certRef).
    WithObservability().
    Build()
```

## Configuration

### PlatformConfig (Cluster-scoped)

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PlatformConfig
metadata:
  name: platform-default
spec:
  certificates:
    provider: cert-manager
    issuerRef:
      name: letsencrypt-prod
      kind: ClusterIssuer
    duration: 7776000  # 90 days
    renewBefore: 2592000  # 30 days

  secrets:
    provider: kubernetes
    versioningEnabled: true
    versionsToKeep: 3

  observability:
    enabled: true
    provider: prometheus
    prometheusPort: 9090
    prometheusPath: /metrics
```

### TenantConfig (Namespace-scoped)

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: TenantConfig
metadata:
  name: tenant-config
  namespace: splunk-prod
spec:
  certificates:
    provider: self-signed  # Override to self-signed for this namespace

  observability:
    enabled: false  # Disable observability for this tenant
```

## Logging and Events

### Logging

The SDK uses structured logging with log levels:

```go
// V(0) - Info: Important state changes (always visible)
rctx.Logger().Info("Certificate provisioned", "name", cert.SecretName)

// V(1) - Debug: Detailed operations
rctx.Logger().V(1).Info("Using cached config", "namespace", namespace)

// V(2) - Trace: Very detailed, all API calls
rctx.Logger().V(2).Info("Calling cert-manager API", "url", endpoint)

// Errors (always visible)
rctx.Logger().Error(err, "Failed to provision certificate", "name", certName)
```

Enable debug logs: `--zap-log-level=debug` or `--v=1`
Enable trace logs: `--zap-log-level=trace` or `--v=2`

### Events

Emit Kubernetes events for important state changes:

```go
// Normal events
rctx.EventRecorder().Event(cr, corev1.EventTypeNormal,
    api.EventReasonCertificateReady,
    "Certificate has been provisioned successfully")

// Warning events
rctx.EventRecorder().Event(cr, corev1.EventTypeWarning,
    api.EventReasonSecretMissing,
    "Secret splunk-credentials not found")
```

View events:
```bash
kubectl describe standalone my-splunk
# Events:
#   Normal   CertificateReady      Certificate my-tls is ready
#   Normal   SecretVersionCreated  Created secret version 3
```

## Examples

See the [examples](examples/) directory for complete working examples:

- [Basic Standalone](examples/basic-standalone/): Simple Splunk standalone with certificates
- [Clustered Setup](examples/clustered/): Indexer cluster with service discovery
- [Custom Configuration](examples/custom-config/): Using PlatformConfig and TenantConfig
- [Observability Integration](examples/observability/): Prometheus metrics and OTel tracing

## Best Practices

1. **One Runtime per operator**: Create the SDK runtime once in SetupWithManager
2. **One ReconcileContext per reconciliation**: Create fresh context in each Reconcile call
3. **Check Ready status**: Always check `cert.Ready` and `secret.Ready` before proceeding
4. **Requeue when not ready**: Return `RequeueAfter: 10s` when resources aren't ready yet
5. **Use structured logging**: Include relevant fields in all log statements
6. **Emit events**: Record important state changes for visibility
7. **Use builders**: Leverage fluent API for readable resource construction
8. **Configure hierarchically**: Use PlatformConfig for defaults, TenantConfig for overrides

## Troubleshooting

### Certificate not ready

```go
if !cert.Ready {
    rctx.Logger().Info("Certificate not ready",
        "provider", cert.Provider,
        "error", cert.Error)
    return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}
```

### Secret version mismatch

```go
if secretRef.Version != nil {
    rctx.Logger().V(1).Info("Using versioned secret",
        "version", *secretRef.Version,
        "secretName", secretRef.SecretName)
}
```

### Service discovery returns empty

```go
endpoints, err := rctx.DiscoverSplunk(selector)
if len(endpoints) == 0 {
    rctx.Logger().Info("No services found",
        "type", selector.Type,
        "namespace", selector.Namespace)
}
```

### Enable debug logging

Set log level to see detailed SDK operations:
```bash
--zap-log-level=debug  # V(1) logs
--zap-log-level=trace  # V(2) logs
```
