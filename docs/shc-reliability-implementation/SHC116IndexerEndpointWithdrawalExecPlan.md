# Wait for indexer endpoint withdrawal before decommission

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

The accepted-image indexer rollout showed that removing Pod readiness is not
by itself proof that clients have stopped routing to the target. Kubernetes
EndpointSlice publication and its consumers are asynchronous. In the observed
failure, the Operator requested Splunk decommission at the same second that a
long-lived in-cluster HEC workload received its only failed submission, while
the target still appeared routable in a later EndpointSlice sample.

SHC-116 adds an Operator-owned traffic-withdrawal barrier before indexer
decommission. The exact target Pod must be not Ready and absent from every
routable entry in the client-facing indexer Service's EndpointSlices. The
Operator persists that observation and waits a configurable propagation delay
before asking Splunk to decommission the peer. If the exact target becomes
routable again during the delay, the observation is invalidated and the delay
must start again from a new observation.

This is a Kubernetes control-plane propagation barrier, not a claim that
EndpointSlice observation can terminate an already-established TCP connection.
Persistent-client behavior remains separately qualified by SHC-107. This work
changes neither Docker-Splunk nor Splunk Enterprise.

## Progress

- [x] (2026-08-04 UTC) Correlated the only baseline HEC failure with the exact
  indexer decommission request and a still-routable target EndpointSlice.
- [x] Created isolated source branch
  `codex/shc-116-indexer-endpoint-withdrawal` from exact SHC-115 source.
- [x] Added a durable target-UID/revision-specific withdrawal observation,
  immutable effective deadline, monotonic sequence/invalidation state,
  configurable delay, bounded Events and metrics, and restart-safe recovery.
- [x] Added API, CRD, defaulting, validation, merge, endpoint-semantics,
  invalidation, deadline, target-identity, recovery, and no-early-decommission
  tests.
- [x] Exact source `96c83dcad` passed generation, formatting, vet, build,
  focused race checks, chart lint, and all 150 Helm tests on macOS and Linux.
- [x] Built and pushed the exact cumulative `linux/amd64` Operator image
  through the repository Make target. ECR reports immutable OCI index digest
  `sha256:f16a128454cbe60f2d6230c452c69efca6dafe6526b8f2589bdc27b3203a3a1f`;
  its runnable manifest is
  `sha256:5259c53cbf6723c3636eb8fe6a3e59e417f1bdf8c1375634496db9c064af2508`.
- [x] Repeated the full native-Linux Make gate at exact source: 43 suites,
  192/192 specs, zero failures, and 78.7 percent composite coverage.
- [x] (2026-08-04 UTC) Replaced the active Operator during ordinal zero's
  persisted propagation interval. The replacement manager recovered the exact
  operation, target UID, observation, deadline, and sequence, and requested
  decommission only after that deadline.
- [x] (2026-08-04 18:20Z) Completed the fresh immutable EKS lifecycle monitor
  across `3 -> 2 -> 1 -> 0`. It recorded 647 full snapshots from
  `15:58:41Z` through `18:20:20Z`, all four Pod UID replacements with stable
  PVC claims, zero container restarts, exact EndpointSlice/deadline ordering,
  every post-replacement Search Head peer-convergence gate, and 60 consecutive
  final-state snapshots. The monitor exited zero.
- [ ] Allow the independent 10,800-sample HEC/distributed-search Job to reach
  its terminal Kubernetes condition, verify exact final uniqueness and all
  counters, and seal the complete evidence directory. The lifecycle monitor
  and controller-replacement sub-gate are complete; the long-workload verdict
  remains open.

## Surprises & Discoveries

- Observation: Pod readiness withdrawal and EndpointSlice withdrawal are
  distinct observations, and EndpointSlice consumers need time after the API
  object changes.
  Consequence: `PodReady=False` alone is not authorization to begin Splunk
  decommission.
- Observation: a nil EndpointSlice `ready` condition means ready unless the
  endpoint is terminating under Kubernetes API semantics.
  Consequence: the barrier must treat nil readiness as routable rather than as
  withdrawn.
- Observation: SHC-112 can hold advancement for tens of minutes while Search
  Heads remove the replacement peer's stale prior Pod-IP entry.
  Consequence: endpoint withdrawal and post-replacement search-peer convergence
  solve different lifecycle boundaries and both must remain fail closed.
- Observation: the baseline HEC client can retain an established connection
  independently of new Service routing decisions.
  Consequence: SHC-116 requires the persistent-client workload gate; the API
  observation alone is insufficient acceptance evidence.
- Observation: `SHC98_STABLE_SAMPLES=60` means 60 consecutive complete
  Kubernetes and Splunk observations, not a fixed five-minute wall-clock
  window. The configured five seconds is a minimum sleep after each sample;
  the retained-cluster sample itself took about eight additional seconds.
  Evidence: the accepted final observations ran from `18:07:18Z` through
  `18:20:20Z`, about 13 minutes for 60 full snapshots.
  Consequence: describe acceptance in sample counts and observed timestamps,
  not as a fixed five-minute duration.
- Observation: an `OnDelete` StatefulSet can retain an older
  `status.currentRevision` after every manually replaced Pod has reached
  `status.updateRevision`.
  Evidence: final StatefulSet current revision was
  `splunk-shcfinal-idxc-indexer-6968767b9b`, update revision was
  `splunk-shcfinal-idxc-indexer-5bc5fb9bd`, and all four live Pod revision
  labels were `splunk-shcfinal-idxc-indexer-5bc5fb9bd`.
  Consequence: `OnDelete` acceptance must inspect every Pod revision label;
  equality of StatefulSet current and update revisions is a RollingUpdate-only
  assertion.

## Decision Log

- Decision: authorize decommission only after both exact target Pod readiness
  withdrawal and absence from routable client-Service EndpointSlice entries.
  Rationale: these are the Kubernetes facts available to the Operator for the
  selected workload identity and routing surface.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: persist the effective deadline and target UID in status.
  Rationale: controller replacement and later policy edits must not shorten or
  reset an already observed operation.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: invalidate the observation if routability returns before the
  deadline.
  Rationale: the quiet interval must be continuous; stale status cannot
  authorize a destructive action after the premise stopped being true.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: default the propagation delay to 30 seconds and permit an explicit
  value from 1 through 86,400 seconds.
  Rationale: the default establishes a nonzero safety interval while allowing
  deployments to tune for their networking dataplane and operational evidence.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: express the post-roll stability gate as 60 consecutive full
  observations and record its actual timestamps, rather than label it a fixed
  five-minute window.
  Rationale: Kubernetes and Splunk REST collection time is part of each sample;
  the configured five seconds is only the sleep between observations.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: for the retained `OnDelete` indexer StatefulSet, prove revision
  convergence from every live Pod's revision label matching
  `status.updateRevision`.
  Rationale: Kubernetes can leave `status.currentRevision` on the prior hash
  after all manually replaced Pods have converged, so current/update equality
  would reject a healthy `OnDelete` result for the wrong reason.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.

## Outcomes & Retrospective

The exact source and immutable Linux image are frozen. Source checks establish
restart-safe and fail-closed state-machine behavior. Live EKS evidence now
establishes restart recovery for one active propagation interval and a complete
fresh `3 -> 2 -> 1 -> 0` lifecycle monitor with 60 consecutive final-state
snapshots. No decommission preceded its persisted deadline, no more than one
indexer was unavailable, all replacement UIDs retained their PVC claims, and
container restarts remained zero. The overall long-workload claim remains open
until the independent Job reaches its terminal exact-completeness verdict.

## Plan of Work

Preserve the accepted-image diagnostic artifacts. Add only the generated v4
IndexerCluster schema fragments to the retained cluster CRD so its installed
v1/v2 compatibility versions remain unchanged. Configure a 30-second delay,
deploy the immutable SHC-116 manager image, and start a fresh monitor plus HEC
and distributed-search workload.

During one target's withdrawal delay, replace the active controller. Require
the same target UID, operation ID, observation timestamp, and deadline after
takeover, with no indexer Pod replacement before the deadline. Complete every
ordinal and the SHC-112 exact peer-convergence gate. Then record workload,
EndpointSlice, Event, metric, resource-soak, PVC, revision, and health evidence.

## Validation and Acceptance

Source acceptance requires:

- target Pod readiness and client-Service EndpointSlice routing must both be
  withdrawn before the observation is recorded;
- nil EndpointSlice readiness remains routable;
- target UID or desired-revision mismatch rejects stale evidence;
- the persisted effective deadline survives manager reconstruction and policy
  change;
- routability returning before the deadline invalidates the sequence;
- neither normal nor recovery decommission can run before the continuous delay
  expires;
- status merge cannot shorten or rewrite a recorded sequence; and
- generation, format, vet, build, full tests, focused race checks, chart lint,
  all Helm tests, and clean-tree checks pass.

Live acceptance additionally requires:

- EndpointSlice non-routability precedes the observed timestamp, and the
  Splunk decommission request is no earlier than the persisted deadline;
- active controller replacement during the delay preserves the exact operation
  and produces no duplicate or early decommission;
- a complete reverse-ordinal indexer roll advances only after SHC-112 exact
  search-peer convergence for the prior replacement;
- the fresh workload has zero HEC and search request failures, delivers every
  event exactly, and records count completeness separately from HTTP success;
- at least the required searchable/serving peers remain available, RF/SF stays
  healthy, PVC identity is stable, and no unexpected restart occurs; and
- Events, status, logs, and bounded-label metrics expose observed and
  invalidated withdrawal without credentials.

## Idempotence and Recovery

Reconciliation can repeat while waiting. The effective deadline is calculated
once for one target UID and observation sequence, then merged monotonically.
Controller replacement reads that durable state. A later policy edit affects a
future sequence only. If the endpoint becomes routable again, the current
sequence is invalidated and cannot be reused; a new continuous observation is
required. Rollback must first inspect any active operation and must not remove
status fields while a target is withdrawn.

## Artifacts and Notes

- Parent source: `cd3498393d2801d8498ca3dc9a2e20e4c30edcf8`.
- Source branch: `codex/shc-116-indexer-endpoint-withdrawal`.
- Exact source: `96c83dcadc25e6034ba2a41898c84ed1b255b570`.
- Native Linux: 43 suites, 192/192 specs, zero failures, and 78.7 percent
  composite coverage.
- Image tag:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc116-96c83dcad-linux-amd64`.
- ECR OCI index:
  `sha256:f16a128454cbe60f2d6230c452c69efca6dafe6526b8f2589bdc27b3203a3a1f`.
- Runnable `linux/amd64` manifest:
  `sha256:5259c53cbf6723c3636eb8fe6a3e59e417f1bdf8c1375634496db9c064af2508`.
- Baseline diagnostic: one HEC failure at `2026-08-04T13:40:27Z`, the
  decommission-request timestamp; no search request failure in that sample.
- EKS controller-replacement sub-gate: ordinal-zero operation
  `28f17551-4fa7-4567-b5b7-817f27851258:splunk-shcfinal-idxc-indexer-7795598cd8:1785857163531756307`
  observed exact target UID `28f17551-4fa7-4567-b5b7-817f27851258` withdrawn at
  `2026-08-04T15:26:14Z`, with immutable deadline
  `2026-08-04T15:26:44Z` and sequence 1.
- The old manager was removed during that interval. Replacement manager UID
  `feb9a040-2053-4fcf-86d5-9dc450e68469` became Ready at
  `2026-08-04T15:26:33Z` and retained the same durable fields. The decommission
  request was recorded at `2026-08-04T15:26:53Z`, nine seconds after the
  deadline. No duplicate or early request was observed.
- EKS full-roll long-workload verdict: in progress.
- Fresh full-roll lifecycle monitor: exit code 0; 647 snapshots from
  `2026-08-04T15:58:41Z` through `2026-08-04T18:20:20Z`; target order
  `[3,2,1,0]`; 60 consecutive final-state samples from `18:07:18Z` through
  `18:20:20Z`; all four Pod UIDs changed, every `etc` and `var` PVC claim was
  preserved, and all four replacement Pods had zero restarts.
- Monitor artifact SHA-256 values: TSV
  `7bb72cc61397ba923fda5645e63146820f76f10b36541ca0eb14c6ba2d186a66`,
  Events
  `43c0ca28a2f3f4df819824e6a441df273490dcb71535c16b7c71c52a8c9e04af`,
  final configuration
  `492e557ab3ae3dc5aa77a2423abcdb871a35fd93a7f60e605cdc37d606d2e971`,
  and monitor stdout
  `16eebe9c61746e9841cac64f4c127c5336bde8efb59bc7341df6f53aeb27a676`.
- The 10,800-sample workload and final evidence-directory hash remain in
  progress and are not inferred from the successful lifecycle monitor.

## Interfaces and Dependencies

SHC-116 is stacked on SHC-115 and composes with SHC-112 through SHC-114. It
adds `spec.lifecyclePolicy.endpointWithdrawalDelaySeconds` and durable
`status.podUpdate.endpointWithdrawal*` fields to v4 IndexerCluster. It observes
the existing client-facing indexer Service and EndpointSlices and changes no
Splunk REST endpoint. No Docker-Splunk or Splunk Enterprise source change is
required.

## Revision Note

Updated on 2026-08-04 after the fresh EKS lifecycle monitor exited zero. This
revision records only the completed full-roll and stability-snapshot evidence;
it deliberately leaves the long-workload verdict open until the Kubernetes Job
finishes and its final counters can be verified.
