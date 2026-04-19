# Pod Cleanup Finalizer: Cluster Manager Peer Removal and PVC Lifecycle

**Type:** Story
**EPIC:** Kubernetes-Native Pod Lifecycle Management for Splunk Operator
**Component:** `pkg/splunk/enterprise/pod_deletion_handler.go`, `internal/controller/pod_controller.go`, `pkg/splunk/client/enterprise.go`

---

## Background

When an indexer pod is permanently removed (scale-down), two cleanup actions are required that cannot happen inside the pod's preStop hook:

**1. Remove the peer from the cluster manager's configuration.**
The Splunk cluster manager maintains a registry of every peer it has ever seen. When a peer is decommissioned and stopped, it transitions to a `Down` state in this registry but is not automatically removed. The peer will remain in the registry indefinitely unless explicitly removed via the `remove_peers` API. Accumulating `Down` peers causes:
- False alerts on cluster health dashboards
- Confusion when diagnosing cluster state
- Eventually hitting the cluster manager's peer limit (capped at 100 peers in some versions)

The preStop hook cannot call this API because:
- It needs the peer's GUID, which is only visible from outside the pod (via the cluster manager API)
- The peer must be fully stopped before removal (calling remove_peers while a peer is still running disrupts it)

**2. Delete the PVCs.**
When a pod is removed for a scale-down, the PVCs it used (`pvc-etc-<pod-name>`, `pvc-var-<pod-name>`) must be deleted to free storage. However, PVCs must be preserved during a restart/config-change — the pod will come back with the same ordinal and must reattach to the same PVC to preserve its indexed data.

StatefulSet does not delete PVCs when pods are deleted. They must be deleted explicitly by the operator.

### Why a finalizer is the right mechanism

A finalizer (`splunk.com/pod-cleanup`) on the pod object blocks Kubernetes from garbage-collecting the pod until the finalizer is removed. This gives the operator a guaranteed window after the container has exited but before the pod object is gone, in which to:
1. Confirm decommission is complete (if not already confirmed by preStop)
2. Call the cluster manager `remove_peers` API
3. Delete the PVCs (scale-down only)
4. Remove the finalizer to allow pod deletion to proceed

Without a finalizer, the pod object (and its name, labels, and annotations — including the peer name) disappears as soon as the container exits, making it impossible to know which peer GUID to remove.

---

## What Needs to Be Built

### 1. `internal/controller/pod_controller.go` — PodReconciler

A new controller that watches `Pod` objects across all namespaces (or the operator's watched namespaces). It reconciles pods that match both conditions:
- `metadata.finalizers` contains `splunk.com/pod-cleanup`
- `metadata.deletionTimestamp` is set (pod is being deleted)

On each reconcile for such a pod, call `HandlePodDeletion(ctx, client, pod)`.

The controller must not modify pods that do not have the finalizer, and must not reconcile pods that are not being deleted.

### 2. `pkg/splunk/enterprise/pod_deletion_handler.go` — `HandlePodDeletion`

The main entry point called by the pod controller.

```
HandlePodDeletion(ctx, client, pod):
    if pod does not have finalizer → return nil
    if pod has no deletionTimestamp → return nil

    instanceType = getInstanceTypeFromPod(pod)  // from "app.kubernetes.io/component" label
    ordinal = getPodOrdinal(pod.Name)           // parse integer suffix from pod name
    statefulSet = getOwningStatefulSet(pod)

    isScaleDown = false
    if annotation["splunk.com/pod-intent"] == "scale-down":
        isScaleDown = true
    else if ordinal >= statefulSet.Spec.Replicas:
        isScaleDown = true  // fallback: ordinal outside desired range

    switch instanceType:
        case SplunkIndexer:  handleIndexerPodDeletion(pod, statefulSet, isScaleDown)
        case SplunkSearchHead: handleSearchHeadPodDeletion(pod, statefulSet, isScaleDown)
        default: (no extra cleanup needed)

    removeFinalizer(pod, "splunk.com/pod-cleanup")
```

### 3. `handleIndexerPodDeletion`

```
handleIndexerPodDeletion(pod, statefulSet, isScaleDown):
    // Verify decommission is complete
    // (preStop hook should have started and likely completed it)
    waitForIndexerDecommission(pod)
    // Returns nil if peer is Down/GracefulShutdown, or if peer not found in CM

    if isScaleDown:
        removeIndexerFromClusterManager(pod, statefulSet)
        deletePVCsForPod(pod, statefulSet)
```

For restart: verify decommission status only. Do not remove peer, do not delete PVCs.

### 4. `handleSearchHeadPodDeletion`

```
handleSearchHeadPodDeletion(pod, statefulSet, isScaleDown):
    if isScaleDown:
        waitForSearchHeadDetention(pod)
        deletePVCsForPod(pod, statefulSet)
```

For restart: no action needed. The preStop hook handles detention.

### 5. `removeIndexerFromClusterManager`

```
removeIndexerFromClusterManager(pod, statefulSet):
    cmName = getClusterManagerNameFromPod(pod)
    // looks up ClusterManagerRef from the owning CR via StatefulSet labels

    cmServiceName = GetSplunkServiceName(SplunkClusterManager, cmName, false)
    cmEndpoint = "https://" + cmServiceName + "." + pod.Namespace + ".svc.cluster.local:8089"
    cmClient = NewSplunkClient(cmEndpoint, "admin", password)

    peers = cmClient.GetClusterManagerPeers()
    peerInfo = peers[pod.Name]  // look up by pod name (peer label)
    if not found: return nil (already removed)

    cmClient.RemoveIndexerClusterPeer(peerInfo.ID)
    // POST /services/cluster/manager/control/control/remove_peers?peers={GUID}
```

Failure to remove the peer must be logged but must not block finalizer removal — the cluster manager may be temporarily unavailable, and blocking the finalizer indefinitely would prevent the pod from ever being deleted.

### 6. `deletePVCsForPod`

```
deletePVCsForPod(pod, statefulSet):
    // PVC names follow StatefulSet naming convention:
    // <volumeClaimTemplateName>-<podName>
    // For Splunk: "pvc-etc-<podName>" and "pvc-var-<podName>"
    for each volumeClaimTemplate in statefulSet.Spec.VolumeClaimTemplates:
        pvcName = volumeClaimTemplate.Name + "-" + pod.Name
        pvc = get(pvcName, pod.Namespace)
        if found: delete(pvc)
```

PVC deletion errors must be logged but must not block finalizer removal.

### 7. `pkg/splunk/client/enterprise.go` — `RemoveIndexerClusterPeer`

```go
func (c *SplunkClient) RemoveIndexerClusterPeer(id string) error
// POST /services/cluster/manager/control/control/remove_peers?peers={id}
// Returns nil on 200 OK, error otherwise.
```

---

## Acceptance Criteria

1. When an indexer pod is permanently deleted (scale-down), the finalizer prevents the pod object from being garbage-collected until `HandlePodDeletion` completes.
2. `HandlePodDeletion` correctly identifies scale-down via the `splunk.com/pod-intent: scale-down` annotation.
3. `HandlePodDeletion` falls back to ordinal comparison (`ordinal >= statefulSet.Spec.Replicas`) when the intent annotation is missing.
4. After scale-down cleanup, the peer is no longer listed in the cluster manager's `/services/cluster/manager/peers` response.
5. After scale-down cleanup, both PVCs for the deleted pod (`pvc-etc-<pod-name>` and `pvc-var-<pod-name>`) no longer exist in the namespace.
6. When an indexer pod is restarted (not scale-down), `HandlePodDeletion` does not call `removeIndexerFromClusterManager` and does not delete the PVCs. The pod rejoins the cluster with its existing PVCs.
7. When a search head pod is scale-downed, both PVCs are deleted. When restarted, PVCs are preserved.
8. If the cluster manager API is unreachable during `removeIndexerFromClusterManager`, the error is logged and the finalizer is still removed (the pod is not stuck in Terminating state indefinitely).
9. If a PVC is already deleted before `deletePVCsForPod` runs, the function handles the 404 gracefully and does not fail.
10. The `RemoveIndexerClusterPeer` Splunk client method calls `POST /services/cluster/manager/control/control/remove_peers?peers={id}` and returns nil on HTTP 200.
11. The pod controller watches all relevant namespaces (matching the operator's `WATCH_NAMESPACE` configuration).

---

## Definition of Done

- [ ] `pod_controller.go` implemented with watch predicate that filters to pods with finalizer + deletionTimestamp.
- [ ] `HandlePodDeletion`, `handleIndexerPodDeletion`, `handleSearchHeadPodDeletion` implemented in `pod_deletion_handler.go`.
- [ ] `removeIndexerFromClusterManager` and `deletePVCsForPod` implemented.
- [ ] `RemoveIndexerClusterPeer` implemented in `pkg/splunk/client/enterprise.go`.
- [ ] Pod controller registered in `cmd/main.go`.
- [ ] Unit tests: scale-down vs restart detection via annotation, scale-down vs restart detection via ordinal fallback, peer removal called only on scale-down, PVC deletion called only on scale-down, PVC preservation on restart, cluster manager unreachable is non-blocking, PVC already deleted is non-blocking.
- [ ] Integration test: full scale-down from 4 to 3 replicas — peer removed from CM, PVCs deleted, pod object eventually gone.
