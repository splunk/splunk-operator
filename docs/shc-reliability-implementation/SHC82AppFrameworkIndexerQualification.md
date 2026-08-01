# SHC-82 App Framework Indexer Restart Availability Qualification

## Status and evidence boundary

This document records the indexer-side evidence first gathered for SHC-82 on
2026-07-30 UTC and the bounded SHC-85 Operator-lifecycle follow-up through
2026-08-01 UTC. It is not a production-readiness claim. The initial campaign
proved that Splunk's searchable rolling restart and Kubernetes traffic
readiness address different parts of the availability problem and identified
the controller-progress defect addressed by the later Operator-owned
lifecycle campaigns below.

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

The later leader-failover campaign reproduced the LicenseManager lookup on a
new leader and traced it to a deterministic regular-Service/headless-FQDN
mismatch. SHC-88 now retains that separate diagnostic requirement; its
existence does not change the bounded controller-restart verdict above.

This campaign qualifies controller-Pod replacement during one persisted
Operator-owned indexer `Decommissioning` operation. By itself it does not
qualify a long-duration API-server disconnection, leader failover with
concurrent controllers, conflicting desired-state changes, insufficient redundancy,
other network/TLS/HEC configurations, or Splunk-managed App Framework
next-peer selection.

## Five-minute controller-absence qualification

A 2026-07-31 UTC campaign extended SHC-85 from a normal controller-Pod
replacement to a complete five-minute period with no Operator controller
running. This is the bounded failure that had previously restarted an
intentionally stopped indexer: after the Operator withdrew the target and
Splunk decommissioned it, the existing level-one liveness probe still required
`splunkd`. If the controller remained unavailable long enough, kubelet treated
the intentional stop as a liveness failure and restarted the container in the
old Pod before the controller could perform the authorized replacement.

The corrected contract keeps three signals separate:

- readiness continues to remove the one authorized target from Services;
- durable IndexerCluster status retains the operation, exact target Pod UID,
  source revision, desired revision, and `ReadyForReplacement` stage; and
- liveness treats an initialized and responsive container as live during the
  explicit Operator-owned hold, even though `splunkd` is intentionally down.

The hold is narrow. It is written only with the Operator-owned indexer
readiness-withdrawal marker. A missing container-state file or an incomplete
container initialization state still fails liveness, and level-one liveness
without the explicit lifecycle marker still requires `splunkd`. Readiness does
not return the held target to traffic.

The campaign used:

- branch `codex/shc-85-lifecycle-hold-qualification`;
- lifecycle correction `5dbe7dac8`, qualification harness `854a76b8d`, and
  production SHC corrections `99da90390` and `ac1fe0db8`;
- production Operator source `ac1fe0db8` at immutable image digest
  `sha256:59fc2afdfafc7e0c2b9f49fceebf1862128521017311776b91f0ce3315eff608`;
- harness-only empty-result classification correction `5ab3a858b`;
- the official Splunk `10.5.2605.0/844c593e9c1d` runtime at digest
  `sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`;
  and
- EKS cluster `vivek-spl-301372`, namespace `shc85-lifecycle-hold`, a
  four-peer RF3/SF2 indexer cluster, and a three-member Search Head Cluster.

The lifecycle monitor created desired indexer revision
`splunk-shc85-idxc-indexer-dc496ddb9`, captured ordinal 3 immediately after
the operation durably reached `ReadyForReplacement`, and scaled the Operator
Deployment to zero. For 300 seconds it required all of the following on every
two-second sample:

- the exact operation, target UID
  `9d81e0d0-5b0a-4198-824d-42b7eed98c91`, and source and desired revisions
  remained unchanged;
- the target container remained running with the same UID and zero restarts,
  while the Pod stayed unready and absent from the EndpointSlice;
- all three non-target indexers retained their UIDs, zero restart counts,
  Kubernetes readiness, and Service publication;
- the explicit lifecycle marker remained present; and
- Kubernetes recorded zero indexer liveness failures and zero kubelet kill
  Events.

The monitor then restored the Operator. The same lifecycle completed in order
`3 -> 2 -> 1 -> 0`, never had more than one unavailable indexer, and required
the previous replacement to return to remote serving before selecting the
next target. Final Pod UIDs were:

- ordinal 0: `3d223b85-b654-4b9c-81d5-58c5fff0c3b2`;
- ordinal 1: `65b56d97-388f-4fbc-a8e0-fda07bd74b65`;
- ordinal 2: `33e6cc31-9e98-4cb2-ad60-05c3383013a2`; and
- ordinal 3: `29f15596-f175-429f-92a6-ccf9ba76545c`.

All four replacements used the desired revision, reached Ready with zero
container restarts, completed Ansible with `ok=111`, `failed=0`, and contained
no prior KV Store upgrade-precheck failure signature. The lifecycle marker was
absent from every replacement. The monitor passed after ten stable final
samples and reported `operator absence=300s order=3,2,1,0`.

Final Cluster Manager health reported RF met, SF met, all data searchable, all
peers Up, and no fixups. The IndexerCluster was Ready 4/4, the Search Head
Cluster was Ready 3/3, and the corresponding Services had four and three ready
endpoints.

The first long workload observer was workstation-driven. One Kubernetes API
operation stalled for approximately 294 seconds between sequence 129 at
`17:41:04Z` and sequence 130 at `17:45:58Z`. No request failed because the
monitor attempted no HEC submission or search during that gap. Its final exact
result was 420 submissions, zero HEC request failures, zero search-request
failures, and exact `count/min/max/distinct=420/1/420/420`. This can describe
sampled request success, but it is not accepted as continuous workload
evidence. Harness source `b2bf2e71d` therefore runs the
HEC/search loop in an in-cluster Job and leaves Kubernetes and Splunk telemetry
as separate observers.

The accepted repeat ran the independent Job from `17:57:45Z` through
`18:31:59Z`, spanning the complete controller absence, controller recovery,
four-Pod roll, and post-roll convergence. In parallel, the lifecycle monitor:

- moved from source revision `splunk-shc85-idxc-indexer-dc496ddb9` to desired
  revision `splunk-shc85-idxc-indexer-7cc68b874c`;
- held ordinal 3 and original Pod UID
  `29f15596-f175-429f-92a6-ccf9ba76545c` under the exact persisted operation
  while the Operator was absent for 302 observed seconds;
- completed the replacement order `3 -> 2 -> 1 -> 0` with ten stable final
  samples; and
- finished with replacement Pod UIDs
  `37c835b1-096a-45e1-bad3-07975244b448`,
  `20f6ef7b-1285-46bc-9d74-0d5eab267f3f`,
  `73c90c68-e2c7-4bd2-aa90-7949c25e05cf`, and
  `f1b2207f-c839-4b9d-ad88-a6c3ef60cef2` for ordinals 0 through 3.

The lifecycle evidence has SHA-256
`655c998ab4d6072769d8efa2c47c83c737f919a730ee3a72467f9714b4df9263`.
Every replacement remained at zero container restarts, completed Ansible with
`ok=111`, `failed=0`, omitted the previous KV Store precheck-failure signature,
and removed the lifecycle marker. Final Cluster Manager status again reported
RF met, SF met, all data searchable, all peers Up, and no fixups. All Splunk
CRs and expected Service endpoints were Ready. After controller restoration,
one License Manager reconciliation saw a transient headless-Service DNS miss;
the next reconciliations recovered without intervention.

The in-cluster workload submitted 1,800 numbered HEC events. It recorded zero
HEC request failures, zero exported-search request failures, zero client Pod
restarts, and final exact results of `count=1800`, `min=1`, `max=1800`, and
`distinct=1800`. Its log has SHA-256
`8b14b210e1224219ee1509b150036c3f599c68f11bbf22b98cbdce71bf1e3faf`.
This accepts the bounded lifecycle-hold and sampled request-availability gate;
it does not accept immediate result completeness.

Twenty-four successful aggregate-search samples returned a lower count than
the preceding successful sample. The maximum sequence-to-count gap was 362 at
`18:24:40Z`: sequence 1418 returned `count=1056`, `min=1`, `max=1417`, and
`distinct=1056`. The valid export response carried `messages: null`, so the
client received no partial-result indication. At matching times, every Search
Head logged `DistributedPeer`, `GetRemoteAuthToken`, `HttpClientRequest`, or
`TcpOutputFd` failures against terminating or newly starting indexer Pod IPs,
including `No route to host`, `Connection refused`, connection timeout, and
HTTP 401 during authentication refresh. Later searches converged to the exact
1,800-event result.

The accepted Job used harness source `b2bf2e71d`; the 24 regressions and
maximum gap above were calculated from its retained per-sample log. Follow-up
harness source `d610d4474` makes later runs report pending-count, count
regressions, and maximum pending count directly in the Job summary.

These observations prove a gap in the qualified contract, not a complete root
cause or an Operator-only fix. Search Heads use Splunk-managed peer addresses
for distributed search rather than the Kubernetes indexer Service. Cluster
Manager `Up/searchable`, Kubernetes endpoint publication, and the Operator's
remote HEC check therefore do not prove that every traffic-eligible Search
Head has removed an old peer address, connected and authenticated to the
replacement, or will explicitly report an incomplete search. That
per-Search-Head convergence and partial-result contract remains open.

## Five-minute observed-decommissioning absence qualification

A separate 2026-07-31 UTC branch
`codex/shc-85-decommissioning-absence-qualification` extended the same oracle
to the prior durable stage. Harness source `8d6a7dbc6` does not remove the
controller when decommission is merely requested. It waits until
`status.podUpdate.stage=Decommissioning`,
`observedDecommissioning=true`, and a decommission-request timestamp are all
persisted, proving that Splunk was observed in the side-effecting stage.
The production Operator remained source `ac1fe0db8` at digest
`sha256:59fc2afdfafc7e0c2b9f49fceebf1862128521017311776b91f0ce3315eff608`,
and the Splunk runtime remained
`10.5.2605.0/844c593e9c1d` at digest
`sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`.

The campaign moved from source revision
`splunk-shc85-idxc-indexer-7cc68b874c` to desired revision
`splunk-shc85-idxc-indexer-b577748bf`. Its first operation retained ordinal 3,
Pod UID `f1b2207f-c839-4b9d-ad88-a6c3ef60cef2`, and operation ID
`f1b2207f-c839-4b9d-ad88-a6c3ef60cef2:splunk-shc85-idxc-indexer-b577748bf:1785523838468286533`
through 306 observed seconds with no controller. During that interval:

- the same target container remained running and unready with zero restarts,
  outside the indexer Service;
- the same durable operation, target UID, source revision, desired revision,
  request timestamp, and observed-decommissioning fact remained unchanged;
- ordinals 0, 1, and 2 retained their UIDs, readiness, zero restart counts,
  and Service endpoints; and
- the monitor observed zero indexer liveness failures.

After controller restoration, the same lifecycle completed in order
`3 -> 2 -> 1 -> 0`, with ten stable final samples. Final replacement UIDs for
ordinals 0 through 3 were:

- `5f0d6419-6095-4f6c-a2d8-a9dbca82e498`;
- `baf075c8-40a1-4f6d-a54e-ca97b3f234c8`;
- `13870583-7184-4d4f-9b92-0431e0083e98`; and
- `bc966da8-226f-4aa6-928b-4874aa898d10`.

All four replacements used the desired revision, remained at zero container
restarts, completed Ansible with `ok=111`, `failed=0`, contained no previous
KV Store precheck-failure signature, and removed the lifecycle marker. Final
Cluster Manager health reported RF met, SF met, all data searchable, all peers
Up, and no fixups. All Splunk CRs were Ready, and all four indexer plus all
three Search Head endpoints were published. The lifecycle evidence has
SHA-256
`e457b347092503b7b4ddbec25047e3dbc1b120bc0293fb5a1cb82cd5a589bdde`.
One License Manager reconciliation immediately after controller restoration
encountered the same transient headless-Service DNS lookup failure seen in the
prior campaign and then recovered without intervention.

The independent Job `shc85-incluster-workload-chdhr`, Pod UID
`7495c4da-ee7a-48a3-a059-f70e9f0e70b5`, ran from `18:50:24Z` through
`19:24:39Z`. It submitted 1,800 events with zero HEC failures, zero
search-request failures, zero client restarts, and final exact
`count/min/max/distinct=1800/1/1800/1800`. Direct post-run searches on each
Search Head independently returned the same exact result. The workload log
has SHA-256
`cf169d21801d25eef3314351e6b5726bb53b8ca993ac1d2297f7c8bd728d4be0`.

Immediate completeness did not pass. The Job reported 41 successful-search
count regressions. Its maximum pending count was 406 at `19:17:27Z`, when
sequence 1423 returned `count=1017`, `min=1`, `max=1421`, and
`distinct=1017`. Splunk had already recorded lifecycle `finishedAt` at
`19:16:56Z`; the simultaneous lifecycle sample showed `Completed`, all four
Pods Ready on the desired revision, zero restarts, and all four endpoints.
At the same second, Search Head logs still showed `DistributedPeer` and
`GetRemoteAuthToken` attempts to old indexer IP `10.0.58.126`, returning
`No route to host`. All three Search Heads logged matching old-peer connection
or authentication failures during the roll.

This repeat closes the bounded five-minute `Decommissioning` controller-absence
gate. It also strengthens the open requirement: current lifecycle completion
and remote-serving recovery are too early to prove distributed-search result
completeness on every Search Head. The lifecycle must not be described as
end-to-end available until Splunk provides a supported per-Search-Head peer
convergence signal or explicitly marks incomplete results as partial.

## Five-minute readiness-withdrawal absence qualification

The isolated branch
`codex/shc-85-withdrawing-readiness-absence-qualification` moved the
controller-absence boundary earlier without changing the production Operator
or Splunk images. Harness source `978d71bc5699382df3b8d54355541aea0365f503`
waited for the durable `WithdrawingReadiness` stage and the explicit
Operator-owned lifecycle marker, then removed the sole controller immediately.
The production Operator remained source `ac1fe0db8` at immutable digest
`sha256:59fc2afdfafc7e0c2b9f49fceebf1862128521017311776b91f0ce3315eff608`;
the Splunk runtime remained `10.5.2605.0/844c593e9c1d` at digest
`sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`.

The campaign moved from source revision
`splunk-shc85-idxc-indexer-6c4676ff9d` to desired revision
`splunk-shc85-idxc-indexer-55fcc8f588`. It retained ordinal 3, Pod UID
`5e089220-4a62-46c2-b81b-1d0d7990f07c`, and operation ID
`5e089220-4a62-46c2-b81b-1d0d7990f07c:splunk-shc85-idxc-indexer-55fcc8f588:1785531269512728517`
through 306 observed seconds with no controller. During the absence, kubelet
readiness changed the still-running target to NotReady and EndpointSlice
removed it without controller participation. The operation, target UID,
revisions, and `WithdrawingReadiness` stage remained unchanged; the target and
all three non-target containers retained zero restarts; and the three
non-target Pods retained their original UIDs, readiness, and Service
endpoints. No indexer liveness failure was recorded.

After controller restoration, the same operation resumed and completed
`3 -> 2 -> 1 -> 0` with one unavailable peer at a time, previous-peer remote
serving recovery before each next target, and ten stable final samples. All
four replacements reached the desired revision and Ready state with zero
container restarts. Each completed Ansible with `ok=111`, `failed=0`, and none
contained the prior `Active KVStore version upgrade precheck FAILED`
signature. Cluster Manager health reported RF met, SF met, all data
searchable, all peers Up, and no fixups. Lifecycle evidence contains 209
samples from `20:54:28Z` through `21:24:06Z` and has SHA-256
`4bba7447b3c245621982bf92d0bf13bc020fdf49e3e49d1c5ac3bf07af0b3752`.

The final `OnDelete` StatefulSet reported `updatedReplicas=4` and all four Pod
labels matched `updateRevision=splunk-shc85-idxc-indexer-55fcc8f588`, while
`currentRevision` retained its original baseline revision. This is consistent
with the Kubernetes v1.31 StatefulSet controller: the `OnDelete` path
[short-circuits after status calculation](https://github.com/kubernetes/kubernetes/blob/v1.31.14/pkg/controller/statefulset/stateful_set_control.go#L508-L512),
and its target-state contract
[does not constrain Pod revisions](https://github.com/kubernetes/kubernetes/blob/v1.31.14/pkg/controller/statefulset/stateful_set_control.go#L311-L318).
The accepted oracle therefore proves the explicit desired revision on every
Pod rather than incorrectly requiring `currentRevision==updateRevision` under
`OnDelete`. The later `RollingUpdate` qualification must use its own native
revision and partition convergence contract.

The independent Job `shc85-incluster-workload-vgqbn`, Pod UID
`4c898950-232a-458d-bdff-df4f026a453c`, ran from `20:54:05Z` through
`21:28:26Z` with zero Pod restarts. It submitted 1,800 numbered events with
zero HEC failures and zero search-request failures. The final result was exact
on the Service and independently on all three Search Heads:
`count/min/max/distinct=1800/1/1800/1800`. Its 1,802-line log has SHA-256
`7b2c7ae19ce41efda8ddb21a2e67d29192fb8589128511fbc17c57ebc034ac7a`.

Immediate completeness remains open. The Job reported 37 successful-search
count regressions and maximum pending 404 at sequence 1522. Therefore this
campaign closes only the bounded five-minute `WithdrawingReadiness`
controller-absence gate. It does not turn a successful but incomplete search
response into an availability success or remove the requirement for supported
per-Search-Head peer convergence and partial-result signaling. A later,
separate campaign qualifies bounded API-server disconnection; this one does
not.

## Five-minute target-selection absence qualification

The isolated branch
`codex/shc-85-target-selected-absence-qualification` moved the absence
boundary to the first durable lifecycle stage. Harness sources `2d430748b`
and `770a27799` use an unbuffered Kubernetes watch because `TargetSelected` is
intentionally short, then apply the supported IndexerCluster pause annotation,
scale the controller Deployment to zero, and remove the remaining controller
Pod concurrently. The monitor accepts the run only when the exact persisted
stage is still `TargetSelected`, the pause is present, all four original Pods
remain Ready and published, and the target has no readiness-withdrawal marker.
The pause is test-only fault-injection coordination. It is removed after the
controller is restored and is not proposed as a production rollout step.
The deployed Operator remained source `ac1fe0db8` at immutable digest
`sha256:59fc2afdfafc7e0c2b9f49fceebf1862128521017311776b91f0ce3315eff608`;
the Splunk runtime remained `10.5.2605.0/844c593e9c1d` at digest
`sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`.

The campaign moved from source revision
`splunk-shc85-idxc-indexer-7fc9cd47d8` to desired revision
`splunk-shc85-idxc-indexer-5f74d6565d`. It retained ordinal 3, Pod UID
`51f86f00-6319-43db-9d24-ff1d754ff020`, and operation ID
`51f86f00-6319-43db-9d24-ff1d754ff020:splunk-shc85-idxc-indexer-5f74d6565d:1785538176691741815`
for exactly 300 controller-absent seconds. The operation identity and
revisions did not change; all four Pods retained their UIDs, readiness,
EndpointSlice publication, and zero restart counts; the target remained free
of the lifecycle marker; and no indexer liveness failure was recorded.

After controller restoration and pause removal, the same operation advanced
normally through readiness withdrawal and decommission. The complete roll
followed `3 -> 2 -> 1 -> 0`, permitted only one unavailable peer at a time,
required previous-peer remote serving recovery before selecting the next
ordinal, and ended with ten stable samples. Final replacement UIDs for
ordinals 0 through 3 were:

- `a696eb67-445f-475a-9767-62f8ac97a5d3`;
- `0a8ee3df-e2e8-40c8-a33c-89f46e068575`;
- `b25e54f4-c13f-4c30-96d7-ddfce9f39d66`; and
- `1af649c1-e47c-4b36-83bf-086750e4808b`.

All four replacements were Ready on the desired revision with zero container
restarts. Each Ansible run completed with `ok=111`, `failed=0`, and none
contained `Active KVStore version upgrade precheck FAILED`. Cluster Manager
health reported RF met, SF met, all data searchable, all peers Up, no fixups,
and readiness for searchable rolling restart. Every Splunk CR was Ready, all
four indexer endpoints and all three Search Head endpoints were published,
and the elected captain was ordinal 1. The lifecycle record contains 228
samples from `22:49:35Z` through `23:18:24Z` and has SHA-256
`01f3cf1fe9330b2a139a2243d2ca3f5771bfada39ee9d25bd267410d52ef9c0e`.

The independent Job `shc85-incluster-workload-8xl68`, Pod UID
`655f8a4e-631c-44fa-91c8-94752892f4e5`, ran from `22:49:09Z` through
`23:23:30Z` with zero Pod restarts. It submitted 1,800 numbered events with
zero HEC failures and zero search-request failures. The final result and
direct queries on every Search Head were exact:
`count/min/max/distinct=1800/1/1800/1800`. Its 1,802-line log has SHA-256
`d0d8de5eb851bea87a9057f0676e1b5d5f6e16a7ea134e0130d4af04ea6b2c3d`.

Immediate completeness remains open. The Job reported 18 successful-search
count regressions and maximum pending 364 at sequence 1481 at `23:17:23Z`.
The lifecycle had already persisted `Completed` at `23:16:52Z`; all four Pods
were Ready and published on the desired revision. During the same minute all
three Search Heads logged distributed-peer, authentication-token, or TCP
connection failures to old indexer Pod IPs. Old-address attempts continued
after final exact convergence even though current indexer Pod IPs were
different. The run therefore closes only the bounded five-minute
`TargetSelected` controller-absence gate. It does not qualify a running
controller losing API-server connectivity, desired-state conflict, or the
customer-visible immediate distributed-search completeness contract.

## Controller API-server disconnection qualification

The isolated branch `codex/shc-85-api-disconnection-qualification` exercised
the K8S-006 boundary that the controller-absence campaigns did not cover. The
fault harness adds one exact Pod-local `OUTPUT` reject rule for the Kubernetes
API Service address and port to a root, `NET_ADMIN` ephemeral container in the
Operator Pod using a test-only privileged debug profile. It first proves an
HTTP 200 API response, proves that the same
request fails after the rule is installed, keeps a fail-safe removal timer,
and proves HTTP 200 again after removal. It does not change node, workload-Pod,
Service, DNS, or Splunk traffic. Harness commits `8e21b9b1b` through
`f78828cc1` also pin the diagnostic image by digest and check in the narrow
custom debug profile used by the campaign.

The accepted 2026-08-01 UTC run isolated Operator Pod UID
`450b9597-fffc-4b15-8907-09d8045e4fbd` from the API server for 401 seconds
while ordinal 3 was durably in observed `Decommissioning`. The exact operation
ID, target Pod UID, source revision, desired revision, request timestamp, and
stage did not change. Thirty-six hold observations covered 302 seconds. In
every sample the target remained running, unready, outside the EndpointSlice,
and at zero restarts; the other three indexers remained Ready and serving;
and no indexer liveness failure was observed.

The manager could no longer renew its leader-election lease and exited with
`leader election lost`. Kubernetes restarted only the manager container in
the same Operator Pod. The replacement manager still had no API path until
the fault timer removed the rule. This is expected controller-runtime
behavior under lease loss, and the evidence requires both the stable Pod UID
and the changed manager container identity so that a container restart cannot
be mistaken for uninterrupted controller execution. The fault record proves
`API_FAULT_APPLIED before=200 blocked=true` and
`API_FAULT_REMOVED after=200`; its SHA-256 is
`a324e0bc639eaba052b475f1342a7595c42be479a38914ead4678b09cfb8876a`.

After API recovery, the restarted manager resumed the same ordinal-3
operation at `ReadyForReplacement` and completed `3 -> 2 -> 1 -> 0`. The
125-sample companion record ended with ten stable samples, one unavailable
peer at most, four desired-revision Ready Pods, zero indexer restarts, and
`PASS order=3,2,1,0 stable=10`; its SHA-256 is
`49dc69a31444997ddf5d5c8045bcfd840002937fd621bdbc4df700f2b1c1de7e`.
All four final Ansible recaps reported `ok=111` and `failed=0`. Cluster Manager
health reported RF met, SF met, all data searchable, every peer Up, no fixups,
and readiness for searchable rolling restart. Every Splunk CR was Ready, and
direct searches on all three Search Heads returned
`count/min/max/distinct=1800/1/1800/1800`.

The API-independent Job submitted all 1,800 events with zero HEC failures,
zero search-request failures, and exact eventual completeness. It recorded 30
successful-search count regressions and maximum pending 417 at sequence 1507,
so the immediate distributed-search completeness requirement remains open.
The 1,802-line workload record has SHA-256
`24328227463010c469cc73c5c24e1bf720f26fa001c804520eb46720d73255a4`.

The accepted evidence is intentionally split between the 42-line hold record
and the companion resume record. After the hold had completed, the test shell
encountered an observer-only parse failure because its source file was edited
in place while that process was still executing. The fail-safe fault container
independently removed the rule and proved API recovery; the companion observer
then captured the complete resumed roll. This was not an Operator or Splunk
failure and is not hidden by treating the two records as one uninterrupted
monitor. The hold-record SHA-256 is
`aca61282531551a7ec970dd2b0139be35dde2c0e1494117ed33e03ff9add5510`.

This closes the bounded K8S-006 API-disconnection gate for one Operator Pod,
one manager container restart, and the observed ordinal-3 `Decommissioning`
boundary. It does not qualify concurrent leaders, API partitions at every
stage, desired-state conflict, insufficient redundancy, or repeated and
long-duration network faults.

## Operator controller leader-failover qualification

The isolated branch `codex/shc-85-leader-failover-qualification` exercises
STS-004 using the Operator's normal Kubernetes Lease protocol. Harness source
`ba220677b` starts from one Ready controller, scales the Deployment to two,
requires both zero-restart Pods to remain Ready while one stable holder renews
Lease `270bec8c.splunk.com`, and then starts a harmless four-indexer revision.
This is different from the API-disconnection campaign: both contenders keep
API connectivity and only the exact active leader Pod is removed.

The run used EKS context `shc85-vivek-spl-301372`, namespace
`shc85-lifecycle-hold`, Operator digest
`sha256:59fc2afdfafc7e0c2b9f49fceebf1862128521017311776b91f0ce3315eff608`,
and the official Splunk build `10.5.2605.0/844c593e9c1d` at runtime digest
`sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`.
The four-indexer topology remained RF3/SF2 with three Search Heads and a
referenced LicenseManager.

At `2026-08-01T02:34:45Z`, ordinal 3 had durably recorded
`Decommissioning`, `observedDecommissioning=true`, the original target UID,
source and desired revisions, and a single decommission Event. The harness
force-deleted active leader Pod UID
`4535a36c-ec11-4a69-8d38-6855054148bd`. Kubernetes does not promise that an
already-waiting follower wins an election. In this run, the newly created
replacement Pod UID `0d250705-9af5-4fc2-a5e1-902608db1a93` legitimately
acquired the Lease after expiry. The transition count increased once from 80
to 81, the successor logged successful acquisition, and the bounded takeover
check completed in 53 seconds. Both controller Pods were Ready with zero
restarts afterward.

The same ordinal-3 operation ID, target UID, revisions, and decommission
timestamp survived takeover. The successor completed the exact
`3 -> 2 -> 1 -> 0` order while the second controller remained only a
contender. All 149 lifecycle observations in the 150-line record retained at
most one unavailable indexer, zero indexer restarts, one stable successor
Lease holder, and two Ready zero-restart controller Pods until convergence.
The target-specific
`IndexerDecommissionRequested` Event count remained one before and after the
roll. Ten final stable samples passed, and no lifecycle marker remained.
Scaling back to the original single replica removed the successor, so the
retained controller correctly acquired the Lease and increased the transition
count from 81 to 82 during cleanup. The harness required that final holder to
renew before reporting `PASS`.

The lifecycle TSV, bounded leader record, and 1,802-line workload log have
SHA-256 values
`9b7193931ac6c72f02edc45265a303d4f88a8e59da6967d6e03e368f837ae6f3`,
`c6d662265eec4e5c5683f344c3f7e39a6532f23264253320d9db113ada66a409`,
and `e34ef36dd49a7f835028d13ebd3336fdd1090f7b7210bbc50d78f27f3ec1ed05`.
The workload submitted 1,800 events with zero HEC or search-request failures
and exact eventual completeness. It recorded 13 successful-search count
regressions and maximum pending 329 at sequence 1239, so immediate
distributed-search completeness remains open. Direct final searches on all
three Search Heads returned `count/min/max/distinct=1800/1/1800/1800` without
a partial-result message.

Final Cluster Manager health reported RF met, SF met, all data searchable,
all four peers `Up`, no fixups, and readiness for searchable rolling restart.
Every Splunk CR was Ready, all four indexer and three Search Head endpoints
were published, all four Ansible recaps reported `ok=111` and `failed=0`, and
no prior KV Store precheck-failure signature or indexer liveness failure was
present. This passes one normal single-active-leader takeover with two healthy
contenders at observed ordinal-3 `Decommissioning`. It does not inject two
simultaneous active leaders, corrupt or delete the Lease, partition the
contenders, exercise API quorum loss, or qualify repeated failovers and other
lifecycle stages.

The cleanup leader start also exposed one adjacent LicenseManager diagnostic
gap. `licensemanager.go` creates only the regular LicenseManager Service, but
`checkLicenseRelatedPodFailures` constructs a Pod FQDN below a nonexistent
LicenseManager headless Service. The Operator logged DNS `no such host`,
continued reconciliation, and left the healthy LicenseManager CR Ready; the
license-expiration query was therefore skipped. Cluster inspection confirmed
that the regular Service and endpoint existed while the headless Service and
EndpointSlice did not. This is registered separately as SHC-88 and is not
claimed as an SHC-85 implementation or failover defect.

## Search Head defects exposed while forming the fixture

Fresh formation for this campaign exposed two independent Search Head captain
transition defects before the indexer fault could be injected:

1. Captain information can be proxied through an active member, but
   `captain/members` is captain-only. After captaincy moved away from ordinal
   zero, the Operator learned the new captain label and still sent the
   authoritative member query to the old ordinal. Splunk correctly returned
   HTTP 503 with the new captain location. Source `99da90390` maps the observed
   captain label to its member ordinal and performs the captain-only query on
   that elected captain.
2. A fresh observation later proved that captain transfer had completed, but
   the workflow evaluated its already-expired transfer deadline before it
   accepted that successful observation. Source `ac1fe0db8` accepts a fresh,
   available, non-conflicting observation of a different ready captain before
   applying the timeout. Missing, stale, conflicting, empty, or unready
   captain observations still fail closed.

Both changes have regression tests and passed the complete Linux
`make fmt vet build test` gate: 41 suites and 156 specs with zero failures.
Because the running operation had already entered terminal `Blocked` state
before the corrected image was deployed, the qualification used one explicit
test-only status repair from `Blocked` back to its prior
`TransferringCaptain` stage. The unchanged operation then advanced through
captain transfer and replacement and completed. This repair is part of the
test record; it is not presented as normal product recovery and no Splunk
Enterprise source change was used.

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
- Operator restart or absence durations and topologies beyond the qualified
  `Decommissioning` restart and five-minute absences during
  `TargetSelected`, `WithdrawingReadiness`, observed `Decommissioning`, and
  `ReadyForReplacement`, plus API-server disconnection at observed
  `Decommissioning`, and one normal two-contender leader takeover at observed
  `Decommissioning`; other stages and topologies remain open;
- concurrent image rollout, app update, scale, node drain, and manual
  deletion;
- previous supported Splunk and Operator/image combinations;
- active, historical, real-time, and scheduled searches;
- per-Search-Head old-peer removal, replacement-peer address/authentication
  convergence, and explicit partial-result signaling when a peer cannot
  participate;
- client retry and HEC acknowledgment behavior;
- recovery stability before the next peer; and
- repeated leader failover, split-brain/concurrent-active-controller faults,
  and desired-state conflict recovery during the automatic
  serving-withdrawal lifecycle.

## Current recommendation

Retain searchable rolling restart for supported indexer topologies, because
it provides Splunk's RF/SF/searchability coordination. Add an explicit,
configuration-aware serving contract and a durable one-target lifecycle
contract rather than relying on probe tuning alone. The later Operator-owned
campaigns demonstrate that contract for one steady-controller RF3/SF2
revision roll, one controller-Pod restart during `Decommissioning`, and one
five-minute controller absence during each of `TargetSelected`,
`WithdrawingReadiness`, observed `Decommissioning`, and `ReadyForReplacement`
on the fixed Splunk build. A later campaign also demonstrates recovery from a
401-second Pod-local API-server disconnection at observed `Decommissioning`,
including the expected leader-lease-loss manager restart. One further
campaign demonstrates a normal two-contender Lease takeover after deleting
the active controller at the same durable stage. Do not generalize those
results to Splunk-managed App Framework restarts, split-brain faults, repeated
failovers, other API-partition stages or topologies,
conflicting disruptions, unsupported redundancy, or the remaining negative
and compatibility gates above.
