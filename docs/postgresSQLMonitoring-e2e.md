# PostgreSQL Monitoring E2E on KIND

This validates the PostgreSQL and PgBouncer monitoring flow in namespace `test`.

## Goal

Verify that:

- PostgreSQL pods are scraped through pod annotations
- PgBouncer pooler pods are scraped through pod annotations
- no dedicated metrics `Service` is required
- no `ServiceMonitor` is used for PostgreSQL or PgBouncer

`ServiceMonitor` is still acceptable for operator-controller metrics if you want that separately, but it is not part of this feature validation.

The reference dashboard in [PostgreSQLObservabilityDashboard.json](./PostgreSQLObservabilityDashboard.json) assumes:

- `namespace=test`
- `cluster=postgresql-cluster-dev`
- `kube_pod_labels` is available for dashboard variables

## Prerequisites

- KIND cluster is running
- CNPG is installed
- Splunk Operator is installed
- CRDs are up to date

## 1. Install Prometheus and Grafana

Create `values.yaml`:

```yaml
grafana:
  adminPassword: admin

alertmanager:
  enabled: false

kubeStateMetrics:
  enabled: true

nodeExporter:
  enabled: false

prometheus:
  prometheusSpec:
    additionalScrapeConfigs:
      - job_name: annotated-pods
        kubernetes_sd_configs:
          - role: pod
        relabel_configs:
          - source_labels:
              [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
            action: keep
            regex: true
          - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_path]
            action: replace
            target_label: __metrics_path__
            regex: (.+)
          - source_labels:
              [__address__, __meta_kubernetes_pod_annotation_prometheus_io_port]
            action: replace
            regex: ([^:]+)(?::\d+)?;(\d+)
            replacement: $1:$2
            target_label: __address__
          - source_labels: [__meta_kubernetes_namespace]
            action: replace
            target_label: namespace
          - source_labels: [__meta_kubernetes_pod_name]
            action: replace
            target_label: pod
```

Install the stack:

```bash
kubectl create namespace monitoring

helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo add grafana https://grafana.github.io/helm-charts
helm repo update

helm install kube-prometheus prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  -f values.yaml
```

## 2. Optional: scrape operator-controller metrics

This is separate from the PostgreSQL and PgBouncer validation.

Grant Prometheus access:

```bash
kubectl apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: prometheus-splunk-operator-metrics
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: splunk-operator-metrics-reader
subjects:
- kind: ServiceAccount
  name: prometheus-kube-prometheus-prometheus
  namespace: monitoring
EOF
```

Optional `ServiceMonitor` for controller metrics only:

```bash
kubectl apply -f - <<EOF
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: splunk-operator-controller
  namespace: monitoring
  labels:
    release: kube-prometheus
spec:
  namespaceSelector:
    matchNames:
    - splunk-operator
  selector:
    matchLabels:
      control-plane: controller-manager
  endpoints:
  - port: https
    path: /metrics
    interval: 5s
    scheme: https
    tlsConfig:
      insecureSkipVerify: true
EOF
```

## 3. Apply sample resources

Create the namespace and apply the sample resources:

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

## 5. Verify annotations on real pods

PostgreSQL pods:

```bash
kubectl get pods -n test -l cnpg.io/cluster=postgresql-cluster-dev -o yaml | rg 'prometheus.io/'
```

Expected:

- `prometheus.io/scrape: "true"`
- `prometheus.io/path: /metrics`
- `prometheus.io/port: "9187"`

Pooler pods:

```bash
kubectl get pods -n test -l cnpg.io/poolerName=postgresql-cluster-dev-pooler-rw -o yaml | rg 'prometheus.io/'
kubectl get pods -n test -l cnpg.io/poolerName=postgresql-cluster-dev-pooler-ro -o yaml | rg 'prometheus.io/'
```

Expected:

- `prometheus.io/scrape: "true"`
- `prometheus.io/path: /metrics`
- `prometheus.io/port: "9127"`

## 6. Access Prometheus

```bash
kubectl port-forward -n monitoring svc/kube-prometheus-prometheus 9090:9090
```

Open:

- http://localhost:9090

Useful checks:

```promql
up{job="annotated-pods", namespace="test"}
```

```promql
count(count by (pod) (cnpg_pg_postmaster_start_time{namespace="test",pod=~"postgresql-cluster-dev-[0-9]+"}))
```

```promql
max(1 - clamp_max(cnpg_pgbouncer_last_collection_error{namespace="test",pod=~"postgresql-cluster-dev-pooler-rw-.*"}, 1))
```

```promql
sum(rate(cnpg_pg_stat_archiver_archived_count{namespace="test",pod=~"postgresql-cluster-dev-[0-9]+"}[5m]))
```

## 7. Access Grafana

Port-forward Grafana:

```bash
kubectl port-forward svc/kube-prometheus-grafana -n monitoring 3000:80
```

Open:

- http://localhost:3000

Login:

- user: `admin`
- password: `admin`

Use Grafana in one of two ways:

### Explore

1. Open **Explore**
2. Select the default **Prometheus** datasource
3. Run PromQL queries such as:

```promql
up{job="annotated-pods", namespace="test"}
```

```promql
cnpg_pg_postmaster_start_time{namespace="test"}
```

```promql
cnpg_pgbouncer_last_collection_error{namespace="test"}
```

### Dashboard import

You can also import the reference dashboard from:

- [PostgreSQLObservabilityDashboard.json](./PostgreSQLObservabilityDashboard.json)

In Grafana:

1. Go to **Dashboards**
2. Click **New** -> **Import**
3. Upload `docs/PostgreSQLObservabilityDashboard.json`
4. Select the Prometheus datasource
5. Set `namespace` to `test`
6. Set `cluster` to `postgresql-cluster-dev`

The dashboard variables use:

```promql
label_values(kube_pod_labels{label_cnpg_io_cluster!=""}, namespace)
```

and:

```promql
label_values(kube_pod_labels{label_cnpg_io_cluster!="", namespace="$namespace"}, label_cnpg_io_cluster)
```

So `kubeStateMetrics.enabled: true` in `values.yaml` is required for the imported dashboard to work as-is.

## 8. Optional disable test

Disable monitoring in the `PostgresCluster` and verify annotations disappear:

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

Then re-check:

```bash
kubectl get pods -n test -l cnpg.io/cluster=<cluster-name> -o yaml | rg 'prometheus.io/' || true
kubectl get pods -n test -l cnpg.io/poolerName=<cluster-name>-pooler-rw -o yaml | rg 'prometheus.io/' || true
kubectl get pods -n test -l cnpg.io/poolerName=<cluster-name>-pooler-ro -o yaml | rg 'prometheus.io/' || true
```

Prometheus should also stop showing those targets under `annotated-pods` after discovery refresh.

## Notes

- Use `ServiceMonitor` only for operator-controller metrics if needed.
- Do not use `ServiceMonitor` for PostgreSQL or PgBouncer in this E2E, because that bypasses the feature under test.
- Verify both:
  - reconciled CNPG specs
  - actual pod annotations
- PostgreSQL annotations come from CNPG `Cluster.Spec.InheritedMetadata`
- pooler annotations come from CNPG `Pooler.Spec.Template`
