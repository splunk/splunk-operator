---
name: sok-reconcile-debugger
description: Debug reconcile loops, stuck status phases, or app framework pipeline behavior in the Splunk Operator. Use when a CR is not progressing, status is stuck, or reconciliation repeatedly requeues and needs root-cause analysis and a fix plan.
---

# SOK Reconcile Debugger

## Overview
Diagnose reconciliation failures and produce a clear root cause, fix, and regression test plan.

## Scope
Allowed paths:
- `api/**`
- `internal/controller/**`
- `pkg/**`
- `config/**`
- `docs/**`
- `test/**`
- `kuttl/**`
- `scripts/**`

Forbidden paths:
- `vendor/**`
- `bin/**`
- `.git/**`

If changes are needed outside the allowed paths, stop and propose a follow-up plan.

## Workflow
1. Gather context.
2. Reproduce with a minimal manifest.
3. Trace the reconcile path and status gating logic.
4. Add targeted debug logs, then remove them before final output.
5. Identify the root cause and propose the smallest safe fix.
6. Add or propose a regression test.
7. Produce a concise incident summary.

## Details

### 1) Gather context
- Capture CR kind, namespace, spec snippet, and operator version.
- Collect `kubectl describe` output and recent operator logs.
- Note whether the issue is a hot loop, terminal error, or stalled status phase.

### Quick triage commands
`kubectl get <kind> -n <ns> -o yaml`
`kubectl describe <kind> <name> -n <ns>`
`kubectl get events -n <ns> --sort-by=.lastTimestamp`
`kubectl logs -n splunk-operator deploy/splunk-operator-controller-manager -c manager --since=30m`
Use `./scripts/debug_reconcile.sh <kind> <name> <namespace>` to capture these into a single output folder.

### 2) Reproduce
- Start from an example in `docs/Examples.md` or `test/example/` and reduce it to the minimal spec that reproduces the bug.
- Prefer a local kind cluster for fast iteration when feasible.

### 3) Trace reconciliation
- Find the controller in `internal/controller` for the affected kind.
- Follow the reconcile flow in shared logic under `pkg/splunk/enterprise` and `pkg/splunk/common`.
- Identify status fields and conditions that gate progression.
- Check paused annotations (see `api/v4/*_types.go` or `api/v3/*_types.go`).
- Check `Phase` constants in `api/v4/common_types.go` for expected state transitions.
- Review predicates in `internal/controller/common/predicate.go` for reconcile triggers.
- Use `docs/agent/RECONCILE_FLOW.md` and `docs/agent/OPERATIONS.md` for guidance.

### 4) Add targeted logs
- Add temporary logs at the gate that stops progression and at any error return.
- Use consistent keys to make log filtering easy.
- Remove debug logs before final output unless explicitly requested to keep them.

### 5) Root cause and fix
- State the precise condition that prevents progression.
- Propose the smallest fix that restores the expected state transition.
- Verify idempotency and avoid new reconcile loops.

### 6) Regression test
- Add or outline a unit test near the affected logic.
- If the bug is integration-only, add a minimal test stub under `test/` or `kuttl/`.
- Prefer helper scripts when available: `scripts/dev/unit.sh`, `scripts/dev/pr_check.sh`.

## Definition of Done
- Root cause is clearly identified and reproducible.
- Fix is minimal, idempotent, and avoids new reconcile loops.
- Regression test is added or explicitly scoped as follow-up.

## Key Paths
- Controllers: `internal/controller/`
- Shared logic: `pkg/splunk/enterprise`, `pkg/splunk/common`
- Examples: `docs/Examples.md`, `test/example/`

## Output Contract
- Changed files
- Commands run
- Results
- PR-ready summary
