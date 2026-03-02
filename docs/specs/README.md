# KEP-Lite Workflow

`docs/specs/` is the system of record for KEP-lite documents that govern
non-trivial behavior changes in Splunk Operator.

## Why KEP-lite
We use a Kubernetes KEP-inspired format so design intent, rollout safety, and
validation are reviewable before implementation.

## When a KEP Is Required
A KEP is required for non-trivial changes, including:
- CRD/API changes
- reconciliation/state-machine changes
- integration/harness workflow changes
- release/compatibility behavior changes

KEP is usually not required for:
- typo fixes
- comment-only edits
- formatting-only edits
- dependency patch bumps with no behavior change

## Lifecycle
Use one status value in each KEP:
- `Draft`
- `In Review`
- `Approved`
- `Implemented`
- `Superseded`

## Naming
Use a stable file name:
- `CSPL-<ticket>-<short-kebab-title>.md`
- `GH-<issue>-<short-kebab-title>.md`

## Review and Merge Policy
- Non-trivial code PRs must include a machine-readable harness manifest in
  `harness/manifests/`.
- Each harness manifest must reference a KEP file in this directory with status
  `Approved` or `Implemented`.
- Keep KEP status and acceptance/graduation criteria current as work lands.

## Required KEP Sections
Each KEP must include:
- `Status:`
- `## Summary`
- `## Motivation`
- `## Goals`
- `## Non-Goals`
- `## Proposal`
- `## API/CRD Impact`
- `## Reconcile/State Impact`
- `## Test Plan`
- `## Harness Validation`
- `## Risks`
- `## Rollout and Rollback`
- `## Graduation Criteria`

Use [SPEC_TEMPLATE.md](SPEC_TEMPLATE.md) as the baseline.
