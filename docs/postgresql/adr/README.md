# Architecture Decision Records

This directory holds the Architecture Decision Records (ADRs) for the Splunk
Operator's managed PostgreSQL feature. An ADR captures a single significant
decision — the context that forced it, the option chosen, the alternatives
rejected, and the consequences we accepted.

ADRs are immutable once **Accepted**: we don't rewrite history. If a decision
changes, add a new ADR that **supersedes** the old one and update the old one's
status to `Superseded by ADR-NNNN`.

## Index

| ADR | Title | Status |
| --- | --- | --- |
| [0001](0001-crd-structure-and-api-group.md) | CRD structure and API group choice | Accepted |
| [0002](0002-actuate-converge-reconcile-pattern.md) | Actuate/Converge reconcile pattern (component pipeline) | Accepted |
| [0003](0003-cnpg-integration-and-drift-reconciliation.md) | CNPG integration approach and drift reconciliation | Accepted |
| [0004](0004-pgbouncer-integration-model.md) | PgBouncer connection-pooler integration model | Accepted |
| [0005](0005-postgresclusterclass-abstraction.md) | PostgresClusterClass abstraction | Accepted |

See also the [PostgreSQL architecture overview](../architecture-overview.md)
(diagrams + state machines) and the [RFC summary](../rfc-summary.md).

## Adding a new ADR

1. Copy [`0000-template.md`](0000-template.md) to `NNNN-short-title.md`, using
   the next free zero-padded number.
2. Fill in Status (`Proposed` until reviewed), Date, Deciders, and Related.
3. Write Context → Decision → Alternatives considered → Consequences →
   References. Ground every claim in code paths, design docs, or Jira tickets.
4. Add a row to the index table above.
5. Open a PR; the ADR becomes **Accepted** when the team merges it.

## Format

ADRs follow a lightweight [MADR](https://adr.github.io/madr/)-style template.
Keep them short and decision-focused — link out to the code and design docs
rather than restating them.
