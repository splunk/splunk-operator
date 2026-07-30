---
title: Feature Gates
parent: Develop & Contribute
nav_order: 6
---

# Feature Gates

The Splunk Operator uses the Kubernetes [FeatureGate](https://pkg.go.dev/k8s.io/component-base/featuregate) pattern to control rollout of new functionality. Feature gates allow new code to be merged to the main branch without activating in production, giving teams a safe, per-environment opt-in mechanism.

## Usage

### Helm Chart

Set feature gates via the `splunkOperator.featureGates` map in your values file:

```yaml
splunkOperator:
  featureGates:
    ValidationWebhook: true
```

Or pass them on the command line:

```bash
helm install splunk-operator splunk/splunk-operator \
  --set splunkOperator.featureGates.ValidationWebhook=true
```

The chart formats the map into a single `--feature-gates=Key1=true,Key2=false` argument automatically. Adding a new gate requires no chart changes — just add an entry to the map.

### Direct Binary

When running the operator binary directly, pass feature gates at startup:

```bash
/manager --feature-gates=ValidationWebhook=true
```

## Maturity Lifecycle

| Stage     | Default | Can Override | Next Step                               |
|-----------|---------|-------------|-----------------------------------------|
| **Alpha** | off     | Yes         | Promote to Beta after validation        |
| **Beta**  | on      | Yes         | Promote to GA after sustained stability |
| **GA**    | on      | No          | Remove gate in a future release         |

## Current Feature Gates

| Gate                         | Default | Stage | Since        | Description                                                               |
|------------------------------|---------|-------|--------------|---------------------------------------------------------------------------|
| `ValidationWebhook`          | `false` | Alpha | v3.2.0       | Centralized validation webhook server for CR admission                    |
| `PostgresController`         | `false` | Alpha | ?            | PostgresCluster, PostgresClusterClass, and PostgresDatabase controllers and CRDs |
| `SplunkPodLifecycle`         | `false` | Alpha | Wave 0 spike | Common Splunk workload Pod lifecycle contract                             |
| `SearchHeadClusterLifecycle` | `false` | Alpha | Wave 0 spike | Durable Search Head Cluster lifecycle policy and orchestration contract   |
| `IndexerClusterLifecycle`    | `false` | Alpha | SHC-85 spike | Durable Indexer Pod-update ownership and serving-path readiness contract  |

`SearchHeadClusterLifecycle=true` and `IndexerClusterLifecycle=true` each
require `SplunkPodLifecycle=true`. The Operator rejects either invalid
combination at startup.

### Splunk Pod lifecycle

When `SplunkPodLifecycle=true`, the Operator renders
`spec.terminationGracePeriodSeconds` into every Splunk Enterprise StatefulSet
Pod template. An explicitly configured value is used when present; otherwise
the Wave 0 spike resolves 1200 seconds. The field covers ClusterManager,
IndexerCluster, IngestorCluster, LicenseManager, MonitoringConsole,
SearchHeadCluster members and deployer, Standalone, and their v3 compatibility
workloads.

When the gate is disabled, the Operator leaves the Pod-template field unset,
including when a stored custom resource contains the gated field. With the
current `OnDelete` StatefulSet strategy, changing this value creates a new
StatefulSet revision but does not itself replace an existing Pod. The new grace
period takes effect when a Pod is subsequently replaced. Automated replacement
is owned by the separate lifecycle-orchestration work.

The Splunk Enterprise Helm chart exposes
`terminationGracePeriodSeconds` under each workload's values section.

### Indexer Cluster lifecycle

When `IndexerClusterLifecycle=true`, the Operator records the exact Indexer Pod,
UID, source revision, desired revision, and lifecycle stage before it starts a
Pod update. The controller may continue past one deliberately unavailable Pod
only when that Pod is the recorded target and the Cluster Manager reports a
controlled decommission state. It does not use this exception for a second or
unrelated unavailable Pod. A replacement completes only after it has a new UID,
the desired StatefulSet revision, Kubernetes readiness, an `Up`, searchable
Cluster Manager peer observation, publication in the client-facing Indexer
Service EndpointSlice, and a remote serving-path observation. When HEC is
enabled, a separate healthy Splunk Pod must reach the replacement's effective
HTTP or HTTPS HEC health endpoint through Pod DNS. When HEC is disabled,
a separate healthy Splunk Pod must establish a TCP connection to the
replacement's declared Splunk-to-Splunk container port. The serving observation
and completion are persisted before another target is selected. If a
replica-count change arrives during an owned Pod update, the controller first
recovers that target and then applies the scale change, so scale-down cannot
decommission a second peer concurrently. If the controller restarts after the
Cluster Manager accepted decommission but before that API result reached CR
status, it records the exact target's controlled peer state as a recovered
durable transition before authorizing replacement.

Readiness withdrawal is itself a durable stage: the controller records the
target first, records withdrawal intent next, writes the Pod-local withdrawal
signal, waits for Kubernetes `Ready=False`, and only then asks the Cluster
Manager to decommission the peer. If a template change is reverted while only
target selection has been persisted, the untouched operation is cancelled.
Once readiness withdrawal has been durably authorized, the exact target is
recovered through replacement before the controller accepts the rollback or
any other disruption.

Aggregate Cluster Manager readiness may become false while the recorded target
is withdrawn. That signal does not abandon the only path that can restore the
peer: the controller may continue the exact recovery only after a successful
Cluster Manager observation shows every non-target peer remains `Up` and
searchable. Any non-target degradation blocks progress.

The Operator emits Kubernetes Events and structured logs for target selection,
readiness withdrawal, decommission, revision adoption, recovery, cancellation,
and completion. Prometheus exposes
`splunk_operator_indexer_lifecycle_transition_total{stage,reason}` and
`splunk_operator_indexer_lifecycle_stage_duration_seconds{stage}`. Labels are
bounded and deliberately exclude namespace, resource name, Pod, UID, revision,
operation ID, and free-form messages.

The gate also enables an Indexer-only HEC serving check in the readiness probe.
If effective local Splunk configuration enables HEC, readiness requires the
local HTTP or HTTPS HEC health endpoint to respond. Disabled HEC is not treated
as a normal-operation failure. An Operator-owned decommission always withdraws
Indexer readiness, including when HEC is disabled, so S2S and other Service
traffic cannot continue selecting the draining Pod. The probe derives HEC
protocol and port from effective `inputs.conf` and uses loopback, so it does not
depend on ingress TLS termination, a service mesh, or external network routing.
These serving checks do not change liveness. When no explicit readiness timing
is configured, the Alpha gate uses a 2-second period, 2-second timeout, and
one-failure threshold; explicitly configured probe values remain authoritative.

The lifecycle gate does not use the historical whole-StatefulSet recreation
fallback for Splunk Enterprise 8-to-9 Indexer migrations because that path
cannot preserve exact one-Pod-at-a-time ownership. Such migrations remain a
separate compatibility qualification: the gated controller uses the normal
per-Pod lifecycle and fails closed if a peer cannot be decommissioned.

This Operator-owned lifecycle cannot interpose between peers in a rolling
restart that Splunk Enterprise performs internally after a Cluster Manager
bundle push. The readiness check still removes a locally non-serving peer from
Kubernetes Service traffic, but Splunk Enterprise currently decides when to
advance its own internal restart from one peer to the next. Requiring remote
HEC recovery before that internal advance needs a supported Splunk Enterprise
rolling-restart readiness contract or callback. Until that product contract is
available and qualified, App Framework indexer restart availability remains an
open end-to-end requirement rather than a capability completed by this gate.

## Adding a New Feature Gate

Follow these steps:

### 1. Register the gate in `pkg/config/featuregates.go`

Add a constant and an entry in `defaultFeatureGates`:

```go
const (
    MyNewFeature featuregate.Feature = "MyNewFeature"
)

var defaultFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
    // existing gates …
    MyNewFeature: {Default: false, PreRelease: featuregate.Alpha},
}
```

### 2. Guard the code path

Check the gate wherever the feature-specific logic runs:

```go
if config.DefaultMutableFeatureGate.Enabled(config.MyNewFeature) {
    // feature-specific logic
}
```

This can guard anything — a reconciler code path, a helper function, a webhook handler, an HTTP endpoint, etc.

### Example: Gating a New Controller (CRD)

When the feature gate introduces an entirely new CRD and controller, there are additional steps beyond the basic gate check. All three steps below are **mandatory** for any new CRD behind a feature gate.

#### a. Gate controller registration in `cmd/main.go`

Wrap the `SetupWithManager` call so the controller only starts when the gate is on:

```go
if config.DefaultMutableFeatureGate.Enabled(config.MyNewFeature) {
    if err = (&controller.MyNewReconciler{
        Client: mgr.GetClient(),
        Scheme: mgr.GetScheme(),
    }).SetupWithManager(mgr); err != nil {
        setupLog.Error(err, "unable to create controller", "controller", "MyNew")
        os.Exit(1)
    }
}
```

#### b. Add a validating webhook for the gated CRD group

A validating webhook **must** reject CR creation when the gate is off. Without this, users can create resources that no controller will reconcile, leading to silent failures:

```go
func (v *MyNewValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
    if !config.DefaultMutableFeatureGate.Enabled(config.MyNewFeature) {
        return nil, fmt.Errorf(
            "the MyNewFeature feature is not enabled; "+
            "set --feature-gates=MyNewFeature=true to activate")
    }
    return nil, nil
}
```

#### c. Label the CRD manifests

Every gated CRD **must** carry maturity annotations and labels in `config/crd/bases/`. These signal to operators and tooling which gate controls the CRD and its current stability level:

```yaml
metadata:
  annotations:
    splunk.com/feature-gate: MyNewFeature
    splunk.com/feature-stage: Alpha
  labels:
    splunk.com/feature-stage: alpha
```

### d. Enable the gate in tests

Validators and controllers gated behind a feature flag will reject all operations in tests unless the gate is explicitly enabled. Enable it via `SetFromMap` before your tests run:

```go
func init() {
    if err := config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{
        string(config.MyNewFeature): true,
    }); err != nil {
        panic(err)
    }
}
```

## Promoting a Gate

- **Alpha → Beta**: Change `Default: false` to `Default: true` in `featuregates.go`; update the CRD label to `beta`
- **Beta → GA**: Set `LockToDefault: true` in the `FeatureSpec`; update the CRD label to `ga`
- **GA → Removed**: Delete the constant and `FeatureSpec` entry; remove the `if` guard in `cmd/main.go`; remove the CRD annotations/labels and the validating webhook
