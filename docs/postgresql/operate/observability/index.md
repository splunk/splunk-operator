---
title: Monitor and diagnose PostgreSQL
parent: Operate PostgreSQL
nav_order: 3
has_children: true
---

# Monitor and diagnose PostgreSQL

Start with the failing task. A cluster can be ready while a backup, database privilege grant, or application connection still fails.

## Find the failing component

1. Read the `PostgresCluster` status and events. If a database is affected, also read its `PostgresDatabase` conditions.
2. If PostgreSQL instances are not ready, check the underlying CloudNativePG (CNPG) cluster and Pods. Use the [controller and lifecycle diagrams](../../architecture-overview.md) to see which controller owns the failing step.
3. If the cluster is healthy but a backup or restore fails, go to [Protect and recover data](../protection/index.md). If a role or grant fails, go to [Manage database access](../access/index.md).
4. After the fix, repeat the original check. Make sure that the conditions and the application or backup operation recover.

Set `CLUSTER` and `NAMESPACE` to the running cluster before you run these commands:

```bash
kubectl describe postgrescluster "$CLUSTER" -n "$NAMESPACE"
kubectl get events -n "$NAMESPACE" --sort-by='.lastTimestamp'
```

## Choose the signals to keep

Use the [sample Grafana dashboard](postgresql-observability-dashboard.md) to compare database, PgBouncer, and controller metrics. If a standard metric cannot answer an operational question, use [custom SQL metrics](custom-metrics.md). Choose cluster scope for instance-level queries and database scope for application tables. Make sure that the collector receives the metric before you add an alert that depends on it.
