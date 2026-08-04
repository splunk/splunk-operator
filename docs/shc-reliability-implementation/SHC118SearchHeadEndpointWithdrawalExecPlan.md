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
- [x] (2026-08-04 UTC) An external architecture review found that status merge
  could accept a different operation ID based only on a newer timestamp while
  the persisted operation was still active. Exact source
  `8152fc042e1da814cc37238b7a9eb4cf22b76222` now rejects that replacement
  until the persisted lifecycle operation reaches `Completed`.
- [x] (2026-08-04 UTC) Passed generation, manifests, focused lifecycle and API
  tests, the complete 43-suite native test gate (192/192 specs, 78.8 percent
  composite coverage), `make build`, and `git diff --check` on the exact source.
- [x] (2026-08-04 UTC) Passed chart lint, all 150 Helm tests, full race checks
  for the v4 API and metrics packages, and 20 race-enabled repetitions of the
  changed policy, withdrawal, no-early-detention, and status-merge paths.
- [x] (2026-08-04 UTC) Repeated the complete native Linux gate with the
  repository-matched Ginkgo 2.32.0 CLI: 43 suites, 192/192 specs, and 78.8
  percent composite coverage. `make build` passed with no generated diff.
- [x] (2026-08-04 UTC) Built and pushed the exact Linux AMD64 Operator candidate
  through the Makefile. Immutable OCI index digest is
  `sha256:bc733990967abade9419be4caa85d68040355c959d86410a93bd8765830eed9f`.
- [x] (2026-08-04 UTC) Added qualification harness through `7363f71a9`. Bash
  syntax and ShellCheck pass. Its read-only rehearsal against the retained cluster proved
  exact Operator-image matching, a Ready three-member zero-restart baseline,
  closed partition, serving readiness gates, and three routable client
  endpoints without mutating the cluster. The harness also refuses to trigger
  without one active Ready API-independent workload client and fails on any
  HEC or search request failure observed before lifecycle completion. A
  separate mode now removes the policy field, identifies the source as the
  Operator-resolved default, and verifies that the persisted deadline is
  exactly 30 seconds after the endpoint-withdrawal observation; this prevents
  an explicitly set value of 30 from being mistaken for omitted-policy
  evidence.
- [x] (2026-08-04 19:56Z) Completed the explicit-120-second EKS lifecycle
  harness. It rolled `2 -> 1 -> 0`, retained at least two routable endpoints,
  allowed at most one unready member, replaced the Operator during ordinal 2's
  active interval, preserved the exact operation/UID/observation/deadline/
  sequence, recorded three observation Events and zero invalidations, retained
  every ordinal's `etc` and `var` claims, replaced all three Pod UIDs, and
  finished 12 stable samples with zero container restarts. Harness exit code is
  zero.
- [x] (2026-08-04 20:42Z) The controller-restart campaign's independent Job
  reached Kubernetes `Complete`: 3,600 submissions, zero HEC failures, zero
  search-request failures, zero count regressions, maximum pending 2, and exact
  final count/min/max/distinct `3600/1/3600/3600`. Runner, workload wait, and
  finalizer exit codes are zero; all 37 listed artifact hashes verify.
- [x] (2026-08-04 21:03Z) Completed the omitted-field,
  Operator-default-30-second EKS lifecycle harness with no controller
  replacement. It rolled `2 -> 1 -> 0`; each exact operation persisted a
  30-second observation-to-deadline interval before detention; minimum
  endpoints were 2, maximum unready Pods 1, all three UIDs changed while claims
  remained stable, invalidation delta was 0, restarts/request failures were 0,
  and 12 stability samples passed. Harness exit code is zero.
- [ ] Allow the default campaign's independent 3,600-sample Job to reach
  Kubernetes `Complete`, verify final uniqueness/counters, and seal hashes.

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
  operation ID only after the persisted operation is `Completed` and the new
  start time is not older than the persisted transition.
- Observation: a broad race run over the entire enterprise package reproduces
  existing races in App Framework scheduler tests and a Cluster Manager test
  seam, including `TestPhaseManagersMsgChannels`, `TestPodCopyWorkerHandler`,
  `TestInstallWorkerHandler`, and `TestApplyClusterManager`.
  Consequence: that broad repository result remains a separate open quality
  issue. None of those paths is changed by SHC-118; the complete normal gate
  and the race-enabled changed-path gates pass.
- Observation: `endpointWithdrawalDelaySeconds` has no CRD admission-default
  marker. The API preserves omission, while
  `ResolveSearchHeadClusterLifecyclePolicy` resolves the missing field to 30.
  Consequence: the omitted-field campaign qualifies the Operator-resolved
  product default. It must not be described as Kubernetes API defaulting.
- Observation: each replacement briefly returned HTTP 503 from
  `/services/shcluster/member/info` while the new Splunk process was not yet
  able to communicate with the captain. The Operator recorded those facts at
  `INFO` level with a structured `error` field and held the lifecycle in
  `WaitingForContainer`.
  Consequence: an audit that searches for the word `error` alone produces false
  positives. Qualification must inspect structured log level and lifecycle
  action; these three expected startup waits are not controller errors.
- Observation: the default campaign's Kubernetes Event count delta was 4 even
  though its sample stream contains exactly three new operation IDs and three
  withdrawal observations. During the run, expired prior Event objects were
  recreated from the long-lived Event broadcaster's correlation series with a
  retained count and first timestamp.
  Consequence: Event series are at-least-once operational signals, not an exact
  lifecycle-operation ledger. Status operation IDs and retained samples prove
  exact cardinality; the Event gate requires at least one observation per
  replacement and zero invalidations rather than equality to replica count.

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
- Decision: fail closed when status merge encounters a different operation ID
  while the persisted lifecycle operation is active.
  Rationale: a newer timestamp alone cannot prove that the prior target and its
  endpoint-withdrawal ownership were safely completed; controller recovery and
  cancellation continue under the persisted operation identity.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: qualify the 30-second default by omitting the field in a separate
  steady-controller run, not by explicitly patching the value to 30.
  Rationale: explicit-value coverage proves policy handling but cannot prove
  the Operator's omitted-field resolver. The harness records the policy source
  as `operator-default` and independently checks the
  observation-to-deadline interval.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.

## Outcomes & Retrospective

The production source is implemented and source-qualified at
`8152fc042e1da814cc37238b7a9eb4cf22b76222`. It adds a customer-configurable,
default-30-second continuous withdrawal interval; persists the exact Pod UID,
observation, immutable deadline, sequence, and invalidation sequence; protects
that proof during status merge; and exposes bounded Events, reasons, and the
`splunk_operator_search_head_endpoint_withdrawal_total` metric. Both Pod-update
and scale-down detention paths fail closed until the interval completes.

The explicit-120-second EKS campaign passes end to end, including manager
replacement inside an active interval, the active-captain ordinal, 3,600
successful HEC/search requests, zero count regressions, exact eventual
uniqueness, and verified artifacts. The omitted-field 30-second lifecycle
harness also passes, but its independent Job has not yet reached its terminal
exact-delivery verdict. This is therefore not yet the final SHC-118 availability
claim.

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
  `8152fc042e1da814cc37238b7a9eb4cf22b76222`.
- Initial feature source:
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
- Immutable Operator OCI index:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator@sha256:bc733990967abade9419be4caa85d68040355c959d86410a93bd8765830eed9f`.
- Qualification branch:
  `codex/shc-118-search-head-endpoint-withdrawal-qualification` at
  `7363f71a90a026b3137c333020422968f6453c8c`.
- Explicit-120-second EKS lifecycle harness: pass, exit code 0, target order
  `[2,1,0]`, minimum endpoints 2, maximum unready Pods 1, Operator restarted,
  observation Event delta 3, invalidation delta 0, stable samples 12, and zero
  HEC/search request failures through roll completion.
- Exact propagation boundaries: ordinal 2 observation/deadline/detention
  `19:37:34Z/19:39:34Z/19:39:36Z`; ordinal 1
  `19:42:50Z/19:44:50Z/19:44:50Z`; ordinal 0
  `19:48:29Z/19:50:29Z/19:50:35Z`.
- Controller-replacement UIDs: before
  `c1e02e73-1ba7-4580-8f0f-9b63b2bda55f`, after
  `75c1be97-1066-4dc9-aaf4-605d1b8511af`. The active-captain target transferred
  captaincy from ordinal 0 to ordinal 1 before replacement authorization.
- Controller-restart workload: 3,600 submissions, HEC failures 0,
  search-request failures 0, count regressions 0, maximum pending 2, final
  count/min/max/distinct `3600/1/3600/3600`, and `complete=true`.
- Controller-restart SHA-256 values: samples
  `3d606e22fc716ffb4c47db57c2a8388f7e0c6d2fc3c974315bfa52048d50a89d`,
  workload
  `1a54f859fdf048cd259cb6f9cf101e35fba29047b9c2485e3dd18a34b7922115`,
  final Pods
  `6d9b1f7e581c50828f9e8596a80ef30d532caddcf589f73b1d9e01165158c4ea`,
  and artifact manifest
  `fd7de67bdadc243609d191bbd5ce9041a0b69bbfbf96d8d545c6daab5e9697e7`.
  All 37 manifest entries verify from the repository root. Structured final
  Operator logs contain no `ERROR` or `FATAL` level record.
- Omitted-field Operator-default lifecycle: passed; terminal workload and
  artifact gates pending.
- Omitted-field lifecycle harness: pass, exit code 0, policy source
  `operator-default`, target order `[2,1,0]`, minimum endpoints 2, maximum
  unready Pods 1, Operator not restarted, Event count delta 4, invalidation
  delta 0, stable samples 12, and zero HEC/search request failures through roll
  completion.
- Omitted-field exact propagation boundaries: ordinal 2
  `20:49:10Z/20:49:40Z/20:49:41Z`; ordinal 1
  `20:53:08Z/20:53:38Z/20:53:39Z`; ordinal 0
  `20:57:21Z/20:57:51Z/20:57:53Z` for observation/deadline/detention.
- Omitted-field replacement UIDs: ordinal 2
  `b6a6fe59-f048-46b8-adde-e2d9700d5ab0 -> 6cb304d3-16c0-4c3b-a4d1-6e1165b5d6b5`,
  ordinal 1
  `3b1146ec-f40a-4ac6-ad66-61933226b3e3 -> f1d8a7af-e6e1-4f88-a8e8-3e1c2e935027`,
  and ordinal 0
  `ca1839a6-c1ca-439e-b9be-0061cda85252 -> 268d4dcf-d812-4896-9994-1b91d17cc61d`;
  every `etc` and `var` claim was preserved.
- Omitted-field workload final counters and evidence hashes: pending.

## Interfaces and Dependencies

SHC-118 depends on the existing Search Head readiness gate, dynamic captain
workflow, lifecycle feature gates, and SHC-116's proven Kubernetes propagation
model. It changes the v4 SearchHeadCluster policy/status schema and Operator
logic only. It does not require Docker-Splunk or Splunk Enterprise source
changes, and it does not replace the separate Splunk-side persistent-connection
and explicit partial-result requirements.

Revision note (2026-08-04 UTC): Corrected the qualification design so the
default-policy campaign removes `endpointWithdrawalDelaySeconds`, records
whether the policy came from the Operator resolver or an explicit value, and
proves the persisted observation-to-deadline interval. Source and generated
CRD inspection also corrected the evidence boundary: this is an
Operator-resolved product default, not Kubernetes API defaulting.

Revision note (2026-08-04 19:56Z): Recorded the passing explicit-120-second
EKS lifecycle harness, including controller replacement, exact per-ordinal
deadline ordering, dynamic captain transfer, stable claims, bounded endpoint
availability, and zero request failures through roll completion. The Job's
terminal exact counters/hashes and the omitted-field default campaign remain
open and are not inferred from this lifecycle pass.

Revision note (2026-08-04 20:42Z): Closed the explicit-policy workload and
artifact gates with 3,600 successful request pairs, zero count regressions,
exact final uniqueness, zero exit codes, and 37 verified hashes. The separate
omitted-field Operator-default campaign remains open.

Revision note (2026-08-04 21:03Z): Recorded the passing omitted-field
Operator-default lifecycle harness with exact 30-second intervals, sequential
replacement, dynamic captain transfer, stable claims, bounded endpoints, and
zero request failures through roll completion. Its Job and final hashes remain
open. The revision also distinguishes correlated Kubernetes Event counts from
the exact three-operation status/sample ledger.
