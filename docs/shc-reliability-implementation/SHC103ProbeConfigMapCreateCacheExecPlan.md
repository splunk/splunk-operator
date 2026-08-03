# Make probe ConfigMap creation independent of informer visibility

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

SHC-102 creates a namespace-scoped probe ConfigMap directly so a customer or
another controller can safely win the fixed-name create race. Its first
implementation then required an immediate read of that object through the
Operator client. In a real controller, writes go to the Kubernetes API server
while ordinary reads can come from an informer cache. A successful create can
therefore be durable before the cache observes it. Treating that temporary
NotFound response as creation failure would emit a false reconciliation error
even though the required object exists.

After SHC-103, a successful Kubernetes `Create` is authoritative. A following
read remains best-effort so established tests and diagnostics can observe the
cached object, but cache lag cannot turn a successful write into failure. If
the create returns AlreadyExists because another controller or customer won
the race, the Operator retries only the expected NotFound visibility window,
then preserves the winning object without mutation.

## Progress

- [x] (2026-08-03 19:22Z) Identified the API-server-write/informer-read timing
  error during external review of the planned live new-namespace test.
- [x] (2026-08-03 19:27Z) Implemented successful-create authority,
  best-effort post-create observation, and a bounded NotFound retry for the
  AlreadyExists winner.
- [x] (2026-08-03 19:30Z) Preserved legacy mock call traces and passed 20
  focused ownership/race repetitions, the complete enterprise package, and
  `make build` on macOS.
- [x] (2026-08-03 19:31Z) Committed and pushed exact source
  `44145c4d120a00095e1bf7324bda7df8e0bab745` on isolated branch
  `codex/shc-103-probe-configmap-create-cache` and the final integration
  branch.
- [x] (2026-08-03 20:21Z) Added a deterministic cache-lag regression that
  injects NotFound after a successful create, proves reconciliation succeeds,
  and verifies the persisted data and marker. Twenty focused repetitions
  passed, and test-only source `070ca5f59a5a995839fb56e4832873222613d58e`
  was pushed on both SHC-103 and final integration branches.
- [x] (2026-08-03 20:25Z) Passed the exact native-Linux Make gate at
  `070ca5f59`: 43 suites, 192/192 specs, 78.3 percent composite coverage,
  `make build`, zero generated-tree changes, and manager SHA-256
  `d9afa7444e5ed64256ae3e4c724847a6ea05a5c92eee6a3047a19fd5d5f98f5c`.
- [x] (2026-08-03 20:27Z) Built and pushed immutable Linux/AMD64 Operator OCI
  index `sha256:2ae4db4155427ade5361f8a4d71f71d7ea0b4bdbf447a40e2dc1434815074308`
  through `make docker-buildx` from the exact source.
- [x] (2026-08-03 20:29Z) Deployed the digest on EKS 1.31.14 and created a
  disposable Standalone. Its generated ConfigMap appeared on the fourth
  one-second observation, contained exactly the three probe scripts, and had
  equal full Data hash and marker
  `ddbc90fba32858eb497c2d2ca947ee38f793869c13162d72cbaf2947edfafe43`
  across repeated reconciliation. Candidate logs contained zero ERROR/FATAL
  records and zero probe-ConfigMap NotFound failures; the namespace emitted
  zero Warning Events during creation.
- [x] (2026-08-03 20:32Z) Deleted the disposable namespace and both tracked
  PVs, restored accepted Operator index
  `sha256:a9f2125097fa823d5182e8729683e5099116a889fdae8e892f0bd3110a8cdf3d`,
  and proved all 20 retained Pod UID/restart/readiness rows and both retained
  ConfigMaps were unchanged. Retained and fresh health snapshots passed.

## Surprises & Discoveries

- Observation: Kubernetes create and controller-runtime read do not
  necessarily share the same consistency path.
  Evidence: `client.Create` is an API-server write while a normal
  `client.Get` may be served by an informer cache.
  Consequence: creation success must not depend on an immediate cached read.
- Observation: an AlreadyExists result has a different contract from a
  successful create.
  Evidence: the Operator does not own the object that won that race and must
  return its current contents rather than its own candidate.
  Consequence: retry only NotFound visibility for that winner; propagate
  authorization, transport, and other terminal errors unchanged.
- Observation: unit-test clients commonly provide immediate read-after-write
  visibility.
  Evidence: the existing fake and mock clients cannot prove informer lag.
  Consequence: final acceptance requires a real controller and API-server
  creation test, not only unit tests.

## Decision Log

- Decision: return the object accepted by a successful `Create` even if the
  best-effort observation read fails.
  Rationale: the API server has already accepted the exact name and data used
  by the rendered Pod volume; cache visibility is not part of write success.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: use client-go's bounded default retry only when an
  AlreadyExists-winner read returns NotFound.
  Rationale: informer propagation is transient, while permission and
  transport failures must remain visible to reconciliation and support.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.
- Decision: keep this correction separate from SHC-102 ownership semantics.
  Rationale: SHC-102 determines whether data may be mutated; SHC-103 determines
  whether a successfully created object must already be visible in cache.
  Independent work items keep the contracts and evidence reviewable.
  Date/Author: 2026-08-03, Codex with Vivek Reddy.

## Outcomes & Retrospective

SHC-103 is complete at `070ca5f59`. Deterministic cache-lag injection, exact
native-Linux gates, an immutable Linux/AMD64 image, real EKS creation,
repeated reconciliation, namespace/storage cleanup, and accepted restoration
all passed. The change adds no CRD, public annotation, probe-script,
Pod-template, Splunk Enterprise, Docker-Splunk, or Splunk Ansible behavior.

## Context and Orientation

`pkg/splunk/enterprise/configuration.go` contains `getProbeConfigMap`. The
function is used while every Splunk tier renders its StatefulSet and shares
one fixed-name probe ConfigMap per namespace. SHC-102 defines its data-
ownership marker and update rules. SHC-101 defines conflict-safe updates.

`pkg/splunk/enterprise/configuration_test.go` contains deterministic creation,
ownership, Conflict, and AlreadyExists behavior tests. A disposable EKS
namespace is still required to establish the informer timing contract against
a real manager.

## Plan of Work

Run `make shc98-monitor-check`, the focused probe ConfigMap tests twenty times,
`make test`, and `make build` on native Linux at exact source `070ca5f59`.
Record source cleanliness and the manager hash before building the immutable
Linux/AMD64 Operator image through the repository Make target.

Deploy only that digest to the existing EKS qualification cluster. Create one
disposable namespace and a minimal supported Splunk CR. Observe the first
probe ConfigMap creation, compute a deterministic hash over its complete Data,
and compare it with
`enterprise.splunk.com/probe-configmap-content-hash`. Inspect candidate-scoped
Operator logs and Kubernetes Events for false NotFound or StatefulSet errors.
Delete the namespace and all storage immediately after capturing the bounded
evidence.

Restore the accepted Operator digest. Confirm the two retained namespace
ConfigMaps remain byte-identical and unmarked, all retained Splunk Pods keep
their UIDs and restart counts, and retained- and fresh-cluster health
snapshots pass.

## Validation and Acceptance

Acceptance requires:

- exact Linux source passes the focused repeated tests, complete Make test,
  and Make build gates;
- the built manager and immutable image are traceable to `070ca5f59`;
- a real new namespace receives exactly one generated probe ConfigMap;
- its recorded marker equals the deterministic hash of all ConfigMap Data;
- candidate-scoped logs and Events contain no false cache-NotFound failure;
- the disposable namespace, CRs, Pods, PVCs, and PVs are removed; and
- the accepted Operator and both retained clusters are restored healthy with
  unchanged workload Pod identity and restart counts.

## Idempotence and Recovery

The source change is idempotent. A successful create returns success; a later
reconcile observes the object and applies SHC-102 ownership rules. A
concurrent winner is read and preserved. Repeating the EKS test uses a new
disposable namespace or first proves the old namespace is fully deleted.

The rollback is the accepted immutable Operator digest
`sha256:a9f2125097fa823d5182e8729683e5099116a889fdae8e892f0bd3110a8cdf3d`.
No retained ConfigMap mutation is required for the SHC-103 live test.

## Artifacts and Notes

- Isolated branch: `codex/shc-103-probe-configmap-create-cache`.
- Integration branch: `codex/shc-kubernetes-reliability-final-integration`.
- Production correction: `44145c4d120a00095e1bf7324bda7df8e0bab745`.
- Exact cumulative source with deterministic regression:
  `070ca5f59a5a995839fb56e4832873222613d58e`.
- Native-Linux manager SHA-256:
  `d9afa7444e5ed64256ae3e4c724847a6ea05a5c92eee6a3047a19fd5d5f98f5c`.
- Final Operator OCI index:
  `sha256:2ae4db4155427ade5361f8a4d71f71d7ea0b4bdbf447a40e2dc1434815074308`.
- Linux/AMD64 manifest:
  `sha256:8130792347239d0ec7e5318e605a59f3e8e2f104a8ef32d8aa9557b15804a188`.
- Build attestation manifest:
  `sha256:80870393a5d2b7235cc6ea8a459c0c993d94cf8da4e1d7f9f2c52105e2788a5e`.
- Live generated ConfigMap Data hash and marker:
  `ddbc90fba32858eb497c2d2ca947ee38f793869c13162d72cbaf2947edfafe43`.
- Candidate Operator log SHA-256:
  `eb1740b4a94eaf2699d340cc56705f4a162e5a579fd8a8c6e522acdd381cb8a4`;
  ERROR/FATAL count zero and probe-NotFound count zero.
- Before/after retained Pod snapshot SHA-256:
  `3dd1ac3888f30681d7ce85343d7cf64b91adb6cbb4cab1b092e5569c799ff90d`;
  20 rows and zero diff lines.
- Accepted-restoration retained snapshot/config SHA-256:
  `b46270690c8607adf492e6bc22a59bc323cb25db1b15d328d5204f631a93c5a4`
  and `508697f3c33b673928f22ba12118eea107cf6e0a49541bc826ccb05d040f127f`.
- Accepted-restoration fresh snapshot/config SHA-256:
  `6695e2d9b6826ecaf94e02af30feffe51018f6e729d106ea0cd98445b83e96fb`
  and `8a87497b9d0a8f6f5fa46e793e862409997b31c7d0194625241a650384b88134`.
- Accepted Operator OCI index:
  `sha256:a9f2125097fa823d5182e8729683e5099116a889fdae8e892f0bd3110a8cdf3d`.
- Two transient `VolumeFailedDelete` Warnings occurred only after namespace
  deletion while the disposable EBS volumes detached. Both tracked PVs and
  the namespace were subsequently absent; no retained resource was involved.

## Interfaces and Dependencies

SHC-103 uses controller-runtime's existing client, Kubernetes NotFound and
AlreadyExists semantics, and client-go `retry.OnError`. It introduces no new
API, RBAC, image input, persistent state, or external service dependency.

Revision note (2026-08-03 19:40Z): Created the bounded execution plan after
review of SHC-102 found that its successful create path still required
immediate informer visibility. Recorded exact source, completed macOS gates,
and the remaining native-Linux and real-cluster acceptance work.

Revision note (2026-08-03 20:35Z): Closed SHC-103 with a deterministic
post-create NotFound regression, exact Linux Make gate, immutable EKS
creation, full marker verification, zero candidate manager errors, complete
disposable namespace/PV deletion, unchanged retained objects, and healthy
accepted restoration.
