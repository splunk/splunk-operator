# User-Created PodDisruptionBudget (PDB) Support

## Overview

The Splunk Operator now **respects user-created PodDisruptionBudgets** and will NOT overwrite them. This allows customers to define custom availability requirements that supersede the operator's default PDB settings.

## How It Works

### Operator-Managed PDBs

By default, the operator creates and manages PodDisruptionBudgets for all Splunk cluster types:

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: splunk-<cr-name>-<type>-pdb
  namespace: <namespace>
  ownerReferences:
    - apiVersion: enterprise.splunk.com/v4
      kind: Standalone  # or IndexerCluster, SearchHeadCluster, IngestorCluster
      name: <cr-name>
      uid: <cr-uid>
      controller: true
spec:
  minAvailable: <replicas - 1>
  selector:
    matchLabels:
      app.kubernetes.io/instance: splunk-<cr-name>-<type>
```

**Default Behavior:**
- `minAvailable = replicas - 1` (allows 1 pod to be disrupted at a time)
- For single-replica: `minAvailable = 0` (allow eviction)
- Automatically updated when replicas change
- Deleted when CR is deleted (via owner reference)

### User-Created PDBs

If a customer creates a PDB with the same name as the operator would use, the operator detects this and **preserves the user's configuration**.

**Detection Logic:**
The operator checks if the PDB has an `ownerReference` pointing to the Splunk CR. If not, it's considered user-created.

```go
// Pseudo-code from util.go
if PDB exists:
    if PDB has ownerReference to this CR:
        // Operator-managed - update if needed
        update PDB
    else:
        // User-created - DO NOT MODIFY
        skip update and log message
```

## Use Cases

### Use Case 1: Higher Availability Requirements

Customer wants to ensure at least 2 pods are always available during rolling updates:

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: splunk-my-indexer-indexer-pdb
  namespace: splunk
  # NO ownerReferences - indicates user-created
spec:
  minAvailable: 2  # Require 2 pods minimum (vs operator default of replicas-1)
  selector:
    matchLabels:
      app.kubernetes.io/instance: splunk-my-indexer-indexer
```

**Result:**
- Operator detects user-created PDB
- Does NOT override with `minAvailable = replicas - 1`
- User's `minAvailable: 2` is preserved
- Rolling updates will only proceed if ≥2 pods remain available

### Use Case 2: Maintenance Window Control

Customer wants to prevent all disruptions during business hours:

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: splunk-my-search-head-search-head-pdb
  namespace: splunk
spec:
  minAvailable: 100%  # Prevent ALL disruptions
  selector:
    matchLabels:
      app.kubernetes.io/instance: splunk-my-search-head-search-head
```

**Result:**
- No pods can be evicted (minAvailable = 100%)
- Rolling updates and restarts will be blocked
- Customer must update/delete PDB to allow operations
- Useful for preventing automatic restarts during critical periods

### Use Case 3: Faster Updates

Customer wants faster rolling updates (multiple pods at once):

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: splunk-my-standalone-standalone-pdb
  namespace: splunk
spec:
  maxUnavailable: 3  # Allow up to 3 pods to be disrupted simultaneously
  selector:
    matchLabels:
      app.kubernetes.io/instance: splunk-my-standalone-standalone
```

**Result:**
- Operator respects user's `maxUnavailable: 3`
- Rolling updates can proceed with 3 pods down at once
- Faster updates, but lower availability during rollout

## Naming Convention

The operator uses a consistent naming pattern for PDBs:

```
splunk-<cr-name>-<instance-type>-pdb
```

Examples:
- `splunk-prod-standalone-pdb` (for Standalone CR named "prod")
- `splunk-idx-cluster-indexer-pdb` (for IndexerCluster CR named "idx-cluster")
- `splunk-sh-cluster-search-head-pdb` (for SearchHeadCluster CR named "sh-cluster")
- `splunk-ingestor-ingestor-pdb` (for IngestorCluster CR named "ingestor")

**To create a user-managed PDB:**
1. Use the exact name pattern above
2. Do NOT set `ownerReferences`
3. Set your desired `minAvailable` or `maxUnavailable`
4. Ensure selector matches the operator's pod labels

## Verification

### Check if PDB is User-Created or Operator-Managed

```bash
# Get PDB
kubectl get pdb splunk-my-indexer-indexer-pdb -n splunk -o yaml

# Check ownerReferences
kubectl get pdb splunk-my-indexer-indexer-pdb -n splunk -o jsonpath='{.metadata.ownerReferences}'
```

**User-Created:**
```yaml
ownerReferences: []  # Empty or not present
```

**Operator-Managed:**
```yaml
ownerReferences:
  - apiVersion: enterprise.splunk.com/v4
    kind: IndexerCluster
    name: my-indexer
    uid: abc-123-def
    controller: true
```

### Check Operator Logs

When operator detects a user-created PDB:

```
INFO  ApplyPodDisruptionBudget  PodDisruptionBudget exists but is not managed by operator, skipping update
  pdbName=splunk-my-indexer-indexer-pdb
  reason=user-created PDB detected
```

## Lifecycle Management

### User-Created PDBs

| Event | Behavior |
|-------|----------|
| CR Created | Operator detects PDB, skips creation, uses user's settings |
| CR Updated (replicas changed) | Operator skips update, user's settings preserved |
| CR Deleted | **PDB is NOT deleted** (no owner reference) - user must delete manually |
| User Updates PDB | Changes take effect immediately |
| User Deletes PDB | Operator creates its own PDB on next reconcile |

### Operator-Managed PDBs

| Event | Behavior |
|-------|----------|
| CR Created | Operator creates PDB with default settings |
| CR Updated (replicas changed) | Operator updates `minAvailable = replicas - 1` |
| CR Deleted | **PDB is deleted automatically** (via owner reference) |
| User Updates PDB | Operator reverts changes on next reconcile |
| User Deletes PDB | Operator recreates PDB on next reconcile |

## Switching Between User-Created and Operator-Managed

### From Operator-Managed to User-Created

1. Delete the operator-managed PDB:
   ```bash
   kubectl delete pdb splunk-my-indexer-indexer-pdb -n splunk
   ```

2. Create user PDB with same name (without ownerReferences):
   ```bash
   kubectl apply -f user-pdb.yaml
   ```

3. Operator will detect and respect user PDB on next reconcile

### From User-Created to Operator-Managed

1. Delete user-created PDB:
   ```bash
   kubectl delete pdb splunk-my-indexer-indexer-pdb -n splunk
   ```

2. Operator will create and manage PDB on next reconcile

## Best Practices

### ✅ DO

1. **Use specific names**: Follow the operator's naming convention exactly
2. **Match selectors**: Ensure your PDB selector matches operator's pod labels
3. **Document intent**: Add labels/annotations explaining why PDB is user-created
4. **Test changes**: Verify PDB blocks/allows disruptions as expected
5. **Monitor logs**: Check operator logs to confirm PDB detection

### ❌ DON'T

1. **Don't set ownerReferences**: This makes it operator-managed
2. **Don't use wrong names**: PDB name must match operator's pattern
3. **Don't forget cleanup**: User-created PDBs are NOT auto-deleted with CR
4. **Don't block forever**: Ensure `minAvailable` allows eventual operations
5. **Don't assume defaults**: User PDB completely overrides operator behavior

## Examples

### Example 1: High Availability Indexer Cluster

```yaml
apiVersion: enterprise.splunk.com/v4
kind: IndexerCluster
metadata:
  name: prod-indexer
  namespace: splunk
spec:
  replicas: 10
---
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: splunk-prod-indexer-indexer-pdb
  namespace: splunk
  labels:
    app: splunk-indexer
    managed-by: platform-team
    reason: high-availability-requirement
spec:
  minAvailable: 8  # Require 8/10 available (vs default 9/10)
  selector:
    matchLabels:
      app.kubernetes.io/instance: splunk-prod-indexer-indexer
```

**Effect:** Allows 2 pods to be disrupted simultaneously (faster updates, acceptable risk)

### Example 2: Dev Environment (Fast Updates)

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: dev-standalone
  namespace: splunk-dev
spec:
  replicas: 5
---
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: splunk-dev-standalone-standalone-pdb
  namespace: splunk-dev
  labels:
    environment: dev
    reason: fast-updates-acceptable
spec:
  minAvailable: 0  # Allow all pods to be disrupted (fastest updates)
  selector:
    matchLabels:
      app.kubernetes.io/instance: splunk-dev-standalone-standalone
```

**Effect:** No disruption protection, maximum update speed (dev environment only!)

### Example 3: Production with Strict Availability

```yaml
apiVersion: enterprise.splunk.com/v4
kind: SearchHeadCluster
metadata:
  name: prod-shc
  namespace: splunk
spec:
  replicas: 5
---
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: splunk-prod-shc-search-head-pdb
  namespace: splunk
  labels:
    environment: production
    reason: sla-requirements
spec:
  minAvailable: 4  # Require 4/5 available (vs default 4/5 - same but explicit)
  selector:
    matchLabels:
      app.kubernetes.io/instance: splunk-prod-shc-search-head
```

**Effect:** Explicitly documents availability requirement matching operator default

## Troubleshooting

### Issue: Operator keeps overwriting my PDB

**Cause:** PDB has `ownerReferences` pointing to CR (operator-managed)

**Solution:**
1. Delete PDB
2. Recreate without `ownerReferences`
3. Verify with `kubectl get pdb <name> -o jsonpath='{.metadata.ownerReferences}'`

### Issue: Rolling update stuck, not progressing

**Cause:** User PDB `minAvailable` too high, blocks all evictions

**Solution:**
1. Check current pod status: `kubectl get pods -n splunk`
2. Check PDB status: `kubectl get pdb -n splunk -o yaml`
3. Temporarily update PDB to allow evictions
4. Or delete PDB to use operator defaults

### Issue: PDB not deleted when CR is deleted

**Cause:** User-created PDB has no owner reference

**Solution:** This is expected behavior. Manually delete user-created PDB:
```bash
kubectl delete pdb splunk-<cr-name>-<type>-pdb -n <namespace>
```

### Issue: Operator logs show PDB creation failed

**Cause:** User-created PDB exists with different settings

**Solution:** Check if PDB is user-created:
```bash
kubectl get pdb -n splunk -o yaml
```
If user-created (no ownerReferences), operator will skip it - no action needed

## Testing

Two test cases verify this behavior:

### TestUserCreatedPDB
```go
// Verifies operator does NOT modify user-created PDBs
// 1. User creates PDB with minAvailable=1
// 2. Operator tries to apply PDB with minAvailable=2
// 3. User's minAvailable=1 is preserved
```

### TestOperatorManagedPDB
```go
// Verifies operator CAN modify its own PDBs
// 1. Operator-managed PDB exists with minAvailable=2
// 2. Operator applies PDB with minAvailable=4
// 3. PDB is updated to minAvailable=4
```

Run tests:
```bash
go test -v ./pkg/splunk/enterprise -run "TestUserCreatedPDB|TestOperatorManagedPDB"
```

## Summary

✅ **Operator respects user-created PDBs** (no owner reference)
✅ **Operator manages its own PDBs** (with owner reference)
✅ **User PDBs take precedence** over operator defaults
✅ **Automatic lifecycle management** for operator-created PDBs
✅ **Manual cleanup required** for user-created PDBs
✅ **Fully tested** with unit tests

This design allows customers full control over availability requirements while maintaining sensible defaults for most deployments.
