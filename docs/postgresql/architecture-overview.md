---
title: Architecture overview
parent: PostgreSQL
nav_order: 1
---

# Managed PostgreSQL — Architecture Overview

This page is the map of how the Splunk Operator manages PostgreSQL. It shows the
components involved, how each controller reconciles, and the lifecycle phases a
`PostgresCluster` and `PostgresDatabase` move through. For the *why* behind each
decision, follow the linked [Architecture Decision Records](adr/README.md);
for the condensed problem/alternatives/chosen-approach narrative, see the
[RFC summary](rfc-summary.md).

> Diagrams below are authored as Mermaid, embedded directly in this page —
> GitLab renders `` ```mermaid `` fences inline, so no separate build step or
> PNG is needed.

## The big picture

The operator does **not** implement PostgreSQL itself. It sits on top of
[CloudNativePG (CNPG)](https://cloudnative-pg.io/) and translates three custom
resources into CNPG objects, credentials, and connection metadata:

- **`PostgresClusterClass`** — a cluster-scoped, immutable template + platform
  policy (provisioner, sizing defaults, backup and pooler policy). Owned by the
  platform team. See [ADR-0001](adr/0001-crd-structure-and-api-group.md) and
  [ADR-0005](adr/0005-postgresclusterclass-abstraction.md).
- **`PostgresCluster`** — one PostgreSQL cluster. References a class by name and
  applies guard-railed overrides. Owns a CNPG `Cluster`, optional PgBouncer
  `Pooler`s, backup objects, the superuser `Secret`, and a connection-info
  `ConfigMap`.
- **`PostgresDatabase`** — the logical databases, roles, extensions, and
  credentials a service consumes on a referenced cluster (same namespace).

```mermaid
C4Container
    title Splunk Operator — Managed PostgreSQL: Component Overview

    Person(platform, "Platform team", "Owns cluster-scoped policy")
    Person(service, "Service team", "Owns cluster + database instances")

    System_Boundary(operator_boundary, "Splunk Operator") {
        Container(pcc_ctrl, "PostgresClusterClass", "CRD (cluster-scoped)", "Immutable template + platform policy")
        Container(pc_ctrl, "PostgresCluster controller", "controller-runtime", "Component pipeline: secret, objectStore, cluster, roles, pooler, backup, configMap")
        Container(pdb_ctrl, "PostgresDatabase controller", "controller-runtime", "Linear pipeline: cluster to secrets to configMaps to roles to databases to privileges")
    }

    System_Boundary(cnpg_boundary, "CloudNativePG") {
        Container(cnpg_op, "CNPG operator", "postgresql.cnpg.io", "Provisions and manages PostgreSQL")
        Container(cnpg_cluster, "CNPG Cluster", "Cluster CR", "Primary + replicas, managed.roles")
        Container(pooler, "PgBouncer Poolers", "Pooler CR (RW / RO)", "Connection pooling")
        Container(scheduled_backup, "ScheduledBackup / ObjectStore", "Barman plugin", "Backup to object storage")
        ContainerDb(pg, "PostgreSQL", "Pods + PVCs", "Databases, roles, WAL")
    }

    Container(eso, "External Secrets Operator", "ESO (optional)", "Materializes external superuser secret")
    Container(superuser_secret, "superuser Secret", "Kubernetes Secret", "Cluster admin credential")
    Container(role_secrets, "per-database role Secrets", "Kubernetes Secret", "App credentials (_rw etc.)")
    Container(conn_configmap, "connection-info ConfigMap", "Kubernetes ConfigMap", "Host/port/pooler endpoints")
    Container(consumer, "Consumer service", "Splunk service", "Reads ConfigMap + Secret to connect")

    Rel(platform, pcc_ctrl, "Defines classes")
    Rel(service, pc_ctrl, "Creates PostgresCluster")
    Rel(service, pdb_ctrl, "Creates PostgresDatabase")

    Rel(pc_ctrl, pcc_ctrl, "Resolves + merges class")
    Rel(pc_ctrl, cnpg_cluster, "Owns / drift-reconciles")
    Rel(pc_ctrl, pooler, "Owns")
    Rel(pc_ctrl, scheduled_backup, "Owns")
    Rel(pc_ctrl, superuser_secret, "Owns / reads")
    Rel(pc_ctrl, conn_configmap, "Owns (cluster endpoints)")
    Rel(eso, superuser_secret, "Materializes (external mode)")

    Rel(cnpg_op, cnpg_cluster, "Reconciles")
    Rel(cnpg_cluster, pg, "Runs")
    Rel(pooler, pg, "Pools connections to")
    Rel(scheduled_backup, pg, "Backs up")

    Rel(pdb_ctrl, pc_ctrl, "clusterRef (same namespace); declares role intent; watches status")
    Rel(pdb_ctrl, pg, "SQL grants for privileges")
    Rel(pdb_ctrl, role_secrets, "Owns")
    Rel(pdb_ctrl, conn_configmap, "Owns (per-database endpoints)")

    Rel(consumer, conn_configmap, "Reads endpoints")
    Rel(consumer, role_secrets, "Reads credentials")
    Rel(consumer, pooler, "Connects via PostgreSQL/TLS")

    UpdateLayoutConfig($c4ShapeInRow="3", $c4BoundaryInRow="1")
```

Key relationships (see [ADR-0003](adr/0003-cnpg-integration-and-drift-reconciliation.md)):

- One `PostgresCluster` owns exactly one CNPG `Cluster` via a controller owner
  reference; `Owns()` watches heal out-of-band edits and deletions for most
  owned objects. The Secret is the exception: its watch only reacts to
  deletion, and existing Secret data is never rewritten, so drifted Secret
  content is not healed.
- The operator is **declarative-first**: roles go through CNPG `managed.roles`,
  arbitrated by the `PostgresCluster` controller (it collects intent from every
  `PostgresDatabase` and applies the full role list with a single merge patch —
  not per-database Server-Side Apply), databases through CNPG `Database` CRs.
  Direct SQL is limited to CNPG gaps: privilege grants CNPG can't express
  declaratively, and a one-time post-restore credential sweep that disables
  recovered roles' stale login credentials on snapshot/object-storage restores.
- Pooling is a class-defined capability with per-cluster enablement; RW and RO
  poolers are separate CNPG `Pooler` CRs, RO gated on effective instances ≥ 2
  (see [ADR-0004](adr/0004-pgbouncer-integration-model.md)).
- Consumers connect using the connection-info `ConfigMap` (endpoints) and the
  per-database role `Secret` (credentials). The superuser secret may be supplied
  externally via the External Secrets Operator.

## Reconcile flows

The two controllers use **deliberately different** reconcile shapes (see
[ADR-0002](adr/0002-actuate-converge-reconcile-pattern.md), which extends
CPI-1961):

- **`PostgresCluster`** runs an ordered **component pipeline**. Each component
  implements a two-phase *Reconcile (actuate) → Observe (converge)* contract
  plus `CheckContracts`/`Requires`/`Provides`. The runner walks the components
  in order (`secret → objectStore → cluster → managedRoles → pooler → backup →
  configMap`); if any component observes an intermediate state
  (`Pending`/`Provisioning`/`Configuring`) it writes that status and returns
  early with a requeue hint. Only when **every** component observes `Ready` does
  the top-level reconciler declare the cluster `Ready`. Component ordering is
  validated on every reconcile by `validateComponentOrder`.
- **`PostgresDatabase`** runs a **linear pipeline** that accumulates conditions:
  `ClusterValidation → CredentialProvisioning → ConnectionMetadata → Roles →
  DatabaseProvisioning → RWRolePrivileges`, persisting status between steps.

**PostgresCluster controller**

```mermaid
flowchart TD
    A[start] --> B["handleFinalizer (Delete / Retain on deletion)"]
    NoteB["Note: finalizer handling comes before class/config\nresolution, so a missing class or invalid config\nnever blocks cleanup"]
    NoteB -.- B
    B --> C{deleted?}
    C -->|yes| Z1[stop]
    C -->|no| D[add finalizer if absent]
    D --> E[Resolve PostgresClusterClass]
    E --> F["GetMergedConfig (class defaults <- cluster overrides)"]
    F --> G[ValidateMergedConfig + ValidateCrossResource]
    G --> H{config valid?}
    H -->|no| I["set Failed (InvalidConfiguration)"] --> Z2[stop]
    H -->|yes| J["Component pipeline (ordered):\nsecret -> objectStore -> cluster\n-> managedRoles -> pooler\n-> backup -> configMap"]
    J --> K["CheckContracts() - inputs present?"]
    K --> L["Reconcile() - actuate desired state"]
    L --> M["Observe() - classify componentHealth"]
    M --> N{health}
    N -->|"intermediate\nPending/Provisioning/Configuring"| O[write component status] --> P["requeue (early return)"] --> Z3[stop]
    N -->|Ready| Q{more components?}
    Q -->|yes| K
    Q -->|no| R[all components Ready]
    R --> S[project phase from CNPG cluster phase]
    S --> T["set top-level phase = Ready\n(only owner sets Ready)"]
    T --> Z4[stop]
```

**PostgresDatabase controller**

```mermaid
flowchart TD
    A[start] --> B[add finalizer]
    B --> C["Phase ClusterValidation\n- resolve clusterRef (same ns), check Ready"]
    C --> D{cluster ready?}
    D -->|no| E["ClusterReady=False, phase Pending"] --> F[requeue] --> Z1[stop]
    D -->|yes| G["Phase CredentialProvisioning\n- reconcile role Secrets (SecretsReady)"]
    G --> H["Phase ConnectionMetadata\n- reconcile ConfigMaps (ConfigMapsReady)"]
    H --> I["Phase Roles\n- gate on cluster's managed.roles status\n(desired roles declared in spec, applied by the cluster controller)"]
    I --> J{role gate}
    J -->|"conflict/failed"| K["RolesReady=False, phase Failed"] --> Z2[stop]
    J -->|"waiting for CNPG (pending)"| L["RolesReady=False, phase Provisioning"] --> M[requeue] --> Z3[stop]
    J -->|ok| N["Phase DatabaseProvisioning\n- reconcile CNPG Database CRs (DatabasesReady)"]
    N --> O["Phase RWRolePrivileges\n- SQL grants for _rw role (PrivilegesReady)"]
    O --> P[set phase = Ready]
    P --> Z4[stop]
```

## Lifecycle state machines

The `PostgresCluster` phase is **projected from CNPG's own cluster phase** rather
than self-diagnosed (`Healthy → Ready`; `FirstPrimary`/`CreatingReplica` →
`Provisioning`; `Switchover`/`Upgrade`/`ApplyingConfiguration`/restart/promotion
→ `Configuring`; `FailOver` → `Pending`;
`Unrecoverable`/`WaitingForUser`/plugin or image errors → `Failed`; empty →
`Pending`). The `PostgresDatabase` phase advances along its
condition pipeline and enters `Deleting` when a deletion timestamp is set —
where the per-database deletion policy (`Delete` vs `Retain`) decides whether
the underlying database is dropped or orphaned.

**PostgresCluster phase**

```mermaid
stateDiagram-v2
    [*] --> Pending

    Pending: upstream inputs not yet present (class, secret, empty CNPG phase); CNPG FailOver in progress
    Provisioning: CNPG FirstPrimary / CreatingReplica; component still creating (e.g. pooler)
    Configuring: CNPG Switchover / Upgrade / ApplyingConfiguration / Restart / Promotion
    Ready: all components Ready + CNPG Healthy
    Failed: CNPG Unrecoverable / WaitingForUser / plugin or image error; build/patch failed

    Pending --> Provisioning: inputs published
    Provisioning --> Configuring: CNPG applies change
    Provisioning --> Ready: all components converge
    Configuring --> Ready: change complete
    Ready --> Configuring: spec change / CNPG switchover
    Ready --> Provisioning: replica rebuild
    Provisioning --> Failed: unrecoverable
    Configuring --> Failed: unrecoverable
    Ready --> Failed: CNPG regresses
    Failed --> Configuring: manual intervention resolves

    note right of Ready
        Phase is projected from the CNPG Cluster phase; only the
        top-level reconciler declares Ready, and only once every
        component observes Ready.
    end note
```

**PostgresDatabase phase**

```mermaid
stateDiagram-v2
    [*] --> PendingDB

    PendingDB: cluster not found / not Ready (ClusterReady=False)
    ProvisioningDB: secrets, configMaps, roles, databases being reconciled
    ReadyDB: phase set once DatabasesReady=True; privilege grants run after, can leave PrivilegesReady=False while still ReadyDB
    FailedDB: role conflict / role reconcile failed
    DeletingDB: deletion in progress (finalizer draining)

    PendingDB --> ProvisioningDB: cluster Ready
    ProvisioningDB --> ReadyDB: DatabasesReady=True (privilege grants follow, may lag)
    ProvisioningDB --> FailedDB: role conflict / failure
    ProvisioningDB --> PendingDB: cluster regresses
    ReadyDB --> ProvisioningDB: spec change / drift
    ReadyDB --> PendingDB: cluster regresses
    FailedDB --> ProvisioningDB: conflict resolved
    ReadyDB --> DeletingDB: deletionTimestamp set
    ProvisioningDB --> DeletingDB: deletionTimestamp set
    DeletingDB --> [*]: finalizer removed (Delete drops, Retain orphans)

    note right of ReadyDB
        Linear pipeline accumulates conditions: ClusterReady ->
        SecretsReady -> ConfigMapsReady -> RolesReady ->
        DatabasesReady -> PrivilegesReady. Phase is set to ReadyDB
        at the DatabasesReady step, one step before PrivilegesReady
        - a superuser secret fetch failure in that last step can
        leave phase ReadyDB with PrivilegesReady still False.
    end note
```

## Where to go next

- [Architecture Decision Records](adr/README.md) — the decisions behind this design.
- [RFC summary](rfc-summary.md) — problem, alternatives, chosen approach.
- Operational guides: [scaling up](scaling-up.md), [scaling out](scaling-out.md),
  [minor version upgrades](minor-version-upgrade.md),
  [backup to object storage](backup-object-storage.md),
  [backup with volume snapshots](backup-volume-snapshots.md),
  [connecting with TLS](connecting-to-postgres-with-TLS.md),
  [managed roles](postgresdatabase-managed-roles.md).
