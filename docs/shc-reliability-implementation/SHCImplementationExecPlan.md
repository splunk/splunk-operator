# Implement Reliable Search Head Cluster Lifecycle on Kubernetes

This ExecPlan is a living document. The `Progress`, `Surprises & Discoveries`,
`Decision Log`, and `Outcomes & Retrospective` sections must be updated as work
proceeds.

## Purpose / Big Picture

After this program, a planned Search Head Pod replacement will be represented
as a durable, resumable Kubernetes reconciliation workflow. The system will
remove the target from new traffic, drain or apply an explicit timeout policy
to existing work, transfer captaincy when required, authorize exactly one
StatefulSet replacement, wait for persistent-member recovery, and then advance
to the next ordinal. Operators and Support will be able to identify the current
stage and where time was spent without reconstructing the sequence from
unrelated logs.

The program applies to the current Splunk compatibility architecture. It does
not require the future distroless or service-decomposed Splunk architecture,
but it defines runtime contracts that can survive that transition.

Execution work-item identifiers and their relationship to stable test-scenario
identifiers are recorded in `SHCWorkItemIndex.md`.

## Progress

- [x] (2026-07-24) Refreshed the GitLab `sok/develop` baseline and recorded
  commit `39316c19fb990f1af84966d5269a8f4116550dbb`.
- [x] (2026-07-24) Compared the current baseline with the requirements, gap
  analysis, and known experimental branches.
- [x] (2026-07-24) Created the implementation-planning document structure.
- [x] (2026-07-24) Added parallel branch ownership, a comprehensive scenario
  matrix, and an executable qualification plan.
- [x] (2026-07-25) Prototyped and unit-qualified the Operator runtime-captain
  observation, ordinal-zero preferred-captain default, Splunk Ansible
  bootstrap/rejoin classifier, deterministic parallel formation actions, and
  dynamic reachable bundle targeting on isolated spike branches.
- [x] (2026-07-28) Integrated and published the Splunk Ansible startup work at
  `9954434703c776665713e9ed7d1a3d1d5dd1c77d`, selected it from the
  Docker-Splunk runtime branch at
  `6376b01116da5bb68ac1e4534cc60ea422bf94c7`, and built the pinned Linux
  runtime image on the vWorkstation.
- [x] (2026-07-28) Integrated and published the Operator feature branch at
  `22ab2ca0c50de8b0d727a301c3db0d39ab5b61bc`. The repository-prescribed
  `make fmt`, `make vet`, `make test`, and `make build` gates passed on the
  Linux vWorkstation. The full Go test run completed 41 Ginkgo suites with no
  failures and reported 78.4 percent composite coverage.
- [x] (2026-07-28) Established a fresh three-member EKS SHC using the pinned
  Operator and runtime images. Fresh formation, retained persistent member
  identity, and runtime configuration without repeated
  `init shcluster-config` were verified.
- [x] (2026-07-28) Completed one integrated three-member `OnDelete` happy-path
  rollout. Each ordinal was detained, drained, authorized, replaced, rejoined,
  identity-checked, and released before the next member advanced. The active
  captain was transferred before its replacement.
- [x] (2026-07-28) Migrated the stable StatefulSet from `OnDelete` to
  partition-gated `RollingUpdate` with an initial partition of three. The
  migration caused no Pod replacement and normalized the StatefulSet revision
  status.
- [x] (2026-07-28) Completed one integrated three-member `RollingUpdate`
  happy-path rollout with observed partition progression
  `3 -> 2 -> 1 -> 0 -> 3`.
- [x] (2026-07-28) Restarted the Operator while ordinal two was durably in
  `WaitingForTermination`. The new controller resumed the same operation ID,
  target ordinal, target Pod UID, desired revision, and stage, then completed
  the full rollout.
- [x] (2026-07-28) Corrected Docker-Splunk TERM handling so PID 1 exits after
  the shared shutdown helper completes. The source-level shutdown suite passed,
  the corrected Linux image was published by immutable digest, and an EKS
  termination smoke test completed in 16 seconds instead of consuming the
  configured 1,200-second grace period.
- [x] (2026-07-28) Rehearsed rollback from `RollingUpdate` to `OnDelete` during
  an active ordinal-two replacement. The Operator held partition two,
  preserved the same operation record through recovery, emitted
  `RollbackPending`, restored `OnDelete` only after ordinal two completed, and
  then completed ordinals one and zero sequentially. Captaincy transferred
  from ordinal zero to ordinal one before the final replacement.
- [x] (2026-07-28) Corrected scale-up and scale-down phase ownership, durable
  stable-replica tracking, additive-member join validation, peer-list revision
  coordination, and resumption of an Operator-owned scale-down after a
  controller image change.
- [x] (2026-07-28) Added fail-closed cancellation of a scale-down whose desired
  replica count is restored before membership removal. The cancellation
  persists recovery before releasing detention, verifies that the original
  Pod and UID still exist, and waits for both member and captain views to
  report the member `Up`.
- [x] (2026-07-28) Corrected repeated scale-down after cancellation so a
  completed historical record cannot suppress a later operation against the
  same ordinal. New lifecycle operations now have generation-scoped
  identities.
- [x] (2026-07-28) Corrected lifecycle observation and support signals. An
  intentionally removed scale-down target no longer produces an error, an
  active lower-ordinal replacement no longer drops healthy higher ordinals
  from SHC status, Pod updates no longer look like scale-up, and scale Events
  follow changes in desired replicas.
- [x] (2026-07-28) Passed final EKS scale cycles with the pinned final Operator
  image: `3 -> 4` and `4 -> 3`, including peer-list revision rollouts, dynamic
  captain transfer, one-member unavailability, persistent-volume policy, and
  exactly one final scale-completion Event per desired-count change.
- [x] (2026-07-28) Passed a 300-second post-scale stability gate over 17
  consecutive samples. Kubernetes and Splunk continuously agreed on three
  ready/serving/updated members, one ready captain, no rolling restart, no KV
  Store maintenance, no pending configuration replication, and zero container
  restarts.
- [x] (2026-07-28) Added durable cancellation and recovery for a Pod update
  that is withdrawn before replacement authorization. The original Pod UID
  must remain intact, detention is released only through observed recovery,
  and the cancelled ordinal is not marked updated or allowed to advance the
  StatefulSet partition.
- [x] (2026-07-28) Passed targeted EKS search-drain qualification with the
  final Operator image. A real-time search reached the configured timeout and
  failed closed without replacing the Pod; withdrawing the revision restored
  the same member to service. A bounded historical search delayed replacement
  until its active count reached zero, after which all three ordinals completed
  a partition-gated `RollingUpdate`, including dynamic captain transfer before
  ordinal zero.
- [x] (2026-07-28) Implemented and qualified audited continuation after a
  search-drain timeout. The Operator issues a post-timeout operation token,
  rejects wrong-token and stale-operation approvals without changing the
  StatefulSet revision, persists one exact approval with its search-count
  snapshot before later authorization, and retains all cluster, detention,
  Pod-identity, KV Store, and captain safety gates.
- [x] (2026-07-28) Passed the SHC-74 EKS campaign with Operator source
  `54a5aae3cd5f0970daee7591c24704b4111a3282` and image
  `shc-reliability-54a5aae3c`. One active real-time search timed out
  fail-closed, one exact continuation advanced only ordinal two, the complete
  rollout replaced `2 -> 1 -> 0`, captaincy moved from ordinal zero to ordinal
  one before the final replacement, and a 312-second post-action gate held all
  Kubernetes and Splunk health invariants with zero container restarts.
- [x] (2026-07-28) Implemented SHC-75 rollback and cancellation corrections at
  `eb6907ee5`, `44ccac31e`, and `3e9e735a7`. The changes distinguish valid
  ControllerRevision reuse from out-of-order Pods, retain lifecycle ownership
  while an in-place cancellation returns to Kubernetes readiness, and wait for
  the StatefulSet controller to observe a newly applied generation before
  planning another target.
- [x] (2026-07-28) Passed the complete Linux source gate for SHC-75:
  `make fmt`, `make vet`, `make build`, and `make test`. All 41 Ginkgo suites
  passed, including 154 controller envtest specifications, with 78.5 percent
  composite coverage.
- [x] (2026-07-28) Qualified LFC-007 on EKS cluster
  `vivek-spl-301372` with Operator source `3e9e735a7` and image digest
  `sha256:98b71dbbb394d51abea5e79a9f63e4423f43ae3f623d5ed3d28cb9d55c0b6f72`.
  Captain transfer timed out fail closed, the captain Pod was not replaced,
  revision withdrawal recovered the same Pod in place, rollback completed
  ordinals `2 -> 1`, and a 321-second stability gate passed.
- [x] (2026-07-28) Implemented SHC-76 post-authorization revision ownership
  and queueing at `24eea3f37`, `243f7a5d2`, and `50eb10514`. An authorized
  target keeps its original durable operation and StatefulSet revision while a
  later CR revision remains queued. The queue releases only after the
  replacement has the authorized revision and a new UID and is both
  Kubernetes Ready and SHC-serving.
- [x] (2026-07-28) Passed the complete Linux source gate for SHC-76:
  `make fmt`, `make vet`, `make build`, and `make test`. All 41 Ginkgo suites
  passed, including 154 controller envtest specifications, with 78.5 percent
  composite coverage.
- [x] (2026-07-28) Qualified STS-014 on EKS cluster
  `vivek-spl-301372` with Operator source `50eb10514` and image digest
  `sha256:62e450584a9788cd9b0f2959164bdcef2c75608c66bb468cc572e887712d7624`.
  A later revision was queued during ordinal-two replacement, received a
  separate authorization only after the first replacement was Ready and
  serving, and then completed `2 -> 1 -> 0` with dynamic captain transfer.
  The accepted run passed 127 uninterrupted searches with maximum
  unavailability one and zero restarts, followed by a 300-second, 37-sample
  stability gate.
- [x] (2026-07-28) Implemented SHC-77 on
  `codex/shc-77-image-pull-classification` at `b3ae4b291`. The Kubernetes
  adapter now distinguishes retryable pull failures from terminal invalid
  image syntax, and the recovery workflow retains the authorized ordinal under
  the replacement startup budget without making a later ordinal eligible.
- [x] (2026-07-28) Passed the complete Linux source gate for SHC-77 at
  `4710438a0`: `make fmt`, `make vet`, `make build`, and `make test`. All 41
  Ginkgo suites passed, including 154 controller envtest specifications, with
  78.5 percent composite coverage.
- [x] (2026-07-28) Qualified SHC-77 on EKS cluster
  `vivek-spl-301372` with Operator source `4710438a0`, Operator image digest
  `sha256:2d9af851e07bbf891b03ad07bec0c849f973280bb92cf03e344620ecbf6154b7`,
  and runtime digest
  `sha256:c295389a5bbcaa0aade25b0a5950952794179059564a525a7200b6f1c26b3547`.
  A missing desired tag remained retryable at ordinal two for 60 seconds,
  recovered after that exact tag was restored, and completed `2 -> 1 -> 0`
  with dynamic captain transfer. Invalid image syntax then blocked immediately
  at ordinal two. All 131 service searches passed, minimum ready endpoints
  stayed at two, maximum unavailability stayed at one, and the Deployer did not
  restart or change revision.
- [x] (2026-07-28/29 UTC) Completed the Kubernetes observation spike for the
  next bounded work item, SHC-78. On the qualification cluster's Kubernetes
  1.31 control plane, an unschedulable Pod exposed
  `PodScheduled=False/Unschedulable`. A scheduled Pod blocked on CSI
  attachment instead exposed `PodReadyToStartContainers=False`,
  `ContainerCreating` with no message, and a matching VolumeAttachment with
  `attached=false`; `FailedAttachVolume` appeared later as a Pod Event. All
  spike resources were removed, including the generated VolumeAttachment.
- [x] (2026-07-29) Closed the SHC-77 publication gap, created
  `codex/shc-78-pod-infrastructure-attribution` from the updated feature
  branch, and registered SHC-78 at `63714251f`.
- [x] (2026-07-29) Implemented SHC-78 at `7b90da269`. The recovery workflow
  now keeps scheduling, exact CSI attachment, generic Pod infrastructure,
  image pull, container startup, and Splunk rejoin as separate durable
  observations under one replacement startup budget. Storage attribution
  requires a bound target-Pod PVC, the target Pod's scheduled node, and a
  matching `VolumeAttachment` with `attached=false`; no free-form Event or
  container message is persisted or parsed.
- [x] (2026-07-29) Passed the complete Linux source gate for SHC-78:
  `make fmt`, `make vet`, `make build`, and `make test`. All 41 Ginkgo suites
  passed, including 154 controller envtest specifications, with 78.5 percent
  composite coverage.
- [x] (2026-07-29) Qualified SHC-78 scheduler attribution on EKS. With all
  workers cordoned, the replacement for ordinal two remained the sole target
  at `WaitingForScheduling/PodUnschedulable`, partition two, for six hold
  samples. Two unaffected endpoints remained Ready, every service search
  returned HTTP 200, and uncordoning recovered the complete `2 -> 1 -> 0`
  rollout with three Ready endpoints and zero container restarts.
- [x] (2026-07-29) Qualified SHC-78 exact CSI attribution on EKS. A newly
  provisioned target-Pod PVC was bound while workers were cordoned; after the
  EBS CSI controller was scaled from two replicas to zero and workers were
  uncordoned, the scheduled replacement reported
  `PodReadyToStartContainers=False` and exactly one bound-PV/node
  `VolumeAttachment` reported `attached=false`. The Operator durably reported
  `WaitingForStorage/VolumeAttachmentPending` for six hold samples, never
  advanced another ordinal, preserved two Ready endpoints and HTTP 200 search,
  and recovered the same replacement through Pod infrastructure, container
  startup, SHC registration, and ready KV Store after CSI returned to two
  replicas.
- [x] (2026-07-29) Implemented SHC-79 at
  `a59fc5103b9199b2a136601ebfbdde1d593c4cc8`. Volume comparison now works on
  deep copies normalized with Kubernetes Pod-volume defaults, so API-server
  defaulting does not create a false desired/observed StatefulSet difference
  and comparison cannot mutate caller-owned desired or observed objects.
- [x] (2026-07-29) Passed the complete Linux source gate for SHC-79:
  `make vet`, `make build`, and `make test`. All 41 Ginkgo suites passed,
  including 154 controller envtest specifications, with 78.6 percent composite
  coverage.
- [x] (2026-07-29) Qualified SHC-79 on EKS with exact Operator image digest
  `sha256:e1b77c45bba3853f96a7ac93ef5d98ac84ebde9ca991d1fbd10a847865767ede`.
  The CR omitted generic-ephemeral `volumeMode`; both returned StatefulSets
  contained the Kubernetes default `Filesystem`; their generations,
  ControllerRevisions, and workload Pod UIDs remained fixed; and zero volume
  difference was logged. After a real Operator restart, six samples retained
  a Ready SHC, four Ready Pods, three endpoints, zero restarts, and HTTP 200
  search.
- [x] (2026-07-29) Selected SHC-80 on isolated branch
  `codex/shc-80-authorized-revision-recovery` from integrated feature baseline
  `9eecde5d68e9dc889bb2b2f1913420396e00cb21`; this registration does not claim
  implementation or qualification.
- [x] (2026-07-29) Implemented and qualified SHC-80 as a durable,
  single-target withdrawal
  barrier. Automatic recovery is limited to a failed authorized target that is
  the only Pod on the failed revision while every peer remains Ready, serving,
  and on the last known-good revision. The accepted EKS run recovered an
  unschedulable authorized ordinal across an Operator restart, released a
  queued revision after recovery completion, completed ordinals `2 -> 1 -> 0`
  with dynamic captain transfer, preserved every Splunk GUID, completed 187
  searches without failure, and passed a 369-second final gate. A partially
  completed rollout or active image upgrade remains fail closed.
- [x] (2026-07-29) Selected SHC-81 on isolated branch
  `codex/shc-81-termination-safe-finalization` from integrated feature
  baseline `efbff783f02be7cee29c45c793e5cd2886dd2325`. Selection does not
  claim implementation or qualification.
- [x] (2026-07-30 UTC) Implemented and qualified SHC-81 termination-safe
  finalization at source `58437e3ad`. A direct namespace deletion of a paused,
  healthy three-member SHC created no namespace content, performed no
  per-member rollout workflow, removed all eight declared PVCs and PVs, and
  produced no namespace-termination, stale status, or storage-precondition
  error. This closes the demonstrated SHC deletion edge; it does not by itself
  claim broader production readiness.
- [x] (2026-07-30 UTC) Selected SHC-82 on isolated branch
  `codex/shc-82-appframework-restart-availability` from integrated feature
  baseline `079e26233267`. Selection does not claim implementation,
  qualification, or a chosen restart policy.
- [ ] Investigate and qualify SHC-82. Reproduce an App Framework deployment
  whose bundle requires Search Head and indexer restarts; pin the exact
  Operator, Docker-Splunk, and Splunk Enterprise sources; establish the
  effective Splunk restart mode and the meaning of `searchable` and `force`;
  and continuously prove ingest, search-result completeness, cluster
  redundancy, and single-disruption coordination before changing defaults.
  Partial EKS evidence from 2026-07-30 used a deterministic version `1.0.1`
  update and 120 numbered HEC events. HEC accepted every event and the final
  search returned `count=120`, `min=1`, `max=120`, and `distinct=120`, but 11
  Service searches failed. Nine samples had zero Search Head Service
  endpoints. The SHC rolling restart was `2 -> 1 -> 0`; the captain transferred
  `0 -> 2 -> 0`. All Kubernetes Pod UIDs and container restart counts remained
  unchanged because Splunk restarted inside each running container. The same
  package did not restart indexers: every peer reported
  `restart_required=0`, so the indexer restart-required and negative cases
  remain open.
  A follow-up `1.0.2` run used source `0fc1bcf31` and immutable Operator
  digest
  `sha256:c55ebe692659300121eef74f2e6897dbc27bdbae15bcfe40c0ae8c3566c02690`.
  It preserved at least two Search Head endpoints throughout the same internal
  restart, accepted all 120 HEC events, recovered every sequence exactly once,
  kept all Pod UIDs unchanged, and recorded zero container restarts. One
  Service search was interrupted after it was admitted on the captain selected
  for the next restart. Therefore the cluster-wide readiness correction is
  qualified, but prompt target withdrawal and active-search drain remain open.
  The indexer-side qualification is recorded in
  [SHC82AppFrameworkIndexerQualification.md](SHC82AppFrameworkIndexerQualification.md).
  On a supported four-peer RF3/SF2 topology, searchable App Framework restarts
  preserved Splunk RF/SF/searchability health but the existing
  management-oriented readiness allowed 7 of 55 HEC submissions to fail.
  Adding HEC health with the default readiness timing reduced that to 1 of 55.
  A 2-second, failure-threshold-one experiment completed 55 of 55 submissions
  and recovered all 55 sequence numbers exactly once, but peer-level
  observation still found two samples with an unavailable HEC peer advertised
  and one sample with only two remotely serving HEC peers. The fast-probe
  StatefulSet revision also exposed an Operator deadlock: intentional serving
  withdrawal reduced `readyReplicas`, and the generic update path waited
  instead of deleting the already-decommissioned target. Probe tuning is
  therefore mitigation evidence, not the final design. Configuration-aware
  serving readiness, durable owned-target progression, previous-peer
  service-recovery gating, client retry/acknowledgment, and the recorded
  negative cases remain open.
- [x] (2026-07-30 UTC) Qualified the bounded SHC-85 Operator-owned indexer
  lifecycle on EKS using official Splunk build
  `10.5.2605.0/844c593e9c1d`, runtime digest
  `sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`,
  Operator source `7ff844f4a0ad3fdd33e34443e009d08aff087124`, and
  Operator digest
  `sha256:f7e2a4f8444ffa1b335486e266e4ed9e940180f78d460639de5703a8bdb2530b`.
  A same-image replacement reused a populated MongoDB/WiredTiger volume and
  did not reproduce the prior KV Store upgrade-precheck failure. A
  Pod-template revision then completed automatically in order
  `3 -> 2 -> 1 -> 0`, with one withdrawn target, durable stages, Splunk
  decommission, replacement, and remote serving recovery before the next
  target. Every final Pod used the desired digest and revision, reached Ready
  with zero restarts, and completed Ansible with `failed=0`. Cluster Manager
  finished with RF/SF met, all data searchable, all peers Up, and no fixups.
  The workload records completed 80/80 and 30/30 exact sequences with zero
  HEC or search-request failures. This closes manual advancement for the
  tested steady-controller Operator path. At this checkpoint, controller-Pod
  restart during `Decommissioning` is qualified separately below; long
  controller or API-server disconnection, conflict, insufficient-redundancy,
  configuration/protocol, persistent-client, and Splunk-managed App Framework
  target-control gates remain open.
- [x] (2026-07-30 UTC) Qualified SHC-85 controller-Pod restart recovery on
  isolated branch `codex/shc-85-controller-restart-qualification`. The
  Operator was deleted after ordinal 3 had durably reached
  `Decommissioning`. Its replacement retained the exact operation ID, target
  Pod and UID, source and desired revisions, and decommission timestamp,
  issued no duplicate ordinal-3 decommission Event, and completed
  `3 -> 2 -> 1 -> 0` with one withdrawn target at a time. The accepted
  workload record completed 100/100 exact events with zero HEC or search
  failures; an overlapping 80-event record also reached exact completeness
  but classified its valid initial `count=0` result as one search failure; a
  stable post-roll record completed 30/30 with zero failures. Final health was
  four Ready peers, RF/SF met, all data searchable, no fixups, four
  `failed=0` Ansible recaps, zero KV Store upgrade-precheck failures, and zero
  container restarts. Controller restart is now qualified for this bounded
  stage; longer disconnection, leader contention, conflict, redundancy, and
  compatibility variants remain open.
- [x] (2026-07-31 UTC) Qualified a five-minute SHC-85 controller absence after
  indexer ordinal 3 durably reached `ReadyForReplacement`. Operator source
  `ac1fe0db8` and immutable image digest
  `sha256:59fc2afdfafc7e0c2b9f49fceebf1862128521017311776b91f0ce3315eff608`
  retained the exact operation, target UID, source revision, and desired
  revision while the Deployment had zero replicas. The held container stayed
  running and unready with zero restarts, three non-target peers stayed Ready
  and serving, and Kubernetes recorded zero liveness failures or kill Events.
  The accepted repeat observed 302 seconds with no controller. Restoring the
  Operator completed `3 -> 2 -> 1 -> 0`, with maximum
  unavailability one, remote-serving recovery before every next target, four
  `failed=0` Ansible recaps, no prior KV Store failure signature, and final
  RF/SF/all-searchable/no-fixup health. The qualification is bounded to
  `ReadyForReplacement`. At this checkpoint, API-server disconnection and
  long absence at other lifecycle stages were open; later bullets record the
  other bounded stage qualifications. The lifecycle record has SHA-256
  `655c998ab4d6072769d8efa2c47c83c737f919a730ee3a72467f9714b4df9263`.
- [x] (2026-07-31 UTC) Ran an API-independent workload Job across the accepted
  controller absence and complete four-Pod roll. It submitted 1,800 numbered
  HEC events with zero HEC request failures, zero exported-search request
  failures, zero client restarts, and final exact
  `count/min/max/distinct=1800/1/1800/1800`. The workload record has SHA-256
  `8b14b210e1224219ee1509b150036c3f599c68f11bbf22b98cbdce71bf1e3faf`.
  This closes the bounded client-request and final-convergence gate, not the
  immediate distributed-search completeness gate below. Harness source
  `d610d4474` makes subsequent Job summaries report pending events, count
  regressions, and maximum pending count directly.
- [x] (2026-07-31 UTC) Qualified a requested five-minute controller absence
  after Splunk was durably observed in indexer ordinal 3
  `Decommissioning`. Harness source `8d6a7dbc6` required
  `observedDecommissioning=true` and a persisted request timestamp before
  scaling the Operator to zero. The exact target, operation, and revision
  boundary survived 306 observed seconds; the held target remained running,
  unready, non-serving, and at zero restarts; and the other three peers
  remained unchanged and serving. Restoration resumed the same operation and
  completed `3 -> 2 -> 1 -> 0` with ten stable samples. Lifecycle evidence
  SHA-256 is
  `e457b347092503b7b4ddbec25047e3dbc1b120bc0293fb5a1cb82cd5a589bdde`.
  The overlapping independent Job completed 1,800 submissions with zero HEC
  or search-request failures, exact eventual results on every Search Head,
  and workload-log SHA-256
  `cf169d21801d25eef3314351e6b5726bb53b8ca993ac1d2297f7c8bd728d4be0`.
- [x] (2026-07-31 UTC) Qualified a five-minute controller absence at the
  first durable `TargetSelected` boundary. Harness sources `2d430748b` and
  `770a27799` use an unbuffered watch and a test-only pause to capture the
  short stage without allowing observer latency to silently test a later
  boundary. Ordinal 3, its UID, operation ID, and both revisions stayed exact
  for 300 controller-absent seconds while all four original Pods remained
  Ready, published, and at zero restarts with no lifecycle marker. Restoration
  removed the pause, resumed the same operation, and completed
  `3 -> 2 -> 1 -> 0` with ten stable samples. Lifecycle evidence SHA-256 is
  `01f3cf1fe9330b2a139a2243d2ca3f5771bfada39ee9d25bd267410d52ef9c0e`.
  The overlapping 1,800-event Job had zero HEC/search request failures and
  exact eventual results on every Search Head; workload-log SHA-256 is
  `d0d8de5eb851bea87a9057f0676e1b5d5f6e16a7ea134e0130d4af04ea6b2c3d`.
  This qualifies controller absence at `TargetSelected`, not concurrent
  desired-state conflict; API-server disconnection is qualified separately
  below.
- [x] (2026-08-01 UTC) Qualified a running SHC-85 controller losing and
  regaining its Kubernetes API path during observed ordinal-3
  `Decommissioning`. A Pod-local, exact-destination `OUTPUT` reject rule
  blocked only API Service port 443 for 401 seconds and independently proved
  HTTP 200 before and after the fault. The exact operation, target UID,
  revisions, request timestamp, and durable stage survived. The manager lost
  its leader lease and restarted once in the same Operator Pod; 36 hold
  observations covered 302 seconds with the target running, unready,
  non-serving, and at zero restarts and the other three peers unchanged and
  serving. After connectivity returned, the restarted manager resumed the
  same operation and the companion monitor passed `3 -> 2 -> 1 -> 0` plus ten
  stable samples. Lifecycle, fault, and resume SHA-256 values are
  `aca61282531551a7ec970dd2b0139be35dde2c0e1494117ed33e03ff9add5510`,
  `a324e0bc639eaba052b475f1342a7595c42be479a38914ead4678b09cfb8876a`,
  and `49dc69a31444997ddf5d5c8045bcfd840002937fd621bdbc4df700f2b1c1de7e`.
  The independent 1,800-event Job had zero HEC/search request failures and
  exact eventual results on all three Search Heads, but retained 30 count
  regressions and maximum pending 417. At that checkpoint this closed only the
  bounded K8S-006 API-disconnection gate at observed `Decommissioning`;
  stage variants, leader contention, conflict, redundancy, and repeated/long
  faults remained open. The next item closes one normal leader takeover only.
- [x] (2026-08-01 UTC) Qualified normal Operator leader failover with two
  healthy controller contenders during observed ordinal-3 `Decommissioning`.
  Harness source `ba220677b` scaled the Operator from one to two Ready,
  zero-restart Pods, proved one stable Lease holder, and force-deleted that
  exact leader. A newly created replacement was allowed to win because
  Kubernetes leader election has no follower-fairness guarantee. Lease
  transitions increased exactly `80 -> 81`; the successor logged acquisition,
  renewed stably, and retained the original operation ID, target UID,
  revisions, and decommission timestamp. The full `3 -> 2 -> 1 -> 0` roll ran
  with two Ready controllers, one stable active leader, no duplicate ordinal-3
  decommission Event, at most one unavailable indexer, zero restarts, and ten
  final stable samples. Cleanup restored one Ready controller and a renewed
  Lease; removing the active successor caused the expected `81 -> 82` cleanup
  transition. Lifecycle, leader, and workload SHA-256 values are
  `9b7193931ac6c72f02edc45265a303d4f88a8e59da6967d6e03e368f837ae6f3`,
  `c6d662265eec4e5c5683f344c3f7e39a6532f23264253320d9db113ada66a409`,
  and `e34ef36dd49a7f835028d13ebd3336fdd1090f7b7210bbc50d78f27f3ec1ed05`.
  The 1,800-event Job had zero request failures and exact final results on all
  Search Heads, while 13 count regressions and maximum pending 329 retain the
  immediate-completeness gap. This passes one normal single-active-leader
  takeover, not split brain, Lease corruption, inter-contender partition,
  repeated failover, or other lifecycle stages.
- [x] (2026-07-31 UTC) Corrected two Search Head captain-transition defects
  exposed while forming the SHC-85 fixture. Source `99da90390` sends the
  captain-only authoritative member query to the newly elected captain instead
  of the prior observation ordinal. Source `ac1fe0db8` accepts a fresh,
  converged captain-transfer observation before applying an elapsed deadline,
  while stale, conflicting, missing, or unready observations still fail
  closed. Both passed regression tests and the complete Linux Make gate. The
  qualification record explicitly retains the one test-only status repair
  needed because the pre-fix running operation had already become terminally
  `Blocked`.
- [ ] Resolve the SHC-85 immediate distributed-search completeness finding.
  The API-independent client continued to receive successful HEC and search
  responses, but some successful aggregate searches temporarily returned a
  smaller subset of previously observed events during indexer Pod IP churn.
  Every Search Head logged distributed-peer connection failures to terminating
  or newly starting peer addresses, while the export response carried no
  partial-result message; direct searches later converged exactly. Preserve
  this as a Splunk Enterprise and lifecycle-stability requirement. Determine
  the exact bucket/dispatch cause, require explicit partial-result semantics,
  and qualify per-Search-Head peer convergence before claiming immediate
  result availability. The accepted record observed 24 successful-search count
  regressions and a maximum sequence-to-count gap of 362 at sequence 1418
  (`count=1056`, `max=1417`). No production fix is claimed by the
  lifecycle-hold branch. The later observed-decommissioning campaign produced
  41 regressions and maximum pending 406 at sequence 1423
  (`count=1017`, `max=1421`). That worst sample occurred after lifecycle
  `Completed` while all replacement Pods were Ready and published, and Search
  Heads still logged requests to old indexer Pod IPs. The later
  `TargetSelected` campaign recorded 18 regressions and maximum pending 364 at
  sequence 1481 after lifecycle `Completed`; all three Search Heads again
  logged connection or authentication failures to old indexer Pod IPs. The
  later controller-leader-failover campaign recorded 13 regressions and
  maximum pending 329 at sequence 1239, again with zero request failures and
  exact eventual results.
- [x] Define and qualify SHC-83 on isolated branch
  `codex/shc-83-startup-readiness-qualification`. During initial formation,
  no Search Head may enter the client Service until every desired Search Head
  container has completed image-owned initialization. The existing live
  member, captain, and synchronization checks still apply after that
  cross-Pod barrier. Previously stable peers must remain eligible during
  scale, rollout, and recovery. Source `2889c8002` and Operator digest
  `sha256:22a4398917a3dc27bdbe68aa4513c70b2bfd4d62f05a474e55fd6f9600db7ae9`
  passed the recorded fresh-formation, non-captain recovery, active-captain
  recovery, and controller-restart EKS campaigns.
- [x] Complete SHC-84. Exact source `67c0d3bd2` and immutable Operator digest
  `sha256:d83ae44c825f13cb12117e72d2ca5415b4ffd9b7af36bcab7e81226e11e6cafe`
  passed the Linux source gate and EKS qualification for existing-v4
  reconciliation, fresh formation, forced liveness recovery, and planned
  non-captain Pod deletion. The supported
  `10.4.2604.0/60dd7967c086 -> 10.5.2605.0/844c593e9c1d` upgrade rolled
  `2 -> 1 -> 0`, retained at least two endpoints, recorded zero container
  restarts and 200/200 successful sampled searches, and completed with three
  registered `Up` target members.
- [x] Define and qualify SHC-86 so namespace-first deletion of a referenced
  LicenseManager performs no create after termination begins, removes its
  finalizer without manual intervention, and cleans its owned resources.
  Source `61b35aabf` passed 41 Linux suites and 157 specs. Immutable Operator
  digest
  `sha256:635d60fecdd203e7d158fb1f95c57d46c7062ed98b156caf8dc68da7515812ec`
  passed adversarial and real referenced-LicenseManager EKS deletion
  campaigns with no manual finalizer patch, no forbidden create, and no
  post-finalization status error. Detailed evidence is in
  `SHC86LicenseManagerNamespaceFinalizationQualification.md`. Later SHC-87
  cleanup exposed the earlier namespace-termination-to-CR-deletion propagation
  window tracked by SHC-90; the broader no-create contract remains open.
- [x] (2026-08-01 UTC) Defined, implemented, and qualified bounded SHC-87 on
  isolated branch `codex/shc-87-dependency-status`. Exact source `20d926658`
  reports absent, Pending, missing-workload, and rolling-to-desired-image
  dependencies as Pending/Progressing with stable reason
  `DependencyNotReady`, a retained specific message, a Normal Event, structured
  logs, and bounded requeue. Explicitly contradictory desired images remain a
  terminal `UpgradeBlockedVersionMismatch`. The exact source passed 41 Linux
  suites and 157 specs. Immutable Operator digest
  `sha256:fbb1a53c45da509fee47edc618eefd93923fc3864df9533dc85dbcbc8914c2a3`
  then passed an EKS absent-to-Pending-to-Ready LicenseManager campaign. The
  dependent SHC never entered Error, cleared its dependency message, and
  completed with Ready Deployer, 3/3 members, three endpoints, all members Up,
  zero restarts, and 8/8 Service searches. No arbitrary fixed timeout was
  introduced; sustained condition age is externally observable, and a future
  timeout requires an explicit configurable product policy. Detailed evidence
  is in `SHC87DependencyStatusQualification.md`.
- [x] (2026-08-01 UTC) Defined, implemented, and qualified bounded SHC-89 on
  isolated branch `codex/shc-89-paused-status`. Exact source `3e1716737`
  initializes current-generation `Pending/Paused` status for all seven active
  v4 Splunk reconcilers, including SearchHeadCluster `deployerPhase`, writes
  only on semantic change, creates no workload while paused, and returns
  without a timer. It passed 41 Linux suites and 157 specs. Operator digest
  `sha256:b83bbb97f89dca45e183e895e4be7e1d7bd11007f08babb41c4c94c97d18f145`
  passed the all-kind EKS fixture and LicenseManager/SearchHeadCluster unpause
  recovery. Detailed evidence is in `SHC89PausedStatusQualification.md`.
- [ ] Define SHC-90 so normal reconciliation stops when the namespace is
  terminating even if deletion propagation has not yet added a deletion
  timestamp to the contained custom resource. Preserve deletion-safe
  finalization and status rules. Cover LicenseManager, SearchHeadCluster, and
  every supported CR controller with a namespace-termination race test. SHC-87
  cleanup recorded six LicenseManager and nine SearchHeadCluster Reconciler
  errors before ordinary finalization completed; no fix is claimed by SHC-87.
- [ ] Define SHC-91 so CR deletion is handled before pause for Standalone,
  ClusterManager, MonitoringConsole, IndexerCluster, and IngestorCluster.
  Preserve deletion-safe finalization, perform no paused-status write once
  deletion starts, and prove every affected finalizer path. SHC-89 only
  registered this adjacent ordering gap; no fix is claimed by SHC-89.
- [x] (2026-08-01 UTC) Defined, implemented, and qualified SHC-88 on isolated
  branch `codex/shc-88-license-health`. Source `241ea3d91` reconciles the
  headless Service already named by the LicenseManager StatefulSet, waits for
  Kubernetes PodReady before calling the exact per-Pod management endpoint,
  and publishes a stable `LicenseHealthCheckFailed` Warning Event for a
  retryable REST failure without returning a terminal error or emitting a
  false expiration result. `make test` passed 41 Linux suites and 156 specs
  with zero failures and 78.6 percent composite coverage. EKS Operator digest
  `sha256:545910a6b769ad399fea42fdb31ddb79af11d38b5e5691ed3a59786a7606180e`
  created the previously missing Service without replacing the existing
  Splunk Pod, resolved the Pod FQDN, and received HTTP 200 license responses.
  A clean Operator restart retained the Service and LicenseManager Pod UIDs,
  produced three more HTTP 200 checks, emitted no new failure, and left all
  tiers Ready. An actual expired license was not installed on EKS; that Event
  path is source-qualified by unit test.
- [ ] Complete the remaining SHC-85 negative and compatibility qualification.
  The bounded Operator-owned lifecycle is source-qualified and EKS-qualified
  for steady-controller operation, one controller-Pod restart during
  `Decommissioning`, five-minute controller absence during observed
  `Decommissioning`, five-minute controller absence during
  `ReadyForReplacement`, and five-minute controller absence during
  `WithdrawingReadiness`, and five-minute controller absence during
  `TargetSelected`, plus a 401-second API-server disconnection during observed
  `Decommissioning`, and one normal two-contender leader takeover during
  observed `Decommissioning`. Remaining gates include other API-partition and
  leader-failover stages and topologies, split-brain/Lease corruption,
  repeated failover, conflicting desired-state changes,
  insufficient RF/SF health, HEC-disabled Splunk-to-Splunk traffic, HTTP and
  HTTPS HEC variants, ingress TLS termination, service-mesh and no-mesh
  deployments, persistent-client connection behavior, and repeated/soak
  campaigns. Per-Search-Head distributed-peer address/authentication
  convergence and explicit partial-result behavior are also open after the
  API-independent client observed transient incomplete successful search
  results during peer IP churn. Splunk-managed bundle-push restarts remain a
  separate boundary:
  Splunk Enterprise, not the Operator, chooses the next peer inside that
  workflow. A supported Splunk Enterprise remote-serving readiness contract
  is still required before OPS-011 or SHC-85 can be closed end to end.
- [x] (2026-07-25) Audited the local integration freeze inputs. Operator,
  Docker-Splunk, and Splunk Ansible worktrees were clean and descended from
  their recorded baselines. The publication gap found by this audit was
  cleared on 2026-07-28 by publishing immutable source commits and pinned
  Linux images used by the EKS campaign.
- [ ] Approve the capability/dependency map and assign technical owners.
- [ ] Resolve the blocking API and lifecycle policy decisions.
- [ ] Complete and approve the Operator lifecycle technical design.
- [ ] Complete and approve the runtime lifecycle contract.
- [ ] Complete and approve the qualification, observability, migration, and
  rollout plan.
- [ ] Implement and qualify Milestone 1.
- [ ] Implement and qualify Milestone 2.
- [ ] Implement and qualify Milestone 3.
- [ ] Implement and qualify Milestone 4.
- [ ] Complete release readiness, rollback rehearsal, and support enablement.

## Surprises & Discoveries

- (2026-07-30 UTC) Restarting the Operator during the persisted indexer
  `Decommissioning` stage produced about a 58-second gap before the replacement
  controller's first relevant lifecycle log. The CR retained the operation
  identity and stage throughout that gap, the one withdrawn peer remained the
  only unavailable target, and the replacement controller resumed by waiting
  for that peer rather than repeating decommission or selecting another
  ordinal. Durable CR status, rather than controller process memory, is the
  recovery boundary for this tested path.

- (2026-07-30 UTC) The replacement Operator logged one transient License
  Manager headless-Service DNS failure in the qualification namespace during
  startup and many Cluster Manager connection failures for a different
  retained test namespace. The target error did not recur, and all workload
  and Splunk health checks passed. Qualification log audits must scope by
  namespace, controller, message, and time; a process-wide ERROR count alone
  can mix unrelated fixtures and cannot establish lifecycle failure.

- (2026-07-30 UTC) A fresh workload run can receive a valid aggregate search
  result with `count=0` before the first accepted HEC event is searchable. The
  current monitor classifies that response as a search failure because
  `min/max` are absent, even though the HTTP request succeeded. Final exact
  completeness remains authoritative for delivered data, while request
  transport failure and result freshness must be reported separately.

- (2026-07-29 UTC) Omitting `volumeMode` from a generic ephemeral
  `volumeClaimTemplate` caused a render/observe loop. Kubernetes defaulted the
  StatefulSet template to `Filesystem`, while the CR remained unset, so the
  Operator repeatedly reported a Pod-volume difference and rewrote both
  StatefulSets. Explicitly setting `volumeMode: Filesystem` stopped the loop.
  Desired and observed Pod templates must be compared after normalizing
  Kubernetes API defaults; customers should not have to repeat a Kubernetes
  default to obtain a stable reconcile. SHC-79 closes the demonstrated
  volume-defaulting loop and source tests retain explicit non-default
  differences.

- (2026-07-29 UTC) The SHC API accepts the generic ephemeral
  `volumeClaimTemplate.spec` used by this qualification but its current strict
  schema rejected optional `volumeClaimTemplate.metadata.labels`. The accepted
  fixture therefore used only schema-supported fields. This is a CRD surface
  constraint, not evidence of Kubernetes defaulting drift.

- (2026-07-29 UTC) `make deploy` uses generated text replacement for both
  `WATCH_NAMESPACE` and `SPLUNK_GENERAL_TERMS`. Leaving the latter empty let
  the cleanup expression match both empty placeholders and temporarily wrote
  `WATCH_NAMESPACE_VALUE` into an unrelated field. The live deployment was
  immediately corrected with the required non-empty SGT acceptance value and
  the Make-mutated files were restored. Qualification invocations must provide
  every required non-empty replacement input and verify the live environment;
  hardening that general deployment helper is separate from SHC-79.

- (2026-07-29 UTC) The `deploy` Make target depends on `uninstall`, and
  `uninstall` deletes the generated Splunk Enterprise CRDs before `deploy`
  reapplies them. Running that target against a cluster containing live
  Splunk custom resources deleted the qualification fixture. In-place
  qualification image changes must update only the Operator Deployment and
  verify the resulting image digest. The deployment helper must be redesigned
  or explicitly guarded before it is safe for an upgrade workflow with live
  custom resources.

- (2026-07-29 UTC) `Immediate` EBS provisioning can bind a generic ephemeral
  volume in a zone that conflicts with the retained per-ordinal `etc` and
  `var` volumes. Kubernetes then correctly leaves the Pod unschedulable with
  volume node-affinity conflict. The accepted CSI qualification constrained
  each test StorageClass to the existing target ordinal's zone. Production
  design and tests must prefer topology-aware binding and must not interpret
  this scheduler result as a Splunk failure.

- (2026-07-29 UTC) Removing the newly requested Pod volume after ordinal two
  was already authorized and unschedulable did not cancel or supersede the
  active revision. The Deployer returned to the revised CR, but the Search Head
  StatefulSet remained frozen on the active desired revision and the target
  remained `WaitingForScheduling`. The existing post-authorization handoff
  protects an in-flight replacement, but it does not provide a recovery path
  when that authorized replacement cannot start and the desired template is
  withdrawn. A separate policy and implementation must safely cancel, roll
  back, or explicitly continue that operation without exposing a second
  target.

- (2026-07-29 UTC) Raising an `OrderedReady` StatefulSet partition back above
  an already-created failed ordinal does not remove that Pod. The first SHC-80
  prototype therefore reached the recovery revision in the template but
  remained blocked by the existing failed Pod. The accepted implementation
  deletes only the authorized failed target after it observes the recovery
  partition and revalidates that all non-target peers are Ready, serving, and
  on the last-known-good revision. Kubernetes then recreates that ordinal at
  the recovery revision.

- (2026-07-29 UTC) A completed recovery operation initially continued to enter
  the recovery-deletion guard. Once a queued template produced a new
  StatefulSet update revision, that guard could no longer prove the historical
  revision invariants and held the queued rollout indefinitely. Completed
  operations now leave that deletion path immediately; a regression test
  proves the historical recovery record cannot delete a Pod, rewrite its
  message, or keep the queued revision pending.

- (2026-07-29 UTC) During fresh SHC formation, the CR and Pods briefly
  reported Ready before image-owned Ansible cluster initialization, bundle
  synchronization, and the resulting internal Splunk restarts had completed.
  The lifecycle readiness gates later removed all Search Head Service
  endpoints until actual member recovery. The accepted SHC-80 action began
  only after sustained Splunk and Service validation. SHC-81 qualification
  reproduced the same sequence: a nominally Ready fixture subsequently lost
  member and endpoint availability during image-owned initialization before
  converging again. This is registered as SHC-83, a separate startup/readiness
  contract gap across the Operator, Docker-Splunk, and Splunk Enterprise.

- (2026-07-30 UTC) The SHC-80 and SHC-81 early-ready observations already
  contained the Operator's per-member SHC serving readiness gate introduced
  by `d58fc2044`. Docker-Splunk writes `starting` before invoking Ansible and
  writes `started` only after the playbook returns successfully; the mounted
  readiness probe requires that state and a serving local management endpoint.
  The missing boundary is cluster-wide initial formation: an earlier member
  can satisfy its local and current Splunk checks while another desired member
  is still inside image-owned initialization and can later initiate
  synchronization or an internal restart. SHC-83 therefore begins with an
  all-desired-container initialization barrier only for a topology that has
  never been stable. It must not withdraw healthy peers during scale or an
  established-cluster recovery.

- (2026-07-30 UTC) The first SHC-83 EKS campaign exposed a circular boundary
  at `TelemetryPending`: the client-serving gate correctly kept Pods
  Kubernetes-not-ready until formation completed, but the internal bundle
  target resolver also required Kubernetes Pod readiness. That prevented the
  work needed to complete formation. The accepted contract separates internal
  management eligibility from client traffic readiness. During the bounded
  first-formation telemetry and App Framework stages, a container-ready,
  registered, `Up`, non-deleting member observed by the captain may receive
  internal bundle work while remaining absent from the client Service.
  Established topologies retain the stricter Kubernetes-ready target rule.

- (2026-07-30 UTC) SHC-83 qualification teardown exposed a separate
  cross-resource deletion edge. Search Head workloads and storage were gone,
  but a referenced LicenseManager retained the terminating namespace while
  reconciliation attempted to recreate its Secret. The exact LicenseManager
  finalizer was cleared only after the remaining resources were verified
  absent. OPS-012/SHC-86 now tracks a no-create, self-finalizing
  LicenseManager deletion contract; SHC-83 makes no implementation claim for
  it.

- (2026-07-29 UTC) Deleting the disposable qualification namespace exposed a
  deletion-finalizer edge. The SHC finalizer attempted to recreate its Secret
  after the namespace had entered termination, which Kubernetes rejected
  because new content is forbidden. The exact test CR finalizer had to be
  cleared after its remaining resources were verified. SHC-81 closes the
  demonstrated edge: deleting CRs now route to finalization before ordinary
  validation or configuration, tolerate already-absent owned resources, make
  no create call, apply the declared PVC retention policy, and stop status
  writers after successful finalizer removal. The accepted namespace-first EKS
  run completed without manual finalizer removal.

- (2026-07-30 UTC) The first SHC-81 fixture exposed a distinct startup and
  termination-budget interaction. Supported Ansible and captain bootstrap work
  took about 7 minutes 24 seconds, longer than the approximately 6 minute
  29 second default startup-probe budget. The resulting kubelet restart used
  the Pod's configured 1200-second termination grace. That legacy runtime did
  not contain the qualified `/sbin/splunk-shutdown` contract and did not exit
  promptly. SHC-84 therefore keeps startup/upgrade duration, probe-triggered
  restart policy, grace-period sizing, and prompt TERM-to-PID-1 exit as one
  explicit qualification boundary. Increasing grace alone is not an accepted
  fix.

- (2026-07-30 UTC) SHC finalizer removal initially succeeded inside
  `ApplySearchHeadCluster`, but the outer SearchHeadCluster reconciler still
  attempted a generic stalled-condition status update using the now-deleted
  object. SHC-81 now suppresses both inner and outer post-finalization status
  writers while preserving the ordinary error-condition path when finalization
  fails. Similar outer condition-writer structure exists in other enterprise
  reconcilers; an Operator-wide audit is a separate follow-up and this SHC
  qualification does not claim their runtime behavior is defective.

- (2026-07-29 UTC) The Splunk 10.6 development runtime could not provide a
  stable same-version restart baseline for this Operator-only qualification.
  Its external KV Store process remained alive and logged successful database
  pings, while Splunkd's supported `/services/kvstore/status` response remained
  `starting` and `splunkd.log` repeatedly reported `KVStoreAdminHandler`
  errors. The Operator correctly held
  `ValidatingCluster/KVStoreNotReady`, kept all three members Up, retained a
  ready captain, and preserved HTTP 200 search. The campaign therefore made no
  Splunkd change and used a pinned Splunk 9.4.1 runtime only to isolate the
  Kubernetes scheduling and CSI behaviors. The 10.6 behavior remains a
  separate Splunk/KV Store investigation.

- (2026-07-28/29 UTC) The live EKS 1.31 API disproved the current storage
  heuristic. During a real CSI attachment wait, the target Pod was scheduled
  and its container state was `ContainerCreating`, but the waiting message was
  empty. The current adapter only sets `StoragePending` when that free-form
  message contains `volume`, `attach`, or `mount`, so it would classify the
  observed attachment delay as a generic container wait.

- (2026-07-28/29 UTC) `PodReadyToStartContainers=False` is the stable boundary
  available on the qualification cluster before image pulling and container
  startup. It proves that Pod sandbox, networking, volume setup, or dynamic
  resource preparation is incomplete, but it does not identify which one.
  The Operator must report a generic Pod-infrastructure wait unless a more
  specific structured Kubernetes object confirms storage attachment state.

- (2026-07-28/29 UTC) The generated VolumeAttachment provided the immediate,
  structured storage signal: it matched the target Pod's bound PVC volume and
  scheduled node and reported `attached=false`. Its `attachError` was empty.
  The namespaced `FailedAttachVolume` Event arrived only after the
  attach/detach controller's timeout. Events therefore remain valuable
  diagnostic evidence but are too late and too best-effort to be the sole
  lifecycle classifier.

- (2026-07-28/29 UTC) The current Operator service account can read namespaced
  core Events and PVCs but cannot read cluster-scoped VolumeAttachments.
  Exact immediate attachment attribution therefore requires a deliberate
  read-only RBAC addition. VolumeAttachment objects have no namespace and
  cannot be queried server-side by persistent-volume name, so the
  implementation must bound and document its list/filter behavior.

- (2026-07-28) The existing lifecycle adapter collapsed
  `ErrImagePull`/`ImagePullBackOff` and
  `InvalidImageName`/`ErrInvalidImage` into one immediately blocked
  observation. Kubernetes retries the former states and can recover without a
  new Pod or revision, while the latter identifies an image reference kubelet
  cannot interpret. Treating both as terminal contradicted REJ-004 and could
  convert a temporary registry interruption into an unnecessarily permanent
  lifecycle block.

- (2026-07-28) A SearchHeadCluster image-string change cannot be used as an
  image-pull fault-injection shortcut. The new image-upgrade workflow correctly
  blocks `UnknownUpgradePath` because no production authoritative
  compatibility provider is connected yet, and it deliberately refuses to
  infer compatibility from image tags. The EKS qualification must preserve
  that safety boundary rather than install a test-only production bypass.

- (2026-07-28) Kubernetes accepts an in-place update to a running Pod's
  container image. Kubelet stops the old container because its definition
  changed and then resolves the new image without requiring an extra signal.
  When the old container has already run, `containerStatuses` can alternate
  between restart `CrashLoopBackOff` and image-pull failure even while Pod
  Events consistently report pull failures. That is an artifact of mutating an
  already-started container, not the normal first-pull failure of a
  StatefulSet replacement. Final qualification therefore holds scheduling,
  injects the fault before the unscheduled authorized replacement's first
  container attempt, and then restores scheduling so only the real image-pull
  path is exercised.

- (2026-07-28) Restoring a missing tag under an out-of-band Pod image alias
  allowed the member to rejoin, but the completed Pod then had a different
  image string from the StatefulSet template. The image-upgrade safety boundary
  correctly rejected that mixed source state. Retryable qualification must use
  a dedicated image tag as the desired image from initial formation, remove
  that same tag only while the authorized replacement is unscheduled, and
  restore it to the identical digest. This creates a real pull interruption
  without leaving desired-state drift.

- (2026-07-28) A qualification override of readiness
  `failureThreshold: 12` with `periodSeconds: 10` kept members in the Service
  while their local port 8089 was refusing connections during post-formation
  splunkd restarts. Direct probes identified ordinals zero and one as
  unavailable while each Pod still reported `Ready=True`; kubelet Events
  independently recorded failed readiness and liveness probes. The Operator
  source defaults are a readiness threshold of three and period of five
  seconds. Slow startup protection belongs in the startup and liveness
  budgets; increasing the readiness failure window directly increases the time
  Kubernetes can continue routing to a locally unavailable member.

- (2026-07-28) `spec.extraEnv` changes both the Search Head and Deployer Pod
  templates. Removing a shared desired image tag during such a revision caused
  the Deployer's legacy rollout path to report terminal `ErrImagePull` before
  the SHC lifecycle adapter could own the result. The qualification revision
  must be Search-Head-only. Pinning `deployerResourceSpec` to the baseline
  resources while changing a harmless common resource request on Search Heads
  provides that isolation without adding a production test hook.

- Observation: current `develop` already detains a member and polls historical
  plus real-time search counts during recycle.
  Evidence: `pkg/splunk/enterprise/searchheadclusterpodmanager.go`.
  Consequence: the plan must harden and make this workflow durable rather than
  create a second drain implementation.

- Observation: current `develop` observes captain identity and readiness, but
  does not use that observation to transfer captaincy before replacement.
  Evidence: `updateStatus` populates captain state while `PrepareRecycle` does
  not branch on the target being captain.
  Consequence: captain observation and captain transition are separate
  capabilities and must have separate acceptance tests.

- Observation: the repository now contains a `pkg/splunk/workflow/shc`
  package boundary, but no implemented SHC workflow in that package.
  Consequence: moving lifecycle logic there is plausible, but must be decided
  against current controller refactoring work rather than assumed.

- Observation: unmerged branches contain useful work but have old merge bases
  and, in at least one case, older repository paths.
  Consequence: use them as reviewed inputs, not as an integration stack.

- Observation: the Operator supplies a compatibility environment variable
  named `SPLUNK_SEARCH_HEAD_CAPTAIN_URL` with ordinal zero, and Splunk Ansible
  historically interpreted that address as captain identity on every start.
  Evidence: `pkg/splunk/enterprise/util.go`,
  `roles/splunk_search_head/tasks/main.yml`, and
  `roles/splunk_search_head/tasks/search_head_clustering.yml`.
  Consequence: retain the address only as a bootstrap seed, disable implicit
  ordinal-zero preferred captaincy for Kubernetes SHCs, and use Splunk runtime
  APIs for every operational captain decision.

- Observation: Docker-Splunk's entrypoint uses shell fail-fast behavior. A
  fatal startup-classification task exits the container even when splunkd has
  persistent SHC state and only needs time to elect or contact a captain.
  Evidence: `splunk/common-files/entrypoint.sh` uses `set -e` before running
  `ansible-playbook`.
  Consequence: ambiguous persistent startup must run no cluster-forming
  command but leave splunkd alive; readiness and the Operator rejoin timeout
  report a failure that does not self-recover.

- Observation: `PodManagementPolicy: Parallel` provides no bootstrap ordering,
  but stable StatefulSet identity can still produce a deterministic plan.
  Evidence: startup-action contract tests exercise every three-member
  scheduling permutation.
  Consequence: exactly one stable seed may bootstrap, all other fresh members
  join with retry, and simultaneous persistent restart must select only rejoin
  or await-rejoin actions.

- Observation: both Operator-owned and image-owned bundle paths can couple
  availability to ordinal zero even though a supported request can use another
  reachable SHC member.
  Consequence: both repositories require dynamic bundle-ready member
  selection, internal splunkd TLS/port handling, and HTTP proxy bypass tests.

- Observation: Docker-Splunk does not track Splunk Ansible as a Git submodule.
  Its `ansible` Make target clones a branch into an ignored directory, skips
  the clone when that directory exists, and writes the current SHA to
  `version.txt`.
  Consequence: reproducible images require an explicit immutable
  `SPLUNK_ANSIBLE_REF`, detached checkout, dirty-tree rejection, and resolved
  SHA recording. Planning and evidence must not refer to a submodule pin.

- Observation: the checked-in `tests/ansible-lint.cfg` is incompatible with
  current ansible-lint, while repository-era ansible-lint rule 106 crashes
  because every role's `meta/main.yml` contains a null `galaxy_info`.
  Consequence: isolate and pin the legacy lint toolchain, skip rule 106 for
  this repository structure, and keep modern lint migration separate from the
  SHC behavior change.

- Observation: Docker-Splunk's enterprise-image Makefile path is not supported
  on the current macOS workstation.
  Consequence: this workstation can prepare and verify exact sources, run
  script/unit tests, lint, syntax, and produce a handoff manifest, but image
  build and container qualification must execute on Linux.

- Observation: the local freeze currently contains 64 Operator commits over
  its recorded baseline, two Docker-Splunk commits over `123ea3c`, and five
  Splunk Ansible commits over `b5fb5bc`. No fetched remote-tracking ref contains
  the three current heads.
  Consequence: preserve a generated freeze manifest outside the source tree,
  publish each intended branch to its approved remote, and verify the full
  commit SHA through the remote before dispatching a Linux build.

- Observation: after an Operator-managed `OnDelete` rollout, every Pod can run
  the new ControllerRevision while StatefulSet `currentRevision` remains the
  old revision because the StatefulSet controller did not own those deletions.
  Evidence: the test StatefulSet showed old `currentRevision` and new
  `updateRevision` after all three replacements.
  Consequence: migration must start with partition equal to replicas and
  verify that revision status converges without replacing a Pod before any new
  template change is introduced.

- Observation: a SearchHeadCluster `extraEnv` change also updates the deployer
  Pod template, so a harmless test revision marker replaced the deployer as
  well as producing a Search Head revision.
  Consequence: qualification must observe deployer stability explicitly and
  future test-only revision triggers should avoid coupling unrelated
  workloads when the API permits it.

- Observation: initial formation can be followed by a Splunk-managed cluster
  rolling restart initiated through the deployer.
  Consequence: lifecycle qualification must not begin when Pods are merely
  Running or initially Ready; it waits until the authoritative captain reports
  `service_ready_flag=1`, `rolling_restart_flag=0`, and KV Store maintenance
  disabled.

- Observation: the SearchHeadCluster and all Kubernetes readiness conditions
  can report Ready while the Splunk-managed initial rolling restart is still
  active, a member reports `Restarting`, or the local management endpoint is
  temporarily unavailable.
  Evidence: the rollback fixture first reported three ready replicas while the
  Splunk restart moved through ordinals one, two, and zero. The later
  search-drain fixtures also reported the CR Ready before Docker-Splunk and
  Splunk Ansible completed their final destructive synchronization; management
  port 8089 then cycled sequentially across the three Pods without a
  Kubernetes container restart.
  Consequence: qualification uses a continuous stability window over both
  Kubernetes, the local management endpoint, and Splunk-internal observations
  before authorizing a test revision. A single Ready sample is not an
  acceptance gate. The current runtime needs a separate startup-complete
  contract so the Operator can make this distinction without a time-based
  qualification soak.

- Observation: during a legitimate member replacement, the local
  `/services/shcluster/member/info` endpoint can return HTTP 503 while the
  member has not yet restored captain communication or minimum peer state.
  Consequence: the Operator must classify this as a bounded rejoin
  observation, keep the target unavailable, and avoid treating it as either
  proof of readiness or an immediate terminal failure.

- Observation: the `id` shown in the captain section of
  `show shcluster-status` is the shared `[shclustering]` cluster ID, while each
  member's persistent identity is the separate `guid` in `instance.cfg`.
  Consequence: qualification records and compares both values and does not use
  the captain label or ordinal as an identity substitute.

- Observation: the Docker-Splunk TERM trap could finish `splunk stop` and
  return to the entrypoint's long-running `wait`, leaving PID 1 alive until the
  entire Kubernetes termination grace period expired.
  Evidence: a deleted deployer completed the shutdown helper but remained
  Terminating with the entrypoint and log-streaming child alive.
  Consequence: the TERM handler must reset its traps, invoke the shared
  shutdown helper exactly once, and explicitly exit PID 1 with the helper
  result.

- Observation: rollback from `RollingUpdate` to `OnDelete` works as a
  convergence boundary, not as cancellation of the desired Pod revision.
  Evidence: partition stayed at two and the StatefulSet remained
  `RollingUpdate` while ordinal two recovered; only after the durable operation
  reached Completed did the StatefulSet return to `OnDelete`, after which the
  Operator manually completed ordinals one and zero.
  Consequence: rollback acceptance distinguishes stopping Kubernetes partition
  advancement from abandoning the requested revision. The current target must
  reach a known state, and later work remains one-member-at-a-time.

- Observation: Splunk-side lifecycle completion can become durable before the
  replacement Pod is Kubernetes Ready and present in Service endpoints.
  Evidence: the first STS-014 EKS run observed `Completed` while ordinal two
  was still `Ready=False` and `shc-serving=False`; releasing the queued
  StatefulSet template then started a second operation too early.
  Consequence: the Splunk recovery state machine continues to use supported
  member and captain observations, while the Kubernetes revision handoff has a
  separate final barrier requiring the expected replacement UID/revision,
  Pod Ready, and SHC serving readiness.

- Observation: one healthy sample immediately after fresh formation is not a
  sufficient qualification baseline.
  Evidence: an excluded STS-014 run saw ordinal two transiently lose
  management and Pod readiness after the first Ready observation. The
  Operator held partition three and emitted `ExistingUnavailablePod` without
  authorizing replacement.
  Consequence: destructive qualification begins only after a sustained
  combined Kubernetes and Splunk health window; fail-closed precondition
  events are classified separately from scenario failures.

- Observation: after the rollback completed under `OnDelete`, all three Pods
  carried `updateRevision` and StatefulSet `updatedReplicas` was three while
  `currentRevision` remained the old hash.
  Consequence: `currentRevision == updateRevision` is a valid
  `RollingUpdate` convergence check but is not a valid `OnDelete` completion
  check. `OnDelete` qualification uses every Pod's
  `controller-revision-hash`, `updatedReplicas`, readiness, and SHC recovery.

- Observation: the earlier successful replacement campaign generated repeated
  error-level member-info connection failures for the intentionally unavailable
  target and emitted a false `ScaledUp 2 to 3` event when the last replacement
  returned.
  Consequence: the follow-up classified expected target unavailability by
  lifecycle stage, retained real failures as errors, kept Pod-update phases
  distinct from scale phases, and tied scale events to changes in desired
  replica count.

- Observation: restoring the desired replica count while a scale-down was
  blocked before membership removal left the member in manual detention unless
  cancellation itself became a durable recovery workflow.
  Evidence: the EKS operation remained blocked after ordinal three had been
  withdrawn; restoring four replicas did not by itself return that member to
  service.
  Consequence: cancellation is allowed only while the same Pod UID still
  exists and membership removal has not been requested. Detention release and
  member recovery are observed and persisted before completion.

- Observation: after cancellation completed, a later `4 -> 3` request targeted
  the same Pod name and matched the historical completed operation identity.
  Evidence: the CR reported `ScalingDown` while the old operation remained
  `Completed` and no detention or membership action began.
  Consequence: a completed scale-down is historical, not reusable active
  state. A later request starts at `ValidatingCluster` with a
  generation-scoped operation ID and no retained side-effect timestamps.

- Observation: `StatefulSet.status.replicas` is a count, not the greatest
  existing ordinal. During replacement of ordinal one, the count temporarily
  fell even though ordinal two remained healthy.
  Evidence: truncating SHC status to that count temporarily removed ordinal
  two and emitted a false `OutOfOrderRevision` warning.
  Consequence: while a durable Pod update is active, observe every desired
  ordinal, retain higher-member status, and classify only the lifecycle target
  as expected unavailable.

- Observation: Kubernetes aggregates repeated Events with the same object,
  reason, and message by increasing `count`.
  Consequence: qualification correlates Event timestamps with the run window
  and does not interpret an aggregated count across separate scale campaigns
  as duplicate partition advancement within one campaign.

- Observation: a correct one-reconcile persistence barrier can be too brief
  for an independent polling client to observe before the next reconcile
  begins.
  Evidence: SHC-74 status recorded continuation approval at 17:04:17Z and
  replacement authorization at 17:04:27Z, but the first external Pod read
  occurred after ordinal two had already received a new UID.
  Consequence: unit and envtest prove the immediate return before side effects;
  EKS evidence proves durable timestamp ordering and later safety
  revalidation. Qualification does not require an arbitrary wall-clock pause
  between two valid reconciliations.

- Observation: three approval-only spec patches changed CR generation from two
  through five but did not change the StatefulSet update revision.
  Evidence: wrong-token, stale-operation, and exact approval patches all left
  `splunk-shc74-search-head-74fd56c498` as the update revision.
  Consequence: lifecycle approval remains controller-only input and cannot
  create a Pod-template revision or restart.

- Observation: Kubernetes may reuse an existing ControllerRevision when a
  requested Pod-template change is withdrawn. During rollback,
  `StatefulSet.status.currentRevision` can equal `updateRevision` before every
  Pod has returned to that revision.
  Evidence: LFC-007 reached the baseline ControllerRevision while ordinals one
  and two still carried the withdrawn revision.
  Consequence: rollout order and completion must be derived from every Pod's
  revision and lifecycle position. An untouched lower ordinal already matching
  the desired revision is valid while higher ordinals roll back.

- Observation: Splunk recovery can complete before Kubernetes has recomputed
  the retained Pod's Ready and serving conditions.
  Evidence: captain-timeout cancellation released detention and completed the
  Splunk recovery operation while the original captain Pod was still
  temporarily non-ready.
  Consequence: in-place cancellation retains ownership through the
  Kubernetes-readiness handoff and cannot authorize another target merely
  because the Splunk lifecycle stage reached `Completed`.

- Observation: a CR Pod-template change and the StatefulSet controller's
  `status.updateRevision` observation are asynchronous.
  Evidence: revision withdrawal briefly exposed the new desired template with
  the previous StatefulSet status revision and produced a false
  `OutOfOrderRevision` diagnostic before SHC-75.
  Consequence: an apply-pending or unobserved StatefulSet generation is a
  bounded `WaitingForRevision` state. It must not start lifecycle work, change
  the partition, delete a Pod, or emit a rollout-block warning.

- Observation: the Operator metrics listener uses delegated Kubernetes
  authentication and authorization. The Operator service account could
  authenticate but did not have non-resource `/metrics` permission.
  Consequence: qualification scraped through an authenticated port-forward
  using the EKS test identity. Production monitoring installation must include
  explicit, least-privilege metrics access rather than assume an
  unauthenticated in-container scrape.

- Observation: a customer reported that App Framework app deployment can
  trigger restarts on both Search Heads and indexers. One indexer-side record
  said `Rolling restart with searchable=0 and force=0 initiated` while its
  preflight reported `rfMet=1 sfMet=1 allSearchable=1`.
  Evidence: current Operator App Framework source invokes
  `splunk apply shcluster-bundle` for an SHC and
  `splunk apply cluster-bundle --skip-validation` for an indexer cluster. The
  Operator command does not itself select `searchable` or `searchable_force`;
  the effective rolling-restart behavior is owned below that command boundary.
  Consequence: do not infer from this line alone that multiple copies of a
  bucket were offline. SHC-82 must trace the exact Splunk Enterprise version,
  effective `server.conf`, bundle restart decision, peer order, replication
  and search-factor state, active-search behavior, and App Framework
  coordination before choosing an Operator, Docker-Splunk, Splunk
  configuration, or Splunk Enterprise change.

- (2026-07-30 UTC) The first real SHC-82 update exposed two independent
  Search Head availability gaps. Splunk performed an internal rolling restart
  in the supported `2 -> 1 -> 0` order, but the generic readiness probe used a
  five-second period and three-failure threshold. Each non-captain splunkd
  outage was short enough that Kubernetes continued to advertise the
  restarting member, and two Service searches failed while all three
  endpoints were still present. When ordinal zero transferred captaincy and
  restarted, the Operator temporarily observed `CaptainReady=false`. The
  serving-gate implementation then set `ClusterNotReady` on every healthy
  member because App Framework is not a durable Operator lifecycle operation.
  The Search Head Service had zero ready endpoints from the monitor's
  `07:22:04Z` sample through `07:23:18Z`; the independent monitor bounded the
  EndpointSlice transition from two endpoints at `07:22:02Z` to three at
  `07:23:29Z`. This contradicts the existing decision that member traffic
  readiness is local and captain health belongs in CR conditions. A fix must
  preserve fresh-formation fail-closed behavior, keep the actual restarting
  member out of traffic promptly, and retain locally healthy members after a
  previously stable cluster enters a transient captain transition.

- (2026-07-30 UTC) The restart-required fixture proved restart-required SHC
  bundle behavior but not indexer restart behavior. The Cluster Manager
  recorded `restart_required=0` for all three indexers and completed the
  bundle by reload. The fixture description and remaining qualification must
  not claim an indexer rolling restart until a configuration that Splunk
  actually classifies as restart-required is selected and observed.

- (2026-07-30 UTC) The first serving-gate correction was qualified with
  App Framework version `1.0.2`. It eliminated the zero-endpoint interval:
  the Service retained at least two endpoints throughout the captain
  transition. The only failed search began on ordinal zero at
  `08:00:11.941Z`, after Splunk logged `Requesting my own restart` at
  `08:00:08.429Z`. Splunk then terminated the streamed dispatch at
  `08:00:13.779Z` with `Local side shutting down`. The request did not fail on
  a recovered member; it was interrupted by the next internal shutdown while
  that member was still a Kubernetes endpoint. The member endpoint exposes
  `restart_state`, and the captain endpoint exposes the authoritative peer
  status used by Splunk to select a restart target. `adhoc_searchhead` is not
  a readiness flag: Splunk's `server.conf` contract defines it as the static
  role that prevents scheduled jobs on a dedicated ad-hoc member.

- (2026-08-01 UTC) SHC-88 live qualification proved two Kubernetes timing
  boundaries that the source-only fix could not show. Creating a headless
  Service and immediately resolving its per-Pod name can race DNS publication,
  so the first lookup produced `no such host` even though later reconciles in
  the same second returned HTTP 200. The stable Event reason and message were
  aggregated into one Kubernetes Event object and the reconcile remained
  retryable. A later qualification annotation was not a harmless trigger:
  parent CR annotations are copied into the StatefulSet Pod template, so the
  annotation changes intentionally produced a LicenseManager replacement.
  During that replacement the new PodReady check suppressed management REST
  calls until the EndpointSlice became ready. Consequence: use an Operator
  restart, not a LicenseManager metadata mutation, to exercise stable
  idempotent reconciliation; classify the replacement and its five aggregated
  Event occurrences as test-induced evidence rather than source-caused churn.

## Decision Log

- Decision: preserve the LicenseManager's existing per-Pod management URL and
  reconcile the headless Service already named by its StatefulSet instead of
  changing health checks to the load-balanced client Service.
  Rationale: the StatefulSet already declares that Service as its stable
  network-identity boundary, all other stateful Splunk tiers reconcile their
  named headless Service, and an exact Pod endpoint keeps readiness and
  diagnostics attributable to one instance. Query only after PodReady; a
  transport failure emits an aggregating Warning and retries without becoming
  a terminal license state. Only a successful response may produce
  `LicenseExpired`.
  Date: 2026-08-01 UTC.

- Decision: qualify controller restart by interrupting an already-persisted
  indexer `Decommissioning` operation, not by restarting before target
  ownership exists or after replacement has already been authorized.
  Rationale: this is the highest-risk boundary for duplicate decommission,
  second-target authorization, or loss of target identity. Acceptance requires
  the same operation ID, target Pod UID, source/desired revisions, and
  decommission timestamp after restart, exactly one decommission Event for
  the target, and previous-peer remote serving recovery before the next
  ordinal.
  Date: 2026-07-30 UTC.

- Decision: preserve valid empty aggregate search responses as a distinct
  freshness/harness observation rather than rewriting them as transport
  success or service outage.
  Rationale: an HTTP-successful `count=0` response proves the request reached
  Splunk but not that a just-accepted event is searchable. Final exact
  completeness and a separate stable post-roll run provide the delivery and
  service evidence without hiding the transient result.
  Date: 2026-07-30 UTC.

- Decision: an established SHC must not withdraw every Kubernetes traffic
  endpoint solely because captain observation is transiently unavailable
  during a Splunk-managed rolling restart.
  Rationale: traffic readiness is a local member property; captain health is a
  cluster condition. The observed implementation amplified a supported
  captain transfer into a zero-endpoint outage. Fresh formation must still
  fail closed, so the implementation must use durable evidence that the
  current replica topology previously reached stable Ready state rather than
  globally ignoring captain readiness.
  Date: 2026-07-30 UTC.

- Decision: target withdrawal for a Splunk-managed SHC restart must use
  Splunk's restart intent, not management-port reachability or
  `adhoc_searchhead`.
  Rationale: the five-second, three-failure container probe remained Ready
  during short internal restarts, and the captain admitted a search after it
  had already selected itself for restart. Splunk source exposes local
  `restart_state` and the captain's authoritative member status before the
  shutdown path; the Operator must map those states to the Pod serving gate
  while keeping liveness independent.
  Date: 2026-07-30 UTC.

- Decision: a qualification app is restart-required only for the topology
  whose Splunk structured status reports `restart_required=1`.
  Rationale: the first fixture restarted Search Heads but every indexer
  reported `restart_required=0`; app metadata and a test-case name are not
  runtime evidence of an indexer restart.
  Date: 2026-07-30 UTC.

- Decision: treat restart-required App Framework delivery as one
  cross-topology availability contract, not as an SHC-only rollout setting or
  a request to copy `rolling_restart=searchable_force` into every deployment.
  Rationale: App Framework can cause distinct Splunk-managed restart workflows
  on Search Heads and indexers. The supported values and their version-specific
  behavior, the meaning of the force mode, and the effect on running searches
  must be established from Splunk documentation and source and then proved
  under insufficient-redundancy and active-search fault cases. Until those
  gates pass, the safe requirement is to serialize the operation, observe the
  effective policy, and fail closed rather than force progress.
  Date: 2026-07-29 UTC.

- Decision: SHC-78 will use `PodScheduled` as the scheduling boundary and
  `PodReadyToStartContainers` as the generic Pod-infrastructure boundary.
  It will classify `WaitingForStorage` only when a matching Kubernetes
  VolumeAttachment for one of the target Pod's bound PVCs and scheduled node
  reports `attached=false`. It will not infer storage state from a container's
  free-form waiting message or depend on an Event being retained.
  Rationale: the live EKS API exposed all three structured signals and showed
  that the current message substring is absent during a real attachment wait.
  This separates Kubernetes infrastructure time from image, container, and
  Splunk time without claiming that every pre-container delay is storage.
  Date: 2026-07-28/29 UTC.

- Decision: grant only `get`, `list`, and `watch` on cluster-scoped
  `storage.k8s.io/volumeattachments` for SHC-78, correlate only the authorized
  target Pod's scheduled node and bound PVC volume names, and persist no raw
  attachment-error or Event message in lifecycle status.
  Rationale: VolumeAttachment is the immediate structured attachment source,
  while raw messages can contain provider volume handles or other
  infrastructure details. Bounded reason codes and booleans provide useful
  attribution without expanding the diagnostic data surface.
  Date: 2026-07-28/29 UTC.

- Decision: namespace-scoped Operator installations use the structured
  `PodReadyToStartContainers=False` boundary for generic infrastructure
  attribution and do not request `VolumeAttachment` access. Cluster-wide
  installations may refine that generic state to `WaitingForStorage` using
  the exact bound-PV and scheduled-node correlation.
  Rationale: Kubernetes namespace Roles cannot grant access to the
  cluster-scoped `VolumeAttachment` resource. The conservative fallback keeps
  namespace-scoped operation least-privileged and accurate without blocking a
  rollout on an unavailable cluster-scoped informer.
  Date: 2026-07-29 UTC.

- Decision: SHC-77 classifies `ErrImagePull` and `ImagePullBackOff` as
  retryable within the existing replacement Pod startup budget, while
  `InvalidImageName` and `ErrInvalidImage` remain immediately terminal.
  Rationale: kubelet already retries pull/backoff states, so the Operator must
  preserve the same authorized target and let that retry recover. Invalid
  image syntax cannot recover without desired-state correction and must remain
  fail-closed. After the startup budget expires, a retryable pull becomes
  `Blocked/ImagePullFailed`; no later ordinal is eligible in either path.
  Date: 2026-07-28.

- Decision: form the qualification SHC with a dedicated image tag that resolves
  to the pinned runtime digest. For the retryable path, temporarily cordon the
  test workers, remove that tag only after the authorized replacement exists
  unscheduled, restore scheduling, and later restore the same tag to the same
  digest. For the terminal path, patch only the unscheduled authorized
  replacement to invalid image syntax and then restore scheduling.
  Rationale: this exercises real first-pull kubelet behavior and the production
  Operator adapter while leaving the fail-closed authoritative image-upgrade
  compatibility boundary unchanged and avoiding mixed Pod/StatefulSet image
  desired state after recovery.
  Date: 2026-07-28.

- Decision: base implementation planning on the GitLab `sok/develop` branch,
  while pinning a commit for reproducible review.
  Rationale: the user identified GitLab as the integration repository, and a
  moving branch name alone is not an auditable baseline.
  Date: 2026-07-24.

- Decision: do not switch StatefulSets to `RollingUpdate` in the first
  milestone.
  Rationale: Kubernetes must not automatically replace a Search Head until
  readiness, captain handling, drain policy, runtime shutdown, rejoin
  validation, and durable orchestration are qualified together.
  Date: 2026-07-24.

- Decision: keep Pod readiness local to the Search Head member and keep captain
  health in CR conditions.
  Rationale: making all Pods unready during captain instability would remove
  otherwise usable local search capacity and could amplify an election.
  Date: 2026-07-24.

- Decision: treat ordinary replacement and permanent scale-down as different
  intents.
  Rationale: ordinary replacement preserves persistent identity and consensus
  membership; scale-down changes membership and may remove storage.
  Date: 2026-07-24.

- Decision: prove the new durable lifecycle under `OnDelete` before enabling
  partition-gated `RollingUpdate`.
  Rationale: this separates Splunk lifecycle failures from Kubernetes rollout
  ownership failures and preserves a clear rollback boundary.
  Date: 2026-07-24.

- Decision: treat the ordinal-zero address as bootstrap discovery input and
  never as an operational captain declaration.
  Rationale: Splunk captaincy is elected dynamically, while StatefulSet
  ordinal identity is static.
  Date: 2026-07-25.

- Decision: on persistent startup with inconclusive local SHC APIs, refuse
  cluster formation but do not fail the container startup play.
  Rationale: exiting every persistent Pod during a simultaneous cold restart
  can create a restart loop and prevent splunkd from recovering its existing
  consensus state.
  Date: 2026-07-25.

- Decision: finish with one Operator feature branch and one Docker-Splunk
  feature branch; the Docker-Splunk branch pins one integrated Splunk Ansible
  commit.
  Rationale: manual qualification must use an immutable, reproducible pairing
  rather than independently moving child branches or a dirty nested checkout.
  Date: 2026-07-25.

- Decision: use a separate supported Linux builder for Docker-Splunk image
  construction and runtime tests.
  Rationale: the Mac-side Makefile path is unsupported, and cross-platform
  source validation does not demonstrate Linux image behavior.
  Date: 2026-07-25.

- Decision: do not commit the concrete freeze manifest into the Operator
  branch.
  Rationale: a manifest containing the Operator HEAD would become stale in the
  commit that adds it. Generate it after the final source commit and store it
  as a handoff artifact; keep only the schema example in source control.
  Date: 2026-07-25.

- Decision: replace only the repeated `init shcluster-config` startup action
  with direct local configuration. Retain the supported live bootstrap,
  add-member, and resynchronization actions.
  Rationale: the repeated initialization was the source of the unnecessary
  restart, while bootstrap and membership changes are distributed cluster
  operations that must remain owned by Splunk.
  Date: 2026-07-28.

- Decision: write deterministic local SHC configuration before every splunkd
  start, but never write the generated `[shclustering] id`.
  Rationale: local inputs must exist before process startup, while the shared
  cluster ID is created and persisted by Splunk and must survive retained-PVC
  restart unchanged.
  Date: 2026-07-28.

- Decision: migrate a stable `OnDelete` StatefulSet to `RollingUpdate` with
  partition equal to the replica count before changing the Pod template.
  Rationale: this gives Kubernetes rollout ownership without authorizing an
  immediate replacement and provides a measurable migration gate.
  Date: 2026-07-28.

- Decision: controller-restart durability requires continuity of operation ID,
  target ordinal, target Pod UID, desired revision, and persisted stage.
  Rationale: merely completing after a restart would not prove that duplicate
  detention, captain-transfer, or replacement intent was avoided.
  Date: 2026-07-28.

- Decision: require five continuous minutes of matching Kubernetes and
  Splunk-internal health before starting a destructive qualification action and
  again after final recovery.
  Rationale: the initial cluster can appear Ready while a Splunk-managed
  rolling restart is still moving between members.
  Date: 2026-07-28.

- Decision: an in-flight rollback first completes the already-authorized
  ordinal, then restores `OnDelete`; it does not cancel the desired revision or
  delete multiple Pods.
  Rationale: completing the active recovery preserves durable operation
  semantics, while restoring `OnDelete` stops further Kubernetes partition
  advancement and returns subsequent replacements to controller ownership.
  Date: 2026-07-28.

- Decision: cancel a requested scale-down only before membership removal and
  only when the original target Pod identity is intact.
  Rationale: releasing detention after consensus membership changed, or after
  Kubernetes replaced the target, would conflate recovery with a new member
  and could restore traffic to an unverified instance.
  Date: 2026-07-28.

- Decision: cancel a requested Pod update only before replacement
  authorization and only when the original target Pod UID remains present.
  Rationale: a withdrawn or superseded revision can safely restore the
  original member only while Kubernetes has not been authorized to replace
  it. Recovery must release manual detention, verify the retained member in
  both local and captain views, and refresh active-search counts before
  completing.
  Date: 2026-07-28.

- Decision: treat a completed scale-down operation as one historical desired
  count change and assign a fresh, generation-scoped operation identity to a
  later scale-down of the same ordinal.
  Rationale: intent, Pod name, and empty desired revision are otherwise
  identical across separate `4 -> 3` requests.
  Date: 2026-07-28.

- Decision: during an active Pod replacement, derive the SHC observation range
  from desired ordinal slots rather than only
  `StatefulSet.status.replicas`.
  Rationale: a temporary decrease in the replica count must not erase a
  healthy higher ordinal or create false rollout-block alerts.
  Date: 2026-07-28.

- Decision: require a two-part, post-timeout continuation handshake containing
  both the exact operation ID and a controller-issued token.
  Rationale: Kubernetes RBAC remains the authority, while the post-timeout
  nonce prevents preapproval and stale approval reuse without introducing a
  general `continueOnTimeout` switch.
  Date: 2026-07-28.

- Decision: persist continuation approval and its active-search snapshot as a
  reconcile barrier before considering replacement authorization.
  Rationale: support evidence must distinguish customer-approved interruption
  from an automatic timeout action, and a controller restart must not lose or
  duplicate the decision.
  Date: 2026-07-28.

- Decision: continuation skips only the active-search count wait.
  Rationale: an explicit decision to interrupt remaining searches does not
  waive fresh cluster, KV Store, detention, Pod-identity, captain-readiness, or
  captain-transfer checks.
  Date: 2026-07-28.

- Decision: retain in-place Pod-update cancellation ownership until the
  original Pod is Ready, serving, registered, and `Up`.
  Rationale: Splunk detention release and member validation can finish before
  Kubernetes updates the readiness signals used for traffic and
  one-member-at-a-time admission.
  Date: 2026-07-28.

- Decision: do not run rollout planning while a StatefulSet apply is pending or
  `metadata.generation` is greater than `status.observedGeneration`.
  Rationale: the planner must compare Pods with an observed controller
  revision, not with a transient mixture of desired spec and stale status.
  Date: 2026-07-28.

- Decision: treat ControllerRevision equality as controller convergence
  evidence, not as proof that every Pod has that revision.
  Rationale: Kubernetes can reuse a prior ControllerRevision during rollback;
  per-Pod labels and reverse-ordinal lifecycle state remain authoritative for
  replacement order.
  Date: 2026-07-28.

## Outcomes & Retrospective

The first integrated positive-path milestone, active-strategy rollback, scale
lifecycle extension, fail-closed search drain, and audited timeout
continuation are complete for one pinned Operator/runtime/Splunk combination
on EKS. Fresh formation, a complete three-member `OnDelete` rollout, safe
strategy migration, complete partition-gated `RollingUpdate` rollouts,
captain replacement, persistent identity, controller restart recovery,
TERM-driven PID 1 exit, active `RollingUpdate` rollback, scale-down
cancellation, repeated scale-down, `3 -> 4`, `4 -> 3`, bounded historical
drain, real-time timeout/cancellation, and exact post-timeout continuation all
passed. The StatefulSet never advanced more than one planned Search Head at a
  time. The final continuation campaign also passed a 312-second stability
  window with every Kubernetes and Splunk health invariant continuously true.
  The SHC-75 campaign additionally proved the failed-captain-transfer path:
  one exact timeout warning, no captain deletion or replacement authorization,
  durable in-place recovery after revision withdrawal, deterministic
  reverse-ordinal rollback, no false rollout diagnostics, and 321 continuous
  seconds of final SHC, Kubernetes, management-endpoint, and KV Store health.
  SHC-77 additionally proved real kubelet `ErrImagePull` and
  `ImagePullBackOff` recovery without releasing the authorized ordinal, plus
  immediate fail-closed handling of `InvalidImageName`. The accepted campaign
  completed 131 uninterrupted service searches with at least two ready
  endpoints and no more than one unavailable Search Head. SHC-78 additionally
  proved structured unschedulable attribution and exact EBS CSI attachment
  attribution, including bounded holds, uninterrupted service through two
  unaffected members, and recovery of the same authorized replacement.
  SHC-80 additionally recovered an already-authorized unschedulable
  replacement at its last-known-good revision across a real Operator restart,
  released the superseding queued revision after recovery completion, and
  completed a captain-safe `2 -> 1 -> 0` rollout with every persistent Splunk
  GUID preserved. The complete monitor recorded 187 successful searches,
  zero failures, and a final 369-second stability gate.
  SHC-85 additionally resumed an indexer rollout after deleting the Operator
  during the persisted ordinal-3 `Decommissioning` stage. The replacement
  controller retained the exact operation and target, completed the same
  target before authorizing ordinal 2, and finished `3 -> 2 -> 1 -> 0` with
  maximum unavailability one. The principal and stable workload records
  completed 100/100 and 30/30 exact events with zero HEC or search failures;
  the overlapping record completed 80/80 exactly with one explicitly
  classified valid-empty initial search result. Final Splunk health, Pod
  revisions, endpoint publication, Ansible recaps, and KV Store log checks
  all passed.
  SHC-88 additionally closed the bounded LicenseManager health-endpoint gap.
  The exact source and image created the StatefulSet's missing headless
  Service without replacing the existing Splunk Pod, resolved the exact Pod
  name, and received valid HTTP 200 license responses. A clean Operator
  restart retained Service and workload identities, emitted no new failure,
  and left the LicenseManager, Search Head Cluster, indexer cluster, and
  Cluster Manager Ready. The intentionally expired-license path remains
  source-qualified rather than EKS-qualified.

This is not production-readiness evidence. Forced deletion and node loss,
additional storage providers and scheduling causes, network and TLS variants,
version skew, other authorized-revision failure and partial-rollout variants,
rollback under other injected failures, repeated runs, soak testing, and
support/alert qualification remain open. The lifecycle-aware log, phase,
scale-event, transient higher-ordinal observation, cancellation, continuation,
failed-transfer, revision-reuse, StatefulSet-observation, image-pull, and
infrastructure-attribution defects exposed by the campaigns are corrected and
passed their targeted cycles. The current result proves the integrated
architecture can execute its intended happy path, resume durable state, cross
the strategy rollback boundary, safely coordinate replica-count changes,
fail closed on active work or failed captain transfer, and continue only
through one audited exception; it does not yet justify default enablement.

## Context and Orientation

The principal current code paths are:

- `api/enterprise/v4/common_types.go` and
  `api/enterprise/v4/searchheadcluster_types.go` for customer API and status;
- `pkg/splunk/enterprise/configuration.go` for the StatefulSet and Pod template;
- `pkg/splunk/splkcontroller/statefulset.go` for current scale and recycle
  sequencing;
- `pkg/splunk/enterprise/searchheadclusterpodmanager.go` for SHC observation,
  detention, drain, membership removal, and recycle completion;
- `pkg/splunk/client/splunk/splunkclient.go` for Splunk management APIs;
- `tools/k8_probes/` for probe behavior;
- `pkg/splunk/client/metrics/metrics.go` and event/logging helpers for current
  observability; and
- `pkg/splunk/workflow/shc/` as a possible destination for stateful,
  CR-agnostic SHC workflows.

The external integration boundaries are:

- Docker-Splunk entrypoint TERM handling;
- Splunk Ansible Search Head bootstrap and join tasks;
- splunkd SHC member readiness, detention, captain, membership, and shutdown
  APIs;
- Kubernetes StatefulSet, Pod lifecycle, Service/EndpointSlice, Eviction, PDB,
  scheduler, and storage behavior; and
- Helm/CRD documentation and upgrade compatibility.

The product requirements remain in
`docs/SearchHeadClusterKubernetesStabilizationRequirements.md`. This plan must
not silently weaken those requirements. Where implementation evidence changes
a factual statement, update the baseline and requirements together through
review.

## Plan of Work

### Workstream A: API and policy contracts

Define customer-visible and internal policy independently:

- termination grace period, defaulting, validation, and migration;
- search-drain timeout and timeout action;
- captain-transfer timeout;
- member-rejoin timeout;
- rollout enablement and compatibility/feature gating;
- safe override or continuation semantics;
- configuration-change classification; and
- status and condition additions within the current v4 API.

The technical design must show example CRs, omitted-field behavior, explicit
zero/invalid behavior, generated CRD changes, Helm mapping, upgrade behavior,
and rollback behavior. Duration fields must not be collapsed into one generic
timeout.

### Workstream B: durable controller lifecycle

Define an idempotent state machine in which each reconcile performs one bounded
observation or action and persists enough state to resume after Operator
restart. At minimum, model validation, detention, search drain, captain
transfer, replacement authorization, termination, scheduling/storage,
container startup, member rejoin, recovery validation, completion, blocked,
and failed states.

The design must specify:

- operation identity and how a new spec change interacts with an active
  operation;
- source of truth for target ordinal and desired revision;
- observed-state freshness and conflicting captain observations;
- retry class, timeout start, timeout action, and manual continuation;
- exactly-once intent for Splunk control APIs using idempotent reconciliation;
- conditions, Events, structured logs, and metrics emitted at transitions;
- behavior after Operator leader failover or restart;
- coordination with App Framework and Splunk-initiated rolling restart; and
- explicit separation of recycle, scale-down, deletion, and recovery.

### Workstream C: local Pod and runtime lifecycle

Define the contract rather than placing distributed-cluster orchestration in a
hook:

- Search Head readiness calls the supported local member-readiness endpoint;
- liveness checks only local irrecoverable process health;
- startup allows local splunkd initialization but does not claim full rejoin;
- `preStop` makes local traffic withdrawal and shutdown intent observable and
  invokes one bounded, idempotent stop path;
- the TERM trap and `preStop` share ownership/locking and an explicit stopping
  state;
- forced deletion, crash, OOM, and node loss are recovery cases where
  `preStop` may not run; and
- persisted restart chooses rejoin rather than cluster formation.

For simultaneous persistent restart, inconclusive member/captain APIs must not
cause startup automation to exit the container. It leaves splunkd alive,
performs no cluster-forming command, and relies on local readiness plus the
Operator's bounded rejoin gate to expose recovery.

This workstream must name which repository owns each action and define
versioned compatibility when Operator, image, and Splunk Enterprise versions
do not upgrade simultaneously.

### Workstream D: Kubernetes-native replacement

Only after Workstreams A through C satisfy their qualification gates, introduce
`RollingUpdate` with Operator-controlled partition. Specify:

- initial partition and migration from an existing `OnDelete` StatefulSet;
- which controller owns partition advancement;
- reverse-ordinal sequencing;
- how the target is prepared before lowering the partition;
- how the desired and current revisions are observed;
- advancement only after the replacement passes member recovery;
- pause, abort, retry, and one-time continuation;
- interaction with `Parallel` Pod management;
- PDB and Eviction behavior for voluntary disruptions; and
- behavior if an administrator manually deletes a Pod.

The design must prove that no more than one planned member is unavailable and
that the StatefulSet controller cannot outrun the Splunk lifecycle gates.

### Workstream E: observability and supportability

Create a bounded reason-code taxonomy and operation-stage contract shared by
status, Events, logs, metrics, alerts, and diagnostic collection. Measure:

- detention and drain duration;
- captain transfer and captain-unavailable duration;
- termination and forced termination;
- scheduling, volume attachment, container start, and local startup;
- SHC registration/synchronization and total rejoin;
- blocked and retry duration; and
- complete rollout outcome.

Avoid unbounded metric labels such as operation IDs, Pod UIDs, arbitrary error
text, or customer values. Operation IDs belong in status and logs.

### Workstream F: qualification, migration, and delivery

Build unit, envtest, integration, and disruption suites before enabling
`RollingUpdate` by default. Test at least:

- captain and non-captain replacement;
- ordinal-zero replacement when it is not captain;
- active historical and real-time searches;
- every timeout and continuation policy;
- Operator restart at every durable stage;
- forced deletion and node loss;
- scheduler and volume delays/failures;
- stale/conflicting captain observations;
- member rejoin and consensus catch-up failure;
- scale-up, permanent scale-down, deletion, and storage retention;
- App Framework and deployer coordination;
- supported Kubernetes distributions, TLS, service mesh, and air gap;
- version skew between Operator, image, and Splunk Enterprise;
- every scheduling order for `Parallel` first formation, simultaneous
  persistent cold restart, and interrupted first-time formation;
- ordinal-zero unavailability during image-owned and Operator-owned bundle
  operations; and
- an Ansible startup failure check proving no persistent member is killed only
  because captain/member APIs are temporarily inconclusive.

Migration must include an opt-in phase, observed rollout canary, rollback to
`OnDelete` without abandoning an in-flight operation, and support guidance for
collecting evidence before manual intervention.

## Milestones

### Milestone 0: approve contracts and establish test harness

Deliver approved technical designs, baseline fault scenarios, reason codes,
fake Splunk API behavior, and an integration environment capable of observing
Pod revision, partition, readiness, captain, member state, and lifecycle
timestamps.

Acceptance: reviewers can trace every requirement to an owner, design section,
test, and release gate. No production rollout behavior changes in this
milestone.

### Milestone 1: health, timing, and diagnostic foundations under `OnDelete`

Deliver SHC member readiness, conservative liveness/startup separation,
configurable termination grace, separate timeout policy fields, durable
operation/stage status, normalized Events/logs/metrics, and diagnostic
collection. Keep the existing `OnDelete` replacement mechanism.

Acceptance: detention removes a member from normal traffic; captain instability
does not make every healthy member unready; omitted and explicit grace settings
behave as documented; every wait reports stage, elapsed time, and timeout; and
the Operator can restart without losing the recorded operation.

### Milestone 2: captain-safe and runtime-safe replacement under `OnDelete`

Deliver planned captain transfer and verification, bounded drain behavior,
single-owner local shutdown, explicit stopping state, persistent rejoin intent,
dynamic healthy-member targeting, and stronger rejoin validation. Continue to
use `OnDelete` so the new lifecycle can be qualified without changing the
Kubernetes rollout owner simultaneously.

Acceptance: captain and non-captain replacements complete through distinct
verified paths; failed transfer blocks deletion; forced termination is
observable; persistent restart does not repeat initial cluster formation; and
no ordinary recycle removes consensus membership. A simultaneous persistent
cold restart leaves splunkd alive on every member, runs no cluster-forming
command, and either recovers one authoritative captain or reaches a classified
Operator rejoin timeout.

### Milestone 3: opt-in partition-gated `RollingUpdate`

Deliver a feature-gated migration to StatefulSet `RollingUpdate` with partition
control. Reuse the Milestone 2 lifecycle state machine; replace direct planned
Pod deletion with partition advancement after preparation.

Acceptance: a complete multi-Pod image rollout advances one ordinal at a time,
survives Operator restart at every stage, never has more than one planned
member unavailable, and will not advance while captain, drain, termination, or
rejoin gates are blocked.

### Milestone 4: default enablement and operational readiness

Complete the deployment matrix, long-running and failure-injection testing,
dashboards, alerts, runbooks, migration documentation, rollback rehearsal, and
support training. Decide whether evidence supports default enablement and for
which version combinations.

Acceptance: release approval records qualified defaults and exclusions,
measured duration distributions, alert thresholds, upgrade/rollback results,
known limitations, and ownership for unresolved splunkd constraints.

## Concrete Steps

All commands are run from `/Users/viveredd/Projects/splunk-operator`.

Refresh and record the baseline:

    git fetch sok develop
    git rev-parse sok/develop
    git log -1 --format='%H %ad %s' --date=iso-strict sok/develop

Create an implementation branch only after the milestone design is approved:

    git switch --create codex/shc-reliability-m1 sok/develop

Before editing APIs, identify generated artifacts and current tests:

    rg -n "type CommonSplunkSpec|type SearchHeadClusterStatus" api/enterprise
    rg -n "UpdateStrategy|TerminationGracePeriodSeconds|Lifecycle" pkg/splunk
    rg -n "PrepareRecycle|FinishRecycle|PrepareScaleDown" pkg/splunk
    rg -n "readinessProbe|livenessProbe|startupProbe" tools pkg helm-chart

For API work, run the repository-prescribed generation and validation:

    make manifests
    make generate
    make fmt
    make vet
    make test
    make build

Add targeted unit and integration commands to this section when each technical
design names its packages and test suites. Record expected output and actual
result in `Artifacts and Notes`.

For the Splunk Ansible startup contract, run from the clean integrated
Splunk Ansible worktree:

    python3 -m unittest tests.small.test_shc_lifecycle -v
    python3 -m unittest tests.small.test_shc_ready -v
    ansible-playbook --syntax-check site.yml
    python3.11 -m venv <lint-venv>
    <lint-venv>/bin/pip install -r tests/requirements-shc-lint.txt
    ansible-lint -c tests/ansible-lint.cfg \
      roles/splunk_search_head/tasks \
      roles/splunk_deployer/tasks

The startup tests must show one bootstrap action and two join actions for every
three-member scheduling permutation, only rejoin/await-rejoin actions for
persistent cold restart, and dynamic bundle selection when ordinal zero is
unavailable.

Before real integration testing, merge all qualified Operator child work into
`feature/shc-k8s-reliability-spike`. Merge the runtime shutdown and integrated
Splunk Ansible commit into one Docker-Splunk
`feature/shc-k8s-reliability-spike` branch. From macOS, produce a handoff
manifest with pushed commits, Splunk build, Linux architecture, image target,
and build arguments by copying
`docs/shc-reliability-implementation/RuntimeLinuxBuildHandoffManifest.example.yaml`.
On the supported Linux builder, verify those inputs, build immutable images,
record both image digests plus the resolved Splunk Ansible source commit and
builder provenance, and use only that pinned pair for the manual scenario
matrix.

## Validation and Acceptance

Each milestone requires:

1. traceability from requirement to implementation and automated test;
2. unit tests for state transitions, idempotency, timeout, stale observation,
   and error classification;
3. controller/envtest coverage for status, conditions, Events, and restart
   recovery;
4. integration evidence with real StatefulSet revisions and Splunk management
   APIs;
5. disruption evidence for node, network, process, storage, and forced-delete
   cases in scope;
6. version-skew and upgrade/rollback evidence;
7. metric-label and diagnostic-redaction review;
8. Product Security review for credentials and lifecycle control paths; and
9. documentation and support-runbook review.

“Pod became Running” is not sufficient acceptance. The replacement must be the
desired revision, locally ready, registered with the expected persistent
identity, synchronized to the agreed product signal, released from detention,
and observed while the cluster has an authoritative ready captain.

## Idempotence and Recovery

All controller stages must be safe to repeat. The controller must observe
before acting, persist transitions before beginning the next destructive step,
and use stable operation intent across retries. A controller restart must not
start a second drain, captain transfer, or replacement.

If an action times out, preserve the target, stage, reason, observations, and
timestamps. Default behavior is to block before destructive continuation unless
the approved policy explicitly permits continuation. Manual Pod deletion is not
an implicit approval to skip lifecycle safety.

Rollback from opt-in `RollingUpdate` must first stop partition advancement,
preserve the active operation record, and reconcile the current ordinal to a
known state before restoring `OnDelete`. Never roll back by deleting all Pods
or removing persistent membership.

## Artifacts and Notes

Store bounded evidence under a milestone-specific test-artifact location
defined by the qualification design. Do not commit credentials, customer
search text, Secret data, private keys, or raw support bundles.

Record:

- baseline and image versions;
- rendered CRD and StatefulSet;
- operation status transitions;
- Kubernetes Events;
- sanitized structured logs;
- metric snapshots;
- Splunk captain/member summaries;
- Pod revision and partition history;
- fault injected and recovery result; and
- measured stage durations.

Local runtime-integration evidence from 2026-07-25:

- Splunk Ansible integration commit:
  `5d6006c11d634db9226e3b655a159b9177e4d26a`;
- 13 bootstrap/rejoin, deterministic-formation, and dynamic-target contract
  tests passed;
- `site.yml` syntax passed with current Ansible and repository-era Ansible
  5.10/ansible-core 2.12;
- directory-level lint passed with Python 3.11, Ansible 5.10,
  ansible-lint 4.3.7, and Rich 9.13 from
  `tests/requirements-shc-lint.txt`;
- Docker-Splunk commit `90b11f5` passed nine source-selection and shutdown
  tests; and
- a clean Docker-Splunk source-preparation run checked out the exact integrated
  SHA in detached state and recorded the same value in `version.txt`.

Current ansible-lint is not a substitute for this gate: it rejects the
repository's legacy configuration and reports broad baseline modernization
work. The spike therefore uses the isolated pinned toolchain; migrating the
whole repository to current Ansible lint rules is separate follow-up work.
No runtime image-build evidence was produced on this Mac. The next artifact
must come from the supported Linux builder and include its operating system,
architecture, container-engine version, full build log, image digest, and
resolved source commits.

The 2026-07-25 local freeze audit found clean worktrees at Operator
`58f96c1f922e05efd06d56854bad4152bccab725`, Docker-Splunk
`90b11f56ef36d75982d2fab7a9f34abd92e0e128`, and Splunk Ansible
`5d6006c11d634db9226e3b655a159b9177e4d26a`. These values are audit inputs,
not a Linux-build authorization: no fetched remote-tracking ref contained the
heads at audit time.

Integrated EKS evidence captured on 2026-07-28:

- Operator source:
  `22ab2ca0c50de8b0d727a301c3db0d39ab5b61bc`;
- Docker-Splunk source:
  `6376b01116da5bb68ac1e4534cc60ea422bf94c7`;
- Splunk Ansible source:
  `9954434703c776665713e9ed7d1a3d1d5dd1c77d`;
- Operator image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-reliability-22ab2ca0c`;
- runtime image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk:shc-prestart-6376b01-ansible-9954434-splunk-10.6.0.0-d9be152689b7`;
- runtime image digest:
  `sha256:f2c8bc7aefd5d060ec396f2cbdd49d28dcdf04ce3d91ebeffc42caf069bbf955`;
- feature gates:
  `SplunkPodLifecycle=true,SearchHeadClusterLifecycle=true`;
- shared `[shclustering]` ID:
  `0E720A3E-610C-4FFE-8765-3188DA79045E`;
- persistent member GUIDs by ordinal:
  `74FEAA89-32D8-4A7E-B29B-15355A4A5D82`,
  `CECD7C09-03D7-42B2-A88F-BB10142F783B`, and
  `DFA6576A-540E-43E0-BCFB-E69157648CA9`;
- final StatefulSet revision:
  `splunk-shc-lifecycle-search-head-75456fb44f`, with current and update
  revisions equal, partition three, and three ready/updated replicas;
- controller restart changed the Operator Pod UID from
  `dbf66ce1-b9b8-4138-8ef0-bc9c6de36bd7` to
  `36882c35-9993-4ba0-a872-fb227afe5b40` while preserving the active ordinal
  two operation;
- final captain was ordinal one, all members were `Up`,
  `service_ready_flag=1`, `rolling_restart_flag=0`, and KV Store maintenance
  was disabled;
- every final Search Head Pod had zero restarts and its startup log contained
  zero repeated SHC initialization tasks, zero restart-handler executions, and
  zero fatal Ansible results; and
- the completed namespace and its retained test PVCs were deleted after
  evidence collection.

SHC scale-lifecycle extension captured on 2026-07-28:

- final Operator source:
  `ccab4fe332e8dfc4a3b14a8ead60d5fe46f323cd` on the SHC
  lifecycle-observability branch;
- relevant incremental Operator commits:
  `255759009`, `e7b696f5e`, `b4d2af703`, `7e97936df`,
  `6ebe009ad`, and `ccab4fe33`;
- final Operator image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-reliability-ccab4fe33`;
- final Operator image digest:
  `sha256:b79ae3f5d81ac1fcc48f998aad08ecda9c6d63fc68f30f745d4aa8c53c8ce96c`;
- runtime source:
  Docker-Splunk `7951d69f82b28d92b118432bea4a513a90a76749`
  with Splunk Ansible
  `9954434703c776665713e9ed7d1a3d1d5dd1c77d`;
- runtime image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk:shc-prestart-7951d69-ansible-9954434-splunk-10.6.0.0-d9be152689b7`;
- runtime digest:
  `sha256:c295389a5bbcaa0aade25b0a5950952794179059564a525a7200b6f1c26b3547`;
- restoring four replicas before membership removal emitted one
  `SHCScaleDownCancelled` Event, retained the original ordinal-three Pod UID,
  released detention only through durable recovery, and completed with all
  four members `Up`;
- a fresh repeated `4 -> 3` exposed and then verified the correction for a
  completed historical scale-down record;
- the final pinned image passed `3 -> 4` and `4 -> 3`. Each peer-list revision
  progressed in reverse ordinal order, captain targets were transferred before
  replacement, and no sample observed more than one withdrawn or deleting
  member;
- final run Events contained the expected rollout-target, partition-advance,
  rollout-complete, and one scale-complete Event. No false
  `OutOfOrderRevision`, false scale direction, or expected target
  member-observation error was emitted;
- the final StatefulSet converged on
  `splunk-shc-rollback-search-head-6bbc69584b`, with current and update
  revisions equal, partition three, and three ready/updated replicas;
- retained PVCs existed only for ordinals zero through two; ordinal three's
  `etc` and `var` PVCs were removed by the configured scale-down policy; and
- a 300-second stability gate passed 17 consecutive samples with three ready
  Kubernetes Pods, three serving readiness gates, three registered `Up`
  members, a service-ready captain, complete configuration replication,
  disabled KV Store maintenance, no Splunk rolling restart, and zero container
  restarts.

SHC search-drain and Pod-update-cancellation extension captured on 2026-07-28:

- Operator image source:
  `5783e5b695d3912e6b0a82017947d432e87f7d10`, following the durable
  cancellation change at
  `23bdb631b423b38ec4ad835b1436947eb52cae26`;
- Operator image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-reliability-5783e5b69`;
- Operator image digest:
  `sha256:986fc45f85ad073d6ac377a8c0b2becc1ebba6aad9620dc17017220dc3f574bf`;
- the Linux repository gates `make fmt`, `make vet`, `make build`, and
  `make test` passed. The test run completed all 41 Ginkgo suites, including
  154 controller envtest cases, with no failures and 78.5 percent composite
  coverage;
- the real-time scenario observed one active real-time search in both the
  durable operation and target-member status. The 30-second drain timeout
  reached `Blocked/SearchDrainTimedOut`; the original Pod UID and revision
  remained unchanged, partition remained three, the Pod readiness gate and
  EndpointSlice became non-serving, and the search remained running;
- the real-time timeout emitted exactly one `SHCRolloutBlocked` Event.
  Cancelling the search and withdrawing the requested revision recovered the
  same Pod in place, returned all search counts to zero, restored readiness and
  serving, and emitted exactly one `SHCPodUpdateCancelled` Event;
- the historical scenario used a bounded Splunk QA search command and observed
  the original Pod and partition remain unchanged while the historical count
  was active. Replacement was authorized only after that count reached zero;
- the historical rollout completed ordinals `2 -> 1 -> 0`, never advanced
  before the current target recovered, transferred captaincy from ordinal zero
  to ordinal one before replacing ordinal zero, and ended with partition
  three, three ready/updated replicas, matching current/update revisions, and
  all members `Up` with zero active searches;
- both fresh fixtures exposed a startup-readiness gap: the CR and Kubernetes
  readiness could report Ready before final Docker-Splunk/Splunk Ansible
  synchronization briefly cycled local management endpoints. The targeted
  tests therefore required 120 continuous seconds of three-member management
  reachability and zero container restarts before mutation. This proves the
  scenarios but does not replace the five-minute pre- and post-action gate for
  the complete release campaign; and
- both namespaces, PVCs, and associated PVs were deleted after sanitized
  evidence collection.

SHC audited drain-continuation extension captured on 2026-07-28:

- Operator image source:
  `54a5aae3cd5f0970daee7591c24704b4111a3282`;
- Operator image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-reliability-54a5aae3c`;
- Operator image digest:
  `sha256:f54427c0497edb09ba42f584641bb323a2f81b5874460f5ef04e2ac92d00bbcf`;
- `make fmt`, `make vet`, `make build`, and `make test` passed on the Linux
  vWorkstation. The final test run passed all 41 Ginkgo suites, including
  154 controller envtest cases, with 78.5 percent composite coverage;
- a fresh fixture passed a five-minute pre-action stability gate only after
  the initial image-owned synchronization cycle completed;
- one active real-time search on ordinal two reached the 30-second drain
  timeout. The operation became `Blocked/SearchDrainTimedOut`, issued a
  64-character operation token, retained the original Pod UID and revision,
  held partition three, withdrew Pod and EndpointSlice serving readiness, and
  emitted one `SHCRolloutBlocked` Event;
- a matching operation with a wrong token and the issued token with a stale
  operation ID both remained blocked. Neither changed the Pod, partition,
  StatefulSet update revision, approval Event count, nor approval metric;
- the exact operation ID and token recorded approval generation five and a
  snapshot of zero historical and one real-time search. Approval time was
  17:04:17Z and replacement authorization time was 17:04:27Z. The run emitted
  one `SHCSearchDrainContinuationApproved` Event, one bounded structured log,
  and changed the unlabelled approval counter from zero to one;
- the later safety decision replaced ordinals `2 -> 1 -> 0`, never observed
  more than one unavailable Search Head, and recorded three target-start and
  three partition-advance Events. Captaincy moved from ordinal zero to ordinal
  one before ordinal-zero replacement authorization;
- final state had matching StatefulSet revisions, partition three, three
  ready, serving, registered `Up` members, no active searches, and zero
  container restarts. A 312-second post-action gate continuously confirmed
  initialized and service-ready SHC state, zero pending configuration
  replication, and local management reachability; and
- the qualification namespace, all PVCs, and all eight associated PVs were
  removed after evidence collection.

SHC-75 captain-transfer-timeout and revision-withdrawal qualification captured
on 2026-07-28:

- Operator commits:
  `eb6907ee51f0655742f2096f8137b55c484792d6`,
  `44ccac31e9aaa0540678d090b3222a5e2a1df1ef`, and
  `3e9e735a776eb90957a0d0d2722b28ce0da5baff`;
- final Operator image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-reliability-3e9e735a7`;
- final Operator image digest:
  `sha256:98b71dbbb394d51abea5e79a9f63e4423f43ae3f623d5ed3d28cb9d55c0b6f72`;
- the Linux vWorkstation passed `make fmt`, `make vet`, `make build`, and
  `make test`; all 41 Ginkgo suites and 154 controller specifications passed
  with 78.5 percent composite coverage;
- the test ran on EKS cluster `vivek-spl-301372`, namespace
  `shc75-captain-timeout`, with
  `SplunkPodLifecycle=true,SearchHeadClusterLifecycle=true`;
- the captain-transfer policy was changed from 300 seconds to one second as a
  controller-only policy edit. CR observation completed without changing any
  Search Head Pod UID or StatefulSet revision;
- the forward rollout replaced ordinals `2 -> 1`. The ordinal-zero captain
  then reached `Blocked/CaptainTransferTimedOut`; partition remained one,
  `replacementAuthorizedAt` remained unset, the original captain UID
  `25230824-f9d8-40f7-8c46-1d4680ccc8b0` remained present and non-deleting,
  and ordinals one and two remained Ready and serving;
- the blocked state was held for 30 seconds and emitted exactly one additional
  `SHCRolloutBlocked` warning for `CaptainTransferTimedOut`;
- withdrawing the requested revision emitted exactly one additional
  `SHCPodUpdateCancelled` Event, released detention through observed recovery,
  restored the original captain Pod in place, and did not begin another target
  before that Pod was Ready and serving;
- rollback reused the baseline ControllerRevision and replaced ordinals
  `2 -> 1`. No sample observed more than one unavailable member, and no
  Search Head container restarted;
- the bounded Event and Operator-log audit found no new
  `OutOfOrderRevision`, `ExistingUnavailablePod`, or `TooManyUnavailable`;
- final state had CR generation eight observed, phase `Ready`, three
  registered `Up` members, ordinal-zero captain ready, StatefulSet generation
  15 observed, matching current/update revisions
  `splunk-shc75-search-head-84cfcdf94d`, partition three, and three
  ready/updated replicas; and
- the restored 300-second policy did not revise or replace a Pod. A final
  321-second continuous gate observed three Ready/serving Pods, HTTP 401 from
  each unauthenticated management check, SHC initialized/minimum peers
  satisfied, three `Up` members, KV Store `ready`, no KV Store version upgrade
  or backup, and zero container restarts.

SHC-78 Pod-infrastructure-attribution qualification captured on 2026-07-29:

- source branch:
  `codex/shc-78-pod-infrastructure-attribution`;
- Operator implementation commit:
  `7b90da2694c1460b5e1522b5abb0a2d2151b190c`;
- final source used for the EKS image:
  `a5a41c07c9c7a9a1e1776f5cc41a146db6616da5`;
- Operator image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-reliability-a5a41c07c`;
- Operator image digest:
  `sha256:e29ac1024865e4f676655c229b01b8ed2690abe5412a669df2d473f074f6207f`;
- the Linux vWorkstation passed `make fmt`, `make vet`, `make build`, and
  `make test`; all 41 Ginkgo suites and 154 controller specifications passed
  with 78.5 percent composite coverage;
- the test ran on EKS cluster `vivek-spl-301372` in `us-west-2`, namespace
  `shc78-infrastructure`, with
  `SplunkPodLifecycle=true,SearchHeadClusterLifecycle=true`;
- the accepted Operator-only runtime was
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk:9.4.1-jdk-11`
  at digest
  `sha256:e51312c90d8cd860065a0fcb887a50c3d227122477b2ca3f5a7336f93d9308cb`;
- scheduler injection cordoned all three workers and produced an exact
  `PodScheduled=False/Unschedulable` replacement for ordinal two. Six hold
  samples retained partition two and unchanged ordinal-zero/one UIDs with two
  Ready endpoints and HTTP 200 search. Uncordoning completed `2 -> 1 -> 0`,
  including captain transfer before ordinal zero, with matching StatefulSet
  revisions, partition three, three Ready endpoints, and zero restarts;
- a topology-controlled generic ephemeral volume rollout established bound,
  attached volumes for the Deployer and all three Search Heads. The target
  ordinal and Deployer were in `us-west-2b`, ordinal one in `us-west-2c`, and
  ordinal zero in `us-west-2a`;
- CSI injection created a new bound ordinal-two PVC and Pod UID while all
  workers were cordoned, then scaled `ebs-csi-controller` from two replicas to
  zero before scheduling. The resulting PV/node pair had exactly one
  `VolumeAttachment` with `attached=false`, and the Pod reported
  `PodReadyToStartContainers=False`;
- six storage hold samples reported
  `WaitingForStorage/VolumeAttachmentPending`, retained ordinal zero/one UIDs,
  kept two Ready endpoints, and returned HTTP 200 for every service search;
- restoring CSI to two replicas progressed the same target through
  `WaitingForPodInfrastructure`, `WaitingForContainer`, and
  `ValidatingRecovery`; it became Kubernetes Ready, registered `Up`, reached
  KV Store `ready`, and returned the Service to three endpoints before another
  target was authorized; and
- cleanup removed the disposable namespace, all PVCs and generated PVs, and
  the test StorageClass. All three workers finished Ready and schedulable, and
  the EBS CSI controller finished at two ready replicas.

SHC-79 Kubernetes-volume-default normalization qualification captured on
2026-07-29:

- source branch: `codex/shc-79-normalize-volume-defaults`;
- implementation and exact image source:
  `a59fc5103b9199b2a136601ebfbdde1d593c4cc8`;
- Operator image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-79-a59fc5103`;
- Operator image digest:
  `sha256:e1b77c45bba3853f96a7ac93ef5d98ac84ebde9ca991d1fbd10a847865767ede`;
- Linux `make vet`, `make build`, and `make test` passed all 41 Ginkgo suites
  and 154 controller specifications with zero failures and 78.6 percent
  composite coverage;
- the accepted EKS fixture used cluster `vivek-spl-301372`, namespace
  `shc79-volume-defaults`, both lifecycle feature gates, and the accepted
  Splunk 9.4.1 runtime digest
  `sha256:e51312c90d8cd860065a0fcb887a50c3d227122477b2ca3f5a7336f93d9308cb`;
- the CR omitted `volumeMode`; the live Deployer and Search Head StatefulSets
  returned `volumeMode: Filesystem`; both remained generation one with one
  ControllerRevision and unchanged matching current/update revisions;
- after initial SHC formation converged, every member returned HTTP 200 for
  server info, SHC member info, and an `_internal` export search;
- restarting the Operator changed only the controller Pod UID and retained the
  same pinned image digest. Six post-restart samples retained all four
  workload Pod UIDs, zero restarts, four Ready Pods, three Search Head Service
  endpoints, initialized SHC state, captain readiness, and successful
  searches; and
- the Operator emitted zero `pod Volumes differ` records for the fixture.
  CR-first cleanup then removed all four Pods, twelve PVCs, and twelve PVs
  before namespace deletion; all workers remained Ready and schedulable, and
  EBS CSI finished at two ready replicas.

SHC-80 authorized-revision recovery qualification captured on 2026-07-29:

- source branch:
  `codex/shc-80-authorized-revision-recovery`;
- source commits:
  `d1f6e301d`, `744bfb096`, `9be744f06`, and
  `0b9253f1181947348c43eec7894ff1a9abd65366`;
- final Operator image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-80-0b9253f11`;
- final Operator image digest:
  `sha256:fecf5134468a2478c0de13ad88b463b8f2db38747d795e60aae3304a0b9986cb`;
- Linux `make fmt vet build test` passed all 41 Ginkgo suites and 154
  controller specifications with zero failures and 78.5 percent composite
  coverage;
- the EKS fixture used cluster `vivek-spl-301372`, namespace
  `shc80-authorized-recovery-v2`, both lifecycle feature gates, and pinned
  Splunk 9.4.1 runtime digest
  `sha256:e51312c90d8cd860065a0fcb887a50c3d227122477b2ca3f5a7336f93d9308cb`;
- after all workers were cordoned, revision
  `splunk-shc80-search-head-b6d6d44d9` was authorized for ordinal two and the
  replacement remained Pending and unschedulable. Revision
  `splunk-shc80-search-head-6987ddbf74` was then queued while both peers
  remained Ready and serving at last-known-good revision
  `splunk-shc80-search-head-8659646985`;
- the controller raised partition three, removed only the failed target,
  recreated it at the last-known-good revision, and retained exact lifecycle
  identity, revisions, UIDs, member GUID, and withdrawal timestamp across a
  real Operator restart while the replacement was Pending;
- after workers were uncordoned, recovered ordinal two rejoined with its
  original GUID. The queued revision then completed `2 -> 1 -> 0`, including
  captain transfers before ordinal-one and ordinal-zero replacement, and
  reset the converged StatefulSet partition to three;
- all final members were registered `Up` with `NoRestart` and their original
  GUIDs. The dynamic captain reported initialized, minimum peers joined, and
  service ready, without maintenance or Splunk rolling restart;
- the complete monitor recorded 187 successful Service searches, zero
  failures, minimum two serving endpoints, maximum one unavailable Search
  Head, and zero workload or Operator restarts;
- 21 final samples from `2026-07-29T21:40:43Z` through
  `2026-07-29T21:46:52Z` spanned 369 seconds with no bad sample; and
- CR-first cleanup removed four Pods, eight PVCs, and eight PVs before
  namespace deletion. No test SHC or PV remained, all workers were Ready and
  schedulable, and EBS CSI was `2/2` Ready.

## Interfaces and Dependencies

The technical designs must define concrete interfaces for:

- Splunk member readiness;
- captain discovery and captain transfer;
- detention and active-search observation;
- member registration, identity, and synchronization;
- upgrade initiation/finalization ownership;
- local shutdown invocation and state;
- container bootstrap versus persistent rejoin intent;
- deterministic bootstrap-seed, fresh-member join, interrupted-formation
  resume, persistent rejoin, and await-rejoin actions;
- dynamic healthy-member selection;
- StatefulSet partition observation and advancement; and
- durable operation state, conditions, Events, logs, and metrics.

Dependency order:

    API/status contract
      -> health and runtime signals
      -> durable lifecycle state machine
      -> captain/drain/shutdown/rejoin safety
      -> partition-gated RollingUpdate
      -> default enablement

Cross-repository delivery must name compatible minimum versions. The Operator
must detect unsupported image/runtime combinations and remain on a safe
behavior rather than assuming a hook, endpoint, or startup contract exists.

## Revision Note

2026-07-24: Added the parallel workstream and comprehensive qualification plans.
The milestone ordering now explicitly requires the integrated lifecycle to pass
under `OnDelete` before partition-gated `RollingUpdate` testing begins.

2026-07-25: Recorded the implementation discoveries around static ordinal-zero
captain interpretation, Docker-Splunk fail-fast startup, simultaneous
persistent cold restart, deterministic parallel formation, preferred-captain
policy, dynamic bundle targeting, and final two-branch integration. The plan
now distinguishes refusing unsafe formation from terminating a persisted
member and adds the missing runtime and manual qualification steps.

2026-07-25: Combined the Splunk Ansible children locally, validated their
contracts and playbook syntax, added and validated immutable Docker-Splunk
source-ref selection, repaired and pinned the repository-era SHC lint gate,
and recorded the modern-linter compatibility limitation. The integration
commit remains local until its target remote and review path are approved.

2026-07-25: Corrected the image-build milestone for the actual workstation.
The current Mac is a source-validation and handoff environment only.
Docker-Splunk image construction and runtime qualification now require a
separate supported Linux builder with recorded provenance.

2026-07-25: Added the local integration-freeze audit and explicit
remote-reachability gate. The concrete handoff manifest is generated after the
source commit rather than committed with a self-invalidating Operator SHA.

2026-07-28: Recorded the first pinned Linux image build and integrated EKS
qualification. Added the passing `OnDelete` rollout, safe strategy migration,
partition-gated `RollingUpdate`, captain-transfer, persistent-identity, and
controller-restart evidence. The plan intentionally leaves failure injection,
version skew, rollback, repetition/soak, and production enablement open.

2026-07-28: Added the corrected Docker-Splunk TERM/PID 1 contract and active
`RollingUpdate` to `OnDelete` rollback rehearsal. Recorded sustained pre- and
post-action stability, operation continuity, one-member safety, captain
transfer, retained identities, the `OnDelete` revision-status nuance, and the
observability defects discovered during an otherwise successful campaign.

2026-07-28: Recorded SHC scale-lifecycle implementation and EKS qualification.
Added safe scale-down cancellation, repeated-operation identity, scale-up join
coordination, scale-down resumption, desired-replica Event semantics, and
desired-ordinal member observation. The final pinned image passed complete
`3 -> 4` and `4 -> 3` cycles plus a 300-second stability gate without false
rollout blocks, false scale Events, expected-lifecycle errors, concurrent
planned disruptions, or container restarts.

2026-07-28: Recorded durable Pod-update cancellation and targeted search-drain
qualification. Added fail-closed real-time timeout recovery, fresh search-count
observation during cancellation, bounded historical drain before replacement,
complete reverse-ordinal `RollingUpdate`, dynamic captain transfer, exact
Event-count assertions, and the startup-complete contract gap observed between
reported readiness and final image-owned synchronization.

2026-07-28: Recorded the audited search-drain continuation milestone. Added the
post-timeout operation/token handshake, durable approval barrier and
search-count snapshot, wrong-token and stale-operation fail-closed evidence,
approval-only revision isolation, exact Event/log/metric audit signals,
reverse-ordinal EKS rollout, captain transfer, 312-second post gate, and the
external-observer timing and secure-metrics-access discoveries.

2026-07-28: Recorded SHC-75 failed-captain-transfer and revision-withdrawal
qualification. Added ControllerRevision-reuse handling, in-place cancellation
ownership through Kubernetes readiness, the StatefulSet generation-observation
barrier, exact warning/cancellation Event deltas, a clean bounded log audit,
reverse-ordinal rollback, and the passing 321-second final stability gate.

2026-07-29: Recorded SHC-78 source and EKS qualification for structured
scheduler, generic Pod-infrastructure, and exact CSI attachment attribution.
Added the complete scheduler recovery, bounded CSI hold and recovery, minimum
service-capacity evidence, the Splunk 10.6 KV Store qualification boundary, and
the newly identified template-defaulting, authorized-revision withdrawal, and
deletion-finalization follow-up requirements.

2026-07-29: Recorded SHC-79 source and EKS qualification for Kubernetes
Pod-volume default normalization. Added exact CR-versus-StatefulSet defaulting
evidence, semantic comparison tests, immutable image provenance, controller
restart recovery, six stable post-restart samples, successful member searches,
and the separate CRD-schema and Make deployment-helper discoveries.

2026-07-29: Registered the customer-reported App Framework restart-availability
concern as SHC-82 and stable scenario OPS-011. The plan now separates the
observed `searchable=0` and `force=0` record from unproven conclusions, spans
both Search Head and indexer clusters, and requires exact Splunk semantics,
active-search behavior, continuous service evidence, and fail-closed
redundancy qualification before a product default or forced mode is selected.

2026-07-29: Selected SHC-80 on isolated branch
`codex/shc-80-authorized-revision-recovery` from integrated feature baseline
`9eecde5d68e9dc889bb2b2f1913420396e00cb21`. Recorded the single-target safety
boundary and retained partially completed rollouts and image upgrades as
fail-closed cases. This registration deliberately makes no implementation or
qualification claim.

2026-07-29: Recorded SHC-80 source and EKS qualification. Added durable
authorized-revision withdrawal, forced-rollback target recycling behind a
partition and peer-safety barrier, completed-recovery release of queued work,
Operator-restart continuity, exact persistent member identity proof, dynamic
captain transfer, 187 uninterrupted searches, a 369-second stability gate,
and complete CR-first storage cleanup. Also recorded the fresh-formation
readiness gap and the destructive CRD dependency in the current `make deploy`
helper as separate follow-up concerns.

2026-07-29: Selected SHC-81 on
`codex/shc-81-termination-safe-finalization` from integrated feature baseline
`efbff783f02be7cee29c45c793e5cd2886dd2325`. This registration deliberately
makes no implementation or qualification claim.

2026-07-30 UTC: Recorded SHC-81 implementation and EKS qualification at exact
source `58437e3ad` and Operator digest
`sha256:f2ffee5a6cc7d33b2aa26e8cbdab81618a3785e31600d7a676ed3ec149c52b6d`.
The accepted namespace-first deletion created no resources after namespace
termination, removed all declared storage, avoided inner and outer stale
status writes, and left no workload or PV behind. Registered SHC-83 and SHC-84
as separate, still-pending requirements for startup-complete traffic readiness
and the startup-budget/TERM-exit contract. Also retained the structurally
similar condition-writer pattern in non-SHC controllers as an Operator-wide
audit item rather than extending SHC-81 without qualification.

2026-07-30 UTC: Selected SHC-82 on
`codex/shc-82-appframework-restart-availability` from integrated feature
baseline `079e26233267`. The branch begins with source tracing and controlled
reproduction; this record deliberately makes no implementation,
qualification, or default-policy claim.

2026-07-30 UTC: Recorded SHC-85 controller-restart qualification on isolated
branch `codex/shc-85-controller-restart-qualification`. Deleted the Operator
at the persisted ordinal-3 `Decommissioning` stage, verified exact operation
and target continuity in the replacement controller, observed exactly one
decommission Event for that target, and completed the full `3 -> 2 -> 1 -> 0`
roll with previous-peer remote serving recovery before each next target.
Recorded 100/100 and 30/30 zero-failure exact workload runs, the transparent
valid-empty classification in the overlapping 80/80 run, immutable runtime
and Operator provenance, final RF/SF/all-searchable/no-fixup health, and the
remaining disconnection, contention, conflict, redundancy, and compatibility
boundaries.

2026-07-30 UTC: Selected SHC-83 on isolated branch
`codex/shc-83-startup-readiness-qualification` from the last SHC-85 qualified
baseline `1e695381a`. Source tracing established that the historical
early-ready evidence already included the per-member serving gate and that
Docker-Splunk exposes successful local Ansible completion through its
container-state-backed readiness probe. The selected contract adds a
first-formation all-desired-container barrier before the existing live Splunk
checks, while preserving availability for previously stable topologies. This
record does not claim implementation or EKS qualification.

2026-07-30 UTC: Recorded SHC-83 implementation and EKS qualification at exact
code source `2889c80025bbf1e9010dc8722a10b35320e39195` and Operator digest
`sha256:22a4398917a3dc27bdbe68aa4513c70b2bfd4d62f05a474e55fd6f9600db7ae9`.
The fresh three-member formation published zero client endpoints until the
durable stage reached `Complete`, emitted exactly one initial-formation
restart Event, then retained three endpoints for twelve stable samples with
zero Kubernetes container restarts. The first campaign exposed and the same
source corrected the internal-bundle-target/client-readiness circular
dependency by keeping those two contracts separate. Established non-captain
and active-captain Pod replacements each retained at least two endpoints,
withheld the replacement until it rejoined, and returned to three stable
endpoints; active captaincy moved dynamically from ordinal zero to ordinal
two. Deleting the Operator Pod changed no Search Head UID, endpoint, restart
count, formation stage, or durable stable-replica result. This qualifies the
bounded SHC-83 contract on the current v4 API; it does not claim the separate
startup-budget and prompt TERM-exit work tracked by SHC-84.

2026-07-30 UTC: Registered OPS-012/SHC-86 as a separate LicenseManager
namespace-finalization requirement after SHC-83 teardown reproduced a Secret
create attempt in a terminating namespace and required finalizer clearing
after owned resources were absent. No implementation or qualification is
claimed.

2026-07-30 UTC: Selected SHC-84 on
`codex/shc-84-startup-term-qualification` from qualified SHC-83 source
`163d5d646`, with Docker-Splunk based on integrated runtime source
`f063cfd3936c42428c0775783b8415c2fcfbb3ef`. The first campaign measures the
rendered current-v4 startup/liveness probes and Pod termination grace, actual
first-start and persistent-restart durations, kubelet probe Events, exact
runtime shutdown ownership, and TERM-to-container-exit time. No threshold or
production-policy decision is claimed before that evidence.

2026-07-31 UTC: Recorded SHC-84 accepted binary source `67c0d3bd2`, evidence
monitor source `cbaef60af`, and immutable Operator digest
`sha256:d83ae44c825f13cb12117e72d2ca5415b4ffd9b7af36bcab7e81226e11e6cafe`.
The exact source passed the Linux Make gate. Existing-v4 reconciliation
converged all three members to startup threshold 60, startup/liveness grace
660, no readiness grace, and Pod grace 1200. Fresh formation completed with
zero container restarts. Forced liveness restarted only the target container
once with the same Pod UID while two peers stayed serving. Planned deletion
replaced only the non-captain target while the peers stayed serving, then
returned the replacement after registered/`Up` recovery. The supported-upgrade
matrix cell was still open at this checkpoint.

2026-07-31 UTC: Registered SHC-87 after the same fresh formation temporarily
reported SHC `Error` and upgrade-validation failure while its referenced
LicenseManager was still starting, then recovered without user action through
`Pending` to `Ready`. This is a retryable dependency-status classification and
supportability requirement; no implementation or qualification was claimed at
registration. The later 2026-08-01 record below completes the bounded item.

2026-07-31 UTC: Completed the bounded SHC-84 qualification with a supported
Splunk `10.4.2604.0/60dd7967c086` to
`10.5.2605.0/844c593e9c1d` upgrade. The source cluster retained zero restarts
despite 29 startup failures on ordinal zero. The LicenseManager upgraded first
without changing any Search Head UID, restart count, or endpoint. The Search
Heads then rolled `2 -> 1 -> 0`, retained at least two endpoints, moved
captaincy dynamically, recorded zero container restarts and 200/200 successful
sampled searches, and finished `Ready`/`Complete`/`Upgraded` with three
registered `Up` target members. The result is bounded to the exact current-v4
version pair; it does not claim v3-to-v4 conversion, every future pair, SAML,
or every workload.

2026-07-31 UTC: Recorded the SHC-85 lifecycle-hold source and EKS campaign on
`codex/shc-85-lifecycle-hold-qualification`. An explicit lifecycle marker now
lets an initialized container remain live while `splunkd` is intentionally
stopped after readiness withdrawal; missing or incomplete container state and
ordinary level-one liveness still fail closed. The monitor removed the
Operator for exactly 300 seconds at persisted ordinal-3
`ReadyForReplacement`, retained the exact target and operation with three
serving peers and zero liveness failures, then restored the controller and
completed `3 -> 2 -> 1 -> 0` with zero restarts. The accepted API-independent
workload submitted 1,800 events with zero HEC or search-request failures and
final exact completeness. It also exposed 24 temporary successful-search count
regressions, with a maximum sequence-to-count gap of 362 while Search Heads
were still converging indexer peer addresses and authentication. The campaign
also corrected the captain-only member query target and
successful-observation/deadline
ordering exposed during fresh SHC formation. The evidence does not claim an
API-server partition, controller absence at every stage, or Operator control
over Splunk-managed App Framework peer selection, and it does not claim that
immediate distributed-search completeness is solved.

2026-07-31 UTC: Recorded the separate SHC-85 observed-decommissioning absence
campaign on `codex/shc-85-decommissioning-absence-qualification`. The harness
waited for persisted `observedDecommissioning=true`, removed the Operator for
306 observed seconds, retained the exact operation and one non-serving target,
then completed `3 -> 2 -> 1 -> 0` with zero restarts and final healthy
RF/SF/searchability. Its independent 1,800-event workload had zero request
failures and exact eventual results on every Search Head. It also recorded 41
count regressions and maximum pending 406 after the lifecycle had already
reported `Completed` with four Ready desired-revision Pods, while Search Heads
still attempted old Pod IPs. This qualifies the bounded controller-absence
stage but leaves immediate distributed-search completeness open.

2026-07-31 UTC: Recorded the isolated SHC-85 target-selection absence
campaign on `codex/shc-85-target-selected-absence-qualification`. The
accepted harness captured the short persisted `TargetSelected` stage, removed
the controller for 300 seconds while all four original Pods stayed Ready and
serving, then restored the same durable operation through the complete
`3 -> 2 -> 1 -> 0` roll. The lifecycle and 1,800-event workload records have
SHA-256 values
`01f3cf1fe9330b2a139a2243d2ca3f5771bfada39ee9d25bd267410d52ef9c0e`
and
`d0d8de5eb851bea87a9057f0676e1b5d5f6e16a7ea134e0130d4af04ea6b2c3d`.
Request continuity and eventual exactness passed; 18 successful-search count
regressions with maximum pending 364 keep immediate distributed-search
completeness open. At this checkpoint, API-server disconnection and the
remaining negative and compatibility variants were separate gates.

2026-08-01 UTC: Recorded the isolated SHC-85 API-disconnection campaign on
`codex/shc-85-api-disconnection-qualification`. Harness commits `8e21b9b1b`
through `f78828cc1` blocked only the Operator Pod's API Service path for 401
seconds with an exact, fail-safe rule. The manager lost its leader lease and
restarted once in the same Pod while the durable ordinal-3 operation remained
unchanged at observed `Decommissioning`. Thirty-six hold samples covered 302
seconds with three serving peers and one unchanged non-serving target. API
recovery resumed that operation and the companion monitor completed
`3 -> 2 -> 1 -> 0` with ten stable samples. The 1,800-event workload retained
zero request failures and exact eventual results but observed 30 count
regressions and maximum pending 417. This closes the bounded K8S-006 gate at
one lifecycle stage; immediate completeness, other partition stages and
topologies, leader contention, conflict, redundancy, and compatibility remain
open.

2026-08-01 UTC: Recorded the isolated SHC-85 controller-leader-failover
campaign on `codex/shc-85-leader-failover-qualification`. Harness source
`ba220677b` established two Ready zero-restart contenders under one stable
Lease holder, then deleted the exact active leader while ordinal 3 retained an
observed durable decommission operation. A newly created replacement acquired
the Lease after expiry; transitions advanced once from 80 to 81 and takeover
completed in 53 seconds. The same operation resumed, the original target's
decommission Event count stayed one, and the successor completed
`3 -> 2 -> 1 -> 0` plus ten stable samples with two healthy controllers,
maximum indexer unavailability one, and zero restarts. The 150-line lifecycle
record and five-line leader record have SHA-256 values
`9b7193931ac6c72f02edc45265a303d4f88a8e59da6967d6e03e368f837ae6f3`
and `c6d662265eec4e5c5683f344c3f7e39a6532f23264253320d9db113ada66a409`.
The 1,800-event Job had zero request failures, exact final results on every
Search Head, 13 count regressions, and maximum pending 329; workload SHA-256
is `e34ef36dd49a7f835028d13ebd3336fdd1090f7b7210bbc50d78f27f3ec1ed05`.
This closes bounded STS-004 for one normal takeover, not concurrent active
leaders, Lease corruption, controller partition, or repeated failover.

The cleanup leader start separately exposed that LicenseManager expiration
checking targets a per-Pod FQDN under a headless Service which the
LicenseManager reconciler does not create. The call logs DNS `no such host`,
continues, and leaves the CR Ready without completing the license query. Code,
Service, EndpointSlice, and cross-Pod DNS inspection confirmed the mismatch.
SHC-88 records this adjacent requirement; no fix is part of SHC-85. The next
record captures its later isolated completion.

2026-08-01 UTC: Completed the bounded SHC-88 source and EKS qualification on
`codex/shc-88-license-health`. Exact source `241ea3d91` passed `make test` on
Linux with 41 suites, 156 specs, zero failures, and 78.6 percent composite
coverage, plus `make build` and a clean generated-tree check. Operator digest
`sha256:545910a6b769ad399fea42fdb31ddb79af11d38b5e5691ed3a59786a7606180e`
created the missing headless Service, retained the existing LicenseManager Pod
at creation, resolved its FQDN, and completed HTTP 200 license requests. One
initial DNS-publication race produced the intended retryable Warning Event.
A qualification-only CR annotation later propagated to the Pod template and
caused one same-version LicenseManager replacement; while that Pod was
unready, the Operator skipped the REST check. The replacement completed with
Ansible `failed=0`, Ready state, and zero container restarts. A subsequent
clean Operator restart kept the Service UID `42512aa1-ba9d-4919-88bd-9dee4909fc92`
and LicenseManager Pod UID `60ba6aef-10da-41a1-a947-9e75efaf36bf`, added three
HTTP 200 checks, added no failure Event occurrence, and left every managed tier
Ready. Detailed evidence and replay boundaries are in
`SHC88LicenseManagerHealthQualification.md`.

2026-08-01 UTC: Completed bounded OPS-012/SHC-86 on
`codex/shc-86-license-finalization`. Source `61b35aabf` routes LicenseManager
deletion before pause and ordinary validation, performs only deletion-safe
cleanup, invokes declared finalizers, and returns without a status refresh
after success. All 41 Linux suites and 157 specs passed. Immutable Operator
digest
`sha256:635d60fecdd203e7d158fb1f95c57d46c7062ed98b156caf8dc68da7515812ec`
passed adversarial and real referenced-LicenseManager EKS deletion campaigns.
The real CR finalized by six seconds, its Ready zero-restart Pod exited around
50 seconds, both bound PVCs and delete-reclaim PVs disappeared, and Kubernetes
removed the namespace naturally at 337 seconds. No manual patch, forbidden
create, post-finalization status error, or LicenseManager reconcile error was
recorded. SHC-86 did not correct the separately observed invalid empty-phase
status retries for LicenseManager and SearchHeadCluster CRs created already
paused; the later SHC-89 entry records that correction.

2026-08-01 UTC: Completed bounded SHC-87 on
`codex/shc-87-dependency-status`. Source `20d926658` classifies missing,
Pending, missing-workload, and not-yet-rolled dependencies as retryable and
retains contradictory desired images as terminal. All 41 Linux suites and 157
specs passed. Operator OCI index digest
`sha256:fbb1a53c45da509fee47edc618eefd93923fc3864df9533dc85dbcbc8914c2a3`
qualified a disposable SHC created before its LicenseManager. The SHC retained
Pending/Progressing `DependencyNotReady` conditions and specific Normal Event
series for the absent and starting dependency, then cleared that state without
Error when the LicenseManager became Ready. It completed with Ready Deployer,
3/3 Ready members, three endpoints, all members Up, zero container restarts,
direct search success on each member, and 8/8 service-routed searches. The
scoped pre-cleanup Operator log audit found zero terminal mismatch and zero
Reconciler error entries. Detailed evidence and remaining source-only boundaries are in
`SHC87DependencyStatusQualification.md`.

The SHC-87 cleanup registered SHC-90 after namespace termination preceded CR
deletion visibility. During that propagation interval, LicenseManager and
SearchHeadCluster entered normal Apply paths and Kubernetes rejected ConfigMap
creation in the terminating namespace, producing six and nine Reconciler
errors respectively. Existing finalization then completed without a patch;
all ten PVCs and PVs disappeared and the namespace completed naturally.
SHC-90 must add a namespace-termination guard without weakening the existing
CR-deletion finalizers. No SHC-90 implementation is claimed here.

2026-08-01 UTC: Completed bounded SHC-89 on
`codex/shc-89-paused-status`. Exact source `3e1716737` passed 41 Linux suites,
157 specs, `make build`, and a clean generated-tree check. Operator digest
`sha256:b83bbb97f89dca45e183e895e4be7e1d7bd11007f08babb41c4c94c97d18f145`
initialized schema-valid `Pending/Paused` status once for Standalone,
LicenseManager, ClusterManager, MonitoringConsole, IndexerCluster,
SearchHeadCluster, and IngestorCluster created already paused. All seven
resourceVersions stayed stable for 45 seconds, no managed workload appeared,
and the namespace-scoped Operator audit found zero paused-status and zero
Reconciler errors. Removing pause let LicenseManager and SearchHeadCluster
continue normally to Ready. The three-member SHC finished with three
endpoints, all members Up, zero restarts, and direct search success on every
member. Cleanup removed the disposable namespace, all ten claims, and every
PV reference to it. Exact evidence is in
`SHC89PausedStatusQualification.md`.

The same source audit registered SHC-91 for deletion-before-pause ordering in
Standalone, ClusterManager, MonitoringConsole, IndexerCluster, and
IngestorCluster. LicenseManager and SearchHeadCluster already route CR
deletion before pause. No SHC-91 implementation or qualification is claimed
here.
