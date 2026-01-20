#!/bin/bash
# Setup script for E2E framework with Neo4j knowledge graph

set -e

echo "======================================"
echo "E2E Framework Setup"
echo "======================================"
echo ""

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    echo "❌ Docker is not installed. Please install Docker first."
    exit 1
fi

echo "✓ Docker is installed"

# Check if Neo4j container already exists
if docker ps -a --format '{{.Names}}' | grep -q '^e2e-neo4j$'; then
    echo "⚠️  Neo4j container 'e2e-neo4j' already exists"
    read -p "Do you want to remove and recreate it? (y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo "Removing existing container..."
        docker rm -f e2e-neo4j
    else
        echo "Skipping Neo4j setup"
        exit 0
    fi
fi

# Start Neo4j
echo ""
echo "Starting Neo4j container..."
docker run -d \
    --name e2e-neo4j \
    -p 7474:7474 \
    -p 7687:7687 \
    -e NEO4J_AUTH=neo4j/e2epassword \
    -e NEO4J_PLUGINS='["apoc"]' \
    -v e2e-neo4j-data:/data \
    -v e2e-neo4j-logs:/logs \
    neo4j:5.15.0

echo "✓ Neo4j container started"

# Wait for Neo4j to be ready
echo ""
echo "Waiting for Neo4j to be ready..."
for i in {1..30}; do
    if docker logs e2e-neo4j 2>&1 | grep -q "Started"; then
        echo "✓ Neo4j is ready"
        break
    fi
    if [ $i -eq 30 ]; then
        echo "❌ Neo4j did not start within 30 seconds"
        exit 1
    fi
    sleep 1
    echo -n "."
done

# Create environment file
ENV_FILE=".env.e2e"
echo ""
echo "Creating environment configuration file: $ENV_FILE"

cat > "$ENV_FILE" << 'EOF'
# E2E Framework Configuration

# Neo4j Configuration
export E2E_NEO4J_ENABLED=true
export E2E_NEO4J_URI=bolt://localhost:7687
export E2E_NEO4J_USER=neo4j
export E2E_NEO4J_PASSWORD=e2epassword
export E2E_NEO4J_DATABASE=neo4j

# Test Configuration
export E2E_SPEC_DIR=./e2e/specs
export E2E_DATASET_REGISTRY=./e2e/datasets/datf-datasets.yaml
export E2E_ARTIFACT_DIR=./e2e/artifacts
export E2E_TOPOLOGY_MODE=suite  # or "test" for per-test topology
export E2E_LOG_COLLECTION=failure  # "always", "failure", or "never"
export E2E_PARALLELISM=4

# Metrics and Telemetry (optional)
export E2E_METRICS_ENABLED=true
export E2E_METRICS_PATH=./e2e/artifacts/metrics.prom
export E2E_GRAPH_ENABLED=true

# OpenTelemetry (optional - uncomment to enable)
# export E2E_OTEL_ENABLED=true
# export E2E_OTEL_ENDPOINT=localhost:4317
# export E2E_OTEL_SERVICE_NAME=splunk-operator-e2e
# export E2E_OTEL_INSECURE=true

# Object Store for App Framework Tests (optional)
# export E2E_OBJECTSTORE_PROVIDER=s3  # s3, gcs, or azure
# export E2E_OBJECTSTORE_BUCKET=your-bucket
# export E2E_OBJECTSTORE_PREFIX=e2e-tests/
# export E2E_OBJECTSTORE_REGION=us-west-2
# export E2E_OBJECTSTORE_ACCESS_KEY=your-access-key
# export E2E_OBJECTSTORE_SECRET_KEY=your-secret-key

# Dataset Cache
export E2E_CACHE_ENABLED=true
export E2E_CACHE_DIR=~/.e2e-cache
EOF

echo "✓ Environment file created"

# Build CLI tools
echo ""
echo "Building CLI tools..."

if ! command -v go &> /dev/null; then
    echo "⚠️  Go is not installed. Skipping CLI tool builds."
    echo "   Install Go to build: e2e-runner, e2e-query, e2e-matrix"
else
    echo "Building e2e-runner..."
    go build -o ./bin/e2e-runner ./e2e/cmd/e2e-runner/main.go 2>/dev/null || echo "⚠️  Failed to build e2e-runner"

    echo "Building e2e-query..."
    go build -o ./bin/e2e-query ./e2e/cmd/e2e-query/main.go 2>/dev/null || echo "⚠️  Failed to build e2e-query"

    echo "Building e2e-matrix..."
    go build -o ./bin/e2e-matrix ./e2e/cmd/e2e-matrix/main.go 2>/dev/null || echo "⚠️  Failed to build e2e-matrix"

    if [ -f "./bin/e2e-query" ]; then
        echo "✓ CLI tools built successfully in ./bin/"
    fi
fi

# Print instructions
echo ""
echo "======================================"
echo "✓ Setup Complete!"
echo "======================================"
echo ""
echo "Next steps:"
echo ""
echo "1. Load environment variables:"
echo "   source $ENV_FILE"
echo ""
echo "2. Access Neo4j browser:"
echo "   Open: http://localhost:7474"
echo "   Username: neo4j"
echo "   Password: e2epassword"
echo ""
echo "3. Run tests:"
echo "   source $ENV_FILE"
echo "   go run ./e2e/cmd/e2e-runner"
echo ""
echo "4. Query results:"
echo "   ./bin/e2e-query flaky-tests"
echo "   ./bin/e2e-query similar-failures --category OOMKilled"
echo "   ./bin/e2e-query success-rate --topology c3"
echo ""
echo "5. Generate tests from matrix:"
echo "   ./bin/e2e-matrix generate -m e2e/matrices/comprehensive.yaml -o e2e/specs/generated/"
echo ""
echo "Documentation:"
echo "  - Framework Guide: e2e/FRAMEWORK_GUIDE.md"
echo "  - Improvements: e2e/IMPROVEMENTS_SUMMARY.md"
echo ""
echo "To stop Neo4j:"
echo "  docker stop e2e-neo4j"
echo ""
echo "To remove Neo4j (including data):"
echo "  docker rm -f e2e-neo4j"
echo "  docker volume rm e2e-neo4j-data e2e-neo4j-logs"
echo ""
