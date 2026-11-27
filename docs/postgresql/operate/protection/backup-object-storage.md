---
title: Back up to object storage
parent: Protect and recover data
grand_parent: Operate PostgreSQL
nav_order: 2
---

Image references below are template variables. Set them to approved complete
`tag@sha256` references and render standalone YAML with `envsubst` before applying.
See [image configuration](../../deploy/public-artifacts.md#pull-a-selected-image) for the
required numeric-leading PostgreSQL tags and same-namespace pull Secrets.


# Automated Backups via Object Storage (Barman)

If Helm owns the shared cluster, [configure backups in Deploy](../../deploy/provision.md#configure-backups-before-enabling-the-cluster). Skip the class and cluster setup examples below. Once backups are enabled, [check a backup run](#53-verify-a-backup-run). Then [test recovery from the archive](disaster-recovery.md#run-an-isolated-restore-drill).

The Splunk Operator uses the **barman-cloud CNPG plugin** (`barman-cloud.cloudnative-pg.io`) as the backup method. This enables:

- **Full base backups** stored in object storage
- **Continuous WAL archiving** so you can replay archived changes as far as the archive allows
- **Retention policies** — automatic pruning of old backups

For CNPG backup internals, see [CloudNativePG — Barman Cloud Plugin](https://cloudnative-pg.io/plugin-barman-cloud/docs/).

> **Choosing a backup method:** See [Automated Backups via Volume Snapshots](backup-volume-snapshots.md) for the alternative CSI-based method. Both methods can coexist on the same cluster.

---

## 1. How it works (overview)

The operator:

1. Creates a `barmancloud.cnpg.io/v1 ObjectStore` resource (named `<cluster>-object-store`) in the cluster's namespace from the class configuration. Users do not create this manually.
2. Configures the CNPG `Cluster` with the barman-cloud plugin and `isWALArchiver: true`, enabling **continuous WAL archiving** immediately when the cluster starts.
3. Creates a CNPG **`ScheduledBackup`** resource (method `plugin`) to trigger periodic **full base backups** at the configured schedule.

CNPG then:

1. Archives completed WAL segments to the `ObjectStore` destination.
2. At each scheduled time, takes a full base backup and uploads it to the store.
3. Updates schedule metadata on the `ScheduledBackup` status.

The Splunk Operator surfaces backup state on `PostgresCluster.status.backupStatus.objectStore`.

---

## 2. Prerequisites

- The **barman-cloud CNPG plugin** (`barmancloud.cnpg.io/v1 ObjectStore` CRD) must be installed on the cluster. The SOK Helm chart can install it with `plugin-barman-cloud.enabled=true`; see [backend Helm configuration](../../../deploy/Helm.md#postgresql-backend-controllers). When managing backends separately, install the plugin before creating PostgreSQL resources:
  ```bash
  helm upgrade --install barman-cloud cnpg/plugin-barman-cloud \
    --namespace cnpg-system --create-namespace
  ```

- An **S3-compatible bucket** and credentials (access key or IAM role) with `s3:PutObject`, `s3:GetObject`, `s3:DeleteObject`, `s3:ListBucket` on the bucket, stored as a Kubernetes Secret in the same namespace as the `PostgresCluster`.

---

## 3. Setup

### Step 1 — Create the S3 credentials Secret

Users need to create one Secret in their namespace containing the AWS credentials. The operator handles everything else.

```bash
kubectl create secret generic s3-credentials -n <namespace> \
  --from-literal=accessKeyId=<ACCESS_KEY_ID> \
  --from-literal=secretAccessKey=<SECRET_ACCESS_KEY>
```

### Step 2 — Configure the PostgresClusterClass (platform team)

The full S3 configuration lives in the class. The operator automatically creates a `barmancloud.cnpg.io/v1 ObjectStore` resource in each cluster's namespace — users do not create it manually.

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresClusterClass
metadata:
  name: production
spec:
  provisioner: postgresql.cnpg.io
  config:
    instances: 3
    storage: 100Gi
    postgresVersion: "17"
    backup:
      enabled: true
      schedule: "0 2 * * *"        # Daily at 2:00 AM (standard 5-field cron)
  cnpg:
    primaryUpdateMethod: switchover
    backup:
      # Which instance performs the base backup
      target: prefer-standby        # Avoids I/O on primary; falls back to primary if no standby
      barmanObjectStore:
        destinationPath: s3://my-pg-backups-bucket/clusters/
        endpointURL: https://s3.us-east-1.amazonaws.com
        retentionPolicy: "30d"
        s3Credentials:
          accessKeyId:
            name: s3-credentials   # Secret must exist in the cluster's namespace
            key: accessKeyId
          secretAccessKey:
            name: s3-credentials
            key: secretAccessKey
        wal:
          compression: gzip
```

> **Note:** The `s3Credentials` fields reference a Secret **by name only** — the same Secret name is expected in every namespace where a `PostgresCluster` using this class is deployed. The platform team controls the class; users create the Secret in their namespace.

**Validation rules (enforced at admission):**

- `config.backup.schedule` is **required** when `config.backup.enabled` is `true`.
- At least one of `cnpg.backup.volumeSnapshot` or `cnpg.backup.barmanObjectStore` is **required** when `config.backup.enabled` is `true`.

### Step 3 — Create the PostgresCluster (user / application team)

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresCluster
metadata:
  name: my-app-db
  namespace: <namespace>
spec:
  images:
    postgresql:
      "17": ${TEST_PG17_IMAGE}
    pgbouncer: ${TEST_PGBOUNCER_IMAGE}
    # Optional Secrets must exist in this PostgresCluster's namespace.
    # imagePullSecrets:
    #   - name: registry-reader
  class: production
  # Override: run backups at 3:30 AM instead of class default (2:00 AM)
  backup:
    schedule: "30 3 * * *"
```

If `spec.backup` is omitted, the cluster **inherits** both `enabled` and `schedule` from the class.

To **disable** backups on a specific cluster when the class has them enabled:

```yaml
spec:
  backup:
    enabled: false
```

---

## 4. Configuration model

Object storage backup shares the same two-layer model as volume snapshots:

| Layer | Controls | Overridable by cluster? |
|-------|----------|------------------------|
| **`PostgresClusterClass.spec.config.backup`** | `enabled`, `schedule` | Yes |
| **`PostgresClusterClass.spec.cnpg.backup`** | `target`, `barmanObjectStore.*` (destination, credentials, WAL, retention) | No (platform policy) |
| **`PostgresCluster.spec.backup`** | `enabled`, `schedule` | N/A (this is the override) |

The operator creates and manages a `barmancloud.cnpg.io/v1 ObjectStore` resource (named `<cluster>-object-store`) in each cluster's namespace from the class configuration. Users only need to create the credentials Secret referenced by `s3Credentials`.

---

## 5. Observing backup status

### 5.1 Conditions

The operator writes two conditions on every `PostgresCluster`:

`ObjectStoreReady` reports the status of the managed `ObjectStore`:

| Status | Reason | Meaning |
|--------|--------|---------|
| `True` | `ObjectStoreConfigured` | ObjectStore created/updated successfully |
| `True` | `ObjectStoreDisabled` | Barman not configured in the class |
| `False` | `ObjectStoreReconcileFailed` | Error creating/updating the ObjectStore |

`BackupReady` reports the status of the scheduled backup:

| Status | Reason | Meaning |
|--------|--------|---------|
| `True` | `BackupConfigured` | ScheduledBackup is active and healthy |
| `True` | `BackupDisabled` | Backup is intentionally off |
| `False` | `BackupProviderMissing` | `backup.enabled=true` but no provider configured in class |
| `False` | `ScheduledBackupFailed` | Error creating/updating the ScheduledBackup |
| `False` | `ScheduledBackupCreated` | Waiting for ScheduledBackup to appear (transient) |

### 5.2 BackupStatus

```yaml
status:
  backupStatus:
    objectStore:
      enabled: true
      lastScheduleTime: "2026-04-30T02:00:00Z"
      nextScheduleTime: "2026-05-01T02:00:00Z"
```

| Field | Description |
|-------|-------------|
| `objectStore.enabled` | Whether object store backups are active |
| `objectStore.lastScheduleTime` | When CNPG last scheduled a base backup. This is not proof that it completed. |
| `objectStore.nextScheduleTime` | Next scheduled base backup time |

Note: WAL archiving runs **continuously** regardless of the schedule — the schedule controls only full base backups. `lastScheduleTime` and `nextScheduleTime` are empty until the first scheduled base backup fires.

### 5.3 Verify a backup run

Find the CNPG `Backup` for the run you want to check. For an on-demand backup, use the name you gave it. For a scheduled backup, match `spec.cluster.name`, the scheduled run time, and the owner reference to the Barman `ScheduledBackup`. The Barman backup uses `spec.method: plugin` and `spec.pluginConfiguration.name: barman-cloud.cloudnative-pg.io`.

Set `NAMESPACE` to the cluster namespace and `BACKUP` to the selected resource name. Make sure that `status.phase` is `completed`. A scheduled run time or an old file in the bucket does not show that this run succeeded.

```bash
kubectl get backups.postgresql.cnpg.io -n "$NAMESPACE"
kubectl get backup.postgresql.cnpg.io "$BACKUP" -n "$NAMESPACE" -o yaml
```

Set `BACKUP_ID` to the value of `status.backupId`. If the backup does not report an ID, use its start and stop times to identify the matching entry in the Barman backup catalog. Do not count this check as a pass if you cannot match the run.

Set `DESTINATION_PATH` to the configured S3 destination, including its prefix. Set `SERVER_NAME` to the source cluster's archive name. Use the same `serverName` that you plan to use for recovery. Make sure that the matching base backup and the archived WAL are under that server directory:

```bash
ARCHIVE_ROOT="${DESTINATION_PATH%/}/$SERVER_NAME"
aws s3 ls "$ARCHIVE_ROOT/base/$BACKUP_ID/" --recursive
aws s3 ls "$ARCHIVE_ROOT/wals/" --recursive
```

For example, the base backup and WAL are under `my-cluster/`:

```
2026-04-30T02:00:01Z   my-cluster/base/20260430T020000/backup.info
2026-04-30T02:00:01Z   my-cluster/base/20260430T020000/data.tar
2026-04-30T01:58:00Z   my-cluster/wals/0000000100000000/000000010000000000000001
```

### 5.4 Verifying from within Kubernetes

```bash
# Check the managed ObjectStore resource
kubectl get objectstore <cluster-name>-object-store -n <namespace>

# List base backups taken by CNPG
kubectl get backups -n <namespace>

# Check the scheduled backup status (barman object-store backups)
kubectl get scheduledbackup <cluster-name>-backup-objectstore -n <namespace> -o yaml
```

> **Note on ScheduledBackup names.** Each configured provider gets its own `ScheduledBackup`:
> volume snapshots use `<cluster-name>-backup` (method `volumeSnapshot`) and barman object storage
> uses `<cluster-name>-backup-objectstore` (method `plugin`). When both providers are configured
> they back up independently on the shared `config.backup.schedule`; removing a provider
> garbage-collects its `ScheduledBackup`.

The `ObjectStore` status also shows the recovery window (first recoverability point and last successful backup) once backups have run:

```bash
kubectl get objectstore <cluster-name>-object-store -n <namespace> -o jsonpath='{.status}' | jq .
```

### Take an on-demand base backup

If the next scheduled backup is too late, create a CloudNativePG `Backup`. Use a new backup name. Set the namespace and cluster name to those of the managed CNPG cluster. Make sure that Barman backups are already configured before you apply this manifest.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata:
  name: mydb-before-maintenance
  namespace: <namespace>
spec:
  cluster:
    name: <cnpg-cluster-name>
  method: plugin
  pluginConfiguration:
    name: barman-cloud.cloudnative-pg.io
```

[Check the backup run and its archive](#53-verify-a-backup-run) using the name `mydb-before-maintenance`. The on-demand backup does not change the schedule.

---

## 6. Retention

Retention is configured in the class via `cnpg.backup.barmanObjectStore.retentionPolicy`. The format is a number of days: `"7d"`, `"30d"`, `"90d"`.

```yaml
cnpg:
  backup:
    barmanObjectStore:
      retentionPolicy: "30d"
```

The barman-cloud plugin automatically prunes base backups and WAL segments older than the policy after each new base backup is completed. The operator propagates this to the managed `ObjectStore` resource automatically.

---

## 7. Point-In-Time Recovery (PITR)

Because WAL is continuously archived, the backups in object storage can be replayed to **any point in time** between the first base backup and the last archived WAL segment.

This is driven entirely through the `PostgresCluster` API: create a new cluster with `spec.bootstrapFrom.objectStorage` (restore purely from the object-store base backup) or with `spec.bootstrapFrom.volumeSnapshot.walArchive` (a snapshot base plus WAL replay from object storage), optionally with a `spec.bootstrapFrom.recoveryTarget`. Bucket path and credentials are resolved from the class's `barmanObjectStore` config — the same one used here for backups — so you can only restore from stores your class already has access to.

See [Restoring a PostgresCluster §8 (PITR)](restore-from-volume-snapshot.md#8-point-in-time-recovery-pitr) and [§9 (Restoring from object storage)](restore-from-volume-snapshot.md#9-restoring-from-object-storage) for the full procedure and the recovery-target caveats.

---

## 8. Schedule format

See [Automated Backups via Volume Snapshots — Schedule format](backup-volume-snapshots.md#7-schedule-format). The same 5-field cron applies; the operator translates it to CNPG's 6-field format internally.

---

## 9. Retention and cluster deletion

When `clusterDeletionPolicy: Retain` is set, the underlying CNPG Cluster is orphaned and continues running after the `PostgresCluster` CR is deleted. Both the `ScheduledBackup` and the `ObjectStore` CRs are operator-managed and owned by the `PostgresCluster`, so they are garbage-collected along with it. To avoid leaving the retained cluster with an active WAL archiver pointing at a soon-to-be-deleted `ObjectStore`, the operator **strips the barman-cloud plugin from the retained CNPG Cluster spec** during finalization (any plugins owned by other controllers are left intact).

**Consequence:** the retained CNPG Cluster will no longer take base backups, and **WAL archiving stops** cleanly when the `PostgresCluster` is deleted. This mirrors the volume-snapshot survivor, which keeps its (dormant) backup configuration but runs no active archiver. Existing base backups and WAL segments already written to object storage are **not** affected; they remain in the bucket and stay usable for PITR. The retained cluster therefore stops shipping new WAL to S3, so object-storage usage does not keep growing after deletion.

If ongoing backups and WAL archiving are required after retention, re-add the barman-cloud plugin to the retained CNPG Cluster and recreate the `ObjectStore` and `ScheduledBackup` CRs manually, targeting it with `method: plugin`.
