# Splunk Enterprise Indexer Rolling-Restart Serving Requirements

## Status and scope

This document records a Splunk Enterprise product boundary identified while
qualifying restart-required App Framework updates on Kubernetes. It is a
requirement record, not a claim that Splunk Enterprise has implemented the
behavior.

The near-term Operator work is intentionally limited to Splunk Operator and
the current Docker-Splunk runtime. No splunkd source change is part of the
SHC-85 Operator branch.

## Observed problem

On a supported four-peer indexer cluster with RF3/SF2, the Cluster Manager
started a searchable rolling restart after a valid bundle push. Splunk
preserved replication factor, search factor, and all-searchable state and
restarted one peer at a time. Kubernetes traffic availability did not follow
the same boundary:

- existing management-oriented readiness allowed 7 of 55 HEC submissions to
  fail;
- adding local HEC health with default readiness timing reduced the result to
  1 failure from 55;
- a 2-second, one-failure experiment completed 55 of 55 submissions exactly;
  and
- peer-level observation still found an unavailable HEC peer advertised for
  traffic and one transition where the next peer stopped serving before the
  previous peer was remotely serving again.

A later API-independent SHC-85 workload added another boundary during an
Operator-owned Pod replacement. HEC submissions and exported-search requests
continued to return success, but some successful aggregate searches
temporarily returned fewer of the already observed numbered events. At the
same timestamps, all Search Heads logged distributed-peer connection failures
to terminating or newly assigned indexer Pod IPs, including `No route to host`,
`Connection refused`, and authentication refresh failures. The export response
contained no partial-result message. Results later converged exactly.

The accepted record submitted 1,800 events with zero HEC request failures,
zero exported-search request failures, and final exact
`count/min/max/distinct=1800/1/1800/1800`. It nevertheless recorded 24
successful-search count regressions. The maximum sequence-to-count gap was 362
at sequence 1418, when the successful result contained 1,056 distinct events
with `min=1` and `max=1417`. The workload log has SHA-256
`8b14b210e1224219ee1509b150036c3f599c68f11bbf22b98cbdce71bf1e3faf`.

This is not evidence that RF or SF was configured incorrectly, and it is not
yet a complete root-cause finding. It proves that HTTP request success, Cluster
Manager `Up/searchable`, Kubernetes endpoint recovery, and remote HEC health
are insufficient by themselves to establish immediate distributed-search
result completeness during peer identity and address churn.

The complete evidence and limitations are in
[SHC82AppFrameworkIndexerQualification.md](SHC82AppFrameworkIndexerQualification.md).

## Current ownership boundary

An Operator-owned `OnDelete` replacement and a Splunk-managed bundle-push
restart are different workflows.

For an Operator-owned replacement, the Operator selects the exact Pod and can
wait for Kubernetes readiness, Cluster Manager status, EndpointSlice
publication, and a remote traffic-path check before selecting another target.
Search Heads do not dispatch distributed searches through that Kubernetes
Service. They retain Splunk-managed peer addresses and refresh connectivity as
the replacement receives a new Pod IP. The Operator's HEC serving check does
not prove that every Search Head has converged its distributed-peer view or
that a successful search is complete.

For a bundle-push restart, Splunk Enterprise owns the peer sequence inside the
Cluster Manager rolling-restart workflow. The Operator initiates or observes
the higher-level bundle operation but does not receive a supported per-peer
authorization point between Splunk's internal restarts. A Kubernetes readiness
probe can remove a non-serving peer from Service traffic, but it cannot tell
the Cluster Manager when it may advance to the next peer.

Therefore the Operator must not claim that its readiness or lifecycle gate
alone prevents previous-peer/next-peer serving overlap during an internally
managed Splunk rolling restart.

## Required Splunk Enterprise contract

Before Splunk Enterprise authorizes the next peer in a searchable rolling
restart, the previous peer must satisfy all applicable conditions:

1. The peer has completed its restart and has a new, stable splunkd process
   identity for that restart attempt.
2. The Cluster Manager reports the exact peer `Up` and searchable.
3. RF, SF, bucket primacy, and all-searchable preflight remain satisfied.
4. Every Search Head that can receive traffic has converged from the old peer
   address to the exact replacement identity and address. No unresolved
   distributed-peer connection or authentication failure remains for the
   replacement attempt.
5. A successful distributed search either includes all qualified searchable
   peers and complete results or explicitly reports that its result is partial.
   An HTTP-success response with silently incomplete results is not an
   availability success.
6. Every configured customer-serving path used by that peer has recovered.
   For HEC this includes the effective enabled/disabled state, HTTP or HTTPS,
   and configured port. For Splunk-to-Splunk ingestion it includes successful
   acceptance on the configured receiving port.
7. Recovery is observed from outside the peer process. A loopback-only result
   is not sufficient evidence of DNS, Pod networking, sidecar, or listener
   availability.
8. The recovery observation is bound to the exact peer restart attempt and
   cannot be reused after another process restart or identity change.
9. The observation remains successful for a qualified stability interval, not
   only one instantaneous sample.
10. Any conflicting peer failure, insufficient redundancy, or inconclusive
   recovery blocks the next restart and exposes a classified reason.

Splunk Enterprise can satisfy this contract internally or expose a supported
per-peer readiness callback/API that lets an external orchestrator provide the
remote serving result. The contract must not depend on a private endpoint or
an undocumented interpretation of logs.

## Required durable state and diagnostics

The rolling-restart operation must expose enough durable state to answer:

- the operation identity and bundle/revision being applied;
- current target and previous target;
- peer restart start and completion times;
- first `Up/searchable` time;
- first remote serving time and the traffic path checked;
- per-Search-Head old-address removal, replacement-address connection, and
  authentication convergence;
- partial-search status, excluded peer identities, and the reason each peer
  was excluded;
- stability-window start and completion;
- redundancy preflight result before each target;
- why advancement is waiting, blocked, cancelled, or failed; and
- whether a controller or Cluster Manager restart resumed the same operation.

Logs and metrics must use bounded reason and stage values. Peer names, bundle
identities, and detailed error text belong in structured logs or operation
status, not unbounded Prometheus labels.

## Failure behavior

The internal rolling restart must fail closed when:

- the previous peer is locally healthy but not remotely reachable;
- Kubernetes or an external observer reports contradictory serving state;
- another peer becomes unavailable;
- RF/SF/all-searchable health is lost;
- a traffic-eligible Search Head still targets an unavailable old peer address
  or cannot authenticate to the replacement;
- distributed search cannot prove complete participation and the response
  cannot explicitly surface partial-result status;
- the configured traffic protocol or port cannot be determined safely;
- a peer identity changes after serving recovery was recorded; or
- the Cluster Manager restarts without enough durable state to prove which
  peer was authorized.

Failing closed means that Splunk preserves the recovered peers, stops selecting
new targets, and reports the exact blocking stage. It does not mean forcefully
continuing, restarting all peers, or silently downgrading from searchable
restart.

## Qualification requirements

The contract is not complete until it passes:

- HEC over HTTP and HTTPS, plus HEC disabled with Splunk-to-Splunk ingestion;
- default and supported custom serving ports;
- no mesh and every supported mesh/mTLS mode;
- ingress TLS termination without using ingress policy for Pod-local protocol;
- persistent connections and stale EndpointSlice/client caches;
- one pre-existing unhealthy peer and insufficient RF/SF;
- Cluster Manager and Operator restart at every durable stage;
- concurrent app update, image rollout, scale, node drain, and manual Pod
  deletion;
- previous supported Splunk and Operator versions;
- continuously acknowledged ingestion with exact final completeness and no
  unreported duplication; and
- historical, real-time, active, and scheduled search behavior throughout the
  restart, including immediate count regression, maximum missing-event window,
  final exact completeness, and explicit partial-result reporting.

Until these requirements are implemented and qualified, searchable rolling
restart should remain enabled for its RF/SF/searchability protection, but it
must not be described as a complete Kubernetes traffic-availability guarantee.
