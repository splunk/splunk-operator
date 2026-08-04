# Qualify persistent HEC and search clients with explicit endpoint protocols

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

SHC-107 proved persistent-client behavior with HTTPS on the in-cluster Splunk
ports. That result must not be generalized to customers that configure HEC as
HTTP or reach a tier through a different supported TLS boundary. SHC-111 makes
the qualification endpoint's scheme and port explicit, preserves HTTPS as the
default, and runs the same connection/recovery evidence against the protocol
that Splunk actually serves.

This is qualification code only. It does not enable or disable HEC, terminate
TLS, create ingress, install a service mesh, or change Operator, Docker-Splunk,
Splunk Ansible, or Splunk Enterprise production behavior.

## Progress

- [x] (2026-08-04 04:00Z) Created isolated source branch
  `codex/shc-111-protocol-qualification` from exact SHC-107 source
  `3e9f47751e439f7a1de49633616ef995f950f111`.
- [x] (2026-08-04 04:00Z) Added explicit HEC and Search Head scheme/port
  configuration while retaining HTTPS/8088 and HTTPS/8089 defaults.
- [x] (2026-08-04 04:02Z) Passed 19 focused tests and client-side manifest
  validation 100 consecutive times, plus `make fmt vet` and
  `git diff --check`. Exact test source is
  `de6f5f8e3`.
- [x] (2026-08-04 04:21Z) Completed the setup transition of all four retained
  indexers from HTTPS HEC to HTTP HEC using the existing Docker-Splunk
  `SPLUNK_HEC_SSL=false` contract. The Operator replaced ordinals
  `3 -> 2 -> 1 -> 0`, retained at least three Ready peers, and recorded zero
  container restarts. This mixed-protocol transition is setup evidence, not a
  client-availability verdict.
- [x] (2026-08-04 04:50Z) Waited for every Search Head to converge to the
  four current peer identities after the setup transition. The accepted
  Operator selected the next ordinal before prior peer convergence; the
  monitor retained that violation and then observed exact convergence.
- [x] (2026-08-04 04:53Z) Completed a stable persistent HTTP HEC smoke. One
  HTTP connection carried all 100 accepted submissions, the HTTPS Search Head
  connection remained stable, no request failed or required recovery, and the
  final numbered event set was exactly `100/1/100/100`.
- [x] (2026-08-04 05:44Z) Completed a same-protocol HTTP indexer
  `3 -> 2 -> 1 -> 0` replacement. Ready Pods and endpoints never fell below
  three; all four Pod UIDs and IPs changed; every PVC claim remained stable;
  and container restarts remained zero.
- [x] The persistent client delivered exactly 2,400 unique numbered events
  with zero HEC, search-request, or Search Head identity failures. One HEC
  HTTP 503 response was retried on a second connection and recovered exactly.
  Three HTTP-successful searches regressed; the largest count drop was 933
  and peak pending was 935 before final exact convergence.
- [x] The independent monitor rejected ordinal 2 selection at
  `04:59:59Z` because ordinal 3 had not converged on every Search Head.
  Kubernetes lifecycle completed at `05:15:54Z`; every Search Head first
  reported exactly four current `Up` peers at `05:42:11Z`, 1,577 seconds
  later; and the monitor retained stable exact samples through `05:44:31Z`.
- [x] (2026-08-04 07:30Z) Restored all four retained indexers to HTTPS through
  the same dependency-safe `3 -> 2 -> 1 -> 0` process. Ready Pods and
  endpoints never fell below three, every Pod UID and IP changed, every PVC
  claim remained stable, all replacement containers retained zero restarts,
  and all four Ansible recaps reported `unreachable=0 failed=0`.
- [x] Verified effective `inputs.conf/[http]/enableSSL = 1` on every indexer.
  HTTPS HEC health returned 200 through the Service and each individual
  headless endpoint; plain HTTP was rejected with a connection reset.
- [x] The restoration monitor retained the expected sequencing rejection at
  `05:53:28Z`: ordinal 2 was selected before ordinal 3 had converged on every
  Search Head. Lifecycle completed at `06:09:19Z`; every Search Head first
  reported exactly the four current enabled `Up` peers at `06:43:05Z`, 2,026
  seconds later. Thirteen consecutive exact observations extended through
  `07:30:44Z` before the monitor returned the retained sequencing failure.
- [x] Final Cluster Manager RF, SF, site RF, and site SF were met; the SHC was
  Ready with three registered `Up` members and a ready captain on ordinal 1;
  the accepted Operator had zero restarts and zero scoped ERROR/FATAL matches.
- [x] Retained event snapshots contained the expected lifecycle Events and
  kubelet `Unhealthy` probe warnings while each replacement initialized; no
  other Warning reason appeared. The final standalone Events snapshot was
  empty after the long observation window, so the TSV snapshots are the
  authoritative event history.
- [ ] Qualify ingress TLS termination/passthrough and a supported transparent
  service mesh on a cluster that actually supplies those components.

## Surprises & Discoveries

- Observation: the qualification cluster has no ingress class and no detected
  service-mesh control plane or injected sidecar.
  Consequence: the cluster can prove no-mesh direct-Service HTTP/HTTPS behavior
  only. It cannot support an honest ingress or mesh claim.
- Observation: changing HEC from HTTPS to HTTP is itself a mixed-protocol
  rolling transition.
  Evidence: during the setup roll, new ordinal 3 returned HTTP 200 from the
  HEC health endpoint and no TLS handshake while ordinals 0 through 2 still
  served HTTPS.
  Consequence: one ClusterIP Service cannot present a uniform client protocol
  while its backends disagree. Do not classify the setup transition as an
  availability test. First converge the configuration, then perform a
  same-protocol replacement.
- Observation: the current readiness path admitted the HTTP-configured peer.
  Evidence: the replacement rendered `SPLUNK_HEC_SSL=false`, effective
  `inputs.conf/[http]/enableSSL = 0`, HTTP health 200, HTTPS failure, and then
  became Ready before the controller advanced.
  Consequence: readiness follows the effective HEC protocol in this bounded
  runtime. The full-roll test must still prove persistent client behavior and
  remote Search Head convergence independently.
- Observation: stable HTTP HEC reused one connection for all 100 requests and
  returned every numbered event exactly once.
  Consequence: HTTP itself is not a workaround for the Search Head
  distributed-peer convergence window. Protocol behavior and orchestration
  ordering require separate verdicts.
- Observation: the same-protocol HTTP roll reproduced the HTTPS campaign's
  response recovery, successful-search regressions, premature ordinal
  advancement, and approximately 26-minute post-lifecycle peer cleanup.
  Consequence: TLS on HEC is not the cause of the reliability gap. The
  accepted Operator's advancement boundary and Splunk's distributed-peer
  lifecycle remain the relevant layers.
- Observation: the HTTP-to-HTTPS restoration reproduced the same sequencing
  failure and required 2,026 seconds after Kubernetes lifecycle completion to
  remove the stale Search Head peer aliases.
  Consequence: restoring TLS changes the HEC transport but does not close the
  distributed-search convergence gap. The SHC-112 all-Search-Head gate remains
  required for candidate qualification.
- Observation: the monitor host paused between some post-convergence samples,
  so the final 13-sample result spans 47 minutes rather than representing a
  uniform five-second trace.
  Consequence: record this as 13 consecutive exact observations plus final
  direct health checks, not as uninterrupted five-second sampling.

## Decision Log

- Decision: keep HTTPS as the test harness default and require `http` or
  `https` explicitly for a variant.
  Rationale: qualification must not silently weaken transport or infer TLS
  behavior from a Service name.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: parameterize both HEC and Search Head endpoints even though the
  first live variant changes only HEC.
  Rationale: future ingress termination/passthrough tests need to describe the
  client-visible endpoint independently for each traffic path.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: do not treat the HTTPS-to-HTTP setup roll as protocol availability
  evidence.
  Rationale: mixed backend protocols make a single Service endpoint
  intrinsically ambiguous during that transition.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: accept the full HTTP run for eventual delivery and request
  availability, but reject it for immediate distributed-search completeness
  and safe lifecycle ordering.
  Rationale: every request succeeded and the final set was exact, while three
  successful searches returned materially incomplete results and the next
  ordinal started before prior peer convergence.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.

## Outcomes & Retrospective

The source, four-peer HTTP setup transition, exact post-transition peer
convergence, stable HTTP reuse, same-protocol disruptive roll, final exact
event and peer convergence, and dependency-safe HTTPS restoration are
complete. The retained topology is back on HTTPS and is healthy. The accepted
Operator again advanced before all Search Heads converged, so the restoration
passes availability and eventual-convergence checks but intentionally rejects
safe lifecycle ordering. Ingress and mesh qualification are blocked by absent
cluster capabilities, not treated as passed or failed.

## Plan of Work

The direct-Service HTTP/HTTPS work is complete. On a separately provisioned
topology, qualify ingress TLS termination and passthrough as distinct cases,
then qualify a supported transparent service mesh with and without injected
sidecars on the client and Splunk tiers. Preserve explicit client-visible
scheme/port settings and keep transport availability, Kubernetes readiness,
and distributed-search completeness as separate verdicts.

## Validation and Acceptance

Source acceptance requires default HTTPS compatibility, explicit plain-HTTP
construction without a TLS context, invalid-scheme rejection, positive port
validation, manifest wiring, repeated Make checks, format, vet, and a clean
diff.

Live HTTP acceptance requires:

- all four starting and replacement indexers have effective `enableSSL = 0`;
- the workload start record states `hecScheme=http` and the intended port;
- one persistent HEC connection carries multiple requests before disruption;
- explicit HEC shutdown rejection and transport failures remain separately
  counted;
- all numbered submissions are eventually complete and unique;
- every successful search-count regression remains visible and independently
  rejects immediate-completeness acceptance;
- at least three indexer Pods and endpoints remain Ready, PVC claims are
  preserved, and container restarts remain zero;
- no next ordinal is accepted before every Search Head has converged from the
  previous peer; and
- Events and Operator/runtime ERROR/FATAL logs are retained and explained.

HTTPS restoration acceptance additionally requires:

- all four replacement indexers have effective `enableSSL = 1` and return
  HTTPS HEC health 200 individually;
- plain HTTP fails rather than being silently accepted;
- replacement order, minimum availability, UID/IP changes, PVC preservation,
  Ansible results, and restart counts remain independently verified;
- Cluster Manager and SHC health are final and exact; and
- any lifecycle-ordering rejection remains visible even when eventual peer
  convergence succeeds.

## Idempotence and Recovery

The Make target recreates only the SHC-107 qualification ConfigMap and Job.
Protocol values are ConfigMap data consumed by the Job; they do not modify a
Splunk resource. The live Splunk protocol transition is separately explicit
in the IndexerCluster `extraEnv` and must be restored explicitly. A failed
client Job can be inspected and recreated without deleting a Splunk Pod or
persistent volume.

## Artifacts and Notes

- Source branch: `codex/shc-111-protocol-qualification`.
- Exact source: `de6f5f8e3`.
- EKS context: `shc85-vivek-spl-301372`.
- Namespace: `shc-final-qualification`.
- HTTP setup generation: 9.
- HTTP setup target revision:
  `splunk-shcfinal-idxc-indexer-57c5967877`.
- HTTP setup lifecycle completion: `2026-08-04T04:21:35Z`.
- HTTP setup-transition monitor SHA-256:
  `d2e19bf23572177c5707d0a2b2bfce3da596414e83a49b18bdc7ac7be38eeda9`.
- Stable HTTP workload log SHA-256:
  `9d04527bc6e511bdd2cc4054629626df5252d72ba9bf806e4d23bc06d6a91e77`.
- Same-protocol HTTP target revision:
  `splunk-shcfinal-idxc-indexer-7dc6b885b`.
- Same-protocol HTTP workload log SHA-256:
  `5803ed0d8d12d047e687d7157271ad352819bbd51f6732a611e7f81d776a4d19`.
- Same-protocol HTTP monitor SHA-256:
  `6805ef8491391667d9f3928d6b1543b11ebaf4a308e819dd89e6519324852f1f`.
- Same-protocol HTTP final Events SHA-256:
  `b42d4795d03b0a6b52c10ae2336c998fd65b4f2ab59fae3b755daf05f2d58e22`.
- Same-protocol HTTP effective configuration SHA-256:
  `15f6befec56c7a4576f28264443c66a7d332f4d80c2b9818a5fbb20c82965231`.
- HTTPS restoration generation: 11.
- HTTPS restoration target revision:
  `splunk-shcfinal-idxc-indexer-698cc59f86`.
- HTTPS restoration lifecycle completion: `2026-08-04T06:09:19Z`.
- First exact post-restoration Search Head peer convergence:
  `2026-08-04T06:43:05Z` (2,026 seconds after lifecycle completion).
- HTTPS restoration monitor SHA-256:
  `bc8ea042c3f7642089f50a27c6069962ba98dac1d83c908db8bf7fca075362f1`.
- HTTPS restoration final Events SHA-256:
  `37517e5f3dc66819f61f5a7bb8ace1921282415f10551d2defa5c3eb0985b570`.
- HTTPS restoration effective configuration SHA-256:
  `430c7a1e498a40eb5c11142ce584ddd50525db7ab33256333524b48195949b90`.
- Accepted Operator image index:
  `sha256:a9f2125097fa823d5182e8729683e5099116a889fdae8e892f0bd3110a8cdf3d`.

## Interfaces and Dependencies

SHC-111 extends the SHC-107 qualification harness. Docker-Splunk/Splunk
Ansible already owns the `SPLUNK_HEC_SSL` input used for the live variant. The
Operator owns Pod replacement and readiness withdrawal. Splunk Enterprise owns
HEC protocol behavior, the shutdown response, and Search Head distributed-peer
convergence. Ingress and mesh variants depend on a separately provisioned,
supported network topology.
