---
title: Logging and Events
parent: Develop & Contribute
nav_order: 5
---

# Logging & Events

## Logging

The operator uses Go's `log/slog` package for structured logging. The global logger is configured once at startup in `cmd/main.go` via `logging.SetupLogger()` and set as the default with `slog.SetDefault()`.

### Getting a Logger

| Where | How |
|---|---|
| **Controller `Reconcile`** | `logger := slog.Default().With("controller", "Name", "name", req.Name, "namespace", req.Namespace, "reconcileID", controller.ReconcileIDFromContext(ctx))` |
| **Business logic (pkg)** | `logger := logging.FromContext(ctx).With("func", "FunctionName")` |

Controllers inject the logger into context so downstream code can retrieve it:

```go
logger := slog.Default().With("controller", "Standalone", "name", req.Name, "namespace", req.Namespace, "reconcileID", controller.ReconcileIDFromContext(ctx))
ctx = logging.WithLogger(ctx, logger)
```

The `reconcileID` is a unique identifier assigned by controller-runtime to each reconcile invocation. It is automatically propagated to all downstream log calls via context, making it possible to correlate every log line produced during a single reconcile pass — even across deeply nested function calls.

### Log Levels

| Level | Use for |
|---|---|
| `Debug` | Verbose diagnostics (state dumps, intermediate values) |
| `Info` | Normal operations (reconcile start, requeue, status changes) |
| `Warn` | Recoverable issues (deprecated config, fallback behavior) |
| `Error` | Failures that need attention (API errors, broken invariants) |

Configure at runtime via `LOG_LEVEL` env var or `--log-level` flag. Values: `debug`, `info`, `warn`, `error`.

### Writing Log Messages

**Message format:** short, lowercase, past tense or present participle. Describe *what happened*, not the function name. Use **PascalCase** for CRD type names in messages — `ClusterManager`, `IndexerCluster`, `SearchHeadCluster`, `LicenseManager`, `LicenseMaster`, `ClusterMaster`, `MonitoringConsole`, `Standalone`.

```go
// Good
logger.InfoContext(ctx, "ClusterManager types not found in namespace", "error", err)
logger.InfoContext(ctx, "scaling up IndexerCluster", "replicas", replicas)

// Bad - inconsistent casing
logger.InfoContext(ctx, "clusterManager types not found")
logger.InfoContext(ctx, "scaling up indexer cluster")
```

```go
// Good
logger.InfoContext(ctx, "statefulset updated", "replicas", replicas)
logger.ErrorContext(ctx, "failed to fetch secret", "error", err, "secret", name)

// Bad - don't restate the function name or use generic messages
logger.InfoContext(ctx, "ApplyStandalone")
logger.InfoContext(ctx, "error occurred")
```

**Rules:**

1. Always use `*Context(ctx, ...)` variants (`InfoContext`, `ErrorContext`, etc.).
2. Always pass `"error", err` as a key-value pair — never as the message string.
3. Use consistent key names across the codebase. Key names **must** be `camelCase` — no spaces, no Title Case, no special characters like parentheses.

```go
// Good
logger.InfoContext(ctx, "start", "crVersion", instance.GetResourceVersion())
logger.InfoContext(ctx, "Requeued", "periodSeconds", int(result.RequeueAfter/time.Second))
logger.InfoContext(ctx, "Getting Apps list", "bucket", client.BucketName)

// Bad - spaces and inconsistent casing in keys
logger.InfoContext(ctx, "start", "CR version", instance.GetResourceVersion())
logger.InfoContext(ctx, "Requeued", "period(seconds)", int(result.RequeueAfter/time.Second))
logger.InfoContext(ctx, "Getting Apps list", "AWS S3 Bucket", client.BucketName)
```

Common key names:

| Key | Meaning |
|---|---|
| `"error"` | The `error` value |
| `"name"` | CR or resource name |
| `"namespace"` | Kubernetes namespace |
| `"controller"` | Controller name |
| `"func"` | Function name (in pkg layer) |
| `"reconcileID"` | Unique ID per reconcile pass (set by controller) |
| `"replicas"` | Replica count |
| `"phase"` | CR phase |
| `"podName"` | Pod name |
| `"appName"` | App name |
| `"bucket"` | Storage bucket name (S3/GCS/Azure) |
| `"crVersion"` | CR resource version |
| `"periodSeconds"` | Requeue delay in seconds |

4. **Don't log and return the same error.** controller-runtime automatically logs every non-nil error returned from `Reconcile()` via `log.Error(err, "Reconciler error")`. If you also log it in the business logic, the same error appears twice. Instead, wrap the error with context using `fmt.Errorf("description: %w", err)` so the single controller-runtime log line is descriptive.

### Error Wrapping

The `Apply*` functions in `pkg/splunk/enterprise/` wrap validation errors before returning them. This adds context about which resource type failed, producing a clear chain when controller-runtime logs the final error:

```
validate clustermanager spec: license not accepted, please adjust SPLUNK_GENERAL_TERMS ...
```

There are two distinct error-return patterns depending on whether the failure is terminal (user-actionable, no requeue) or transient (retryable):

**Terminal errors** (spec validation, missing required CRs, malformed secrets) — use `splcommon.NewTerminalError`:

```go
err = validateClusterManagerSpec(ctx, client, cr)
if err != nil {
    eventPublisher.Warning(ctx, EventReasonValidateSpecFailed,
        fmt.Sprintf("Spec validation failed for %s — check operator logs", cr.GetName()))
    setPhaseAndConditions(enterpriseApi.PhaseError, "Cluster Manager spec validation failed")
    return reconcile.Result{}, splcommon.NewTerminalError(EventReasonValidateSpecFailed, "Cluster Manager spec validation failed", err)
}
```

**Transient errors** (API blips, config apply failures) — use `fmt.Errorf` with `%w`:

```go
namespaceScopedSecret, err := ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkIndexer)
if err != nil {
    eventPublisher.Warning(ctx, EventReasonApplySplunkConfigFailed,
        fmt.Sprintf("Failed to apply general config for %s — check operator logs", cr.GetName()))
    setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to apply configuration")
    return result, fmt.Errorf("apply splunk config: %w", err)
}
```

**Rules:**

- Use `splcommon.NewTerminalError(reason, message, cause)` for failures that require user intervention and must not be retried automatically. The controller layer intercepts terminal errors and sets `Stalled=True` on the CR status automatically.
- Use `fmt.Errorf("description: %w", err)` with `%w` (not `%v`) for transient failures so callers can inspect the error chain with `errors.Is` / `errors.Unwrap`. The prefix should be lowercase and describe the operation, e.g. `"apply splunk config"`, `"update monitoring console configmap"`.
- Don't wrap and log the same error — pick one. Wrapping is preferred because it keeps context in the error chain and avoids double-logging.

```go
// Good — wrap and return, no explicit log needed
return result, fmt.Errorf("apply splunk config: %w", err)

// Bad — double-logged: once here, once by controller-runtime
scopedLog.Error(err, "Failed to apply splunk config")
return result, err
```

**When writing tests** that assert on wrapped errors, use `strings.Contains` rather than an exact match so the test doesn't break if the wrapping prefix changes:

```go
// Good — resilient to wrapping changes
if !strings.Contains(err.Error(), "license not accepted") {
    t.Errorf("Unexpected error: %v", err)
}

// Bad — brittle, breaks if wrapping prefix is renamed
if err.Error() != "validate standalone spec: license not accepted, ..." {
    t.Errorf("Unexpected error: %v", err)
}
```
5. Sensitive data (passwords, tokens, secrets) is automatically redacted by the handler.

### Configuration

| Env Var | Flag | Default | Description |
|---|---|---|---|
| `LOG_LEVEL` | `--log-level` | `info` | Minimum log level |
| `LOG_FORMAT` | `--log-format` | `json` | `json` or `text` |
| `LOG_ADD_SOURCE` | `--log-add-source` | `false` | Include source file/line (auto-enabled at debug) |

Flags take precedence over env vars.

---

## Kubernetes Events

Events are user-facing signals visible via `kubectl describe`. Use them for significant state changes that an operator user should see — not for internal debugging.

### How It Works

1. Controllers pass the event recorder into context:
   ```go
   ctx = context.WithValue(ctx, splcommon.EventRecorderKey, r.Recorder)
   ```
2. Business logic retrieves a publisher:
   ```go
   eventPublisher := GetEventPublisher(ctx, cr)
   ```
3. Emit events using constants from `pkg/splunk/enterprise/event_reasons.go`:
   ```go
   eventPublisher.Normal(ctx, EventReasonScaledUp,
       fmt.Sprintf("Successfully scaled %s from %d to %d replicas", cr.GetName(), old, new))
   eventPublisher.Warning(ctx, EventReasonValidateSpecFailed,
       fmt.Sprintf("Spec validation failed for %s — check operator logs", cr.GetName()))
   ```

### Event Reason Constants

All event reasons are defined as constants in `pkg/splunk/enterprise/event_reasons.go`. **Never use string literals for event reasons** — always use the `EventReason*` constants. This ensures consistency across controllers and makes reasons searchable.

**Normal reasons:**

| Constant | Value | Use for |
|---|---|---|
| `EventReasonScaledUp` | `ScaledUp` | Successful scale up |
| `EventReasonScaledDown` | `ScaledDown` | Successful scale down |
| `EventReasonClusterInitialized` | `ClusterInitialized` | Cluster first becomes ready |
| `EventReasonClusterQuorumRestored` | `ClusterQuorumRestored` | Quorum recovered |
| `EventReasonPasswordSyncCompleted` | `PasswordSyncCompleted` | Secret sync finished |
| `EventReasonStalledResolved` | `StalledResolved` | `Stalled` condition cleared — reconciliation has resumed |

**Warning reasons (common):**

| Constant | Value | Use for |
|---|---|---|
| `EventReasonValidateSpecFailed` | `ValidateSpecFailed` | CR spec validation errors |
| `EventReasonApplySplunkConfigFailed` | `ApplySplunkConfigFailed` | General config apply failures |
| `EventReasonAppFrameworkInitFailed` | `AppFrameworkInitFailed` | App framework init errors |
| `EventReasonApplyServiceFailed` | `ApplyServiceFailed` | Service create/update failures |
| `EventReasonStatefulSetFailed` | `StatefulSetFailed` | StatefulSet get failures |
| `EventReasonStatefulSetUpdateFailed` | `StatefulSetUpdateFailed` | StatefulSet update failures |
| `EventReasonDeleteFailed` | `DeleteFailed` | CR deletion failures |
| `EventReasonSecretMissing` | `SecretMissing` | Required secret not found |
| `EventReasonCertSecretMalformed` | `CertSecretMalformed` | TLS Secret missing required key |
| `EventReasonResolveQueueObjectStorageFailed` | `ResolveQueueObjectStorageFailed` | **Terminal error reason** when the referenced Queue or ObjectStorage CR is not found — sets `Stalled=True` with message `"referenced Queue or ObjectStorage CR not found"`, no requeue. Other failures from the same path (transient API errors, ConfigMap/Secret write failures) are retryable. The accompanying Warning event uses the literal reason `EnsureDefaultsFailed` |
| `EventReasonImmutableRefsModified` | `ImmutableRefsModified` | Defined for future use. Mutating `queueRef`/`objectStorageRef` after initial apply is currently rejected by the admission webhook; this event is not emitted at runtime |
| `EventReasonEmptyClusterManagerRef` | `EmptyClusterManagerRef` | ClusterManagerRef is empty during reconciliation |
| `EventReasonUpgradeCheckFailed` | `UpgradeCheckFailed` | Upgrade path validation errors |
| `EventReasonStalled` | `Stalled` | `Stalled` condition onset — manual intervention required |

See [`event_reasons.go`](https://github.com/splunk/splunk-operator/blob/main/pkg/splunk/enterprise/event_reasons.go) for the full list.

To add a new event reason: add a constant to `event_reasons.go`, then use it in the code.

### When to Use Events vs Logs

| Scenario | Log | Event |
|---|---|---|
| Reconcile start/requeue | Yes | No |
| API call failed (transient) | Yes | No |
| Spec validation failed | Yes | Yes (Warning) |
| Successful scale up/down | Yes | Yes (Normal) |
| Phase transition | Yes | Yes (Normal) |
| Security-related failure | Yes | Yes (Warning) |

### Writing Event Messages

**Reason:** always use an `EventReason*` constant — e.g. `EventReasonScaledUp`, `EventReasonValidateSpecFailed`.

**Message:** sentence case, user-friendly, include the resource name. **Never include raw `err.Error()` in events** — error details belong in logs only. Events are visible to any user with `kubectl describe` access and may leak internal paths, secret names, or stack traces. Instead, write a user-actionable summary and point to logs for details.

```go
// Good — uses constant, event gives a user-actionable summary, log has full error
logger.ErrorContext(ctx, "smartstore volume key validation failed", "error", err)
eventPublisher.Warning(ctx, EventReasonRemoteVolumeKeyCheckFailed,
    fmt.Sprintf("Remote volume key change check failed for %s — check operator logs", cr.GetName()))

eventPublisher.Normal(ctx, EventReasonScaledUp,
    fmt.Sprintf("Successfully scaled %s from %d to %d replicas", cr.GetName(), oldCount, newCount))

// Bad — string literal instead of constant
eventPublisher.Warning(ctx, "ValidationFailed", "...")

// Bad — leaking err.Error() into events
eventPublisher.Warning(ctx, EventReasonValidateSpecFailed,
    fmt.Sprintf("Invalid smartstore config: %s", err.Error()))

// Bad — too vague, no context
eventPublisher.Warning(ctx, "Error", "something went wrong")
```

**Log + Event combo for errors:**

```go
err = validateStandaloneSpec(ctx, client, cr)
if err != nil {
    // Log: full error for operator developers / debugging
    logger.ErrorContext(ctx, "spec validation failed", "error", err)

    // Event: user-actionable summary visible in kubectl describe
    eventPublisher.Warning(ctx, EventReasonValidateSpecFailed,
        fmt.Sprintf("Spec validation failed for %s — check operator logs", cr.GetName()))

    return result, err
}
```

**Rules:**

1. Always use `EventReason*` constants — never string literals for event reasons.
2. Use `Normal` for successful operations the user should know about.
3. Use `Warning` for failures or degraded states that may need user intervention.
4. Always include the resource name in the message.
5. Never pass `err.Error()` into event messages — log the full error, keep events clean.
6. Don't emit events for routine internal operations — keep the event stream actionable.

---

## Terminal Errors

Some failure conditions cannot self-heal without external intervention — for example, a pod stuck in `ImagePullBackOff`, a TLS Secret with a missing key, or a CR spec that fails validation. The operator wraps these in `splcommon.NewTerminalError(reason, message, err)` before returning from the `Apply*` function. controller-runtime treats terminal errors as non-retriable: the CR is not requeued and the reconcile loop stops.

Terminal failures are surfaced to the user via the **`Stalled` status condition** (`Stalled=True`) in addition to `phase=Error`. See [Status Conditions](../operate/CustomResources.md#status-conditions) for the full condition schema.

### How terminal errors propagate

Business logic signals a non-retriable failure by returning `splcommon.NewTerminalError(reason, message, cause)` directly — no event publisher helpers are needed. The **controller layer** intercepts any terminal error returned from `Reconcile()` and automatically calls `splcommon.UpsertStalledCondition` to set `Stalled=True` on the CR status.

**Four-layer propagation pattern:**

```go
// Layer 1 — business logic (e.g. certs.ValidateCertSecret):
//   returns splcommon.NewTerminalError(reason, message, cause) for user-actionable failures.

// Layer 2 — getXxxStatefulSet:
//   fmt.Errorf with %w preserves the TerminalError in the chain — no explicit check needed.
certMounts, err := certs.ReconcileCerts(ctx, client, cr, toCertEntries(cr.Spec.Certs))
if err != nil {
    return nil, fmt.Errorf("reconcile certs: %w", err)
}

// Layer 3 — Apply* function:
//   emits event and sets phase, then returns result + err; terminal vs transient is the controller's concern.
statefulSet, err := getStandaloneStatefulSet(ctx, client, cr)
if err != nil {
    eventPublisher.Warning(ctx, EventReasonStatefulSetFailed, "get standalone statefulset failed — check operator logs")
    setPhaseAndConditions(enterpriseApi.PhaseError, "Failed to create or update StatefulSet")
    return result, err
}

// Layer 4 — controller Reconcile:
//   returns reconcile.Result{} (no requeue) for terminal errors, result for transient ones.
if _, ok := splcommon.TerminalMessage(err); ok {
    return reconcile.Result{}, err
}
return result, err
```

**Layer responsibilities:**

- **Business logic** (`certs.ValidateCertSecret`, etc.) — returns `splcommon.NewTerminalError` for non-retryable failures. The error's `Reason` and `Message` are structured for the `Stalled` condition; full detail lives in `Err` for operator logs.
- **`getXxxStatefulSet`** — wraps errors with `fmt.Errorf("...: %w", err)`. No explicit terminal check needed: `%w` preserves the full error chain, so `TerminalError` remains detectable via `errors.As` / `splcommon.TerminalMessage` at any point up the stack.
- **`Apply*`** — emits a `Warning` event, sets `phase=Error`, returns `result, err`. Does not inspect for terminal errors — that is the controller's responsibility.
- **Controller `Reconcile`** — calls `splcommon.TerminalMessage(err)` to upsert the `Stalled` condition, then returns `reconcile.Result{}, err` for terminal errors (stops requeueing) or `result, err` for transient ones.

Callers must **not** inspect specific sub-package error types (e.g. `certs.ErrCertSecretMalformed`) — the sub-package is responsible for returning a terminal error when appropriate.

**Rules:**

1. Return `splcommon.NewTerminalError(reason, message, cause)` from business logic — the controller sets `Stalled=True` automatically.
2. Never return `reconcile.TerminalError` for transient failures (network blips, temporary API unavailability).
3. Use `splcommon.IsStalled(cr.Status.Conditions)` to check programmatically whether a CR is currently stalled.

**Stalled condition events**

After computing the new `Stalled` condition (and before persisting it), every enterprise controller calls `enterprise.EmitStalledTransitionEvents`. A Warning event fires on **every** reconcile where `Stalled=True`; the Normal recovery event fires only when the condition transitions from `True→False`:

| Condition | Event type | Reason constant |
|---|---|---|
| `Stalled=True` (every terminal reconcile) | `Warning` | `EventReasonStalled` |
| `Stalled=True` → `Stalled=False` | `Normal` | `EventReasonStalledResolved` |

These events are visible via `kubectl describe <cr-type> <name>` and can be used in alerting pipelines to detect stalls without polling the condition array. `EmitStalledTransitionEvents` is defined in `pkg/splunk/enterprise/events.go`.

**Terminal container waiting states** detected by `splctrl.CheckPodsForTerminalFailures`:

| Container `Waiting.Reason` | Cause |
|---------------------------|-------|
| `ErrImagePull` | Image pull failed (bad credentials or unreachable registry) |
| `ImagePullBackOff` | kubelet backing off after repeated pull failures |
| `InvalidImageName` | Image reference is syntactically malformed |
| `ErrInvalidImage` | Image reference resolves but is not a valid image |
| `CreateContainerConfigError` | env-var or volume references a missing ConfigMap or Secret key |
| `CreateContainerError` | OCI runtime cannot create the container |
| `RunContainerError` | OCI runtime cannot run the container (invalid entrypoint, missing binary) |
