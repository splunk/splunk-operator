# pkg/splunk — Package Architecture

## Overview

Domain logic is organized **by concern** with a strict layered import
direction. The `enterprise/` package is legacy — it is being incrementally
decomposed into the packages described below.

```
pkg/splunk/
├── client/           1:1 external-API wrappers (Splunk REST, storage SDKs)
├── common/           Types, interfaces, constants — no business logic
├── enterprise/       LEGACY — shrinks over time, eventually deleted
├── reconcile/        Per-CR orchestration (one sub-package per CRD)
├── workflow/         Multi-step, CR-agnostic state-change workflows
├── resources/        K8s object builders — pure functions, no I/O
├── k8sops/           K8s API read/apply/diff/merge
├── validation/       Admission webhooks and CR spec validation
├── test/             Shared test helpers
└── util/             Stateless helpers — no CRD type imports
```

## Import Rules

Imports flow **downward only**. A package may import packages below it in this
table but never above it. Packages at the same level (e.g. two `reconcile/`
sub-packages) must never import each other.

```
 ┌─────────────────┐
 │  reconcile/<cr> │  Reads CR, builds objects, applies, delegates workflows
 └──┬───┬───┬───┬──┘
    │   │   │   │
    │   │   │   ▼
    │   │   │  workflow/<domain>   Multi-step stateful operations
    │   │   │   │
    │   │   ▼   ▼
    │   │  client/<system>        External API wrappers
    │   │
    │   ▼
    │  k8sops/                    K8s API CRUD, diff/merge, finalizers
    │   │
    ▼   ▼
   resources/                     K8s object construction (no I/O)
    │
    ▼
   util/                          Stateless helpers
    │
    ▼
   common/                        Types, interfaces, constants
```

All packages may also import `api/enterprise/v4` (CRD types).

| Package | Purpose | Allowed imports from `pkg/splunk/` |
|---|---|---|
| `common/` | Types, interfaces, constants | _(none)_ |
| `util/` | Stateless helpers, naming, events | `common/` |
| `client/<system>/` | 1:1 external API wrappers | `common/`, `util/` |
| `resources/` | K8s object builders (pure functions) | `common/`, `util/` |
| `k8sops/` | K8s API CRUD, diff/merge, finalizers | `common/`, `util/`, `resources/` |
| `workflow/<domain>/` | Multi-step stateful workflows | `common/`, `util/`, `client/` |
| `reconcile/<cr>/` | Per-CR orchestration loop | `common/`, `util/`, `resources/`, `k8sops/`, `client/`, `workflow/` |

## Package Details

### `reconcile/<cr>/`

One sub-package per Custom Resource type. Each owns a thin orchestration loop:

1. Read the CR and current cluster state
2. Build desired K8s objects via `resources/`
3. Apply them via `k8sops/`
4. Delegate multi-step operations to `workflow/<domain>/`
5. Write status and decide requeue

Sub-packages: `clustermanager/`, `indexercluster/`, `ingestorcluster/`,
`licensemanager/`, `monitoringconsole/`, `searchheadcluster/`, `standalone/`

### `workflow/<domain>/`

Multi-step, stateful operations that are consumed by one or more `reconcile/`
packages. These are CR-agnostic — they operate on domain concepts, not specific
CRD types.

| Sub-package | Scope |
|---|---|
| `appframework/` | Bundle discovery, staging, scheduling, push |
| `bootstrap/` | First-time init, admin secret seeding |
| `indexercluster/` | Peer decommission, rebalance wait, scale-down |
| `shc/` | Captain election, member join/drain |
| `telapp/` | SOK usage-tracking app install |
| `upgrade/` | Rolling upgrade sequencing, version gating |

### `resources/`

Pure-function builders for Kubernetes objects (StatefulSets, Services,
ConfigMaps, PVCs, volumes, probes, labels, env vars). Takes specs as input,
returns constructed objects. No `client.Create/Update`, no external API calls.

### `k8sops/`

Kubernetes API layer — read, create-or-update with diff/merge, and finalizer
execution. Renamed from `splkcontroller/` to reflect its actual scope and avoid
confusion with `reconcile/<cr>/`.

### `client/<system>/`

1:1 wrappers around external APIs. Takes inputs, makes the call, returns the
parsed result. No scheduling, no multi-step logic, no K8s client I/O.

| Sub-package | Scope |
|---|---|
| `splunk/` | Splunk REST API (cluster, searchhead, indexer, license) |
| `storage/{aws,azure,gcp,minio}/` | Object storage SDKs |
| `queue/` | Queue / pub-sub (future) |

### `validation/`

Admission webhooks and CR spec validation.

### `enterprise/` (LEGACY)

The original monolithic package. Code migrates out incrementally — App Framework
files remain here until the App CR fully replaces them, at which point the
package is deleted.

## Where to Put New Code

| I'm writing... | Target package |
|---|---|
| A new CRD reconciler | `reconcile/<cr>/` |
| A multi-step operation (upgrade, decommission, etc.) | `workflow/<domain>/` |
| A K8s object builder (StatefulSet, Service, etc.) | `resources/` |
| A Splunk REST API call | `client/splunk/` |
| An object-storage SDK call | `client/storage/<provider>/` |
| An admission webhook | `validation/` |
| A change to existing enterprise logic | `enterprise/` (for now — migrate in Phase 2) |

## Documentation Convention

Every package has a `doc.go` file documenting its purpose and allowed imports.
Run `go doc ./pkg/splunk/<package>` to see it.
