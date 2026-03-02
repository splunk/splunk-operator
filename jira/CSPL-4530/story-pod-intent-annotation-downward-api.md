# Pod Intent Annotation and Downward API Volume

**Type:** Story
**EPIC:** Kubernetes-Native Pod Lifecycle Management for Splunk Operator
**Component:** `pkg/splunk/enterprise/configuration.go`, `pkg/splunk/splkcontroller/statefulset.go`, `api/v4/`

---

## Background

When a pod is terminated, the preStop hook must behave differently depending on *why* the pod is terminating:

- **Restart / config change:** The pod will come back with the same ordinal and reattach to the same PVC. Bucket migration is not needed. Calling decommission with `enforce_counts=1` would migrate all buckets away from this peer only to re-ingest them when the pod comes back — this is wasteful and slow. Instead, call decommission with `enforce_counts=0` (tell the cluster to stop routing new data here, but don't migrate existing buckets) and proceed quickly.
- **Scale-down:** The pod is being permanently removed. All primary buckets on this peer must be migrated to the remaining peers before the pod stops, otherwise data is unreachable. Call decommission with `enforce_counts=1` and wait for the cluster manager to confirm migration is complete before allowing the pod to stop.

The preStop hook script runs inside the pod container. It has no access to the Kubernetes API and no RBAC. The only mechanism available to communicate the reason for termination is something mounted into the pod's filesystem.

### Why environment variables cannot be used

Environment variables in a running container are resolved once, at pod start, and are never updated by Kubernetes while the pod is running. If the operator sets an env var in the pod spec after the pod is already running, the running container does not see the change. A scale-down annotation set immediately before the scale-down operation would therefore not be visible to the preStop hook via an env var.

### Why Downward API volume works

A Downward API volume mounts pod metadata (labels, annotations, etc.) as files on the container filesystem. When an annotation changes on the pod object, the kubelet updates the mounted file within the configured sync period (typically seconds). The preStop hook can read `/etc/podinfo/intent` and see the current value of `splunk.com/pod-intent` at the moment of termination.

---

## What Needs to Be Built

### 1. Add finalizer and default intent annotation to StatefulSet pod template

In `getSplunkStatefulset` (or the equivalent builder for each cluster type), when building the pod template for IndexerCluster or SearchHeadCluster pods:

- Add finalizer `splunk.com/pod-cleanup` to `podTemplateSpec.ObjectMeta.Finalizers`. This ensures every pod created from the StatefulSet has the finalizer from birth. Deduplicate — do not add it twice.
- Add annotation `splunk.com/pod-intent: "serve"` to `podTemplateSpec.ObjectMeta.Annotations`. This is the default state. The operator will overwrite it to `"scale-down"` on specific pods before a scale-down operation.

### 2. Mount the intent annotation as a Downward API volume

In `updateSplunkPodTemplateWithConfig`, add a Downward API volume that exposes the `splunk.com/pod-intent` annotation as a file:

```yaml
volumes:
- name: podinfo
  downwardAPI:
    items:
    - path: "intent"
      fieldRef:
        fieldPath: metadata.annotations['splunk.com/pod-intent']
```

Mount it read-only at `/etc/podinfo`:

```yaml
volumeMounts:
- name: podinfo
  mountPath: /etc/podinfo
  readOnly: true
```

The preStop hook reads `/etc/podinfo/intent` to determine the current intent value.

### 3. Implement `markPodForScaleDown` in `statefulset.go`

Before reducing the StatefulSet replica count, the operator must annotate the pod(s) that will be removed. StatefulSets always remove pods starting from the highest ordinal, so the pods to be annotated are those with ordinals `[newReplicas, currentReplicas)`.

```
func markPodForScaleDown(ctx, client, statefulSet, newReplicas):
    for ordinal in range(newReplicas, currentReplicas):
        podName = statefulSet.Name + "-" + ordinal
        pod = get(podName)
        pod.Annotations["splunk.com/pod-intent"] = "scale-down"
        update(pod)
```

This function must be called after intent annotation is set on the pod and before the StatefulSet replica count is updated. The ordering matters: if the StatefulSet replica count is reduced first, Kubernetes may begin deleting the pod before the annotation update arrives.

If the annotation update fails (e.g., the pod does not exist yet), the operator must not fail the reconcile — the finalizer handler has a fallback that compares pod ordinal against `statefulSet.Spec.Replicas` to detect scale-down without the annotation.

### 4. Define annotation constants

In `pkg/splunk/enterprise/pod_deletion_handler.go`:

```go
const (
    PodCleanupFinalizer = "splunk.com/pod-cleanup"
    PodIntentAnnotation = "splunk.com/pod-intent"
    PodIntentServe      = "serve"
    PodIntentScaleDown  = "scale-down"
    PodIntentRestart    = "restart"
)
```

These constants must be used everywhere the annotation or finalizer name appears — no bare strings.

---

## Acceptance Criteria

1. Every pod created from an IndexerCluster or SearchHeadCluster StatefulSet has the finalizer `splunk.com/pod-cleanup` in its `metadata.finalizers` from the moment the pod is created.
2. Every pod created from an IndexerCluster or SearchHeadCluster StatefulSet has the annotation `splunk.com/pod-intent: serve` by default.
3. Every such pod has a volume named `podinfo` of type `downwardAPI` projecting `metadata.annotations['splunk.com/pod-intent']` to the path `intent`.
4. The `podinfo` volume is mounted at `/etc/podinfo` in the Splunk container.
5. When a 4-replica IndexerCluster is scaled to 3 replicas:
   - The pod at ordinal 3 (`<name>-indexer-3`) has its annotation updated to `splunk.com/pod-intent: scale-down` before the StatefulSet replica count is patched.
   - The file `/etc/podinfo/intent` on the running pod reads `scale-down` within the kubelet sync period.
6. When a config change (e.g., image update) is applied to an IndexerCluster, the pods' `splunk.com/pod-intent` annotation is not changed — it remains `serve`.
7. If `markPodForScaleDown` cannot find a pod (it may already be gone), the function returns without error and the reconcile continues.
8. The annotation and finalizer constants (`PodCleanupFinalizer`, `PodIntentAnnotation`, `PodIntentServe`, `PodIntentScaleDown`) are defined and used consistently — no bare string literals for these values anywhere in the codebase.
9. The `podinfo` volume and mount do not appear on pod templates for cluster roles that do not use the finalizer mechanism (ClusterManager, LicenseManager, MonitoringConsole, Deployer).

---

## Definition of Done

- [ ] `PodCleanupFinalizer`, `PodIntentAnnotation`, and intent value constants defined in `pod_deletion_handler.go`.
- [ ] Pod template builder adds finalizer and default `serve` intent annotation for IndexerCluster and SearchHeadCluster pods.
- [ ] Downward API volume and mount added to pod template for IndexerCluster and SearchHeadCluster.
- [ ] `markPodForScaleDown` implemented and called from scale-down path in `statefulset.go`.
- [ ] `markPodForScaleDown` is called before the StatefulSet replica count is patched.
- [ ] StatefulSet fixture JSON files updated to include finalizer, annotation, and downward API volume.
- [ ] Unit tests: finalizer present on template, annotation present, downward API volume configured, `markPodForScaleDown` sets correct annotation, `markPodForScaleDown` is a no-op if pod missing.
