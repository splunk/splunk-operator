# Operator Lifecycle Contracts Technical Design

Status: Wave 0 spike contract with audited search-drain continuation qualified
on EKS.

This document defines the API and status contracts that the parallel
implementation branches consume. It intentionally does not select concrete
controller functions or implement the StatefulSet rollout algorithm. Those
details remain owned by the Pod-lifecycle, SHC-orchestrator, observability, and
RollingUpdate workstreams.

The design baseline is GitLab
`sok/develop@39316c19fb990f1af84966d5269a8f4116550dbb`.

## Compatibility principles

Existing SearchHeadCluster resources must continue using the current
`OnDelete` behavior after an Operator upgrade. Adding the API types alone must
not change a Pod template, create a new StatefulSet revision, or start a
lifecycle operation.

Every new spec field is optional. Pointer-valued integer fields distinguish
omission from an explicitly supplied value. The CRD schema validates explicitly
supplied values, but it does not use Kubebuilder default markers for the spike.
Resolved defaults are applied in controller policy only when the owning feature
gate is enabled.

This distinction is required for safe migration. If admission inserted
`terminationGracePeriodSeconds: 1200` into every existing object while the
feature was disabled, an Operator upgrade could make existing resources appear
opted in and could create a new Pod-template revision.

## Feature gates

Add two disabled-by-default Alpha feature gates in `pkg/config/featuregates.go`.

`SplunkPodLifecycle` owns the common Splunk workload Pod contract, beginning
with configurable termination grace. It is separate from Search Head
orchestration because the common grace field applies to every StatefulSet-based
Splunk Enterprise workload.

`SearchHeadClusterLifecycle` owns Search Head-specific durable orchestration,
captain transfer, drain/rejoin policy, and the opt-in update strategy. For the
spike, enabling `SearchHeadClusterLifecycle` requires
`SplunkPodLifecycle=true`. Startup or reconciliation must report a clear
configuration error when the dependency is missing.

When `SplunkPodLifecycle` is disabled:

- an omitted common termination-grace field preserves current Pod behavior;
- an explicitly configured field is rejected at admission when the centralized
  validation webhook is enabled;
- reconciliation remains behaviorally gated even when that optional webhook is
  not enabled; and
- no Pod-template change is produced by this feature.

When `SearchHeadClusterLifecycle` is disabled:

- an omitted lifecycle policy preserves current behavior;
- a non-empty lifecycle policy is rejected at admission when the centralized
  validation webhook is enabled;
- reconciliation remains behaviorally gated even when that optional webhook is
  not enabled; and
- existing SHC status continues without a new active lifecycle operation.

## Captain identity and bootstrap compatibility

StatefulSet ordinal identity and SHC captain identity are different facts.
Ordinal zero remains a stable compatibility seed for first-time formation, but
the Operator never treats it as the operational captain. Captain transfer,
rollout admission, captain-unavailable handling, and recovery use the captain
reported by Splunk member/captain APIs.

The current image contract still consumes an environment variable named
`SPLUNK_SEARCH_HEAD_CAPTAIN_URL`. Until that cross-repository interface is
versioned or renamed, the Operator may render ordinal zero into it only as
bootstrap discovery input. The runtime must not reinterpret that value as
durable captaincy on a persistent restart.

For Kubernetes SHCs, the Operator resolves
`SPLUNK_PREFERRED_CAPTAINCY=false` unless the customer has explicitly supplied
a supported alternative. This prevents bootstrap identity from becoming an
ongoing election preference. Production design review must decide whether the
generic environment override remains the supported interface or is replaced
by a typed policy field.

Operator-owned App Framework and bundle actions select a currently reachable,
qualified member from observed SHC status. They do not store ordinal zero or a
previous captain as a durable target. Image-owned deployer operations have the
same requirement through the runtime contract.

## Customer spec contract

### Common workload termination grace

Add the following optional field to `CommonSplunkSpec` in
`api/enterprise/v4/common_types.go`:

    TerminationGracePeriodSeconds *int64 `json:"terminationGracePeriodSeconds,omitempty"`

Schema contract:

- minimum explicit value: 1 second;
- maximum explicit value: 86400 seconds;
- omitted value: no customer override;
- resolved spike default when `SplunkPodLifecycle` is enabled: 1200 seconds;
- gate disabled and omitted: preserve the existing Kubernetes behavior; and
- gate disabled and supplied: validation error.

The upper bound is a safety bound for the spike, not evidence that a Pod should
normally take one day to stop. Qualification must recommend the release range
from measured stage durations.

Because the field is part of `CommonSplunkSpec`, it applies consistently to
SearchHeadCluster, IndexerCluster, ClusterManager, LicenseManager,
MonitoringConsole, Standalone, and their compatibility aliases that embed the
common type. The Pod-lifecycle workstream owns proving that each rendered
StatefulSet receives the resolved value.

### Search Head lifecycle policy

Add an optional pointer to `SearchHeadClusterSpec` in
`api/enterprise/v4/searchheadcluster_types.go`:

    LifecyclePolicy *SearchHeadClusterLifecyclePolicy `json:"lifecyclePolicy,omitempty"`

Define:

    type SearchHeadClusterLifecyclePolicy struct {
        PodUpdateStrategy              SearchHeadClusterPodUpdateStrategy `json:"podUpdateStrategy,omitempty"`
        SearchDrainTimeoutSeconds      *int64 `json:"searchDrainTimeoutSeconds,omitempty"`
        CaptainTransferTimeoutSeconds *int64 `json:"captainTransferTimeoutSeconds,omitempty"`
        MemberRejoinTimeoutSeconds     *int64 `json:"memberRejoinTimeoutSeconds,omitempty"`
    }

`PodUpdateStrategy` has two values:

- `OnDelete`, which retains the current StatefulSet rollout owner; and
- `RollingUpdate`, which requests the future partition-gated strategy.

Empty strategy resolves to `OnDelete`. `RollingUpdate` is accepted only while
`SearchHeadClusterLifecycle` is enabled. The later rollout workstream may add a
stricter compatibility check for runtime image capability.

Every explicit timeout has a schema range of 1 through 86400 seconds. The spike
controller resolves omitted values to:

- search drain: 180 seconds;
- captain transfer: 180 seconds; and
- member rejoin: 1800 seconds.

These are experimental spike defaults. They are deliberately represented as
controller constants rather than CRD defaults so qualification can change the
release recommendation without mutating stored customer objects. The
qualification plan measures historical and real-time drain behavior
separately.

The default timeout action remains fail closed. A search-drain timeout blocks
destructive progression and records `Blocked/SearchDrainTimedOut`. It also
publishes an opaque, operation-scoped continuation token in status. The token
is not a credential and does not replace Kubernetes authorization. Permission
to update the SearchHeadCluster remains the authority; the token prevents an
approval from being supplied before the timeout or reused for another
operation.

### Audited search-drain continuation

`SearchHeadClusterSpec` has one optional approval object:

    LifecycleApproval *SearchHeadClusterLifecycleApproval `json:"lifecycleApproval,omitempty"`

The object contains the exact blocked `operationID`, the 64-character
lowercase hexadecimal token issued in
`status.lifecycleOperation.searchDrainContinuationToken`, and the single
supported action `ContinueAfterSearchDrainTimeout`. Admission validates the
shape and feature-gate dependency. A structurally valid approval with the
wrong operation ID or token is ignored by reconciliation and leaves the
operation blocked.

The controller accepts an approval only when all of the following facts are
true:

- the current operation is `Blocked/SearchDrainTimedOut`;
- operation ID and token both match;
- the target Pod UID has already been captured;
- no approval or replacement authorization was previously recorded; and
- the target member is present in the latest refreshed SHC observation.

Acceptance is a durable persistence barrier. The controller records approval
time, CR generation, and the active historical and real-time search counts
observed at approval, emits one Normal
`SHCSearchDrainContinuationApproved` Event, increments
`splunk_operator_shc_search_drain_continuation_approval_total`, and returns
without authorizing replacement. A later reconciliation may skip only the
active-search count wait. It still refreshes and validates cluster health,
KV Store state, detention, captain identity and readiness, target Pod
identity, and captain transfer before it can advance the StatefulSet
partition.

The approval object is controller input, not Pod-template configuration.
Adding, correcting, or leaving a stale approval must not create a StatefulSet
revision. Lifecycle operation IDs include the CR generation when an operation
starts, so an approval left in spec cannot match or authorize a later ordinal,
revision, or scale operation. The same exact handshake applies to replacement
intents that use the search-drain stage; it does not continue a different
timeout class or the cluster-deletion workflow.

### Example opt-in

The initial integration-spike resource uses:

    spec:
      terminationGracePeriodSeconds: 1200
      lifecyclePolicy:
        podUpdateStrategy: OnDelete
        searchDrainTimeoutSeconds: 180
        captainTransferTimeoutSeconds: 180
        memberRejoinTimeoutSeconds: 1800

The Operator deployment enables:

    SplunkPodLifecycle=true
    SearchHeadClusterLifecycle=true

The `RollingUpdate` value is not used until the integrated lifecycle passes
under `OnDelete`.

## Durable status contract

Add one optional operation object to `SearchHeadClusterStatus`:

    LifecycleOperation *SearchHeadClusterLifecycleOperationStatus `json:"lifecycleOperation,omitempty"`

Only the current operation and most recent terminal result are required for the
spike. The orchestrator must not append unbounded history to the CR.

Define an operation status containing:

    OperationID
    Intent
    DesiredRevision
    TargetPod
    TargetOrdinal
    Stage
    StartedAt
    StageStartedAt
    LastTransitionTime
    CompletedOrdinals
    RetryCount
    Reason
    Message
    Captain
    CaptainReady
    ActiveHistoricalSearches
    ActiveRealtimeSearches
    LastSuccessfulSHCObservation

Time fields use `*metav1.Time`. `TargetOrdinal` uses `*int32` so ordinal zero is
not confused with an unset field. Counts use non-negative integers. The
message is bounded diagnostic text and must not contain credentials, search
text, or arbitrary REST response bodies.

### Intent values

Define:

- `PodUpdate`;
- `ScaleDown`;
- `ClusterDeletion`; and
- `Recovery`.

Ordinary Pod replacement uses `PodUpdate` and never removes consensus
membership. Permanent replica reduction uses `ScaleDown`. Complete resource
deletion and recovery remain distinct so later code cannot infer destructive
membership intent only from a missing or restarting Pod.

`ClusterDeletion` records whole-resource finalization and does not run
per-member detention, captain transfer, recycle, or consensus removal.
Kubernetes owner-reference deletion remains responsible for the workload
resources. The existing optional `enterprise.splunk.com/delete-pvc` finalizer
continues to express whether the Operator removes associated PVCs; without
that finalizer, persistent storage is retained.

### Stage values

Define:

- `ValidatingCluster`;
- `DetainingTarget`;
- `DrainingSearches`;
- `TransferringCaptain`;
- `AuthorizingReplacement`;
- `WaitingForTermination`;
- `WaitingForScheduling`;
- `WaitingForStorage`;
- `WaitingForContainer`;
- `WaitingForMemberRejoin`;
- `ValidatingRecovery`;
- `FinalizingClusterDeletion`;
- `Completed`;
- `Blocked`; and
- `Failed`.

Stages are stable API values. Renaming a stage later requires compatibility
review because dashboards, alerts, tests, and support tooling consume them.

### Reason values

Define a typed, bounded reason vocabulary. Wave 0 includes:

- `OperationStarted`;
- `ClusterNotSafe`;
- `ObservationStale`;
- `ConflictingCaptainObservation`;
- `DetentionRequested`;
- `SearchesActive`;
- `SearchDrainTimedOut`;
- `CaptainTransferRequired`;
- `CaptainTransferTimedOut`;
- `InitialFormationPending`;
- `CaptainUnavailable`;
- `ReplacementAuthorized`;
- `PodTerminationTimedOut`;
- `PodUnschedulable`;
- `VolumeAttachmentPending`;
- `ImagePullFailed`;
- `SplunkStartupFailed`;
- `MemberNotRegistered`;
- `MemberNotUp`;
- `MemberIdentityMismatch`;
- `MemberSynchronizationPending`;
- `MemberRejoinTimedOut`;
- `RecoveryValidated`;
- `ClusterDeletionRequested`;
- `OperationCompleted`; and
- `UnsupportedRuntimeContract`.

Implementation branches may propose an additional reason only by updating this
contract and adding its status/Event/log/metric/test mapping.

The partition coordinator uses a related bounded rollout-decision vocabulary.
These values explain why Kubernetes partition progression is stable, waiting,
authorized, or blocked and must map to status, Events, logs, metrics, and
scenario assertions:

- `Stable`;
- `Paused`;
- `InitialFormationPending`;
- `CaptainUnavailable`;
- `RollbackPending`;
- `WaitingForRevision`;
- `PrepareTarget`;
- `PartitionAdvanceAuthorized`;
- `WaitingForKubernetes`;
- `WaitingForRecovery`;
- `TooManyUnavailable`;
- `ExistingUnavailablePod`;
- `MemberRecoveryPending`;
- `OutOfOrderRevision`;
- `ConflictingLifecycleOperation`;
- `LifecycleBlocked`; and
- `InvalidState`.

The rollout vocabulary does not replace lifecycle-operation reasons. It
describes the partition decision made from Kubernetes and durable lifecycle
observations, while the lifecycle reason describes the underlying Splunk
operation.

### StatefulSet observation, revision reuse, and cancellation handoff

The StatefulSet API is asynchronous. After the Operator applies a Pod-template
change, the desired spec can be visible before the StatefulSet controller has
published the corresponding `status.updateRevision`. Rollout planning must not
combine those two different observation times.

Before selecting a target, changing a partition, or classifying Pod revisions,
the controller checks both of these barriers:

- no StatefulSet apply remains pending in the current reconciliation; and
- `StatefulSet.metadata.generation` is not greater than
  `StatefulSet.status.observedGeneration`.

If either barrier is closed, the bounded rollout decision is
`Wait/WaitingForRevision`. That decision starts no lifecycle operation, changes
no partition, deletes no Pod, and emits no rollout-block warning. A later
reconcile replans only after the StatefulSet controller has observed the
generation.

`currentRevision == updateRevision` means the StatefulSet controller has one
current desired ControllerRevision. It does not prove that every Pod already
carries that revision. Kubernetes can reuse a previous ControllerRevision when
a Pod-template change is withdrawn. During that rollback, current and update
revision can become equal while higher ordinals still carry the withdrawn
revision. The coordinator therefore:

- observes the `controller-revision-hash` on every desired ordinal;
- keeps reverse-ordinal ordering and the one-unavailable invariant;
- does not classify an untouched lower ordinal that already matches the
  desired revision as out of order while a higher ordinal is still rolling
  back; and
- declares completion only after every required Pod revision and recovery
  invariant is satisfied.

A desired revision withdrawn before replacement authorization is an in-place
cancellation only while the original target Pod UID still exists and is not
deleting. The durable lifecycle continues to own that Pod through detention
release, refreshed local and captain observations, and Splunk recovery.
Lifecycle `Completed` alone does not release ownership: the controller waits
until the same Pod is also Kubernetes Ready, serving, registered, and `Up`.
Only then may it select a rollback target.

Withdrawal after replacement authorization is not an in-place cancellation.
The authorized target must first reach a known recovered or classified terminal
state under its original durable operation. No second target may begin during
that handoff. The subsequent desired revision is then rolled back or queued by
a new deterministic planning decision. `STS-014` owns qualification of this
separate path.

## Kubernetes condition contract

Continue using the existing `[]metav1.Condition` list. Add condition types only
when the lifecycle feature gate is enabled:

- `SearchHeadClusterReady`;
- `TrafficReadyMembers`;
- `CaptainReady`;
- `MembersReady`;
- `RolloutInProgress`;
- `RolloutBlocked`;
- `MemberDraining`;
- `CaptainTransferInProgress`;
- `MemberRejoining`;
- `Degraded`; and
- `TerminalFailure`.

Conditions use Kubernetes transition semantics: update
`LastTransitionTime` only when status changes, preserve
`ObservedGeneration`, use the typed reason vocabulary, and keep messages
human-readable and sanitized.

`SearchHeadClusterReady` is cluster-level status. It must not be copied into
each Pod's readiness result. `TrafficReadyMembers` communicates a count in its
message/status context until a dedicated numeric status field is approved.

## Validation contract

Extend `pkg/splunk/validation/common_validation.go` for the common grace field
and `pkg/splunk/validation/searchheadcluster_validation.go` for lifecycle
policy.

Validation tests cover:

- omitted fields with gates disabled and enabled;
- explicit minimum, maximum, below-minimum, and above-maximum values;
- every strategy enum value and an unknown value;
- lifecycle policy supplied while its gate is disabled;
- termination grace supplied while its gate is disabled;
- SHC lifecycle gate enabled without the Pod lifecycle dependency;
- all timeout values independently different;
- update from omitted to explicit and explicit to omitted; and
- existing resources remaining valid when no new field is supplied.

Kubebuilder schema validation and Go validation must agree. Generated CRDs must
contain the same numeric bounds and enum values.

The centralized validation webhook is independently feature-gated in the
current Operator. Therefore schema-invalid values are always rejected by the
Kubernetes API server, while gate-combination admission errors require
`ValidationWebhook=true`. Controller branches must still gate every behavioral
entry point; they must never rely on admission as the only enforcement layer.

## Versioning decision

The spike adds customer configuration only to the v4 storage API. v3 continues
to read existing common/status fields and must not silently lose a v4 lifecycle
policy through an unsupported round trip. Before production delivery, the team
must choose one of:

1. add equivalent fields and conversion preservation to v3; or
2. formally require v4 for the capability and reject incompatible conversion.

For Wave 0, API-006 remains an explicit blocking test and no production
enablement is claimed until the conversion decision is implemented.

## Ownership boundaries

The contracts branch owns:

- feature-gate declarations and tests;
- API types and generated deep-copy/CRD artifacts;
- validation and validation tests;
- resolved-policy types and default-resolution unit tests;
- Helm exposure of Operator feature gates and customer fields where required;
  and
- API documentation.

It does not edit probe scripts, implement `preStop`, add Splunk REST calls,
implement the lifecycle state machine, change StatefulSet strategy, advance a
partition, or add production metrics.

## Acceptance trace

The contracts branch must automate:

- API-001 through API-008 from `SHCTestScenarioMatrix.md`;
- compilation of every CR that embeds `CommonSplunkSpec`;
- generated-file cleanliness;
- feature gates disabled by default;
- no Pod-template change when gates are disabled and fields are omitted; and
- stable JSON round-trip for all new v4 types.

The last Pod-template assertion may use a focused unit test owned jointly with
the Pod-lifecycle workstream after the contracts merge. The contracts branch
must at least prove that default resolution is never invoked through a disabled
gate.

## Open production decisions

The spike can proceed with the contracts above, but production enablement still
requires measured timeout/default recommendations, the v3 compatibility
decision, product and RBAC governance for who may submit an operation-scoped
continuation, runtime capability discovery/version skew, and the final set of
supported configuration changes eligible for rolling replacement. It must
also decide whether preferred-captain policy needs a typed customer field, how
the bootstrap-seed capability is versioned between Operator and image, and
when the misleading compatibility variable can be renamed or retired.

## Revision Note

2026-07-24: Replaced the design outline with the Wave 0 contract. The contract
uses optional pointer fields, controller-resolved spike defaults, two
disabled-by-default feature gates, a bounded durable operation status, stable
stage/reason values, explicit validation, and an `OnDelete` compatibility
default. This structure avoids changing existing Pod templates merely by
installing a new Operator version.

2026-07-25: Added the distinction between ordinal identity, bootstrap seed, and
runtime captaincy; the Kubernetes preferred-captain default and override
decision; dynamic member targeting; and the `InitialFormationPending` and
`CaptainUnavailable` reason values. It also records the complete bounded
partition-decision vocabulary implemented by the rollout coordinator. These
close contract gaps found while tracing the Operator and Splunk Ansible
behavior together.

2026-07-28: Replaced the unresolved continuation placeholder with the
qualified two-part post-timeout contract. The design now defines exact
operation/token matching, the durable approval barrier, bounded audit fields,
continued captain and cluster safety checks, revision isolation, one Event,
one counter increment, stale-approval behavior, and the remaining production
RBAC/governance decision.

2026-07-28: Added the SHC-75 StatefulSet observation and rollback contract.
The design now requires a generation-observation barrier, handles
ControllerRevision reuse with per-Pod revision evidence, retains in-place
cancellation ownership through Kubernetes readiness, and separates
pre-authorization cancellation from post-authorization revision withdrawal.
