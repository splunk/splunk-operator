---
title: Scaling
parent: PostgreSQL
nav_order: 5
---

# PostgresCluster Scaling

This document describes how to scale out or scale down a Splunk Operator `PostgresCluster` by changing the number of instances, what guard rails the operator enforces, and what level of interruption to expect.

Examples here use scaling between 1, 2, and 3 instances. The same process applies to any value of `spec.instances`.

Major-version upgrades, minor-version upgrades, and backups are not covered here.

### How scaling works

The number of replicas is driven by the effective `instances` value for the `PostgresCluster`.

That value can be defined in either field:

- `PostgresClusterClass.spec.config.instances`
- `PostgresCluster.spec.instances`

If both are set, the `PostgresCluster` value is the effective override used for that cluster. Removing `spec.instances` returns the cluster to the class default; the admission webhook applies the same scaling rules to that transition as to any other change in the effective instance count.

When the effective `instances` changes:

- the Postgres Operator updates the managed CNPG `Cluster`
- CNPG provisions new replicas (scale-out) or drains and removes replicas (scale-down)
- `PostgresCluster.status.phase` moves to `Provisioning` while the change is in progress
- the cluster returns to `Ready` when CNPG reports a healthy state and the observed instance count matches the desired count

### Status fields populated during scaling

The operator mirrors a few CNPG status fields onto the `PostgresCluster` so consumers can track scale progress without watching the underlying CNPG `Cluster`:

| Field                     | Source                              | Notes                                                                                                                              |
| ------------------------- | ----------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `status.instances`        | `cnpgCluster.status.instances`      | Declared instance count after CNPG reconciliation.                                                                                 |
| `status.readyInstances`   | `cnpgCluster.status.readyInstances` | Count of replicas reporting ready. Tracks scale-down progress, since CNPG keeps its phase `Healthy` while removing pods.            |
| `status.currentPrimary`   | `cnpgCluster.status.currentPrimary` | Pod hosting the primary. Unaffected by scale-down — CNPG only removes replicas. Changes only on switchover/failover (e.g. minor-version upgrade with `switchover`, node failure). |
| `status.phase`            | derived                             | Reads `Provisioning` whenever desired and observed instance counts disagree, even if CNPG is `PhaseHealthy`. Returns to `Ready` when both `instances` and `readyInstances` equal the desired count. |

These fields are also surfaced as printer columns on `kubectl get postgrescluster`.

### Validation rules

The admission webhook enforces one rule to prevent unsafe edits:

| Rule                              | When                          | What it does                                                                                                                                                                                                                                              |
| --------------------------------- | ----------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Switchover requires `>= 2`        | CREATE and UPDATE             | Rejects any `PostgresCluster` whose effective `instances < 2` when the referenced class has `cnpg.primaryUpdateMethod: switchover`. Switchover requires a replica to fail over to.                                                                        |

The rule compares the merged effective `instances` value rather than the raw spec field, so a cluster relying on the class default is still subject to it.

Mid-flight retargeting is supported: editing `spec.instances` while the cluster is still `Provisioning` from a prior change is accepted, and CNPG converges on the latest target. This matches level-based reconciliation; expect `status.phase` to remain `Provisioning` until the observed and ready counts match the new desired count.

The rule ships behind the existing `PostgresController` feature gate.

### Scale-out

Scale-out is the safer direction. CNPG provisions new replicas while the existing primary continues to serve writes; expect no client-visible write interruption on the RW endpoint.

What to expect during scale-out:

- `PostgresCluster.status.phase` moves `Ready -> Provisioning -> Ready`
- CNPG cluster phases observed: `PhaseCreatingReplica`, `PhaseWaitingForInstancesToBeActive`
- events emitted: `ClusterUpdateStarted`, then `ClusterReady`

Procedure:

1. Confirm the cluster is `Ready`.
2. Update `PostgresCluster.spec.instances` in the tracked manifest.
3. Review and commit the manifest change.
4. Apply the manifest with `kubectl apply`.
5. Watch progress until `status.phase=Ready`.

Example:

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresCluster
metadata:
  name: postgresql-cluster-dev
spec:
  class: postgresql-dev
  instances: 3
```

```bash
kubectl apply -n $NS -f path/to/postgrescluster.yaml
kubectl get postgrescluster -n $NS $CLUSTER -w
kubectl get cluster -n $NS $CLUSTER -w
kubectl get events -n $NS --sort-by=.lastTimestamp
```

### Scale-down

Scale-down removes a read replica. CNPG never evicts the primary as part of scaling — its scale-down logic explicitly skips the primary pod and removes the replica with the highest serial. Writes on the RW endpoint are not interrupted.

What to expect:

- the replica with the highest serial is drained and removed; the primary keeps serving
- read clients connected directly to the evicted replica's pod-level endpoint will see their connection dropped and should reconnect via the RO service
- CNPG phase typically stays `PhaseHealthy` throughout; scale progress is tracked via `status.readyInstances`
- `PostgresCluster.status.phase` moves `Ready -> Provisioning -> Ready`
- if scaling down to `1`, the RO endpoint loses its backend and the operator publishes empty RO endpoint values in the access `ConfigMap` — see [Read-only endpoint unavailability](#read-only-endpoint-unavailability-when-fewer-than-two-instances-are-ready)

Constraints:

- Scaling to `1` is **rejected** when the class uses `primaryUpdateMethod: switchover`. Stay at `>= 2`, or recreate the cluster against a different class if you need to drop below 2 (`spec.class` is immutable).

Procedure is the same as scale-out: edit `spec.instances` in the manifest, review, apply, watch.

### Read-only endpoint unavailability when fewer than two instances are ready

CNPG does not publish a usable read-only service until at least two instances are ready. The operator surfaces this honestly: whenever `cnpgCluster.status.readyInstances < 2`, the access `ConfigMap` is generated with the RO endpoint values cleared:

- `CLUSTER_RO_ENDPOINT` is set to `""` (the key remains present so the contract is stable)
- `CLUSTER_POOLER_RO_ENDPOINT` is set to `""` (when the connection pooler is enabled)

In practice this applies to the steady-state case where `spec.instances=1`: CNPG holds `readyInstances=1`, the cluster reaches `phase=Ready`, and the access `ConfigMap` exposes the RW endpoints normally while the RO keys are empty.

Scaling is owned entirely by the cluster component: while a scale is in flight — `cnpgCluster.status.readyInstances` not yet equal to the desired `instances`, even while CNPG holds `phase=Healthy` — the cluster reports `Provisioning` and the reconcile short-circuits before the pooler and `ConfigMap` components run. They react only once the instance count has settled, so the access `ConfigMap` is never (re)generated against a transient mid-scale ready count and the prior contents are left in place. The RO endpoint value therefore reflects the settled ready count, not a momentary dip during a scale-out or scale-down.

Two distinct triggers govern the read-only path; they are intentionally split:

| Surface                                | Gated on                              | Why                                                                                                                |
| -------------------------------------- | ------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| `CLUSTER_RO_ENDPOINT` / `CLUSTER_POOLER_RO_ENDPOINT` value | `cnpgCluster.status.readyInstances < 2` | Reflects the settled ready count. The `ConfigMap` is only regenerated once a scale completes, so transient dips during a scale do not flap the value. |
| RO `Pooler` resource (`<cluster>-pooler-ro`) | `spec.connectionPooler.readOnly = false` (explicit opt-out) **or** `spec.instances < 2` (declared)        | Avoids deleting/recreating the resource on every transient `readyInstances` dip. Created/destroyed only when the declared count crosses the threshold or when the user toggles the explicit knob. |

Consumer guidance:

- applications must check whether the RO keys are non-empty before connecting; an empty value means there is no read replica and the consumer should fall back to the RW endpoint or fail explicitly
- the RO pooler resource (`<cluster>-pooler-ro`) is **not** created when either `spec.connectionPooler.readOnly = false` (user opted out explicitly) or `spec.instances < 2` (declared count below the RO threshold). When neither is true, the operator creates and tears down the RO `Pooler` automatically as the declared count crosses the threshold. The RW pooler is always present when the connection pooler is enabled and `readWrite` is not explicitly set to `false`. Programmatic consumers should rely on the `CLUSTER_POOLER_RO_ENDPOINT` ConfigMap key rather than querying the `Pooler` object directly — `kubectl get pooler <cluster>-pooler-ro` may return `NotFound` in either case.
- avoid running production workloads at `instances=1` longer than necessary

#### Explicit RW/RO pooler controls

The connection pooler exposes per-endpoint knobs on both the class and the cluster:

```yaml
# class default — RW + RO poolers are reconciled when enabled
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresClusterClass
metadata:
  name: postgresql-prod
spec:
  config:
    connectionPooler:
      enabled: true
      readWrite: true   # default
      readOnly: true    # default
```

```yaml
# cluster — overrides any sub-field independently
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresCluster
spec:
  class: postgresql-prod
  connectionPooler:
    readOnly: false     # opt out of the RO pooler explicitly
```

Sub-fields are merged independently, so a cluster can opt out of one endpoint while inheriting the rest from the class.

The admission webhook rejects `connectionPooler.readOnly = true` when the effective `instances < 2`, on both `CREATE` and `UPDATE`. Use `readOnly: false` to explicitly opt out of the RO pooler at lower instance counts. At least one of `readWrite` or `readOnly` must be enabled when `connectionPooler.enabled = true`.

Detecting RO unavailability without watching events:

```bash
kubectl get configmap -n $NS ${CLUSTER}-configmap \
  -o jsonpath='{.data.CLUSTER_RO_ENDPOINT}{"\n"}'
```

An empty value indicates the RO endpoint is unavailable.

### What to monitor during scaling

At the `PostgresCluster` level, expect:

- `status.phase=Provisioning` during the change
- `status.phase=Ready` after it completes

Common events include:

- `ClusterUpdateStarted`
- `ClusterReady`

You may also see additional namespace events from CNPG and Kubernetes:

- CNPG lifecycle events like `CreatingReplica`, `Switchover`, `FailOver`
- normal pod lifecycle events such as `Scheduled`, `Pulling`, `Started`, and `Killing`
