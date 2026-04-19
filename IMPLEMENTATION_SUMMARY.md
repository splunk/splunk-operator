# Implementation Summary - Per-Pod Rolling Restart Enhancements

## Overview

This implementation completes three major enhancements to the per-pod rolling restart mechanism:

1. ✅ Created missing `preStop.sh` script
2. ✅ Refactored decommission/detention to use preStop hooks
3. ✅ Added percentage-based rolling update support

---

## 1. PreStop Hook Script Implementation

### File Created
- **`tools/k8_probes/preStop.sh`** (10KB, executable)

### Features
- **Role-based shutdown logic:**
  - **Indexers:** Decommission with enforce_counts based on intent → splunk stop
  - **Search Heads:** Detention (remove from SHC) → splunk stop
  - **Ingestors, Standalone, CM, LM, MC, Deployer:** Graceful splunk stop only

- **Intent annotation detection:**
  - Reads `splunk.com/pod-intent` from Kubernetes API
  - `scale-down` → decommission/detention with enforce_counts=1 (rebalance)
  - `restart` → decommission/detention with enforce_counts=0 (no rebalance)
  - `serve` → no decommission/detention (default)

- **Status monitoring:**
  - **Indexers:** Polls Cluster Manager for peer status until "Down" or "GracefulShutdown"
  - **Search Heads:** Polls member info until `is_registered=false`
  - Configurable timeouts via `PRESTOP_MAX_WAIT` env var (default: 300s)

- **Error handling:**
  - Retries and fallbacks for API failures
  - Comprehensive logging for debugging
  - Graceful degradation if status checks fail

### Environment Variables Required
- `POD_NAME` - Pod name (from downward API)
- `POD_NAMESPACE` - Pod namespace (from downward API)
- `SPLUNK_ROLE` - Splunk role (already set)
- `SPLUNK_PASSWORD` - Admin password (from secret)
- `SPLUNK_CLUSTER_MANAGER_URL` - CM URL for indexers (already set)

### Pod Configuration Updates
**File:** `pkg/splunk/enterprise/configuration.go`

Added environment variables:
```go
{
    Name: "POD_NAME",
    ValueFrom: &corev1.EnvVarSource{
        FieldRef: &corev1.ObjectFieldSelector{
            FieldPath: "metadata.name",
        },
    },
},
{
    Name: "POD_NAMESPACE",
    ValueFrom: &corev1.EnvVarSource{
        FieldRef: &corev1.ObjectFieldSelector{
            FieldPath: "metadata.namespace",
        },
    },
},
{
    Name: "SPLUNK_PASSWORD",
    ValueFrom: &corev1.EnvVarSource{
        SecretKeyRef: &corev1.SecretKeySelector{
            LocalObjectReference: corev1.LocalObjectReference{
                Name: secretToMount, // Set dynamically
            },
            Key: "password",
        },
    },
}
```

### Termination Grace Period
Already configured in `configuration.go:1154-1159`:
- **Indexers:** 300 seconds (5 minutes) - for decommission + stop
- **Other roles:** 120 seconds (2 minutes) - for graceful stop

---

## 2. Decommission/Detention Refactoring

### Changes Made

#### A. Indexer Decommission

**File:** `pkg/splunk/enterprise/indexercluster.go`

**Before:** Operator executed decommission via API call
```go
// Line 1079 (OLD)
return false, c.DecommissionIndexerClusterPeer(enforceCounts)
```

**After:** Operator only waits for preStop hook to complete decommission
```go
// Line 1068 (NEW)
case "Up":
    // Decommission should be initiated by preStop hook when pod terminates
    // Operator just waits for it to progress
    mgr.log.Info("Waiting for preStop hook to initiate decommission", "peerName", peerName)
    return false, nil
```

**Function updated:** `decommission(ctx context.Context, n int32, enforceCounts bool)`
- Removed API call execution
- Kept status monitoring logic
- Updated comments to reflect new behavior

#### B. Search Head Detention

**File:** `pkg/splunk/enterprise/searchheadclusterpodmanager.go`

**Before:** Operator executed detention via API call
```go
// Line 86 (OLD)
err = c.RemoveSearchHeadClusterMember()
```

**After:** Operator only waits for preStop hook to complete detention
```go
// Lines 85-102 (NEW)
// Pod is quarantined; preStop hook handles detention when pod terminates
// Operator just waits for detention to complete
memberName := GetSplunkStatefulsetPodName(SplunkSearchHead, mgr.cr.GetName(), n)
mgr.log.Info("Waiting for preStop hook to complete detention", "memberName", memberName)

// Check if member is still in cluster consensus
c := mgr.getClient(ctx, n)
info, err := c.GetSearchHeadClusterMemberInfo()
if err != nil {
    mgr.log.Info("Could not get member info, may already be removed", "memberName", memberName, "error", err)
    return true, nil
}

if !info.Registered {
    mgr.log.Info("Member successfully removed from cluster", "memberName", memberName)
    return true, nil
}

mgr.log.Info("Member still registered in cluster, waiting", "memberName", memberName)
return false, nil
```

**Function updated:** `PrepareScaleDown(ctx context.Context, n int32)`
- Removed API call execution
- Added registration check logic
- Updated comments to reflect new behavior

#### C. Pod Deletion Handler

**File:** `pkg/splunk/enterprise/pod_deletion_handler.go`

**Enhanced:** `waitForSearchHeadDetention(ctx context.Context, c splcommon.ControllerClient, pod *corev1.Pod)`

**Before:** Placeholder function
```go
// Lines 301-305 (OLD)
scopedLog.Info("Search head detention verification not implemented yet")
return nil
```

**After:** Full implementation with verification
```go
// Lines 301-336 (NEW)
// Get Splunk admin credentials from secret
secret, err := splutil.GetSecretFromPod(ctx, c, pod.Name, pod.Namespace)
if err != nil {
    scopedLog.Error(err, "Failed to get secret for search head")
    return err
}

// Create Splunk client for the search head pod
splunkClient := splclient.NewSplunkClient(
    fmt.Sprintf("https://%s:8089", pod.Status.PodIP),
    string(secret.Data["splunk_admin_username"]),
    string(secret.Data["password"]),
)

// Check if member is still registered in cluster
memberInfo, err := splunkClient.GetSearchHeadClusterMemberInfo()
if err != nil {
    scopedLog.Info("Could not get member info, assuming detention complete", "error", err.Error())
    return nil
}

// Check registration status
if !memberInfo.Registered {
    scopedLog.Info("Search head successfully removed from cluster")
    return nil
}

// Still registered - detention not complete
scopedLog.Info("Search head still registered in cluster, detention in progress")
return fmt.Errorf("detention not complete, member still registered")
```

**Import added:**
```go
splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
```

### Key Benefits
1. ✅ **Separation of concerns:** PreStop hook handles execution, operator handles verification
2. ✅ **Faster pod termination:** Decommission/detention happens during SIGTERM, not before
3. ✅ **Better error handling:** PreStop failures are visible in pod events
4. ✅ **Consistent behavior:** All pod lifecycle operations in one place (preStop hook)
5. ✅ **Reduced operator complexity:** Less API call orchestration in reconcile loop

---

## 3. Percentage-Based Rolling Update Support

### API Changes

**File:** `api/v4/common_types.go`

Added new configuration types:
```go
// Line 245-263
// RollingUpdateConfig defines configuration for StatefulSet rolling updates
type RollingUpdateConfig struct {
    // MaxPodsUnavailable specifies the maximum number or percentage of pods that can be unavailable during the update.
    // Can be an absolute number (e.g., 1) or a percentage (e.g., "25%").
    // Defaults to 1 if not specified.
    // +optional
    MaxPodsUnavailable string `json:"maxPodsUnavailable,omitempty"`

    // Partition indicates that all pods with an ordinal that is greater than or equal to the partition
    // will be updated when the StatefulSet's .spec.template is updated. All pods with an ordinal that
    // is less than the partition will not be updated, and, even if they are deleted, they will be
    // recreated at the previous version.
    // Useful for canary deployments. Defaults to 0.
    // +optional
    Partition *int32 `json:"partition,omitempty"`
}
```

Added to `CommonSplunkSpec`:
```go
// Line 243-245
// RollingUpdateConfig defines the rolling update strategy for StatefulSets
// +optional
RollingUpdateConfig *RollingUpdateConfig `json:"rollingUpdateConfig,omitempty"`
```

### Implementation

**File:** `pkg/splunk/enterprise/configuration.go`

Added `buildUpdateStrategy` function (lines 834-878):
```go
// buildUpdateStrategy builds the StatefulSet update strategy based on RollingUpdateConfig
func buildUpdateStrategy(spec *enterpriseApi.CommonSplunkSpec, replicas int32) appsv1.StatefulSetUpdateStrategy {
    strategy := appsv1.StatefulSetUpdateStrategy{
        Type: appsv1.RollingUpdateStatefulSetStrategyType,
        RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
            MaxUnavailable: &intstr.IntOrString{
                Type:   intstr.Int,
                IntVal: 1, // Default: 1 pod unavailable at a time
            },
        },
    }

    // Apply custom rolling update config if specified
    if spec.RollingUpdateConfig != nil {
        config := spec.RollingUpdateConfig

        // Set maxPodsUnavailable if specified
        if config.MaxPodsUnavailable != "" {
            // Parse as percentage or absolute number
            if strings.HasSuffix(config.MaxPodsUnavailable, "%") {
                // Percentage value
                strategy.RollingUpdate.MaxUnavailable = &intstr.IntOrString{
                    Type:   intstr.String,
                    StrVal: config.MaxPodsUnavailable,
                }
            } else {
                // Absolute number
                val, err := strconv.ParseInt(config.MaxPodsUnavailable, 10, 32)
                if err == nil && val > 0 {
                    strategy.RollingUpdate.MaxUnavailable = &intstr.IntOrString{
                        Type:   intstr.Int,
                        IntVal: int32(val),
                    }
                }
            }
        }

        // Set partition if specified (for canary deployments)
        if config.Partition != nil && *config.Partition >= 0 && *config.Partition <= replicas {
            strategy.RollingUpdate.Partition = config.Partition
        }
    }

    return strategy
}
```

Updated `getSplunkStatefulSet` to use the function (line 735):
```go
// Build update strategy based on config
updateStrategy := buildUpdateStrategy(spec, replicas)

statefulSet.Spec = appsv1.StatefulSetSpec{
    // ...
    UpdateStrategy: updateStrategy,
    // ...
}
```

### Usage Examples

#### Example 1: Percentage-based rolling updates
```yaml
apiVersion: enterprise.splunk.com/v4
kind: IndexerCluster
metadata:
  name: example
spec:
  replicas: 10
  rollingUpdateConfig:
    maxPodsUnavailable: "25%"  # Allow up to 2-3 pods down at once (25% of 10)
```

#### Example 2: Absolute number
```yaml
apiVersion: enterprise.splunk.com/v4
kind: SearchHeadCluster
metadata:
  name: example
spec:
  replicas: 5
  rollingUpdateConfig:
    maxPodsUnavailable: "2"  # Allow up to 2 pods down at once
```

#### Example 3: Canary deployment with partition
```yaml
apiVersion: enterprise.splunk.com/v4
kind: IngestorCluster
metadata:
  name: example
spec:
  replicas: 10
  rollingUpdateConfig:
    partition: 8  # Only update pods 8 and 9 (ordinals >= 8)
    maxPodsUnavailable: "1"
```

### Benefits
1. ✅ **Faster rollouts:** Update multiple pods simultaneously
2. ✅ **Flexible control:** Choose between percentage and absolute numbers
3. ✅ **Canary deployments:** Test updates on subset of pods first
4. ✅ **Backward compatible:** Defaults to existing behavior (1 pod at a time)

---

## Files Modified

### New Files
1. ✅ `tools/k8_probes/preStop.sh` - PreStop lifecycle hook script

### Modified Files
1. ✅ `pkg/splunk/enterprise/configuration.go` - Pod env vars, update strategy
2. ✅ `pkg/splunk/enterprise/indexercluster.go` - Refactored decommission
3. ✅ `pkg/splunk/enterprise/searchheadclusterpodmanager.go` - Refactored detention
4. ✅ `pkg/splunk/enterprise/pod_deletion_handler.go` - Implemented detention verification
5. ✅ `api/v4/common_types.go` - Added RollingUpdateConfig types

---

## Testing Checklist

### PreStop Hook Testing
- [ ] Test indexer decommission on scale-down (enforce_counts=1)
- [ ] Test indexer decommission on restart (enforce_counts=0)
- [ ] Test search head detention on scale-down
- [ ] Test search head detention on restart
- [ ] Test graceful stop for ingestor/standalone
- [ ] Test timeout handling (PRESTOP_MAX_WAIT)
- [ ] Test with missing env vars (graceful degradation)

### Decommission/Detention Refactoring Testing
- [ ] Verify indexer decommission completes before pod deletion
- [ ] Verify search head detention completes before pod deletion
- [ ] Verify PVCs are preserved on restart
- [ ] Verify PVCs are deleted on scale-down
- [ ] Test finalizer cleanup after decommission/detention

### Rolling Update Testing
- [ ] Test percentage-based maxPodsUnavailable (e.g., "25%")
- [ ] Test absolute maxPodsUnavailable (e.g., "2")
- [ ] Test partition for canary deployments
- [ ] Test default behavior (no config = 1 pod at a time)
- [ ] Verify PDB is respected with custom maxPodsUnavailable

---

## Deployment Notes

### Prerequisites
- Kubernetes 1.21+ (for StatefulSet RollingUpdate.MaxUnavailable)
- Splunk Enterprise 8.x+
- Operator with finalizer support

### Migration from Previous Version
1. No CRD changes required for existing resources
2. New `rollingUpdateConfig` field is optional
3. PreStop hook is automatically injected into all pods
4. Existing pods will be updated on next rolling restart

### Monitoring
- Watch pod events for preStop hook execution
- Monitor StatefulSet rolling update progress
- Check pod logs for decommission/detention status
- Verify PDB disruptions match maxPodsUnavailable

---

## Known Limitations

1. **MaxPodsUnavailable percentage:** Requires Kubernetes 1.21+
2. **PreStop timeout:** Limited by terminationGracePeriodSeconds (300s for indexers, 120s for others)
3. **Partition:** Only supports ordinal-based canary (not label-based)
4. **Decommission verification:** Operator polls status after preStop completes

---

## Future Enhancements

1. **Dynamic timeout adjustment:** Calculate based on bucket count/size
2. **Progressive rollouts:** Automatically advance partition based on health checks
3. **Blue/green deployments:** Support for multiple StatefulSet versions
4. **Rollback on failure:** Automatic rollback if decommission/detention fails
5. **Metrics exposure:** Prometheus metrics for decommission/detention duration

---

Generated: 2026-02-19
Branch: spike/CSPL-4530
PR: #1710
