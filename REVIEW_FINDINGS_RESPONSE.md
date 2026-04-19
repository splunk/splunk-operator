# Review Findings - Response and Fixes

## Summary

Review identified 7 issues (3 High, 3 Medium, 1 Low) plus 1 open question. This document tracks our response and fixes for each.

---

## ✅ HIGH PRIORITY ISSUES

### Issue #1: Eviction RBAC in Wrong API Group [FIXED]

**Finding:**
```
RBAC annotations grant pods/eviction under core group, but eviction is a policy API resource.
Files: standalone_controller.go (lines 67-71), ingestorcluster_controller.go (lines 56-60), role.yaml (lines 33-45)
```

**Root Cause:**
- `//+kubebuilder:rbac:groups=core,resources=pods/eviction,verbs=create`
- Should be `groups=policy` not `groups=core`
- Eviction API is `policy/v1.Eviction`, not `core/v1`

**Impact:**
- Runtime errors when calling Eviction API
- Pods cannot be evicted for restart_required scenarios
- RBAC forbidden errors break automatic restarts

**Fix Applied:**
```go
// BEFORE (WRONG):
//+kubebuilder:rbac:groups=core,resources=pods/eviction,verbs=create

// AFTER (CORRECT):
//+kubebuilder:rbac:groups=policy,resources=pods/eviction,verbs=create
```

**Files Changed:**
- `internal/controller/standalone_controller.go` line 70
- `internal/controller/ingestorcluster_controller.go` line 59
- `config/rbac/role.yaml` (regenerated)

**Verification:**
```bash
# RBAC now correctly grants:
- apiGroups:
  - policy
  resources:
  - pods/eviction
  verbs:
  - create
```

**Status:** ✅ FIXED in this commit

---

### Issue #2: Scale-Down Intent Never Applied [FALSE POSITIVE / ALREADY FIXED]

**Finding:**
```
Scale-down intent is never applied to pods. The only explicit scale-down marker is MarkPodsForScaleDown,
but there are no call sites. Pods keep splunk.com/pod-intent=serve, and preStop will default to "serve"
even for scale-downs, so indexers won't rebalance (enforce_counts=1) on scale-down.
Files: pod_deletion_handler.go (lines 498-543), configuration.go (lines 799-817), preStop.sh (lines 40-49), preStop.sh (lines 131-142).
```

**Analysis:**
This is a **FALSE POSITIVE**. Scale-down intent IS applied.

**Evidence:**
1. **Function exists and is called:**
   ```go
   // pkg/splunk/splkcontroller/statefulset.go:156
   err = markPodForScaleDown(ctx, c, statefulSet, n)
   ```

2. **Call site is correct:**
   ```go
   // Line 139: Detect scale-down
   if readyReplicas > desiredReplicas {
       n := readyReplicas - 1  // New replica count

       // Line 156: Mark pod BEFORE scaling down
       err = markPodForScaleDown(ctx, c, statefulSet, n)

       // Line 164: Scale down StatefulSet
       *statefulSet.Spec.Replicas = n
       err = splutil.UpdateResource(ctx, c, statefulSet)
   }
   ```

3. **Implementation marks correct pod:**
   ```go
   // pkg/splunk/splkcontroller/statefulset.go:450-485
   func markPodForScaleDown(..., newReplicas int32) error {
       podName := fmt.Sprintf("%s-%d", statefulSet.Name, newReplicas)
       // Gets pod with ordinal = newReplicas (the one being deleted)
       pod.Annotations["splunk.com/pod-intent"] = "scale-down"
       c.Update(ctx, pod)
   }
   ```

4. **Test coverage exists:**
   ```go
   // TestScaleDownWithIntentAnnotation verifies:
   // 1. Pod ordinal 2 exists
   // 2. Scaling 3 → 2 replicas
   // 3. Pod 2 marked with "scale-down" intent
   // 4. preStop.sh reads this and sets enforce_counts=1
   ```

**Why reviewer may have missed this:**
- Function named `markPodForScaleDown` (lowercase) vs `MarkPodsForScaleDown` (uppercase exported version)
- Inline implementation in `statefulset.go` to avoid import cycle
- Comment "V3 FIX #1" indicates this was added later

**PreStop Integration:**
```bash
# preStop.sh lines 40-49
pod_intent=$(get_pod_intent)  # Reads splunk.com/pod-intent annotation

# preStop.sh lines 131-142
if [ "$intent" = "scale-down" ]; then
    enforce_counts="1"  # Rebalance buckets
else
    enforce_counts="0"  # No rebalancing
fi
```

**Status:** ✅ ALREADY IMPLEMENTED (no changes needed)

---

### Issue #3: preStop Cluster Manager URL Malformed [HIGH PRIORITY - NEEDS FIX]

**Finding:**
```
SPLUNK_CLUSTER_MANAGER_URL is set to a service name without scheme/port, but preStop.sh uses it
as a full URL. It also appends an undefined SPLUNK_CLUSTER_MANAGER_SERVICE to the peer name,
which makes peer lookup fail and can falsely report decommission complete.
Files: configuration.go (lines 1151-1155), preStop.sh (lines 97-104), preStop.sh (lines 152-170).
```

**Root Cause Analysis:**

1. **URL Construction Issue:**
   ```go
   // configuration.go line 1151
   {
       Name:  "SPLUNK_CLUSTER_MANAGER_URL",
       Value: GetSplunkServiceName(SplunkClusterManager, cr.GetName(), false),
       // Returns: "splunk-cluster-splunk-cluster-manager-service"
       // Missing: https:// and :8089 port
   }
   ```

2. **preStop.sh expects full URL:**
   ```bash
   # preStop.sh line 99
   response=$(curl -s -k -u "${SPLUNK_USER}:${SPLUNK_PASSWORD}" \
       "${cluster_manager_url}/services/cluster/manager/peers?output_mode=json" 2>/dev/null)
   # This fails because cluster_manager_url="service-name" not "https://service-name:8089"
   ```

3. **Undefined variable:**
   ```bash
   # preStop.sh line 168
   peer_status=$(get_indexer_peer_status "$cm_url" "${POD_NAME}.${SPLUNK_CLUSTER_MANAGER_SERVICE}")
   # SPLUNK_CLUSTER_MANAGER_SERVICE is never set!
   ```

**Impact:**
- Peer status check always fails
- Decommission verification doesn't work
- May falsely report decommission complete
- Indexers may be terminated before buckets are replicated

**Fix Required:**
```go
// Option 1: Construct full URL in configuration.go
{
    Name:  "SPLUNK_CLUSTER_MANAGER_URL",
    Value: fmt.Sprintf("https://%s:8089",
        GetSplunkServiceName(SplunkClusterManager, cr.GetName(), false)),
}

// Option 2: Set separate variables
{
    Name:  "SPLUNK_CLUSTER_MANAGER_SERVICE",
    Value: GetSplunkServiceName(SplunkClusterManager, cr.GetName(), false),
},
{
    Name:  "SPLUNK_CLUSTER_MANAGER_PORT",
    Value: "8089",
},
```

**Recommended Fix:** Option 1 (full URL) - simpler and less error-prone

**Status:** 🔴 NEEDS FIX (critical for indexer decommission)

---

## ⚠️ MEDIUM PRIORITY ISSUES

### Issue #4: preStop Timeout Exceeds Grace Period [MEDIUM - NEEDS FIX]

**Finding:**
```
preStop max wait exceeds termination grace period. The script can wait up to 300s for decommission
and then another 300s for splunk stop, but non-indexer pods only get a 120s termination grace period.
Kubelet will SIGKILL before the hook finishes, so cleanup can be cut short.
Files: preStop.sh (lines 16-20), preStop.sh (lines 162-192), preStop.sh (lines 246-267), configuration.go (lines 1183-1192).
```

**Root Cause:**
```bash
# preStop.sh line 29
MAX_WAIT_SECONDS="${PRESTOP_MAX_WAIT:-300}"  # Default 5 minutes

# But configuration.go lines 1183-1192 sets:
TerminationGracePeriodSeconds = 120  # Only 2 minutes for non-indexers!
```

**Timeline for Non-Indexer (Search Head, Standalone, etc.):**
```
T=0s    : Pod receives SIGTERM
T=0s    : preStop hook starts
T=0-120s: preStop waits for detention/decommission (max 300s configured!)
T=120s  : Kubelet SIGKILL (grace period exceeded)
T=120s  : preStop hook killed mid-execution
T=120s  : Splunk process killed without graceful shutdown
```

**Impact:**
- Search heads may not complete detention
- Splunk processes killed without graceful shutdown
- Data in write buffers may be lost
- Connections not cleaned up properly

**Fix Options:**

**Option 1: Align timeout with grace period (RECOMMENDED)**
```bash
# preStop.sh
if [ "$SPLUNK_ROLE" = "splunk_indexer" ]; then
    MAX_WAIT_SECONDS="${PRESTOP_MAX_WAIT:-270}"  # 4.5 min (leave 30s for splunk stop)
else
    MAX_WAIT_SECONDS="${PRESTOP_MAX_WAIT:-90}"   # 1.5 min (leave 30s for splunk stop)
fi
```

**Option 2: Increase grace period for all roles**
```go
// configuration.go
if instanceType == SplunkIndexer {
    TerminationGracePeriodSeconds = 360  // 6 minutes (300s decom + 60s buffer)
} else {
    TerminationGracePeriodSeconds = 180  // 3 minutes (120s operation + 60s buffer)
}
```

**Option 3: Read grace period from pod spec (MOST ROBUST)**
```bash
# preStop.sh
GRACE_PERIOD=$(curl -s --cacert /var/run/secrets/kubernetes.io/serviceaccount/ca.crt \
    -H "Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
    "https://kubernetes.default.svc/api/v1/namespaces/${POD_NAMESPACE}/pods/${POD_NAME}" | \
    grep -o '"terminationGracePeriodSeconds":[0-9]*' | cut -d':' -f2)
MAX_WAIT_SECONDS=$((GRACE_PERIOD - 30))  # Leave 30s buffer for splunk stop
```

**Recommended:** Option 1 (simplest) + Option 2 (increase grace period buffer)

**Status:** ⚠️ NEEDS FIX (prevents graceful shutdown)

---

### Issue #5: PDB Selector Mismatch with ClusterManagerRef [MEDIUM - NEEDS INVESTIGATION]

**Finding:**
```
PDB selector can miss Indexer pods when ClusterManagerRef is set. Pods use labels derived from
partOfIdentifier=ClusterManagerRef.Name, but PDBs are built with partOfIdentifier="",
so selectors won't match in that case. PDBs end up ineffective for those indexer clusters.
Files: configuration.go (lines 703-714), util.go (lines 2623-2641).
```

**Analysis:**

**Scenario:** IndexerCluster with ClusterManagerRef pointing to external CM
```yaml
apiVersion: enterprise.splunk.com/v4
kind: IndexerCluster
metadata:
  name: idx-cluster
spec:
  clusterManagerRef:
    name: external-cm
  replicas: 10
```

**Pod Labels (configuration.go:703-714):**
```go
labels = getSplunkLabels(
    cr.GetName(),                    // "idx-cluster"
    instanceType,                    // "indexer"
    cr.Spec.ClusterManagerRef.Name,  // "external-cm" ← partOfIdentifier
)
// Result: app.kubernetes.io/instance: splunk-external-cm-indexer
```

**PDB Selector (util.go:2623-2641):**
```go
labels := getSplunkLabels(
    cr.GetName(),      // "idx-cluster"
    instanceType,      // "indexer"
    "",                // "" ← empty partOfIdentifier!
)
// Result: app.kubernetes.io/instance: splunk-idx-cluster-indexer
```

**Mismatch:**
- Pod label: `app.kubernetes.io/instance: splunk-external-cm-indexer`
- PDB selector: `app.kubernetes.io/instance: splunk-idx-cluster-indexer`
- **PDB does not select any pods!**

**Impact:**
- PDB doesn't protect pods during eviction
- Multiple pods can be disrupted simultaneously
- Availability guarantees not enforced

**Fix Required:**
```go
// util.go ApplyPodDisruptionBudget()
func ApplyPodDisruptionBudget(
    ctx context.Context,
    client client.Client,
    cr splcommon.MetaObject,
    instanceType InstanceType,
    replicas int32,
) error {
    // ... existing code ...

    // FIX: Use same partOfIdentifier logic as pod labels
    var partOfIdentifier string

    // Type assertion to get ClusterManagerRef
    switch v := cr.(type) {
    case *enterpriseApi.IndexerCluster:
        if v.Spec.ClusterManagerRef.Name != "" {
            partOfIdentifier = v.Spec.ClusterManagerRef.Name
        }
    }

    // Get labels with correct partOfIdentifier
    labels := getSplunkLabels(cr.GetName(), instanceType, partOfIdentifier)

    // ... rest of PDB creation ...
}
```

**Status:** 🔴 NEEDS FIX (PDB ineffective for ClusterManagerRef scenarios)

---

### Issue #6: Partition Blocks Eviction Forever [MEDIUM - NEEDS FIX]

**Finding:**
```
Eviction suppression can block forever when RollingUpdateConfig.Partition is used.
UpdatedReplicas < Spec.Replicas is always true when a partition is set, so restart_required evictions never happen.
Files: standalone.go (lines 363-377), ingestorcluster.go (lines 870-885).
```

**Root Cause:**
```go
// standalone.go lines 374-377
if statefulSet.Status.UpdatedReplicas < *statefulSet.Spec.Replicas {
    scopedLog.Info("StatefulSet rolling update in progress, skipping pod eviction")
    return nil
}
```

**Problem with Partition:**
```yaml
# User sets partition for canary deployment
spec:
  rollingUpdateConfig:
    partition: 8  # Only update pods 8-9
  replicas: 10
```

**StatefulSet Status:**
```yaml
status:
  replicas: 10
  updatedReplicas: 2    # Only pods 8-9 updated
  readyReplicas: 10
```

**Result:**
- `updatedReplicas (2) < replicas (10)` is ALWAYS true
- Eviction is blocked forever
- Pods 0-7 never get restarted even if restart_required

**Impact:**
- Canary deployments break restart_required feature
- Pods with config changes never restart
- Manual intervention required

**Fix Required:**
```go
// standalone.go checkAndEvictStandaloneIfNeeded()
if statefulSet.Status.UpdatedReplicas < *statefulSet.Spec.Replicas {
    // Check if partition is set
    if statefulSet.Spec.UpdateStrategy.RollingUpdate != nil &&
       statefulSet.Spec.UpdateStrategy.RollingUpdate.Partition != nil {

        partition := *statefulSet.Spec.UpdateStrategy.RollingUpdate.Partition

        // If all pods >= partition are updated, rolling update is "complete" for its partition
        // Allow eviction of pods < partition
        if statefulSet.Status.UpdatedReplicas >= (*statefulSet.Spec.Replicas - partition) {
            scopedLog.Info("Partition-based update complete, allowing eviction of non-partitioned pods",
                "partition", partition,
                "updatedReplicas", statefulSet.Status.UpdatedReplicas)
            // Fall through to eviction logic
        } else {
            scopedLog.Info("Partition-based rolling update in progress, skipping eviction",
                "partition", partition,
                "updatedReplicas", statefulSet.Status.UpdatedReplicas)
            return nil
        }
    } else {
        // No partition - normal rolling update in progress
        scopedLog.Info("StatefulSet rolling update in progress, skipping pod eviction")
        return nil
    }
}
```

**Status:** 🔴 NEEDS FIX (breaks restart_required with canary deployments)

---

## ℹ️ LOW PRIORITY ISSUES

### Issue #7: PDB Violation Detection is Brittle [LOW - IMPROVEMENT]

**Finding:**
```
PDB violation detection is a brittle string match. strings.Contains(err.Error(), "Cannot evict pod") is fragile;
apierrors.IsTooManyRequests (429) is more reliable.
Files: standalone.go (lines 469-472), ingestorcluster.go (lines 980-984).
```

**Current Implementation:**
```go
// standalone.go:469-472
func isPDBViolationStandalone(err error) bool {
    return err != nil && strings.Contains(err.Error(), "Cannot evict pod")
}
```

**Problem:**
- String matching is fragile and locale-dependent
- Error message could change in future Kubernetes versions
- Doesn't match all PDB violation scenarios

**Better Implementation:**
```go
import (
    k8serrors "k8s.io/apimachinery/pkg/api/errors"
)

func isPDBViolationStandalone(err error) bool {
    // Eviction API returns 429 Too Many Requests when PDB blocks eviction
    return k8serrors.IsTooManyRequests(err)
}
```

**Why 429?**
- Kubernetes Eviction API returns HTTP 429 when PDB budget is exhausted
- `apierrors.IsTooManyRequests()` checks for `StatusReasonTooManyRequests`
- More reliable than string matching

**Status:** ✅ EASY FIX (low priority, nice-to-have improvement)

---

## ❓ OPEN QUESTIONS

### Question: preStop Pod Intent RBAC Dependency

**Question:**
```
Do Splunk pods' service accounts have RBAC to GET their own Pod? If not, preStop always falls back
to "serve," which breaks scale-down decommission. If the intent is to avoid that RBAC dependency,
a downward-API env var for splunk.com/pod-intent would be more reliable.
```

**Current Implementation:**
```bash
# preStop.sh lines 40-49
get_pod_intent() {
    intent=$(curl -s --max-time 10 --cacert /var/run/secrets/kubernetes.io/serviceaccount/ca.crt \
        -H "Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
        "https://kubernetes.default.svc/api/v1/namespaces/${POD_NAMESPACE}/pods/${POD_NAME}" \
        2>/dev/null | grep -o '"splunk.com/pod-intent":"[^"]*"' | cut -d'"' -f4)

    if [ -z "$intent" ]; then
        log_warn "Could not read pod intent annotation, defaulting to 'serve'"
        echo "serve"
    fi
}
```

**RBAC Required:**
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: splunk-pod-reader
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get"]
```

**Problem:**
- If RBAC not granted, preStop always defaults to "serve"
- Scale-down won't rebalance buckets (enforce_counts=0 instead of 1)
- Data loss risk during scale-down

**Solution Options:**

**Option 1: Add RBAC (CURRENT APPROACH)**
```yaml
# Add to role.yaml
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get"]
  # Splunk pods need to read their own pod metadata
```

**Pros:** Works with current code, no changes needed
**Cons:** Additional RBAC permission required, may be blocked by security policies

**Option 2: Use Downward API (RECOMMENDED)**
```go
// configuration.go - add environment variable
{
    Name: "SPLUNK_POD_INTENT",
    ValueFrom: &corev1.EnvVarSource{
        FieldRef: &corev1.ObjectFieldSelector{
            FieldPath: "metadata.annotations['splunk.com/pod-intent']",
        },
    },
}
```

```bash
# preStop.sh - read from environment
get_pod_intent() {
    local intent="${SPLUNK_POD_INTENT:-serve}"
    echo "$intent"
}
```

**Pros:**
- No RBAC required
- More reliable (no API calls, no timeouts)
- Faster (no network latency)
- Works in restricted environments

**Cons:**
- Requires code change to add env var
- Annotation must be set before pod starts (but we already do this)

**Recommendation:** Implement Option 2 (Downward API)
- Simpler and more reliable
- No additional RBAC required
- Eliminates API call failure mode

**Status:** 💡 RECOMMENDATION: Use Downward API instead of API call

---

## Summary of Required Fixes

| Issue | Priority | Status | Complexity |
|-------|----------|--------|------------|
| #1 Eviction RBAC API Group | HIGH | ✅ FIXED | Easy |
| #2 Scale-Down Intent | HIGH | ✅ ALREADY IMPLEMENTED | N/A |
| #3 Cluster Manager URL | HIGH | 🔴 NEEDS FIX | Medium |
| #4 preStop Timeout | MEDIUM | 🔴 NEEDS FIX | Easy |
| #5 PDB Selector Mismatch | MEDIUM | 🔴 NEEDS INVESTIGATION | Medium |
| #6 Partition Blocks Eviction | MEDIUM | 🔴 NEEDS FIX | Medium |
| #7 PDB Violation Detection | LOW | ✅ EASY FIX | Easy |
| Open Q: Pod Intent RBAC | N/A | 💡 RECOMMENDATION | Easy |

## Next Steps

1. ✅ Fix Issue #1 (Eviction RBAC) - DONE
2. 🔴 Fix Issue #3 (Cluster Manager URL) - HIGH PRIORITY
3. 🔴 Fix Issue #4 (preStop Timeout) - MEDIUM PRIORITY
4. 🔴 Investigate Issue #5 (PDB Selector) - MEDIUM PRIORITY
5. 🔴 Fix Issue #6 (Partition Eviction) - MEDIUM PRIORITY
6. ✅ Fix Issue #7 (PDB Detection) - LOW PRIORITY
7. 💡 Implement Downward API for pod intent - RECOMMENDED

**Estimated Effort:**
- Critical fixes (#3, #4, #5, #6): 4-6 hours
- Nice-to-have improvements (#7, Open Q): 1-2 hours
- Total: 1 day of work

**Risk Assessment:**
- Issue #3 is critical - indexer decommission doesn't work without it
- Issue #4 can cause data loss during graceful shutdown
- Issue #5 breaks PDB protection in specific configurations
- Issue #6 breaks restart_required with canary deployments
