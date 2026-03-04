# Runtime Issue Tracker

Last updated: `2026-03-03` (UTC)

This file tracks runtime and integration issues discovered during operator
validation (unit/integration/KUTTL/kind/cluster runs).

## How To Use
- Add one row per new issue.
- Keep statuses in lifecycle order: `Open` -> `In Progress` -> `Mitigated` -> `Closed`.
- Keep evidence concrete: timestamp, namespace/resource, and log/event signature.
- Do not delete historical rows; close them with validation evidence.

## Operator Runtime Issues

| ID | Status | Issue | Evidence | Impact | Next Action |
|---|---|---|---|---|---|
| `OP-4577-001` | Open | Governance hardening rollout pending validation on mixed CI providers. | 2026-03-03 initial capture during CSPL-4577 rollout. | Temporary uncertainty across GitHub and downstream pipelines. | Track failures to closure while enabling new governance gates incrementally. |

## Test Harness Issues

| ID | Status | Issue | Evidence | Impact | Next Action |
|---|---|---|---|---|---|
| `TH-4577-001` | Open | Baseline runtime issue tracking introduced; historical issue migration pending. | 2026-03-03 governance bootstrap. | Existing open issues may not yet be represented uniformly. | Backfill high-priority open issues from active test runs. |
