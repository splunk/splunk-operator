# preStop Lifecycle Hook: Role-Aware Decommission, Detention, and Graceful Stop

**Type:** Story
**EPIC:** Kubernetes-Native Pod Lifecycle Management for Splunk Operator
**Component:** `tools/k8_probes/preStop.sh`, `pkg/splunk/enterprise/configuration.go`

---

## Background

When Kubernetes terminates a pod, it runs the `preStop` lifecycle hook before sending SIGTERM to the container. The hook runs synchronously — Kubernetes waits for it to complete (or for `terminationGracePeriodSeconds` to expire, after which SIGKILL is sent) before proceeding with container termination.

This makes the preStop hook the correct place to perform Splunk-specific graceful shutdown that must complete before the Splunk process stops:

- **Indexers:** Must call the decommission API to tell the cluster manager this peer is going away. With `enforce_counts=1` (scale-down), this migrates all primary buckets to remaining peers before the pod stops. With `enforce_counts=0` (restart), this tells the cluster to stop routing new data here but does not migrate buckets — the pod will be back momentarily.
- **Search heads:** Must call the detention/removal API to remove themselves from the cluster consensus before stopping. Without this, the SHC captain may try to coordinate with a member that is no longer running.
- **All roles:** Must call `splunk stop` to give the Splunk process time to flush its write-ahead log and close index files cleanly. Sending SIGTERM directly (which is what Kubernetes does without a preStop hook) does not guarantee clean shutdown.

The preStop hook runs inside the pod container, where it has direct `localhost:8089` access to the Splunk management API. No RBAC, no network policy exceptions, and no service discovery are needed.

---

## What Needs to Be Built

### 1. `tools/k8_probes/preStop.sh`

A bash script that handles shutdown for every Splunk role. The script is mounted into the container at a well-known path and configured as the `preStop` exec command in the pod spec.

#### Required environment variables (supplied by the pod spec)

| Variable | Source | Purpose |
|---|---|---|
| `SPLUNK_ROLE` | Static env var | Determines which shutdown path to take |
| `POD_NAME` | Downward API env var (`metadata.name`) | Used for log context and peer lookup |
| `POD_NAMESPACE` | Downward API env var (`metadata.namespace`) | Used for log context |
| `SPLUNK_CLUSTER_MANAGER_URL` | Static env var (indexers only) | Endpoint for polling decommission status |

The Splunk admin password is read from a mounted secret file at `/mnt/splunk-secrets/password`. It must not be passed as an environment variable.

#### Intent reading

The script reads `/etc/podinfo/intent` (Downward API volume, see the pod-intent-annotation-downward-api story). If the file does not exist or is empty, the script defaults to `"serve"`. The value is trimmed of whitespace.

#### Shutdown paths by role

**`splunk_indexer`:**
1. Read intent from `/etc/podinfo/intent`.
2. Call `POST localhost:8089/services/cluster/peer/control/control/decommission` with:
   - `enforce_counts=1` if intent is `scale-down`
   - `enforce_counts=0` if intent is `serve` or `restart`
3. Poll the cluster manager (`SPLUNK_CLUSTER_MANAGER_URL/services/cluster/manager/peers`) every 10 seconds until the peer's status is `Down` or `GracefulShutdown`, or until `DECOMMISSION_MAX_WAIT` seconds have elapsed. Log status at each poll with elapsed time. If timeout is reached, log an error and continue (do not block `splunk stop`).
4. Call `splunk stop` with `STOP_MAX_WAIT` seconds timeout (clamped to remaining budget — see timeout section below).

**`splunk_search_head`:**
1. Check if this member is currently registered in the cluster: `GET localhost:8089/services/shcluster/member/info?output_mode=json`. If `is_registered` is false, skip detention.
2. Call `POST localhost:8089/services/shcluster/member/consensus/default/remove_server`. Treat HTTP 503 as "already removed" and proceed.
3. Poll `GET localhost:8089/services/shcluster/member/info` every 5 seconds until `is_registered` is false, or until `DECOMMISSION_MAX_WAIT` seconds have elapsed.
4. Call `splunk stop` with remaining budget timeout.

**`splunk_standalone`, `splunk_ingestor`, `splunk_cluster_manager`, `splunk_cluster_master`, `splunk_license_manager`, `splunk_license_master`, `splunk_monitoring_console`, `splunk_deployer`:**
Call `splunk stop` only.

#### Timeout budget management

The total time available to the preStop hook is bounded by `terminationGracePeriodSeconds`. The budget must be split between the decommission/detention phase and the `splunk stop` phase. A fixed allocation for each role:

| Role | `terminationGracePeriodSeconds` | `DECOMMISSION_MAX_WAIT` | `STOP_MAX_WAIT` | Buffer |
|---|---|---|---|---|
| `splunk_indexer` | 1020s | 900s | 90s | 30s |
| `splunk_search_head` | 360s | 300s | 50s | 10s |
| All others | 120s | 80s | 30s | 10s |

`DECOMMISSION_MAX_WAIT` and `STOP_MAX_WAIT` are read from environment variables `PRESTOP_DECOMMISSION_WAIT` and `PRESTOP_STOP_WAIT` with the above values as defaults. This allows tuning without rebuilding the image.

**Cumulative tracking:** `TOTAL_BUDGET` is computed as `DECOMMISSION_MAX_WAIT + STOP_MAX_WAIT`. `SCRIPT_START_TIME` is set at the top of the script before the role-detection block. In `stop_splunk`, the actual stop timeout is:
```
remaining = TOTAL_BUDGET - (now - SCRIPT_START_TIME)
actual_stop_timeout = min(STOP_MAX_WAIT, remaining)
```
If `remaining <= 0`, use a minimal timeout (5 seconds) and log a warning. This prevents a long decommission from consuming the stop budget entirely, which would prevent `splunk stop` from running and would leave Splunk processes running when SIGKILL arrives.

#### Logging

Every action logs a timestamp and structured fields:
```
[INFO] 2025-02-23 14:01:01 - Starting preStop hook for pod: splunk-idx-indexer-2, role: splunk_indexer
[INFO] 2025-02-23 14:01:01 - Pod intent: scale-down
[INFO] 2025-02-23 14:01:01 - Scale-down detected: decommission with enforce_counts=1 (rebalance buckets)
[INFO] 2025-02-23 14:03:22 - Decommission in progress (141s elapsed), status: ReassigningPrimaries
[INFO] 2025-02-23 14:16:01 - Decommission complete after 900s, peer status: GracefulShutdown
[INFO] 2025-02-23 14:16:01 - Stopping Splunk gracefully (timeout: 90s, elapsed: 901s, budget: 990s)...
[INFO] 2025-02-23 14:16:47 - Splunk stopped successfully
```

Errors go to stderr.

### 2. Configure preStop hook and `terminationGracePeriodSeconds` in pod template

In `updateSplunkPodTemplateWithConfig` (`configuration.go`), for the Splunk container:

```yaml
lifecycle:
  preStop:
    exec:
      command: ["/bin/bash", "/opt/splunk-operator/k8_probes/preStop.sh"]
```

Set `terminationGracePeriodSeconds` on the pod spec based on role:

| `instanceType` | `terminationGracePeriodSeconds` |
|---|---|
| `SplunkIndexer` | 1020 |
| `SplunkSearchHead` | 360 |
| All others | 120 |

### 3. Inject required environment variables into pod spec

`POD_NAME` and `POD_NAMESPACE` must be injected via Downward API env vars (they are not static). `SPLUNK_CLUSTER_MANAGER_URL` is already set for indexers by existing code.

```yaml
env:
- name: POD_NAME
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: POD_NAMESPACE
  valueFrom:
    fieldRef:
      fieldPath: metadata.namespace
```

---

## Acceptance Criteria

1. When an indexer pod with intent `scale-down` receives a termination signal, the preStop script calls `decommission` with `enforce_counts=1`, polls cluster manager status until `Down` or `GracefulShutdown`, then calls `splunk stop`.
2. When an indexer pod with intent `serve` (config change / restart) receives a termination signal, the preStop script calls `decommission` with `enforce_counts=0` (no bucket migration).
3. When a search head pod receives a termination signal, the preStop script calls `remove_server`, polls until `is_registered` is false, then calls `splunk stop`.
4. When a standalone, ingestor, cluster manager, license manager, or other non-cluster role receives a termination signal, the preStop script calls only `splunk stop`.
5. If the decommission wait exhausts `DECOMMISSION_MAX_WAIT` seconds, the script logs an error and proceeds to `splunk stop` without hanging indefinitely.
6. If `DECOMMISSION_MAX_WAIT` is nearly exhausted, `stop_splunk` clamps its timeout to the remaining budget (`TOTAL_BUDGET - elapsed`) so that the total script runtime never exceeds `TOTAL_BUDGET`.
7. If `PRESTOP_DECOMMISSION_WAIT` or `PRESTOP_STOP_WAIT` environment variables are set, they override the defaults and `TOTAL_BUDGET` reflects the overridden values.
8. The Splunk admin password is read from the file at `SPLUNK_PASSWORD_FILE`, never from an environment variable.
9. `SPLUNK_ROLE`, `POD_NAME`, `POD_NAMESPACE` are present as environment variables in every pod created by the operator for all cluster types.
10. `terminationGracePeriodSeconds` is set to 1020 for indexers, 360 for search heads, and 120 for all other roles.
11. All Splunk API calls use `https://localhost:${MGMT_PORT}` — no service discovery, no DNS.
12. Every log line includes a timestamp. Error lines go to stderr.

---

## Definition of Done

- [ ] `tools/k8_probes/preStop.sh` written with all role paths, timeout budget tracking, and logging.
- [ ] preStop hook exec command configured in pod template for all StatefulSet-managed instance types.
- [ ] `terminationGracePeriodSeconds` set correctly per role in `updateSplunkPodTemplateWithConfig`.
- [ ] `POD_NAME` and `POD_NAMESPACE` Downward API env vars added to pod template.
- [ ] Script is executable and included in the operator Docker image at the correct path.
- [ ] Manual test: indexer pod terminated during active decommission — preStop logs show status polling, completes cleanly.
- [ ] Manual test: indexer pod terminated for restart — preStop logs show `enforce_counts=0`, completes in under 2 minutes.
- [ ] Manual test: search head pod terminated — detention completes, `is_registered` becomes false before `splunk stop`.
