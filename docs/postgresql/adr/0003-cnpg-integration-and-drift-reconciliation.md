# ADR-0003: CNPG integration approach and drift reconciliation

- **Status:** Accepted
- **Date:** 2026-07-20
- **Deciders:** Postgres operator team (CPI)
- **Related:** [ADR-0002](0002-actuate-converge-reconcile-pattern.md), [ADR-0004](0004-pgbouncer-integration-model.md), [RFC summary](../rfc-summary.md)

## Context

The operator does not implement PostgreSQL clustering, failover, backup, or
recovery itself. Those are hard, well-solved problems, and
[CloudNativePG (CNPG)](https://cloudnative-pg.io/) is a mature Kubernetes
operator that already does them. The design question is *how* the Splunk
Operator should sit on top of CNPG:

- Should the operator talk to PostgreSQL directly (SQL) or drive CNPG's declarative
  API? Both? Where is the line?
- Who owns the CNPG `Cluster` object and its lifecycle relative to our
  `PostgresCluster`?
- CNPG (and users, and other controllers) can mutate the CNPG `Cluster` spec
  out of band. How do we keep the live object matching what our class + spec
  imply, without fighting CNPG over the fields *it* legitimately owns?

## Decision

**CNPG is the sole provisioner, driven declaratively.** `PostgresClusterClass`
requires `spec.provisioner: postgresql.cnpg.io` and a `spec.cnpg` block; the
combination is enforced by CEL validation on the class. The operator translates
the merged class+cluster config into a `cnpgv1.Cluster` (plus `Pooler`,
`ScheduledBackup`, and barman `ObjectStore`) and lets CNPG do the actual
database work.

**One `PostgresCluster` owns exactly one CNPG `Cluster`,** via a controller
owner reference. The reconciler `Owns(&cnpgv1.Cluster{})` (and the Pooler,
ScheduledBackup, Secret, ConfigMap, and — when the CRD is present — the barman
ObjectStore), so out-of-band edits or deletion of most owned objects re-enqueue
the `PostgresCluster` and are healed. The Secret is the exception: its watch
predicate only reacts to deletion (`secretPredicator` in
`postgrescluster_controller.go`), and the secret component only creates it when
missing — it never rewrites an existing secret's data — so out-of-band edits to
Secret **content** are not drift-reconciled. `status.provisionerRef` on the
`PostgresCluster` is the bridge pointing at the CNPG object.

**Declarative-first, SQL only where CNPG can't express it.** User and database
lifecycle go through CNPG's own declarative surfaces:

- **Roles** via CNPG `managed.roles`, arbitrated by the `PostgresCluster`
  controller — see the ownership model in
  [ADR-0005](0005-postgresclusterclass-abstraction.md) and the managed-roles flow. The
  managed-roles component collects role intent across all `PostgresDatabase`
  objects for the cluster, computes the full desired role list, and applies it
  to the CNPG `Cluster` with a single merge patch
  (`reconcileManagedRoles` in `managed_roles_model.go`). This is plain
  optimistic-concurrency patching, not Server-Side Apply — individual
  `PostgresDatabase` controllers do not own separate SSA slices of
  `managed.roles`.
- **Databases** via CNPG `Database` CRs owned by the `PostgresDatabase`.
- **Direct SQL** (over the cluster's superuser secret — either a
  `spec.passwordConfig.superuserExternalSecretRef` or the operator-derived
  `<cluster>-secret`) is used **only** for privilege grants that CNPG's
  declarative model cannot express — e.g. granting the per-database `_rw` role
  its object privileges. This keeps the SQL surface as small as possible.

**Phase projection, not self-diagnosis.** The cluster's status phase is a
translation of CNPG's own cluster phase into our vocabulary
(`Healthy → Ready`; `FirstPrimary`/`CreatingReplica` → `Provisioning`;
`Switchover`/`Upgrade`/`ApplyingConfiguration`/restart/promotion → `Configuring`;
`FailOver` → `Pending`; `Unrecoverable`/`WaitingForUser`/plugin/image errors →
`Failed`; empty → `Pending`). We do not re-derive cluster health from pod state — CNPG already
computes it.

**Drift reconciliation on a normalized subset.** The operator rebuilds the
desired CNPG spec every reconcile and compares it against the live object using
**normalized structs that contain only the fields the operator sets**
(`normalizedCNPGClusterSpec`, `normalizedCNPGPoolerSpec`,
`normalizedBackupSpec`, `normalizedPluginSpec` in
`pkg/postgresql/cluster/core/types.go`). CNPG-injected defaults (e.g.
`targetTLI`) are deliberately **excluded** from the normalized form so they
never register as false-positive drift. When the normalized desired and actual
differ, the operator patches; otherwise it leaves the object untouched to avoid
etcd churn and reconcile storms. `spec.postgresql.parameters` is a special case
managed by SSA under its own field manager
(`splunk-postgrescluster-postgresql-parameters`) so the operator owns only the
keys it sets and coexists with CNPG-owned parameters.

## Alternatives considered

- **Provider-agnostic abstraction now** (support CNPG + others behind a port).
  The hexagonal target architecture keeps this *possible* — provisioning goes
  through adapter interfaces — but CNPG is the only implementation, and building
  a second-provider abstraction with one provider would be speculative. Chosen:
  CNPG-only, but isolated behind adapters (`cluster/infrastructure/cnpg`, the
  `BackupBackend` port) so a second provisioner is not precluded.
- **Manage users/databases entirely via direct SQL.** Rejected: it duplicates
  what CNPG's `managed.roles` and `Database` CRs already do declaratively, and
  it would make the operator responsible for connection management, retries, and
  idempotency that CNPG handles. SQL is kept to the residual (privilege grants).
- **Full-object drift comparison** (compare the entire live CNPG spec to
  desired). Rejected: CNPG mutates its own spec with defaults and runtime fields,
  so a full comparison drifts on every reconcile and fights CNPG. Chosen:
  compare only the normalized operator-owned subset.
- **No owner reference / label-based association.** Rejected: owner references
  give us cascade delete, the `Owns()` watch for prompt drift repair, and a
  clear single-writer model. (The one place we *remove* the owner reference is
  the `Retain` deletion policy — `spec.clusterDeletionPolicy: Retain` orphans the
  CNPG `Cluster` instead of cascading the delete; see `handleFinalizer` in
  `pkg/postgresql/cluster/core/cluster.go`.)

## Consequences

- **Positive:** we inherit CNPG's HA, failover, backup, and recovery; small SQL
  surface; drift on most owned objects (Cluster spec, Pooler, ScheduledBackup,
  ConfigMap) is healed promptly via owner watches without fighting CNPG over
  its own defaulted fields; the `postgresql.parameters` field manager lets the
  operator own only the keys it sets and coexist with CNPG-owned parameters.
- **Negative / costs:**
  - Hard dependency on CNPG being installed and on its API shape; a CNPG version
    bump can change phases or spec fields and require updates to the phase map
    and normalized structs.
  - The normalized-subset approach means a field the operator does *not* model
    is *not* drift-protected — adding operator control over a new CNPG field
    requires adding it to the relevant normalized struct, or drift on it is
    silently accepted.
  - The residual direct-SQL path needs the superuser secret and network
    reachability to the cluster, and carries its own retry/terminal-failure
    handling (see the privileges phase of the database pipeline).

## References

- Code: `pkg/postgresql/cluster/core/cluster_model.go` (desired-spec build +
  drift patch), `pkg/postgresql/cluster/core/types.go` (`normalized*` structs,
  CNPG phase → reason mapping), `pkg/postgresql/cluster/core/managed_roles_model.go`
  (managed-roles arbitration and merge patch), `internal/controller/enterprise/postgrescluster_controller.go`
  (`Owns`/`Watches`, `secretPredicator`, ObjectStore CRD probe)
- Design docs: "PostgresCluster Controller Design Document",
  "PostgresCluster PostgreSQL Parameters SSA Spike", "PostgreSQL Components State Machine"
- Jira: CPI-2066
