# Classify expected SHC observation transients without hiding failures

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

An SHC captain or member can be temporarily unreachable while its Pod is being
terminated, its Service endpoint is withdrawn, and captain election or member
rejoin is in progress. The controller must keep observing the cluster through
that interval. A failed observation is operationally important, but it is not
always a controller or availability failure.

SHC-108 defines stage-aware, bounded classification for these observations.
Expected planned observations remain visible with target identity, lifecycle
stage, elapsed time, serving capacity, and recovery. An unplanned Pod deletion
must still create one clear unexpected-disruption error; after Kubernetes has
authoritatively identified that same deleted target, repeated identical polls
can be deduplicated while capacity remains safe. A failure with no matching
Pod event, one that exceeds its budget, or one that threatens quorum or the
serving floor remains an error. The goal is supportable evidence without alarm
noise and without suppressing a real SHC failure.

This work item does not change captain election, captain-transfer safety,
readiness, or rollout progression. It changes only how known observation
failures are classified, counted, and explained after those safety decisions
have been made.

## Progress

- [x] (2026-08-04 02:10Z) Registered the requirement from the SHC-107
  active-captain replacement evidence.
- [ ] Inventory every controller path that retrieves captain election state,
  captain identity, member information, and member management endpoints.
- [ ] Define one shared classifier using durable lifecycle status and current
  Kubernetes Pod/Endpoint observations.
- [ ] Add focused tests for expected, expired, unexpected, and capacity-losing
  observation failures.
- [ ] Add low-cardinality metrics and Events that expose duration and outcome.
- [ ] Qualify planned and unplanned captain replacement, controller restart,
  API-server interruption, quorum loss, and recovery on EKS.

## Surprises & Discoveries

- Observation: a successful availability campaign can still produce many
  controller ERROR records.
  Evidence: during SHC-107 deletion of the selected active captain, the
  Operator emitted 23 `captain election failed` records while the management
  request was proxied to the terminating captain and returned HTTP 503, plus
  16 `unable to retrieve SearchHeadCluster member info` records while the old
  member refused connections. The window lasted from `01:15:15Z` through
  `01:16:48Z`.
  Consequence: raw ERROR count alone cannot distinguish the expected
  observation boundary from a persistent controller or SHC failure.
- Observation: lowering severity based only on an HTTP status or connection
  error would hide real failures.
  Evidence: the same 503 or connection refusal can occur when no owned target
  is terminating, after a lifecycle timeout, or while serving capacity/quorum
  is already below its required floor.
  Consequence: classification must combine error type with durable operation
  identity, target UID, current stage, target Pod deletion state, EndpointSlice
  state, elapsed budget, and cluster capacity.
- Observation: planned target-member unavailability is already classified in
  current source, but captain observation and unplanned deletion are different
  paths.
  Evidence: `updateStatus` calls
  `lifecycleMemberObservationExpectedUnavailable` and logs the owned lifecycle
  target at info severity. The SHC-107 active-captain replacement was a direct
  unplanned Pod deletion, so no durable Operator lifecycle operation owned that
  target; member and captain observation failures followed the normal error
  path.
  Consequence: SHC-108 must extend/deduplicate the missing boundaries rather
  than replacing the existing planned-member classification or pretending an
  unplanned deletion is expected.

## Decision Log

- Decision: classify an observation as expected-transient only when all
  required lifecycle and Kubernetes facts agree.
  Rationale: a network error by itself has no safe operational meaning.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: retain one error transition for an unplanned Pod deletion, then
  deduplicate identical continuing observations while the same Kubernetes
  disruption remains authoritative and capacity is safe.
  Rationale: the initiating disruption is unexpected and alertable; every
  controller poll is not a new incident.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: retain one bounded warning/condition and metrics rather than log
  every controller poll at warning or error severity.
  Rationale: support needs the start, continuing duration, current owner, and
  recovery outcome; repeated identical poll records add noise and can trigger
  false alerts.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: any capacity-floor, quorum, timeout, unexpected-member, or
  outside-operation failure remains an error.
  Rationale: supportability must not weaken fail-closed lifecycle behavior.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.

## Outcomes & Retrospective

Requirements and evidence boundaries are defined. No production source change
or live correction qualification has been completed. The SHC-107 workload
availability result remains valid, and its controller log is the negative
supportability evidence for this work item.

## Context and Orientation

The SearchHeadCluster controller observes member and captain state through
Splunk management endpoints while Kubernetes independently reports Pod
deletion, Pod readiness, and Service endpoint conditions. Durable lifecycle
status identifies the operation, target Pod UID, current stage, desired
revision, and elapsed timeout budget.

An observation failure can be classified as a planned expected transient only
when:

1. a durable lifecycle operation identifies the same target Pod UID;
2. the operation is in an explicit withdrawal, captain-transfer/election,
   replacement, or rejoin observation stage;
3. Kubernetes shows that target terminating, withdrawn, absent, or not yet
   serving in a way consistent with the stage;
4. the failure is against that target or the captain endpoint known to depend
   on it;
5. the operation remains inside its configured stage budget;
6. no unrelated member is unavailable; and
7. SHC quorum and the minimum serving endpoint invariant remain satisfied.

If any fact is absent or contradictory, normal error classification applies.

For an unplanned Pod deletion there is no durable lifecycle operation. The
first matching member/captain observation failure remains unexpected and must
be recorded as an error transition. Continuing failures may be coalesced into
that incident only when Kubernetes still identifies the same Pod UID as
deleting/absent, the StatefulSet replacement is consistent with that loss,
elapsed recovery remains bounded, unrelated members are healthy, and serving
capacity/quorum stay above their required floors. Recovery closes the incident;
timeout or any contradictory observation produces a new escalation.

## Plan of Work

First enumerate the controller call sites and their current logs/Events. Build
a table of operation stage, observed endpoint, expected error classes, retry
budget, and recovery signal. Do not encode string matching independently in
each caller.

Add a shared classification result with at least:

- operation and target identity;
- current lifecycle stage;
- observation type and normalized reason;
- expected/unexpected classification;
- first-observed time and elapsed duration;
- current Ready/serving member counts and required floor; and
- recovered, expired, or escalated outcome.

Use that result consistently:

- expected first occurrence creates one deduplicated Warning Event and a
  structured warning/info log;
- unplanned deletion creates one deduplicated Error/Warning transition before
  continuing identical polls are coalesced;
- continuing identical polls update duration metrics without repeating the
  same Event or ERROR record;
- recovery emits one Normal Event with total duration;
- budget expiry or contradictory capacity state escalates to ERROR and the
  existing durable blocked condition; and
- an unexpected failure remains ERROR immediately.

Add low-cardinality metrics for total observations by operation kind, stage,
normalized reason, classification, and outcome. Add a duration histogram for
recovered and expired intervals. Do not label metrics with Pod name, UID,
resource name, namespace, raw URL, error string, or operation ID.

## Validation and Acceptance

Source acceptance requires focused tests that inject:

- terminating owned captain returning 503 inside the stage budget;
- owned member connection refusal while its endpoint is withdrawn;
- the same failures after the stage budget expires;
- the same failures with no active lifecycle operation;
- an unrelated member failure during target replacement;
- serving endpoints below the required minimum;
- quorum or captain state that is stale or contradictory;
- controller restart with the transient interval already in progress;
- recovery without duplicated Event or metric transition counts; and
- direct unplanned active-captain deletion with one initiating error,
  coalesced continuing observations, and one recovery outcome.

Live acceptance requires planned and unplanned active-captain replacement with
continuous workload evidence, one controller restart during the observation
window, and negative fault injection outside any owned lifecycle. The accepted
record must show no suppressed terminal failure, no unbounded Event/log loop,
one recovery outcome for the expected interval, unchanged fail-closed rollout
behavior, and exact final SHC/workload recovery.

## Idempotence and Recovery

Classification derives from API-backed lifecycle state plus current
Kubernetes/Splunk observations. Controller restart must reconstruct the same
interval without incrementing a transition counter or creating a duplicate
Event. If durable identity is missing or cannot be reconciled with current Pod
state, classification fails closed as unexpected.

## Artifacts and Notes

- Planning branch: `codex/shc-108-transient-observation-severity`.
- Source implementation: pending.
- Negative evidence: SHC-107 unplanned active-captain Operator log, SHA-256
  `e8ff1addb6338f96ecf918e90ecd47049795254dd7bca02a7a8125dbe803caca`.
- Related plan:
  [SHC107PersistentClientQualificationExecPlan.md](SHC107PersistentClientQualificationExecPlan.md).

## Interfaces and Dependencies

SHC-108 consumes existing lifecycle status, Pod deletion state, EndpointSlice
conditions, SHC member/captain observations, timeout configuration, structured
logging, Kubernetes Events, and Operator metrics. It must not issue a captain
transfer, delete a Pod, alter readiness, advance a lifecycle stage, or change
the accepted safety policy.
