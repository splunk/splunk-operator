# Stop normal Splunk reconciliation when a namespace starts terminating

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds.

This document is maintained in accordance with the ExecPlan requirements in
the `execution-plan` skill.

## Purpose / Big Picture

Kubernetes begins namespace deletion by placing a deletion timestamp on the
Namespace. A short interval can pass before that timestamp is propagated to
every custom resource inside the Namespace. During that interval, the current
Splunk Operator can enter an ordinary reconcile and attempt to create a
ConfigMap, Secret, Service, StatefulSet, or other managed object. Kubernetes
rejects new content in a terminating Namespace, so an expected deletion is
reported as repeated controller failure.

With SHC-90, every active v4 Splunk tier controller reads the authoritative
Namespace immediately before ordinary work. If the Namespace is terminating or
already absent, the reconcile will return successfully without applying desired
state, writing status, creating an Event, or setting a timer. If the custom
resource itself already has a deletion timestamp, its existing Apply path will
still run so finalizers can remove declared resources and storage according to
policy. The behavior is visible in focused tests and in an EKS namespace-first
deletion campaign whose Operator log contains no post-termination create
attempt or Reconciler error.

## Progress

- [x] (2026-08-01 UTC) Created isolated branch
  `codex/shc-90-namespace-termination-guard` from completed SHC-89 history
  `8adad645f`.
- [x] (2026-08-01 UTC) Audited the seven active v4 Splunk controllers, manager
  cache configuration, generated RBAC, Helm RBAC, and existing deletion tests.
- [x] (2026-08-01 UTC) Added test-first coverage for the shared namespace
  state decision, all seven controller entry points, direct-read manager
  configuration, finalization reachability, deletion-transition events, and
  both Helm RBAC modes. The unchanged source failed all seven controller cases
  with one Apply call per type; the implemented source passes all twenty-one
  preflight-stop, finalizer-bypass, and admission-race controller cases.
- [x] (2026-08-01 UTC) Implemented the shared guard, least-privilege live
  Namespace read, deletion-transition event predicate, generated RBAC, and
  collision-safe Helm RBAC.
- [x] (2026-08-01 UTC) Passed focused tests, `make manifests`, `make fmt`,
  `make build` (including generate, vet, and binary build), `make test`, and
  `make helm-check` on the final macOS source shape. The complete final test
  gate ran 42 suites with zero failures and composite coverage of 78.1%; the
  enterprise controller suite passed all 178 specs. All 124 Helm tests passed.
- [x] (2026-08-01 UTC) Created source-only commits `7ce2483f7` and
  `0c291c8c8`. The second closes the unavoidable preflight-to-admission race
  using the typed Kubernetes lifecycle cause without suppressing other
  Forbidden errors. A verified complete-history transfer bundle is maintained
  at `/tmp/shc90-source.bundle`.
- [x] (2026-08-01 UTC) Pushed exact source tip `0c291c8c8` to official GitLab
  remote `sok` and checked out the same clean SHA at
  `~/splunk-complete/splunk-operator` on the Linux vWorkstation.
- [x] (2026-08-01 UTC) Completed the detached Linux qualification at exact clean
  tip `0c291c8c8`: `make test`, `make build`, and `make helm-check` exited zero.
  The run passed 42 suites, 180 JUnit nodes, 78.1 percent composite coverage,
  and all 124 Helm tests.
- [x] (2026-08-01 UTC) Verified direct EKS read access through context alias
  `shc85-vivek-spl-301372`, which resolves to the exact target cluster ARN.
  The pre-deployment SHC-89 Operator was 1/1 Ready; its service account had
  Namespace get/list/watch all denied. The retained SHC-85 namespace was
  Active and was not modified.
- [x] (2026-08-01 UTC) Built and pushed the exact source from Linux using
  `make docker-buildx` for linux/amd64. ECR OCI index
  `sha256:c2438c14e238e101cba52d758968a2cd7c64fc2798ed5a0a4781acb3e836e764`
  contains linux/amd64 manifest `sha256:a05c2197a9754d89a93ad2652933eea224ae071fbcf2c98239a61bdb1bdd99a4`.
- [x] (2026-08-01 UTC) Added Namespace get-only live RBAC, verified
  get=yes/list=no/watch=no, rolled EKS to the immutable SHC-90 digest, and kept
  the retained SHC-85 environment Ready and unchanged.
- [x] (2026-08-01 UTC) Formed a disposable real LicenseManager and three-member
  SHC to Ready/Ready and 3/3 with three Service endpoints and zero restarts.
  Namespace-first deletion proved the target propagation interval, produced
  five guard records per controller, zero fixture errors, and natural CR
  finalization with all ten PVC/PV claim references removed.
- [x] (2026-08-01 UTC) Confirmed the Namespace absent naturally at 22:19:07Z,
  9m06s after its deletion timestamp and before the 1200-second Pod grace
  deadline. No namespaced resource, PVC, PV, or PV claim reference remained,
  and no finalizer or cleanup patch was used.
- [x] (2026-08-01 UTC) Finalized qualification documentation as a separate
  documentation-only commit from the two source commits and prepared the
  completed branch for its official GitLab `sok` push.

## Surprises & Discoveries

- Observation: Namespace termination can precede CR deletion visibility.
  Evidence: the SHC-87 cleanup recorded six LicenseManager and nine
  SearchHeadCluster Reconciler errors from rejected ConfigMap creates before
  the CR deletion timestamps became visible; finalization later completed.
- Observation: the generated kustomize manager permission is a ClusterRole,
  but a namespace-scoped Helm install uses a Role. A Role cannot grant access
  to the cluster-scoped Namespace resource.
  Evidence: `config/rbac/role.yaml` is `kind: ClusterRole`, while
  `helm-chart/splunk-operator/templates/rbac/role.yaml` is rendered as
  `kind: Role` when `splunkOperator.clusterWideAccess=false`.
- Observation: using the normal cached client for Namespace would create an
  informer and require list/watch permission. It also introduces cache delay
  at exactly the boundary SHC-90 is meant to close.
  Evidence: controller-runtime v0.24.1 supports
  `client.CacheOptions.DisableFor`; objects in that list are read live from the
  API server.
- Observation: LicenseManager and SearchHeadCluster already route a CR with a
  deletion timestamp around pause and suppress a generic status write after
  successful finalization. The other five active v4 controllers do not yet
  prove deletion-before-pause ordering.
  Evidence: the controller entry-point audit registered the latter behavior as
  SHC-91. SHC-90 will not represent SHC-91 as complete.
- Observation: the unchanged source reaches Apply for every active v4 Splunk
  type when its fake Namespace already has a deletion timestamp and
  `NamespaceTerminating` phase.
  Evidence: the focused 2026-08-01 test ran seven specs and failed all seven at
  `applyCalls == 1`; the required value is zero. An earlier harness attempt was
  discarded because a relative envtest path prevented BeforeSuite startup.
- Observation: namespace-scoped Helm mode uses `.Release.Namespace` for
  `WATCH_NAMESPACE`, even when `namespaceOverride` places the Deployment and
  service account elsewhere.
  Evidence: `templates/deployment.yaml` renders the watched value from the
  release namespace, while resource metadata uses the namespace helper. The
  SHC-90 Namespace reader therefore restricts `resourceNames` to the release
  namespace and binds the service account in its actual metadata namespace.
  Changing this pre-existing Helm behavior is outside SHC-90 and requires a
  separate compatibility decision, registered as SHC-92.
- Observation: the existing tier-controller event filters accepted generation,
  annotation, label, and owned-resource changes, but did not accept a primary
  resource update that only set `metadata.deletionTimestamp`.
  Evidence: Kubernetes does not increment `metadata.generation` for deletion,
  and the common predicates contained no deletion-timestamp comparison. Since
  SHC-90 intentionally returns without a polling timer, an explicit
  deletion-transition predicate is required to guarantee that the later CR
  deletion update reaches Apply/finalization.
- Observation: a namespace-scoped Helm installation introduces a
  cluster-scoped reader object. The pre-existing operator fullname is not
  release-specific, so using it alone would collide across installations in
  different namespaces.
  Evidence: the Namespace reader name now includes the first eight hexadecimal
  characters of the release-namespace SHA-256 digest, while its permission
  remains restricted to that exact Namespace name.
- Observation: an authoritative preflight GET narrows but cannot atomically
  eliminate the interval between reading the Namespace and a later resource
  create reaching API admission.
  Evidence: Kubernetes NamespaceLifecycle returns a typed Forbidden status
  with standard cause `NamespaceTerminating` when termination begins in that
  interval. Kubernetes' own clients use `apierrors.HasStatusCause` for this
  lifecycle result. SHC-90 therefore needs both the preflight stop and narrow
  admission-error cancellation; unrelated Forbidden errors must remain
  visible failures.
- Observation: the Linux Make target reports a pre-existing Ginkgo toolchain
  warning: PATH resolves CLI 2.28.1 while the module imports 2.32.0. The CLI
  continued into the test suites.
  Evidence: the interrupted first Linux attempt emitted the version warning,
  then passed the new cache tests and progressed into enterprise specs before
  the Coder/VPN connection disappeared. This is recorded as environment
  hygiene and is not treated as a source result; the final gate still requires
  an exit-zero Make result.
- Observation: the Linux Docker host contained corrupt stale BuildKit state.
  Evidence: the first image attempt reported a nil writable layer for the old
  builder container, and the clean builder then reported a missing parent
  snapshot while extracting `moby/buildkit`. Removing the exact builder,
  pruning unused Docker state without volumes, restarting Docker, and repulling
  BuildKit made the identical Make build pass. No source change was involved.
- Observation: a Namespace can retain stale `SomeResourcesRemain` conditions
  after every namespaced resource and matching PV claim reference has gone.
  Evidence: the initial namespace-controller pass observed five Pods with
  1200-second grace and ten protected PVCs. The former captain exited and all
  PVCs/PVs disappeared, but the Namespace object retained the original
  condition timestamp while waiting for another namespace-controller pass.
  SHC-90 does not force or mutate Kubernetes' Namespace finalizer.

## Decision Log

- Decision: Query the containing Namespace before pause or ordinary Apply, but
  only while the custom resource itself is not deleting.
  Rationale: namespace termination must stop all normal writes, while a CR
  deletion timestamp must continue to the existing finalizer path.
  Date/Author: 2026-08-01, Codex with Vivek Reddy.
- Decision: Configure the manager client to bypass its cache for
  `corev1.Namespace`, then use that same client in the shared guard.
  Rationale: a live GET sees the authoritative deletion timestamp and needs
  only `get`, avoiding both stale cache state and broad list/watch permission.
  Date/Author: 2026-08-01, Codex with Vivek Reddy.
- Decision: Treat a missing Namespace as terminating and treat any other
  Namespace read error as a fail-closed reconcile error.
  Rationale: an existing namespaced CR cannot legitimately outlive its
  Namespace. On an authorization or transport failure, running Apply would be
  less safe than retrying without mutation.
  Date/Author: 2026-08-01, Codex with Vivek Reddy.
- Decision: Return an empty successful result for a terminating or missing
  Namespace, without status or Event writes.
  Rationale: Kubernetes already owns deletion progress; Events require creates
  and status adds no durable value to an object being deleted. Avoiding a timer
  also prevents a deletion-driven retry loop.
  Date/Author: 2026-08-01, Codex with Vivek Reddy.
- Decision: Accept deletion-timestamp changes in every active v4 tier
  controller's event filter.
  Rationale: the Namespace guard has no retry timer, and a CR entering deletion
  must deterministically re-enter reconciliation so its Apply/finalizer path
  remains reachable. This does not resolve SHC-91's separate
  deletion-before-pause ordering gap in five controllers.
  Date/Author: 2026-08-01, Codex with Vivek Reddy.
- Decision: Treat only a Forbidden Apply error carrying the Kubernetes
  `NamespaceTerminating` status cause as expected cancellation.
  Rationale: this closes the unavoidable preflight-to-admission race without
  string matching or suppressing RBAC, policy, or other authorization errors.
  The controller returns no timer or status write; the CR deletion transition
  then re-enters reconciliation through the explicit lifecycle predicate.
  Date/Author: 2026-08-01, Codex with Vivek Reddy.
- Decision: For namespace-scoped Helm installations, add a small ClusterRole
  whose Namespace `get` is restricted by `resourceNames` to the Operator's
  configured release namespace, plus a matching ClusterRoleBinding.
  Rationale: Namespace is cluster-scoped and cannot be authorized by the
  existing Role. Resource-name restriction preserves the namespace-scoped
  security boundary.
  Date/Author: 2026-08-01, Codex with Vivek Reddy.
- Decision: Derive the namespace-scoped reader ClusterRole and binding name
  from the operator fullname plus a stable digest of `.Release.Namespace`.
  Rationale: cluster-scoped names must not collide when separate namespaces
  install namespace-scoped operators with the same default operator name.
  Date/Author: 2026-08-01, Codex with Vivek Reddy.
- Decision: Keep SHC-91 separate.
  Rationale: SHC-90 stops work in a terminating Namespace before CR deletion
  propagation. SHC-91 is the distinct case where the CR is already deleting
  and also paused in one of five controllers. The SHC-90 branch will preserve,
  but not overstate, that existing boundary.
  Date/Author: 2026-08-01, Codex with Vivek Reddy.

## Outcomes & Retrospective

The source and live behavior are qualified at exact source tip `0c291c8c8` and
Operator OCI index
`sha256:c2438c14e238e101cba52d758968a2cd7c64fc2798ed5a0a4781acb3e836e764`.
Mac and Linux Make gates passed. The EKS campaign directly observed the target
state—Namespace deleting while both CR deletion timestamps were empty—and the
guard stopped ten triggered reconciles without an Apply failure, status error,
or Reconciler error. CR deletion events then reached the preserved finalizer
paths, which removed all ten PVCs and their PV claim references without manual
finalizer intervention. Kubernetes removed the Namespace naturally after the
former captain's graceful shutdown. Documentation is finalized separately
from the two source commits.

## Context and Orientation

The seven active v4 Splunk reconcilers live in
`internal/controller/enterprise/*_controller.go`: Standalone,
LicenseManager, ClusterManager, MonitoringConsole, IndexerCluster,
SearchHeadCluster, and IngestorCluster. Each entry point first reads its custom
resource and eventually calls a type-specific `Apply*` function. Apply is the
mutation path that creates or updates Kubernetes objects and also contains
type-specific deletion finalization.

A namespace-first race is different from ordinary CR deletion. In the race,
the Namespace has a deletion timestamp but the custom resource does not yet
have one. Kubernetes admission rejects creation in that Namespace. Once the CR
deletion timestamp arrives, the Operator must no longer stop: it must call
Apply so finalizers can run. A finalizer is a string in object metadata that
keeps an object present until its controller has completed required cleanup.

The manager client is configured in `pkg/config/config.go` and constructed in
`cmd/main.go`. By default, controller-runtime serves reads from a local watch
cache. SHC-90 will add Namespace to `Client.Cache.DisableFor`, which makes only
Namespace reads go directly to the API server. All existing namespaced
workload reads remain cached.

Generated kustomize permission is in `config/rbac/role.yaml` and comes from
kubebuilder RBAC markers. Helm permission is maintained separately in
`helm-chart/splunk-operator/templates/rbac`. Cluster-wide Helm mode can add
Namespace `get` to its manager ClusterRole. Namespace-scoped Helm mode needs a
separate minimal ClusterRole and ClusterRoleBinding because its manager Role
cannot authorize a cluster-scoped Namespace.

SHC-90 changes only the Operator and its deployment permissions. It does not
change Splunk Enterprise, Docker-Splunk, Ansible, probes, Pod termination, or
the SHC lifecycle state machine.

## Plan of Work

Create `internal/controller/enterprise/namespace_termination.go` with a small
reader-based function. It will perform a live-compatible GET by Namespace name
and return stop=true when the object has a deletion timestamp, reports
`NamespaceTerminating`, or is NotFound. Other errors will be wrapped with the
Namespace identity and returned. The function will log one structured
informational record only when a reconcile is being skipped.

At each of the seven v4 controller entry points, after fetching the CR and
before pause or Apply, invoke the helper only if the CR deletion timestamp is
nil. Return immediately when it says to stop. Do not publish a condition or
Event and do not requeue. Add a deletion-timestamp-change predicate to each
controller so the subsequent CR deletion transition deterministically reaches
Apply. If Namespace termination races between the preflight GET and API
admission, classify only the typed `NamespaceTerminating` cause as expected
cancellation before generic status/error handling. Leave each type's Apply and
finalizer code unchanged.

In `pkg/config/config.go`, preserve any existing client-cache options and add
`corev1.Namespace` to `DisableFor`. Add a focused standard Go test proving the
configuration in cluster-wide, single-namespace, and preconfigured-client
cases.

Add test-first controller coverage in a shared enterprise-controller test
file. Use a fake client containing a CR and a Namespace whose deletion
timestamp is already present. Stub every `Apply*` function and count status
subresource writes. For each active controller, expect an empty successful
result, zero Apply calls, and zero status writes. Separately test active,
terminating-phase, deletion-timestamp, NotFound, and read-error helper results.
Existing per-controller fake fixtures must include their active Namespace so
the new authoritative precondition does not accidentally short-circuit tests
that intend to exercise pause or Apply.

Add the kubebuilder `namespaces/get` marker and regenerate
`config/rbac/role.yaml` with `make manifests`. Update the Helm cluster-wide
manager ClusterRole, add the restricted namespace-reader ClusterRole and
binding for namespace-scoped mode, and add Helm unit tests for both modes.

After source qualification, use the Linux vWorkstation repository at
`~/splunk-complete/splunk-operator` to check out the exact source commit. Run
the same Make gates, build with `make docker-buildx` for `linux/amd64`, push an
immutable ECR tag, and record both OCI index and Linux manifest digests.

On EKS cluster `vivek-spl-301372`, deploy the exact Operator digest. Use a
disposable namespace containing at least a real referenced LicenseManager and
three-member SearchHeadCluster. Record the CR, workload, Service, PVC, and PV
state before deletion. Delete the Namespace and sample Namespace and CR
deletion timestamps closely. Audit the Operator log from the exact deletion
start for forbidden creates, namespace-termination admissions, status writes
after finalization, and Reconciler errors. Require natural finalization with no
manual patch and verify every expected workload, PVC, and delete-reclaim PV is
gone. Keep the retained SHC-85 fixture untouched.

## Concrete Steps

All local commands run from
`/Users/viveredd/Projects/splunk-operator-shc90-namespace-termination`.

First run focused test-first checks. The new controller guard tests must fail
against the starting source because each stubbed Apply function is called.
Then implement the guard and run:

    go test ./pkg/config
    go test ./internal/controller/enterprise
    make manifests
    make generate
    make fmt
    make vet
    make build
    make test
    make helm-check
    git diff --check
    git status --short

Use the repository Make targets as the source of truth if their exact command
expansion differs from the examples above. Do not hand-edit generated
`config/rbac/role.yaml`; regenerate it with `make manifests`.

The Linux checkout must identify the exact source commit before running:

    make test
    make build
    make helm-check
    make docker-buildx IMG=<immutable-ECR-tag> PLATFORMS=linux/amd64

EKS commands run through
`vworkstation.sok-search-head-v2.vivekr.coder` and always pass the exact
context:

    arn:aws:eks:us-west-2:667741767953:cluster/vivek-spl-301372

Use `kubectl get ... -o json` with `jq` for timestamps and counts, and use
`kubectl logs --since-time=<deletion-start>` for a bounded audit. Never infer
PV deletion only from absent PVCs; query each recorded PV name and then query
remaining PV claim references for the disposable namespace.

## Validation and Acceptance

Source acceptance requires a failing-before/passing-after regression for all
seven active v4 controller entry points. A terminating Namespace must produce
zero Apply calls, zero status writes, no error, and an empty reconcile result.
An active Namespace must allow ordinary Apply. NotFound must stop normally.
Authorization or transport error must stop mutation and return an error. A CR
whose own deletion timestamp is present must bypass the namespace guard so
existing deletion paths remain reachable; LicenseManager and
SearchHeadCluster retain their explicit no-post-finalization-status tests. A
typed `NamespaceTerminating` admission error after an active preflight must
return empty success with no status write for all seven controllers, while an
unrelated Forbidden error must not be suppressed.

Permission acceptance requires generated kustomize RBAC to grant only
Namespace `get`. Cluster-wide Helm output must grant Namespace `get` through
the manager ClusterRole. Namespace-scoped Helm output must render exactly one
supplemental ClusterRole restricted to the watched release namespace by
`resourceNames`, and exactly one uniquely named binding to the Operator service
account. It must not grant Namespace list, watch, create, update, patch, or
delete.

EKS acceptance requires a real Namespace deletion timestamp to precede at
least one contained CR deletion observation, or a controlled test hook that
holds CR deletion propagation without blocking the Namespace timestamp. No
normal Apply/create attempt may occur in that interval. The log must contain
zero Kubernetes `NamespaceTerminating` admission rejection and zero
controller-runtime Reconciler errors attributable to the fixture. Existing
finalization must finish naturally, with zero manual finalizer patches and no
remaining workload, PVC, or PV claim reference.

## Idempotence and Recovery

The shared check is read-only and can run repeatedly. A terminating or absent
Namespace yields no mutation and no timer. Retrying a transient read error is
safe because Apply has not run. The RBAC additions are declarative and can be
applied repeatedly.

The EKS namespace is disposable. Record PV names before deletion. If a test
harness fails, remove only resources whose exact names and namespace belong to
the SHC-90 campaign. Do not patch finalizers to make a failed acceptance run
appear successful; preserve the evidence, diagnose it, correct the source or
harness, and rerun in a fresh namespace. Restore the previous Operator digest
if the new controller fails unrelated health checks.

## Artifacts and Notes

Starting source history:

    8adad645f docs(shc): record SHC-89 qualification
    3e1716737 fix: persist valid status for paused v4 resources

Pre-existing EKS evidence from SHC-87 cleanup:

    LicenseManager Reconciler errors: 6
    SearchHeadCluster Reconciler errors: 9
    Cause: ConfigMap create rejected after Namespace termination began and
           before CR deletion timestamps were visible
    Finalization result: natural completion, ten PVCs and ten PVs removed

Exact SHC-90 commits, image digests, timestamps, counts, and cleanup evidence
are recorded in `SHC90NamespaceTerminationQualification.md`.

Local source commit:

    0c291c8c87ceb629bb573fcf036c6048c28cedf2

## Interfaces and Dependencies

The shared function accepts `context.Context`, a
`sigs.k8s.io/controller-runtime/pkg/client.Reader`, and a Namespace name. It
returns `(bool, error)`, where true means ordinary reconciliation must stop
without mutation. It depends only on `corev1.Namespace`, Kubernetes API error
classification, `types.NamespacedName`, and the existing structured logging
context.

`pkg/config.ManagerOptionsWithNamespaces` preserves its signature and adds
`&corev1.Namespace{}` to
`ctrl.Options.Client.Cache.DisableFor`. Controller constructors and Apply
function signatures did not change.

The Operator service account gains only live Namespace GET capability. No
Splunk management endpoint, runtime script, persistent data format, or custom
resource schema changes.

Revision note, 2026-08-01 UTC: created the SHC-90 plan after source, manager,
cache, RBAC, Helm, and prior EKS evidence inspection. The direct-read and
resource-name-restricted permission decisions were added because Namespace is
cluster-scoped and stale cached state would not close the observed race.

Revision note, 2026-08-01 UTC: added deletion-transition event acceptance after
the event-filter audit proved that a deletion timestamp does not change CR
generation. Added collision-safe cluster-scoped Helm resource names and
registered the separate `namespaceOverride`/`WATCH_NAMESPACE` compatibility
gap as SHC-92.

Revision note, 2026-08-01 UTC: recorded completed macOS and Linux Make gates,
the immutable linux/amd64 image, get-only EKS rollout, the live
Namespace-deleting/CR-not-deleting race, guard and finalizer log counts, natural
Namespace completion, and zero storage residue. Added the BuildKit repair and
namespace-controller condition delay as discoveries because they shaped the
repeatable qualification and cleanup interpretation.
