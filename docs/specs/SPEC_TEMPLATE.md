# <Short Title>

- ID: <CSPL-XXXX or GH-issue>
- Status: Draft
- Owners: <name(s)>
- Reviewers: <name(s)>
- Created: YYYY-MM-DD
- Last Updated: YYYY-MM-DD
- Related Links: <issue/PR/docs links>

## Summary
One paragraph describing what is changing and why.

## Motivation
Describe user/operator pain and current limitations.

## Goals
- Goal 1
- Goal 2

## Non-Goals
- Out of scope 1
- Out of scope 2

## Proposal
Describe the technical design and decision rationale.

## API/CRD Impact
- Affected CRDs/kinds:
- Schema/marker/defaulting changes:
- Compatibility notes:

## Reconcile/State Impact
- Reconciler flows touched:
- Status phase/conditions behavior:
- Idempotency and requeue behavior:

## Test Plan
- Unit:
- Integration (Ginkgo):
- KUTTL:
- Upgrade/regression:

## Harness Validation
List objective checks that must pass:
- `scripts/dev/spec_check.sh`
- `scripts/dev/harness_manifest_check.sh`
- `scripts/dev/harness_eval.sh --suite docs/agent/evals/policy-regression.yaml`
- `scripts/dev/pr_check.sh`
- Additional checks specific to this spec

## Risks
List behavioral, operational, and compatibility risks plus mitigations.

## Rollout and Rollback
Define rollout order, observability, and rollback steps.

## Graduation Criteria
- [ ] Design reviewed and status moved to `Approved`
- [ ] Implementation merged and status moved to `Implemented`
- [ ] Required harness checks pass in CI
- [ ] Docs and examples updated (if applicable)
