---
title: "ADR-0005: PostgresClusterClass abstraction"
parent: Architecture Decision Records
grand_parent: PostgreSQL
nav_order: 5
---

# ADR-0005: PostgresClusterClass abstraction

- **Status:** Accepted
- **Date:** 2026-07-20
- **Deciders:** Postgres operator team (CPI)
- **Related:** [ADR-0001](0001-crd-structure-and-api-group.md), [ADR-0003](0003-cnpg-integration-and-drift-reconciliation.md), [ADR-0004](0004-pgbouncer-integration-model.md)

## Context

Two teams interact with a managed PostgreSQL cluster with opposite goals:

- The **platform / operations team** wants to enforce policy — which provisioner,
  what backup regime, which pooler behavior, sane sizing defaults — uniformly and
  without every service team having to get PostgreSQL right.
- A **service team** wants a cluster that fits their workload with minimal
  knobs, ideally "pick a tier and go", plus a small set of safe overrides.

Kubernetes has a well-known pattern for exactly this split: the *Class* (as in
`StorageClass`, `IngressClass`) — a cluster-scoped, admin-owned template that
instances reference by name. The design questions were: what does the class
template (the cluster, or the databases?), what is fixed vs overridable, and how
do shared-vs-dedicated cluster topologies emerge?

The shipped type is `PostgresClusterClass`: it templates the **cluster**, not an
individual database. (Earlier design drafts and the CPI-2066 ticket refer to a
"DatabaseClass"; that name was never implemented — the class is the cluster-level
`PostgresClusterClass`.)

## Decision

**`PostgresClusterClass` is a cluster-scoped template + policy object** owned by
the platform team. A `PostgresCluster` references it by name and inherits its
configuration, applying only guard-railed overrides. The class splits its config
into two zones with different override rules:

| Zone | Field | Overridable per cluster? | Rationale |
| --- | --- | --- | --- |
| **Overridable defaults** | `spec.config.*` (instances, storage, version, resources, `postgresqlConfig`, `pgHBA`, pooler enable, monitoring, backup enable/schedule) | Yes, with guardrails | Service teams tune shape within policy. |
| **Platform policy** | `spec.cnpg.*` (provisioner behavior: `primaryUpdateMethod`, PgBouncer `mode`/`instances`/`config`, backup implementation, volume-snapshot/barman config) | **No** | These are correctness/capacity/security decisions the platform owns. |

**The class is immutable after creation** (`self == oldSelf` CEL rule). Rather
than mutating a class (which would retroactively change every cluster built on
it), the platform team publishes a **catalog of classes** (e.g. tiers) and
migrates workloads by pointing new clusters at a new class. This makes a class a
stable, auditable contract.

**Overrides are guard-railed on the instance, not free-form.** Merge happens in
`GetMergedConfig` (class defaults ← cluster overrides), and the admission webhook
plus CEL enforce the safety envelope against the *effective* merged values:

- `spec.class` is immutable.
- Storage can only increase (PostgreSQL cannot shrink a PVC).
- `postgresVersion` major version cannot be downgraded and cannot be removed once
  set.
- Instance count changes are validated against scaling rules (e.g. switchover
  needs ≥ 2; RO pooler needs ≥ 2).

**Shared vs dedicated is emergent, not a flag.** There is no `dedicated: true`.
A cluster is "dedicated" when exactly one service's `PostgresDatabase` points at
it and "shared" when several do — a fact the `clusterRef` graph already encodes.
Multi-tenancy is expressed by multiple `PostgresDatabase` objects referencing one
`PostgresCluster`, with role ownership arbitrated on the cluster (see the managed
roles model and [ADR-0003](0003-cnpg-integration-and-drift-reconciliation.md)).

## Alternatives considered

- **Mutable classes.** Rejected: mutating a shared template silently
  reconfigures every cluster built on it, which is surprising and hard to audit.
  Immutable classes + a class catalog make changes explicit and reviewable.
- **One flat config with no class** (put everything on `PostgresCluster`).
  Rejected: no place to enforce platform policy; every service team would have to
  re-specify (and could get wrong) backup, pooler, and provisioner settings.
- **Everything overridable.** Rejected: some fields (provisioner, pooler mode,
  backup implementation) are platform decisions; making them per-cluster
  overridable would let a service team quietly opt out of policy. Hence the
  `spec.config` (overridable) vs `spec.cnpg` (fixed) split.
- **Explicit `dedicated`/`shared` topology flag.** Rejected: redundant with the
  reference graph and would need to be kept consistent with actual usage.
- **Namespaced class.** Rejected: a template is a platform asset meant to be
  shared across namespaces, so the class is cluster-scoped (matching
  `StorageClass`/`IngressClass`). Consequence: `PostgresCluster→class` is a bare
  name; `PostgresDatabase→cluster` is same-namespace only (see
  [ADR-0001](0001-crd-structure-and-api-group.md)).

## Consequences

- **Positive:** clean platform/consumer ownership split; policy is enforced
  centrally; service teams get a small, safe override surface; immutable classes
  are auditable contracts; multi-tenancy needs no extra API surface.
- **Negative / costs:**
  - Immutability means changing policy requires publishing a new class and
    migrating clusters — more operational choreography than editing one object.
  - The two-zone config (`spec.config` vs `spec.cnpg`) plus per-field
    inheritance/merge is more complex to reason about than a flat spec, and the
    merge/override rules must be kept consistent between CEL, the webhook, and
    `GetMergedConfig`.
  - "Dedicated vs shared" being emergent means operators must inspect the
    reference graph to know a cluster's tenancy; it isn't a single readable
    field.

## References

- Code: `api/platform/v1alpha1/postgresclusterclass_types.go`
  (`PostgresClusterClassSpec`, `PostgresClusterClassConfig`, `CNPGConfig`,
  immutability + cross-field CEL rules), `pkg/postgresql/cluster/core/cluster_model.go`
  (`GetMergedConfig`, `ValidateMergedConfig`, `ValidateCrossResource`)
- Design docs: "ERD improvement — ClusterClass CRD", "Integration & Onboarding
  Guide for Splunk Operator's PostgreSQL Support"
- Jira: CPI-2066
