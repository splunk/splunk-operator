---
title: Architecture
parent: Develop & Contribute
nav_order: 3
---

# Architecture

## Repository Structure

```
├── api/
│   └── enterprise/   CRD Go types for current and compatibility API versions
├── cmd/              Main entry point for the operator
├── config/           Kubernetes manifests and configuration
│   ├── crd/          CRD base files
│   ├── samples/      Example CR manifests
│   ├── default/      Default kustomize configurations
│   └── rbac/         RBAC configurations
├── docs/             User-facing documentation
├── helm-chart/       Helm charts for operator and enterprise
├── internal/         Internal controller logic
├── kuttl/            KUTTL test scenarios
├── pkg/              Core business logic
│   └── splunk/
├── test/             Integration tests
│   ├── testenv/      Test environment utilities
│   └── */            Test suites by feature
└── tools/            Helper scripts and utilities
```

## Package Architecture

Domain logic under `pkg/splunk/` is organized by concern with a strict layered import direction. See [`pkg/splunk/README.md`](https://github.com/splunk/splunk-operator/blob/develop/pkg/splunk/README.md) for the full package layout, import rules, and guidance on where to put new code.
