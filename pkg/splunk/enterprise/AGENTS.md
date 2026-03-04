# pkg/splunk/enterprise/ — Core Operator Logic

## What Lives Here
- Enterprise CR orchestration and state transitions
- App framework workflows
- Stateful resource creation helpers

## Invariants
- State transitions must be monotonic and recoverable.
- Helpers should be idempotent and tolerate partial resources.
- Respect spec defaults and validate inputs before use.

## Common Pitfalls
- Assuming resources exist without checking.
- Updating status too early (before resources are ready).
- Cross-CR dependencies without clear ordering.

## Commands
- Unit tests: `make test`
- Repo verify: `make verify-repo`

## Notes
When touching app framework paths, add/adjust tests in `test/` and
consider any KUTTL coverage under `kuttl/`.
