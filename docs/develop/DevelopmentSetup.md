---
title: Development Setup
parent: Develop & Contribute
nav_order: 2
---

# Development Setup

This guide walks you through setting up a local development environment for the Splunk Operator.

## Prerequisites

| Tool | Version | Notes |
|------|---------|-------|
| [Go](https://golang.org/doc/install) | 1.26.2+ | See `GO_VERSION` in `.env` for the exact version used in CI |
| [Docker Engine](https://docs.docker.com/install/) | Latest | Required for building container images |
| [Operator SDK](https://github.com/operator-framework/operator-sdk) | v1.42.0 | See `OPERATOR_SDK_VERSION` in `.env` |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | v1.29+ | For interacting with your test cluster |

### Installing the Operator SDK

```shell
git clone -b v1.42.0 https://github.com/operator-framework/operator-sdk
cd operator-sdk
make install
```

You may need to add `$GOPATH/bin` to your `PATH`:

```shell
export PATH=${PATH}:${GOPATH}/bin
```

### Recommended Go tools

These are used by various `make` targets:

```shell
go install golang.org/x/lint/golint@latest
go install golang.org/x/tools/cmd/cover@latest
go install github.com/mattn/goveralls@latest
go install github.com/mikefarah/yq/v3@latest
go install github.com/go-delve/delve/cmd/dlv@latest
```

## Cloning the Repository

```shell
git clone git@github.com:splunk/splunk-operator.git
cd splunk-operator
```

## Common Makefile Targets

Run `make help` to see all available targets grouped by category. The most frequently used ones during development:

### Development

| Target | Description |
|--------|-------------|
| `make fmt` | Format Go source files |
| `make vet` | Run `go vet` on the codebase |
| `make manifests` | Generate CRDs, RBAC, and webhook manifests |
| `make generate` | Generate DeepCopy methods and other codegen |
| `make build` | Build the `manager` binary |
| `make test` | Run unit tests with coverage output |

### Build & Push

| Target | Description |
|--------|-------------|
| `make docker-build IMG=<image>` | Build the operator container image |
| `make docker-buildx IMG=<image>` | Build multi-platform images (linux/amd64, linux/arm64) |
| `make docker-push IMG=<image>` | Push the image to a registry |

### Deploy & Run

| Target | Description |
|--------|-------------|
| `make run` | Run the operator locally against your current kubeconfig |
| `make install` | Install CRDs into the cluster |
| `make uninstall` | Remove CRDs from the cluster |
| `make deploy IMG=<image>` | Deploy the operator to a cluster |
| `make undeploy` | Remove the operator from the cluster |

### Documentation

| Target | Description |
|--------|-------------|
| `make docs-preview` | Preview documentation locally at `http://localhost:4000/splunk-operator` |

## Development Workflow

A typical change follows this flow:

```shell
# 1. Create a feature branch from develop
git checkout -b feature/your-feature develop

# 2. Make your code changes
#    - API types:       api/v4/*.go
#    - Controllers:     internal/controller/*.go
#    - Business logic:  pkg/splunk/**/*.go

# 3. If you modified API types, regenerate manifests and code
make manifests generate

# 4. Format and vet
make fmt vet

# 5. Run unit tests
make test

# 6. Build
make build
```

## Deploying Locally

The `make deploy` command installs all necessary resources (RBAC, services, configmaps, deployment) into the `splunk-operator` namespace:

```shell
# Cluster-wide (watches all namespaces)
make deploy IMG=docker.io/splunk/splunk-operator:<tag>

# Namespace-scoped
make deploy IMG=docker.io/splunk/splunk-operator:<tag> WATCH_NAMESPACE="namespace1"

# With a specific Splunk Enterprise version
make deploy IMG=docker.io/splunk/splunk-operator:<tag> \
  WATCH_NAMESPACE="namespace1" \
  RELATED_IMAGE_SPLUNK_ENTERPRISE="splunk/splunk:edge"
```

Or run the operator as a local foreground process:

```shell
make run
```

This uses your current kubeconfig context (`~/.kube/config`).

## Environment Variables

Key variables used during development and testing:

| Variable | Default | Description |
|----------|---------|-------------|
| `NAMESPACE` | `splunk-operator` | Target namespace |
| `WATCH_NAMESPACE` | `""` (all) | Namespaces the operator watches |
| `SPLUNK_ENTERPRISE_IMAGE` | See `.env` | Splunk Enterprise image |
| `SPLUNK_GENERAL_TERMS` | `""` | Must be set to `--accept-sgt-current-at-splunk-com` |
| `LOG_LEVEL` | `info` | Operator log level (`debug`, `info`, `warn`, `error`) |
| `LOG_FORMAT` | `json` | Log format (`json` or `text`) |

## Debugging Tips

```shell
# Watch pods being reconciled
kubectl get pods -n splunk-operator -w

# Stream operator logs
kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager -f

# Describe a Custom Resource
kubectl describe <cr-type> <cr-name> -n <namespace>
```

### Common Issues

| Problem | Solution |
|---------|----------|
| CRD not found | Run `make install` to install CRDs |
| Permission errors | Check RBAC with `kubectl auth can-i --list` |
| Image pull errors | Verify the `IMG` variable and registry access |
