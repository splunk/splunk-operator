# SHC-95 Search Head App-Restart Qualification

## Scope

SHC-95 qualifies the customer-visible availability of a three-member Search
Head Cluster while App Framework installs a restart-required app. It also
separates three mechanisms that must not be treated as interchangeable:

1. a Kubernetes StatefulSet revision roll owned by the Operator;
2. a Splunk-managed internal rolling restart inside unchanged Pods; and
3. a live edit of replicated SHC configuration.

This record is intentionally bounded to the current Splunk, Docker-Splunk, and
Operator implementations used by the campaign. It does not claim that the
complete OPS-011 matrix is qualified.

## Immutable inputs

- Kubernetes context:
  `arn:aws:eks:us-west-2:667741767953:cluster/vivek-spl-301372`;
- namespace: `shc-final-qualification`;
- topology: one LicenseManager, one ClusterManager, four indexers with RF3/SF2,
  one deployer, and three Search Heads;
- Splunk build: `10.5.2605.0/844c593e9c1d`;
- runtime image digest used for the first recorded runs:
  `sha256:d6e11fe00dcadb6a3b168b23081950f85265daf0c923a314034160a495a6db4b`;
- Operator image digest used for the first recorded runs:
  `sha256:968e2855193c3e236a3b9eb4ad0f14d55b412ca4a6e07fd6fc3e822b8dc3acb3`;
- replicated-policy baseline Operator source:
  `d9742547beca0ff0668386a82bdfd4f376bd6a7a`;
- replicated-policy baseline Operator image digest:
  `sha256:6d914bfad64b26a776aee0627946a299a20e98b71d537809d600e949fb0af897`;
- final-runtime resume Operator source:
  `bb4f598193603bf7bf8eeee2faecc5de83041865`;
- final-runtime resume Operator OCI index digest:
  `sha256:104d2c908e691eb9ee24460e173d3114b46c5d5917a7bda3f4796d2940bab7bb`;
- final-runtime resume Operator Linux AMD64 manifest digest:
  `sha256:b0f0dbdfc365337d78c2d073d047bbc6061727cb5a769afe14d6bcb115f7c3d5`;
- final Docker-Splunk source:
  `5ee4c85860cf7a3868e45de5a4e4df76247170c0`;
- final Splunk-Ansible source:
  `a7af832c604f5fd9202e28f67261370ffe514402`;
- final runtime OCI index digest under qualification:
  `sha256:381fd7a878765d8ff7a222c04fb2dd26d150bc7f71375fa90485cf09445e56de`;
- final runtime Linux AMD64 manifest digest:
  `sha256:1c44527b63ca612a7d35e4307644c3bd0497c6f61251344eb845def7b174ffed`;
- Kubernetes-owned App Framework restart implementation source:
  `dda3142f5abdb743fa5e4ac7d211ca6d8bf06481`;
- authoritative restart-observation correction:
  `4c54c1bf8c2b08a9d6c7b4f4101c5e4689254c2b`;
- converged stale-image-intent correction and final EKS source:
  `71beda7b5026e036013e9dcc43f56fb6b606735d`;
- final Operator OCI index digest:
  `sha256:fa0b8eae504f11b10c2af5a16eaf71ffc878eb80ee74bf50d733492dd56ddb80`;
- final Operator Linux AMD64 manifest digest:
  `sha256:7508d144a2394eaa6f6fc1d748295b169f7f1dc547325274b455a04f9cfd204a`;
- final integrated branch source after diagnostic redaction:
  `8aebfb0cc1dea1de7d7eaad0b51460deab63df69`;
- final integrated Operator OCI index digest:
  `sha256:0b2eb8f11d881f28d0dbb38417bdd49bce2695e8d8cb5245eedee7bdd3d405d4`;
- final integrated Operator Linux AMD64 manifest digest:
  `sha256:c879a74ec7324e9ada3a0bf52b4b85b4b0465b1a2a61d855e7f904781ab1897b`;
- deterministic Search Head test app: version `1.0.2`, SHA-256
  `85040ddf61eaf84430818f1c42f0d098a2a8982670b4c2b54af6d6554079f40a`;
- exact App Framework bundle and Pod-template restart revision:
  `34a81a7ae95db0ce9d7cdd8ba32e18165f529589e9cb8ec10d6d3bd28a0d643d`;
- feature gates:
  `SplunkPodLifecycle=true`, `SearchHeadClusterLifecycle=true`, and
  `IndexerClusterLifecycle=true`.

## Accepted evidence

### Phase-three App Framework convergence

Legacy App Framework state can retain `deployStatus=1` after the deployer has
already completed installation and bundle push stage three. The controller
must use the durable phase-three terminal record rather than keep reporting
`AppFrameworkOperationActive`. Source `3903e01df` implements that correction.

### Splunk-managed classic restart

The 300-sample record in
`build/_test/shc-final/operator-recovery-shc-roll-v1.log` accepted and later
found every HEC sequence exactly once, but three Service searches failed while
Splunk performed internal member restarts with the effective
`rolling_restart=restart` policy. The evidence checksum is:

`f256e6548b1e9b73c6ddab24340e83b26fbd20594eafca67d400facdf864d307`.

The failures bound two independent gaps. The legacy three-failure readiness
window can continue advertising a Pod after its local management endpoint has
stopped, and classic SHC restart does not wait for admitted searches to drain.
Source `3f8881f52` changes only the legacy-default readiness tuple for the SHC
lifecycle path to timeout two seconds, period two seconds, and failure
threshold one. Explicit user probe configuration remains unchanged.

### Operator-owned StatefulSet replacement

The 120-sample record in
`build/_test/shc-final/searchable-config-roll-v2.log` replaced Search Head
ordinals `2 -> 1 -> 0`, kept at least two serving endpoints, recorded zero
container restarts, and completed with:

- 120 submitted HEC events;
- zero HEC request failures;
- zero search request failures; and
- exact final `count=120`, `min=1`, `max=120`, and `distinct=120`.

Its SHA-256 checksum is
`224c63bd8190a4734b0521eea9596af4855a561ca4af53cdec88bb71eaba7267`.

### Local-overlay repair and controller recovery

The 120-sample record in
`build/_test/shc-final/local-overlay-repair.log` repaired the accidental
member-local `server.conf` overlay and again replaced Search Head ordinals
`2 -> 1 -> 0`. The Operator itself was replaced during ordinal zero recovery.
The new controller observed the durable `WaitingForKubernetes` stage, resumed
that ordinal, and emitted one terminal `SHCRolloutCompleted` event without
deleting a second Pod or skipping the in-progress Pod.

The run completed with 120 submitted HEC events, zero HEC request failures,
zero search request failures, exact final `count=120`, `min=1`, `max=120`, and
`distinct=120`, at least two serving Search Head endpoints, and zero container
restarts. Its SHA-256 checksum is
`b49bf7185f95a4bcf352c78b488d29a04b58b0aa92fbbfc10690c1fd0d4a9a57`.

After convergence, all three members reported the replicated values
`rolling_restart=searchable`, `decommission_search_jobs_wait_secs=180`, and
`rolling_restart_with_captaincy_exchange=true`. Decrypting each member's
effective `[general] pass4SymmKey` produced the same SHA-256 digest as the
mounted secret, confirming that repair restored the cluster credential rather
than merely making the Pods ready.

### Final-source build gates

On the Linux build host, final Operator source `d9742547b` passed `make test`:
43 Ginkgo suites passed, including 192 of 192 controller specs, with zero
failures. `make helm-check` also passed all chart lints and 150 Helm unit tests.
The final Docker-Splunk image passed its pinned-Ansible reference tests and all
15 shutdown tests, including direct TERM, preStop plus TERM, concurrent
callers, stop failure, and timeout cases. These gates establish source and
packaging correctness; the live immutable runtime rollout remains a separate
acceptance item.

### Same-version runtime image classification

The first final-runtime attempt changed the declared image from one immutable
digest to another while retaining the same Splunk build. LicenseManager and
ClusterManager converged first, and the four-indexer rollout then proceeded.
The Search Head StatefulSet held partition three and the controller reported
`UnknownUpgradePath`, because an image-reference difference is not evidence
that the embedded Splunk versions form a supported upgrade path. This is the
intended fail-closed behavior of the version-upgrade classifier; parsing a
private-registry name or assuming that two opaque digests contain the same
build would be unsafe.

The run also demonstrated that the existing API could not distinguish a
same-Splunk-version Docker-Splunk/Ansible replacement from a Splunk version
upgrade. Source `ea844b0e0` adds the explicit image-intent contract anticipated
by the image-upgrade design. One declaration binds `SameVersionRestart` to an
exact source image and exact target image. It is valid only with the
partition-gated `RollingUpdate` strategy, cannot authorize a later target,
and is rejected if any Pod image or controller revision falls outside the
current StatefulSet partition boundary. A matching declaration uses the
ordinary per-member lifecycle and does not call Splunk's `upgrade-init` or
`upgrade-finalize` APIs. An omitted, incomplete, stale, or mismatched
declaration retains the existing fail-closed version-upgrade behavior.

The source tests cover initial classification, continuation after a higher
ordinal reached the target digest, mismatch rejection, API round-trip and
deep-copy behavior, and pre-reconciliation validation.

The same attempt showed that Deployer replacement occurred before member image
classification returned `UnknownUpgradePath`. A newer or unsupported Deployer
must not become capable of pushing bundles to an SHC whose member transition
has not been authorized. Source `a92a93d8e` therefore adds a read-only member
image preflight before any Deployer StatefulSet apply or Pod replacement on
the lifecycle-enabled `RollingUpdate` path. Unknown, unsupported, mixed, or
stale transitions now stop before Deployer mutation. The exact same-version
pair and an already recorded version-upgrade workflow may proceed; the legacy
`OnDelete` path is deliberately unchanged.

Live qualification then exposed a resume boundary in the first version of the
contract. The Operator durably recorded ordinal 2, withdrew that Pod's serving
readiness, and entered `ValidatingCluster`. On the next reconciliation the
classifier required every Pod to remain `PodReady`, including the target whose
readiness the same operation had deliberately withdrawn. The partition stayed
at three and no Pod was deleted, so availability was preserved, but the state
machine could not progress.

Source `bb4f59819` closes that self-deadlock. Initial classification still
requires every member to be present, non-deleting, ready, and exactly aligned
with the declared source/target pair and partition. After a matching
`PodUpdate` operation durably owns one ordinal, only that ordinal may be
unready, terminating, absent during replacement, or starting on the target
revision. A present target must match the exact source/current or
target/update pair; every unowned member remains subject to the original
strict invariant. Tests cover each permitted transition and reject an
unavailable unowned member or undeclared image. The immutable live Operator
resumed the persisted ordinal-2 operation, advanced through detention, lowered
the partition to two, and created the target-digest replacement without
creating image-upgrade status or invoking version-upgrade initialization.

The enclosing runtime workload window completed 240 of 240 acknowledged HEC
writes and 240 of 240 distributed-search results with zero request failures,
exact final sequence convergence, and no container restarts. Its local evidence
is `build/_test/shc-final/final-runtime-381fd7a.log`, with SHA-256
`9657f8d08088a8dda845625bbdc9fcc3fee7eab8bbb258a22f5c890234194e99`.

A second 180-sample workload window observed all three target-digest
replacements complete in reverse ordinal order. Ordinal 2 recovered before
ordinal 1 was withdrawn; ordinal 1 recovered before ordinal 0 was withdrawn;
and captaincy moved from ordinal 0 to ordinal 2 before ordinal 0's partition
was released. The service retained at least two endpoints and returned to
three of three ready and serving members. The window completed 180 HEC writes
and 180 distributed-search results with zero request failures, exact sequence
convergence, and no container restarts. All ten Splunk workload Pods finished
on the exact target image and Linux AMD64 image ID, and the Search Head
StatefulSet returned to partition three with equal current and update
revisions.

### Searchable Splunk-managed App Framework restart

The restart-required Search Head app version `1.0.1` was then delivered as the
only object in the App Framework repository. With the effective replicated
policy set to `rolling_restart=searchable`, Splunk performed an internal
member sequence in ordinal order `0 -> 1 -> 2`. The StatefulSet did not replace
any Pod: every Search Head Pod UID remained unchanged and every container
restart count remained zero. A Pod `preStop` hook cannot participate in this
path because the container itself is not being terminated.

The 150-sample workload record completed all 150 acknowledged HEC writes with
exact final `count=150`, `min=1`, `max=150`, and `distinct=150`. It recorded one
distributed-search request failure. At that sample Kubernetes already exposed
only two Search Head endpoints and the affected member's serving condition was
false, but the request reached a stale kube-proxy or dataplane route and the
member's management port refused the connection. All subsequent searches
succeeded. The evidence is
`build/_test/shc-final/shc-final-search-app-101.log`, with SHA-256
`5d2c22e741b9048460bc1f9dd87a935ed4f7a4f50a075d203addbac7edff8921`.

This run does not establish zero-disruption availability for Splunk-managed
internal restart. It establishes why a restart-required bundle should become
a durable StatefulSet revision whose readiness withdrawal, drain, captain
transfer, Pod replacement, and recovery are owned by one lifecycle.

### Kubernetes-owned App Framework restart

App version `1.0.2` exercised the Kubernetes-owned path. The deployer staged
and sent the bundle without starting an internal Splunk rolling restart. All
three Search Heads then advertised that a restart was required, while their
Pod UIDs, container restart counts, Splunkd process IDs, and process start
times remained unchanged. This separates bundle delivery from the disruptive
work and gives the Operator a durable boundary at which to schedule the
restart.

The first live candidate exposed an observation gap. Before a lifecycle was
already active, the controller queried the authoritative captain-members view
only during initial formation or captain change. Sending a bundle without
allowing Splunk to start its internal restart therefore left no trigger for the
new Kubernetes lifecycle. Source `4c54c1bf8` records an exact
`AppFrameworkRestartObservedRevision` and forces one authoritative observation
for each completed, previously unobserved bundle. A positive observation
schedules the exact restart revision; a negative observation is also retained
so ordinary reconciliation does not poll the captain indefinitely. Replacing
the Operator after bundle send proved that the observation resumed from CR
status rather than process memory.

The resulting annotation-only StatefulSet revision exposed a second ownership
boundary. The CR still retained the already completed `SameVersionRestart`
image intent from the preceding immutable runtime rollout. Although every Pod
had converged to that target image, the stale declaration initially claimed
the new annotation-only revision and rejected its partition boundary. Source
`71beda7b5` first proves that every expected Pod already uses the declared
target image. A converged declaration can no longer authorize or block a later
configuration, Secret, certificate, or App Framework Pod-template revision.
The regression test retains fail-closed behavior while any image transition is
actually incomplete. No Search Head was withdrawn before this correction was
deployed.

With exact source `71beda7b5`, the bundle revision became StatefulSet revision
`splunk-shcfinal-shc-search-head-68bcb58885` and replaced members in order
`2 -> 1 -> 0`. Ordinal 2 was the captain, so captaincy moved to ordinal 0
before its replacement. Ordinal 0 later held captaincy, so captaincy moved to
ordinal 1 before ordinal 0 was replaced. The controller was replaced during
the bundle-to-lifecycle handoff and resumed the same content-addressed
revision. The final state has:

- SearchHeadCluster `Ready`, three of three members ready, with ordinal 1 as
  captain;
- StatefulSet partition three and equal current/update revision
  `splunk-shcfinal-shc-search-head-68bcb58885`;
- all three Pods on the exact runtime digest with zero container restarts;
- the same exact bundle value in `AppFrameworkBundleRevision`,
  `AppFrameworkRestartObservedRevision`, `AppFrameworkRestartRevision`, and
  the Pod-template annotation;
- all three members `Up` with no restart advertised after recovery;
- no image-upgrade status; and
- one Normal `SHCAppFrameworkRestartScheduled` Event naming the exact bundle
  and all three restart-required members, followed by target, partition, and
  terminal rollout Events.

The 240-sample workload record spans bundle delivery, controller replacement,
authoritative restart observation, captain transfers, all three replacements,
and stable recovery. It completed with:

- 240 acknowledged HEC submissions and zero HEC request failures;
- 240 successful distributed-search requests and zero search request failures;
- exact final `count=240`, `min=1`, `max=240`, and `distinct=240`;
- at least two serving Search Head endpoints throughout each replacement; and
- zero container restarts across every workload Pod.

The evidence is
`build/_test/shc-final/shc-final-k8s-app-102.log`, with SHA-256
`63ccf3da922af9e38ba60d0b6ceca6d67fbe6e2dec5b59cf72cab08494b55e74`.
This accepts the bounded Kubernetes-owned Search Head app-restart availability
window. It does not extend that acceptance to indexer app restart, insufficient
redundancy, every network path, or every Splunk build.

## Rejected configuration path

Adding `rolling_restart=searchable` through inline `splunk.conf.server`
defaults is not a valid way to change the policy of an established SHC.
Splunk's replicated SHC configuration restored the effective value to
`restart`. In addition, Docker-Splunk's generic configuration task removes the
entire target `server.conf` before reconstructing only the supplied stanza.
In this campaign that removed the previously written `[general]
pass4SymmKey`, caused a LicenseManager signature mismatch, and consumed the
full retry budget on one replacement member.

Source `d9742547b` therefore fails closed for member-local changes to
`rolling_restart` and `decommission_search_jobs_wait_secs`. A future API must
not route replicated SHC policy through the generic member-local
`server.conf` mechanism.

## Splunk defect exposed by the supported endpoint

Splunk documents `/services/shcluster/config/config` as the endpoint behind
`splunk edit shcluster-config` and permits the command from any member. The
campaign posted `rolling_restart=searchable` and
`decommission_search_jobs_wait_secs=180` from a non-captain. The captain
accepted and replicated both settings, and the crash record marked the request
`200 OK` with `_restartRequired=N`. The originating member then aborted on an
assertion in `HttpServerTransaction::postFinalResult` while returning the
proxied result.

The crash was produced by build `844c593e9c1d` in `TcpChannelThread`; its stack
includes `shpProxyRequest::proxyReqToMemberEP`,
`SHPoolAdminHandler::proxyCliReqToCaptainOrThrow`, and
`SHPoolConfigHandler::handleEditParams`. Kubernetes immediately removed the
failed Pod from the Search Head Service, while the container itself remained
running with restart count zero until Splunk was started again.

This is a Splunk defect, not an Operator or Docker-Splunk restart request. It
must be fixed or explicitly bounded before automation can safely invoke the
configuration endpoint through an arbitrary member. Calling the endpoint on a
hard-coded ordinal is also invalid because captaincy is dynamic.

## Required product direction

The near-term safe design requires a first-class replicated SHC restart policy
contract. The controller must discover the current captain, verify that the
captain observation is current and service-ready, apply policy directly to
that captain, read it back from multiple members, and retain retryable status
without disrupting another member. Automation must not assume ordinal zero is
captain and must not call the proxy path until the Splunk assertion defect is
closed.

Source `dda3142f5`, with live corrections `4c54c1bf8` and `71beda7b5`,
implements the bounded Kubernetes-native App Framework path. On the qualified
lifecycle-enabled `RollingUpdate` path, operational
bundle work is staged and sent without allowing Splunk to begin an internal
rolling restart. The Operator derives and persists a content-addressed bundle
revision, observes Splunk's restart-required state from a clean member
baseline, and places that exact revision on the Search Head Pod template. The
existing durable one-member StatefulSet lifecycle then owns readiness
withdrawal, drain, captain transfer, Pod replacement, and recovery. Initial
SHC formation, disabled lifecycle gates, and the compatibility `OnDelete` path
retain the existing Splunk-managed behavior because they have not satisfied
the prerequisites for Kubernetes-owned replacement.

Exact EKS source `71beda7b5` completed the immutable live orchestration and
240-sample workload above. Final branch source `8aebfb0cc` contains the same
orchestration plus the separately recorded App Framework diagnostic-redaction
correction. On the native Linux AMD64 vWorkstation, exact final source passed
`make test` with all 192 enterprise/controller specs, `make build`, all chart
lints, and all 150 Helm tests. The exact EKS source also passed an isolated
Linux AMD64 `make test`. A separate emulated Linux run of intermediate
redaction source reported one non-reproducing spec failure; the authoritative
native run is the accepted gate rather than silently treating that result as a
pass.

## Remaining acceptance work

- qualify the indexer restart-required app independently;
- test controller restart during a durable App Framework lifecycle stage;
- test insufficient SHC and indexer redundancy, unhealthy captain/manager,
  failed policy application, and conflicting rollout ownership; and
- file and track the Splunk non-captain proxy assertion with the exact crash
  evidence before enabling automated arbitrary-member policy edits.
