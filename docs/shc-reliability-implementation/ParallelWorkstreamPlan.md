# Parallel Workstream and Branch Plan

## Purpose

This plan lets multiple teams prototype Search Head Cluster reliability at the
same time without implementing the same behavior twice or building on
incompatible assumptions. The shared outcome is one feature-gated integration
spike based on GitLab `sok/develop`. The spike proves the lifecycle design; it
is not merged directly into `develop`.

The baseline observed on 2026-07-24 is
`sok/develop@39316c19fb990f1af84966d5269a8f4116550dbb`. Refresh that reference
before creating the branches.

## Branch topology

Create one Operator integration branch from the refreshed GitLab development
branch:

    sok/develop
      └── feature/shc-k8s-reliability-spike

Child merge requests target the integration branch:

    feature/shc-k8s-reliability-spike
      ├── spike/shc-contracts
      ├── spike/shc-pod-lifecycle
      ├── spike/shc-orchestrator
      ├── spike/shc-observability
      ├── spike/shc-qualification
      └── spike/shc-rollingupdate

Docker-Splunk or the image/runtime repository uses an independent branch from
its current development baseline:

    <runtime-development-branch>
      └── feature/shc-k8s-reliability-spike
            ├── spike/shc-runtime-lifecycle
            └── pinned Splunk Ansible integration commit

Splunk Ansible is a nested repository and therefore requires its own reachable
integration commit before Docker-Splunk can select it by immutable source ref:

    <splunk-ansible-development-branch>
      └── feature/shc-k8s-reliability-spike
            ├── spike/shc-bootstrap-rejoin
            └── spike/shc-dynamic-target

Create a splunkd branch only if the spike proves that a product change is
required. Operator validation should initially use a pinned, supported Splunk
build whose endpoint behavior has been confirmed. If splunkd changes are
needed, create a fresh branch from the current internal development baseline,
not from the stale local checkout.

## Integration rules

The integration branch is a merge and test branch. Do not implement independent
features directly on it. Every child branch has one technical owner, one
planning document, declared file/symbol ownership, and automated acceptance
tests.

Sibling branches do not cherry-pick from one another. When a dependency merges,
merge the integration branch into the dependent child branch. This keeps one
history for every shared contract and prevents multiple variants of the same
type or helper.

All spike behavior is disabled by default. The existing `OnDelete` path remains
available until the integration tests have separately qualified the new
lifecycle. `RollingUpdate` is never merged ahead of its safety dependencies.

The final testable deliverable consists of one Operator feature branch and one
Docker-Splunk feature branch. The Docker-Splunk branch must pin a pushed,
reachable Splunk Ansible integration commit; a dirty nested checkout or a
local-only commit is not an integration artifact. Docker-Splunk currently
clones Splunk Ansible into an ignored build-context directory rather than
tracking a Git submodule, so its build must explicitly resolve
`SPLUNK_ANSIBLE_REF`, detach at that commit, reject unrelated local changes,
and record the resolved SHA in `version.txt`.

Every merge request records:

- integration-branch commit used as its base;
- Operator image digest;
- runtime image digest and source commit;
- Splunk Enterprise version and build;
- Kubernetes version and environment;
- tests run and artifact location;
- API or interface changes;
- known failures and deferred scenarios; and
- the exact feature-gate state.

## Workstream ownership

### Contracts

Branch: `spike/shc-contracts`

Owns customer API fields, internal policy types, defaults, validation,
conditions, durable operation/status types, feature gates, generated CRDs,
Helm value mapping, API documentation, and compatibility tests.

Primary paths:

- `api/enterprise/v3/`
- `api/enterprise/v4/`
- `config/crd/bases/`
- feature-gate declarations
- Helm templates and values for the new API
- API-focused validation and generation tests

It does not implement the state machine, Splunk API calls, probe scripts,
StatefulSet partition advancement, or runtime shutdown.

Completion means other branches can compile against stable policy and status
interfaces without defining their own copies.

### Pod lifecycle

Branch: `spike/shc-pod-lifecycle`

Owns Search Head readiness, conservative liveness/startup wiring, termination
grace application, lifecycle-hook rendering, and Pod-template tests. To avoid
multiple branches editing the large StatefulSet builder, this workstream should
introduce one Search Head Pod-template helper and own the minimal call-site
change in `pkg/splunk/enterprise/configuration.go`.

Primary paths:

- `tools/k8_probes/`
- a new focused Pod-template helper under `pkg/splunk/enterprise/`
- its unit tests and fixtures
- the minimal integration point in `pkg/splunk/enterprise/configuration.go`

It does not perform captain transfer, search draining, runtime stop
implementation, or partition advancement.

### SHC orchestrator

Branch: `spike/shc-orchestrator`

Owns the durable SHC lifecycle state machine, Splunk API adapters, captain
transfer, detention, search drain, replacement authorization, rejoin
validation, and separation of recycle from permanent scale-down.

Primary paths:

- `pkg/splunk/workflow/shc/`
- SHC additions to `pkg/splunk/client/splunk/`
- a narrow adapter in
  `pkg/splunk/enterprise/searchheadclusterpodmanager.go`
- state-machine unit and fake-client tests

It consumes contract types from `spike/shc-contracts`. It does not render
probes/hooks or mutate StatefulSet partitions.

### Runtime lifecycle

Branch: `spike/shc-runtime-lifecycle` in Docker-Splunk or the selected runtime
repository.

Owns a single idempotent local shutdown operation, shared `preStop` and TERM
coordination, an explicit stopping state, bounded exit reporting, and
bootstrap-versus-persistent-rejoin intent in the image startup path.

Primary paths in the current Docker-Splunk checkout include:

- `splunk/common-files/entrypoint.sh`
- new narrowly scoped lifecycle scripts
- container-level unit tests
- a focused three-Search-Head image test

The associated Splunk Ansible integration work owns:

- persisted and runtime SHC startup-state observation;
- stable bootstrap-seed versus join action;
- persistent rejoin and await-rejoin behavior;
- interrupted-formation recovery;
- dynamic deployer bundle-target selection;
- internal splunkd scheme and management-port handling;
- HTTP proxy bypass for local/internal lifecycle observations; and
- branch-local action-planner and target-selector tests.

During a simultaneous persistent cold restart, temporary member/captain API
ambiguity must suppress every cluster-forming command without failing the
container startup play. The Docker-Splunk entrypoint is fail-fast, so a fatal
Ansible task can otherwise exit every container before splunkd recovers.

It does not choose the captain-transfer target or advance Kubernetes rollout
state. Cluster-wide decisions remain in the Operator.

### Observability

Branch: `spike/shc-observability`

Owns the common stage/reason vocabulary, condition/Event helpers, structured
logging fields, low-cardinality metric collectors, alert/dashboard definitions,
and diagnostic redaction/collection.

This branch creates reusable emitters. It does not duplicate state transitions.
The orchestrator calls the emitters at state boundaries after both branches
share the same integration baseline.

### Qualification

Branch: `spike/shc-qualification`

Owns new end-to-end test suites, fault injectors, availability probes, evidence
collection, artifact schemas, and test documentation.

Primary paths:

- a new `test/shc_lifecycle/` Ginkgo suite
- additions to `test/testenv/` that are generic test utilities
- a focused KUTTL smoke/migration scenario where declarative assertions are
  sufficient
- test scripts that collect sanitized evidence

Implementation branches own unit tests beside their code. Qualification does
not rewrite those unit tests and does not implement production behavior.

### RollingUpdate

Branch: `spike/shc-rollingupdate`

Owns StatefulSet `RollingUpdate` migration, partition calculation,
authorization/advancement, desired/current revision observation, pause,
rollback, and compatibility with the retained `OnDelete` path.

Primary paths:

- a focused rollout coordinator under `pkg/splunk/workflow/upgrade/` or another
  location approved by the Operator technical design
- the narrow StatefulSet integration point
- partition/revision unit, envtest, and integration tests

This branch is created only after contracts, Pod lifecycle, and orchestrator
branches have merged into the integration branch.

## Dependency waves

Wave 0 is short and serial. Create the integration branch, approve public and
internal interfaces, and merge `spike/shc-contracts`.

Wave 1 runs in parallel:

- Pod lifecycle;
- SHC orchestrator against fake Splunk APIs;
- runtime lifecycle in the image repository;
- observability foundations; and
- qualification harness and baseline tests.

Wave 2 merges Wave 1 into the integration branch and validates the complete new
lifecycle while retaining `OnDelete`. Failures are fixed in the branch that
owns the faulty behavior.

Wave 3 creates `spike/shc-rollingupdate` from the tested integration revision.
The qualification branch adds partition and migration scenarios without
implementing rollout behavior.

Wave 4 is integrated failure injection, cloud qualification, version skew,
rollback rehearsal, and design revision.

Wave 5 freezes delivery. Merge all qualified Operator work into
`feature/shc-k8s-reliability-spike`. Merge the Splunk Ansible child branches
into one pushed integration commit, pin it from the Docker-Splunk feature
branch, merge the runtime shutdown work, and build immutable Operator and
runtime images. Run the complete manual scenario matrix only from those two
feature branches.

## Conflict-prevention procedure

Before a child branch begins, its plan lists every file it expects to edit.
The integration owner compares those lists. If two branches name the same file,
resolve ownership before either edits it. Prefer a new focused package and a
single-owner adapter over parallel edits to
`searchheadclusterpodmanager.go`, `configuration.go`, or `statefulset.go`.

Generated CRDs belong only to the contracts branch until that branch merges.
Fixture regeneration caused by an API or Pod-template change belongs to the
branch that owns the source change. Qualification tests should assert behavior,
not copy internal implementation fixtures.

## Merge gates

A child branch can merge into the integration branch only when:

1. its declared API is stable for the spike;
2. its branch-local unit/static tests pass;
3. it has no undeclared production-file overlap with another active branch;
4. its feature remains disabled by default;
5. its evidence contains source and image identifiers;
6. failure behavior is tested, not only success behavior; and
7. the branch plan records discoveries and unresolved limitations.

The integration branch can enable `RollingUpdate` testing only when the
`OnDelete` lifecycle gate in `QualificationObservabilityRolloutPlan.md` passes.

## Final integration and manual qualification

Before integration, create a manifest for every child branch containing its
repository, commit, owned scenario IDs, tests, unresolved limitations, and
whether equivalent changes already exist in another branch. Use ancestry and
patch-equivalence checks to avoid merging duplicate commits.

In Splunk Ansible, merge the bootstrap/rejoin and dynamic-target children into
one integration branch from the selected development baseline. Run unit tests,
Ansible syntax, ansible-lint, and distributed runtime tests. Push that commit
before updating the Docker-Splunk source ref.

In Docker-Splunk, merge the idempotent shutdown child into the final feature
branch and set the build to the tested Splunk Ansible integration commit. The
checkout target must fail on local source changes or an unreachable ref. A
build records the Docker-Splunk commit, resolved Splunk Ansible commit, Splunk
Enterprise build, and immutable runtime image digest. The current macOS
workstation prepares and verifies these immutable inputs but does not build the
enterprise image. Transfer the pushed commits and build manifest to a supported
Linux builder, and record its operating system, architecture, container-engine
version, build log, and resulting digest.

In the Operator, merge or fast-forward the qualified child history into the
final feature branch, audit every child with `git cherry` or an equivalent
patch check, merge the selected current development baseline once, regenerate
all API and Helm artifacts, and build an immutable Operator image. Do not keep
rebasing against a moving baseline during the manual campaign.

The manual campaign first runs the complete lifecycle under `OnDelete`, then
enables partition-gated `RollingUpdate` with the same pinned images. Every
scenario records the stable ID from `SHCTestScenarioMatrix.md`, source commits,
image digests, Kubernetes and Splunk versions, rendered resources, lifecycle
timeline, Splunk member/captain observations, Events, logs, metrics, and
redaction result. A failed cleanup quarantines the environment rather than
deleting all members or changing consensus membership.

## Revision Note

2026-07-25: Added explicit Splunk Ansible child/integration topology, the
Docker-Splunk immutable source-ref rule, ownership for bootstrap/rejoin and
dynamic-target changes, the simultaneous cold-restart constraint, and Wave 5
final integration/manual qualification. This closes the earlier gap where the
plan named only a runtime branch but did not explain how independently
developed nested-repository work becomes one reproducible image. A later
implementation check confirmed that Docker-Splunk uses an ignored cloned
checkout, not a Git submodule, and the plan now reflects the actual build
contract.

2026-07-25: Assigned Docker-Splunk image construction and container
qualification to a supported Linux builder. The macOS workstation is limited
to source validation and immutable handoff preparation because its Makefile
path is not a supported enterprise-image build environment.
