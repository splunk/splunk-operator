---
title: Disaster Recovery Runbook
parent: PostgreSQL
nav_order: 2.5
---

# PostgreSQL Disaster Recovery Runbook

This runbook is for **operators and on-call engineers** who need to recover a
`PostgresCluster` after data loss, corruption, or a storage/cluster failure. It
defines the recovery objectives for each supported method, maps failure modes to
a recovery path, provides step-by-step recovery drills, and defines a DR testing
schedule.

It is operational. For the full restore API reference (every field, every
recovery-target type, cross-class compatibility rules) see
[Restoring a PostgresCluster](restore-from-volume-snapshot.md). For backup setup
see [Volume Snapshot Backups](backup-volume-snapshots.md) and
[Object Storage Backups](backup-object-storage.md). To choose a backup strategy
in the first place, see [Backup and Restore Choices](backup-and-restore-choices.md).

> **You cannot recover what you did not back up.** DR readiness is decided when
> you configure the `PostgresClusterClass`, not when disaster strikes. Confirm a
> usable backup exists *before* you need it — that is what the [testing
> schedule](#7-dr-testing-schedule) is for.

---

## 1. Recovery model in one paragraph

Recovery is never an in-place repair. You **create a new `PostgresCluster`** with
a `spec.bootstrapFrom` block that names the backup to recover from. The operator
bootstraps the new cluster's database from that backup instead of initializing an
empty one. The failed/original cluster is untouched — you run both side by side,
validate the recovered one, cut over the application to the recovered cluster,
then retire the original.
`bootstrapFrom` is immutable and the restore runs exactly once, during initial
provisioning.

Three recovery sources are supported:

| Source | `bootstrapFrom` | Recovers to | Requires |
|---|---|---|---|
| **Volume snapshot** | `volumeSnapshot` | The moment the snapshot was taken | A CSI `VolumeSnapshot` in the namespace |
| **Object storage** | `objectStorage` | The latest archived WAL | A Barman base backup + WAL in S3 |
| **Point-in-time (PITR)** | `objectStorage` (or `volumeSnapshot` + `walArchive`) with `recoveryTarget` | An arbitrary instant you choose | A base backup + continuous WAL covering that instant |

---

## 2. Recovery objectives (RPO / RTO)

**RPO (Recovery Point Objective)** — how much recent data you can lose: the gap
between the last recoverable point and the moment of failure.

**RTO (Recovery Time Objective)** — how long recovery takes: from starting the
restore to a validated, application-ready cluster.

| Method | RPO (data loss) | RTO (time to recover) | Notes |
|---|---|---|---|
| **Volume snapshot** | = snapshot interval. Everything written after the last snapshot is lost. | **Fastest.** Restores a PVC clone directly in the cluster. | No WAL replay. Bounded by CSI clone speed and standby rebuild. |
| **Object storage (latest WAL)** | Bounded by **`archive_timeout`** — CNPG defaults it to **5 minutes**, so WAL is archived at least that often even under low write load, giving a deterministic time-based RPO. Any commits in the not-yet-shipped current segment at the moment of failure are the potential loss. | **Moderate.** Pull base backup + replay WAL from S3. | Survives loss of the whole Kubernetes storage system. |
| **PITR** | **User-selectable** to any instant within the archive window, down to transaction granularity — you choose the recovery point. | **Slowest of the three.** Base backup pull + WAL replay to the chosen target. | The recovery answer for logical corruption / bad deploy. |

> **Backup-based recovery is not zero-RPO.** Continuous WAL archiving gives a *small, bounded* RPO (the `archive_timeout` window above), not RPO = 0. In PostgreSQL/CloudNativePG, true **RPO = 0 requires synchronous replication** — a high-availability property that prioritizes durability over write availability, distinct from these backup/restore paths. **The Splunk Operator does not currently expose synchronous replication** (the CNPG `dataDurability` / `spec.postgresql.synchronous` settings are not surfaced through the `PostgresClusterClass` API), so the DR methods documented here are the recoverability tools available today. If a workload requires zero-loss durability, raise it as a platform requirement rather than assuming these backup paths provide it.

> **RTO is driven by base-backup frequency.** For object-storage and PITR, RTO = time to fetch the base backup + time to replay WAL from that base up to the target. The more WAL there is to replay since the last base backup, the longer recovery takes. CloudNativePG notes a **weekly base backup is typically sufficient**; increase base-backup frequency if measured WAL-replay time pushes RTO past your objective. This is why the [testing schedule](#7-dr-testing-schedule) measures RTO against production-representative data.

### Measured reference values

Measured on a KIND cluster with small (~1 GiB) test databases, operator build
`dr-cpi1867`, on 2026-07-29. **These are indicative, not SLAs.** RTO scales with
database size, WAL volume to replay, object-store bandwidth, and instance count.
Re-measure against production-representative data — that is a goal of the
[testing schedule](#7-dr-testing-schedule).

| Method | Measured RTO (1 GiB test DB) | What was measured |
|---|---|---|
| Volume snapshot | **~36 s** | Restore CR created → CNPG `ClusterReady=True` + credential sweep complete |
| Object storage (latest WAL) | **~83 s** | Restore CR created → credential sweep complete (base pull + WAL replay) |
| PITR (`recoveryTarget: time`) | **~105 s** | Restore CR created → credential sweep complete (base pull + WAL replay to target) |

Re-provisioning application access with a `PostgresDatabase` after the sweep added
~35 s in each drill and is part of end-to-end RTO — see [§4.4](#44-application-cutover).

---

## 3. Failure mode → recovery path

Pick the recovery path from the nature of the failure, not habit.

| Failure | Recommended path | Why |
|---|---|---|
| Node / PVC lost, storage system intact, recent snapshot exists | **Volume snapshot** ([§4.1](#41-scenario-a--volume-snapshot-restore)) | Fastest RTO; snapshot lives in the same storage system. |
| Storage system or whole cluster lost | **Object storage** ([§4.2](#42-scenario-b--object-storage-restore)) | Backups live independently of PostgreSQL volumes. |
| Logical corruption / bad migration / erroneous `DELETE` at a known time | **PITR** ([§4.3](#43-scenario-c--point-in-time-recovery-pitr)) | Stop recovery *just before* the damaging change. |
| Snapshot exists but you also need WAL replay past it | **Volume snapshot + `walArchive`** ([reference §8.2](restore-from-volume-snapshot.md#82-pitr-from-a-volume-snapshot)) | Fast base + forward replay. |
| Need to know how far back you *can* recover | Check the `ObjectStore` recovery window ([§5.3](#53-confirm-the-object-store-recovery-window)) | Bounds every object-store/PITR option. |

---

## 4. Recovery drills

Each drill is a self-contained runbook: detect, recover, validate, cut over. All
examples use placeholders `<namespace>`, `<your-class>`, and a source cluster
named `mydb`. **Read [§4.5 (the Retain gotcha)](#45-critical-do-not-reuse-a-restore-name) before your first real recovery** — it is the most common way to silently recover the *wrong* data.

**Recovery-environment preflight (whole-cluster or new-environment loss).** If you
are recovering into a fresh cluster — not just a new namespace in a surviving one —
the restore will fail before it reaches the archive unless the recovery environment
is already in place. Confirm all of the following exist in the target environment
first:

1. The **Splunk Operator** is installed and running (with the `PostgresController`
   feature gate enabled).
2. **CloudNativePG** and, for object-storage / PITR, the **barman-cloud CNPG
   plugin** and its `ObjectStore` CRD are installed. See
   [Object Storage Backups §2](backup-object-storage.md#2-prerequisites).
3. The **`PostgresClusterClass`** the restore references exists and, for
   object-storage / PITR, defines `cnpg.backup.barmanObjectStore` pointing at the
   source's archive.
4. The **S3 credentials Secret** named by that class's `s3Credentials` exists **in
   the restore namespace** (it is referenced by name only and must be present in
   every namespace that uses the class), with read access to the archive bucket.

Common preflight for every drill:

1. Restore into the **same `PostgresClusterClass`** as the source — not merely one
   with the same PostgreSQL major version. A snapshot is a raw volume image, so the
   target class must match the source's storage provisioner and PostgreSQL server
   settings as well as its major version; cross-class snapshot restores are
   unsupported and a mismatch can leave the cluster stuck in `Provisioning`. The
   operator does not validate this — a mismatch fails the restore with no clear
   error. See [reference §5](restore-from-volume-snapshot.md#5-restore-compatibility).
2. Confirm the target class has **equal or higher** capacity parameters
   (`max_connections`, etc.) than the source.
3. For object-storage / PITR, confirm the target class defines
   `cnpg.backup.barmanObjectStore` pointing at the source's archive.
4. Use a **fresh restore-target name** and set `clusterDeletionPolicy: Delete` on
   the restore CR (see [§4.5](#45-critical-do-not-reuse-a-restore-name)).

### 4.1 Scenario A — Volume snapshot restore

**Use when:** a recent CSI snapshot exists and recovering to snapshot time is
acceptable. **RPO** = snapshot interval. **RTO** = fastest (~36 s measured on a
1 GiB DB).

1. **Find a usable snapshot** (`READYTOUSE=true`):
   ```bash
   kubectl get volumesnapshots -n <namespace>
   ```
2. **Create the restored cluster:**
   ```yaml
   apiVersion: platform.splunk.com/v1alpha1
   kind: PostgresCluster
   metadata:
     name: mydb-restored          # NEW, unused name
     namespace: <namespace>
   spec:
     class: <your-class>
     clusterDeletionPolicy: Delete
     bootstrapFrom:
       volumeSnapshot:
         storage: mydb-backup-20260729095100   # snapshot name
   ```
   If the source had a separate WAL volume, also set
   `volumeSnapshot.walStorage` to the matching WAL snapshot. **Confirm both
   snapshots come from the same backup set** before pairing them — CNPG labels each
   snapshot with `cnpg.io/backupName` and `cnpg.io/cluster`; the data and WAL
   snapshots must share the same `cnpg.io/backupName`. Pairing snapshots from
   different backups produces an inconsistent physical restore.
   ```bash
   kubectl get volumesnapshot -n <namespace> \
     -L cnpg.io/cluster,cnpg.io/backupName
   ```
3. **Wait for Ready and confirm the sweep:**
   ```bash
   kubectl get postgrescluster mydb-restored -n <namespace> -w
   kubectl get postgrescluster mydb-restored -n <namespace> \
     -o jsonpath='{.status.restore}'
   ```
   Expect `source.volumeSnapshot` set and `credentialSweep.completed: true`.
4. **Validate data**, then re-provision access ([§4.4](#44-application-cutover)).

> **Drill result (2026-07-29):** snapshot `READYTOUSE` ~22 s after firing;
> physical restore to `ClusterReady` + sweep in **~36 s**; all rows intact via
> freshly provisioned credentials; pre-restore application credentials correctly
> rejected (`FATAL: password authentication failed`).

### 4.2 Scenario B — Object storage restore

**Use when:** the storage system or whole cluster is gone, or no snapshot exists.
Recovers to the **latest archived WAL**. **RPO** = the `archive_timeout` window
(CNPG default 5 min) plus any commits in the current unshipped segment. **RTO** =
moderate (~83 s measured on a 1 GiB DB).

1. **Confirm the archive is reachable and note the source `serverName`** (the
   folder under which its WAL/base backups live — matches the source cluster's
   name). See [§5.3](#53-confirm-the-object-store-recovery-window).
2. **Create the restored cluster** (no `recoveryTarget` = recover to latest WAL):
   ```yaml
   apiVersion: platform.splunk.com/v1alpha1
   kind: PostgresCluster
   metadata:
     name: mydb-restored          # NEW, unused name
     namespace: <namespace>
   spec:
     class: <your-class>          # must define cnpg.backup.barmanObjectStore
     clusterDeletionPolicy: Delete
     bootstrapFrom:
       objectStorage:
         serverName: mydb          # source's server name in the object store
   ```
   Bucket path, endpoint, and credentials come from the class's
   `barmanObjectStore` — never from `bootstrapFrom`.
3. **Wait and verify** as in Scenario A. Expect `status.restore.source.objectStorage`
   set and `credentialSweep.completed: true`.
4. **Validate data**, then re-provision access ([§4.4](#44-application-cutover)).

> **Drill result (2026-07-29):** restore from `serverName: vol-src` reached
> credential-sweep-complete in **~83 s** (base backup pull + WAL replay from real
> AWS S3); all rows intact.

### 4.3 Scenario C — Point-in-time recovery (PITR)

**Use when:** you must stop recovery *before* a specific damaging event (bad
migration, erroneous bulk delete). **RPO** = user-selectable to the chosen
instant. **RTO** = slowest of the three (~105 s measured on a 1 GiB DB).

1. **Determine the target time.** Pick an instant *just before* the damage. When
   verifying with a test write, capture the target with `clock_timestamp()` in a
   statement that runs *after* your seed transaction commits — `now()` /
   `transaction_timestamp()` return the transaction *start* time and will land the
   target before your committed rows.
2. **Confirm the target falls inside the archive window** ([§5.3](#53-confirm-the-object-store-recovery-window)).
3. **Create the restored cluster with a `recoveryTarget`:**
   ```yaml
   apiVersion: platform.splunk.com/v1alpha1
   kind: PostgresCluster
   metadata:
     name: mydb-restored          # NEW, unused name
     namespace: <namespace>
   spec:
     class: <your-class>          # must define cnpg.backup.barmanObjectStore
     clusterDeletionPolicy: Delete
     bootstrapFrom:
       objectStorage:
         serverName: mydb
       recoveryTarget:
         type: time                          # time | lsn (for objectStorage source)
         value: "2026-07-29T10:05:53.159125Z"  # RFC 3339 timestamp
   ```
   For an `objectStorage` source only `time` and `lsn` targets are supported.
   Types `xid`, `name`, and `immediate` require a volume-snapshot base — see
   [reference §8.3](restore-from-volume-snapshot.md#83-recovery-target-types).
4. **Wait and verify.** `status.restore.source.requestedRecoveryTarget` echoes the
   `{type, value}` you asked for. Confirm `credentialSweep.completed: true`.
5. **Validate that data past the target is absent** and data before it is present,
   then re-provision access ([§4.4](#44-application-cutover)).

> **Drill result (2026-07-29):** recovery to a `type: time` target reached
> sweep-complete in **~105 s**; the restored DB contained exactly the rows
> committed before the target and excluded rows written after it.

### 4.4 Application cutover

A restore resets credentials by design. By default the restored cluster gets a
**new superuser secret**, and the operator runs a **credential sweep** that
disables all unmanaged application login roles recovered from the backup — so
stale source credentials cannot silently keep working.

> **Exception — externally-managed superuser secret.** If the source cluster set
> `spec.passwordConfig.superuserExternalSecretRef`, the operator does **not**
> create a superuser secret for the restored cluster — it reuses the referenced
> external Secret as-is (it only validates it; it never creates, owns, or mutates
> it). Restoring into the same namespace would make the source and recovered
> clusters **share the same privileged credential**. Before restoring, create (or
> rotate to) a **distinct** external superuser Secret in the restore namespace —
> carrying the `cnpg.io/reload="true"` label — and point the restored cluster's
> `passwordConfig` at it.

> **Fence writes to the original first.** During cutover both clusters can be
> running, and any client, worker, or CronJob still pointed at the **original**
> primary will keep writing to it. Those writes are **not** replicated to the
> recovered cluster and are lost the moment you cut over. Before enabling the
> recovered cluster for traffic, put the source into maintenance / stop its
> writers (scale down the application, pause CronJobs, or otherwise fence writes),
> and confirm exactly **one** primary is writable. If the original cluster is gone
> (whole-cluster loss) this is already satisfied.

After `credentialSweep.completed: true`:

1. **Fence writes to the original cluster** (unless it is already lost): stop or
   scale down every writer, and confirm no client is still connected to the old
   primary. This guarantees the recovered cluster is the single source of truth
   from cutover onward.
2. Recreate a `PostgresDatabase` for each application database against the restored
   cluster. By default the operator provisions fresh role secrets and grants
   privileges.

   > **Exception — externally-managed role secrets.** If the `PostgresDatabase`
   > sets `spec.databases[].passwordConfig` (external admin/RW Secrets), the
   > operator does **not** create role secrets — it validates and reuses the
   > referenced `externalAdminSecretRef` / `externalRWSecretRef` Secrets as-is.
   > Create (or restore/rotate) those Secrets in the recovery namespace **before**
   > applying the `PostgresDatabase`; each must carry the `cnpg.io/reload="true"`
   > label and the admin and RW refs must point at different Secrets. In this mode
   > the "new secrets exist" checkpoint in step 3 does not apply — the Secrets you
   > provisioned are the credentials.
   ```yaml
   apiVersion: platform.splunk.com/v1alpha1
   kind: PostgresDatabase
   metadata:
     name: myapp-db-restored
     namespace: <namespace>
   spec:
     clusterRef:
       name: mydb-restored
     databases:
       - name: myapp
   ```
3. Wait for the `PostgresDatabase` to reach `Ready` (new secrets exist at this
   point — measured ~35 s in the drills).
4. Point the application at the new secrets and the restored cluster's endpoint.
5. Retire the original cluster once you are satisfied.

See [PostgresDatabase Managed Roles](postgresdatabase-managed-roles.md) for detail.

### 4.5 CRITICAL: do not reuse a restore name

The default `clusterDeletionPolicy` is **`Retain`**. Deleting a restored
`PostgresCluster` under that policy **orphans** the underlying CNPG `Cluster` and
its PVC — they keep existing. If you then re-apply a restore CR with the **same
name**, the operator **adopts the orphaned volume instead of re-running recovery**.
You get the *previous* attempt's data with no fresh recovery — silently wrong, no
error surfaced.

**Avoid it — do any one of these:**

- Use a **unique name** for every restore attempt (simplest); **or**
- Set `clusterDeletionPolicy: Delete` on the restore CR (used throughout the
  drills above); **or**
- Fully tear down before reusing a name:
  ```bash
  kubectl delete postgrescluster <name> -n <namespace>
  kubectl delete cluster.postgresql.cnpg.io <name> -n <namespace>
  kubectl delete pvc -l cnpg.io/cluster=<name> -n <namespace>
  ```

Observed 2026-07-29: a re-applied restore reusing a name showed the prior
attempt's data; the CNPG `Cluster` `creationTimestamp` proved the old volume was
adopted and no recovery ran.

---

## 5. Verification and troubleshooting

### 5.1 Check restore progress

```bash
kubectl get postgrescluster <name> -n <namespace> -o jsonpath='{.status.restore}'
kubectl describe postgrescluster <name> -n <namespace>
kubectl get cluster.postgresql.cnpg.io <name> -n <namespace>   # CNPG-level phase
```

`credentialSweep.completed: true` is the signal that post-restore work is done and
application roles can be re-provisioned. Until then the cluster is still working.

### 5.2 Cluster stuck in `Provisioning`

Almost always a compatibility mismatch the operator does not validate: PostgreSQL
major-version difference, lower capacity parameters in the target class, or smaller
storage than the backup. Recheck the [preflight](#4-recovery-drills). See
[reference §5.2](restore-from-volume-snapshot.md#52-restoring-across-different-classes-not-supported).

### 5.3 Confirm the object-store recovery window

Object-storage and PITR recovery are bounded by what is retained in the bucket.
Check the managed `ObjectStore` status for the first recoverable point and last
successful backup:

```bash
kubectl get objectstore <source>-object-store -n <namespace> \
  -o jsonpath='{.status}' | jq .
aws s3 ls s3://<bucket>/<prefix>/ --recursive | tail -20   # base/ and wals/
```

A PITR target older than the first recoverable point, or a WAL segment pruned by
the `retentionPolicy`, cannot be recovered. WAL archiving also **stops** when a
`Retain`ed source cluster's `PostgresCluster` CR is deleted (the operator strips
the archiver during finalization) — existing objects stay usable, but no new WAL
ships. See [Object Storage Backups §9](backup-object-storage.md#9-retention-and-cluster-deletion).

### 5.4 Recovered credentials do not work

Expected. The credential sweep disables them. Re-provision via `PostgresDatabase`
([§4.4](#44-application-cutover)) — do not attempt to reuse source secrets.

---

## 6. Validation evidence

All three recovery paths were executed end-to-end on a KIND cluster (operator
build `dr-cpi1867`) on 2026-07-29 and verified for correctness. The three
restores were applied together from a single manifest, each with a fresh name and
`clusterDeletionPolicy: Delete`, recovering from two live source clusters:

- `vol-src` — database `appdb`, table `public.orders` with 3 rows (ids 1,2,3);
  used by Scenario A (from its CSI snapshot) and Scenario B (from its S3 archive).
- `pitr-src` — database `pitrdb`, table `public.events`; rows 1,2,3 and 6,7 were
  committed **before** the recovery target `2026-07-29T10:05:53.159125Z`, rows 8,9
  **after** it.

**Restore status — all three reached `Ready` with the credential sweep complete,
and each recorded the correct source:**

```text
$ kubectl get postgrescluster dr-ev-vol -n dr-test \
    -o jsonpath='PHASE={.status.phase} RESTORE={.status.restore}'
PHASE=Ready  RESTORE={"credentialSweep":{"completed":true},
  "source":{"volumeSnapshot":"vol-src-backup-20260729103000"}}

$ kubectl get postgrescluster dr-ev-os -n dr-test -o jsonpath='...'
PHASE=Ready  RESTORE={"credentialSweep":{"completed":true},
  "source":{"objectStorage":"vol-src"}}

$ kubectl get postgrescluster dr-ev-pitr -n dr-test -o jsonpath='...'
PHASE=Ready  RESTORE={"credentialSweep":{"completed":true},
  "source":{"objectStorage":"pitr-src",
    "requestedRecoveryTarget":{"type":"time","value":"2026-07-29T10:05:53.159125Z"}}}
```

**Data verification — each recovered exactly the expected rows:**

```text
# A — volume snapshot: appdb.public.orders
3 rows: 1,2,3                                    ✓ snapshot contents intact

# B — object storage (latest WAL): appdb.public.orders
3 rows: 1,2,3                                    ✓ recovered from S3 base + WAL

# C — PITR to 2026-07-29T10:05:53.159125Z: pitrdb.public.events
 id |      label      |          created_at
----+-----------------+-------------------------------
  1 | before-target-1 | 2026-07-29 10:02:33.123522+00
  2 | before-target-2 | 2026-07-29 10:02:33.123522+00
  3 | before-target-3 | 2026-07-29 10:02:33.123522+00
  6 | phase2-before-1 | 2026-07-29 10:05:50.154103+00
  7 | phase2-before-2 | 2026-07-29 10:05:50.154103+00
(5 rows)                                         ✓ rows 8,9 (10:05:56, AFTER
                                                   target) correctly excluded
```

**Credential sweep — recovered application roles were login-disabled**, so stale
source credentials cannot authenticate against the restored cluster:

```text
$ kubectl exec -n dr-test dr-ev-vol-1 -c postgres -- \
    psql -U postgres -tAc \
    "select rolname, rolcanlogin from pg_roles where rolname like 'appdb%';"
appdb_admin|f
appdb_rw|f
```

> **Note on timing.** This verification run applied all three restores
> **concurrently** on a single-node KIND cluster already hosting the source
> clusters, so the observed times to `Ready` (~1–3.5 min) reflect that contention
> and are **not** representative RTO. The per-method RTO figures in
> [§2](#measured-reference-values) come from **sequential** drills and remain the
> reference. This run's purpose was functional correctness, which passed on all
> three paths.

---

## 7. DR testing schedule

DR procedures are only real if they are exercised. Run these drills on a
non-production copy and record RTO, RPO, and any deviation each time.

| Cadence | Drill | Pass criteria |
|---|---|---|
| **Monthly** | Volume snapshot restore ([§4.1](#41-scenario-a--volume-snapshot-restore)) with a fresh cluster name **in the snapshot's own namespace** | Cluster `Ready`, sweep complete, row counts match, RTO recorded |
| **Monthly** | Confirm object-store recovery window ([§5.3](#53-confirm-the-object-store-recovery-window)) covers the retention SLA | First recoverable point older than the required window; `base/` + `wals/` present |
| **Quarterly** | Object-storage restore ([§4.2](#42-scenario-b--object-storage-restore)) from S3 | Cluster `Ready` from object store alone, data validated, RTO recorded |
| **Quarterly** | PITR to a chosen timestamp ([§4.3](#43-scenario-c--point-in-time-recovery-pitr)) | Data before target present, data after target absent, RTO recorded |
| **Per release / major change** | Full end-to-end drill including [application cutover](#44-application-cutover) | Application connects with re-provisioned credentials against the restored cluster |
| **After any class change** | Re-validate PG major version + capacity parameters against the last backup | Restore preflight ([§4](#4-recovery-drills)) passes |

> **Volume snapshots are namespace-scoped.** The restore API takes only the
> snapshot *name* and looks it up in the restored cluster's namespace, so a
> snapshot restore must run **in the same namespace as the `VolumeSnapshot`** (with
> a fresh cluster name). To rehearse in a separate namespace, first copy/import the
> `VolumeSnapshot` into that namespace (e.g. via a `VolumeSnapshotContent` with a
> pre-provisioned handle). Object-storage and PITR restores are not affected — they
> read from the bucket, not from a namespaced snapshot.

Each drill should:

1. Use a **fresh restore-target name** and `clusterDeletionPolicy: Delete`
   ([§4.5](#45-critical-do-not-reuse-a-restore-name)).
2. Record **measured RTO** (restore CR created → sweep complete) and re-provision
   time, comparing against the [reference values](#measured-reference-values)
   scaled for data size.
3. Confirm the **RPO** matches expectations for the method
   (snapshot interval vs. latest WAL vs. chosen target).
4. Tear down the drill cluster afterwards.

Track drift over time: growing RTO usually signals database growth or object-store
bandwidth limits and should feed back into capacity and backup-schedule planning.

---

## 8. Related documentation

- [Backup and Restore Choices](backup-and-restore-choices.md) — choosing a strategy
- [Restoring a PostgresCluster](restore-from-volume-snapshot.md) — full restore API reference
- [Volume Snapshot Backups](backup-volume-snapshots.md) — snapshot backup setup
- [Object Storage Backups](backup-object-storage.md) — Barman/S3 backup setup
- [PostgresDatabase Managed Roles](postgresdatabase-managed-roles.md) — re-provisioning access

Upstream CloudNativePG background:

- [CNPG — Backup](https://cloudnative-pg.io/documentation/current/backup/) — base-backup frequency and its effect on RTO
- [CNPG — WAL archiving](https://cloudnative-pg.io/documentation/current/wal_archiving/) — `archive_timeout` and the deterministic RPO it provides
- [CNPG — Recovery](https://cloudnative-pg.io/documentation/current/recovery/) — PITR and recovery-target model
- [CNPG — Replication](https://cloudnative-pg.io/documentation/current/replication/) — synchronous replication for RPO = 0 (a CNPG capability **not currently exposed** by the Splunk Operator API; background only)
