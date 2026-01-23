# Splunk Operator - AI Agent Guide

This guide helps AI coding assistants understand the Splunk Operator project structure, development workflow, and common operations.

## Project Overview

The Splunk Operator is a Kubernetes operator that manages Splunk Enterprise deployments. It is built using:
- **Language**: Go 1.25.5
- **Framework**: Kubernetes Operator SDK with controller-runtime
- **Test Framework**: Ginkgo/Gomega
- **CRD API Versions**: v1, v1alpha2, v1alpha3, v1beta1, v2, v3, v4

## Repository Structure

```
├── api/                    # Custom Resource Definitions (CRDs) for all API versions
│   ├── v4/                # Current stable API version
│   └── v3/                # Previous API version
├── cmd/                    # Main entry point for the operator
├── config/                 # Kubernetes manifests and configuration
│   ├── crd/               # CRD base files
│   ├── samples/           # Example CR manifests
│   ├── default/           # Default kustomize configurations
│   └── rbac/              # RBAC configurations
├── docs/                   # User-facing documentation
├── helm-chart/            # Helm charts for operator and enterprise
├── internal/              # Internal controller logic
├── pkg/                   # Core business logic
│   ├── splunk/
│   │   ├── common/       # Common utilities
│   │   ├── enterprise/   # Enterprise-specific logic
│   │   ├── client/       # Splunk API client
│   │   └── util/         # Utility functions
├── test/                  # Integration tests
│   ├── testenv/          # Test environment utilities
│   └── */                # Test suites by feature
├── tools/                 # Helper scripts and utilities
└── kuttl/                 # KUTTL test scenarios
```

## Common Makefile Commands

### Development Commands

```bash
# Display all available make targets with descriptions
make help

# Format code
make fmt

# Generate manifests (CRDs, RBAC, webhooks)
make manifests

# Generate DeepCopy methods
make generate

# Build the operator binary
make build

# Run unit tests
make test

# Build multi-platform images with buildx
make docker-buildx IMG=<your-image> PLATFORMS=linux/amd64,linux/arm64
```

### Deployment Commands

```bash
# Install CRDs into cluster
make install

# Uninstall CRDs from cluster
make uninstall

# Deploy operator to cluster
make deploy IMG=<your-image> NAMESPACE=<namespace> ENVIRONMENT=<env>

# Undeploy operator from cluster
make undeploy
```

### Documentation Commands

```bash
# Preview documentation locally (requires Ruby and bundler)
make docs-preview
# Access at http://localhost:4000/splunk-operator
```

## Development Workflow

### 1. Making Code Changes

When modifying the operator code, follow this workflow:

```bash
# 1. Create a feature branch from develop
git checkout -b feature/your-feature develop

# 2. Make your changes to the codebase
#    - API changes: api/v4/*.go
#    - Controller logic: internal/controller/*.go
#    - Business logic: pkg/splunk/**/*.go

# 3. If you modified API types, regenerate code
make manifests generate

# 4. Format and vet your code
make fmt vet

# 5. Run unit tests
make test

# 6. Build the operator
make build
```

### 2. Testing Changes

#### Unit Tests

Unit tests are located alongside source files and use Ginkgo/Gomega:

```bash
# Run all unit tests with coverage
make test

# Run specific test packages directly
KUBEBUILDER_ASSETS="$(shell setup-envtest use 1.34.0 -p path)" \
  ginkgo -v ./pkg/splunk/common
```

Test coverage includes:
- `pkg/splunk/common` - Common utilities
- `pkg/splunk/enterprise` - Enterprise logic
- `pkg/splunk/client` - API client
- `pkg/splunk/util` - Utilities
- `internal/controller` - Controller reconciliation logic

#### Integration Tests

**Integration Test Structure:**
- Each test suite has its own directory under `test/`
- Suite file: `*_suite_test.go` - Creates TestEnv (namespace)
- Spec files: `*_test.go` - Contains test cases (It blocks)
- Test utilities: `test/testenv/` - Helper functions for deployments

**Test Categories:**
- `test/smoke/` - Basic smoke tests
- `test/licensemanager/` - License manager tests
- `test/monitoring_console/` - Monitoring console tests
- `test/appframework_aws/` - App Framework with AWS S3
- `test/appframework_az/` - App Framework with Azure Blob
- `test/appframework_gcp/` - App Framework with GCP Storage
- `test/smartstore/` - SmartStore functionality
- `test/secret/` - Secret management
- `test/custom_resource_crud/` - CR CRUD operations

#### KUTTL Tests

KUTTL provides declarative end-to-end testing:

```bash
# KUTTL test scenarios are in kuttl/tests/
# Run with kubectl-kuttl (if installed)
kubectl kuttl test --config kuttl/kuttl-test-kind.yaml
```

### 3. Documentation Updates

When making changes that affect users:

```bash
# 1. Update relevant documentation in docs/
#    - GettingStarted.md - Installation and basic usage
#    - Examples.md - Code examples
#    - CustomResources.md - CR specifications
#    - AppFramework.md - App Framework details
#    - SmartStore.md - SmartStore configuration

# 2. Preview documentation locally
make docs-preview

# 3. Update CONTRIBUTING.md if workflow changes
```

## Environment Variables

Key environment variables used in development:

```bash
# Operator configuration
NAMESPACE=splunk-operator                          # Target namespace
WATCH_NAMESPACE=""                                 # Watch all namespaces (cluster-wide)
ENVIRONMENT=default                                # Deployment environment

# Splunk configuration
SPLUNK_ENTERPRISE_IMAGE=docker.io/splunk/splunk:10.0.0   # Splunk Enterprise image
SPLUNK_GENERAL_TERMS=""                           # SGT acceptance (required)

# Testing
SPLUNK_OPERATOR_IMAGE=splunk/splunk-operator:latest
CLUSTER_PROVIDER=kind                              # kind, eks, azure, gcp
PRIVATE_REGISTRY=localhost:5000

# Cloud provider credentials (for integration tests)
TEST_S3_ACCESS_KEY_ID=...
TEST_S3_SECRET_ACCESS_KEY=...
STORAGE_ACCOUNT=...                                # Azure
STORAGE_ACCOUNT_KEY=...                            # Azure
GCP_SERVICE_ACCOUNT_KEY=...                        # GCP
```

## Debugging Tips

### Local Development

```bash
# Watch CRDs being reconciled
kubectl get pods -n splunk-operator -w

# Check operator logs
kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager -f

# Describe a Custom Resource
kubectl describe <cr-type> <cr-name> -n <namespace>
```

### Common Issues

1. **CRD not found**: Run `make install` to install CRDs
2. **Permission errors**: Check RBAC with `kubectl auth can-i --list`
3. **Image pull errors**: Verify `IMG` variable and registry access

## Additional Resources
- [Operator SDK Documentation](https://sdk.operatorframework.io/)
- [Kubernetes API Reference](https://kubernetes.io/docs/reference/)
- [Splunk Enterprise Documentation](https://help.splunk.com/en)
