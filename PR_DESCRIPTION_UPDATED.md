## Overview

This spike implements a comprehensive pod lifecycle management system for Splunk Operator that enables graceful pod termination, role-specific cleanup, and flexible rolling update strategies. The implementation follows Kubernetes-native patterns using finalizers, preStop hooks, PodDisruptionBudgets, and the Eviction API.

## Key Features

### 1. Graceful Pod Lifecycle Management

**PreStop Lifecycle Hooks**
- Implements role-specific shutdown procedures in preStop hooks
- **Indexers:** Automatic decommission with bucket rebalancing (scale-down) or graceful stop (restart)
- **Search Heads:** Automatic detention and cluster removal with graceful stop
- **Other Roles:** Graceful Splunk shutdown with proper connection cleanup
- Configurable timeouts: 5 minutes for indexers, 2 minutes for other roles

**Intent-Aware Operations**
- Uses `splunk.com/pod-intent` annotation to distinguish operations:
  - `serve` - Normal operation
  - `scale-down` - Permanent removal with full cleanup
  - `restart` - Temporary termination with data preservation
- Enables correct handling of PVC lifecycle (preserve on restart, delete on scale-down)

### 2. Safe Cluster Operations

**Finalizer-Based Cleanup**
- Ensures cleanup completes before pod deletion
- Prevents data loss during scale-down operations
- Verifies decommission/detention completion before removing pods
- Automatic PVC cleanup on scale-down

**PodDisruptionBudget Integration**
- Automatically creates PDBs with `minAvailable = replicas - 1`
- Ensures minimum availability during updates and restarts
- Works with Kubernetes Eviction API for safe pod termination
- Handles single-replica deployments correctly

### 3. Flexible Rolling Update Strategies

**Percentage-Based Updates**
- Configure maximum unavailable pods as percentage (e.g., "25%") or absolute number
- Faster rollouts by updating multiple pods simultaneously
- Example:
  ```yaml
  spec:
    rollingUpdateConfig:
      maxPodsUnavailable: "25%"  # For 10 replicas, allow 2-3 pods down
  ```

**Canary Deployments**
- Support for partition-based staged rollouts
- Test updates on subset of pods before full rollout
- Example:
  ```yaml
  spec:
    rollingUpdateConfig:
      partition: 8  # Only update pods with ordinal >= 8
      maxPodsUnavailable: "1"
  ```

**Intelligent Update Management**
- Mutual exclusion between operator-triggered evictions and StatefulSet rolling updates
- Prevents PDB violations from simultaneous pod terminations
- Automatic staleness detection for stuck updates
- Smart coordination between multiple update mechanisms

### 4. Automatic Restart Detection (IngestorCluster & Standalone)

**Per-Pod Restart Monitoring**
- Monitors Splunk API `restart_required` messages for configuration changes
- Automatically evicts pods requiring restart
- One pod at a time to maintain availability
- Respects PodDisruptionBudget automatically

**Configuration-Driven Restarts**
- Secret changes (Queue/Pipeline credentials) trigger StatefulSet rolling updates
- Splunk config changes trigger per-pod eviction
- Both mechanisms coordinate to prevent conflicts

### 5. Kubernetes-Native Design

**Follows Best Practices**
- Uses Kubernetes Eviction API (not direct pod deletion)
- Eviction API automatically respects PodDisruptionBudget
- Finalizers prevent premature pod deletion
- PreStop hooks ensure graceful shutdown
- StatefulSet RollingUpdate for template changes

**Production-Ready Error Handling**
- Timeout protection for API calls (10-second timeouts)
- Environment variable validation
- Proper error signaling from preStop hooks
- Update staleness detection
- Duplicate finalizer prevention

## Architecture

See the complete C4 architecture diagram: `per-pod-rolling-restart-architecture.png`

**Key Components:**
- **Pod Controller** - Watches pods with finalizers and triggers cleanup
- **PreStop Hooks** - Role-specific decommission, detention, and graceful stop
- **Pod Deletion Handler** - Intent-based cleanup with PVC lifecycle management
- **PodDisruptionBudget** - Ensures minimum availability during operations
- **StatefulSet Controller** - Kubernetes-native rolling update management

## Implementation Details

### Cluster Type Behaviors

**IngestorCluster & Standalone**
- Automatic restart detection via `restart_required` monitoring
- Per-pod eviction when restart needed
- No in-product orchestrator, so operator manages full lifecycle
- Coordination with StatefulSet updates to prevent conflicts

**IndexerCluster**
- Cluster Manager (CM) handles restart coordination
- Operator provides finalizer-based cleanup during scale-down
- PreStop hook handles decommission (with/without bucket rebalancing)
- StatefulSet rolling updates for configuration changes

**SearchHeadCluster**
- Deployer + Captain handle restart coordination
- Operator provides finalizer-based cleanup during scale-down
- PreStop hook handles detention and cluster removal
- StatefulSet rolling updates for configuration changes

### Environment Variables

All pods receive via Kubernetes downward API:
- `POD_NAME` - Pod name for API queries
- `POD_NAMESPACE` - Namespace for API queries
- `SPLUNK_ROLE` - Role type for preStop hook logic
- `SPLUNK_CLUSTER_MANAGER_URL` - Cluster Manager URL (for indexers)

Passwords read from mounted secrets at `/mnt/splunk-secrets/password`

### Termination Grace Periods

- **Indexers:** 300 seconds (5 minutes) for decommission + stop
- **Other roles:** 120 seconds (2 minutes) for graceful stop

## Benefits

### Operational

1. **Zero Data Loss** - Proper decommission ensures bucket replication completes
2. **Maintained Availability** - PDB ensures minimum pods available during operations
3. **Faster Updates** - Percentage-based updates allow multiple pods simultaneously
4. **Staged Rollouts** - Partition support enables canary deployments
5. **Automatic Recovery** - Pods automatically restart when configuration changes require it

### Technical

1. **Kubernetes-Native** - Uses standard K8s patterns (Eviction API, PDB, Finalizers, PreStop hooks)
2. **Conflict Prevention** - Mutual exclusion prevents simultaneous pod terminations
3. **Proper Cleanup** - Finalizers ensure cleanup completes before pod deletion
4. **Visibility** - PreStop failures visible in pod events for easy debugging
5. **Error Handling** - Timeouts, validation, and proper error signaling throughout

### Developer

1. **Clear Intent** - Annotation system makes pod lifecycle explicit
2. **Separation of Concerns** - PreStop hooks handle execution, operator handles verification
3. **Testability** - Each component can be tested independently
4. **Maintainability** - Standard patterns make code easy to understand and modify

## Configuration Examples

### Basic Rolling Update with Percentage

```yaml
apiVersion: enterprise.splunk.com/v4
kind: IngestorCluster
metadata:
  name: example
spec:
  replicas: 10
  rollingUpdateConfig:
    maxPodsUnavailable: "25%"  # Allow 2-3 pods updating simultaneously
```

### Canary Deployment

```yaml
apiVersion: enterprise.splunk.com/v4
kind: IndexerCluster
metadata:
  name: example
spec:
  replicas: 10
  rollingUpdateConfig:
    partition: 8  # Update only pods 8 and 9 first
    maxPodsUnavailable: "1"
```

### Conservative Update (Default)

```yaml
apiVersion: enterprise.splunk.com/v4
kind: SearchHeadCluster
metadata:
  name: example
spec:
  replicas: 5
  # No rollingUpdateConfig - defaults to 1 pod at a time
```

## Testing

### Completed
- ✅ IngestorCluster restart with restart_required detection
- ✅ IngestorCluster scale-down with PVC cleanup
- ✅ PodDisruptionBudget enforcement
- ✅ Finalizer cleanup verification
- ✅ Code compilation and build verification

### Recommended Testing
- Scale-down operations for all cluster types
- Percentage-based rolling updates (various percentages)
- Canary deployments with partition
- Restart detection and automatic eviction
- PDB behavior with different replica counts
- PreStop hook timeout handling
- Finalizer cleanup with unreachable services

## Files Changed

### New Files
- `tools/k8_probes/preStop.sh` - PreStop lifecycle hook implementation
- `per-pod-rolling-restart-architecture.png` - C4 architecture diagram
- `KUBERNETES_NATIVE_REVIEW_FINDINGS.md` - K8s patterns review and validation

### Modified Core Logic
- `pkg/splunk/enterprise/configuration.go` - StatefulSet creation, PreStop hooks, finalizers
- `pkg/splunk/enterprise/pod_deletion_handler.go` - Finalizer handler with intent-based cleanup
- `pkg/splunk/enterprise/ingestorcluster.go` - Restart detection and pod eviction
- `pkg/splunk/enterprise/standalone.go` - Restart detection and pod eviction
- `pkg/splunk/enterprise/util.go` - PodDisruptionBudget creation and management
- `pkg/splunk/splkcontroller/statefulset.go` - Rolling update coordination

### API Changes
- `api/v4/common_types.go` - RollingUpdateConfig type for percentage-based updates
- CRD manifests (auto-generated from API changes)

## User Guide

Complete user guide available in: `per-pod-rolling-restart-user-guide.md`

Covers:
- How to monitor pod lifecycle and intent annotations
- How to configure rolling update strategies
- How to trigger restarts and scale operations
- Troubleshooting common scenarios
- FAQ and best practices

## Documentation

- **Architecture Diagram:** `per-pod-rolling-restart-architecture.png` - Complete C4 diagram showing all components and interactions
- **User Guide:** `per-pod-rolling-restart-user-guide.md` - Comprehensive guide for operators
- **Review Findings:** `KUBERNETES_NATIVE_REVIEW_FINDINGS.md` - K8s native patterns validation

## Backward Compatibility

- ✅ No breaking changes to existing APIs
- ✅ New `rollingUpdateConfig` field is optional (defaults to existing behavior)
- ✅ PreStop hooks automatically injected into all pods
- ✅ Existing pods updated on next rolling restart
- ✅ PDB creation backward compatible with existing deployments

## Future Enhancements

- Dynamic timeout adjustment based on bucket count/size
- Progressive rollout automation based on health checks
- Blue/green deployment support
- Automatic rollback on failed operations
- Prometheus metrics for decommission/detention duration

---

🔬 **This is a SPIKE** - For evaluation and architectural review

🤖 Generated with [Claude Code](https://claude.com/claude-code)
