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
| SHC-81 | Make SHC CR deletion finalization safe after namespace termination begins | Pending | OPS-004, OBS-001, OBS-005 | Selected on `codex/shc-81-termination-safe-finalization` from integrated baseline `efbff783f`; implementation and qualification are not claimed by this registration |
| SHC-82 | Define and qualify App Framework restart-required app availability across Search Head and indexer clusters | Pending | OPS-006, OPS-011, OBS-001, OBS-003, OBS-005 | Customer-reported behavior includes a bundle-triggered indexer message with `searchable=0` and `force=0`; exact Splunk semantics, effective configuration, active-search behavior, and end-to-end availability remain to be established before selecting a solution |

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

## Next execution records

SHC-79 through SHC-81 record separate gaps discovered by the accepted SHC-78
campaign. SHC-79 and SHC-80 are now source- and EKS-qualified on their
isolated branches.
SHC-82 records a separate customer-reported App Framework availability
requirement that spans both Search Head and indexer clusters. SHC-81 is
selected on its isolated branch; SHC-82 remains registered but unassigned.
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

2026-07-29 UTC: Registered SHC-82 and OPS-011 from a customer-reported
App Framework restart-availability concern. The record deliberately preserves
the observed `searchable=0` and `force=0` signal without treating that one log
line as proof of replica loss, data unavailability, or root cause. Source,
Splunk semantic, active-search, and end-to-end qualification work remains
pending.

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
