---
title: Protect and recover data
parent: Operate PostgreSQL
nav_order: 1
has_children: true
---

# Protect and recover data

If data is missing or corrupt now, start with [Choose a recovery path](disaster-recovery.md#3-failure-mode--recovery-path). Do not delete the source to start a restore. Restore into a separate cluster, then check the data and application access before moving traffic.

## Keep backups usable

1. Set an acceptable amount of data loss and downtime. Use the [backup method comparison](backup-and-restore-choices.md#decision-summary) to compare what snapshots and archived data can recover.
2. Follow the configuration path for your cluster. For the shared Helm cluster, [configure Barman in Deploy](../../deploy/provision.md#configure-backups-before-enabling-the-cluster). Helm does not configure volume snapshots. For independently managed clusters, use the [object-storage](backup-object-storage.md#3-setup) or [volume-snapshot](backup-volume-snapshots.md#3-enabling-backups) procedure.
3. Make sure that the backup actually completed. For Barman, [check the specific backup run and archived WAL](backup-object-storage.md#53-verify-a-backup-run). WAL is PostgreSQL's write-ahead log. For snapshots, [check the backup status](backup-volume-snapshots.md#4-observing-backup-status) and [find the snapshots](backup-volume-snapshots.md#5-finding-your-snapshots).
4. If you need a fresh base backup before maintenance, [take an on-demand backup](backup-object-storage.md#take-an-on-demand-base-backup). Make sure that it completes before you start the change.
5. [Run an isolated restore drill](disaster-recovery.md#run-an-isolated-restore-drill). Compare the time and data loss that you measure with the limits you set. If either limit fails, adjust the backup policy or recovery procedure and repeat the drill.

## Recover after a failure

Pick a source that survived the failure. Use a snapshot if its storage system survived and recovering to snapshot time is enough. Use the object store if the storage system is gone. If an unwanted change corrupted data, use point-in-time recovery (PITR) to stop before the change.

Follow the [recovery runbook](disaster-recovery.md#4-recovery-drills) for all three paths. If you need a different target type or have a separate WAL volume, use the [restore API and compatibility details](restore-from-volume-snapshot.md). All paths return to the same [access preparation](disaster-recovery.md#prepare-access-to-the-restored-cluster) after the credential sweep. If you need to move production clients, [stop source writes before cutover](disaster-recovery.md#44-application-cutover). Keep the old cluster until you have checked application access and the new backup coverage.
