# Engineering Constitution

This is the always-on engineering contract for Splunk Operator for Kubernetes (SOK).

If a change conflicts with this constitution, either:
1. Update this document with a technical justification, or
2. Redesign the change to comply.

## Product Goal

Deliver a Kubernetes operator that keeps Splunk deployments safe, predictable,
and auditable across install, upgrade, and steady-state operations.

## Security Baselines
- Default to least privilege in RBAC and pod/container security context.
- Never log secrets, credentials, session tokens, or private keys.
- Validate and constrain user-provided config before using it in reconcile actions.
- Prefer immutable image references for test and production deployments.

## Reconcile and State Management
- Reconcile paths must be idempotent.
- Status transitions must be explicit and explain waiting/failure conditions.
- Transient dependency failures should use retry/backoff, not terminal phase flips.
- Controller changes must include regression tests for phase/condition behavior.

## CRD and Compatibility Rules
- CRD/API changes require an approved KEP in `docs/specs/`.
- CRD schema and generated manifests must be regenerated with project tooling.
- Backward compatibility expectations and upgrade behavior must be documented.

## Test and Harness Requirements
- Non-trivial changes require harness manifest + policy checks.
- Required baseline checks are executed through `scripts/dev/pr_check.sh`.
- Integration and KUTTL coverage must be updated when runtime behavior changes.
- App framework changes must satisfy parity gate requirements.

## Documentation Requirements
- Implementation-heavy changes must start with `docs/changes/*.md`.
- User-facing behavior changes require docs updates under `docs/`.
- Governance/process changes require updates under `docs/agent/` or `docs/engineering/`.

## Runtime Issue Governance
- Newly observed runtime/test issues must be recorded in
  `docs/testing/RUNTIME_ISSUE_TRACKER.md` before marking work complete.
- Issue status lifecycle is mandatory: `Open` -> `In Progress` -> `Mitigated` -> `Closed`.
- Closing an issue must include evidence (PR, commit, or validated test run).
