# Wait for Search Head endpoint withdrawal before detention

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

The current Search Head lifecycle first makes its target not Ready, waits until
the client Service's EndpointSlices no longer route to that Pod, and can then
request manual detention in the same reconciliation. SHC-116 established from
a live indexer failure that EndpointSlice publication is not the end of
dataplane propagation and that a continuous delay must precede the Splunk-side
traffic withdrawal action.

SHC-118 applies the same Kubernetes boundary to Search Heads before detention.
The exact target Pod must remain not Ready and absent from routable client
Service EndpointSlices for one persisted propagation interval. The observation
must survive controller replacement and must be invalidated if the exact Pod
becomes routable again. Existing detention, search-drain, captain-transfer,
shutdown, replacement, and rejoin gates remain unchanged and still follow this
new barrier.

This delay reduces newly routed traffic during the transition. It cannot force
an already-established TCP connection to move; the SHC-107 response-aware
client evidence and a Splunk-side explicit draining/partial-result contract
remain separate requirements.

## Progress

- [x] (2026-08-04 UTC) Re-audited the Search Head readiness-gate,
  EndpointSlice, and detention control path after the SHC-116 live finding.
- [x] Confirmed source currently observes withdrawal and immediately calls the
  detention action without a durable propagation deadline.
- [x] (2026-08-04 UTC) Implemented the bounded API, durable state, validation,
  status merge, observability, and tests on isolated source
  `cd79208b8b18ad90aa916cc1e1418911d42bd924`, stacked on exact SHC-116.
- [x] (2026-08-04 UTC) Passed generation, manifests, focused lifecycle and API
  tests, the complete 43-suite native test gate (192/192 specs, 78.8 percent
  composite coverage), `make build`, and `git diff --check` on the exact source.
- [x] (2026-08-04 UTC) Passed chart lint, all 150 Helm tests, full race checks
  for the v4 API and metrics packages, and 20 race-enabled repetitions of the
  changed policy, withdrawal, no-early-detention, and status-merge paths.
- [ ] Build an immutable Linux image and qualify a complete Search Head roll on
  EKS, including controller replacement during the propagation interval.

## Surprises & Discoveries

- Observation: the Search Head path already checks the correct client Service,
  exact target UID, Pod Ready condition, serving readiness gate, and nil-ready
  fail-closed EndpointSlice semantics.
  Consequence: the missing boundary is the continuous propagation interval,
  not basic EndpointSlice selection.
- Observation: detention changes Splunk behavior after Kubernetes has stopped
  choosing the target for new Service connections.
  Consequence: a propagation barrier belongs before detention rather than in
  preStop or after the member is already draining.
- Observation: persistent connections can remain pinned independently of
  EndpointSlice state.
  Consequence: acceptance must distinguish reduced new routing from complete
  connection draining and must not overstate the Operator's control.
- Observation: the existing 180-second effective detention timeout can be
  configured independently from the new propagation delay.
  Consequence: API and controller validation require the effective endpoint
  withdrawal delay to remain strictly below the effective detention timeout;
  otherwise an operation would be guaranteed to time out before detention.
- Observation: lifecycle status can be written by reconciliations that began
  from different resource versions.
  Consequence: status merge preserves monotonic observation and invalidation
  state for the same operation, rejects corrupt active proof, and permits a new
  operation only when its start time is not older than the persisted operation.
- Observation: a broad race run over the entire enterprise package reproduces
  existing races in App Framework scheduler tests and a Cluster Manager test
  seam, including `TestPhaseManagersMsgChannels`, `TestPodCopyWorkerHandler`,
  `TestInstallWorkerHandler`, and `TestApplyClusterManager`.
  Consequence: that broad repository result remains a separate open quality
  issue. None of those paths is changed by SHC-118; the complete normal gate
  and the race-enabled changed-path gates pass.

## Decision Log

- Decision: use the existing Search Head lifecycle policy and operation status
  rather than a separate controller-local timer.
  Rationale: the effective delay, exact UID, observation, deadline, sequence,
  and invalidation must survive leader/controller replacement.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: default to the same 30-second nonzero interval and 1 through 86,400
  second validation range as the indexer barrier unless live evidence requires
  a different tier-specific value.
  Rationale: both actions rely on asynchronous EndpointSlice consumers, while
  retaining an explicit customer tuning surface.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: keep this work separate from SHC-116 and SHC-117.
  Rationale: it changes a different CRD/status schema and lifecycle action;
  independent review avoids conflating indexer evidence, Search Head behavior,
  and qualification-only duration changes.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: enforce the delay on both Pod-update and permanent scale-down
  intents before either path can request detention.
  Rationale: Service routing does not distinguish why the exact member is being
  removed, so both lifecycle paths need the same Kubernetes propagation
  contract.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.

## Outcomes & Retrospective

The production source is implemented and source-qualified at
`cd79208b8b18ad90aa916cc1e1418911d42bd924`. It adds a customer-configurable,
default-30-second continuous withdrawal interval; persists the exact Pod UID,
observation, immutable deadline, sequence, and invalidation sequence; protects
that proof during status merge; and exposes bounded Events, reasons, and the
`splunk_operator_search_head_endpoint_withdrawal_total` metric. Both Pod-update
and scale-down detention paths fail closed until the interval completes.

This is not yet a live availability claim. Immutable Linux image construction
and EKS qualification, including controller replacement during the interval,
remain open.

## Plan of Work

Add `endpointWithdrawalDelaySeconds` to the Search Head lifecycle policy and
durable observation/deadline/UID/sequence/invalidation fields to the lifecycle
operation. Resolve and validate the policy, enforce monotonic status merge, and
replace the boolean withdrawal check at the detention boundary with a
restart-safe barrier. Add bounded Events and metrics consistent with SHC-116.

Test target UID/revision identity, nil readiness, first observation, delay,
invalidation, immutable effective deadline, manager reconstruction, scale-down
and Pod-update intents, and no detention before expiry. Then run Linux gates,
build an immutable image, and qualify both Service-routed and long-lived search
clients across a full Search Head roll with dynamic captain handling.

## Validation and Acceptance

- detention cannot be requested before the target is not Ready, absent from
  routable client EndpointSlices, and continuously withdrawn through the
  persisted deadline;
- target replacement or returned routability invalidates stale proof;
- policy edits and controller replacement cannot shorten an active deadline;
- scale-down and Pod-update intents use the same exact target contract;
- current detention timeout, search drain, captain transfer, replacement,
  rejoin, cancellation, and rollback behavior remains fail closed;
- at least two Search Heads remain serving in a three-member cluster and the
  supported captain workflow precedes active-captain replacement;
- the full workload records request failures, explicit detention responses,
  count regressions, final completeness, and connection topology separately;
  and
- Events, status, logs, and bounded-label metrics expose observation and
  invalidation without credentials.

## Idempotence and Recovery

The barrier performs no Splunk mutation while waiting. One exact target UID and
sequence owns an immutable effective deadline. A manager replacement resumes
that state. Returned routing invalidates the sequence and requires a new
continuous interval. Rollback must inspect an active withdrawn target and must
not discard the lifecycle operation before restoring serving eligibility.

## Artifacts and Notes

- Production parent: exact SHC-116 source
  `96c83dcadc25e6034ba2a41898c84ed1b255b570`.
- Source branch:
  `codex/shc-118-search-head-endpoint-withdrawal`.
- Exact source:
  `cd79208b8b18ad90aa916cc1e1418911d42bd924`.
- Existing immediate-action source:
  `pkg/splunk/enterprise/searchheadcluster_lifecycle.go`.
- Existing withdrawal observation source:
  `pkg/splunk/enterprise/searchhead_serving_readiness.go`.
- Source evidence: generation, manifests, focused API/lifecycle tests, all 43
  native suites with 192/192 specs and 78.8 percent composite coverage,
  `make build`, chart lint, all 150 Helm tests, full v4 API and metrics package
  race checks, 20 changed-path enterprise race repetitions, and
  `git diff --check` passed on the exact source.
- Broad enterprise race log SHA-256:
  `f412235c58fe8b8ac7b47441b854e092036f8500932dee2d53d6280d535c7b87`;
  failures are confined to the pre-existing App Framework/Cluster Manager test
  paths listed above rather than SHC-118 files.
- Immutable image and EKS evidence: pending.

## Interfaces and Dependencies

SHC-118 depends on the existing Search Head readiness gate, dynamic captain
workflow, lifecycle feature gates, and SHC-116's proven Kubernetes propagation
model. It changes the v4 SearchHeadCluster policy/status schema and Operator
logic only. It does not require Docker-Splunk or Splunk Enterprise source
changes, and it does not replace the separate Splunk-side persistent-connection
and explicit partial-result requirements.
