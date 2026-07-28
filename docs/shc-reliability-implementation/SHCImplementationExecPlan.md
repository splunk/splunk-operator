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

## Decision Log

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

## Outcomes & Retrospective

The first integrated positive-path milestone, active-strategy rollback, and
scale lifecycle extension are complete for one pinned
Operator/runtime/Splunk combination on EKS. Fresh formation, a complete
three-member `OnDelete` rollout, safe strategy migration, complete
partition-gated `RollingUpdate` rollouts, captain replacement, persistent
identity, controller restart recovery, TERM-driven PID 1 exit, active
`RollingUpdate` rollback, scale-down cancellation, repeated scale-down,
`3 -> 4`, and `4 -> 3` all passed. The StatefulSet never advanced more than
one planned Search Head at a time. The final five-minute stability window also
kept every Kubernetes and Splunk health invariant continuously true.

This is not production-readiness evidence. Active-search timeout behavior,
failed captain transfer, forced deletion and node loss, storage and scheduling
delay, network and TLS variants, version skew, rollback under failure, repeated
runs, soak testing, and support/alert qualification remain open. The
lifecycle-aware log, phase, scale-event, and transient higher-ordinal
observation defects exposed by the earlier campaign are corrected and passed
the final scale cycles. The current result proves the integrated architecture
can execute its intended happy path, resume durable state, cross the strategy
rollback boundary, and safely coordinate replica-count changes; it does not
yet justify default enablement.

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
- status/condition compatibility across v3 and v4 APIs.

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
