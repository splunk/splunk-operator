# Reconcile shared probe scripts without false lifecycle failures

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

Every Splunk custom resource in a namespace mounts the same namespace-scoped
probe ConfigMap. When a new Operator image contains updated health scripts,
several controllers can reconcile that one object at the same time. A normal
Kubernetes resource-version conflict must not become an ERROR log, a false
`GetIndexerStatefulSetFailed` Warning, or a temporary Error phase when another
controller has already applied the identical desired scripts.

After SHC-101, a controller that loses this expected concurrency race reads
the latest ConfigMap and retries. If the winning controller already wrote the
same scripts, reconciliation succeeds without another write. Persistent
conflicts and unrelated API failures remain visible.

## Progress

- [x] (2026-08-03 18:21Z) Reproduced the race by changing the immutable
  Operator image while four Splunk tiers in each of two namespaces shared one
  probe ConfigMap. Both ConfigMaps converged, but the losing License Manager
  and Indexer Cluster controllers logged six errors and emitted one
  `GetIndexerStatefulSetFailed` Warning per namespace.
- [x] (2026-08-03 18:23Z) Added latest-object conflict retry in
  `pkg/splunk/enterprise/configuration.go` and a client that injects one
  Kubernetes Conflict in
  `pkg/splunk/enterprise/configuration_test.go`.
- [x] (2026-08-03 18:24Z) Passed 20 conflict repetitions, 20 SHC-99 probe
  repetitions, the complete enterprise package, `make build`, and generated
  diff checks on macOS. Committed the correction as `0b56ec79b`.
- [x] (2026-08-03 18:29Z) Passed the exact native-Linux gate: 43 Make suites,
  192/192 enterprise/controller specs, 78.3 percent composite coverage, and
  `make build`; generation left the worktree clean.
- [x] (2026-08-03 18:32Z) Deployed immutable Operator index
  `sha256:0f2480b1e8e39d6e5a00e014df280c5aa3167abe5e498dd1deaac7399254f0f6`
  against the accepted probe hash in both namespaces. Both ConfigMaps changed
  to the final hash under concurrent reconciliation with zero Warning Events
  and zero controller ERROR/FATAL logs. All 20 managed Splunk Pods remained
  Ready with zero restarts.
- [x] (2026-08-03 18:36Z) Restored accepted Operator index
  `sha256:a9f2125097fa823d5182e8729683e5099116a889fdae8e892f0bd3110a8cdf3d`.
  Restoration also completed without Warning or controller error and both
  clusters retained complete healthy snapshots.

## Surprises & Discoveries

- Observation: the generic Warning named the Indexer StatefulSet even though
  that StatefulSet was present and healthy.
  Evidence: the underlying log error was `the object has been modified` on
  the shared probe ConfigMap. Both Indexer StatefulSets remained 4/4 Ready.
  Consequence: an error raised while rendering a StatefulSet must retain the
  causative resource in logs, and expected ConfigMap conflicts must be retried
  before a controller publishes a workload failure.
- Observation: the race is intermittent but reproducible across image
  changes.
  Evidence: one accepted-image restoration produced conflicts in both
  namespaces, while later runs sometimes selected one writer early enough to
  avoid a collision.
  Consequence: acceptance requires deterministic injected-conflict coverage
  plus a real multi-controller image-change run; one quiet rollout alone is
  insufficient.

## Decision Log

- Decision: use Kubernetes `RetryOnConflict` with a fresh `Get` for each
  attempt.
  Rationale: optimistic locking is the API contract being exercised. A fresh
  read distinguishes an identical successful winner from a real outstanding
  update without weakening other error handling.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: call the controller client update directly inside the retry.
  Rationale: the generic update helper logs every failed attempt as an error;
  conflicts inside a bounded retry are intermediate control flow, not an
  operational failure.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: scope SHC-101 to conflict-safe updates and defer ownership to a
  separate compatibility correction.
  Rationale: the initial SHC-101 implementation treated every existing probe
  ConfigMap as Operator-owned. Final review found that this contradicted the
  documented custom-probe contract. SHC-102 supersedes that assumption with
  explicit content-integrity ownership while retaining this conflict retry for
  unchanged Operator-generated defaults.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.

## Outcomes & Retrospective

SHC-101 is complete at source `0b56ec79b`. The original two-namespace race is
covered deterministically and was repeated on EKS through the same accepted-to-
candidate script transition. Probe data converged, no false StatefulSet
Warning or controller error appeared, and no Splunk Pod rolled or restarted.
SHC-102 subsequently restricted the update path to unchanged
Operator-generated defaults so the same retry does not overwrite custom probe
scripts. Neither correction introduces a CRD, Docker-Splunk, Splunk Ansible,
Splunk Enterprise, or persistent-data change.

## Context and Orientation

`pkg/splunk/enterprise/configuration.go` builds StatefulSets for all Splunk
roles. Its `getProbeConfigMap` function reads the three scripts packaged in the
Operator image and reconciles the ConfigMap named
`splunk-<namespace>-probe-configmap`. Because License Manager, Cluster Manager,
Indexer Cluster, Search Head Cluster, and other controllers call this shared
function independently, they may read the same resource version and then try
to update it concurrently.

Kubernetes returns a Conflict when an update uses a stale resource version.
This does not mean the desired change failed: another controller may already
have written the same data. The corrected function re-reads the object during
each retry, returns success immediately when its data equals the desired
scripts, and otherwise retries the update using the latest version.

## Plan of Work

Keep the production change inside `getProbeConfigMap`. Preserve the existing
early return when the first read already matches. When data differs, retain the
desired script map, use the client-go conflict retry policy, read the newest
ConfigMap inside the retry, and update only when the data still differs. Return
all non-conflict errors through the existing controller path.

In `pkg/splunk/enterprise/configuration_test.go`, wrap the standard mock client
with an update method that returns exactly one Kubernetes Conflict and then
delegates subsequent updates. Require two update attempts and require the
stored ConfigMap to contain the desired probe data.

## Validation and Acceptance

Source acceptance requires 20 injected-conflict repetitions, the existing
probe reconciliation test, the complete enterprise package, formatting, vet,
generation, `make build`, and complete `make test` on Linux. The worktree must
remain clean after generated checks.

EKS acceptance starts with both namespaces holding a different accepted probe
hash. Deploy the immutable candidate image, wait for both ConfigMaps and one
Pod-mounted projection to reach the candidate hash, and scope Events and
controller logs from the new controller Pod's creation time. Acceptance is
both ConfigMaps converged, no Warning Event, no ERROR/FATAL controller log,
all managed Pods Ready, aggregate restart count zero, and healthy Splunk peer
snapshots.

## Idempotence and Recovery

Every retry reads current Kubernetes state and writes the same deterministic
three-script map. Repeating reconciliation is safe. A persistent conflict or
API failure still returns an error after the bounded client-go retry policy.
The environment rollback is the accepted Operator digest; both tested
restorations preserved all workloads and persistent volumes.

## Artifacts and Notes

- Exact source: `0b56ec79b99cc4d58aa36eb1e8bb7f9ebf7e6932`.
- Linux manager SHA-256:
  `0064e7fd7372a59669be01ed7a906ec6e37cbe904e870571ddc9d255b3922758`.
- Candidate Operator OCI index:
  `sha256:0f2480b1e8e39d6e5a00e014df280c5aa3167abe5e498dd1deaac7399254f0f6`.
- Candidate probe SHA-256:
  `8faf8fac6bb133db53f4c6b9190495885f97bd494d9afd499d5e1b0a5fc98d66`.
- Accepted probe SHA-256:
  `300d907217442b8ac4198581c0a39791a9137292adedc244afe1bbf1d2eb1f26`.
- Candidate fresh/retained snapshot SHA-256:
  `2c5736f4aa527e431e364726155b84a8a42bf9e3e51173f9479b9ba87ac06f6`
  and `e05c08b2b6e68584cc0a77f075f844dccb3b1e2d43b4f0231613ff8b87bf22ab`.
- Accepted-restoration fresh/retained snapshot SHA-256:
  `2cbf54d6b7d5e7f192775aa641aa5bd48a58c8148acaa598f9b6778f9ab5fa5b`
  and `35ab757d3b5ef457c15bddedd012f02977240009e7be55d7239961989c309bd7`.

## Interfaces and Dependencies

The implementation uses `k8s.io/client-go/util/retry.RetryOnConflict` with the
existing controller-runtime `client.Client`. It changes no public API. After
SHC-102, this retry is reachable only for a ConfigMap whose recorded
Operator-content hash still matches its current data.

Revision note (2026-08-03 18:36Z): Created and completed this plan from the
conflict observed during SHC-99 final image qualification, including exact
source, Linux, immutable-image, live concurrency, and rollback evidence.

Revision note (2026-08-03 19:05Z): Recorded the SHC-102 ownership correction
after final review found that unconditional reconciliation contradicted the
supported custom-probe contract.
