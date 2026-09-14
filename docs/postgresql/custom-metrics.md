---
title: Custom PostgreSQL Metrics
parent: PostgreSQL
nav_order: 8
---

# Custom PostgreSQL metrics

Custom metrics let you expose information that matters to your application as
Prometheus metrics. You provide a small SQL query definition in a Kubernetes
`ConfigMap`; the Splunk Operator combines the definitions for a PostgreSQL
cluster and prepares them for collection by CloudNativePG (CNPG).

For example, a team can publish the number of waiting orders, grouped by region,
without managing CNPG's monitoring configuration directly.

The feature has two scopes:

| Scope | Configure it on | Where the query runs |
|---|---|---|
| Cluster-wide | `PostgresCluster.spec.monitoring` | The cluster's normal metrics database |
| One database | `PostgresDatabase.spec.databases[].monitoring` | Only the named database |

Use database scope for queries against application tables. Use cluster scope for
PostgreSQL catalog and instance-level information such as connection counts.

## Before you begin

PostgreSQL metrics must be enabled in the `PostgresClusterClass`, or enabled for
the individual cluster:

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresCluster
metadata:
  name: orders-postgres
  namespace: apps
spec:
  class: production-postgres
  monitoring:
    postgresqlMetrics: true
```

Custom metrics are exposed through the PostgreSQL metrics endpoint. This feature
does not install Prometheus, an OpenTelemetry Collector, Grafana, or another
metrics backend. Your monitoring system must scrape that endpoint.

The source `ConfigMap` must:

- be in the same namespace as the `PostgresCluster` or `PostgresDatabase` that
  references it;
- exist before the reference is created or changed; and
- contain the referenced data key.

When applying separate manifests, create the source `ConfigMap` first. A missing
`ConfigMap` is rejected immediately. A missing data key is reported through the
cluster's `CustomMetricsReady` condition.

The generated custom-metrics data has a fixed maximum of 1 MiB, matching
Kubernetes' ConfigMap data limit. Reduce the number or size of query definitions
if the aggregate reaches that limit.

## Write a query definition

A source data key contains one or more query definitions:

```yaml
<query-name>:
  type: gauge
  help: "A short description of the metric"
  query: |
    SELECT ... AS value_column
  value: value_column
  labels:
    - optional_label_column
```

| Field | Required | Meaning |
|---|---|---|
| Map key, such as `active_connections` | Yes | Name of the custom query and the base of the exported metric name |
| `type` | Yes | `gauge` for a value that may rise or fall, or `counter` for a cumulative value |
| `help` | Yes | Human-readable description of the value |
| `query` | Yes | SQL statement used to collect samples |
| `value` | Yes | Result column containing the numeric value |
| `labels` | No | Result columns to attach as Prometheus labels |

The `value` and every label must be returned by the SQL query. A query may return
several rows; each distinct label combination becomes a separate metric series.
Query names, value columns, and labels must match
`[a-zA-Z_][a-zA-Z0-9_]*`. Labels beginning with `__` are reserved by
Prometheus. Labels must also be unique, and the `value` column cannot be used as
a label. The operator projects the value first and labels in their declared
order before giving the query to CNPG.

Each selected key must contain at least one query. Empty, whitespace-only,
`{}`, and `null` packages are rejected as invalid definitions and do not replace
the last confirmed configuration.

Prefer lightweight, read-only queries. They run as the CNPG
`cnpg_metrics_exporter` role when metrics are collected, so expensive queries can
slow down every scrape. Catalog views covered by the role's `pg_monitor`
membership are available by default. Queries against application tables need
explicit access, for example:

```sql
GRANT CONNECT ON DATABASE orders TO cnpg_metrics_exporter;
GRANT USAGE ON SCHEMA public TO cnpg_metrics_exporter;
GRANT SELECT ON TABLE public.orders TO cnpg_metrics_exporter;
```

Keep label values bounded. A customer ID, request ID, or other nearly unique
value creates too many Prometheus series and should not be used as a label.

The operator validates the definition's shape, but it does not execute the SQL
or verify its result columns. `CustomMetricsReady=True` means the configuration was
accepted and installed; it does not guarantee that the query can run. SQL syntax,
permissions, and returned column names should be verified at the metrics endpoint
and in the CNPG pod logs.

## Example: a cluster-wide metric

First, create the source `ConfigMap`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: postgres-platform-metrics
  namespace: apps
data:
  queries.yaml: |
    active_connections:
      type: gauge
      help: "Active PostgreSQL connections by application"
      query: |
        SELECT
          COALESCE(application_name, 'unknown') AS application,
          count(*)::float AS connection_count
        FROM pg_catalog.pg_stat_activity
        WHERE state = 'active'
        GROUP BY application
      value: connection_count
      labels:
        - application
```

Then reference it from the cluster:

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresCluster
metadata:
  name: orders-postgres
  namespace: apps
spec:
  class: production-postgres
  monitoring:
    postgresqlMetrics: true
    customQueriesConfigMap:
      - name: postgres-platform-metrics
        key: queries.yaml
```

The operator translates the source and creates
`orders-postgres-metrics`. The relevant generated content looks like this;
Kubernetes metadata and YAML ordering may vary:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: orders-postgres-metrics
  namespace: apps
  annotations:
    platform.splunk.com/monitoring-config-hash: "sha256:<content-hash>"
data:
  queries.yaml: |
    "splunk_operator_cluster:active_connections":
      name: splunk_operator_cluster_active_connections
      metrics:
        - connection_count:
            description: Active PostgreSQL connections by application
            usage: GAUGE
        - application:
            usage: LABEL
      query: |
        SELECT "connection_count", "application"
        FROM (
          SELECT
            COALESCE(application_name, 'unknown') AS application,
            count(*)::float AS connection_count
          FROM pg_catalog.pg_stat_activity
          WHERE state = 'active'
          GROUP BY application
        ) AS splunk_operator_custom_metrics
```

The operator also points the underlying CNPG cluster at this generated
`ConfigMap`. You do not need to configure CNPG yourself.

CNPG adds its `cnpg_` prefix and the value-column name. A collected sample from
this example therefore looks like:

```text
cnpg_splunk_operator_cluster_active_connections_connection_count{application="checkout-api"} 12
```

## Example: a metric for one database

Database-scoped metrics are useful for application tables. Create a source
`ConfigMap` in the same namespace:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: orders-application-metrics
  namespace: apps
data:
  queries.yaml: |
    waiting_orders:
      type: gauge
      help: "Orders currently waiting, grouped by region"
      query: |
        SELECT
          pg_catalog.current_database() AS database,
          region,
          count(*)::float AS order_count
        FROM public.orders
        WHERE status = 'waiting'
        GROUP BY region
      value: order_count
      labels:
        - database
        - region
```

Reference it from the matching database entry:

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresDatabase
metadata:
  name: orders-database
  namespace: apps
spec:
  clusterRef:
    name: orders-postgres
  databases:
    - name: orders
      monitoring:
        customQueriesConfigMap:
          - name: orders-application-metrics
            key: queries.yaml
```

The operator adds the database name to the generated configuration. You do not
put `target_databases` in your source document:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: orders-postgres-metrics
  namespace: apps
data:
  queries.yaml: |
    "splunk_operator_database:orders:waiting_orders":
      name: splunk_operator_database_orders_waiting_orders
      metrics:
        - order_count:
            description: Orders currently waiting, grouped by region
            usage: GAUGE
        - database:
            usage: LABEL
        - region:
            usage: LABEL
      query: |
        SELECT "order_count", "database", "region"
        FROM (
          SELECT
            pg_catalog.current_database() AS database,
            region,
            count(*)::float AS order_count
          FROM public.orders
          WHERE status = 'waiting'
          GROUP BY region
        ) AS splunk_operator_custom_metrics
      target_databases:
        - orders
```

The `splunk_operator_database:` prefix is an internal key namespace. The
generated `name` also places exported metrics in the operator-owned
`splunk_operator` namespace, separating them from CNPG built-ins and other
selectors. Do not add either prefix to the source query name. The collected
series is:

```text
cnpg_splunk_operator_database_orders_waiting_orders_order_count{database="orders",region="eu"} 8
```

If several `PostgresDatabase` resources target the same cluster, their valid
queries are combined into this same generated `ConfigMap`.

Database-scoped metrics participate in the database's readiness. The database
controller publishes custom-metrics participation before unrelated provisioning
gates, so an external-secret or role failure cannot hide the database's
monitoring intent from the cluster controller. The `PostgresDatabase` reports
`CustomMetricsReady=Unknown/CustomMetricsPending` and remains `Provisioning`
until the cluster confirms that the current complete contribution was applied.
This exchange is automatic; do not copy selectors into the `PostgresCluster`.

## Use more than one source

A monitoring block can reference several sources. This makes it possible for a
platform team and an application team to manage their definitions independently:

```yaml
spec:
  monitoring:
    customQueriesConfigMap:
      - name: postgres-platform-metrics
        key: queries.yaml
      - name: orders-team-metrics
        key: queries.yaml
```

Each monitoring block accepts up to 100 `ConfigMap` references. A single data key
can contain several query definitions, so one reference does not mean one metric.

## What you can and cannot configure

| You can | You cannot |
|---|---|
| Define several queries in one `ConfigMap` key | Put query definitions inline in a `PostgresCluster` or `PostgresDatabase` |
| Combine several source `ConfigMap` objects | Reference a `ConfigMap` from another namespace |
| Use `gauge` and `counter` values with optional labels | Define histograms, summaries, or CNPG-only query options through this API |
| Scope a query to one declared database | Set `target_databases` yourself |
| Reuse one source for different databases | Make a missing source optional |
| Update a source and let the operator regenerate the combined configuration | Treat the generated `<cluster-name>-metrics` `ConfigMap` as user-owned configuration |

`customQueriesConfigMap` uses Kubernetes' `ConfigMapKeySelector` shape, which
includes an `optional` field. This feature does not support optional sources.
Admission rejects the field when it is explicitly present, whether its value is
`true` or `false`. Omit `optional` entirely.

## Names and collisions

Query names must be unique within the scope where they run:

- Two cluster-wide definitions with the same name collide.
- Two definitions for the same database with the same name collide.
- The same name may be used in two different databases.
- A cluster-wide definition and a database-scoped definition may use the same
  name.

User-defined metrics always use the exported
`cnpg_splunk_operator_cluster_...` or
`cnpg_splunk_operator_database_...` namespace. This prevents a source query
named like a CNPG built-in, such as `backends`, from replacing that built-in.

Each referenced `ConfigMap` key is treated as one monitoring package. The
operator reports a collision as `CustomMetricsReady=False` and excludes the complete
later package, including its non-conflicting queries. It still publishes the
earlier package and other non-conflicting packages. This avoids installing only
part of a team's monitoring definition while keeping unaffected metrics current.

Do not rely on which package wins; rename or remove the duplicate.

For cluster-wide sources, reference order is used to resolve conflicts. This
ordering is deterministic for recovery, not a recommended override mechanism.
Database-scoped sources retain the order declared on that database; older
`PostgresDatabase` resources win before newer contributors are considered.

## Updates, errors, and recovery

The source `ConfigMap` is your configuration. The generated
`<cluster-name>-metrics` `ConfigMap` is managed by the operator, owned by the
underlying CNPG cluster, and safe to inspect—but not to edit. Manual edits are
overwritten, and deletion causes the operator to recreate it.

The operator publishes a new combined configuration only after it can read and
parse every referenced source. This keeps a malformed update from replacing a
working configuration.

While CNPG is still consuming an applied ConfigMap revision, database
acknowledgements are `Unknown/CustomMetricsConfiguring` even when an older
positive acknowledgement has the same database intent revision. Readiness
returns only after CNPG confirms the current generated ConfigMap resource
version.

The generated configuration must fit within Kubernetes' 1 MiB ConfigMap data
limit. The operator checks the translated aggregate before writing anything. If
it is too large, the previous complete configuration remains active and the
condition reports the actual and maximum sizes. Reduce the number or size of the
query definitions; raising the 100-reference limit does not raise the ConfigMap
size limit.

| Change | Result |
|---|---|
| Add or update valid source content | The generated configuration is refreshed |
| Delete a source, remove its key, or publish malformed YAML | `CustomMetricsReady=False`; the previous complete configuration remains active, if one exists. A database-scoped failure marks that contributor not ready. A cluster-scoped failure also blocks contributors that do not already have an exact positive acknowledgement for the active revision. |
| Introduce a duplicate name in one scope | The complete later source package is excluded; accepted packages are published and the condition reports the collision |
| Exceed the fixed 1 MiB generated ConfigMap size limit | Nothing is written; the previous complete configuration remains active |
| Fix or recreate the source | Reconciliation runs again and the condition recovers |
| Remove every cluster-wide and database-scoped reference | The generated `ConfigMap` is removed and monitoring reports `CustomMetricsDisabled` |
| Put a foreign object at the generated name while metrics are enabled | The operator disconnects its generated selector, leaves the foreign object untouched, reports an ownership conflict, and marks current database contributors not ready |
| Disable metrics while a foreign generated-name object is already selected | The operator preserves both the foreign object and selector and reports an ownership conflict. Remove the foreign selector or object to complete disablement. |

The `PostgresCluster` condition reports aggregate health. A database-scoped
problem is also reported on the affected `PostgresDatabase`, so users can see
that their current database contribution was not accepted.

```bash
kubectl get postgrescluster orders-postgres -n apps \
  -o jsonpath='{range .status.conditions[?(@.type=="CustomMetricsReady")]}{.status}{"\t"}{.reason}{"\t"}{.message}{"\n"}{end}'

kubectl get postgresdatabase orders-database -n apps \
  -o jsonpath='{range .status.conditions[?(@.type=="CustomMetricsReady")]}{.status}{"\t"}{.reason}{"\t"}{.message}{"\n"}{end}'
```

After a successful apply, the operator stores a private safety copy of the
confirmed complete payload. While a mutable source is invalid, it uses that copy
to recreate or repair the active generated ConfigMap and CNPG selector without
depending on the invalid source. The cluster condition remains `False` until the
source itself is fixed. A database revision may therefore still appear as
applied while its status is `False`; the status and reason are authoritative.
Fixing the source causes the cluster acknowledgement and database readiness to
recover automatically.

The operator emits a normal `CustomMetricsQueryApplied` Event only after CNPG
reports the exact generated ConfigMap resource version active and the operator
saves that newly confirmed revision.
It emits `CustomMetricsQueryRepaired` when the safety copy restores active state
during source invalidity. Readiness remains configuring until CNPG observes the
restored resource version. Idempotent reconciliations do not repeat these Events.

For a structurally valid YAML document, an invalid-query message lists every
missing or malformed field found across the selected package. Fix the reported
issues together; you do not need to submit repeatedly to discover the next
field error. A YAML syntax error can only report the parser failure because the
document cannot be decoded into query definitions. For exceptionally large
invalid packages, the message ends with `additional diagnostics omitted` before
it reaches Kubernetes' condition-message size limit.

Common reasons are:

| Reason | What to do |
|---|---|
| `CustomMetricsReady` | No action is needed; custom metrics are configured |
| `CustomMetricsDisabled` | No custom metric references exist |
| `CustomMetricsPending` | Wait for the cluster to acknowledge the database contribution; if it persists, inspect both resources' status |
| `CustomMetricsConfiguring` | CNPG has not yet confirmed the exact generated ConfigMap revision. This can occur during forward apply or while Safety restores last-known-good state; the operator rechecks automatically |
| `CustomMetricsConfigMapNotFound` | Restore the `ConfigMap` or its data key, or correct the reference |
| `InvalidQueryDefinition` | Correct the YAML; ensure required fields are present and query, value, and label identifiers use Prometheus-compatible names |
| `MetricNameCollision` | Rename or remove the duplicate in the reported scope |
| `CustomMetricsConfigTooLarge` | Reduce the number or size of referenced query definitions; the message contains actual and maximum byte counts |
| `CustomMetricsApplyRetrying` | A transient Kubernetes conflict, timeout, throttling, or service error interrupted apply; the operator retries automatically |
| `GeneratedResourceOwnershipConflict` | A ConfigMap using the generated `<cluster>-metrics` name is not owned by the current CNPG cluster; the operator disconnects that selector and will not modify the object. Remove or rename it so the operator can recover |
| `CustomMetricsApplyFailed` | Inspect the `PostgresCluster` events and operator logs for a Kubernetes or CNPG error |

For troubleshooting, inspect the combined configuration and recent events:

```bash
kubectl get configmap orders-postgres-metrics -n apps -o yaml
kubectl describe postgrescluster orders-postgres -n apps
```

Once your collector is scraping the PostgreSQL endpoint, use its query interface
to confirm the new series and labels. The reference
[observability dashboard](PostgreSQLObservabilityDashboard.md) describes the
repository's example monitoring setup and assumptions.
