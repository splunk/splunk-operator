---
title: Choose cluster policy
parent: Deploy PostgreSQL
nav_order: 2
---

# Choose cluster policy

Helm creates a `PostgresClusterClass` from the release values. The class records defaults and policy that you cannot change later. Choose those values before you first enable shared PostgreSQL.

## Size the cluster before provisioning

Size the cluster for the queries and traffic you expect. Before production, use a load test to measure how many queries it can handle, how long queries take, and how much capacity it uses.

| What to size | What to decide |
| --- | --- |
| PostgreSQL instances | Choose a primary and enough replicas for your availability requirements. One instance has no standby. If you require pod separation, provide an eligible node for each instance. |
| Storage | Budget for data, indexes, write-ahead log, and growth. Size storage per instance. Provision space for backup retention separately. Storage can grow but cannot shrink in place. |
| CPU and memory | Set requests and limits per instance. Use query rates, query cost, and concurrent work to estimate demand. Leave room for maintenance and recovery. |
| Connections | Estimate how many sessions all application replicas open at once. If you use PgBouncer, choose a pooling mode. Allow for connections to the pooler and connections from the pooler to PostgreSQL. |
| Placement | Check node capacity, storage topology, and placement constraints. Set aside capacity for the pooler if you enable it. |

To see the shared PostgreSQL defaults, use the `splunk` Helm repository and `CHART_VERSION` from [controller installation](install.md#install-or-enable-postgresql-through-helm). Inspect the `sharedPostgres` section of the output:

```bash
helm show values splunk/splunk-operator --version "$CHART_VERSION"
```

If you use a mirrored chart, substitute its reference. If you use a local package, pass its path and omit `--version`.

For example, three instances with 100Gi of storage each need 300Gi for database volumes. If each instance requests 2 CPU and 8Gi of memory, budget 6 CPU and 24Gi before counting poolers or other workloads. Use the values for your chart version and size them for your workload.

Set `sharedPostgres.instances`, `sharedPostgres.storage`, and `sharedPostgres.resources` in your Helm values file. If you use one PostgreSQL instance, set `sharedPostgres.connectionPooler.readOnly: false`. Next, [choose storage, placement, and certificate policy](cluster-policy-reference.md#choose-shared-cluster-policy). Keep your sizing values in the same Helm values file.
