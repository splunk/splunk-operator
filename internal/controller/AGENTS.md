# internal/controller/ — Reconcilers

## What Lives Here
- Controller setup and reconciliation logic for CRs
- Watches, predicates, and event handling

## Invariants
- Reconcile must be idempotent and safe to retry.
- Status updates must reflect actual observed state.
- Avoid tight loops; use requeues sparingly and intentionally.

## Common Pitfalls
- Updating status without checking resource version or observed generation.
- Creating resources without proper ownership or labels.
- Missing RBAC updates when new resources are added.

## Commands
- Unit tests (envtest + ginkgo): `make test`
- Repo verify: `make verify-repo`

## Notes
Controller behavior is tightly coupled to `pkg/splunk/enterprise/` helpers.
When updating reconciliation, update or add tests in `test/`.
