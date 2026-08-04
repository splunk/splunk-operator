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
- [x] (2026-08-04 01:43Z) Rejected a transport-only persistent-client result
  during unplanned indexer replacement. The selected HEC connection remained
  pinned after EndpointSlice withdrawal and received HTTP 503 with
  `Connection: Keep-Alive`; 28 submissions were not accepted and two
  HTTP-successful searches returned lower aggregate counts.
- [x] (2026-08-04 01:46Z) Updated the qualification client to expose response
  status/code and to close and retry once only when HEC explicitly returns
  HTTP 503. Exact test-only source `d57db8d7a` passed 12 focused tests, 100
  repeated Make runs, `make fmt vet`, and `git diff --check`.
- [x] (2026-08-04 01:59Z) Passed a Service-backed unplanned replacement of the
  selected indexer with response-aware retry. One explicit shutdown rejection
  caused one bounded reconnect and one recovered logical request. All 600 HEC
  submissions were accepted and all 600 unique events became searchable.
  Two HTTP-successful aggregate searches still regressed while the indexer was
  recovering, so immediate distributed-search completeness remains open.
- [x] (2026-08-04 02:43Z) Completed the transport-only negative control across
  repeated Operator-owned Search Head `2 -> 1 -> 0` replacement. Ingestion was
  exact and complete at 1,800 events, but established connections remained
  pinned to detained members and 218 searches failed: 86 on ordinal 0, 111 on
  ordinal 1, and 21 on ordinal 2. Six later transport failures reconnected;
  they did not repair the earlier application-level detention responses.
- [x] (2026-08-04 02:49Z) Qualified response-aware Search Head recovery at
  exact source `3e9f47751` across two complete Operator-owned reverse-ordinal
  rolls. Four explicit HTTP 405 detention responses each caused the client to
  close the stale connection and retry once through the Service. All 1,200
  HEC submissions and distributed searches completed with zero logical,
  identity, or count-regression failure and exact final uniqueness.
- [x] (2026-08-04 02:49Z) Deleted the active Operator Pod during generation 18
  while ordinal 2 was in durable `WaitingForTermination`. The replacement
  controller retained operation ID
  `PodUpdate:splunk-shcfinal-shc-search-head-2:splunk-shcfinal-shc-search-head-7b8d4746b7:18`,
  completed `2 -> 1 -> 0`, and returned generation 18 to three Ready/Up
  members, three serving endpoints, a ready captain, and zero container
  restarts.
- [x] (2026-08-04 02:49Z) Qualified the bounded no-service-mesh path. The
  namespace had no mesh injection label, every Splunk and workload Pod had one
  container, and no Pod had sidecar status. The result does not claim a
  transparent-mesh or ingress path.
- [x] (2026-08-04 03:46Z) Completed an Operator-owned indexer
  `3 -> 2 -> 1 -> 0` replacement with exact source `3e9f47751`. One explicit
  HEC HTTP 503/code 23 response closed the selected connection and recovered
  once through the Service; all 2,400 submissions were accepted and became
  searchable. The persistent search connection returned HTTP 200 throughout
  but recorded three count regressions, maximum count drop 847 and maximum
  pending 849, so immediate distributed-search completeness failed.
- [x] (2026-08-04 03:46Z) Preserved the full-roll ordering failure. The
  controller selected ordinal 2 before every Search Head had converged from
  ordinal 3, and the monitor ended
  `FAIL-next-target-before-search-head-peer-3-converged`. The lifecycle
  completed at `03:17:48Z`; every Search Head first showed exactly the four
  current `Up` peers at `03:44:11Z`, 1,583 seconds later.
- [ ] Repeat with supported service-mesh routing and TLS termination at
  ingress. Add HTTP HEC coverage separately; the completed bounded fixture
  uses HTTPS on the in-cluster Splunk ports.
- [ ] Run a longer candidate-image soak after the bounded accepted-image
  campaigns. The completed 1,800-, 1,200-, and 2,400-sample runs establish
  trustworthy connection evidence but do not replace a release stability
  gate.

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
- Observation: EndpointSlice withdrawal does not migrate an established HEC
  connection.
  Evidence: during the rejected unplanned indexer campaign, the connection
  selected indexer 2 and remained open after Kubernetes withdrew that endpoint.
  Splunk returned HTTP 503 with `Connection: Keep-Alive`; the transport-only
  client did not reconnect and 28 numbered submissions were not accepted.
  Consequence: readiness protects new Service flows but cannot provide a
  delivery guarantee for an already-established TCP/TLS flow.
- Observation: the HEC shutdown response is intentional, but its connection
  lifetime is not Kubernetes-friendly.
  Evidence: the targeted indexer-0 diagnostic recorded HTTP 503 and Splunk HEC
  code 23, `Server is shutting down`, while the response advertised
  `Connection: Keep-Alive`. Local Splunk source shows manual detention calls
  `HttpInputServer::stopHEC()`, the HEC transaction selects
  `HttpInputShutDownHandler`, and that handler returns code 23 without marking
  the transaction as the final connection request.
  Consequence: register a Splunk Enterprise requirement to close the
  connection when returning the shutdown rejection. Until that product
  behavior is available, a capable producer can safely retry the explicit 503
  once through the Service because that response says the request was not
  accepted. Ambiguous transport failures still require HEC acknowledgement or
  producer idempotency; they must not be treated as equivalent to an explicit
  rejection.
- Observation: a successful distributed-search HTTP response is not proof of
  a complete result during indexer replacement.
  Evidence: in the accepted response-aware campaign, sequence 61 returned
  count 0 after sequence 60 returned 56; sequence 156 returned 96 after
  sequence 155 returned 154. The client received no HTTP failure. The final
  result converged exactly to 600 unique events.
  Consequence: the HEC recovery result passes, but OPS-011 immediate search
  completeness remains open. Splunk must either preserve the supported
  completeness contract or expose machine-detectable partial-result status so
  clients and qualification can distinguish an incomplete success.
- Observation: Search Head detention rejects work without closing the
  established HTTP connection.
  Evidence: during the transport-only planned-roll control, all three selected
  members continued returning application-level failure while their
  connections remained open. The run recorded 218 logical search failures but
  only six later transport failures. Local Splunk source returns HTTP 405 from
  `HandleJobsDataProvider` when the member is in detention and does not mark
  the transaction as the final request on that connection.
  Consequence: readiness and EndpointSlice withdrawal protect only new flows.
  Current clients need a bounded response-aware reconnect for this explicit
  rejection; Splunk Enterprise should close the connection after sending the
  complete detention response. This is a documented Splunkd requirement, not
  an Operator or Docker-Splunk production change in SHC-107.
- Observation: the bounded response-aware rule preserved logical work across
  planned Search Head replacement and controller loss.
  Evidence: exact test source `3e9f47751` treats only HTTP 405 as the Search
  Head detention response, preserves the first-response counter, closes the
  stale connection, and retries once. Across two complete reverse-ordinal
  rolls it recorded four first-response failures, four recovered requests,
  five total Search Head connections, zero first transport failures, zero
  logical search failures, and exact 1,200-event completion. The second roll
  continued through deletion of the active Operator Pod without changing its
  durable operation ID.
  Consequence: the explicit-response mitigation is effective for this bounded
  client and topology. It is not proof that every search client retries HTTP
  405, and it does not remove the product-side connection-lifetime gap.
- Observation: the accepted Operator again disrupted the Deployer and a
  Search Head concurrently when one common Pod-template annotation changed.
  Evidence: the Deployer received `Killing` at `02:38:37Z` while ordinal 2 was
  selected for the same generation and received `Killing` at `02:39:05Z`.
  The Search Head workload remained available through response-aware routing,
  but the overlap repeats the SHC-106 negative control. At final capture the
  Deployer Pod carried the update revision and was Ready while StatefulSet
  status still reported the prior `currentRevision`.
  Consequence: this accepted-image observation is not evidence that SHC-106 is
  fixed. Native candidate-image qualification must prove serialized ownership
  and exact StatefulSet convergence.
- Observation: response-aware HEC recovery remained effective across a full
  four-indexer replacement, but HTTP success did not make search results
  complete.
  Evidence: one explicit HTTP 503/code 23 response caused one bounded
  reconnect, after which 2,400 submissions completed exactly with zero HEC
  failure. The unchanged Search Head connection returned HTTP 200 for every
  search yet regressed at sequences 907, 1060, and 1107. The largest two
  drops were 847 events and maximum pending was 849; exact results recovered
  before the run ended.
  Consequence: the explicit HEC mitigation qualifies eventual delivery for
  this accepted-image campaign. It does not satisfy the independent
  immediate distributed-search completeness requirement.
- Observation: Indexer lifecycle `Completed`, Kubernetes readiness, and
  Cluster Manager serving recovery still precede cluster-wide Search Head
  peer convergence.
  Evidence: all four Pod UIDs and IPs changed in reverse ordinal order while
  their `etc` and `var` claims were preserved, at least three Pods and three
  endpoints remained Ready, and restarts stayed zero. Nevertheless, the
  controller selected ordinal 2 before ordinal 3 had converged on every
  Search Head. At final lifecycle completion all three Search Heads still
  listed eight peers: four current `Up` entries and four stale `Down` entries.
  Lifecycle finished at `03:17:48Z`; exact four-peer convergence first
  appeared on every Search Head at `03:44:11Z`, 1,583 seconds later.
  Consequence: a successful per-Pod readiness gate is necessary but not a safe
  ordinal-advancement gate. The Operator must observe every Search Head's
  distributed-peer view, or Splunk must supply an equivalent authoritative
  convergence signal, before selecting the next indexer.
- Observation: Cluster Manager factor checks protected the replication
  boundary but did not cover the distributed-search boundary.
  Evidence: the Cluster Manager endpoint remained available in all 241
  monitor samples. RF/SF/site factors reported zero in 11 samples during the
  four replacement windows, and the controller did not select the next
  ordinal until those factors recovered. The separate Search Head peer guard
  still failed.
  Consequence: keep the existing Cluster Manager safety checks and add the
  missing cluster-wide Search Head convergence gate; neither can replace the
  other.
- Observation: the qualification Job's Kubernetes `Complete` condition means
  exact final delivery, not uninterrupted search completeness.
  Evidence: the harness intentionally exits successfully when all numbered
  events are eventually complete and there are no logical HEC/search request
  failures; it reports count regressions independently. This run therefore
  succeeded as a Job while recording three regressions.
  Consequence: automation and release decisions must evaluate the regression
  counters and convergence monitor, not only Job success.

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
- Decision: retry only an explicit HEC HTTP 503 once and keep the response
  failure visible in counters.
  Rationale: code 23 states that Splunk did not accept the request, so retry is
  safe and should select a current Service endpoint after closing the stale
  connection. Transport loss after submission is ambiguous and is not covered
  by this rule.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: retry only the explicit Search Head detention HTTP 405 once and
  keep the first response visible.
  Rationale: current Splunk source rejects the search before dispatch and tells
  the caller to use another Search Head. Closing before the retry forces a new
  Service endpoint selection without treating unrelated 4xx responses as
  retryable.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.

## Outcomes & Retrospective

The deterministic harness, stable reuse, unplanned active-captain replacement,
one selected-indexer replacement, a full four-indexer replacement, two
Operator-owned Search Head rolls, controller replacement, and the no-mesh
topology are qualified for their bounded claims.
The Search Head campaign recovered one visible transport boundary and
completed 600 events exactly. The indexer campaign first proved that
transport-only retry loses explicit 503-rejected HEC requests on an established
connection. Exact source `d57db8d7a` then closed that stale connection and
retried the explicit rejection once through the Service: one response failure
was recovered, all 600 submissions were accepted, and the final result was
complete and unique. This is a client mitigation result, not a Splunkd fix.
Two silently incomplete successful searches during indexer recovery keep the
immediate distributed-search completeness requirement open. The Search Head
negative control proved that readiness does not move a persistent connection:
it delivered 1,800 events exactly but recorded 218 detention failures. Exact
source `3e9f47751` then recovered four explicit HTTP 405 responses across two
complete planned rolls, including an Operator restart, and completed 1,200
events with zero logical failure. The same source completed a full accepted-
image indexer roll with one recovered HEC 503 and exact 2,400-event delivery,
but three HTTP-successful search-count regressions and premature ordinal
advancement reject immediate-completeness and safe-convergence claims.
Service-mesh and ingress variants, HTTP HEC, candidate-image qualification,
and release-duration soak remain open.

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

HTTP server close behavior, explicit response rejection, search-count
regression, or a failed first attempt
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
- Exact current harness source:
  `3e9f47751e439f7a1de49633616ef995f950f111`. Stable reuse and the
  active-captain campaign used `f3ec88026bd316e56ff9cfcba46d5a547676cc14`.
- Production Operator source under test for disruptive correction campaigns:
  `a6cda92a3` after native image construction. The completed stable smoke used
  the accepted Operator image explicitly recorded below; it did not claim to
  exercise SHC-106.
- Fixture: `test/fixtures/shc-reliability/shc107_persistent_client.py`.
- Job: `test/fixtures/shc-reliability/shc107-incluster-workload-job.yaml`.
- Source gates: 15 tests, 100 repeated runs, `make fmt vet`, client-side
  Kubernetes validation, and `git diff --check`. The earlier `fmt`/`vet` log has
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
- Rejected transport-only indexer result:
  `shc107-unplanned-indexer-rejected-accepted-operator-20260804T0129Z.log`,
  SHA-256
  `4fbed017e6089abb7cb1840168e9104a509d5409a78d9bd9151258251eb14829`.
  Its rollout monitor has SHA-256
  `4f54fcdc61460ac6e8b937c1e52b4d7727e9469ee3e04e6e07dbdfffe1b0657e`.
- Targeted HTTP 503 diagnostic:
  `shc107-indexer0-targeted-503-diagnostic-20260804T0143Z.log`, SHA-256
  `c1f8aae0b48cb69bfa5a14f3c61ba3b46b621b8d9db3f9a913a70d73db00dd11`.
  The delete record has SHA-256
  `7c1b4170bab7d4bf88f74a3bd0c94e538d27ebcb8b691c4c0f582944f75c993d`.
- Response-aware selected-indexer result:
  `shc107-hec503-retry-workload-20260804T0148Z.log`, SHA-256
  `35edcfab76356bdcc2c6adc64fa5a9d30429084b4c55a817d89000cbb2165c77`.
  The selected-backend/delete record has SHA-256
  `aa9b9b0c439b9d020ca5aee7c5c5501bfff35083885d7cc84b230349f6aa66af`.
  The filtered Kubernetes Event window has SHA-256
  `a260b79155225f636c68bb6842eedf5e66a859cbd46ab4315e3e7888ea9cfe76`;
  the Operator log window has SHA-256
  `2f27170f296a9d447d02a711d301d38bfff4df6d8d962921fc8690de1144df59`.
  Indexer 3 changed from UID
  `4cb64f16-ca10-43fa-ba62-ff48ef754f17` to
  `a2ade4fe-bf36-4f46-8fb7-10507b0b70d0`, became Ready at
  `01:52:43Z`, and retained zero container restarts. The final IndexerCluster
  was Ready with four peers and four serving endpoints; the Operator window
  contained zero ERROR/FATAL entries. The four Warning Events were the
  expected readiness/startup probe failures while the target was shutting
  down or starting; they identify the intended serving-withdrawal interval
  and did not affect another Pod.
- Transport-only Operator-owned Search Head negative control:
  `shc107-operator-owned-sh-roll-negative-workload-20260804T0210Z.log`,
  SHA-256
  `f99a12d7cefa2638126f0bb868149c0b0df5e14d449a3dc81c4929ad21ea7111`.
  It submitted 1,800 events with zero HEC failure and exact final completeness,
  while 218 searches failed on detained members. Its 311-sample Pod,
  EndpointSlice, lifecycle, captain, and StatefulSet monitor has SHA-256
  `e40e3d339efa1c9f03f40ddf0848e1494d036eb8b87e837ea2d52d4fdde5e198`.
- Response-aware Operator-owned Search Head result:
  `shc107-response-aware-sh-roll-workload-20260804T0226Z.log`, SHA-256
  `ef141b12057a97e50d7eaddee59302b9ef6125a45a0fde542bccf6b68d9fe179`.
  It completed 1,200 events exactly with four explicit response failures,
  four recovered requests, five Search Head connections, and zero logical,
  identity, HEC, or count-regression failure. The final 184-sample rollout
  monitor has SHA-256
  `df5c1ff1d680e1fab99de4be5291d6e8830bbe7afe13dcbc0372d8ea3555568e`.
- Controller-restart record:
  `shc107-response-aware-controller-restart-delete-20260804T0238Z.log`.
  The active Operator UID changed from
  `cf64dd23-c574-42ea-8e52-be62df078131` to
  `d6569aa6-0e13-463f-af27-53ee6a1ddff6` while the ordinal-2 durable operation
  remained in progress. The replacement Operator log has SHA-256
  `126939467ff7f29f0a6bf39f009d49523160570e88cc0caaeb620384584c9873`.
- Final response-aware state records: SearchHeadCluster SHA-256
  `7ba25bf43601301e8d3e433aea88437a117f8081b8be9d9521edc307be4fae8d`,
  Search Head StatefulSet SHA-256
  `6c5f65ccc73fa61366f395d322d94714d10e533f44d9a11cd21038bce45f5657`,
  Pod snapshot SHA-256
  `20c835fac99448a2367a56a6491abbc014afcdc67dab5caf290650fce5f1f7b9d`,
  and EndpointSlice snapshot SHA-256
  `a4536026d58d08c91847d07357eba2dc1546104c0451a967a0ab9425e4d012fe`.
  Generation 18 was observed and Ready with all members `Up`, three serving
  endpoints, a ready captain, equal Search Head current/update revisions, and
  zero Search Head or Operator restarts.
- Operator-owned full indexer-roll result:
  `shc107-full-indexer-roll-workload-20260804T0257Z.log`, SHA-256
  `0a5a0193e402084533cc91c163602823f9d80b71010a3ce0158ef883090c6150`.
  It completed 2,400 submissions exactly with zero HEC, search, or identity
  failure; one explicit HEC response failure recovered through a second
  connection. It recorded three count regressions, maximum drop 847 and
  maximum pending 849. The 241-sample monitor has SHA-256
  `0aa60e7d2a93104702bddb1a1dddeff83dce880c74fc7146560dea77f6cdbdd3`
  and ended `FAIL-next-target-before-search-head-peer-3-converged`. Lifecycle
  completed at `03:17:48Z`; all three Search Heads first converged to exactly
  four current `Up` peers at `03:44:11Z`. The final direct result from each
  Search Head was `count/min/max/distinct=2400/1/2400/2400`; that record has
  SHA-256
  `9a0851182a142d3ff2af3b614dabf6af82cf4ec476d0c0f994a2f234e90fc2af`.
  Final IndexerCluster, Pod, EndpointSlice, StatefulSet, and Operator-log
  records have SHA-256 `97e156a08b1d3b1209af367e3a802215aa3e0c278fb15a2f8f2d39452136d30f`,
  `551ab1f6a7157346831ae8a0472ab339832943945c96be26c43235b5522e3df6`,
  `65d68a0f63a98e9731a569809bf1cfd6f311e6852c7f0de2b671c217ff5d7777`,
  `d02d97980c50267c82e98b7a37314e4951922032fbadeea2bb3a4a74245f309b`,
  and `4b859bf94fb4484ead0d7f34e9acbc8fd1a887c34d5b98bc81b9527ed4268adb`.
- Transparent-mesh, ingress TLS termination, HTTP HEC, candidate-image, and
  release-duration soak evidence: pending.

## Interfaces and Dependencies

SHC-107 consumes the indexer HEC endpoint, Search Head management/search
endpoint, the existing qualification credential Secret, and the client image's
Python 3 standard library. It observes but does not own StatefulSets,
EndpointSlices, lifecycle status, captaincy, App Framework state, or Kubernetes
Events. It changes no public API and adds no production runtime dependency.
