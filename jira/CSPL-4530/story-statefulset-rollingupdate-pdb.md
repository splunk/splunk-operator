# StatefulSet RollingUpdate Strategy and PodDisruptionBudget Management

**Type:** Story
**EPIC:** Kubernetes-Native Pod Lifecycle Management for Splunk Operator
**Component:** `pkg/splunk/enterprise/configuration.go`, `pkg/splunk/enterprise/util.go`, `pkg/splunk/enterprise/indexercluster.go`, `pkg/splunk/enterprise/ingestorcluster.go`

---

## Background

StatefulSets in the current operator are configured with `updateStrategy: OnDelete`. This means Kubernetes never updates pods on its own — the operator must manually delete each pod to force an update. The operator therefore re-implements rolling update logic (iterate pods, call decommission, delete pod, wait for ready, repeat) that is built into Kubernetes and available for free through `RollingUpdate` strategy.

PodDisruptionBudgets (PDBs) do not exist for any Splunk cluster type. This leaves clusters unprotected during node drains, cluster upgrades, and any other voluntary disruption source outside the operator's control.

---

## What Needs to Be Built

### 1. Change StatefulSet `updateStrategy` to `RollingUpdate`

For every StatefulSet created by the operator (IndexerCluster, SearchHeadCluster, IngestorCluster), set:

```yaml
spec:
  updateStrategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 1       # default: one pod at a time
      partition: 0            # default: update all pods
```

The default of `maxUnavailable: 1` ensures serial pod updates — the same behaviour as the current manual loop — without any operator involvement. Users can override both values through the `rollingUpdateConfig` field on the CR spec (see acceptance criteria below).

### 2. Implement `buildUpdateStrategy` in `configuration.go`

A function `buildUpdateStrategy(spec *CommonSplunkSpec, replicas int32) appsv1.StatefulSetUpdateStrategy` that:

- Returns `RollingUpdate` with `maxUnavailable: 1` by default.
- If `spec.RollingUpdateConfig` is set:
  - Parses `MaxPodsUnavailable` as either an integer string (`"2"`) or a percentage string (`"25%"`) and sets `strategy.RollingUpdate.MaxUnavailable` as `intstr.Int` or `intstr.String` accordingly. Rejects invalid values.
  - If `Partition` is set and is in the range `[0, replicas]`, sets `strategy.RollingUpdate.Partition`. If partition equals `replicas`, no pods are updated (full canary hold). Rejects values outside this range.

### 3. Implement `ApplyPodDisruptionBudget` in `util.go`

A function `ApplyPodDisruptionBudget(ctx, client, cr, instanceType, replicas)` that creates or updates a PodDisruptionBudget for a given cluster resource. Requirements:

- **`minAvailable` formula:** `replicas - 1`. This allows exactly one pod to be unavailable at any time, whether from the StatefulSet rolling update, a node drain, or any other voluntary disruption.
- **Single-replica edge case:** If `replicas <= 1`, set `minAvailable = 0`. A PDB with `minAvailable: 1` on a single-pod deployment would block all evictions, including rolling updates and node maintenance.
- **Pod selector:** Must match the labels assigned to the StatefulSet pods for that instance type. For an IndexerCluster, the label set includes the `part-of` label derived from `ClusterManagerRef.Name` (or `ClusterMasterRef.Name` for v3 compatibility). Using incorrect labels results in a PDB that matches no pods, giving false safety.
- **Ownership:** Set the CR as owner via `SetOwnerReferences` so the PDB is garbage-collected when the CR is deleted.
- **Create-or-update:** If the PDB does not exist, create it. If it exists with a different `minAvailable` (e.g., after a replica count change), patch it.

### 4. Call `ApplyPodDisruptionBudget` from each cluster reconciler

`ApplyPodDisruptionBudget` must be called during the reconcile loop for:
- `IndexerCluster` (both standalone and with ClusterManager)
- `IngestorCluster`
- `SearchHeadCluster`

It must be called with the current desired replica count, not the current observed replica count, so that the PDB is updated before the StatefulSet replica count changes.

### 5. Detect and report RollingUpdate stall in `statefulset.go`

When the StatefulSet strategy is `RollingUpdate`, the controller must not attempt to manually delete or evict pods that are part of the rolling update. Instead, it must detect when an update is in progress and log/event appropriately. Additionally:

- If a RollingUpdate has been in progress for longer than a configurable threshold and no pods are transitioning, surface a warning event on the CR indicating the update may be stalled (e.g., a preStop hook is hanging or a PDB is blocking all eviction).

---

## API Changes

### `CommonSplunkSpec` (api/v4/common_types.go)

```go
// RollingUpdateConfig controls how StatefulSet rolling updates behave.
// If unset, updates proceed one pod at a time (maxUnavailable: 1, partition: 0).
// +optional
RollingUpdateConfig *RollingUpdateConfig `json:"rollingUpdateConfig,omitempty"`
```

```go
type RollingUpdateConfig struct {
    // MaxPodsUnavailable is the maximum number of pods that can be unavailable
    // during the update. Accepts an integer ("2") or a percentage ("25%").
    // Defaults to 1 if not set.
    // +optional
    MaxPodsUnavailable string `json:"maxPodsUnavailable,omitempty"`

    // Partition controls canary deployments. Only pods with ordinal >= Partition
    // are updated when the pod template changes. Pods with ordinal < Partition
    // are not updated, even if deleted and recreated.
    // Must be in [0, replicas]. Defaults to 0 (update all pods).
    // +optional
    Partition *int32 `json:"partition,omitempty"`
}
```

---

## Acceptance Criteria

1. All StatefulSets created by the operator for IndexerCluster, SearchHeadCluster, and IngestorCluster have `updateStrategy: RollingUpdate` with `maxUnavailable: 1` and `partition: 0` by default.
2. When `spec.rollingUpdateConfig.maxPodsUnavailable: "25%"` is set on a 12-replica IndexerCluster, the StatefulSet has `maxUnavailable: "25%"` and Kubernetes allows up to 3 pods to be updated simultaneously.
3. When `spec.rollingUpdateConfig.partition: 8` is set on a 10-replica IndexerCluster, updating the pod template updates only pods `8` and `9`; pods `0`-`7` remain at the previous template version.
4. A PodDisruptionBudget exists for every IndexerCluster, SearchHeadCluster, and IngestorCluster. Its `minAvailable` equals `replicas - 1`.
5. When a 3-replica IndexerCluster is scaled to 4 replicas, the PDB's `minAvailable` is updated from `2` to `3` before the StatefulSet replica count changes.
6. When a 1-replica IngestorCluster has a PDB, the PDB's `minAvailable` is `0`, not `1`, so that the single pod can be evicted for updates.
7. The PDB's pod selector correctly matches the pods of the corresponding StatefulSet. For an IndexerCluster with `clusterManagerRef.name: cm-1`, the PDB selector includes the `part-of: cm-1` label.
8. When the CR is deleted, the PDB is also deleted (owner reference garbage collection).
9. If a RollingUpdate is detected to be in progress, the operator does not attempt to manually delete or evict any pod that belongs to that StatefulSet.
10. The `buildUpdateStrategy` function rejects a `MaxPodsUnavailable` value that is neither a valid integer string nor a percentage string and falls back to the default.

---

## Definition of Done

- [ ] `buildUpdateStrategy` implemented and called from `getSplunkStatefulset`.
- [ ] `ApplyPodDisruptionBudget` implemented in `util.go`.
- [ ] `ApplyPodDisruptionBudget` called from IndexerCluster, IngestorCluster, and SearchHeadCluster reconcilers.
- [ ] CRD YAML for `CommonSplunkSpec` updated with `rollingUpdateConfig` field and validation.
- [ ] Existing StatefulSet fixture JSON files in `testdata/fixtures/` updated to reflect `RollingUpdate` strategy.
- [ ] Unit tests cover: default strategy, percentage MaxPodsUnavailable, integer MaxPodsUnavailable, partition, PDB creation, PDB update on replica change, PDB deletion on CR deletion, single-replica PDB edge case, PDB selector correctness.
