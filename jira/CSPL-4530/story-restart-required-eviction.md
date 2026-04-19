# Per-Pod restart_required Detection and Eviction API

**Type:** Story
**EPIC:** Kubernetes-Native Pod Lifecycle Management for Splunk Operator
**Component:** `pkg/splunk/enterprise/ingestorcluster.go`, `pkg/splunk/enterprise/standalone.go`, `pkg/splunk/client/enterprise.go`

---

## Background

StatefulSet RollingUpdate handles the case where a change to the pod template spec (image, environment variables, resource limits, etc.) requires pods to be replaced. Kubernetes detects this automatically by comparing the pod's current spec hash to the StatefulSet's target spec hash.

However, there is a separate category of changes that do not alter the pod spec but still require a Splunk process restart:

- A Kubernetes Secret referenced by the pod is updated (e.g., queue or bucket credentials). The secret is mounted as a file, so the file content changes on disk without the pod spec changing. Splunk does not automatically reload from updated files — it requires a restart.
- Splunk itself writes a `restart_required` bulletin board message at `/services/messages/restart_required` when its internal state requires a restart (e.g., after a configuration push from a deployment server).

In both cases, the pod spec hash does not change, so the StatefulSet controller does not initiate a rolling update. The operator must detect the need for restart and trigger it per pod.

### Why per-pod eviction, not bulk delete

Eviction through the Kubernetes Eviction API (`policy/v1.Eviction`) is the correct primitive for voluntarily removing a pod when the PodDisruptionBudget must be respected. A direct `Delete` on a pod bypasses PDB enforcement, potentially taking down more pods than the budget allows. The Eviction API returns HTTP 429 (Too Many Requests) when the eviction would violate the PDB, and the operator retries on the next reconcile cycle.

Only one pod is evicted per reconcile cycle to enforce a serial restart cadence.

---

## What Needs to Be Built

### 1. `CheckRestartRequired` in `pkg/splunk/client/enterprise.go`

A method on `SplunkClient` that queries the Splunk bulletin board for a `restart_required` entry:

```go
func (c *SplunkClient) CheckRestartRequired() (restartRequired bool, message string, err error)
```

- Calls `GET /services/messages/restart_required?output_mode=json`.
- Accepts HTTP 200 (entry exists, restart required) and HTTP 404 (no entry, no restart required).
- Returns `(true, message, nil)` if any entry exists in the response.
- Returns `(false, "", nil)` if the response contains no entries or is HTTP 404.
- Returns `(false, "", err)` on any other error.

The detection mechanism: Splunk creates the `restart_required` message entry when a restart is needed. The entry is automatically removed after a successful restart. The presence of the entry is the signal — the content (message field) is logged for observability.

### 2. Secret version tracking for IngestorCluster

IngestorCluster references a Queue and an ObjectStorage, each of which may reference Kubernetes Secrets for credentials. When those secrets are updated, the ingestor pods need to restart to pick up the new credentials.

Track the current secret version in `cr.Status.QueueBucketAccessSecretVersion`. On each reconcile:

```
currentVersion = hash(secret.ResourceVersion)
if currentVersion != cr.Status.QueueBucketAccessSecretVersion:
    mark all ingestor pods for restart
    update cr.Status.QueueBucketAccessSecretVersion = currentVersion
```

IRSA (IAM Roles for Service Accounts) is a special case: when IRSA is configured there is no secret to hash. Use the sentinel value `"irsa-config-applied"` so subsequent reconciles do not falsely detect a version change.

### 3. `checkPodsRestartRequired` for IngestorCluster and Standalone

A function that iterates all ready pods for a given cluster, checks each pod's `restart_required` endpoint, and returns a list of pod names that need restart:

```
checkPodsRestartRequired(ctx, client, pods):
    results = []
    for pod in pods where pod.Status.Phase == Running and pod is Ready:
        splunkClient = NewSplunkClientForPod(pod)
        restartRequired, message, err = splunkClient.CheckRestartRequired()
        if err: log and skip  // don't fail entire check if one pod is unreachable
        if restartRequired:
            results.append(pod.Name, message)
    return results
```

### 4. `evictPod` using Kubernetes Eviction API

```go
func evictPod(ctx context.Context, c client.Client, pod *corev1.Pod) error {
    eviction := &policyv1.Eviction{
        ObjectMeta: metav1.ObjectMeta{
            Name:      pod.Name,
            Namespace: pod.Namespace,
        },
    }
    return c.SubResource("eviction").Create(ctx, pod, eviction)
}
```

Uses `policy/v1` (not `policy/v1beta1`). The Eviction API automatically checks the PodDisruptionBudget before evicting. If the PDB would be violated, returns HTTP 429.

### 5. `isPDBViolation` helper

```go
func isPDBViolation(err error) bool {
    // Eviction API returns HTTP 429 Too Many Requests when PDB blocks eviction
    return k8serrors.IsTooManyRequests(err)
}
```

### 6. Rolling restart orchestration in IngestorCluster and Standalone reconcilers

The reconciler checks for restart-required conditions and evicts pods one at a time:

```
reconcile(cr):
    // ... StatefulSet reconciliation ...

    // Skip if a StatefulSet RollingUpdate is already in progress
    // Eviction conflicts with an in-progress rolling update
    if rollingUpdateInProgress(statefulSet):
        return

    // Check secret version change (IngestorCluster only)
    if secretVersionChanged(cr):
        markAllPodsForRestart(cr)

    // Check per-pod restart_required signal
    podsThatNeedRestart = checkPodsRestartRequired(ctx, client, pods)
    if len(podsThatNeedRestart) > 0:
        pod = podsThatNeedRestart[0]  // one pod per reconcile
        err = evictPod(pod)
        if isPDBViolation(err):
            log("PDB blocked eviction, will retry on next reconcile")
            return
        updateRestartStatus(cr, pod)
```

### 7. Update `RestartStatus` in CR status

After initiating eviction of a pod, update `cr.Status.RestartStatus`:
- `Phase`: `InProgress`
- `Message`: e.g., `"Restarting pod splunk-ing-ingestor-3 (1/5 restarted)"`
- `TotalPods`, `PodsNeedingRestart`, `PodsRestarted`, `LastRestartTime`

When no more pods report `restart_required`, set `Phase` to `Completed` or clear it.

---

## Acceptance Criteria

1. `CheckRestartRequired` returns `(true, message, nil)` when the Splunk API has a `restart_required` entry, and `(false, "", nil)` when there is none or the endpoint returns 404.
2. When a secret referenced by an IngestorCluster's Queue or ObjectStorage is updated, the reconciler detects the version change on the next reconcile and marks pods for restart.
3. When `checkPodsRestartRequired` finds 3 of 5 pods have `restart_required` set, the reconciler evicts exactly one pod per reconcile cycle.
4. When the PDB blocks eviction (HTTP 429), the eviction is not retried in the same reconcile cycle — it is retried on the next requeue.
5. `evictPod` uses `policy/v1.Eviction`, not direct pod deletion.
6. When a StatefulSet RollingUpdate is in progress (`statefulSet.Status.UpdatedReplicas < statefulSet.Status.Replicas`), per-pod eviction is skipped to avoid conflict.
7. `cr.Status.RestartStatus` accurately reflects restart progress at each reconcile cycle.
8. When IRSA is configured, `QueueBucketAccessSecretVersion` is set to `"irsa-config-applied"` and subsequent reconciles do not trigger spurious restarts.
9. If `CheckRestartRequired` returns an error for a specific pod, that pod is skipped and others continue to be checked.
10. The Standalone reconciler uses the same `restart_required` detection and per-pod eviction for secret-triggered restarts.

---

## Definition of Done

- [ ] `CheckRestartRequired` implemented and tested in `pkg/splunk/client/enterprise.go`.
- [ ] `evictPod` implemented using `policy/v1.Eviction` (extracted to `util.go` for sharing between IngestorCluster and Standalone).
- [ ] `isPDBViolation` implemented using `k8serrors.IsTooManyRequests`.
- [ ] Secret version tracking implemented in IngestorCluster reconciler using `QueueBucketAccessSecretVersion`.
- [ ] `checkPodsRestartRequired` implemented with graceful handling of unreachable pods.
- [ ] Rolling-update conflict guard implemented in both IngestorCluster and Standalone reconcilers.
- [ ] `RestartStatus` updated in CR status during eviction operations.
- [ ] Standalone reconciler updated to use per-pod `restart_required` eviction for secret changes.
- [ ] Unit tests: `CheckRestartRequired` return values, `isPDBViolation` detects HTTP 429, one-pod-per-reconcile enforcement, PDB block handling, rolling update conflict guard, secret version change detection, IRSA sentinel handling, unreachable pod is skipped.
