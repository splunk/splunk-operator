# Close every Splunk REST response in the Operator

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

The Operator's shared Splunk REST client returned from requests without
closing the response body. The SHC-112 peer-convergence gate increases the
number of bounded observation requests while an Indexer replacement waits for
every Search Head. Every successful, empty, malformed, and unexpected-status
response must release its HTTP resources deterministically so repeated
reconciliation does not accumulate response bodies, transports, sockets, or
file descriptors.

This correction is entirely in the Operator. It does not change a Splunk REST
endpoint, retry policy, request timeout, authentication, Docker-Splunk,
Splunk Ansible, or Splunk Enterprise behavior.

## Progress

- [x] (2026-08-04 05:49Z) Confirmed from source that the shared request path
  read response data but did not close the body on success, unexpected status,
  empty-body, malformed-body, or no-response-target paths.
- [x] Created isolated stacked branch
  `codex/shc-113-close-splunk-rest-response-bodies` from exact SHC-112 source
  `79f751075`.
- [x] Added deterministic response-body closure and table-driven coverage for
  successful JSON, successful no-target, unexpected status, empty response,
  and invalid JSON outcomes.
- [x] Exact source `961fe9b06` passed 100 focused repetitions, 20 race-enabled
  focused repetitions, `make fmt vet build`, all 43 Make test suites, all
  192 enterprise/controller specifications, 78.6 percent composite coverage,
  chart lint, all 150 Helm tests, and `git diff --check`.
- [ ] Build one immutable Linux Operator image from the exact source and run a
  bounded reconciliation soak while the SHC-112 gate polls multiple Search
  Heads.
- [ ] Compare Operator file-descriptor and established-connection counts
  before, during, and after the soak, and require them to return to a stable
  bound.

## Surprises & Discoveries

- Observation: the routine Splunk request client already has a five-second
  request timeout, but timeout and response ownership are independent.
  Consequence: the request duration was bounded while response resources were
  not explicitly released by the caller.
- Observation: the no-target success path returned before reading the body.
  Consequence: a single deferred close must be established immediately after
  a successful client call so every later return path is covered.
- Observation: this behavior predates SHC-112 and is shared by other Operator
  Splunk REST calls.
  Consequence: the fix belongs in the common client rather than only in the
  peer-convergence method.

## Decision Log

- Decision: close every non-error response in the shared request path.
  Rationale: ownership is established when the HTTP client returns a response;
  cleanup must not depend on status or decoding outcome.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: keep request timeouts, retries, and endpoint behavior unchanged.
  Rationale: response ownership is independently correct and can be qualified
  without changing network semantics.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: keep this correction on a separate stacked branch.
  Rationale: review can distinguish the common-client resource fix from the
  SHC-112 lifecycle gate.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.

## Outcomes & Retrospective

Source qualification is complete at `961fe9b06`. The close contract is
exercised on every return path relevant to successful HTTP responses and
passes the full native test and chart gates. Immutable Linux packaging and a
live repeated-observation resource soak remain open.

## Plan of Work

On the authorized Linux builder, build and push an immutable Operator image
from `961fe9b06` using the repository Make target. Record the source commit,
OCI index, platform manifest, build command, and generated CRD hash. Install
the generated CRD before the controller image.

Create or reuse a healthy four-indexer, three-Search-Head topology and trigger
an inert Indexer Pod-template update with the lifecycle features enabled.
Hold one Search Head observation unavailable for a bounded interval so the
Operator repeats the classified convergence wait without deleting another
Pod. Record the Operator process file-descriptor count, socket states, memory,
reconcile count, stage, and error Events at fixed intervals. Restore the
Search Head, require exact convergence, and continue sampling after recovery.

## Validation and Acceptance

Source acceptance requires:

- the body is closed after successful JSON decoding;
- the body is closed when no response target is supplied;
- the body is closed after unexpected status, empty body, or decode failure;
- focused tests pass repeatedly with and without the race detector;
- `make fmt vet build`, `make test`, chart lint, Helm tests, and
  `git diff --check` pass; and
- no generated source or schema drift remains.

Live acceptance additionally requires:

- an immutable Linux image tied to the exact source;
- the SHC-112 wait remains durable and no second Indexer is disrupted;
- response polling does not create monotonically increasing Operator file
  descriptors or established connections;
- counts return to their steady bound after peer convergence;
- no scoped Operator ERROR/FATAL log, resource-exhaustion Event, panic, or
  restart; and
- the final Indexer and Search Head inventories are exact and healthy.

## Idempotence and Recovery

The code change does not mutate Kubernetes or Splunk state. Repeated requests
close their own response independently. A failed live soak can be stopped
without changing persistent data. Rollback restores the prior Operator image;
the active lifecycle status must be inspected first so controller ownership is
not changed during an in-progress Pod replacement.

## Artifacts and Notes

- Source branch: `codex/shc-113-close-splunk-rest-response-bodies`.
- Parent source: `79f751075`.
- Exact source: `961fe9b06`.
- Native Make gate: 43 suites, 192/192 specs, 78.6 percent composite
  coverage.
- Focused repetitions: 100 normal and 20 race-enabled.
- Helm gate: chart lint and 150/150 tests.
- Immutable image and live soak evidence: pending.

## Interfaces and Dependencies

SHC-113 changes the shared Operator Splunk REST client used by SHC-112 and
other controllers. It depends on no Docker-Splunk or Splunk Enterprise source
change. Live qualification depends on the same Linux builder and EKS topology
as SHC-112, but its resource-ownership verdict remains separate from
distributed-search completeness and lifecycle sequencing.
