---
title: "ADR-0001: CRD structure and API group choice"
parent: Architecture Decision Records
grand_parent: PostgreSQL
nav_order: 1
---

# ADR-0001: CRD structure and API group choice

- **Status:** Accepted
- **Date:** 2026-07-20
- **Deciders:** Postgres operator team (CPI)
- **Related:** CPI-2030 (API group migration), [ADR-0003](0003-cnpg-integration-and-drift-reconciliation.md), [ADR-0005](0005-postgresclusterclass-abstraction.md), [RFC summary](../rfc-summary.md)

## Context

The PostgreSQL feature adds managed PostgreSQL to the Splunk Operator. It has to
express three distinct concerns, each with a different owner and lifecycle:

1. A **platform policy / template** — sizing, versions, backup policy, pooler
   policy, provider choice — owned by the platform/operations team and shared
   across many workloads.
2. A **cluster instance** — one running PostgreSQL cluster, owned by a service
   team, that references a template and applies a small set of guard-railed
   overrides.
3. **Application databases** — the logical databases, roles, and credentials a
   service actually consumes, which may live in a shared cluster alongside other
   tenants.

The original ERD modeled this with a two-CRD split and role naming
(`_migration`/`_runtime`) that conflated cluster infrastructure with the
databases living inside it. Two questions had to be settled before 10.9:

- **How many CRDs, and where is the seam?** A single fat CRD would couple
  infrastructure provisioning to per-database lifecycle and force one RBAC
  boundary onto two teams.
- **Which API group and version?** The operator already ships the stable
  `enterprise.splunk.com/v4` API for Splunk Enterprise resources. PostgreSQL is
  independently owned and still evolving, so it needs an API boundary and
  pre-v1 version that do not couple it to the enterprise API's stability contract.

## Decision

**Three CRDs, split by concern:**

| CRD | Scope | Owner | Purpose |
| --- | --- | --- | --- |
| `PostgresClusterClass` | Cluster-scoped | Platform | Reusable template + immutable platform policy (provisioner, sizing defaults, backup/pooler policy, CNPG-specific config). |
| `PostgresCluster` | Namespaced | Service team | One PostgreSQL cluster. References a class by name; applies guard-railed overrides. |
| `PostgresDatabase` | Namespaced | Service team | Logical databases, roles, extensions, and credentials on a referenced cluster. |

**API group and version:** all three types live in the dedicated
`platform.splunk.com/v1alpha1` group (`api/platform/v1alpha1/`). They register
into their own group-version scheme alongside, but independently from, the
stable Splunk Enterprise APIs.

Key structural rules that fall out of this split:

- `PostgresClusterClass` is **cluster-scoped** because a template is a platform
  asset shared across namespaces; the two instance CRDs are **namespaced**.
- `PostgresCluster.spec.class` is a plain name reference (the class is
  cluster-scoped, so no namespace is needed) and is **immutable**
  (`self == oldSelf`).
- `PostgresDatabase.spec.clusterRef` is a `LocalObjectReference` — same-namespace
  only — and is immutable. Cross-namespace references are intentionally *not*
  supported (see Consequences).
- Config scope is strictly partitioned: platform-only policy lives under
  `spec.cnpg` on the class and **cannot** be overridden per cluster; overridable
  fields live under `spec.config` on the class and are echoed on
  `PostgresClusterSpec` with merge semantics (see [ADR-0005](0005-postgresclusterclass-abstraction.md)).

## Alternatives considered

- **Single CRD ("PostgresCluster does everything").** Rejected: it forces one
  RBAC boundary onto the platform team and the service teams, couples
  infrastructure changes (version, storage) to database churn, and makes
  multi-tenant shared clusters awkward — every database edit would reconcile the
  whole cluster.
- **Two CRDs (class + one instance CRD holding both cluster and databases).**
  This was the ERD-era shape. Rejected because a shared cluster hosting several
  service teams' databases needs *independent* lifecycles per database
  (create/delete a database without touching the cluster), and because the
  cluster and database reconcilers have genuinely different status models and
  failure modes (see [ADR-0002](0002-actuate-converge-reconcile-pattern.md)).
- **Keep `enterprise.splunk.com/v4`.** Rejected: `v4` is a stable public API
  contract governed by the core SOK API owners, while the PostgreSQL API is
  pre-v1 and owned by the developing PostgreSQL team. Keeping the types in the
  enterprise group would couple independent ownership and release timelines.
- **Use `database.splunk.com`.** Rejected in favor of `platform.splunk.com` so
  this API group can represent platform-managed services without naming the
  implementation technology in the group itself.
- **A dedicated `spec.dedicated: true` flag on PostgresCluster** to distinguish
  shared vs dedicated clusters. Rejected: shared-vs-dedicated is emergent from
  how many `PostgresDatabase` objects point at a cluster, and encoding it as
  policy would duplicate a fact the reference graph already carries. See
  [ADR-0005](0005-postgresclusterclass-abstraction.md).

## Consequences

- **Positive:** clean API ownership and RBAC seam; pre-v1 PostgreSQL types can
  evolve independently from the stable enterprise API; platform policy remains
  cluster-scoped while service instances remain namespaced; database lifecycles
  remain independent from cluster infrastructure.
- **Negative / costs:**
  - `clusterRef` being a same-namespace `LocalObjectReference` means a
    `PostgresDatabase` cannot target a cluster in another namespace. This is a
    known limitation; cross-namespace database provisioning is out of scope for
    10.9.
  - A separate API group requires its own scheme registration, RBAC rules,
    webhook rule, generated CRDs, and version lifecycle.
  - Moving groups creates new Kubernetes resource identities; no automatic
    cross-group conversion exists for previously created objects.
  - Three CRDs is more surface area for docs, samples, and validation than one.
- **Follow-ups:** CPI-2030 (completed API group migration). Cross-namespace
  `clusterRef` remains unaddressed by design.

## References

- Code: `api/platform/v1alpha1/postgresclusterclass_types.go`,
  `api/platform/v1alpha1/postgrescluster_types.go`,
  `api/platform/v1alpha1/postgresdatabase_types.go`,
  `api/platform/v1alpha1/groupversion_info.go`
- Design docs: "ERD improvement — ClusterClass CRD", "PostgreSQL on k8s + SOK"
- Jira: CPI-2030, CPI-2066
