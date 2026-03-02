# CSPL-4577 Agentic Harness Governance

- ID: CSPL-4577
- Status: Implemented
- Owners: Splunk Operator maintainers
- Reviewers: Splunk Operator maintainers
- Created: 2026-02-27
- Last Updated: 2026-03-02 (Spec Kit bridge + risk policy + scorecard)
- Related Links: Jira CSPL-4577, PR #1738

## Summary
Adopt a production-grade, KEP-driven harness workflow so non-trivial Splunk
Operator changes are governed by reviewed design docs, machine-readable
implementation manifests, deterministic checks, and auditable run artifacts.

## Motivation
The repository had useful agent skills and helper scripts, but governance and
execution contracts were still partly implicit. That allowed implementation to
move ahead without a strongly enforced linkage between approved design intent,
scoped edits, and deterministic harness validation.

## Goals
- Use KEP-lite docs as the design system of record for non-trivial changes.
- Require machine-readable harness manifests for non-trivial implementation PRs.
- Enforce policy, scope, and quality gates in local and CI workflows.
- Keep validation replayable and auditable for maintainers and contributors.

## Non-Goals
- Replace all existing CI workflows with a new framework.
- Introduce external policy engines or paid governance tooling.
- Require KEP files for typo-only or formatting-only edits.

## Proposal
1. Convert `docs/specs/` to a KEP-lite format aligned with Kubernetes-style
   design sections.
2. Enforce KEP structure and lifecycle checks via `scripts/dev/spec_check.sh`.
3. Add `scripts/dev/harness_manifest_check.sh` to validate machine-readable
   implementation manifests and spec linkage.
4. Add replayable policy evaluations via `scripts/dev/harness_eval.sh` and an
   evaluation corpus in `docs/agent/evals/`.
5. Add `scripts/dev/harness_run.sh` to produce run artifacts under
   `.harness/runs/` with per-step logs and summaries.
6. Add `scripts/dev/speckit_bridge.sh` so Spec Kit outputs can bootstrap KEP
   and manifest artifacts in a deterministic way.
7. Add `scripts/dev/risk_policy_check.sh` for risk-tiered governance
   (approvals, merge queue, and required validation depth).
8. Add `scripts/dev/risk_label_check.sh` so PR labels must match manifest
   `risk_tier`.
9. Add `scripts/dev/autonomy_scorecard.sh` and a CI workflow to publish
   autonomy metrics for each PR.
10. Add merge queue CI workflow and branch policy documentation.
11. Wire all checks into `scripts/dev/pr_check.sh` and CI `pr-check`.

## API/CRD Impact
- No CRD schema changes.
- Process and governance changes only.

## Reconcile/State Impact
- No controller reconcile behavior changes.
- No status phase or condition semantics changed.

## Test Plan
- Unit: not applicable for this governance-only change.
- Integration (Ginkgo): not applicable.
- KUTTL: not applicable.
- Governance/harness:
  - `scripts/dev/spec_check.sh`
  - `scripts/dev/harness_manifest_check.sh`
  - `scripts/dev/risk_policy_check.sh`
  - `scripts/dev/risk_label_check.sh --labels risk:medium`
  - `scripts/dev/harness_eval.sh --suite docs/agent/evals/policy-regression.yaml`
  - `scripts/dev/harness_run.sh --skip-pr-check`
  - `scripts/dev/autonomy_scorecard.sh --base-ref develop`
  - `scripts/dev/pr_check.sh`

## Harness Validation
- `scripts/dev/spec_check.sh`
- `scripts/dev/harness_manifest_check.sh`
- `scripts/dev/risk_policy_check.sh`
- `scripts/dev/risk_label_check.sh`
- `scripts/dev/harness_eval.sh --suite docs/agent/evals/policy-regression.yaml`
- `scripts/dev/harness_run.sh`
- `scripts/dev/autonomy_scorecard.sh`
- `scripts/dev/pr_check.sh`
- CI workflow `PR Check` (`.github/workflows/pr-check.yml`)
- CI workflow `Merge Queue Check` (`.github/workflows/merge-queue-check.yml`)
- CI workflow `Autonomy Scorecard` (`.github/workflows/autonomy-scorecard.yml`)

## Risks
- Risk: Additional workflow overhead for contributors.
  - Mitigation: templates, scripts, and clear docs keep overhead predictable.
- Risk: false positives from path/scope checks.
  - Mitigation: explicit allowed/forbidden patterns in manifests.
- Risk: process drift.
  - Mitigation: replayable eval suite and required PR gates.

## Rollout and Rollback
Rollout:
1. Merge KEP-lite docs, scripts, and CI wiring.
2. Require harness manifests for non-trivial implementation PRs.
3. Enable branch protection and merge queue with required status checks.
4. Monitor failures and tighten policies incrementally.

Rollback:
1. Set `SKIP_MANIFEST_CHECK=1` and/or `SKIP_HARNESS_EVAL=1` in emergency CI.
2. Revert harness script integration in `scripts/dev/pr_check.sh` if needed.
3. Retain KEP docs for auditability.

## Graduation Criteria
- [x] KEP-lite template and lifecycle documented in-repo.
- [x] Harness manifest and scope checks enforced for non-trivial changes.
- [x] Replayable harness policy evaluation suite added.
- [x] Harness run artifacts are generated with logs and summaries.
- [x] CI `pr-check` includes governance + harness gates.
- [x] Risk-tier policy checks are enforced for changed manifests.
- [x] Spec Kit bridge is available for planning-to-implementation handoff.
- [x] Autonomy scorecard is published for each PR.
