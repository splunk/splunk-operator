## Overview

This spike implements a pod lifecycle management system for Splunk Operator that differentiates between pod restarts and scale-down operations using Kubernetes finalizers and intent annotations. This ensures proper cleanup, prevents data loss, and enables graceful pod removal.

## Problem Statement

**Before this change:**
- Operator couldn't distinguish between pod restart (rolling update) vs scale-down operations
- Scale-down operations left orphaned PVCs consuming storage
- Indexer scale-down didn't properly decommission nodes, causing data replication issues
- Search head scale-down didn't properly detain members, causing SHC stability issues
- No guarantee that cleanup completed before pod deletion

## Solution

**Finalizer-Based Cleanup:**
- Adds `splunk.com/pod-cleanup` finalizer to all Splunk pods
- Blocks pod deletion until cleanup completes
- New Pod Controller watches finalizer pods and triggers cleanup on deletion

**Intent Annotation System:**
- `splunk.com/pod-intent` annotation with three values:
  - `serve` - Pod is actively serving traffic (default)
  - `scale-down` - Pod is being removed due to scale-down
  - `restart` - Pod is being restarted/updated
- Scale-down intent is marked BEFORE scaling down StatefulSet
- Fallback: Compare pod ordinal vs StatefulSet replicas

**Role-Specific Cleanup:**
- **Indexers:** Decommission via Splunk API, remove from peer list, delete PVCs on scale-down
- **Search Heads:** Detain member via Splunk API, remove from SHC, delete PVCs on scale-down
- **Ingestors/Standalone:** Delete PVCs on scale-down
- **Restarts:** Preserve PVCs for all roles

## Architecture

See the complete C4 architecture diagram: `per-pod-rolling-restart-architecture.png`

Key components:
- **Pod Controller** (`internal/controller/pod_controller.go`) - Watches finalizer pods
- **Pod Deletion Handler** (`pkg/splunk/enterprise/pod_deletion_handler.go`) - Intent-based cleanup logic
- **StatefulSet Utility** - Marks pods with scale-down intent before scaling

## Scope

### Controllers with restart_required Detection

**IngestorCluster** and **Standalone** retain `restart_required` monitoring and eviction:
- No in-product orchestrator available
- Operator monitors Splunk REST API `/services/messages/restart_required`
- Automatically evicts pods one at a time when restart needed
- Respects PodDisruptionBudget

### Controllers WITHOUT restart_required Detection

**IndexerCluster** and **SearchHeadCluster** removed `restart_required` monitoring:
- **IndexerCluster:** Cluster Manager (CM) handles restart orchestration
- **SearchHeadCluster:** Deployer + Captain handle restart orchestration
- Operator only handles finalizer cleanup during scale-down/restart
- StatefulSet rolling updates trigger restarts naturally

### All Controllers Have Finalizers

All 4 controllers (IndexerCluster, SearchHeadCluster, IngestorCluster, Standalone) have:
- Finalizer-based cleanup
- Intent annotation system
- Pod deletion handler integration
- PreStop lifecycle hooks

## Key Files Changed

### New Files
- `internal/controller/pod_controller.go` - New Pod Controller
- `pkg/splunk/enterprise/pod_deletion_handler.go` - Cleanup logic with intent detection
- `per-pod-rolling-restart-user-guide.md` - User documentation (405 lines)
- `per-pod-rolling-restart-architecture.puml` - PlantUML C4 diagram source
- `per-pod-rolling-restart-architecture.png` - Architecture diagram (PNG)

### Modified Files
- `pkg/splunk/splkcontroller/statefulset.go` - Scale-down intent marking
- `pkg/splunk/enterprise/indexercluster.go` - **Removed** restart_required detection (~110 lines)
- `pkg/splunk/enterprise/searchheadcluster.go` - **Removed** restart_required detection (~110 lines)
- `pkg/splunk/enterprise/ingestorcluster.go` - **Kept** restart_required detection
- `pkg/splunk/enterprise/standalone.go` - **Kept** restart_required detection
- `pkg/splunk/enterprise/configuration.go` - Adds finalizers to pod templates
- `api/v4/indexercluster_types.go` - Added RestartStatus field
- `api/v4/searchheadcluster_types.go` - Added RestartStatus field
- `config/rbac/role.yaml` - Added Pod Controller permissions

## Testing Summary

### Completed Tests
- ✅ **Test A1:** IndexerCluster restart (pod eviction) - Passed
- ✅ **Test A2:** IndexerCluster scale-down (4→3 replicas) - Passed after clearing RollingUpdate state
- ✅ **Test B1:** SearchHeadCluster restart - Passed (PDB working as designed)

### Pending Tests
- ⏳ **Test B2:** SearchHeadCluster scale-down (3→2 replicas)
- ⏳ **Test B3:** SearchHeadCluster scale-up (2→3 replicas)

## Benefits

1. **Safe Scale-Down:** Proper decommission/detention before pod removal
2. **Storage Management:** Automatic PVC cleanup on scale-down, preservation on restart
3. **Data Protection:** Prevents data loss during indexer scale-down via decommissioning
4. **Cluster Stability:** Proper SHC member detention prevents split-brain scenarios
5. **Automatic Restarts:** Ingestor/Standalone pods restart automatically when needed
6. **Minimal Disruption:** Respects PDB, one pod at a time
7. **Kubernetes Native:** Uses Eviction API, StatefulSet rolling updates, finalizers

## User Guide

Complete user guide available in: `per-pod-rolling-restart-user-guide.md`

Covers:
- How to monitor pod intent annotations
- How to trigger restarts manually
- How to scale clusters safely
- Troubleshooting common scenarios
- FAQ

## Commits

1. `2cef0db3` - CSPL-4530: Implement per-pod rolling restart mechanism with finalizers
2. `af2850a6` - Add user guide for per-pod rolling restart feature
3. `4c5c5eb3` - Remove restart_required detection from IndexerCluster and SearchHeadCluster
4. `40724518` - Update dependencies and apply code formatting

---

🔬 **This is a SPIKE** - For evaluation and architectural review

🤖 Generated with [Claude Code](https://claude.com/claude-code)
