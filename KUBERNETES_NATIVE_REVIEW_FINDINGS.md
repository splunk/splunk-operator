# Kubernetes Native Patterns Review - Findings & Action Plan

## Executive Summary

The per-pod rolling restart implementation demonstrates **strong Kubernetes-native design** with good use of PDBs, RollingUpdate, Finalizers, PreStop hooks, and Eviction API. However, several **critical issues** need immediate attention for production readiness.

**Overall Score: 7/10** - Good foundation, needs refinement for edge cases and error handling.

---

## Critical Issues (Fix Immediately)

### 1. ⚠️ CRITICAL: Duplicate Finalizer Prevention
**Location:** `pkg/splunk/enterprise/configuration.go:805-808`

**Problem:**
```go
// Current code - NO duplicate check!
statefulSet.Spec.Template.ObjectMeta.Finalizers = append(
    statefulSet.Spec.Template.ObjectMeta.Finalizers,
    "splunk.com/pod-cleanup",
)
```

**Impact:**
- Each reconcile appends another copy of the finalizer
- Finalizer handler called multiple times (2x, 3x, Nx)
- Cleanup operations run redundantly
- Pod deletion delayed

**Fix Required:**
```go
// Check for existence before appending
finalizer := "splunk.com/pod-cleanup"
if !hasFinalizer(statefulSet.Spec.Template.ObjectMeta.Finalizers, finalizer) {
    statefulSet.Spec.Template.ObjectMeta.Finalizers = append(
        statefulSet.Spec.Template.ObjectMeta.Finalizers,
        finalizer,
    )
}
```

**Priority:** CRITICAL
**Effort:** Low (15 minutes)
**Risk if Not Fixed:** High - Pod deletions hang, cleanup runs multiple times

---

### 2. ⚠️ CRITICAL: Pod Eviction vs RollingUpdate Conflict
**Location:** `pkg/splunk/enterprise/ingestorcluster.go:369-375` (documented but not prevented)

**Problem:**
Two INDEPENDENT restart mechanisms can run simultaneously:
1. **StatefulSet RollingUpdate** (Kubernetes-managed) - for template changes/secrets
2. **Pod Eviction** (operator-managed) - for restart_required flags

**Example Scenario:**
```
1. ConfigMap changes → StatefulSet RollingUpdate starts
2. Pod reports restart_required → Operator evicts pod
3. Both mechanisms try to terminate SAME pod
4. PDB sees 2 pods down → VIOLATES minAvailable!
```

**Fix Required:**
```go
// Before evicting pods, check if RollingUpdate in progress
if isRollingUpdateInProgress(statefulSet) {
    scopedLog.Info("StatefulSet rolling update in progress, skipping pod eviction")
    return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
}
```

**Priority:** CRITICAL
**Effort:** Medium (1-2 hours)
**Risk if Not Fixed:** High - PDB violations, simultaneous pod terminations, availability impact

---

### 3. ⚠️ HIGH: Incomplete SearchHeadCluster Pod Eviction
**Location:** `pkg/splunk/enterprise/searchheadcluster.go:787`

**Problem:**
- Documentation mentions "per-pod eviction like IngestorCluster"
- **But eviction logic NOT FOUND in searchheadcluster.go**
- Only RollingUpdate mechanism present
- restart_required detection removed (correct) but no alternative

**Impact:**
- SearchHeadCluster cannot automatically restart for cloud config changes
- Users must manually trigger restarts
- Inconsistent behavior across cluster types

**Status:**
- This may be **INTENTIONAL** since Deployer + Captain handle restarts
- Need clarification from user

**Priority:** HIGH
**Effort:** Depends on intent
**Risk if Not Fixed:** Medium - Feature gap vs other cluster types

---

## High Priority Issues (Fix Soon)

### 4. ⚠️ Pod Intent Annotation Fetch Has No Timeout
**Location:** `tools/k8_probes/preStop.sh:39-52`

**Problem:**
```bash
# No timeout specified - could hang for 300 seconds!
curl -s --cacert /var/run/secrets/kubernetes.io/serviceaccount/ca.crt \
    -H "Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
    "https://kubernetes.default.svc/api/v1/namespaces/${POD_NAMESPACE}/pods/${POD_NAME}"
```

**Impact:**
- PreStop hook hangs if API server slow/unavailable
- Exceeds terminationGracePeriodSeconds (300s for indexers, 120s for others)
- Pod forcefully killed with SIGKILL
- Decommission/detention incomplete

**Fix Required:**
```bash
curl -s --max-time 10 --cacert ... # Add 10 second timeout
```

**Priority:** HIGH
**Effort:** Low (5 minutes)
**Risk if Not Fixed:** High - PreStop hooks can hang, causing forced pod kills

---

### 5. ⚠️ Missing Environment Variable Validation in PreStop
**Location:** `tools/k8_probes/preStop.sh:24-34`

**Problem:**
- Script assumes env vars like `SPLUNK_CLUSTER_MANAGER_URL` are set
- No validation before use
- Decommission/detention may fail silently

**Fix Required:**
```bash
# Validate required env vars at startup
if [ "$SPLUNK_ROLE" = "splunk_indexer" ] && [ -z "$SPLUNK_CLUSTER_MANAGER_URL" ]; then
    log_error "SPLUNK_CLUSTER_MANAGER_URL not set for indexer role"
    exit 1
fi
```

**Priority:** HIGH
**Effort:** Low (30 minutes)
**Risk if Not Fixed:** Medium - Silent failures, hard to debug

---

### 6. ⚠️ PreStop Decommission Timeout Returns Success
**Location:** `tools/k8_probes/preStop.sh:191`

**Problem:**
```bash
# After timeout, returns 0 (success) even if decommission incomplete!
log_warn "Decommission did not complete within ${MAX_WAIT_SECONDS}s, proceeding anyway"
return 0  # ← Should return error!
```

**Impact:**
- Bucket migration incomplete
- Peer state inconsistent
- Data loss risk during scale-down

**Fix Required:**
```bash
log_error "Decommission timeout after ${MAX_WAIT_SECONDS}s"
return 1  # Signal failure so operator can investigate
```

**Priority:** HIGH
**Effort:** Low (5 minutes)
**Risk if Not Fixed:** Medium - Data integrity issues

---

## Medium Priority Issues

### 7. PDB MinAvailable Blocks Single-Replica Deployments
**Location:** `pkg/splunk/enterprise/util.go:2618-2620`

**Problem:**
```go
minAvailable := replicas - 1
if minAvailable < 1 {
    minAvailable = 1  // ← Blocks ALL evictions for single-pod!
}
```

**Impact:**
- Single-pod deployments cannot be evicted
- Pod eviction always fails with PDB violation
- Rolling restarts hang

**Fix Required:**
```go
minAvailable := replicas - 1
if replicas <= 1 {
    minAvailable = 0  // Allow eviction for single replica
}
```

**Priority:** MEDIUM
**Effort:** Low (10 minutes)

---

### 8. Missing Update Staleness Detection
**Location:** `pkg/splunk/splkcontroller/statefulset.go:205-220`

**Problem:**
- No timeout for rolling updates
- If update stalls (preStop hangs), stays in PhaseUpdating forever
- No alert or escalation

**Fix Required:**
```go
// Track update start time
if statefulSet.Status.UpdatedReplicas < statefulSet.Status.Replicas {
    updateAge := time.Since(cr.Status.LastUpdateTime)
    if updateAge > 30*time.Minute {
        return enterpriseApi.PhaseError, fmt.Errorf("rolling update stalled for %v", updateAge)
    }
    return enterpriseApi.PhaseUpdating, nil
}
```

**Priority:** MEDIUM
**Effort:** Medium (1 hour)

---

### 9. Finalizer Cleanup Can Block Forever
**Location:** `pkg/splunk/enterprise/pod_deletion_handler.go:212-258`

**Problem:**
```go
// If Cluster Manager unreachable, blocks pod deletion forever!
peers, err := cmClient.GetClusterManagerPeers()
if err != nil {
    return err  // Pod never deleted!
}
```

**Fix Required:**
```go
// Add timeout and fallback
ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
defer cancel()

peers, err := cmClient.GetClusterManagerPeers()
if err != nil {
    if errors.Is(ctx.Err(), context.DeadlineExceeded) {
        log.Warn("Timeout waiting for CM, allowing deletion anyway")
        return nil  // Allow deletion
    }
    return err
}
```

**Priority:** MEDIUM
**Effort:** Medium (1 hour)

---

## Summary of All Issues

| Issue | Priority | Effort | Risk | Status |
|-------|----------|--------|------|--------|
| Duplicate finalizer | CRITICAL | Low | High | Not Fixed |
| Eviction vs RollingUpdate conflict | CRITICAL | Medium | High | Not Fixed |
| SearchHeadCluster eviction | HIGH | TBD | Medium | Needs clarification |
| PreStop API timeout | HIGH | Low | High | Not Fixed |
| PreStop env validation | HIGH | Low | Medium | Not Fixed |
| Decommission timeout | HIGH | Low | Medium | Not Fixed |
| PDB single replica | MEDIUM | Low | Medium | Not Fixed |
| Update staleness | MEDIUM | Medium | Low | Not Fixed |
| Finalizer cleanup timeout | MEDIUM | Medium | Medium | Not Fixed |

---

## What's Working Well ✓

1. **PodDisruptionBudget** - Proper use of minAvailable (except edge case)
2. **Eviction API** - Correctly uses Eviction API instead of direct delete
3. **Finalizer Pattern** - Proper ordering and cleanup logic
4. **PreStop Hooks** - Correct lifecycle hook usage
5. **Role-Specific Grace Periods** - Indexers get 5min, others 2min
6. **Intent Annotations** - Good pattern for scale-down detection
7. **Separate Pod Controller** - Good separation of concerns

---

## Recommendations

### Immediate (Before Production):
1. ✅ Fix duplicate finalizer check
2. ✅ Add mutual exclusion between eviction and RollingUpdate
3. ⚠️ Clarify SearchHeadCluster eviction intent
4. ✅ Add timeouts to preStop script
5. ✅ Add env var validation to preStop

### Short-term (Next Sprint):
1. Fix PDB single-replica edge case
2. Add update staleness detection
3. Add finalizer cleanup timeout
4. Improve error reporting with Kubernetes events

### Long-term (Future):
1. Add eviction dry-run capability
2. Implement progressive rollout with partition
3. Add metrics and observability
4. Create troubleshooting runbook

---

## Questions for User

1. **SearchHeadCluster eviction:** Should SearchHeadCluster support automatic pod eviction for restart_required, or is Deployer+Captain handling sufficient?

2. **PDB configuration:** Should we support custom PDB configurations, or is the current `replicas - 1` formula sufficient?

3. **Timeout values:** Are the current grace periods appropriate?
   - Indexers: 300s (5 min)
   - Others: 120s (2 min)
   - PreStop max wait: 300s

4. **Error handling:** Should we force-delete pods after cleanup timeout, or keep them blocked until manual intervention?

---

Generated: 2026-02-19
Branch: spike/CSPL-4530
PR: #1710
