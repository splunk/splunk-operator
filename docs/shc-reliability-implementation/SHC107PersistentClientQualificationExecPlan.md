# Qualify persistent clients across SHC and indexer Pod replacement

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

The existing availability monitors start a new `curl` process for every HEC
submission and every distributed search. That proves repeated application
requests can resolve the current Kubernetes Service endpoints, but it does not
exercise a client whose TCP/TLS connection remains pinned to one backend Pod.
Kubernetes removes a terminating Pod from new Service endpoint selection; it
does not move an already-established TCP connection to another Pod.

SHC-107 adds an API-independent in-cluster client that reuses one HTTPS
connection to the indexer HEC Service and one to the Search Head Service. It
records the exact Search Head identity serving the connection, transport
interruptions, bounded reconnects, server-requested closes, logical request
failures, and final result completeness. This is qualification code only. It
does not change the Operator, Docker-Splunk, Splunk Ansible, or Splunk
Enterprise.

The stable scenario identifier for this requirement is `HLT-014` in
`SHCTestScenarioMatrix.md`.

## Progress

- [x] (2026-08-04 00:42Z) Created isolated qualification branch
  `codex/shc-107-persistent-client-qualification` from exact cumulative
  Operator source `a6cda92a3`.
- [x] (2026-08-04 00:42Z) Implemented deterministic source `9e9cbc819`: a
  standard-library Python client, digest-pinned Kubernetes Job, Make validation
  and deployment targets, seven focused unit tests, and fixture documentation.
- [x] (2026-08-04 01:06Z) Completed exact harness source `f3ec88026`. The
  follow-up commits expose HTTP version and `Connection` headers, request
  explicit keep-alive, and use a neutral qualification `User-Agent` so the
  current Splunk HTTP server permits standards-compliant HTTP/1.1 reuse.
- [x] (2026-08-04 01:08Z) Passed the focused tests 100 consecutive times,
  Kubernetes client-side manifest validation, `make fmt vet`, and
  `git diff --check`.
- [x] (2026-08-04 01:06Z) Passed the stable EKS smoke on the accepted Operator:
  12 HEC submissions used one connection, 25 Search Head identity/search
  requests used one connection, both paths returned HTTP/1.1
  `Connection: Keep-Alive`, no request failed, and all 12 unique events became
  searchable. The two earlier diagnostic runs correctly recorded Splunk's
  server-requested close behavior before the neutral user agent was added.
- [x] (2026-08-04 01:25Z) Passed an unplanned active-captain Pod replacement
  on the accepted Operator. Search Head 1 was the selected persistent backend
  and current captain. Endpoint withdrawal interrupted one transport attempt;
  the same logical search recovered once, connection generation advanced from
  one to two, Search Head 2 became the selected member and captain, and all
  600 HEC/search requests and unique events completed exactly.
- [ ] Run the client before an Operator-owned Search Head `2 -> 1 -> 0`
  replacement and prove the connection pinned to each replaced member either
  stays valid until supported shutdown or reconnects with no logical request
  loss.
- [ ] Run the same client before an Operator-owned indexer `3 -> 2 -> 1 -> 0`
  replacement and record HEC connection recovery plus distributed-search
  completeness.
- [ ] Repeat with Operator restart, no service mesh, supported service-mesh
  routing, and TLS termination at ingress. Add HTTP HEC coverage separately;
  the first bounded fixture uses HTTPS on the in-cluster Splunk ports.
- [ ] Run repeated and soak campaigns only after the bounded smoke and one
  controlled roll establish trustworthy connection evidence.

## Surprises & Discoveries

- Observation: the existing in-cluster and host-side monitors create a new
  client process for every request.
  Evidence: `shc85_incluster_workload.sh` and
  `shc82_appframework_monitor.sh` invoke `curl` independently inside each
  iteration.
  Consequence: their zero-request-failure records remain valid for short-lived
  requests but cannot be cited as persistent-connection qualification.
- Observation: a Service connection needs backend identity evidence, not only
  a Service DNS name.
  Evidence: kube-proxy or the cloud dataplane selects one EndpointSlice backend
  when the TCP flow is created, while the client-visible destination remains
  the Service address.
  Consequence: each SHC-107 sample calls authenticated `server/info` on the
  same connection and records the member name plus the identity and search
  connection generations.
- Observation: application servers may legitimately return
  `Connection: close`.
  Consequence: the harness records server closes and maximum requests per
  connection rather than falsely claiming persistence. A server-close result
  is a product behavior finding, not a Kubernetes failure.
- Observation: current Splunkd intentionally declines HTTP/1.1 reuse when the
  `User-Agent` is absent, identifies Python's default HTTP client, or starts
  with the official `splunk-sdk-python/` identifier.
  Evidence: the first stable run and an explicit-keep-alive run both received
  HTTP/1.1 `Connection: Close` for every HEC and Search Head response. The
  local Splunk source implementation in `RestHttpTcpConnection::trustHttp11`
  explicitly returns false for those client identities. The same EKS paths
  returned `Connection: Keep-Alive` and reused one connection after exact
  harness source `f3ec88026` sent `User-Agent: SOK-SHC-Qualification/1.0`.
  Consequence: the stable test proves Splunk and the in-cluster Service path
  support persistent HTTP/1.1 connections for a capable client. It also
  identifies a separate client-compatibility limitation: an official Python
  SDK client does not exercise persistence unless Splunk's compatibility
  policy or that client identity changes. No Splunkd change is made here.
- Observation: deleting the selected active-captain Pod produced one visible,
  recoverable transport boundary rather than silent Service migration.
  Evidence: deletion was requested at `01:15:07Z`; the endpoint became
  not-serving by the `01:15:08Z` monitor sample. Sequence 41 at `01:15:13Z`
  recorded one failed first attempt and one recovered request. Sequence 42
  identified Search Head 2 on connection generation two. The old Pod UID was
  replaced by `01:15:57Z`, the new Pod was serving by `01:17:32Z`, and the SHC
  was Ready with three endpoints and Search Head 2 as captain by `01:17:36Z`.
  Consequence: Kubernetes correctly stops new selection of the terminating
  endpoint, but a capable client must reconnect an existing flow. The bounded
  single retry was sufficient in this run; this is not a universal retry-time
  guarantee.
- Observation: the accepted Operator reported expected observations as errors
  while the known captain backend was absent.
  Evidence: from `01:15:15Z` through `01:16:48Z`, the Operator emitted 23
  `captain election failed` and 16 `unable to retrieve SearchHeadCluster
  member info` ERROR entries. They were 503 proxy failures or connection
  refusal to the terminating member. Availability and recovery still passed.
  Consequence: this is a supportability/severity gap, not a failed SHC-107
  availability result. A separate work item must distinguish an expected
  transient during observed Pod termination/election from a persistent or
  quorum-threatening controller error.

## Decision Log

- Decision: allow one reconnect inside a logical request and record the first
  failed attempt separately.
  Rationale: a production client must recover a connection whose backend exits,
  while evidence must not hide that the original transport was interrupted.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: do not trigger a rollout from the workload Job.
  Rationale: lifecycle orchestration, Kubernetes state evidence, and client
  traffic are separate concerns. The campaign owns the rollout and correlates
  its timestamps with the workload log.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: use the standard library in a digest-pinned existing client image.
  Rationale: the qualification must not depend on downloading packages at Job
  startup or rebuilding the Splunk runtime image.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: keep SHC-107 separate from the SHC-106 production image source.
  Rationale: a test harness must not change the immutable production candidate
  being qualified.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.

## Outcomes & Retrospective

The deterministic test harness, stable reuse, and unplanned active-captain
replacement behavior are qualified. The stable smoke reused one HEC and one
Search Head TLS connection with exact results. During replacement, all 600 HEC
writes stayed on one connection, the selected Search Head connection recorded
one failed first attempt and one recovered request, and the replacement
connection served the rest of the run. There were zero logical failures,
server closes, count regressions, or missing/duplicate final events. At least
two Search Head endpoints remained serving, the old captain was replaced by a
new Pod UID, and the SHC returned to three registered members with zero
container restarts. Operator-owned Search Head rollout, indexer replacement,
Operator restart, network variants, and soak remain open.

## Context and Orientation

The final qualification topology has a three-member SearchHeadCluster, a
four-peer IndexerCluster, and stable Kubernetes Services for Search Head
management/search traffic and indexer HEC. Search Heads advertise only members
whose Operator-owned serving condition is true. Indexers use the serving-aware
readiness profile in the lifecycle-enabled fixture.

EndpointSlice withdrawal affects new connection selection. Existing TCP flows
remain associated with their original backend until the peer, network path, or
client closes them. A robust result must therefore separate:

1. backend readiness and EndpointSlice membership;
2. the identity and generation of the already-open client connection;
3. the first transport attempt and any bounded reconnect;
4. the logical HEC or search result; and
5. eventual indexed-result completeness.

## Plan of Work

The stable smoke used exact harness source `f3ec88026`. It confirmed that
credentials are absent from logs, `server/info` identifies a real Search Head,
identity and search use the same connection generation, and both Services
carry multiple requests per connection. The two diagnostic forced-close runs
are retained as negative evidence and are not labeled persistent.

For Search Head qualification, start a longer unique run before the controlled
reverse-ordinal rollout. Record Job logs, EndpointSlices, Search Head Pod UIDs,
serving conditions, StatefulSet revisions/partition, lifecycle operation,
captain, and Kubernetes Events. Correlate the member named by the connection
with its withdrawal and replacement. Repeat for an active captain and a
non-captain if the first deterministic order does not cover both.

For indexer qualification, retain the same Search Head connection while the
indexer HEC connection is exposed to every reverse-ordinal replacement. Record
Indexer Pod identity, endpoints, lifecycle state, RF/SF/searchable health, HEC
transport recovery, every successful search count, regressions, maximum
pending sequences, and exact final convergence. Do not treat eventual
completeness as proof of immediate distributed-search completeness.

Repeat each bounded campaign with one Operator restart. Then qualify network
variants separately: no mesh, a supported transparent mesh, external TLS
termination with the configured ingress hostname/port, and HTTP versus HTTPS
HEC. Do not assume mesh availability or that TLS reaches the Splunk Pod when an
ingress terminates it.

## Validation and Acceptance

Acceptance requires:

- exact harness commit, client-image digest, cluster context, namespace, and
  production Operator/runtime digests are recorded;
- the stable smoke proves which paths actually reuse a connection;
- every Search Head identity sample is tied to the connection generation used
  by its distributed search;
- transport interruption, reconnect, server close, and logical failure counts
  are separately reported;
- a replaced selected backend never remains falsely advertised as serving;
- the client either completes the logical request on the original connection
  or reconnects within the bounded single retry;
- HEC and distributed-search logical request failures remain zero for the
  accepted planned-roll campaign;
- at least two Search Head and three indexer endpoints remain serving during
  the corresponding planned replacements;
- container restart counts remain zero unless the scenario explicitly targets
  container restart behavior;
- final numbered events are complete and unique; and
- Warning Events and Operator/runtime ERROR/FATAL logs are explained and
  contain no new lifecycle or credential-exposure defect.

HTTP server close behavior, search-count regression, or a failed first attempt
must remain visible even when the final exit code is successful. Any such
finding narrows the claim and may create a separate product requirement.

## Idempotence and Recovery

`make shc107-persistent-client` validates the fixture, updates its ConfigMap,
deletes only the prior SHC-107 Job, and recreates it in the selected namespace.
It does not mutate a Splunk Custom Resource or StatefulSet. A unique Job
hostname becomes the default indexed run ID. A failed Job can be inspected and
then safely recreated; deleting the Job and ConfigMap removes all SHC-107
objects without affecting Splunk Pods or persistent volumes.

## Artifacts and Notes

- Qualification source branch:
  `codex/shc-107-persistent-client-qualification`.
- Exact harness source: `f3ec88026bd316e56ff9cfcba46d5a547676cc14`.
- Production Operator source under test for disruptive correction campaigns:
  `a6cda92a3` after native image construction. The completed stable smoke used
  the accepted Operator image explicitly recorded below; it did not claim to
  exercise SHC-106.
- Fixture: `test/fixtures/shc-reliability/shc107_persistent_client.py`.
- Job: `test/fixtures/shc-reliability/shc107-incluster-workload-job.yaml`.
- Source gates: seven tests, 100 repeated runs, `make fmt vet`, client-side
  Kubernetes validation, and `git diff --check`. The final `fmt`/`vet` log has
  SHA-256 `3aa0e397494cfef2991968ee6320c9c3f380f4c3ea18e36dab967e9e2121e34b`.
- Stable EKS inputs: context `shc85-vivek-spl-301372`, namespace
  `shc-final-qualification`, accepted Operator image index
  `sha256:a9f2125097fa823d5182e8729683e5099116a889fdae8e892f0bd3110a8cdf3d`,
  Splunk runtime image index
  `sha256:49b12103f8444319dcf823eb829d2dfc020410e44d46273461c1b15e52c724fd`,
  and client image index
  `sha256:d6e11fe00dcadb6a3b168b23081950f85265daf0c923a314034160a495a6db4b`.
- Stable persistent result:
  `shc107-user-agent-persistent-smoke-accepted-operator-20260804T0106Z.log`,
  SHA-256
  `43f0c1da3d7797a6ae3ebf08a085bfeae16cb1c2e26cf90caf9c35253bca0447`.
- Unplanned active-captain replacement result:
  `shc107-unplanned-sh-replacement-accepted-operator-20260804T0114Z.log`,
  SHA-256
  `f67ac71a012e6913e149a0ab846e913bd36a7ad5108dcb321ca27c42389f0d07`.
  The 50-sample Pod/Endpoint/SHC monitor has SHA-256
  `c5a1ed90846123a4f2954db17cdbae00de0ece1593601d3591d5280260b8b8fa`;
  the Operator log has SHA-256
  `e8ff1addb6338f96ecf918e90ecd47049795254dd7bca02a7a8125dbe803caca`.
- Diagnostic forced-close results:
  `shc107-stable-smoke-accepted-operator-20260804T0058Z.log`, SHA-256
  `a29c2adc5d6226c5df2c918b03ae86e393175547c3fc76abaaad8e41de1be43f`,
  and `shc107-keepalive-smoke-accepted-operator-20260804T0102Z.log`, SHA-256
  `c06ac53715a7fb606f17e68ddc2ba47747c62eabf596f6bfc23a60a3df94b70d`.
- Operator-owned rollout, indexer, network-variant, controller-restart, and
  soak evidence: pending.

## Interfaces and Dependencies

SHC-107 consumes the indexer HEC endpoint, Search Head management/search
endpoint, the existing qualification credential Secret, and the client image's
Python 3 standard library. It observes but does not own StatefulSets,
EndpointSlices, lifecycle status, captaincy, App Framework state, or Kubernetes
Events. It changes no public API and adds no production runtime dependency.
