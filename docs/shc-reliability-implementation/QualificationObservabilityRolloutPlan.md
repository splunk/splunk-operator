# Prove Search Head Cluster Lifecycle Reliability Before Enabling RollingUpdate

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

## Purpose / Big Picture

This plan proves that the proposed Search Head lifecycle preserves useful
service during planned Kubernetes replacement, safely handles the captain,
resumes after controller interruption, and exposes where time or failure
occurred. A reviewer can see it working by following one operation from
detention through replacement and rejoin while independent probes continuously
measure member traffic, captain availability, search completion, StatefulSet
revision, and lifecycle evidence.

The test program first validates the new lifecycle using the current
`OnDelete` StatefulSet strategy. Only after that gate passes does it enable an
Operator-managed `RollingUpdate` partition and repeat the same tests. This
separates Splunk lifecycle correctness from the change in Kubernetes rollout
ownership.

## Progress

- [x] (2026-07-24) Refreshed and inspected the GitLab Operator development
  baseline at `39316c19fb990f1af84966d5269a8f4116550dbb`.
- [x] (2026-07-24) Identified reusable Operator unit, envtest, Ginkgo,
  KUTTL, search, scale, and upgrade test infrastructure.
- [x] (2026-07-24) Identified existing Docker-Splunk distributed-image tests
  and splunkd SHC unit-test targets.
- [x] (2026-07-24) Defined branch ownership and dependency waves in
  `ParallelWorkstreamPlan.md`.
- [x] (2026-07-24) Defined stable scenarios and priorities in
  `SHCTestScenarioMatrix.md`.
- [x] (2026-07-25) Added branch-local unit contracts for ordinal-zero
  preferred-captain behavior, bootstrap versus rejoin, every three-member
  parallel startup ordering, persistent cold restart, and dynamic bundle
  targeting when the seed is unavailable.
- [x] (2026-07-28) Integrated and published the Splunk Ansible startup work,
  selected its immutable ref from Docker-Splunk, and built the resulting
  runtime image on the Linux vWorkstation.
- [ ] Refresh all three repository baselines and record immutable commits.
- [x] (2026-07-25) Audited current local heads and baseline ancestry. The
  publication gap found by this audit was cleared on 2026-07-28 by publishing
  immutable source commits and pinned Linux images used by the EKS campaign.
- [ ] Approve lifecycle invariants, test reason codes, and evidence schema.
- [ ] Implement branch-local test doubles and contract tests.
- [x] (2026-07-28) Established a reproducible three-member EKS SHC integration
  environment using pinned Operator, Docker-Splunk, Splunk Ansible, and Splunk
  Enterprise inputs.
- [ ] Capture current-behavior baselines before enabling spike behavior.
- [ ] Pass the health, API, state-machine, and runtime branch gates.
- [ ] Pass the complete integrated `OnDelete` lifecycle gate. One full
  three-member happy-path rollout, including captain replacement and retained
  identity, passed on 2026-07-28; failure injection, active-search policy,
  repetition, and soak requirements remain.
- [ ] Pass the complete opt-in partition-gated `RollingUpdate` gate. Safe
  migration, one full `3 -> 2 -> 1 -> 0 -> 3` rollout, and controller restart
  during `WaitingForTermination` passed on 2026-07-28. One active-target
  rollback to `OnDelete` also passed; rollback under failure and the remaining
  failure scenarios remain.
- [ ] Complete disruption, version-skew, upgrade, and rollback qualification.
- [x] (2026-07-28) Corrected and qualified lifecycle-aware member-observation
  log severity, Pod-update phase reporting, stable desired-replica tracking,
  and scale-event accuracy.
- [x] (2026-07-28) Qualified scale-down cancellation before membership removal,
  repeated scale-down of the same ordinal, additive `3 -> 4`, permanent
  `4 -> 3`, reverse-ordinal peer-list rollouts, and final PVC retention/removal
  behavior on EKS.
- [x] (2026-07-28) Passed a 300-second post-scale stability window over 17
  samples with all Kubernetes and Splunk health invariants continuously true.
- [x] (2026-07-28) Qualified one real-time drain-timeout and cancellation
  path on EKS. The operation failed closed before replacement, kept the
  original Pod UID and StatefulSet partition, removed the target from service,
  and recovered that same member when the requested revision was withdrawn.
- [x] (2026-07-28) Qualified one bounded historical-search path on EKS. The
  active historical count prevented replacement authorization, then all three
  ordinals completed a native partition-gated `RollingUpdate` after the count
  reached zero, including captain transfer before ordinal-zero replacement.
- [x] (2026-07-28) Qualified LFC-006 audited continuation on a fresh EKS SHC.
  Wrong-token and stale-operation approvals remained fail closed; the exact
  post-timeout operation and token produced one durable approval, Event,
  structured log, and counter increment before later replacement
  authorization.
- [x] (2026-07-28) Completed the approved rollout in reverse ordinal order with
  at most one unavailable Search Head, dynamic captain transfer, matching
  revisions, three ready and serving members, zero container restarts, and a
  312-second post-action stability gate.
- [x] (2026-07-28) Qualified LFC-007 captain-transfer timeout on EKS with the
  final `3e9e735a7` Operator image. The captain target failed closed without
  deletion or replacement authorization, revision withdrawal recovered the
  same Pod in place, and rollback completed ordinals `2 -> 1` with at most one
  unavailable member.
- [x] (2026-07-28) Audited the LFC-007 run window. It added exactly one
  `CaptainTransferTimedOut` warning and one cancellation Event, emitted no
  `OutOfOrderRevision`, `ExistingUnavailablePod`, or `TooManyUnavailable`
  Event or log, and passed a 321-second final combined Kubernetes, SHC,
  management, and KV Store stability gate.
- [x] (2026-07-29) Qualified SHC-79 Kubernetes-volume default normalization on
  EKS with exact source `a59fc5103b9199b2a136601ebfbdde1d593c4cc8`.
  The CR omitted generic-ephemeral `volumeMode`, both API-server-returned
  StatefulSets contained `Filesystem`, and six samples after a real Operator
  restart retained identical workload Pod UIDs, zero restarts, unchanged
  StatefulSet generations and revisions, three endpoints, and successful
  searches without a volume-difference reconcile.
- [x] (2026-07-29) Qualified SHC-80 authorized-revision recovery on EKS with
  exact source `0b9253f1181947348c43eec7894ff1a9abd65366`. One authorized,
  unschedulable ordinal recovered at the last-known-good revision across an
  Operator restart, then released a queued revision that completed
  `2 -> 1 -> 0` with dynamic captain transfer and every persistent member GUID
  preserved. The monitor recorded 187 HTTP 200 searches, zero failures,
  minimum two serving endpoints, maximum one unavailable member, zero
  restarts, and a clean 369-second final gate.
- [x] (2026-07-30 UTC) Qualified SHC-81 termination-safe finalization on EKS
  with exact source `58437e3ad` and Operator digest
  `sha256:f2ffee5a6cc7d33b2aa26e8cbdab81618a3785e31600d7a676ed3ec149c52b6d`.
  Direct namespace deletion of a paused, healthy three-member SHC removed the
  CR finalizer and all eight declared PVCs without creating namespace content,
  writing status after successful finalization, or producing namespace-
  termination or storage-precondition errors. The namespace, four workload
  Pods, eight PVCs, and all eight PVs were absent about 40 seconds after the
  deletion request.
- [ ] Qualify OPS-011/SHC-82 with a restart-required App Framework package on
  both a three-member SHC and a replicated indexer cluster. Capture the
  effective restart flags and exact peer/member order while continuous ingest,
  real-time, historical, and scheduled searches verify availability and result
  completeness. Repeat with an active Operator rollout and with replication or
  search factor intentionally unmet; both cases must block rather than start a
  conflicting or forced restart.
  Partial evidence from 2026-07-30 confirms that the ClusterManager,
  IndexerCluster, and SearchHeadCluster all referenced the same
  LicenseManager, and every generated non-LicenseManager Pod received the
  expected LicenseManager Service URL. The LicenseManager itself initially
  used the built-in Enterprise trial license, which Splunk rejected for remote
  manager operation. A Secret-backed, remote-manager-capable license removed
  that qualification-environment error. A subsequent version `1.0.1` update
  submitted 120 numbered HEC events with zero ingestion failure and recovered
  every sequence exactly once, but produced 11 failed Service searches and
  nine zero-endpoint samples during the SHC rolling restart. All Pod UIDs and
  Kubernetes container restart counts remained unchanged. The package reloaded
  on the indexers without a restart, so App Framework availability,
  indexer-restart, active-search, conflict, and unhealthy-redundancy gates stay
  open.
- [ ] Qualify SHC-83 with an explicit startup-complete traffic-readiness
  contract across image-owned initialization, synchronization, and internal
  Splunk restarts.
- [ ] Qualify SHC-84 with measured first-start and upgrade startup budgets,
  kubelet probe-triggered restart behavior, and bounded TERM-to-container-exit
  evidence for every supported runtime.
- [ ] Complete cloud-provider qualification and release-readiness review.

## Surprises & Discoveries

- Observation: the Operator already has label-driven test entry points:
  `make test-unit`, `make test-integration`, `make test-e2e-pr`, and
  `make test-e2e-full`.
  Evidence: the current `sok/develop` Makefile.
  Consequence: add an SHC lifecycle label and suite rather than creating a
  separate test runner.

- Observation: existing in-cluster helpers can issue searches and inspect
  search jobs, but the current ingest/search suite covers Standalone, not SHC.
  Evidence: `test/testenv/search_utils.go` and
  `test/ingest_search/ingest_search_test.go`.
  Consequence: reuse the helpers and add SHC-specific availability and
  long-running-search fixtures.

- Observation: existing deployer verification in
  `test/testenv/search_head_cluster_utils.go` addresses ordinal zero directly.
  Consequence: the test harness itself must stop encoding the static-captain
  assumption before it can verify dynamic healthy-member selection.

- Observation: Docker-Splunk has a distributed three-Search-Head pytest, but
  its current test assumes `sh1` is preferred captain and the checkout has
  unrelated local modifications.
  Consequence: use a clean worktree and add focused lifecycle tests; do not use
  this dirty checkout as the spike baseline.

- Observation: the local splunkd checkout is more than eleven thousand commits
  behind its remote development branch.
  Consequence: source references remain useful for orientation, but any
  splunkd implementation or qualification baseline must be freshly fetched and
  pinned by the Search Head team.

- Observation: a simultaneous persistent restart can temporarily make both
  local member and captain APIs inconclusive on every Pod. Because the
  Docker-Splunk entrypoint uses `set -e`, a fatal Ansible classification would
  exit every container and can create a restart loop.
  Consequence: STS-012 must prove that this state runs no formation commands,
  leaves splunkd alive, keeps readiness false until recovery, and is eventually
  classified by the Operator if recovery does not complete.

- Observation: there are two bundle paths to qualify. Operator App Framework
  scheduling and image-owned deployer startup historically had independent
  ordinal-zero coupling.
  Consequence: OPS-005 does not pass until both paths succeed through a
  non-zero healthy member with ordinal zero unavailable.

- Observation: a customer reported restart-required App Framework delivery on
  both Search Heads and indexers. The supplied indexer record had
  `searchable=0`, `force=0`, and a successful preflight with replication
  factor, search factor, and all-data-searchable flags set.
  Consequence: preserve that record as the reproduction signature, but do not
  use it alone as an availability verdict. Qualification must correlate every
  peer transition with the Cluster Manager's structured replication,
  search-factor, searchable-bucket, and rolling-restart state and with
  end-to-end searches. It must separately record what happens to searches that
  were already running when each Search Head or indexer restarts.

- Observation: the first versioned SHC-82 update performed a Splunk-managed
  Search Head rolling restart without replacing a Pod or incrementing a
  Kubernetes container restart count. The member order was `2 -> 1 -> 0`, with
  captain transfer `0 -> 2 -> 0`. The five-second, three-failure readiness
  probe did not withdraw each short internal splunkd outage before traffic
  could reach it. Later, transient captain unavailability caused the
  Operator-owned readiness gate to mark all three Pods `ClusterNotReady`, even
  though this was not an Operator lifecycle target operation.
  Consequence: qualification must observe internal splunkd start identity in
  addition to Pod UID and container restart count, and must report
  `ContainersReady`, Pod `Ready`, the SHC serving gate, and EndpointSlice state
  separately. The implementation must combine prompt local withdrawal with
  previously-formed-cluster evidence so a captain transition cannot remove all
  otherwise healthy local search capacity.

- Observation: the same package did not exercise an indexer rolling restart.
  Every peer reported `restart_required=0`, and the Cluster Manager completed
  the bundle by reload.
  Consequence: the indexer-side SHC-82 gate remains untested; the qualification
  fixture must be replaced or extended with a configuration Splunk classifies
  as restart-required before any searchable-restart policy is evaluated.

- Observation: the compatibility variable named as a captain URL is also a
  bootstrap seed. Its name cannot be treated as proof of runtime captaincy.
  Consequence: tests must independently observe the elected captain from
  Splunk and verify that Kubernetes SHCs do not prefer ordinal zero by default.

- Observation: the current workstation is macOS, and the Docker-Splunk
  Makefile does not provide a supported local build path for the target
  enterprise Linux image.
  Consequence: local success is limited to source preparation, exact-ref
  verification, unit contracts, lint, syntax, and manifest generation. Image
  build, container execution, and distributed-image tests require a separate
  Linux builder.

- Observation: clean local commits are insufficient for a remote Linux build.
  The current Operator, Docker-Splunk, and Splunk Ansible heads have no
  containing fetched remote-tracking refs.
  Consequence: the pre-dispatch gate must verify each full SHA from the
  approved remote, not merely from a local branch or worktree.

- Observation: an `OnDelete` rollout can leave StatefulSet
  `currentRevision` on the old ControllerRevision even when every Pod carries
  the new revision.
  Evidence: the integrated three-member campaign observed exactly that state.
  Consequence: the migration assertion checks Pod revisions independently,
  applies `RollingUpdate` with partition equal to replicas, and requires
  current/update revision convergence without a Pod UID change.

- Observation: the first stable SHC formation may be followed by a
  Splunk-managed rolling restart.
  Evidence: the deployer initiated this internal operation before the test
  lifecycle campaign.
  Consequence: the fixture is ready only after the captain reports service
  ready, no rolling restart, no maintenance, and all expected members Up.

- Observation: the CR and Kubernetes Pod conditions can all report Ready while
  that Splunk-managed rolling restart remains active.
  Evidence: the rollback fixture reported three ready replicas while Splunk
  reported `rolling_restart_flag=1`, moved `Restarting` through multiple
  members, and briefly refused the local management connection. Both later
  search-drain fixtures also reached CR Ready before final
  Docker-Splunk/Splunk Ansible destructive synchronization cycled port 8089
  sequentially across the three members without a container restart.
  Consequence: use a continuous pre-action and post-action stability window
  that includes local management reachability; never authorize a lifecycle
  test from one Ready observation. Record the missing image-to-Operator
  startup-complete signal as a runtime contract gap.

- Observation: `member/info` can return HTTP 503 during a legitimate rejoin
  window before captain communication and minimum peer state are restored.
  Consequence: tests must prove readiness remains false and partition does not
  advance until the bounded rejoin observation succeeds; the transient 503 is
  neither ignored nor treated as an immediate terminal error.

- Observation: broad Pod-template inputs such as `extraEnv` also revise the
  deployer.
  Consequence: evidence collection watches both StatefulSets so an SHC result
  does not hide an unintended deployer replacement or overlapping operation.

- Observation: the original Docker-Splunk TERM path could complete Splunk
  shutdown without exiting PID 1, causing Pod deletion to wait for the complete
  termination grace period.
  Consequence: qualification includes a direct TERM-to-container-exit contract,
  exact-once shutdown invocation, and an in-cluster elapsed-time smoke test.
  The first SHC-81 fixture confirmed why the contract is mandatory: about
  7 minutes 24 seconds of supported first-start work exceeded the approximately
  6 minute 29 second default startup-probe budget, the kubelet applied the
  configured 1200-second grace to that probe-triggered restart, and a legacy
  image without `/sbin/splunk-shutdown` did not exit promptly. SHC-84 must
  qualify startup duration, restart policy, grace, and process exit together;
  a larger grace period alone is not acceptance.

- Observation: switching the requested policy to `OnDelete` during an active
  `RollingUpdate` target does not cancel the desired revision.
  Consequence: rollback passes when partition advancement stops, the same
  target operation reaches Completed, StatefulSet ownership returns to
  `OnDelete`, and any remaining desired-revision work proceeds through the
  controller's one-member-at-a-time path.

- Observation: `OnDelete` can report three updated replicas with every Pod on
  `updateRevision` while StatefulSet `currentRevision` retains the older hash.
  Consequence: the `OnDelete` completion oracle uses per-Pod revision labels
  and `updatedReplicas`, not equality of StatefulSet revision fields.

- Observation: the earlier campaign's planned target unavailability created
  error-level member-info logs, and recovery from a Pod update can emit a false
  scale-up event because ready replicas return from two to three.
  Consequence: the follow-up qualified lifecycle-aware log severity, corrected
  update-versus-scale phase reporting, and tied scale events to desired replica
  changes rather than transient readiness.

- Observation: cancellation cannot be modeled as merely changing the CR
  replica count. A member already withdrawn from traffic remains detained until
  a durable recovery operation releases it and verifies both local and captain
  member views.
  Consequence: the cancellation test records the target Pod UID, requires that
  membership removal has not begun, and proves `Up` registration before
  completion.

- Observation: a completed scale-down record can have the same intent, target
  Pod, ordinal, and desired revision as a later scale-down of a re-added
  member.
  Consequence: the repeated-scale test requires a new generation-scoped
  operation identity and a fresh `ValidatingCluster` transition.

- Observation: `StatefulSet.status.replicas` can temporarily decrease when a
  lower ordinal is absent even though a higher ordinal still exists and is
  healthy.
  Consequence: the test continuously checks every desired ordinal and rejects
  any false `OutOfOrderRevision` warning caused by truncating SHC member status
  to that temporary count.

- Observation: an external poll can miss the short state in which approval is
  persisted but replacement is not yet authorized.
  Evidence: the durable operation recorded approval at 17:04:17Z and
  authorization at 17:04:27Z, while the first post-approval Pod read occurred
  after replacement had begun.
  Consequence: branch-local tests assert the immediate reconcile barrier; EKS
  asserts timestamp ordering, exact audit signals, fresh safety observations,
  and no authorization before the durable approval.

- Observation: approval is a spec update and therefore increments CR
  generation even though it must not revise the Pod template.
  Consequence: LFC-006 records CR generation separately and asserts that
  wrong-token, stale-operation, and exact approval patches leave the
  StatefulSet update revision unchanged.

- Observation: withdrawal can cause Kubernetes to reuse a previous
  ControllerRevision. StatefulSet current/update equality can therefore
  precede completion of per-Pod rollback.
  Consequence: LFC-007 records every Pod revision and UID continuously and
  proves reverse-ordinal rollback independently of StatefulSet revision-field
  equality.

- Observation: in-place Splunk recovery and Kubernetes traffic readiness are
  separate observations.
  Consequence: the cancellation oracle does not allow the next rollback target
  until the retained captain Pod is again Ready and serving, even after the
  lifecycle operation reports completed recovery.

- Observation: the StatefulSet desired generation and its observed
  `status.updateRevision` can differ for a short interval after a CR template
  change or withdrawal.
  Consequence: qualification requires `WaitingForRevision` behavior with no
  target start, partition mutation, Pod deletion, or rollout-block diagnostic
  during that interval.

- Observation: the secure Operator metrics endpoint requires delegated
  Kubernetes authorization for the non-resource `/metrics` path.
  Consequence: EKS qualification uses an authenticated port-forward and a
  bounded test identity. Production monitoring must explicitly qualify its
  scrape RBAC.

- Observation: increasing an `OrderedReady` StatefulSet partition above an
  already-created failed ordinal does not remove that Pod.
  Consequence: SHC-80 recovery observes the higher recovery partition and
  revalidates every non-target peer before deleting only the failed authorized
  target. Qualification records both the failed Pod UID and the recovery Pod
  UID and proves Kubernetes recreated the ordinal at the last-known-good
  ControllerRevision.

- Observation: durable completed-operation history must remain available for
  support without continuing to control future destructive work.
  Consequence: a completed authorized-revision recovery exits the recovery
  deletion path before evaluating a later update revision. A source regression
  test proves queued work is released without deleting a Pod or changing the
  completed message.

- Observation: the fresh fixture briefly reported Kubernetes and CR readiness
  before image-owned SHC initialization, synchronization, and internal Splunk
  restarts completed. The readiness gates later withdrew all endpoints until
  the members recovered.
  Consequence: SHC-80 fault injection began only after sustained endpoint,
  member, captain, and search validation. The early-ready interval remains a
  separate startup-contract gap and cannot be treated as proof that the
  cluster is ready for disruption. The final SHC-81 fixture reproduced this
  sequence before converging to two consecutive clean health samples; SHC-83
  now tracks an explicit startup-complete traffic gate across the Operator,
  Docker-Splunk, and Splunk Enterprise.

- Observation: namespace-first deletion originally entered ordinary reconcile
  work after namespace termination, and successful finalizer removal was
  followed by inner and outer status writes against a deleted object.
  Consequence: SHC-81 routes deletion ahead of ordinary validation and
  configuration, treats already-absent owned objects as success, creates no
  namespace content, applies declared PVC retention, and stops both status
  writers after successful finalization. The accepted EKS run began direct
  namespace deletion at `2026-07-30T00:05:34Z`, observed the CR absent about
  11 seconds later, and observed the namespace and all exact PVs absent about
  40 seconds after the request. The bounded log audit found no
  `NamespaceTerminating`, `StorageError`, precondition, configuration, delete,
  or stalled-condition-write failure.

- Observation: the generic outer condition-writer shape corrected for the
  SearchHeadCluster controller also appears structurally in other enterprise
  reconcilers.
  Consequence: retain an Operator-wide source and behavior audit as a separate
  follow-up. SHC-81 proves only the SearchHeadCluster deletion path and does
  not infer a failure in controllers that were not exercised.

- Observation: SHC recovery and withdrawal counters are process-local. The
  deliberate Operator restart retained durable CR status and Events but reset
  those counters; the final process correctly reported only the later queued
  rollout transitions.
  Consequence: qualification and support must correlate durable operation
  status, Kubernetes Events, structured logs, and persisted Prometheus series.
  A zero counter after a controller restart is not evidence that recovery did
  not occur.

- Observation: the current `make deploy` target depends on `uninstall`, which
  deletes the Splunk Enterprise CRDs before deployment.
  Consequence: live-fixture image qualification updates only the Operator
  Deployment and verifies its image digest. The Make target must be redesigned
  or guarded before it can represent an in-place Operator upgrade procedure.

## Decision Log

- Decision: record internal splunkd identity and all four Kubernetes traffic
  signals separately for every App Framework run: container readiness, Pod
  readiness, the Operator SHC-serving gate, and EndpointSlice readiness.
  Rationale: Splunk can restart inside an unchanged container. Pod UID and
  Kubernetes restart count alone missed the entire `2 -> 1 -> 0` restart, and
  container readiness alone did not reveal the later cluster-wide gate
  withdrawal.
  Date: 2026-07-30 UTC.

- Decision: OPS-011 passes only with continuous customer-visible evidence, not
  merely a successful bundle command or final healthy cluster.
  Rationale: the reported concern is temporary service disruption. The test
  must retain timestamped ingest acknowledgements and search job outcomes,
  verify no silently partial result, track Ready service capacity and exact
  member/peer restart order, and correlate those observations with replication
  factor, search factor, all-data-searchable, captain, and rolling-restart
  state. A concurrent Kubernetes rollout or insufficient redundancy must hold
  the App Framework operation. A force mode is a separate explicit fault case,
  not the default success path.
  Date/Author: 2026-07-29, planning team.

- Decision: a scenario passes only when service, Splunk cluster, Kubernetes
  rollout, and diagnostic invariants all pass.
  Rationale: Pod phase alone cannot distinguish a safe rejoin from a running
  but detained, unregistered, stale, or wrong-revision member.
  Date/Author: 2026-07-24, planning team.

- Decision: validate the new orchestrator under `OnDelete` before testing
  `RollingUpdate`.
  Rationale: changing lifecycle semantics and rollout ownership in one step
  makes failures difficult to attribute and rollback.
  Date/Author: 2026-07-24, planning team.

- Decision: real-time and historical search drain are separate assertions.
  Rationale: they have different lifetime semantics and must not be hidden
  behind one combined zero count.
  Date/Author: 2026-07-24, planning team.

- Decision: test forced deletion and node loss as recovery, not graceful
  shutdown.
  Rationale: Kubernetes does not guarantee `preStop` on those paths.
  Date/Author: 2026-07-24, planning team.

- Decision: the spike uses a three-member SHC as the minimum HA topology and
  adds a five-member run for concurrency and scale confidence.
  Rationale: three members exercise majority and captain movement economically;
  five members catch assumptions tied to a fixed three-ordinal layout.
  Date/Author: 2026-07-24, planning team.

- Decision: test `Parallel` first formation and simultaneous persistent cold
  restart as separate scenarios.
  Rationale: first formation requires exactly one bootstrap action, while cold
  restart requires zero cluster-forming actions and must leave existing
  splunkd processes alive.
  Date/Author: 2026-07-25, planning team.

- Decision: final manual qualification uses one immutable Operator feature
  branch and one immutable Docker-Splunk feature branch whose build resolves
  the integrated Splunk Ansible commit through an exact source ref.
  Rationale: a test result is not reproducible if its runtime behavior depends
  on unrecorded child branches or a dirty nested checkout.
  Date/Author: 2026-07-25, planning team.

- Decision: build and qualify the Docker-Splunk runtime image on a supported
  Linux builder, not on the macOS source workstation.
  Rationale: the existing Makefile target and container runtime assumptions
  are Linux-specific. Treating a Mac-side source check as an image result would
  create false evidence.
  Date/Author: 2026-07-25, planning team.

- Decision: store each concrete freeze manifest with the test artifacts after
  the source commits are final, rather than committing it into a source branch.
  Rationale: committing a manifest that names its own repository HEAD
  immediately invalidates that recorded SHA.
  Date/Author: 2026-07-25, planning team.

- Decision: the first strategy migration sets `RollingUpdate.partition` equal
  to replicas and makes no simultaneous Pod-template change.
  Rationale: migration must prove that Kubernetes rollout ownership can change
  without authorizing replacement.
  Date/Author: 2026-07-28, qualification team.

- Decision: a controller-restart test passes only if operation ID, target
  ordinal, target Pod UID, desired revision, and durable stage are identical
  before and after the restart.
  Rationale: completion alone cannot rule out duplicate lifecycle intent.
  Date/Author: 2026-07-28, qualification team.

- Decision: cluster identity and member identity are separate assertions.
  Rationale: the shared `[shclustering] id` must remain stable across the
  campaign, and each retained ordinal must also return with its original
  `instance.cfg` GUID.
  Date/Author: 2026-07-28, qualification team.

- Decision: a qualification fixture must pass five continuous minutes of
  Kubernetes and Splunk-internal stability before mutation and after final
  recovery.
  Rationale: this catches the interval where Kubernetes reports Ready while
  Splunk is still performing its own rolling restart.
  Date/Author: 2026-07-28, qualification team.

- Decision: strategy rollback is accepted only after the active durable
  operation completes recovery and the StatefulSet then changes to
  `OnDelete`.
  Rationale: immediately changing ownership while a partition-authorized Pod
  is recovering can abandon state or allow ambiguous responsibility.
  Date/Author: 2026-07-28, qualification team.

- Decision: scale-down cancellation passes only before membership removal and
  only when the original Pod UID remains present.
  Rationale: a changed Pod identity or removed consensus member requires a
  different recovery workflow and must fail closed.
  Date/Author: 2026-07-28, qualification team.

- Decision: Pod-update cancellation passes only before replacement
  authorization and only when the original Pod UID remains present.
  Rationale: restoring service is safe only while Kubernetes has not replaced
  the member. The test must prove durable detention release, retained identity,
  recovery in local and captain views, unchanged partition, zero current search
  counts, and exactly one cancellation Event.
  Date/Author: 2026-07-28, qualification team.

- Decision: final scale acceptance requires correct semantic signals in
  addition to successful convergence.
  Rationale: a false scale direction, expected-lifecycle error, or transient
  rollout-block warning would mislead automation and Support even if the
  cluster eventually recovered.
  Date/Author: 2026-07-28, qualification team.

- Decision: LFC-006 always tests wrong token and stale operation ID before the
  exact approval.
  Rationale: a successful exact path alone does not prove that the handshake
  prevents preapproval or replay.
  Date/Author: 2026-07-28, qualification team.

- Decision: approval acceptance requires one status record, one Normal Event,
  one structured log, and a counter delta of exactly one.
  Rationale: the customer decision must be supportable after the transient Pod
  and search state are gone, without unbounded metric labels.
  Date/Author: 2026-07-28, qualification team.

- Decision: continuation qualification keeps every non-search replacement
  invariant active and completes the remaining reverse-ordinal rollout.
  Rationale: approval to interrupt active work is not approval to bypass
  captain transfer, target identity, one-member availability, rejoin, or final
  stability.
  Date/Author: 2026-07-28, qualification team.

- Decision: failed captain transfer qualification holds the blocked state for
  a bounded observation period before withdrawing the revision.
  Rationale: an instantaneous observation cannot prove that the controller
  remains fail closed or that warning Events are deduplicated.
  Date/Author: 2026-07-28, qualification team.

- Decision: revision-withdrawal evidence must include Event deltas and a
  bounded Operator-log window, not only final convergence.
  Rationale: a cluster can recover while transient false diagnostics still
  mislead automation and Support.
  Date/Author: 2026-07-28, qualification team.

- Decision: ControllerRevision reuse is qualified with per-Pod revision and
  reverse-ordinal assertions.
  Rationale: StatefulSet status equality alone cannot prove that rollback
  respected the one-member lifecycle.
  Date/Author: 2026-07-28, qualification team.

- Decision: automatic post-authorization recovery is limited to one failed
  target whose revision is absent from every non-target Pod and whose peers
  are Ready, serving, and at the last-known-good revision.
  Rationale: this proves a single-target rollback boundary. A partial rollout,
  uncertain peer state, or active image upgrade remains fail closed rather
  than broadening recovery into an implicit multi-member rollback.
  Date/Author: 2026-07-29, qualification team.

- Decision: recovery completion releases future queued work but does not erase
  the completed lifecycle record.
  Rationale: support needs durable evidence of the withdrawal, while a
  historical operation must never authorize deletion against a later
  StatefulSet revision.
  Date/Author: 2026-07-29, qualification team.

## Outcomes & Retrospective

The first integrated positive-path campaign, one active-strategy rollback, the
scale lifecycle extension, targeted real-time and historical search-drain
scenarios, and one audited continuation are complete on EKS. A pinned Linux
runtime image formed a
three-member SHC,
completed an Operator-managed `OnDelete` rollout, migrated safely to
partition-gated `RollingUpdate`, completed the reverse-ordinal rollout,
transferred captaincy before captain replacement, preserved all retained
member identities, resumed the same durable operation after the Operator Pod
was deleted, safely returned to `OnDelete` while ordinal two was actively
recovering, cancelled a pre-membership-removal scale-down, repeated that
scale-down with fresh identity, and completed final `3 -> 4` and `4 -> 3`
cycles. The final image emitted no false rollout block, false scale direction,
or expected-lifecycle error, and passed a five-minute combined Kubernetes and
Splunk stability window. The later search-drain image also failed closed on a
real-time timeout, recovered a cancelled Pod update in place, and delayed a
historical-search replacement until the active count reached zero. The SHC-74
image rejected wrong-token and stale-operation approvals, accepted one exact
post-timeout decision with a one-search audit snapshot, then completed
ordinals `2 -> 1 -> 0`, transferred captaincy, and passed a 312-second
post-action stability gate with zero container restarts.
The SHC-75 image additionally failed closed on captain-transfer timeout,
retained the original captain Pod, restored it in place after revision
withdrawal, completed deterministic rollback, emitted only the expected
warning and cancellation signals, and passed a 321-second final stability
gate.
The SHC-80 image additionally recovered one already-authorized,
unschedulable target at its last-known-good revision across a real Operator
restart, then released the superseding queued revision and completed a
captain-safe `2 -> 1 -> 0` rollout with all persistent GUIDs preserved. Its
complete monitor recorded 187 successful searches and zero failures, and the
converged cluster passed 369 seconds of final stability.

The complete `OnDelete` and `RollingUpdate` gates remain open because the
required repetitions, full-campaign soak around the new search cases,
additional timeout classes, forced disruption, scheduling/storage/network
faults, version skew, other post-authorization and partial-rollout variants,
and other rollback-under-failure cases have not passed. The earlier log-severity,
phase, scale-event, higher-ordinal observation, cancelled-update recovery,
stale search-count, unaudited continuation, failed-transfer, revision-reuse,
and stale-StatefulSet-observation defects are corrected for the tested paths.
The campaign establishes that the integrated components can execute the
intended happy path, active-operation rollback, replica-count lifecycle,
fail-closed drain and captain transfer, and one explicitly audited
interruption; it is not evidence for default enablement.

## Context and Orientation

The Splunk Operator creates a Kubernetes StatefulSet for Search Heads. A
StatefulSet gives each Pod a stable ordinal and persistent-volume identity.
The current Operator uses `OnDelete`, meaning Kubernetes records a new
StatefulSet revision but waits for the Operator to delete each old Pod. The
target design eventually uses `RollingUpdate` with a `partition`, an ordinal
boundary controlled by the Operator. Lowering the partition authorizes
Kubernetes to replace the next Pod; therefore the Operator must not lower it
until Splunk-specific safety work is complete.

A captain is the Search Head member currently coordinating cluster work.
Captaincy is dynamic and is not permanently assigned to ordinal zero.
Detention prevents a member from accepting new search work. Rejoin means a
restarted member with retained storage returns using its existing identity;
ordinary rejoin is not new-member bootstrap and must not remove and recreate
consensus membership.

Relevant Operator paths at the observed baseline are:

- `api/enterprise/v4/common_types.go` and
  `api/enterprise/v4/searchheadcluster_types.go`;
- `pkg/splunk/enterprise/configuration.go`;
- `pkg/splunk/enterprise/searchheadclusterpodmanager.go`;
- `pkg/splunk/splkcontroller/statefulset.go`;
- `pkg/splunk/client/splunk/splunkclient.go`;
- `pkg/splunk/client/metrics/metrics.go`;
- `tools/k8_probes/`;
- `internal/controller/enterprise/searchheadcluster_controller_test.go`;
- `pkg/splunk/enterprise/searchheadcluster_test.go`;
- `pkg/splunk/splkcontroller/statefulset_test.go`;
- `test/testenv/search_utils.go`;
- `test/testenv/search_head_cluster_utils.go`;
- `test/smoke/`; and
- `kuttl/tests/upgrade/c3-with-operator/`.

The current Docker-Splunk checkout contains
`splunk/common-files/entrypoint.sh` and
`tests/test_distributed_splunk_image.py`. The current splunkd checkout contains
the SHC unit-test definitions in `src/shpooling/tests/CMakeLists.txt`, including
manual detention, captain, member, Raft configuration, and searchable rolling
restart tests.

## Test Architecture

### Layer 0: static and generated artifacts

Run on every implementation branch. It proves formatting, generated CRDs,
deep-copy code, Helm rendering, validation schemas, shell syntax, and metric
label policy. It must detect generated-file drift and unintentional API
changes.

### Layer 1: isolated unit and contract tests

Run on every implementation branch. The Operator uses fake Kubernetes clients,
fake clocks, and scripted Splunk API responses. The runtime repository invokes
lifecycle scripts with a fake `splunk` executable so signal, lock, timeout, and
state-file behavior can be tested without a full image. splunkd changes, if
required, extend the existing SHC C++ suites.

Every state-machine transition gets success, transient failure, terminal
failure, timeout, retry, stale observation, and Operator-restart cases.

### Layer 2: controller integration with envtest

Run on every Operator merge request into the integration branch. Envtest starts
a real Kubernetes API server and etcd without kubelet Pods. It proves CR
validation, reconciliation, StatefulSet rendering, durable status, conditions,
partition changes, and resume after a new reconciler instance.

It cannot prove kubelet probes, container signals, storage attachment, Service
traffic, or actual Splunk behavior.

### Layer 3: runtime image contract

Run when the runtime branch changes on a supported Linux builder with a Linux
container engine and access to the pinned Splunk artifact. Build the selected
image with a pinned Splunk Ansible commit and Splunk build. Test direct TERM,
hook-then-TERM,
concurrent triggers, stopping-state transitions, failure, timeout, and
persistent restart. Then run a three-Search-Head distributed image scenario
that covers all fresh-start orderings, interrupted formation, single retained
member restart, simultaneous retained-member cold restart, ordinal-zero
unavailability, and dynamic deployer targeting. A persistent member with
inconclusive runtime APIs must leave splunkd running and execute no
cluster-forming command.

### Layer 4: Kubernetes integration with a real SHC

Run on the Operator integration branch. Deploy one Cluster Manager, an Indexer
Cluster sufficient to execute searches, one deployer, and a three-member SHC
with persistent volumes. Use the feature gate to run the same lifecycle suite
first under `OnDelete`, then under partition-gated `RollingUpdate`.

Continuous observers run independently from the controller:

- local readiness for every member;
- Service-selected traffic;
- captain identity and service readiness;
- member identity, registration, status, and detention;
- historical and real-time active search counts;
- search job completion and results;
- StatefulSet current/update revisions and partition;
- Pod UID, revision, readiness, node, and restart count;
- PVC identity and attachment;
- CR operation stage and conditions;
- Kubernetes Events;
- Operator/runtime logs; and
- Prometheus metrics.

### Layer 5: disruption and cloud qualification

Run after Layer 4 passes. Exercise Eviction, direct and forced deletion, node
loss, network partitions, scheduler pressure, image-pull failure, volume delay,
Operator loss, service mesh, TLS, and provider-specific behavior. Execute the
P2 matrix on supported EKS, AKS, GKE, OpenShift, and the minimum and latest
qualified Kubernetes versions.

## Plan of Work

First, create clean worktrees and record immutable baseline commits. The
Operator branch follows `ParallelWorkstreamPlan.md`. Do not reuse the current
dirty Operator or Docker-Splunk worktree for builds.

Second, add a new Ginkgo suite at `test/shc_lifecycle/`. Label the branch-gate
subset `tier:e2e-pr && feature:shc-lifecycle` and the full suite
`tier:e2e-full && feature:shc-lifecycle`. Extend `test/testenv/` with helpers
that discover a reachable member dynamically, create historical and real-time
search jobs, inspect captain/member endpoints, watch EndpointSlices, record
StatefulSet revisions/partition, and collect sanitized lifecycle evidence.

Third, add fake-clock and fake-Splunk-response tests around the new SHC workflow
package. Avoid wall-clock sleeps in unit and envtest tests. Timeouts advance the
fake clock and assert the durable stage/reason result.

Fourth, add runtime script tests. Extract shutdown behavior from the entrypoint
into a focused script with an explicit interface. Test it with a temporary
state directory and fake Splunk command on the source workstation before
building an image. Transfer only pushed commits and a manifest of immutable
inputs to the Linux builder. The distributed-image test there proves that a
retained Search Head restart does not repeat initial cluster formation. It must
also stop or delay the bootstrap seed to vary first-start ordering, restart all
retained members together, inspect the Ansible task/action evidence, and prove
splunkd stays alive when captain and member APIs are temporarily inconclusive.

Fifth, capture a current-behavior baseline with the feature disabled. Record
current readiness during detention, captain replacement behavior, 30-second
default termination behavior where applicable, shutdown duration, and current
diagnostic gaps. Baseline failures are expected evidence, not release success.

Sixth, qualify the feature-gated lifecycle under `OnDelete`. Execute all P0
API, health, lifecycle, runtime, rejoin, StatefulSet baseline, and observability
scenarios. Repeat each destructive scenario at least three times and run one
twenty-cycle soak to reveal leaked detention, duplicate operations, or
increasing duration.

Seventh, enable partition-gated `RollingUpdate`. Begin with an existing
`OnDelete` StatefulSet whose template revision is stable. Migrate strategy with
a partition that prevents immediate replacement. Trigger an image-only revision
and verify reverse-ordinal progression. Inject an Operator restart and a rejoin
failure before partition advancement. Rehearse rollback before continuing.

Eighth, run P1 disruptions, upgrade/version skew, scale, App Framework, and
storage scenarios. Then run P2 provider qualification and five-member soak.

Ninth, freeze integration inputs. Merge qualified Operator child work into one
Operator feature branch. Merge runtime shutdown plus the qualified Splunk
Ansible startup and deployer work into one Docker-Splunk feature branch, with
the Splunk Ansible integration commit pushed and selected through an immutable
Docker-Splunk source ref. On the Linux builder, build immutable images and
record all source commits, builder identity, and image digests before the
manual campaign.

Finally, summarize measured stage-duration distributions and failure
classifications. Use evidence—not the original proposed numbers—to recommend
probe thresholds, drain timeout, captain-transfer timeout, rejoin timeout,
termination grace, alert tolerance, and whether opt-in/default enablement is
safe.

## Concrete Steps

Run Operator commands from a clean worktree rooted at the integration or child
branch:

    git fetch sok develop
    git rev-parse sok/develop
    make manifests
    make generate
    make fmt
    make vet
    make test-unit
    make test-integration
    make helm-test
    make build

Expected result: commands exit zero, generated files are clean, and JUnit plus
coverage artifacts identify the exact commit.

Run targeted Operator packages while developing:

    go test ./pkg/splunk/enterprise/... -count=1
    go test ./pkg/splunk/splkcontroller/... -count=1
    go test ./pkg/splunk/client/splunk/... -count=1
    go test ./pkg/splunk/workflow/shc/... -count=1

The last command becomes valid when the workflow implementation and tests are
added. Expect each command to exit zero without a live cluster.

After deploying the pinned Operator and runtime images to the dedicated test
cluster, run:

    TEST_LABELS='tier:e2e-pr && feature:shc-lifecycle' test/run-tests.sh

For the complete suite:

    TEST_LABELS='tier:e2e-full && feature:shc-lifecycle' test/run-tests.sh

The test runner must print the Operator digest, runtime digest, Splunk build,
Kubernetes version, feature gates, namespace, scenario IDs, and artifact
directory before changing the cluster.

Do not run the Docker-Splunk image targets on the macOS source workstation.
First create a handoff manifest containing the pushed Docker-Splunk commit,
full Splunk Ansible commit, Splunk version/build, requested Linux architecture,
image target, build arguments, and expected image name. Copy
`RuntimeLinuxBuildHandoffManifest.example.yaml` into the run artifact directory
and replace every placeholder before starting the Linux job.

On a clean supported Linux builder, fetch those exact commits and verify the
manifest before running Docker-Splunk. The builder must have a Linux container
engine, access to the pinned Splunk artifact and required package repositories,
and enough capacity for the distributed test. Exact platform and image target
are pinned in the run manifest. Use the Red Hat 8 target for the initial spike
because it matches the current enterprise-image family, then add other
supported image families during compatibility qualification:

    make splunk-redhat-8 IMAGE_VERSION=shc-lifecycle-<source-sha>
    make test_setup
    pytest -vv tests/test_shc_lifecycle.py
    pytest -vv \
      tests/test_distributed_splunk_image.py::TestDockerSplunk::test_compose_1idx3sh1cm1dep \
      --platform=redhat-8

The new focused test must run before the existing full distributed test.

Before the Docker-Splunk image build, run from the clean integrated
Splunk Ansible worktree:

    python3 -m unittest tests.small.test_shc_lifecycle -v
    python3 -m unittest tests.small.test_shc_ready -v
    ansible-playbook --syntax-check site.yml
    python3.11 -m venv <lint-venv>
    <lint-venv>/bin/pip install -r tests/requirements-shc-lint.txt
    ansible-lint -c tests/ansible-lint.cfg \
      roles/splunk_search_head/tasks \
      roles/splunk_deployer/tasks

Run the lint command through `<lint-venv>/bin/ansible-lint`. The isolated
requirements reproduce the repository's legacy numeric rules; current
ansible-lint modernization is not part of the SHC qualification gate.

The Docker-Splunk build manifest records the exact resolved Splunk Ansible
commit. Source preparation fails if the ignored nested checkout contains local
changes or the requested ref cannot be resolved.

If splunkd code changes, start from a current build and run the affected SHC
suites using the repository-supported runner:

    python3 cmake/build_and_ctest.py -v -O \
      '^(shc_test_manual_detention|shc_captain_test|SHPCaptainTest|SHPMemberTest|shc_test_searchable_rolling_restart)$'

The Search Head team must confirm and record any additional suite required for
the changed endpoint or consensus behavior.

## Evidence and Artifact Layout

Each test run writes:

    artifacts/shc-lifecycle/<run-id>/
      manifest.json
      scenario-results.json
      timeline.jsonl
      kubernetes/
      operator/
      runtime/
      splunk/
      metrics/
      searches/
      redaction-report.json

`manifest.json` contains immutable source commits, image digests, Splunk build,
the resolved Splunk Ansible source commit, Kubernetes version, cluster
provider, storage class, network mode, feature gates, test command, start/end
time, scenario selection, builder operating system and architecture, container
engine/version, and build-log location.

`timeline.jsonl` records normalized observations with monotonic and wall-clock
timestamps. It must be possible to calculate time spent in detention, drain,
captain transfer, termination, scheduling, storage, container startup, member
rejoin, and validation.

For startup and rejoin scenarios, the runtime evidence records a sanitized
classification and selected action for every member: fresh bootstrap seed,
fresh joiner, established-member rejoin, interrupted-formation resume, or
ambiguous-persistent await-rejoin. It also records whether any
cluster-forming command was attempted and whether splunkd remained responsive.
This evidence is required for STS-012 and must not include credentials, raw
authentication material, or unredacted management API responses.

Artifacts contain metadata and sanitized summaries, never Secret data,
authorization headers, private keys, passwords, session tokens, or customer
search text. The redaction test deliberately injects recognizable canary
secrets and fails if they appear.

## Validation and Acceptance

The branch-local gate passes when Layer 0 through Layer 2 tests pass and the
branch's owned P0 scenarios have deterministic automated coverage.

The runtime gate passes when RUN-001 through RUN-009 pass, STS-012 proves
fresh and persistent parallel startup, OPS-005 passes through the image-owned
deployer path with ordinal zero unavailable, and a retained three-member
restart uses rejoin or await-rejoin behavior without repeating formation.
Mac-side source checks cannot satisfy this gate; its evidence must identify the
Linux builder and immutable runtime image digest.

The integrated `OnDelete` gate passes when all P0 scenarios except
RollingUpdate-specific STS-005 through STS-010 pass three consecutive times,
the twenty-cycle soak has no concurrent planned disruption or stuck detention,
and every failure can be attributed to a stage.

The `RollingUpdate` gate passes when STS-005 through STS-010 and the remaining
P0 scenarios pass three consecutive times, partition history proves one
authorized ordinal at a time, and rollback is successful.

Production opt-in requires all P0 and P1 scenarios for supported version
combinations. Default enablement additionally requires applicable P2 provider
qualification, measured alert thresholds, published runbooks, and an approved
list of known exclusions.

Continuous ad-hoc search success, scheduled-search behavior, and searches
already running on the target are reported separately. A good aggregate
availability result cannot hide loss or duplication in one of those classes.

No gate may waive a failed captain-transfer, persistent-identity,
multiple-planned-unavailable, uncontrolled-partition, credential-leak, or
ordinary-restart-membership-removal assertion.

## Idempotence and Recovery

Tests create a unique namespace and run ID. Re-running a scenario must either
reuse its healthy fixture intentionally or create a new namespace. Fault
injectors label every resource they change and record the original value before
mutation.

After each scenario, restore network policy, scheduling constraints, image,
feature gates, replica count, StatefulSet strategy/partition, and any paused
controller. Confirm all members are `Up`, detention is released, captain is
ready, and no lifecycle operation remains active.

If cleanup cannot prove cluster health, preserve the namespace and artifacts,
mark the environment quarantined, and do not run the next destructive
scenario. Never recover a test by deleting all Search Head Pods or removing
consensus membership.

Cloud resources are deleted only after artifacts are uploaded and the
environment manifest confirms the exact cluster target.

## Artifacts and Notes

Record short evidence here as gates run. Include scenario IDs, run IDs, image
digests, and links or paths to sanitized artifacts. Do not paste raw credentials
or entire support bundles.

2026-07-28 integrated EKS campaign:

- Operator commit:
  `22ab2ca0c50de8b0d727a301c3db0d39ab5b61bc`;
- Docker-Splunk commit:
  `6376b01116da5bb68ac1e4534cc60ea422bf94c7`;
- Splunk Ansible commit:
  `9954434703c776665713e9ed7d1a3d1d5dd1c77d`;
- Operator image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-reliability-22ab2ca0c`;
- runtime image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk:shc-prestart-6376b01-ansible-9954434-splunk-10.6.0.0-d9be152689b7`;
- runtime image digest:
  `sha256:f2c8bc7aefd5d060ec396f2cbdd49d28dcdf04ce3d91ebeffc42caf069bbf955`;
- feature gates:
  `SplunkPodLifecycle=true,SearchHeadClusterLifecycle=true`;
- `OnDelete` revision transition:
  `splunk-shc-lifecycle-search-head-69b76b7f7` to
  `splunk-shc-lifecycle-search-head-d8dfd64bb`;
- `RollingUpdate` happy-path revision:
  `splunk-shc-lifecycle-search-head-5455bd75c8`, with partition history
  `3 -> 2 -> 1 -> 0 -> 3`;
- controller-restart revision:
  `splunk-shc-lifecycle-search-head-75456fb44f`;
- interrupted ordinal-two operation:
  `PodUpdate:splunk-shc-lifecycle-search-head-2:splunk-shc-lifecycle-search-head-75456fb44f`;
- Operator Pod UID changed from
  `dbf66ce1-b9b8-4138-8ef0-bc9c6de36bd7` to
  `36882c35-9993-4ba0-a872-fb227afe5b40`; the operation ID, ordinal, target
  Pod UID, desired revision, and `WaitingForTermination` stage were preserved;
- shared SHC ID:
  `0E720A3E-610C-4FFE-8765-3188DA79045E`;
- retained member GUIDs for ordinals zero through two:
  `74FEAA89-32D8-4A7E-B29B-15355A4A5D82`,
  `CECD7C09-03D7-42B2-A88F-BB10142F783B`, and
  `DFA6576A-540E-43E0-BCFB-E69157648CA9`;
- final state: all three members Up, dynamic captain on ordinal one, service
  ready, no cluster rolling restart, no KV Store maintenance, StatefulSet
  current/update revisions equal, partition three, three ready/updated
  replicas, and zero Pod restarts; and
- final runtime log checks found zero repeated SHC initialization tasks, zero
  restart-handler executions, and zero fatal Ansible results on every Search
  Head. The test namespace and its PVCs were deleted after collection.

2026-07-28 active rollback extension:

- corrected Docker-Splunk commit:
  `7951d69f82b28d92b118432bea4a513a90a76749`;
- corrected runtime image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk:shc-prestart-7951d69-ansible-9954434-splunk-10.6.0.0-d9be152689b7`;
- corrected runtime digest:
  `sha256:c295389a5bbcaa0aade25b0a5950952794179059564a525a7200b6f1c26b3547`;
- Docker-Splunk `make test_shutdown` passed seven tests, and an EKS TERM smoke
  Pod exited completely in 16 seconds with a 1,200-second grace period;
- the fresh SHC did not enter qualification on its first Ready state because
  Splunk still reported an internal rolling restart; the fixture passed five
  continuous minutes of combined Kubernetes and Splunk stability first;
- rollback target revision:
  `splunk-shc-rollback-search-head-85fcdcbdc5`;
- rollback operation:
  `PodUpdate:splunk-shc-rollback-search-head-2:splunk-shc-rollback-search-head-85fcdcbdc5`;
- the rollback request was made in `WaitingForContainer`; StatefulSet strategy
  remained `RollingUpdate` at partition two and the same operation ID, target
  Pod UID, replacement Pod UID, desired revision, and member GUID remained
  durable through recovery;
- ordinal two reached Completed and `RollbackPending` before the StatefulSet
  changed to `OnDelete`; ordinals one and zero then completed sequentially,
  with no more than one withdrawn or deleting Search Head at any sample;
- captaincy transferred from ordinal zero to ordinal one before ordinal zero
  was authorized for deletion;
- shared SHC ID remained
  `99F446B4-C28F-423B-8316-53796A674385`; retained member GUIDs remained
  `184EEC06-3DBE-40D6-9915-3930E4667E20`,
  `1BEAEBDD-DC2A-474E-ACF9-B75D48DB50E2`, and
  `9B7CD01A-C397-4C29-AD10-0652E56FB5A8`;
- all three final Pods carried the target revision, were Ready and serving,
  had zero restarts, and passed five continuous minutes with all members Up,
  `service_ready_flag=1`, `rolling_restart_flag=0`, and KV Store maintenance
  disabled;
- current final-container logs contained zero repeated SHC initialization
  tasks, zero restart-handler executions, and zero fatal or panic results; and
- supportability follow-up was required because expected target unavailability
  was logged repeatedly at error level and the final Pod recovery emitted a
  false `ScaledUp 2 to 3` event. That follow-up is completed in the extension
  below.

2026-07-28 SHC scale-lifecycle extension:

- final Operator commit:
  `ccab4fe332e8dfc4a3b14a8ead60d5fe46f323cd`;
- final Operator image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-reliability-ccab4fe33`;
- final Operator image digest:
  `sha256:b79ae3f5d81ac1fcc48f998aad08ecda9c6d63fc68f30f745d4aa8c53c8ce96c`;
- runtime image and digest remained
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk:shc-prestart-7951d69-ansible-9954434-splunk-10.6.0.0-d9be152689b7`
  and
  `sha256:c295389a5bbcaa0aade25b0a5950952794179059564a525a7200b6f1c26b3547`;
- the cancellation path preserved the original ordinal-three Pod UID and
  completed only after local and captain observations both reported the member
  `Up`;
- a later scale-down of the same ordinal started a new generation-scoped
  operation rather than reusing the completed cancellation record;
- the final `3 -> 4` run progressed through `ScalingUp`, validated ordinal
  three in both SHC views, rolled the peer-list revision across ordinals two,
  one, and zero, and emitted one `ScaledUp` completion;
- the final `4 -> 3` run withdrew ordinal three, removed SHC membership before
  StatefulSet reduction, deleted ordinal-three storage under the configured
  policy, rolled the peer-list revision across ordinals two, one, and zero,
  and emitted one `ScaledDown` completion;
- every sampled scale and rollout interval had at most one withdrawn or
  deleting Search Head and zero container restarts;
- event/log review for the final cycles found no false scale direction, no
  false `OutOfOrderRevision`, and no error for the intentionally unavailable
  scale-down target. Intentional membership departure remained visible as
  warning-level lifecycle context; and
- the final 300-second gate passed 17 samples with CR phase `Ready`, stable
  replicas three, StatefulSet current/update revisions equal, partition three,
  three ready/serving members at the target revision, one ready captain, all
  members `Up`, no pending configuration replication, no Splunk rolling
  restart, disabled KV Store maintenance, and zero container restarts.

2026-07-28 SHC search-drain and cancellation extension:

- Operator image source:
  `5783e5b695d3912e6b0a82017947d432e87f7d10`, following
  `23bdb631b423b38ec4ad835b1436947eb52cae26`;
- Operator image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-reliability-5783e5b69`;
- Operator image digest:
  `sha256:986fc45f85ad073d6ac377a8c0b2becc1ebba6aad9620dc17017220dc3f574bf`;
- `make fmt`, `make vet`, `make build`, and `make test` passed on the Linux
  builder. All 41 Ginkgo suites passed, including 154 controller envtest cases,
  with 78.5 percent composite coverage;
- in the real-time scenario, the durable operation and target-member status
  both reported one active real-time search. The 30-second timeout reached
  `Blocked/SearchDrainTimedOut` while the original Pod UID and revision
  remained unchanged, StatefulSet partition remained three, the serving
  readiness gate and EndpointSlice were false, and the search was still
  running;
- exactly one `SHCRolloutBlocked` Event described the real-time count. After
  the search was cancelled and the requested revision withdrawn, the same Pod
  returned to Ready and serving, both operation and member search counts were
  zero, and exactly one `SHCPodUpdateCancelled` Event was present;
- in the historical scenario, a bounded Splunk QA command produced one active
  historical search. The original Pod and partition remained unchanged during
  drain, and replacement authorization occurred only after the count reached
  zero;
- the resulting rollout replaced ordinals `2 -> 1 -> 0`, recovered each member
  before advancing, transferred captaincy from ordinal zero to ordinal one
  before the final replacement, and ended with all members `Up`, three
  ready/updated replicas, matching StatefulSet revisions, and partition three;
- both fixtures required a 120-second targeted runtime-stability gate after
  reported readiness because final image-owned synchronization briefly cycled
  management endpoints. That targeted gate detected the race and made the
  scenario deterministic; the documented five-minute gate still applies to
  complete campaign acceptance before release; and
- test namespaces, PVCs, and associated PVs were removed after evidence
  collection.

2026-07-28 audited search-drain continuation extension:

- Operator source was
  `54a5aae3cd5f0970daee7591c24704b4111a3282`; image tag was
  `shc-reliability-54a5aae3c`; and ECR digest was
  `sha256:f54427c0497edb09ba42f584641bb323a2f81b5874460f5ef04e2ac92d00bbcf`;
- Linux `make fmt`, `make vet`, `make build`, and `make test` passed. The final
  test run completed all 41 Ginkgo suites, including 154 controller envtest
  cases, with 78.5 percent composite coverage;
- a fresh three-member fixture passed a five-minute pre-action gate with
  matching revisions, partition three, three `Up` members, a service-ready
  captain, zero pending configuration replication, and zero container
  restarts;
- a real-time search remained active through the 30-second timeout. The
  original target UID and revision remained intact, partition remained three,
  Pod and EndpointSlice serving readiness were false, and exactly one
  `SHCRolloutBlocked` Event described historical zero and real-time one;
- the wrong-token and stale-operation approvals changed CR generation but
  changed neither the blocked operation nor StatefulSet update revision. They
  emitted no approval Event and did not increment the approval counter;
- the exact operation and token recorded approval generation five, zero
  historical and one real-time search, approval time 17:04:17Z, and later
  replacement authorization time 17:04:27Z. Exactly one approval Event, one
  bounded structured log, and one unlabelled counter increment were observed;
- the rollout completed `2 -> 1 -> 0` with a maximum of one unavailable member,
  three partition advances, three target starts, no container restart, and
  captain transfer from ordinal zero to ordinal one before the last
  replacement;
- a 312-second post-action gate continuously observed three ready and serving
  Pods, three registered `Up` members, initialized/service-ready SHC state,
  matching current/update revisions, partition three, zero pending
  configuration replication, and local management reachability; and
- the namespace, PVCs, and all eight associated PVs were deleted after
  sanitized evidence collection.

2026-07-28 LFC-007 captain-transfer-timeout qualification:

- Operator source commits were
  `eb6907ee51f0655742f2096f8137b55c484792d6`,
  `44ccac31e9aaa0540678d090b3222a5e2a1df1ef`, and
  `3e9e735a776eb90957a0d0d2722b28ce0da5baff`.
  The final image was
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-reliability-3e9e735a7`
  at digest
  `sha256:98b71dbbb394d51abea5e79a9f63e4423f43ae3f623d5ed3d28cb9d55c0b6f72`;
- Linux `make fmt`, `make vet`, `make build`, and `make test` passed. All 41
  Ginkgo suites and 154 controller specifications passed with 78.5 percent
  composite coverage;
- the run used EKS cluster `vivek-spl-301372`, namespace
  `shc75-captain-timeout`, with both lifecycle feature gates enabled;
- changing the captain-transfer policy from 300 seconds to one second changed
  no Pod UID or StatefulSet revision. The forward rollout then replaced
  ordinals `2 -> 1`;
- the ordinal-zero captain reached `Blocked/CaptainTransferTimedOut` with its
  original UID and baseline revision intact, no deletion timestamp, no
  replacement authorization, partition one, and ordinals one and two Ready and
  serving;
- the harness held that blocked state for 30 seconds. Exactly one additional
  timeout warning was emitted and no destructive progress occurred;
- revision withdrawal emitted exactly one additional cancellation Event,
  released detention, and restored the same captain Pod. No rollback target
  began until that Pod was Ready, serving, registered, and `Up`;
- rollback reused the baseline ControllerRevision, replaced ordinals
  `2 -> 1`, observed maximum unavailability `1/1`, and recorded zero container
  restarts;
- the run-window Event and Operator-log audit contained no
  `OutOfOrderRevision`, `ExistingUnavailablePod`, or `TooManyUnavailable`;
- final CR and StatefulSet generations were observed, phase was `Ready`,
  captain was ordinal zero and ready, all three members were `Up`, current and
  update revisions matched, partition was three, and all three Pods were Ready
  and serving; and
- the restored 300-second policy changed no Pod identity or revision. The final
  continuous gate passed at 321 seconds with management HTTP 401 on all three
  Pods, KV Store `ready`, no KV Store version upgrade or backup, and zero
  container restarts.

2026-07-28 STS-014 post-authorization revision-handoff qualification:

- Operator source commits were
  `24eea3f37ddb95032cb495dc0b422e8ca3cf9116`,
  `243f7a5d295196e1003ea70a37947bb04bed681c`, and
  `50eb10514a550d67652663cd7ab6644313681dcc`. The final image was
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-reliability-50eb10514`
  at digest
  `sha256:62e450584a9788cd9b0f2959164bdcef2c75608c66bb468cc572e887712d7624`;
- Linux `make fmt`, `make vet`, `make build`, and `make test` passed. All 41
  Ginkgo suites and 154 controller specifications passed with 78.5 percent
  composite coverage;
- the accepted run used EKS cluster `vivek-spl-301372`, namespace
  `shc76-revision-withdrawal`, the pinned SHC runtime image, and both lifecycle
  feature gates;
- before mutation, 25 consecutive samples covered 180 seconds with three
  Ready and serving Pods, three registered `Up` members, authoritative dynamic
  captain readiness, matching StatefulSet revisions, partition three, and zero
  restarts;
- revision A changed the StatefulSet from
  `splunk-shc76-search-head-548ff7f745` to
  `splunk-shc76-search-head-86557c5445`. Its durable ordinal-two operation was
  `PodUpdate:splunk-shc76-search-head-2:splunk-shc76-search-head-86557c5445:2`;
- revision B was submitted after revision A lowered partition to two and
  persisted replacement authorization. While ordinal two terminated, started,
  and rejoined, the StatefulSet continued to expose revision A, partition two,
  and one stable replacement UID;
- the first attempt exposed an unsafe boundary: Splunk lifecycle `Completed`
  preceded Kubernetes Ready and serving readiness. The final source retains
  the queued template until the replacement has the authorized
  ControllerRevision, a new Pod UID, Ready true, and `shc-serving` true;
- after that barrier, revision B became
  `splunk-shc76-search-head-785987d79b`, partition returned to three, and a new
  operation separately detained and authorized ordinal two. Its replacement
  received a second new UID only after the new authorization;
- revision B completed reverse-ordinal progression `2 -> 1 -> 0`. Ordinal zero
  was the active captain at its turn; the workflow transferred captaincy to
  ordinal one before replacement;
- 127 continuous service searches had zero failures. Minimum Ready endpoints
  were two, maximum unavailable Pods were one, maximum container restarts were
  zero, and the run-window Event audit contained no
  `ConflictingLifecycleOperation`, `OutOfOrderRevision`,
  `TooManyUnavailable`, or `ExistingUnavailablePod`;
- final Splunk status reported `dynamic_captain=1`, `initialized_flag=1`,
  `min_peers_joined_flag=1`, `service_ready_flag=1`,
  `rolling_restart_flag=0`, and three `Up` members. KV Store reported
  `status=ready`, three members, `versionUpgradeInProgress=0`, and no backup in
  progress; and
- the independent post-convergence gate passed for 300 seconds over 37
  samples. Every sample had three Ready endpoints, a successful search,
  authoritative captain readiness, and zero restarts.

Two non-accepted runs remain part of the engineering record. The first found
the Ready/serving handoff defect and led to `50eb10514`. The second began too
soon after fresh formation; ordinal two transiently lost readiness before any
lifecycle operation. The Operator correctly held partition three and refused a
planned disruption. That run was classified as an invalid precondition and led
to the sustained pre-action gate used by the accepted run.

2026-07-28/29 UTC REJ-004 image-pull classification qualification:

- Operator source was `4710438a031e77f0906a4eaf26d5821ee70d0ed8`,
  including classification commit
  `b3ae4b291`. The image was
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-reliability-4710438a0`
  at digest
  `sha256:2d9af851e07bbf891b03ad07bec0c849f973280bb92cf03e344620ecbf6154b7`;
- Linux `make fmt`, `make vet`, `make build`, and `make test` passed. All 41
  Ginkgo suites and 154 controller specifications passed with 78.5 percent
  composite coverage;
- the accepted run used EKS cluster `vivek-spl-301372`, namespace
  `shc77-image-pull`, both lifecycle feature gates, and a dedicated desired
  runtime tag resolving to pinned digest
  `sha256:c295389a5bbcaa0aade25b0a5950952794179059564a525a7200b6f1c26b3547`;
- the retryable operation was
  `PodUpdate:splunk-shc77-search-head-2:splunk-shc77-search-head-68bf667f46:2`.
  After replacement authorization and before its first container attempt, the
  desired tag was temporarily removed. Kubelet reported `ErrImagePull` and
  `ImagePullBackOff`; lifecycle status remained
  `WaitingForContainer/ImagePullFailed` for 60 seconds, partition remained two,
  and no later ordinal became eligible;
- restoring the exact desired tag to the exact original digest recovered the
  same operation. The rollout completed ordinals `2 -> 1 -> 0`, and captaincy
  moved from ordinal zero to ordinal one before ordinal zero was replaced;
- the terminal operation was
  `PodUpdate:splunk-shc77-search-head-2:splunk-shc77-search-head-6df8487b99:3`.
  Invalid image syntax produced kubelet `InvalidImageName` and immediate
  lifecycle `Blocked/ImagePullFailed`. The target remained ordinal two,
  partition remained two, and no later ordinal was authorized;
- the 169-sample state timeline included attributable scheduling, container,
  captain-transfer, and blocked stages. Across the accepted window, 131
  Service searches succeeded with zero failures, minimum Ready endpoints were
  two, and maximum unavailable Search Heads were one;
- the Deployer did not change revision during either Search-Head-only
  revision, remained Ready, and had zero restarts. The final CR phase was
  `Error`, as expected for the intentionally terminal lifecycle block, while
  both non-target members remained Ready;
- the campaign used the Operator's default readiness timing. Earlier
  non-accepted fixture work showed that a readiness threshold of 12 with a
  10-second period could keep a member in Service endpoints while port 8089
  was refusing connections. Startup and migration tolerance belongs in
  startup/liveness budgets rather than a slower traffic-removal decision; and
- the qualification namespace, PVCs, and associated PVs were deleted, all
  workers were returned to schedulable state, and the temporary ECR tag was
  removed after evidence collection.

The accepted fault injection did not bypass the image-upgrade compatibility
validator and did not leave an out-of-band Pod image alias. The SHC was formed
with the dedicated desired tag, scheduling was held only long enough to inject
the first-pull fault into the already-authorized replacement, and the exact tag
and digest were restored for recovery. This avoided both a synthetic
already-running-container restart artifact and mixed Pod/StatefulSet image
desired state.

2026-07-29 API-005 Kubernetes-volume-default normalization qualification:

- Operator source was
  `a59fc5103b9199b2a136601ebfbdde1d593c4cc8`; the image was
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-79-a59fc5103`
  at digest
  `sha256:e1b77c45bba3853f96a7ac93ef5d98ac84ebde9ca991d1fbd10a847865767ede`;
- Linux `make vet`, `make build`, and `make test` passed. All 41 Ginkgo suites
  and 154 controller specifications passed with zero failures and 78.6 percent
  composite coverage;
- the accepted run used EKS cluster `vivek-spl-301372`, Kubernetes
  `v1.31.14-eks-7d6f6ec`, namespace `shc79-volume-defaults`, both lifecycle
  feature gates, and runtime digest
  `sha256:e51312c90d8cd860065a0fcb887a50c3d227122477b2ca3f5a7336f93d9308cb`;
- the SHC CR's generic ephemeral volume omitted `volumeMode`; Kubernetes
  returned `volumeMode: Filesystem` in both generated StatefulSets. The
  Deployer and Search Head StatefulSets remained generation one, retained
  matching current/update revisions
  `splunk-shc79-deployer-c96f56679` and
  `splunk-shc79-search-head-fc79bcf47`, and retained one ControllerRevision
  each;
- before the disruption, all three Search Heads returned HTTP 200 for
  `/services/server/info`, `/services/shcluster/member/info`, and an
  `_internal` export search;
- the Operator Pod UID changed from
  `b52ff38e-8d05-4f84-a2a4-959d133cd217` to
  `0402be07-3c2f-44ee-8e7e-7d181263291e`, with the exact image digest
  unchanged;
- six samples from `2026-07-29T17:21:54Z` through
  `2026-07-29T17:24:28Z` observed phase Ready, initialized true, a ready
  captain, four Ready workload Pods, three Search Head Service endpoints,
  unchanged Pod UIDs, zero container restarts, unchanged StatefulSet
  generations and revisions, and successful HTTP 200 searches; and
- Operator logs contained zero `pod Volumes differ` records for the fixture.
  CR-first cleanup then removed all four Pods, all twelve PVCs, and all twelve
  associated PVs before namespace deletion. Every worker remained Ready and
  schedulable, and the EBS CSI controller finished at two ready replicas.

2026-07-29 STS-003/STS-008/STS-014 authorized-revision recovery
qualification:

- source branch:
  `codex/shc-80-authorized-revision-recovery`;
- registration, implementation, forced-rollback, and queued-release commits:
  `d1f6e301d`, `744bfb096`, `9be744f06`, and
  `0b9253f1181947348c43eec7894ff1a9abd65366`;
- final Operator image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-80-0b9253f11`
  at digest
  `sha256:fecf5134468a2478c0de13ad88b463b8f2db38747d795e60aae3304a0b9986cb`;
- Linux `make fmt vet build test` passed all 41 Ginkgo suites and 154
  controller specifications with zero failures and 78.5 percent composite
  coverage;
- the accepted run used EKS cluster `vivek-spl-301372`, Kubernetes
  `v1.31.14-eks-8f14419`, namespace `shc80-authorized-recovery-v2`, both
  lifecycle feature gates, and pinned Splunk 9.4.1 runtime digest
  `sha256:e51312c90d8cd860065a0fcb887a50c3d227122477b2ca3f5a7336f93d9308cb`;
- the healthy baseline revision was
  `splunk-shc80-search-head-8659646985`. After all workers were cordoned,
  revision `splunk-shc80-search-head-b6d6d44d9` was authorized for ordinal
  two, and its replacement remained Pending and unschedulable. Revision
  `splunk-shc80-search-head-6987ddbf74` was queued before recovery;
- the operation retained ID
  `PodUpdate:splunk-shc80-search-head-2:splunk-shc80-search-head-b6d6d44d9:2`,
  original and replacement Pod UIDs, desired and recovery revisions, target
  GUID, and withdrawal timestamp across a real Operator Pod replacement while
  the recovery Pod remained Pending;
- after workers were uncordoned, ordinal two rejoined the last-known-good
  revision with original GUID
  `E308A2D4-49A3-4595-A71F-7D4B7AE01FDB`. The queued revision then completed
  ordinals `2 -> 1 -> 0`, including captain transfers before replacing
  ordinal one and ordinal zero, and reset partition to three after current and
  update revisions converged;
- final GUIDs exactly matched baseline:
  `E35DC033-3CEF-4ACE-B9EE-A7ABAE5F9AB2`,
  `B723CD8C-7BB0-4190-BA67-8919769A583E`, and
  `E308A2D4-49A3-4595-A71F-7D4B7AE01FDB`. Every member was registered `Up`
  with `NoRestart`; the captain was initialized, service ready, and outside
  maintenance or rolling restart;
- Kubernetes Events recorded one withdrawal, one recovery start, one recovery
  completion, three queued-rollout target starts, three partition advances,
  and one final completion. After the final Operator restart, Prometheus
  reported `PrepareTarget=3`, `SetPartition=3`, `Complete=1`,
  `WaitingForKubernetes=4`, and four authorized partition changes. Recovery
  and withdrawal counters were zero in that final process, as expected after
  the deliberate controller restart;
- the availability monitor recorded 187 HTTP 200 searches, zero failures,
  minimum two serving endpoints, maximum one unavailable Search Head, and
  zero workload or Operator restarts;
- 21 final samples from `2026-07-29T21:40:43Z` through
  `2026-07-29T21:46:52Z` spanned 369 seconds with a Ready 3/3 CR, three
  endpoints, equal revisions, partition three, zero restarts, and no bad
  sample; and
- CR-first cleanup removed four Pods, eight PVCs, and all eight PVs before
  namespace deletion. No test SHC or PV remained; all three workers were
  Ready and schedulable, and EBS CSI finished at `2/2` Ready.

2026-07-30 OPS-011/SHC-82 LicenseManager qualification correction:

- the Operator documentation and v4 API establish two separate relationships:
  `spec.volumes` plus `spec.licenseUrl` load the license into the
  LicenseManager, while `spec.licenseManagerRef` on each managed tier points
  that tier at the LicenseManager Service;
- the active ClusterManager, IndexerCluster, and SearchHeadCluster already set
  `spec.licenseManagerRef.name: shc82`. Generated Pod inspection confirmed
  `SPLUNK_LICENSE_MASTER_URL=splunk-shc82-license-manager-service` on the
  ClusterManager, all three indexers, the SHC deployer, and all three Search
  Heads. The LicenseManager correctly did not reference itself;
- the LicenseManager had no external `licenseUrl` or volume and loaded
  `enttrial.lic`. Its `splunkd.log` repeatedly reported
  `This license does not support being a remote manager` for peer usage
  requests. This was a license-capability failure, not a missing
  `licenseManagerRef`, Service-routing failure, or authentication failure, and
  it explains the repeated license-peer retry interval seen during formation;
- a non-committed Kubernetes Secret named `shc82-license` was created from the
  development-only license already present in the Splunk source workspace;
  that license advertises remote-manager capability. The LicenseManager
  mounted the Secret at `/mnt/licenses`, loaded
  `/mnt/licenses/enterprise.lic`, and Ansible recorded a successful license
  apply with zero failed tasks;
- the replacement LicenseManager Pod was created at `06:50:58Z`, applied the
  license at `06:51:53Z`, completed its internal restart and play at
  `06:52:35Z`, and reached Kubernetes Ready at `06:52:44Z`. The last
  remote-manager rejection remained at `06:42:15Z`, before this replacement;
  no new rejection appeared after the licensed instance started;
- throughout the LicenseManager replacement, all three Search Heads and all
  three Service endpoints remained available, container restart count stayed
  zero, and every monitored Service search succeeded. The SHC status
  temporarily reported `Error` while its referenced LicenseManager was
  unavailable and returned to `Ready` at `06:54:11Z`; and
- the reusable baseline fixture now declares the Secret-backed LicenseManager
  volume and `licenseUrl`. The deterministic `make shc82-license-secret`
  target creates or updates the Secret from a caller-supplied file without
  storing license contents in Git.

## Interfaces and Dependencies

The test harness requires stable adapters for:

- local member readiness;
- captain and member summaries;
- search creation, status, and results;
- detention and active-search counts;
- captain transfer invocation and observation;
- persistent member identity;
- lifecycle operation/status and conditions;
- StatefulSet revisions and partition;
- Pod/PVC/EndpointSlice observations;
- metrics scrape; and
- sanitized evidence collection.

The Operator suite uses the existing Ginkgo/Gomega test framework and
`test/run-tests.sh`. Controller integration uses controller-runtime envtest.
Runtime tests use the repository's pytest framework. splunkd tests use its
CMake/CTest runner. Do not introduce a fourth general-purpose test framework
for the spike.

## Revision Note

2026-07-24: Replaced the initial outline with a self-contained testing ExecPlan
after reviewing current Operator, Docker-Splunk, and splunkd test
infrastructure. Added layered gates, concrete commands, evidence schema,
acceptance rules, recovery behavior, and explicit `OnDelete`-before-
`RollingUpdate` sequencing.

2026-07-25: Added the missing qualification for static ordinal-zero
interpretation, deterministic `Parallel` formation, simultaneous persistent
cold restart, fail-closed leave-running behavior, both bundle-target paths,
internal management transport/proxy behavior, immutable Splunk Ansible source
selection, and the final two-feature-branch manual campaign.

2026-07-25: Corrected the runtime-source model after verifying that
Docker-Splunk clones Splunk Ansible into an ignored build-context directory
rather than using a Git submodule. Qualification now requires an immutable
source ref, detached checkout, dirty-tree rejection, and recorded resolved SHA.

2026-07-25: Corrected the execution environment after confirming that the
current macOS workstation and Docker-Splunk Makefile do not provide the
supported enterprise-image build path. Local work now ends at immutable source
and manifest validation; image build and runtime qualification explicitly run
on a separate Linux builder and must record its provenance.

2026-07-25: Added the pre-dispatch freeze audit and remote-reachability gate.
The current local heads are clean but not represented by fetched
remote-tracking refs, so the Linux build remains blocked until publication and
full-SHA verification.

2026-07-28: Recorded the pinned Linux runtime build and first integrated EKS
campaign. Safe `OnDelete`, migration, partition-gated `RollingUpdate`,
captain-transfer, persistent-identity, and controller-restart paths passed.
The document keeps the full gates open until failure injection, repetition,
soak, version-skew, rollback, and operational-readiness work completes.

2026-07-28: Recorded the Docker-Splunk PID 1 TERM correction and active
`RollingUpdate` to `OnDelete` rollback rehearsal. Added continuous stability
gates, exact rollback continuity evidence, sequential OnDelete completion,
captain transfer, identity preservation, OnDelete revision semantics, and the
log/event accuracy issues discovered during qualification.

2026-07-28: Recorded the SHC scale-lifecycle extension. Added durable
scale-down cancellation, repeated-operation identity, scale-direction and
stable-replica assertions, desired-ordinal member observation, final
`3 -> 4`/`4 -> 3` evidence, PVC-policy verification, semantic log/Event audit,
and a passing 300-second post-scale stability gate.

2026-07-28: Recorded the SHC search-drain and cancellation extension. Added
real-time timeout fail-closed assertions, in-place restoration of the original
member when an unauthorized revision is withdrawn, fresh recovery search
counts, bounded historical drain before native replacement, dynamic captain
transfer, exact Event-count checks, and the startup-complete signal gap exposed
by final image-owned synchronization after reported readiness.

2026-07-28: Recorded LFC-006 audited continuation. Added wrong-token and
stale-operation negative paths, approval-only revision isolation, durable
approval/authorization timestamp ordering, active-search audit snapshots,
authenticated metrics evidence, exact Event/log/counter assertions, complete
reverse-ordinal rollout and captain transfer, a 312-second post gate, and
resource cleanup.

2026-07-28: Recorded LFC-007 failed-captain-transfer and revision-withdrawal
qualification. Added the 30-second fail-closed hold, exact Event deltas,
per-Pod ControllerRevision rollback proof, in-place readiness restoration,
bounded Operator-log audit, reverse-ordinal rollback, and the 321-second final
stability gate.

2026-07-28: Recorded STS-014 post-authorization revision handoff. Added durable
ownership of the already-authorized target, CR-driven template queueing,
separate Splunk-completion and Kubernetes Ready/serving barriers, two distinct
revision authorizations and Pod identities, uninterrupted search evidence,
dynamic captain transfer, final Splunk/KV validation, a sustained pre-action
gate, and a passing 300-second post-convergence gate.

2026-07-28/29 UTC: Recorded REJ-004 image-pull classification. Added real
first-pull `ErrImagePull`/`ImagePullBackOff` retry and recovery, immediate
`InvalidImageName` blocking, authorized-ordinal and partition retention,
Search-Head-only revision isolation, uninterrupted Service-search evidence,
default-readiness findings, and full test-resource cleanup.

2026-07-29: Recorded API-005/SHC-79 Kubernetes-volume default normalization.
Added immutable source and image provenance, exact CR-versus-StatefulSet
defaulting evidence, a real Operator restart, six stable post-restart samples,
unchanged StatefulSet and workload identities, successful searches, and zero
false volume-difference reconciles.

2026-07-29: Registered OPS-011/SHC-82 for the customer-reported App Framework
restart-availability concern across Search Heads and indexers. Added the exact
reproduction signature, required active-search and result-completeness
evidence, redundancy and conflict negative cases, and the rule that
`searchable=0` is an observation to explain rather than proof of the reported
impact.

2026-07-29: Recorded SHC-80 authorized-revision recovery qualification. Added
immutable source and image provenance, partition-barrier forced rollback,
durable Operator-restart recovery, completed-operation release of queued work,
dynamic captain-transfer evidence, exact persistent GUID continuity, Event
and process-local metric interpretation, 187 uninterrupted searches, a
369-second final gate, complete storage cleanup, and the separate early-ready
and destructive `make deploy` follow-up findings.

2026-07-30 UTC: Recorded SHC-81 termination-safe finalization qualification.
Added immutable source, Operator, runtime, and EKS provenance; a sustained
healthy-and-paused precondition; direct namespace-first deletion; no-create
and no-post-finalization-status assertions; exact PVC/PV cleanup; bounded
error-log audit; and worker/CSI health evidence. Registered SHC-83 and SHC-84
for the repeated early-ready interval and the coupled startup-budget,
probe-restart, grace-period, and TERM-exit contract. Retained the similar
non-SHC condition-writer structure as an audit item without making an
unqualified defect claim.

2026-07-30 UTC: Selected SHC-82 on isolated branch
`codex/shc-82-appframework-restart-availability` from integrated feature
baseline `079e26233267`. Qualification remains pending and must preserve the
separation between observed restart-mode signals and proven ingest, search,
redundancy, and disruption outcomes.
