# SHC-86 LicenseManager Namespace-First Finalization Qualification

## Purpose

This record explains and proves the bounded OPS-012/SHC-86 correction. A
LicenseManager can share a namespace with Search Heads that reference it. When
the namespace is deleted, Kubernetes marks the namespace and its namespaced
objects for deletion concurrently. From that point, the API server rejects
creation of replacement namespaced content. The LicenseManager must therefore
finish deletion without first running normal validation, migration, remote-app
initialization, Secret creation, or StatefulSet reconciliation.

Before this correction, ordinary LicenseManager reconciliation could run
after namespace termination began. In the qualification that registered
SHC-86, it attempted to recreate a Secret and retained its finalizer after the
Search Head workload and storage were already gone. That made a cross-resource
cleanup detail appear as a stuck Search Head namespace.

The corrected contract is deletion-first. A deleting LicenseManager performs
only cleanup that is valid in a terminating namespace, removes its declared
PVC finalizer without manual intervention, and returns without a status write
after successful finalization.

## Result

OPS-012/SHC-86 passes bounded source and EKS qualification at exact Operator
source `61b35aabffe312d85d3c0def8257696bbcd97af6` on branch
`codex/shc-86-license-finalization`.

Two disposable EKS campaigns were used deliberately:

- a synthetic adversarial fixture proved that a paused LicenseManager with
  invalid normal-reconcile configuration, absent shared objects, and a real
  delete-PVC finalizer can finalize after namespace termination; and
- a real LicenseManager fixture proved the same path with a Ready Splunk Pod,
  a StatefulSet, three Secrets, two Services, two bound EBS claims, two bound
  PVs, and a paused SearchHeadCluster custom resource that referenced the
  LicenseManager.

The adversarial namespace completed deletion in 14 seconds. In the real
fixture, both custom resources were absent by the six-second observation, the
LicenseManager Pod completed graceful termination at approximately 50
seconds, and both PVCs then disappeared. Both delete-reclaim PVs were absent at
final storage verification. The namespace controller removed its own finalizer
at 337 seconds without a manual patch. Operator logs contained zero forbidden
create failures, zero LicenseManager reconcile errors, and zero
post-finalization status errors in either deletion window.

## Kubernetes contract

The accepted contract has six parts.

First, a non-nil custom-resource deletion timestamp takes precedence over the
pause annotation and every ordinary reconciliation stage. Pause prevents
desired-state work; it must not prevent deletion finalization.

Second, deletion does not depend on normal-spec validation. A malformed or
obsolete normal-reconcile input must not make a namespace undeletable once the
API server has already accepted the object and namespace deletion has begun.

Third, finalization issues no Kubernetes `Create` operation. Removal from an
existing Monitoring Console environment ConfigMap and cleanup of an existing
App Framework coordination ConfigMap are allowed, but absence means cleanup is
already complete. Secret and StatefulSet owner-reference cleanup is also
best-effort because the namespace controller may have deleted those objects
first.

Fourth, the registered `enterprise.splunk.com/delete-pvc` callback remains the
authority for the LicenseManager's declared PVC cleanup. It selects only claims
whose instance label matches `splunk-<name>-license-manager`.

Fifth, a successful finalizer update is the last write to the custom resource.
The reconciler and controller must not race deletion with a deferred status
refresh or generic stalled-condition write. If finalization fails, the
finalizer remains and error/status reporting stays available for retry and
diagnosis.

Sixth, LicenseManager finalization time and namespace deletion time are
different measurements. The Operator can remove the custom-resource finalizer
quickly while kubelet, PVC protection, CSI reclaim, and the namespace
controller continue their independent work. Support evidence must retain those
stage boundaries instead of attributing the complete namespace duration to the
Operator.

## Source behavior

The LicenseManager Apply path now checks deletion immediately after initializing
its result and phase helpers. A deleting object is routed to a bounded cleanup
function before:

- LicenseManager spec validation;
- App Framework status migration;
- remote object-store client initialization;
- normal Splunk configuration reconciliation;
- Service or StatefulSet desired-state work; and
- license-health observation.

The bounded cleanup path sets the in-memory phase to `Terminating`, removes
only existing cross-resource references, tolerates already-absent Secret,
StatefulSet, and shared ConfigMap objects, invokes the registered finalizers,
and suppresses status refresh after successful finalizer removal.

The LicenseManager controller treats the pause annotation as effective only
while the object is not deleting. After successful deletion reconciliation it
returns before the generic condition writer. These controller rules are
required in addition to the Apply-path ordering; correcting only one layer
would leave either a pause barrier or a post-finalization write race.

## Source qualification

The focused tests cover:

- deletion before normal validation with deliberately invalid App Framework
  configuration;
- deletion while the supported LicenseManager pause annotation is true;
- an induced error for every client `Create` call, proving that the deletion
  path makes none;
- a present PVC and the registered delete-PVC finalizer;
- absent Monitoring Console, App Framework, Secret, and StatefulSet objects;
- no LicenseManager status refresh after successful finalizer removal;
- controller routing of a deleting paused LicenseManager with zero status
  writes after successful finalization.

The exact source passed on the Linux vWorkstation:

- `make test`: 41 suites, 157 specs, zero failures, and 78.6 percent composite
  coverage;
- `make build`; and
- a clean generated/source tree after the Make targets.

The isolated local worktree also passed `make test-unit`, `make fmt`,
`make vet`, `make build`, and `git diff --check`. The repository's standalone
`make test-integration` target currently points Ginkgo at the non-recursive
`./internal/controller` path and finds no suite there; the canonical
`make test` target is the accepted recursive controller test gate.

## Immutable EKS inputs

- Date: 2026-08-01 UTC.
- EKS cluster:
  `arn:aws:eks:us-west-2:667741767953:cluster/vivek-spl-301372`.
- Operator source:
  `61b35aabffe312d85d3c0def8257696bbcd97af6`.
- Operator image tag:
  `667741767953.dkr.ecr.us-west-2.amazonaws.com/vivek/splunk/splunk-operator:shc-86-61b35aabf`.
- Operator image index digest:
  `sha256:635d60fecdd203e7d158fb1f95c57d46c7062ed98b156caf8dc68da7515812ec`.
- Linux amd64 manifest digest:
  `sha256:fe2299581d9e8fc73ed4e89abfb598813a98a0ff307db8b4e8b3c1f9bcfa2605`.
- Splunk runtime digest:
  `sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`.
- Synthetic namespace: `shc86-license-finalization`.
- Real namespace: `shc86-license-finalization-real`.

The Operator image was built and pushed on the Linux vWorkstation through the
repository's `make docker-buildx` target for `linux/amd64`. ECR and Buildx both
reported the same immutable index digest and the expected platform. The EKS
Deployment was updated directly to that digest; the deployment rollout reached
one of one Ready replicas. The existing SHC-85 namespace was not modified.

## EKS evidence: adversarial finalizer fixture

The first fixture contained:

- a LicenseManager named `shc86` with UID
  `82aff9db-1b2a-451b-9ba2-5220dbb0d2d1`;
- the supported pause annotation set to `true`;
- the real `enterprise.splunk.com/delete-pvc` finalizer;
- an App Framework source that referenced a missing volume;
- a Monitoring Console reference whose object and ConfigMap were absent; and
- a Pending PVC labeled for the LicenseManager finalizer.

No StatefulSet or Splunk Pod was allowed to form. Namespace deletion began at
`2026-08-01T04:53:11Z`. The LicenseManager retained its finalizer while the
namespace first entered `Terminating`, then disappeared by the six-second
sample. Its PVC disappeared in the same interval and the namespace was absent
at 14 seconds.

The finalization log recorded absent shared objects as completed cleanup,
processed the registered delete-PVC callback, removed the finalizer, and
reported deletion complete. The bounded log audit found:

- zero namespace-termination create failures;
- zero post-finalization status errors; and
- zero reconciler errors.

This is the strongest control-flow proof because normal validation and any
attempted resource creation would have prevented the finalizer from being
removed.

## EKS evidence: real referenced LicenseManager

The second fixture formed a real LicenseManager named `shc86-real`. It reached
`Ready` at `2026-08-01T04:58:35Z` after Ansible completed with `ok=111`,
`failed=0`. The Pod was Kubernetes Ready with zero restarts. The fixture then
added a paused SearchHeadCluster custom resource named `shc86-reference` whose
`licenseManagerRef.name` was `shc86-real`, and paused the already-Ready
LicenseManager.

The exact pre-deletion resource inventory was:

| Object | Name or UID | Precondition |
|---|---|---|
| LicenseManager | UID `a8cfc994-d371-4836-86ce-7560b3c022db` | `Ready`, paused, delete-PVC finalizer present |
| SearchHeadCluster reference | UID `c9972634-6ceb-4d5a-81cb-6b2212157d86` | paused, references `shc86-real`, delete-PVC finalizer present |
| StatefulSet | UID `b242dd1e-5c0b-47a6-b87b-f05160c8c460` | one Ready Pod |
| Pod | UID `acbc6025-273d-44d7-b06a-79a291e1c05f` | Running, Ready, zero restarts |
| License input Secret | UID `33e4b94a-5cc2-4e56-b127-66f0f135fe0d` | present |
| Namespace Secret | UID `b025f872-ac87-409e-9652-b8b8ed4760fd` | present |
| Versioned workload Secret | UID `c96156ea-ffd8-40a1-af10-87e2d477cd5d` | present |
| Headless Service | UID `3ebce01f-8f81-47e1-bcf9-7163d71cbfb8` | present |
| Regular Service | UID `aef0232a-0019-4954-b78c-02d70c04b6b7` | present |
| etc PVC/PV | PVC UID `8177319e-396c-4c3b-ad70-006de2afc27d`; PV UID `d0682f6b-4158-456a-b0e8-6b289af51007` | 10 Gi, Bound, reclaim `Delete` |
| var PVC/PV | PVC UID `a55c6ccb-daed-4b7e-9f0b-a9bc5052e1ee`; PV UID `72587623-9089-402c-9efc-af55feac5ce5` | 10 Gi, Bound, reclaim `Delete` |

Namespace deletion began at `2026-08-01T05:00:04Z`. At
`2026-08-01T05:00:10Z`, one LicenseManager reconcile performed only existing-
object cleanup, logged both exact PVC deletions, processed the registered
finalizer, removed it, and logged deletion complete. Both custom resources
were absent by the six-second sample without removing their pause annotations
or manually editing a finalizer.

The Pod received its normal 1,200-second termination budget but exited at
approximately 50 seconds. PVC protection retained both claims while the Pod
was still terminating; after Pod exit, both claims disappeared. Both
delete-reclaim PV objects were absent at final storage verification.

The namespace controller retained stale status conditions from its first
inventory after all content had disappeared. It refreshed and removed its own
`kubernetes` finalizer without intervention at `2026-08-01T05:05:41Z`, 337
seconds after the request. This tail is Kubernetes namespace-controller retry
latency, not LicenseManager finalizer time: the LicenseManager finalizer had
completed at six seconds and a complete namespaced-resource inventory was
already empty before the namespace controller's final refresh.

The bounded Operator log audit found:

- both labeled PVC deletion requests;
- one LicenseManager finalizer removal and deletion-complete record;
- zero forbidden-create failures;
- zero post-finalization status errors; and
- zero LicenseManager reconciler errors.

## Acceptance assessment

| Assertion | Evidence | Result |
|---|---|---|
| Deletion bypasses pause | Both paused LicenseManagers finalized; real referenced CR absent by six seconds | Pass |
| Deletion bypasses invalid normal configuration | Synthetic fixture used an unresolved App Framework volume and absent references | Pass |
| Deletion performs no client Create | Intercepted source test fails any Create; both EKS log windows had zero create/namespace-termination failure | Pass |
| Already-absent cleanup is idempotent | Synthetic shared ConfigMap, Secret, and StatefulSet absence completed without error | Pass |
| Declared PVC cleanup executes | Real fixture logged deletion of both exact labeled PVCs | Pass |
| Successful finalization has no status-write race | Source interception and both EKS windows recorded zero post-finalization status error | Pass |
| Failure remains retryable and diagnosable | Source control flow suppresses status only when finalization returns success | Code-reviewed; failure injection remains an additional test |
| Real workload and storage disappear | Pod, StatefulSet, Services, Secrets, both PVCs, and both delete-reclaim PVs absent | Pass |
| Namespace requires no force-finalization | Synthetic namespace gone in 14 seconds; real namespace gone in 337 seconds | Pass |

## Safe replay

Use a dedicated namespace. Record the Operator digest, custom-resource UIDs,
pause annotations, finalizers, owned workload UIDs, PVC UIDs, PV UIDs, reclaim
policies, Pod readiness, and container restart counts before deletion.

Delete the namespace normally:

    kubectl delete namespace <qualification-namespace> --wait=false

Observe the custom resources separately from the namespace:

    kubectl -n <qualification-namespace> get \
      licensemanager,searchheadcluster,pod,pvc
    kubectl get namespace <qualification-namespace> -o yaml

Audit Operator logs from the exact deletion timestamp. Require the registered
finalizer callback and deletion-complete record. Reject any resource-create
attempt after namespace termination, status update after successful finalizer
removal, unrecognized finalizer, finalizer conflict, or reconciler error.

After the namespace disappears, query every recorded PV name explicitly. Do
not treat the absence of PVCs alone as proof of the configured storage policy.
Do not patch the LicenseManager, SearchHeadCluster, or namespace finalizers; a
campaign requiring such a patch has failed the contract.

## Newly observed adjacent requirement

Creating a LicenseManager with the pause annotation already set exposed a
separate status-initialization defect. The controller attempted to write a
Paused condition while `status.phase` was still empty. The v4 status schema
rejected the write because an empty phase is not an allowed value, and the
controller retried noisily until deletion began. The paused SearchHeadCluster
reference reproduced the same defect for its empty `status.phase` and
`status.deployerPhase`. This did not block SHC-86 because deletion correctly
bypassed pause, and it was not observed when an already-Ready LicenseManager
was paused.

SHC-89 records the shared requirement: every supported custom resource created
already paused must initialize all required phase fields and a schema-valid
Paused condition exactly once, must not create its desired workload, and must
not enter an error retry loop. SHC-86 did not implement or qualify that
separate requirement; it was later completed in the bounded
`SHC89PausedStatusQualification.md` record.

## Later namespace-transition boundary

SHC-87 cleanup on 2026-08-01 exposed an earlier timing window that the two
SHC-86 fixtures did not exercise. Kubernetes can mark the namespace
terminating before its deletion propagation has placed a deletion timestamp
on each contained custom resource. During that interval, both LicenseManager
and SearchHeadCluster briefly entered normal reconciliation and attempted
ConfigMap creation. Kubernetes rejected the requests because the namespace was
already terminating. Existing CR deletion and finalization then completed,
and all content disappeared without a patch.

SHC-90 owns the broader requirement to guard normal reconciliation from
authoritative namespace-termination state even before the CR deletion
timestamp is visible. The no-create result in this record remains valid for
the two exact SHC-86 campaigns, but it must not be generalized across that
newly observed propagation window.

## Remaining boundaries

This record qualifies the current v4 LicenseManager namespace-first path on
one EKS/Kubernetes/storage combination. It does not qualify retained-PVC
policy, a CSI delete failure, an unavailable API server during finalizer
update, conflicting third-party finalizers, every cloud provider, every
supported Kubernetes version, or every legacy LicenseMaster API path.

The SearchHeadCluster reference object was intentionally paused so the
campaign could isolate LicenseManager finalization rather than create another
three-member SHC. SHC-81 separately qualifies deletion-first finalization of a
healthy three-member Search Head Cluster. The combined evidence proves the
cross-resource namespace contract without attributing Kubernetes
namespace-controller or CSI tail time to Splunk shutdown.

No Docker-Splunk or Splunk Enterprise source change was required for SHC-86.
The real workload's prompt exit inside its configured grace budget is retained
as integration evidence, but the correction itself is entirely in the
Operator's Kubernetes reconciliation and finalization ordering.
