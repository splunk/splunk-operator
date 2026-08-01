# SHC-87 Referenced-Tier Dependency Status Qualification

## Purpose

This record explains and proves the bounded SHC-87 correction. Splunk tiers
are often submitted to Kubernetes independently, and an explicitly referenced
tier can be absent, starting, or still applying the requested image when its
dependent tier reconciles. Those states are normal declarative convergence.
They must not be reported as a failed upgrade or a terminal custom-resource
error.

The correction gives normal dependency convergence a stable Kubernetes status,
Event, log, and retry contract. It separately preserves a terminal result when
two custom resources explicitly request contradictory images. The contract is
used by SearchHeadCluster, ClusterManager, IndexerCluster, and
MonitoringConsole reconciliation.

## Result

Bounded OBS-001/OBS-004/OBS-005 and SHC-87 pass source qualification. Exact
Operator source `20d926658bdb7bd0a617a471acea1f83644149ce` passed all Linux
source gates and proves retryable and terminal classification with focused
tests.

The retryable path also passes EKS qualification. A three-member
SearchHeadCluster was created before its referenced LicenseManager. The SHC
reported `Pending`, `Ready=False`, and `Progressing=True`, all with reason
`DependencyNotReady`; retained the specific dependency message; emitted
aggregating Normal Events; and did not enter `Error`. After the LicenseManager
was created and became Ready, the SHC cleared the dependency message and
continued through its normal formation workflow without a Pod replacement or
container restart caused by the dependency wait.

The deliberately contradictory desired-image case is source-qualified and was
not imposed on the live EKS workload. This record therefore does not claim an
EKS terminal-mismatch test.

## Problem and classification boundary

Before SHC-87, some expected dependency states returned ordinary errors or a
false result with insufficient meaning. Callers could convert a starting
LicenseManager into `Phase=Error` and an upgrade-path validation failure even
though no user action was required. Other branches returned `false, nil`,
which stopped work without a durable reason. This made GitOps ordering and
slow Splunk startup look like product failure, and made support evidence
ambiguous.

The accepted classification is:

| Observed state | Classification | Required behavior |
|---|---|---|
| Referenced object does not exist yet | Retryable dependency convergence | Pending/Progressing, `DependencyNotReady`, bounded requeue |
| Referenced object exists but its phase is not Ready | Retryable dependency convergence | Pending/Progressing, include kind, namespace, name, and phase |
| Referenced object is Ready but its workload is not created yet | Retryable dependency convergence | Pending/Progressing, identify the absent workload |
| Dependency desired image agrees, but the workload still runs the previous image | Retryable dependency convergence | Pending/Progressing until the desired image reaches the workload |
| Dependency and dependent custom resources explicitly request different images | Terminal desired-state contradiction | terminal `UpgradeBlockedVersionMismatch` and Warning Event |
| Kubernetes API or data error is neither NotFound nor a known convergence state | Reconciliation error | preserve the underlying error; do not relabel it as dependency convergence |

A fixed elapsed-time failure was deliberately not introduced. Splunk startup,
MongoDB/KV Store work, scheduling, and storage attachment can legitimately
take different amounts of time, and Kubernetes object ordering is not bounded.
An arbitrary controller timeout would turn a healthy but slow deployment into
a false terminal failure. Sustained `DependencyNotReady` condition age, Event
age, and structured logs provide the signal needed for an external alerting or
policy layer. A future timeout, if required, must be an explicit product policy
with a configurable budget and a defined recovery action.

## Accepted status and observability contract

While a dependency is converging, the dependent custom resource records:

- `status.phase: Pending`;
- `status.observedGeneration` equal to the current generation;
- `Ready=False` with reason `DependencyNotReady`;
- `Progressing=True` with reason `DependencyNotReady`;
- `Stalled=False` unless an independent workflow has made it stalled;
- a status message naming the dependency and the observed wait state;
- a Normal Kubernetes Event with reason `DependencyNotReady`; and
- structured controller fields for dependency kind, namespace, name, phase,
  observed image, and desired image.

The reconcile returns normally and uses the controller's bounded requeue. It
does not return a synthetic error and does not publish a Warning Event for
normal ordering. The dependency message survives the status merge while the
condition remains active and clears on the first ordinary reconcile after the
dependency is ready.

A terminal image contradiction retains the existing
`UpgradeBlockedVersionMismatch` Warning Event and terminal error. SHC-87 does
not weaken that fail-closed boundary.

## Source scope and corrections

The typed dependency result is consumed by all current callers of upgrade-path
validation:

- SearchHeadCluster, including its Deployer phase;
- ClusterManager;
- both IndexerCluster reconciliation paths; and
- MonitoringConsole.

Direct LicenseManager and ClusterManager references honor an explicitly set
`ObjectReference.Namespace`, falling back to the dependent resource namespace
only when the reference namespace is empty.

The same audit corrected two reverse-reference defects in MonitoringConsole
validation. An IndexerCluster is now matched through
`spec.monitoringConsoleRef.name` instead of comparing the indexer name to the
MonitoringConsole name, and Standalone dependencies are listed with
`StandaloneList` instead of `IndexerClusterList`. Two Kubernetes API error
paths that previously returned `false, nil` now retain their actual errors.

## Source qualification

The isolated branch is `codex/shc-87-dependency-status`. The source commit is
`20d926658bdb7bd0a617a471acea1f83644149ce`.

Test-first coverage failed against the previous behavior, then passed after
the correction. The added regressions cover:

- missing and Pending LicenseManager references;
- a Ready LicenseManager whose workload still runs the previous image;
- contradictory dependency and dependent desired images;
- missing and Pending cross-namespace ClusterManager references;
- MonitoringConsole reverse references for ClusterManager,
  SearchHeadCluster, IndexerCluster, and Standalone;
- dependency conditions, Normal Event, and non-error reconcile result;
- dependency message persistence across status merging and clearing after
  recovery; and
- the previously hidden Kubernetes API errors.

The exact source passed locally:

- `make test-unit`;
- `make fmt`;
- `make vet`;
- `make build`; and
- `make test`: 41 suites, 157 specs, zero failures, 78.6 percent composite
  coverage.

The exact commit was then checked out on the Linux vWorkstation and passed:

- `make test`: 41 suites, 157 specs, zero failures, 78.5 percent composite
  coverage; and
- `make build`, followed by a clean Git worktree check.

## Immutable EKS inputs

- Date: 2026-08-01 UTC.
- EKS cluster:
  `arn:aws:eks:us-west-2:667741767953:cluster/vivek-spl-301372`.
- Disposable namespace: `shc87-dependency-status`.
- Operator source: `20d926658bdb7bd0a617a471acea1f83644149ce`.
- Operator image tag:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-87-20d926658`.
- Operator OCI index digest:
  `sha256:fbb1a53c45da509fee47edc618eefd93923fc3864df9533dc85dbcbc8914c2a3`.
- Linux/amd64 image manifest:
  `sha256:ee4bf98bfc9c0bb8b56327ee0ae8223c9849a19462cf582ce75736d78ec716d5`.
- Splunk runtime digest:
  `sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`.
- SearchHeadCluster: `shc87-shc`, three members.
- Referenced LicenseManager: `shc87`.

The Operator image was built and pushed from the Linux vWorkstation with the
repository's `make docker-buildx` target for `linux/amd64`. Qualification
updated only the manager container in the existing Operator Deployment and
pinned the OCI digest. It did not reinstall CRDs or disturb the retained
SHC-85 workload.

## EKS evidence

### Referenced object absent

The SearchHeadCluster was deliberately submitted before its referenced
LicenseManager. At `2026-08-01T06:06:29Z`, the first aggregated Event message
was:

    Waiting for LicenseManager dependency shc87-dependency-status/shc87: referenced object does not exist yet

The custom resource reported `Phase=Pending`, `DeployerPhase=Pending`, an
empty-error reconcile result, current observed generation, and these
conditions:

- `Ready=False`, reason `DependencyNotReady`;
- `Progressing=True`, reason `DependencyNotReady`;
- `Paused=False`, reason `NotPaused`; and
- `Stalled=False`, reason `NotStalled`.

The controller emitted a Normal Event rather than a Warning and logged the
dependency identity as structured fields. Repeated reconciles were aggregated
by Kubernetes rather than generating unbounded distinct reason strings.

### Referenced object Pending

After creating the LicenseManager, its StatefulSet and Pod entered ordinary
storage, container, Ansible, and Splunk startup. The SHC stayed Pending and its
message changed to identify the existing dependency's Pending phase and
desired image. The second Normal Event series reached count 15. The SHC did
not briefly enter Error and did not publish
`UpgradeBlockedVersionMismatch`.

The LicenseManager Pod became Kubernetes Ready with zero restarts. Its custom
resource reached Ready at `2026-08-01T06:09:46Z`; the monitor observed the
dependent SHC's next state by `2026-08-01T06:10:05Z`.

### Dependency recovery and SHC formation

As soon as the LicenseManager was Ready, the SearchHeadCluster changed from
`DependencyNotReady` to the normal `ReplicasNotReady` startup condition. Its
dependency status message cleared, `Stalled` remained false, and no user
action was required. The Deployer then became Ready and the Search Heads
continued through the image-owned first-formation sequence.

The first-formation workflow intentionally kept all Search Head client
endpoints unpublished while Splunk performed its accepted rolling restart.
Transient member-info HTTP 503, connection-refused, and timeout log entries
were scoped to those internal Splunk restarts. They did not produce a
dependency Error, a reconcile error, a container restart, or premature client
readiness.

The SearchHeadCluster conditions reached their final stable state at
`2026-08-01T06:20:39Z`; the monitor sampled the complete result at
`2026-08-01T06:20:41Z`:

- `Phase=Ready` and `DeployerPhase=Ready`;
- `ReadyReplicas=3` and three Kubernetes Ready Pods;
- three client Service endpoints, each targeting the current Search Head Pod
  UID;
- all three members registered with member status `Up`, captain status `Up`,
  and restart state `NoRestart`;
- `Ready=True/AllReplicasReady`, `Progressing=False/Stable`, and
  `Stalled=False/NotStalled`, all at observed generation one;
- an empty dependency status message; and
- zero container restarts on the LicenseManager, Deployer, and all three
  Search Heads.

The elected captain was ordinal zero in this final sample and reported
`service_ready_flag=true` and `initialized_flag=true`. Captain identity was
observed, not assumed or configured from ordinal identity. A direct
`makeresults` search returned HTTP 200 and the expected result on each Search
Head. Eight additional searches routed through the Kubernetes Search Head
Service between `06:23:22Z` and `06:24:06Z` all succeeded while every sample
retained exactly three endpoints.

The bounded pre-cleanup Operator log audit from `06:06:04Z` through the final
smoke window recorded 49 structured dependency-wait entries, zero
`UpgradeBlockedVersionMismatch` entries, and zero controller-runtime
`Reconciler error` entries. It also retained 52 member-info errors during the
image-owned internal rolling restart; their timing and targets matched the
members temporarily stopping and starting splunkd. All five container Ansible
recaps ended with `failed=0`. Pod probe Warning Events during those internal
restarts are expected runtime evidence and are not represented as dependency
failure.

### Cleanup and adjacent namespace-transition finding

The disposable namespace was deleted at `2026-08-01T06:24:37Z`. Kubernetes
removed all ten PVCs by `06:25:55Z`, all ten delete-reclaim PVs by
`06:26:16Z`, and the namespace naturally by `06:31:01Z`. No finalizer was
manually patched. The retained SHC-85 namespace and all four of its custom
resources remained Ready.

Cleanup exposed a separate timing window not covered by the earlier SHC-86
campaign. After the namespace had a deletion timestamp, but before deletion
timestamps were visible on the LicenseManager and SearchHeadCluster custom
resources, both reconcilers briefly followed their normal Apply paths and
attempted to recreate deleted ConfigMaps. Kubernetes correctly rejected those
requests because the namespace was terminating. The cleanup window contained
15 controller-runtime reconcile errors: six for LicenseManager and nine for
SearchHeadCluster. Once CR deletion became visible, their existing finalization
paths completed and all content disappeared.

This is not caused or fixed by dependency classification. It is registered as
SHC-90: normal reconciliation must also stop when the namespace is terminating,
including the propagation interval before a CR deletion timestamp is observed.

## Acceptance assessment

| Assertion | Evidence | Result |
|---|---|---|
| Missing reference is normal convergence | Pending conditions and Normal Event named the absent LicenseManager | Pass |
| Starting reference is normal convergence | Pending LicenseManager phase remained `DependencyNotReady`, never Error | Pass |
| Dependency detail survives status update | Specific message remained present across repeated status writes | Pass |
| Recovery clears dependency state | Ready LicenseManager caused ordinary `ReplicasNotReady`; message cleared | Pass |
| Retry does not restart workloads | All observed Kubernetes container restart counts remained zero | Pass |
| Contradictory desired images fail closed | Focused test retained terminal mismatch and Warning Event | Source pass; not imposed on EKS |
| Cross-namespace reference is honored | Focused missing/Pending ClusterManager coverage | Source pass |
| MonitoringConsole reverse references are correct | Focused ClusterManager, SHC, indexer, and Standalone coverage | Source pass |
| Full three-member SHC converges | Ready/Ready, 3/3 replicas, three endpoints, all members Up, zero restarts, direct member searches and 8/8 Service searches | Pass |

## Safe replay

Use a disposable namespace and a valid license Secret. Submit the dependent
tier first, and capture phase, message, conditions, Events, and controller logs
before creating the referenced tier:

    kubectl -n <namespace> get searchheadcluster <name> -o yaml
    kubectl -n <namespace> get events \
      --field-selector involvedObject.kind=SearchHeadCluster,involvedObject.name=<name>
    kubectl -n <operator-namespace> logs deployment/<operator-deployment> \
      -c manager --since=<bounded-window>

Create the dependency with the same desired image. Observe the absent-to-
Pending-to-Ready transition without editing either status. Verify that the
dependent message clears and that ordinary tier formation completes.

Do not use a production workload to test the contradictory-image branch. Its
source test is deterministic and proves the terminal classifier without
starting a deliberately inconsistent roll.

## Remaining boundaries

This is a bounded status-semantics correction, not a redesign of dependency
orchestration. The current SearchHeadCluster Apply order can create Services,
the Deployer, and Search Head workload objects before upgrade-path dependency
validation runs. SHC-87 makes that state truthful and retryable; it does not
promise that no dependent workload exists while a reference is absent.

The EKS campaign covers one same-namespace SearchHeadCluster-to-
LicenseManager ordering and recovery path. Cross-namespace references,
MonitoringConsole reverse dependencies, multisite previous-indexer ordering,
terminal desired-image contradiction, API outages, service mesh, custom TLS,
dual-stack networking, and long-duration external alert policy remain
source-only or separate scenarios.

No Docker-Splunk or Splunk Enterprise source change was required for this
classification. The member-info errors during first formation are a separate
observability concern in the existing SHC lifecycle and must not be attributed
to the dependency-status correction. The namespace-transition create race is
separately tracked by SHC-90 and means the broader namespace-first no-create
contract remains open even though cleanup completed naturally.
