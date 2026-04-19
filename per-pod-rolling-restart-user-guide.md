# Per-Pod Rolling Restart Feature Guide

## Overview

The Splunk Operator now intelligently manages pod lifecycle with **per-pod rolling restarts**, ensuring your Splunk clusters remain healthy and available during configuration changes, secret updates, and scale operations.

## What This Feature Does For You

### Automatic Restart Management

Your Splunk pods will automatically restart when needed, without manual intervention:

- **Secret Changes**: When you update secrets (passwords, certificates), the operator detects the change and safely restarts affected pods one at a time
- **Configuration Updates**: Pods that need restart after configuration changes are automatically identified and restarted
- **Zero Manual Intervention**: No need to manually delete pods or trigger restarts

### Safe Scale-Down Operations

When scaling down your cluster, the operator ensures proper cleanup:

- **Data Protection**: Indexer data is safely replicated before the pod is removed
- **Graceful Decommission**: Indexers are properly decommissioned from the cluster
- **Automatic Cleanup**: Persistent volumes are automatically deleted during scale-down (but preserved during restarts)

### High Availability During Restarts

Your cluster stays available during maintenance:

- **One Pod at a Time**: Only one pod restarts at a time, maintaining cluster quorum
- **Respects Pod Disruption Budgets**: Ensures minimum availability requirements are met
- **Health Checks**: Waits for pods to become healthy before proceeding to the next

## Key Benefits

### 1. Reduced Operational Burden
**Before**: You had to manually monitor and restart pods when configurations changed
**Now**: The operator automatically detects and handles restarts for you

### 2. Safer Operations
**Before**: Scaling down could leave orphaned data or improperly decommissioned nodes
**Now**: Automatic cleanup ensures proper decommissioning and data protection

### 3. Better Availability
**Before**: Cluster-wide rolling restarts could cause service disruptions
**Now**: Individual pod restarts minimize impact on cluster availability

### 4. Faster Recovery
**Before**: Manual intervention delayed restart operations
**Now**: Automated detection and restart speeds up configuration changes

## How It Works (User Perspective)

### When You Update a Secret

```bash
# Update your Splunk admin password
kubectl create secret generic splunk-secret \
  --from-literal=password='newpassword' \
  --dry-run=client -o yaml | kubectl apply -f -
```

**What Happens**:
1. ✅ Operator detects the secret change
2. ✅ Identifies all pods using that secret
3. ✅ Restarts pods one at a time
4. ✅ Waits for each pod to become healthy before continuing
5. ✅ Your data is preserved (volumes are not deleted)

**Timeline**: Typically 5-10 minutes per pod depending on cluster size

### When You Scale Down

```bash
# Scale your indexer cluster from 5 to 3 replicas
kubectl patch indexercluster my-cluster \
  -p '{"spec":{"replicas":3}}' --type=merge
```

**What Happens**:
1. ✅ Operator marks pods for removal
2. ✅ Decommissions indexers (waits for data replication)
3. ✅ Removes pods from cluster manager
4. ✅ Deletes persistent volumes for removed pods
5. ✅ Remaining cluster continues serving traffic

**Timeline**: Depends on data volume, typically 10-30 minutes per indexer

### When You Scale Up

```bash
# Scale your indexer cluster from 3 to 5 replicas
kubectl patch indexercluster my-cluster \
  -p '{"spec":{"replicas":5}}' --type=merge
```

**What Happens**:
1. ✅ New pods are created with proper finalizers
2. ✅ Pods automatically join the cluster
3. ✅ Data replication begins automatically
4. ✅ New pods become available for search and indexing

## Monitoring Restart Operations

### Check Pod Status

Monitor pods during restart operations:

```bash
# Watch pod status
kubectl get pods -l app.kubernetes.io/component=indexer -w

# Check specific pod intent
kubectl get pod <pod-name> -o jsonpath='{.metadata.annotations.splunk\.com/pod-intent}'
```

**Pod Intent Values**:
- `serve` - Pod is actively serving traffic (normal operation)
- `scale-down` - Pod is being removed (scale-down in progress)
- `restart` - Pod is restarting (configuration change)

### View Operator Logs

See what the operator is doing:

```bash
# Watch restart operations
kubectl logs -f deployment/splunk-operator-controller-manager \
  -n splunk-operator | grep -E "(restart|eviction|scale)"
```

**Key Log Messages**:
- `"Pod needs restart, evicting pod"` - Restart detected and initiated
- `"Scale-down detected via annotation"` - Scale-down in progress
- `"Restart operation: preStop hook handles decommission"` - Safe restart (preserving data)
- `"Deleting PVCs for scale-down operation"` - Cleanup during scale-down

### Check Cluster Status

Monitor your cluster during operations:

```bash
# Check IndexerCluster status
kubectl get indexercluster my-cluster -o jsonpath='{.status.phase}'

# Check restart status
kubectl get indexercluster my-cluster \
  -o jsonpath='{.status.restartStatus.podsNeedingRestart}'
```

## Common Scenarios

### Scenario 1: Update Admin Password

**Goal**: Change the Splunk admin password for your cluster

**Steps**:
```bash
# 1. Update the secret
kubectl create secret generic splunk-my-cluster-secret \
  --from-literal=password='newpassword123' \
  --dry-run=client -o yaml | kubectl apply -f -

# 2. Wait and watch (operator handles the rest)
kubectl get pods -w
```

**Expected Behavior**:
- Pods restart one at a time
- Each pod takes 3-5 minutes to restart
- Total time: ~15-30 minutes for a 5-pod cluster
- No data loss
- Cluster remains searchable throughout

### Scenario 2: Scale Down for Cost Savings

**Goal**: Reduce indexer count from 5 to 3 to save costs

**Steps**:
```bash
# 1. Scale down
kubectl patch indexercluster my-cluster \
  -p '{"spec":{"replicas":3}}' --type=merge

# 2. Monitor progress
kubectl get pods -l app.kubernetes.io/component=indexer -w
```

**Expected Behavior**:
- Indexer-4 and indexer-5 are decommissioned
- Data is replicated to remaining indexers
- PVCs for removed indexers are deleted
- Total time: ~20-40 minutes depending on data volume
- Search and indexing continue on remaining pods

### Scenario 3: Routine Maintenance

**Goal**: Apply Splunk configuration changes that require restart

**Steps**:
```bash
# 1. Update your ConfigMap or other configuration
kubectl apply -f my-splunk-config.yaml

# 2. Trigger restart by setting restart_required message
kubectl exec <pod-name> -- curl -k -u admin:password \
  -X POST https://localhost:8089/services/messages/restart_required \
  -d name="maintenance" \
  -d value="Applied configuration changes"

# 3. Operator detects and handles restart
kubectl get pods -w
```

**Expected Behavior**:
- Operator detects restart_required message
- Pod is gracefully evicted
- Pod restarts with new configuration
- Health checks pass before proceeding
- Process repeats for other pods if needed

## Best Practices

### 1. Plan Maintenance Windows
Although restarts are automated, plan for:
- **Secret updates**: 5-10 minutes per pod
- **Scale-down**: 10-30 minutes per pod (data dependent)
- **Configuration changes**: 5-10 minutes per pod

### 2. Monitor During Operations
Always watch the operator logs during:
- First-time operations in a new environment
- Large scale-down operations (> 3 pods)
- Critical production changes

### 3. Verify Before Scale-Down
Before scaling down, ensure:
- Replication factor allows for the reduction
- Search factor allows for the reduction
- Sufficient capacity remains for your data volume

### 4. Test in Non-Production First
For major changes:
- Test secret updates in dev/staging first
- Validate scale-down operations in lower environments
- Verify restart timing with your data volumes

## Troubleshooting

### Pod Stuck in Terminating

**Symptom**: Pod shows `Terminating` for more than 10 minutes

**Possible Causes**:
- Decommission is waiting for data replication
- Network issues preventing cluster communication
- Operator is processing another pod

**Resolution**:
```bash
# Check operator logs
kubectl logs deployment/splunk-operator-controller-manager -n splunk-operator

# Check pod events
kubectl describe pod <pod-name>

# Check if decommission is complete
kubectl exec <pod-name> -- curl -k -u admin:password \
  https://localhost:8089/services/cluster/slave/info
```

### Restart Not Triggering

**Symptom**: Changed secret but pods are not restarting

**Possible Causes**:
- Pod Disruption Budget is blocking eviction
- Another pod is currently restarting
- Secret reference doesn't match pod configuration

**Resolution**:
```bash
# Check PDB status
kubectl get pdb

# Check if eviction is blocked
kubectl get events --sort-by='.lastTimestamp'

# Verify secret reference
kubectl get pod <pod-name> -o yaml | grep -A 5 secretRef
```

### Scale-Down Not Completing

**Symptom**: Scale-down initiated but pod won't terminate

**Possible Causes**:
- Replication factor prevents scale-down
- Data replication is in progress
- Cluster manager is unreachable

**Resolution**:
```bash
# Check cluster manager status
kubectl exec <cluster-manager-pod> -- curl -k -u admin:password \
  https://localhost:8089/services/cluster/master/peers

# Check replication status
kubectl logs <pod-name> | grep -i replication

# Verify cluster health
kubectl exec <pod-name> -- curl -k -u admin:password \
  https://localhost:8089/services/cluster/master/health
```

## Configuration Options

### Pod Disruption Budget

Control how many pods can be unavailable during restarts:

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: splunk-indexer-pdb
spec:
  minAvailable: 2  # Minimum 2 indexers must be available
  selector:
    matchLabels:
      app.kubernetes.io/component: indexer
```

### Restart Annotations

View or modify pod intent annotations:

```bash
# View current intent
kubectl get pod <pod-name> \
  -o jsonpath='{.metadata.annotations.splunk\.com/pod-intent}'

# Manually mark for scale-down (advanced use only)
kubectl annotate pod <pod-name> \
  splunk.com/pod-intent=scale-down --overwrite
```

**⚠️ Warning**: Manual annotation changes should only be done by advanced users and may cause unexpected behavior.

## Feature Compatibility

### Supported Cluster Types
- ✅ IndexerCluster
- ✅ SearchHeadCluster
- ✅ Standalone (secret change detection only)

### Supported Operations
- ✅ Secret updates → Automatic rolling restart
- ✅ Scale-down → Safe decommission with PVC cleanup
- ✅ Scale-up → Automatic pod creation with finalizers
- ✅ Manual restart triggers → Per-pod eviction

### Requirements
- Kubernetes 1.21+
- Splunk Operator 3.0+
- Splunk Enterprise 8.0+

## Frequently Asked Questions

### Q: Will my data be lost during restart?
**A**: No. Restarts preserve all persistent volumes. Only scale-down operations delete PVCs for removed pods.

### Q: How long does a restart take?
**A**: Typically 5-10 minutes per pod, depending on pod size and startup time. The operator waits for health checks before proceeding.

### Q: Can I restart multiple pods simultaneously?
**A**: No. The operator enforces one pod at a time to maintain cluster availability and respect Pod Disruption Budgets.

### Q: What happens if the operator crashes during a restart?
**A**: The finalizer prevents pod deletion until cleanup completes. When the operator restarts, it will continue the cleanup process.

### Q: Can I disable automatic restarts?
**A**: Currently, automatic restart on secret changes is enabled by default. You can control the pace by adjusting Pod Disruption Budgets.

### Q: Will restarts affect search performance?
**A**: Yes, temporarily. During restart, one indexer/search head is unavailable. However, the cluster continues serving traffic with remaining pods.

## Getting Help

If you encounter issues:

1. **Check operator logs**: Most issues are visible in operator logs
2. **Review pod events**: `kubectl describe pod <pod-name>`
3. **Verify cluster status**: Check Splunk UI for cluster health
4. **Consult documentation**: Review the full operator documentation
5. **Contact support**: Reach out to Splunk support with operator logs

## Summary

The per-pod rolling restart feature provides:
- ✅ Automatic restart management for secret and configuration changes
- ✅ Safe scale-down with proper decommissioning
- ✅ High availability during maintenance operations
- ✅ Reduced operational burden through automation

This feature is enabled by default in Splunk Operator 3.0+ and requires no additional configuration for basic usage.
