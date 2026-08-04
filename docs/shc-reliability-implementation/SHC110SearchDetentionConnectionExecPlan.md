# Close persistent Search Head connections after detention rejection

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

Search Head detention intentionally stops that member from accepting new
search jobs. Kubernetes withdraws the Pod from the Search Head Service so new
connections select another serving member. An HTTP/1.1 client whose management
connection was established before withdrawal remains attached to the detained
Pod, however. Current Splunk rejects each new search on that connection with
HTTP 405 while leaving the connection reusable.

SHC-110 defines the Splunk Enterprise behavior required for persistent
Kubernetes clients: when a Search Head refuses new search creation because it
is in detention, it must make the current connection non-reusable after the
complete refusal response is written. A compliant client can then reconnect
through the Service and follow Splunk's existing instruction to use another
Search Head.

This is a Splunk Enterprise requirement discovered by Operator qualification.
The current workstream changes only the test client so the behavior can be
measured and bounded; it does not change Splunkd production code.

## Progress

- [x] (2026-08-04 02:16Z) Reproduced repeated logical search failures on an
  established Service connection after its selected Search Head entered
  Operator-owned detention.
- [x] (2026-08-04 02:22Z) Traced the exact rejection to Splunk source and
  existing product messaging.
- [x] (2026-08-04 02:25Z) Added test-only response status/code evidence and one
  bounded reconnect for the explicit HTTP 405 response at exact source
  `3e9f47751`; 15 tests and 100 repeated Make checks passed.
- [x] (2026-08-04 02:43Z) Completed the transport-only negative control. The
  run delivered all 1,800 events exactly but recorded 218 logical search
  failures on persistent connections pinned to detained members: 86 on
  ordinal 0, 111 on ordinal 1, and 21 on ordinal 2.
- [x] (2026-08-04 02:49Z) Completed the response-aware EKS campaign across two
  full Operator-owned reverse-ordinal rolls. Four explicit HTTP 405 responses
  caused four bounded reconnects; all 1,200 HEC submissions and searches
  completed exactly with zero logical, identity, or count-regression failure.
  The second roll completed after the active Operator Pod was deleted while
  ordinal 2 was in durable `WaitingForTermination`.
- [ ] Review and own the server-side connection-close change with the Splunk
  search/SHC team.
- [ ] Implement and qualify the official Splunkd behavior in a separately
  authorized workstream.

## Surprises & Discoveries

- Observation: readiness withdrawal did not stop requests on the selected
  established connection.
  Evidence: during an Operator-owned `2 -> 1 -> 0` roll, the persistent client
  selected Search Head 1. After ordinal 1 was withdrawn and detained,
  `server/info` still identified Search Head 1 while new export-search requests
  failed repeatedly. Only a later transport break moved the connection to
  Search Head 0.
  Consequence: EndpointSlice state cannot migrate an existing TCP/TLS flow.
- Observation: the refusal is deliberate and tells the user to choose another
  member.
  Evidence: `HandleJobsDataProvider.cpp` checks SHC member detention before
  creating a new job and returns `HTTPSTATUS_METHOD_NOT_ALLOWED` (405) with
  message key `DISPATCH_JOBS:SEARCH_JOB_NOT_ALLOWED_IN_DETENTION`.
  `messages.conf.in` explains that the instance does not allow new search jobs
  during SHC rolling restart and directs the user to another Search Head.
  Consequence: keeping the same connection alive conflicts with the intended
  recovery action for Service clients.
- Observation: HTTP 405 must not be retried without endpoint and operation
  context.
  Evidence: 405 can also mean an unsupported method unrelated to detention.
  Consequence: the qualification mitigation retries once only on the known
  correct search-creation endpoint. A second 405 remains a logical failure and
  the first response remains visible in counters.
- Observation: retrying the exact detention response is an effective bounded
  client mitigation, but it is not equivalent to server-side closure.
  Evidence: source `3e9f47751` recorded four first-response failures, four
  recovered requests, five Search Head connections, zero first transport
  failures, and exact 1,200-event completion across two complete rolls. The
  response-aware rule explicitly closed the connection before every retry.
  Consequence: existing clients that do not recognize this endpoint-specific
  405 can continue failing until the socket breaks. SHC-110 remains a Splunkd
  requirement even though the qualification client passed.

## Decision Log

- Decision: require a server-requested connection close on detention refusal.
  Rationale: Splunk knows the job was not created and knows the connection is
  attached to a member that should receive no new searches.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: preserve the existing 405 and product message unless the search
  team explicitly versions the public contract.
  Rationale: connection lifetime can be corrected without unnecessarily
  changing the established REST response.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: keep this separate from captain transfer, Service readiness, and
  distributed-search partial-result work.
  Rationale: those mechanisms solve different availability boundaries and
  must retain independent acceptance evidence.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.

## Outcomes & Retrospective

The negative runtime behavior, exact source path, and bounded client behavior
are identified and live-qualified. The transport-only control produced 218
logical failures despite correct serving-endpoint withdrawal. The
response-aware client recovered four explicit detention responses and
completed 1,200 events exactly across two full rolls, including active
controller replacement. This proves a bounded mitigation, not closure of the
product requirement. Splunkd implementation, native product tests, HTTP and
HTTPS parity, search-type breadth, transparent mesh, and ingress qualification
remain open.

## Context and Orientation

The current source behavior is:

1. the Operator persists a target and withdraws its serving readiness;
2. Kubernetes removes that endpoint for new Service connections;
3. the Operator enables Search Head manual detention and drains/handles
   captaincy before replacement;
4. `HandleJobsDataProvider` receives a new search-creation request on an
   already-open management connection;
5. the provider checks `member_isInAnyKindDetention()`;
6. it returns HTTP 405 using the detention message without launching a search;
   and
7. the general HTTP server keeps the connection reusable unless the response
   transaction is explicitly marked final or the transport later fails.

Splunk's HTTP framework exposes a supported transaction operation for closing
the connection after a response. That implementation fact is for Splunk team
review; this plan does not authorize a current Splunkd change.

## Required Product Behavior

When a Search Head rejects new search creation because of detention:

- it does not create or partially dispatch the rejected search job;
- it returns the complete established refusal response;
- it marks the connection non-reusable and closes it gracefully after writing
  the response;
- an already-running search/status request follows the separately documented
  drain and status-query contract rather than being confused with a new job;
- leaving detention permits new jobs only when the member is again ready to
  serve;
- HTTP and HTTPS management paths behave consistently;
- the behavior remains valid with direct, Service, ingress, and supported mesh
  routing; and
- metrics/logs count accepted, detention-rejected, and connection-closed
  search creations without credential or high-cardinality labels.

The response must be machine-detectable without relying solely on localized or
customer-overridden message text. If the existing HTTP status is insufficient
for this contract, the search team should add a stable error identifier while
preserving compatibility.

## Plan of Work

Add a Splunk HTTP/REST test that opens one persistent connection, successfully
creates a search, enters manual detention, and attempts a second creation on
the same connection. The negative fixture must first prove the current 405 is
returned with a reusable connection. Update the detention refusal path through
the supported response transaction interface, then prove the complete 405
body is followed by graceful connection close and that no search was created.

Test status/control requests for existing jobs separately so the correction
does not interrupt supported drain observation. Test ad-hoc, export, real-time,
and scheduled-search creation paths to establish which share the same handler
and which need their own contract. Cover detention enter/leave, repeated
detention, captain and non-captain members, TLS, pipelining, and concurrent
requests.

Build an official artifact and retain the current Operator lifecycle and
Docker-Splunk shutdown behavior. Run the persistent Service client before a
complete `2 -> 1 -> 0` rollout, recording selected member, response status,
connection generation, endpoint state, target UID, captain, and exact workload
result. The server close should move the next request to a serving endpoint
without waiting for Pod process exit.

## Validation and Acceptance

Source acceptance requires:

- exact 405 response and stable machine-readable detention identity;
- `Connection: Close` and graceful socket closure after the body;
- proof that the rejected job was never created;
- no interruption of existing-job status/control required by drain;
- ad-hoc/export/real-time/scheduled path coverage as applicable;
- captain and non-captain behavior; and
- HTTP/HTTPS, detention exit, and non-SHC regression coverage.

Kubernetes acceptance requires:

- a persistent normal-Service connection with selected backend identity;
- complete Operator-owned reverse-ordinal rollout;
- response/connection evidence when the selected member is each target;
- zero logical search-creation failures after bounded reconnect;
- at least two serving Search Head endpoints throughout;
- supported captain transfer before active-captain replacement;
- all replacement members rejoined and synchronized with zero restarts;
- controller restart and no-mesh/mesh/ingress network variants; and
- exact final HEC delivery and representative ad-hoc, historical, real-time,
  and scheduled-search results.

This acceptance does not permit a silently partial successful search. OPS-011
and the distributed-search completeness oracle remain independently required.

## Idempotence and Recovery

Repeated detention must not create competing close operations or terminate a
response before it is complete. Controller or Pod restart needs no persistent
per-connection state. A client that reconnects to another detained member may
receive a second 405; qualification records that as a failure rather than an
unbounded retry loop.

## Artifacts and Notes

- Planning branch: `codex/shc-110-search-detention-connection`.
- Test-only response-aware source:
  `3e9f47751e439f7a1de49633616ef995f950f111`.
- First Operator-owned negative workload:
  `shc107-operator-owned-sh-roll-negative-workload-20260804T0210Z.log`,
  SHA-256
  `f99a12d7cefa2638126f0bb868149c0b0df5e14d449a3dc81c4929ad21ea7111`.
  Its 311-sample lifecycle/EndpointSlice monitor has SHA-256
  `e40e3d339efa1c9f03f40ddf0848e1494d036eb8b87e837ea2d52d4fdde5e198`.
- Response-aware workload:
  `shc107-response-aware-sh-roll-workload-20260804T0226Z.log`, SHA-256
  `ef141b12057a97e50d7eaddee59302b9ef6125a45a0fde542bccf6b68d9fe179`.
  Its final 184-sample monitor has SHA-256
  `df5c1ff1d680e1fab99de4be5291d6e8830bbe7afe13dcbc0372d8ea3555568e`.
- Final SearchHeadCluster state record SHA-256:
  `7ba25bf43601301e8d3e433aea88437a117f8081b8be9d9521edc307be4fae8d`.
  Generation 18 was Ready with three `Up` members, three serving endpoints, a
  ready captain, equal Search Head revisions, and zero restarts.
- Related plan:
  [SHC107PersistentClientQualificationExecPlan.md](SHC107PersistentClientQualificationExecPlan.md).

## Interfaces and Dependencies

SHC-110 is owned by Splunk Enterprise search dispatch and HTTP server code. It
depends on SHC detention state, new-search creation, existing-job handling,
localized REST messages, and connection lifecycle. The Operator owns target
selection, endpoint withdrawal, captain safety, and replacement order;
Docker-Splunk owns the supported container shutdown entry point. Neither layer
can safely close an established Splunkd management socket.
