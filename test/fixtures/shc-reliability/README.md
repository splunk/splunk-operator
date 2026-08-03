# SHC reliability qualification fixtures

The baseline topology uses a `LicenseManager` named `shc82`. The
`ClusterManager`, `IndexerCluster`, and `SearchHeadCluster` each set
`spec.licenseManagerRef.name: shc82`. For the `SearchHeadCluster`, that single
reference configures both the deployer and every Search Head member.

The LicenseManager mounts the `shc82-license` Secret at `/mnt/licenses` and
loads `/mnt/licenses/enterprise.lic`. The license file is deliberately not
stored in Git.

Create or update the Secret before applying the baseline manifest:

```bash
make shc82-license-secret \
  SHC82_NAMESPACE=<qualification-namespace> \
  SHC82_LICENSE_FILE=/absolute/path/to/enterprise.lic
```

The supplied license must support operation as a remote license manager. The
built-in Enterprise trial license does not satisfy this qualification
requirement.

`shc82_appframework_monitor.sh` sends numbered events through the Indexer
Cluster HTTP Event Collector and searches for those events through the Search
Head Cluster Service while collecting Kubernetes, SHC, IDXC, captain, and App
Framework state. It does not print the HEC token or administrator password.
Search Head observations report container readiness, Kubernetes Pod readiness,
the Operator-owned SHC serving gate, and Service EndpointSlice readiness as
separate signals so an internal Splunk restart cannot be mistaken for a
healthy Kubernetes traffic endpoint. Each sample also records the exact
EndpointSlice target Pods, each serving-gate reason, and the local and
captain-reported member/restart states exposed by the SearchHeadCluster.
Failed searches retain a bounded, whitespace-normalized response detail
without logging credentials.
The monitor exits successfully only when every submitted sequence is present
exactly once at the final completeness gate.

Run it from the repository root:

```bash
SHC82_NAMESPACE=<qualification-namespace> \
SHC82_SAMPLES=180 \
test/fixtures/shc-reliability/shc82_appframework_monitor.sh
```

The default request source is the stack's LicenseManager Pod. When the
qualification itself replaces that Pod, use a separate, stable in-cluster
client and set both `SHC82_PROBE_POD` and `SHC82_PROBE_CONTAINER`. This keeps
loss of the test client distinct from loss of the Splunk Services being
measured. The client image should be pinned by digest and must provide `sh`
and `curl`.

The evidence log is written below `build/_test/shc82` unless
`SHC82_EVIDENCE_FILE` specifies another location.

`shc82_indexer_restart_required` is the separate indexer-cluster fixture. Its
packaging target deterministically derives `health_report_period` from the
requested patch version, so overwriting one stable remote object key with a
new fixture version changes `health.conf`. That change was observed by the live
10.5 qualification to set `restart_required_for_apply_bundle=true`; the
fixture must not be substituted with the Search Head fixture because that app
reloaded on the tested indexers without a restart. Build a versioned indexer
archive with:

```text
make shc82-indexer-app-package SHC82_INDEXER_APP_VERSION=1.0.0
```

The final live gate still requires the Cluster Manager bundle status to prove
that the selected Splunk build classified the app as restart-required before
availability conclusions are accepted.

The final IndexerCluster manifest intentionally does not override
`readinessProbe`. With the indexer lifecycle gate enabled, the Operator uses
the serving-readiness profile (`timeoutSeconds: 2`, `periodSeconds: 2`, and
`failureThreshold: 1`). Startup and liveness retain their separate extended
budgets. Increasing readiness failure tolerance does not protect a slow
startup; it only leaves an indexer that has stopped serving HEC in Service
endpoints longer.

Both SHC-82 app targets use the same standard-library packager. It replaces
only the archived `default/app.conf` version, leaves the checked-in source
unchanged, and normalizes archive order, timestamps, ownership, and modes.
Run `make shc82-app-package-test` to validate determinism on the current host.

## Final integrated qualification manifest

`shc-final-qualification-cluster.yaml.in` is the clean-campaign topology: one
License Manager, one Cluster Manager, four RF3/SF2 indexers, and three Search
Heads using opt-in partition-gated `RollingUpdate`. Both App Framework paths
are configured but must be empty during cluster formation. The manifest uses
extended startup, liveness-termination, Pod-termination, and lifecycle
budgets so a slow persistent-volume restart or KV Store check is not restarted
from the beginning by an undersized kubelet clock.

Render it only with the final runtime image resolved by digest:

```text
make shc-final-manifest \
  SHC_FINAL_RUNTIME_IMAGE=<repository>:<tag>@sha256:<digest> \
  SHC_FINAL_NAMESPACE=shc-final-qualification \
  SHC_FINAL_S3_BUCKET=vivekr-shc82-afw-667741767953-us-west-2 \
  SHC_FINAL_S3_PREFIX=shc-final
```

The renderer rejects mutable-only image references and unresolved template
tokens. `make shc-final-manifest-test` validates reproducible rendering. Before
applying the output, create `shcfinal-license` with `enterprise.lic` and
`s3-secret` with `s3_access_key` and `s3_secret_key` in the rendered namespace.
The credentials and license are never written to the manifest or evidence.

## SHC-83 initial-formation readiness

`shc83-startup-readiness-cluster.yaml` creates an isolated LicenseManager and
three-member Search Head Cluster using the current qualification runtime. The
license file is deliberately not stored in Git.

Create the namespace and license Secret before applying the fixture:

```bash
kubectl create namespace shc83-startup-readiness
make shc83-license-secret \
  SHC83_LICENSE_FILE=/absolute/path/to/enterprise.lic
```

Start `shc83_startup_readiness_monitor.sh` before applying the fixture. The
monitor fails immediately if any Search Head becomes a ready Service endpoint,
or its Operator-owned serving gate becomes true, before the durable v4 initial
formation stage is complete. It records the formation stage, restart decision,
stabilization timestamp, Pod UIDs, container and Pod readiness, serving-gate
reasons, EndpointSlice targets, container restart counts, local container-state
files, and aggregate SHC formation status.

```bash
SHC83_NAMESPACE=shc83-startup-readiness \
test/fixtures/shc-reliability/shc83_startup_readiness_monitor.sh &
kubectl apply -f \
  test/fixtures/shc-reliability/shc83-startup-readiness-cluster.yaml
wait
```

The default success gate requires twelve consecutive five-second samples with
all desired members ready, serving, present in the client Service
EndpointSlice, reflected by `status.lastStableReplicas`, and reported in the
`Complete` initial-formation stage after the required restart was initiated.

After startup qualifies, use `shc83_established_recovery_monitor.sh` to verify
deletion of an established member. The target may be a non-captain or the
active captain. The monitor fails if either unaffected peer leaves the
EndpointSlice or fewer than two established endpoints remain. It succeeds only
after a replacement Pod with a new UID rejoins the captain view, becomes
serving and ready, returns to the EndpointSlice, and remains stable. For a
captain target, also verify that Splunk elects a live dynamic captain rather
than assuming ordinal zero retains that role.

```bash
SHC83_TARGET_POD=splunk-shc83-shc-search-head-1 \
SHC83_EVIDENCE_FILE=build/_test/shc83/established-recovery.tsv \
test/fixtures/shc-reliability/shc83_established_recovery_monitor.sh &
kubectl -n shc83-startup-readiness delete pod \
  splunk-shc83-shc-search-head-1 --wait=false
wait
```

Repeat the monitor with the currently observed captain as
`SHC83_TARGET_POD`. Finally, delete the Operator Pod while recording the SHC
phase, initial-formation stage, `lastStableReplicas`, Search Head UIDs, restart
counts, and EndpointSlice targets. All three established endpoints and Search
Head UIDs must remain unchanged while the replacement controller resumes from
durable `Complete` state.

## SHC-84 startup and termination budgets

`shc84-startup-term-baseline.yaml` keeps the Search Head probe and Pod
termination fields omitted so the campaign measures the rendered v4 defaults.
The LicenseManager uses an explicit extended startup budget so a dependency
does not obscure the Search Head result.

```bash
kubectl create namespace shc84-startup-term
make shc84-license-secret \
  SHC84_LICENSE_FILE=/absolute/path/to/enterprise.lic
SHC84_EVIDENCE_FILE=build/_test/shc84/default-startup.tsv \
test/fixtures/shc-reliability/shc84_probe_budget_monitor.sh &
kubectl apply -f \
  test/fixtures/shc-reliability/shc84-startup-term-baseline.yaml
wait
```

The monitor records the rendered startup and liveness probes, Pod-level grace,
container start and termination states, restart counts, kubelet `Unhealthy`
and `Killing` Events, client endpoints, and the runtime shutdown owner/result
artifacts. A stable result does not by itself accept the defaults: the evidence
must be compared with first-start and supported-upgrade durations, and any
probe-triggered restart must prove bounded, exact-once shutdown.

After the cluster is stable, qualify the image's direct TERM path on a current
non-captain. The command intentionally bypasses `preStop`; Kubernetes restarts
the container in the same Pod. The monitor requires exactly one restart, the
same Pod UID, complete SHC recovery, and all three client endpoints.

```bash
SHC84_TARGET_POD=splunk-shc84-shc-search-head-1 \
SHC84_EVIDENCE_FILE=build/_test/shc84/direct-term.tsv \
test/fixtures/shc-reliability/shc84_term_exit_monitor.sh &
kubectl -n shc84-startup-term exec \
  splunk-shc84-shc-search-head-1 -c splunk -- /bin/kill -TERM 1
wait
```

To qualify the kubelet restart path, run the same recovery monitor against a
current non-captain and make the image-owned state marker unhealthy. Readiness
must first remove the member from the client Service. After three consecutive
liveness failures, Kubernetes must restart only the container, `preStop` and
TERM must converge on one runtime stop, and the persistent member must rejoin.

```bash
SHC84_TARGET_POD=splunk-shc84-shc-search-head-2 \
SHC84_SCENARIO="forced liveness failure" \
SHC84_EVIDENCE_FILE=build/_test/shc84/forced-liveness.tsv \
test/fixtures/shc-reliability/shc84_term_exit_monitor.sh &
monitor_pid=$!
kubectl -n shc84-startup-term exec \
  splunk-shc84-shc-search-head-2 -c splunk -- /bin/sh -ec \
  'printf "%s\n" forced-liveness-failure > \
    "${CONTAINER_ARTIFACT_DIR:-/opt/container_artifact}/splunk-container.state"'
wait "${monitor_pid}"
```

The target must be resolved from the live
`SearchHeadCluster.status.captain`; ordinal zero is not assumed to be captain.
The candidate run must additionally show
`livenessProbe.terminationGracePeriodSeconds: 660`.

To qualify a supported-version upgrade, first form the fully serving v4 source
fixture pinned to the qualified 10.4 runtime digest:

```bash
kubectl create namespace shc84-upgrade-candidate
make shc84-license-secret \
  SHC84_NAMESPACE=shc84-upgrade-candidate \
  SHC84_LICENSE_FILE=/absolute/path/to/enterprise.lic
kubectl apply -f \
  test/fixtures/shc-reliability/shc84-supported-upgrade-source.yaml
```

Upgrade its referenced LicenseManager to the target image and wait for the
LicenseManager to become Ready on that image. Then start the upgrade monitor
before changing the SearchHeadCluster image:

```bash
SHC84_NAMESPACE=shc84-upgrade-candidate \
SHC84_TARGET_IMAGE_DIGEST=sha256:<target-runtime-digest> \
SHC84_EVIDENCE_FILE=build/_test/shc84/supported-upgrade.tsv \
test/fixtures/shc-reliability/shc84_upgrade_monitor.sh &
monitor_pid=$!
kubectl -n shc84-upgrade-candidate patch searchheadcluster \
  shc84-shc --type merge \
  -p '{"spec":{"image":"<target-image-by-digest>"}}'
wait "${monitor_pid}"
```

The monitor captures the StatefulSet strategy, partition and revisions; CR
generation, conditions, upgrade phase, captain and members; Pod UIDs, images,
image IDs, restart and termination states; EndpointSlice targets; rendered
probe/grace values; and relevant Pod Events. It fails if fewer than two
Search Heads remain endpoints, a retained container or replacement Pod
restarts, the four-clock probe contract changes, or final Pods do not all use
the target digest and rejoin registered and `Up`.

## SHC-85 planned indexer lifecycle hold

`shc85-lifecycle-hold-cluster.yaml` creates the isolated four-indexer RF3/SF2
topology used to qualify a long controller absence after an Operator-owned
target has durably reached `TargetSelected`, `WithdrawingReadiness`,
`Decommissioning`, or `ReadyForReplacement`. At `TargetSelected`, the monitor
requires the selected target and all other indexers to remain ready and in the
Service because traffic withdrawal has not been requested. For
`WithdrawingReadiness`, the monitor additionally waits until the selected Pod
contains the explicit lifecycle marker, removes the controller, and then
requires the Pod to become unready and absent from the indexer Service before
starting the five-minute clock. For
`Decommissioning`, the monitor waits until the accepted operation records that
Splunk has actually been observed in a decommission state; issuing the command
alone is not sufficient. The fixture also includes a three-member Search Head
Cluster so the numbered HEC/search monitor can run throughout the lifecycle
test. It pins the accepted Splunk Cloud `10.5.2605.0/844c593e9c1d` runtime
digest.

Create the namespace and license Secret, apply the fixture, and wait for all
four indexers and three Search Heads to become Ready:

```bash
kubectl create namespace shc85-lifecycle-hold
make shc85-license-secret \
  SHC85_LICENSE_FILE=/absolute/path/to/enterprise.lic
kubectl apply -f \
  test/fixtures/shc-reliability/shc85-lifecycle-hold-cluster.yaml
```

The Operator must be running with `SplunkPodLifecycle`,
`SearchHeadClusterLifecycle`, and `IndexerClusterLifecycle` enabled. Start the
in-cluster workload Job first, then run the lifecycle monitor. The Job sends
numbered HEC events and searches through the Services without depending on a
workstation `kubectl exec` connection. The lifecycle monitor owns the harmless
`spec.podAnnotations` revision trigger, waits for ordinal three to reach
the stage selected by `SHC85_HOLD_STAGE`, and applies the fault selected by
`SHC85_CONTROLLER_FAULT`. The default `ControllerAbsent` mode scales the
Operator Deployment to zero and observes an uninterrupted five-minute
controller absence. The default stage is `ReadyForReplacement`; the other
supported values are `TargetSelected`, `WithdrawingReadiness`, or
`Decommissioning`. Its exit trap restores the original controller replica
count on success or failure. In `ControllerAbsent` mode, after setting the
Deployment replica count to zero, the harness immediately removes any
remaining controller Pod so it cannot cross the selected stage while
terminating.

The monitor rejects a contaminated baseline before changing the CR. The
IndexerCluster must be Ready with every desired Pod Ready, serving, at zero
restarts, and labeled with the StatefulSet `updateRevision`; any durable Pod
update must already be terminal. This is deliberately stronger than checking
the CR phase alone, especially for an `OnDelete` StatefulSet whose
`currentRevision` is not a valid completion oracle.

```bash
make shc85-incluster-workload
kubectl -n shc85-lifecycle-hold logs -f job/shc85-incluster-workload &
workload_log_pid=$!

SHC85_NAMESPACE=shc85-lifecycle-hold \
SHC85_HOLD_STAGE=ReadyForReplacement \
SHC85_HOLD_SECONDS=300 \
SHC85_EVIDENCE_FILE=build/_test/shc85/lifecycle-hold.tsv \
test/fixtures/shc-reliability/shc85_lifecycle_hold_monitor.sh

kubectl -n shc85-lifecycle-hold wait \
  --for=condition=complete job/shc85-incluster-workload --timeout=2h
wait "${workload_log_pid}"
```

To qualify a long absence after Splunk decommission has been observed but
before replacement authorization, set
`SHC85_HOLD_STAGE=Decommissioning` and use a new evidence-file name.
To remove the controller as soon as the target contains the persisted
readiness-withdrawal marker, and then prove that kubelet-driven readiness and
Service withdrawal complete without the controller, use
`SHC85_HOLD_STAGE=WithdrawingReadiness`.
To remove the controller immediately after durable target selection, before
readiness withdrawal or decommission is requested, use
`SHC85_HOLD_STAGE=TargetSelected`.

`TargetSelected` is intentionally a short reconciliation boundary. For this
stage the harness starts a Kubernetes watch before triggering the revision,
captures the exact persisted operation, and immediately applies the supported
IndexerCluster pause annotation, scales the controller Deployment to zero, and
force-deletes the controller Pod concurrently. The run is accepted only if the
post-stop state still has the exact `TargetSelected` identity, all four Pods
remain Ready and serving, and no readiness-withdrawal marker exists. The pause
annotation must remain present throughout the absence and is removed only
after the controller Deployment is restored. The exit trap restores the
controller and removes the annotation on every failure path. This prevents
observer polling latency from silently turning the scenario into a later-stage
test; it does not qualify concurrent desired-state conflict behavior.

During controller absence the lifecycle monitor requires the exact persisted
operation, target UID and revisions to remain fixed. At `TargetSelected`, all
four peers must remain running, ready, and present in the EndpointSlice, with
no readiness-withdrawal marker. At every later supported stage, the target
must remain running with zero restarts while staying unready and outside the
EndpointSlice. All three non-target peers must retain their UIDs, restart
counts, readiness, and endpoints. After controller restoration the monitor
requires a complete `3 -> 2 -> 1 -> 0` roll, at most one unavailable indexer,
zero container restarts, remote serving recovery on the final replacement,
the desired revision on all four Pods, and removal of the temporary lifecycle
marker with Pod replacement. The workload monitor independently requires zero
HEC and search request failures plus exact eventual sequence recovery. Its Pod
name supplies a unique run ID, so repeated Jobs do not reuse prior
events. Workstation-side Kubernetes and Splunk telemetry should run as a
separate observer; an observer/API stall must be recorded as a telemetry gap
and must not silently pause the workload used for the availability verdict.

The same monitor can qualify a running controller that loses only its path to
the Kubernetes API. This is distinct from scaling the Deployment to zero: the
original Operator Pod remains scheduled while a test-only privileged
ephemeral container installs one OUTPUT rule in that Pod's network namespace.
The rule rejects TCP port 443 only for the current in-cluster Kubernetes
Service IP. It does not change node, cluster, or workload Pod networking. The
utility image is pinned by digest, the monitor requires the original Operator
Pod UID and the rule to remain present throughout the requested interval, and
the evidence records its container identity, restart count, and runtime state.
The checked-in custom debug profile explicitly overrides the Operator Pod's
non-root runtime identity for this test container and grants `NET_ADMIN`;
without that override the kernel rejects the Pod-local iptables operation.
The ephemeral container deliberately uses its own process namespace rather
than targeting the manager container. Both containers already share the Pod
network namespace, while an ephemeral container joined to the manager's
process namespace can be terminated when leader-election loss restarts the
manager and would remove the fault prematurely.
The ephemeral process has an independent timeout and EXIT trap that remove the
rule even if the workstation monitor is interrupted. If that process is
force-killed before its trap runs, the monitor starts a separate root cleanup
container that removes only the exact tagged rule and verifies API recovery;
that fallback is cleanup evidence, not a passing qualification result. The run
is accepted only after the primary fault log proves API access returned with
HTTP 200.
The monitor creates the ephemeral container without attaching its stdout
stream and reads its Kubernetes container log separately. This keeps the
durable fault markers observable when leader-election loss restarts the
manager container and interrupts an attached debug stream.

This mode currently starts at observed `Decommissioning`, where the durable
operation proves the Splunk command has taken effect before the API path is
removed. It requires authorization to update `pods/ephemeralcontainers` and a
cluster policy that permits the privileged diagnostic container. Those are
qualification-lab permissions, not product runtime requirements. Run it with:

```bash
SHC85_NAMESPACE=shc85-lifecycle-hold \
SHC85_HOLD_STAGE=Decommissioning \
SHC85_CONTROLLER_FAULT=APIDisconnected \
SHC85_HOLD_SECONDS=300 \
SHC85_EVIDENCE_FILE=build/_test/shc85/api-disconnected.tsv \
test/fixtures/shc-reliability/shc85_lifecycle_hold_monitor.sh
```

The default `SHC85_CONTROLLER_FAULT=ControllerAbsent` retains the controller
scale-to-zero campaign described above. API-disconnection qualification must
not be inferred from NetworkPolicy unless the target CNI is independently
proved to enforce the selected policy.

### Active controller leader failover

`SHC85_CONTROLLER_FAULT=LeaderFailover` qualifies loss of the active Operator
controller while a second controller is contending for the Kubernetes
leader-election Lease. This mode requires an uncontaminated one-replica
Operator baseline and `SHC85_HOLD_STAGE=Decommissioning`. Before changing the
IndexerCluster, the harness scales the Operator Deployment to two, requires
both Pods to be Ready with zero restarts, and proves that one stable Lease
holder continues to renew while the other Pod remains a contender.

When ordinal three has durably recorded both the decommission request and an
observed Splunk decommission state, the harness resolves the active Lease
holder and force-deletes that exact Pod. It accepts any different live
controller contender as the successor; Kubernetes leader election does not
promise that the Pod which was already waiting will win instead of a newly
created replacement. A passing run requires the Lease transition count to
increase, the successor to log successful acquisition, its Lease renewal to
remain stable, the Deployment to return to two Ready zero-restart Pods, and
the durable indexer operation to resume without cancellation. The two
controller Pods remain running for the complete `3 -> 2 -> 1 -> 0` roll. Every
sample requires the same sole Lease holder, two healthy contenders, at most
one unavailable indexer, and zero indexer container restarts.

The monitor also records the target-specific
`IndexerDecommissionRequested` Event count immediately before leader deletion
and after convergence. The count must not increase, proving that takeover did
not issue a duplicate decommission request for the interrupted target. At the
end, the harness restores the original one-replica Operator configuration and
requires the remaining Pod to hold and renew the Lease before reporting
success. The main TSV adds a `leader_election` JSON column; a bounded
`*.leader-failover.log` records the before/after holder UIDs, transition
counts, durable operation, Event counts, failover duration, and contender
runtime.

Run the campaign with:

```bash
SHC85_NAMESPACE=shc85-lifecycle-hold \
SHC85_HOLD_STAGE=Decommissioning \
SHC85_CONTROLLER_FAULT=LeaderFailover \
SHC85_LEADER_ELECTION_LEASE=270bec8c.splunk.com \
SHC85_EVIDENCE_FILE=build/_test/shc85/leader-failover.tsv \
test/fixtures/shc-reliability/shc85_lifecycle_hold_monitor.sh
```

This is a single-active-leader failover qualification using the Operator's
normal Lease protocol. It does not inject two simultaneous active leaders,
corrupt or delete the Lease, partition contenders from one another, or prove
behavior under an API quorum loss. Those conflict and split-brain scenarios
remain separate qualification work.

## SHC-98 stable indexer search-address evidence

`shc98_stable_address_monitor.sh` is a read-only monitor for the SHC-98
experiment. It does not patch a Custom Resource, change an image, delete a
Pod, or advance lifecycle status. Start it before the staged IndexerCluster is
unpaused. It records the StatefulSet revisions, durable lifecycle operation,
Pod UIDs/IPs/PVC claims, EndpointSlices, Cluster Manager health and registered
search addresses, and the independently observed distributed-peer inventory
from every Search Head. Snapshot and final configuration artifacts also record
each indexer's effective `register_search_address`, system FQDN, and resolved
Pod IP without recording a credential.

The normal mode waits for one complete Operator-owned reverse-ordinal roll and
then requires five minutes of stable samples. It fails if more than one indexer
is unavailable, a container restarts, the target order is not `3,2,1,0`, a Pod
or PVC identity transition is invalid, Cluster Manager or any Search Head does
not retain the expected per-ordinal FQDNs, or effective indexer configuration
and system DNS identity disagree. Run the API-independent SHC-85 workload Job
at the same time; the two evidence streams deliberately remain separate. The
monitor records a qualification violation as soon as a later ordinal appears
before the prior ordinal's GUID is represented by exactly one `Up` expected
peer on every Search Head. It preserves the rest of the rollout evidence and
ultimately fails the run even if the deployment later converges. This catches
stale aliases and divergent peer selection when the Cluster Manager already
reports the replacement searchable. The
SHC-98 Job is pinned to the previously accepted runtime OCI index, uses the
Pod hostname as a unique workload run ID, and does not mount a Kubernetes
service-account token.

The default expected-address mode is `fqdn`. For a controlled rollback that
uses `SPLUNK_IDXC_REGISTER_SEARCH_ADDRESS=absent`, set
`SHC98_EXPECTED_ADDRESS_MODE=pod-ip`. In that mode the monitor derives the
current expected `PodIP:8089` values from the replacement Pods, requires one
such identity per GUID on every Search Head, requires Cluster Manager to
publish those same search addresses, and verifies that the effective
`register_search_address` option is absent while the system FQDN remains the
stable StatefulSet identity.

Validate the script before use:

```sh
make shc98-monitor-check
make shc98-workload-check
```

Capture a single non-mutating baseline from the retained qualification
cluster:

```sh
SHC98_KUBE_CONTEXT=shc85-vivek-spl-301372 \
SHC98_SNAPSHOT_ONLY=true \
SHC98_EVIDENCE_FILE=build/_test/shc98/baseline.tsv \
test/fixtures/shc-reliability/shc98_stable_address_monitor.sh
```

Immediately before the IndexerCluster is unpaused, recreate the in-cluster
workload and follow its log. The Make target is intentionally scoped to the
retained `shc-final-qualification` fixture:

```sh
make shc98-incluster-workload \
  SHC98_KUBECTL='kubectl --context shc85-vivek-spl-301372'
kubectl --context shc85-vivek-spl-301372 \
  -n shc-final-qualification logs -f job/shc98-incluster-workload
```

For the rollout, omit `SHC98_SNAPSHOT_ONLY=true` and start the monitor before
unpausing `shcfinal-idxc`. The namespace credential is read from the existing
namespace Secret and passed to in-Pod REST calls over standard input. It is
never written to the evidence files or placed in a command argument.
