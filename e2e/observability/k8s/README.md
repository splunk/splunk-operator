# E2E Observability Stack for Kubernetes

This directory contains Kubernetes manifests to deploy a complete observability stack for E2E test monitoring.

## Components

- **kube-prometheus-stack** (Helm): Prometheus, Grafana, Alertmanager
- **OpenTelemetry Collector**: Receives OTLP traces and metrics from E2E tests
- **Neo4j**: Graph database for test relationship visualization

## Architecture

```
E2E Tests → OTel Collector (OTLP) → Prometheus
                                   → Tempo (traces)

E2E Tests → Graph Export → Neo4j

Grafana → Prometheus (metrics)
       → Tempo (traces)
       → Neo4j (graph queries)
```

## Quick Start

### 1. Install kube-prometheus-stack

```bash
# Add Prometheus community Helm repo
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

# Install kube-prometheus-stack in observability namespace
kubectl create namespace observability

helm install kube-prometheus prometheus-community/kube-prometheus-stack \
  --namespace observability \
  --set prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false \
  --set grafana.adminPassword=admin123 \
  --set prometheus.prometheusSpec.retention=7d \
  --set prometheus.prometheusSpec.storageSpec.volumeClaimTemplate.spec.resources.requests.storage=10Gi
```

### 2. Deploy OpenTelemetry Collector

```bash
kubectl apply -f otel-collector/
```

### 3. Deploy Neo4j

```bash
kubectl create namespace neo4j
kubectl apply -f neo4j/
```

### 4. Access Services

**Port Forward Grafana:**
```bash
kubectl port-forward -n observability svc/kube-prometheus-grafana 3000:80
```
- URL: http://localhost:3000
- Username: `admin`
- Password: `admin123`

**Port Forward Prometheus:**
```bash
kubectl port-forward -n observability svc/kube-prometheus-kube-prometheus 9090:9090
```
- URL: http://localhost:9090

**Port Forward Neo4j:**
```bash
kubectl port-forward -n neo4j svc/neo4j 7474:7474 7687:7687
```
- Browser: http://localhost:7474
- Bolt: bolt://localhost:7687
- Username: `neo4j`
- Password: `changeme123`

**OTel Collector Endpoint (from within cluster):**
- OTLP gRPC: `otel-collector.observability.svc.cluster.local:4317`
- OTLP HTTP: `otel-collector.observability.svc.cluster.local:4318`

## Running E2E Tests with Observability

### Enable OTel Export

```bash
export E2E_OTEL_ENABLED=true
export E2E_OTEL_ENDPOINT="otel-collector.observability.svc.cluster.local:4317"
export E2E_OTEL_INSECURE=true

# Run tests with graph and metrics enabled
./bin/e2e-runner \
  -spec e2e/specs/operator/smoke_fast.yaml \
  -cluster-provider eks \
  -graph=true \
  -metrics=true \
  -default-timeout 15m
```

### Enable Neo4j Graph Export

```bash
export E2E_NEO4J_ENABLED=true
export E2E_NEO4J_URI="bolt://localhost:7687"
export E2E_NEO4J_USER="neo4j"
export E2E_NEO4J_PASSWORD="changeme123"
export E2E_NEO4J_DATABASE="neo4j"

# Run tests
./bin/e2e-runner -spec e2e/specs/operator/smoke.yaml -graph=true
```

## Grafana Dashboard

The E2E test dashboard will be automatically provisioned. Access it via:
1. Open Grafana at http://localhost:3000
2. Navigate to Dashboards → E2E Test Metrics
3. View test duration, success rates, step performance, etc.

## ServiceMonitor for Prometheus

The OTel Collector exposes Prometheus metrics that are automatically scraped by Prometheus via ServiceMonitor.

## Querying Neo4j Graph

Example Cypher queries for test analysis:

```cypher
// Find all failed tests
MATCH (t:test {status: "failed"})
RETURN t.label, t.attributes

// Find tests using a specific dataset
MATCH (t:test)-[:USES_DATASET]->(d:dataset {label: "access_combined_data"})
RETURN t.label, t.attributes.status

// Find all tests in a run
MATCH (r:run {label: "20260119T194243Z"})-[:HAS_TEST]->(t:test)
RETURN t.label, t.attributes.status

// Analyze test dependencies
MATCH (t1:test)-[:HAS_STEP]->(s:step)-[:USES_DATASET]->(d:dataset)<-[:USES_DATASET]-(s2:step)<-[:HAS_STEP]-(t2:test)
WHERE t1 <> t2
RETURN t1.label, t2.label, d.label
```

## Troubleshooting

**OTel Collector not receiving metrics:**
- Check that E2E_OTEL_ENDPOINT is accessible from where tests run
- If running tests outside cluster, use port-forward: `kubectl port-forward -n observability svc/otel-collector 4317:4317`

**Neo4j connection refused:**
- Ensure port-forward is active
- Check Neo4j pod status: `kubectl get pods -n neo4j`
- View logs: `kubectl logs -n neo4j deployment/neo4j`

**Prometheus not scraping OTel metrics:**
- Verify ServiceMonitor: `kubectl get servicemonitor -n observability`
- Check Prometheus targets: http://localhost:9090/targets

## Cleanup

```bash
# Remove all observability components
helm uninstall kube-prometheus -n observability
kubectl delete namespace observability
kubectl delete namespace neo4j
```

## Advanced Configuration

### Persistent Storage

All components use persistent volumes. To customize storage:

**Prometheus**: Edit `storageSpec` in Helm values
**Neo4j**: Edit PVC in `neo4j/neo4j-deployment.yaml`

### Grafana Dashboards

Custom dashboards can be added via ConfigMaps in the `observability` namespace with label `grafana_dashboard: "1"`.

### OTel Collector Pipeline

Modify `otel-collector/otel-collector-config.yaml` to:
- Add additional exporters (Jaeger, Zipkin, etc.)
- Configure sampling
- Add processors for filtering/transformation
