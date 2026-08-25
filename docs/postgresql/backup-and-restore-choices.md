---
title: Backup and Restore Choices
parent: PostgreSQL
nav_order: 0
---

# Backup and Restore Choices

This page helps platform administrators choose a PostgreSQL backup method, configure the platform, and select a restore path. It covers the supported choices at a practical level. The detailed provider and recovery procedures are linked at the [end](#detailed-procedures).

## Decision summary

| Requirement | Recommended choice | Why |
|---|---|---|
| Fast restore inside the Kubernetes environment | Volume snapshots | Restores the PostgreSQL data volume directly from a Kubernetes `VolumeSnapshot`. |
| Restore to the time a snapshot was taken | Volume snapshots | The snapshot captures the database state at that point. |
| Disaster recovery outside the storage system | Barman object storage | Base backups and WAL are stored independently of the PostgreSQL volumes. |
| Point-in-time recovery | Barman object storage | Continuous WAL archiving allows recovery between base backups. |
| Fast base restore plus point-in-time recovery | Volume snapshot plus `walArchive` | Uses a snapshot for the base data and Barman for WAL replay. |
| Restore without a Kubernetes volume snapshot | `objectStorage` restore | Uses a Barman base backup and WAL from object storage. |
| Automatic retention of older backups | Barman object storage | Retention policies prune base backups and archived WAL. |
| Both fast local recovery and independent disaster recovery | Enable both providers | The providers run independently and protect different failure scenarios. |

## Backup methods

### Volume snapshots

Volume snapshot backups create Kubernetes `VolumeSnapshot` resources for the PostgreSQL data volume and, when configured, a separate WAL volume.

Choose volume snapshots when:

- the Kubernetes storage platform supports CSI snapshots;
- fast restores from the same Kubernetes environment are important; and
- recovery to the most recent snapshot is sufficient.

Limitations:

- snapshots do not provide point-in-time recovery between snapshots;
- snapshot retention is managed separately from object-storage retention; and
- snapshots depend on the availability and compatibility of the Kubernetes storage system.

### Barman object storage

Barman object-storage backups create scheduled physical base backups and continuously archive WAL to an S3-compatible object store.

Choose Barman when:

- backups must survive loss of the Kubernetes storage system or cluster;
- point-in-time recovery is required;
- longer or policy-based retention is required; or
- backups must be available for a new cluster without restoring a volume snapshot.

Limitations:

- the barman-cloud CNPG plugin, object-store CRD, bucket, credentials, and network access are required;
- restore time depends on object-store access and the amount of data transferred; and
- PITR is only possible while the required base backup and WAL segments are retained.

Both providers may be configured on the same class. When backups are enabled, the operator creates an independent scheduled backup for each configured provider.

## Platform setup

Backup provider configuration belongs in `PostgresClusterClass`. The class controls how backups are implemented. A `PostgresCluster` can override whether backups run and when they run, but cannot change provider-specific policy.

### Prerequisites

For volume snapshots:

- Kubernetes CSI snapshot CRDs and snapshot controller support.
- A CSI driver that supports snapshots.
- A `VolumeSnapshotClass` for the PostgreSQL data volume.
- A second `VolumeSnapshotClass` if the PostgreSQL WAL volume uses different storage.

For Barman object storage:

- The barman-cloud CNPG plugin and its `ObjectStore` CRD.
- An S3-compatible bucket and network access from the cluster.
- A credentials Secret in each namespace that uses the class.
- Permissions to write, read, list, and delete objects according to the retention policy.

### Configure both providers

The following class enables both backup methods. Remove the provider block that is not required.

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
      schedule: "0 2 * * *"        # Standard 5-field cron, daily at 02:00
  cnpg:
    primaryUpdateMethod: switchover
    backup:
      target: prefer-standby
      volumeSnapshot:
        className: csi-hostpath-snapclass
        # walClassName: csi-wal-snapclass
        snapshotOwnerReference: "none"
        online: true
      barmanObjectStore:
        destinationPath: s3://my-pg-backups-bucket/clusters/
        endpointURL: https://s3.us-east-1.amazonaws.com
        retentionPolicy: "30d"
        s3Credentials:
          accessKeyId:
            name: s3-credentials
            key: accessKeyId
          secretAccessKey:
            name: s3-credentials
            key: secretAccessKey
        wal:
          compression: gzip
```

The class has two backup configuration layers:

| Field | Purpose | Cluster override? |
|---|---|---|
| `spec.config.backup.enabled` | Enables or disables scheduled backups by default | Yes |
| `spec.config.backup.schedule` | Default backup schedule | Yes |
| `spec.cnpg.backup.volumeSnapshot` | Snapshot provider policy | No |
| `spec.cnpg.backup.barmanObjectStore` | Object-store destination, credentials, WAL, and retention policy | No |
| `spec.cnpg.backup.target` | Preferred instance for backups | No |

The schedule is standard five-field cron. The operator translates it for CNPG. When both providers are configured, both use the shared schedule but run as separate provider backups.

### Configure a cluster's backup behavior

The cluster inherits `enabled` and `schedule` from its class when `spec.backup` is omitted:

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresCluster
metadata:
  name: my-app-db
  namespace: team-alpha
spec:
  class: production
```

Override the schedule for one cluster:

```yaml
spec:
  backup:
    schedule: "30 3 * * *"
```

Disable scheduled backups for one cluster:

```yaml
spec:
  backup:
    enabled: false
```

Disabling scheduled backups does not remove provider configuration from the class. That distinction matters when a cluster is being restored from object storage: the class still needs `cnpg.backup.barmanObjectStore` so the operator can access the archive.

## Restore paths

Restore behavior is selected when creating a new `PostgresCluster` through `spec.bootstrapFrom`. The field is immutable after creation. The source cluster is not changed.

### Snapshot restore

Use this when recovery to the snapshot point is sufficient:

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresCluster
metadata:
  name: mydb-restored
  namespace: <namespace>
spec:
  class: production
  bootstrapFrom:
    volumeSnapshot:
      storage: mydb-backup-20260709
```

The named `VolumeSnapshot` must exist in the target namespace and be compatible with the target class.

### Snapshot restore with a separate WAL volume

Use this when the source cluster had a separate PostgreSQL WAL volume:

```yaml
spec:
  class: production
  bootstrapFrom:
    volumeSnapshot:
      storage: mydb-data-backup-20260709
      walStorage: mydb-wal-backup-20260709
```

`storage` and `walStorage` must be matching snapshots from the same backup point. This is still a snapshot-time restore, not PITR.

### Snapshot-based PITR

Use this for a fast snapshot base followed by archived WAL replay:

```yaml
spec:
  class: production                  # Must define barmanObjectStore
  bootstrapFrom:
    volumeSnapshot:
      storage: mydb-backup-20260709
      walArchive:
        serverName: mydb               # Source archive directory
    recoveryTarget:
      type: time
      value: "2026-07-09T12:30:00Z"
```

The source cluster must have archived WAL in the configured Barman object store. `walArchive` is required whenever `recoveryTarget` is set on a volume-snapshot source.

### Object-storage restore

Use this when the base backup and WAL are in object storage and no Kubernetes volume snapshot is required:

```yaml
spec:
  class: production                  # Must define barmanObjectStore
  bootstrapFrom:
    objectStorage:
      serverName: mydb
```

Without a recovery target, CNPG restores from the object-store base backup and replays available WAL to the latest recoverable point.

### Object-storage PITR

Use this to restore entirely from Barman and stop at a timestamp or WAL position:

```yaml
spec:
  class: production                  # Must define barmanObjectStore
  bootstrapFrom:
    objectStorage:
      serverName: mydb
    recoveryTarget:
      type: time
      value: "2026-07-09T12:30:00Z"
```

For an `objectStorage` source, this API supports target types `time` and `lsn`. Types `xid`, `name`, and `immediate` are rejected because the API does not expose a base-backup identifier needed to select an unambiguous object-store base backup.

## Recovery targets

`recoveryTarget` is a recovery policy, not a backup provider. It is a discriminated pair: `type` selects which kind of target to stop at, and `value` carries the target (interpreted according to `type`). Set at most one target:

| `type` | `value` meaning | Supported source |
|---|---|---|
| `time` | Stop at an RFC 3339 timestamp | Volume snapshot plus `walArchive`, or `objectStorage` |
| `lsn` | Stop at a WAL log sequence number | Volume snapshot plus `walArchive`, or `objectStorage` |
| `xid` | Stop at a transaction ID | Volume snapshot plus `walArchive` |
| `name` | Stop at a named PostgreSQL restore point | Volume snapshot plus `walArchive` |
| `immediate` | Stop at the first consistent state (no `value`) | Volume snapshot plus `walArchive` |

`value` is required for every type except `immediate`, where it must be omitted. `exclusive: true` is a modifier that stops just before the selected target instead of after it.

PITR requires archived WAL. A volume snapshot alone cannot provide PITR.

## Restore prerequisites and validation

Before creating a restored cluster:

1. Confirm the base backup or snapshots exist and are usable.
2. Use the same PostgreSQL major version as the source.
3. Confirm the target class has compatible storage and PostgreSQL settings.
4. For Barman restores, confirm the target class points to the source archive and the credentials Secret can read it.
5. Confirm the requested `serverName` matches the source archive directory.
6. For PITR, confirm the required WAL segments have not expired under the object-store retention policy.

The operator validates that:

- exactly one of `volumeSnapshot` and `objectStorage` is set;
- a snapshot PITR request includes `volumeSnapshot.walArchive`;
- object-store restore paths have class-level `barmanObjectStore` configuration; and
- only supported recovery targets are used with an `objectStorage` source.

## After restore

A restore creates a new, independent cluster. The original cluster continues running.

The restored cluster receives a new superuser Secret. Because recovered PostgreSQL roles may contain credentials from the source cluster, the operator runs a credential sweep and disables unmanaged login roles. Recreate application access with `PostgresDatabase` resources after the sweep completes — that is, once the restore status reports:

```yaml
status:
  restore:
    credentialSweep:
      completed: true
```

The restored cluster can have its own backup schedule and provider behavior after it becomes operational.

## Detailed procedures

- [Automated Backups via Volume Snapshots](backup-volume-snapshots.md)
- [Automated Backups via Object Storage](backup-object-storage.md)
- [Restoring a PostgresCluster](restore-from-volume-snapshot.md)
