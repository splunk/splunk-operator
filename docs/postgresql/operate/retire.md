---
title: Retire a PostgreSQL cluster
parent: Operate PostgreSQL
nav_order: 5
---

# Retire a PostgreSQL cluster

Use this checklist before you take a cluster out of service. If you are replacing it after a restore, finish the [application cutover](protection/disaster-recovery.md#44-application-cutover) first.

1. Stop writers and make sure that no application needs the old endpoint. Include scheduled jobs and other clients.
2. Record which data and backups must remain. [Check the backup and recovery window](protection/index.md#keep-backups-usable) before removing protection.
3. Identify how the cluster is managed. For a Helm-owned shared cluster, do not set `sharedPostgres.enabled=false` to remove it. The chart retains the database resources on uninstall. Uninstalling the operator release affects other workloads it manages and is not a database cleanup step.
4. If an independently managed `PostgresCluster` uses `clusterDeletionPolicy: Retain`, removing that resource leaves the underlying CNPG cluster and volumes. With `Delete`, removing it also deletes the managed cluster and its data volumes. If a blue/green upgrade is in progress or retains an old environment, [check the upgrade deletion rules](maintenance/blue-green-upgrade.md#delete-a-cluster-during-an-upgrade) before you remove the cluster.
5. After retirement, check the resources and backups that remain. If you retained the CNPG cluster but removed its `PostgresCluster`, scheduled backups stop. With Barman, WAL archiving also stops. Use the [object-storage](protection/backup-object-storage.md#9-retention-and-cluster-deletion) or [snapshot](protection/backup-volume-snapshots.md#8-cluster-retention-and-backup-schedule) procedure if ongoing protection is required.

Keep the final recovery point and the backup expiration date with the retirement record.
