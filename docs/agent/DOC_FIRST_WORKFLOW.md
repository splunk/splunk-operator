# Doc-First Workflow

For implementation-gated changes, this repository requires a change-intent file under `docs/changes/`.

## Workflow
1. Create a change-intent file:
   - `scripts/dev/start_change.sh "<topic>"`
2. Fill all required sections.
3. Implement code/tests.
4. Run governance gates and include evidence in PR.

## Gate
- `scripts/dev/doc_first_check.sh`

## Required Sections
- `## Intent`
- `## Scope`
- `## Constitution Impact`
- `## Harness Coverage Plan`
- `## Test Plan`
- `## Runtime Issue Tracker Review`
- `## Implementation Log`
