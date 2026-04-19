# EPIC: Kubernetes-Native Pod Lifecycle Management for Splunk Operator

## Summary

Redesign how Splunk Operator manages pod lifecycle during scale, configuration change, and restart operations so that it operates as a true Kubernetes-native controller. The current implementation uses an operator-driven model where the controller directly calls Splunk decommission and detention APIs, manually deletes pods, and blocks reconciliation while waiting for each pod to cycle — one at a time, serially. This must be replaced with a model where the operator sets desired state, Kubernetes orchestrates pod lifecycle through StatefulSet rolling updates and PodDisruptionBudgets, and each pod handles its own Splunk-specific shutdown through preStop lifecycle hooks.

This EPIC also adds a per-pod secret-change detection mechanism that uses Splunk's `/services/messages/restart_required` bulletin board API to evict individual pods precisely when their running configuration has diverged from what is on disk.

---

## Problem Statement

### Why the current approach is wrong

**1. The operator blocks on pod operations**
When a configuration change or scale event requires pods to cycle, the current reconcile loop calls the Splunk decommission or detention REST API from the controller goroutine, then waits for the operation to complete, then deletes the pod, then waits for the new pod to be ready — and only then moves to the next pod. For a 100-indexer cluster requiring 15 minutes of bucket migration per pod, this serialises to over 25 hours of blocked reconciliation. No other resource in the namespace can be reconciled while this is in progress.

**2. The StatefulSet is set to OnDelete**
With `updateStrategy: OnDelete`, the StatefulSet controller never manages pod updates. The operator is reimplementing rolling-update logic that Kubernetes already provides — and doing so with less reliability, no back-off, and no awareness of node conditions.

**3. No PodDisruptionBudgets**
Nothing prevents multiple pods from being unavailable simultaneously during node drain, cluster upgrade, or any other voluntary disruption. A cluster upgrade can take down all indexer pods at once.

**4. No differentiation between restart and scale-down**
Every pod deletion is treated identically: full decommission with `enforce_counts=1`, which triggers complete bucket migration. A configuration change that simply requires a pod restart causes the same 15-minute bucket migration as a permanent scale-down. This is unnecessary and slow.

**5. Operator crash = inconsistent state**
If the operator crashes mid-rolling-update, it has no record of which pods were already updated. The cluster is left in a mixed-version state with no automatic recovery.

---

## Solution

### Kubernetes-native lifecycle

Change `updateStrategy` to `RollingUpdate`. Set `terminationGracePeriodSeconds` long enough to accommodate the slowest Splunk operation (bucket migration). Move all Splunk-specific shutdown logic — decommission, detention, graceful stop — into a `preStop` lifecycle hook script that runs inside the pod, where it has direct localhost access to the Splunk management API and does not require any RBAC. Use PodDisruptionBudgets to enforce a minimum number of available pods during any voluntary disruption.

### Pod intent annotation + downward API volume

The preStop hook must know whether the pod is being deleted because of a restart/config-change (in which case bucket migration is unnecessary) or because of a permanent scale-down (in which case full bucket migration is required). Communicate this via an annotation on the pod (`splunk.com/pod-intent`). Mount the annotation as a file through a Downward API volume (`/etc/podinfo/intent`) so the file updates live when the annotation changes. Environment variables cannot be used for this purpose — they are frozen at pod start time and will not reflect an annotation that the operator sets immediately before initiating scale-down.

### Finalizer for post-termination cleanup

After the preStop hook completes and the container exits, a finalizer (`splunk.com/pod-cleanup`) prevents the pod object from being garbage-collected until the operator has run post-termination cleanup. For indexers on scale-down, this cleanup calls the cluster manager `remove_peers` API to remove the peer GUID from the cluster manager's configuration (without this, a "Down" peer accumulates indefinitely), and then deletes the pod's PVCs. PVCs are preserved on restart so that the pod rejoins the cluster with its existing data.

### Per-pod restart_required eviction

For IngestorCluster and Standalone, secret or configuration changes that require a Splunk restart are detected by polling each pod's `/services/messages/restart_required` endpoint. When a pod reports restart is required, it is evicted through the Kubernetes Eviction API, which checks the PodDisruptionBudget before proceeding. Only one pod is evicted per reconcile cycle, ensuring the cluster stays within its disruption budget.

---

## Scope

| Story | Title |
|-------|-------|
| statefulset-rollingupdate-pdb | StatefulSet RollingUpdate strategy and PodDisruptionBudget management |
| pod-intent-annotation-downward-api | Pod intent annotation and Downward API volume for preStop |
| prestop-lifecycle-hook | preStop lifecycle hook: role-aware decommission, detention, and graceful stop |
| pod-cleanup-finalizer | Pod cleanup finalizer: cluster manager peer removal and PVC lifecycle |
| restart-required-eviction | Per-pod restart_required detection and Eviction API for IngestorCluster and Standalone |
| rbac-helm-crd-manifests | RBAC, Helm chart, and CRD manifest updates |
| testing | Unit and integration tests |
| documentation | Documentation |
| erd | Write the Engineering Requirements Document for this EPIC |

---

## Acceptance Criteria (EPIC level)

1. A 3-replica IndexerCluster config change (e.g., image update) completes without the operator goroutine blocking; each pod decommissions with `enforce_counts=0` and rejoins; total time is bounded by `terminationGracePeriodSeconds`, not serial operator waits.
2. A scale-down from 4 to 3 indexers triggers full bucket migration (`enforce_counts=1`) on the removed pod, removes the peer from the cluster manager via `remove_peers`, and deletes the pod's PVCs. The operator does not block during decommission.
3. At no point during a rolling update or scale-down are more pods simultaneously unavailable than permitted by the PodDisruptionBudget.
4. If the operator crashes mid-update, the StatefulSet controller continues the rolling update independently and pods continue running their preStop hooks without operator involvement.
5. A secret change on an IngestorCluster results in each pod being individually evicted (one per reconcile cycle) only after `/services/messages/restart_required` confirms that specific pod needs a restart.
6. All new and modified code has unit test coverage. Key scenario flows (scale-down, restart, secret-change eviction) have integration tests.

---

## Out of Scope

- SearchHeadCluster scale-out (adding members is managed separately by the SHC deployer)
- Multi-site cluster topology changes
- AppFramework app deployment changes
- Monitoring Console configuration
