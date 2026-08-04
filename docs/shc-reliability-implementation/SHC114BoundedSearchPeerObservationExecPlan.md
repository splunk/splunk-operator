# Bound and cancel Search Head peer-convergence observations

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

SHC-112 waits for every managed Search Head to converge one replacement
Indexer identity before the Operator selects another Indexer. Its first
implementation queried Search Heads serially. Each request had a five-second
HTTP-client timeout, so an observation across `N` unavailable members could
occupy one reconcile worker for approximately `N * 5` seconds. The request was
also created without the reconcile context, so controller cancellation did
not stop it immediately.

SHC-114 keeps the SHC-112 convergence policy unchanged while making the
observation Kubernetes-controller-safe: run no more than four Search Head
requests concurrently, bound the complete batch to 15 seconds, propagate the
reconcile context through the HTTP request, evaluate results in deterministic
Pod-name order, and release every short-lived Search Head and Cluster Manager
transport after use.

This work does not change Splunk Enterprise, Docker-Splunk, Splunk Ansible,
the peer identity/address rule, the two-observation lifecycle rule, or the
classification of a failed Splunk observation as a durable wait.

## Progress

- [x] (2026-08-04 UTC) Re-audited exact cumulative source `c700a077e` from the
  perspective of controller cancellation and worst-case reconcile duration.
- [x] Created isolated stacked branch
  `codex/shc-114-bounded-peer-observation` from SHC-113.
- [x] Added context-aware GET and Cluster Manager/Search Head observation
  methods while retaining the existing background-context methods for
  compatibility.
- [x] Added a four-worker, 15-second Search Head observation batch and
  deterministic result evaluation.
- [x] Made the short-lived Cluster Manager info and peer wrappers propagate
  the reconcile context and close their idle connections.
- [x] Focused client, batch, convergence, cancellation, and concurrency tests
  passed 100 normal repetitions and 20 race-enabled repetitions.
- [x] `make manifests generate` and `make fmt vet build` passed with no
  generated schema drift.
- [x] (2026-08-04 UTC) Exact source `5440b8c2e` passed all 43 Make suites,
  192/192 enterprise/controller specs, 78.7 percent composite coverage,
  `make fmt vet build`, generation with no schema drift, chart lint, all 150
  Helm tests, and `git diff --check`.
- [ ] Build an immutable Linux image and run bounded-duration, cancellation,
  lifecycle, and socket/file-descriptor qualification on EKS.

## Surprises & Discoveries

- Observation: a five-second timeout was attached to each HTTP client, not to
  the complete all-Search-Head observation.
  Consequence: serial worst-case duration grew with SHC size even though each
  individual call was bounded.
- Observation: the shared REST `Get` path built requests without the
  reconcile context.
  Consequence: controller shutdown or reconcile cancellation had to wait for
  the HTTP-client timeout rather than canceling the in-flight request.
- Observation: the same reconcile obtains Cluster Manager info and peers from
  newly created clients.
  Consequence: the resource-ownership correction must cover those private
  transports as well as the new Search Head fan-out.
- Observation: unlimited parallel fan-out would reduce latency but multiply
  management-port load across controllers and clusters.
  Consequence: concurrency and total time both need explicit bounds.

## Decision Log

- Decision: use at most four concurrent Search Head observations and one
  15-second total batch deadline.
  Rationale: the normal three-member SHC completes in one wave, multiple
  clusters remain bounded, and a reconcile cannot grow linearly by five
  seconds for every unavailable member.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: construct clients in deterministic controller order, execute the
  independent reads concurrently, and evaluate stored results by Pod name.
  Rationale: concurrency must not make the reported first failing member or
  tests nondeterministic.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: retain failed Cluster Manager and Search Head observations as
  classified pending state.
  Rationale: timing and cancellation mechanics must not weaken SHC-112's
  fail-closed advancement rule.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: preserve existing context-free client methods as wrappers around
  the new context-aware variants.
  Rationale: unrelated callers keep source compatibility while controller
  paths can honor cancellation explicitly.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.

## Outcomes & Retrospective

Source qualification proves the batch concurrency ceiling, parent context
cancellation, request-context propagation, deterministic result shape, and
unchanged convergence behavior. Every exact full-source and chart gate passed;
immutable Linux-image and live EKS qualification remain open.

## Plan of Work

Complete generation, format, vet, build, full unit/controller, race, lint, and
Helm gates on the exact source. On the authorized Linux builder, create and
push one immutable cumulative Operator image and install its generated CRD
before deploying the controller.

During EKS qualification, capture reconcile start/end time, stage, operation
identity, target Pod UID, Operator goroutine/file-descriptor/socket counts, and
REST outcome for the normal three-member SHC. Then hold more Search Head
observations unavailable than the concurrency limit, require the batch to
remain within its 15-second budget, and prove that no later Indexer target is
selected. Replace the active Operator while requests are blocked and require
prompt cancellation plus exact durable-stage recovery by the new controller.
Restore access, require two current observations, and complete the full
reverse-ordinal roll with the SHC-112 workload and peer-order gates.

## Validation and Acceptance

Source acceptance requires:

- a caller-provided context reaches Search Head and Cluster Manager requests;
- parent cancellation ends blocked Search Head observations promptly;
- no more than four Search Head observations execute concurrently;
- the complete batch has a 15-second deadline independent of member count;
- all clients close response bodies and their private idle connections;
- results are evaluated in stable Pod-name order;
- missing, stale, disabled, `Down`, duplicate, unavailable, or canceled
  observations remain fail-closed waits; and
- `make manifests generate`, `make fmt vet build`, `make test`, focused race
  repetitions, chart lint, Helm tests, and `git diff --check` pass.

Live acceptance additionally requires:

- measured observation and reconcile durations remain within the documented
  budget under normal and unavailable-member cases;
- controller replacement cancels in-flight work and resumes the same durable
  operation without duplicate decommission or target selection;
- Operator file descriptors and sockets return to a stable bound after the
  wait and do not grow monotonically across reconciles;
- SHC-112 still blocks the next ordinal until every matching Search Head is
  exact and revalidated;
- minimum Kubernetes availability, PVC preservation, zero container restarts,
  final RF/SF health, workload integrity, and exact peer inventory pass; and
- Events and scoped Operator/runtime logs distinguish timeout, cancellation,
  remote observation failure, and successful convergence without credentials.

## Idempotence and Recovery

All added operations are read-only. Canceling or timing out a batch persists
no positive convergence proof and deletes no Pod. Reconciliation repeats from
the durable lifecycle status. Rolling the controller back is safe only after
inspecting the active operation; rollback must not be implemented by deleting
Splunk Pods or persistent volumes.

## Artifacts and Notes

- Parent source: `c700a077e`.
- Source branch: `codex/shc-114-bounded-peer-observation`.
- Exact source: `5440b8c2e6ceafc38bcd1c647317d27eebf295fd`.
- Concurrency limit: 4.
- Total Search Head observation timeout: 15 seconds.
- Focused repetitions: 100 normal and 20 race-enabled.
- Full source: 43 suites, 192/192 specs, 78.7 percent composite coverage.
- Helm: 18 Operator suites/60 tests and 12 Universal Forwarder suites/90
  tests.
- Immutable image and EKS evidence: pending.

## Interfaces and Dependencies

SHC-114 extends the shared Operator Splunk REST client with context-aware GET,
Cluster Manager, and Search Head observation methods. SHC-112 owns the durable
peer-convergence policy; SHC-113 owns response and transport cleanup. No
Docker-Splunk or Splunk Enterprise change is required. Live qualification
depends on the designated Linux builder and the existing EKS topology.
