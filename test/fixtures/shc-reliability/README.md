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

The evidence log is written below `build/_test/shc82` unless
`SHC82_EVIDENCE_FILE` specifies another location.

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
target has durably reached `ReadyForReplacement`. The fixture also includes a
three-member Search Head Cluster so the existing numbered HEC/search monitor
can run throughout the lifecycle test. It pins the accepted Splunk Cloud
`10.5.2605.0/844c593e9c1d` runtime digest.

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
numbered workload monitor first, then run the lifecycle monitor. The latter
owns the harmless `spec.podAnnotations` revision trigger, waits for ordinal
three to reach `ReadyForReplacement`, scales the Operator Deployment to zero,
and observes an uninterrupted five-minute controller absence. Its exit trap
restores the original controller replica count on success or failure.

```bash
SHC82_NAMESPACE=shc85-lifecycle-hold \
SHC82_STACK_NAME=shc85 \
SHC82_SAMPLES=180 \
SHC82_RUN_ID=shc85-ready-replacement-absence \
SHC82_EVIDENCE_FILE=build/_test/shc85/workload.log \
test/fixtures/shc-reliability/shc82_appframework_monitor.sh &
workload_pid=$!

SHC85_NAMESPACE=shc85-lifecycle-hold \
SHC85_HOLD_SECONDS=300 \
SHC85_EVIDENCE_FILE=build/_test/shc85/lifecycle-hold.tsv \
test/fixtures/shc-reliability/shc85_lifecycle_hold_monitor.sh

wait "${workload_pid}"
```

During controller absence the lifecycle monitor requires the exact persisted
operation, target UID and revisions to remain fixed; the target container to
remain running with zero restarts while staying unready and outside the
EndpointSlice; and all three non-target peers to retain their UIDs, restart
counts, readiness, and endpoints. After controller restoration it requires a
complete `3 -> 2 -> 1 -> 0` roll, at most one unavailable indexer, zero
container restarts, remote serving recovery on the final replacement, the
desired revision on all four Pods, and removal of the temporary lifecycle
marker with Pod replacement. The workload monitor independently requires
zero HEC and search request failures plus exact eventual sequence recovery.
