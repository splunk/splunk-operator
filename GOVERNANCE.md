# Governance

## Purpose
This document defines project decision-making, ownership, and the required
spec-first + harness workflow for Splunk Operator development.

## Roles
- **Maintainers**: final technical decision makers, release owners, and policy
  stewards.
- **Contributors**: anyone proposing or implementing changes.

Maintainers are listed in `MAINTAINERS.md`.

## Decision Model
- Default model: maintainers seek technical consensus in issue/spec/PR review.
- If consensus is not reached, maintainers decide based on correctness,
  security, operability, and compatibility.

## Spec-First Policy
Non-trivial changes must start with a spec under `docs/specs/`.

Non-trivial includes:
- API/CRD or webhook behavior changes
- reconciliation/state-machine logic changes
- harness/test architecture changes
- release/upgrade behavior changes

The governing spec is part of the codebase and must evolve with
implementation.

## Spec Lifecycle
Valid status values:
- `Draft`
- `In Review`
- `Approved`
- `Implemented`
- `Superseded`

Required sections are defined in `docs/specs/README.md` and
`docs/specs/SPEC_TEMPLATE.md`.

## Harness Policy
Validation is harness-driven, not ad hoc:
- `scripts/dev/spec_check.sh` enforces spec-first policy for changed files.
- `scripts/dev/pr_check.sh` runs repository verification gates.
- CI `PR Check` runs these checks on pull requests.

Any emergency bypass must be explicit and documented in the PR with rationale
and rollback.

## Pull Request Requirements
Each non-trivial PR must include:
- governing spec path under `docs/specs/`
- harness results
- risk and rollback notes

PR templates and CODEOWNERS enforce review structure.

## Entropy Management
To keep the repo maintainable:
- remove stale specs by marking them `Superseded`
- keep harness scripts deterministic and lightweight
- update docs/process whenever policy changes

## Code of Conduct
All participants must follow `CODE_OF_CONDUCT.md`.
