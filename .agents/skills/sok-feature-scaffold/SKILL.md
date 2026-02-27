---
name: sok-feature-scaffold
description: Add or change Splunk Operator behavior by introducing a new field in a CRD spec/status, wiring it into reconciliation, and updating tests and docs. Use for changes to Standalone, IndexerCluster, SearchHeadCluster, ClusterManager, LicenseManager, MonitoringConsole, or shared CRD config. Do not use for pure refactors, dependency bumps, or formatting-only changes.
---

# SOK Feature Scaffold

## Overview
Implement CRD-driven features end-to-end with code, tests, and docs in this repository.

## Scope
Allowed paths:
- `api/**`
- `internal/controller/**`
- `pkg/**`
- `config/**`
- `docs/**`
- `test/**`
- `kuttl/**`
- `bundle/**`
- `helm-chart/**`
- `scripts/**`
- `Makefile`, `README.md`, `PROJECT`, `go.mod`, `go.sum`

Forbidden paths:
- `vendor/**`
- `bin/**`
- `.git/**`

If changes are needed outside the allowed paths, stop and propose a follow-up plan.

## Workflow
1. Print the files you plan to change and the test commands you will run before editing.
2. Identify the target CRD kind and API version. Confirm the mapping in `PROJECT` and `docs/agent/CRD_MAP.md`.
3. Locate the API types under `api/v*/` and update the spec/status struct.
4. Add JSON tags, `omitempty` rules, and kubebuilder markers consistent with adjacent fields.
5. Update any defaulting or validation logic that applies to the new field.
6. Regenerate CRD/RBAC artifacts via the operator-sdk workflow.
7. Wire the field into reconciliation with idempotent logic.
8. Update or add unit tests, and add an integration test stub when relevant.
9. Update docs and examples to expose the new field.
10. Produce a PR-ready summary with tests and risks.

## Implementation Details

### 1) Find the right API types
- `PROJECT` is the source of truth for kind and version mapping.
- Use `docs/agent/CRD_MAP.md` for fast navigation to types, controllers, and enterprise logic.
- Prefer the latest stable API version (typically `api/v4`).
- Legacy kinds still use `api/v3` (legacy cluster manager and legacy license manager).
- Use `rg "type .*Spec" api -g "*_types.go"` to locate the spec struct.
- If the field is shared across CRDs, check `api/v4/common_types.go` (and `api/v3/common_types.go` for legacy kinds).

### 2) Schema, CRD, and RBAC generation (operator-sdk workflow)
- Use `operator-sdk create api` when introducing a new API or controller (scaffolding).
- Add the field with a clear JSON name and `omitempty` as appropriate.
- Follow nearby kubebuilder markers for validation, defaults, and list/map behavior.
- Regenerate code and manifests with the repo targets (operator-sdk scaffolding uses controller-gen under the hood).
`make generate` for deepcopy code.
`make manifests` for CRDs and RBAC.
Run `make bundle` to refresh `bundle/manifests/*` and `helm-chart/splunk-operator/crds` when bundle or Helm CRDs are tracked.
- For verification, use `./scripts/verify_crd.sh` and optionally `./scripts/verify_bundle.sh` or `make verify VERIFY_BUNDLE=1`.
- If you add new RBAC needs, update kubebuilder RBAC markers in the controller and re-run `make manifests` to refresh `config/rbac/role.yaml`.

### 3) Reconcile wiring
- Locate the controller in `internal/controller` and shared logic in `pkg/splunk/enterprise` or `pkg/splunk/common`.
- Read the new field from the spec and apply it in a single, idempotent reconciliation path.
- Update status only when the desired state is reached and avoid hot-looping.

### 4) Tests
- Add or update unit tests near the logic you touched (often under `internal/controller` or `pkg/splunk/*`).
- If the behavior is user-visible or multi-resource, add a minimal integration test stub in `test/` or `kuttl/` to document coverage intent.
- Prefer helper scripts when available: `scripts/dev/unit.sh`, `scripts/dev/lint.sh`, `scripts/dev/pr_check.sh`.

### 5) Docs
- Update `docs/CustomResources.md` for spec fields.
- Update any feature-specific doc under `docs/` and add an example manifest if needed.

## Definition of Done
- CRD/schema generation is updated and verified.
- Reconcile logic is idempotent and status updates are gated.
- Tests added/updated for the new behavior.
- Docs/examples reflect the new field or behavior.

## Assets
- Use `assets/pr-template.md` for the PR summary format.
- Use `assets/crd-change-checklist.md` as a guardrail for CRD edits.

## Key Paths
- API types: `api/v*/` (e.g. `api/v4/*_types.go`)
- Controllers: `internal/controller/`
- Shared logic: `pkg/splunk/enterprise`, `pkg/splunk/common`
- CRDs: `config/crd/bases/`
- RBAC output: `config/rbac/role.yaml`
- Bundles: `bundle/manifests/`, `helm-chart/splunk-operator/crds`
- Docs: `docs/CustomResources.md`, `docs/Examples.md`

## Repo Map (Common Cases)
- Standalone: `api/v4/standalone_types.go`, `internal/controller/standalone_controller.go`, `pkg/splunk/enterprise/standalone.go`
- IndexerCluster: `api/v4/indexercluster_types.go`, `internal/controller/indexercluster_controller.go`, `pkg/splunk/enterprise/indexercluster.go`
- SearchHeadCluster: `api/v4/searchheadcluster_types.go`, `internal/controller/searchheadcluster_controller.go`, `pkg/splunk/enterprise/searchheadcluster.go`
- ClusterManager: `api/v4/clustermanager_types.go`, `internal/controller/clustermanager_controller.go`, `pkg/splunk/enterprise/clustermanager.go`
- LicenseManager: `api/v4/licensemanager_types.go`, `internal/controller/licensemanager_controller.go`, `pkg/splunk/enterprise/licensemanager.go`
- MonitoringConsole: `api/v4/monitoringconsole_types.go`, `internal/controller/monitoringconsole_controller.go`, `pkg/splunk/enterprise/monitoringconsole.go`
- Legacy v3 control-plane types/controllers: search under `api/v3/`, `internal/controller/`, and `pkg/splunk/enterprise/`

## Output Contract
- Changed files
- Commands run
- Results
- PR-ready summary
