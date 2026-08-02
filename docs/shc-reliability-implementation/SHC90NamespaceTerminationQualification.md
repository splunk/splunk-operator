# SHC-90 Namespace Termination Guard Qualification

## Scope and current result

SHC-90 addresses the interval in which Kubernetes has marked a Namespace for
deletion but has not yet placed deletion timestamps on every contained Splunk
custom resource. During that interval, ordinary reconciliation must not create
or update managed content. Once the custom resource itself is deleting, its
existing finalizer path must remain reachable.

Source qualification is complete on macOS and Linux at exact tip
`0c291c8c8`. The immutable linux/amd64 image is deployed on EKS and the live
namespace-termination race, guard, finalizer, and cleanup paths passed. The
disposable Namespace completed naturally with no remaining resource, PVC, PV,
or PV claim reference and without a manual finalizer patch.

The bounded source scope is the seven active v4 Splunk tier controllers:
Standalone, LicenseManager, ClusterManager, MonitoringConsole,
IndexerCluster, SearchHeadCluster, and IngestorCluster. Legacy v3
ClusterMaster/LicenseMaster, Telemetry, and Postgres controllers are not
claimed by this work item. SHC-91 separately owns deletion-before-pause
ordering in five v4 controllers and was subsequently qualified at source
`a76c30e0c`; no part of that correction is attributed to SHC-90. SHC-92
separately owns the existing Helm `namespaceOverride` and watch-target
compatibility decision and was subsequently qualified at source `91f742b52`.

No Splunk Enterprise, Docker-Splunk, Ansible, CRD schema, StatefulSet,
container lifecycle, probe, or persistent-data-format change is part of
SHC-90.

## Observed failure before SHC-90

The SHC-87 cleanup established the real failure signature. Namespace deletion
started before LicenseManager and SearchHeadCluster deletion timestamps became
visible. Their ordinary Apply paths attempted ConfigMap creation after the
Namespace had started terminating. Kubernetes rejected those creates and the
bounded Operator log recorded six LicenseManager and nine SearchHeadCluster
Reconciler errors. Existing finalization later completed naturally; all ten
PVCs and ten delete-reclaim PVs disappeared.

That evidence proves two different lifecycle intervals:

1. Namespace terminating, CR not yet deleting: normal Apply must stop.
2. CR deleting: Apply/finalization must continue.

The earlier SHC-86 and SHC-87 results remain valid for their exact fixtures,
but neither closed the first interval across the controllers.

## Source contract

The source tip contains two separately reviewable commits:

- `7ce2483f7`: authoritative Namespace guard, direct-read manager
  configuration, deletion-transition event acceptance, generated and Helm
  RBAC, and seven-controller test coverage; and
- `0c291c8c8`: typed cancellation for the unavoidable race in which Namespace
  termination begins after the preflight GET but before a create reaches API
  admission.

For a custom resource without a deletion timestamp, the controller performs a
live Namespace GET immediately before pause or ordinary Apply. Namespace reads
bypass the controller cache. A deletion timestamp, `Terminating` phase, or
NotFound result stops reconciliation with an empty successful result and no
status, Event, Apply, or timer. Other Namespace read failures stop mutation
and remain ordinary reconcile errors.

Kubernetes cannot make a client-side GET and a later create atomic. If the
Namespace changes in that interval, NamespaceLifecycle admission returns a
Forbidden status with the standard `NamespaceTerminating` cause. SHC-90 treats
only that typed cause as expected cancellation before generic condition/error
handling. It does not use message matching and does not suppress unrelated
RBAC, policy, transport, or validation failures.

When a CR receives its deletion timestamp, the controller event filter now
accepts that lifecycle-only update even though Kubernetes does not increment
`metadata.generation`. The reconcile bypasses the Namespace preflight stop and
reaches the existing Apply/finalizer path. The separate SHC-91 ordering
boundary was subsequently corrected at both the affected controller and real
Apply entry points at source `a76c30e0c`; its evidence is recorded in
`SHC91DeletionBeforePauseQualification.md`.

## Permission contract

The manager service account gains only `get` on the cluster-scoped Namespace
resource. It does not gain Namespace list, watch, create, update, patch, or
delete.

Cluster-wide Helm mode adds that verb to the manager ClusterRole.
Namespace-scoped Helm mode cannot use its namespaced Role for a cluster-scoped
resource, so it renders a supplemental ClusterRole restricted by
`resourceNames` to `.Release.Namespace` and binds the actual Operator service
account. The cluster-scoped object names contain a stable digest of the release
namespace to avoid collisions between namespace-scoped installations using
the default operator name.

SHC-90 preserved the chart's then-existing behavior: `WATCH_NAMESPACE` remains
`.Release.Namespace` even when `namespaceOverride` changes where the Operator
Deployment and service account are placed. SHC-92 subsequently selected and
qualified the effective-namespace contract and aligned the affected templates
and documentation at source `91f742b52`.

## Test-first and local qualification

The unchanged-source controller fixture used a Namespace that already carried
a deletion timestamp and `Terminating` phase. All seven cases failed the new
expectation because every controller called its Apply function once. No source
failure was inferred from an earlier discarded attempt whose relative envtest
path prevented BeforeSuite startup.

The final focused fixture runs twenty-one controller cases:

- seven nondeleting CRs in a terminating Namespace stop before Apply and status;
- seven deleting CRs in the same Namespace reach Apply exactly once; and
- seven active-preflight Apply calls returning wrapped, typed
  `NamespaceTerminating` admission errors become empty successful results with
  no status write.

All twenty-one passed. Helper tests additionally prove active, deletion
timestamp, `Terminating` phase, NotFound, read-error, wrapped lifecycle-cause,
unrelated Forbidden, and nil-error behavior. Manager tests prove Namespace
cache bypass while preserving existing cache and multi-namespace settings.
Predicate coverage proves a deletion-timestamp-only update is accepted. Helm
tests assert exact cluster-wide and namespace-scoped permission output and the
absence of supplemental objects in the wrong mode.

Final macOS source evidence at `0c291c8c8`:

- `make build`: passed generation, formatting, vet, and manager binary build;
- `make test`: 42 suites passed with zero failures;
- enterprise controller suite: 178/178 specs passed;
- composite coverage: 78.1 percent;
- `make helm-check`: 124/124 Helm tests passed; and
- `git diff --check`: passed.

## Linux and immutable image

The official GitLab branch and Linux checkout were clean at exact tip
`0c291c8c87ceb629bb573fcf036c6048c28cedf2`. The final gate ran in detached
`tmux` session `shc90-gates` so transport loss could not terminate it:

- `make test`: passed 42 suites in 2m58s with zero failures, 180 JUnit test
  nodes, and 78.1 percent composite coverage;
- `make build`: passed manifests, generation, formatting, vet, and manager
  binary build;
- `make helm-check`: passed 39 Operator and 85 Universal Forwarder Helm tests,
  124 total; and
- final gate marker: `SHC90_GATE_EXIT=0`.

PATH selected Ginkgo CLI 2.28.1 while the module imports 2.32.0. Go selected
the required 1.25.12 toolchain and the complete Make gate exited zero, so the
warning is recorded as Linux environment hygiene rather than a source failure.

The image was built and pushed from that exact Linux checkout using:

```text
make docker-buildx \
  IMG=667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc90-0c291c8c8 \
  PLATFORMS=linux/amd64
```

Immutable image evidence:

- ECR tag:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc90-0c291c8c8`;
- OCI index:
  `sha256:c2438c14e238e101cba52d758968a2cd7c64fc2798ed5a0a4781acb3e836e764`;
- linux/amd64 manifest:
  `sha256:a05c2197a9754d89a93ad2652933eea224ae071fbcf2c98239a61bdb1bdd99a4`;
- provenance attestation manifest:
  `sha256:b62ec3a1a5fb68acf6626627cd37aa191c642ccdbbd24784eab7ba34ed33ca55`;
  and
- Make result: `SHC90_IMAGE_EXIT=0`.

The first two image attempts failed before source execution because the
workstation's old BuildKit container had a missing writable layer and its
cached BuildKit image referenced a missing parent snapshot. The exact broken
builder and unused stopped local-registry container were removed, unused
Docker state was pruned without volumes, Docker was restarted, and
`moby/buildkit:buildx-stable-1` was repulled successfully at
`sha256:2f5adac4ecd194d9f8c10b7b5d7bceb5186853db1b26e5abd3a657af0b7e26ec`.
The identical Make command then built and pushed successfully. This was Docker
host-state repair, not a source or product change.

## EKS baseline and live evidence

The direct kubeconfig context `shc85-vivek-spl-301372` resolves to exact
cluster ARN
`arn:aws:eks:us-west-2:667741767953:cluster/vivek-spl-301372`. Before SHC-90
deployment, the Operator was healthy at one of one Ready replicas on immutable
SHC-89 digest
`sha256:b83bbb97f89dca45e183e895e4be7e1d7bd11007f08babb41c4c94c97d18f145`.
Service account `splunk-operator/splunk-operator-controller-manager` had no
Namespace get, list, or watch permission. Retained namespace
`shc85-lifecycle-hold` was Active and was not modified.

The live ClusterRole was first extended with only `get` on core
`namespaces`. Authorization then reported get=yes, list=no, and watch=no for
service account
`splunk-operator/splunk-operator-controller-manager`. At 2026-08-01
21:53:38Z the manager rolled successfully to the exact OCI index digest above,
finished one of one Ready with zero restarts, and reported the same digest as
its runtime image ID. Startup logs contained no error or panic. The retained
`shc85-lifecycle-hold` LicenseManager, ClusterManager, four-member
IndexerCluster, deployer, and three-member SHC all remained Ready with their
existing Pods and zero restarts.

Disposable namespace `shc90-namespace-termination` used the previously
qualified runtime digest
`sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`.
It formed one LicenseManager and a three-member SHC without a ClusterManager
dependency. Before deletion:

- both CRs were `Ready`, the deployer was `Ready`, and SHC status was 3/3;
- the authoritative captain was ordinal zero and all three members were
  registered `Up`;
- all five Pods were Kubernetes Ready with zero container restarts;
- the client Search Head Service contained three ready addresses; and
- ten gp3 PVCs were Bound to ten distinct delete-reclaim PVs.

Initial formation took approximately thirteen minutes because the existing
durable formation workflow completed cluster formation, one-member-at-a-time
telemetry restarts, captain convergence, and its stability windows before
opening the custom SHC serving gates. This startup behavior was not changed by
SHC-90.

Namespace deletion was requested at 2026-08-01 22:10:00Z and the Namespace
received deletion timestamp 22:10:01Z. A bounded annotation loop generated
primary-resource updates during propagation. Three consecutive observations
saw the Namespace deletion timestamp while both CR deletion timestamps were
still empty, directly proving the target race window. The Operator then
recorded:

- five LicenseManager and five SearchHeadCluster
  `namespace is terminating; skipping normal reconciliation` messages;
- zero typed admission-race cancellations, because the preflight guard won
  every observed race;
- zero fixture-level `ERROR` records;
- zero fixture-level controller-runtime Reconciler errors; and
- zero `NamespaceTerminating` create-admission failures.

When Kubernetes subsequently marked the CRs for deletion, the explicit
deletion-transition predicate re-entered both controllers. Their existing
finalizer paths ran once, deleted two LicenseManager PVCs and eight SHC PVCs,
removed both `enterprise.splunk.com/delete-pvc` finalizers, and reported
deletion complete. No manual finalizer patch was used and no status write was
attempted after successful finalization.

All non-captain Pods and their volumes disappeared first. The former captain
used the runtime `/sbin/splunk-shutdown` contract, received
`/opt/splunk/bin/splunk stop` at 22:10:08Z, and exited within the configured
1200-second Pod grace period. Afterward the namespace contained zero resources,
and all ten PVs and all PV claim references were absent. Kubernetes retained
the Namespace object temporarily with the condition snapshot from its initial
1200-second Pod-grace estimate, then removed it naturally. It was confirmed
absent at 22:19:07Z, 9m06s after the Namespace deletion timestamp and well
before the grace deadline. No force-finalization, finalizer mutation, or
cleanup patch was used.

## Remaining boundaries

- SHC-91 subsequently qualified deletion-before-pause and
  deletion-before-ordinary-Apply behavior at source `a76c30e0c`; it remains a
  separate work item from this Namespace propagation guard.
- SHC-92 subsequently qualified namespace-scoped Helm `namespaceOverride` and
  watched-namespace semantics at source `91f742b52`; it remains a separate
  work item from SHC-90.
- Legacy v3, Telemetry, and Postgres controllers are outside this bounded v4
  Splunk-tier work item.
- Provider breadth and live Kubernetes-version breadth remain part of the
  broader qualification plan. SHC-92 later added render-only 1.27 and live EKS
  1.31 evidence for the namespace-scoped chart contract.

## Rollback and cleanup

Rollback restores the prior immutable Operator digest and removes the added
Namespace permission if the old binary is retained. The guard does not change
CR or persistent-data schemas, so no data conversion is required.

The EKS fixture namespace is disposable. Cleanup records all PV names before
deletion and verifies both direct PV absence and zero remaining claim
references. The retained SHC-85 namespace and workloads are not cleanup
targets.
