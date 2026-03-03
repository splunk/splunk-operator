# Harness Contract

This document defines the minimum harness contract for agent-driven changes.

## Inputs
- Governing KEP in `docs/specs/`
- Harness manifest in `harness/manifests/`
- Code change set
- Target branch and CI context

## Required Local Gates
- `scripts/dev/spec_check.sh`
- `scripts/dev/harness_manifest_check.sh`
- `scripts/dev/risk_policy_check.sh`
- `scripts/dev/risk_label_check.sh --labels risk:<tier>` (optional local, required in PR CI)
- `scripts/dev/harness_eval.sh --suite docs/agent/evals/policy-regression.yaml`
- `scripts/dev/skill_lint.sh`
- `scripts/dev/script_sanity_check.sh`
- `scripts/dev/pr_check.sh`

## Required CI Gate
- `.github/workflows/pr-check.yml` job `pr-check`
- `.github/workflows/merge-queue-check.yml` job `merge-queue-pr-check`
- `.github/workflows/autonomy-scorecard.yml` job `autonomy-scorecard` (reporting)

## Output Contract
Every implementation PR should report:
- governing KEP path and status
- harness manifest path
- risk tier and delivery mode
- changed files summary
- commands run
- results and known risks

## Scope Contract
- Non-trivial implementation PRs must include a changed manifest under
  `harness/manifests/`.
- The manifest must define `allowed_paths`, `forbidden_paths`, and
  `required_commands`.
- Changed files must satisfy the manifest scope policy.

## Runtime Audit Contract
- Harness runs should emit artifacts under `.harness/runs/<timestamp>-<sha>/`:
  - step logs
  - `trace.tsv`
  - `summary.txt`

## Failure Policy
- If `spec_check.sh` fails, the change is not merge-ready.
- If `harness_manifest_check.sh` fails, the change is not merge-ready.
- If `risk_policy_check.sh` fails, the change is not merge-ready.
- If `risk_label_check.sh` fails in CI, the PR risk label must be corrected.
- If `harness_eval.sh` fails, governance regressions must be fixed first.
- If harness checks fail, fix the implementation or update the spec and tests.
