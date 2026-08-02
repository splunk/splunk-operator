# Search Head Cluster Reliability Test Scenario Matrix

## How to use this matrix

Every scenario has a stable identifier. A test result records the identifier,
source and image versions, environment, start and end times, outcome, failing
lifecycle stage, reason code, and evidence location. “Pass” means all listed
invariants were asserted; a Pod returning to `Running` is not sufficient.

Priority P0 blocks the integration spike. P1 blocks production opt-in. P2
blocks default enablement for the affected environment or feature.

## Common invariants

Unless a scenario explicitly tests loss of majority, assert:

- no more than one planned Search Head member is unavailable;
- healthy non-target members remain locally traffic-ready;
- exactly one authoritative service-ready captain is eventually observed;
- ordinary replacement preserves the target member's persistent identity and
  does not remove/re-add consensus membership;
- the replacement runs the desired StatefulSet revision;
- the rollout does not advance before target recovery;
- operation stage, reason, elapsed time, and timeout are observable;
- the Operator can reconcile again without duplicating a destructive action;
  and
- credentials and customer search text do not appear in evidence.

## Contract and API scenarios

| ID | Priority | Scenario | Required proof |
|---|---|---|---|
| API-001 | P0 | Grace omitted | Documented compatibility default appears in rendered Pod |
| API-002 | P0 | Grace explicitly set | Customer value is preserved |
| API-003 | P0 | Invalid grace and timeouts | Admission rejects invalid values with stable messages |
| API-004 | P0 | All timeout fields differ | Grace, drain, transfer, and rejoin remain independent |
| API-005 | P1 | Existing v4 CR after Operator upgrade | No uncontrolled replacement; rollout state is visible |
| API-007 | P1 | Feature gate disabled | Current `OnDelete` behavior remains selected |
| API-008 | P1 | Unknown/unsupported runtime capability | Operator blocks or safely falls back |

## Probe and traffic scenarios

| ID | Priority | Scenario | Required proof |
|---|---|---|---|
| HLT-001 | P0 | Healthy non-captain | Readiness succeeds |
| HLT-002 | P0 | Healthy captain | Same readiness contract succeeds |
| HLT-003 | P0 | Manual detention | Readiness fails and normal Service endpoints stop selecting target |
| HLT-004 | P0 | Captain election in progress | Healthy members remain ready; captain condition changes separately |
| HLT-005 | P0 | splunkd process unavailable | Liveness eventually fails according to local policy |
| HLT-006 | P0 | Captain unreachable from one member | Liveness does not restart a locally live process |
| HLT-007 | P1 | Planned shutdown begins | Readiness becomes false before local process exit |
| HLT-008 | P1 | Management port temporarily saturated | Threshold avoids a restart cascade and records probe failures |
| HLT-009 | P1 | Startup with slow persistent recovery | Startup budget permits local start; rejoin remains a controller gate |
| HLT-010 | P2 | Ingress/service-mesh/LB propagation | New traffic ceases within measured qualified tolerance |
| HLT-011 | P2 | No service mesh installed | Local member readiness succeeds without a sidecar, mesh control plane, ingress, or external network dependency |
| HLT-012 | P2 | TLS terminates at ingress | External TLS mode does not select the local management scheme; readiness follows the actual Splunkd `enableSplunkdSSL` configuration |
| HLT-013 | P2 | Mesh mTLS, passthrough, or re-encryption | Local readiness bypasses proxy routing while supported inter-Pod management traffic retains its qualified TLS policy |

## Drain and captain scenarios

| ID | Priority | Scenario | Required proof |
|---|---|---|---|
| LFC-001 | P0 | Replace non-captain with no searches | Detain, authorize, replace, rejoin, release |
| LFC-002 | P0 | Replace captain with no searches | Transfer to a different confirmed captain before authorization |
| LFC-003 | P0 | Historical search active | Target stays detained until completion or approved timeout policy |
| LFC-004 | P0 | Real-time search active | Real-time policy is applied and independently observable |
| LFC-005 | P0 | Drain timeout, default policy | Operation blocks without deleting target |
| LFC-006 | P1 | Audited continuation after timeout | Pre-timeout, wrong-token, and stale-operation approvals cannot advance; one exact post-timeout approval is durably recorded before later safety revalidation and advances only its named operation |
| LFC-007 | P0 | Captain transfer fails | Operation fails closed with no replacement authorization; captain Pod UID/revision and partition remain unchanged, healthy peers remain serving, one deduplicated warning identifies the timeout, and revision withdrawal restores the retained target before any later rollback target begins |
| LFC-008 | P0 | Captain observation is stale/conflicting | Destructive progression blocks |
| LFC-009 | P1 | Captain changes independently during drain | Controller re-observes and follows the current authoritative state |
| LFC-010 | P1 | Operator restarts during transfer | No duplicate transfer; state resumes |
| LFC-011 | P1 | Upgrade init/finalize retries | Each logical workflow has one observable intent and idempotent retry |
| LFC-012 | P0 | Continuous ad-hoc searches through rollout | Healthy members continue accepting and completing new searches |
| LFC-013 | P1 | Scheduled searches through captain transition | Expected scheduled work is neither silently lost nor duplicated outside the documented product tolerance |
| LFC-014 | P0 | Search already running on target | Completion, timeout, or interruption matches the selected drain policy and is recorded |

### Qualified LFC-006 spike evidence

On 2026-07-28, a fresh three-member EKS SHC passed LFC-004, LFC-005, and
LFC-006 together. A real-time search on ordinal two remained running through
the 30-second drain timeout. The operation reached
`Blocked/SearchDrainTimedOut`, the original Pod UID and revision remained
unchanged, StatefulSet partition remained three, and the Pod plus
EndpointSlice were non-serving. A matching operation with the wrong token and
the issued token with a stale operation ID both left the same operation
blocked, produced no approval Event, did not change the approval counter, and
did not change the StatefulSet update revision.

The exact operation ID and issued token produced one durable approval with a
snapshot of one active real-time search. Status recorded the approval at
17:04:17Z and replacement authorization at 17:04:27Z, demonstrating that the
approval was persisted before the later safety decision. The campaign emitted
one `SHCSearchDrainContinuationApproved` Event and incremented the bounded
approval counter once. The native rollout then replaced ordinals
`2 -> 1 -> 0`, never observed more than one unavailable member, transferred
captaincy from ordinal zero to ordinal one before replacing ordinal zero, and
finished with three ready, serving, registered `Up` members and zero container
restarts. A 312-second post-action stability gate remained continuously
healthy. The namespace, PVCs, and all eight associated PVs were removed after
evidence collection.

### Qualified LFC-007 spike evidence

On 2026-07-28, a three-member SHC on EKS cluster `vivek-spl-301372` passed
LFC-007 with the final `3e9e735a7` Operator image. A forward
partition-gated rollout replaced ordinals `2 -> 1`, with each replacement
Ready, serving, registered, and `Up` before the next target began. Ordinal zero
was the active captain. With a one-second test policy, it passed through
detention and captain-transfer stages and then reached
`Blocked/CaptainTransferTimedOut`.

The timeout failed closed. StatefulSet partition remained one,
`replacementAuthorizedAt` remained unset, the original captain Pod UID and
baseline revision remained present without a deletion timestamp, and ordinals
one and two remained Ready and serving. The harness held this state for 30
seconds and observed exactly one additional `SHCRolloutBlocked` warning.

Withdrawing the requested revision emitted exactly one additional
`SHCPodUpdateCancelled` Event. The Operator released detention, recovered the
same captain Pod in place, and waited for its Kubernetes Ready and serving
conditions before beginning rollback. Kubernetes reused the baseline
ControllerRevision; rollback still proceeded deterministically through
ordinals `2 -> 1` by inspecting each Pod revision. Maximum unavailability was
one in both directions and all container restart counts remained zero.

The bounded run-window audit found no `OutOfOrderRevision`,
`ExistingUnavailablePod`, or `TooManyUnavailable` Event or Operator log.
After restoring the 300-second policy without a Pod or revision change, a
321-second continuous gate observed three Ready/serving Pods, three registered
`Up` members, a ready ordinal-zero captain, matching StatefulSet revisions and
partition three, reachable local management endpoints, KV Store `ready`, no KV
Store upgrade or backup, and zero container restarts.

## Runtime shutdown and restart scenarios

| ID | Priority | Scenario | Required proof |
|---|---|---|---|
| RUN-001 | P0 | `preStop` invokes shutdown | Explicit stopping state, one stop owner, bounded exit |
| RUN-002 | P0 | TERM follows or overlaps `preStop` | No concurrent second `splunk stop`; TERM waits and returns the owner's exact result |
| RUN-003 | P0 | TERM without `preStop` | Same idempotent shutdown path runs |
| RUN-004 | P0 | Grace expires | Forced termination is detected and reported |
| RUN-005 | P1 | Stop command fails | Error is preserved; retry/exit behavior follows contract |
| RUN-006 | P0 | Persistent member restart | Rejoin path selected; cluster-forming commands are not repeated |
| RUN-007 | P1 | New empty member | Bootstrap/join intent is selected explicitly |
| RUN-008 | P1 | Process crash/OOM | Recovery does not assume `preStop` ran |
| RUN-009 | P1 | Concurrent lifecycle triggers | Lock/ownership produces exactly one local stop and preserves success or failure for every caller |
| RUN-010 | P2 | Single- and multi-container images | Shutdown contract holds for each supported layout |
| RUN-011 | P1 | Shutdown owner disappears before recording a result | Follower remains bounded, returns timeout, and does not issue a second stop |

## StatefulSet and controller scenarios

| ID | Priority | Scenario | Required proof |
|---|---|---|---|
| STS-001 | P0 | `OnDelete` lifecycle baseline | New orchestrator safely drives one replacement |
| STS-002 | P0 | Operator restart at every lifecycle stage | Operation resumes without a second target |
| STS-003 | P0 | New spec change during active operation | Deterministic queue, coalesce, or block policy |
| STS-004 | P1 | Controller leader failover | New leader resumes from durable state |
| STS-005 | P0 | Migrate existing StatefulSet to opt-in `RollingUpdate` | Initial partition prevents immediate uncontrolled rollout |
| STS-006 | P0 | Complete three-member rollout | Reverse ordinal, one target, recovery gate before advancement |
| STS-007 | P0 | Partition write conflict | Retry does not skip an ordinal |
| STS-008 | P0 | Replacement never becomes ready | Partition remains blocked with classified reason |
| STS-009 | P1 | Manual Pod deletion during rollout | Observed as unplanned disruption; no second planned deletion |
| STS-010 | P1 | Rollback to `OnDelete` | Advancement stops and current target reaches known state first |
| STS-011 | P1 | Ordinal zero replaced while not captain | No static-captain assumption |
| STS-012 | P0 | `Parallel` Pod management bootstrap and persistent cold restart (SHC-R24) | Every scheduling order selects exactly one stable bootstrap seed and join intent for all other members; a parallel persistent restart selects rejoin or await-rejoin only, runs no formation commands, leaves splunkd alive, and records each member's startup classification and selected action |
| STS-013 | P1 | Search Head preferred-captain configuration | Kubernetes default does not prefer ordinal zero; an explicit customer override is preserved |
| STS-014 | P0 | Desired revision is withdrawn after replacement authorization | The already-authorized target completes to a known recovered or classified terminal state under the original durable operation; the controller does not claim in-place cancellation for a replaced Pod, does not authorize a second disruption, and then deterministically rolls back or queues the new desired revision |

### STS-014 qualification evidence

On 2026-07-28, a three-member EKS SHC passed the supported CR-driven
post-authorization handoff. Revision A received durable authorization for
ordinal two and lowered the StatefulSet partition from three to two. Revision B
was submitted while the replacement was starting. During that interval:

- the revision-A lifecycle operation retained ownership;
- the StatefulSet template and `status.updateRevision` remained revision A;
- partition remained two;
- the first replacement Pod kept one stable new UID; and
- the other two members remained Ready and serving.

The initial EKS attempt found that Splunk-side lifecycle `Completed` could be
observed before the Pod's Kubernetes Ready and `shc-serving` conditions became
true. Releasing the queued Pod template at that point was unsafe because
Kubernetes could begin another replacement before traffic eligibility was
restored. The handoff was corrected so Splunk completion remains a cluster-side
fact while release of the queued Kubernetes revision additionally requires:

- the authorized target Pod exists and is not deleting;
- its UID differs from the pre-replacement UID;
- its `controller-revision-hash` matches the original authorization;
- Kubernetes Ready is true; and
- the SHC serving readiness gate is true.

The accepted run used a separate revision-B lifecycle operation and
authorization after that boundary. The complete revision-B rollout then
advanced in reverse ordinal order `2 -> 1 -> 0`; ordinal zero was the active
captain at its turn and captaincy moved to ordinal one before replacement.
Across 127 service-search probes, there were zero failures, at least two Ready
endpoints, no more than one unavailable Pod, no container restarts, and no
run-window `ConflictingLifecycleOperation`, `OutOfOrderRevision`,
`TooManyUnavailable`, or `ExistingUnavailablePod` warning. Final Splunk status
reported all members `Up`, dynamic captaincy, and service readiness. KV Store
reported `ready`, three members, no version upgrade, and no backup. A subsequent
300-second gate passed 37 searches with three Ready endpoints throughout.

This evidence covers revisions submitted through the SearchHeadCluster
contract. Direct external mutation of the generated StatefulSet is not a
supported revision-submission path; revision or identity disagreement there
remains fail-closed rather than being treated as a successful handoff.

## Rejoin, membership, and storage scenarios

| ID | Priority | Scenario | Required proof |
|---|---|---|---|
| REJ-001 | P0 | Normal retained-PVC rejoin | Same member identity registers and reaches recovery contract |
| REJ-002 | P0 | Scheduling delay | Stage identifies scheduler wait, not Splunk failure |
| REJ-003 | P0 | Volume attachment delay | Stage identifies storage wait |
| REJ-004 | P0 | Image pull failure | Terminal/transient classification and no next target |
| REJ-005 | P0 | splunkd startup failure | Local startup stage and evidence identify failure |
| REJ-006 | P0 | Member cannot reach captain | Rejoin blocks without liveness cascade |
| REJ-007 | P0 | Member present but not `Up` | Recovery gate remains closed |
| REJ-008 | P1 | Missing or changed persistent identity | Operation blocks; no automatic remove/re-add |
| REJ-009 | P1 | Rejoin timeout | Cause category and bounded snapshot recorded |
| REJ-010 | P1 | Suspected Raft catch-up limitation | Blocks and preserves evidence; no destructive automatic recovery |
| REJ-011 | P1 | Configuration or KV synchronization delayed | Pod-local readiness and full recovery gate remain distinguishable |

### REJ-004 qualification evidence

On 2026-07-28/29 UTC, a three-member EKS SHC exercised both sides of the
image-pull classification while the lifecycle controller owned ordinal two.
For the retryable path, the desired image tag was removed before the authorized
replacement's first container attempt. Kubelet reported `ErrImagePull` and
`ImagePullBackOff`; the lifecycle remained
`WaitingForContainer/ImagePullFailed` for 60 seconds, partition remained two,
and no later ordinal became eligible. Restoring the same tag to the same digest
recovered the operation and the rollout completed `2 -> 1 -> 0`.

For the terminal path, invalid image syntax on a newly authorized ordinal-two
replacement produced kubelet `InvalidImageName` and immediate
`Blocked/ImagePullFailed`. Partition remained two and no later ordinal was
authorized. Across the accepted campaign, all 131 Service searches succeeded,
at least two Ready endpoints remained available, no more than one Search Head
was unavailable, and the Deployer remained unchanged and Ready.

This evidence is specific to real first-pull behavior. The test did not bypass
the authoritative image-upgrade compatibility boundary, mutate a container
that had already started, or leave a Pod image that disagreed with the
StatefulSet template.

## Scale, delete, application, and upgrade scenarios

| ID | Priority | Scenario | Required proof |
|---|---|---|---|
| OPS-001 | P0 | Scale up | New-member join intent, stable existing members |
| OPS-002 | P0 | Permanent scale down of non-captain | Drain, membership removal, replica/storage policy |
| OPS-003 | P0 | Permanent scale down of captain | Transfer precedes membership removal |
| OPS-004 | P1 | Complete CR deletion | Explicit deletion policy; no confusion with recycle |
| OPS-005 | P1 | App/bundle operation while ordinal zero unavailable | Dynamic reachable target succeeds |
| OPS-006 | P1 | App/bundle operation during rollout | Coordination prevents conflicting planned disruption |
| OPS-007 | P1 | Supported image upgrade | Init/finalize and per-member lifecycle complete |
| OPS-008 | P1 | Unsupported simultaneous-restart configuration | Admission or controller blocks rolling treatment |
| OPS-009 | P2 | TLS, ingress termination, and optional service mesh | No mesh is required; local readiness follows Splunkd TLS rather than ingress TLS, bypasses configured HTTP proxies, and management traffic remains valid in each qualified mesh mode |
| OPS-010 | P2 | Private registry/air gap | Registry-qualified and digest-pinned image references plus all pull secrets survive rendering and rollout tracking unchanged; lifecycle and diagnostics add no helper image or undeclared external service |
| OPS-011 | P1 | App Framework deploys an app whose bundle requires Search Head or indexer restart | The effective Splunk restart policy is observed and recorded; SHC and indexer restart work is serialized with every other planned disruption; insufficient redundancy fails closed; serving withdrawal is role-, protocol-, and configuration-aware; previous-peer Splunk and network-path recovery precedes the next target; continuous acknowledged ingest and representative real-time, historical, and scheduled searches prove that supported app deployment does not create a customer-visible search outage or silently incomplete result |
| OPS-012 | P1 | Namespace-first deletion with a referenced LicenseManager | The LicenseManager performs no create after namespace termination, removes its finalizer without manual intervention, and leaves no owned Secret, workload, PVC, or PV |
| OPS-013 | P1 | LicenseManager health and expiration observation | The StatefulSet's named headless Service exists before a per-Pod management request; the Operator waits for Pod readiness, resolves the exact Pod identity, receives and parses the license response, emits `LicenseExpired` only from a successful response proving expiration, and represents a retryable transport failure through an aggregating Warning Event without a terminal condition or false expiration result |

### OPS-012 LicenseManager qualification evidence

The accepted 2026-08-01 source and EKS evidence is recorded in
[SHC86LicenseManagerNamespaceFinalizationQualification.md](SHC86LicenseManagerNamespaceFinalizationQualification.md).
Exact source `61b35aabf` routes LicenseManager deletion ahead of pause and
normal validation, performs no client Create, tolerates namespace-controller
cleanup races, invokes declared PVC finalization, and suppresses successful-
finalization status writes.

An adversarial paused and invalid fixture disappeared with its namespace in 14
seconds. A second fixture formed a Ready LicenseManager with a real StatefulSet,
Ready zero-restart Pod, three Secrets, two Services, two bound gp3 PVCs, and
two delete-reclaim PVs, then added a paused SearchHeadCluster reference. Both
custom resources were gone by the six-second sample, the Pod exited at about
50 seconds, the PVCs and PVs disappeared, and the namespace controller
completed naturally at 337 seconds. Neither run used a manual finalizer patch;
the bounded log audit found no forbidden create, post-finalization status
error, or LicenseManager reconcile error.

Later SHC-87 cleanup exposed the earlier namespace-transition interval: a
Namespace can become terminating before deletion timestamps appear on its
custom resources. LicenseManager and SearchHeadCluster briefly attempted
normal ConfigMap Apply work in that interval; Kubernetes rejected it, then
existing finalization completed.

SHC-90 closes that bounded gap. Exact source `0c291c8c8` stops normal Apply
from an authoritative Namespace read across all seven active v4 Splunk tier
controllers and preserves finalization after the CR deletion timestamp is
visible. Immutable EKS Operator digest
`sha256:c2438c14e238e101cba52d758968a2cd7c64fc2798ed5a0a4781acb3e836e764`
observed the real Namespace-deleting/CR-not-deleting window for a Ready
LicenseManager and three-member SHC. It produced five guard records per
controller, zero fixture-level error, natural deletion finalization, zero
remaining PVC/PV claim reference, and natural Namespace completion without a
manual finalizer patch.

### OPS-004 and OPS-012 SHC-91 deletion-order evidence

The accepted 2026-08-01 source and EKS evidence is recorded in
[SHC91DeletionBeforePauseQualification.md](SHC91DeletionBeforePauseQualification.md).
Exact source `a76c30e0c` makes CR deletion authoritative over pause in all seven
active v4 tier controllers and over normal validation, dependency,
configuration, and workload processing in the six affected real Apply entry
points. Successful finalization returns without a post-delete status write;
failed finalization retains the existing observable error path.

Immutable Operator digest
`sha256:4903f70a95b150c0a29bcd3ac70e063b5c55b6a030399a4636297586dea85cea`
deleted all seven paused tier resources directly in 5 seconds and completed an
all-tier namespace-first deletion by the 13-second sample. Structured logs
contained the expected finalizer, PVC deletion, and completion records with
zero normal creates, status errors, or Reconciler errors. A real Ready
Standalone then removed its StatefulSet, zero-restart Pod, two bound PVCs, and
both delete-reclaim PVs naturally; the final PV was absent by the 73-second
sample. No fixture used a manual pause or finalizer patch. SHC-92 subsequently
closed the bounded namespace-scoped Helm contract. Provider/live-version,
legacy-v3, and every-tier real runtime breadth remain open.

### OPS-013 LicenseManager qualification evidence

The accepted 2026-08-01 source and EKS evidence is recorded in
[SHC88LicenseManagerHealthQualification.md](SHC88LicenseManagerHealthQualification.md).
Exact source `241ea3d91` reconciled the headless Service already named by the
LicenseManager StatefulSet and queried the per-Pod management endpoint only
after Kubernetes Pod readiness. On EKS, the Service was created while the
existing LicenseManager Pod retained its UID and zero restarts, controller DNS
resolved the Pod FQDN, and Splunk recorded HTTP 200 responses for
`/services/licenser/licenses?output_mode=json`.

The first lookup raced initial DNS publication and produced the expected
non-terminal `LicenseHealthCheckFailed` Event. A deliberately test-induced
LicenseManager replacement later kept REST calls suppressed while PodReady was
false. After recovery, a clean Operator restart retained both the Service and
LicenseManager Pod UIDs, generated three successful HTTP 200 checks, and did
not increase the failure Event count. Unit coverage separately proves the
expired-license Event and invalid-secret error paths. No EKS claim is made for
an intentionally expired production license.

### OPS-011 indexer qualification evidence

The accepted 2026-07-30 EKS evidence and remaining gates are recorded in
[SHC82AppFrameworkIndexerQualification.md](SHC82AppFrameworkIndexerQualification.md).
On four peers with RF3/SF2, `searchable=1`, `force=0`, and successful RF/SF/all
searchable preflight, existing readiness still allowed 7 of 55 HEC
submissions to fail. An HEC-aware default-timing gate reduced that to 1 of 55.
A faster experimental gate completed 55 of 55 with exact eventual
completeness, while a peer-level monitor still observed asynchronous
non-serving/advertised and next-peer/recovery boundaries. The same experiment
exposed a controller deadlock when intentional serving withdrawal made the
target container unready during an `OnDelete` template update. These are
partial qualification results, not an OPS-011 pass.

## Kubernetes disruption scenarios

| ID | Priority | Scenario | Required proof |
|---|---|---|---|
| K8S-001 | P1 | Node drain through Eviction API | An SHC-owned `maxUnavailable: 1` PDB denies another voluntary eviction while one selected member is unavailable; PDB is not rollout sequencing |
| K8S-002 | P1 | Direct graceful Pod deletion | Classified as unplanned; partition remains fail-closed and no other planned target starts until the replacement is Kubernetes-ready, registered, and `Up` |
| K8S-003 | P1 | Force deletion | No hook assumption; missing/deleting Pod blocks rollout, and Kubernetes readiness alone cannot resume it before member recovery and captain readiness are observed |
| K8S-004 | P1 | Captain node loss | `CaptainUnavailable` is distinct from initial formation; partition and durable target remain unchanged until one authoritative service-ready captain is observed, then unplanned member recovery is evaluated separately |
| K8S-005 | P1 | Network partition member/captain | No restart cascade; conflicting state blocks rollout |
| K8S-006 | P1 | Operator disconnected from API server | Durable state resumes after connectivity returns |
| K8S-007 | P2 | EndpointSlice propagation delay | Traffic observations and tolerance captured |
| K8S-008 | P2 | Cluster autoscaler/zone movement | Scheduling/storage stages remain attributable |
| K8S-009 | P1 | PDB across supported SHC sizes | The PDB selects only this SHC, retains at most one voluntarily unavailable member, is reconciled idempotently, and never takes over a user-owned name collision |
| K8S-010 | P1 | Namespace-scoped Helm install with `namespaceOverride` | The documented watch target, Deployment and service-account placement, Role/RoleBinding namespaces, and cluster-scoped Namespace-reader `resourceNames` agree; two installations do not collide; an upgrade preserves the selected contract |
| K8S-011 | P1 | Operator manager is alive but cannot participate in reconciliation | Readiness distinguishes process health from cache/watch and leader-election participation; a single replica that cannot obtain required access is not falsely Available, a capable non-leading HA replica is not treated as failed, and recovery/takeover do not create a restart loop |

### K8S-010 and bounded CMP-006/CMP-007 SHC-92 evidence

The accepted 2026-08-02 evidence is recorded in
[SHC92NamespaceScopedHelmQualification.md](SHC92NamespaceScopedHelmQualification.md).
Exact source `91f742b52` defines one effective namespace for chart placement,
namespace-scoped watch, service-account authorization, leader election, and
the get-only Namespace reader. The preceding chart reproduced a false-Ready
manager that could not acquire its Lease. Exact packaged chart SHA-256
`23258a699126ae318fee287a5734d939521f3d32ef8741f936ff44b31ef9b5b8`
recovered the same Deployment and service-account identities through a normal
Helm upgrade without a manual patch.

Fresh default and overridden releases acquired leadership in their effective
namespaces. Two releases stored in one Helm release namespace used distinct
reader names and could not access each other's Namespace or Splunk custom
resources. Normal uninstall removed every chart object and the bound
delete-reclaim PV. The exact package rendered for Kubernetes 1.27.0 and
1.31.14; live qualification covers only EKS 1.31.14. This passes K8S-010 and
bounded render/live evidence for CMP-006/CMP-007. It does not pass a live 1.27
cluster, another provider, changing an established override during upgrade,
or overlapping Operator watch scopes.

### K8S-011 SHC-93 evidence

The accepted 2026-08-02 evidence is recorded in
[SHC93OperatorReadinessQualification.md](SHC93OperatorReadinessQualification.md).
Exact source `90103bef5` keeps `/healthz` process-local and makes `/readyz`
require the complete initial enabled-controller informer barrier plus current
authorization for the leader Lease. Current leadership is a separate metric,
so an authorized, synchronized standby remains Ready.

Cold list/watch denial kept health/readiness at HTTP 200/500 and readiness
metrics at `0/0/0`; cold Lease denial reported `1/0/0`. Restoring either
dependency recovered the same Pod without a kubelet restart. The metrics
Service retained the NotReady Pod endpoint for authenticated diagnosis. Two
Ready contenders retained exactly one leader, and deleting that leader moved
the Lease to the existing standby in 35 seconds without replacing or
restarting it. When the active leader lost API access, controller-runtime
exited after its Lease-renewal deadline; its API-isolated restart served health
and remained NotReady behind the informer barrier instead of entering a
CrashLoop. Removing the fault recovered that same Pod in place.

This passes bounded K8S-011 on one EKS 1.31.14 cluster. It does not qualify
other providers or versions, productized manager replica/rollout settings,
ongoing post-start per-informer health, or production alert delivery.

### STS-002 and bounded K8S-006 SHC-85 evidence

The 2026-07-31 SHC-85 campaigns removed the sole Operator controller for a
requested five minutes during all four durable indexer lifecycle stages. The
`TargetSelected` record observed exactly 300 seconds while all four original
Pods remained Ready and serving with no lifecycle marker; the
`ReadyForReplacement` record observed 302 seconds; the
`Decommissioning` record waited for persisted
`observedDecommissioning=true` and observed 306 seconds; and the
`WithdrawingReadiness` record observed 306 seconds after the lifecycle marker
was persisted and kubelet independently removed the still-running target from
readiness and the EndpointSlice. All four retained the same exact target and
operation, kept every non-target peer serving, recorded zero liveness
failures and container restarts, and resumed the same operation through
`3 -> 2 -> 1 -> 0` after controller restoration.

This qualifies the bounded STS-002 controller-absence behavior for all four
stages. By itself it is still only K8S-006-adjacent evidence: scaling the
controller to zero does not exercise a running controller that loses and later
regains API-server connectivity. The `TargetSelected` test-only pause also
does not qualify a concurrent desired-state conflict.

The separate 2026-08-01 campaign passes bounded K8S-006 at observed ordinal-3
`Decommissioning`. A fail-safe Pod-local network rule blocked only the running
Operator Pod's API Service destination for 401 seconds, proving HTTP 200 before
the rule, connection failure during it, and HTTP 200 after removal. The
manager lost its leader-election lease and restarted once in the same Pod.
The exact durable operation, target UID, revisions, request timestamp, and
stage remained unchanged; 36 observations covered 302 seconds with the target
running, unready, outside the EndpointSlice, and at zero restarts and all
three non-targets Ready and serving. After recovery, the restarted manager
resumed the same operation and completed `3 -> 2 -> 1 -> 0` plus ten stable
samples. The hold, fault, and resume records have SHA-256 values
`aca61282531551a7ec970dd2b0139be35dde2c0e1494117ed33e03ff9add5510`,
`a324e0bc639eaba052b475f1342a7595c42be479a38914ead4678b09cfb8876a`,
and `49dc69a31444997ddf5d5c8045bcfd840002937fd621bdbc4df700f2b1c1de7e`.
This does not pass API partitions at other stages or topologies, concurrent
leaders, desired-state conflict, insufficient redundancy, or soak behavior.

The independent workloads spanning all four long absences had zero HEC/search
request failures and exact eventual completeness. They did not pass immediate
distributed-search completeness: the observed-decommissioning run reported 41
count regressions and maximum pending 406 after lifecycle `Completed`, the
readiness-withdrawal run reported 37 regressions and maximum pending 404, and
the target-selection run reported 18 regressions and maximum pending 364 after
`Completed`. That observation remains an OPS-011 and Splunk Enterprise
convergence requirement rather than a pass of the customer-visible
availability invariant.

The API-disconnection workload had the same bounded outcome: 1,800 successful
HEC submissions, zero search-request failures, and exact final results on each
Search Head, but 30 successful-search count regressions and maximum pending
417. K8S-006 recovery therefore passes without converting the separate
OPS-011 immediate distributed-search completeness requirement into a pass.

### Bounded STS-004 SHC-85 evidence

The 2026-08-01 controller-leader-failover campaign passes STS-004 for one
normal Kubernetes Lease takeover at observed ordinal-3 `Decommissioning`.
Starting from one Ready controller, the harness scaled the Deployment to two,
required both zero-restart Pods to be Ready, and proved that one stable holder
renewed Lease `270bec8c.splunk.com`. It then force-deleted that exact holder
after the durable indexer operation had recorded the decommission request,
target UID, revisions, timestamp, and observed Splunk decommission state.

A newly created replacement, rather than the already-waiting follower,
acquired the Lease after expiry. That is valid because Kubernetes leader
election does not guarantee follower fairness. The transition count advanced
exactly from 80 to 81, acquisition was logged, the successor renewed stably,
and two Ready zero-restart contenders were restored. The same indexer
operation resumed. The full roll completed `3 -> 2 -> 1 -> 0` while every
sample retained the same sole leader, two healthy controller Pods, no more
than one unavailable indexer, and zero indexer restarts. The original target's
`IndexerDecommissionRequested` Event count remained one. Ten final stable
samples passed.

The lifecycle and bounded leader records have SHA-256 values
`9b7193931ac6c72f02edc45265a303d4f88a8e59da6967d6e03e368f837ae6f3`
and `c6d662265eec4e5c5683f344c3f7e39a6532f23264253320d9db113ada66a409`.
The independent Job submitted 1,800 events with zero HEC/search request
failures and exact final results, but retained 13 successful-search count
regressions and maximum pending 329; its SHA-256 is
`e34ef36dd49a7f835028d13ebd3336fdd1090f7b7210bbc50d78f27f3ec1ed05`.
Cleanup restored the original one-replica topology and a stable renewed Lease.
This does not pass simultaneous active leaders, Lease deletion/corruption,
inter-contender network partition, repeated failovers, or leader loss at every
lifecycle stage.

## Observability and security scenarios

| ID | Priority | Scenario | Required proof |
|---|---|---|---|
| OBS-001 | P0 | Every stage transition | Status, condition, Event, structured log, and duration agree |
| OBS-002 | P0 | Repeated polling | Logs are sampled/debug-level; Events are deduplicated |
| OBS-003 | P0 | Metrics scrape | Bounded labels, stable counters/gauges/histograms |
| OBS-004 | P1 | Alert timing | Short expected transitions do not page; sustained failures do |
| OBS-005 | P0 | Diagnostic bundle | Stage-organized evidence is complete and redacted |
| OBS-006 | P0 | Secrets/search content injected in errors | No credential, authorization header, Secret data, or search text leaks |
| OBS-007 | P1 | Operation history rollover | Bounded retention preserves current and most recent result |
| OBS-008 | P1 | Custom resource is created with pause annotation already set | Every required phase field and a schema-valid Paused condition are persisted once, no managed workload is created, and reconciliation does not enter an error retry loop |

### OBS-001, OBS-004, and OBS-005 SHC-87 evidence

The accepted 2026-08-01 SHC-87 source and EKS evidence is recorded in
[SHC87DependencyStatusQualification.md](SHC87DependencyStatusQualification.md).
Exact source `20d926658` distinguishes normal referenced-tier convergence from
explicitly contradictory desired state. Missing, Pending, missing-workload,
and not-yet-rolled dependencies produce Pending/Progressing conditions with
stable reason `DependencyNotReady`, a retained specific message, a Normal
Event, structured dependency fields, and a bounded non-error retry. Explicit
dependency and dependent desired-image disagreement remains terminal.

On EKS, one SearchHeadCluster submitted before its LicenseManager accumulated
two aggregating Normal Event series for the absent and Pending dependency,
never entered Error, and recovered without a container restart. The final SHC
had Ready Deployer, 3/3 members, three endpoints, every member registered and
Up, and 8/8 service-routed searches. Cross-namespace references, reverse
MonitoringConsole dependencies, and the terminal desired-image case passed
source coverage but were not imposed on the live fixture. Sustained condition
age is observable; no unsafe universal startup timeout is claimed.

### OBS-001 and OBS-008 SHC-89 evidence

The accepted 2026-08-01 SHC-89 source and EKS evidence is recorded in
[SHC89PausedStatusQualification.md](SHC89PausedStatusQualification.md).
Exact source `3e1716737` initialized current-generation `Pending/Paused`
status for Standalone, LicenseManager, ClusterManager, MonitoringConsole,
IndexerCluster, SearchHeadCluster, and IngestorCluster created already paused.
SearchHeadCluster additionally reported `deployerPhase=Pending`.

All seven resource versions remained unchanged for 45 seconds, no managed
workload was created, and the scoped Operator audit found zero paused-status
and zero Reconciler errors. Removing pause let a LicenseManager and a
three-member SearchHeadCluster converge to Ready. Queue and ObjectStorage have
no active enterprise reconcilers in this baseline and are not claimed as live
OBS-008 targets. SHC-91 subsequently qualified deletion-before-pause and
deletion-before-ordinary-Apply behavior; its independent evidence is recorded
in `SHC91DeletionBeforePauseQualification.md`.

## Compatibility and qualification scenarios

| ID | Priority | Scenario | Required proof |
|---|---|---|---|
| CMP-001 | P0 | Old runtime image with new Operator | Safe fallback or explicit incompatibility |
| CMP-002 | P0 | New runtime image with old Operator | Existing behavior remains functional |
| CMP-003 | P1 | Operator upgrade during active `OnDelete` workflow | No duplicate operation |
| CMP-004 | P1 | Operator upgrade during `RollingUpdate` workflow | Partition and durable state are preserved |
| CMP-005 | P1 | Previous supported Splunk version | Confirmed endpoint and lifecycle compatibility |
| CMP-006 | P2 | Kubernetes minimum supported version | All required primitives work |
| CMP-007 | P2 | Latest qualified Kubernetes version | No changed StatefulSet/probe behavior |
| CMP-008 | P2 | EKS, AKS, GKE, and OpenShift | Provider-specific scheduling, storage, eviction, and networking evidence |
