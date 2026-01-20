#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "========================================="
echo "Deploying E2E Observability Stack"
echo "========================================="

# Check if helm is installed
if ! command -v helm &> /dev/null; then
    echo "Error: helm is not installed. Please install helm first."
    exit 1
fi

# Check if kubectl is installed
if ! command -v kubectl &> /dev/null; then
    echo "Error: kubectl is not installed. Please install kubectl first."
    exit 1
fi

# Step 1: Add Prometheus Helm repo
echo ""
echo "[1/6] Adding Prometheus Helm repository..."
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

# Step 2: Create observability namespace
echo ""
echo "[2/6] Creating observability namespace..."
kubectl create namespace observability --dry-run=client -o yaml | kubectl apply -f -

# Step 3: Install kube-prometheus-stack
echo ""
echo "[3/6] Installing kube-prometheus-stack..."
helm upgrade --install kube-prometheus prometheus-community/kube-prometheus-stack \
  --namespace observability \
  --set prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false \
  --set grafana.adminPassword=admin123 \
  --set prometheus.prometheusSpec.retention=7d \
  --set prometheus.prometheusSpec.storageSpec.volumeClaimTemplate.spec.resources.requests.storage=10Gi \
  --set grafana.sidecar.dashboards.enabled=true \
  --set grafana.sidecar.dashboards.label=grafana_dashboard \
  --wait \
  --timeout 10m

# Step 4: Deploy OTel Collector
echo ""
echo "[4/6] Deploying OpenTelemetry Collector..."
kubectl apply -f "${SCRIPT_DIR}/otel-collector/"

# Wait for OTel Collector to be ready
echo "Waiting for OTel Collector to be ready..."
kubectl wait --for=condition=available --timeout=5m deployment/otel-collector -n observability

# Step 5: Deploy Grafana Dashboard ConfigMap
echo ""
echo "[5/6] Deploying Grafana E2E Dashboard..."
kubectl apply -f "${SCRIPT_DIR}/prometheus/grafana-dashboard-configmap.yaml"

# Step 6: Deploy Neo4j
echo ""
echo "[6/6] Deploying Neo4j..."
kubectl apply -f "${SCRIPT_DIR}/neo4j/neo4j-deployment.yaml"

# Wait for Neo4j to be ready
echo "Waiting for Neo4j to be ready..."
kubectl wait --for=condition=available --timeout=10m deployment/neo4j -n neo4j

echo ""
echo "========================================="
echo "Deployment Complete!"
echo "========================================="
echo ""
echo "Access services via port-forward:"
echo ""
echo "Grafana:"
echo "  kubectl port-forward -n observability svc/kube-prometheus-grafana 3000:80"
echo "  URL: http://localhost:3000"
echo "  User: admin"
echo "  Pass: admin123"
echo ""
echo "Prometheus:"
echo "  kubectl port-forward -n observability svc/kube-prometheus-kube-prometheus 9090:9090"
echo "  URL: http://localhost:9090"
echo ""
echo "Neo4j:"
echo "  kubectl port-forward -n neo4j svc/neo4j 7474:7474 7687:7687"
echo "  Browser: http://localhost:7474"
echo "  User: neo4j"
echo "  Pass: changeme123"
echo ""
echo "OTel Collector (for tests running outside cluster):"
echo "  kubectl port-forward -n observability svc/otel-collector 4317:4317"
echo ""
echo "Environment variables for E2E tests:"
echo ""
echo "export E2E_OTEL_ENABLED=true"
echo "export E2E_OTEL_ENDPOINT=\"localhost:4317\""
echo "export E2E_OTEL_INSECURE=true"
echo "export E2E_NEO4J_ENABLED=true"
echo "export E2E_NEO4J_URI=\"bolt://localhost:7687\""
echo "export E2E_NEO4J_USER=\"neo4j\""
echo "export E2E_NEO4J_PASSWORD=\"changeme123\""
echo ""
echo "========================================="
