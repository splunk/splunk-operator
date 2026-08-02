# SHC-93 Operator Reconciliation Readiness Qualification

## Result

SHC-93 is source-qualified and EKS-qualified for the bounded Operator-manager
contract described here. Kubernetes process liveness is separate from the
ability to participate in reconciliation. A manager does not become Ready
until the complete enabled-controller informer set has completed its initial
synchronization and the service account is currently authorized for the
leader-election Lease operations. A synchronized and authorized HA contender
remains Ready even when it is not the current leader.

The final source is commit `90103bef5d87546cadc419738752a0d6b0cd813e`
on branch `codex/shc-93-operator-readiness`. The final EKS artifacts are:

```text
Operator OCI index:
sha256:b5a022a788c7cacf8b7ee33e7132eae56d82b14eb631809ddd116c8b816e9d63

linux/amd64 manifest:
sha256:2302269199434b738979a199e56bd7fcb2d9539b4c5f523b6233c3f41db01afc

linux/amd64 /manager SHA-256:
55914940988b05b4ba00c2d74dbabdd03f4cce4f9b30a2b04aeec894f7e72d74

Chart: splunk-operator-3.1.0.tgz
Size: 11266 bytes
SHA-256: 008abda67d13775ce6cd7e0f8e77365edce01af82f6ad9c12ecf34911a2f6925
```

The work does not change a Splunk Enterprise Pod probe, Splunk custom-resource
schema, StatefulSet, Docker-Splunk behavior, Ansible behavior, or splunkd.

## Readiness contract

The manager signals have intentionally different meanings:

| Signal | Meaning |
|---|---|
| `GET /healthz` | The manager process and local health server are alive. API access, leadership, and Splunk health are not implied. |
| `GET /readyz` | The complete initial controller informer barrier has passed and the latest bounded Lease authorization review permits `get`, `create`, and `update`. |
| `leader_election_master_status` | `1` identifies the current leader; `0` is expected on a healthy non-leading contender. |

The manager health server starts before the informer barrier. Informers are
registered in controller-runtime's cache runnable group, API discovery is
retried while unavailable, and the manager cannot enter leader election until
the complete set has synchronized. The readiness monitor then performs three
exact `SelfSubjectAccessReview` requests every 10 seconds under one 3-second
deadline. Kubelet probes read only the last in-memory result.

The complete default informer set covers the active v4 and legacy v3 Splunk
custom resources used by enabled controllers plus StatefulSet, Secret,
ConfigMap, and Pod. Postgres informer types are added when the Postgres feature
is enabled. The optional unstructured Barman ObjectStore informer is added
only when the feature is enabled and that CRD is installed.

Readiness failure does not restart the process. An active controller-runtime
leader that can no longer renew its Lease still exits after the leader-election
renew deadline; that is leader-election safety behavior, not a kubelet
liveness restart. A healthy standby remains eligible to take over.

## Source qualification

The final source passed the following gates on both macOS and Linux:

- `make build`, including generation, formatting, vet, and manager build;
- `make test` with 43 Ginkgo suites, 187 JUnit nodes, and all 185 enterprise
  controller specs passing;
- zero test failures;
- 78.3 percent composite statement coverage on macOS and 78.4 percent on
  Linux;
- focused `go test -race ./pkg/operatorreadiness ./cmd` on the manager
  readiness source;
- all default, default-with-webhook, and debug Kustomize overlays rendered;
- `make helm-check` with 60 Operator tests and 85 Universal Forwarder tests;
  and
- `git diff --check`.

The secure metrics listener and NotReady-address publication were introduced
through test-first chart assertions. The unchanged chart first failed exactly
the missing listener/port assertions and later failed exactly the missing
`publishNotReadyAddresses` assertion. The final chart passed both sets.

## Environment and immutable build

Live qualification used:

```text
EKS context:
arn:aws:eks:us-west-2:667741767953:cluster/vivek-spl-301372

Kubernetes server:
v1.31.14-eks-8f14419

Helm:
v3.18.4

Disposable namespace:
shc93-operator-readiness

Disposable Helm release:
shc93-operator
```

The final image was built and pushed from the clean Linux checkout using the
repository Make target:

```text
make docker-buildx \
  IMG=667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc93-90103bef5 \
  PLATFORMS=linux/amd64
```

ECR recorded the final index at 2026-08-02 03:42:34 UTC with size 99,829,878
bytes. Its provenance attestation manifest is
`sha256:e162ea951559af470db5296a9774a65d917fe3002b76d87da0c3204b7732f51d`.

## Starting failure evidence

SHC-92 had already established the user-visible problem: the old manager Pod
and Deployment reported Ready/Available while the service account repeatedly
received Forbidden responses for its leader Lease. No manager became leader
and no controller workers started. The HTTP server alone was therefore not a
valid control-plane readiness signal.

## Candidate corrections rejected during qualification

Qualification rejected four incomplete candidates before accepting the final
contract:

1. Image index
   `sha256:b07f7b0a6406123bdb6acc1009d0f45f683fe104873652c7550d808b91663254`
   treated an empty controller-runtime informer set as synchronized. A cold
   list/watch denial showed `cache_synchronized=1` before any controller watch
   had completed.
2. Image index
   `sha256:c3e82b8e761d87caac59e961051b81ad6da225359dd3c50550bad31cd0cc4a83`
   used a `WarmupRunnable`. Controller-runtime does not wait for its Warmup
   function before leader election, so the manager acquired the Lease and
   later exited when controller sources timed out.
3. Image index
   `sha256:e85e4e4c5be3ea777def3d870791b9c2435b7e7b309dddf957f87b3087d34a7d`
   registered informers before `Manager.Start`. During an API-isolated leader
   restart, REST discovery failed before the health server could start and the
   container entered CrashLoopBackOff.
4. Source `262e37265` and image index
   `sha256:d9ffc71392250a7b306092078ea22009b827bbb3bb0d3a14d2451c6b2bf5d798`
   completed the manager barrier and runtime fault contract, but its packaged
   chart did not provide a usable secure metrics Service. Source `3f7b3ee34`
   fixed the listener and delegated-authentication path. Its chart still
   omitted NotReady Pod addresses, which hid failure metrics until final
   source `90103bef5` corrected that Kubernetes Service behavior.

The SHC-93 manager executable in the accepted final image and in the image
used for the detailed cache/API/HA campaign is byte-identical: SHA-256
`55914940988b05b4ba00c2d74dbabdd03f4cce4f9b30a2b04aeec894f7e72d74`,
size 133,919,911 bytes. This ties the earlier fault observations to the final
manager binary while retaining the final image and chart as the accepted
deployment artifacts.

## EKS scenarios

### Normal single-replica startup

The manager started with `/healthz` available while readiness was initially
false. After informer synchronization and successful authorization review it
emitted `OperatorReconciliationReady`, became Ready with zero restarts, and
entered leader election. The healthy authenticated metrics snapshot was:

```text
cache_synchronized             1
leader_election_access         1
reconciliation_participation  1
```

The current-leader metric can remain zero for a Ready contender during normal
Lease acquisition. Readiness therefore does not mistake role ownership for
Pod health.

### Controller list/watch denied at cold start

The disposable Deployment was scaled to zero, the manager RoleBinding was
removed, and `kubectl auth can-i list pods` returned `no` before the new Pod
was created. For more than two minutes:

- `/healthz` returned HTTP 200 and `/readyz` returned HTTP 500;
- cache, Lease, and aggregate readiness metrics were `0/0/0`;
- leader status remained zero;
- no leader-election attempt or controller worker started;
- the process did not exit and the container restart count remained zero; and
- the Deployment was not Available.

Restoring the RoleBinding recovered the same Pod UID without a restart. Only
after the complete informer barrier succeeded did the manager emit the Ready
Event and attempt leader election.

### Leader Lease access denied at cold start

The final chart and desired image index were used for this accepted fixture.
Before Pod creation, Lease authorization returned `no`. Pod
`splunk-operator-controller-manager-5b6cd8dc74-xr6qp`, UID
`2e4bc4df-1f13-4fb8-8839-7d5054f45c78`, remained Running, NotReady, and at
zero restarts. It emitted one `OperatorReconciliationNotReady` Event with
reason `lease_access_denied`.

The final metrics Service retained `10.0.62.123:8443` while the Pod was
NotReady. Authenticated scraping through that Service returned:

```text
leader_election_master_status  0
cache_synchronized             1
leader_election_access         0
reconciliation_participation  0
not_ready transition count     1
```

Restoring the binding recovered the same UID with zero restarts. The next
scrape returned `1/1/1` and retained one denied plus one allowed transition.
Repeated identical monitor results did not create a polling-driven Event or
log storm.

### Healthy leader and standby

The final revision-8 Deployment was scaled to two. Both manager Pods were
Ready with zero restarts while exactly one Lease holder existed. Deleting the
leader left the existing standby
`splunk-operator-controller-manager-5b6cd8dc74-6qbwg`, UID
`5ba22526-bd2b-4be2-919d-bae5403617e7`, Ready with zero restarts. That same
standby acquired the Lease in 35 seconds. Its UID did not change, a replacement
contender also became Ready with zero restarts, and the disposable Deployment
was returned to one replica.

This proves that a non-leading Ready Pod is a capable contender rather than a
failed replica.

### Active-leader API interruption

A Pod-local rule rejected only the Kubernetes API Service address from the
active leader. The leader could no longer renew the Lease and
controller-runtime exited that manager process with code 1 after its renew
deadline. The Ready standby took over. The original Pod network namespace
retained the fault while the manager container restarted.

With the accepted cache-group barrier, the restarted manager did not enter a
CrashLoop. It served `/healthz` as HTTP 200, returned HTTP 500 from `/readyz`,
remained at one expected restart, did not enter leader election, and logged a
bounded informer-registration discovery failure every 10 seconds. Removing
the exact network rule let the same Pod recover in place; the standby remained
leader and Ready.

This differs from the rejected pre-start registration candidate, which exited
before the health server and accumulated six restart attempts under the same
fault.

## Metrics and diagnostics

The accepted manager publishes:

```text
splunk_operator_manager_readiness_status{check="cache_synchronized"}
splunk_operator_manager_readiness_status{check="leader_election_access"}
splunk_operator_manager_readiness_status{check="reconciliation_participation"}
splunk_operator_manager_readiness_transitions_total{state,reason}
splunk_operator_manager_readiness_last_transition_timestamp_seconds
leader_election_master_status{name="270bec8c.splunk.com"}
```

Labels contain only bounded check, state, and reason values. The chart serves
HTTPS metrics on port 8443, grants delegated TokenReview/SubjectAccessReview
permission through cluster-scoped RBAC, publishes a least-privilege metrics
reader ClusterRole, and leaves binding that reader to the monitoring identity
as an installation decision. `publishNotReadyAddresses: true` applies only to
the manager metrics Service and does not route Splunk client traffic.

Recommended alert boundaries are:

- no reconciliation-capable manager for two minutes: critical;
- a capable manager exists but no leader is reported for one minute: leader
  election incident; and
- at least one capable manager and one degraded contender for five minutes:
  HA degradation warning.

Diagnosis should correlate Deployment/Pod conditions, Lease holder, Events,
manager logs, the three authorization decisions, readiness metrics, and the
leader metric in the same time window.

## Known boundaries

- Live qualification covers one EKS 1.31.14 cluster. Other providers and live
  Kubernetes versions remain open.
- The Helm chart still installs one manager replica with `Recreate`; the HA
  scenarios deliberately scaled the disposable Deployment to two. A product
  decision to expose HA replica count and rollout policy is separate work.
- `cache_synchronized` is an initial synchronization latch. A selective
  post-start removal of non-Lease list/watch permission is visible in
  controller-runtime watch errors, but controller-runtime exposes no supported
  ongoing per-informer health signal for this probe.
- During complete API isolation, the protected metrics handler cannot complete
  delegated TokenReview/SubjectAccessReview. Health and readiness endpoints
  and local logs remain available; Event publication is also best effort and
  can fail during the outage. Monitoring must treat scrape absence together
  with Pod conditions and healthy-replica telemetry as a signal.
- Leader-election process exit after loss of Lease renewal is intentional.
  Readiness failure itself never asks kubelet to restart the container.
- Alert rules are documented and their underlying signals were exercised;
  no production Prometheus/Alertmanager installation or notification delivery
  was changed by this work item.
- Managed Splunk resource health and Splunk Pod probes remain separate from
  manager readiness.

## Cleanup and retained-workload invariant

Normal Helm uninstall removed release `shc93-operator`. Namespace
`shc93-operator-readiness`, the temporary metrics-reader binding, and all
namespace-hashed SHC-93 ClusterRoles and ClusterRoleBindings were absent after
cleanup. No finalizer or force-deletion patch was used.

The retained `shc85-lifecycle-hold` SearchHeadCluster remained
Ready/Ready at 3/3. Its LicenseManager, ClusterManager, four indexers, deployer,
and three Search Heads all remained Running and Ready with zero container
restarts; the retained workload Job remained successfully Completed.
