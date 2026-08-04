# Serialize SHC Deployer and member disruptions

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

A SearchHeadCluster owns two Kubernetes StatefulSets: one Deployer and the
Search Head members. A common Pod-template change can affect both. The current
controller reconciles the Deployer first and then continues into Search Head
member lifecycle work without treating the two StatefulSets as one disruption
domain. A Deployer replacement can therefore overlap a detained, terminating,
or recovering Search Head.

SHC-106 gives the established SHC one planned-disruption slot. Durable App
Framework work, an active Search Head member lifecycle, or an already-started
Deployer update retains that slot until its Kubernetes-observed recovery is
complete. Initial cluster formation and compatibility-mode behavior remain
unchanged.

## Progress

- [x] (2026-08-03 23:51Z) Reproduced the current behavior on EKS during a real
  restart-required Search Head app rollout. Search Head ordinal 2 began
  termination at `23:51:27Z`; a competing common Pod annotation then caused
  the Deployer to begin termination at `23:51:49Z`, while the Search Head was
  still unavailable.
- [x] (2026-08-03 23:54Z) Verified that the existing Search Head queued-
  revision guard remained correct. It held the second Search Head template,
  restored the partition to three after the first target recovered, published
  the queued revision, and then started a separate controlled lifecycle. The
  second ordinal-2 stop was not an uncontrolled StatefulSet replacement.
- [x] (2026-08-04 00:04Z) Observed final recovery of the unmodified live
  system: the SHC returned to `Ready` with three members, partition three,
  equal current/update Search Head revision, a Ready replacement Deployer, and
  no container restarts.
- [x] (2026-08-04 00:55Z) Closed and hashed the complete accepted-image
  reproduction record. All 240 HEC submissions and searches succeeded, all
  240 unique events became searchable, minimum serving capacity remained two
  Search Heads and four indexers, and the ten sampled Deployer-unavailable
  intervals all overlapped the still-unavailable Search Head.
- [x] (2026-08-04 00:06Z) Implemented the bounded controller correction at
  production correction `ab342d7a5` and controller-boundary test source
  `67d2897c1` on branch
  `codex/shc-106-deployer-coordination`.
- [x] (2026-08-04 00:18Z) Passed 100 normal and 100 race repetitions of both
  the coordination decisions and real `ApplySearchHeadCluster` ownership
  boundaries, the complete enterprise package, and the complete Make test
  gate: 43 suites, 192/192 controller specs, zero failures, and 78.6 percent
  composite coverage.
- [x] (2026-08-04 00:27Z) Verified the coordination boundary through the
  persisted CR status path. The durable
  `SHC RollingUpdate DeployerUpdateActive` reason now survives the normal
  status refresh, and exact cumulative source `a6cda92a3` passed the complete
  Make test gate again.
- [x] (2026-08-04 00:07Z) Passed `make build`, generation, formatting, vet,
  Helm lint, all 150 Helm unit tests, `git diff --check`, and new-change lint
  with zero issues.
- [ ] Build an immutable Linux/AMD64 Operator image from exact source
  `a6cda92a3` and deploy it by digest to the qualification cluster.
- [ ] Repeat the real App Framework plus competing-template campaign and prove
  that the Deployer UID remains unchanged until the Search Head lifecycle is
  complete.
- [ ] Qualify the inverse ordering: begin a Deployer replacement, introduce a
  Search Head revision while it is unavailable, and prove that no Search Head
  partition is released until the Deployer Pod is Ready on the update
  revision.
- [ ] Restart the Operator during each ownership direction and prove that
  Kubernetes objects and durable SHC status, rather than process memory,
  preserve the single-disruption contract.

## Surprises & Discoveries

- Observation: one CR Pod annotation changed both the Search Head and Deployer
  templates.
  Evidence: the live update created Search Head revision `5db5595f76` and
  Deployer revision `6957c548f6` from the same desired-state change.
  Consequence: per-StatefulSet readiness is insufficient; reconciliation must
  coordinate the SHC-level disruption domain.
- Observation: the existing controller allowed both StatefulSet Pods to be
  unavailable simultaneously.
  Evidence: Kubernetes Events record Search Head 2 `Killing` at `23:51:27Z`
  and Deployer 0 `Killing` at `23:51:49Z`. The Search Head replacement did not
  become serving between those timestamps.
  Consequence: availability happened to remain intact with two serving Search
  Heads, but the controller consumed two planned recovery paths at once and
  reduced operational margin.
- Observation: the Search Head queued-revision safety boundary worked and
  must not be replaced by SHC-106.
  Evidence: monitor sample 23 observed partition three with the queued
  revision; sample 24 observed partition two only after a new lifecycle had
  withdrawn ordinal 2. The lifecycle then progressed `2 -> 1 -> 0` and
  finished with partition three.
  Consequence: SHC-106 composes with the existing revision queue. It addresses
  the Deployer/member overlap rather than redesigning Search Head revision
  supersession.
- Observation: the competing disruptions did not cause a request outage in
  this one run, but they consumed independent recovery paths concurrently.
  Evidence: the 240-sample monitor recorded zero HEC or search failures and
  exact final completeness. It recorded 46 samples with a Search Head
  disruption, ten with a Deployer disruption, and ten with both; the overlap
  ran from sample 15 at `23:51:52Z` through sample 24 at `23:54:31Z`.
  Consequence: the requirement is preventative reliability control, not a
  claim that every overlap immediately causes customer-visible data loss.
- Observation: a generic StatefulSet manager can report Ready while the
  StatefulSet controller has not yet published `observedGeneration` or
  `updateRevision`.
  Evidence: those fields are asynchronous Kubernetes status observations and
  are not gates in the generic manager's final Ready path.
  Consequence: the SHC coordinator requires one later observation that proves
  the Deployer Pod is Ready on the published update revision before releasing
  Search Head work.
- Observation: an ordinary non-error status message is cleared by the SHC
  status refresh path unless it carries the existing rolling-update prefix.
  Evidence: a controller-boundary test that fetched the CR back through the
  API found an empty coordination reason even though the in-memory reconcile
  object had set it.
  Consequence: the Deployer owner now publishes a durable
  `SHC RollingUpdate DeployerUpdateActive` status reason. A controller restart
  or support capture can therefore identify why member mutation is blocked.

## Decision Log

- Decision: use one planned-disruption owner for an established SHC.
  Rationale: App Framework, Search Head lifecycle, and Deployer replacement all
  operate on one logical SHC and should not independently consume availability
  margin.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: an already-started Deployer replacement does not yield when a
  later App Framework or Search Head operation appears.
  Rationale: abandoning a terminating, absent, stale-revision, or not-Ready
  Deployer cannot restore the old Pod. Recovery of the owner already in motion
  is the only deterministic path.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: preserve initial formation and lifecycle-disabled compatibility
  behavior.
  Rationale: the new ordering depends on the established-cluster durability
  contract and must not silently change legacy `OnDelete` behavior.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: do not claim live correction from the unmodified Operator run.
  Rationale: that run proves the defect and validates the existing Search Head
  queue behavior. Only an immutable image built from `a6cda92a3` can qualify
  the correction.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.

## Outcomes & Retrospective

SHC-106 is source-qualified and pushed, but is not complete. The accepted
Operator conclusively reproduced the Deployer/member overlap while HEC and
search remained available: 240 numbered events completed exactly, no count
regression occurred, maximum pending was one, at least two Search Head and all
four indexer endpoints remained serving, and no container restarted. The SHC
finished Ready with three registered members, captain Search Head 1, partition
three, and equal current/update Search Head revision. Native Linux image
construction and candidate EKS qualification remain blocked while the
dedicated vWorkstation Coder endpoint fails below authentication during TLS/API
connection setup.

## Context and Orientation

`ApplySearchHeadCluster` reconciles the Deployer StatefulSet before rendering
and updating the Search Head StatefulSet. The generic Deployer manager applies
its desired template and manually recycles an `OnDelete` Pod. The Search Head
manager separately owns the partitioned `RollingUpdate` lifecycle, detention,
drain, captain transfer, replacement authorization, and serving recovery.

SHC-94 already distinguishes durable App Framework work from an empty
repository poll. SHC-76 and SHC-80 already queue a later Search Head template
behind an authorized member replacement. SHC-106 adds the missing coordination
between those mechanisms and the Deployer StatefulSet.

The triggering topology is namespace `shc-final-qualification` on EKS context
`shc85-vivek-spl-301372`: three Search Heads, one Deployer, four indexers, one
Cluster Manager, and one License Manager. The accepted runtime image is
immutable digest
`sha256:49b12103f8444319dcf823eb829d2dfc020410e44d46273461c1b15e52c724fd`.
The live reproduction used the accepted Operator source `14d885390` and did
not contain SHC-106.

## Plan of Work

Build exact source `a6cda92a3` on the Linux vWorkstation and publish the
Operator image by immutable digest. Retain the existing runtime image, PVCs,
topology, and app repository so that the controller change is the only
variable.

Run two directed campaigns. In the first, update a restart-required Search
Head app, wait for durable App Framework or member-lifecycle ownership, and
apply one competing common Pod-template change. In the second, start a
Deployer template replacement and introduce a Search Head revision before the
replacement Deployer becomes Ready. For both campaigns, capture StatefulSet
generation/revisions, partition, Pod UIDs, deletion timestamps, readiness,
serving endpoints, lifecycle operation identity/stage, captain, App Framework
revisions, Events, controller decisions, HEC delivery, and exact distributed-
search results.

Repeat each conflict with one Operator restart after the owner is durable.
Reject any candidate that deletes both StatefulSet Pods concurrently, releases
a Search Head partition before Deployer convergence, changes initial-formation
behavior, loses the queued Search Head revision, or requires manual status
editing.

## Validation and Acceptance

Acceptance requires:

- exact Linux/AMD64 source and immutable Operator digest are recorded;
- all source, race, Make, build, lint, and Helm gates remain green;
- no interval contains a terminating/not-Ready Deployer and a planned
  terminating/not-serving Search Head at the same time;
- durable App Framework work and active Search Head lifecycle defer a new
  Deployer replacement;
- an already-active Deployer replacement completes before Search Head template
  publication or partition release;
- the Search Head queued-revision guard retains its partition-three barrier
  and controlled reverse-ordinal lifecycle;
- controller restart does not duplicate deletion or lose the active owner;
- at least two Search Head client endpoints remain serving throughout each
  planned member replacement;
- every Pod ends Ready on its expected revision with zero container restarts;
- numbered HEC and distributed-search workloads have zero request failures and
  exact final completeness; and
- candidate Warning Events and ERROR/FATAL logs contain no new lifecycle,
  requeue, or StatefulSet coordination failures.

## Idempotence and Recovery

The change adds no CRD field or persisted-state migration. Reconciliation
derives an already-active Deployer update from its StatefulSet generation and
update revision plus the Pod UID, deletion timestamp, revision label, and Ready
condition. A controller restart therefore resumes from Kubernetes state.

If the candidate fails, restore the accepted Operator by immutable digest.
Do not edit lifecycle or App Framework status. Let the in-progress owner
recover, then remove only the qualification annotation through the CR spec if
another normal rollout is required.

## Artifacts and Notes

- Production branch: `codex/shc-106-deployer-coordination`.
- Production correction: `ab342d7a5`.
- Persisted-status correction: `a6cda92a3`.
- Exact cumulative source: `a6cda92a3`.
- Documentation branch: `codex/shc-106-qualification-docs`.
- Triggering monitor:
  `build/_test/shc-final/shc94-real-app-conflict-20260803T2348Z.log`.
- Triggering monitor SHA-256:
  `238ff88035e37fc58d270a907e5c04f7e87142ec62b3896de6d22e6422b8c621`.
- Accepted-Operator log SHA-256:
  `ed0c727359e0368c0e30652cbf1d9991a0db0a8bca1b57795ea1b87a4db1635c`.
- Final Kubernetes evidence SHA-256 values: Events
  `191f8f13f903a54ad15d9240e4ac3d40a41eefd34c1dc42a710877a40a390ab9`,
  SearchHeadCluster
  `0a23bd89294d02aad498daffe222eaa284bef3868e37e1c5403dc17c42e6f780`,
  StatefulSets
  `12ea94714b15a94c3d8f3fb1a1e0d891a9b3c3e6139ad419066358a9eeb9cc78`,
  routing
  `797163d8af2991fbb3a87ac8f2c74637defd4fdd4639d4d7f4f8994abec19119`,
  and Operator snapshot
  `76424634a731f764c3e36d8251821f6d93d2909dd7e40390691438164e9b461b`.
- The final Event export is a retained API snapshot, not a complete
  event-history counter: Kubernetes Event TTL left six `Unhealthy` objects
  representing 45 expected startup/readiness probe attempts. Exact disruption
  timing comes from the independent monitor plus the observed `Killing`
  Events, not from treating the final Event object count as the entire run.
- Final source test log:
  `build/_test/shc-final/shc106-make-test-persisted-status.log`.
- Source gates: 43 suites, 192/192 specs, zero failures, 78.6 percent
  composite coverage, 100 normal and 100 race repetitions of both helper and
  real controller-boundary tests, 150 Helm tests, and zero new lint issues.
- Live candidate image and correction qualification: pending native Linux
  builder availability.

## Interfaces and Dependencies

SHC-106 changes no public API. It depends on established SHC lifecycle status,
durable App Framework status, StatefulSet generation/revisions, standard Pod
revision labels, deletion timestamps, and the Pod Ready condition. It composes
with SHC-94 durable App Framework ownership and the existing Search Head
queued-revision recovery contract. It does not modify Splunk Enterprise,
Docker-Splunk, Splunk Ansible, app packages, or customer lifecycle policy.
