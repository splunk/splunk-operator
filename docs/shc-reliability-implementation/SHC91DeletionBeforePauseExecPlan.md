# Make deletion authoritative over pause and ordinary Apply work

This ExecPlan is a living record of SHC-91. It is retained with the completed
source and qualification evidence so a reviewer can reconstruct the problem,
the test-first boundary, the architectural correction, and the bounded result.

## Purpose / Big Picture

A pause annotation stops ordinary desired-state reconciliation. It must not
stop Kubernetes deletion. Once a paused Splunk custom resource has a deletion
timestamp, every declared finalizer must remain able to remove owned resources
and release the object. Successful finalization must also return before a
generic status write races an object that Kubernetes may already have removed.

SHC-91 makes that contract consistent across all seven active v4 Splunk tier
controllers. It corrects the five controller entry points that previously
honored pause before deletion: Standalone, ClusterManager, MonitoringConsole,
IndexerCluster, and IngestorCluster. LicenseManager and SearchHeadCluster were
already ordered correctly and remain positive controls.

The source audit also established that controller ordering alone was not a
complete boundary. Six real Apply entry points could perform normal
validation, configuration, dependency, or workload operations before reaching
their deletion blocks. SHC-91 therefore makes deletion first in Standalone,
ClusterManager, MonitoringConsole, both IndexerCluster manager compatibility
paths, and IngestorCluster Apply. LicenseManager and SearchHeadCluster already
had deletion-first Apply behavior.

## Progress

- [x] (2026-08-01 UTC) Created isolated branch
  `codex/shc-91-deletion-before-pause` from qualified SHC-90 documentation tip
  `56f819c8a`.
- [x] Audited all seven active v4 controller entry points and their real Apply
  functions.
- [x] Added seven-controller deletion/pause success and failure invariants.
  On unchanged source, the success invariant failed for exactly Standalone,
  ClusterManager, MonitoringConsole, IndexerCluster, and IngestorCluster;
  LicenseManager and SearchHeadCluster passed as controls.
- [x] Corrected the five affected controllers and committed that bounded layer
  as `86a0bc80a` (`fix: let paused resources complete deletion`).
- [x] Added deletion-first Apply tests. On the controller-only source, all six
  affected entry points failed before finalization because their ordinary paths
  rejected adversarial inputs or attempted unrelated work.
- [x] Moved existing finalizer behavior ahead of normal Apply work without
  changing the finalizer policy, public API, or generated schema. Committed as
  `a76c30e0c` (`fix: finalize tier deletion before normal apply`).
- [x] Passed final macOS and Linux source gates at exact source
  `a76c30e0c2395506cbfbb8d9e2643c186df0a3ef`.
- [x] Built and pushed an immutable linux/amd64 Operator image through the
  repository Make target and deployed it by OCI index digest.
- [x] Qualified active-namespace CR deletion and namespace-first deletion for
  all seven active v4 tiers on EKS without a manual pause or finalizer patch.
- [x] Qualified deletion of a real Ready Standalone, including its StatefulSet,
  Pod, two bound PVCs, and delete-reclaim PVs.
- [x] Verified natural cleanup and that retained SHC-85 workloads remained
  Ready with zero restarts.
- [x] Recorded the bounded evidence in
  `SHC91DeletionBeforePauseQualification.md` and the central work-item,
  scenario, qualification, and implementation indexes.

## Surprises & Discoveries

- The initial controller fixture was necessary but insufficient. Stubbing
  Apply proved that the controller no longer returned at pause, but hid the
  fact that several real Apply functions still performed normal work before
  finalization.
- The Apply-level test-first run exposed six failures through independent
  normal-path checks, including App Framework validation, license acceptance,
  and unresolved Queue/ObjectStorage dependencies. Those failures were useful
  because they proved deletion was not yet isolated from desired-state work.
- Successful and failed finalization need different status boundaries.
  Successful deletion returns immediately because the object may be gone.
  An Apply error remains on the existing condition/error path so the failure is
  observable and retryable.
- Pending PVCs are sufficient to exercise the real delete-PVC finalizer without
  starting all seven Splunk workloads. A separate Ready Standalone fixture
  proves the same path against actual StatefulSet, Pod, PVC, and PV objects.
- Kubernetes object deletion and storage reclamation are asynchronous. The
  Ready Standalone CR and StatefulSet were removed first, the Pod and PVCs
  followed, and the final delete-reclaim PV disappeared 73 seconds after the
  delete request. This is expected lifecycle sequencing, not a finalizer stall.

## Decision Log

- Decision: CR deletion is authoritative over pause for every active v4 Splunk
  tier.
  Rationale: pause controls ordinary reconciliation; finalizers are Kubernetes
  lifecycle obligations.
- Decision: deletion must be first at both controller and Apply boundaries.
  Rationale: either layer can otherwise prevent or delay finalization with
  unrelated validation, dependency, configuration, or workload work.
- Decision: reuse each tier's existing finalization implementation.
  Rationale: SHC-91 changes ordering, not deletion policy or storage ownership.
- Decision: return immediately only after successful deleting Apply.
  Rationale: this prevents a post-finalization status race while preserving the
  current observable error path when finalization fails.
- Decision: qualify all seven v4 tiers with lightweight adversarial fixtures
  and one real Ready workload.
  Rationale: the combination proves breadth of routing and depth of real
  Kubernetes cleanup without conflating this work item with full lifecycle
  qualification of every Splunk tier.

## Context and Orientation

Each active v4 controller in `internal/controller/enterprise` reads its custom
resource, applies lifecycle controls, invokes a tier-specific function in
`pkg/splunk/enterprise`, and may write generic status. A deletion timestamp is
set by Kubernetes after deletion is requested. A finalizer keeps the custom
resource present until required cleanup finishes. The supported pause
annotation asks the Operator to stop normal reconciliation and persist a
`Paused` condition.

SHC-90 handles the preceding namespace lifecycle interval: when the containing
Namespace is terminating but the custom resource does not yet have a deletion
timestamp, normal reconciliation stops. SHC-91 handles the next interval: once
the custom resource is deleting, it bypasses pause and ordinary work and runs
finalization.

The affected source boundaries are:

- five controller Reconcile entry points: Standalone, ClusterManager,
  MonitoringConsole, IndexerCluster, and IngestorCluster; and
- six Apply entry points: Standalone, ClusterManager, MonitoringConsole,
  IndexerCluster through both current ClusterManager and compatibility
  ClusterMaster paths, and IngestorCluster.

LicenseManager and SearchHeadCluster provide existing positive controls at both
layers. No v3 compatibility expansion is part of SHC-91.

## Plan of Work

First, add a controller invariant for each active v4 tier. A paused resource
with a deletion timestamp must call Apply exactly once. Successful Apply must
produce no status write; an injected Apply failure must retain the current
error/status path. Run it against unchanged source to record the affected set.

Second, align the five controller entry points with the existing
LicenseManager/SearchHeadCluster lifecycle ordering. Keep pause behavior
unchanged for resources that are not deleting.

Third, test the real Apply entry points with deleting resources that would fail
or perform unrelated work on the normal path. Extract or reposition only the
existing deletion logic so it runs before normal validation, configuration,
dependency, and workload processing. Preserve finalizer names, PVC policy,
ConfigMap cleanup behavior, and error reporting.

Finally, run complete source gates on macOS and Linux, build the exact Linux
commit through `make docker-buildx`, deploy by immutable digest, and exercise:

1. paused direct CR deletion for all seven tiers in an Active Namespace;
2. paused namespace-first deletion for all seven tiers; and
3. paused deletion of a real Ready Standalone with persistent storage.

Keep `shc85-lifecycle-hold` read-only and verify it after qualification.

## Validation and Acceptance

Source acceptance requires:

- all seven controller success cases call Apply once and write no status;
- all seven controller failure cases preserve the injected error path;
- all six affected real Apply entry points finalize before adversarial normal
  inputs can be validated or acted on;
- existing active-paused behavior remains unchanged;
- complete `make test`, `make build`, and `make helm-check` gates pass on both
  development hosts where run; and
- no CRD schema, public API, RBAC, probe, StatefulSet policy, Docker-Splunk,
  Splunk Enterprise, or persistent-data-format change is introduced.

Live acceptance requires:

- no manual removal of pause annotations or finalizers;
- all seven paused resources disappear in both direct and namespace-first
  deletion paths;
- declared PVC finalization runs for every fixture;
- no ordinary workload create, post-finalization status error, or Reconciler
  error appears in the scoped log audit;
- a real Ready workload and its bound storage disappear naturally; and
- the Operator and retained SHC-85 workloads remain Ready without restart.

## Completed Evidence

Final macOS source at `a76c30e0c` passed 42 suites, 185/185 enterprise
controller specs, 78.3 percent composite coverage, build, and 124 Helm tests.
Final Linux source at the same exact commit passed 42 suites, 185 controller
specs, 78.3 percent composite coverage, build, and 124 Helm tests.

The immutable Operator image is:

- tag:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc91-a76c30e0c`;
- OCI index:
  `sha256:4903f70a95b150c0a29bcd3ac70e063b5c55b6a030399a4636297586dea85cea`;
- linux/amd64 manifest:
  `sha256:6da77f0cdd1a4be2e2e8f6b9fa5f983f4a8824dab12942bb37f2df2cbd467008`.

On EKS, seven direct CR deletions completed in 5 seconds. Seven namespace-first
deletions completed with the Namespace absent at the 13-second sample. The
Ready Standalone deletion removed the CR and StatefulSet immediately, its Pod
and PVCs by the 49-second sample, and both PVs by the 73-second sample. Scoped
logs recorded the expected finalizer and PVC deletion operations and zero
status or Reconciler errors. All disposable namespaces and PV references were
absent afterward.

## Idempotence and Recovery

The source tests copy fake objects and restore Apply stubs with test cleanup.
The source change has no schema or migration. EKS attempts use distinct
disposable namespaces. A failed run must preserve Events, status, and logs
before cleanup; removing a finalizer is not an acceptable way to turn a failed
qualification into a pass. Rollback restores the prior immutable Operator
digest because no data conversion is required.

## Outcomes & Retrospective

SHC-91 is source-, image-, and EKS-qualified for the bounded current-v4
deletion-before-pause contract. The key architectural result is that lifecycle
ordering must be enforced twice: the controller must not return at pause, and
the real Apply path must not perform ordinary work before finalization.

This does not claim v3 behavior, all supported Kubernetes versions or
providers, namespace-scoped Helm behavior, every real Splunk tier, or broader
graceful Splunk shutdown semantics. Those remain separate work items and test
matrix boundaries.

## Artifacts and Notes

Starting history:

    56f819c8a docs: record SHC-90 qualification
    0c291c8c8 fix: handle namespace termination admission race
    7ce2483f7 fix: stop reconciliation in terminating namespaces

SHC-91 source history:

    86a0bc80a fix: let paused resources complete deletion
    a76c30e0c fix: finalize tier deletion before normal apply

Detailed replay inputs and bounded live evidence are in
`SHC91DeletionBeforePauseQualification.md`.

## Interfaces and Dependencies

No public interface changes. Reconciler signatures, CRD types, pause
annotations, finalizer names, RBAC, StatefulSet rendering, probes, and manager
configuration remain unchanged. The implementation uses only existing
deletion timestamps, status helpers, finalizers, Kubernetes clients, and tier
Apply paths.

Revision note, 2026-08-01 UTC: created the plan after auditing all seven active
v4 controller entry points.

Revision note, 2026-08-01 UTC: expanded the plan after real Apply tests proved
that controller-only ordering did not isolate finalization from ordinary tier
work.

Revision note, 2026-08-01 UTC: recorded final macOS/Linux gates, immutable image
identity, all-seven-tier direct and namespace-first deletion, real Ready
Standalone cleanup, retained-baseline health, and bounded completion.
