# Integrate and qualify the final SHC reliability branches

This ExecPlan is a living document. The `Progress`, `Surprises & Discoveries`,
`Decision Log`, and `Outcomes & Retrospective` sections must be updated as the
integration and qualification work proceeds.

## Purpose / Big Picture

The SHC reliability work was deliberately implemented and qualified as small,
independent increments. The final integration must prove that the cumulative
Operator and Docker-Splunk sources preserve those contracts together, include
the current upstream repository work, and can be reproduced from named Git
commits and immutable container-image digests.

After this plan completes, reviewers will have one Operator feature branch and
one Docker-Splunk feature branch. Both branches will pass their repository
Makefile gates on Linux. A clean EKS campaign will install only images built
from those exact commits and will exercise formation, normal and captain Pod
replacement, restart-required App Framework changes, dependency ordering,
failure recovery, and final stability. A failed scenario remains an open item;
it is not converted into a qualified claim by documentation alone.

## Progress

- [x] (2026-08-02 UTC) Audited the cumulative Operator branch through SHC-93
  against all child work branches. A raw patch-ID comparison reported
  `578447335` as absent, but source and test comparison established that its
  lifecycle-hold implementation is already present as qualified commit
  `5dbe7dac8`; no production change was missing.
- [x] (2026-08-02 UTC) Created clean worktree
  `/Users/viveredd/Projects/splunk-operator-shc-final` and selected final local
  branch `feature/shc-kubernetes-reliability`. The existing primary checkout
  and its unrelated user changes were left untouched.
- [x] (2026-08-02 UTC) Fast-forwarded the final branch through cumulative
  SHC-93 documentation tip `0213882c9`.
- [x] (2026-08-02 UTC) Merged current upstream Operator commit
  `8c8598597c4f9d8af6dfa879157cdf4084869173` exactly once as merge commit
  `058283da3`. Semantic conflicts were resolved without rewriting the
  individually qualified SHC history.
- [x] (2026-08-02 UTC) Preserved typed retryable dependency convergence during
  merge resolution, including License Manager phase and workload-image lag,
  while initially retaining SHC-87's terminal classification for contradictory
  desired images. The later clean campaign disproved immediate terminal
  classification as a safe Kubernetes assumption; `14047127c`, recorded
  below, supersedes that merge-time decision.
- [x] (2026-08-02 UTC) Preserved namespace-termination safety and corrected the
  newly merged terminal-event tests to create the namespace required by the
  production preflight contract. The focused controller suite passed 192 of
  192 specifications.
- [x] (2026-08-02 UTC) Reconciled the newly merged License Manager gate test
  with SHC-87 semantics. Its targeted test and the complete
  `pkg/splunk/enterprise` package pass. Integration test corrections are
  isolated in commit `55696a8b6`.
- [x] (2026-08-02 UTC) Passed the complete macOS source and generated-tree
  gate on the exact integrated working tree: `make build`, `make helm-check`,
  and `make test`; all 43 Ginkgo suites and all 192 enterprise-controller
  specifications passed with 78.3 percent composite coverage. Formatting,
  vet, manifests, and generation ran through the Make prerequisites, and
  `git diff --check` passed.
- [x] (2026-08-02 UTC) Reproduced exact Operator commit `6108b04d9` on the
  Linux vWorkstation. `make build`, full `make test`, and `make helm-check`
  passed; the controller JUnit report contained 194 nodes with zero failures
  or errors, composite coverage was 78.3 percent, and the Helm gates passed 60
  Operator plus 90 Universal Forwarder tests. Generation left the repository
  clean.
- [x] (2026-08-02 UTC) Froze Splunk Ansible branch
  `feature/shc-kubernetes-reliability` at `fa09e87f8e5bd61ed78da80af1cb8a1ef047acfd`.
  Its reproducible `make shc-check` gate passed on macOS and Linux with
  focused lint, full playbook syntax, and 25 Search Head clustering tests.
- [x] (2026-08-02 UTC) Added repository-owned deterministic Search Head and
  indexer restart-required App Framework sources and a standard-library
  packager. Repeated archives are byte-identical on macOS; the two packager
  tests and Operator `make build` pass at `0ea542801`.
- [x] (2026-08-02 UTC) Added a reproducible final EKS manifest renderer at
  `e65ac74b0`. It rejects mutable-only runtime image references and unresolved
  tokens, renders the four-tier topology with the final digest, and passed
  two unit tests plus Kubernetes client dry-run against the target context.
- [x] (2026-08-02 UTC) Corrected a Docker-Splunk preStop/TERM overlap race at
  `f44f1d8780cc4119c6d991c7bba309e6d0361d34`. A follower now waits for and
  propagates its owner's exact result instead of allowing PID 1 to exit during
  an in-progress stop. Fifteen shutdown tests, four exact-Ansible-ref tests,
  the Red Hat signing-key test, shell syntax, and ShellCheck pass on macOS.
- [x] (2026-08-02 UTC) Completed the clean campaign's dependency and runtime
  image rollout through License Manager, Cluster Manager, Deployer, and four
  RF3/SF2 indexers. The Operator advanced indexers `3 -> 2 -> 1 -> 0`, required
  previous-peer serving recovery, retained at least three indexer endpoints,
  and finished with four Ready peers on runtime digest
  `sha256:d6e11fe00dcadb6a3b168b23081950f85265daf0c923a314034160a495a6db4b`
  and zero container restarts.
- [x] (2026-08-02 UTC) Completed workload run
  `shc-final-runtime-upgrade-v3`: 150 HEC submissions, zero submission
  failures, zero search-request failures, and exact final
  `count/min/max/distinct=150/1/150/150`. The evidence SHA-256 is
  `b8d328954d66716afda5c48aa4cbf0b7168869fc9ada6a88ee94c9db7bcacb9e`.
  Successful aggregate results briefly lagged accepted events and later
  converged; this is not claimed as immediate distributed-search completeness.
- [x] (2026-08-02 UTC) Corrected a false terminal dependency classification at
  `14047127c`. A multi-object Kubernetes apply can expose a short desired-image
  mismatch between a referenced tier and its dependent; this is now typed as
  retryable coordinated convergence rather than emitting a false stalled
  condition and terminal upgrade-mismatch Warning.
- [x] (2026-08-02 UTC) Registered and source-qualified SHC-94 at `9500d8d34`.
  The live SHC repeatedly listed an empty repository, retained no pending app
  or bundle record, and nevertheless remained at partition three because the
  transient poll lock was treated as active deployment work. The correction
  blocks only on durable pending/in-progress app or bundle state. The exact
  source passed 43 suites, all 192 enterprise specifications, 78.3 percent
  composite coverage, 60 Operator and 90 Universal Forwarder Helm tests,
  `make build`, package tests, and generated-tree checks on macOS.
- [x] (2026-08-03 UTC) Integrated and EKS-qualified bounded SHC-100. The safe
  source removed implicit retained-cluster address migration, preserved an
  explicit customer value, restored the retained topology, and qualified a
  fresh-cluster FQDN identity roll `3 -> 2 -> 1 -> 0`. All 157 peer samples
  retained exactly four addresses and GUIDs; final results converged exactly,
  while successful-search partial-result behavior during replacement remains
  an explicit Splunk Enterprise boundary.
- [x] (2026-08-03 UTC) Integrated and EKS-qualified SHC-99 plus SHC-101 at
  exact cumulative source `0b56ec79b`. Live Pods required exact `start` and
  `restart` process forms; shared probe ConfigMap updates required
  optimistic-lock retry. The exact Linux gate passed 43 suites, 192/192 specs,
  78.3 percent coverage, and `make build`. Immutable index
  `sha256:0f2480b1e8e39d6e5a00e014df280c5aa3167abe5e498dd1deaac7399254f0f6`
  passed both live process forms, false-positive rejection, lifecycle hold,
  concurrent two-namespace script propagation, and final snapshots with zero
  candidate Warning Events, controller errors, or workload restarts. The
  accepted Operator was restored cleanly.
- [x] (2026-08-03 UTC) Integrated and EKS-qualified SHC-102 plus SHC-103 at
  exact cumulative source `070ca5f59`. Content-integrity ownership preserves
  every unmarked or customer-edited probe ConfigMap while allowing unchanged
  generated defaults to advance. Successful creation no longer depends on
  immediate informer visibility, and deterministic post-create NotFound
  injection prevents regression. The exact Linux gate passed 43 suites,
  192/192 specs, 78.3 percent coverage, and `make build`. Immutable index
  `sha256:2ae4db4155427ade5361f8a4d71f71d7ea0b4bdbf447a40e2dc1434815074308`
  created a real three-script ConfigMap with equal full Data hash and marker,
  zero candidate manager errors, complete disposable namespace/PV cleanup,
  unchanged retained objects, healthy final snapshots, and clean accepted-
  Operator restoration.
- [ ] Build and qualify the updated `9500d8d34` Operator on Linux, replace the
  controller image by immutable digest, and prove that the already-pending SHC
  revision resumes without CR or StatefulSet mutation.
- [ ] Audit and freeze the final Docker-Splunk branch and its exact Splunk
  Ansible commit; reproduce the final `f44f1d8` source gates and build the
  runtime image on Linux.
- [ ] Complete SHC-85 distributed-search convergence analysis and explicitly
  separate Operator/runtime corrections from Splunk Enterprise changes that
  are only identified for later ownership.
- [ ] Complete SHC-107 persistent-client qualification at test-only source
  `f3ec88026`. The deterministic harness and stable EKS reuse are qualified:
  one HEC connection carried 12 writes, one Search Head connection carried 25
  identity/search requests, and every request and final event completed
  exactly. Search Head and indexer replacement, Operator restart, network
  variants, and soak remain open.
- [ ] Complete SHC-82 restart-required App Framework qualification for Search
  Heads and indexers, including searchable indexer restart behavior.
- [ ] Run the clean final EKS qualification matrix and stability gate from
  immutable Operator and runtime image digests.
- [ ] Update the program index, scenario matrix, qualification record, and
  final branch manifests; commit and push documentation separately.
- [x] (2026-08-03 22:31Z) Completed SHC-104 at canonical Docker-Splunk source
  `0604eeb`. A pinned Linux/AMD64 Python 3.10.18 environment passed clean and
  idempotent aggregate setup, the unchanged 15/4/1 bounded contracts, five
  bootstrap regressions, 91 broader test collections, Compose validation, and
  exact cleanup without user site-package mutation.
- [x] (2026-08-03 23:14Z) Registered and source-qualified SHC-105 at exact
  Operator source `0e638dac4`. The exact App Framework poll boundary now uses
  the existing five-second overdue retry instead of emitting a false zero-
  duration requeue error. One thousand focused repetitions, `make fmt vet`,
  `make build`, all 43 Make test suites, Helm lint, and all 150 chart tests
  passed. Immutable Linux image and live multi-boundary EKS qualification
  remain open.
- [x] (2026-08-04 00:18Z) Registered and source-qualified SHC-106 at production
  correction `ab342d7a5` and cumulative source `a6cda92a3`. A real EKS app
  rollout plus competing common
  template update reproduced overlapping Deployer and Search Head disruption.
  The bounded correction introduces one established-SHC disruption owner,
  retains an already-active Deployer until Kubernetes-observed convergence,
  and prevents Search Head mutation while that owner remains active. One
  hundred normal and race repetitions of helper and real controller-boundary
  tests, all 43 Make suites, 192/192 specs, 78.6 percent coverage, build/vet,
  150 Helm tests, and new-change lint passed. Immutable Linux image and live
  correction qualification remain open. The cumulative source also persists
  the bounded `SHC RollingUpdate DeployerUpdateActive` reason across the
  controller status refresh and verifies it through the API-backed boundary.

## Surprises & Discoveries

- Observation: patch-ID comparison alone is not sufficient when independent
  branches contain equivalent changes with different commits.
  Evidence: child commit `578447335` appeared absent under `git cherry`, while
  the cumulative source at `5dbe7dac8` contains `lifecycleHoldEnv`,
  `SPLUNK_OPERATOR_LIFECYCLE_HOLD`, the liveness behavior, and its focused
  tests.
  Consequence: final integration audits compare named production symbols,
  tests, and rendered behavior in addition to commit ancestry.

- Observation: the cumulative feature history and the current upstream branch
  had both changed the dependency-upgrade path, but with different status
  contracts.
  Evidence: upstream added a License Manager gate test that treated transient
  not-Ready state as `false, nil`; SHC-87 introduced
  `DependencyNotReadyError` so reconcilers can publish Pending/Progressing
  conditions and an aggregatable Normal Event instead of losing the reason.
  Consequence: the integration retains the typed retryable error. Desired CR
  image contradictions still return a terminal error; runtime phase and
  workload-image convergence do not.

- Observation: the namespace-termination preflight correctly treats an absent
  namespace as cancellation, which exposed incomplete isolated fake-client
  fixtures in newly merged Stalled-event tests.
  Evidence: seven tests returned before calling their injected terminal Apply
  function. Adding the named Namespace to each fake client made all seven
  exercise and pass the intended event contract without weakening production
  behavior.

- Observation: parallel Ginkgo packages write the same configured JUnit file,
  so a later suite can hide an earlier suite's failure payload even though the
  command exits nonzero.
  Evidence: the combined report contained only the controller suite while the
  Ginkgo summary also named `pkg/splunk/enterprise`. A package-isolated JSON
  run identified `TestUpgradePathValidation_LicenseManagerGate`.
  Consequence: failures are isolated by package before changing source, and
  final acceptance uses the complete Make target exit status plus package
  summaries rather than assuming one JUnit file is complete.

- Observation: the existing SHC-82 app packager used GNU-specific `sed -i`,
  deterministic-tar flags, and `sha256sum`.
  Evidence: both app targets failed on the macOS review host even though the
  source fixtures were valid.
  Consequence: one standard-library packager now substitutes only the archived
  version and normalizes ordering, timestamps, ownership, and modes without
  changing checked-in source.

- Observation: exact-once shutdown ownership was not sufficient when a
  follower returned before its owner completed.
  Evidence: the prior helper returned success immediately when TERM found a
  preStop lock without a result, allowing the PID-1 TERM trap to exit while
  the preStop-owned stop remained in progress.
  Consequence: concurrent followers wait through the stop deadline and
  TERM-to-KILL interval, reuse the atomic result, preserve failures, and time
  out if an owner disappears.

- Observation: applying several dependent custom resources is not an atomic
  Kubernetes transaction.
  Evidence: the License Manager desired image became visible milliseconds
  before the dependent Cluster Manager desired image. The prior controller
  emitted both a retryable dependency Event and a false terminal
  `UpgradeBlockedVersionMismatch`/`Stalled` sequence; the same manifest then
  updated the dependent and convergence continued without customer action.
  Consequence: desired-image disagreement during dependency convergence is a
  typed retryable wait with both desired images retained in status detail.

- Observation: the App Framework deployment flag is also a repository-poll
  lock, not a durable statement that an app mutation exists.
  Evidence: from 14:25 through 14:35 UTC the SearchHeadCluster listed an empty
  repository every 60 to 62 seconds, ran zero app workers, and cleared the flag
  in the same reconcile. Each next poll set the flag before rollout planning,
  so the StatefulSet stayed at partition three with no lifecycle operation.
  Consequence: rollout ownership is derived from durable pending/in-progress
  per-app or bundle state; an empty poll, completed app, or app error does not
  own the disruption boundary.

- Observation: exact process identity still needs to account for supported
  runtime invocation history.
  Evidence: the first exact SHC-99 image accepted `start` but failed direct
  level-one liveness on a healthy Pod whose daemon retained `restart`; both
  forms existed across the same 20-Pod campaign.
  Consequence: the final matcher accepts two exact action tokens and retains
  strict executable/action boundaries.

- Observation: an Operator image change makes multiple tier controllers
  reconcile one namespace probe ConfigMap concurrently.
  Evidence: accepted-image restoration produced resource-version conflicts,
  six controller errors, and false Indexer StatefulSet Warnings although both
  StatefulSets were 4/4 Ready.
  Consequence: SHC-101 retries conflicts from current state and treats an
  identical concurrent winner as success.

- Observation: the shared ConfigMap name cannot distinguish generated
  defaults from supported customer scripts.
  Evidence: pre-created overrides and Operator-generated defaults use the same
  namespace-scoped name, and older objects have no durable origin marker.
  Consequence: SHC-102 treats every unmarked or content-mismatched object as
  customer-managed and updates only an unchanged marked default.

- Observation: a successful API-server create can precede informer-cache
  visibility.
  Evidence: SHC-102's first create path required an immediate cached read even
  though Kubernetes had already accepted the write.
  Consequence: SHC-103 makes create success authoritative and bounds only the
  AlreadyExists-winner visibility window.

- Observation: Docker-Splunk's aggregate test bootstrap is not reproducible
  with the current Linux builder's Python packaging toolchain.
  Evidence: all 20 bounded SHC tests passed, then `make test_setup` selected
  PyYAML 5.4.1 through Docker Compose 1.29.2 and failed in isolated build
  requirements with a missing `cython_sources` attribute.
  Consequence: SHC-104 owns an isolated locked test environment; the failure
  is not attributed to runtime or shutdown behavior.

## Decision Log

- Decision: use `feature/shc-kubernetes-reliability` as the single final
  Operator review branch.
  Rationale: it is the existing integration line, contains the earlier
  feature history, and can be advanced without modifying the user's primary
  checkout.

- Decision: merge the current upstream line once, resolve semantic conflicts,
  and qualify the resulting merge commit rather than rebasing the entire
  qualified history.
  Rationale: the existing commits identify individually qualified increments;
  rewriting all of them would sever the recorded code/evidence mapping. The
  merge makes upstream ancestry explicit and confines integration corrections
  to reviewable commits.

- Decision: keep ordinary dependency convergence retryable and observable.
  Rationale: a referenced License Manager that is absent, starting, updating,
  or whose StatefulSet has not yet reached its desired image is normal
  Kubernetes convergence. Marking the dependent CR terminal would require
  customer action for a condition that recovers automatically.

- Decision: do not add Splunk Enterprise production changes to the final
  Operator or Docker-Splunk qualification branches.
  Rationale: the current program can identify Splunk-owned readiness,
  distributed-search, KV Store, or shutdown gaps, but those changes require
  separate product ownership and compatibility qualification. A temporary
  diagnostic build is not a supported migration solution.

- Decision: protect the retained `shc85-lifecycle-hold` fixture until the
  final clean campaign explicitly replaces disposable resources.
  Rationale: it is the stable reference environment for checking that branch
  assembly and source validation do not mutate live workloads.

- Decision: do not serialize a StatefulSet rollout behind the transient App
  Framework poll lock alone.
  Rationale: actual remote changes are materialized as durable pending app
  records before rollout planning, and pending/in-progress bundle state is
  already durable. Those are safe mutual-exclusion signals. A completed or
  failed app remains diagnosable but cannot starve Kubernetes convergence
  indefinitely.

- Decision: generate the final qualification topology from a checked-in
  template and require an immutable runtime digest before rendering.
  Rationale: formation and App Framework results must be attributable to one
  exact image; ad hoc live image patches are not reproducible evidence.

- Decision: reject a source-qualified probe assumption when an immutable live
  runtime shows another supported process form.
  Rationale: liveness controls destructive kubelet restarts. Runtime evidence
  across the qualified image has priority over an incomplete synthetic
  process table, and the correction must remain exact rather than broad.

- Decision: make shared probe script upgrades conflict-safe before final
  integration.
  Rationale: expected Kubernetes optimistic-lock races must not emit false
  workload failure Events that obscure real lifecycle problems.

- Decision: fail closed on probe ConfigMap ownership and cache visibility.
  Rationale: preserving an ambiguous legacy/custom object is recoverable;
  overwriting supported customer scripts is not. A successful API write is
  authoritative, while a concurrent winner is re-read and preserved.

## Implementation and qualification sequence

1. Finish the Operator merge and make the integration fixes separate from the
   merge commit where Git permits. Run `make fmt`, `make build`, `make test`,
   `make helm-check`, and `git diff --check`. Confirm generated files are
   committed or clean.
2. Push the exact Operator commit, fetch it into
   `~/splunk-complete/splunk-operator` on the vWorkstation, and reproduce the
   source gates with the repository Makefile. Record suite counts, coverage,
   and the commit ID.
3. Audit Docker-Splunk's final branch from its feature line. Verify TERM/PID 1
   ownership, the shared shutdown helper, readiness/liveness behavior,
   version propagation, and the exact Splunk Ansible pin. Reproduce Makefile
   tests on Linux.
4. Build Operator and runtime images only on the vWorkstation, publish them to
   ECR, and resolve their immutable image-index or image-manifest digests.
   Record source labels and package provenance from the running containers.
5. Run bounded SHC-85 and SHC-82 diagnostics before the destructive clean
   campaign. If a failure is attributable to Splunk Enterprise rather than
   orchestration or container lifecycle, record the exact REST/log/source
   evidence and do not conceal it with an Operator workaround.
6. Remove only the disposable campaign namespaces and their PVCs/PVs. Install
   the final Operator, License Manager, Cluster Manager/indexers, Deployer, and
   three-member Search Head Cluster from explicit CR references and exact
   digests.
7. Exercise fresh formation, same-version Pod replacement, image upgrade,
   non-captain and active-captain replacement, Operator restart during a
   durable stage, retryable image pull and scheduling/storage recovery,
   restart-required App Framework deployment, service-routed searches, direct
   member searches, HEC ingestion, and final exact result completeness.
8. Hold a post-action stability gate. Require desired/ready/updated replica
   agreement, exactly one captain, all intended members Up and serving,
   healthy KV Store, no pending bundle/configuration replication, no
   unauthorized target advance, bounded unavailability, zero unexplained
   container restarts, and exact eventual workload results.

## Acceptance evidence

The final record must include:

- Operator, Docker-Splunk, Splunk Ansible, and packaged Splunk source/version
  identifiers;
- immutable image digests and the Kubernetes manifests that selected them;
- Make target commands, exit status, suite/spec counts, and coverage summary;
- CR status, StatefulSet revision/partition history, Pod UID/GUID/restart
  history, EndpointSlice membership, captain history, and lifecycle stage
  history;
- relevant Kubernetes Events and scoped Operator, container, Ansible, Splunk,
  SHC, KV Store, and indexer-cluster log evidence;
- continuous workload request results and final exact completeness; and
- explicit unqualified boundaries and separately owned Splunk Enterprise
  requirements.

## Outcomes & Retrospective

The full program outcome remains open only for the unchecked compatibility and
negative gates above; those gaps are not converted into claims. The cumulative
integration now additionally closes bounded SHC-99 through SHC-103, with the
latest exact source `070ca5f59`: complete Linux gates, immutable Operator
deployment, real start/restart liveness, fail-closed false-positive behavior,
lifecycle hold, conflict-safe and ownership-safe probe propagation,
cache-independent creation, healthy final peer snapshots, and accepted-
Operator restoration all passed. The observed successful-search partial-
result behavior during indexer replacement remains an explicit SHC-85/OPS-011
and Splunk Enterprise boundary.

Revision note (2026-08-02 UTC): created this integration ExecPlan after the
branch/patch audit and during the first complete post-merge source gate; then
updated it with merge commits and the passing macOS and Linux results. The plan
records facts already established and leaves all incomplete qualification as
unchecked work.

Revision note (2026-08-03 18:36Z): Added bounded SHC-99 through SHC-101 final
integration evidence, including the rejected start-only image, exact
start/restart correction, conflict-safe probe ConfigMap upgrade, complete
Linux and immutable EKS gates, healthy snapshots, and accepted restoration.

Revision note (2026-08-03 20:35Z): Added SHC-102/103 ownership and cache-
visibility corrections, deterministic regression evidence, exact cumulative
Linux/image provenance, live EKS creation, complete disposable cleanup,
unchanged retained objects, and accepted restoration.

Revision note (2026-08-03 20:45Z): Registered SHC-104 after canonical
Docker-Splunk bounded tests passed but its aggregate legacy Python dependency
bootstrap failed reproducibly on the current Linux builder.
