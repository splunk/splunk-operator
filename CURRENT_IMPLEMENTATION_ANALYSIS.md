# Current Implementation Analysis - Per-Pod Rolling Restart

## Executive Summary

Based on comprehensive code analysis, here's what we have implemented vs. what needs to be changed according to your requirements.

---

## ✅ WHAT WE HAVE IMPLEMENTED

### 1. restart_required Detection & Pod Eviction

**Status:** ✅ IMPLEMENTED for IngestorCluster and Standalone

**Location:**
- `pkg/splunk/enterprise/ingestorcluster.go:863-943` (`checkAndEvictIngestorsIfNeeded`)
- `pkg/splunk/enterprise/standalone.go:356-436` (`checkAndEvictStandaloneIfNeeded`)

**How it Works:**
```go
// For each pod:
1. Check restart_required via Splunk API: GET /services/messages/restart_required
2. If restart needed, call evictPod() using Kubernetes Eviction API
3. Eviction API automatically respects PDB
4. StatefulSet controller recreates pod automatically
5. Only 1 pod evicted per reconcile cycle (5 seconds)
```

**PDB Handling:** ✅ Automatic via Kubernetes Eviction API
- If PDB would be violated, eviction returns error: "Cannot evict pod"
- Operator detects via `isPDBViolation()` and retries next cycle

### 2. PodDisruptionBudget (PDB) Creation

**Status:** ✅ IMPLEMENTED for all cluster types

**Location:** `pkg/splunk/enterprise/util.go:2601-2716` (`ApplyPodDisruptionBudget`)

**Configuration:**
```yaml
minAvailable: replicas - 1  # Allows 1 pod disruption at a time
# For 1 replica: minAvailable = 1 (no disruptions allowed)
# For 3 replicas: minAvailable = 2 (1 disruption allowed)
```

**Applied To:**
- IndexerCluster ✅
- SearchHeadCluster ✅
- IngestorCluster ✅
- Standalone ✅ (only if replicas > 1)

### 3. Pod Finalizers & Intent Annotations

**Status:** ✅ IMPLEMENTED

**Finalizer:** `splunk.com/pod-cleanup`
- Blocks pod deletion until cleanup completes
- Added to IndexerCluster and SearchHeadCluster pods

**Intent Annotation:** `splunk.com/pod-intent`
- Values: `serve`, `scale-down`, `restart`
- Marked BEFORE scale-down to distinguish from restart
- Location: `pkg/splunk/splkcontroller/statefulset.go` (`markPodForScaleDown`)

**Handler:** `pkg/splunk/enterprise/pod_deletion_handler.go`
- Detects intent (scale-down vs restart)
- Waits for decommission/detention to complete
- Deletes PVCs on scale-down, preserves on restart

### 4. Lifecycle PreStop Hook Registration

**Status:** ⚠️ PARTIALLY IMPLEMENTED (registration done, script missing)

**Location:** `pkg/splunk/enterprise/configuration.go:1141-1152`

**Code:**
```go
podTemplateSpec.Spec.Containers[idx].Lifecycle = &corev1.Lifecycle{
    PreStop: &corev1.LifecycleHandler{
        Exec: &corev1.ExecAction{
            Command: []string{"/mnt/probes/preStop.sh"},
        },
    },
}
```

**Applied To:** ALL Splunk pods (Indexer, SearchHead, Ingestor, Standalone, CM, LM, MC, Deployer)

**Problem:** ❌ `tools/k8_probes/preStop.sh` script does NOT exist!

### 5. StatefulSet Rolling Update Strategy

**Status:** ✅ IMPLEMENTED

**Strategy:** RollingUpdate (Kubernetes native)
- All StatefulSets use `RollingUpdateStatefulSetStrategyType`
- Kubernetes automatically handles one-at-a-time updates
- Respects PDB during rolling updates

---

## ❌ WHAT NEEDS TO BE CHANGED

### 1. Move Decommission from Operator Code to PreStop Hook

**Current State:** Decommission is called IN operator code

**Location:** `pkg/splunk/enterprise/indexercluster.go:1078-1079`
```go
func (mgr *indexerClusterPodManager) decommission(ctx context.Context, n int32, enforceCounts bool) (bool, error) {
    // ...
    c := mgr.getClient(ctx, n)
    return false, c.DecommissionIndexerClusterPeer(enforceCounts)  // ❌ Called from operator
}
```

**Called From:**
- `PrepareScaleDown()` (line 1030): `enforceCounts=true` (rebalance buckets)
- `PrepareRecycle()` (line 1045): `enforceCounts=false` (no rebalance)

**❌ NEEDS TO CHANGE:**
1. ✅ Keep decommission call in `pod_deletion_handler.go` for **waiting/verification**
2. ❌ Remove decommission call from `indexercluster.go`
3. ✅ Move decommission **execution** to `preStop.sh` script
4. ✅ Operator finalizer handler should only **wait** for decommission to complete

### 2. Move Detention from Operator Code to PreStop Hook

**Current State:** Detention is called IN operator code

**Location:** `pkg/splunk/enterprise/searchheadclusterpodmanager.go:86`
```go
func (mgr *searchHeadClusterPodManager) PrepareScaleDown(ctx context.Context, n int32) (bool, error) {
    // ...
    c := mgr.getClient(ctx, n)
    err = c.RemoveSearchHeadClusterMember()  // ❌ Called from operator
}
```

**API Called:** `POST /services/shcluster/member/consensus/default/remove_server`

**❌ NEEDS TO CHANGE:**
1. ✅ Keep detention waiting in `pod_deletion_handler.go` for **verification**
2. ❌ Remove detention call from `searchheadclusterpodmanager.go`
3. ✅ Move detention **execution** to `preStop.sh` script
4. ✅ Operator finalizer handler should only **wait** for detention to complete

### 3. Create Missing preStop.sh Script

**Status:** ❌ MISSING - Script does NOT exist!

**Expected Location:** `tools/k8_probes/preStop.sh`

**Required Logic:**
```bash
#!/bin/bash
# Detect pod role (indexer, search head, ingestor, standalone, etc.)
# Read pod intent annotation: splunk.com/pod-intent
#
# For INDEXERS:
#   - Call: POST /services/cluster/peer/control/control/decommission
#   - If scale-down: enforce_counts=1 (rebalance buckets)
#   - If restart: enforce_counts=0 (no rebalance)
#   - Wait for status to become "Down" or "GracefulShutdown"
#   - Call: splunk stop
#
# For SEARCH HEADS:
#   - Call: POST /services/shcluster/member/consensus/default/remove_server
#   - Wait for removal (member no longer in consensus)
#   - Call: splunk stop
#
# For INGESTORS, STANDALONE, CM, LM, MC, DEPLOYER:
#   - Call: splunk stop (graceful shutdown)
```

### 4. Support Rolling Restart with Percentage-Based Strategy

**Current State:** StatefulSet uses RollingUpdate but no partition/percentage control

**❌ NEEDS TO CHANGE:** Add support for percentage-based rolling updates

**Options:**
1. Use `StatefulSetSpec.UpdateStrategy.RollingUpdate.Partition` for staged rollouts
2. Add custom logic to control percentage of pods updated at a time
3. Consider using MaxUnavailable (if Kubernetes version supports it)

**Example:**
```yaml
spec:
  updateStrategy:
    type: RollingUpdate
    rollingUpdate:
      partition: 0  # Start with highest ordinal
      maxUnavailable: 25%  # Allow 25% of pods down during update
```

---

## 📋 REQUIRED CHANGES SUMMARY

### High Priority (Blocking)

1. **Create `tools/k8_probes/preStop.sh` script** ⚠️ CRITICAL
   - Implement role-specific logic (indexer, search head, others)
   - Read pod intent annotation
   - Call appropriate Splunk APIs
   - Handle decommission/detention
   - Call splunk stop

2. **Remove decommission from operator code**
   - Remove from `indexercluster.go:1078` (keep only waiting logic)
   - Keep `waitForIndexerDecommission()` in `pod_deletion_handler.go`

3. **Remove detention from operator code**
   - Remove from `searchheadclusterpodmanager.go:86` (keep only waiting logic)
   - Implement `waitForSearchHeadDetention()` in `pod_deletion_handler.go` (currently placeholder)

### Medium Priority

4. **Update restart_required detection scope**
   - ✅ Already done: Removed from IndexerCluster/SearchHeadCluster
   - ✅ Already done: Kept for IngestorCluster/Standalone

5. **Add percentage-based rolling update support**
   - Add configuration option for update percentage
   - Implement partition-based or custom rolling logic
   - Update StatefulSet spec with partition/maxUnavailable

### Low Priority (Enhancements)

6. **Improve PreStop hook error handling**
   - Add timeout configuration
   - Add retry logic
   - Improve logging/observability

7. **Add monitoring for PreStop hook execution**
   - Track decommission/detention duration
   - Expose metrics
   - Alert on failures

---

## 🔍 KEY FINDINGS

### What Works Well

1. ✅ **PDB Integration:** Automatic via Kubernetes Eviction API
2. ✅ **Finalizer System:** Properly blocks deletion until cleanup completes
3. ✅ **Intent Detection:** Annotation-based with ordinal fallback
4. ✅ **PVC Lifecycle:** Correct preservation vs deletion logic
5. ✅ **Per-Pod Eviction:** IngestorCluster/Standalone work correctly

### What Needs Improvement

1. ❌ **PreStop Script Missing:** Critical blocker for decommission/detention
2. ❌ **Decommission/Detention in Wrong Place:** Should be in PreStop, not operator
3. ⚠️ **No Percentage-Based Updates:** All-or-nothing rolling updates
4. ⚠️ **IndexerCluster/SearchHeadCluster:** Removed restart detection, but decommission/detention still in operator code

### Architecture Decision Validation

Your requirements align with best practices:
- ✅ PreStop hooks for pod-local operations (decommission/detention)
- ✅ Finalizers for cluster-wide cleanup (PVC deletion, peer removal)
- ✅ Eviction API for respecting PDB automatically
- ✅ StatefulSet RollingUpdate for automatic pod recreation

---

## 🎯 NEXT STEPS

### Immediate Actions

1. **Create `preStop.sh` script** with:
   - Role detection (read pod labels/env vars)
   - Intent annotation reading
   - Indexer decommission logic
   - Search head detention logic
   - Generic splunk stop for others
   - Proper error handling and logging

2. **Refactor decommission calls:**
   - Remove execution from `indexercluster.go`
   - Keep only waiting/verification in `pod_deletion_handler.go`

3. **Refactor detention calls:**
   - Remove execution from `searchheadclusterpodmanager.go`
   - Implement waiting in `pod_deletion_handler.go`

### Testing Plan

1. Test preStop script locally (simulate pod shutdown)
2. Test with 1 pod restart (verify decommission/detention)
3. Test with scale-down (verify PVC cleanup)
4. Test with percentage-based rolling updates
5. Test PDB violations (ensure proper blocking)

---

## 📊 EFFORT ESTIMATE

- **PreStop Script Creation:** 4-6 hours (including testing)
- **Refactor Decommission/Detention:** 2-3 hours
- **Percentage-Based Updates:** 3-4 hours
- **Testing & Validation:** 4-6 hours
- **Total:** 13-19 hours

---

Generated: 2026-02-19
Branch: spike/CSPL-4530
PR: #1710
