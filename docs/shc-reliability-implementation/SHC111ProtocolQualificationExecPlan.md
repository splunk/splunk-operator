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
- [ ] Wait for every Search Head to converge to the four current peer
  identities after the setup transition.
- [ ] Run a stable persistent HTTP HEC smoke and prove the recorded scheme,
  connection reuse, and exact final event set.
- [ ] Trigger a same-protocol HTTP indexer `3 -> 2 -> 1 -> 0` replacement and
  record response-aware HEC recovery, distributed-search completeness,
  ordinal ordering, PVCs, endpoints, restarts, Events, and logs.
- [ ] Restore HTTPS through the same dependency-safe process after HTTP
  qualification unless the next retained campaign explicitly requires HTTP.
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

## Outcomes & Retrospective

The source and the four-peer HTTP setup transition are complete. Stable HTTP
reuse, a same-protocol disruptive roll, final Search Head peer convergence,
and HTTPS restoration remain in progress. Ingress and mesh qualification are
blocked by absent cluster capabilities, not treated as passed or failed.

## Plan of Work

After all Search Heads list exactly the four current `Up` indexer peers, run a
short stable smoke with `SHC107_HEC_SCHEME=http` and retain the start/end
records. Confirm more than one HEC request uses the same connection, no TLS
context is used, every request is accepted, and the final numbered set is
complete and unique.

Then start a longer response-aware workload before changing only an inert Pod
template annotation on the IndexerCluster. This creates a same-protocol Pod
replacement: both old and new backends remain HTTP. Run the existing
reverse-ordinal monitor in Pod-IP mode and keep the HEC and distributed-search
verdicts separate. Job `Complete` is not sufficient if count-regression or
peer-convergence guards fail.

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
- Persistent HTTP and same-protocol full-roll hashes: pending.

## Interfaces and Dependencies

SHC-111 extends the SHC-107 qualification harness. Docker-Splunk/Splunk
Ansible already owns the `SPLUNK_HEC_SSL` input used for the live variant. The
Operator owns Pod replacement and readiness withdrawal. Splunk Enterprise owns
HEC protocol behavior, the shutdown response, and Search Head distributed-peer
convergence. Ingress and mesh variants depend on a separately provisioned,
supported network topology.
