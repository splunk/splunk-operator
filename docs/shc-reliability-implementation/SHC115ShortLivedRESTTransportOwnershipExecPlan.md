# Bound every short-lived Splunk REST transport

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

SHC-113 made the shared Splunk REST client close every HTTP response body and
released the private transports created by SHC-112. SHC-114 extended that
ownership to its Cluster Manager observations. A repository-wide follow-up
found that older controller paths also create a new Splunk client for one or a
small number of requests and then discard it. Closing a response makes its
connection reusable, but a private Go transport can retain that idle socket
after its caller has returned.

SHC-115 makes the ownership rule consistent across current production callers:
Search Head lifecycle, captain, KV Store, detention, formation, and restart
operations; Indexer and Cluster Manager lifecycle/status operations;
Monitoring Console updates; license checks; bundle pushes; and telemetry all
release their privately owned idle connections. The client constructor also
sets a finite idle-connection lifetime as a defensive bound for future callers.

This changes no Splunk REST endpoint, request timeout, credentials, retry,
lifecycle state, or Kubernetes availability policy. It does not alter
Docker-Splunk, Splunk Ansible, or Splunk Enterprise.

## Progress

- [x] (2026-08-04 UTC) Audited all production `NewSplunkClient`, manager
  `newSplunkClient`, Search Head/Indexer `getClient`, Cluster Manager, Cluster
  Master, and Monitoring Console client call sites at cumulative source
  `5440b8c2e`.
- [x] Created isolated branch
  `codex/shc-115-short-lived-rest-transports` from SHC-114.
- [x] Added explicit transport release to every identified short-lived
  production caller and a positive 90-second idle-connection safety bound to
  new client transports.
- [x] Targeted Splunk client, enterprise, and telemetry tests passed; `make
  fmt vet` and `git diff --check` passed.
- [x] (2026-08-04 UTC) Exact source `cd3498393` passed `make build`, all 43
  Make suites, 192/192 enterprise/controller specs, 78.7 percent composite
  coverage, chart lint, all 150 Helm tests, and the changed-path race gate.
- [ ] Build the cumulative Linux image and qualify file-descriptor/socket
  behavior together with SHC-112 through SHC-114 on EKS.

## Surprises & Discoveries

- Observation: the client constructor created one private transport shared by
  its routine and long SHC-control HTTP clients, but the transport had no idle
  connection timeout.
  Consequence: losing the last application reference did not itself establish
  a bounded socket lifetime.
- Observation: the current controller architecture usually creates a fresh
  Splunk client at a wrapper or reconcile call site rather than retaining one
  shared client across reconciles.
  Consequence: ownership belongs to the constructing call site, and normal
  keep-alive must not be disabled globally to compensate for missing cleanup.
- Observation: the repeated SHC lifecycle paths are not the only constructors.
  License health, multisite discovery, bundle push, Monitoring Console update,
  secret synchronization, scale-down, upgrade validation, and telemetry also
  own short-lived transports.
  Consequence: a peer-gate-only resource soak could pass while unrelated
  reconciles continued to grow resource use.
- Observation: a package-wide race run reached an existing App Framework
  worker-scheduler race outside every changed file. The complete normal suite
  passed, and a race run limited to every changed client/lifecycle path passed.
  Consequence: SHC-115 records the unrelated broad race transparently without
  claiming it as a regression or silently weakening the changed-path gate.

## Decision Log

- Decision: release the transport explicitly at every current short-lived
  production ownership boundary.
  Rationale: deterministic cleanup is stronger than waiting for garbage
  collection or a fallback timer.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: retain keep-alive and the existing two-client shared-transport
  model within one Splunk client.
  Rationale: a caller that intentionally performs multiple requests can still
  reuse one connection until it releases the client.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.
- Decision: add a 90-second idle-connection bound in addition to explicit
  ownership cleanup.
  Rationale: a future caller omission must be bounded rather than permanent;
  this is a safety net, not the primary cleanup mechanism.
  Date/Author: 2026-08-04, Codex with Vivek Reddy.

## Outcomes & Retrospective

The source audit, full regression gate, and changed-path race gate establish a
complete current-caller inventory and preserve request behavior. Exact source
is frozen and present in both review repositories. Immutable-image and live
resource-soak qualification remain open.

## Plan of Work

Complete focused normal/race repetition, generation, format, vet, build, full
unit/controller, chart lint, Helm, and clean-diff gates. Freeze and push the
exact commit, then build one immutable cumulative Operator image on the
authorized Linux vWorkstation through repository Make targets.

On EKS, measure the controller process's file descriptors, sockets, goroutines,
and reconcile duration at a stable baseline; across ordinary steady-state
reconciles; while SHC-112 waits for peer convergence; during a complete Search
Head and Indexer lifecycle; after injected REST failures; and after active
controller replacement. Sample beyond the 90-second defensive bound and after
recovery. Require resources to return to a stable range with no monotonic
growth, then run the full availability, persistence, workload, and exact-peer
acceptance gates.

## Validation and Acceptance

Source acceptance requires:

- every current production short-lived Splunk client has an explicit release;
- successful and failed requests both reach the release boundary;
- clients performing two sequential operations remain open until the final
  operation;
- new transports have a positive finite idle-connection lifetime;
- default and long SHC-control HTTP clients continue to share one owned
  transport;
- endpoint, timeout, authentication, response, lifecycle, and retry semantics
  remain unchanged; and
- focused normal/race checks, all Make suites, build/vet/generation, chart lint,
  all Helm tests, and `git diff --check` pass.

Live acceptance additionally requires:

- file-descriptor and established/idle management-socket counts do not grow
  monotonically across repeated reconciles or lifecycle stages;
- counts return to an established stable range after failures and after the
  90-second fallback bound;
- controller replacement leaves no duplicate lifecycle action and resumes the
  exact durable operation;
- SHC-112 and SHC-114 advancement/cancellation gates still pass;
- minimum service endpoints, PVC identity, zero unexpected container restarts,
  exact peer convergence, RF/SF health, and workload integrity pass; and
- logs, Events, and metrics contain no credentials and distinguish remote
  observation failure from lifecycle progress.

## Idempotence and Recovery

Closing idle connections is local process cleanup; it performs no Splunk or
Kubernetes mutation and does not cancel an active request. Reconciliation may
repeat normally. The idle timeout is a fallback for an already-idle connection
only. Rollback must preserve and inspect any active durable lifecycle operation
before changing the controller image.

## Artifacts and Notes

- Parent source: `5440b8c2e`.
- Source branch: `codex/shc-115-short-lived-rest-transports`.
- Exact source: `cd3498393d2801d8498ca3dc9a2e20e4c30edcf8`.
- Full source: 43 suites, 192/192 specs, 78.7 percent composite coverage.
- Helm: 18 Operator suites/60 tests and 12 Universal Forwarder suites/90
  tests.
- Race: Splunk client and telemetry packages passed 20 repetitions; all
  changed enterprise lifecycle paths passed one race-enabled run.
- Defensive idle-connection timeout: 90 seconds.
- Immutable Operator image and EKS evidence: pending.

## Interfaces and Dependencies

SHC-115 extends the SHC-113 resource-ownership contract across existing
Operator callers. It is stacked on SHC-114 and must be included in the same
immutable EKS image used to qualify SHC-112 through SHC-114. No runtime image or
Splunk Enterprise source change is required.
