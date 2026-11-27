---
title: Check the shared deployment
parent: Deploy PostgreSQL
nav_order: 4
---

# Check the shared PostgreSQL deployment

Keep `NAMESPACE` and `CLUSTER` from [shared provisioning](provision.md#enable-the-shared-helm-cluster). Make sure that the cluster reports `Ready`:

```bash
: "${NAMESPACE:?Set the shared cluster namespace}"
: "${CLUSTER:?Set the Helm-owned cluster name}"
kubectl wait -n "$NAMESPACE" --for=jsonpath='{.status.phase}'=Ready \
  "postgrescluster/$CLUSTER" --timeout=10m
```

If readiness times out, inspect the cluster status, storage claims, and events. Do not create a replacement cluster or a test database to work around a failed check.

```bash
kubectl describe postgrescluster "$CLUSTER" -n "$NAMESPACE"
kubectl get pods,pvc -n "$NAMESPACE"
kubectl get events -n "$NAMESPACE" --sort-by='.lastTimestamp'
```

## Check backups and test recovery

If you configured backups, [check that the intended base backup completed and that its archive contains WAL](../operate/protection/backup-object-storage.md#53-verify-a-backup-run). WAL is the log PostgreSQL uses to replay changes during recovery.

Before you rely on the deployment, run the [isolated object-storage restore drill](../operate/protection/disaster-recovery.md#run-an-isolated-restore-drill) on the [separate PostgreSQL target you planned](provision.md#prepare-for-a-restore-drill). Use the existing operator. Leave the source cluster and its Helm release unchanged. Record how much data you could recover and how long it took to restore and test access. Compare both results with the limits you set before deployment.

## Check monitoring

If you configured monitoring, make sure that the collector sees the expected metrics. See the [dashboard guide](../operate/observability/postgresql-observability-dashboard.md).

A ready cluster does not prove application access or recovery. Before handoff, make sure that the backup and monitoring checks that apply to your deployment pass. For later work, use the [operating guides](index.md#after-deployment).
