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

| Gate                  | Default | Stage | Since   | Description                                              |
|-----------------------|---------|-------|---------|----------------------------------------------------------|
| `ValidationWebhook`   | `false` | Alpha | v3.2.0  | Centralized validation webhook server for CR admission   |
| `PostgresController`  | `false` | Alpha | ?       | PostgresCluster, PostgresClusterClass, and PostgresDatabase controllers and CRDs |

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
