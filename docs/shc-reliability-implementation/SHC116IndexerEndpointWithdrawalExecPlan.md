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
- [ ] Complete immutable EKS qualification, including controller replacement
  during the delay.

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

## Outcomes & Retrospective

The exact source and immutable Linux image are frozen. Source checks establish
restart-safe and fail-closed state-machine behavior, but the live availability
claim remains open until the same workload that reproduced the failure
completes a full `3 -> 2 -> 1 -> 0` roll with the candidate controller.

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
- EKS candidate evidence: pending.

## Interfaces and Dependencies

SHC-116 is stacked on SHC-115 and composes with SHC-112 through SHC-114. It
adds `spec.lifecyclePolicy.endpointWithdrawalDelaySeconds` and durable
`status.podUpdate.endpointWithdrawal*` fields to v4 IndexerCluster. It observes
the existing client-facing indexer Service and EndpointSlices and changes no
Splunk REST endpoint. No Docker-Splunk or Splunk Enterprise source change is
required.
