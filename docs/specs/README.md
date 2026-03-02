# Specification Workflow

`docs/specs/` is the system of record for design decisions that affect behavior,
APIs, reconciliation logic, test strategy, or release posture.

## When a Spec Is Required
A spec is required for non-trivial changes, including:
- CRD/API changes
- reconciliation/state-machine changes
- integration or harness workflow changes
- release/compatibility behavior changes

Spec is usually not required for:
- typo fixes
- comment-only edits
- formatting-only changes
- dependency patch bumps with no behavior change

## Lifecycle
Use one status value in each spec:
- `Draft`
- `In Review`
- `Approved`
- `Implemented`
- `Superseded`

## Review and Merge Policy
- A PR with non-trivial code changes must include a changed spec file in this folder.
- The spec must include harness validation and rollback details.
- Implementation PRs should link the governing spec and update status as work
  progresses.

## Naming
Use a stable file name:
- `CSPL-<ticket>-<short-kebab-title>.md`
- `GH-<issue>-<short-kebab-title>.md`

## Required Sections
Each spec must include:
- `Status:`
- `## Problem`
- `## Goals`
- `## Non-Goals`
- `## Proposal`
- `## Harness Validation`
- `## Risks`
- `## Rollout and Rollback`

Use [SPEC_TEMPLATE.md](SPEC_TEMPLATE.md) as the baseline.
