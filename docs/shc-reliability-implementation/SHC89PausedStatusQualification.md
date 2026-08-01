# SHC-89 Paused-at-Creation Status Qualification

## Purpose

This record defines and proves the bounded SHC-89 behavior for a Splunk custom
resource that is submitted with the pause annotation already present. Pausing
must be a valid, quiet declarative state. The API object must immediately tell
an administrator that reconciliation is paused, while the Operator must avoid
creating Splunk workloads or repeatedly retrying a rejected status update.

This correction applies to the seven active v4 Splunk reconcilers:

- Standalone;
- LicenseManager;
- ClusterManager;
- MonitoringConsole;
- IndexerCluster;
- SearchHeadCluster; and
- IngestorCluster.

Queue and ObjectStorage API types expose pause annotations but do not have
active enterprise reconcilers in this source baseline. They are therefore not
claimed as live SHC-89 qualification targets.

## Result

Bounded OBS-001/OBS-008 and SHC-89 pass source and EKS qualification. Exact
Operator source `3e171673794c6bd9b570c7d94abd6bc9292ab147` passed the complete
local and Linux source gates. Its immutable Operator image then initialized a
valid `Pending/Paused` status for all seven active v4 resource kinds, wrote
that state once, created no managed workload, returned without a timer, and
produced no reconciliation error.

Removing the annotation from a LicenseManager and SearchHeadCluster caused
ordinary event-driven reconciliation to resume. The LicenseManager became
Ready, and the three-member SearchHeadCluster completed first formation with
three Ready members, three Service endpoints, all members Up, zero container
restarts, and successful direct search on every member.

This is bounded spike evidence. It does not declare every pause, deletion,
namespace-termination, or controller-restart scenario complete, and it does
not make a production-default recommendation.

## Pre-correction evidence

A disposable baseline namespace contained a LicenseManager and
SearchHeadCluster created with the pause annotation. The Operator created no
managed workload, but both custom resources retained empty required status
fields. CRD validation rejected each status update. Over approximately two
minutes, the scoped Operator log recorded 30 paused-status update failures and
30 controller-runtime Reconciler errors, recurring at roughly 30-second
intervals. The SearchHeadCluster had both an empty `phase` and an empty
`deployerPhase`; the LicenseManager had an empty `phase`.

The problem was not that pause created a workload. The problem was that a
valid user request could not be represented by a schema-valid status and was
therefore reported as repeated controller failure.

## Accepted status and reconciliation contract

For a new, non-deleting resource that is already paused, the Operator must:

- set every empty required phase field to `Pending`;
- set `status.observedGeneration` to the resource generation;
- set `Ready=False` with reason `ReplicasNotReady`;
- set `Progressing=False` with reason `PausedByAnnotation`;
- set `Paused=True` with reason `PausedByAnnotation`;
- set `Stalled=False` with reason `NotStalled`;
- persist the status only when its semantic value changed;
- create no StatefulSet, Pod, Service, PVC, or other managed Splunk resource;
- return without an error and without a periodic requeue timer; and
- resume ordinary reconciliation when removal of the annotation updates the
  custom resource.

SearchHeadCluster additionally requires an empty `deployerPhase` to become
`Pending`. An existing non-empty workload phase is preserved while pause is
reported, so pausing a previously Ready resource does not rewrite its observed
workload history to Pending.

## Source qualification

The isolated branch is `codex/shc-89-paused-status`. The source commit is
`3e171673794c6bd9b570c7d94abd6bc9292ab147`.

Test-first checks reproduced the prior failures: paused Pending and Updating
resources were reported as Progressing, and no shared paused-status
initialization behavior existed. The accepted focused tests cover:

- valid Pending status and current observed generation;
- SearchHeadCluster `deployerPhase` initialization;
- preservation of an existing non-empty phase;
- `Paused=True` and `Progressing=False/PausedByAnnotation` conditions;
- one semantic status change followed by an unchanged result;
- no reconcile error, `Requeue`, or `RequeueAfter`; and
- the paused-at-creation path in every active v4 Splunk controller.

The exact commit passed locally:

- `make test`: 41 suites, 157 specs, zero failures, 78.5 percent composite
  coverage; and
- `make build`, including generation, formatting, vetting, and binary build.

The same exact commit was checked out on the Linux vWorkstation and passed:

- `make test`: 41 suites, 157 specs, zero failures, 78.5 percent composite
  coverage; and
- `make build`.

The Linux environment reported a pre-existing Ginkgo CLI/package version
notice (`2.28.1` versus `2.32.0`). It did not fail or skip the test suites and
is retained as a build-environment note, not represented as an SHC product
failure.

## Immutable EKS inputs

- Date: 2026-08-01 UTC.
- EKS cluster:
  `arn:aws:eks:us-west-2:667741767953:cluster/vivek-spl-301372`.
- Disposable namespace: `shc89-paused-status`.
- Operator source: `3e171673794c6bd9b570c7d94abd6bc9292ab147`.
- Operator image tag:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc89-3e1716737`.
- Operator OCI index digest:
  `sha256:b83bbb97f89dca45e183e895e4be7e1d7bd11007f08babb41c4c94c97d18f145`.
- Linux/amd64 manifest:
  `sha256:ff1766db777a9211df4a4760819f78237159ad1c9bee74837470f7817268ce71`.
- Splunk runtime digest:
  `sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`.

The image was built and pushed from the Linux vWorkstation with the
repository's deterministic `make docker-buildx` target for `linux/amd64`.
Qualification pinned the Operator Deployment to the OCI digest and did not
change the retained SHC-85 workload.

## EKS paused-at-creation evidence

At `2026-08-01T08:30:31Z`, the seven resource kinds were submitted already
paused. Server-side dry-run validation passed before creation. Within eight
seconds, every resource reported:

- `phase: Pending`;
- `observedGeneration: 1`;
- `Ready=False/ReplicasNotReady`;
- `Progressing=False/PausedByAnnotation`;
- `Paused=True/PausedByAnnotation`; and
- `Stalled=False/NotStalled`.

The SearchHeadCluster also reported `deployerPhase: Pending`. The namespace
contained no StatefulSet, Pod, Service, or PVC. Apart from the Kubernetes
root-CA ConfigMap, the only namespaced object provided alongside the custom
resources was the user-supplied license Secret.

| Resource kind | First stable resourceVersion | After 45 seconds |
|---|---:|---:|
| Standalone | `10793688` | `10793688` |
| LicenseManager | `10793690` | `10793690` |
| ClusterManager | `10793692` | `10793692` |
| MonitoringConsole | `10793694` | `10793694` |
| IndexerCluster | `10793696` | `10793696` |
| SearchHeadCluster | `10793698` | `10793698` |
| IngestorCluster | `10793700` | `10793700` |

The resource versions for all seven custom resources remained unchanged
across a 45-second observation window. The scoped Operator log contained zero
entries for the namespace, zero paused-status errors, and zero Reconciler
errors. This proves a stable, quiet live state rather than a timer-driven or
error-driven loop; the focused source test separately proves that an identical
second preparation produces no semantic status change or write request.

## Annotation removal and normal recovery

The LicenseManager pause annotation was removed at
`2026-08-01T08:32:44Z`. Its status immediately changed to `Paused=False`, the
normal controller path created its StatefulSet and Pod, and the resource
became Ready at `08:36:05Z`. The Pod was `1/1` Ready with zero restarts.

The SearchHeadCluster pause annotation was removed at
`2026-08-01T08:36:21Z`. Its status immediately changed to `Paused=False`, and
normal reconciliation created the Deployer and three Search Head Pods.
Captaincy was observed moving dynamically during formation rather than being
assigned permanently to ordinal zero. At `08:48:36Z`, the final state was:

- `phase: Ready`, `deployerPhase: Ready`, and `readyReplicas: 3`;
- `Ready=True/AllReplicasReady`, `Progressing=False/Stable`,
  `Paused=False/NotPaused`, and `Stalled=False/NotStalled`;
- minimum peer and captain-member observations satisfied;
- initial formation stage `Complete`;
- three members `Up`, with captain status `Up` and restart state
  `NoRestart`;
- three Ready Pods and three Service endpoints; and
- zero container restarts.

A direct `makeresults` search succeeded independently on all three Search
Heads. The final check proves complete recovery after unpause; it does not
claim continuous client availability during first formation because no
previously formed Search Head service existed to preserve.

## Probe and event interpretation

The final namespace audit contained 12 Warning Event objects associated with
ordinary startup and the Splunk-controlled restart used during first
formation. They consisted of startup probe waits plus readiness and liveness
probe failures while individual management ports were intentionally
unavailable. Failure thresholds prevented kubelet restarts: every container
finished with restart count zero, the warnings stopped before the custom
resource reached Ready, and all three members and endpoints converged.

These events remain useful runtime evidence; they are not suppressed or
reclassified as successful probes. They also are not evidence of an SHC-89
pause-status failure, because they occurred only after the pause annotation
was removed and normal Splunk formation began.

## Cleanup

The five resources that remained paused were deleted without creating managed
workloads. The SearchHeadCluster and LicenseManager were then deleted and
their workloads and Services disappeared. All ten retained PVCs were
explicitly deleted under the disposable-test storage policy. The namespace
was removed, and a final cluster query found zero PVs whose claim reference
named `shc89-paused-status`.

The retained `shc85-lifecycle-hold` namespace was not modified and remained
Active with its SearchHeadCluster at `Ready/Ready`, `3/3` replicas.

## Remaining boundaries and follow-up

SHC-89 closes only schema-valid, quiet paused-at-creation behavior for the
seven active v4 Splunk reconcilers.

- SHC-90 remains responsible for stopping normal Apply work during the short
  namespace-termination-to-CR-deletion propagation window.
- SHC-91 records a separate deletion-order gap. LicenseManager and
  SearchHeadCluster already route deletion before pause, but Standalone,
  ClusterManager, MonitoringConsole, IndexerCluster, and IngestorCluster do
  not yet prove the same ordering. A paused existing resource with a finalizer
  must not let pause prevent deletion-safe finalization.
- Queue and ObjectStorage have no active enterprise reconcilers in this
  baseline, so no live pause/unpause behavior is claimed for them.
- Controller restart while resources remain paused and a full mixed-tier
  pause/unpause campaign remain useful additional qualification scenarios.

No Splunk Enterprise or Docker-Splunk correction is required for this bounded
status issue. Those layers begin work only after unpause allows the Operator
to create a workload.
