# Branch and Merge Queue Policy

This document defines repository settings needed to enforce harness governance.

## Protected Branches
Apply to `develop` and `main`:
- Require pull request before merge.
- Require approvals (`2` recommended baseline).
- Require review from Code Owners.
- Dismiss stale approvals on new commits.
- Require conversation resolution.
- Require linear history (recommended).

## Required Status Checks
Require these checks before merge:
- `pr-check`
- `check-formating`
- `unit-tests`
- `Analyze (go)`
- `Analyze (python)`
- `ContributorLicenseAgreement`
- `CodeOfConduct`
- `Semgrep Scanner`
- `FOSSA-scanner`

## Merge Queue
Enable merge queue on protected branches and require:
- `.github/workflows/merge-queue-check.yml` job `merge-queue-pr-check`
- Up-to-date branch before merging

## Risk-Tier Rules
Risk tier is declared in harness manifest and validated by
`scripts/dev/risk_policy_check.sh`.

- `low`:
  - `human_approvals_required >= 0`
  - `auto_merge_allowed` may be `true`
- `medium`:
  - `human_approvals_required >= 1`
  - `merge_queue_required: true`
  - `auto_merge_allowed: false`
- `high`:
  - `human_approvals_required >= 2`
  - `merge_queue_required: true`
  - `auto_merge_allowed: false`
  - command list includes deeper validation (unit/integration)
