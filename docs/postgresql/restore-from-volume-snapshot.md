---
title: Restoring from a Volume Snapshot
parent: PostgreSQL
nav_order: 6
---

# Restoring a PostgresCluster from a Volume Snapshot

This guide covers restoring a PostgreSQL cluster from a volume snapshot backup. It is intended for **users** who need to recover a `PostgresCluster` from an existing `VolumeSnapshot`.

Before following this guide, ensure your `PostgresClusterClass` has volume snapshot backups configured and at least one `VolumeSnapshot` is available in your namespace. See [Automated Backups via Volume Snapshots](backup-volume-snapshots.md) for backup setup.

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
apiVersion: enterprise.splunk.com/v4
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
apiVersion: enterprise.splunk.com/v4
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

- **Point-in-time recovery (PITR) is not supported in v1.** Restore always recovers to the exact moment the snapshot was taken. PITR (recovering to an arbitrary timestamp between backups) requires continuous WAL streaming to object storage (a WAL archive, distinct from the per-PVC WAL volume snapshot covered by `spec.bootstrapFrom.volumeSnapshot.walStorage`) and is planned for a future release.
- **Restore from object storage is not supported in v1.** Only volume snapshot restore is available. Object storage restore is planned for a future release.
- **Cross-class / cross-version restore is not supported.** The operator does not validate that the snapshot's PostgreSQL version matches the target class at admission time, and a mismatch results in a failed cluster with no clear error surfaced through the operator. Matching the PostgreSQL major version of the source is the user's responsibility — see [§5.2](#52-restoring-across-different-classes-not-supported).
