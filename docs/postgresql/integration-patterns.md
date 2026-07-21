---
title: Integration & Onboarding Guide
parent: PostgreSQL
nav_order: 0.5
---

# Integration & Onboarding Guide

This guide is for anyone integrating a workload with the Splunk Operator's PostgreSQL support —
configuring a cluster, provisioning application databases, and choosing an access pattern that fits
their team. It goes beyond a first "hello world" deployment and covers what you'll actually face:
which class to build on, whether to share a cluster or get a dedicated one, whether to put a
connection pooler in front of it, and how to keep credentials scoped to the right consumers.

## The three custom resources

The operator's PostgreSQL support is built from three CRDs that form a dependency chain:

```
PostgresClusterClass  (cluster-scoped, platform policy, immutable)
        │  spec.class
        ▼
PostgresCluster        (namespaced, one running database cluster)
        │  spec.clusterRef.name
        ▼
PostgresDatabase        (namespaced, application databases + roles on that cluster)
```

- **`PostgresClusterClass`** is a cluster-scoped template that a platform team defines. It sets
  defaults and policy — sizing, PostgreSQL version, backup strategy, connection pooling — and is
  immutable once created.
- **`PostgresCluster`** is the custom resource you create to actually get a running PostgreSQL
  cluster — one or more databases can live on it. It must reference a class via `spec.class` and can
  override many of the class's defaults.
- **`PostgresDatabase`** declares one or more application databases (and their login roles) on an
  existing `PostgresCluster` in the *same namespace*.

The rest of this guide walks through each layer in order, then covers two cross-cutting use cases:
connection pooling, and shared vs. dedicated clusters.

For deep dives on specific operational topics not covered here, see:
[Connecting with TLS](connecting-to-postgres-with-TLS.md),
[Automated Backups via Object Storage](backup-object-storage.md),
[Automated Backups via Volume Snapshots](backup-volume-snapshots.md),
[Scaling](scaling-out.md), [Vertical Scaling](scaling-up.md), and
[PostgresDatabase Managed Roles](postgresdatabase-managed-roles.md).

## Scope and naming notes

This guide uses the current CRD names in this repository:

- `PostgresClusterClass` is the class/template resource.
- `PostgresCluster` is the running PostgreSQL cluster.
- `PostgresDatabase` is the application database and role declaration.

Two items from early planning are intentionally framed against the API that exists today:

- **Custom certificate management:** the PostgreSQL certificate management workflow is in progress
  and will be documented separately when the API and workflow land. Until then, this guide shows how
  to require TLS at the PostgreSQL access-policy layer with `pgHBA` and points to the current
  connection guide.
- **Storage tier:** the current `PostgresClusterClass`/`PostgresCluster` API exposes storage size
  through `storage`; it does not expose a storage class or storage tier selector. Do not invent
  fields such as `storageClassName` in these resources unless the API is extended.

## Prerequisites

Before creating any of these resources, confirm:

- **CloudNativePG (CNPG) is installed** in the target cluster. It is currently the only supported
  provisioner (`spec.provisioner: postgresql.cnpg.io`).
- **The Splunk Operator is installed** with the `PostgresClusterClass`, `PostgresCluster`, and
  `PostgresDatabase` CRDs registered.
- **A `PostgresClusterClass` you're allowed to use already exists**, or you have a platform-team
  contact who can create one — classes are typically owned by a platform/infra team, not by each
  consumer.
- **A namespace to deploy into.** Your `PostgresCluster` and every `PostgresDatabase` that
  references it must live in the same namespace — see [No cross-namespace access](#no-cross-namespace-access).
- If the workload requires **TLS enforcement today**: configure `pgHBA` to reject non-SSL traffic and
  follow [Connecting to PostgreSQL with TLS](connecting-to-postgres-with-TLS.md). Custom certificate
  management is being tracked in the PostgreSQL certificate management workflow and will be documented
  separately when available.
- If the class enables **backups**: an S3-compatible credentials Secret for object-storage backups
  (see [Automated Backups via Object Storage](backup-object-storage.md)), or a
  `VolumeSnapshotClass` for volume-snapshot backups (see
  [Automated Backups via Volume Snapshots](backup-volume-snapshots.md)).

## PostgresClusterClass — the platform-policy layer

A `PostgresClusterClass` is **cluster-scoped and immutable after creation**: once applied, its
spec cannot be edited (`self == oldSelf` is enforced by the API). If you need different defaults,
you must create a new class — there is no way to change an existing one.

For the currently supported provisioner, a class must set both `spec.provisioner` and `spec.cnpg`:
the API requires `spec.cnpg` whenever `spec.provisioner` is `postgresql.cnpg.io`. Most operational
defaults live under `spec.config` (overridable per-cluster) or `spec.cnpg` (platform policy, **not**
overridable by any `PostgresCluster` that uses the class):

| Field | Layer | Impact |
|---|---|---|
| `spec.provisioner` | required | Must be `postgresql.cnpg.io` today. |
| `spec.cnpg` | required with CNPG | Holds CloudNativePG-specific platform policy for the class. |
| `spec.config.instances` | overridable | Instance count (1 = no HA; 3+ recommended for production). |
| `spec.config.storage` | overridable | Per-instance PVC size. Can only be *increased* later, never decreased. |
| `spec.config.postgresVersion` | overridable | Major/minor version. Major version can only go up once set. |
| `spec.config.resources` | overridable | CPU/memory requests and limits, shared by all instances. |
| `spec.config.postgresqlConfig` | overridable | `postgresql.conf` parameters, cluster-wide. |
| `spec.config.pgHBA` | overridable | `pg_hba.conf` rules, cluster-wide. |
| `spec.config.backup` | overridable (generic fields only) | Enable/schedule for automated backups. |
| `spec.config.connectionPooler` | overridable | Enable/disable toggle for PgBouncer — see [Connection pooling](#connection-pooling-pgbouncer). |
| `spec.cnpg.primaryUpdateMethod` | platform policy | `restart` (brief downtime, default) or `switchover` (near-zero downtime, needs >1 instance). |
| `spec.cnpg.connectionPooler` | platform policy | Pooler mode, instance count, PgBouncer parameters. |
| `spec.cnpg.backup` | platform policy | Backup target and provider-specific settings (volume snapshot or barman). |

### Customizing classes for a workload tier

Since a class can't be edited after creation, platform teams instead maintain a set of classes for
different tiers, and consumers pick the one that fits. The repo ships representative examples you
can adapt:

- [`config/samples/enterprise_v4_postgresclusterclass_dev.yaml`](../../config/samples/enterprise_v4_postgresclusterclass_dev.yaml) —
  single instance, minimal resources (500m/1Gi requests), `primaryUpdateMethod: restart`. Suitable
  for development/test workloads that can tolerate restarts.
- [`config/samples/enterprise_v4_postgresclusterclass_prod.yaml`](../../config/samples/enterprise_v4_postgresclusterclass_prod.yaml) —
  3 instances (HA), tuned `postgresqlConfig` for OLTP, SSL-only `pgHBA`, `primaryUpdateMethod:
  switchover`, and a 3-instance transaction-mode pooler. Suitable for production workloads.
- [`config/samples/enterprise_v4_postgresclusterclass_backup.yaml`](../../config/samples/enterprise_v4_postgresclusterclass_backup.yaml) —
  adds a daily volume-snapshot backup schedule on top of a mid-tier resource profile.

If none of these fit — for example, a workload needs more memory than the `prod` class's 8Gi
request but doesn't need the full production HBA/pooler policy — the right move is a **new class**
that copies the closest sample and adjusts `spec.config.resources` (and any other
workload-specific field), not a request to mutate an existing class.

Complete example — a memory-heavy class for a workload that needs larger pods and larger PVCs, but
does not need a read-only pooler:

```yaml
# PostgresClusterClass is cluster-scoped; do not set metadata.namespace.
apiVersion: enterprise.splunk.com/v4
kind: PostgresClusterClass
metadata:
  name: postgresql-memory-heavy
spec:
  # Required today; CNPG is the only supported provisioner.
  provisioner: postgresql.cnpg.io
  config:
    # Three instances gives one primary plus replicas for HA and switchover.
    instances: 3
    # Storage is a per-instance PVC size. Storage tier/class is not exposed by
    # this API today; use a larger size here, not a storageClassName field.
    storage: 250Gi
    postgresVersion: "18"
    # CPU/memory apply to every PostgreSQL instance in clusters using this class.
    resources:
      requests:
        cpu: "4"
        memory: "24Gi"
      limits:
        cpu: "8"
        memory: "32Gi"
    postgresqlConfig:
      max_connections: "300"
      shared_buffers: "6GB"
      effective_cache_size: "18GB"
      work_mem: "32MB"
    pgHBA:
      - "hostnossl all all 0.0.0.0/0 reject"
      - "hostssl all all 0.0.0.0/0 scram-sha-256"
    connectionPooler:
      enabled: true
      readWrite: true
      # This class exposes only the RW pooler. Choose this when replicas are
      # reserved for HA rather than application read traffic.
      readOnly: false
  # Required with the CNPG provisioner. These settings are platform policy and
  # cannot be overridden from a PostgresCluster.
  cnpg:
    primaryUpdateMethod: switchover
    connectionPooler:
      instances: 3
      mode: transaction
      config:
        max_client_conn: "200"
        default_pool_size: "40"
```

Required fields and impact:

- `spec.provisioner` and `spec.cnpg` make this a valid CNPG-backed class.
- `spec.config.resources` and `spec.config.storage` set the default per-instance resource profile
  for all clusters using the class.
- `spec.config.connectionPooler.enabled: true` requires `spec.cnpg.connectionPooler` so the operator
  knows how many PgBouncer pods to create and which pool mode to use.
- `readOnly: false` avoids advertising an RO pooler for workloads that should not send application
  reads to replicas.

### Common mistakes

- Trying to change a class's spec after creation — it's immutable; create a new class instead.
- Setting `connectionPooler.readOnly: true` on a class whose clusters may run with `instances: 1`
  — the admission webhook rejects a read-only pooler when the effective instance count is below 2.
- Using `primaryUpdateMethod: switchover` with only 1 instance — there's no replica to fail over
  to, so switchover can't provide the downtime benefit it's meant for.

## PostgresCluster — an instance of a class

A `PostgresCluster` is namespaced and represents one running database cluster. `spec.class` is
**required and immutable** — you pick a class at creation time and cannot change it later.

Most `spec.config` fields from the class can be overridden per-cluster, with guard rails:

- `storage` — can only be increased, never decreased, relative to the class default or a prior
  value.
- `postgresVersion` — major version can only go up, never down, once set.
- `instances`, `resources`, `postgresqlConfig`, `pgHBA`, `backup` (generic fields), `monitoring`,
  `connectionPooler` — freely overridable within the class's platform-policy constraints.
- `clusterDeletionPolicy` (`Delete` or `Retain`, default `Retain`) — what happens to the underlying
  CNPG `Cluster` when this `PostgresCluster` is deleted.
- `passwordConfig` and `bootstrapFrom` — settable at creation but **immutable** afterward.

Reference samples (do not need to be modified to use as a starting point):

- [`config/samples/enterprise_v4_postgrescluster_dev.yaml`](../../config/samples/enterprise_v4_postgrescluster_dev.yaml) —
  built on `postgresql-dev`, overrides `storage`, `postgresVersion`, and `resources` above the
  class defaults.
- [`config/samples/enterprise_v4_postgrescluster_prod.yaml`](../../config/samples/enterprise_v4_postgrescluster_prod.yaml) —
  built on `postgresql-prod`. Note that this sample currently overrides `instances: 1`, while the
  `postgresql-prod` class uses `primaryUpdateMethod: switchover` and enables a read-only pooler by
  default. That combination is not a production-ready shape and may be rejected by admission; omit
  the override or set `instances >= 2` when using the production class.
- [`config/samples/enterprise_v4_postgrescluster_backup.yaml`](../../config/samples/enterprise_v4_postgrescluster_backup.yaml) —
  built on `postgresql-backup`, overrides only the backup schedule.

For TLS enforcement today, see [Connecting to PostgreSQL with TLS](connecting-to-postgres-with-TLS.md)
— the class/cluster `pgHBA` settings (for example `hostnossl ... reject` followed by `hostssl ...
scram-sha-256`) work together with CNPG's certificate handling described there. Custom certificate
management is not modeled in these CRDs yet; it is expected to be covered by the PostgreSQL
certificate management workflow when that work lands.

### Connection pooling (PgBouncer)

Connection pooling is configured at two layers: the class sets whether pooling is *available and
enabled by default*, and platform policy (also class-owned, under `spec.cnpg.connectionPooler`)
sets *how* PgBouncer behaves. A cluster can override the enable/disable toggle but not the
platform-policy pooler settings.

**When to use it:**

- **Protecting the primary from connection storms.** If your workload opens many short-lived
  connections concurrently (e.g. a web tier under bursty traffic), PgBouncer caches and reuses
  server-side connections instead of letting PostgreSQL's own connection/backend-process overhead
  absorb the spike — this avoids the primary being overwhelmed by connection setup/teardown cost.
- **Near-zero-downtime cluster cutover.** During a switchover (or a cutover to a new cluster) once
  replication lag has reached zero, pointing clients at a stable pooler endpoint rather than the
  primary directly lets the pooler absorb the brief reconnect instead of every client failing a
  connection at the same instant.

Complete example — transaction-mode PgBouncer with RW and RO pooler endpoints:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: PostgresClusterClass
metadata:
  name: postgresql-pooler-transaction
spec:
  provisioner: postgresql.cnpg.io
  config:
    # RO pooler endpoints require an effective instance count of at least 2.
    instances: 3
    storage: 100Gi
    postgresVersion: "18"
    resources:
      requests:
        cpu: "2"
        memory: "8Gi"
      limits:
        cpu: "4"
        memory: "16Gi"
    postgresqlConfig:
      # Size max_connections high enough for the server-side connections PgBouncer
      # will hold, not for every application client connection.
      max_connections: "200"
    pgHBA:
      - "hostnossl all all 0.0.0.0/0 reject"
      - "hostssl all all 0.0.0.0/0 scram-sha-256"
    # This toggle says poolers should be created for clusters using the class.
    connectionPooler:
      enabled: true
      readWrite: true
      readOnly: true
  cnpg:
    primaryUpdateMethod: switchover
    # These pooler details are platform policy. A PostgresCluster can turn pooling
    # on/off, but it cannot change mode, replica count, or PgBouncer parameters.
    connectionPooler:
      instances: 3
      mode: transaction
      config:
        max_client_conn: "300"
        default_pool_size: "30"
---
apiVersion: enterprise.splunk.com/v4
kind: PostgresCluster
metadata:
  name: orders-postgres
  namespace: orders
spec:
  class: postgresql-pooler-transaction
  clusterDeletionPolicy: Retain
---
apiVersion: enterprise.splunk.com/v4
kind: PostgresDatabase
metadata:
  name: orders-db
  namespace: orders
spec:
  clusterRef:
    name: orders-postgres
  databases:
    - name: orders
      deletionPolicy: Delete
```

Required fields and impact:

- **`mode`** — `transaction` is recommended for most workloads (a connection is returned to the
  pool after each transaction, giving the best multiplexing). `session` holds a connection for the
  client's whole session — most compatible, least efficient. `statement` returns the connection
  after every statement — highest multiplexing, but incompatible with multi-statement transactions.
- A `PostgresCluster` can override only the enable/disable toggle
  (`spec.connectionPooler.enabled/readWrite/readOnly`), inheriting `mode`/`instances`/`config` from
  the class.
- When enabled, pooler endpoints are published as `CLUSTER_POOLER_RW_ENDPOINT` and
  `CLUSTER_POOLER_RO_ENDPOINT` in the cluster's access ConfigMap — see the ConfigMap key table in
  [Connecting to PostgreSQL with TLS](connecting-to-postgres-with-TLS.md#configmap-keys-what-apps-read).
- If a workload needs session-level features, create a separate class with `mode: session` rather than
  overriding the pool mode on the `PostgresCluster`; pool mode is class policy.

**Common mistakes:**

- Using `transaction` mode with client code that relies on session-level state (prepared
  statements across queries, session `SET` variables, advisory locks held across statements) —
  these break under transaction pooling because the underlying server connection can change
  between statements.
- Enabling a read-only pooler (`readOnly: true`) on a cluster effectively running 1 instance — it
  will be rejected by the admission webhook.
- Pointing read-heavy application traffic at the RW pooler/endpoint instead of the RO one, missing
  the chance to offload reads from the primary.

### Common mistakes

- Assuming `spec.class` can be changed later to move a cluster to a different tier — it's
  immutable; you'd need a new `PostgresCluster`.
- Trying to shrink `storage` or downgrade the major `postgresVersion` — both are rejected by
  validation.
- Setting `passwordConfig` after cluster creation, expecting it to take effect — it's immutable
  once set at creation.

## PostgresDatabase — application databases

A `PostgresDatabase` declares one or more application databases on an existing `PostgresCluster`
via `spec.clusterRef.name` — a same-namespace reference (see
[No cross-namespace access](#no-cross-namespace-access)) that is **immutable** after creation. Each
entry in `spec.databases[]` (1–10 per resource) produces a `<name>_admin` and `<name>_rw`
PostgreSQL role, generated credential Secrets, and a connection ConfigMap. See
[PostgresDatabase Managed Roles](postgresdatabase-managed-roles.md) for the full role-reconciliation
and deletion-policy behavior.

Reference sample:
[`config/samples/enterprise_v4_postgresdatabase.yaml`](../../config/samples/enterprise_v4_postgresdatabase.yaml) —
one `PostgresDatabase` declaring two databases (`kvstore`, `analytics`) against a single cluster.

### Shared cluster vs. dedicated cluster

This is the main access-pattern decision, and it's made by how many `PostgresDatabase`/database
entries you point at a given `PostgresCluster` — there's no separate CRD or flag for it.

**Shared cluster** — multiple workloads' databases on one `PostgresCluster`, either as multiple
entries in one `PostgresDatabase`'s `databases[]` list, or as separate `PostgresDatabase` resources
in the same namespace all referencing the same `clusterRef`:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: PostgresClusterClass
metadata:
  name: postgresql-shared-standard
spec:
  provisioner: postgresql.cnpg.io
  config:
    # Shared clusters should usually be HA because multiple workloads inherit the
    # same failure and maintenance domain.
    instances: 3
    storage: 100Gi
    postgresVersion: "18"
    resources:
      requests:
        cpu: "2"
        memory: "8Gi"
      limits:
        cpu: "4"
        memory: "16Gi"
    pgHBA:
      - "hostnossl all all 0.0.0.0/0 reject"
      - "hostssl all all 0.0.0.0/0 scram-sha-256"
    # Shared clusters commonly benefit from a pooler because multiple workloads
    # can otherwise create independent connection storms.
    connectionPooler:
      enabled: true
      readWrite: true
      readOnly: true
  cnpg:
    primaryUpdateMethod: switchover
    connectionPooler:
      instances: 3
      mode: transaction
      config:
        max_client_conn: "300"
        default_pool_size: "30"
---
apiVersion: enterprise.splunk.com/v4
kind: PostgresCluster
metadata:
  name: shared-postgres
  namespace: shared-ns
spec:
  class: postgresql-shared-standard
  clusterDeletionPolicy: Retain
---
apiVersion: enterprise.splunk.com/v4
kind: PostgresDatabase
metadata:
  name: team-a-db
  namespace: shared-ns
spec:
  clusterRef:
    name: shared-postgres
  databases:
    - name: teamaapp
      deletionPolicy: Delete
---
apiVersion: enterprise.splunk.com/v4
kind: PostgresDatabase
metadata:
  name: team-b-db
  namespace: shared-ns
spec:
  clusterRef:
    name: shared-postgres   # same cluster as above
  databases:
    - name: teambapp
      deletionPolicy: Delete
```

Use this when workloads are small, low-traffic, or cost-sensitive enough that running a separate
cluster per workload isn't justified, and when independent lifecycle/resource isolation between
those workloads isn't a hard requirement. All databases on a shared cluster inherit the same
instance count, resource allocation, PostgreSQL version, and backup/pooling policy — and every
`PostgresDatabase` on a shared cluster depends on that cluster staying available. Deleting the
`PostgresCluster` is a cluster-level operation: `spec.clusterDeletionPolicy: Delete` deletes the
underlying CNPG cluster, while `spec.clusterDeletionPolicy: Retain` orphans it. Per-database
`databases[].deletionPolicy` applies only when deleting the `PostgresDatabase` resource (see
[PostgresDatabase Managed Roles](postgresdatabase-managed-roles.md)); it does not protect data from
a cluster-level delete. Any `PostgresDatabase` still pointed at a deleted cluster reports a
degraded/not-ready status until it is repointed or removed. Sharing a cluster means sharing its
failure and maintenance domain.

Required fields and impact:

- `PostgresCluster.spec.class` selects the shared sizing, PostgreSQL version, backup, TLS, and pooler
  policy for every database on the cluster.
- Each `PostgresDatabase.spec.clusterRef.name` must match a `PostgresCluster` in the same namespace.
- Each `databases[].name` gets its own PostgreSQL database plus `<name>_admin` and `<name>_rw` roles.
- `deletionPolicy` is per database entry, so one team's retention choice does not change another
  team's entry when deleting a `PostgresDatabase`; cluster-level deletion is controlled by
  `PostgresCluster.spec.clusterDeletionPolicy`.

**Dedicated cluster** — one `PostgresCluster` (and its own `PostgresDatabase`) per workload:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: PostgresClusterClass
metadata:
  name: postgresql-dedicated-prod
spec:
  provisioner: postgresql.cnpg.io
  config:
    instances: 3
    storage: 200Gi
    postgresVersion: "18"
    resources:
      requests:
        cpu: "4"
        memory: "16Gi"
      limits:
        cpu: "8"
        memory: "32Gi"
    postgresqlConfig:
      max_connections: "250"
      shared_buffers: "4GB"
      effective_cache_size: "12GB"
    pgHBA:
      - "hostnossl all all 0.0.0.0/0 reject"
      - "hostssl all all 0.0.0.0/0 scram-sha-256"
    connectionPooler:
      enabled: true
      readWrite: true
      readOnly: true
  cnpg:
    primaryUpdateMethod: switchover
    connectionPooler:
      instances: 3
      mode: transaction
      config:
        max_client_conn: "200"
        default_pool_size: "25"
---
apiVersion: enterprise.splunk.com/v4
kind: PostgresCluster
metadata:
  name: team-c-postgres
  namespace: team-c
spec:
  class: postgresql-dedicated-prod
  # Dedicated clusters can still override class defaults when this workload needs
  # a larger shape than the baseline class.
  storage: 300Gi
  resources:
    requests:
      cpu: "6"
      memory: "24Gi"
    limits:
      cpu: "10"
      memory: "40Gi"
  clusterDeletionPolicy: Retain
---
apiVersion: enterprise.splunk.com/v4
kind: PostgresDatabase
metadata:
  name: team-c-db
  namespace: team-c
spec:
  clusterRef:
    name: team-c-postgres
  databases:
    - name: teamcapp
      deletionPolicy: Retain
```

Use this when a workload needs independent resource sizing (as above — `resources`, `storage`, and
`instances` can all be overridden past the class default), its own upgrade/maintenance timing,
separate backup schedules, or its own `pgHBA`/TLS configuration (see
[Connecting to PostgreSQL with TLS](connecting-to-postgres-with-TLS.md)) — none of which can be
tuned per-database on a shared cluster, only per-cluster.

Required fields and impact:

- The dedicated class captures the baseline production policy for this workload family.
- `PostgresCluster.spec.storage` and `spec.resources` override the class defaults only for this
  cluster.
- `clusterDeletionPolicy: Retain` prevents deleting the `PostgresCluster` CR from immediately
  deleting the underlying CNPG cluster.
- `databases[].deletionPolicy: Retain` keeps the application database and roles in place if the
  `PostgresDatabase` CR is deleted.

Common mistakes specific to a dedicated cluster:

- Building it on a lower tier class (e.g. `postgresql-dev`) and then trying to override `resources`
  or `instances` up to production levels piecemeal — if the workload needs production-grade
  `postgresqlConfig`, `pgHBA`, or pooler policy too, start from a class that already has that
  platform policy (e.g. `postgresql-prod`) rather than fighting per-cluster overrides.
- Provisioning one dedicated cluster per workload without checking whether the workload's traffic
  and cost profile actually needs isolation — see [Shared cluster](#shared-cluster-vs-dedicated-cluster)
  above for when sharing is the better fit.

### Multi-tenant isolation within one namespace

Because `clusterRef` is same-namespace-only, workloads sharing a cluster typically also share a
namespace. Use Kubernetes RBAC to isolate credential reads and, if your cluster enforces network
policies, use `NetworkPolicy` to restrict which pods can open TCP connections to the PostgreSQL or
pooler pods. Cross-namespace `PostgresDatabase` access is not supported; see
[No cross-namespace access](#no-cross-namespace-access).

Complete example — give only the `team-a-app` ServiceAccount access to Team A's generated
credentials and allow only Team A pods to connect to the shared PostgreSQL pods:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: team-a-app
  namespace: shared-ns
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: read-team-a-db-credentials
  namespace: shared-ns
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    # Scope to exactly the Secrets this workload's PostgresDatabase generated —
    # do not grant get/list on all secrets in the namespace.
    resourceNames: ["team-a-db-teamaapp-admin", "team-a-db-teamaapp-rw"]
    verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: team-a-app-can-read-credentials
  namespace: shared-ns
subjects:
  - kind: ServiceAccount
    name: team-a-app
roleRef:
  kind: Role
  name: read-team-a-db-credentials
  apiGroup: rbac.authorization.k8s.io
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-team-a-to-shared-postgres
  namespace: shared-ns
spec:
  # CNPG applies this label to PostgreSQL pods for the cluster. Confirm labels in
  # your installed CNPG version before treating this as a hard security boundary.
  podSelector:
    matchLabels:
      cnpg.io/cluster: shared-postgres
  policyTypes:
    - Ingress
  ingress:
    - from:
        # Label the Team A application Pods, Deployment, or StatefulSet template
        # with app.kubernetes.io/name=team-a-app.
        - podSelector:
            matchLabels:
              app.kubernetes.io/name: team-a-app
      ports:
        - protocol: TCP
          port: 5432
```

Bind RBAC to each workload's own ServiceAccount, scoped via `resourceNames` to only the Secrets that
workload's `PostgresDatabase` created, and have workload pods use that ServiceAccount rather than a
shared or default one. A `NetworkPolicy` is namespace-local and label-based; it does not provide
database-level authorization, and it does not replace PostgreSQL roles or Kubernetes Secret RBAC.

Required fields and impact:

- `resourceNames` must match the generated Secret names: `<PostgresDatabase>-<database>-admin` and
  `<PostgresDatabase>-<database>-rw`.
- `subjects[].name` must be the ServiceAccount used by the consuming workload Pods.
- `podSelector.matchLabels` on the `NetworkPolicy` selects the database-side Pods to protect.
- `ingress[].from[].podSelector` selects the application Pods allowed to connect.

### Common mistakes

- Expecting `clusterRef` to reach a `PostgresCluster` in a different namespace — it can't; see
  [No cross-namespace access](#no-cross-namespace-access).
- Setting `passwordConfig` on a `databases[]` entry after creation, expecting it to switch to
  externally managed credentials — it's immutable once set.
- Not setting RBAC on credential Secrets when multiple workloads share a namespace and cluster,
  leaving one workload able to read another's database credentials.
- Relying on `NetworkPolicy` alone for tenant isolation — it controls network reachability, not who
  can read database passwords or what a database role can do after connecting.

## No cross-namespace access

`PostgresDatabase.spec.clusterRef` is a `corev1.LocalObjectReference` — it has no `namespace`
field, so a `PostgresDatabase` can only reference a `PostgresCluster` in its own namespace. A
`PostgresCluster` in one namespace cannot currently serve `PostgresDatabase` consumers in other
namespaces through these CRDs. This is recorded here as a known gap rather than a supported
pattern.
