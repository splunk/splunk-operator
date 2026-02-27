---
name: sok-ci-fixer
description: Analyze CI failures, map them to local repro commands, and propose fixes. Use when CI logs or pipeline failures need a local reproduction and patch.
---

# SOK CI Fixer

## Overview
Turn CI failures into a local repro and a minimal fix with a validation plan.

## Scope
Allowed paths:
- `scripts/**`
- `test/**`
- `kuttl/**`
- `config/**`
- `api/**`
- `internal/**`
- `pkg/**`
- `Makefile`, `go.mod`, `go.sum`

Forbidden paths:
- `vendor/**`
- `bin/**`
- `.git/**`

If changes are needed outside the allowed paths, stop and propose a follow-up plan.

## Workflow
1. Summarize the CI failure (job name, step, error).
2. Map to a local command (prefer `scripts/dev/pr_check.sh` or `./scripts/verify_repo.sh`).
3. Reproduce locally if possible and capture the failing output.
4. Implement the smallest safe fix.
5. Re-run the local repro command.

## Commands
- PR gate: `scripts/dev/pr_check.sh`
- Repo verify: `./scripts/verify_repo.sh`

## Definition of Done
- CI failure is reproducible locally or explained why not.
- Fix is minimal and validated with a local repro command.
- Regression risk is noted.

## Output Contract
- Changed files
- Commands run
- Results
- PR-ready summary
