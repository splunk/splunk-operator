# SHC-82 App Framework Indexer Restart Availability Qualification

## Status and evidence boundary

This document records the indexer-side evidence gathered for SHC-82 on
2026-07-30 UTC. It is not a production-readiness claim. The campaign proves
that Splunk's searchable rolling restart and Kubernetes traffic readiness
address different parts of the availability problem. It also identifies a
controller-progress defect that must be corrected before an indexer serving
gate can be adopted.

The accepted final run used:

- EKS cluster `vivek-spl-301372` and namespace
  `shc82-afw-annotation-filter`;
- one Cluster Manager, four indexer peers, replication factor 3, search factor
  2, three Search Heads, one deployer, and one License Manager;
- Operator source `632d9155c` plus documentation source `266f9ff43`;
- Operator image digest
  `sha256:bbe264b9ea3b102ce57b31ba87ae353b9703314e9ae9c378944b3fa5a277ec28`;
- versioned App Framework archive
  `shc82_indexer_restart_required-1.0.4.tgz` with SHA-256
  `7df19aeb7517b0c6e5a721f91d48026a85dd80dc99016dfb0c67343a8d8c58b0`;
- resulting Splunk cluster-bundle ID
  `92FF60362F4D274B0912EF0E7ACBBB69`; and
- continuously numbered HEC submissions with exact-result searches through
  the Search Head Service.

No credential, license content, HEC token, or authorization header is part of
the recorded evidence.

## Official-build Operator lifecycle qualification

A later 2026-07-30 UTC campaign qualified the Operator-owned indexer Pod
replacement path that the earlier App Framework experiments had left blocked.
This is a separate result from a Splunk-managed internal App Framework rolling
restart; it must not be used to claim that the Operator controls Splunk's
internal next-peer selection.

The campaign used:

- Splunk Cloud build `10.5.2605.0`, build `844c593e9c1d`, from the official
  `splunkcloud-10.5.2605.0-844c593e9c1d-linux-amd64.tgz` artifact;
- Docker-Splunk source `f063cfd3936c42428c0775783b8415c2fcfbb3ef`
  with Splunk Ansible source
  `5e9e12fd46f2d24823b2b9a291cc5fa14abaf8f5`;
- runtime image digest
  `sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`;
- Operator source `7ff844f4a0ad3fdd33e34443e009d08aff087124`
  and Operator image digest
  `sha256:f7e2a4f8444ffa1b335486e266e4ed9e940180f78d460639de5703a8bdb2530b`;
  and
- the same EKS cluster, namespace, four-peer RF3/SF2 indexer topology, and
  three-member SHC used by the preceding qualification.

The first bounded test replaced indexer ordinal 3 without changing its image
or deleting its storage. Kubernetes changed the Pod UID from
`968a9337-58a9-49ab-9234-59b07c72892c` to
`2624d1ab-364a-43b7-a54f-2b4a4a4b5dd0`, while the `/opt/splunk/etc` and
`/opt/splunk/var` PVC UIDs remained
`816b3724-0297-48c1-a3ee-4dc255293b49` and
`5c36029f-c3e7-4844-940d-e728f20bf851`. The replacement mounted the populated
MongoDB/WiredTiger directory, completed the normal Ansible-internal Splunk
restart with `ok=111`, `failed=0`, reached Ready with zero container restarts,
and did not emit `Active KVStore version upgrade precheck FAILED`.

A harmless Pod annotation then created StatefulSet revision
`splunk-shc82-idxc-indexer-7d6f677979`. The Operator completed ordinals
`3 -> 2 -> 1 -> 0`. For each target it durably recorded the target and stage,
withdrew readiness, requested decommission, waited for Splunk to reassign
primaries, replaced only that Pod, required Kubernetes and remote serving
recovery, and only then selected the next ordinal. No manual Pod deletion was
used to advance the four-member roll.

All four final Pods:

- had new UIDs and the desired per-Pod controller revision;
- used the same immutable runtime digest;
- reached Ready with zero container restarts;
- completed Ansible with `failed=0`;
- produced zero matches for the prior KV Store failure signature; and
- retained their original Splunk peer GUIDs.

The final Cluster Manager observation reported replication factor met, search
factor met, all data searchable, all peers Up, no fixups, and readiness for a
searchable rolling restart. The IndexerCluster reported `Ready`, four of four
ready replicas, and durable lifecycle stage `Completed`.

Two numbered workload monitors covered the roll and recovery:

| Run | Coverage | Result | Evidence SHA-256 |
|---|---|---|---|
| `official-844c593-operator-roll` | Three complete replacements and the final ordinal's drain | 80 submissions, zero HEC failures, zero search-request failures, final `count=80`, `min=1`, `max=80`, `distinct=80` | `a6fcfad5c6eccbcaa1b887d0cff924fe8ee2c69dc04d75c1b28f7a837fe721e0` |
| `official-844c593-operator-roll-final` | Final replacement and stable post-roll service | 30 submissions, zero HEC failures, zero search-request failures, final `count=30`, `min=1`, `max=30`, `distinct=30` | `dd3b21d8caa07935c4a7f905a0009c08e77d5d5eb5f17121fb1216ada3db8a2b` |

Some intermediate aggregate searches temporarily lagged recently accepted
events and later converged. The result therefore proves eventual exact
completeness and uninterrupted request handling for this workload; it does not
claim that every immediate search saw every event accepted seconds earlier.

One Kubernetes status nuance remains relevant to supportability. The
StatefulSet retained its old `.status.currentRevision` even after all four
Pods carried the desired `controller-revision-hash` and the IndexerCluster
lifecycle reported `Completed`. Qualification therefore used the per-Pod
revision labels, Pod UIDs, durable CR stage, and serving/cluster observations
rather than treating StatefulSet `currentRevision` alone as completion proof.

## Operator restart recovery qualification

A separate 2026-07-30 UTC campaign tested the durable controller boundary on
branch `codex/shc-85-controller-restart-qualification`. It reused the same
pinned Operator source and image, official Splunk runtime, EKS cluster,
namespace, and four-peer RF3/SF2 topology described above. A harmless Pod
annotation created desired StatefulSet revision
`splunk-shc82-idxc-indexer-64d4d7b6d4`.

The Operator selected ordinal 3, persisted operation
`5ef37b13-b3a0-4507-9ee6-e24ab3ecd884:splunk-shc82-idxc-indexer-64d4d7b6d4:1785428315376697503`,
withdrew only that Pod from the indexer Service, and reached durable stage
`Decommissioning`. At `2026-07-30T16:18:50Z`, the Operator Pod
`splunk-operator-controller-manager-9f8dfdd4b-l77lf`, UID
`b66e3ae4-9f2f-4695-880c-5470985381a2`, was deleted. Its replacement,
`splunk-operator-controller-manager-9f8dfdd4b-6sj2n`, UID
`ed0cf975-b0be-494d-8f41-b089101867f7`, became Ready with zero container
restarts.

After controller startup and leader/cache recovery, the replacement controller
read the same operation ID, target Pod, target UID, source revision, desired
revision, and `decommissionRequestedAt` value from IndexerCluster status. Its
first relevant lifecycle logs at `16:19:48Z` continued the already-withdrawn
ordinal 3 and waited for that peer to become Down. It did not select another
ordinal or issue a second ordinal-3 decommission Event. The single
`IndexerDecommissionRequested` Event for ordinal 3 remained timestamped
`16:18:47Z`.

The recovered controller then completed the entire roll in order
`3 -> 2 -> 1 -> 0`. Before each next target it observed the previous
replacement as Kubernetes Ready, Cluster Manager Up/searchable, published in
the EndpointSlice, and remotely serving. At most one indexer was withdrawn
from the client Service at a time. The final Pod UIDs were:

- ordinal 0: `9ff8e018-b35f-474d-a0c0-90ad9f00490a`;
- ordinal 1: `1cd55acb-8568-46ff-8caf-9ed2e306af1d`;
- ordinal 2: `18e63db7-cb7b-4307-b21a-06573e8f5b73`; and
- ordinal 3: `c4e3b3d3-4241-4e4f-b835-f716c10ac3a4`.

Every final indexer used revision
`splunk-shc82-idxc-indexer-64d4d7b6d4` and runtime digest
`sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`,
was Ready with zero container restarts, and completed Ansible with
`ok=111`, `failed=0`. No replacement emitted
`Active KVStore version upgrade precheck FAILED`. The final Cluster Manager
check reported RF met, SF met, all data searchable, all peers Up and
searchable, no fixups, and readiness for a searchable rolling restart. The
four persistent peer GUIDs remained unchanged.

Three workload records covered failure injection, the complete roll, and
stable recovery:

| Run | Coverage | Result | Evidence SHA-256 |
|---|---|---|---|
| `official-844c593-controller-restart` | Controller deletion during ordinal-3 `Decommissioning`, recovery, and most of the four-peer roll | 100 submissions, zero HEC failures, zero search failures, final `count=100`, `min=1`, `max=100`, `distinct=100` | `46760971e2a3f31a04b837db2fc655a7146926b351ff2ee3f69c949a54cb8814` |
| `official-844c593-controller-restart-final` | Overlapping coverage through ordinal 0 and final convergence | 80 submissions, zero HEC failures, final `count=80`, `min=1`, `max=80`, `distinct=80`; one valid initial `count=0` result was classified as a search failure because the fresh result omitted `min/max` | `7872034182cacc7be9df2b67a7612e515ea44b6c3daa78e720131007bc0d8059` |
| `official-844c593-controller-restart-stable` | Stable post-roll service | 30 submissions, zero HEC failures, zero search failures, final `count=30`, `min=1`, `max=30`, `distinct=30` | `bdd63879007c265ca2a1b9268ee98a6cdcee7f6fa25233aea89ea4c162838e1e` |

The continuation record's first search returned a valid Splunk response with
`count=0` immediately after accepting its first event. The monitor calls that
a search failure because its aggregate parser requires `min`, `max`, and
`distinct`; it was not an HTTP or Service request failure. The final exact
result and the independent post-roll record preserve this distinction rather
than rewriting the raw evidence.

The replacement Operator emitted no panic. Its target namespace contained one
transient License Manager DNS lookup failure at `16:19:22Z` during controller
startup; that error did not recur and did not affect License Manager, indexer,
SHC, ingest, or search health. The log also contained repeated errors for an
unavailable Cluster Manager in a different retained test namespace. Those
entries are not evidence from this qualification namespace and were excluded
from the lifecycle result.

This campaign qualifies controller-Pod replacement during one persisted
Operator-owned indexer `Decommissioning` operation. It does not qualify a
long-duration API-server disconnection, leader failover with concurrent
controllers, conflicting desired-state changes, insufficient redundancy,
other network/TLS/HEC configurations, or Splunk-managed App Framework
next-peer selection.

## What the accepted searchable restart did

The Cluster Manager validated the bundle on all four peers and logged:

`Rolling restart with searchable=1 and force=0 initiated, pre-flight health
check results: rfMet=1 sfMet=1 allSearchable=1`.

Splunk then restarted peers internally in order `2 -> 1 -> 3 -> 0`. Each peer
progressed through `Up -> ReassigningPrimaries -> Restarting -> Up`. The
complete operation ran from the first decommission at
`11:29:54.817Z` until bundle-push success at `11:35:55.381Z`.
Replication-factor, search-factor, and all-searchable health remained
satisfied in the cluster observations. Pod UIDs did not change during the
operation and Kubernetes container restart counts remained zero because
Splunk restarted inside the existing containers.

The final workload result was:

- 55 submissions;
- zero HEC request failures;
- zero search-request failures;
- final `count=55`, `min=1`, `max=55`, and `distinct=55`; and
- no missing or duplicate sequence number.

Search results temporarily lagged accepted submissions during the restart and
later converged. Therefore `searchable=1` is evidence that Splunk coordinated
bucket availability and peer redundancy; it is not evidence that every
in-flight or immediately repeated distributed search returns an instantly
complete result.

## Comparison that isolated traffic readiness

The following runs used the supported four-peer RF3/SF2 topology and the same
restart-required app pattern.

| Run | Indexer traffic gate | HEC result | Final exact result | What it proves |
|---|---|---|---|---|
| `1.0.2` | Existing management-oriented readiness | 7 failures from 55 submissions | 48 distinct sequences, incomplete | Searchable rolling restart alone does not prevent the Kubernetes Service from routing HEC traffic to an internally restarting peer |
| `1.0.3` | HEC health added to indexer readiness; default 5-second period and failure threshold 3 | 1 failure from 55 submissions | 54 distinct sequences, incomplete | A serving-path check materially reduces the failure window, but the default failure budget leaves an observed 10-15 second withdrawal delay |
| `1.0.4` | Same HEC check; 2-second period and failure threshold 1 | 0 failures from 55 submissions | 55 distinct sequences, complete | Faster withdrawal can avoid failures in this workload, but does not eliminate the underlying asynchronous boundary |

The `1.0.3` monitor counted one search failure when its first empty result did
not contain the expected aggregate fields. That was a harness-classification
error, not an HTTP search failure. Its one HEC failure and missing accepted
sequence were real.

## Why the fast probe is not the complete solution

The peer-level `1.0.4` monitor independently checked every Pod's HEC endpoint
from another Pod, Kubernetes container readiness, and EndpointSlice
advertisement every approximately three seconds. It found:

- at least three EndpointSlice-ready indexers in every sample;
- two samples in which an HEC-unavailable peer was still advertised as ready;
- one transition where the next peer stopped serving HEC before the previous
  peer was remotely serving HEC again, leaving only two actually serving HEC
  peers for one sample; and
- no workload failure only because the 55 low-rate submissions did not land
  in those observed windows.

This is expected from the composition of several asynchronous systems:
Splunk changes process and cluster state, kubelet evaluates a local probe, the
Pod Ready condition changes, EndpointSlice state propagates, and clients
refresh or reuse connections. Reducing the probe interval reduces exposure;
it cannot make this chain atomic. Existing connections and client-side
endpoint caches also do not drain merely because a new EndpointSlice revision
exists.

The recovery side has a second boundary. The Cluster Manager marked a peer Up
and started decommissioning the next peer about half a second later. A peer
being Up/searchable in cluster metadata did not always mean that its HEC
listener was already reachable through the Pod network. The next restart must
not begin until the previous peer satisfies both cluster recovery and the
serving-path recovery contract.

## Controller/readiness coupling discovered during qualification

Applying the fast readiness configuration changed the IndexerCluster
StatefulSet template. Under the current `OnDelete` workflow the Operator:

1. detected the Pod-template revision difference;
2. lowered the highest old-revision peer's probe level;
3. requested peer decommission;
4. observed the HEC-aware readiness probe make the peer unready; and
5. repeatedly stopped at the generic `UpdateStatefulSetPods` readiness wait
   instead of deleting the already decommissioned target.

The source behavior is consistent with the observation:

- `validateReadinessProbe` reports values below its recommendations but does
  not reject or clamp them;
- `getProbeWithConfigUpdates` renders the configured non-zero values;
- `UpdateStatefulSetPods` normally waits when
  `StatefulSet.status.readyReplicas < spec.replicas`;
- its inner traversal also requires the container Ready status before it calls
  `PrepareRecycle`; and
- the IndexerCluster manager does not currently provide the verified
  readiness-withdrawal contract used by the Search Head lifecycle path.

The campaign advanced only after a controlled test procedure verified that
the one target was already `Restarting` or `Down`, deleted that exact Pod, and
waited for its new UID, desired revision, Kubernetes readiness, Cluster
Manager `Up/searchable` state, and stable health before touching another
ordinal. This manual advancement is test evidence, not an acceptable product
workflow.

Serving readiness must therefore be separate from controller progress. The
controller needs durable ownership and stage information for the one intended
target and must continue that target's authorized lifecycle while it is
deliberately absent from Service endpoints. Every unrelated unready Pod must
still make the workflow fail closed.

## Required product behavior

### Serving contract

Indexer traffic readiness must reflect the actual customer-facing path being
served. If HEC is enabled and exposed by an indexer Service, readiness must
not advertise that Pod while HEC is refusing or unable to accept work.
Protocol selection must come from the effective Splunk HEC SSL
configuration; it must not assume HTTPS, infer the local scheme from ingress
TLS, or require a service mesh.

HEC can be disabled. A hard-coded HEC check for every indexer would make such
deployments permanently unready. The contract must be role- and
configuration-aware. Kubernetes Pod readiness also affects every port of a
Service selecting that Pod. If management, ingest, Splunk-to-Splunk,
forwarding, and user traffic require different availability semantics, they
need explicitly separated Services or serving conditions instead of one
accidental all-port policy.

Liveness must remain a local process/deadlock decision. Detention,
decommission, a temporary cluster-manager loss, an HEC traffic withdrawal, or
a remote cluster-health failure must not cause a liveness restart cascade.

### Lifecycle contract

For Operator-owned replacement or shutdown, the required order is:

1. persist operation identity, target UID/revision, stage, and timeout budget;
2. withdraw only the target from the applicable traffic Service;
3. observe the withdrawal through Kubernetes before starting destructive
   work;
4. perform Splunk decommission, restart, or shutdown;
5. observe the process return;
6. require Cluster Manager `Up/searchable`, RF/SF health, and the applicable
   serving path from another network location;
7. require a bounded stability interval; and
8. complete the target and authorize the next peer.

The controller must not use global Pod Ready as the only lifecycle-progress
signal after it has deliberately withdrawn the authorized target. It must use
the durable target and stage plus authoritative Splunk and Kubernetes
observations. An unowned unready peer, changed UID, changed revision,
insufficient redundancy, or conflicting planned disruption must continue to
block progression.

For a Splunk-managed internal App Framework restart, the Operator currently
does not own each next-target boundary. Production qualification therefore
requires one of two explicit contracts:

- Splunk exposes a pre-restart target/hold and post-recovery signal that lets
  the Kubernetes layer withdraw and later qualify each peer; or
- the Operator requests bundle installation without an uncontrolled internal
  roll and owns the bounded per-peer restart workflow.

A faster probe is a useful compatibility mitigation when neither contract is
available, but it is not sufficient evidence for zero interruption.

### Delivery and search contract

Endpoint routing is not a delivery guarantee. HEC producers that require
lossless delivery must use bounded retry and an acknowledgment/idempotency
contract appropriate to their input path. The Operator cannot recover an
event that a client abandons after receiving a connection failure.

Qualification must separately report:

- request acceptance;
- acknowledgment, where enabled;
- exact eventual event completeness and duplication;
- immediate and eventual distributed-search completeness;
- active-search interruption;
- scheduled-search execution; and
- RF, SF, bucket primacy, and peer searchability.

## Remaining qualification gates

SHC-82 remains open until all of the following are source- and
environment-qualified:

- HEC enabled with HTTP and HTTPS;
- HEC disabled without permanently failing readiness;
- no service mesh, supported mesh modes, and ingress TLS termination;
- separate Service behavior for HEC and other indexer ports;
- persistent HEC connections and deliberately stale EndpointSlice/client
  caches;
- insufficient RF/SF/peer redundancy, which must fail closed;
- one peer already unhealthy before the app update;
- Operator restart and API disconnection during every durable stage;
- concurrent image rollout, app update, scale, node drain, and manual
  deletion;
- previous supported Splunk and Operator/image combinations;
- active, historical, real-time, and scheduled searches;
- client retry and HEC acknowledgment behavior;
- recovery stability before the next peer; and
- Operator restart and API-disconnection recovery during the now-qualified
  automatic serving-withdrawal lifecycle. The uninterrupted four-member run
  above removed the earlier manual-advancement requirement for the tested
  steady-controller path, but restart/disconnection and conflict recovery
  remain open.

## Current recommendation

Retain searchable rolling restart for supported indexer topologies, because
it provides Splunk's RF/SF/searchability coordination. Add an explicit,
configuration-aware serving contract and a durable one-target lifecycle
contract rather than relying on probe tuning alone. The later Operator-owned
campaign demonstrates that contract for one steady-controller RF3/SF2
revision roll and fixed Splunk build. Do not generalize that result to
Splunk-managed App Framework restarts, controller interruption, conflicting
disruptions, unsupported redundancy, or the remaining negative and
compatibility gates above.
