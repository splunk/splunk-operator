---
title: Major Version Upgrades
parent: PostgreSQL
nav_order: 4
---

## PostgreSQL Major Version Upgrades

This document describes how to perform a PostgreSQL major version upgrade for a Splunk Operator `PostgresCluster`, what safety checks the operator performs, and what to monitor during the upgrade.

Examples here use `15 -> 16`, but the same process applies to other supported PostgreSQL major versions.

Minor-version upgrades, scaling, and backup configuration are covered separately.

### How major upgrades work

A PostgreSQL major version upgrade changes the database catalog version and on-disk layout. The operator treats this as an explicit workflow, not as normal steady-state drift.

Major upgrades are requested by setting `PostgresCluster.spec.postgresVersion`
to the target version and explicitly allowing the major-upgrade workflow with
`PostgresCluster.spec.postgresMajorUpgradeConfig.allow=true`:

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresCluster
metadata:
  name: postgresql-cluster-prod
spec:
  class: postgresql-prod
  postgresVersion: "16"
  postgresMajorUpgradeConfig:
    allow: true
```

When `allow: true` is present and `postgresVersion` requests a new major version:

- the operator records upgrade progress in `status.postgresMajorUpgradeStatus`
- the operator verifies rollback protection before starting destructive work
- CNPG performs the PostgreSQL upgrade workflow
- the operator verifies the upgraded system and records completion

Do not edit the underlying CNPG `Cluster` directly to perform a major version upgrade.

Changing `spec.postgresVersion` across a major version without
`postgresMajorUpgradeConfig.allow=true` does not start the upgrade. The
operator keeps the managed PostgreSQL cluster on its current major version and
reports that an explicit major-upgrade confirmation is required.

After the upgrade is complete, keep `spec.postgresVersion` at the completed
target version and remove `spec.postgresMajorUpgradeConfig` from the tracked
manifest. If the operator used multiple upgrade hops, wait until all status
entries for the requested upgrade are `Completed`.

The operator only supports single-major-version upgrades. A request that skips intermediate versions, such as `15 -> 18`, is rejected with a terminal `Failed` condition and requires manual correction of `spec.postgresVersion` before the workflow can proceed. To reach PostgreSQL 18 from 15, perform sequential upgrades: `15 -> 16`, then `16 -> 17`, then `17 -> 18`.

Minor PostgreSQL version changes are different. A same-major change, such as `15.10 -> 15.12`, is handled by normal cluster reconciliation and is documented in [Minor Version Upgrades](minor-version-upgrade.md).

### Safety model

Before the upgrade starts, the operator must establish rollback capability. This usually means a valid backup or recovery artifact exists for the old PostgreSQL major version.

If rollback capability is not ready, the upgrade remains in `PreUpgradeBackup` and retries while the backup is pending or temporarily unavailable. If a CNPG Backup reaches a failed state, the operator records a terminal failure; after correcting the cause, delete the failed backup object for that gate and request a one-time retry.

Terminal failures are different. If the operator records a terminal `Failed` condition, it will not retry automatically until an operator explicitly requests retry.

### Upgrade procedure

Before starting the upgrade:

- make sure the operator runs with `PostgresController` enabled
- make sure the cluster is healthy and `Ready`
- confirm backup configuration is enabled and healthy
- confirm application clients tolerate a write outage during the upgrade window
- review PostgreSQL release notes for extension, collation, and compatibility changes
- ensure no superuser secret rotation is planned within one week of the upgrade
  in either direction (see [Secret rotation and upgrades](#secret-rotation-and-upgrades))

Example variables:

```bash
export NS=test
export CLUSTER=postgresql-cluster-prod
```

Update the tracked `PostgresCluster` manifest:

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresCluster
metadata:
  name: postgresql-cluster-prod
spec:
  class: postgresql-prod
  postgresVersion: "16"
  postgresMajorUpgradeConfig:
    allow: true
```

Apply the manifest:

```bash
kubectl apply -n $NS -f path/to/postgrescluster.yaml
```

For ad hoc testing or an emergency change window, you can patch the existing
`PostgresCluster` instead:

```bash
kubectl patch postgrescluster $CLUSTER -n $NS \
  --type=merge \
  -p '{
    "spec": {
      "postgresVersion": "16",
      "postgresMajorUpgradeConfig": { "allow": true }
    }
  }'
```

Prefer the tracked manifest for production changes so the requested target
version and explicit upgrade allow are reviewed together.

Watch progress:

```bash
kubectl get postgrescluster -n $NS $CLUSTER -w
kubectl get events -n $NS --sort-by=.lastTimestamp
```

Inspect the upgrade status:

```bash
kubectl get postgrescluster -n $NS $CLUSTER \
  -o jsonpath='phase={.status.phase}{"\n"}{range .status.postgresMajorUpgradeStatus[*]}majorUpgrade source={.sourcePgVersion} target={.targetPgVersion} phase={.phase}{"\n"}{range .conditions[*]}  {.type} {.status} {.reason}: {.message}{"\n"}{end}{end}'
```

If the operator reports that major-upgrade confirmation is required, review the
target version. If the major upgrade is intentional, add
`postgresMajorUpgradeConfig.allow=true` and apply the manifest again.

### What to monitor during the upgrade

At the `PostgresCluster` level, expect `status.postgresMajorUpgradeStatus[n].phase` to move through:

- `Scheduled`
- `PreUpgradeBackup`
- `Preflight`
- `Upgrading`
- `Verifying`
- `PostUpgradeBackup`
- `Completed`

The `phase` field and the conditions in `status.postgresMajorUpgradeStatus[n].conditions` are independent. While the phase is `PreUpgradeBackup` or `PostUpgradeBackup` and the backup has not yet completed, the active condition is `MajorUpgradeRetryableFailure` (the operator is waiting for the backup, not failing). The phase advances to the next step only after the backup is confirmed.

Each status entry may contain conditions:

| Condition | Meaning |
| --- | --- |
| `MajorUpgradeProgressing` | upgrade is actively making forward progress (image patch applied, upgrade job running, verification in progress) |
| `MajorUpgradeRetryableFailure` | a dependency is not ready — pre-upgrade backup pending, backup storage full, state temporarily unavailable — the operator will keep retrying automatically |
| `MajorUpgradeTerminalFailure` | operator will not retry automatically; manual action is required before requesting retry |
| `MajorUpgradeCompleted` | matching upgrade intent completed |

`MajorUpgradeProgressing` and `MajorUpgradeRetryableFailure` are both set with `Retry: true` and both require no user action. The distinction is intent: `Progressing` means the upgrade is moving forward normally; `RetryableFailure` means the operator is blocked on a prerequisite and waiting for it to resolve. If `MajorUpgradeRetryableFailure` persists for an extended period, inspect the condition message to identify the blocked dependency (for example, a backup that cannot complete due to storage pressure).

For terminal failures, review the message in status and operator logs before retrying.

### Retrying after terminal failure

Do not use a permanent `retry: true` field in the spec. For terminal failures, request a one-time retry with an annotation:

```bash
kubectl annotate postgrescluster $CLUSTER \
  platform.splunk.com/major-upgrade-retry-at="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -n $NS --overwrite
```

Use this only after the cause of the terminal failure has been corrected. Retryable failures such as temporary backup unavailability do not require this annotation.

The annotation value must be an RFC3339 UTC timestamp. The operator compares this value with the `MajorUpgradeTerminalFailure` condition timestamp in status. If the annotation timestamp is newer, the matching failed upgrade attempt may be scheduled again. If it is absent, invalid, or older than the failure timestamp, the failed attempt remains stopped.

This makes retry edge-triggered. Reapplying the same annotation value does not retry a newer failure. To retry again after another terminal failure, overwrite the annotation with a new current timestamp.

Kubernetes annotations are string key-value metadata stored under `metadata.annotations`. They are not part of the desired spec and they are not used for resource identity or selection. Updating an annotation still changes the Kubernetes object, so the operator receives a watch event and reconciles the cluster.

### Secret rotation and upgrades

Do not rotate the superuser secret during a major-version upgrade. Plan at least **one week** of separation between any secret rotation and the upgrade in either direction.

A major-version upgrade blocks cluster reconciliation for its entire duration — which can span many passes over minutes to hours. If a rotation triggers a reconciliation change (for example an ESO-managed secret being replaced with a new object reference) while that window is open, the new credentials may not propagate in time. If the old secret is deleted or revoked before the upgrade completes, CNPG superuser authentication breaks for the duration of the upgrade and manual intervention is required to restore it before the upgrade can continue.

### After completion

After the upgrade reports `Completed` for the requested target version:

- verify application reads and writes through the normal endpoint
- confirm backup status for the upgraded cluster
- remove `spec.postgresMajorUpgradeConfig` from the tracked manifest

Example final manifest shape after a completed `15 -> 16` upgrade:

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresCluster
metadata:
  name: postgresql-cluster-prod
spec:
  class: postgresql-prod
  postgresVersion: "16"
```

The completion record remains in status. Keeping `postgresMajorUpgradeConfig` after completion is not required once `spec.postgresVersion` already reflects the new steady-state PostgreSQL version.

### Backup artifacts

The operator creates two named backups during a successful upgrade. Their names are recorded in `status.postgresMajorUpgradeStatus[n].backupNames`:

- `preUpgrade` — taken before any CNPG mutation; the recovery anchor if the upgrade fails
- `postUpgrade` — taken after the upgrade completes; the new baseline for the upgraded cluster

To find the backup names for the current upgrade and inspect them:

```bash
PRE_UPGRADE_BACKUP=$(kubectl get postgrescluster "${CLUSTER}" -n "${NS}" \
  -o jsonpath='{range .status.postgresMajorUpgradeStatus[*]}{.backupNames.preUpgrade}{"\n"}{end}' \
  | grep -v '^$' | tail -1)

POST_UPGRADE_BACKUP=$(kubectl get postgrescluster "${CLUSTER}" -n "${NS}" \
  -o jsonpath='{range .status.postgresMajorUpgradeStatus[*]}{.backupNames.postUpgrade}{"\n"}{end}' \
  | grep -v '^$' | tail -1)

kubectl get backups.postgresql.cnpg.io "${PRE_UPGRADE_BACKUP}" -n "${NS}" -o yaml
kubectl get backups.postgresql.cnpg.io "${POST_UPGRADE_BACKUP}" -n "${NS}" -o yaml
```

To watch backup objects as they are created during an upgrade:

```bash
kubectl get backups.postgresql.cnpg.io -n "${NS}" -w
```

Retain both backup artifacts according to your operational retention policy. The pre-upgrade backup is the only recovery path if a problem is found after the upgrade.

### Recovery from a failed upgrade

The operator does not implement an automated rollback path. If the upgrade fails at any point — including after `pg_upgrade` starts — recovery is a manual restore from the pre-upgrade backup.

The pre-upgrade backup name is recorded in `status.postgresMajorUpgradeStatus[n].backupNames.preUpgrade` before the operator makes any changes to the CNPG cluster. A `Failed` condition with reason `UpgradeUnrecoverablePreConversion` or `UpgradeUnrecoverablePostConversion` means CNPG reported an unrecoverable state during the upgrade job itself; the message on the condition explains which phase was reached.

In all cases the recovery instruction is the same: restore the cluster from the named pre-upgrade backup.

For object-storage (Barman) backups, see [Backup and restore with object storage](backup-object-storage.md).

For volume-snapshot backups, see [Restore from volume snapshot](restore-from-volume-snapshot.md).
