# Search Head Cluster Reliability Work-Item Index

## Purpose

This file is the central registry for the `SHC-*` execution identifiers used
while implementing and qualifying the Search Head Cluster reliability design.
It answers three separate questions:

- which bounded engineering work item changed the integrated feature branch;
- which immutable commits contain that work; and
- which stable requirement scenarios provide acceptance evidence.

`SHC-*` identifiers are implementation and qualification work items. They are
not product requirements and are not substitutes for the stable scenario IDs
in `SHCTestScenarioMatrix.md`. A work item can satisfy several scenarios, and a
scenario can depend on several work items.

The authoritative program status remains in `SHCImplementationExecPlan.md`.
The detailed test evidence remains in
`QualificationObservabilityRolloutPlan.md`. This index links those records
without duplicating their full content.

## Status vocabulary

- **Integrated** means the source commit is in the SHC reliability feature
  history.
- **Source-qualified** means its branch-local unit, controller, generation, or
  build gates passed.
- **EKS-qualified** means the behavior was exercised as part of a recorded EKS
  campaign. It does not mean every scenario associated with the capability is
  complete or that production default enablement is approved.

## Work-item registry

| Work item | Scope | Source commits | Primary scenarios | Recorded status |
|---|---|---|---|---|
| SHC-60 | Parse and retain member management URI needed for dynamic captain operations | `0e3864f1e` | LFC-002, LFC-009 | Integrated; source-qualified; exercised by later EKS captain-transfer campaigns |
| SHC-61 | Wait for local and captain views to converge before destructive progression | `9061027f7` | LFC-008, LFC-009, REJ-006 | Integrated; source-qualified; exercised by later EKS campaigns |
| SHC-62 | Bound replacement Pod startup and classify a replacement that does not start | `fd5d32ed1` | STS-008, REJ-004, REJ-005 | Integrated; source-qualified; remaining fault-injection variants are open |
| SHC-63 | Surface a blocked rollout through durable status and Kubernetes conditions | `8255c818e` | STS-008, OBS-001, OBS-004 | Integrated; source-qualified; blocked status exercised on EKS |
| SHC-64 | Preserve terminal lifecycle reason and diagnostic message | `63cc5cf2f` | OBS-001, OBS-005 | Integrated; source-qualified; terminal-detail behavior exercised by blocked campaigns |
| SHC-65 | Keep healthy non-target peers serving during rollout planning and target withdrawal | `4ff606a57` | HLT-003, HLT-004, LFC-012 | Integrated; source-qualified; serving invariant continuously checked on EKS |
| SHC-66 | Count rollout decision transitions once without polling-driven metric inflation | `dbc80363a` | OBS-002, OBS-003 | Integrated; source-qualified; targeted metric evidence recorded |
| SHC-67 | Continue a verified `OnDelete` lifecycle and recognize a detained owned target | `605e7cb37`, `702eb982a` | LFC-001, STS-001, STS-002 | Integrated; source-qualified; complete three-member `OnDelete` EKS rollout passed |
| SHC-68 | Make detention release, upgrade initialization/control, and uncertain detention requests bounded and retry-safe | `85d86c55e`, `60a32d728`, `8659c63ae`, `c77c3fb86` | LFC-010, LFC-011, OBS-002 | Integrated; source-qualified; exercised in integrated lifecycle campaigns |
| SHC-69 | Require ready KV Store before rollout authorization and recovery advancement | `22ab2ca0c` | REJ-011, STS-006 | Integrated; source-qualified; KV Store gate exercised in EKS happy path and later campaigns |
| SHC-70 | Record the first complete lifecycle qualification evidence | `ed7b1b656` | LFC-001, LFC-002, STS-001, STS-002, STS-005, STS-006 | EKS-qualified for the recorded three-member happy path and controller restart |
| SHC-71 | Rehearse active `RollingUpdate` rollback to `OnDelete` | `1f7dd6041` | STS-010 | EKS-qualified for one active ordinal-two rollback; additional rollback-under-fault variants remain open |
| SHC-72 | Correct and qualify scale lifecycle, cancellation, repeated-operation identity, member observation, and scale observability | `255759009`, `e7b696f5e`, `b4d2af703`, `7e97936df`, `6ebe009ad`, `ccab4fe33`, `89f4aebb4` | OPS-001, OPS-002, OPS-003, OBS-001, OBS-002 | EKS-qualified for cancellation, repeated `4 -> 3`, final `3 -> 4` and `4 -> 3`, storage policy, and 300-second stability |
| SHC-73 | Recover a withdrawn Pod update after drain timeout and refresh search counts during cancellation | `23bdb631b`, `5783e5b69`, `a463e89e6` | LFC-003, LFC-004, LFC-005, LFC-014 | EKS-qualified for real-time fail-closed cancellation and bounded historical drain |
| SHC-74 | Add audited post-timeout continuation with operation/token matching and a durable approval barrier | `54a5aae3c`, `5bfd23b18` | LFC-006, OBS-001, OBS-003, OBS-006 | EKS-qualified for wrong-token, stale-operation, exact approval, reverse-ordinal rollout, and 312-second stability |
| SHC-75 | Qualify failed captain transfer and pre-authorization revision withdrawal; handle ControllerRevision reuse, in-place readiness handoff, and StatefulSet generation observation | `eb6907ee5`, `44ccac31e`, `3e9e735a7` | LFC-007, OBS-001, OBS-002 | EKS-qualified for pre-authorization failure/cancellation, reverse-ordinal rollback, clean Event/log audit, and 321-second stability |
| SHC-76 | Retain an already-authorized target across a superseding desired revision, queue the later Pod template, and release it only after Kubernetes traffic readiness | `24eea3f37`, `243f7a5d2`, `50eb10514` | STS-003, STS-014, OBS-001, OBS-002 | EKS-qualified for post-authorization revision handoff, two distinct target authorizations, complete reverse-ordinal convergence, 127 uninterrupted searches, and 300-second final stability |
| SHC-77 | Distinguish retryable image-pull backoff from terminal invalid image syntax and retain the authorized ordinal under the replacement startup budget | `b3ae4b291`, `4710438a0` | STS-008, REJ-004, REJ-005, OBS-001, OBS-002 | EKS-qualified for a 60-second retryable pull hold and recovery, complete `2 -> 1 -> 0` convergence, immediate `InvalidImageName` block at ordinal two, 131 uninterrupted searches, minimum two Ready endpoints, and maximum unavailability one |
| SHC-78 | Attribute scheduling, Pod-infrastructure, and CSI attachment waits without collapsing them into image-pull or container-startup time | `63714251f`, `7b90da269`, `a5a41c07c` | REJ-002, REJ-003, STS-008, OBS-001, OBS-002 | EKS-qualified for six-sample unschedulable and exact CSI-attachment holds, complete scheduler recovery, same-target storage recovery, minimum two Ready endpoints, uninterrupted HTTP 200 search, and zero restarts |
| SHC-79 | Normalize Kubernetes-defaulted Pod volume fields before desired/observed StatefulSet comparison | `96c16b49b`, `a59fc5103` | API-005, STS-003, OBS-002 | Source-qualified; EKS-qualified for omitted/defaulted generic-ephemeral `volumeMode`, stable StatefulSet generations and revisions, controller restart, six post-restart samples, HTTP 200 search on every member, and zero Pod replacement or restart |
| SHC-80 | Define and implement safe withdrawal or supersession when an authorized replacement cannot start | `d1f6e301d`, `744bfb096`, `9be744f06`, `0b9253f11` | STS-003, STS-008, STS-014, OBS-001 | Source-qualified and EKS-qualified for an authorized unschedulable replacement, superseding queued revision, durable last-known-good recovery across an Operator restart, complete queued rollout, dynamic captain transfer, 187 uninterrupted searches, and 369 seconds of final stability |
| SHC-81 | Make SHC CR deletion finalization safe after namespace termination begins | `d053ff65b`, `33ff143d1`, `58437e3ad` | OPS-004, OBS-001, OBS-005 | Source-qualified and EKS-qualified for direct namespace deletion of a paused, healthy three-member SHC: no create after namespace termination, no post-finalization status write, declared PVC deletion completed, and no workload or PV remained |
| SHC-82 | Define and qualify App Framework restart-required app availability across Search Head and indexer clusters | First SH serving correction `0fc1bcf31`; SH drain work `632d9155c`; indexer lifecycle work under SHC-85 | OPS-006, OPS-011, K8S-007, OBS-001, OBS-003, OBS-005 | Partial EKS evidence: the SH correction removed the zero-endpoint captain-transition outage, but an already-admitted captain search still failed. Four-peer searchable indexer restart preserved RF/SF/searchability; existing readiness lost 7/55 HEC submissions, default HEC-aware readiness lost 1/55, and a fast experiment completed 55/55 exactly. SHC-85 later removed manual lifecycle advancement for tested Operator-owned four-peer rolls, including controller-Pod restart recovery during `Decommissioning`, with exact 80/80, 30/30, 100/100, and stable 30/30 records on the official fixed KV Store build. A final integrated effective-policy run then completed an internal searchable `3 -> 2 -> 1 -> 0` restart with at least three indexer endpoints, unchanged Pod UIDs, zero indexer container restarts, zero failures across 90 HEC and 90 search requests, and exact final 90/90 completeness; three successful-search count regressions converged with maximum lag three. Typed policy delivery, Splunk-managed target control, configuration variants, client delivery, conflict, and unhealthy-redundancy gates remain open |
| SHC-83 | Prevent traffic readiness before image-owned SHC initialization, synchronization, and internal Splunk restarts are complete | `635b81bc4`, `daf6b0608`, `0a2465cbe`, `85b00fd9f`, `2889c8002` on `codex/shc-83-startup-readiness-qualification` | HLT-001, HLT-002, HLT-009, STS-012, OBS-001 | Source-qualified and EKS-qualified for a fresh three-member formation with zero premature client endpoints, exactly one initial-formation restart Event, and twelve stable three-endpoint samples after `Complete`. Established non-captain and active-captain replacements retained at least two endpoints and returned to three; captaincy moved dynamically from ordinal zero to ordinal two. Operator replacement retained all three endpoints, Search Head UIDs, durable formation state, and zero restarts. The EKS campaign also corrected a circular dependency by separating internal management target eligibility from client Service readiness during bounded first formation |
| SHC-84 | Bound first-start and upgrade startup probes and guarantee prompt TERM exit for kubelet-initiated restarts | Policy `968e19b94`, API validation `c58ff86cd`, merge fix `67c0d3bd2`; monitors `cbaef60af`, `524636f39`; source fixture `4718cef6f` on `codex/shc-84-startup-term-qualification` | HLT-009, RUN-003, RUN-004, REJ-005, OBS-005 | Source- and EKS-qualified with Operator digest `sha256:d83ae44c825f13cb12117e72d2ca5415b4ffd9b7af36bcab7e81226e11e6cafe`: existing-v4 reconciliation, fresh formation, forced liveness, planned deletion, and the supported `10.4.2604.0/60dd7967c086` to `10.5.2605.0/844c593e9c1d` upgrade passed. The upgrade replaced ordinals `2 -> 1 -> 0`, retained at least two endpoints, recorded zero container restarts, moved captaincy dynamically, completed 200/200 sampled searches, and finished with three registered `Up` target members. Startup/liveness grace rendered 660, readiness grace remained unset, and Pod grace remained 1200 |
| SHC-85 | Separate indexer serving readiness from lifecycle progress and require previous-peer network-path recovery before authorizing another disruption | `3f60d9301`, `11d719f64`, `7ff844f4a`; controller-restart evidence on `codex/shc-85-controller-restart-qualification`; lifecycle hold `5dbe7dac8`, `99da90390`, `ac1fe0db8`; harness `854a76b8d`, `b2bf2e71d`, `d610d4474`; observed-decommissioning absence harness `8d6a7dbc6`; readiness-withdrawal absence harness `978d71bc5`; target-selection absence harness `2d430748b`, `770a27799`; API-disconnection harness `8e21b9b1b` through `f78828cc1`; leader-failover harness `ba220677b` | OPS-011, STS-004, K8S-006, K8S-007, OBS-001, OBS-002, OBS-003 | Source-qualified and EKS-qualified for Operator-owned four-peer RF3/SF2 `OnDelete` revision rolls on official Splunk build `10.5.2605.0/844c593e9c1d`: automatic `3 -> 2 -> 1 -> 0` progress, one withdrawn target at a time, previous-peer remote serving recovery before the next target, zero container restarts, four Ansible `failed=0` results, no prior KV Store failure signature, and final RF/SF/all-searchable health. Separate campaigns qualified controller restart, five-minute controller absences at all four durable ordinal-3 stages, a 401-second Pod-local API-server disconnection at observed `Decommissioning`, and one normal two-contender Lease takeover at observed `Decommissioning`; each retained durable ownership and completed the full roll. The leader-failover run advanced the Lease once, preserved the exact interrupted operation, emitted no duplicate target decommission Event, kept one stable active leader and two Ready controller Pods through convergence, and restored the original single-controller topology. API-independent 1,800-event workloads spanning the long absences, API fault, and leader failover had zero HEC/search request failures and exact final completeness. The records exposed 24, 41, 37, 18, 30, and 13 successful-search count regressions, with maximum pending 362, 406, 404, 364, 417, and 329 during peer-address/authentication convergence and no partial-result signal. Immediate distributed-search completeness, other API-partition/leader-failover stages and topologies, split brain, conflict, redundancy, protocol/configuration variants, and Splunk-managed App Framework next-target control remain open |
| SHC-86 | Make referenced LicenseManager finalization safe after namespace termination begins | `61b35aabf` on `codex/shc-86-license-finalization` | OPS-012, OBS-001, OBS-005 | Source-qualified with 41 Linux suites and 157 specs; EKS-qualified with immutable Operator digest `sha256:635d60fecdd203e7d158fb1f95c57d46c7062ed98b156caf8dc68da7515812ec`. A paused invalid fixture and a Ready LicenseManager referenced by a paused SearchHeadCluster both finalized after namespace termination with no create failure, no post-finalization status error, and no manual finalizer patch. The real fixture logged deletion of both bound PVCs, both delete-reclaim PVs disappeared, and the namespace completed naturally after Kubernetes cleanup. Later SHC-87 cleanup exposed an earlier namespace-termination-to-CR-deletion propagation race; SHC-90 subsequently closed that separate guard gap without changing the bounded SHC-86 evidence |
| SHC-87 | Distinguish retryable referenced-tier dependency convergence from terminal dependency or upgrade failure | `20d926658` on `codex/shc-87-dependency-status`; final-integration refinement `14047127c` | OBS-001, OBS-004, OBS-005 | Source-qualified with 41 Linux suites and 157 specs; EKS-qualified for one SHC-to-LicenseManager absent, Pending, Ready, and full-formation path with Operator digest `sha256:fbb1a53c45da509fee47edc618eefd93923fc3864df9533dc85dbcbc8914c2a3`. The SHC remained Pending/Progressing with `DependencyNotReady`, specific status and Normal Event evidence, then cleared the dependency state and reached Ready/Ready, 3/3 replicas, three endpoints, all members Up, zero restarts, and 8/8 Service searches. Cross-namespace behavior is source-qualified. Final integration source `14047127c` supersedes the historical immediate-terminal desired-image classification after EKS reproduced that observation during a normal non-atomic apply; disagreement is now retryable and observable while still blocking dependent progress. |
| SHC-88 | Use a resolvable LicenseManager endpoint for license-health and expiration checks | `241ea3d91` on `codex/shc-88-license-health` | OPS-013, OBS-001, OBS-004, OBS-005 | Source-qualified and EKS-qualified for the bounded endpoint contract. The reconciler now creates the headless Service already named by the LicenseManager StatefulSet, waits for Kubernetes Pod readiness before calling the per-Pod management URL, and emits one aggregating `LicenseHealthCheckFailed` Warning Event series for retryable REST failures. Exact Operator digest `sha256:545910a6b769ad399fea42fdb31ddb79af11d38b5e5691ed3a59786a7606180e` created the missing Service without replacing a Splunk Pod, resolved the Pod FQDN from the controller, and received HTTP 200 from `/services/licenser/licenses`. A clean Operator restart retained the Service and LicenseManager Pod UIDs, added three successful checks, emitted no new failure, and left every managed tier Ready. Expired-license Event behavior is source-qualified; a deliberately expired production license was not installed on EKS |
| SHC-89 | Initialize a schema-valid status when a custom resource is created already paused | `3e1716737` on `codex/shc-89-paused-status` | OBS-001, OBS-008 | Source-qualified with 41 Linux suites and 157 specs; EKS-qualified across all seven active v4 Splunk reconcilers with Operator digest `sha256:b83bbb97f89dca45e183e895e4be7e1d7bd11007f08babb41c4c94c97d18f145`. Every paused-at-creation resource reported current-generation `Pending/Paused` status once, created no managed workload, retained stable resourceVersion for 45 seconds, and produced no paused-status or Reconciler error. Annotation removal took a LicenseManager and three-member SearchHeadCluster to Ready; the SHC had three endpoints, all members Up, zero restarts, and direct search success on every member. Queue and ObjectStorage have no active enterprise reconcilers in this baseline and are not live targets |
| SHC-90 | Stop normal reconciliation as soon as the namespace is terminating, including before a CR deletion timestamp is observed | `7ce2483f7`, `0c291c8c8` on `codex/shc-90-namespace-termination-guard` | OPS-004, OPS-012, OBS-001, OBS-005 | Source-, Linux-, image-, and EKS-qualified for the bounded contract. Authoritative uncached Namespace GET, Namespace `get`-only RBAC, zero-mutation stop across all seven active v4 tier controllers, explicit deletion-transition event acceptance, finalization bypass when the CR is already deleting, and typed `NamespaceTerminating` admission cancellation close the preflight and admission races. Final Linux source passed 42 suites, 180 JUnit nodes, 78.1% composite coverage, build/vet/generate, and 124 Helm tests. Immutable Operator digest `sha256:c2438c14e238e101cba52d758968a2cd7c64fc2798ed5a0a4781acb3e836e764` observed the real Namespace-deleting/CR-not-deleting interval for a Ready LicenseManager and 3/3 SHC, stopped five reconciles per controller with zero fixture error, preserved both deletion finalizers, removed all ten PVC/PV claim references, and completed the Namespace naturally without a manual patch. SHC-92 subsequently qualified namespace-scoped live Helm; provider/live-version breadth remains a separate gate |
| SHC-91 | Route deletion-safe finalization before pause and ordinary Apply work in every active v4 Splunk controller | `86a0bc80a`, `a76c30e0c` on `codex/shc-91-deletion-before-pause` | OPS-004, OPS-012, OBS-001, OBS-005 | Source-, Linux-, image-, and EKS-qualified for the bounded current-v4 contract. Five controller entry points now bypass pause for deletion, six real Apply entry points finalize before normal work, successful finalization suppresses post-delete status, and failure remains observable. Exact source passed 42 Linux suites, 185 controller specs, 78.3 percent composite coverage, build, and 124 Helm tests. Immutable Operator digest `sha256:4903f70a95b150c0a29bcd3ac70e063b5c55b6a030399a4636297586dea85cea` completed direct and namespace-first deletion across all seven tiers with zero status or Reconciler errors. A real Ready Standalone removed its workload and two PVC/PVs naturally, with the final PV absent by 73 seconds. SHC-92 subsequently qualified namespace-scoped Helm; provider/live-version breadth, v3, and every-tier real runtime shutdown remain open |
| SHC-92 | Make namespace-scoped Helm watch-target and `namespaceOverride` semantics explicit and consistent | `91f742b52` on `codex/shc-92-namespace-scoped-helm` | K8S-010, CMP-006, CMP-007 | Source-, chart-, Linux-, and EKS-qualified for the bounded effective-namespace contract. `namespaceOverride` when non-empty, otherwise the release namespace, now controls every namespaced chart resource, namespace-scoped watch target, Role/RoleBinding placement, service-account identity, leader Lease, and get-only Namespace reader. Exact source passed 42 macOS and Linux suites, 185 enterprise specs, 78.3 percent composite coverage, build, lints, and 137 Helm tests. Packaged chart SHA-256 `23258a699126ae318fee287a5734d939521f3d32ef8741f936ff44b31ef9b5b8` reproduced the old false-Ready/leader-election failure, recovered the same Deployment and service-account UIDs through an unpatched Helm upgrade, qualified fresh default and overridden installs, and ran two releases from one release namespace with distinct target-derived readers. Uninstall removed every fixture and delete-reclaim PV while the retained Operator and SHC remained Ready with zero restarts. Kubernetes 1.27 evidence is render-only; live evidence is EKS 1.31. Changing an established override, overlapping watch scopes, and provider/version breadth remain separate boundaries |
| SHC-93 | Make Operator Pod readiness distinguish a live health server from the ability to participate in reconciliation | Core `47cd2d3ba`; cache barrier `262e37265`; secure metrics `3f7b3ee34`; final `90103bef5` on `codex/shc-93-operator-readiness` | K8S-011, OBS-001, OBS-004, OBS-005 | Source-, chart-, Linux-, and EKS-qualified for the bounded manager contract. `/healthz` remains process-local; `/readyz` requires the complete initial enabled-controller informer barrier plus current exact Lease authorization and does not require current leadership. Exact final source passed 43 macOS and Linux suites, all 185 enterprise specs, build, focused race, Kustomize, and 145 Helm tests. Immutable Operator OCI index `sha256:b5a022a788c7cacf8b7ee33e7132eae56d82b14eb631809ddd116c8b816e9d63` and chart SHA-256 `008abda67d13775ce6cd7e0f8e77365edce01af82f6ad9c12ecf34911a2f6925` qualified cold informer and Lease denial, same-Pod recovery, secure failure metrics, normal startup, active-leader API interruption, two Ready contenders, and 35-second takeover on EKS 1.31.14. Cleanup was complete and the retained SHC stayed 3/3 Ready with zero restarts. Other providers/versions, productized manager HA, ongoing post-start per-informer health, and production alert delivery remain open |
| SHC-94 | Distinguish empty App Framework repository polling from durable app work when coordinating an SHC StatefulSet rollout | `9500d8d34` on `feature/shc-kubernetes-reliability` | OPS-006, OPS-011, STS-003, OBS-001 | Source-qualified on macOS: focused regression tests, complete enterprise package tests, `make build`, and the full 43-suite/192-enterprise-spec/150-Helm-test gate passed. The defect is reproduced on EKS: an empty repository was listed once per poll interval, durable app and bundle status remained empty, yet the pending SHC revision remained at partition three with `AppFrameworkOperationActive`. The correction uses durable pending/in-progress app or bundle state as the ownership signal. Updated-image recovery and actual restart-required App Framework qualification remain open. |
| SHC-95 | Qualify Search Head availability across restart-required App Framework work and separate replicated SHC policy from member-local configuration | Phase-three convergence `3903e01df`; workload error preservation `70db3cf83`; readiness withdrawal `3f8881f52`; fail-closed policy delivery `d9742547b`; exact same-version image intent `ea844b0e0`; Deployer preflight `a92a93d8e`; owned-target resume `bb4f59819`; Kubernetes-owned App Framework path `dda3142f5`; authoritative restart observation `4c54c1bf8`; converged stale-intent isolation `71beda7b5` on `feature/shc-kubernetes-reliability` | OPS-006, OPS-011, HLT-003, HLT-004, OBS-001, OBS-005 | EKS-qualified for the bounded Search Head app-restart path; broader OPS-011 remains open. Exact source `71beda7b5` staged and sent deterministic app `1.0.2` without an internal restart, persisted one content-addressed restart revision, survived controller replacement, dynamically transferred captaincy, and completed the Kubernetes-owned `2 -> 1 -> 0` StatefulSet roll. The 240-sample record passed 240 HEC and 240 distributed-search requests with zero failures and exact convergence. The SHC recovered 3/3 Ready with equal current/update revisions, partition three, all members `Up/NoRestart`, exact runtime digest, zero container restarts, and no image-upgrade workflow. Exact final branch source `8aebfb0cc`, which adds only the separate SHC-96 diagnostic correction, passed native Linux AMD64 `make test` with all 192 enterprise/controller specs, `make build`, chart lints, and all 150 Helm tests. Indexer app qualification and negative/fault breadth remain open. See `SHC95SearchHeadAppRestartQualification.md`. |
| SHC-96 | Prevent App Framework errors from exposing the namespace admin credential | `fdcc772d8`, `8aebfb0cc` on `feature/shc-kubernetes-reliability` | OBS-006 | Source-qualified. The exact Linux gate for SHC-95 exposed that `isAppAlreadyInstalled` returned the full `splunk list app ... -auth` command on a real execution error or an empty successful response. The test password was synthetic, but production uses the namespace-scoped admin credential in the same path. The correction redacts raw and shell-quoted password forms from the command, stdout, stderr, and nested error before returning or logging them. Focused tests cover a password containing a single quote and credentials injected into every diagnostic field. Exact final source passed native Linux AMD64 `make test`, `make build`, chart lints, and all 150 Helm tests; immutable live no-regression deployment remains open. |
| SHC-97 | Prevent Docker-Splunk startup from reissuing `splunk start` while the process created by the first invocation is still initializing KV Store | Splunk Ansible `e0fed1c1`, `ae8ecf4a`; Docker-Splunk pin `118cae68` on `feature/shc-kubernetes-reliability` | HLT-009, OBS-001, OBS-005 | Source- and EKS-qualified for the bounded single-start contract. The focused Make gates passed, an executable exhausted status poll remained fatal, and the Linux-built runtime OCI index `sha256:49b12103f8444319dcf823eb829d2dfc020410e44d46273461c1b15e52c724fd` completed two same-PVC Cluster Manager replacements and full dependency-ordered convergence with one start per replacement, no port 8191 conflict, and zero container restarts. Two workload windows ended exactly complete with zero HEC or search-request failures. The live starts returned zero, so the conditional nonzero status-poll branch is source-qualified rather than live-forced. Transient successful-search count regressions during indexer replacement remain the open SHC-85/OPS-011 boundary. No Splunkd change, marker fabrication, process kill, data deletion, or probe weakening is part of this work. See [SHC97DockerSplunkStartupQualification.md](SHC97DockerSplunkStartupQualification.md). |
| SHC-98 | Test whether stable per-ordinal indexer search addresses reduce the SHC-85 distributed-peer convergence window | Operator experiment `2c607d6e2`; monitor `78ff404c7`; workload `aa9a566aa`; Splunk Ansible `9dff0999c`; Docker-Splunk `6ee266c1` on `codex/shc-98-stable-indexer-search-address` | OPS-011, STS-004, OBS-001, OBS-003, OBS-005 | The automatic retained-cluster experiment is EKS-rejected. Native Linux gates, immutable images, dependency-ordered tier rollout, reverse-ordinal indexer replacement, zero container restarts, PVC identity, Cluster Manager FQDN registration, RF/SF health, Kubernetes DNS, and direct management reachability all passed. Nevertheless, every Search Head retained old Pod-IP peers and added FQDN peers with the same GUID/server name. Splunk reported multiple-cluster and duplicate-server-name conflicts and deliberately retained peers missing from the latest generation for more than 1,800 seconds. The forward workload had 735 materially incomplete successful samples, 45 major regressions, and peak pending 641; lifecycle had already declared `Completed`. Controlled rollback preserved every PVC and returned all peers to Pod-IP mode, but again required 26 minutes after lifecycle completion for exact Search Head convergence and produced 221 materially incomplete samples, 32 major regressions, and peak pending 345. Stable identity remains viable only as a separate initial-formation experiment until Splunk supplies a safe retained-peer address migration. Explicit partial-result semantics remains a Splunk requirement. See [SHC98StableIndexerSearchAddressExecPlan.md](SHC98StableIndexerSearchAddressExecPlan.md). |
| SHC-99 | Make level-one Pod liveness identify real `splunkd ... start` and `splunkd ... restart` commands without accepting unrelated substrings | Isolated source `184061106`; integrated correction `05b7b3ea7`; cumulative source `0b56ec79b` on `codex/shc-kubernetes-reliability-final-integration` | HLT-002, HLT-009, OBS-001, OBS-005 | Complete. The first exact matcher rejected the known Coder `autostart` false positive but EKS showed that 16 of 20 healthy Splunk Pods retained an exact `restart` daemon argument while four retained `start`. The final matcher accepts both delimited actions and rejects `autostart`, `restartable`, and `mysplunkd`. Exact source passed 43 native-Linux suites, 192/192 specs, 78.3 percent coverage, and `make build`. Immutable index `sha256:0f2480b1e8e39d6e5a00e014df280c5aa3167abe5e498dd1deaac7399254f0f6` passed real start/restart, synthetic acceptance/rejection, and lifecycle-hold checks with unchanged Pod UIDs and zero restarts. Both candidate and accepted-restoration snapshots were healthy. See [SHC99ExactSplunkdProcessMatchExecPlan.md](SHC99ExactSplunkdProcessMatchExecPlan.md). |
| SHC-100 | Keep stable indexer search identity explicit and reject ordinal advancement evidence when the prior peer identity has not converged on every Search Head | Operator safety `ca8c5eab6`; monitor guard `cc63afe81`; rollback mode `bbeaa5644`; evidence preservation `91c39b89b`; stale-target isolation `c952d242a`; Linux `jq` guard correction `10730a6a2` on `codex/shc-100-safe-stable-indexer-address-opt-in` | OPS-011, STS-004, OBS-001, OBS-003, OBS-005 | Complete for its bounded scope. The Operator no longer changes persistent peer identity merely because Pod and Indexer lifecycle gates are enabled; an explicit customer `extraEnv` value remains preserved for coordinated qualification. Exact production source `2e4bfa333` passed all 43 native-Linux Make suites and 192 enterprise/controller specs, built successfully, and ran on EKS at immutable Operator index `sha256:da21db17e031dfd03ad0de1020a12eda46ca8b98bd4d8f4cba4c12134b6d59b1` without altering a retained IndexerCluster revision or explicit value. Dependency-ordered restoration returned all four retained tiers to the accepted runtime with supported SHC captain transfer, minimum two SH endpoints, minimum three indexer endpoints, preserved PVCs, and zero restarts. Its 3,600-event workload completed exactly with zero request failures but exposed 215 materially incomplete successful samples while Search Heads required about 26 minutes after lifecycle Ready to remove old peers. The original monitor's Linux `jq` keyword defect invalidated its automated per-ordinal claim; source `10730a6a2` corrects it, and the preserved rollback sample still proves the factual prior-peer violation on all three Search Heads. A separate fresh cluster then formed with exactly four explicit FQDN peers and completed same-runtime indexer replacement `3 -> 2 -> 1 -> 0`: all four UIDs and IPs changed, all PVCs and FQDNs remained stable, at least three endpoints remained Ready, and restarts stayed zero. Across 157 samples every Search Head always held exactly four unique addresses and GUIDs; the corrected guard and 60 final exact samples passed. Its 3,600-event workload completed exactly with zero request failures; all 21 count regressions, maximum count drop 280, and maximum pending 282 occurred during the roll, with no count regression after lifecycle completion. Linux Ansible ownership tests also passed customer-value preservation and stale-ownership relinquishment. Final SHC-99 integration and accepted-Operator restoration preserved both clusters. This supports a bounded new-cluster opt-in, not automatic or retained-cluster address migration, and does not close the partial-result requirement. |
| SHC-101 | Retry concurrent reconciliation of the namespace-scoped probe ConfigMap without false workload failures | Exact source `0b56ec79b` on `codex/shc-kubernetes-reliability-final-integration` | OBS-001, OBS-005, HLT-002 | Complete. An Operator image change made several tier controllers update the same probe ConfigMap from one resource version; losing writers emitted six errors and false `GetIndexerStatefulSetFailed` Warnings even though both Indexer StatefulSets were healthy. Reconciliation now re-reads and retries conflicts, treating an identical concurrent winner as success while preserving terminal errors. Twenty injected-conflict repetitions, the complete Linux gate, and immutable EKS index `sha256:0f2480b1e8e39d6e5a00e014df280c5aa3167abe5e498dd1deaac7399254f0f6` passed. The same two-namespace hash transition then produced zero Warning Events, zero controller ERROR/FATAL logs, and zero workload restarts. See [SHC101ProbeConfigMapConflictExecPlan.md](SHC101ProbeConfigMapConflictExecPlan.md). |
| SHC-102 | Preserve customer-created or customer-edited probe scripts while allowing unchanged Operator-generated defaults to advance | Ownership source `e887d4bec` on `codex/shc-102-probe-configmap-ownership`; cumulative source `070ca5f59` on final integration | OBS-001, OBS-005, HLT-002 | Complete. A complete-data integrity marker permits updates only while the ConfigMap remains byte-for-byte as the Operator last wrote it. Unmarked legacy/pre-created data and marker-mismatched customer edits are preserved, including during conflict retry. Exact ownership source passed repeated focused tests, 43 native-Linux suites, 192/192 specs, 78.3 percent coverage, `make build`, and immutable EKS ownership index `sha256:2680a7fee145458e6d70355f25e1dfd4d8b19ca15cf11e0ed8793ff43fcf8a7e`. The cumulative SHC-103 gate at `070ca5f59` then proved real generated-object creation and final restoration. See [SHC102ProbeConfigMapOwnershipExecPlan.md](SHC102ProbeConfigMapOwnershipExecPlan.md). |
| SHC-103 | Make successful probe ConfigMap creation independent of immediate controller-cache visibility | Production correction `44145c4d1`; cumulative test source `070ca5f59` on `codex/shc-103-probe-configmap-create-cache` and final integration | OBS-001, OBS-005, HLT-002 | Complete. A successful API-server create is authoritative even if a following best-effort cached read has not observed it. An AlreadyExists winner is read through a bounded NotFound-only retry and preserved unchanged; other errors remain terminal. Deterministic post-create NotFound injection, 43 exact Linux suites, 192/192 specs, 78.3 percent coverage, `make build`, and immutable index `sha256:2ae4db4155427ade5361f8a4d71f71d7ea0b4bdbf447a40e2dc1434815074308` passed. EKS created the three-script ConfigMap with equal Data hash/marker `ddbc90fba32858eb497c2d2ca947ee38f793869c13162d72cbaf2947edfafe43`, zero candidate manager errors or probe-NotFound failures, exact retained Pod/ConfigMap preservation, complete namespace/PV cleanup, and healthy accepted restoration. See [SHC103ProbeConfigMapCreateCacheExecPlan.md](SHC103ProbeConfigMapCreateCacheExecPlan.md). |
| SHC-104 | Make the Docker-Splunk aggregate test bootstrap reproducible on current supported Linux builders | Complete at canonical Docker-Splunk source `0604eeb` | OBS-005 | Complete. The test-only correction creates a repository-local `.test-venv`, locks the Python 3.10 dependency graph and PyYAML build toolchain, retains Docker Compose 1.29.2, routes all broader pytest commands through that environment, and adds five bootstrap regressions. Clean and repeated local and Linux/AMD64 runs pass. The immutable Linux Python image gate passed the unchanged 15 shutdown, four exact-Ansible-ref, and one base-image-security tests twice, five bootstrap regressions twice, `pip check`, pytest and Compose startup, all 91 broader image-test collections, Compose scenario validation, and exact environment cleanup. Production runtime source is unchanged. See [SHC104DockerSplunkTestBootstrapExecPlan.md](SHC104DockerSplunkTestBootstrapExecPlan.md). |
| SHC-105 | Prevent an exact App Framework repository-poll boundary from producing a false invalid-requeue error | Source-qualified at `0e638dac4` on `codex/shc-105-appframework-requeue-boundary` | OBS-001, OBS-005 | Source-qualified; live qualification remains open. The final SHC-94 EKS campaign emitted one zero-duration requeue error at a normal poll boundary, while an independent five-second lifecycle requeue allowed the rollout to continue. The correction applies the existing five-second overdue fallback to exact zero as well as negative delay. Positive, boundary, and overdue tests, 1,000 focused repetitions, `make fmt vet`, `make build`, all 43 Make test suites, 78.3 percent composite coverage, Helm lint, and all 150 chart tests passed. Native Linux image and multi-boundary EKS validation remain pending. See [SHC105AppFrameworkRequeueBoundaryExecPlan.md](SHC105AppFrameworkRequeueBoundaryExecPlan.md). |
| SHC-106 | Serialize Deployer replacement, durable App Framework work, and Search Head member lifecycle as one established-SHC disruption domain | Production correction `ab342d7a5`; cumulative source `a6cda92a3` on `codex/shc-106-deployer-coordination` | OPS-006, STS-003, OBS-001, OBS-002, OBS-005 | Source-qualified; live correction qualification remains open. The accepted Operator reproduced concurrent planned disruption: Search Head 2 began termination at `23:51:27Z`, and a competing common template update terminated the Deployer at `23:51:49Z` before the member recovered. The existing Search Head queued-revision barrier independently worked: partition returned to three before a separate controlled revision lifecycle. SHC-106 defers a new Deployer update while durable App Framework or member lifecycle owns the slot, resumes an already-started Deployer to convergence, and blocks Search Head mutation until the Deployer is Ready on its published update revision. The bounded `SHC RollingUpdate DeployerUpdateActive` reason persists through the API-backed status refresh so restart recovery and support captures retain the current owner. Helper and real `ApplySearchHeadCluster` ownership-boundary tests each passed 100 normal and 100 race repetitions; all 43 Make suites, 192/192 specs, 78.6 percent coverage, build/vet, 150 Helm tests, and new-change lint passed. Native Linux image, both ownership directions, and controller-restart EKS qualification remain pending. See [SHC106DeployerMemberCoordinationExecPlan.md](SHC106DeployerMemberCoordinationExecPlan.md). |
| SHC-107 | Qualify long-lived HEC and distributed-search client connections across Kubernetes Service backend replacement | Stable/captain source `f3ec88026`; HEC response source `d57db8d7a`; Search Head response source `3e9f47751` on `codex/shc-107-persistent-client-qualification` | HLT-014, OPS-006, OPS-011, OBS-001, OBS-003, OBS-005 | Stable reuse, unplanned active-captain replacement, one selected-indexer replacement, two Operator-owned Search Head rolls, active-controller replacement, the no-mesh path, and a full accepted-image indexer roll are qualified for bounded claims. The indexer negative proved EndpointSlice withdrawal cannot move an established HEC flow; Splunk code 23 / HTTP 503 remained `Keep-Alive` and 28 submissions were rejected. Retrying only that explicit response recovered one request and delivered 600 events exactly, although two successful searches returned lower counts. The Search Head negative delivered 1,800 events exactly but recorded 218 searches rejected on connections pinned to detained members. Retrying only the explicit detention HTTP 405 recovered four responses across two complete `2 -> 1 -> 0` rolls and completed 1,200 events with zero request, identity, or count-regression failure, including after deleting the active Operator in durable ordinal-2 state. The full indexer `3 -> 2 -> 1 -> 0` campaign recovered one HEC 503 and delivered 2,400 events exactly, but three HTTP-successful searches regressed by up to 847 events. The monitor rejected ordinal-2 selection before ordinal-3 convergence, and exact four-current-peer convergence followed lifecycle completion by 1,583 seconds. Mesh/ingress variants, HTTP HEC, candidate-image qualification, release soak, immediate distributed-search completeness, a cluster-wide peer-convergence advancement gate, and Splunkd connection-close changes remain open. See [SHC107PersistentClientQualificationExecPlan.md](SHC107PersistentClientQualificationExecPlan.md). |
| SHC-111 | Qualify explicit HTTP/HTTPS endpoint protocols for the persistent HEC/search client without assuming ingress or service mesh | Test source `de6f5f8e3` on `codex/shc-111-protocol-qualification` | HLT-014, OPS-011, OBS-001, OBS-003, OBS-005 | Source-qualified with 19 tests and manifest validation repeated 100 times, format, vet, and clean diff. HTTPS remains the default; HTTP/HTTPS scheme and port are explicit per path, and unsupported schemes fail closed. The accepted-image setup transition placed all four indexers on HTTP HEC in reverse ordinal order with at least three Ready peers and zero restarts; it is not an availability verdict because old and new backends used different protocols during that change. Stable HTTP and same-protocol full-roll evidence are in progress. The cluster has no IngressClass or detected mesh control plane/sidecar, so ingress and mesh variants remain unqualified rather than assumed. See [SHC111ProtocolQualificationExecPlan.md](SHC111ProtocolQualificationExecPlan.md). |
| SHC-112 | Require every managed Search Head to converge the replacement Indexer GUID to exactly one current enabled `Up` address before an Operator-owned roll selects the next ordinal | REST source `63c2a5459`; lifecycle source `d7a6e6125`; cumulative source, negative qualification, and reconstructed-manager test `79f751075` on `codex/shc-112-indexer-search-peer-gate` | OPS-011, STS-004, OBS-001, OBS-002, OBS-003, OBS-005 | Source-qualified; immutable EKS qualification remains open. Exact source passed the full 43-suite Make gate, 192/192 enterprise/controller specs, 78.6 percent composite coverage, build/vet, 100 repeated focused checks, 20 race-enabled focused repetitions, chart lint, and all 150 Helm tests. Negative coverage includes transient Cluster Manager and Search Head observations, Kubernetes discovery failure, no matching SearchHeadCluster, and multiple matching clusters. A reconstructed manager also resumed from persisted convergence state without prior process memory; live Operator-Pod replacement remains open. The accepted-image full roll proved ordinal 2 selection preceded ordinal 3 convergence and exact four-peer convergence followed lifecycle completion by 1,583 seconds. The gate adds durable `AwaitingSearchPeerConvergence` state, exact GUID/address/status/disabled comparison on every matching Search Head, two-observation revalidation, and monotonic invalidation. It does not control Splunk-managed restart sequencing or prove complete search results. See [SHC112IndexerSearchPeerConvergenceGateExecPlan.md](SHC112IndexerSearchPeerConvergenceGateExecPlan.md). |
| SHC-113 | Close every successful Splunk REST response body and release each short-lived peer-observation transport | Initial body-close source `961fe9b06`; exact source `c700a077e` on `codex/shc-113-close-splunk-rest-response-bodies`, stacked on SHC-112 | OBS-001, OBS-002, OBS-005, HLT-002 | Source-qualified; immutable Linux/EKS resource soak remains open. The common client now closes responses on successful JSON, successful no-target, unexpected-status, empty-body, and malformed-body paths. Because SHC-112 creates a private client and transport per Search Head observation, that caller also releases its default and SHC-control idle connections after every request, including observation errors. This preserves normal keep-alive for long-lived clients and changes no endpoint, timeout, authentication, or retry. Exact source passed 100 focused response-ownership repetitions, 20 race-enabled focused repetitions, `make fmt vet build`, all 43 Make suites, 192/192 enterprise/controller specs, 78.6 percent composite coverage, chart lint, and all 150 Helm tests. One unrelated MonitoringConsole status conflict in an initial full run passed 10/10 isolated retries before the clean 192/192 rerun. No Docker-Splunk or Splunk Enterprise change is involved. See [SHC113SplunkRESTResponseClosureExecPlan.md](SHC113SplunkRESTResponseClosureExecPlan.md). |
| SHC-114 | Bound and cancel the all-Search-Head peer-convergence observation batch | Exact source `5440b8c2e` on `codex/shc-114-bounded-peer-observation`, stacked on SHC-113 | OBS-001, OBS-002, OBS-005, HLT-002, OPS-011 | Source-qualified; immutable Linux/EKS qualification remains open. The first SHC-112 implementation had a five-second timeout per serial Search Head request and did not attach the reconcile context, so worst-case worker time grew with member count and controller cancellation waited for the client timeout. SHC-114 limits the batch to four concurrent requests and 15 seconds total, propagates cancellation to Search Head and Cluster Manager observations, evaluates results deterministically, releases private transports, and keeps every failed observation as a fail-closed wait. Focused client, batch, convergence, cancellation, and concurrency tests passed 100 normal and 20 race-enabled repetitions; exact source passed all 43 Make suites, 192/192 specs, 78.7 percent composite coverage, generation, `make fmt vet build`, chart lint, all 150 Helm tests, and `git diff --check`. Live gates remain pending. See [SHC114BoundedSearchPeerObservationExecPlan.md](SHC114BoundedSearchPeerObservationExecPlan.md). |
| SHC-115 | Bound every short-lived Splunk REST transport owned by the Operator | Exact source `cd3498393` on `codex/shc-115-short-lived-rest-transports`, stacked on SHC-114 | OBS-001, OBS-002, OBS-005, HLT-002 | Source-qualified; immutable Linux/EKS qualification remains open. A repository-wide audit found that older Search Head lifecycle, Indexer/Cluster Manager, license, bundle, Monitoring Console, secret-sync, and telemetry callers also create private clients. SHC-115 releases every identified production transport on success and failure while preserving keep-alive within a client, and adds a 90-second defensive idle bound. Exact source passed `make build`, all 43 Make suites, 192/192 specs, 78.7 percent composite coverage, chart lint, all 150 Helm tests, 20 race-enabled client and telemetry repetitions, and one race-enabled run across every changed enterprise lifecycle path. A broad race run exposed an unrelated existing App Framework worker-scheduler race outside the changed files; the normal full suite passed. See [SHC115ShortLivedRESTTransportOwnershipExecPlan.md](SHC115ShortLivedRESTTransportOwnershipExecPlan.md). |
| SHC-116 | Require continuous Kubernetes endpoint withdrawal before Operator-owned indexer decommission | Exact source `96c83dcad` on `codex/shc-116-indexer-endpoint-withdrawal`, stacked on SHC-115 | OPS-011, STS-004, HLT-014, OBS-001, OBS-002, OBS-003, OBS-005 | Source and Linux-image qualified; immutable EKS availability qualification remains open. The accepted-image diagnostic aligned its only HEC failure with the decommission request while the target still appeared routable through the client Service. SHC-116 requires exact target Pod readiness withdrawal plus no routable EndpointSlice target, persists the effective deadline and UID, invalidates an interrupted quiet interval monotonically, and gates both normal and recovery decommission. API/CRD/default/validation, target-identity, nil-ready, deadline, invalidation, merge, recovery, and no-early-action tests pass. Exact source passed 43 native-Linux suites, 192/192 specs, 78.7 percent coverage, generation, build/vet, focused race checks, chart lint, all 150 Helm tests, and produced immutable OCI index `sha256:f16a128454cbe60f2d6230c452c69efca6dafe6526b8f2589bdc27b3203a3a1f`. See [SHC116IndexerEndpointWithdrawalExecPlan.md](SHC116IndexerEndpointWithdrawalExecPlan.md). |
| SHC-117 | Keep the workload and monitor active through a complete exact-peer-gated four-indexer roll | Exact test source `cd522e119` on `codex/shc-117-long-indexer-roll-qualification`, stacked on SHC-116 | OPS-011, HLT-014, OBS-003, OBS-005 | Test-source qualified; live evidence remains open. The observed per-ordinal stale-peer cleanup can make a complete roll exceed the former one-hour workload and two-hour monitor. SHC-117 changes only the qualification fixture: 10,800 one-second samples, a 10,800-second monitor timeout, and a 14,400-second Job deadline. Makefile shell/manifest validation passes where the required tools are installed; Linux bash syntax and Kubernetes dry-run pass. The production Operator and runtime images are unchanged. See [SHC117LongIndexerRollQualificationExecPlan.md](SHC117LongIndexerRollQualificationExecPlan.md). |
| SHC-118 | Require continuous Kubernetes endpoint withdrawal before Search Head detention | Exact source `8152fc042` on `codex/shc-118-search-head-endpoint-withdrawal`, stacked on SHC-116; Operator OCI index `sha256:bc733990967abade9419be4caa85d68040355c959d86410a93bd8765830eed9f`; harness `49b6c5057` | LFC-001, LFC-003, LFC-008, HLT-014, OBS-001, OBS-002, OBS-003, OBS-005 | Source- and immutable-Linux-image-qualified; the harness passes Bash/ShellCheck and a read-only retained-cluster preflight, and requires one live API-independent workload before mutation; destructive EKS qualification remains open. The exact target Pod must remain not Ready and absent from routable client EndpointSlices through a persisted default-30-second deadline before either Pod-update or scale-down detention. Exact UID, observation, deadline, sequence, and invalidation survive controller replacement and merge monotonically; returned routing invalidates proof. A different operation ID cannot replace an active persisted lifecycle operation. Effective delay must be shorter than effective detention timeout. Generation, manifests, focused tests, all 43 native suites (192/192 specs, 78.8 percent coverage), `make build`, chart lint, all 150 Helm tests, full v4 API/metrics race checks, 20 changed-path enterprise race repetitions, and clean-diff checks passed. A broad enterprise race run reproduced existing App Framework and Cluster Manager test-seam races outside the changed paths. Existing drain, captain, replacement, rejoin, cancellation, and rollback gates remain in force. This reduces new routing but does not claim to terminate established client connections. See [SHC118SearchHeadEndpointWithdrawalExecPlan.md](SHC118SearchHeadEndpointWithdrawalExecPlan.md). |

## SHC-93 immutable qualification inputs

- source branch: `codex/shc-93-operator-readiness`;
- exact qualified source:
  `90103bef5d87546cadc419738752a0d6b0cd813e`;
- accepted Operator tag:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc93-90103bef5`;
- Operator OCI index:
  `sha256:b5a022a788c7cacf8b7ee33e7132eae56d82b14eb631809ddd116c8b816e9d63`;
- linux/amd64 manifest:
  `sha256:2302269199434b738979a199e56bd7fcb2d9539b4c5f523b6233c3f41db01afc`;
- linux/amd64 manager SHA-256:
  `55914940988b05b4ba00c2d74dbabdd03f4cce4f9b30a2b04aeec894f7e72d74`;
- packaged chart: `splunk-operator-3.1.0.tgz`, 11,266 bytes, SHA-256
  `008abda67d13775ce6cd7e0f8e77365edce01af82f6ad9c12ecf34911a2f6925`;
- EKS context:
  `arn:aws:eks:us-west-2:667741767953:cluster/vivek-spl-301372`;
- EKS server `v1.31.14-eks-8f14419`; Helm `v3.18.4`;
- source gates: 43 macOS and Linux suites, 187 JUnit nodes, all 185
  enterprise-controller specs, zero failures, 78.3/78.4 percent composite
  coverage, build, focused race, three Kustomize renders, 60 Operator Helm
  tests, and 85 Universal Forwarder Helm tests;
- failure/recovery: cold list/watch denial held health/readiness at 200/500 and
  metrics at `0/0/0`; cold Lease denial retained the NotReady endpoint and
  reported `1/0/0`; restoring access recovered the same Pod UIDs with zero
  restarts;
- HA/API behavior: two authorized, synchronized contenders were Ready while
  exactly one led; deletion transferred leadership to the existing standby in
  35 seconds; API isolation caused the active leader to exit only after Lease
  renewal loss, and its restarted manager remained healthy/NotReady without a
  CrashLoop until in-place recovery;
- cleanup: the Helm release, disposable namespace, metrics-reader binding, and
  SHC-93 cluster-scoped RBAC were absent; the retained SHC remained 3/3 Ready
  with zero restarts; and
- detailed evidence: `SHC93OperatorReadinessQualification.md` and
  `SHC93OperatorReadinessExecPlan.md`.

## SHC-92 immutable qualification inputs

- source branch: `codex/shc-92-namespace-scoped-helm`;
- source commit and exact source tip:
  `91f742b52b0e3483ff8a156189e64b1914e38ecd`;
- preceding chart source used for the upgrade fixture:
  `a76c30e0c2395506cbfbb8d9e2643c186df0a3ef`;
- packaged chart: `splunk-operator-3.1.0.tgz`, 10,369 bytes, SHA-256
  `23258a699126ae318fee287a5734d939521f3d32ef8741f936ff44b31ef9b5b8`;
- unchanged manager image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator@sha256:4903f70a95b150c0a29bcd3ac70e063b5c55b6a030399a4636297586dea85cea`;
- EKS context:
  `arn:aws:eks:us-west-2:667741767953:cluster/vivek-spl-301372`;
- EKS server: `v1.31.14-eks-8f14419`; render-only versions: `1.27.0`
  and `1.31.14`;
- macOS and Linux gates: 42 Ginkgo suites, 185 enterprise-controller specs,
  zero failures, 78.3 percent composite coverage, successful build, both
  lints, 52 Operator Helm tests, and 85 Universal Forwarder Helm tests;
- upgrade result: the pre-fix Pod was Kubernetes Ready but could not acquire
  its leader Lease; Helm revision 2 preserved Deployment UID
  `8532104a-a239-477d-bc4c-b291bcd2cbd3` and service-account UID
  `e15f25e4-2a93-4017-a9b5-8c0294db905b`, moved 25 Roles and two RoleBindings,
  replaced the release-derived reader, acquired leadership, and started every
  controller without a manual patch;
- coexistence result: releases `shc92-upgrade` and `shc92-peer` shared release
  namespace `shc92-old-release` while watching `shc92-old-watch` and
  `shc92-new-watch` through readers `5a5312bf` and `98ad5634` respectively;
- cleanup: no disposable release, Namespace, reader, probe, PVC, PV, or PV
  claim reference remained; the retained Operator and SHC stayed Ready with
  zero restarts; and
- detailed evidence: `SHC92NamespaceScopedHelmQualification.md` and
  `SHC92NamespaceScopedHelmExecPlan.md`.

## SHC-91 immutable qualification inputs

- source branch: `codex/shc-91-deletion-before-pause`;
- source commits: `86a0bc80a` and `a76c30e0c`;
- exact source tip:
  `a76c30e0c2395506cbfbb8d9e2643c186df0a3ef`;
- Operator image tag:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc91-a76c30e0c`;
- Operator OCI index:
  `sha256:4903f70a95b150c0a29bcd3ac70e063b5c55b6a030399a4636297586dea85cea`;
- linux/amd64 manifest:
  `sha256:6da77f0cdd1a4be2e2e8f6b9fa5f983f4a8824dab12942bb37f2df2cbd467008`;
- EKS cluster/context: `vivek-spl-301372` in `us-west-2`, context
  `shc85-vivek-spl-301372`;
- disposable namespaces: `shc91-active-delete`,
  `shc91-namespace-delete`, and `shc91-real-delete`;
- runtime digest for the real Standalone:
  `sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`;
- Linux gate: 42 Ginkgo suites, 185 controller specs, zero failures, 78.3
  percent composite coverage, successful build, and 124 Helm tests;
- EKS result: all seven paused tiers finalized under direct and
  namespace-first deletion without a manual patch or status/Reconciler error;
  the direct fixture completed in 5 seconds and the Namespace fixture was
  absent at 13 seconds;
- real workload result: a Ready zero-restart Standalone finalized both bound
  PVCs; Pod and PVCs were absent at 49 seconds and both delete-reclaim PVs were
  absent at 73 seconds;
- cleanup: all three disposable namespaces and every PV claim reference were
  absent, while retained SHC-85 workloads remained Ready with zero restarts;
  and
- detailed evidence: `SHC91DeletionBeforePauseQualification.md`.

## SHC-90 immutable qualification inputs

- source branch: `codex/shc-90-namespace-termination-guard`;
- source commits: `7ce2483f7` and `0c291c8c8`;
- exact source tip:
  `0c291c8c87ceb629bb573fcf036c6048c28cedf2`;
- Operator image tag:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc90-0c291c8c8`;
- Operator OCI index:
  `sha256:c2438c14e238e101cba52d758968a2cd7c64fc2798ed5a0a4781acb3e836e764`;
- linux/amd64 manifest:
  `sha256:a05c2197a9754d89a93ad2652933eea224ae071fbcf2c98239a61bdb1bdd99a4`;
- EKS cluster: `vivek-spl-301372` in `us-west-2`;
- disposable namespace: `shc90-namespace-termination`;
- runtime digest:
  `sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`;
- Linux gate: 42 Ginkgo suites, 180 JUnit nodes, zero failures, 78.1 percent
  composite coverage, successful build/vet/generation, and 124 Helm tests;
- EKS result: five LicenseManager and five SearchHeadCluster preflight guard
  records in the real propagation window, zero fixture-level error or
  Reconciler error, both CR finalizers completed, all ten PVC/PV claim
  references disappeared, and the Namespace completed naturally without a
  manual patch; and
- detailed evidence: `SHC90NamespaceTerminationQualification.md`.

## SHC-89 immutable qualification inputs

- source branch: `codex/shc-89-paused-status`;
- source commit: `3e171673794c6bd9b570c7d94abd6bc9292ab147`;
- Operator image tag:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc89-3e1716737`;
- Operator OCI index digest:
  `sha256:b83bbb97f89dca45e183e895e4be7e1d7bd11007f08babb41c4c94c97d18f145`;
- Linux/amd64 manifest:
  `sha256:ff1766db777a9211df4a4760819f78237159ad1c9bee74837470f7817268ce71`;
- EKS cluster: `vivek-spl-301372` in `us-west-2`;
- disposable namespace: `shc89-paused-status`;
- runtime digest:
  `sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`;
  and
- detailed evidence:
  `SHC89PausedStatusQualification.md`.

## SHC-87 immutable qualification inputs

- source branch: `codex/shc-87-dependency-status`;
- source commit: `20d926658bdb7bd0a617a471acea1f83644149ce`;
- Operator image tag:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-87-20d926658`;
- Operator OCI index digest:
  `sha256:fbb1a53c45da509fee47edc618eefd93923fc3864df9533dc85dbcbc8914c2a3`;
- Linux/amd64 manifest:
  `sha256:ee4bf98bfc9c0bb8b56327ee0ae8223c9849a19462cf582ce75736d78ec716d5`;
- EKS cluster: `vivek-spl-301372` in `us-west-2`;
- disposable namespace: `shc87-dependency-status`;
- runtime digest:
  `sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`;
  and
- detailed evidence:
  `SHC87DependencyStatusQualification.md`.

## SHC-75 immutable qualification inputs

- source branch:
  `codex/shc-75-captain-transfer-timeout-qualification`;
- final source before this documentation commit:
  `3e9e735a776eb90957a0d0d2722b28ce0da5baff`;
- Operator image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-reliability-3e9e735a7`;
- Operator image digest:
  `sha256:98b71dbbb394d51abea5e79a9f63e4423f43ae3f623d5ed3d28cb9d55c0b6f72`;
- EKS cluster: `vivek-spl-301372` in `us-west-2`;
- qualification namespace: `shc75-captain-timeout`;
- runtime image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk:shc-prestart-7951d69-ansible-9954434-splunk-10.6.0.0-d9be152689b7`;
- Linux gate: `make fmt vet build test`, 41 Ginkgo suites, 154 controller
  specifications, zero failures, 78.5 percent composite coverage; and
- EKS result: forward `2 -> 1`, captain timeout failed closed, original
  captain UID preserved, revision withdrawal restored it in place, rollback
  `2 -> 1`, maximum unavailable `1/1`, expected Event deltas only, zero
  container restarts, and 321 continuous seconds of final stability.

## SHC-76 immutable qualification inputs

- source branch:
  `codex/shc-76-post-authorization-revision-withdrawal`;
- final source before this documentation commit:
  `50eb10514a550d67652663cd7ab6644313681dcc`;
- Operator source commits:
  `24eea3f37ddb95032cb495dc0b422e8ca3cf9116`,
  `243f7a5d295196e1003ea70a37947bb04bed681c`, and
  `50eb10514a550d67652663cd7ab6644313681dcc`;
- Operator image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-reliability-50eb10514`;
- Operator image digest:
  `sha256:62e450584a9788cd9b0f2959164bdcef2c75608c66bb468cc572e887712d7624`;
- EKS cluster: `vivek-spl-301372` in `us-west-2`;
- accepted qualification namespace: `shc76-revision-withdrawal`;
- runtime image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk:shc-prestart-7951d69-ansible-9954434-splunk-10.6.0.0-d9be152689b7`;
- Linux gate: `make fmt vet build test`, 41 Ginkgo suites, 154 controller
  specifications, zero failures, 78.5 percent composite coverage;
- pre-action gate: 180 seconds, 25 healthy samples, three Ready and serving
  members, matching StatefulSet revisions, partition three, an authoritative
  dynamic captain, and zero restarts;
- STS-014 result: revision A was authorized for ordinal two; revision B was
  submitted during its replacement; the StatefulSet retained revision A and
  partition two until the first replacement was Ready, serving, registered,
  and `Up`; revision B then received a separate operation and authorization;
  and the rollout completed ordinals `2 -> 1 -> 0`;
- availability result: 127 successful service searches, zero failures,
  minimum two Ready endpoints, maximum one unavailable Pod, zero container
  restarts, and zero conflicting rollout Events in the run window; and
- final result: dynamic captain on ordinal one, all members `Up`,
  `service_ready_flag=1`, no Splunk rolling restart, KV Store `ready` with
  three members and no upgrade or backup, followed by a 300-second gate with
  37 successful samples and three Ready endpoints throughout.

The first destructive run exposed a real boundary error: Splunk-side lifecycle
`Completed` could precede the replacement Pod's Kubernetes Ready and serving
conditions, allowing the queued template to be released early. Commit
`50eb10514` closes that gap. A second run was intentionally excluded because
the just-formed baseline transiently lost member readiness before any
lifecycle operation; the Operator kept partition three and reported
`ExistingUnavailablePod` without authorizing disruption. The accepted third
run began only after the sustained pre-action gate.

## SHC-77 immutable qualification inputs

- source branch:
  `codex/shc-77-image-pull-classification`;
- final source before this documentation commit:
  `4710438a031e77f0906a4eaf26d5821ee70d0ed8`;
- Operator source commit:
  `b3ae4b291`;
- Operator image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-reliability-4710438a0`;
- Operator image digest:
  `sha256:2d9af851e07bbf891b03ad07bec0c849f973280bb92cf03e344620ecbf6154b7`;
- EKS cluster: `vivek-spl-301372` in `us-west-2`;
- accepted qualification namespace: `shc77-image-pull`;
- runtime image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk:shc-prestart-7951d69-ansible-9954434-splunk-10.6.0.0-d9be152689b7`;
- runtime image digest:
  `sha256:c295389a5bbcaa0aade25b0a5950952794179059564a525a7200b6f1c26b3547`;
- transient desired tag:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk:shc77-runtime-4710438a0`,
  resolving to the pinned runtime digest before and after fault injection;
- Linux gate: `make fmt vet build test`, 41 Ginkgo suites, 154 controller
  specifications, zero failures, and 78.5 percent composite coverage;
- retryable result: ordinal two remained the only authorized target through
  real kubelet `ErrImagePull` and `ImagePullBackOff` for 60 seconds, recovered
  when the exact desired tag was restored, and the rollout then completed
  ordinals `2 -> 1 -> 0` with captain transfer before ordinal zero;
- terminal result: invalid image syntax produced `InvalidImageName` and
  immediate `Blocked/ImagePullFailed` at ordinal two, partition remained two,
  and no later ordinal became eligible;
- availability result: 131 successful Service searches, zero failures,
  minimum two Ready endpoints, maximum one unavailable Search Head, and an
  unchanged Ready Deployer with zero restarts; and
- cleanup result: the qualification namespace and its persistent volumes were
  removed, all worker nodes were schedulable, and the transient ECR tag was
  absent after the campaign.

The qualification preserved the production image-upgrade safety boundary:
without an authoritative compatibility provider, the Operator continued to
report an unknown upgrade path rather than infer compatibility from image
tags. Fault injection instead exercised a first-pull failure of the already
authorized StatefulSet replacement. It also used the default readiness timing;
an earlier non-accepted run showed that increasing the readiness failure
window could leave a Pod in Service endpoints while its local management port
was refusing connections.

## SHC-78 immutable qualification inputs

- source branch:
  `codex/shc-78-pod-infrastructure-attribution`;
- implementation commit:
  `7b90da2694c1460b5e1522b5abb0a2d2151b190c`;
- final source used for the qualification image:
  `a5a41c07c9c7a9a1e1776f5cc41a146db6616da5`;
- Operator image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-reliability-a5a41c07c`;
- Operator image digest:
  `sha256:e29ac1024865e4f676655c229b01b8ed2690abe5412a669df2d473f074f6207f`;
- exact cluster-wide RBAC:
  `get`, `list`, and `watch` on
  `storage.k8s.io/volumeattachments`;
- EKS cluster: `vivek-spl-301372` in `us-west-2`;
- accepted qualification namespace: `shc78-infrastructure`;
- accepted runtime image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk:9.4.1-jdk-11`;
- accepted runtime digest:
  `sha256:e51312c90d8cd860065a0fcb887a50c3d227122477b2ca3f5a7336f93d9308cb`;
- Linux gate: `make fmt vet build test`, 41 Ginkgo suites, 154 controller
  specifications, zero failures, and 78.5 percent composite coverage;
- scheduler result: all workers were cordoned, ordinal two remained the only
  target at `WaitingForScheduling/PodUnschedulable` for six samples, and
  uncordoning completed `2 -> 1 -> 0` with a captain transfer, three Ready
  endpoints, HTTP 200 search, and zero restarts;
- storage result: the newly bound ordinal-two PV and scheduled node matched
  exactly one `VolumeAttachment` with `attached=false`; the target reported
  `PodReadyToStartContainers=False`; six samples remained at
  `WaitingForStorage/VolumeAttachmentPending`; ordinal-zero and ordinal-one
  UIDs were unchanged; minimum Ready endpoints stayed at two; and every search
  returned HTTP 200;
- recovery result: restoring the EBS CSI controller from zero to two replicas
  advanced the same target through generic Pod infrastructure and container
  startup, then to Kubernetes Ready, registered `Up`, KV Store `ready`, and
  three Service endpoints before another replacement began; and
- cleanup result: the qualification namespace, all test PVCs/PVs, and the test
  StorageClass were removed; all nodes finished Ready and schedulable; and EBS
  CSI finished at two ready replicas.

The original Splunk 10.6 development runtime was excluded from the accepted
Operator-only result because same-version Pod restart left the supported KV
Store status at `starting` even while the external database process remained
alive and continued successful database pings. The Operator correctly held
`ValidatingCluster/KVStoreNotReady` and preserved search availability. No
Splunkd change or weakened KV gate is part of SHC-78.

## SHC-79 immutable qualification inputs

- source branch: `codex/shc-79-normalize-volume-defaults`;
- integrated feature baseline:
  `884427c05`;
- registration commit:
  `96c16b49b`;
- implementation source and exact source used for the qualification image:
  `a59fc5103b9199b2a136601ebfbdde1d593c4cc8`;
- Operator image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-79-a59fc5103`;
- Operator image digest:
  `sha256:e1b77c45bba3853f96a7ac93ef5d98ac84ebde9ca991d1fbd10a847865767ede`;
- EKS cluster: `vivek-spl-301372` in `us-west-2`, Kubernetes
  `v1.31.14-eks-7d6f6ec`;
- accepted qualification namespace: `shc79-volume-defaults`;
- accepted runtime image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk:9.4.1-jdk-11`;
- accepted runtime digest:
  `sha256:e51312c90d8cd860065a0fcb887a50c3d227122477b2ca3f5a7336f93d9308cb`;
- feature gates:
  `SplunkPodLifecycle=true,SearchHeadClusterLifecycle=true`;
- Linux gate: `make vet build test`, 41 Ginkgo suites, 154 controller
  specifications, zero failures, and 78.6 percent composite coverage;
- API-defaulting result: the SHC CR continued to omit
  `volumeMode` from its generic ephemeral `volumeClaimTemplate`, while the
  API-server-returned Deployer and Search Head StatefulSets both contained
  `volumeMode: Filesystem`;
- reconciliation result: the Deployer and Search Head StatefulSets remained at
  generation one with one ControllerRevision each, unchanged matching
  current/update revisions
  `splunk-shc79-deployer-c96f56679` and
  `splunk-shc79-search-head-fc79bcf47`, and zero
  `pod Volumes differ` log records;
- controller-recovery result: the Operator Pod changed UID from
  `b52ff38e-8d05-4f84-a2a4-959d133cd217` to
  `0402be07-3c2f-44ee-8e7e-7d181263291e` and resumed with the same pinned
  image digest without revising or replacing either StatefulSet;
- stability result: six post-restart samples from
  `2026-07-29T17:21:54Z` through `2026-07-29T17:24:28Z` retained the same four
  workload Pod UIDs, zero container restarts, four Ready Pods, three Search
  Head Service endpoints, an initialized Ready SHC with a ready dynamic
  captain, and successful HTTP 200 searches;
- semantic-safety result: source tests prove comparison does not mutate the CR
  or observed StatefulSet volume slices, treats explicit Kubernetes defaults
  as equal to omitted fields, preserves explicit non-default differences, and
  prevents `MergePodSpecUpdates` from requesting an update solely because the
  API server defaulted generic-ephemeral `volumeMode`; and
- cleanup result: CR-first deletion removed all four Pods, all twelve PVCs,
  and all twelve associated PVs before namespace deletion. The namespace was
  then removed, all three workers remained Ready and schedulable, and the EBS
  CSI controller finished at two ready replicas.

## SHC-80 immutable qualification inputs

- source branch:
  `codex/shc-80-authorized-revision-recovery`;
- integrated feature baseline:
  `9eecde5d68e9dc889bb2b2f1913420396e00cb21`;
- registration, implementation, forced-rollback, and queued-revision-release
  commits:
  `d1f6e301d`, `744bfb096`, `9be744f06`, and
  `0b9253f1181947348c43eec7894ff1a9abd65366`;
- exact source used for the final qualification image:
  `0b9253f1181947348c43eec7894ff1a9abd65366`;
- Operator image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-80-0b9253f11`;
- Operator image digest:
  `sha256:fecf5134468a2478c0de13ad88b463b8f2db38747d795e60aae3304a0b9986cb`;
- EKS cluster: `vivek-spl-301372` in `us-west-2`, Kubernetes
  `v1.31.14-eks-8f14419`;
- accepted qualification namespace:
  `shc80-authorized-recovery-v2`;
- accepted runtime image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk:9.4.1-jdk-11`;
- accepted runtime digest:
  `sha256:e51312c90d8cd860065a0fcb887a50c3d227122477b2ca3f5a7336f93d9308cb`;
- feature gates:
  `SplunkPodLifecycle=true,SearchHeadClusterLifecycle=true`;
- Linux gate: `make fmt vet build test`, 41 Ginkgo suites, 154 controller
  specifications, zero failures, and 78.5 percent composite coverage;
- revision sequence: last-known-good revision
  `splunk-shc80-search-head-8659646985`, failed authorized revision
  `splunk-shc80-search-head-b6d6d44d9`, and queued desired revision
  `splunk-shc80-search-head-6987ddbf74`;
- injected failure: all three workers were cordoned after healthy formation;
  ordinal two was authorized for the failed revision and remained the only
  Pending, unschedulable replacement while both non-target peers remained
  Ready and serving. The queued revision was submitted before recovery;
- durable recovery result: the controller raised the recovery partition to
  three, deleted only the failed target after observing that barrier, and
  retained the operation ID, original and replacement Pod UIDs, desired and
  recovery revisions, target member GUID, and withdrawal timestamp across a
  real Operator Pod replacement. After workers were uncordoned, ordinal two
  rejoined at the last-known-good revision with GUID
  `E308A2D4-49A3-4595-A71F-7D4B7AE01FDB`;
- queued rollout result: the completed historical recovery record no longer
  held the recovery deletion path. The queued revision then completed
  ordinals `2 -> 1 -> 0`; partition changes were authorized one at a time;
  captaincy moved before both captain replacements; the StatefulSet converged
  with current and update revision equal and reset partition to three;
- identity and Splunk result: all final members were registered `Up` with
  `NoRestart`; their GUIDs remained
  `E35DC033-3CEF-4ACE-B9EE-A7ABAE5F9AB2`,
  `B723CD8C-7BB0-4190-BA67-8919769A583E`, and
  `E308A2D4-49A3-4595-A71F-7D4B7AE01FDB`; the dynamic captain reported
  initialized, minimum peers joined, and service ready, with no rolling
  restart or maintenance mode;
- availability result: 187 continuous Service searches returned HTTP 200,
  with zero failures, minimum two serving endpoints, maximum one unavailable
  Search Head, and zero workload or Operator container restarts;
- final stability result: 21 clean samples from
  `2026-07-29T21:40:43Z` through `2026-07-29T21:46:52Z`, spanning 369 seconds,
  retained a Ready three-member CR, three endpoints, equal StatefulSet
  revisions, partition three, and zero workload or Operator restarts; and
- cleanup result: CR-first deletion removed four workload Pods, eight PVCs,
  and all eight associated PVs before namespace deletion. No test SHC or PV
  remained; all three workers finished Ready and schedulable, and the EBS CSI
  controller finished at two ready replicas.

## SHC-81 immutable qualification inputs

- source branch:
  `codex/shc-81-termination-safe-finalization`;
- integrated feature baseline:
  `efbff783f02be7cee29c45c793e5cd2886dd2325`;
- registration and implementation commits:
  `ec27268a3b3e25d41b0da539ec0dcdd5ed430c01`,
  `d053ff65be837ed0e2e23b163c9b53fb2f9492f8`,
  `33ff143d18d448961d33c1774602863420e79ac0`, and
  `58437e3ad6e63a7d244bb75b180b180325b47de2`;
- exact source used for the final qualification image:
  `58437e3ad6e63a7d244bb75b180b180325b47de2`;
- Operator image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-81-58437e3ad`;
- Operator image digest:
  `sha256:f2ffee5a6cc7d33b2aa26e8cbdab81618a3785e31600d7a676ed3ec149c52b6d`;
- EKS cluster: `vivek-spl-301372` in `us-west-2`;
- accepted qualification namespace and CR:
  `shc81-termination-finalization-v4` and `shc81v4`;
- runtime image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk:shc-prestart-7951d69-ansible-9954434-splunk-10.6.0.0-d9be152689b7`;
- runtime image digest:
  `sha256:c295389a5bbcaa0aade25b0a5950952794179059564a525a7200b6f1c26b3547`;
- feature gates:
  `SplunkPodLifecycle=true,SearchHeadClusterLifecycle=true`;
- Linux gate: `make vet`, `make build`, and `make test`, 41 Ginkgo suites,
  155 controller specifications, zero failures, and 78.5 percent composite
  coverage;
- precondition: two consecutive samples retained CR phase `Ready`, three
  ready Search Heads, three registered `Up` members, a ready dynamic captain,
  minimum peers joined, three serving endpoints, and zero container restarts.
  The CR was then paused with its supported annotation and retained the same
  health before deletion;
- finalization result: direct namespace deletion began at
  `2026-07-30T00:05:34Z`. The deleting CR routed to finalization before normal
  validation, tolerated already-absent owned resources, deleted all eight
  declared PVCs, removed its finalizer, and was absent by
  `2026-07-30T00:05:45Z`;
- safety result: the accepted window contained no
  `NamespaceTerminating`, post-finalization `StorageError` or precondition
  failure, `ApplySplunkConfigFailed`, `DeleteFailed`, or failed stalled
  condition write. The finalizer created no namespace content and did not run
  per-member detention, captain transfer, or consensus-removal workflows for
  whole-CR deletion;
- cleanup result: the namespace, four workload Pods, eight PVCs, and all eight
  exact PVs were absent by `2026-07-30T00:06:14Z`, about 40 seconds after the
  deletion request; and
- infrastructure result: all three EKS workers remained Ready and schedulable,
  both EBS CSI controller replicas and all three node daemons remained Ready,
  and those components recorded zero restarts.

## SHC-83 immutable qualification inputs

- source branch:
  `codex/shc-83-startup-readiness-qualification`;
- selection commit:
  `23c905bf61f669f21d444397bd672e81397363e8`;
- implementation commits:
  `635b81bc490d12dd47b6eda636d1dc32f323d47e`,
  `daf6b06082c16cc66f11a32817e4bba916c4d244`,
  `0a2465cbe71d4f414099f3a0686fe92830652d78`,
  `85b00fd9fdd718d34c5d0f38dfbcbf1ffb762c6c`,
  `2889c80025bbf1e9010dc8722a10b35320e39195`;
- qualification-harness commits:
  `c302ead2a26c2f780e78a8c98fda1b7d6383509d`,
  `0aa4917218b7825b50c01afefe465b3450b8dcc1`,
  `7511bdc0788e01f0c60a0a52979669c6d1689437` and
  `f20b29e679d510f8e7a166fafad4d90a4562052e`;
- exact source used for the qualification image:
  `2889c80025bbf1e9010dc8722a10b35320e39195`;
- Operator image digest:
  `sha256:22a4398917a3dc27bdbe68aa4513c70b2bfd4d62f05a474e55fd6f9600db7ae9`;
- EKS cluster: `vivek-spl-301372` in `us-west-2`;
- qualification namespace and CR:
  `shc83-startup-readiness` and `shc83-shc`;
- runtime image:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk:shc85-f063cfd-ansible-5e9e12f-splunkcloud-10.5.2605.0-844c593e9c1d`;
- runtime image digest:
  `sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`;
- feature gates:
  `SplunkPodLifecycle=true,SearchHeadClusterLifecycle=true,IndexerClusterLifecycle=true`;
- source gates: local and Linux `make fmt vet build test`, 41 Ginkgo
  suites, 155 controller specifications, zero failures, and 78.5 percent
  composite coverage;
- fresh-formation result: 99 samples from
  `2026-07-30T22:23:38Z` through `2026-07-30T22:40:15Z`, zero client endpoints
  and zero serving gates before `Complete`, exactly one
  `SHCInitialFormationRestartStarted` Event, zero Kubernetes container
  restarts, and twelve stable final samples with three endpoints;
- established non-captain recovery: ordinal one changed UID, the two
  unaffected members stayed in the EndpointSlice, the replacement remained
  withheld until it rejoined as registered and `Up`, endpoint count never
  fell below two, and twelve stable samples returned three endpoints;
- active-captain recovery: ordinal zero changed UID, the two unaffected
  members stayed in the EndpointSlice, endpoint count never fell below two,
  Splunk elected ordinal two as captain, and twelve stable samples returned
  three endpoints;
- controller-restart result: the Operator Pod changed UID while all three
  Search Head UIDs, all three endpoints, zero Search Head restarts, formation
  stage `Complete`, and `lastStableReplicas=3` remained unchanged; and
- evidence files on the qualification workstation:
  `build/_test/shc83/startup-readiness-2889c8002.tsv`,
  `build/_test/shc83/established-recovery-2889c8002.tsv`,
  `build/_test/shc83/captain-recovery-2889c8002.tsv`, and
  `build/_test/shc83/controller-restart-2889c8002.tsv`.

## SHC-84 immutable qualification inputs

- source branch:
  `codex/shc-84-startup-term-qualification`;
- accepted Operator binary source:
  `67c0d3bd28c3d88a72d629ffb1245a139399fc0d`;
- evidence-monitor source:
  `cbaef60af652a17acdefe31a608cc8ced265c4f1` and
  `524636f3938e87ae180b286d5ad4007aaef7de9e`;
- supported-upgrade source fixture:
  `4718cef6fd2c4a738dca80dc163b7f55e77525f4`;
- Operator image digest:
  `sha256:d83ae44c825f13cb12117e72d2ca5415b4ffd9b7af36bcab7e81226e11e6cafe`;
- EKS cluster: `vivek-spl-301372` in `us-west-2`, Kubernetes
  `v1.31.14-eks-8f14419`;
- accepted namespace:
  `shc84-startup-term-candidate`;
- fixed target runtime digest:
  `sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`;
- supported source runtime:
  `10.4.2604.0/60dd7967c086`, digest
  `sha256:04b0a011f27e4cfb9930d1dd8c430d5da11ef596d08c6b98f98184589d727a9a`;
- supported upgrade namespace:
  `shc84-upgrade-candidate`;
- Linux gate: `make generate manifests`, `make fmt vet build`,
  `git diff --exit-code`, and `make test`: 41 suites, 156 specifications,
  zero failures, 78.6 percent composite coverage;
- rendered contract: startup failure threshold 60, startup and liveness probe
  grace 660, readiness probe grace unset, and Pod grace 1200;
- fresh formation: zero container restarts and twelve stable
  `Ready`/`Complete` samples with three endpoints;
- forced liveness: only non-captain ordinal two restarted, exactly once with
  unchanged Pod UID, while two peer endpoints remained serving;
- planned deletion: only non-captain ordinal one changed Pod UID, while two
  peer endpoints remained serving, before registered/`Up` and client readiness
  returned;
- supported source formation: ordinal zero accumulated 29 startup failures
  without restarting, and the cluster reached twelve stable three-endpoint
  samples with zero restarts;
- LicenseManager prerequisite upgrade: the dependency changed to the target
  digest while all Search Head UIDs, restart counts, and three endpoints
  remained unchanged;
- supported Search Head upgrade: ordinals `2 -> 1 -> 0` were replaced one at a
  time, endpoint count never fell below two, captaincy moved from ordinal zero
  to ordinal two, all replacement containers retained zero restarts, and final
  state had three registered `Up` target members;
- workload evidence: 200/200 authenticated Service searches returned HTTP 200
  with non-empty responses during the sampled portion of the upgrade; and
- evidence files on the qualification workstation:
  `build/_test/shc84/candidate-fresh-fixed.tsv`,
  `build/_test/shc84/candidate-forced-liveness-fixed.tsv`, and
  `build/_test/shc84/candidate-planned-delete-fixed.tsv`,
  `build/_test/shc84/upgrade-source-formation.tsv`,
  `build/_test/shc84/license-manager-upgrade.tsv`,
  `build/_test/shc84/supported-upgrade.tsv`,
  `build/_test/shc84/supported-upgrade-search.tsv`, and
  `build/_test/shc84/post-upgrade-validation.txt`.

## Next execution records

SHC-79 through SHC-81 record separate gaps discovered by the accepted SHC-78
campaign. All three are now source- and EKS-qualified on their isolated
branches.
SHC-82 records a separate customer-reported App Framework availability
requirement that spans both Search Head and indexer clusters. SHC-83 and
SHC-84 preserve two distinct runtime-contract gaps exposed during repeated
formation and finalization campaigns: early traffic readiness, and the
interaction among startup duration, kubelet restart policy, and prompt
process exit. SHC-85 preserves the independently bounded indexer
serving-readiness/lifecycle-progress gap exposed by SHC-82. SHC-85 is now
source-qualified and EKS-qualified for the bounded Operator-owned steady path,
controller restart during `Decommissioning`, five-minute controller absence
at all four durable lifecycle stages, bounded API-server disconnection at
observed `Decommissioning`, and one normal two-contender leader takeover at
that stage. SHC-82 remains selected on its isolated evidence branch. SHC-83 is
source- and EKS-qualified for its bounded current-v4 contract. SHC-84 is
source- and EKS-qualified for its bounded current-v4 contract and the exact
supported 10.4-to-10.5 upgrade recorded above. SHC-86 records the independently
observed LicenseManager namespace-finalization gap and is bounded source- and
EKS-qualified, with the earlier namespace-transition window now separated as
SHC-90. SHC-87 closes the bounded retryable dependency-status classification
gap on its isolated source and qualification branch.
SHC-88 closes the bounded LicenseManager health-check endpoint mismatch on its
isolated source and qualification branch; an intentionally expired-license
EKS fixture and broader LicenseManager lifecycle work remain outside that
bounded result. SHC-89 closes the bounded paused-at-creation status gap across
the seven active v4 Splunk reconcilers. SHC-90 closes the bounded namespace
propagation guard at source `0c291c8c8`. SHC-91 closes deletion-before-pause
and deletion-before-ordinary-Apply ordering at source `a76c30e0c` for the
seven active v4 tiers, with the bounded all-tier and real-Standalone EKS
evidence recorded above.
Registration or assignment alone does not claim implementation. Each
remaining item must use its own branch and immutable source commit.

Other remaining scenarios continue to be selected from
`SHCTestScenarioMatrix.md`; the absence of a new `SHC-*` number does not make a
scenario complete.

## Revision Note

2026-07-29 UTC: Extended the central SHC-60 through SHC-81 execution
registry, linked implementation commits to stable scenario identifiers,
recorded qualification scope without claiming production readiness, recorded
the retryable and terminal image-pull classification results, recorded SHC-78
source and EKS qualification, and registered the independently bounded
template-defaulting, authorized-revision-withdrawal, and deletion-finalization
follow-up gaps without claiming them as implemented.

2026-07-29 UTC: Selected SHC-79 on
`codex/shc-79-normalize-volume-defaults` from integrated feature baseline
`884427c05`. No implementation is claimed by this branch-registration record.

2026-07-29 UTC: Recorded SHC-79 implementation source
`a59fc5103b9199b2a136601ebfbdde1d593c4cc8`, the complete Linux source gate,
and accepted EKS qualification of Kubernetes volume-default normalization,
including exact API defaulting evidence, a real Operator restart, six stable
post-restart samples, successful searches, and zero workload replacement or
restart.

2026-07-29 UTC: Selected SHC-81 on isolated branch
`codex/shc-81-termination-safe-finalization` from integrated feature baseline
`efbff783f02be7cee29c45c793e5cd2886dd2325`. This registration does not claim
implementation or qualification.

2026-07-30 UTC: Recorded SHC-81 source and EKS qualification. Direct namespace
deletion of a paused, healthy three-member SHC routed through a no-create
finalization path, removed all declared PVCs and PVs, avoided stale
post-finalization status writes, and completed without namespace-termination
or storage-precondition errors. Registered SHC-83 and SHC-84 separately for
the observed early-ready startup interval and the startup-budget/TERM-exit
contract; neither follow-up is claimed as implemented.

2026-07-30 UTC: Selected SHC-82 on isolated branch
`codex/shc-82-appframework-restart-availability` from integrated feature
baseline `079e26233267`. Selection records the evidence boundary only; it does
not claim implementation, qualification, or a chosen restart policy.

2026-07-29 UTC: Registered SHC-82 and OPS-011 from a customer-reported
App Framework restart-availability concern. The record deliberately preserves
the observed `searchable=0` and `force=0` signal without treating that one log
line as proof of replica loss, data unavailability, or root cause. Source,
Splunk semantic, active-search, and end-to-end qualification work remains
pending.

2026-07-30 UTC: Corrected the SHC-82 qualification LicenseManager setup. The
ClusterManager, IndexerCluster, SHC deployer, and all Search Heads already
received the intended LicenseManager Service reference. The built-in trial
license did not support remote-manager operation and caused repeated peer
usage rejection. A Secret-backed development license with remote-manager
capability removed that environmental failure while continuous SHC Service
searches remained successful. This is partial qualification evidence only;
the remaining App Framework availability and negative-case gates stay open.

2026-07-30 UTC: Ran the first versioned SHC-82 App Framework update with
continuous numbered ingestion and exact-result searches. All 120 HEC events
were accepted and later found exactly once; Pod UIDs were unchanged and
Kubernetes reported zero container restarts. Splunk nevertheless restarted
Search Heads internally in order `2 -> 1 -> 0`, transferring captaincy
`0 -> 2 -> 0`. Eleven Service searches failed. The readiness probe initially
left short splunkd outages advertised, then the Operator's cluster-wide
captain check marked every member `ClusterNotReady`, yielding nine
zero-endpoint samples. The same package did not restart indexers: each peer
reported `restart_required=0`. Added a deterministic completeness monitor and
separated container, Pod, serving-gate, and EndpointSlice observations. This
is failure evidence that bounds the next fix; it is not a completed
qualification claim.

2026-07-30 UTC: Qualified the first serving-gate correction at source
`0fc1bcf31` and Operator image digest
`sha256:c55ebe692659300121eef74f2e6897dbc27bdbae15bcfe40c0ae8c3566c02690`.
The `1.0.2` App Framework run preserved at least two Search Head Service
endpoints throughout the internal `2 -> 1 -> 0` restart and eliminated the
previous zero-endpoint captain-transition outage. It accepted all 120 HEC
events, recovered `count=120`, `min=1`, `max=120`, and `distinct=120`, kept
every Pod UID unchanged, and recorded zero Kubernetes container restarts.
One Service search was interrupted. Splunk requested the captain's own restart
at `08:00:08.429Z`; the search was admitted on that captain at
`08:00:11.941Z`; and Splunk terminated its streamed dispatch at
`08:00:13.779Z` with `Local side shutting down`. The effective SHC setting was
`rolling_restart=restart`. This proves that the cluster-wide readiness
correction works, while selection and drain of a Splunk-managed restart target
remain open.

2026-07-30 UTC: Recorded the first isolated indexer-side SHC-82 campaign in
`SHC82AppFrameworkIndexerQualification.md`. On a four-peer RF3/SF2 cluster,
Splunk's searchable restart retained successful RF/SF/all-searchable
preflight and completed one peer at a time. Existing readiness lost 7 of 55
HEC submissions. An HEC-aware default-timing experiment lost 1 of 55. A fast
2-second, failure-threshold-one experiment completed 55 of 55 and recovered
all sequence numbers exactly once, with unchanged Pod UIDs and zero container
restarts. Peer-level sampling still observed two unavailable-but-advertised
samples and one previous-peer/next-peer overlap with only two remotely serving
HEC peers. Installing the fast probe also caused the generic `OnDelete`
controller path to wait on the intentional readiness withdrawal after
decommission, requiring controlled manual advancement for test purposes.
These results register serving/lifecycle separation, previous-peer
service-recovery gating, and client delivery semantics as open requirements;
they do not claim completion.

2026-07-30 UTC: Recorded SHC-85 EKS qualification on Operator source
`7ff844f4a0ad3fdd33e34443e009d08aff087124` and immutable Operator digest
`sha256:f7e2a4f8444ffa1b335486e266e4ed9e940180f78d460639de5703a8bdb2530b`.
The runtime used official Splunk build `10.5.2605.0/844c593e9c1d` at digest
`sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`.
A same-image replacement reused the populated KV Store volumes and completed
without the prior upgrade-precheck failure. The Operator then advanced a
four-peer RF3/SF2 revision automatically in order `3 -> 2 -> 1 -> 0`, kept one
target withdrawn at a time, required remote serving recovery before selecting
the next target, retained peer GUIDs, and finished with four Ready peers,
RF/SF met, all data searchable, and no fixups. The two workload records
completed 80/80 and 30/30 exact sequences with zero HEC or search-request
failures. This qualifies the bounded Operator-owned lifecycle; it does not
claim control over a Splunk-managed App Framework internal rolling restart or
the remaining negative and compatibility variants.

2026-07-30 UTC: Recorded the isolated SHC-85 controller-restart campaign on
`codex/shc-85-controller-restart-qualification`. The Operator was deleted at
ordinal 3's persisted `Decommissioning` stage. Its replacement resumed the
same operation ID, target Pod UID, revisions, and timestamp; emitted no
duplicate ordinal-3 decommission Event; and completed
`3 -> 2 -> 1 -> 0` with previous-peer remote serving recovery and one
withdrawn target at a time. The primary workload record completed exact
100/100 with zero HEC or search failures, the stable record completed exact
30/30 with zero failures, and the overlapping record completed exact 80/80
while transparently retaining one valid initial empty-result classification.
Final evidence retained four Ready endpoints, RF/SF met, all data searchable,
all peers Up, no fixups, four `failed=0` Ansible recaps, zero prior KV Store
failure signatures, and zero container restarts. Long controller/API
disconnection and the other recorded negative and compatibility variants
remain open.

2026-07-31 UTC: Recorded the isolated SHC-85 five-minute controller-absence
campaign on `codex/shc-85-lifecycle-hold-qualification`. Operator source
`ac1fe0db8` at immutable digest
`sha256:59fc2afdfafc7e0c2b9f49fceebf1862128521017311776b91f0ce3315eff608`
retained the exact ordinal-3 operation, Pod UID, and revisions for 300 seconds
with zero controller replicas. The target stayed running, unready, outside the
Service, and at zero restarts; the other three peers stayed Ready and serving;
and no indexer liveness failure or kubelet kill occurred. Restoring the
controller completed `3 -> 2 -> 1 -> 0` with maximum unavailability one,
remote-serving recovery before every next target, four clean Ansible recaps,
and final RF/SF/all-searchable/no-fixup health. The same campaign corrected
the elected-captain query target and the ordering between fresh successful
captain observation and transfer timeout. By itself it did not qualify
API-server disconnection, controller absence at other stages, or
Splunk-managed restart target selection; later records below qualify the
other bounded controller-absence stages and API disconnection.

The accepted repeat observed 302 seconds without a controller and stored
lifecycle evidence SHA-256
`655c998ab4d6072769d8efa2c47c83c737f919a730ee3a72467f9714b4df9263`.
The independent workload Job spanned the hold and complete roll, submitted
1,800 numbered events with zero HEC or search-request failures and final exact
completeness, and stored workload evidence SHA-256
`8b14b210e1224219ee1509b150036c3f599c68f11bbf22b98cbdce71bf1e3faf`.
It also observed 24 successful-search count regressions and a maximum
sequence-to-count gap of 362 while all Search Heads logged peer connectivity
or authentication-convergence errors. That immediate-completeness and
partial-result contract remains open; no production fix is claimed for it.

2026-07-31 UTC: On isolated branch
`codex/shc-85-decommissioning-absence-qualification`, harness source
`8d6a7dbc6` waited until ordinal 3 had persisted both `Decommissioning` and
`observedDecommissioning=true`, then removed the controller for 306 observed
seconds. The same operation and target survived with zero restarts and three
unchanged serving peers. Restoration completed `3 -> 2 -> 1 -> 0`; lifecycle
evidence SHA-256 is
`e457b347092503b7b4ddbec25047e3dbc1b120bc0293fb5a1cb82cd5a589bdde`.
The overlapping independent Job submitted 1,800 events with zero HEC or
search-request failures and exact eventual results on each Search Head;
workload-log SHA-256 is
`cf169d21801d25eef3314351e6b5726bb53b8ca993ac1d2297f7c8bd728d4be0`.
It reported 41 count regressions and maximum pending 406. The maximum occurred
after lifecycle `Completed` with four Ready desired-revision Pods while Search
Heads still attempted old peer IPs. This closes the bounded long
`Decommissioning` absence gate, not the immediate-completeness contract.

2026-07-31 UTC: On isolated branch
`codex/shc-85-withdrawing-readiness-absence-qualification`, harness source
`978d71bc5699382df3b8d54355541aea0365f503` removed the controller after the
durable `WithdrawingReadiness` stage and explicit lifecycle marker were
present. Kubelet made the running target NotReady and EndpointSlice removed it
with no controller. The exact ordinal-3 operation survived 306 seconds, the
other three peers remained serving, and restoration completed
`3 -> 2 -> 1 -> 0` with zero restarts. Lifecycle evidence SHA-256 is
`4bba7447b3c245621982bf92d0bf13bc020fdf49e3e49d1c5ac3bf07af0b3752`.
The overlapping 1,800-event Job had zero HEC/search request failures and exact
eventual results on every Search Head; workload-log SHA-256 is
`7b2c7ae19ce41efda8ddb21a2e67d29192fb8589128511fbc17c57ebc034ac7a`.
It reported 37 count regressions and maximum pending 404. This closes the
bounded `WithdrawingReadiness` absence gate, not the immediate-completeness
contract. A later record separately qualifies bounded API-server
disconnection.

2026-07-31 UTC: On isolated branch
`codex/shc-85-target-selected-absence-qualification`, harness sources
`2d430748b` and `770a27799` captured the short persisted `TargetSelected`
stage and removed the controller for exactly 300 seconds. All four original
Pods remained Ready and serving at zero restarts with no readiness-withdrawal
marker. Restoration resumed the same operation and completed
`3 -> 2 -> 1 -> 0`. Lifecycle evidence SHA-256 is
`01f3cf1fe9330b2a139a2243d2ca3f5771bfada39ee9d25bd267410d52ef9c0e`.
The overlapping 1,800-event Job had zero HEC/search request failures and exact
eventual results on every Search Head; workload-log SHA-256 is
`d0d8de5eb851bea87a9057f0676e1b5d5f6e16a7ea134e0130d4af04ea6b2c3d`.
It reported 18 count regressions and maximum pending 364 after lifecycle
`Completed`. This closes the bounded `TargetSelected` controller-absence gate,
not desired-state conflict or immediate distributed-search completeness. The
next record separately qualifies bounded API-server disconnection.

2026-08-01 UTC: On isolated branch
`codex/shc-85-api-disconnection-qualification`, harness commits `8e21b9b1b`
through `f78828cc1` applied a fail-safe, exact-destination API Service block
inside the Operator Pod. The API path was unavailable for 401 seconds at
observed ordinal-3 `Decommissioning`; the manager lost its lease and restarted
once in the same Pod, while 36 hold observations retained the exact operation
and target for 302 seconds with three serving peers. API recovery resumed and
completed `3 -> 2 -> 1 -> 0` with ten stable samples. The 1,800-event workload
had zero request failures and exact eventual results on all three Search Heads,
but reported 30 count regressions and maximum pending 417. This closes the
bounded K8S-006 gate at one lifecycle stage, not the other API-partition
variants, desired-state conflict, or immediate distributed-search
completeness.

2026-08-01 UTC: On isolated branch
`codex/shc-85-leader-failover-qualification`, harness source `ba220677b`
scaled the Operator from one to two Ready zero-restart contenders, proved one
stable Lease holder, and deleted that active leader at observed ordinal-3
`Decommissioning`. A newly created replacement acquired the Lease after
expiry, advancing transitions once from 80 to 81. It resumed the exact
operation and completed `3 -> 2 -> 1 -> 0` with one stable active leader, two
healthy controller Pods, maximum indexer unavailability one, zero restarts,
ten final stable samples, and an unchanged single ordinal-3 decommission Event.
Lifecycle and leader-record SHA-256 values are
`9b7193931ac6c72f02edc45265a303d4f88a8e59da6967d6e03e368f837ae6f3`
and `c6d662265eec4e5c5683f344c3f7e39a6532f23264253320d9db113ada66a409`.
The 1,800-event workload had zero request failures, exact final results on all
three Search Heads, 13 count regressions, and maximum pending 329; its SHA-256
is `e34ef36dd49a7f835028d13ebd3336fdd1090f7b7210bbc50d78f27f3ec1ed05`.
This closes bounded STS-004 for one normal takeover, not split brain,
controller partition, Lease corruption, repeated failover, or other stages.

The cleanup leader start separately exposed that
`checkLicenseRelatedPodFailures` uses a LicenseManager Pod FQDN below a
headless Service which the LicenseManager reconciler does not create. The
regular Service and endpoint were healthy, the cross-Pod headless name did not
resolve, the check logged `no such host`, and the CR remained Ready while the
license query was skipped. This observation registered SHC-88 and made no
implementation claim at that checkpoint. Bounded implementation and
qualification were completed later in the 2026-08-01 record below.

2026-07-29 UTC: Selected SHC-80 on
`codex/shc-80-authorized-revision-recovery` from integrated feature baseline
`9eecde5d68e9dc889bb2b2f1913420396e00cb21`. The bounded scope is safe
withdrawal or supersession of one already-authorized revision that cannot
start, while every peer remains healthy at the last known-good revision. No
implementation or qualification is claimed by this branch-registration
record.

2026-07-29 UTC: Recorded SHC-80 source and EKS qualification. Added durable
single-target authorized-revision withdrawal, partition-barrier recovery,
Operator-restart continuity, completed-recovery release of a queued revision,
dynamic captain transfer, persistent GUID proof, 187 uninterrupted searches,
a 369-second stability gate, and complete CR-first storage cleanup.

2026-07-30 UTC: Recorded SHC-83 source and EKS qualification on
`codex/shc-83-startup-readiness-qualification`. The accepted current-v4
contract kept all client endpoints closed through image-owned initialization,
the required first-formation restart, telemetry, App Framework work, and the
final stability gate. It then preserved healthy endpoints during established
non-captain recovery, active-captain recovery, and Operator replacement.
Internal management target eligibility is intentionally independent of client
Service readiness only during the bounded first-formation stages needed to
complete that work. The separate SHC-84 startup-budget and TERM-exit contract
was still open at this point and was subsequently closed by the bounded
2026-07-31 qualification recorded below.

2026-07-30 UTC: Registered OPS-012/SHC-86 after SHC-83 qualification teardown
removed the Search Head Pods and storage but a LicenseManager finalizer
retained the terminating namespace while reconciliation attempted to recreate
its Secret. The finalizer was cleared only after the remaining resources were
verified absent. This record preserves a separate LicenseManager
namespace-finalization requirement and does not claim implementation.

2026-07-30 UTC: Selected SHC-84 on isolated branch
`codex/shc-84-startup-term-qualification` from qualified SHC-83 source
`163d5d646`. Docker-Splunk work is based on the integrated runtime source
`f063cfd3936c42428c0775783b8415c2fcfbb3ef`, which already contains the
exact-once shutdown helper and PID-1 TERM correction. The campaign first
measures current-v4 startup, liveness, and Pod termination defaults on the
official fixed Splunk runtime. Selection does not claim a new policy,
implementation, or qualification.

2026-07-31 UTC: Closed the bounded SHC-84 matrix. Exact Operator source
`67c0d3bd2` and digest
`sha256:d83ae44c825f13cb12117e72d2ca5415b4ffd9b7af36bcab7e81226e11e6cafe`
passed existing-v4 reconciliation, fresh formation, forced liveness, planned
deletion, and the supported Splunk
`10.4.2604.0/60dd7967c086 -> 10.5.2605.0/844c593e9c1d` upgrade. The source
formation retained zero restarts despite 29 startup failures on one member.
The dependency-first LicenseManager upgrade changed no Search Head identity or
restart count and retained three endpoints. The Search Heads then rolled
`2 -> 1 -> 0`, retained at least two endpoints, dynamically moved captaincy,
recorded zero container restarts and 200/200 successful sampled searches, and
finished with three registered `Up` target members. This record does not
generalize to every version pair, v3-to-v4 conversion, SAML, or every workload.

2026-08-01 UTC: Closed bounded OPS-013/SHC-88 on isolated branch
`codex/shc-88-license-health`. Exact source `241ea3d91` and Operator digest
`sha256:545910a6b769ad399fea42fdb31ddb79af11d38b5e5691ed3a59786a7606180e`
passed the Linux source gate and EKS endpoint campaign. The Operator created
the headless Service already named by the LicenseManager StatefulSet, retained
the existing Splunk Pod during that creation, resolved the per-Pod FQDN, and
received HTTP 200 license responses. One DNS-publication race and a later
test-induced LicenseManager replacement accumulated as five occurrences on
one `LicenseHealthCheckFailed` Event object; PodReady gating suppressed checks
through the unready interval. After the replacement returned Ready, a clean
Operator restart kept the Service and LicenseManager Pod UIDs, generated three
HTTP 200 checks, added no Event occurrence, and left all tiers Ready. The
replacement Ansible recap was `failed=0`, all non-LicenseManager workload UIDs
remained unchanged, and no container restart was recorded. Expired-license
Event behavior is source-qualified rather than EKS-qualified.

2026-08-01 UTC: Closed bounded OPS-012/SHC-86 on isolated branch
`codex/shc-86-license-finalization`. Exact source `61b35aabf` passed 41 Linux
suites and 157 specs with zero failures and 78.6 percent composite coverage.
Operator digest
`sha256:635d60fecdd203e7d158fb1f95c57d46c7062ed98b156caf8dc68da7515812ec`
passed a 14-second adversarial namespace deletion and a real
referenced-LicenseManager campaign. The latter began with a Ready zero-restart
Pod, StatefulSet, Secrets, Services, two bound PVCs, and two delete-reclaim
PVs. Both custom resources were absent by six seconds, the Pod exited at about
50 seconds, the PVCs and PVs disappeared, and the namespace completed without
a patch at 337 seconds. The Operator log windows contained no forbidden
create, post-finalization status error, or LicenseManager reconcile error.
The campaign also registered SHC-89 for schema-valid initialization of
LicenseManager and SearchHeadCluster objects created already paused. SHC-86
did not correct it; the later SHC-89 record below qualifies the separate fix.

2026-08-01 UTC: Closed bounded SHC-87 on isolated branch
`codex/shc-87-dependency-status`. Exact source `20d926658` passed 41 Linux
suites and 157 specs with zero failures. Operator OCI index digest
`sha256:fbb1a53c45da509fee47edc618eefd93923fc3864df9533dc85dbcbc8914c2a3`
then qualified a SearchHeadCluster submitted before its LicenseManager. The
absent and Pending intervals produced Pending/Progressing
`DependencyNotReady` conditions, two aggregating Normal Event series with
counts 10 and 15, a retained dependency message, and no terminal mismatch or
reconcile error. The LicenseManager became Ready at `06:09:46Z`; the SHC left
dependency wait by `06:10:05Z`, cleared the message, and completed at
`06:20:39Z` with Ready Deployer, 3/3 Search Heads, three endpoints, all members
Up, zero container restarts, direct search success on every member, and 8/8
service-routed searches. Terminal desired-image contradiction,
cross-namespace references, and reverse MonitoringConsole dependencies remain
source-qualified boundaries rather than live EKS claims.

The same cleanup registered SHC-90. Namespace deletion began at `06:24:37Z`.
Before the LicenseManager and SearchHeadCluster deletion timestamps became
visible, their normal Apply paths attempted ConfigMap creation in the already
terminating namespace. Kubernetes rejected the creates, producing six
LicenseManager and nine SearchHeadCluster Reconciler errors. Existing
finalization then removed all custom resources and workloads; ten PVCs were
gone by `06:25:55Z`, ten PVs by `06:26:16Z`, and the namespace by `06:31:01Z`
without a manual patch. SHC-90 owns the namespace-transition guard; no fix is
claimed by SHC-87.

2026-08-01 UTC: Closed bounded OBS-008/SHC-89 on isolated branch
`codex/shc-89-paused-status`. Exact source `3e1716737` passed 41 Linux suites
and 157 specs with zero failures and 78.5 percent composite coverage.
Operator digest
`sha256:b83bbb97f89dca45e183e895e4be7e1d7bd11007f08babb41c4c94c97d18f145`
initialized all seven active v4 Splunk resource kinds to current-generation
`Pending/Paused` status. SearchHeadCluster also reported
`deployerPhase=Pending`. No managed workload was created, every
resourceVersion remained unchanged for 45 seconds, and the scoped Operator
log contained no paused-status or Reconciler error. Removing pause took a
LicenseManager and three-member SearchHeadCluster to Ready; the SHC completed
with three endpoints, all members Up, zero restarts, and direct search success
on every member. Cleanup removed all ten PVCs, the namespace, and every PV
claim reference to the namespace. Detailed evidence is in
`SHC89PausedStatusQualification.md`.

The source audit separately registered SHC-91. It was subsequently completed
on `codex/shc-91-deletion-before-pause` at exact source `a76c30e0c`. The final
scope covers controller ordering plus the real Apply boundary and is recorded
in `SHC91DeletionBeforePauseQualification.md`.

2026-08-02 UTC: Closed bounded K8S-011/OBS-001/OBS-004/OBS-005 SHC-93 on
`codex/shc-93-operator-readiness`. Exact source `90103bef5` passed final macOS
and Linux build/test gates, 43 suites, all 185 enterprise specs, focused race,
three Kustomize renders, and 145 Helm tests. Operator OCI index
`sha256:b5a022a788c7cacf8b7ee33e7132eae56d82b14eb631809ddd116c8b816e9d63`
and chart SHA-256
`008abda67d13775ce6cd7e0f8e77365edce01af82f6ad9c12ecf34911a2f6925`
proved that informer or Lease denial makes the manager NotReady without using
liveness to restart it, restored access recovers the same Pod, a healthy
standby remains Ready, and takeover completes without a restart loop. Secure
metrics retained the NotReady manager endpoint for diagnosis. Cleanup removed
all disposable resources and retained the existing SHC at 3/3 Ready with zero
restarts. Exact evidence and bounded limitations are in
`SHC93OperatorReadinessQualification.md`.

2026-08-03 UTC: Completed the bounded SHC-97 source, packaging, and EKS
qualification after a same-PVC Cluster Manager campaign exposed a
Docker-Splunk/Ansible failure distinct from the fixed Splunk KV Store
version-marker precheck. Splunk Ansible source
`ae8ecf4af1eb4c143a441e440626a17a5dfeaf6a` and Docker-Splunk source
`118cae68a8fdecbac1286582d32eecd996510564` passed their focused Make gates.
Linux-built runtime OCI index
`sha256:49b12103f8444319dcf823eb829d2dfc020410e44d46273461c1b15e52c724fd`
completed image convergence and a controlled same-image Cluster Manager
deletion on unchanged PVCs, then converged the four-indexer and three-member
Search Head topology in dependency order. Every replacement issued one start,
reported no port 8191 conflict, became Ready, and retained zero container
restarts. Workload windows completed exactly at 120/120 and 240/240 with zero
HEC and search-request failures. Four transient successful-search count
regressions during indexer replacement, with maximum pending 13 in the tier
window, remain the open SHC-85/OPS-011 boundary. Because each live start
returned zero, the nonzero status-poll path remains established by executable
source tests rather than by an induced live failure. Exact evidence and
limitations are in `SHC97DockerSplunkStartupQualification.md`.

2026-08-03 UTC: Recorded the bounded SHC-100 safety and fresh-cluster
qualification on `codex/shc-100-safe-stable-indexer-address-opt-in`. Exact
production source `2e4bfa333` passed 43 native-Linux Make suites, all 192
enterprise/controller specs, `make build`, and immutable EKS deployment.
Source `10730a6a2` then corrected the evidence-only Linux `jq` argument. A
retained-cluster restoration reached the accepted runtime without PVC loss or
container restart, but Search Heads needed about 26 minutes after lifecycle
Ready to remove stale peers; its exact 3,600-event workload still exposed 215
materially incomplete successful samples. A separate empty-PVC cluster formed
with four explicit per-ordinal FQDN peers and later completed indexer
replacement `3 -> 2 -> 1 -> 0`. Every UID and IP changed while all PVCs and
FQDNs remained stable, at least three endpoints remained Ready, and 157
samples never contained a fifth address or duplicate GUID. The corrected
guard plus 60 final exact samples passed. The fresh 3,600-event workload had
zero HEC/search request failures and exact final completeness; all 21 count
regressions and maximum pending 282 occurred during the planned roll, with no
count regression afterward. This qualifies a bounded new-cluster opt-in,
rejects automatic retained-cluster address migration, and leaves explicit
partial-result semantics open.

2026-08-03 UTC: Completed SHC-99 and registered/completed SHC-101 on final
integration source `0b56ec79b`. The first start-only exact process matcher was
rejected after live EKS inspection found healthy daemon commands using both
exact `start` and exact `restart`; final source `05b7b3ea7` accepts both while
rejecting substring false positives. Qualification also reproduced concurrent
probe ConfigMap resource-version conflicts that generated false StatefulSet
Warnings; `0b56ec79b` retries from current state. All 43 native-Linux suites,
192/192 specs, 78.3 percent coverage, and `make build` passed. Immutable index
`sha256:0f2480b1e8e39d6e5a00e014df280c5aa3167abe5e498dd1deaac7399254f0f6`
passed the bounded live probe matrix and the two-namespace concurrent script
transition with zero candidate Warning Events, zero controller errors, and
zero workload restarts. Candidate and accepted-restoration snapshots retained
four searchable indexers and four Up peers on every Search Head. The accepted
Operator index was restored cleanly.

2026-08-03 UTC: Completed SHC-102 and SHC-103 on exact cumulative source
`070ca5f59`. The Operator now distinguishes unchanged generated probe defaults
from unmarked or edited customer data using a complete-data integrity marker,
revalidates ownership on every conflict retry, treats successful creation as
authoritative across informer lag, and bounds the AlreadyExists-winner read.
A deterministic post-create NotFound regression, 43 exact native-Linux
suites, 192/192 specs, 78.3 percent coverage, `make build`, and immutable
Operator index
`sha256:2ae4db4155427ade5361f8a4d71f71d7ea0b4bdbf447a40e2dc1434815074308`
passed. A real EKS namespace received exactly three probe scripts with equal
full Data hash and marker, candidate logs contained zero ERROR/FATAL or probe-
NotFound failures, and creation produced zero Warning Events. Cleanup removed
the disposable namespace and both PVs. Accepted restoration preserved all 20
retained Pod UID/restart/readiness rows, both unmarked ConfigMaps, and healthy
retained and fresh cluster snapshots exactly.

2026-08-03 UTC: Registered SHC-104 after canonical Docker-Splunk branch
`feature/shc-kubernetes-reliability` was fast-forwarded to `6ee266c1`. Its 15
shutdown, four exact-Ansible-ref, and one base-image-security tests passed on
native Linux. The aggregate `make test_setup` then failed during the broader
legacy dependency bootstrap, before broader test execution, when PyYAML 5.4.1
could not build under the current isolated setuptools/Cython environment.
The repository remained clean. This is tracked as test-infrastructure debt and
does not change the qualified runtime result.

2026-08-03 22:27Z: Implemented the isolated SHC-104 candidate at Docker-Splunk
commit `0604eeb`. Clean and idempotent Python 3.10 bootstrap runs passed
locally, including the new five-test bootstrap regression, all 91 broader
image-test collections, Docker Compose 1.29.2 configuration validation, and
the unchanged 15/4/1 bounded SHC contracts. Native-Linux acceptance remains
open because the vWorkstation Coder API is returning an EOF before SSH setup.

2026-08-03 22:31Z: Completed SHC-104 at Docker-Splunk commit `0604eeb` and
fast-forwarded canonical branch `feature/shc-kubernetes-reliability`. A clean
Linux/AMD64 Python 3.10.18 gate pinned by digest
`sha256:ee0c7d26e2dba416773cb51c042496e9cffacc872459f57726935dd6833f2d96`
passed two aggregate runs, the unchanged 15/4/1 bounded contracts, five
bootstrap regressions, all 91 broader image-test collections, Compose
configuration validation, and exact virtual-environment cleanup.
