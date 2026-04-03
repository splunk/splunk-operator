# PostgreSQL Observability Dashboard Example

This file provides a reference Grafana dashboard for the PostgreSQL observability model described in the PostgreSQL observability notes.

The dashboard JSON lives at:

- [PostgreSQLObservabilityDashboard.json](/Users/dpishchenkov/splunk-operator/docs/PostgreSQLObservabilityDashboard.json)

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
