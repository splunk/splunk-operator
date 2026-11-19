---
title: Architecture
parent: Develop & Contribute
nav_order: 3
---

# Architecture

## Repository Structure

```
├── api/              CRD Go types organized by API group and version
│   ├── enterprise/   Splunk Enterprise APIs (current stable: v4)
│   └── platform/     Platform-managed service APIs (PostgreSQL: v1alpha1)
├── cmd/              Main entry point for the operator
├── config/           Kubernetes manifests and configuration
│   ├── crd/          CRD base files
│   ├── samples/      Example CR manifests
│   ├── default/      Default kustomize configurations
│   └── rbac/         RBAC configurations
├── docs/             User-facing documentation
├── helm-chart/       Helm charts for operator and enterprise
├── internal/         Internal controller logic organized by API group
│   └── controller/
│       ├── enterprise/  Splunk Enterprise reconcilers
│       └── platform/    Platform service reconcilers (PostgreSQL)
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

## KVService reconciliation

For installation, configuration and recovery procedures, see the
[KVService operating guide](../operate/KVService.md). Implementation details are
split between `internal/controller/enterprise/kvservice_controller.go` and the
owning packages under `pkg/splunk/`:

- `reconcile/kvservice` coordinates discovery, retained database provisioning,
  keyring publication and serving resources. Namespace metadata is the discovery
  input; generated runtime membership is an output. The shared address parser
  supplies the same backend identity to client configuration and Secret routing.
- `resources` constructs workloads and database requests; `k8sops` reads and
  applies Kubernetes dependencies. `CommonWorkloadSpec` supplies the reusable
  workload API fields without Enterprise storage, cluster or license references.
- Startup depends on `kvservicesystem`. Per-tenant dependency failures are recorded
  separately and excluded from eligible membership, so healthy tenants continue.
  Tenant status describes prerequisites, not runtime schema completion. Readiness
  events are emitted on transitions rather than on each retry.

Tenant evaluation returns an explicit status and retry decision. Discovery and keyring
publication return tenant-local failures as data; operation errors remain errors.
Tenant readiness is Unknown when a pass cannot evaluate tenants, and transition
events are emitted only after the status update succeeds.

### Dependency revisions

Tenant membership and keyring revisions deliberately change the pod template.
Password and keyring revisions use Secret identity, key name, UID and
resourceVersion, not credential values or hashes of those values. Consequently,
metadata or unrelated-key updates can also roll pods. Selected non-secret
connection ConfigMap fields use content hashes. An unchanged dependency snapshot
leaves the template unchanged.

The controller also routes changes to user-provided ConfigMaps referenced by
direct or projected volumes. Their content revisions participate in rollout,
subject to the shared ConfigMap restart opt-out annotation. Namespace peer
CREATE/DELETE events requeue singleton checks; status-only peer updates do not.
Configuration resolution errors prevent applying an incomplete Deployment.

### Probe and certificate construction

Runtime probe construction supplies defaults for omitted or zero timings. The
CRD defaults timeout, period and failure threshold for omitted probe objects;
the controller supplies the initial-delay fallback so the separate delay fields
remain effective. Probe paths and success threshold are fixed.

Certificate configuration uses the shared certificate workflow. `spec.scheme`
selects HTTP (the default) or HTTPS independently of certificate entries. HTTP
ignores `spec.certs` and logs a warning on each reconcile; HTTPS requires
certificates and the CertManagement feature gate. The first `role: server` entry
is selected, or the first entry if no server role exists. The controller passes
the scheme explicitly to `kvstore-server` and uses it for both probes.
Certificate mounts must have distinct paths, and each role may select only one
distinct Secret; role-free mounts use `/mnt/tls/<secret-name>/`. See
[certificate operations](../operate/Certificates.md) for the shared mounting,
issuance and rotation contract.

Client protocol diagnostics compare selected namespace URLs with the configured
listener independently of tenant provisioning. `ClientConfigurationReady` is
Unknown until discovery runs, False for a mismatch and True when all selected
namespace protocols match. Transition events follow a successful status write.
Certificate preflight checks the serving PEM pair, validity, server usage and
short Service DNS before either serving resource changes. It does not configure
client trust or provide an atomic protocol cutover across rolling clients/pods.
