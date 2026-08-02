# Search Head Cluster Image Upgrade Workflow Technical Design

Status: OPS-007 implementation design for review. This document does not claim
that the behavior is implemented or production-qualified.

## Purpose

This design defines how the Splunk Operator coordinates a supported Search Head
Cluster (SHC) image upgrade with the partition-gated StatefulSet rollout and
the existing per-member lifecycle workflow.

The required outcome is one cluster-wide Splunk upgrade workflow containing:

1. one durable upgrade identity;
2. one successful upgrade-initialization step;
3. one safe lifecycle operation for every Search Head member;
4. proof that every member returned at the intended image and StatefulSet
   revision; and
5. one successful upgrade-finalization step.

Upgrade initialization and finalization are cluster-wide operations. Detention,
search drain, captain transfer, replacement authorization, Pod recreation,
member rejoin, synchronization, and detention release are per-member
operations. They must not be represented by the same status object or retried
with the same rules.

This design covers OPS-007 and the upgrade-specific part of LFC-011. It assumes
that OPS-006 supplies the shared planned-disruption gate that prevents an App
Framework or deployer bundle workflow from overlapping the image rollout.

## Scope

This design covers:

- classifying a StatefulSet template change as an SHC image upgrade;
- validating that the requested source-to-target image transition is known to
  be supported;
- creating and persisting a cluster-scoped image-upgrade operation;
- ordering upgrade initialization before the first member enters detention;
- composing initialization with the existing reverse-ordinal member lifecycle;
- ordering finalization after every member has recovered;
- restart, retry, status-conflict, and desired-state-change behavior;
- bounded status, Events, logs, and metrics; and
- unit, envtest, integration, disruption, and compatibility qualification.

This design does not:

- define which Splunk Enterprise version pairs are supported;
- infer support from an image tag alone;
- make an unsupported `[shclustering]` configuration change safe to roll;
- replace the per-member lifecycle state machine;
- change permanent scale-down or complete-deletion semantics;
- coordinate deployer bundle operations independently of OPS-006; or
- claim exactly-once HTTP delivery when Splunk exposes no authoritative
  upgrade-state readback.

## Current behavior and gap

The current lifecycle adapter combines two different side effects when a
member first enters detention:

1. call the Splunk `upgrade-init` endpoint; and
2. call the member manual-detention endpoint.

As a result, each member prepared for replacement can call `upgrade-init`.
The existing upgrade phase and start/end timestamps are cluster-wide fields,
but they do not identify the StatefulSet revision, source image, target image,
or completed ordinals. They therefore cannot distinguish a retry of one
workflow from a new rollout.

The current rolling coordinator correctly waits for all Pods to reach the
update revision and complete per-member recovery before reaching its completion
path. It then calls upgrade finalization. However, the existing finalization
method changes the CR status to `Upgraded` before the external request has
succeeded. Because reconciliation persists status on error, a failed
finalization can appear complete and will not be retried.

The current `UpgradePathValidation` function orders upgrades relative to
referenced Splunk resources. For an SHC, it does not establish that a
source-to-target Splunk Enterprise image transition is supported. Resource
ordering and version-path support are separate validations.

## Design principles

### One cluster workflow, many member workflows

The image-upgrade operation is keyed to one desired StatefulSet revision and
one target image. It survives the replacement of every ordinal.

`SearchHeadClusterStatus.LifecycleOperation` continues to describe the current
or most recent per-member operation. Starting the next ordinal must not erase
the cluster-wide initialization result or the set of ordinals already
qualified for the image upgrade.

### Persist before acting

The controller must persist:

- image-upgrade identity before calling `upgrade-init`;
- initialization intent before the first initialization attempt;
- successful initialization before detaining the first member;
- each recovered ordinal before preparing the next member;
- finalization intent before the first finalization attempt; and
- successful finalization before reporting the SHC ready.

Each transition that authorizes a new class of side effect is observed on a
later reconciliation. A single reconciliation must not both create the
cluster-wide workflow and call Splunk, or both record initialization success
and begin member detention.

### Kubernetes and Splunk observations are both required

A recovered member must have:

- the desired StatefulSet revision;
- the desired Splunk container image;
- a new Pod identity relative to the authorized target;
- Kubernetes readiness;
- the expected persistent SHC member identity;
- registered and `Up` status in the local and captain views;
- required synchronization health; and
- released detention.

Kubernetes readiness alone does not add an ordinal to the image-upgrade
operation's completed set.

### Fail closed on ambiguity

The controller must block rather than guess when:

- the update revision is not yet available;
- existing Pods have an unexplained mixture of images before an operation was
  recorded;
- the source-to-target path is unknown or unsupported;
- the desired revision, target image, or replica set changes during the
  workflow;
- another planned disruptive workflow owns the coordination gate;
- more than one member is unavailable;
- a per-member lifecycle operation targets a different revision; or
- final recovery proof is incomplete.

## API and durable status contract

No new customer spec field is required for OPS-007. A change to
`spec.image` remains the declarative request. The Operator must not add an
imperative `startUpgrade` boolean.

Add one optional cluster-scoped object to `SearchHeadClusterStatus`:

```go
ImageUpgrade *SearchHeadClusterImageUpgradeStatus `json:"imageUpgrade,omitempty"`
```

Define:

```go
type SearchHeadClusterImageUpgradeStatus struct {
    OperationID string `json:"operationID"`

    StatefulSetName string `json:"statefulSetName"`
    DesiredRevision string `json:"desiredRevision"`
    SourceImage string `json:"sourceImage"`
    TargetImage string `json:"targetImage"`
    TargetReplicas int32 `json:"targetReplicas"`

    Phase SearchHeadClusterImageUpgradePhase `json:"phase"`
    Reason SearchHeadClusterImageUpgradeReason `json:"reason,omitempty"`
    Message string `json:"message,omitempty"`

    StartedAt *metav1.Time `json:"startedAt,omitempty"`
    PhaseStartedAt *metav1.Time `json:"phaseStartedAt,omitempty"`
    LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`

    InitializationIntentAt *metav1.Time `json:"initializationIntentAt,omitempty"`
    InitializationLastAttemptAt *metav1.Time `json:"initializationLastAttemptAt,omitempty"`
    InitializationSucceededAt *metav1.Time `json:"initializationSucceededAt,omitempty"`
    InitializationAttemptCount int32 `json:"initializationAttemptCount,omitempty"`

    CompletedOrdinals []int32 `json:"completedOrdinals,omitempty"`

    FinalizationIntentAt *metav1.Time `json:"finalizationIntentAt,omitempty"`
    FinalizationLastAttemptAt *metav1.Time `json:"finalizationLastAttemptAt,omitempty"`
    FinalizationSucceededAt *metav1.Time `json:"finalizationSucceededAt,omitempty"`
    FinalizationAttemptCount int32 `json:"finalizationAttemptCount,omitempty"`

    CompletedAt *metav1.Time `json:"completedAt,omitempty"`
}
```

The multiline formatting above is illustrative; generated Go should follow the
repository formatter.

### Field semantics

- `OperationID`: The deterministic value
  `image-upgrade:<StatefulSetName>:<DesiredRevision>`. Status is scoped to one
  CR, and `DesiredRevision` identifies the complete target Pod template,
  including its image. The target image is also stored and checked explicitly
  to make conflicts diagnosable. The ID is used in status, Events, and
  structured logs, but never as a Prometheus label.
- `DesiredRevision`: The non-empty StatefulSet `status.updateRevision` whose
  Pods the workflow is authorized to create. A later different revision is a
  conflict, not an implicit mutation of this operation.
- `SourceImage` and `TargetImage`: Exact image references from the observed
  source Pods and desired Splunk container. These fields identify the declared
  transition; they do not prove semantic-version compatibility and must not be
  parsed as the sole support decision.
- `TargetReplicas`: The ordinal set captured before initialization. Replica
  changes are blocked until the upgrade completes or an explicit recovery
  policy is designed.
- `CompletedOrdinals`: A bounded, unique set of ordinals that completed the
  per-member replacement and SHC recovery contract for `DesiredRevision` and
  `TargetImage`. The list is sorted for stable status and cannot contain
  values outside `[0, TargetReplicas)`.
- Attempt fields: Record observable intent and bounded retry evidence. Attempt
  counts increment when an external request is attempted. Success timestamps
  are set only after an HTTP success response. They must not be used as metric
  labels.
- `Message`: Bounded diagnostic text. It must not contain credentials,
  authorization headers, arbitrary HTTP response bodies, registry
  credentials, or customer search content.

### Compatibility projection

Retain the existing `UpgradePhase`, `UpgradeStartTimestamp`, and
`UpgradeEndTimestamp` fields during the compatibility period:

- successful initialization projects `UpgradePhaseUpgrading` and the start
  timestamp;
- successful finalization projects `UpgradePhaseUpgraded` and the end
  timestamp; and
- neither projection is changed before the corresponding endpoint succeeds.

The new `ImageUpgrade` object is authoritative for workflow identity and retry
state. The legacy fields must not be used to decide whether a different
revision has already been initialized.

### Phase values

Define:

- `PendingInitialization`;
- `Initializing`;
- `RollingMembers`;
- `PendingFinalization`;
- `Finalizing`;
- `Completed`;
- `Blocked`; and
- `Failed`.

`Blocked` is recoverable after the observed cause is corrected while the
operation identity remains valid. `Failed` is terminal for the current
operation and requires an explicit, separately designed recovery action.
Endpoint transport errors remain in `Initializing` or `Finalizing` with a
bounded reason; they are not automatically terminal.

### Reason values

Use a bounded enum, including:

- `WorkflowRecorded`;
- `InitializationIntentRecorded`;
- `InitializationRetrying`;
- `InitializationSucceeded`;
- `MemberLifecycleInProgress`;
- `MemberRecovered`;
- `AllMembersRecovered`;
- `FinalizationIntentRecorded`;
- `FinalizationRetrying`;
- `FinalizationSucceeded`;
- `UnsupportedUpgradePath`;
- `UnknownUpgradePath`;
- `RevisionConflict`;
- `TargetImageConflict`;
- `ReplicaConflict`;
- `MixedSourceImages`;
- `ConflictingPlannedOperation`;
- `ClusterNotReady`;
- `MemberLifecycleBlocked`; and
- `OperationCompleted`.

## Upgrade classification

Classification occurs only after the desired StatefulSet template has been
persisted and Kubernetes reports a non-empty `status.updateRevision`.

The classifier observes:

- the Splunk container image in the desired StatefulSet template;
- each current ordinal's Splunk container image;
- each current ordinal's controller revision;
- StatefulSet current and update revisions;
- replica count and partition;
- any existing image-upgrade status; and
- the shared planned-operation owner from OPS-006.

### New image upgrade

Create a new image-upgrade operation only when:

1. no nonterminal image-upgrade operation exists;
2. the StatefulSet has a non-empty update revision;
3. all expected source Pods exist and are stably ready;
4. all source Pods have one identical image reference;
5. the source image differs from the desired template image;
6. no Pod has already moved to the desired revision;
7. the partition prevents an unauthorized replacement;
8. the desired replica count is stable;
9. the OPS-006 coordination gate is available; and
10. the source-to-target transition is authoritatively classified as
    supported.

The first reconciliation creates `PendingInitialization` and returns without
calling Splunk or detaining a member.

### Existing image upgrade

When `status.imageUpgrade` is nonterminal, its desired revision, target image,
and replica set are authoritative. A mixed old/new Pod population is expected
during this recorded workflow and is not reclassified.

If the StatefulSet or CR now requests a different revision, image, or replica
set, transition to `Blocked` with the corresponding conflict reason. Do not
replace the operation object or initialize the new request.

### Ordinary Pod-template rollout

If every existing Pod image equals the desired image and there is no active
image-upgrade operation, the revision is not an image upgrade. The ordinary
per-member lifecycle may proceed without calling `upgrade-init` or
`upgrade-finalize`.

This distinction prevents certificate, resource, probe, annotation, or other
safe rolling changes from being presented to Splunk as an image upgrade.
OPS-008 separately decides whether a configuration change is safe for
one-at-a-time replacement.

### Same-tag image replacement

An image registry tag that resolves to new bytes while its string remains
unchanged is not a classifiable image-upgrade request. Kubernetes also does not
create a new StatefulSet revision from an unchanged image string alone.

The Operator must not infer an upgrade from a runtime image ID mismatch without
an approved digest or explicit image-intent contract. Qualification should use
immutable image references or distinct declared versions.

### Exact same-version image intent

A private-registry or air-gapped deployment can replace Docker-Splunk,
Splunk-Ansible, certificates, or operating-system content while retaining the
same Splunk Enterprise build. The source and target image references differ,
but Splunk's version-upgrade initialization and finalization APIs are not the
correct workflow. An image reference alone cannot prove this distinction.

The bounded contract is an explicit `SameVersionRestart` declaration tied to
one exact source image and one exact target image. The target must also equal
the CR's desired image. The declaration is valid only for the
partition-controlled `RollingUpdate` path and is not a general compatibility
override. It becomes stale when either endpoint of the pair changes.

Before starting a lifecycle operation, the Operator must prove that the Pod
population matches the StatefulSet partition boundary: ordinals below the
partition use the declared source image and current revision; ordinals at or
above the partition use the declared target image and update revision. Every
Pod must be present, non-deleting, and stably ready. Once one exact target is
durably owned by a matching `PodUpdate` lifecycle operation, that target is
the sole exception to the readiness and presence requirement. The Operator
itself withdraws its readiness before detention and Kubernetes subsequently
exposes a terminating source Pod, a temporarily absent Pod, or a starting
target Pod. During that interval the observed target, when present, must still
match either the exact declared source/current pair or the exact declared
target/update pair. Every unowned ordinal remains present, non-deleting,
stably ready, and aligned with the partition. Any third image, unexpected
revision, unavailable unowned member, or invalid target ordinal blocks. This
invariant makes the intent resumable across controller replacement without
allowing a stale declaration to authorize an unrelated transition.

A valid declaration runs the ordinary per-member lifecycle and does not create
an image-upgrade status or invoke `upgrade-init`/`upgrade-finalize`. Omission or
mismatch retains the authoritative compatibility decision described below.

Image classification must precede Deployer mutation. An unsupported or unknown
member transition cannot safely run a newer Deployer first, because that
Deployer may produce or push bundles that are incompatible with the current
members. On the lifecycle-enabled `RollingUpdate` path, the controller performs
a read-only observation of existing member Pod images and the partition before
applying the desired Deployer StatefulSet. Only an unchanged member image, the
exact same-version declaration, an already recorded matching upgrade workflow,
or an authoritative `Supported` decision may pass this gate. This preflight is
not applied to the retained `OnDelete` compatibility path.

### Upgrade-path support

Introduce a side-effect-free validator boundary conceptually equivalent to:

```go
type SHCImageUpgradePathValidator interface {
    Validate(ctx context.Context, sourceImage, targetImage string)
        (SHCImageUpgradePathDecision, error)
}
```

The decision is one of `Supported`, `Unsupported`, or `Unknown`.

- `Supported` permits workflow creation.
- `Unsupported` blocks with `UnsupportedUpgradePath`.
- `Unknown` blocks with `UnknownUpgradePath`.
- a transient validator error leaves the rollout unmodified and retryable.

The validator must use an authoritative product compatibility source or an
explicitly versioned capability contract. Parsing tags such as `latest`,
private-registry aliases, or arbitrary customer naming conventions is not
authoritative.

OPS-007 integration tests may use a known supported image pair. Production
opt-in cannot claim general supported-path enforcement until the validator's
data source and ownership are approved.

## State machine

```text
No operation
    |
    | classify supported image transition
    v
PendingInitialization
    |
    | persist initialization intent
    v
Initializing
    |
    | POST upgrade-init succeeds; persist success
    v
RollingMembers
    |
    | for each ordinal, highest to lowest:
    | detain -> drain -> transfer captain if needed ->
    | authorize partition -> replace -> rejoin -> synchronize ->
    | release detention -> persist completed ordinal
    |
    | all captured ordinals complete and partition reset
    v
PendingFinalization
    |
    | persist finalization intent
    v
Finalizing
    |
    | POST upgrade-finalize succeeds; persist success
    v
Completed
```

Any nonterminal phase can transition to `Blocked` for a durable desired-state
conflict, unsupported path, conflicting planned operation, or blocked member
lifecycle. Removing a transient endpoint or observation failure resumes the
same operation; it does not create a new ID.

## Initialization sequence and persistence barriers

The controller performs these reconciliations:

1. **Record workflow.** Persist the operation identity, source/target images,
   desired revision, target replicas, `PendingInitialization`, and
   `WorkflowRecorded`. Return `PhaseUpdating`. Do not call Splunk.
2. **Record intent.** Set `InitializationIntentAt`, transition to
   `Initializing`, and return. Do not call Splunk.
3. **Attempt initialization.** Select a reachable, registered, `Up`,
   Kubernetes-ready member while requiring an authoritative service-ready
   captain. Record attempt time/count in memory, call `upgrade-init` through
   that member, and:
   - on error, retain `Initializing`, persist bounded retry evidence, and
     return an error or controlled retry result according to controller error
     policy;
   - on HTTP success, set `InitializationSucceededAt`, project the legacy
     start fields, and remain in a success-pending barrier.
4. **Observe success.** On a later reconciliation, transition to
   `RollingMembers`. Return without detaining a member.
5. **Prepare first member.** Only a reconciliation observing the persisted
   `RollingMembers` phase may call the per-member `PrepareRecycle`.

The lifecycle-enabled detention helper must perform detention only. It must not
call `InitiateUpgrade`. The compatibility `OnDelete` path can retain its legacy
behavior until it is migrated under a separately qualified change; OPS-007
must not silently alter disabled-feature behavior.

## Per-member rollout composition

The existing partition coordinator remains the owner of ordinal order:

1. select the highest incomplete ordinal;
2. create a `PodUpdate` lifecycle operation for the image-upgrade revision;
3. detain and drain that member;
4. transfer captaincy when the target is captain;
5. persist replacement authorization;
6. lower the partition by one;
7. let Kubernetes replace the Pod;
8. verify Pod identity, revision, target image, readiness, persistent member
   identity, registration, `Up` status, and synchronization;
9. release detention;
10. complete the member lifecycle; and
11. add the ordinal to `ImageUpgrade.CompletedOrdinals`.

Adding an ordinal and preparing the next ordinal occur in different
reconciliations. The completed set is idempotent: observing the same completed
member more than once does not add duplicates or change timestamps.

The next ordinal cannot start unless:

- initialization success is persisted;
- the previous ordinal is in the completed set;
- no unrelated Pod is unavailable;
- the StatefulSet revision and target image still match the upgrade operation;
- the OPS-006 coordination owner remains the image workflow; and
- the current SHC captain and majority satisfy lifecycle policy.

## Finalization sequence and persistence barriers

Finalization is eligible only when:

- `CompletedOrdinals` contains every ordinal captured by `TargetReplicas`;
- every Pod exists, is not terminating, and is Kubernetes-ready;
- every Pod has `DesiredRevision` and `TargetImage`;
- the StatefulSet current and update revisions have converged;
- the partition has been reset to the replica count and that reset has been
  observed from the API server;
- the latest per-member lifecycle operation is complete;
- the SHC is initialized, has minimum peers joined, and has an authoritative
  service-ready captain;
- every member is registered and `Up`; and
- no conflicting planned operation is active.

The controller then performs:

1. **Record finalization eligibility.** Transition from `RollingMembers` to
   `PendingFinalization` with `AllMembersRecovered`. Return without calling
   Splunk.
2. **Record intent.** Set `FinalizationIntentAt`, transition to `Finalizing`,
   and return without calling Splunk.
3. **Attempt finalization.** Select a dynamic reachable member and call
   `upgrade-finalize`.
   - On error, retain `Finalizing`, persist attempt evidence, and retry the same
     logical operation.
   - On HTTP success, set `FinalizationSucceededAt`, project the legacy end
     fields, and return `PhaseUpdating`.
4. **Complete.** On a later reconciliation observing persisted success, set
   `CompletedAt`, transition to `Completed`, release the OPS-006 coordination
   owner, and report `PhaseReady`.

Status must never change to `Upgraded` or `Completed` before the finalization
request succeeds.

## Controller integration points

The implementation should remain localized and avoid putting upgrade semantics
inside the pure StatefulSet partition evaluator.

### API

`api/enterprise/v4/searchheadcluster_types.go`
: Add the optional `ImageUpgrade` status object, phase and reason enums.

`api/enterprise/v4/zz_generated.deepcopy.go`
: Regenerate deep-copy methods.

`config/crd/bases/enterprise.splunk.com_searchheadclusters.yaml`
: Regenerate the CRD schema. Status fields are optional except for required
  fields within a present operation object as selected during API review.

### Pure workflow

Add a package under `pkg/splunk/workflow/upgrade` containing:

- classification input and decision types;
- image-upgrade state transitions;
- completed-ordinal set handling;
- conflict detection;
- initialization and finalization eligibility; and
- no Kubernetes client or Splunk client calls.

The existing `EvaluateSHCRollout` continues to decide partition actions. The
image-upgrade workflow decides whether that evaluator is currently allowed to
prepare a target or finish the overall rollout.

### Enterprise adapter

`pkg/splunk/enterprise/searchhead_rollout_controller.go`
: Before `PrepareTarget`, reconcile the image-upgrade initialization gate.
  During member completion, project the completed ordinal. In the rollout
  completion path, replace the direct `FinishUpgrade(ctx, 0)` call with the
  finalization state machine.

`pkg/splunk/enterprise/searchhead_rollout_controller.go`
: Extend the bounded rollout observation, or add a neighboring observation
  helper, to capture the desired Splunk image and each Pod's declared Splunk
  image without putting arbitrary image values into metrics.

`pkg/splunk/enterprise/searchheadcluster_lifecycle.go`
: Remove `InitiateUpgrade` from the lifecycle-enabled
  `requestSearchHeadDetention` action. Detention remains a per-member action.

`pkg/splunk/enterprise/searchheadclusterpodmanager.go`
: Do not use the existing `FinishUpgrade` method for the new RollingUpdate
  workflow. Preserve or separately migrate the compatibility path. Any shared
  finalization helper must set success status only after the REST call
  succeeds.

`pkg/splunk/enterprise/afwscheduler.go` or a small shared helper
: Reuse the dynamic healthy-member selection contract introduced for SHC
  bundle targeting. Upgrade endpoints proxy to the captain, but the member
  through which the request is sent must still be registered, `Up`, not
  terminating, and Kubernetes-ready.

`pkg/splunk/enterprise/upgrade.go`
: Keep dependency ordering separate from image-path support. Invoke the
  approved SHC image-path validator before recording the workflow rather than
  treating the current SHC branch of `UpgradePathValidation` as support proof.

### Side-effect seams

Add narrow injectable functions for unit tests:

```go
var initiateSearchHeadClusterUpgrade = func(
    ctx context.Context,
    mgr *searchHeadClusterPodManager,
    ordinal int32,
) error

var finalizeSearchHeadClusterUpgrade = func(
    ctx context.Context,
    mgr *searchHeadClusterPodManager,
    ordinal int32,
) error
```

These seams wrap the existing Splunk client. Tests must assert call count,
target selection, action ordering, and status after success or failure.

## Reconcile ordering with OPS-006

The planned-operation coordinator must expose one active owner. For an image
upgrade:

1. acquire or durably record the image-upgrade owner before
   `InitializationIntentAt`;
2. reject new App Framework, bundle, manual rolling-restart, scale, or
   incompatible lifecycle work while the image workflow is nonterminal;
3. do not initialize while a previously recorded conflicting owner is active;
4. retain ownership through finalization retries; and
5. release ownership only after `ImageUpgrade.Completed` is persisted.

Losing an in-memory lease during Operator restart must not release ownership.
The owner must be derivable from durable CR status.

## Failure and restart semantics

### Operator restart before initialization

`PendingInitialization` resumes by recording initialization intent.
`Initializing` with no success timestamp resumes the same logical
initialization attempt policy. No member may be detained.

### Initialization request error

Remain in `Initializing`, increment bounded retry evidence, and do not start a
member lifecycle or move the partition. A service-ready captain and dynamic
reachable target are re-observed before each retry.

### Operator restart after initialization success

When `InitializationSucceededAt` is persisted, never call `upgrade-init` again
for that operation. Resume by entering or observing `RollingMembers`.

### Per-member failure

The image-upgrade operation remains `RollingMembers` and reports
`MemberLifecycleBlocked`. The per-member lifecycle retains the detailed stage
and reason. Finalization is forbidden. Correcting a recoverable problem resumes
the same ordinal.

### Operator restart during member replacement

The partition, per-member lifecycle operation, image-upgrade identity, and
completed ordinals are durable. Re-observe Kubernetes and Splunk, then resume
the same target. Do not initialize again and do not mark the target complete
from Kubernetes readiness alone.

### Desired revision, image, or replicas change

Transition the current operation to `Blocked`. Do not overwrite its target,
start a second image operation, move the partition, or finalize the old
workflow. A follow-up design must define cancellation, rollback, or queued
desired state; OPS-007 does not silently coalesce it.

### Finalization request error

Remain in `Finalizing`; do not project `UpgradePhaseUpgraded`, set
`CompletedAt`, release coordination ownership, or report ready. Re-observe the
cluster and retry through a current reachable member.

### Operator restart after finalization success

When `FinalizationSucceededAt` is persisted, do not call the endpoint again.
Complete the durable operation and report ready on a later reconciliation.

### Status update conflict

If a side effect succeeds but the subsequent status update conflicts, fetch the
latest object and merge only when its operation ID still matches. Never apply
success to a different desired revision. The endpoint idempotency limitation
below still applies to a crash or conflict in the post-success persistence
window.

## Endpoint idempotency and exactly-once limitation

The Splunk client currently posts to:

- `/services/shcluster/captain/control/control/upgrade-init`; and
- `/services/shcluster/captain/control/control/upgrade-finalize`.

The client treats HTTP 200 as success. The Operator code does not currently
query an authoritative Splunk state that proves whether initialization or
finalization already took effect.

This creates an unavoidable distributed-systems ambiguity:

1. Splunk accepts and applies the request.
2. The Operator process stops, or its status update fails, before persisting
   the success timestamp.
3. After restart, durable status still says the request is incomplete.

Marking success before the request would risk permanently skipping an action
when the process stops before sending it. Marking success after the request is
correct, but can repeat the request in this ambiguity window.

Therefore OPS-007 can guarantee:

- one durable logical intent per image-upgrade operation;
- no repeated request after success is durably recorded;
- retries associated with the same operation ID; and
- complete visibility into intent, attempts, and observed success.

It cannot guarantee exactly-once HTTP delivery unless one of these contracts is
approved:

1. Splunk Enterprise documents both endpoints as idempotent for repeated calls
   in the same current upgrade state;
2. Splunk exposes a supported read endpoint that reports authoritative upgrade
   initialization/finalization state; or
3. Splunk accepts a caller-supplied idempotency key and deduplicates requests.

Production enablement must resolve and qualify this contract. Tests using a
fake endpoint may validate Operator retry behavior, but cannot establish
Splunk endpoint idempotency.

## Observability

### Status and conditions

Status exposes the current phase, bounded reason, elapsed time, target
revision, target image, completed ordinal count, and retry counts.

The SHC `Progressing` condition remains true from workflow recording through
successful finalization. `Ready` must not become true while initialization,
member rollout, or finalization is incomplete. `Blocked` and `Failed` reasons
must be reflected without copying arbitrary external error bodies.

### Kubernetes Events

Emit deduplicated Events for:

- image-upgrade workflow recorded;
- initialization requested, retrying, and succeeded;
- each ordinal started and recovered;
- image-upgrade blocked;
- finalization requested, retrying, and succeeded; and
- image-upgrade completed.

Polling observations must not emit Events repeatedly.

### Structured logs

Include operation ID, phase, bounded reason, StatefulSet revision, target
ordinal when applicable, attempt count, and elapsed duration. Image references
may appear where operationally required because they already exist in the CR,
but credentials and registry authorization data must never appear.

### Prometheus metrics

Use bounded labels only, such as phase, action, result, and reason. Do not use
operation ID, image reference, revision, Pod name, UID, arbitrary error text,
or customer namespace as metric labels.

Measure:

- initialization attempts and duration;
- member rollout duration;
- recovered member count;
- finalization attempts and duration;
- blocked duration by bounded reason; and
- complete image-upgrade duration and outcome.

## Test matrix

### Pure workflow unit tests

| ID | Scenario | Required assertion |
|---|---|---|
| OPS007-U01 | Supported image difference | Records one `PendingInitialization` operation |
| OPS007-U02 | Images unchanged, template revision changed | Classified as ordinary rollout; no upgrade operation |
| OPS007-U03 | Unsupported path | Blocks without initialization intent |
| OPS007-U04 | Unknown path | Blocks without initialization intent |
| OPS007-U05 | Mixed source images without durable operation | Blocks as ambiguous |
| OPS007-U06 | Existing mixed old/new images with matching operation | Resumes recorded workflow |
| OPS007-U07 | Empty update revision | Waits without side effects |
| OPS007-U08 | Desired revision changes | Existing operation blocks |
| OPS007-U09 | Target image changes | Existing operation blocks |
| OPS007-U10 | Replica count changes | Existing operation blocks |
| OPS007-U11 | Completed ordinal observed twice | Set remains unique and stable |
| OPS007-U12 | Missing ordinal recovery | Finalization remains ineligible |
| OPS007-U13 | All ordinals recovered but partition not reset | Finalization remains ineligible |
| OPS007-U14 | All final gates satisfied | Transitions to finalization intent path |

### Enterprise adapter unit tests

| ID | Scenario | Required assertion |
|---|---|---|
| OPS007-A01 | First classification reconcile | Persists identity; zero init, detention, or partition calls |
| OPS007-A02 | Initialization intent reconcile | Persists intent; zero init and detention calls |
| OPS007-A03 | Initialization succeeds | One init call; success recorded; no same-reconcile detention |
| OPS007-A04 | Initialization endpoint fails | Retryable state; no detention or partition movement |
| OPS007-A05 | Reconcile after persisted init success | Zero additional init calls |
| OPS007-A06 | Three member preparations | Init call count remains one |
| OPS007-A07 | Member lifecycle completion | Ordinal added only after full SHC recovery |
| OPS007-A08 | Non-image RollingUpdate | Zero init and finalize calls |
| OPS007-A09 | Completion before partition reset | Zero finalize calls |
| OPS007-A10 | Finalization intent reconcile | Persists intent; zero finalize calls |
| OPS007-A11 | Finalization fails | Status remains retryable and not `Upgraded` |
| OPS007-A12 | Finalization succeeds | Success recorded only after endpoint success |
| OPS007-A13 | Reconcile after persisted finalize success | Zero additional finalize calls |
| OPS007-A14 | Ordinal zero unavailable as request target | Selects another eligible member |
| OPS007-A15 | No eligible management target | Blocks/retries without endpoint call |
| OPS007-A16 | Bundle owner active | Does not record initialization intent |

Run action-ordering and restart-sensitive unit tests repeatedly to expose
non-deterministic call ordering.

### Envtest/controller tests

| ID | Scenario | Required assertion |
|---|---|---|
| OPS007-E01 | Status identity barrier | Persisted operation is observed before external action |
| OPS007-E02 | Operator restart in every image phase | Same operation ID and target revision resume |
| OPS007-E03 | Restart between member ordinals | Completed set and next ordinal are preserved |
| OPS007-E04 | Status update conflict after endpoint success | Merge cannot update a different operation |
| OPS007-E05 | Spec image changes while active | Condition becomes blocked; partition does not advance |
| OPS007-E06 | Spec replicas change while active | Condition becomes blocked; no scale action overlaps |
| OPS007-E07 | Controller leader failover | New leader resumes from CR and StatefulSet state |
| OPS007-E08 | Upgrade completion | `Ready` follows persisted finalization success |
| OPS007-E09 | CRD round trip and deep copy | Every pointer, count, enum, and ordinal survives |

### Real SHC integration tests

| ID | Scenario | Required assertion |
|---|---|---|
| OPS-007 | Supported three-member image upgrade | One logical init, ordinals 2/1/0 safely recover, one logical finalize |
| LFC-011 | Init/finalize retry | Retry is observable and does not duplicate member disruption |
| OPS007-I01 | First target is captain | Confirmed transfer precedes replacement |
| OPS007-I02 | Captain changes independently | Dynamic observations continue safely |
| OPS007-I03 | Historical search on target | Upgrade waits for drain policy |
| OPS007-I04 | Real-time search on target | Independent drain policy is observable |
| OPS007-I05 | Image pull failure | No next ordinal and no finalize |
| OPS007-I06 | Member rejoin/synchronization delay | No next ordinal and no finalize |
| OPS007-I07 | Operator restart at each barrier | Workflow resumes with one durable identity |
| OPS007-I08 | Dynamic management target failure | Another eligible member is selected |
| OPS007-I09 | App/bundle request during upgrade | OPS-006 gate prevents overlap |
| OPS007-I10 | Upgrade request during app/bundle workflow | Initialization waits for ownership |

The fake Splunk API should record every endpoint request and operation phase.
The real-Splunk test must separately document whether repeated init/finalize
requests are accepted and what authoritative state, if any, can be observed.

### Compatibility and delivery tests

- old runtime image with the new Operator must retain safe compatibility or
  report an explicit unsupported capability;
- new runtime image with an old Operator must retain the documented legacy
  behavior;
- upgrading the Operator during `Initializing`, `RollingMembers`, and
  `Finalizing` must preserve status;
- supported previous Splunk versions must expose compatible endpoints;
- TLS, service mesh, private registry, and air-gap environments must preserve
  management endpoint reachability without undeclared external dependencies;
- rollback to `OnDelete` during an active image workflow must not abandon
  initialization or incorrectly finalize; and
- status, Events, logs, metrics, and diagnostic bundles must redact sensitive
  data and use bounded labels.

## Acceptance criteria

OPS-007 is complete only when:

1. an image change is distinguished from an ordinary Pod-template change;
2. an authoritative validator accepts the tested source-to-target transition;
3. one durable image-upgrade identity exists across all member replacements;
4. no member is detained before initialization success is persisted;
5. initialization is not called again after durable success;
6. every ordinal completes the existing lifecycle and is recorded once;
7. no more than one planned member is unavailable;
8. finalization is impossible before every Kubernetes and SHC recovery gate
   passes and the partition reset is observed;
9. finalization failure remains retryable and cannot project `Upgraded`;
10. finalization is not called again after durable success;
11. Operator restart and leader failover resume the same logical operation;
12. desired-state conflicts block without silently changing workflow identity;
13. OPS-006 prevents conflicting bundle or deployer disruption;
14. the endpoint idempotency limitation is resolved or explicitly accepted for
    the intended release stage; and
15. the full OPS-007, LFC-011, compatibility, observability, and redaction
    evidence is attached to the release gate.

## Open decisions

The following decisions require owner approval before production opt-in:

1. the authoritative source and owner for supported Splunk image transitions;
2. whether `upgrade-init` and `upgrade-finalize` are idempotent, observable, or
   can accept an idempotency key;
3. the cancellation or rollback procedure for a desired image change during an
   active workflow;
4. whether replica changes are queued or rejected while upgrading;
5. retention policy for the most recent completed image-upgrade status;
6. compatibility behavior for lifecycle-enabled `OnDelete`; and
7. the runtime capability signal required before enabling this workflow for a
   particular Splunk image.
