# Gate Operator-owned indexer advancement on Search Head peer convergence

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

An Indexer Pod can be Ready, published through Kubernetes, `Up/searchable` on
the Cluster Manager, and remotely serving HEC while Search Heads still retain
the prior Pod address. Selecting another Indexer during that interval can
compound distributed-peer churn and produce HTTP-successful searches that
contain fewer results than an earlier search.

SHC-112 adds the missing advancement boundary for Operator-owned Indexer Pod
replacement. Before completing one replacement and selecting the next
ordinal, the Operator waits until every SOK-managed Search Head that references
the same Cluster Manager reports exactly one current, enabled, `Up` entry for
the replacement peer GUID at the address published by the Cluster Manager.

This work does not change Splunk Enterprise peer-retention behavior, declare a
successful search complete, alter Splunkd, or control the peer sequence inside
a Splunk-managed searchable rolling restart.

## Progress

- [x] (2026-08-04 03:44Z) Preserved the accepted-image reproduction: the
  Operator selected ordinal 2 before ordinal 3 had converged on all Search
  Heads. At lifecycle `Completed`, every Search Head listed four current `Up`
  and four stale `Down` entries. Exact four-peer convergence followed 1,583
  seconds later.
- [x] (2026-08-04 04:50Z) Reproduced the same advancement violation while
  transitioning the retained cluster to HTTP HEC. The monitor reached exact
  four-peer convergence only after retaining the violation verdict. Evidence
  SHA-256 is
  `d2e19bf23572177c5707d0a2b2bfce3da596414e83a49b18bdc7ac7be38eeda9`.
- [x] (2026-08-04 04:57Z) Added the Splunk REST client observation and durable
  Indexer lifecycle gate on isolated source branch
  `codex/shc-112-indexer-search-peer-gate`. Cumulative source is
  `79f751075`.
- [x] Added exact, duplicate, wrong-address, `Down`, disabled, missing,
  unrelated-cluster, current/deprecated manager-reference, transient
  observation, durable two-observation, invalidation, and status-merge tests.
- [x] (2026-08-04 05:32Z) Exact cumulative source `79f751075` passed
  generation, `make fmt vet build`, the full 43-suite Make gate, all 192
  enterprise/controller specs, 78.6 percent composite coverage, a 100-run
  focused observation check, 20 race-enabled focused repetitions, all 150
  Operator and Universal Forwarder Helm tests, chart lint, and
  `git diff --check`.
- [x] Source-qualified multiple matching SearchHeadClusters, no matching
  SearchHeadCluster, one unreachable Search Head, a temporarily unavailable
  Cluster Manager observation, and a Kubernetes SearchHeadCluster discovery
  failure. Splunk observation failures remain classified waits; Kubernetes
  discovery failure remains a controller error.
- [x] Reconstructed a fresh controller manager from persisted CR status
  between convergence observation and revalidation. This restart boundary
  passed 100 focused repetitions, 20 race-enabled repetitions, and the full
  Make gate without relying on prior process memory.
- [ ] Build an immutable Linux Operator image and exercise a complete
  `3 -> 2 -> 1 -> 0` EKS replacement while a persistent client and independent
  monitor are running.
- [ ] Prove live Operator-Pod replacement while
  `AwaitingSearchPeerConvergence` is durable on the immutable EKS candidate.
- [ ] Repeat the multiple-SHC, no-matching-SHC, unreachable-Search-Head, and
  temporarily unavailable Cluster Manager variants on the immutable EKS
  candidate.
- [ ] Retain the separate Splunk Enterprise requirement for complete or
  explicitly partial distributed-search results.

## Surprises & Discoveries

- Observation: Kubernetes Pod readiness and EndpointSlice publication occur
  much earlier than distributed-peer convergence on Search Heads.
  Consequence: a readiness probe cannot supply this cluster-wide gate.
- Observation: Cluster Manager `register_search_address` and
  `host_port_pair` both reported the replacement Pod's current `IP:8089` in the
  retained deployment; the distributed-peer endpoint reported the same GUID
  at both old and new addresses during the stale interval.
  Consequence: address-only or GUID-only comparison is insufficient. The gate
  must require exactly one entry matching both identity and current address.
- Observation: a later failed observation cannot safely erase a persisted
  successful status field because the status merge guard rejects regressions
  and stale writers could restore old proof.
  Consequence: convergence invalidation is monotonic. A failure invalidates
  the last observation sequence, and two fresh successful reconciles are
  required before completion.
- Observation: the retained EKS cluster has neither an IngressClass nor a
  detected service-mesh control plane or sidecar.
  Consequence: candidate qualification can establish the direct no-mesh path
  only; ingress and mesh claims remain open.

## Decision Log

- Decision: gate only Operator-owned Indexer replacement in this work item.
  Rationale: the Operator owns the exact target boundary there. Splunk
  Enterprise owns the internal target sequence during a bundle-push searchable
  restart and needs a separate supported callback or internal gate.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: query every ordinal of every non-deleting SearchHeadCluster in the
  namespace that references the IndexerCluster's Cluster Manager.
  Rationale: a Kubernetes Service request can reach any traffic-eligible
  member, so captain-only or single-member evidence is insufficient.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: pass without a Search Head gate when no managed
  SearchHeadCluster references that Cluster Manager.
  Rationale: Indexer-only deployments must not acquire a dependency that does
  not exist.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: treat unavailable Splunk observations as classified pending state
  and Kubernetes list failures as controller errors.
  Rationale: both paths fail closed, but support must distinguish remote
  convergence from control-plane access.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.

## Outcomes & Retrospective

The accepted-image evidence proves the missing advancement boundary, and the
source-qualified candidate now represents it as a durable lifecycle stage
rather than an implicit readiness assumption. Live candidate acceptance
remains separate. The work cannot close immediate distributed-search
completeness or Splunk-managed restart sequencing.

## Plan of Work

First complete exact-source generation, format, vet, build, unit, controller,
and status-merge gates. Build and push the Operator only on the supported
Linux builder, record its immutable OCI index, and deploy the generated CRD
before the controller image.

On EKS, start the persistent HEC/search workload and independent peer monitor
before changing an inert Indexer Pod-template annotation. Prove that ordinal 3
reaches `AwaitingSearchPeerConvergence`, remains the only owned disruption
while stale peer entries exist, records a durable observation, revalidates it,
and only then selects ordinal 2. Repeat through ordinal 0. Replace the active
controller while the wait is persisted and require the same operation ID,
target UID, desired revision, stage, and convergence sequence afterward.

Then run explicit negative variants: duplicate same-GUID peer, one unreachable
Search Head, transient Cluster Manager observation failure, another unrelated
SearchHeadCluster, and no matching SearchHeadCluster. Keep HEC delivery,
search request success, search-count completeness, Kubernetes availability,
and peer-order verdicts separate.

## Validation and Acceptance

Source acceptance requires:

- generated deepcopy and CRD schema exactly match the API status fields;
- one current peer passes and duplicate, stale, missing, disabled, `Down`, or
  wrong-address identities fail;
- one failed observation invalidates prior proof monotonically;
- two current observations bound to the same replacement Pod UID are required;
- stale status writers cannot regress stage, observation, or invalidation
  sequences;
- transient Splunk observation failures persist a classified wait; and
- `make fmt vet build`, `make test`, and `git diff --check` pass.

Live acceptance additionally requires:

- reverse ordinal order with maximum one unavailable Indexer;
- no next ordinal selected while any matching Search Head retains the prior
  address for the replacement GUID;
- controller replacement resumes the exact durable wait;
- all PVC identities remain stable and all container restart counts remain
  zero;
- final Cluster Manager RF/SF/all-searchable health and exact peer inventory;
- exact final event completeness on every Search Head; and
- retained Events, stage durations, bounded transition metrics, and scoped
  Operator/runtime error audit.

## Idempotence and Recovery

All observations are read-only. Reconciliation repeats them after controller
restart, replacement Pod UID change, or transient REST failure. A current
observation is bound to the exact replacement UID and a monotonic sequence;
invalidation never decreases. The lifecycle remains fail closed without
deleting another Pod. Removing or rolling back the candidate Operator does not
alter Splunk configuration or persistent data, but an active operation must be
inspected before any rollback changes lifecycle ownership.

## Artifacts and Notes

- Source branch: `codex/shc-112-indexer-search-peer-gate`.
- REST client source: `63c2a5459`.
- Lifecycle gate source: `d7a6e6125`.
- Cumulative source, negative qualification, and reconstructed-manager test:
  `79f751075`.
- Accepted-image workload SHA-256:
  `0a5a0193e402084533cc91c163602823f9d80b71010a3ce0158ef883090c6150`.
- Accepted-image monitor SHA-256:
  `0aa60e7d2a93104702bddb1a1dddeff83dce880c74fc7146560dea77f6cdbdd3`.
- HTTP setup-transition monitor SHA-256:
  `d2e19bf23572177c5707d0a2b2bfce3da596414e83a49b18bdc7ac7be38eeda9`.
- EKS context: `shc85-vivek-spl-301372`.
- Candidate Operator image and live gate hashes: pending.

## Interfaces and Dependencies

The Operator reads the documented Splunk management endpoints for Cluster
Manager peers and Search Head distributed peers over the existing in-cluster
HTTPS management path. It uses the namespace-scoped admin credential already
required by reconciliation and does not log it. The gate depends on current
Cluster Manager and Search Head REST schemas and on SOK-managed
SearchHeadClusters being discoverable in the same namespace.

The current source does not qualify an HTTP management port, cross-namespace
references, external unmanaged Search Heads, ingress, or service mesh. Those
are explicit compatibility or topology gates, not inferred successes. Splunk
Enterprise still owns stale-peer removal, internal searchable-restart
sequencing, and complete-or-explicitly-partial search semantics.
