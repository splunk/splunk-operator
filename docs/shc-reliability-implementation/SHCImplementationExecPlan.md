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
- [x] (2026-07-25) Audited the local integration freeze inputs. Operator,
  Docker-Splunk, and Splunk Ansible worktrees are clean and descend from their
  recorded baselines. No fetched remote-tracking ref contains any current
  integration head, so the Linux handoff remains blocked on remote publication
  and reachability verification.
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

## Outcomes & Retrospective

The first integrated positive-path milestone is complete for one pinned
Operator/runtime/Splunk combination on EKS. Fresh formation, a complete
three-member `OnDelete` rollout, safe strategy migration, a complete
partition-gated `RollingUpdate`, captain replacement, persistent identity, and
controller restart recovery all passed. The StatefulSet never advanced more
than one planned Search Head at a time.

This is not production-readiness evidence. Active-search timeout behavior,
failed captain transfer, forced deletion and node loss, storage and scheduling
delay, network and TLS variants, version skew, rollback during an active
operation, repeated runs, soak testing, and support/alert qualification remain
open. The current result proves the integrated architecture can execute its
intended happy path and resume durable state; it does not yet justify default
enablement.

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
