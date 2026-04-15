# PostgreSQL Monitoring E2E with OTel Collector

This document describes how to validate PostgreSQL and PgBouncer monitoring with OpenTelemetry Collector using the current annotation-based design.

## Goal

Verify that:

- PostgreSQL pods are discoverable through `prometheus.io/*` pod annotations
- PgBouncer pooler pods are discoverable through `prometheus.io/*` pod annotations
- OTel Collector scrapes those targets through Kubernetes pod discovery
- scraped metrics are re-exposed by OTel and then queried through Grafana
- disabling monitoring removes those scrape targets

This test intentionally does not use dedicated metrics `Service`s or `ServiceMonitor`s for PostgreSQL and PgBouncer.

The reference dashboard in [PostgreSQLObservabilityDashboard.json](./PostgreSQLObservabilityDashboard.json) assumes:

- `namespace=test`
- `cluster=postgresql-cluster-dev`
- `kube_pod_labels` is available from kube-state-metrics

## Prerequisites

- KIND cluster is running
- CNPG is installed
- Splunk Operator is installed
- CRDs are up to date
- test resources exist in namespace `test`

## Recommended Setup

Use OTel Collector for scraping and re-expose the metrics to Prometheus for Grafana queries.

In this setup:

- OTel Collector scrapes annotated PostgreSQL and PgBouncer pods
- OTel Collector re-exposes those metrics on its own Prometheus exporter endpoint
- Prometheus scrapes the OTel Collector pod
- Grafana queries Prometheus

Grafana does not query OTel Collector directly. The Grafana datasource remains Prometheus.

## 1. Deploy OTel Collector

Use the concrete Helm values file:

- [otel-collector-values.yaml](../test/postgresql/monitoring/otel-collector-values.yaml)
- [otel-rbac.yaml](../test/postgresql/monitoring/otel-rbac.yaml)

Install the Collector:

```bash
kubectl create namespace monitoring

helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm repo update

helm install otel open-telemetry/opentelemetry-collector \
  --namespace monitoring \
  -f test/postgresql/monitoring/otel-collector-values.yaml
```

If the `otel` release already exists, use:

```bash
helm upgrade otel open-telemetry/opentelemetry-collector \
  --namespace monitoring \
  -f test/postgresql/monitoring/otel-collector-values.yaml
```

Grant the Collector RBAC required for Kubernetes pod discovery:

```bash
kubectl apply -f test/postgresql/monitoring/otel-rbac.yaml
```

This setup uses:

- Prometheus receiver with Kubernetes pod discovery
- `prometheus.io/*` relabeling
- Prometheus exporter on port `8889`
- `debug` exporter for easy validation in logs

If this RBAC is missing, the Collector will fail with errors like:

```text
failed to list *v1.Pod: pods is forbidden
```

because the service account needs cluster-scoped `get`, `list`, and `watch` access for pod discovery.

## 2. Install Prometheus and Grafana for the OTel path

Use the Prometheus values file that scrapes only the OTel Collector exporter:

- [prometheus-via-otel-values.yaml](../test/postgresql/monitoring/prometheus-via-otel-values.yaml)

Install:

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo add grafana https://grafana.github.io/helm-charts
helm repo update

helm install kube-prometheus prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  -f test/postgresql/monitoring/prometheus-via-otel-values.yaml
```

If the `kube-prometheus` release already exists, use:

```bash
helm upgrade kube-prometheus prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  -f test/postgresql/monitoring/prometheus-via-otel-values.yaml
```

This is important: Prometheus should scrape the OTel Collector exporter, not the PostgreSQL and PgBouncer pods directly. Otherwise Grafana will bypass OTel or you will get duplicate series.

## 3. Apply PostgreSQL sample resources

Apply:

- `config/samples/enterprise_v4_postgresclusterclass_dev.yaml`
- `config/samples/enterprise_v4_postgrescluster_dev.yaml`
- `config/samples/enterprise_v4_postgresdatabase.yaml`

Example:

```bash
kubectl create namespace test
kubectl apply -f config/samples/enterprise_v4_postgresclusterclass_dev.yaml
kubectl apply -n test -f config/samples/enterprise_v4_postgrescluster_dev.yaml
kubectl apply -n test -f config/samples/enterprise_v4_postgresdatabase.yaml
```

These samples create:

- `PostgresClusterClass` `postgresql-dev`
- `PostgresCluster` `postgresql-cluster-dev`
- `PostgresDatabase` `splunk-databases`

## 4. Verify reconciled resources

```bash
kubectl get postgrescluster -n test
kubectl get postgresdatabase -n test
kubectl get cluster.postgresql.cnpg.io -n test
kubectl get pooler.postgresql.cnpg.io -n test
kubectl get pods -n test
```

## 5. Verify annotations on workloads

PostgreSQL pods:

```bash
kubectl get pods -n test -l cnpg.io/cluster=postgresql-cluster-dev -o yaml | rg 'prometheus.io/'
```

Expected:

- `prometheus.io/scrape: "true"`
- `prometheus.io/path: /metrics`
- `prometheus.io/port: "9187"`

PgBouncer RW pooler pods:

```bash
kubectl get pods -n test -l cnpg.io/poolerName=postgresql-cluster-dev-pooler-rw -o yaml | rg 'prometheus.io/'
```

PgBouncer RO pooler pods:

```bash
kubectl get pods -n test -l cnpg.io/poolerName=postgresql-cluster-dev-pooler-ro -o yaml | rg 'prometheus.io/'
```

Expected for both:

- `prometheus.io/scrape: "true"`
- `prometheus.io/path: /metrics`
- `prometheus.io/port: "9127"`

## 6. Verify OTel Collector scraping

If using the `debug` exporter:

```bash
kubectl logs -n <otel-namespace> deploy/<otel-collector> -f
```

Look for metrics such as:

- `cnpg_pg_postmaster_start_time`
- `cnpg_pg_database_size_bytes`
- `cnpg_collector_pg_wal`
- `cnpg_pg_stat_archiver_archived_count`
- `cnpg_pgbouncer_last_collection_error`
- `cnpg_pgbouncer_pools_cl_active`

## 7. Verify Prometheus is scraping OTel

Port-forward Prometheus:

```bash
kubectl port-forward -n monitoring svc/kube-prometheus-prometheus 9090:9090
```

Check that Prometheus is scraping the OTel Collector pod:

```promql
up{job="otel-collector", namespace="monitoring"}
```

If this returns no series, the usual causes are:

- the `kube-prometheus` release was not upgraded with [prometheus-via-otel-values.yaml](../test/postgresql/monitoring/prometheus-via-otel-values.yaml)
- Prometheus is still using the old `annotated-pods` scrape job
- kube-state-metrics is still disabled, which also breaks the dashboard variables

Then verify PostgreSQL and PgBouncer metrics coming through that OTel path:

```promql
count(count by (pod) (cnpg_pg_postmaster_start_time{job="otel-collector",namespace="test",pod=~"postgresql-cluster-dev-[0-9]+"}))
```

```promql
max(1 - clamp_max(cnpg_pgbouncer_last_collection_error{job="otel-collector",namespace="test",pod=~"postgresql-cluster-dev-pooler-rw-.*"}, 1))
```

## 8. Access Grafana

Port-forward Grafana:

```bash
kubectl port-forward -n monitoring svc/kube-prometheus-grafana 3000:80
```

Open:

- http://localhost:3000

Login:

- user: `admin`
- password: `admin`

Use the default Prometheus datasource. In this setup, Grafana is using metrics that flowed through OTel because Prometheus is scraping only the OTel Collector exporter.

## 9. Verify dashboard queries

Import:

- [PostgreSQLObservabilityDashboard.json](./PostgreSQLObservabilityDashboard.json)

Set:

- `namespace=test`
- `cluster=postgresql-cluster-dev`

The dashboard does not need query changes for this path, but it assumes kube-state-metrics is enabled for the `namespace` and `cluster` variables.

You can confirm the live Prometheus release picked up the right values with:

```bash
helm get values -n monitoring kube-prometheus
```

Expected:

- `kubeStateMetrics.enabled: true`
- additional scrape job `otel-collector`

## 10. Verify backend metrics

Validate with queries such as:

```promql
up{job="otel-collector", namespace="monitoring"}
```

```promql
count(count by (pod) (cnpg_pg_postmaster_start_time{job="otel-collector",namespace="test",pod=~"postgresql-cluster-dev-[0-9]+"}))
```

```promql
max(1 - clamp_max(cnpg_pgbouncer_last_collection_error{job="otel-collector",namespace="test",pod=~"postgresql-cluster-dev-pooler-rw-.*"}, 1))
```

```promql
sum by (pooler) (label_replace(cnpg_pgbouncer_pools_cl_active{job="otel-collector",namespace="test",pod=~"postgresql-cluster-dev-pooler-(rw|ro)-.*"}, "pooler", "$1", "pod", ".*-pooler-(rw|ro)-.*"))
```

The dashboard variables use:

```promql
label_values(kube_pod_labels{label_cnpg_io_cluster!=""}, namespace)
```

and:

```promql
label_values(kube_pod_labels{label_cnpg_io_cluster!="", namespace="$namespace"}, label_cnpg_io_cluster)
```

## 11. Disable monitoring and validate removal

Disable both metrics paths:

```bash
kubectl patch postgrescluster postgresql-cluster-dev -n test --type=merge -p '
spec:
  monitoring:
    postgresqlMetrics:
      disabled: true
    connectionPoolerMetrics:
      disabled: true
'
```

Re-check pod annotations:

```bash
kubectl get pods -n test -l cnpg.io/cluster=postgresql-cluster-dev -o yaml | rg 'prometheus.io/' || true
kubectl get pods -n test -l cnpg.io/poolerName=postgresql-cluster-dev-pooler-rw -o yaml | rg 'prometheus.io/' || true
kubectl get pods -n test -l cnpg.io/poolerName=postgresql-cluster-dev-pooler-ro -o yaml | rg 'prometheus.io/' || true
```

Expected:

- scrape annotations disappear
- OTel stops scraping those targets after discovery refresh

## Test Plan

### Test 1: PostgreSQL annotations are present

Steps:

1. Apply monitoring-enabled class and cluster
2. Wait for PostgreSQL pods
3. Inspect pod annotations

Pass criteria:

- PostgreSQL pods contain the expected scrape annotations

### Test 2: Pooler annotations are present

Steps:

1. Apply class with `connectionPoolerEnabled=true`
2. Wait for RW and RO poolers and their pods
3. Inspect pooler pod annotations

Pass criteria:

- RW and RO pooler pods contain the expected scrape annotations

### Test 3: OTel Collector scrapes PostgreSQL and poolers

Steps:

1. Run Collector with pod discovery
2. Inspect Collector logs or backend metrics

Pass criteria:

- PostgreSQL metrics are visible in OTel logs
- PgBouncer metrics are visible in OTel logs

### Test 4: Prometheus and Grafana use the OTel path

Steps:

1. Verify `up{job="otel-collector", namespace="monitoring"}`
2. Verify PostgreSQL and PgBouncer metrics with `job="otel-collector"`
3. Import the dashboard and select the Prometheus datasource

Pass criteria:

- Prometheus is scraping the OTel Collector exporter
- Grafana panels return data from the `otel-collector` job

### Test 5: Disable override removes scrape targets

Steps:

1. Patch the `PostgresCluster` to disable both monitoring paths
2. Re-check workload annotations
3. Re-check Collector or backend

Pass criteria:

- annotations are removed
- targets disappear from Collector/backend over time

### Test 6: Cluster-only disable path

Steps:

1. Keep class monitoring enabled
2. Disable only in `PostgresCluster.spec.monitoring`

Pass criteria:

- class defaults remain unchanged
- only the target cluster loses annotations

## Troubleshooting

If no metrics appear:

1. Check pod annotations first
2. Check Collector logs
3. Check whether the Collector is using `role: pod`
4. Check relabeling for `prometheus.io/scrape`
5. Check namespace filters in your backend queries

Useful quick queries:

```promql
up{namespace="test"}
```

```promql
cnpg_pg_postmaster_start_time_seconds{namespace="test"}
```

```promql
cnpg_pgbouncer_up{namespace="test"}
```
