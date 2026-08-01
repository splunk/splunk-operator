# SHC-91 Deletion-Before-Pause Qualification

## Scope and result

SHC-91 makes Kubernetes deletion authoritative over the Splunk Operator pause
annotation and over ordinary tier reconciliation. A paused custom resource
that has a deletion timestamp must reach its existing finalizer before normal
validation, configuration, dependency, or workload work. After successful
finalization, reconciliation must return without trying to update status on an
object that may already have disappeared.

The bounded result is complete for all seven active v4 Splunk tier
controllers: Standalone, LicenseManager, ClusterManager, MonitoringConsole,
IndexerCluster, SearchHeadCluster, and IngestorCluster. Source passed on macOS
and Linux. An immutable linux/amd64 Operator image passed three EKS fixtures:

1. direct deletion of all seven paused resources in an Active Namespace;
2. namespace-first deletion of all seven paused resources; and
3. direct deletion of a paused, real Ready Standalone with a StatefulSet, Pod,
   two bound PVCs, and two delete-reclaim PVs.

No fixture required removal of its pause annotation, manual finalizer patch,
or force-finalization. All disposable resources and PV claim references
disappeared naturally. The retained SHC-85 workload remained Ready with zero
restarts.

This is bounded spike evidence. It does not declare production readiness for
every Kubernetes provider/version, namespace-scoped Helm mode, legacy v3
controllers, or every real Splunk topology.

## Problem and required contract

Pause is a desired-state control. A finalizer is a Kubernetes deletion
obligation. If pause is evaluated first, an object can remain indefinitely in
deletion because the controller returns before cleanup. If deletion reaches a
real Apply function but ordinary work runs first, invalid or unresolved normal
configuration can still block finalization. If the controller writes status
after successful finalization, the write can race deletion and produce noisy
NotFound or conflict errors.

The accepted contract is therefore:

- while a resource is active and paused, ordinary reconciliation stops and its
  paused status remains observable;
- once the resource is deleting, pause no longer blocks Apply;
- inside Apply, existing finalization precedes normal work;
- successful finalization returns without a generic status write;
- failed finalization retains the existing error/status path; and
- finalizer policy and storage ownership do not otherwise change.

SHC-90 owns the immediately preceding interval in which the Namespace is
terminating but the CR deletion timestamp has not appeared. SHC-91 begins once
the CR itself has a deletion timestamp.

## Exact source

- branch: `codex/shc-91-deletion-before-pause`;
- controller commit:
  `86a0bc80ada0b456aeffc25cd161f0f6eaf33102`;
- Apply-order commit:
  `a76c30e0c2395506cbfbb8d9e2643c186df0a3ef`;
- final source tip:
  `a76c30e0c2395506cbfbb8d9e2643c186df0a3ef`.

The first commit corrects the five controller entry points that previously
honored pause before deletion and adds the successful-finalization return. The
second commit moves each tier's existing deletion work ahead of ordinary Apply
work for Standalone, ClusterManager, MonitoringConsole, both IndexerCluster
manager paths, and IngestorCluster. LicenseManager and SearchHeadCluster were
positive controls with deletion-first behavior already present.

No Splunk Enterprise, Docker-Splunk, Ansible, CRD schema, public API, RBAC,
StatefulSet policy, probe, container lifecycle, or persistent-data-format
change is part of SHC-91.

## Test-first source evidence

The controller fixture covers every active v4 tier with a paused, deleting
resource and an intercepted Apply function. Against unchanged source, the
successful-finalization assertion failed for exactly five controllers because
Apply was never called:

- Standalone;
- ClusterManager;
- MonitoringConsole;
- IndexerCluster; and
- IngestorCluster.

LicenseManager and SearchHeadCluster passed as controls. After the controller
change, all seven successful cases called Apply exactly once and made zero
status writes. Seven injected-failure cases called Apply once, returned the
injected error, and retained existing status handling.

Apply-level tests then used deleting resources with adversarial normal
configuration. Before the second correction, all six affected entry points
failed before finalization through normal-path App Framework validation,
license acceptance, or missing Queue/ObjectStorage dependencies. After the
change, every case ran existing deletion cleanup first. A representative
cleanup-error case also proved that finalizer failure is returned and remains
observable rather than being treated as successful deletion.

## Final source gates

Final macOS evidence at exact source `a76c30e0c`:

- `make test`: 42 suites passed with zero failures;
- enterprise controller suite: 185/185 specs passed;
- composite coverage: 78.3 percent;
- `make build`: passed manifests, generation, formatting, vet, and binary
  build;
- `make helm-check`: 39 Operator plus 85 Universal Forwarder tests passed,
  124 total; and
- focused `go test ./pkg/splunk/enterprise -count=1`: passed.

Final Linux evidence from the exact clean commit:

- `make test`: 42 suites passed with zero failures in 2m54s;
- enterprise controller suite: 185 specs passed;
- composite coverage: 78.3 percent;
- `make build`: passed; and
- `make helm-check`: 39 plus 85 tests passed, 124 total.

The complete final macOS `make test` took 3m55s. The package-only Apply test
run took 65 seconds. These timings are recorded as replay context, not product
performance claims.

## Immutable image and EKS environment

The image was built and pushed from the clean Linux checkout with the
repository-controlled target:

```text
make docker-buildx \
  IMG=667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc91-a76c30e0c \
  PLATFORMS=linux/amd64
```

Immutable identities:

- ECR tag:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc91-a76c30e0c`;
- OCI index:
  `sha256:4903f70a95b150c0a29bcd3ac70e063b5c55b6a030399a4636297586dea85cea`;
- linux/amd64 manifest:
  `sha256:6da77f0cdd1a4be2e2e8f6b9fa5f983f4a8824dab12942bb37f2df2cbd467008`.

The deployment used the OCI index digest, not the mutable tag:

- context: `shc85-vivek-spl-301372`;
- cluster ARN:
  `arn:aws:eks:us-west-2:667741767953:cluster/vivek-spl-301372`;
- Operator namespace: `splunk-operator`;
- deployment: `splunk-operator-controller-manager`;
- running Pod:
  `splunk-operator-controller-manager-575cbcdd78-hb4mx`;
- result: one of one Ready, zero restarts, exact OCI index image ID.

The real Standalone fixture used the previously qualified runtime:

```text
667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk:shc85-f063cfd-ansible-5e9e12f-splunkcloud-10.5.2605.0-844c593e9c1d@sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2
```

## Fixture 1: all tiers, Active Namespace, direct CR deletion

Namespace `shc91-active-delete` contained one resource of each active v4 tier.
Every resource was deliberately created paused, reported `Pending/Paused`, and
carried the real `enterprise.splunk.com/delete-pvc` finalizer. Applicable
resources also carried adversarial normal configuration so a successful result
could not be explained by proceeding through ordinary reconciliation.

The resource UIDs were:

| Tier | UID |
|---|---|
| Standalone | `d3173f17-7204-413f-81a5-e53988d3a593` |
| ClusterManager | `122fe175-f1b6-419a-962d-def2c679b7a3` |
| MonitoringConsole | `7239e7e9-2f35-40e1-af8e-942de00d3f7c` |
| LicenseManager | `9518b61d-f4ec-4ff1-8797-a11ad6716772` |
| SearchHeadCluster | `40c9500e-2f8a-43a6-924d-ef9568b14406` |
| IndexerCluster | `03ad5cc4-3a8d-40f2-a4e1-5355e42f8178` |
| IngestorCluster | `fec9b911-093e-491a-a84e-e59fd5816420` |

Seven Pending PVCs used exact labels matching each finalizer's selection
contract. No managed workload existed; only the normal root CA ConfigMap was
present. The fixture was ready at 2026-08-01 23:23:10Z. All CR deletions were
requested at 23:23:41Z and all seven resources plus all seven PVCs were absent
at 23:23:46Z, five seconds later, while the Namespace remained Active.

The scoped Operator log contained exactly seven finalizer removals and seven
PVC deletion records, all at 23:23:42Z. It contained no normal create or
admission failure, post-finalization status error, or Reconciler error. The
Namespace was deleted after the evidence was collected and disappeared
naturally.

## Fixture 2: all tiers, namespace-first deletion

Namespace `shc91-namespace-delete` used the same seven-tier paused,
finalizer-bearing, adversarial layout and seven Pending PVCs. Resource UIDs
were:

| Tier | UID |
|---|---|
| Standalone | `02ba7c3d-2a7d-47b6-b5e2-ae59a1b66e4a` |
| ClusterManager | `9813d82e-6080-463d-9fc7-503fe3138e48` |
| MonitoringConsole | `74c8ac8b-97b2-453b-82bb-cd53b2d0aafe` |
| LicenseManager | `64042f4f-9b86-4f0b-8953-caeb22b593ef` |
| SearchHeadCluster | `17cd5295-2c7b-4b5f-89e2-723883b1089f` |
| IndexerCluster | `7f74dfd2-964e-4010-acb2-9dc92a2e45f5` |
| IngestorCluster | `16a19df1-2d96-4f47-bc14-89f5ce04cfb3` |

The fixture was ready at 23:24:47Z. Namespace deletion was requested at
23:25:12Z. At the one-second observation, the Namespace was Terminating and all
seven CRs and seven PVCs still existed. At the 13-second observation, the
Namespace, every CR, and every PVC were absent. All finalization records were
emitted at 23:25:18Z.

The structured audit found:

- seven finalizer removals;
- seven PVC deletions;
- seven deletion completions;
- zero Operator creates;
- zero status errors; and
- zero Reconciler errors.

Kubernetes supplied the deletion timestamps; the fixture used no manual pause
or finalizer mutation.

## Fixture 3: real Ready Standalone and persistent storage

Namespace `shc91-real-delete` proved the deletion contract against an actual
running workload rather than label-only PVC fixtures. The Standalone CR UID
was `0cf142ae-cd78-4f76-b487-05929a6be201`. It was created at 23:26:18Z and
became Ready at 23:28:15Z. Its image-owned Ansible run completed in 49.604
seconds with `ok=94`, `changed=12`, `unreachable=0`, `failed=0`, and
`skipped=89`.

Before pause and deletion:

| Object | Evidence |
|---|---|
| StatefulSet | UID `64fc3a71-6b18-4f83-a567-72cce6834060`, 1/1 Ready, 1200-second termination grace, exact runtime image |
| Pod | UID `78a232be-0888-4f63-b320-da60189e5c00`, Running and Ready, zero restarts, exact runtime child image ID |
| etc PVC/PV | PVC UID `2082b258-ab39-4e9b-9b3b-38654a615096`; PV UID `1aa9fd6e-4c01-43e7-af6d-0b5f6926dadf`; Delete reclaim |
| var PVC/PV | PVC UID `50eac360-ec4a-4d3e-8e40-01dd82065fc2`; PV UID `888d6627-99ff-4c69-b66e-2301a3ff0965`; Delete reclaim |

The pause annotation was added at 23:28:21Z. The CR remained `Ready` and
reported `Paused=True`; its finalizer remained present. Deletion was requested
at 23:28:40Z. At 23:28:41Z, the Operator recorded the deletion request, both
PVC delete calls, finalizer removal, and deletion completion.

Observed cleanup timeline from the delete request:

| Sample | CR | StatefulSet | Pod | PVCs | PVs |
|---|---:|---:|---:|---:|---:|
| 1 second | 0 | 0 | 1 | 2 | 2 |
| 49 seconds | 0 | 0 | 0 | 0 | 2 |
| 57 seconds | 0 | 0 | 0 | 0 | 1 |
| 73 seconds | 0 | 0 | 0 | 0 | 0 |

The scoped log contained one finalizer removal, two PVC deletions, one deletion
completion, zero status errors, and zero Reconciler errors. The remaining
objects were only shared namespace Secret/probe/root-CA content; the disposable
Namespace was then deleted and disappeared naturally.

## Retained baseline and cleanup

The retained namespace `shc85-lifecycle-hold` was not a qualification target.
After SHC-91, its LicenseManager, ClusterManager, four IndexerCluster peers,
deployer, and three Search Heads were all Running and Ready with zero restarts;
its completed Job remained complete.

At the end of qualification:

- `shc91-active-delete` was absent;
- `shc91-namespace-delete` was absent;
- `shc91-real-delete` was absent;
- no PV retained a claim reference to any SHC-91 namespace; and
- the Operator remained one of one Ready on the exact SHC-91 digest with zero
  restarts.

## Failure audit and supportability

The qualification checked structured Operator logs by fixture namespace and
resource identity. A valid pass required positive evidence of finalizer
removal, PVC deletion, and reconciliation completion, plus absence of:

- normal create attempts from an adversarial deleting resource;
- `NamespaceTerminating` admission failures;
- status updates after successful finalization;
- controller-runtime Reconciler errors; and
- manual finalizer or pause changes.

The asynchronous Standalone storage timeline is intentionally retained. It
lets support distinguish immediate Operator finalization from later kubelet,
CSI, and cloud-volume reclamation rather than treating the full 73 seconds as
time spent inside the controller finalizer.

## Remaining boundaries

- EKS is the only provider in this qualification. AKS, GKE, OpenShift, and
  supported Kubernetes minimum/latest version breadth remain open.
- Namespace-scoped Helm and `namespaceOverride` behavior remain SHC-92.
- Legacy v3 ClusterMaster/LicenseMaster, Telemetry, Postgres, Queue, and
  ObjectStorage controller behavior is not claimed. Queue and ObjectStorage do
  not have active enterprise reconcilers in this source baseline.
- Only Standalone was exercised as a complete real running workload. The
  all-tier fixtures prove real controller and finalizer routing with PVC API
  objects, not full runtime shutdown of every topology.
- This work proves Kubernetes lifecycle ordering and observed Standalone
  cleanup. It does not claim graceful captain transfer, SHC drain, indexer
  searchable restart, or every Splunk process-shutdown semantic.
- No claim is made about deletion under API partition, CSI failure, stuck
  volume attachment, PodDisruptionBudget conflict, or a failed finalizer beyond
  the source-level observable error test.

## Conclusion

At exact source `a76c30e0c`, a deleting v4 Splunk resource no longer remains
paused or enters unrelated normal Apply work before its existing finalizer.
The bounded source and EKS evidence covers all seven active tier controllers,
both Active-Namespace and namespace-first deletion, a real Ready workload with
persistent storage, clean failure logging, natural Kubernetes cleanup, and no
regression to the retained SHC baseline.
