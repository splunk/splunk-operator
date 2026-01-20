#!/bin/bash
# Setup Neo4j on Kubernetes cluster for E2E tests

set -e

echo "======================================"
echo "Neo4j on Kubernetes Setup"
echo "======================================"
echo ""

# Check if kubectl is installed
if ! command -v kubectl &> /dev/null; then
    echo "❌ kubectl is not installed. Please install kubectl first."
    exit 1
fi

# Check if connected to cluster
if ! kubectl cluster-info &> /dev/null; then
    echo "❌ Not connected to a Kubernetes cluster."
    echo "   Please configure kubectl to connect to your cluster."
    exit 1
fi

echo "✓ Connected to Kubernetes cluster"
kubectl cluster-info | head -1

# Prompt for namespace
read -p "Enter namespace for Neo4j (default: default): " NAMESPACE
NAMESPACE=${NAMESPACE:-default}

# Check if namespace exists
if ! kubectl get namespace "$NAMESPACE" &> /dev/null; then
    echo ""
    read -p "Namespace '$NAMESPACE' doesn't exist. Create it? (y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        kubectl create namespace "$NAMESPACE"
        echo "✓ Namespace '$NAMESPACE' created"
    else
        echo "❌ Aborted"
        exit 1
    fi
fi

# Update namespace in deployment file
echo ""
echo "Updating deployment file with namespace: $NAMESPACE"
sed "s/namespace: default/namespace: $NAMESPACE/g" e2e/k8s/neo4j-deployment.yaml > /tmp/neo4j-deployment.yaml

# Apply the deployment
echo ""
echo "Deploying Neo4j to Kubernetes..."
kubectl apply -f /tmp/neo4j-deployment.yaml

# Wait for Neo4j to be ready
echo ""
echo "Waiting for Neo4j pod to be ready (this may take 2-3 minutes)..."
kubectl wait --for=condition=ready pod \
    -l app=neo4j \
    -n "$NAMESPACE" \
    --timeout=300s

echo "✓ Neo4j pod is ready"

# Get service details
NEO4J_SERVICE=$(kubectl get svc neo4j -n "$NAMESPACE" -o jsonpath='{.spec.clusterIP}')
echo ""
echo "======================================"
echo "Neo4j is running!"
echo "======================================"
echo ""
echo "Service Details:"
echo "  Cluster IP: $NEO4J_SERVICE"
echo "  HTTP Port: 7474"
echo "  Bolt Port: 7687"
echo ""

# Check service type
SERVICE_TYPE=$(kubectl get svc neo4j -n "$NAMESPACE" -o jsonpath='{.spec.type}')
if [ "$SERVICE_TYPE" = "LoadBalancer" ]; then
    echo "Getting LoadBalancer IP (may take a minute)..."
    EXTERNAL_IP=""
    for i in {1..30}; do
        EXTERNAL_IP=$(kubectl get svc neo4j -n "$NAMESPACE" -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || echo "")
        if [ -n "$EXTERNAL_IP" ]; then
            break
        fi
        sleep 2
    done
    if [ -n "$EXTERNAL_IP" ]; then
        echo "  External IP: $EXTERNAL_IP"
        echo ""
        echo "Access Neo4j browser:"
        echo "  http://$EXTERNAL_IP:7474"
    fi
fi

# Create environment configuration
ENV_FILE=".env.e2e-k8s"
echo ""
echo "Creating environment configuration: $ENV_FILE"

cat > "$ENV_FILE" << EOF
# E2E Framework Configuration (Kubernetes Neo4j)

# Neo4j Configuration
export E2E_NEO4J_ENABLED=true
export E2E_NEO4J_URI=bolt://${NEO4J_SERVICE}:7687
export E2E_NEO4J_USER=neo4j
export E2E_NEO4J_PASSWORD=e2epassword
export E2E_NEO4J_DATABASE=neo4j

# Test Configuration
export E2E_SPEC_DIR=./e2e/specs
export E2E_DATASET_REGISTRY=./e2e/datasets/datf-datasets.yaml
export E2E_ARTIFACT_DIR=./e2e/artifacts
export E2E_TOPOLOGY_MODE=suite
export E2E_LOG_COLLECTION=failure
export E2E_PARALLELISM=4

# Metrics and Telemetry
export E2E_METRICS_ENABLED=true
export E2E_METRICS_PATH=./e2e/artifacts/metrics.prom
export E2E_GRAPH_ENABLED=true

# Dataset Cache
export E2E_CACHE_ENABLED=true
export E2E_CACHE_DIR=~/.e2e-cache
EOF

echo "✓ Environment file created"

# Port forwarding instructions
echo ""
echo "======================================"
echo "Usage Instructions"
echo "======================================"
echo ""
echo "1. For LOCAL access to Neo4j browser, run:"
echo "   kubectl port-forward -n $NAMESPACE svc/neo4j 7474:7474 7687:7687"
echo "   Then open: http://localhost:7474"
echo "   Username: neo4j"
echo "   Password: e2epassword"
echo ""
echo "2. For TESTS running IN K8s cluster:"
echo "   Tests will connect to: bolt://neo4j.${NAMESPACE}.svc.cluster.local:7687"
echo "   (Already configured in .env.e2e-k8s)"
echo ""
echo "3. For TESTS running LOCALLY (e.g., on laptop):"
echo "   Terminal 1: kubectl port-forward -n $NAMESPACE svc/neo4j 7687:7687"
echo "   Terminal 2: source .env.e2e-k8s"
echo "              # Update E2E_NEO4J_URI to bolt://localhost:7687"
echo "              export E2E_NEO4J_URI=bolt://localhost:7687"
echo "              go run ./e2e/cmd/e2e-runner"
echo ""
echo "4. Query the graph:"
echo "   go build -o e2e-query ./e2e/cmd/e2e-query"
echo "   ./e2e-query flaky-tests"
echo ""
echo "To remove Neo4j:"
echo "  kubectl delete -f e2e/k8s/neo4j-deployment.yaml -n $NAMESPACE"
echo ""
