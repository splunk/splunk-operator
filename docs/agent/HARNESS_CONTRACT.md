# Harness Contract

This document defines the minimum harness contract for agent-driven changes.

## Inputs
- Governing spec in `docs/specs/`
- Code change set
- Target branch and CI context

## Required Local Gates
- `scripts/dev/spec_check.sh`
- `scripts/dev/pr_check.sh`

## Required CI Gate
- `.github/workflows/pr-check.yml` job `pr-check`

## Output Contract
Every implementation PR should report:
- governing spec path and status
- changed files summary
- commands run
- results and known risks

## Failure Policy
- If `spec_check.sh` fails, the change is not merge-ready.
- If harness checks fail, fix the implementation or update the spec and tests.
