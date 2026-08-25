---
title: Restoring from a Volume Snapshot
parent: PostgreSQL
nav_order: 6
---

# Restoring a PostgresCluster

This guide covers restoring a PostgreSQL cluster from a backup. It is intended for **users** who need to recover a `PostgresCluster`. Three restore modes are supported:

- **Volume snapshot** — recover to the exact moment a snapshot was taken ([§3](#3-performing-a-restore)).
- **Point-in-time recovery (PITR)** — recover to an arbitrary timestamp, transaction ID, or LSN by replaying WAL from an object-store archive on top of a volume snapshot or object-store base backup ([§8](#8-point-in-time-recovery-pitr)).
- **Object storage** — recover entirely from an object-store (barman-cloud) backup, no volume snapshot required ([§9](#9-restoring-from-object-storage)).

Before following this guide, ensure your `PostgresClusterClass` has backups configured and a suitable backup is available. See [Automated Backups via Volume Snapshots](backup-volume-snapshots.md) and [Object Storage Backups](backup-object-storage.md) for backup setup. PITR and object-storage restore both require a `barmanObjectStore` configured on the class.

---

## 1. How restore works

Restoring a cluster is modelled as **creating a new `PostgresCluster`** with a `bootstrapFrom` field that points to an existing `VolumeSnapshot`. The operator bootstraps the underlying database from the snapshot instead of initializing a fresh empty database.

The original cluster is not affected — it continues running independently. You manage both clusters separately and delete the original when you are satisfied with the restored one.

Key properties of the restore process:

- **`bootstrapFrom` is immutable.** Once set at creation time, it cannot be changed. This is enforced by the operator. The field records how the cluster was created and must remain stable for the lifetime of the cluster.
- **Restore is one-time.** The operator restores the cluster once during initial provisioning. Subsequent reconciles do not re-trigger the restore.
- **The restored cluster is fully independent.** It has its own superuser secret, its own lifecycle, and its own backup schedule (if configured in the class).

---

## 2. What gets restored

A volume snapshot is a consistent point-in-time copy of the PostgreSQL data directory (`PGDATA`). Restoring from it recovers:

- All databases and their schemas, tables, sequences, and data as they existed at snapshot time.
- All PostgreSQL roles and their privilege grants.

What is **not** restored:

- Data written after the snapshot was taken.
- Application-level credentials managed by `PostgresDatabase` resources (see [Section 4](#4-credentials-after-restore)).

---

## 3. Performing a restore

### 3.1 Find the snapshot to restore from

List available snapshots in your namespace:

```bash
kubectl get volumesnapshots -n <namespace>
```

Choose a snapshot with `READYTOUSE = true`. Snapshot names follow the pattern `<cluster-name>-backup-<timestamp>`.

### 3.2 Create the restored cluster

Create a new `PostgresCluster` with `spec.bootstrapFrom.volumeSnapshot.storage` set to the snapshot name:

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresCluster
metadata:
  name: mydb-restored
  namespace: <namespace>
spec:
  class: <your-class>
  clusterDeletionPolicy: Delete
  bootstrapFrom:
    volumeSnapshot:
      storage: mydb-backup-20260501120000   # name of the VolumeSnapshot
```

If the source cluster had a separate WAL volume (configured in the class with `walClassName`), also specify the WAL snapshot:

```yaml
    bootstrapFrom:
      volumeSnapshot:
        storage: mydb-backup-20260501120000
        walStorage: mydb-wal-backup-20260501120000
```

### 3.3 Wait for the cluster to become Ready

```bash
kubectl get postgrescluster mydb-restored -n <namespace> -w
```

When `PHASE` shows `Ready`, the cluster is operational.

### 3.4 Verify the restore completed

Check the restore status:

```bash
kubectl get postgrescluster mydb-restored -n <namespace> \
  -o jsonpath='{.status.restore}'
```

A complete restore looks like:

```json
{
  "source": { "volumeSnapshot": "mydb-backup-20260501120000" },
  "credentialSweep": { "completed": true }
}
```

`credentialSweep.completed: true` confirms the post-restore credential sweep ran successfully. Until that field is true, the cluster is still performing post-restore work and application roles are not yet re-enabled.

---

## 4. Credentials after restore

### 4.1 Superuser password is reset

The restored cluster gets a **new, freshly generated superuser secret** — independent of the original cluster's secret. The original cluster's superuser password is not carried over.

To retrieve the superuser credentials for the restored cluster:

```bash
kubectl get secret mydb-restored-secret -n <namespace> \
  -o jsonpath='{.data.password}' | base64 -d
```

### 4.2 Application role credentials are revoked

A volume snapshot restores the entire PostgreSQL role catalog, including all application roles that existed at backup time. However, the Splunk Operator **revokes login access for all application roles** as soon as the restored cluster becomes reachable after promotion. This happens on the first reconcile pass where the database accepts connections, so there can be a brief window after promotion during which the restored credentials are still valid before the sweep runs.

This means: after a restore, no application role can authenticate until you explicitly re-provision it via a `PostgresDatabase` resource.

**Why this happens:** the operator has no knowledge of the original cluster's role secrets after restore. Those secrets were managed by the original cluster and are not transferred. Rather than leave stale credentials that could be used without the operator's awareness, the operator disables them.

### 4.3 Re-enabling access for your applications

Create a `PostgresDatabase` resource pointing at the restored cluster for each database your application uses:

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

The operator will:
1. Create new role secrets with fresh passwords.
2. Publish role intent on the `PostgresDatabase` and wait for the `PostgresCluster` to reconcile the managed roles (`myapp_admin`, `myapp_rw`).
3. Grant the appropriate privileges on the database.

Your application can then connect using the generated secrets. The data is immediately accessible — the credential sweep does not affect the data itself, only the ability to authenticate. For more detail, see [PostgresDatabase Managed Roles](postgresdatabase-managed-roles.md).

---

## 5. Restore compatibility

### 5.1 Restoring within the same class (supported)

Restoring a cluster using the same `PostgresClusterClass` as the source cluster is the **supported and tested path**. The PostgreSQL version, storage configuration, and server parameters all match — the snapshot is guaranteed to be compatible.

### 5.2 Restoring across different classes (not supported)

**Cross-class restore — restoring a snapshot into a cluster that uses a different `PostgresClusterClass` than the source — is not supported.** The operator performs no cross-class or cross-version compatibility validation: it does not check the snapshot's PostgreSQL version against the target class, and it will not block an incompatible restore at admission time. **Before restoring, you must ensure the target class uses the same PostgreSQL major version as the source so the restore is safe to proceed.**

It is technically possible to point a restore at a different class, but doing so is unsupported and entirely at your own risk. The following class differences can cause the restore to fail completely, leaving the cluster stuck in `Provisioning` with no useful error message:

| Difference | Effect |
|---|---|
| **Different PostgreSQL major version** (e.g. class uses PG18, snapshot is from PG17) | PostgreSQL refuses to start — physical restore requires matching major versions. The cluster will not recover without manual intervention. |
| **Lower values for capacity-related PostgreSQL parameters** (e.g. `max_connections`, `max_prepared_transactions`, `max_locks_per_transaction`, `max_worker_processes`) | PostgreSQL refuses to start if the new cluster is configured with lower values than what was recorded in the snapshot's control file. Going higher is safe; going lower is a hard failure. |
| **Smaller storage size** | The PVC clone will fail if the requested storage is smaller than the snapshot's data size. |

Because none of this is validated by the operator, verifying compatibility is your responsibility: confirm the target class uses the same PostgreSQL major version and equal or higher values for all capacity-related parameters compared to the source class before starting the restore.

---

## 6. Cutover procedure

When you are ready to switch your application to the restored cluster:

1. Verify the restored cluster is `Ready` and `status.restore.credentialSweep.completed` is `true`.
2. Create `PostgresDatabase` resources on the restored cluster for all databases your application uses.
3. Wait for the `PostgresDatabase` resources to reach `Ready` phase — new secrets are created at this point.
4. Update your application's configuration to use the new secrets and the restored cluster's connection endpoint (available in its ConfigMap).
5. Delete the original cluster when you no longer need it.

---

## 7. Limitations

- **Cross-class / cross-version restore is not supported.** The operator does not validate that the snapshot's PostgreSQL version matches the target class at admission time, and a mismatch results in a failed cluster with no clear error surfaced through the operator. Matching the PostgreSQL major version of the source is the user's responsibility — see [§5.2](#52-restoring-across-different-classes-not-supported).
- **Target types `name`, `xid`, and `immediate` are only supported with a volume-snapshot base.** The operator rejects them on an [`objectStorage` source](#9-restoring-from-object-storage) because CloudNativePG can only auto-select the object-store base backup for types `time` or `lsn`. With a volume snapshot the base is the snapshot itself (unambiguous), so all target types are accepted. See [§8.3](#83-recovery-target-types).

---

## 8. Point-in-time recovery (PITR)

PITR recovers the cluster to an arbitrary point **between** backups — a timestamp, transaction ID, or LSN — rather than to the exact backup point. This is done by replaying WAL (write-ahead log) segments from an object-store archive on top of a base backup.

### 8.1 Prerequisites

- The source cluster must have archived its WAL: when it was running, its class must have had `cnpg.backup.barmanObjectStore` configured with WAL archiving, so WAL segments are continuously shipped to object storage. See [Object Storage Backups](backup-object-storage.md).
- **The class the restored cluster references** (`spec.class`) — not necessarily the source cluster's class — must define `cnpg.backup.barmanObjectStore` pointing at that same archive. This is where the operator resolves the bucket path and credentials to read WAL from. In the common case both clusters use the same class, but any class that points at the source's archive works. The operator rejects a PITR/object-storage restore whose referenced class has no `barmanObjectStore`.
- You need the source cluster's **server name** in the object store — this is the folder under which its WAL is stored, matching the source cluster's name.

### 8.2 PITR from a volume snapshot

Combine a volume snapshot (the base backup) with a `walArchive` (the WAL source) and a `recoveryTarget`:

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresCluster
metadata:
  name: mydb-restored
  namespace: <namespace>
spec:
  class: <your-class>          # must define cnpg.backup.barmanObjectStore
  bootstrapFrom:
    volumeSnapshot:
      storage: mydb-backup-20260501120000
      walArchive:
        serverName: mydb        # source cluster's server name in the object store
    recoveryTarget:
      type: time
      value: "2026-05-01T13:30:00Z"
```

The snapshot restores the data directory up to the backup point; WAL segments from the archive are then replayed forward until the target time is reached. **`walArchive` is required whenever `recoveryTarget` is set on a volume-snapshot source** — the snapshot alone cannot reach a point past the moment it was taken. The operator rejects a PITR request that omits it.

### 8.3 Recovery target types

`recoveryTarget` is a discriminated pair: a `type` selects which kind of target to recover to, and `value` carries the target itself (interpreted according to `type`). This mirrors PostgreSQL's own model, where a single `recovery_target_*` is chosen, so the API cannot express two conflicting targets at once.

| `type` | `value` meaning | Example `value` |
|---|---|---|
| `time` | An RFC 3339 timestamp (recommended) | `"2026-05-01T13:30:00Z"` |
| `lsn` | A WAL log sequence number | `"0/16D68D0"` |
| `xid` | A transaction ID | `"1234567"` |
| `name` | A named restore point (`pg_create_restore_point`) | `"before-migration"` |
| `immediate` | Stop as soon as a consistent state is reached — takes no `value` | *(omit `value`)* |

`value` is required for every type except `immediate`, where it must be omitted. For example:

```yaml
    recoveryTarget:
      type: lsn
      value: "0/16D68D0"
```

`exclusive: true` stops recovery just *before* the target rather than just after (default is inclusive). It is ignored for type `immediate`. See [§7](#7-limitations) for the base-backup caveat affecting types `name`/`xid`/`immediate`.

> **Types `xid`, `name`, and `immediate` require a volume-snapshot base.** With an [object-storage source](#9-restoring-from-object-storage), CloudNativePG can only auto-select the base backup for types `time` or `lsn`; for the other types it would fall back to the latest backup, which may sit past the target and start recovery from the wrong base. The operator therefore rejects types `xid`/`name`/`immediate` on an `objectStorage` source. Use type `time`/`lsn`, or restore from a `volumeSnapshot` base (which is unambiguous).

### 8.4 Verifying a PITR restore

`status.restore.source` echoes both the source and the recovery target that was requested:

```bash
kubectl get postgrescluster mydb-restored -n <namespace> -o jsonpath='{.status.restore}'
```

```json
{
  "source": {
    "volumeSnapshot": "mydb-backup-20260501120000",
    "requestedRecoveryTarget": {
      "type": "time",
      "value": "2026-05-01T13:30:00Z"
    }
  },
  "credentialSweep": { "completed": true }
}
```

`requestedRecoveryTarget` is derived from the desired spec — it records what the restore was *asked* to recover to (mirroring the `recoveryTarget` you set), not a provider-confirmed observation of where recovery actually stopped. It is omitted for a plain snapshot restore or a recovery to the latest available WAL.

### 8.5 Large clusters: restore single-instance first

For a snapshot-based restore of a large database with `instances > 1`, only the primary is bootstrapped from the fast snapshot clone — standbys are rebuilt with a full `pg_basebackup` copy over the network. To avoid multiple full copies:

1. Restore with `instances: 1`.
2. Wait for the primary to be `Ready`.
3. Take a fresh snapshot of the recovered primary.
4. Scale to the desired instance count — standbys are then provisioned from the new snapshot.

---

## 9. Restoring from object storage

When a base backup exists in object storage (not just WAL), you can restore entirely from the object store with no volume snapshot. Use the top-level `objectStorage` source:

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresCluster
metadata:
  name: mydb-restored
  namespace: <namespace>
spec:
  class: <your-class>          # must define cnpg.backup.barmanObjectStore
  bootstrapFrom:
    objectStorage:
      serverName: mydb          # source cluster's server name in the object store
    recoveryTarget:             # optional — omit to recover to the latest WAL
      type: time
      value: "2026-05-01T13:30:00Z"
```

`volumeSnapshot` and `objectStorage` are mutually exclusive — set exactly one. Bucket path, credentials, and endpoint are resolved from the class's `barmanObjectStore` config; they are never specified in `bootstrapFrom`. This keeps restore authorization consistent with backup authorization: you can only restore from object stores your class already has access to.

Credential handling ([§4](#4-credentials-after-restore)) and cross-class compatibility ([§5](#5-restore-compatibility)) apply identically to object-storage restores — the post-restore credential sweep runs for every recovery source.
