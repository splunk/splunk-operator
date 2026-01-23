# E2E Framework - Quick Start Guide

Get started with E2E testing in 5 minutes.

## Prerequisites

- Kubernetes cluster with Splunk Operator installed
- kubectl configured
- Go 1.22+ (for building the runner)

## Quick Start

### 1. Build the E2E Runner

```bash
go build -o bin/e2e-runner ./e2e/cmd/e2e-runner
```

### 2. Run a Smoke Test

```bash
./bin/e2e-runner \
  -cluster-provider eks \
  -operator-namespace splunk-operator \
  -spec-dir e2e/specs/operator/smoke_fast.yaml
```

### 3. View Results

```bash
# Check results
cat artifacts/results.json | jq '.tests[] | {name: .name, status: .status}'

# View summary
cat artifacts/summary.json

# View auto-generated PlantUML diagrams
ls artifacts/*.plantuml
# Output:
# - topology.plantuml         (topology architecture)
# - run-summary.plantuml      (test statistics)
# - failure-analysis.plantuml (failure patterns)
# - test-sequence-*.plantuml  (per-test sequences)
```

### 4. Visualize Test Execution (Optional)

Generate PNG images from PlantUML diagrams:

```bash
# Install PlantUML
brew install plantuml  # macOS
# or
apt-get install plantuml  # Ubuntu

# Generate images
cd artifacts/
plantuml *.plantuml

# View diagrams
open topology.png
open run-summary.png
```

**Or use VS Code**:
```bash
code --install-extension jebbs.plantuml
code artifacts/topology.plantuml  # Press Alt+D to preview
```

## With Observability (Optional)

Enable real-time metrics and graph export:

```bash
# Set observability endpoints
export E2E_OTEL_ENABLED=true
export E2E_OTEL_ENDPOINT="otel-collector.example.com:4317"
export E2E_NEO4J_ENABLED=true
export E2E_NEO4J_URI="bolt://neo4j.example.com:7687"
export E2E_NEO4J_USER="neo4j"
export E2E_NEO4J_PASSWORD="your-password"

# Run tests
./bin/e2e-runner e2e/specs/operator/smoke_fast.yaml
```

View graph data at: `http://neo4j.example.com:7474`

## Common Use Cases

### Use a Config File

```bash
# Start with the example config and override per-run values with flags/env
./bin/e2e-runner \
  --config e2e/config.example.yaml \
  -spec-dir e2e/specs/operator/smoke_fast.yaml
```

### Run Specific Tests by Tag

```bash
./bin/e2e-runner \
  -include-tags smoke \
  -spec-dir e2e/specs/operator
```

### Run Tests in Parallel

```bash
./bin/e2e-runner \
  -parallel 3 \
  -spec-dir e2e/specs/operator/smoke_fast.yaml
```

### Keep Resources for Debugging

```bash
./bin/e2e-runner \
  -skip-teardown \
  -spec-dir e2e/specs/operator/my_test.yaml

# Then inspect
export NS=$(cat artifacts/results.json | jq -r '.tests[0].metadata.namespace')
kubectl get all -n $NS
```

## Next Steps

- Read the full [README.md](./README.md) for detailed documentation
- Explore test specs in `e2e/specs/operator/`
