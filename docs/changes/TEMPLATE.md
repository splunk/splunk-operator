# Change Intent: <short-title>

## Intent
- What problem this change solves.

## Scope
- In-scope paths and behavior.
- Out-of-scope items.

## Constitution Impact
- Risk tier expectation (`low|medium|high`).
- Any policy/contract updates needed.

## Harness Coverage Plan
- `scripts/dev/spec_check.sh`
- `scripts/dev/harness_manifest_check.sh`
- `scripts/dev/doc_first_check.sh`
- `scripts/dev/commit_discipline_check.sh`
- `scripts/dev/appframework_parity_check.sh`
- `scripts/dev/pr_check.sh --fast`

## Test Plan
- Unit/integration/e2e commands that validate behavior.

## Runtime Issue Tracker Review
- `docs/testing/RUNTIME_ISSUE_TRACKER.md` updated, or
- Explicit note that no new runtime issues were observed.

## Implementation Log
- Timestamped notes for key implementation decisions.
