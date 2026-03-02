# Unit and Integration Tests

**Type:** Story
**EPIC:** Kubernetes-Native Pod Lifecycle Management for Splunk Operator
**Component:** `pkg/splunk/enterprise/*_test.go`, `internal/controller/*_test.go`, `pkg/splunk/client/*_test.go`

---

## Background

The lifecycle changes introduced in this EPIC affect the most critical operations in the operator: pod termination, scale-down, and rolling updates. These paths must have high confidence test coverage because failures here result in data loss (buckets not migrated before pod deletion), data unavailability (too many pods down simultaneously), or permanent stuck states (pods that never complete termination).

Unit tests verify individual functions in isolation using mocks. Integration tests (using envtest) verify that the controllers interact correctly with the Kubernetes API under realistic conditions.

---

## Unit Tests Required

### `pkg/splunk/enterprise/configuration_test.go`

**`buildUpdateStrategy`**
| Test | Expected |
|---|---|
| No `RollingUpdateConfig` set | Strategy is `RollingUpdate`, `maxUnavailable: Int(1)`, `partition: nil` |
| `MaxPodsUnavailable: "3"` | `maxUnavailable: Int(3)` |
| `MaxPodsUnavailable: "25%"` | `maxUnavailable: Str("25%")` |
| `MaxPodsUnavailable: "0%"` | Falls back to default `Int(1)` (0 unavailable blocks all updates) |
| `MaxPodsUnavailable: "not-a-number"` | Falls back to default |
| `Partition: 5` with 10 replicas | `partition: 5` |
| `Partition: 10` with 10 replicas | `partition: 10` (no pods update) |
| `Partition: 11` with 10 replicas | Rejected, falls back to `partition: 0` |
| `Partition: -1` | Rejected, falls back to `partition: 0` |

**Pod template components (for IndexerCluster)**
| Test | Expected |
|---|---|
| Finalizer on pod template | `metadata.finalizers` contains `"splunk.com/pod-cleanup"` |
| Default intent annotation | `metadata.annotations["splunk.com/pod-intent"] == "serve"` |
| Downward API volume present | Volume named `podinfo` of type `downwardAPI` exists |
| Downward API volume projects intent | `items[0].path == "intent"`, `fieldRef.fieldPath == "metadata.annotations['splunk.com/pod-intent']"` |
| Downward API volume mounted | Mount at `/etc/podinfo` exists on splunk container |
| preStop hook configured | `lifecycle.preStop.exec.command` set correctly |
| `terminationGracePeriodSeconds` for indexer | `1020` |
| `terminationGracePeriodSeconds` for search head | `360` |
| `terminationGracePeriodSeconds` for standalone | `120` |
| `terminationGracePeriodSeconds` for cluster manager | `120` |
| `POD_NAME` env var | Downward API `metadata.name` |
| `POD_NAMESPACE` env var | Downward API `metadata.namespace` |
| Finalizer NOT on non-cluster pods | ClusterManager, LicenseManager templates have no `splunk.com/pod-cleanup` |

### `pkg/splunk/enterprise/util_test.go`

**`ApplyPodDisruptionBudget`**
| Test | Expected |
|---|---|
| 3-replica IndexerCluster | PDB created with `minAvailable: 2` |
| 1-replica IngestorCluster | PDB created with `minAvailable: 0` |
| Replica count changes from 3→5 | PDB patched to `minAvailable: 4` |
| PDB already exists with correct value | No update issued |
| IndexerCluster with `ClusterManagerRef.Name: cm-1` | PDB selector includes `part-of: cm-1` label |
| IndexerCluster with no ClusterManagerRef | PDB selector uses CR name as partOfIdentifier |
| CR deleted | PDB garbage-collected via owner reference |

### `pkg/splunk/splkcontroller/statefulset_test.go`

**`markPodForScaleDown`**
| Test | Expected |
|---|---|
| Scale from 4→3 | Pod `<sts-name>-3` annotation set to `"scale-down"` |
| Scale from 4→2 | Pods `<sts-name>-3` and `<sts-name>-2` both annotated |
| Pod does not exist | Returns nil (no error) |
| Annotation already set | No-op, no duplicate update |

**RollingUpdate detection**
| Test | Expected |
|---|---|
| StatefulSet has `UpdatedReplicas < Replicas` | Returns true (update in progress) |
| StatefulSet has `UpdatedReplicas == Replicas` | Returns false |

### `pkg/splunk/enterprise/pod_deletion_handler_test.go`

**`HandlePodDeletion`**

| Test | Expected |
|---|---|
| Pod without finalizer | Returns nil immediately, no cleanup |
| Pod without deletionTimestamp | Returns nil immediately |
| Indexer pod, annotation `scale-down`, isScaleDown=true | `removeIndexerFromClusterManager` called, PVCs deleted |
| Indexer pod, annotation `serve`, isScaleDown=false | Peer removal not called, PVCs not deleted |
| Indexer pod, no annotation, ordinal 3, STS replicas 3 | isScaleDown=true (ordinal >= replicas) |
| Indexer pod, no annotation, ordinal 2, STS replicas 3 | isScaleDown=false |
| Search head pod, scale-down | PVCs deleted |
| Search head pod, restart | PVCs not deleted |
| Cluster manager pod | No special cleanup, finalizer removed |
| CM unreachable in `removeIndexerFromClusterManager` | Error logged, finalizer still removed |
| PVC already deleted in `deletePVCsForPod` | 404 handled gracefully, finalizer still removed |

### `pkg/splunk/client/enterprise_test.go`

**`CheckRestartRequired`**
| Test | Expected |
|---|---|
| API returns 200 with entry | Returns `(true, message, nil)` |
| API returns 200 with empty entries array | Returns `(false, "", nil)` |
| API returns 404 | Returns `(false, "", nil)` |
| API returns 500 | Returns `(false, "", err)` |
| API connection refused | Returns `(false, "", err)` |

**`RemoveIndexerClusterPeer`**
| Test | Expected |
|---|---|
| API returns 200 | Returns nil |
| API returns 404 | Returns error |
| API returns 503 | Returns error |

### `pkg/splunk/enterprise/ingestorcluster_test.go`

**`evictPod` and `isPDBViolation`**
| Test | Expected |
|---|---|
| Eviction API returns 200 | Returns nil |
| Eviction API returns 429 | `isPDBViolation(err)` returns true |
| Eviction API returns 500 | `isPDBViolation(err)` returns false |

**`checkPodsRestartRequired`**
| Test | Expected |
|---|---|
| All pods return `restart_required=false` | Returns empty list |
| 2 of 5 pods return `restart_required=true` | Returns 2 pod names |
| One pod is unreachable | That pod is skipped, others are checked |
| Pod is not Ready | Pod is skipped |

**Rolling update conflict guard**
| Test | Expected |
|---|---|
| StatefulSet rolling update in progress | `checkPodsRestartRequired` is not called |
| No rolling update in progress | `checkPodsRestartRequired` is called |

**Secret version tracking**
| Test | Expected |
|---|---|
| Secret version unchanged | No restart triggered |
| Secret version changed | `PodsNeedingRestart` updated, one pod evicted |
| IRSA configured | `QueueBucketAccessSecretVersion` set to `"irsa-config-applied"` |

---

## Integration Tests (envtest)

Integration tests use `controller-runtime/pkg/envtest` with real CRD schemas loaded. No actual Splunk processes — mock Splunk API responses via `httptest.Server`.

### `internal/controller/pod_controller_test.go`

1. **Finalizer blocks deletion:** Create a pod with `splunk.com/pod-cleanup` finalizer. Issue `kubectl delete`. Verify pod is in Terminating state. Run `HandlePodDeletion`. Verify pod object is eventually deleted.
2. **Scale-down indexer cleanup:** Create indexer pod with annotation `scale-down` and mock cluster manager. Verify `remove_peers` is called and PVCs are deleted before finalizer is removed.
3. **Restart indexer:** Create indexer pod with annotation `serve`. Verify `remove_peers` is not called and PVCs are not deleted.

### `internal/controller/ingestorcluster_controller_test.go`

1. **Basic reconcile:** Create IngestorCluster with valid Queue and ObjectStorage references. Verify StatefulSet, Services, and PDB are created.
2. **Missing Queue reference:** Create IngestorCluster with non-existent Queue. Verify status.phase is Error.
3. **Secret version change:** Update the referenced secret. Verify `QueueBucketAccessSecretVersion` is updated and one pod eviction is issued.
4. **PDB updated on replica change:** Scale IngestorCluster from 3 to 5 replicas. Verify PDB `minAvailable` changes from 2 to 4.

### `internal/controller/indexercluster_controller_test.go`

1. **`markPodForScaleDown` called before replica update:** Use a reconcile tracker to verify annotation is set on the pod before the StatefulSet replica count is patched.

---

## Acceptance Criteria

1. All unit tests listed above are implemented and pass.
2. `go test ./pkg/splunk/...` and `go test ./internal/...` pass with no failures.
3. `TestIsPDBViolation` uses `k8serrors.NewTooManyRequests` (not a hand-constructed error) to simulate the Eviction API response.
4. StatefulSet fixture JSON files in `testdata/fixtures/` reflect the updated pod template (RollingUpdate strategy, preStop hook, terminationGracePeriodSeconds, downward API volume, finalizer, intent annotation).
5. No test uses bare string literals for `"splunk.com/pod-cleanup"` or `"splunk.com/pod-intent"` — all tests use the exported constants.
6. Test coverage for `pod_deletion_handler.go` is at least 80%.
7. Integration tests run successfully against envtest (no real cluster required).

---

## Definition of Done

- [ ] All unit tests implemented and passing.
- [ ] All integration tests implemented and passing.
- [ ] `testdata/fixtures/` JSON files updated to match new StatefulSet spec.
- [ ] `TestIsPDBViolation` uses correct `k8serrors` constructor.
- [ ] CI pipeline runs all tests on PR.
- [ ] Test coverage report shows ≥80% for new files.
