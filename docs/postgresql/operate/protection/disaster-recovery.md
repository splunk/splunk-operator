---
title: Recover after data loss
parent: Protect and recover data
grand_parent: Operate PostgreSQL
nav_order: 4
---

Image references below are template variables. Set them to approved complete
`tag@sha256` references and render standalone YAML with `envsubst` before applying.
See [image configuration](../../deploy/public-artifacts.md#pull-a-selected-image) for the
required numeric-leading PostgreSQL tags and same-namespace pull Secrets.


# PostgreSQL Disaster Recovery Runbook

If you are responding to an incident, [choose a recovery path](#3-failure-mode--recovery-path) based on what failed and which backups survived. Prepare the target before you run the chosen restore procedure. All paths share the restore and data checks. After those checks, [test in isolation](#run-an-isolated-restore-drill) or [move production traffic](#44-application-cutover).

If you are rehearsing a restore, start with the [isolated drill](#run-an-isolated-restore-drill). Use the [testing schedule](#6-dr-testing-schedule) to plan repeat drills. Use the [restore API and compatibility details](restore-from-volume-snapshot.md) to check supported fields and targets.

---

## 1. Recovery model in one paragraph

Create a separate `PostgresCluster` and set `spec.bootstrapFrom` to the backup source. The operator loads the backup as it provisions the cluster. You cannot change `bootstrapFrom` to run another restore later.

Leave the source cluster and its Helm release unchanged. If the operator still runs, use it to manage the restore target. You do not need a second Helm release.

Three recovery sources are supported:

| Source | `bootstrapFrom` | Recovers to | Requires |
|---|---|---|---|
| **Volume snapshot** | `volumeSnapshot` | The moment the snapshot was taken | A CSI `VolumeSnapshot` in the namespace |
| **Object storage** | `objectStorage` | The latest archived WAL | A Barman base backup + WAL in S3 |
| **Point-in-time (PITR)** | `objectStorage` (or `volumeSnapshot` + `walArchive`) with `recoveryTarget` | An arbitrary instant you choose | A base backup + continuous WAL covering that instant |

---

## 2. Recovery objectives (RPO / RTO)

The recovery point objective (RPO) is the most data loss you can accept, measured in time. Track the gap between the last recoverable write and the failure.

The recovery time objective (RTO) is the most downtime you can accept. Measure the restore and the time to make the application work on the recovered cluster.

| Method | RPO (data loss) | RTO (time to recover) | Notes |
|---|---|---|---|
| **Volume snapshot** | Recovers to the last usable snapshot. Later writes are lost. | Clone the volume and rebuild standbys. | No replay from an external WAL archive. |
| **Object storage (latest WAL)** | Depends on the last WAL segment that reached the archive. CNPG defaults `archive_timeout` to **5 minutes**, but a slow or failed upload can increase data loss. | Fetch a base backup and replay archived WAL. | Survives loss of the Kubernetes storage system if the archive is available. |
| **PITR** | Choose a target that the retained base backup and WAL cover. | Restore the base and replay WAL up to the target. | Use it to recover to a point before a bad change. |

Backups do not guarantee zero data loss. A restore cannot replay changes that did not reach the archive. Synchronous replication can protect writes during failover, but it can block writes until a replica acknowledges them. The `PostgresClusterClass` API does not expose the CNPG `dataDurability` or `spec.postgresql.synchronous` settings. If a workload cannot tolerate any data loss, do not rely on these restore methods to meet that requirement.

Object-store recovery takes time to fetch the base backup and replay WAL. More WAL since the last base backup can make recovery take longer. CloudNativePG gives weekly base backups as a starting point. Use the [testing schedule](#6-dr-testing-schedule) to measure recovery time with data that represents production. If WAL replay takes you past your objective, take base backups more often.

## 3. Failure mode → recovery path

Choose the path that matches the failure and the backups that survived.

| Failure | Recommended path | Why |
|---|---|---|
| Node or PVC lost, storage system intact, recent snapshot exists | [Volume snapshot](#41-scenario-a--volume-snapshot-restore) | Restore the volume from the same storage system. |
| Storage system or whole cluster lost | **Object storage** ([§4.2](#42-scenario-b--object-storage-restore)) | Backups live independently of PostgreSQL volumes. |
| Logical corruption / bad migration / erroneous `DELETE` at a known time | **PITR** ([§4.3](#43-scenario-c--point-in-time-recovery-pitr)) | Stop recovery *just before* the damaging change. |
| Snapshot exists but you also need WAL replay past it | **Volume snapshot + `walArchive`** ([reference §8.2](restore-from-volume-snapshot.md#82-pitr-from-a-volume-snapshot)) | Fast base + forward replay. |
| Need to know how far back you *can* recover | Check the `ObjectStore` recovery window ([§5.3](#53-confirm-the-object-store-recovery-window)) | Bounds every object-store/PITR option. |

---

## 4. Recovery drills

Use `<namespace>` and `<your-class>` for the target namespace and class. The examples name the source cluster `mydb`. Use a [fresh name for each restore](#45-critical-do-not-reuse-a-restore-name). If you reuse a name, the operator can adopt old data instead of restoring the backup.

If you are restoring into a new Kubernetes cluster, prepare the environment before creating the restore target:

1. Make sure that the Splunk Operator runs with the `PostgresController` feature gate enabled.
2. Make sure that CloudNativePG runs. If you need object storage or point-in-time recovery, [install the Barman plugin and its `ObjectStore` CRD](backup-object-storage.md#2-prerequisites).
3. Make sure that the `PostgresClusterClass` that the restore references exists in the target environment.

Before any restore:

1. Use the source's `PostgresClusterClass`. For a snapshot restore, the storage provisioner, PostgreSQL server configuration, and major version must match. The operator does not check these requirements at admission. A mismatch can leave the cluster in `Provisioning`. See the [restore compatibility rules](restore-from-volume-snapshot.md#5-restore-compatibility).
2. Make sure that the target class matches or exceeds the source's capacity parameters, such as `max_connections`.
3. If the restore needs archived WAL or an object-storage backup, make sure that `cnpg.backup.barmanObjectStore` points to the source archive. Make sure that the Secret named in `s3Credentials` exists in the restore namespace. The Secret must grant read access to the archive.
4. Use a [fresh restore target name](#45-critical-do-not-reuse-a-restore-name). The examples set `clusterDeletionPolicy: Delete` for drill cleanup.
5. If the restore target uses `spec.passwordConfig.superuserExternalSecretRef`, create a separate Secret in the restore namespace. Give it the `cnpg.io/reload="true"` label. Set the restored cluster's `passwordConfig` to use that Secret. If both clusters use the same Secret, they share privileged credentials.

### 4.1 Scenario A — Volume snapshot restore

Use this path if a usable CSI snapshot exists and you can lose writes made after it. The drill below ran on a 1 GiB database.

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
     images:
       postgresql:
         "17": ${TEST_PG17_IMAGE}
       pgbouncer: ${TEST_PGBOUNCER_IMAGE}
       # Optional Secrets must exist in this PostgresCluster's namespace.
       # imagePullSecrets:
       #   - name: registry-reader
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
4. Check the data, then [prepare restore access](#prepare-access-to-the-restored-cluster).

> **Drill result (2026-07-29):** snapshot `READYTOUSE` ~22 s after firing;
> physical restore to `ClusterReady` + sweep in **~36 s**; all rows intact via
> freshly provisioned credentials; pre-restore application credentials correctly
> rejected (`FATAL: password authentication failed`).

### 4.2 Scenario B — Object storage restore

Use this path if no usable snapshot remains but the source archive is available. Without a recovery target, the restore replays WAL to the latest recoverable point. The drill took about 83 seconds on a 1 GiB database.

1. Make sure that the target can [reach the archive](#53-confirm-the-object-store-recovery-window). Record the source `serverName`. It names the archive directory that holds the source cluster's backups and WAL.
2. **Create the restored cluster** (no `recoveryTarget` = recover to latest WAL):
   ```yaml
   apiVersion: platform.splunk.com/v1alpha1
   kind: PostgresCluster
   metadata:
     name: mydb-restored          # NEW, unused name
     namespace: <namespace>
   spec:
     images:
       postgresql:
         "17": ${TEST_PG17_IMAGE}
       pgbouncer: ${TEST_PGBOUNCER_IMAGE}
       # Optional Secrets must exist in this PostgresCluster's namespace.
       # imagePullSecrets:
       #   - name: registry-reader
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
4. Check the data, then [prepare restore access](#prepare-access-to-the-restored-cluster).

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
     images:
       postgresql:
         "17": ${TEST_PG17_IMAGE}
       pgbouncer: ${TEST_PGBOUNCER_IMAGE}
       # Optional Secrets must exist in this PostgresCluster's namespace.
       # imagePullSecrets:
       #   - name: registry-reader
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
   then [prepare restore access](#prepare-access-to-the-restored-cluster).

> **Drill result (2026-07-29):** recovery to a `type: time` target reached
> sweep-complete in **~105 s**; the restored DB contained exactly the rows
> committed before the target and excluded rows written after it.

### Prepare access to the restored cluster

By default, the operator creates a new superuser Secret for the restored cluster. If you configured an external superuser Secret during [restore preparation](#4-recovery-drills), the operator checks and uses it. The operator does not create, own, or change that Secret.

The operator runs a credential sweep to disable unmanaged application login roles from the backup.

After `credentialSweep.completed: true`:

1. Recreate a `PostgresDatabase` for each application database against the restored
   cluster. By default the operator provisions fresh role secrets and grants
   privileges.

   If you set `spec.databases[].passwordConfig`, prepare the external Secrets before you apply the resource. Put them in the recovery namespace. Give each Secret the `cnpg.io/reload="true"` label. Set `externalAdminSecretRef` and `externalRWSecretRef` to different Secrets. The operator checks and uses these Secrets instead of creating new ones.
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
2. Wait for `PostgresDatabase` to reach `Ready`. This took about 35 seconds in the drills. If the operator manages the Secrets, make sure that the new Secrets exist.

See [managed database roles](../access/postgresdatabase-managed-roles.md) for details.

### Run an isolated restore drill

Leave the source cluster, its Helm release, production applications, and scheduled jobs unchanged. Use a fresh restore target and a test client that cannot send writes to the source.

1. Run the restore that matches your backup: [volume snapshot](#41-scenario-a--volume-snapshot-restore), [object storage](#42-scenario-b--object-storage-restore), or [point-in-time recovery](#43-scenario-c--point-in-time-recovery-pitr). If you came from Deploy, use the object-storage path.
2. After the data checks pass, [prepare access to the restored cluster](#prepare-access-to-the-restored-cluster). Use credentials that are separate from the source.
3. Use the test client to connect to the restored endpoint. Make sure that it can read the expected data and complete a write. Do not change any production endpoint or Secret.
4. Record the data loss and the time to restore and test access. Compare both results with your limits. Remove only the drill resources when you finish.

### 4.4 Application cutover

Use these steps only when you intend to move production traffic. If you are rehearsing, use the [isolated drill](#run-an-isolated-restore-drill) instead.

1. [Prepare access to the restored cluster](#prepare-access-to-the-restored-cluster) after the data checks pass.
2. If the source still runs, stop all client writes to it before you move traffic. Stop or scale down applications. Pause scheduled jobs. Make sure that no client is connected to the old primary. Keep only the restored primary writable during cutover. Writes to the old primary do not reach the restored cluster.
3. Point the application at the restored endpoint and its role Secrets.
4. Test the application against the restored cluster. Make sure that backups protect the restored cluster before you [retire the source](../retire.md).

### 4.5 CRITICAL: do not reuse a restore name

Use a new name for each restore attempt. With the default `clusterDeletionPolicy: Retain`, deleting the `PostgresCluster` leaves the CNPG cluster and its PVC. If you reuse the name, the operator can adopt the old volume without reporting an error. The data then comes from the previous attempt, not from a new restore.

If you need to reuse a name, use `clusterDeletionPolicy: Delete` on the drill target or remove the old drill resources first. The commands below delete the named cluster and matching volumes. Make sure that the name and namespace identify the old drill target, not the source:
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

Start with the [cluster status and events](../observability/index.md#find-the-failing-component). If recovery does not start, repeat the [preflight](#4-recovery-drills). Check the PostgreSQL major version, capacity settings, and storage size. The operator does not check every [compatibility rule](restore-from-volume-snapshot.md#52-restoring-across-different-classes-not-supported).

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

The credential sweep disables unmanaged login roles from the backup. [Set up access to the restored cluster](#prepare-access-to-the-restored-cluster). Do not reuse source Secrets.

---

## 6. DR testing schedule

Run these drills on a non-production copy. Record the recovery time, data loss, and any departure from the procedure.

| Cadence | Drill | Pass criteria |
|---|---|---|
| **Monthly** | Volume snapshot restore ([§4.1](#41-scenario-a--volume-snapshot-restore)) with a fresh cluster name **in the snapshot's own namespace** | Cluster `Ready`, sweep complete, row counts match, RTO recorded |
| **Monthly** | Confirm object-store recovery window ([§5.3](#53-confirm-the-object-store-recovery-window)) covers the retention SLA | First recoverable point older than the required window; `base/` + `wals/` present |
| **Quarterly** | Object-storage restore ([§4.2](#42-scenario-b--object-storage-restore)) from S3 | Cluster `Ready` from object store alone, data validated, RTO recorded |
| **Quarterly** | PITR to a chosen timestamp ([§4.3](#43-scenario-c--point-in-time-recovery-pitr)) | Data before target present, data after target absent, RTO recorded |
| **Per release / major change** | [Isolated drill](#run-an-isolated-restore-drill) with a test application | Test application reads and writes with new credentials. Production traffic stays on the source. |
| **After any class change** | Re-validate PG major version + capacity parameters against the last backup | Restore preflight ([§4](#4-recovery-drills)) passes |

> **Volume snapshots are namespace-scoped.** The restore API takes only the
> snapshot *name* and looks it up in the restored cluster's namespace, so a
> snapshot restore must run **in the same namespace as the `VolumeSnapshot`** (with
> a fresh cluster name). To rehearse in a separate namespace, first copy/import the
> `VolumeSnapshot` into that namespace (e.g. via a `VolumeSnapshotContent` with a
> pre-provisioned handle). Object-storage and PITR restores are not affected — they
> read from the bucket, not from a namespaced snapshot.

For each drill:

1. Use a **fresh restore-target name** and `clusterDeletionPolicy: Delete`
   ([§4.5](#45-critical-do-not-reuse-a-restore-name)).
2. Record the time from restore resource creation to credential sweep completion. Record the time to restore application access separately. Compare the full recovery time with your objective and previous drills.
3. Confirm the **RPO** matches expectations for the method
   (snapshot interval vs. latest WAL vs. chosen target).
4. Tear down the drill cluster afterwards.

If recovery takes longer in later drills, check database size and object-store bandwidth. Use the results to review capacity and backup frequency.

---

## 7. Related documentation

- [Backup and Restore Choices](backup-and-restore-choices.md) — choosing a strategy
- [Restoring a PostgresCluster](restore-from-volume-snapshot.md) — full restore API reference
- [Volume Snapshot Backups](backup-volume-snapshots.md) — snapshot backup setup
- [Object Storage Backups](backup-object-storage.md) — Barman/S3 backup setup
- [PostgresDatabase Managed Roles](../access/postgresdatabase-managed-roles.md) — re-provisioning access

Upstream CloudNativePG background:

- [CNPG — Backup](https://cloudnative-pg.io/documentation/current/backup/) — base-backup frequency and its effect on RTO
- [CNPG WAL archiving](https://cloudnative-pg.io/documentation/current/wal_archiving/) explains when `archive_timeout` closes a WAL segment.
- [CNPG — Recovery](https://cloudnative-pg.io/documentation/current/recovery/) — PITR and recovery-target model
- [CNPG — Replication](https://cloudnative-pg.io/documentation/current/replication/) — synchronous replication for RPO = 0 (a CNPG capability **not currently exposed** by the Splunk Operator API; background only)
