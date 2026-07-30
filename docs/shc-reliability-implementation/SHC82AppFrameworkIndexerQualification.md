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
- automatic controller progress after intentional serving withdrawal, with no
  manual Pod deletion.

## Current recommendation

Retain searchable rolling restart for supported indexer topologies, because
it provides Splunk's RF/SF/searchability coordination. Add an explicit,
configuration-aware serving contract and a durable one-target lifecycle
contract rather than relying on probe tuning alone. Do not declare the
workflow interruption-free until the controller can continue an owned
withdrawn target, the previous peer is proven remotely serving before the next
restart, and the workload passes the negative and compatibility gates above.
