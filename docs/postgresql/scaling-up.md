---
title: Vertical Scaling
parent: PostgreSQL
nav_order: 6
---

# PostgresCluster Vertical Scaling

This document describes how to change CPU and memory resources, and how to expand storage, for a `PostgresCluster`. It covers what the operator does during each operation, what level of interruption to expect, and how to track progress.

Adding or removing instances is not covered here — see [Scaling](scaling-out.md).

### How vertical scaling works

Resource allocations (CPU, memory) and storage size are driven by the effective values for the `PostgresCluster`.

Each can be defined in either field:

- `PostgresClusterClass.spec.config.resources` / `PostgresClusterClass.spec.config.storage`
- `PostgresCluster.spec.resources` / `PostgresCluster.spec.storage`

If both are set, the `PostgresCluster` value takes effect for that cluster. Removing a `spec.resources` override returns the cluster to the class default on the next reconcile. `spec.storage` cannot be removed once set — the admission webhook rejects the update (see [Validation rules](#validation-rules)).

When resources or storage change:

- the operator patches the managed CNPG `Cluster` spec
- CNPG applies the change — CPU/memory triggers a rolling restart; storage triggers PVC expansion
- `PostgresCluster.status.phase` moves to `Provisioning` while the change is in progress
- the cluster returns to `Ready` once CNPG is healthy and all PVCs have finished resizing

### CPU and memory changes

CNPG applies resource updates by restarting instances. The method depends on `primaryUpdateMethod` in the class:

| `primaryUpdateMethod` | What happens | Write interruption |
| --------------------- | ------------ | ------------------ |
| `restart` (default)   | CNPG restarts the primary in place | Brief downtime on the RW endpoint during primary restart |
| `switchover`          | CNPG promotes a replica before restarting the old primary | Minimal — a switchover is performed first; requires `instances >= 2` |

CNPG transitions through `PhaseInplacePrimaryRestart`, `PhaseApplyingConfiguration`, or `PhaseSwitchover` during the update. The operator reflects these as `status.phase=Configuring` while the restart is running, then `Provisioning` if CNPG reports `Healthy` but the patch was just applied, and finally `Ready` once convergence is confirmed.

### Storage expansion

Storage changes are **one-directional**: `spec.storage` can only be increased. The admission webhook rejects any attempt to decrease it.

CNPG expands the underlying PVCs in place using the Kubernetes volume expansion API. No pod restarts are required in most cases, though the kubelet may need to resize the filesystem while the pod is running. Progress is tracked via `cnpgCluster.status.resizingPVC` — the operator holds `status.phase=Provisioning` until that list is empty.

Storage expansion requires the `StorageClass` to have `allowVolumeExpansion: true`. If the class does not support expansion, the PVC resize will remain pending indefinitely.

### Status during vertical scaling

| Field | Notes |
| ----- | ----- |
| `status.phase` | `Provisioning` while a resource patch was just applied and CNPG has not yet started reacting, or while PVCs are still resizing. `Configuring` while CNPG is actively restarting. `Ready` when fully converged. |
| `status.currentPrimary` | Changes when a switchover occurs during resource update with `primaryUpdateMethod: switchover`. |

### Procedure

1. Confirm the cluster is `Ready`.
2. Update `spec.resources` or `spec.storage` in the tracked manifest.
3. Review and commit the manifest change.
4. Apply the manifest with `kubectl apply`.
5. Watch progress until `status.phase=Ready`.

Example — increase CPU and memory:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: PostgresCluster
metadata:
  name: postgresql-cluster-prod
spec:
  class: postgresql-prod
  resources:
    requests:
      cpu: "500m"
      memory: "1Gi"
    limits:
      cpu: "2"
      memory: "2Gi"
```

Example — expand storage:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: PostgresCluster
metadata:
  name: postgresql-cluster-prod
spec:
  class: postgresql-prod
  storage: 50Gi   # was 20Gi; can only increase
```

```bash
kubectl apply -n $NS -f path/to/postgrescluster.yaml
kubectl get postgrescluster -n $NS $CLUSTER -w
kubectl get cluster -n $NS $CLUSTER -w
kubectl get events -n $NS --sort-by=.lastTimestamp
```

For storage expansions, also watch the PVCs:

```bash
kubectl get pvc -n $NS -w
```

### Validation rules

| Rule | When | What it does |
| ---- | ---- | ------------ |
| Storage cannot decrease | UPDATE | The CEL rule on `spec.storage` rejects any update that lowers the value below the previously-set size. |
| Storage cannot be removed | UPDATE | Once `spec.storage` is set it cannot be cleared — the CEL rule requires the field to remain present. |

### What to monitor

At the `PostgresCluster` level, expect:

- `status.phase=Provisioning` or `Configuring` during the change
- `status.phase=Ready` after it completes

Common events include:

- `ClusterUpdateStarted` — operator patched the CNPG cluster spec
- `ClusterReady` — cluster returned to a healthy state

You may also see additional namespace events from CNPG and Kubernetes:

- CNPG lifecycle events such as `InplacePrimaryRestart`, `Switchover`, `ApplyingConfiguration`
- Kubernetes PVC events such as `Resizing`, `FileSystemResizeSuccessful`
