---
title: SOK Code Contribution Guide
parent: Develop & Contribute
nav_order: 9
---

# SOK Code Contribution Guide

This guide explains how to design, implement, validate, and document changes to
the Splunk Operator for Kubernetes (SOK). It is for contributors working in
this repository. Use the linked source documents for complete setup,
architecture, and test procedures. This guide describes the engineering path
through them.

## Start with the Change

Before editing code, identify:

1. The behavior users see today and the behavior the change should add or fix.
2. The API, controller, package, tests, generated assets, and user documentation
   that own that behavior.
3. Compatibility requirements: existing CRs, upgrades, rollback, scale, and
   deletion behavior.
4. The narrowest validation that exercises the changed boundary.

Read the relevant package `doc.go` and nearby tests before adding code. For
business logic under `pkg/splunk/`, read
[`pkg/splunk/README.md`](../../pkg/splunk/README.md) and follow its current
package boundaries. Do not use an older structure proposal as a package map.
The checked-in package README and source tree describe the current layout.

## Design the API Contract

CRD fields are durable user-facing API. Keep JSON field names stable after
release, preserve existing meaning, and document defaults, validation,
feature-gate requirements, and unsupported combinations. Prefer additive,
backward-compatible changes. A breaking change needs an explicit migration
plan and the required API owner review.

### Choose field types intentionally

For every new CRD field, explicitly declare whether it is optional or required.
Add `// +optional` for an optional field or
`// +kubebuilder:validation:Required` for a required field. Do not rely on the
absence of `+optional` to imply that a field is required.

For optional fields, decide whether the implementation must distinguish
omission from an explicit zero value. A pointer gives that distinction:

```go
// +optional
// +kubebuilder:validation:Minimum=1
Replicas *int32 `json:"replicas,omitempty"`
```

Here `nil` means omitted and a pointer to `0` is an explicit zero that can be
rejected. Use a value type when the zero value is valid and omission does not
need separate meaning. Maps and slices can represent omission with `nil`,
though an empty and nil value may still need deliberate API semantics.

For every optional field:

- Add `// +optional`.
- Normally use `omitempty` in the JSON tag so unset zero values are omitted.
- Use a pointer when defaulting or validation must distinguish omitted from
  explicitly set zero/false/empty.
- Check existing defaults and conversion code before changing a value field to
  a pointer. Go call sites must handle `nil`.

Do not copy the field-marker table from older notes without checking the field's
semantics. In particular, `+optional` and JSON `omitempty` describe different
layers. Both are normally needed for an optional field.

### Put validation at the right layer

Use the simplest layer that can express the rule:

1. **Kubebuilder schema markers** for field-local constraints such as minimum,
   maximum, pattern, length, requiredness, and defaults.
2. **CEL (`XValidation`)** for declarative cross-field rules that Kubernetes
   can evaluate from the object itself.
3. **Webhook validation** for rules needing Go logic, old and new objects,
   Kubernetes lookups, or domain-specific error handling.

Keep validation deterministic and fast. Avoid external network calls in
admission. Return field-scoped errors with actionable messages. Follow
[`Webhook Development`](WebhookDevelopment.md) and
[`Validation Webhook`](../reference/ValidationWebhook.md).

### Preserve compatibility and regenerate outputs

Treat changes to released API fields, defaults, enums, validation, and
serialization as compatibility changes. Check how older manifests behave and
whether conversion, defaulting, or update validation changes the result. After
editing API types or markers, regenerate the manifests and code, then build
the OLM bundle:

```bash
make manifests generate
make bundle
```

Review the resulting CRD schemas and generated DeepCopy code. Never hand-edit
generated CRDs, RBAC, webhook manifests, or DeepCopy methods. API version and
schema changes require owner sign-off before merge.

## Place Code at Its Owning Boundary

Keep each layer focused and respect the dependency direction documented in
[`pkg/splunk/README.md`](../../pkg/splunk/README.md):

| Change | Usual owner |
|---|---|
| CRD API shape | `api/<group>/<version>/` |
| Controller wiring and Kubernetes reconciliation entry point | `internal/controller/<api-group>/` |
| Per-CR reconciliation | `pkg/splunk/reconcile/<cr>/` |
| Shared multi-step lifecycle workflow | `pkg/splunk/workflow/<domain>/` |
| Splunk configuration generation | `pkg/splunk/splunkconfig/` |
| Kubernetes object construction without I/O | `pkg/splunk/resources/` |
| Kubernetes reads, writes, diff/merge, and finalizers | `pkg/splunk/k8sops/` |
| External service client calls | `pkg/splunk/client/<system>/` |
| Admission validation | `pkg/splunk/validation/` |

Keep controllers focused on Kubernetes reconciliation and delegation. Put
reusable state transitions and external-system behavior in the package that
owns that domain. Add or update `doc.go` when a package's responsibility or
allowed imports change. If the existing code is still in the legacy
`pkg/splunk/enterprise/`, follow the README's migration guidance rather than
creating a new dependency in the legacy package by default.

## Build Reconciliation That Converges

The controller repeatedly compares desired state from the CR with observed
Kubernetes and Splunk state. A reconcile must be safe to run again after a
partial failure, process restart, status conflict, or duplicate event.

- Make operations idempotent. Derive names and values deterministically where
  possible. Create-or-update the desired state and preserve fields owned by
  users or other controllers.
- Observe the source of truth when needed. Do not depend on status as the only
  record of state that cannot be reconstructed after restore.
- Watch every dependency whose change should trigger convergence. Check
  predicates and ownership watches when relying on events to re-run reconcile.
- Avoid several successive writes to the watched CR in one reconcile. If a CR
  update is a workflow step, persist it and return so the next pass reads the
  latest resource version.
- Keep external calls bounded by the reconcile context and handle timeouts,
  cancellation, conflicts, and partial completion explicitly.
- Coordinate ownership when multiple controllers can affect the same object.
  Do not permit concurrent writers without a clear serialization strategy.

### Choose the reconcile return deliberately

Use the return path that matches the reason another pass is needed. In this
repository, controller-runtime is pinned in `go.mod`. Confirm semantics against
that version when changing controller behavior.

| Situation | Return pattern | Effect |
|---|---|---|
| Temporary API, network, or dependency failure | Return a wrapped non-terminal error | controller-runtime retries with rate limiting/backoff |
| Permanent invalid input or unrecoverable user action required | Set an actionable condition and return the repository's terminal error (`splcommon.NewTerminalError`) | No automatic retry until a new event. The controller records the failure. |
| Waiting for an external process or a known polling interval | `ctrl.Result{RequeueAfter: delay}, nil` | Reconcile after the chosen delay. The rate limiter is not used for this wait. |
| This pass completed, or a watched object was updated | `ctrl.Result{}, nil` | Wait for a watch event |
| No watch exists for a state that must be polled | `ctrl.Result{RequeueAfter: delay}, nil` | Poll at a deliberate interval |

Do not use `Requeue: true` as an immediate retry mechanism. It uses the
rate-limited error retry path and can consume queue capacity under repeated
success-with-requeue behavior. Use an error for a transient failure and
`RequeueAfter` when the controller should wait. Do not combine a non-nil error
with a result expecting the delay to be honored. The error retry path takes
precedence. `RequeueAfter` is unnecessary when a watch event reliably triggers
the next pass.

## Make Failures Understandable

Set status and conditions so a user can understand whether SOK is progressing,
ready, or blocked, and what they can do next. Keep status reconstructible from
the CR and observed resources wherever possible. Use events for user-visible
milestones and actionable failures. Use logs for technical detail.

- Use the established condition types, phases, and event-reason constants for
  the resource you are changing.
- Wrap retryable errors with operation context using `%w`. Do not log and then
  return the same error when controller-runtime will log it.
- Use the repository's terminal-error path for permanent, user-actionable
  failures and ensure the condition explains the correction.
- Never put passwords, tokens, private keys, or sensitive configuration in
  logs, status, events, error strings, or test output.
- Follow [`Logging & Events`](LoggingAndEvents.md) for structured fields,
  redaction, event wording, and error handling.

## Test the Changed Behavior

Choose tests at the boundary of the change and include failure and recovery
paths, not only the happy path.

- API types and validation: test optional, zero, default, invalid, and
  cross-field cases as applicable.
- Resource builders and workflows: test deterministic output, updates,
  partial failure, idempotency, and preservation of user-owned state.
- Controllers: test watches/filters, status and conditions, terminal versus
  retryable errors, and requeue behavior.
- Cross-resource behavior: add controller or integration coverage when the
  behavior depends on real Kubernetes API semantics or multiple components.
- Persisted resources: cover upgrade, rollback, scale, and deletion paths when
  the change affects them.

Use the test strategy and commands in
[`Integration Testing`](IntegrationTesting.md). Unit tests and envtest do not
require a live customer cluster. Run cluster-backed tests only in an authorized
test environment with the required configuration and credentials.

## Update Documentation and Examples

When user-visible behavior changes, update the matching user guide, API
examples, troubleshooting material, and developer docs in the same change when
practical. Describe the field users set, defaults and constraints, feature-gate
state, resulting resources, readiness signals, and recovery steps. Keep samples
valid for the CRD version they target.

## Preserve Copyright and License Notices

Use the [Copyright and License Notices](CopyrightAndLicenseNotices.md) reference
for the source header template and rules for generated files, existing
attribution, and third-party notices.

## Prepare the Change for Review

Follow [`Development Setup`](DevelopmentSetup.md) to configure the local
toolchain and [`Contributing`](Contributing.md) for the repository contribution
workflow.
Use `make help` and the `Makefile` as the source of truth for available
commands. The standard pre-MR validation loop is:

```bash
make fmt vet
make test
make build
```

After changing API types under `api/enterprise/**`, also run `make manifests
generate` and `make bundle`. Select focused checks for the changed
boundary, then run the broader required checks. Report what passed and what
could not be run, with the missing prerequisite. Do not claim checks that were
not run.

Keep changes small enough to review, include tests and documentation, and
explain API compatibility and operational effects in the merge request. Do not
commit or push unless the task explicitly asks for it.

## Reference Documents

- [`Development Workflow`](../../DEVELOPMENT.md)
- [`Development Setup`](DevelopmentSetup.md)
- [`Architecture`](Architecture.md)
- [`Package Architecture`](../../pkg/splunk/README.md)
- [`Contributing`](Contributing.md)
- [`Webhook Development`](WebhookDevelopment.md)
- [`Feature Gates`](FeatureGates.md)
- [`Logging & Events`](LoggingAndEvents.md)
- [`Integration Testing`](IntegrationTesting.md)
- [`Custom Resources`](../operate/CustomResources.md)
