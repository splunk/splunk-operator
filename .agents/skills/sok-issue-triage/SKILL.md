---
name: sok-issue-triage
description: Turn a Splunk Operator issue report into scope, impacted components, proposed change list, test plan, and risks. Use when asked to triage a GitHub issue, bug report, or feature request into a PR plan.
---

# SOK Issue Triage

## Overview
Convert issue context into a PR-ready plan with scope, changes, tests, and risks.

## Scope
Allowed paths:
- `.agents/**`
- `docs/**`
- `templates/**`

Forbidden paths:
- `api/**`
- `internal/**`
- `pkg/**`
- `test/**`
- `kuttl/**`
- `config/**`
- `bundle/**`
- `helm-chart/**`
- `vendor/**`

This skill should not change product code. If code changes are required, stop and hand off to the appropriate skill.

## Workflow
1. Extract the problem statement and expected behavior.
2. Identify impacted CRDs, controllers, and packages.
3. Determine the minimal scope for a safe fix.
4. Propose the change list in implementation order.
5. Define a concrete test plan.
6. Call out risks, migrations, and backward compatibility.

## Details

### 1) Parse the issue
- Capture user intent, repro steps, and current behavior.
- Identify the CR kind and any referenced fields.

### 2) Map to code
- Find the spec/status types under `api/v*/`.
- Locate controllers in `internal/controller`.
- Identify shared helpers in `pkg/splunk/enterprise` or `pkg/splunk/common`.
- Use `PROJECT` to confirm CRD kind and version mapping.
- Use `docs/agent/CRD_MAP.md` for a fast file map.
- Use `docs/agent/RECONCILE_FLOW.md` for flow and phase context.

### 3) Build a PR plan
- List files or directories to touch.
- Keep the change list ordered: schema, reconcile logic, tests, docs.

### 4) Test plan
- Prefer `make test` for unit coverage.
- Propose an integration test or minimal stub when behavior is user-visible.

## Definition of Done
- Scope, impacted components, and change list are explicit.
- Test plan is concrete and executable.
- Risks and open questions are called out.

## Key Paths
- API types: `api/v*/`
- Controllers: `internal/controller/`
- Shared logic: `pkg/splunk/enterprise`, `pkg/splunk/common`
- Docs: `docs/`
- Project mapping: `PROJECT`
- Agent docs: `docs/agent/CRD_MAP.md`, `docs/agent/RECONCILE_FLOW.md`

## Output Contract
- Changed files
- Commands run
- Results
- PR-ready summary

Use `assets/issue-triage-template.md` for the final structure and include open questions if any context is missing.
