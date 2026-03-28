# Engineering Requirements Document
# Kubernetes-Native Pod Lifecycle Management for Splunk Operator

**Document type:** Engineering Requirements Document (ERD)
**EPIC:** Kubernetes-Native Pod Lifecycle Management for Splunk Operator
**Status:** Draft

---

## 1. Purpose

This document specifies the functional and non-functional requirements for redesigning how Splunk Operator manages pod lifecycle during scale, configuration change, and restart operations. It is the authoritative source of requirements that all implementation stories in this EPIC must satisfy.

---

## 2. Problem Statement

### 2.1 Blocking reconcile loops

The current reconcile loop calls Splunk decommission or detention REST APIs from the controller goroutine, waits for the operation to complete, deletes the pod, and waits for the replacement pod to be ready before advancing to the next pod. For a cluster requiring 15 minutes of bucket migration per indexer pod, a 100-pod cluster serialises to over 25 hours of blocked reconciliation. No other resource in the operator's watch scope is reconciled during this time.

### 2.2 OnDelete update strategy

StatefulSets are configured with `updateStrategy: OnDelete`. The StatefulSet controller never replaces pods on its own. The operator re-implements rolling update logic — iterate pods, decommission, delete, wait for ready, repeat — that Kubernetes already provides through the built-in `RollingUpdate` strategy. The operator's reimplementation has no back-off, no awareness of node conditions, and no crash recovery.

### 2.3 No PodDisruptionBudgets

No PodDisruptionBudget exists for any cluster type. During a node drain, cluster upgrade, or any other voluntary disruption source outside the operator's control, all pods of a cluster can be evicted simultaneously. There is no floor on how many pods must remain available.

### 2.4 No differentiation between restart and scale-down

Every pod deletion — whether from a configuration change that simply requires a process restart, or from a permanent scale-down — is handled identically: full decommission with `enforce_counts=1`, triggering complete bucket migration. A pod restart that could complete in under two minutes instead triggers the same 15-minute bucket migration as a permanent removal.

### 2.5 No crash recovery for in-progress updates

If the operator crashes mid-rolling-update, it has no record of which pods were already updated. The cluster is left in a mixed-version state with no mechanism for automatic recovery. The operator re-runs the entire update on restart.

### 2.6 No intent propagation to pods

A pod being terminated has no way to know whether it is being restarted or permanently removed. Environment variables are resolved once at pod start and cannot be updated while the pod is running. The operator has no channel to communicate intent to the preStop hook.

---

## 3. Goals

1. Replace operator-managed pod deletion with Kubernetes StatefulSet `RollingUpdate` strategy so that the operator does not block on pod lifecycle operations.
2. Move all Splunk-specific graceful shutdown logic (decommission, detention, graceful stop) into a `preStop` lifecycle hook running inside the pod.
3. Enforce availability guarantees during voluntary disruption using PodDisruptionBudgets.
4. Differentiate restart from scale-down so that restart triggers fast decommission (`enforce_counts=0`) and scale-down triggers full bucket migration (`enforce_counts=1`).
5. Enable per-pod restart detection for configuration changes that do not alter the pod template spec (secret rotation, Splunk-internal restart signals).
6. Ensure post-termination cleanup (cluster manager peer removal, PVC deletion) runs reliably after scale-down without blocking the pod object from being garbage-collected.

## 4. Non-Goals

- SearchHeadCluster scale-out (adding members is managed separately by the SHC deployer).
- Multi-site cluster topology changes.
- AppFramework app deployment.
- Monitoring Console configuration.
- IngestorCluster, Queue, or ObjectStorage CRD definitions (already merged to the develop branch; this EPIC adds lifecycle management on top of those existing types).

---

## 5. Functional Requirements

### 5.1 StatefulSet Update Strategy

**REQ-STS-01:** All StatefulSets created by the operator for IndexerCluster, SearchHeadCluster, and IngestorCluster must use `updateStrategy: RollingUpdate`.

**REQ-STS-02:** The default `maxUnavailable` for all StatefulSets must be `1`, producing serial pod-by-pod updates equivalent to the current manual loop.

**REQ-STS-03:** The operator must expose a `rollingUpdateConfig` field on `CommonSplunkSpec` that allows users to override `maxUnavailable` (as an integer or percentage string) and `partition` (as an integer in `[0, replicas]`).

**REQ-STS-04:** When a `RollingUpdate` is in progress (`statefulSet.Status.UpdatedReplicas < statefulSet.Status.Replicas`), the operator must not attempt to manually delete, evict, or restart any pod belonging to that StatefulSet.

**REQ-STS-05:** If a `RollingUpdate` has been in progress with no pod transitions for longer than a configurable stall threshold, the operator must surface a warning event on the CR.

### 5.2 PodDisruptionBudgets

**REQ-PDB-01:** A PodDisruptionBudget must be created for every IndexerCluster, IngestorCluster, and SearchHeadCluster. The PDB must be owned by the CR via `ownerReferences` and garbage-collected when the CR is deleted.

**REQ-PDB-02:** The PDB `minAvailable` must equal `replicas - 1`, allowing exactly one pod to be unavailable at any time from any source of voluntary disruption.

**REQ-PDB-03:** When `replicas <= 1`, `minAvailable` must be `0`. A PDB with `minAvailable: 1` on a single-pod deployment blocks all evictions, including rolling updates.

**REQ-PDB-04:** The PDB pod selector must exactly match the labels on the StatefulSet pods. For IndexerCluster, this includes the `part-of` label derived from `clusterManagerRef.name` (or `clusterMasterRef.name` for v3 compatibility).

**REQ-PDB-05:** When the desired replica count changes, the PDB `minAvailable` must be updated before the StatefulSet replica count is patched.

### 5.3 Pod Intent Annotation and Downward API Volume

**REQ-INTENT-01:** Every pod created from an IndexerCluster or SearchHeadCluster StatefulSet must carry the annotation `splunk.com/pod-intent: serve` by default.

**REQ-INTENT-02:** The annotation value must be exposed to the pod's container filesystem via a Downward API volume mounted at `/etc/podinfo/intent`. Environment variables must not be used for this purpose — env vars are frozen at pod start and do not reflect updates made while the pod is running.

**REQ-INTENT-03:** Before reducing the StatefulSet replica count, the operator must set `splunk.com/pod-intent: scale-down` on the pods that will be removed (those with ordinals in `[newReplicas, currentReplicas)`). This annotation must be set before the StatefulSet replica count is patched.

**REQ-INTENT-04:** If the annotation update fails (e.g., the pod no longer exists), the operator must not fail the reconcile. The finalizer handler has an ordinal-based fallback for detecting scale-down.

**REQ-INTENT-05:** Configuration changes (image update, env var change) must not modify the `splunk.com/pod-intent` annotation. It must remain `serve` unless the operator explicitly sets it to `scale-down`.

**REQ-INTENT-06:** All annotation and finalizer name strings must be defined as exported constants. No bare string literals for `splunk.com/pod-cleanup` or `splunk.com/pod-intent` anywhere in the codebase.

### 5.4 preStop Lifecycle Hook

**REQ-PRESTOP-01:** Every StatefulSet-managed pod must have a `preStop` exec hook pointing to `tools/k8_probes/preStop.sh`.

**REQ-PRESTOP-02:** The script must read the pod intent from `/etc/podinfo/intent`. If the file is absent or empty, the script must default to `serve`.

**REQ-PRESTOP-03:** For `splunk_indexer` pods, the script must call the decommission API with `enforce_counts=1` when intent is `scale-down`, and `enforce_counts=0` when intent is `serve` or `restart`.

**REQ-PRESTOP-04:** For `splunk_search_head` pods, the script must call the SHC member removal API and poll until `is_registered` is false or the decommission budget is exhausted.

**REQ-PRESTOP-05:** For all other roles (`splunk_standalone`, `splunk_ingestor`, `splunk_cluster_manager`, `splunk_license_manager`, `splunk_deployer`, `splunk_monitoring_console`), the script must call only `splunk stop`.

**REQ-PRESTOP-06:** The Splunk admin password must be read from the mounted secret file at the path in `SPLUNK_PASSWORD_FILE`. It must never be passed as an environment variable.

**REQ-PRESTOP-07:** All Splunk management API calls must target `https://localhost:${MGMT_PORT}`. No service discovery and no external DNS resolution.

**REQ-PRESTOP-08:** The total script execution time must not exceed `terminationGracePeriodSeconds`. The timeout budget must be split dynamically: `TOTAL_BUDGET = DECOMMISSION_MAX_WAIT + STOP_MAX_WAIT`, where both values are computed from environment variable overrides (`PRESTOP_DECOMMISSION_WAIT`, `PRESTOP_STOP_WAIT`) before `TOTAL_BUDGET` is calculated. A start timestamp must be recorded at script entry; the `splunk stop` timeout must be clamped to the remaining budget.

**REQ-PRESTOP-09:** Default timeout allocations by role:

| Role | `terminationGracePeriodSeconds` | `DECOMMISSION_MAX_WAIT` | `STOP_MAX_WAIT` |
|---|---|---|---|
| `splunk_indexer` | 1020s | 900s | 90s |
| `splunk_search_head` | 360s | 300s | 50s |
| All others | 120s | 80s | 30s |

**REQ-PRESTOP-10:** If the decommission phase exhausts its budget, the script must log an error and proceed to `splunk stop` without hanging. If the remaining budget is zero or negative when `splunk stop` is reached, a minimal 5-second timeout must be used and a warning logged.

**REQ-PRESTOP-11:** Every log line must include a timestamp. Error output must go to stderr.

### 5.5 Pod Cleanup Finalizer

**REQ-FINALIZER-01:** Every pod created from an IndexerCluster or SearchHeadCluster StatefulSet must carry the finalizer `splunk.com/pod-cleanup` from birth (set in the pod template).

**REQ-FINALIZER-02:** A `PodReconciler` controller must watch pods that carry this finalizer and have a non-nil `deletionTimestamp`. It must not modify pods without the finalizer, and must not reconcile pods that are not being deleted.

**REQ-FINALIZER-03:** The finalizer handler must determine whether the pod deletion is a scale-down or a restart. Primary signal: `splunk.com/pod-intent: scale-down` annotation. Fallback: pod ordinal >= `statefulSet.Spec.Replicas`.

**REQ-FINALIZER-04:** For indexer scale-down: the handler must call `remove_peers` on the cluster manager to remove the peer GUID from the cluster manager's registry, then delete the pod's PVCs (`pvc-etc-<podName>` and `pvc-var-<podName>`).

**REQ-FINALIZER-05:** For indexer restart: the handler must verify decommission status only. It must not call `remove_peers` and must not delete PVCs. The pod must rejoin the cluster with its existing PVCs.

**REQ-FINALIZER-06:** For search head scale-down: the handler must delete the pod's PVCs. For search head restart: no action is required beyond removing the finalizer.

**REQ-FINALIZER-07:** Failure to call `remove_peers` (e.g., cluster manager temporarily unreachable) must be logged but must not block finalizer removal. A pod must never be stuck in Terminating state indefinitely due to cluster manager unavailability.

**REQ-FINALIZER-08:** If a PVC is already deleted before the handler runs, the 404 must be handled gracefully. The finalizer must still be removed.

**REQ-FINALIZER-09:** The `PodReconciler` must respect the operator's `WATCH_NAMESPACE` configuration. If set to a specific namespace, it must not reconcile pods in other namespaces.

**REQ-FINALIZER-10:** The `RemoveIndexerClusterPeer` Splunk client method must call `POST /services/cluster/manager/control/control/remove_peers?peers={GUID}` and return nil on HTTP 200.

### 5.6 Per-Pod restart_required Detection and Eviction

**REQ-EVICT-01:** A `CheckRestartRequired` method on `SplunkClient` must call `GET /services/messages/restart_required?output_mode=json`. It must return `(true, message, nil)` when the entry exists (HTTP 200 with entries), `(false, "", nil)` when no entry exists or the endpoint returns 404, and `(false, "", err)` on any other error.

**REQ-EVICT-02:** The IngestorCluster reconciler must track secret version changes. When the ResourceVersion of the Queue or ObjectStorage credential secret changes, all pods must be marked for restart. The current version must be stored in `cr.Status.QueueBucketAccessSecretVersion`.

**REQ-EVICT-03:** When IRSA is configured (no credential secret exists), `QueueBucketAccessSecretVersion` must be set to the sentinel value `"irsa-config-applied"`. Subsequent reconciles must not trigger a restart due to this sentinel value.

**REQ-EVICT-04:** `checkPodsRestartRequired` must iterate all Running and Ready pods, call `CheckRestartRequired` on each, skip pods where the call returns an error (log and continue), and return the list of pods that need restart.

**REQ-EVICT-05:** Pod eviction must use the Kubernetes Eviction API (`policy/v1.Eviction`) via `client.SubResource("eviction").Create(...)`. Direct pod deletion bypasses PDB enforcement and must not be used.

**REQ-EVICT-06:** Exactly one pod must be evicted per reconcile cycle. The operator must not evict multiple pods in a single reconcile, regardless of how many pods report restart required.

**REQ-EVICT-07:** When the Eviction API returns HTTP 429 (PDB violation), the eviction must not be retried in the same reconcile cycle. The reconciler must log the block and return, allowing the next scheduled requeue to retry.

**REQ-EVICT-08:** Per-pod eviction must be skipped entirely when a StatefulSet RollingUpdate is already in progress. Eviction during a rolling update conflicts with the StatefulSet controller's own pod management.

**REQ-EVICT-09:** After evicting a pod, `cr.Status.RestartStatus` must be updated to reflect: `Phase: InProgress`, current progress counts (`TotalPods`, `PodsNeedingRestart`, `PodsRestarted`), and `LastRestartTime`. When no pods report restart required, `Phase` must be set to `Completed` or cleared.

**REQ-EVICT-10:** Both the IngestorCluster and the Standalone reconcilers must implement the `restart_required` detection and per-pod eviction path.

### 5.7 RBAC

**REQ-RBAC-01:** The operator's ClusterRole must include `create`, `delete`, `get`, `list`, `patch`, `update`, `watch` on `policy/poddisruptionbudgets`.

**REQ-RBAC-02:** The operator's ClusterRole must include `create` on `policy/pods/eviction`. This must be under the `policy` API group, not the core (`""`) API group.

**REQ-RBAC-03:** The operator's ClusterRole must include `get`, `list`, `watch`, `update`, `patch` on core `pods` (for annotation updates and finalizer removal).

**REQ-RBAC-04:** The operator's ClusterRole must include `get`, `list`, `delete` on core `persistentvolumeclaims` (for PVC cleanup in the finalizer handler).

**REQ-RBAC-05:** The operator's ClusterRole must include `get`, `list`, `watch` on core `secrets` (for credential reading in the finalizer handler and restart detection).

**REQ-RBAC-06:** The Helm chart ClusterRole template (`helm-chart/splunk-operator/templates/rbac/clusterrole.yaml`) must be kept in sync with `config/rbac/role.yaml`. The permissions granted by a Helm-deployed operator must be identical to those granted by a kustomize-deployed operator.

### 5.8 CRD Schema Updates

**REQ-CRD-01:** `CommonSplunkSpec` must gain a `rollingUpdateConfig` field of type `*RollingUpdateConfig`, optional, with appropriate OpenAPI schema validation.

**REQ-CRD-02:** `RollingUpdateConfig` must expose `maxPodsUnavailable: string` (accepts integer or percentage) and `partition: *int32`.

**REQ-CRD-03:** IndexerCluster, IngestorCluster, and SearchHeadCluster status must include a `restartStatus` field of type `RestartStatus`.

**REQ-CRD-04:** `RestartStatus` must include: `phase` (enum: `""`, `"Pending"`, `"InProgress"`, `"Completed"`, `"Failed"`), `message: string`, `totalPods: int32`, `podsNeedingRestart: int32`, `podsRestarted: int32`, `lastCheckTime: *metav1.Time`, `lastRestartTime: *metav1.Time`.

**REQ-CRD-05:** All CRD YAML files that embed `CommonSplunkSpec` must be regenerated via `make generate manifests` and committed. Schema changes must be backwards-compatible — existing clusters must be able to apply the updated CRDs without data migration.

---

## 6. Non-Functional Requirements

**REQ-NFR-01 — Operator availability:** The operator reconcile loop must not block on any pod lifecycle operation. All Splunk API calls during pod termination must run inside the pod's preStop hook, not in the controller goroutine.

**REQ-NFR-02 — Crash recovery:** If the operator crashes mid-rolling-update, the StatefulSet controller must continue the update independently without operator involvement. No operator-side state is required to continue or complete a rolling update.

**REQ-NFR-03 — Disruption budget:** At no point during a rolling update, scale-down, or secret-rotation eviction cycle may more pods be simultaneously unavailable than permitted by the PodDisruptionBudget.

**REQ-NFR-04 — Pod termination ceiling:** The total time from pod termination signal to container exit must not exceed `terminationGracePeriodSeconds`. The preStop script must enforce this through cumulative budget tracking.

**REQ-NFR-05 — Finalizer non-blocking:** The pod cleanup finalizer handler must always complete and remove the finalizer, even when downstream systems (cluster manager, Kubernetes API for PVC deletion) are temporarily unavailable. A pod must never be permanently stuck in Terminating state.

**REQ-NFR-06 — No spurious restarts:** A secret version change must not trigger more than one restart cycle per actual change. IRSA-configured clusters must never trigger secret-version-based restarts.

**REQ-NFR-07 — Test coverage:** All new code paths must have unit tests. Critical paths (scale-down detection, PVC deletion, PDB violation handling, budget clamping, IRSA sentinel) must have explicit test cases. Coverage for `pod_deletion_handler.go` must be at least 80%.

**REQ-NFR-08 — No bare string literals:** All Kubernetes annotation keys, finalizer names, and intent values used across multiple files must be defined as exported Go constants. Tests must use those constants, not string literals.

---

## 7. API Changes Summary

| Resource | Field | Change | Notes |
|---|---|---|---|
| `CommonSplunkSpec` | `rollingUpdateConfig` | Added | Optional; controls StatefulSet update strategy |
| `IndexerCluster.status` | `restartStatus` | Added | Tracks per-pod eviction progress |
| `IngestorCluster.status` | `restartStatus` | Added | Tracks per-pod eviction progress |
| `IngestorCluster.status` | `queueBucketAccessSecretVersion` | Added | Tracks credential secret version for restart detection |
| `SearchHeadCluster.status` | `restartStatus` | Added | Tracks per-pod eviction progress |

All changes are additive. No existing fields are removed or renamed.

---

## 8. Constraints

- **Kubernetes version:** Requires Kubernetes 1.21+ for `policy/v1` PodDisruptionBudget and Eviction API. `policy/v1beta1` must not be used.
- **Splunk version:** Requires Splunk 9.x management API. The decommission endpoint path (`/services/cluster/peer/control/control/decommission`) must be verified against the minimum supported Splunk version.
- **StatefulSet partition:** The `partition` field is a StatefulSet-native feature. Values outside `[0, replicas]` are rejected by the Kubernetes API; the operator must validate and reject them before submitting.
- **PVC naming:** PVC cleanup relies on the StatefulSet naming convention `<volumeClaimTemplateName>-<podName>`. Any deviation from this convention in existing clusters would cause PVC cleanup to miss volumes. This constraint must be documented.
- **Downward API sync latency:** The kubelet updates Downward API volume files on a sync period (default 60 seconds, configurable via `--sync-frequency`). The operator must set the `scale-down` annotation with sufficient lead time before the pod is terminated. In practice, setting the annotation before patching the StatefulSet replica count provides adequate lead time.

---

## 9. Out of Scope

- SearchHeadCluster scale-out.
- Multi-site cluster topology changes.
- AppFramework app deployment changes.
- Monitoring Console configuration.
- IngestorCluster, Queue, and ObjectStorage CRD definitions (already in develop branch).
