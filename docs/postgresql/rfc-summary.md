---
title: RFC summary
parent: PostgreSQL
nav_order: 2
---

# RFC Summary — Managed PostgreSQL for the Splunk Operator

This is a condensed, in-repo summary of the design proposal for adding managed
PostgreSQL to the Splunk Operator. It records the problem, the alternatives that
were weighed, and the approach that was chosen, and points at the
[Architecture Decision Records](adr/README.md) and
[architecture overview](architecture-overview.md) for detail.

> The full design corpus (ERD documents, spikes, and component design notes)
> lives in the team's Confluence space (CCP) and the SharePoint ERD documents.
> This page is the authoritative *summary* kept next to the code so it stays
> current with the 10.9 branch cut. Where this page and a design doc disagree,
> the code and the ADRs win.

## Problem statement

Splunk services running on Kubernetes need PostgreSQL databases, but each team
provisioning and operating PostgreSQL independently is expensive and risky:
high-availability, failover, backups, point-in-time recovery, connection
pooling, TLS, and version upgrades are all easy to get wrong. We need a way to:

- let a **platform team** define and enforce PostgreSQL policy (sizing,
  versions, backup regime, pooling, provider) once, across many workloads;
- let **service teams** get a fit-for-purpose cluster and databases with a small,
  safe set of knobs — "pick a tier and go";
- support **multi-tenant** shared clusters (several services' databases on one
  cluster) as well as dedicated clusters;
- deliver HA, backup/PITR, pooling, TLS, and upgrades without re-implementing
  them in the operator;
- integrate with the existing Splunk Operator (its API group, scheme, RBAC, and
  release cadence) rather than shipping a separate operator.

## Considered alternatives

**Build PostgreSQL management in the operator directly.** Rejected. Clustering,
failover, backup, and recovery are hard, well-solved problems; re-implementing
them would be a large, error-prone effort duplicating mature software. We chose
to build **on top of CloudNativePG (CNPG)** and keep the operator's own logic to
translation, policy, and drift reconciliation.
(See [ADR-0003](adr/0003-cnpg-integration-and-drift-reconciliation.md).)

**A single "does everything" CRD.** Rejected. It forces one RBAC boundary onto
both the platform team and service teams, couples infrastructure changes to
per-database churn, and makes shared multi-tenant clusters awkward. We chose a
**three-CRD split** — `PostgresClusterClass` (platform, cluster-scoped),
`PostgresCluster` and `PostgresDatabase` (service team, namespaced).
(See [ADR-0001](adr/0001-crd-structure-and-api-group.md).)

**A new `database.splunk.com` API group.** Rejected. A separate group means a
second scheme, second RBAC group rules, a separate conversion-webhook story, and
its own version timeline. Because the resources are shipped by and only
meaningful within the Splunk Operator, we chose to **reuse
`enterprise.splunk.com/v4`** (API group migration tracked in CPI-2030).

**Mutable classes / everything overridable.** Rejected. Mutating a shared
template silently reconfigures every cluster built on it, and letting service
teams override provisioner/pooler/backup behavior would let them opt out of
policy. We chose **immutable classes** with a two-zone config: overridable
`spec.config` defaults vs fixed `spec.cnpg` platform policy, merged with
guardrails. Policy changes ship as a new class in a catalog.
(See [ADR-0005](adr/0005-postgresclusterclass-abstraction.md).)

**Manage users/databases via direct SQL.** Rejected as the primary mechanism. It
duplicates CNPG's declarative `managed.roles` and `Database` CRs and would make
the operator own connection management, retries, and idempotency. We chose
**declarative-first**, using direct SQL only for the residual privilege grants
CNPG can't express.

**Full-object drift comparison against the live CNPG spec.** Rejected. CNPG
mutates its own spec with defaults and runtime fields, so a full comparison
drifts every reconcile and fights CNPG. We chose to compare only a **normalized,
operator-owned subset** of fields, and to use an SSA field manager where the
operator co-owns a field with CNPG (`postgresql.parameters`).

**Self-managed / sidecar PgBouncer.** Rejected. CNPG's `Pooler` CR is
first-class and integrates with CNPG's TLS and service discovery. We chose CNPG
`Pooler`s (separate RW and RO), owned and drift-reconciled like the rest of the
stack, with pooling as class policy + per-cluster enablement.
(See [ADR-0004](adr/0004-pgbouncer-integration-model.md).)

**A single generic component-manager abstraction for both controllers.**
Rejected. The cluster controller *mirrors* CNPG's phase (and can regress a
condition), while the database controller *accumulates* conditions monotonically
along a pipeline; one abstraction hid that difference. We chose a **component
pipeline** for the cluster and a **linear condition pipeline** for the database,
sharing only small helpers. (See
[ADR-0002](adr/0002-actuate-converge-reconcile-pattern.md).)

## Chosen approach

The Splunk Operator manages PostgreSQL by **orchestrating CNPG declaratively**
through three CRDs on the existing `enterprise.splunk.com/v4` group:

1. **`PostgresClusterClass`** (cluster-scoped, immutable) carries platform
   policy and overridable defaults. Service teams reference it by name.
2. **`PostgresCluster`** (namespaced) merges class defaults with guard-railed
   overrides and owns one CNPG `Cluster` plus its `Pooler`s, backup objects,
   superuser `Secret`, and connection-info `ConfigMap`. It reconciles through an
   ordered **Reconcile/Observe component pipeline** and projects its phase from
   CNPG's cluster phase.
3. **`PostgresDatabase`** (namespaced) provisions logical databases, roles, and
   credentials on a referenced cluster through a **linear condition pipeline**,
   declaring role intent for the cluster controller to arbitrate and merge-patch
   into CNPG `managed.roles`, and using direct SQL only for privilege grants.

Drift is healed via owner references and `Owns()` watches, comparing only a
normalized operator-owned subset so the operator never fights CNPG over its own
defaulted fields. Connection pooling, backups, PITR, TLS, and upgrades are
delivered through CNPG's first-class features under platform policy.

This keeps the operator's own code focused on **policy, translation, and drift
reconciliation**, inherits CNPG's maturity for the hard database problems, and
gives the platform and service teams a clean ownership seam.

## References

- [Architecture overview](architecture-overview.md) (component, reconcile-flow,
  and lifecycle diagrams)
- [ADR index](adr/README.md): ADR-0001 … ADR-0005
- Jira: CPI-2066 (this documentation), CPI-2061 (M5 Customer Enablement),
  CPI-1961 (reconcile pattern), CPI-2030 (API group migration)
- Full design corpus: Confluence space **CCP** (PostgreSQL design folder) and the
  SharePoint ERD documents.
