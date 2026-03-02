# CSPL-4577 Agentic Harness Governance

- ID: CSPL-4577
- Status: Implemented
- Owners: Splunk Operator maintainers
- Reviewers: Splunk Operator maintainers
- Created: 2026-02-27
- Last Updated: 2026-03-02 (tool portability update)
- Related Links: Jira CSPL-4577, PR #1738

## Problem
The repo had useful agent skills and scripts, but no hard requirement for
spec-first changes. This allowed implementation to move ahead without a durable
design record or a deterministic harness gate tied to the design intent.

## Goals
- Enforce a spec-first workflow for non-trivial changes.
- Keep approved specs versioned in-repo as part of deliverables.
- Make harness validation mandatory in PR checks.
- Keep the workflow lightweight enough for daily development.

## Non-Goals
- Replace existing CI pipelines with a new system.
- Introduce external policy engines.
- Require specs for typo-only or formatting-only edits.

## Proposal
1. Add `docs/specs/` with a required template and lifecycle states.
2. Add `scripts/dev/spec_check.sh` that:
   - identifies non-trivial changed paths,
   - requires at least one changed spec file for such changes,
   - validates required spec sections.
3. Wire `spec_check.sh` into `scripts/dev/pr_check.sh` and PR workflow.
4. Update governance and contribution docs to make this process explicit.
5. Update PR templates so every PR links spec and harness evidence.
6. Ensure `spec_check.sh` works in runner environments that may not have `rg`
   installed by using a `grep` fallback.

## Harness Validation
- `scripts/dev/spec_check.sh`
- `scripts/dev/pr_check.sh`
- CI workflow `PR Check` must pass on `develop` and `main` pull requests.

## Risks
- Risk: Increased friction for small functional changes.
  - Mitigation: narrow spec requirement to non-trivial paths only.
- Risk: Spec quality could degrade into boilerplate.
  - Mitigation: enforce required sections and reviewer ownership.
- Risk: Process drift over time.
  - Mitigation: include governance text and PR template prompts.

## Rollout and Rollback
Rollout:
1. Merge governance/docs/script/CI updates together.
2. Require spec references in new PRs.
3. Track misses and tune path matching as needed.

Rollback:
1. Set `SKIP_SPEC_CHECK=1` in CI as emergency bypass.
2. Revert `spec_check.sh` integration in `pr_check.yml` if necessary.
3. Keep spec docs intact for auditability.

## Acceptance Criteria
- [x] Spec template and lifecycle documented in-repo.
- [x] Harness enforces spec-first behavior for non-trivial changes.
- [x] Governance and contribution docs describe the policy.
- [x] PR templates require spec and harness evidence.
