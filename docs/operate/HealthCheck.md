---
title: Health Checks
parent: Operate & Manage
nav_order: 8
---


# Splunk Operator Health Check with K8 Probes
Splunk Operator supports Startup, Liveness and Readiness Probes (with its own default values) for Splunk Custom Resources. The following probe configurations are allowed to be modified through Custom Resources: 
* initialDelaySeconds
* timeoutSeconds
* periodSeconds
* failureThreshold
* terminationGracePeriodSeconds (startup and liveness only)

Please refer to [Kubernetes documentation](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/) for more information on Startup, Liveness and Readiness Probes.

## Splunk Operator manager health

The probes on the Splunk Operator manager Pod have a different scope from the
probes on Splunk Enterprise Pods. They answer whether the Kubernetes control
plane component can participate in reconciliation; they do not report the
health of a Search Head, indexer, KV Store, or another Splunk service.

| Signal | Meaning | Must not be inferred |
| :--- | :--- | :--- |
| `GET /healthz` | The manager process and local probe server are alive | Kubernetes API access, leadership, controller progress, or Splunk health |
| `GET /readyz` | Every registered controller informer has completed its initial synchronization and the service account is currently authorized to `get`, `create`, and `update` the exact leader-election Lease | That this replica is the current leader or that every managed Splunk resource is healthy |
| `leader_election_master_status{name="270bec8c.splunk.com"}` | `1` for the current leader and `0` for a non-leading contender | A zero-valued contender is unhealthy |

The readiness distinction is important for both single-replica and highly
available Operator deployments. A process that cannot read or update its
leader Lease must not make a single-replica Deployment look operational. In a
multi-replica deployment, however, a synchronized and authorized standby is
Ready even though it is not the current leader. Current leadership is a role
that can transfer; it is not Pod health.

An informer cache-barrier runnable containing the complete controller informer
set is added before manager startup. The manager starts its health server
first, so `/readyz` begins false while `/healthz` is available even when API
discovery or initial lists are unavailable. In controller-runtime's cache
startup group, the barrier requests the informer set without blocking each
request and then waits for all of those informers to complete their initial
lists. Only after that barrier succeeds can the manager start the readiness
monitor and enter leader election. This explicit barrier is required because
an otherwise empty controller-runtime cache is technically synchronized even
though no controller watch has completed its initial list. The same barrier
applies to a future leader and every standby, which prevents a contender from
being advertised as ready or entering leader election while its controller
watches are still cold.

When leader election is enabled, the monitor also immediately submits three
exact `SelfSubjectAccessReview` requests and repeats them every 10 seconds with
one shared 3-second deadline. These reviews are read-only authorization
decisions and do not create or modify the Lease. Kubernetes documents
[`SelfSubjectAccessReview`](https://kubernetes.io/docs/reference/kubernetes-api/definitions/self-subject-access-review-v1-authorization/)
as the API used to determine whether the current identity may perform a
specific action.

The `get` and `update` reviews include the Lease name. The `create` review
targets the namespaced Lease collection without a resource name, matching the
actual Kubernetes create request.

The kubelet probe itself only reads the last in-memory result; it does not wait
on an API request. With the default Operator chart timing, a continuing
dependency failure is detected by the monitor within approximately 13 seconds
and changes the Pod Ready condition after three failed probes at 10-second
intervals. Recovery is detected by the next review and one successful
readiness probe. Exact timing changes if the chart probe values are overridden.

An API-server or Lease-authorization failure makes `/readyz` return HTTP 500,
but `/healthz` remains HTTP 200 while the process is alive. Readiness failure
does not cause kubelet to restart the container. This preserves logs, metrics,
and diagnostic state and avoids a restart loop that cannot repair an external
API or RBAC problem.

The manager publishes these bounded metrics:

- `splunk_operator_manager_readiness_status{check="cache_synchronized"}`;
- `splunk_operator_manager_readiness_status{check="leader_election_access"}`;
- `splunk_operator_manager_readiness_status{check="reconciliation_participation"}`;
- `splunk_operator_manager_readiness_transitions_total{state,reason}`; and
- `splunk_operator_manager_readiness_last_transition_timestamp_seconds`.

The `check`, `state`, and `reason` labels use fixed values and do not contain
resource names or error messages. Detailed API errors are written only to the
structured manager log. On result transitions, the manager also attempts one
Pod Event with reason `OperatorReconciliationReady`,
`OperatorReconciliationNotReady`, or `OperatorReconciliationRecovered`.
Repeated identical failures do not produce an Event or log for every probe.
Event delivery is best effort: an API outage can prevent the failure Event,
while local readiness, logs, and metrics still retain the signal.

The metrics endpoint uses delegated Kubernetes authentication and
authorization. The chart creates a `ClusterRole` that grants only
`get` on the non-resource `/metrics` path. Its name is
`splunk-operator-metrics-reader` for a cluster-wide installation and includes
an effective-namespace hash for a namespace-scoped installation. Bind the
Prometheus service account to that role; do not make the endpoint anonymous or
grant the Operator service account permission to read its own metrics merely
for scraping.

Example Prometheus alert boundaries are:

```promql
# No manager replica can participate in reconciliation. Alert after 2 minutes.
max by (namespace) (
  splunk_operator_manager_readiness_status{check="reconciliation_participation"}
) == 0

# Ready contenders exist, but none reports ownership of the leader Lease.
# Alert after 1 minute, longer than the default 20-second renew deadline.
sum by (namespace) (
  leader_election_master_status{name="270bec8c.splunk.com"}
) == 0
and on (namespace)
max by (namespace) (
  splunk_operator_manager_readiness_status{check="reconciliation_participation"}
) == 1

# An HA deployment has at least one capable manager and at least one degraded
# contender. Warn after 5 minutes; do not classify this as total unavailability.
count by (namespace) (
  splunk_operator_manager_readiness_status{check="reconciliation_participation"} == 0
) > 0
and on (namespace)
max by (namespace) (
  splunk_operator_manager_readiness_status{check="reconciliation_participation"}
) == 1
```

The `namespace` label in these examples is normally attached by the Prometheus
scrape target. Adapt the grouping label to the monitoring system's target
metadata. Do not alert on `leader_election_master_status == 0` for each Pod;
that is the expected value for every healthy standby.

For diagnosis, correlate the same time window across the following views:

```bash
kubectl get deployment,pod -n <operator-namespace>
kubectl get lease 270bec8c.splunk.com -n <operator-namespace> -o yaml
kubectl get events -n <operator-namespace> --sort-by=.lastTimestamp
kubectl logs -n <operator-namespace> deployment/splunk-operator-controller-manager
kubectl auth can-i get leases.coordination.k8s.io -n <operator-namespace> \
  --as=system:serviceaccount:<operator-namespace>:<operator-service-account>
kubectl auth can-i create leases.coordination.k8s.io -n <operator-namespace> \
  --as=system:serviceaccount:<operator-namespace>:<operator-service-account>
kubectl auth can-i update leases.coordination.k8s.io -n <operator-namespace> \
  --as=system:serviceaccount:<operator-namespace>:<operator-service-account>
```

The three authorization checks must all return `yes`. A Ready Pod with leader
metric zero is a normal standby when another replica owns the Lease. A manager
with both readiness prerequisites equal to one but no leader anywhere requires
leader-election diagnosis rather than a liveness restart. Splunk workload
conditions and Splunk Pod probes must be investigated separately.

`cache_synchronized` is an initial informer-synchronization latch. A later
selective removal of non-Lease list/watch RBAC is reported by controller-runtime
watch errors in the manager log; controller-runtime does not expose a supported
per-informer ongoing-health signal to this probe. A general API outage also
causes the periodic Lease reviews to fail and withdraws readiness. Treat a
post-start stream of `Failed to watch` or `forbidden` messages as a controller
availability incident even if only selective non-Lease access was removed.

## Default probe values

| Probe Type | initialDelaySeconds | timeoutSeconds | periodSeconds | failureThreshold | terminationGracePeriodSeconds |
| :--- | :--- | :--- | :--- | :--- | :--- |
| Startup Probe | 40 | 30 | 30 | 60 | 660 with `SplunkPodLifecycle`; otherwise Pod value |
| Readiness Probe | 10 | 5 | 5 | 3 | Not supported by Kubernetes |
| Liveness Probe | 30 | 30 | 30 | 3 | 660 with `SplunkPodLifecycle`; otherwise Pod value |

The startup failure budget is approximately 30 minutes. Startup protects
first start and upgrade work; liveness and readiness do not begin until startup
succeeds.

When `SplunkPodLifecycle` is enabled, an existing v4 resource that contains the
exact previous default tuple (`40/30/30/12`) resolves to the new failure
threshold. A probe with any customized tuple is preserved.

Probe-level termination grace controls a container restart caused by a failed
startup or liveness probe. It is separate from the Pod-level
`spec.terminationGracePeriodSeconds` used when Kubernetes deletes a Pod. When
`SplunkPodLifecycle` supplies the 660-second probe default, the current Splunk
image has 600 seconds for its bounded local shutdown and 60 seconds of kubelet
margin. If `SPLUNK_SHUTDOWN_TIMEOUT_SECONDS` is increased in the image, set the
startup and liveness probe grace to a correspondingly larger value.

The following example shows how to modify the defaults.

### Example to configure Probes for Startup, Liveness and Readiness

```yaml
apiVersion: enterprise.splunk.com/v4
kind:  Standalone
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1
  startupProbe:
    initialDelaySeconds: 40
    timeoutSeconds: 30
    periodSeconds: 30
    failureThreshold: 60
    terminationGracePeriodSeconds: 660
  livenessProbe:
    initialDelaySeconds: 30
    timeoutSeconds: 30
    periodSeconds: 30
    failureThreshold: 3
    terminationGracePeriodSeconds: 660
  readinessProbe:
    initialDelaySeconds: 10
    timeoutSeconds: 5
    periodSeconds: 5
    failureThreshold: 3
```
