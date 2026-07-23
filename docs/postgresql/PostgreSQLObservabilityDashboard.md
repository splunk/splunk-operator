---
title: Observability Dashboard
parent: PostgreSQL
nav_order: 4
---

# PostgreSQL Observability Dashboard Example

This file provides a reference Grafana dashboard for the PostgreSQL observability model described in the PostgreSQL observability notes.

The dashboard JSON lives at:

- [PostgreSQLObservabilityDashboard.json](./PostgreSQLObservabilityDashboard.json)

## Purpose

This dashboard is a reference artifact only.

It is meant to show how a Grafana dashboard could combine:

- runtime PostgreSQL and PgBouncer metrics exposed through the `PostgresCluster` observability path
- controller metrics emitted by the PostgreSQL controllers

It is not meant to imply that Grafana runtime resources are managed by the operator.

## Panels Included

The sample dashboard includes:

- PostgreSQL target count
- RW and RO PgBouncer availability
- WAL archive rate
- failed `PostgresDatabase` count
- database size by database
- PgBouncer active and waiting clients
- WAL activity
- fleet database phases
- controller reconcile activity and errors
- PostgreSQL time-to-Ready latency (P99)

## Provisioning latency

`splunk_operator_postgres_provisioning_duration_seconds` is a controller-level
histogram with the `controller` label set to `postgrescluster` or
`postgresdatabase`. Each sample measures one completed time-to-Ready cycle.
For a newly created resource, the cycle begins at its Kubernetes creation time.
For an existing resource, it begins when a readiness-affecting operation such
as scaling, upgrade, switchover, recovery, restart, or configuration rollout
is detected, and ends when the resource successfully persists `Ready` again.

The duration is wall-clock time, so it includes operator downtime and
scheduling delay. Samples are not capped. While a cycle is in progress, the
resource status exposes `lastTransitionTime`; the operator clears it when that
cycle reaches Ready. Ordinary reconciliations that do not leave Ready do not
add samples.

### End-to-end readiness and active reconcile time

Use the following complementary metrics together:

| Metric | What it measures | Dependency wait time |
| --- | --- | --- |
| `splunk_operator_postgres_provisioning_duration_seconds` | One complete, user-visible time-to-Ready cycle. | Included: CNPG/Kubernetes convergence, image pulls, scheduling, recovery, retries, operator downtime, and time between reconciles. |
| `controller_runtime_reconcile_time_seconds` | One active controller reconcile invocation. | Excluded between invocations: a `RequeueAfter` delay or time while a dependency converges does not accumulate. Synchronous API and database calls within that invocation are included. |

A high time-to-Ready P99 with a low reconcile-time P99 indicates that most of
the delay occurred outside active controller work, commonly while waiting for
CNPG, Kubernetes, or another dependency. These aggregate histograms should not
be subtracted to calculate an exact dependency-wait duration; use resource
phase and condition transitions to identify the specific blocker.

Both metrics use `controller="postgrescluster"` or
`controller="postgresdatabase"`, so their series can be grouped and joined by
the same controller label.

### Day-2 use

To see the number of completed time-to-Ready cycles in the last day, run:

    sum by (controller) (
      increase(splunk_operator_postgres_provisioning_duration_seconds_count[24h])
    )

The reference dashboard includes a `PostgreSQL provisioning latency (P99)`
panel. To add the same panel to another Grafana dashboard, create a **Time
series** visualization, use seconds as the unit, set the legend to
`{{controller}}`, and use this PromQL query:

    histogram_quantile(0.99,
      sum by (le, controller) (
        rate(splunk_operator_postgres_provisioning_duration_seconds_bucket[15m])
      )
    )

For the active reconcile-time P99, use the controller-runtime histogram:

    histogram_quantile(0.99,
      sum by (le, controller) (
        rate(controller_runtime_reconcile_time_seconds_bucket{
          controller=~"postgrescluster|postgresdatabase"
        }[15m])
      )
    )

For alerting, use the same P99 query and configure a condition above `120`
seconds for 15 minutes. It produces separate results for `postgrescluster` and
`postgresdatabase`. The P99 query has no result until at least one readiness
cycle is observed during its selected time window.

## Assumptions

The sample queries assume:

- Prometheus is scraping the PostgreSQL metrics `Service` created by the `PostgresCluster` controller
- Prometheus is scraping the PgBouncer metrics `Service` objects created for RW and RO poolers
- Prometheus series include `namespace` and `service` labels
- the cluster metrics service is named `<cluster>-postgres-metrics`
- the PgBouncer metrics services are named `<cluster>-pooler-rw-metrics` and `<cluster>-pooler-ro-metrics`
- the controller metrics branch is present for the `splunk_operator_postgres_*` metrics

If your Prometheus relabeling differs, you may need to adjust the dashboard queries.

## Import Notes

To use the dashboard:

1. Import the JSON file into Grafana.
2. Select the correct Prometheus datasource.
3. Choose the namespace.
4. Choose the cluster name using the derived `cluster` variable.

## Notes On Candidate Metrics

Some PgBouncer queries in the sample use metrics that are good candidates but should still be verified against actual exporter output in the merged branch:

- `cnpg_pgbouncer_pools_cl_waiting`
- `cnpg_pgbouncer_pools_maxwait`
- `cnpg_pgbouncer_stats_avg_wait_time`
- `cnpg_pgbouncer_stats_total_wait_time`

If those exact series are not present, keep the panel shape and replace the query with the actual exported metric name.
