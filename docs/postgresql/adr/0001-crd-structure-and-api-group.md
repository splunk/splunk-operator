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
- **Which API group and version?** The operator already ships an established
  `enterprise.splunk.com` group (currently at `v4`) for its Splunk Enterprise
  resources. A greenfield group such as `database.splunk.com` was possible.

## Decision

**Three CRDs, split by concern:**

| CRD | Scope | Owner | Purpose |
| --- | --- | --- | --- |
| `PostgresClusterClass` | Cluster-scoped | Platform | Reusable template + immutable platform policy (provisioner, sizing defaults, backup/pooler policy, CNPG-specific config). |
| `PostgresCluster` | Namespaced | Service team | One PostgreSQL cluster. References a class by name; applies guard-railed overrides. |
| `PostgresDatabase` | Namespaced | Service team | Logical databases, roles, extensions, and credentials on a referenced cluster. |

**API group and version:** all three types live in the existing
`enterprise.splunk.com/v4` group (`api/enterprise/v4/`,
`api/enterprise/v4/groupversion_info.go`), not a new group. They register into
the operator's existing scheme alongside the Splunk Enterprise types.

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
- **New `database.splunk.com` API group.** A clean group would read well in
  isolation, but it would mean a second scheme, second set of RBAC group rules,
  second conversion-webhook story, and a separate versioning timeline to
  maintain. The resources are shipped by, and only meaningful within, the Splunk
  Operator, so reusing `enterprise.splunk.com` keeps one scheme, one RBAC group,
  and one release cadence. **Chosen: reuse `enterprise.splunk.com/v4`** (tracked
  in CPI-2030, which migrated the types onto the shared group).
- **A dedicated `spec.dedicated: true` flag on PostgresCluster** to distinguish
  shared vs dedicated clusters. Rejected: shared-vs-dedicated is emergent from
  how many `PostgresDatabase` objects point at a cluster, and encoding it as
  policy would duplicate a fact the reference graph already carries. See
  [ADR-0005](0005-postgresclusterclass-abstraction.md).

## Consequences

- **Positive:** clean RBAC seam (platform owns classes cluster-wide; service
  teams own instances in their namespace); independent database lifecycles on a
  shared cluster; infrastructure changes don't churn database state; one scheme
  and one release cadence by staying on `enterprise.splunk.com`.
- **Negative / costs:**
  - `clusterRef` being a same-namespace `LocalObjectReference` means a
    `PostgresDatabase` cannot target a cluster in another namespace. This is a
    known limitation; cross-namespace database provisioning is out of scope for
    10.9.
  - Reusing `v4` couples the Postgres types' served version to the enterprise
    group's version timeline. A future breaking change to the Postgres types
    would ride the enterprise group's version bump.
  - Three CRDs is more surface area for docs, samples, and validation than one.
- **Follow-ups:** CPI-2030 (completed API group migration). Cross-namespace
  `clusterRef` remains unaddressed by design.

## References

- Code: `api/enterprise/v4/postgresclusterclass_types.go`,
  `api/enterprise/v4/postgrescluster_types.go`,
  `api/enterprise/v4/postgresdatabase_types.go`,
  `api/enterprise/v4/groupversion_info.go`
- Design docs: "ERD improvement — ClusterClass CRD", "PostgreSQL on k8s + SOK"
- Jira: CPI-2030, CPI-2066
