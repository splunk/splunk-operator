---
name: sok-new-crd-controller
description: Create a new CRD and controller skeleton (operator-sdk), wire RBAC, add sample YAML, and add tests/docs. Use when introducing a brand-new custom resource to Splunk Operator.
---

# SOK New CRD + Controller

## Overview
Scaffold and wire a new CRD + controller end-to-end, with RBAC, samples, tests, and docs.

## Preconditions
- New CRD scope, API group/version/kind, and ownership are agreed.
- `operator-sdk` scaffolding flow is available.
- A spec/issue reference exists for non-trivial API additions.

## Scope
Allowed paths:
- `api/**`
- `internal/controller/**`
- `cmd/**`
- `config/**`
- `docs/**`
- `test/**`
- `kuttl/**`
- `bundle/**`
- `helm-chart/**`
- `scripts/**`
- `PROJECT`, `Makefile`, `go.mod`, `go.sum`

Forbidden paths:
- `vendor/**`
- `bin/**`
- `.git/**`

If changes are needed outside the allowed paths, stop and propose a follow-up plan.

## Workflow
1. Print the files you plan to change and test commands you will run.
2. Run `operator-sdk create api` (if a brand-new API) and confirm entries in `PROJECT`.
3. Implement the spec/status types and kubebuilder markers in `api/v*/`.
4. Wire the controller under `internal/controller/` and register in `cmd/main.go`.
5. Update RBAC markers and regenerate manifests: `make generate manifests` or `./scripts/verify_crd.sh`.
6. Add a sample CR under `config/samples/`.
7. Add unit tests (and an integration stub if user-visible behavior).
8. Update docs and examples.

## Notes
- Prefer scripts when available: `scripts/dev/unit.sh`, `scripts/dev/lint.sh`, `scripts/dev/pr_check.sh`.
- Ensure CRD output is updated in `config/crd/bases/` and, if tracked, `bundle/` and `helm-chart/`.

## Pass / Fail Criteria
- Pass: CRD/controller scaffolding compiles, generated artifacts are in sync, and tests/docs are present.
- Fail: registration/generation is incomplete, or validation artifacts are missing.

## Output Contract
- Changed files
- Commands run
- Results
- PR-ready summary
