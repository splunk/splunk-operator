# RBAC, CRD Manifest, and Helm Chart Updates

**Type:** Story
**EPIC:** Kubernetes-Native Pod Lifecycle Management for Splunk Operator
**Component:** `config/rbac/role.yaml`, `helm-chart/splunk-operator/`, `config/crd/`, `cmd/main.go`

---

## Background

The rolling restart mechanism introduces capabilities the operator did not previously exercise: creating and patching PodDisruptionBudgets, evicting pods via the Eviction API, and reconciling pods with a finalizer. Each of these requires explicit RBAC permissions in the operator's ClusterRole. Missing permissions result in `Forbidden` errors at runtime — typically silent from the end-user perspective, causing scale-down or restart operations to silently fail or stall.

Additionally, three existing CRDs gain new spec fields (`rollingUpdateConfig` on `CommonSplunkSpec`) and new status fields (`RestartStatus` on IngestorCluster, IndexerCluster, and SearchHeadCluster) that must be reflected in the generated CRD YAML. The Helm chart's ClusterRole template must mirror the `config/rbac/role.yaml` changes so that Helm-deployed operators have the same permissions as kustomize-deployed operators.

A new controller — `PodReconciler` — must be registered in `cmd/main.go`.

---

## What Needs to Be Built

### 1. ClusterRole additions (`config/rbac/role.yaml`)

#### PodDisruptionBudgets (policy/v1)

```yaml
- apiGroups: ["policy"]
  resources: ["poddisruptionbudgets"]
  verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]
```

Required for `ApplyPodDisruptionBudget` to create and update PDBs for IndexerCluster, IngestorCluster, and SearchHeadCluster.

#### Pod Eviction (policy/v1)

```yaml
- apiGroups: ["policy"]
  resources: ["pods/eviction"]
  verbs: ["create"]
```

Required for `evictPod` via `c.SubResource("eviction").Create(...)`. **Must be under the `policy` API group.** Using the `""` (core) API group for `pods/eviction` is incorrect and was the cause of a previous bug — the Eviction API is `policy/v1`, not core.

#### Verify existing permissions

The following must be present. If any are missing, add them:

- `apps/statefulsets`: `get, list, watch, create, update, patch` — for reading StatefulSet update status (rolling update detection) and writing StatefulSet spec.
- `core/pods`: `get, list, watch, update, patch` — for `markPodForScaleDown` (annotation update) and the finalizer handler (reading pod metadata, removing finalizer).
- `core/persistentvolumeclaims`: `get, list, delete` — for `deletePVCsForPod` in the finalizer handler.
- `core/secrets`: `get, list, watch` — for reading credentials in `waitForIndexerDecommission` and `removeIndexerFromClusterManager`.

### 2. Updated CRD YAML manifests

Run `make generate manifests` and commit the output. The following CRD files change because `CommonSplunkSpec` gains `rollingUpdateConfig`, and IngestorCluster, IndexerCluster, and SearchHeadCluster status gains `RestartStatus`:

- `config/crd/bases/enterprise.splunk.com_indexerclusters.yaml` — `rollingUpdateConfig` in spec, `restartStatus` in status
- `config/crd/bases/enterprise.splunk.com_ingestorclusters.yaml` — `rollingUpdateConfig` in spec, `restartStatus` in status
- `config/crd/bases/enterprise.splunk.com_searchheadclusters.yaml` — `rollingUpdateConfig` in spec, `restartStatus` in status
- `config/crd/bases/enterprise.splunk.com_standalones.yaml` — `rollingUpdateConfig` in spec
- `config/crd/bases/enterprise.splunk.com_clustermanagers.yaml` — `rollingUpdateConfig` in spec
- `config/crd/bases/enterprise.splunk.com_licensemanagers.yaml` — `rollingUpdateConfig` in spec
- `config/crd/bases/enterprise.splunk.com_monitoringconsoles.yaml` — `rollingUpdateConfig` in spec

All other CRDs that embed `CommonSplunkSpec` are similarly affected.

### 3. Helm chart ClusterRole update

**`helm-chart/splunk-operator/templates/rbac/clusterrole.yaml`**

Mirror the two new RBAC rules from `config/rbac/role.yaml` exactly:
- `policy/poddisruptionbudgets` with full verbs
- `policy/pods/eviction` with `create`

Verify all existing rules are present and consistent with `config/rbac/role.yaml`. The Helm ClusterRole and the kustomize ClusterRole must grant identical permissions.

### 4. Register PodReconciler in `cmd/main.go`

The `PodReconciler` (see pod-cleanup-finalizer story) watches Pod objects with the `splunk.com/pod-cleanup` finalizer. It must be registered alongside the existing reconcilers:

```go
if err = (&controller.PodReconciler{
    Client: mgr.GetClient(),
    Scheme: mgr.GetScheme(),
}).SetupWithManager(mgr); err != nil {
    setupLog.Error(err, "unable to create controller", "controller", "Pod")
    os.Exit(1)
}
```

Ensure it respects the same `WATCH_NAMESPACE` configuration used by the other controllers.

---

## Acceptance Criteria

1. `kubectl auth can-i create poddisruptionbudgets --as=system:serviceaccount:<ns>:splunk-operator` returns `yes` after applying the updated ClusterRole.
2. `kubectl auth can-i create pods/eviction --as=system:serviceaccount:<ns>:splunk-operator` returns `yes`. Confirm the binding is under the `policy` API group, not core.
3. The operator can create a PDB without receiving a `Forbidden` error in its logs.
4. The operator can evict a pod via the Eviction API without receiving a `Forbidden` error in its logs.
5. All updated CRD YAML files pass `kubectl apply --dry-run=server` against a cluster with the previous CRD version installed (no breaking schema changes).
6. The `rollingUpdateConfig` field appears in the OpenAPI schema of all CRDs that embed `CommonSplunkSpec`.
7. The `restartStatus` field appears in the status schema of IndexerCluster, IngestorCluster, and SearchHeadCluster CRDs.
8. `helm install splunk-operator helm-chart/splunk-operator` on a clean cluster results in a ClusterRole that includes both the PDB and eviction rules.
9. `helm lint helm-chart/splunk-operator` passes with no errors or warnings.
10. The PodReconciler is running after operator startup (visible in operator logs: `"Starting Controller" controller=Pod`).
11. The PodReconciler respects `WATCH_NAMESPACE` — if set to a specific namespace, it does not watch pods in other namespaces.

---

## Definition of Done

- [ ] `config/rbac/role.yaml` updated with PDB and eviction rules.
- [ ] Existing pod, PVC, and secret rules verified present.
- [ ] All affected CRD YAML files regenerated via `make generate manifests` and committed.
- [ ] Helm ClusterRole template updated to match `config/rbac/role.yaml`.
- [ ] `PodReconciler` registered in `cmd/main.go` with correct namespace scoping.
- [ ] `helm lint` passes.
- [ ] Manual smoke test: operator starts, creates a PDB, evicts a pod — no `Forbidden` errors in operator logs.
