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
  while retaining terminal classification for contradictory desired images.
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
- [ ] Reproduce the exact Operator commit on the Linux vWorkstation with the
  repository Makefile gates.
- [ ] Audit and freeze the final Docker-Splunk branch and its exact Splunk
  Ansible commit; pass source gates and build the runtime image on Linux.
- [ ] Complete SHC-85 distributed-search convergence analysis and explicitly
  separate Operator/runtime corrections from Splunk Enterprise changes that
  are only identified for later ownership.
- [ ] Complete SHC-82 restart-required App Framework qualification for Search
  Heads and indexers, including searchable indexer restart behavior.
- [ ] Run the clean final EKS qualification matrix and stability gate from
  immutable Operator and runtime image digests.
- [ ] Update the program index, scenario matrix, qualification record, and
  final branch manifests; commit and push documentation separately.

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

The final outcome is not yet claimed. Current evidence establishes that the
cumulative SHC-93 source can be combined with the current upstream branch and
that the complete macOS source gate passes after resolving the two
merge-boundary test/contract conflicts. Linux reproduction, final
Docker-Splunk freezing, immutable image builds, SHC-82/SHC-85 closure, and the
clean EKS campaign remain required before this plan can be marked complete.

Revision note (2026-08-02 UTC): created this integration ExecPlan after the
branch/patch audit and during the first complete post-merge source gate; then
updated it with merge commits and the passing macOS result. The plan records
facts already established and leaves all incomplete qualification as unchecked
work.
