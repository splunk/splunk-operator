# Make Operator readiness represent reconciliation participation

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

Kubernetes must not report the Splunk Operator Deployment Available merely
because the manager's HTTP health server is listening. A manager can be alive
while its caches have not synchronized or its service account cannot
participate in leader election; in that state no Splunk controller is able to
reconcile.

After SHC-93, liveness remains a process-health signal and cannot restart the
Pod for an API-server, cache, or RBAC problem. Readiness becomes a bounded
reconciliation-participation signal. It stays false until controller-runtime
has completed cache startup and the running service account can perform the
Lease operations required by leader election in the Operator namespace. The
same contract applies to the active leader and to a healthy non-leading
contender, so an HA standby is not incorrectly presented as failed. Loss and
recovery are visible through the Pod condition, stable structured logs,
Kubernetes Events when the API permits them, and bounded Prometheus metrics.

## Progress

- [x] (2026-08-02 00:35Z) Created isolated worktree
  `/Users/viveredd/Projects/splunk-operator-shc93-operator-readiness` and
  branch `codex/shc-93-operator-readiness` from the completed SHC-92
  documentation tip `4c306ec05`.
- [x] (2026-08-02 00:43Z) Audited `cmd/main.go`, the Operator Helm chart,
  static manager manifest, controller-runtime v0.24.1 startup ordering, and
  live EKS service-account authorization.
- [x] Selected the readiness contract and test boundaries described below.
- [x] (2026-08-02 00:49Z) Added failing unit and Helm assertions before
  changing manager behavior. The new package failed to compile only on its
  intentionally absent contract; the focused chart suite passed 21 tests and
  failed exactly the missing `POD_NAMESPACE` assertion.
- [x] (2026-08-02 01:05Z) Implemented the cache-start and exact Lease-access
  readiness monitor, bounded transition telemetry, manager registration, and
  Pod namespace/UID downward-API inputs.
- [x] Corrected the authorization design from rules enumeration to three exact
  `SelfSubjectAccessReview` decisions after checking the Kubernetes API
  contract. `get` and `update` name the Lease; collection `create` correctly
  leaves the name empty.
- [x] (2026-08-02 01:19Z) Passed focused race tests, manager compilation, all
  three Kustomize renders, `make build`, `make helm-check` with 54 Operator and
  85 Universal Forwarder tests, and a clean full macOS `make test` with 43
  suites and all 185 enterprise-controller specs in 132.589 seconds.
- [x] (2026-08-02 01:19Z) Committed and pushed the first source increment as
  `47cd2d3ba`; reproduced it on Linux, passed 43 suites and 185 enterprise
  specs, and published the now-superseded image digest
  `sha256:b07f7b0a6406123bdb6acc1009d0f45f683fe104873652c7550d808b91663254`.
- [x] (2026-08-02 01:30Z) Corrected namespace-scoped metrics-server RBAC and
  published a namespace-unique metrics-reader role in commits `6e8a3e79a` and
  `2bf8f8626`; all 59 Operator and 85 Universal Forwarder Helm tests pass.
- [x] (2026-08-02 01:35Z) Qualified the missing-Lease-RBAC cold start and
  in-place recovery on EKS: health stayed 200, readiness was 500, the
  Deployment was unavailable, cache/Lease/aggregate metrics were 1/0/0, and
  restoring the binding recovered the same Pod UID with zero restarts.
- [x] (2026-08-02 01:52Z) Rejected the original cache assumption after a true
  EKS cold-start denial showed that an empty controller-runtime informer set
  is considered synchronized. Added an initial explicit informer attempt and
  passed the revised local race, build, vet, and complete `make test` gates:
  43 suites and 185/185 enterprise specs in 131.987 seconds.
- [x] (2026-08-02 01:58Z) Committed and pushed that correction as
  `980ce6ece`, reproduced all 43 suites and 185/185 enterprise specs on Linux,
  and published image digest
  `sha256:c3e82b8e761d87caac59e961051b81ad6da225359dd3c50550bad31cd0cc4a83`.
- [x] (2026-08-02 02:12Z) Rejected the `Warmup` hook as a cache/leadership
  barrier after exact-image EKS qualification. With list/watch RBAC denied,
  the manager acquired the Lease; controller sources timed out after two
  minutes and the process exited. Replaced the hook with non-blocking informer
  pre-registration before `Manager.Start`, so controller-runtime's native
  cache runnable owns the synchronization barrier.
- [x] (2026-08-02 02:23Z) Committed and pushed direct informer
  pre-registration as `f7b123f59`, passed the complete Linux gate, and
  published image digest
  `sha256:e85e4e4c5be3ea777def3d870791b9c2435b7e7b309dddf957f87b3087d34a7d`.
- [x] (2026-08-02 02:50Z) Rejected direct pre-start registration after the
  active-leader API fault showed that restart-time REST mapping failed before
  the manager health server could start. Moved registration and retry into a
  cache-group barrier, preserving the native pre-leadership synchronization
  boundary while allowing health serving during API discovery failure.
- [x] (2026-08-02 03:43Z) Committed and pushed the complete cache-group
  barrier and secure metrics corrections through exact source `90103bef5`,
  reproduced the final source gates on Linux, and published the immutable
  Operator image and packaged chart recorded in the qualification document.
- [x] (2026-08-02 04:28Z) Qualified normal startup, true cold-start
  list/watch denial, missing Lease RBAC, same-Pod recovery, active-leader API
  interruption, two-contender leader/standby readiness, takeover, bounded
  telemetry, cleanup, and retained-workload health on EKS.
- [x] (2026-08-02 04:46Z) Recorded exact artifacts, observations, rejected
  candidates, and remaining boundaries in
  `SHC93OperatorReadinessQualification.md` and aligned the program indexes and
  scenario matrices. Documentation is committed separately from source.

## Surprises & Discoveries

- Observation: the current liveness and readiness endpoints are equivalent.
  Evidence: `cmd/main.go` registers `healthz.Ping` for both `healthz` and
  `readyz`; the checker always returns success.
- Observation: a listening health server is intentionally earlier than
  controller readiness in controller-runtime.
  Evidence: controller-runtime v0.24.1 starts its health, metrics, and pprof
  HTTP servers before webhooks, cache synchronization, non-leader runnables,
  warmup, leader election, and leader-only controller runnables.
- Observation: cache synchronization can define one common readiness boundary
  for leaders and standbys.
  Evidence: controller-runtime starts and waits for the shared cluster cache
  before starting leader election; this occurs in every manager replica, not
  only in the elected replica.
- Observation: `Manager.Elected()` cannot be the Pod readiness signal for an
  HA deployment.
  Evidence: the channel closes only for the elected manager. A healthy
  non-leading contender remains intentionally open and would be permanently
  NotReady if readiness were gated directly on that channel.
- Observation: the checked-in Helm chart currently renders one replica with a
  `Recreate` Deployment strategy, while controller-runtime and prior SHC-85
  qualification support multiple contenders when the Deployment is scaled for
  a controlled test.
  Evidence: `templates/deployment.yaml` hard-codes `replicas: 1`; a prior EKS
  campaign ran one leader and one Ready standby and completed a normal
  takeover.
- Observation: the service account can use the stable Kubernetes
  `SelfSubjectAccessReview` API without chart-specific authorization.
  Evidence: live EKS authorization for the retained Operator service account
  permits creation of `selfsubjectaccessreviews.authorization.k8s.io`, and its
  namespace Role permits the Lease `get`, `create`, and `update` operations
  used by client-go leader election.
- Observation: the initially considered rules-summary API is not a valid
  correctness boundary.
  Evidence: the Kubernetes API reference says `SelfSubjectRulesReview` can be
  incomplete and must not drive decisions in external systems; it identifies
  exact access reviews as the correct authorization mechanism. SHC-93 changed
  to three exact `SelfSubjectAccessReview` requests before implementation
  qualification.
- Observation: the existing probe-path assertion was already green before the
  implementation.
  Evidence: the test-first Helm run passed 21 tests, including distinct
  `/healthz` and `/readyz` paths, and failed only the new Pod-namespace input.
- Observation: the repository's enterprise envtest suite is not safely
  repeatable while a preceding suite-owned manager or control-plane process is
  still alive.
  Evidence: the first complete macOS gate passed, but an immediate repeat
  encountered the existing out-of-band `Expect` in
  `internal/controller/enterprise/suite_test.go` after a second manager could
  not start, then could not finalize its missing coverage profile. Exact
  suite-owned Ginkgo, test, etcd, and kube-apiserver processes were stopped;
  a clean repeat then passed 43 suites in 4m2.548s. SHC-93 did not change this
  test harness, and Linux qualification must begin from a process-clean host.
- Observation: protected metrics were not usable for a namespace-scoped Helm
  installation.
  Evidence: the chart rendered TokenReview and SubjectAccessReview permissions
  in a namespaced Role even though both APIs are cluster scoped; an
  authenticated scrape returned HTTP 500. After that was fixed, the scrape
  correctly returned 403 until a separate identity was bound to non-resource
  `/metrics` access.
  Consequence: namespace-scoped releases now receive uniquely named
  cluster-scoped delegated-authentication RBAC and a uniquely named,
  least-privilege metrics-reader role. The reader role remains deliberately
  unbound until a monitoring identity is selected.
- Observation: merely starting and waiting on the controller-runtime cache
  runnable does not prove that a controller informer has synchronized.
  Evidence: with the manager RoleBinding absent and `kubectl auth can-i list
  pods` already returning `no`, the original SHC-93 image reported
  `cache_synchronized=1`, became Ready, acquired the Lease, and then logged
  repeated forbidden failures for every controller watch. The cache had no
  informer when its initial `WaitForCacheSync` snapshot was taken, and an empty
  informer set returns success.
  Consequence: the first cache design was invalid and its image is
  superseded. Every enabled controller informer must be requested before the
  manager starts, so the native cache startup group has a complete set to
  synchronize.
- Observation: controller-runtime v0.24.1's `WarmupRunnable` interface does
  not wait for the `Warmup` function to return before leader election.
  Evidence: its runnable group uses an always-true readiness callback for the
  warmup function. In the exact-image EKS cache-denial test, the manager
  acquired the Lease while explicit informer synchronization was still
  blocked. Two minutes later, leader-only controller sources timed out and
  `Manager.Start` returned an error, causing the container to restart; this
  was a process exit, not a kubelet liveness failure.
  Consequence: image digest
  `sha256:c3e82b8e761d87caac59e961051b81ad6da225359dd3c50550bad31cd0cc4a83`
  is also superseded. The next image registered the full informer set with
  `BlockUntilSynced(false)` before `Manager.Start`, which made the native cache
  runnable synchronize that set before non-leader runnables or leader
  election. The following API-fault observation then exposed why registration
  itself must move after HTTP server startup.
- Observation: direct informer registration before `Manager.Start` performs
  REST mapping and can fail before the health server starts.
  Evidence: during an exact-image active-leader API fault, controller-runtime
  exited the leader after its renew deadline and a Ready standby took over.
  The fault persisted in the original Pod network namespace while the manager
  container restarted. Each new process exited at informer registration with
  `failed to get server groups`, and kubelet entered CrashLoopBackOff. This was
  process exit, not a failed liveness probe.
  Consequence: digest
  `sha256:e85e4e4c5be3ea777def3d870791b9c2435b7e7b309dddf957f87b3087d34a7d`
  is superseded. The informer set is now registered from a cache-group
  runnable after HTTP servers start. Its cache readiness callback retries API
  discovery and then waits for the full set to synchronize; the manager cannot
  enter leader election while the barrier is false.
- Observation: deleting RBAC and immediately starting a Pod is not a reliable
  cold-start fixture because Kubernetes authorization decisions can remain
  cached briefly.
  Evidence: one attempt synchronized before the permission removal propagated,
  then began logging watch denials about 34 seconds later.
  Consequence: cache-denial qualification requires a negative `kubectl auth
  can-i` precondition before the replacement Pod is created.
- Observation: a secure metrics listener is not sufficient unless the chart
  also publishes the correct port and delegated-authentication RBAC.
  Evidence: the manager first exposed authenticated HTTPS metrics on 8443,
  but the namespace-scoped chart's TokenReview and SubjectAccessReview rules
  were ineffective in a namespaced Role and the Service did not target the
  listener. Final chart tests and an authenticated EKS scrape proved the
  cluster-scoped delegated-authentication role, metrics-reader role, Service,
  and listener agree.
  Consequence: SHC-93 includes the minimum chart transport and authorization
  needed to consume its manager metrics; binding a monitoring identity to the
  unbound reader role remains an installation decision.
- Observation: ordinary Service endpoint publication hides telemetry from a
  manager precisely while its readiness probe is false.
  Evidence: after the Lease-denied Pod became NotReady, its address was absent
  until the manager-only metrics Service used
  `publishNotReadyAddresses: true`. The final EKS fixture then retained
  `10.0.62.123:8443` and exposed cache/Lease/aggregate values `1/0/0` through
  the Service.
  Consequence: only the Operator metrics Service publishes NotReady manager
  addresses. This does not affect any Splunk client-facing Service.

## Decision Log

- Decision: Keep `/healthz` as a local process ping and never include API,
  cache, Lease, current-leader, or Splunk-cluster health in liveness.
  Rationale: restarting a live manager during an API or authorization incident
  cannot repair the dependency and can create a restart loop that removes
  diagnostic state.
  Date/Author: 2026-08-02, Codex with Vivek Reddy.
- Decision: Define `/readyz` as cache startup complete plus current permission
  to `get`, `create`, and `update` the exact leader Lease in the Operator
  namespace.
  Rationale: these are the operations used by the Lease resource lock. The
  check detects the SHC-92 failure before Kubernetes calls the Deployment
  Available and remains valid for both the current leader and a contender.
  Date/Author: 2026-08-02, Codex with Vivek Reddy.
- Decision: Do not gate Pod readiness on `Manager.Elected()`.
  Rationale: current leadership is a role, not instance health. A synchronized,
  authorized standby must remain Ready so it can take over without being
  classified as a failed Pod.
  Date/Author: 2026-08-02, Codex with Vivek Reddy.
- Decision: Add a cache-group runnable whose cache readiness callback
  registers the complete enabled-controller informer set and waits for it to
  synchronize. Use a non-leader monitor runnable to record that the barrier
  was crossed and use the Kubernetes authorization API for a read-only Lease
  capability review.
  Rationale: the EKS cold-start test disproved the assumption that the manager
  cache boundary automatically contains controller informers, and the next
  tests disproved both the `WarmupRunnable` completion assumption and direct
  pre-start registration during API failure. The cache-group runnable starts
  after HTTP servers but still holds the native pre-leadership cache boundary
  for every contender. Authorization reviews test all Lease verbs without
  creating, updating, or competing for the real Lease.
  Date/Author: 2026-08-02, Codex with Vivek Reddy.
- Decision: Run the capability review immediately and periodically with a
  bounded request timeout; readiness reads only the most recent in-memory
  result.
  Rationale: kubelet probes remain fast and do not initiate API calls, while
  an RBAC or API failure and its recovery are discovered without restarting
  the manager.
  Date/Author: 2026-08-02, Codex with Vivek Reddy.
- Decision: Keep current-leader state separate in the existing client-go
  leader-election metric and logs.
  Rationale: combining leader role with participation readiness would lose HA
  semantics. Operators and alerts need both signals to distinguish a healthy
  standby, an unavailable control plane, and an ordinary takeover.
  Date/Author: 2026-08-02, Codex with Vivek Reddy.

## Outcomes & Retrospective

SHC-93 completed its bounded source and EKS qualification at exact source
`90103bef5d87546cadc419738752a0d6b0cd813e`. The accepted contract keeps
`/healthz` process-local, makes `/readyz` require the complete initial
controller informer barrier plus current Lease authorization, and leaves
current leadership as a separate role metric. A cold list/watch or Lease
denial kept the process healthy but the Deployment unavailable; restoring
access recovered the same Pod without a restart. Two Ready contenders retained
one leader and completed a 35-second takeover. An active leader exited after
it could no longer renew its Lease, while its API-isolated restart remained
healthy/NotReady behind the informer barrier instead of CrashLooping.

Final macOS and Linux gates passed 43 suites, all 185 enterprise controller
specs, build, Helm, Kustomize, and focused race checks. The accepted image OCI
index is
`sha256:b5a022a788c7cacf8b7ee33e7132eae56d82b14eb631809ddd116c8b816e9d63`;
the packaged chart SHA-256 is
`008abda67d13775ce6cd7e0f8e77365edce01af82f6ad9c12ecf34911a2f6925`.
Cleanup removed every SHC-93 fixture and retained the SHC-85 workload at 3/3
Ready with zero restarts. Provider/version breadth, productized manager HA,
post-start per-informer health, and production alert delivery remain outside
this bounded result. Detailed evidence is in
`SHC93OperatorReadinessQualification.md`.

## Context and Orientation

`cmd/main.go` creates the controller-runtime manager. The SHC-93 source keeps
an unconditional process ping for `/healthz` and registers the
reconciliation-participation checker for `/readyz`. The chart values in
`helm-chart/splunk-operator/values.yaml` send liveness to `/healthz` and
readiness to `/readyz`; the Deployment template always enables manager leader
election. The leader-election Role grants broad Lease verbs in the effective
Operator namespace.

Controller-runtime v0.24.1 starts the probe server first, starts and waits for
its cache runnables, starts non-leader runnables, and then enters leader
election. Controllers are leader-only runnables. Its cache wait only covers
informers requested before the wait snapshot; an empty informer set is
therefore successfully synchronized even though no controller watch has
completed an initial list. SHC-93 adds a cache-group runnable whose cache
readiness callback registers every informer type used by the enabled
controllers with `BlockUntilSynced(false)`, retries registration while API
discovery is unavailable, and then waits for the complete set to synchronize.
Because HTTP servers precede the cache group, health remains available while
this barrier is false. Only after it succeeds does the readiness monitor's
`Start` method record cache readiness and run the periodic Lease authorization
review on each replica. Its readiness checker returns only the combined
in-memory snapshot and never blocks kubelet on the API server.

Kubernetes `SelfSubjectAccessReview` asks the API server whether the calling
identity may perform one exact action. It is the API used by `kubectl auth
can-i` and works across authorization modes. SHC-93 submits one review for
each Lease verb client-go's `LeaseLock` requires: `get`, `create`, and
`update`, with the exact API group, resource, namespace, and request-path name
semantics. Any denial, missing response, timeout, or API error fails the
aggregate check.
Leader election disabled for local development skips the Lease reviews but
still waits for cache startup.

The access attributes mirror the Kubernetes request paths: `get` and `update`
name `270bec8c.splunk.com`; `create` targets the namespaced Lease collection
and therefore leaves `resourceAttributes.name` empty. Kubernetes cannot
authorize a create request through a `resourceNames` restriction because the
object name is not part of the authorization attributes for collection create.

## Plan of Work

Create `pkg/operatorreadiness` as process-level Operator infrastructure rather
than Splunk domain logic. Add tests for the initial NotReady state, cache-start
transition, leader-election-disabled mode, exact action attributes and order,
denied/missing verbs, missing responses, API errors, repeated identical
failures, recovery, and concurrent checks. Add a fake recorder so
state-transition Event behavior is deterministic. Add metric assertions with
bounded labels.

Register the monitor with the manager before `Start`. Preserve
`AddHealthzCheck("healthz", healthz.Ping)`. Replace the unconditional readiness
ping with a named reconciliation-participation checker. Pass the effective
leader-election namespace, Lease name, service-account Pod identity, request
timeout, and refresh period. Add `POD_NAMESPACE` to both Helm and static
manager manifests through the downward API.

The monitor begins with both cache and Lease checks at zero. A cache-group
runnable registers every enabled controller's informer with
`BlockUntilSynced(false)` after the manager's HTTP servers start, retries API
discovery failure, and waits for the complete set before the manager can start
the monitor or leader election. The monitor's `Start` method records that the
cache barrier was crossed, performs an immediate bounded authorization review,
and refreshes it periodically. A result transition changes gauges and
transition counters, emits one structured log, and attempts one Kubernetes
Event. Identical repeated results do not create an Event or log storm.
Readiness returns success only while both signals are true. Liveness remains
independent.

Update Helm tests to prove the two probe paths remain distinct and the
namespace environment is sourced from the Pod. Add Operator documentation
that explains exactly what each endpoint means, how a standby is represented,
the expected failure timing, Prometheus queries, alert recommendations, and a
diagnostic sequence that correlates Pod conditions, Lease holder, Events,
logs, and metrics.

Run focused package tests, `make fmt`, `make test`, `make build`, and `make
helm-check` on macOS. Commit the source and user documentation, push the exact
commit, fetch it on the Linux vWorkstation, and repeat the gates. Build and
push an immutable linux/amd64 Operator image through the repository Make
target, package the chart, and record both digests.

On EKS, first install a disposable single-replica Operator with correct RBAC
and confirm readiness occurs only after cache and authorization success. Remove
only the Lease RoleBinding and prove `/healthz` remains 200, `/readyz` becomes
500, the Pod remains running without a container restart, and the Deployment
becomes unavailable. Restore the binding and prove readiness and reconciliation
recover without replacing the Pod. Repeat with a bounded Pod-local API path
interruption.

Scale a disposable Deployment to two contenders. Confirm both caches and Lease
permissions become Ready while exactly one replica reports leader state. Delete
the leader Pod and prove the standby remains Ready, acquires the Lease, starts
controllers, and produces no readiness/liveness restart loop. Exercise a
repeated denial/recovery cycle to prove telemetry aggregation and alert timing.
Do not mutate the retained SHC workload; verify its Pod UIDs, readiness, restart
counts, and cluster health before and after the campaign.

## Validation and Acceptance

SHC-93 is accepted only when all of the following are true:

- `/healthz` remains 200 for a live process during Lease RBAC and API failures;
- `/readyz` is false before cache completion and whenever any latest bounded
  Lease action review is denied or unavailable;
- the SHC-92 missing-Lease-RBAC topology is not Deployment Available;
- restoring RBAC or API access makes the same Pod Ready without a restart;
- a healthy leader and non-leading contender both remain Ready;
- takeover starts controllers on the contender without classifying it as a
  failed Pod;
- Events and logs occur on transitions rather than every probe;
- metrics distinguish cache readiness, Lease capability, aggregate readiness,
  role ownership, and transitions with bounded labels;
- alert examples separate no-ready-manager, no-leader, and degraded-standby
  cases and include a transition tolerance;
- complete macOS and Linux Make gates pass on the exact source;
- EKS uses immutable image and chart digests with recorded Kubernetes version;
  and
- all disposable resources are naturally removed while retained workloads
  remain healthy and unchanged.

## Idempotence and Recovery

The monitor performs read-only authorization reviews and keeps only process
memory. Repeating tests does not mutate the leader Lease or Splunk resources.
Event publication is best-effort and transition-bounded. If the API is
unavailable, the failure remains visible through readiness, logs, and local
metrics; the recovery transition can publish after access returns.

EKS fixtures use a dedicated namespace and release name. RBAC denial is created
by removing only the disposable leader-election binding and recovered by
reapplying the exact rendered object. API interruption is scoped to the
disposable Pod. Cleanup uses normal Helm uninstall and namespace deletion; it
does not patch finalizers or delete retained resources.

## Artifacts and Notes

Record the following in `SHC93OperatorReadinessQualification.md`:

- source commits and official remote branch;
- macOS and Linux suite/spec/coverage totals;
- Operator image repository, tag, and digest;
- chart package name, size, and SHA-256;
- EKS context and server version;
- timestamped health/readiness, Pod condition, Deployment availability, Lease,
  leader metric, readiness metrics, Event, log, and restart evidence for every
  scenario;
- exact RBAC and API fault boundaries and recovery commands;
- retained Operator and SHC before/after invariants; and
- unqualified provider, version, topology, scale, and alerting boundaries.

## Interfaces and Dependencies

The new package exposes a manager runnable and readiness checker. Its external
dependencies are controller-runtime's manager contract, the stable Kubernetes
authorization v1 API, the manager Event recorder, `logr`, and the existing
controller-runtime Prometheus registry. It must not import Splunk CRD or
reconciliation packages.

The minimum constructor inputs are:

```text
leader election enabled
leader Lease namespace and name
authorization review client
Pod namespace and name for Events
refresh interval and per-review timeout
logger, Event recorder, and metrics recorder
```

The runnable must implement `NeedLeaderElection() bool` and return `false`.
The checker must satisfy `healthz.Checker`. No CRD field, Splunk Enterprise
REST endpoint, docker-splunk behavior, or Splunk Pod probe changes are part of
SHC-93.
