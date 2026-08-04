# Close persistent HEC connections when an indexer stops accepting work

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

Kubernetes stops selecting a terminating or unready indexer for new Service
connections. It cannot move an already-established TCP/TLS connection from
that Pod to a different backend. Current Splunk HEC correctly rejects new work
after indexer detention begins, but it returns HTTP 503 on the existing
HTTP/1.1 connection while advertising `Connection: Keep-Alive`.

SHC-109 defines the Splunk Enterprise behavior needed at this boundary: when
HEC reports that the server is shutting down and has not accepted the request,
the response must make that connection non-reusable. This lets a compliant
producer reconnect through the Kubernetes Service and select a serving peer.
The behavior must preserve accepted in-flight work and HEC acknowledgment
semantics. It must not turn an ambiguous transport failure into an unsafe
automatic replay.

This is a Splunk Enterprise requirement identified by Operator/Docker-Splunk
qualification. Splunkd production code is intentionally not changed in the
current Operator and Docker-Splunk workstream.

## Progress

- [x] (2026-08-04 01:43Z) Reproduced persistent HEC request loss during
  unplanned selected-indexer replacement with transport-only recovery.
- [x] (2026-08-04 01:46Z) Isolated the exact response as Splunk HEC code 23,
  HTTP 503, `Server is shutting down`, and `Connection: Keep-Alive`.
- [x] (2026-08-04 01:59Z) Proved the bounded compatibility behavior: close and
  retry one explicit rejection through the Service, with 600/600 accepted and
  exact final completeness.
- [x] (2026-08-04 03:41Z) Repeated the compatibility behavior across a
  complete Operator-owned indexer `3 -> 2 -> 1 -> 0` replacement. One
  explicit HTTP 503/code 23 rejection caused one response-aware reconnect;
  all 2,400 submissions were accepted and the final result on every Search
  Head was exact.
- [x] (2026-08-04 02:10Z) Traced the current behavior through local Splunk
  source without changing it.
- [ ] Review and own the product change with the Splunk HEC/indexer team.
- [ ] Implement server-side connection close with unit and integration tests
  in a separately authorized Splunkd workstream.
- [ ] Build an official Splunk artifact and repeat HTTP/HTTPS, HEC ACK,
  Kubernetes Service, mesh, and ingress qualification.

## Surprises & Discoveries

- Observation: Kubernetes endpoint withdrawal protects only new flows.
  Evidence: the selected indexer was withdrawn from its EndpointSlice, but the
  established client connection continued receiving responses from that Pod.
  Consequence: neither the Operator nor a readiness probe can migrate that
  socket.
- Observation: Splunk gives the client an explicit safe-to-retry rejection but
  leaves the stale connection reusable.
  Evidence: a direct diagnostic returned HEC code 23 and HTTP 503 with
  `Connection: Keep-Alive` repeatedly after shutdown began.
  Consequence: transport-only reconnect logic is not activated; the client can
  continue sending rejected requests to the same withdrawn backend.
- Observation: a one-response retry is not a general delivery guarantee.
  Evidence: the accepted qualification client recovered the explicit HTTP 503
  and finished exactly, but a connection loss before a response cannot prove
  whether Splunk accepted the event.
  Consequence: HEC acknowledgement or producer idempotency remains required
  for ambiguous failures.
- Observation: distributed-search partial results are independent of this HEC
  connection fix.
  Evidence: the response-aware campaign accepted all 600 HEC events while two
  HTTP-successful aggregate searches returned lower counts during indexer
  recovery.
  Consequence: OPS-011 immediate search completeness remains a separate open
  product contract.
- Observation: the response-aware workaround scales from one selected-indexer
  replacement to a full four-indexer roll, but the server defect remains.
  Evidence: the full accepted-image campaign used one persistent HEC
  connection until Splunk returned one HTTP 503/code 23 response with
  `Connection: Keep-Alive`. The client deliberately closed it, retried once
  through the Service, and completed 2,400 events exactly with zero HEC
  failure. There was no server-requested close.
  Consequence: this strengthens the bounded client-mitigation evidence; it
  does not qualify the required Splunkd behavior or prove that arbitrary HEC
  producers implement this Splunk-specific response rule.

## Decision Log

- Decision: require a server-requested connection close on the explicit
  shutdown rejection.
  Rationale: the server knows the request was not accepted and knows the
  connection is attached to an endpoint that should receive no new work.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: do not implement this behavior through an Operator probe, Service
  change, preStop command sequence, or Docker-Splunk wrapper.
  Rationale: those layers cannot safely change the HTTP lifetime of a socket
  already owned by Splunkd.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: preserve response-aware client retry as a compatibility rule, not
  as the product fix.
  Rationale: customers use different HEC producers; application-server
  connection semantics should not require every producer to discover a
  Splunk-specific stale-connection rule.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.

## Outcomes & Retrospective

The failure, exact server response, client mitigation, and product ownership
boundary are established. No Splunkd production change has been made. The
bounded mitigation avoided the observed loss in both a selected-indexer
replacement and a full `3 -> 2 -> 1 -> 0` roll, delivering 600 and 2,400
events exactly. SHC-109 remains open until an official Splunk build closes the
connection and passes the complete qualification matrix.

## Context and Orientation

Current local Splunk source establishes the following path:

1. indexer detention calls `CMSlave::slavePauseDataPorts()`;
2. that method calls `HttpInputServer::stopHEC()`;
3. `HttpInputServer` marks HEC disabled for detention;
4. a new transaction on an existing HEC connection selects
   `HttpInputShutDownHandler` when
   `rollingRestartReturnServerBusy` is enabled;
5. `HttpInputShutDownHandler::onAttach()` returns
   `HttpInputReply::ServerIsShuttingDown`;
6. that reply is code 23 with HTTP status 503; and
7. the handler does not mark the transaction as the final request, so the HTTP
   framework emits `Connection: Keep-Alive`.

The same HTTP framework exposes
`HttpServerTransaction::terminateConnectionAfterRequest()`, which marks the
response as the final transaction and renders `Connection: Close`. This is an
implementation fact for Splunk team review, not an instruction to change
Splunkd in the current workstream.

## Required Product Behavior

When HEC enters detention or shutdown rejection state:

- readiness/health for the HEC serving path becomes false before destructive
  process exit;
- already-accepted in-flight requests complete according to the supported HEC
  and ACK contract;
- each new request that is not accepted returns the existing unambiguous
  shutdown response;
- that response declares the current connection non-reusable and Splunkd
  closes it gracefully after the response is written;
- a pipelined request cannot be silently discarded or reported accepted;
- HEC HTTP and HTTPS modes behave consistently;
- leaving detention restores acceptance only after the endpoint is again
  actually serving; and
- metrics/logs distinguish accepted, explicitly rejected, drained, and
  shutdown-closed requests without credentials or unbounded labels.

The product contract must specify how code 23 interacts with HEC ACK channels,
whether an ACK query on an existing channel remains available during drain,
and how long accepted in-flight work may delay listener shutdown.

## Plan of Work

The Splunk-owned implementation should first add a focused HTTP transaction
test that demonstrates the current negative result: code 23/HTTP 503 and a
reusable connection. Change the shutdown handler through the supported HTTP
transaction API, then prove the response is fully written with
`Connection: Close` and the connection cannot accept a following request.

Add concurrency tests for an accepted in-flight request, a later rejected
request, HTTP pipelining, idle persistent connections, TLS, HEC ACK enabled and
disabled, detention enter/leave, repeated detention, and normal process
shutdown. Preserve existing response code/text compatibility unless the HEC
team explicitly versions that contract.

Build an official Splunk artifact. Use the existing Docker-Splunk packaging and
preStop path unchanged except for the artifact. The Operator must withdraw the
target endpoint first and retain the same durable one-target lifecycle. Run
the persistent client through the normal indexer Service and prove that the
server-requested close, not a client-specific status special case, advances
the connection generation to a serving backend.

## Validation and Acceptance

Source acceptance requires:

- an exact test for code 23 / HTTP 503 / `Connection: Close`;
- proof that the response body is complete before graceful socket close;
- rejection of a following request on the old connection;
- no loss or duplicate acceptance of an already in-flight request;
- explicit ACK-enabled and ACK-disabled behavior;
- HTTP and HTTPS coverage; and
- no regression in detention exit or ordinary non-clustered HEC service.

Kubernetes acceptance requires:

- identify the backend selected by one persistent Service connection;
- withdraw and terminate that exact Pod;
- observe a server-requested close and connection generation change;
- record zero HEC logical failures and exact acknowledged/idempotent final
  delivery;
- retain at least three serving endpoints in the four-indexer RF3/SF2 fixture;
- return the replacement to Cluster Manager Up/searchable and Service Ready;
- repeat for Operator-owned reverse-ordinal rollout and App Framework internal
  searchable restart;
- cover HTTP, HTTPS, no mesh, supported transparent mesh, ingress TLS
  termination/passthrough as applicable, and controller restart; and
- explain all Warning Events and Operator/runtime ERROR/FATAL records.

The acceptance verdict must keep immediate distributed-search completeness
separate. An exact final HEC result does not close OPS-011 if successful
searches remain silently partial.

## Idempotence and Recovery

Entering detention more than once must not create competing shutdown actors or
truncate a response. Leaving and re-entering detention must restore and close
connections according to current state. Process restart must not require
persisting individual connection state. If the product cannot determine
whether a request was accepted, it must not advertise the explicit safe-retry
shutdown result for that request.

## Artifacts and Notes

- Planning branch: `codex/shc-109-hec-shutdown-connection`.
- Splunkd implementation branch and official build: pending.
- Rejected Service-backed workload SHA-256:
  `4fbed017e6089abb7cb1840168e9104a509d5409a78d9bd9151258251eb14829`.
- Targeted code-23/HTTP-503 diagnostic SHA-256:
  `c1f8aae0b48cb69bfa5a14f3c61ba3b46b621b8d9db3f9a913a70d73db00dd11`.
- Response-aware bounded mitigation workload SHA-256:
  `35edcfab76356bdcc2c6adc64fa5a9d30429084b4c55a817d89000cbb2165c77`.
- Full indexer-roll compatibility workload SHA-256:
  `0a5a0193e402084533cc91c163602823f9d80b71010a3ce0158ef883090c6150`.
  It recorded one HEC response failure, one response-aware recovery, two HEC
  connections, zero HEC logical or transport-first-attempt failure, and exact
  2,400-event completion. The full-roll monitor SHA-256 is
  `0aa60e7d2a93104702bddb1a1dddeff83dce880c74fc7146560dea77f6cdbdd3`;
  its separate Search Head peer-convergence failure remains SHC-107/OPS-011
  evidence rather than SHC-109 acceptance.
- Related plan:
  [SHC107PersistentClientQualificationExecPlan.md](SHC107PersistentClientQualificationExecPlan.md).

## Interfaces and Dependencies

SHC-109 is owned by Splunk Enterprise HEC/indexer code. It depends on the
Splunk HTTP server transaction lifecycle, manual detention, HEC health,
in-flight/ACK handling, and the supported shutdown sequence. Docker-Splunk
initiates that supported sequence, and the Operator withdraws/routes Pods and
owns replacement ordering; neither layer can close an established application
socket on Splunkd's behalf.
