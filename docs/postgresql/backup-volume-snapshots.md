---
title: Automated Backups via Volume Snapshots
parent: PostgreSQL
nav_order: 1
---

# Automated Backups via Volume Snapshots

This guide is for **platform teams** configuring automated PostgreSQL backups and **users** who need to understand backup status on their **`PostgresCluster`** instances. The Splunk Operator uses Kubernetes CSI **Volume Snapshots** as the backup method, delegating snapshot execution to CloudNativePG.

For CNPG backup internals, see [CloudNativePG — Volume Snapshots (1.28)](https://cloudnative-pg.io/docs/devel/backup).

---

## 1. How it works (overview)

The operator creates a CNPG **`ScheduledBackup`** resource per `PostgresCluster` when backup is enabled. CNPG then:

1. Takes a **consistent volume snapshot** of the PostgreSQL data (PG_DATA) and optionally WAL (PG_WAL) PVCs at the configured schedule.
2. Creates Kubernetes **`VolumeSnapshot`** objects in the cluster namespace.
3. Updates schedule metadata (last/next backup times) on the `ScheduledBackup` status.

The Splunk Operator surfaces this information on `PostgresCluster.status.backupStatus`.

---

## 2. Configuration model

Backup configuration uses the **two-layer** model:

| Layer | Controls | Overridable by cluster? |
|-------|----------|------------------------|
| **`PostgresClusterClass.spec.config.backup`** | `enabled`, `schedule` | Yes |
| **`PostgresClusterClass.spec.cnpg.backup`** | `target`, `volumeSnapshot.*` (snapshot class, online mode, ownership) | No (platform policy) |
| **`PostgresCluster.spec.backup`** | `enabled`, `schedule` | N/A (this is the override) |

**Rule:** A cluster can override *when* backups run (schedule) and *whether* they run (enabled), but not *how* (snapshot class, target instance, ownership). The "how" is platform policy set in the class.

---

## 3. Enabling backups

### 3.1 PostgresClusterClass (platform team)

```yaml
apiVersion: enterprise.splunk.com/v4
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
      # Which instance performs the snapshot
      target: prefer-standby        # Avoids I/O on primary; falls back to primary if no standby
      volumeSnapshot:
        # CSI VolumeSnapshotClass — must exist on the cluster
        className: csi-hostpath-snapclass
        # Optional: separate class for WAL PVC (omit to use className for both)
        # walClassName: csi-wal-snapclass
        # Snapshot lifecycle:
        #   "none" — snapshots persist independently, require manual cleanup
        #   "cluster" — snapshots are garbage collected when the CNPG Cluster is deleted
        snapshotOwnerReference: "none"
        # Online (hot) snapshots — no downtime during backup
        online: true
```

**Validation rules (enforced at admission):**

- `config.backup.schedule` is **required** when `config.backup.enabled` is `true`.
- At least one of `cnpg.backup.volumeSnapshot` or `cnpg.backup.barmanObjectStore` is **required** when `config.backup.enabled` is `true`.

> **Object storage backups:** For S3/Barman-based backups instead of (or alongside) volume snapshots, see [Automated Backups via Object Storage](./backup-object-storage.md).

### 3.2 PostgresCluster (user / application team)

```yaml
apiVersion: enterprise.splunk.com/v4
kind: PostgresCluster
metadata:
  name: my-app-db
  namespace: team-alpha
spec:
  class: production
  # Override: run backups at 3:30 AM instead of class default (2:00 AM)
  backup:
    schedule: "30 3 * * *"
```

If `spec.backup` is omitted entirely, the cluster **inherits** both `enabled` and `schedule` from the class.

To **disable** backups on a specific cluster (when the class has them enabled):

```yaml
spec:
  backup:
    enabled: false
```

---

## 4. Observing backup status

### 4.1 Conditions

The operator writes a **`BackupReady`** condition on every `PostgresCluster`:

| Status | Reason | Meaning |
|--------|--------|---------|
| `True` | `BackupConfigured` | ScheduledBackup is active and healthy |
| `True` | `BackupDisabled` | Backup is intentionally off (desired state met) |
| `False` | `ScheduledBackupFailed` | Error creating/updating the ScheduledBackup |
| `False` | `ScheduledBackupCreated` | Waiting for ScheduledBackup to appear (transient) |

### 4.2 BackupStatus

```yaml
status:
  backupStatus:
    volumeSnapshot:
      enabled: true
      lastScheduleTime: "2026-04-30T02:00:00Z"
      nextScheduleTime: "2026-05-01T02:00:00Z"
```

| Field | Description |
|-------|-------------|
| `volumeSnapshot.enabled` | Whether volume snapshot backups are active |
| `volumeSnapshot.lastScheduleTime` | When CNPG last scheduled a backup |
| `volumeSnapshot.nextScheduleTime` | Next scheduled backup time |

Times update automatically as CNPG executes backups.

---

## 5. Finding your snapshots

Volume snapshots are standard Kubernetes resources in the cluster namespace:

```bash
kubectl get volumesnapshots -n <namespace>
```


---

## 6. Snapshot retention

**Important:** CNPG does **not** automatically prune volume snapshots. Automatic retention only applies to object storage backups — see [Automated Backups via Object Storage](./backup-object-storage.md#6-retention).

Cleanup depends on `snapshotOwnerReference`:

| Value | Behavior | When to use |
|-------|----------|-------------|
| `none` | Snapshots persist forever until manually deleted | Production (safest — backups survive cluster deletion) |
| `cluster` | Snapshots are garbage collected when the CNPG Cluster is deleted | Development / ephemeral environments |


---

## 7. Schedule format

The `schedule` field accepts **standard 5-field cron**:

```
minute hour day-of-month month day-of-week
```

Examples:
- `"0 2 * * *"` — daily at 2:00 AM
- `"0 */6 * * *"` — every 6 hours
- `"30 1 * * 0"` — Sundays at 1:30 AM

The operator translates this to CNPG's 6-field format internally (prepends `0` for the seconds field).

> **Note:** The [CNPG backup documentation](https://cloudnative-pg.io/docs/1.28/backup/#cron-schedule) uses a **6-field cron** where the first field is seconds (e.g. `"0 0 2 * * *"`). This operator accepts standard **5-field cron only** — do not include the seconds field.

---

## 8. Cluster retention and backup schedule

When `clusterDeletionPolicy: Retain` is set on a `PostgresCluster`, the operator orphans the underlying CNPG Cluster so it continues running after the `PostgresCluster` CR is deleted. However, the `ScheduledBackup` CR is operator-managed and will be garbage-collected along with the `PostgresCluster`.

**Consequence:** the retained CNPG Cluster will no longer have automated backups. Existing `VolumeSnapshot` objects are not affected — they remain in the namespace.

If ongoing backups are required after retention, recreate the `ScheduledBackup` CR manually targeting the retained CNPG Cluster.

---

## 9. Prerequisites

- A **CSI driver** with snapshot support must be installed on the Kubernetes cluster.
- A **VolumeSnapshotClass** matching `className` must exist.
- The CNPG Cluster must be **healthy** before the operator creates the ScheduledBackup.

If the CNPG cluster is not yet healthy, the `BackupReady` condition will remain unset until the cluster reaches healthy state.
