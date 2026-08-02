/*
Copyright (c) 2018-2022 Splunk Inc. All rights reserved.


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v4

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// default all fields to being optional
// +kubebuilder:validation:Optional

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
// see also https://book.kubebuilder.io/reference/markers/crd.html

const (
	// SearchHeadClusterPausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	SearchHeadClusterPausedAnnotation = "searchheadcluster.enterprise.splunk.com/paused"
)

// SearchHeadClusterPodUpdateStrategy identifies which system owns Pod
// replacement for a Search Head Cluster.
// +kubebuilder:validation:Enum=OnDelete;RollingUpdate
type SearchHeadClusterPodUpdateStrategy string

const (
	// SearchHeadClusterPodUpdateStrategyOnDelete preserves the existing
	// Operator-owned Pod replacement behavior.
	SearchHeadClusterPodUpdateStrategyOnDelete SearchHeadClusterPodUpdateStrategy = "OnDelete"
	// SearchHeadClusterPodUpdateStrategyRollingUpdate requests the future
	// partition-gated Kubernetes StatefulSet rollout behavior.
	SearchHeadClusterPodUpdateStrategyRollingUpdate SearchHeadClusterPodUpdateStrategy = "RollingUpdate"
)

// SearchHeadClusterImageUpdateIntent identifies an explicit classification
// for one exact Search Head Cluster image transition.
// +kubebuilder:validation:Enum=SameVersionRestart
type SearchHeadClusterImageUpdateIntent string

const (
	// SearchHeadClusterImageUpdateIntentSameVersionRestart declares that the
	// exact source and target images contain the same Splunk Enterprise build.
	// The Operator may therefore use the ordinary per-Pod lifecycle without
	// invoking Splunk's version-upgrade initialization or finalization APIs.
	SearchHeadClusterImageUpdateIntentSameVersionRestart SearchHeadClusterImageUpdateIntent = "SameVersionRestart"
)

// SearchHeadClusterImageUpdateIntentSpec binds one declared image-update
// intent to an exact immutable source and target pair. Retaining both values
// prevents a declaration from authorizing a later unrelated image change.
type SearchHeadClusterImageUpdateIntentSpec struct {
	// Intent classifies the exact source-to-target transition.
	Intent SearchHeadClusterImageUpdateIntent `json:"intent"`

	// SourceImage must equal the image declared by every not-yet-replaced
	// Search Head Pod.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	SourceImage string `json:"sourceImage"`

	// TargetImage must equal spec.image and the desired StatefulSet image.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	TargetImage string `json:"targetImage"`
}

// SearchHeadClusterLifecycleApprovalAction identifies one explicitly approved
// exception to the default fail-closed lifecycle policy.
// +kubebuilder:validation:Enum=ContinueAfterSearchDrainTimeout
type SearchHeadClusterLifecycleApprovalAction string

const (
	// SearchHeadClusterLifecycleApprovalActionContinueAfterSearchDrainTimeout
	// permits one named operation to continue after its search-drain timeout.
	SearchHeadClusterLifecycleApprovalActionContinueAfterSearchDrainTimeout SearchHeadClusterLifecycleApprovalAction = "ContinueAfterSearchDrainTimeout"
)

// SearchHeadClusterLifecycleApproval records customer approval for one exact
// blocked lifecycle operation. The token is issued only after the operation
// reaches its fail-closed timeout, so an approval cannot be supplied in
// advance or reused by another operation.
type SearchHeadClusterLifecycleApproval struct {
	// OperationID must equal status.lifecycleOperation.operationID.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	OperationID string `json:"operationID"`

	// Token must equal status.lifecycleOperation.searchDrainContinuationToken.
	// +kubebuilder:validation:Pattern=`^[a-f0-9]{64}$`
	Token string `json:"token"`

	// Action identifies the exception being approved.
	Action SearchHeadClusterLifecycleApprovalAction `json:"action"`
}

// SearchHeadClusterLifecyclePolicy configures lifecycle timing and rollout
// ownership for a Search Head Cluster.
type SearchHeadClusterLifecyclePolicy struct {
	// PodUpdateStrategy selects the Pod replacement owner. Empty resolves to
	// OnDelete.
	// +optional
	PodUpdateStrategy SearchHeadClusterPodUpdateStrategy `json:"podUpdateStrategy,omitempty"`

	// ImageUpdateIntent explicitly classifies one exact source-to-target image
	// transition. Omission preserves fail-closed image-upgrade classification.
	// SameVersionRestart is valid only with podUpdateStrategy=RollingUpdate.
	// +optional
	ImageUpdateIntent *SearchHeadClusterImageUpdateIntentSpec `json:"imageUpdateIntent,omitempty"`

	// DetentionTimeoutSeconds bounds the wait for traffic withdrawal and
	// authoritative confirmation that the target entered manual detention.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=86400
	DetentionTimeoutSeconds *int64 `json:"detentionTimeoutSeconds,omitempty"`

	// SearchDrainTimeoutSeconds bounds the wait for active searches to drain.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=86400
	SearchDrainTimeoutSeconds *int64 `json:"searchDrainTimeoutSeconds,omitempty"`

	// CaptainTransferTimeoutSeconds bounds the supported captain-transfer
	// workflow when the target member is the active captain.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=86400
	CaptainTransferTimeoutSeconds *int64 `json:"captainTransferTimeoutSeconds,omitempty"`

	// PodStartupTimeoutSeconds bounds the wait for a replacement Pod to
	// schedule, attach storage, and reach local container readiness.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=86400
	PodStartupTimeoutSeconds *int64 `json:"podStartupTimeoutSeconds,omitempty"`

	// MemberRejoinTimeoutSeconds bounds the wait for a replaced member to
	// register, become up, and synchronize with the cluster.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=86400
	MemberRejoinTimeoutSeconds *int64 `json:"memberRejoinTimeoutSeconds,omitempty"`
}

// SearchHeadClusterSpec defines the desired state of a Splunk Enterprise search head cluster
type SearchHeadClusterSpec struct {
	CommonSplunkSpec `json:",inline"`

	// Number of search head pods; a search head cluster will be created if > 1
	// +optional
	// +kubebuilder:default=3
	Replicas int32 `json:"replicas,omitempty"`

	// Splunk Enterprise App repository. Specifies remote App location and scope for Splunk App management
	AppFrameworkConfig AppFrameworkSpec `json:"appRepo,omitempty"`

	// Splunk Deployer resource spec
	DeployerResourceSpec corev1.ResourceRequirements `json:"deployerResourceSpec,omitempty"`

	// Splunk Deployer Node Affinity
	DeployerNodeAffinity *corev1.NodeAffinity `json:"deployerNodeAffinity,omitempty"`

	// LifecyclePolicy configures Kubernetes lifecycle orchestration for the
	// Search Head Cluster. It is active only when the
	// SearchHeadClusterLifecycle feature gate is enabled.
	// +optional
	LifecyclePolicy *SearchHeadClusterLifecyclePolicy `json:"lifecyclePolicy,omitempty"`

	// LifecycleApproval permits one exact blocked lifecycle operation to
	// continue after an operator-issued token is copied from status. It is
	// active only when the SearchHeadClusterLifecycle feature gate is enabled.
	// Stale or non-matching approvals are ignored.
	// +optional
	LifecycleApproval *SearchHeadClusterLifecycleApproval `json:"lifecycleApproval,omitempty"`
}

// SearchHeadClusterMemberStatus is used to track the status of each search head cluster member
type SearchHeadClusterMemberStatus struct {
	// Name of the search head cluster member
	Name string `json:"name"`

	// Indicates the status reported by the member itself.
	Status string `json:"status"`

	// Indicates the authoritative status reported by the SHC captain. This is
	// populated while Splunk is performing a rolling restart or captaincy is
	// changing.
	CaptainStatus string `json:"captain_status,omitempty"`

	// Indicates whether the member has been instructed to restart.
	RestartState string `json:"restart_state,omitempty"`

	// Indicates whether the member has advertised to the captain that a
	// restart is required.
	AdvertiseRestartRequired bool `json:"advertise_restart_required,omitempty"`

	// Indicates whether this member is configured as an ad-hoc search head and
	// therefore does not run scheduled jobs.
	Adhoc bool `json:"adhoc_searchhead"`

	// Indicates if this member is registered with the searchhead cluster captain.
	Registered bool `json:"is_registered"`

	// Number of currently running historical searches.
	ActiveHistoricalSearchCount int `json:"active_historical_search_count"`

	// Number of currently running realtime searches.
	ActiveRealtimeSearchCount int `json:"active_realtime_search_count"`
}

// SearchHeadClusterInitialFormationStage identifies the durable startup stage
// for a Search Head Cluster that has not yet served traffic.
type SearchHeadClusterInitialFormationStage string

const (
	// SearchHeadClusterInitialFormationStageClusterFormation waits for the
	// initial SHC join and any restart advertised by the members.
	SearchHeadClusterInitialFormationStageClusterFormation SearchHeadClusterInitialFormationStage = "ClusterFormation"
	// SearchHeadClusterInitialFormationStageTelemetryPending allows the
	// controller to apply the Operator telemetry bundle before exposing the
	// newly formed cluster.
	SearchHeadClusterInitialFormationStageTelemetryPending SearchHeadClusterInitialFormationStage = "TelemetryPending"
	// SearchHeadClusterInitialFormationStageTelemetryApplied waits for the
	// telemetry bundle's Splunk-managed restart to settle.
	SearchHeadClusterInitialFormationStageTelemetryApplied SearchHeadClusterInitialFormationStage = "TelemetryApplied"
	// SearchHeadClusterInitialFormationStageAppFrameworkPending allows initial
	// App Framework work and its deployer bundle push to finish.
	SearchHeadClusterInitialFormationStageAppFrameworkPending SearchHeadClusterInitialFormationStage = "AppFrameworkPending"
	// SearchHeadClusterInitialFormationStageFinalStabilization waits for the
	// SHC to remain continuously healthy after initial App Framework work.
	SearchHeadClusterInitialFormationStageFinalStabilization SearchHeadClusterInitialFormationStage = "FinalStabilization"
	// SearchHeadClusterInitialFormationStageComplete permits the initial
	// topology to enter Kubernetes Service traffic.
	SearchHeadClusterInitialFormationStageComplete SearchHeadClusterInitialFormationStage = "Complete"
)

// SearchHeadClusterLifecycleIntent identifies why a durable lifecycle
// operation exists.
type SearchHeadClusterLifecycleIntent string

const (
	SearchHeadClusterLifecycleIntentPodUpdate       SearchHeadClusterLifecycleIntent = "PodUpdate"
	SearchHeadClusterLifecycleIntentScaleDown       SearchHeadClusterLifecycleIntent = "ScaleDown"
	SearchHeadClusterLifecycleIntentClusterDeletion SearchHeadClusterLifecycleIntent = "ClusterDeletion"
	SearchHeadClusterLifecycleIntentRecovery        SearchHeadClusterLifecycleIntent = "Recovery"
)

// SearchHeadClusterLifecycleStage identifies the durable stage of a lifecycle
// operation.
type SearchHeadClusterLifecycleStage string

const (
	SearchHeadClusterLifecycleStageValidatingCluster           SearchHeadClusterLifecycleStage = "ValidatingCluster"
	SearchHeadClusterLifecycleStageDetainingTarget             SearchHeadClusterLifecycleStage = "DetainingTarget"
	SearchHeadClusterLifecycleStageDrainingSearches            SearchHeadClusterLifecycleStage = "DrainingSearches"
	SearchHeadClusterLifecycleStageTransferringCaptain         SearchHeadClusterLifecycleStage = "TransferringCaptain"
	SearchHeadClusterLifecycleStageAuthorizingReplacement      SearchHeadClusterLifecycleStage = "AuthorizingReplacement"
	SearchHeadClusterLifecycleStageWaitingForTermination       SearchHeadClusterLifecycleStage = "WaitingForTermination"
	SearchHeadClusterLifecycleStageWaitingForScheduling        SearchHeadClusterLifecycleStage = "WaitingForScheduling"
	SearchHeadClusterLifecycleStageWaitingForPodInfrastructure SearchHeadClusterLifecycleStage = "WaitingForPodInfrastructure"
	SearchHeadClusterLifecycleStageWaitingForStorage           SearchHeadClusterLifecycleStage = "WaitingForStorage"
	SearchHeadClusterLifecycleStageWaitingForContainer         SearchHeadClusterLifecycleStage = "WaitingForContainer"
	SearchHeadClusterLifecycleStageWaitingForMemberRejoin      SearchHeadClusterLifecycleStage = "WaitingForMemberRejoin"
	SearchHeadClusterLifecycleStageValidatingRecovery          SearchHeadClusterLifecycleStage = "ValidatingRecovery"
	SearchHeadClusterLifecycleStageFinalizingClusterDeletion   SearchHeadClusterLifecycleStage = "FinalizingClusterDeletion"
	SearchHeadClusterLifecycleStageCompleted                   SearchHeadClusterLifecycleStage = "Completed"
	SearchHeadClusterLifecycleStageBlocked                     SearchHeadClusterLifecycleStage = "Blocked"
	SearchHeadClusterLifecycleStageFailed                      SearchHeadClusterLifecycleStage = "Failed"
)

// SearchHeadClusterLifecycleReason is a bounded, machine-readable explanation
// for the current lifecycle operation state.
type SearchHeadClusterLifecycleReason string

const (
	SearchHeadClusterLifecycleReasonOperationStarted                SearchHeadClusterLifecycleReason = "OperationStarted"
	SearchHeadClusterLifecycleReasonClusterNotSafe                  SearchHeadClusterLifecycleReason = "ClusterNotSafe"
	SearchHeadClusterLifecycleReasonObservationStale                SearchHeadClusterLifecycleReason = "ObservationStale"
	SearchHeadClusterLifecycleReasonConflictingCaptainObservation   SearchHeadClusterLifecycleReason = "ConflictingCaptainObservation"
	SearchHeadClusterLifecycleReasonDetentionRequested              SearchHeadClusterLifecycleReason = "DetentionRequested"
	SearchHeadClusterLifecycleReasonDetentionTimedOut               SearchHeadClusterLifecycleReason = "DetentionTimedOut"
	SearchHeadClusterLifecycleReasonKVStoreNotReady                 SearchHeadClusterLifecycleReason = "KVStoreNotReady"
	SearchHeadClusterLifecycleReasonDetentionReleasePending         SearchHeadClusterLifecycleReason = "DetentionReleasePending"
	SearchHeadClusterLifecycleReasonDetentionReleaseTimedOut        SearchHeadClusterLifecycleReason = "DetentionReleaseTimedOut"
	SearchHeadClusterLifecycleReasonSearchesActive                  SearchHeadClusterLifecycleReason = "SearchesActive"
	SearchHeadClusterLifecycleReasonSearchDrainTimedOut             SearchHeadClusterLifecycleReason = "SearchDrainTimedOut"
	SearchHeadClusterLifecycleReasonSearchDrainContinuationApproved SearchHeadClusterLifecycleReason = "SearchDrainContinuationApproved"
	SearchHeadClusterLifecycleReasonCaptainTransferRequired         SearchHeadClusterLifecycleReason = "CaptainTransferRequired"
	SearchHeadClusterLifecycleReasonCaptainTransferTimedOut         SearchHeadClusterLifecycleReason = "CaptainTransferTimedOut"
	SearchHeadClusterLifecycleReasonReplacementAuthorized           SearchHeadClusterLifecycleReason = "ReplacementAuthorized"
	SearchHeadClusterLifecycleReasonPodTerminationTimedOut          SearchHeadClusterLifecycleReason = "PodTerminationTimedOut"
	SearchHeadClusterLifecycleReasonPodUnschedulable                SearchHeadClusterLifecycleReason = "PodUnschedulable"
	SearchHeadClusterLifecycleReasonPodInfrastructurePending        SearchHeadClusterLifecycleReason = "PodInfrastructurePending"
	SearchHeadClusterLifecycleReasonPodRevisionMismatch             SearchHeadClusterLifecycleReason = "PodRevisionMismatch"
	SearchHeadClusterLifecycleReasonVolumeAttachmentPending         SearchHeadClusterLifecycleReason = "VolumeAttachmentPending"
	SearchHeadClusterLifecycleReasonImagePullFailed                 SearchHeadClusterLifecycleReason = "ImagePullFailed"
	SearchHeadClusterLifecycleReasonPodStartupTimedOut              SearchHeadClusterLifecycleReason = "PodStartupTimedOut"
	SearchHeadClusterLifecycleReasonSplunkStartupFailed             SearchHeadClusterLifecycleReason = "SplunkStartupFailed"
	SearchHeadClusterLifecycleReasonAuthorizedRevisionWithdrawn     SearchHeadClusterLifecycleReason = "AuthorizedRevisionWithdrawn"
	SearchHeadClusterLifecycleReasonMemberNotRegistered             SearchHeadClusterLifecycleReason = "MemberNotRegistered"
	SearchHeadClusterLifecycleReasonMemberNotUp                     SearchHeadClusterLifecycleReason = "MemberNotUp"
	SearchHeadClusterLifecycleReasonMemberIdentityMismatch          SearchHeadClusterLifecycleReason = "MemberIdentityMismatch"
	SearchHeadClusterLifecycleReasonMemberSynchronizationPending    SearchHeadClusterLifecycleReason = "MemberSynchronizationPending"
	SearchHeadClusterLifecycleReasonMemberRejoinTimedOut            SearchHeadClusterLifecycleReason = "MemberRejoinTimedOut"
	SearchHeadClusterLifecycleReasonRecoveryValidated               SearchHeadClusterLifecycleReason = "RecoveryValidated"
	SearchHeadClusterLifecycleReasonScaleDownCancelled              SearchHeadClusterLifecycleReason = "ScaleDownCancelled"
	SearchHeadClusterLifecycleReasonPodUpdateCancelled              SearchHeadClusterLifecycleReason = "PodUpdateCancelled"
	SearchHeadClusterLifecycleReasonClusterDeletionRequested        SearchHeadClusterLifecycleReason = "ClusterDeletionRequested"
	SearchHeadClusterLifecycleReasonOperationCompleted              SearchHeadClusterLifecycleReason = "OperationCompleted"
	SearchHeadClusterLifecycleReasonUnsupportedRuntimeContract      SearchHeadClusterLifecycleReason = "UnsupportedRuntimeContract"
)

// SearchHeadClusterImageUpgradePhase identifies the durable cluster-wide
// phase of an image upgrade. Per-member replacement remains in
// LifecycleOperation.
type SearchHeadClusterImageUpgradePhase string

const (
	SearchHeadClusterImageUpgradePhasePendingInitialization SearchHeadClusterImageUpgradePhase = "PendingInitialization"
	SearchHeadClusterImageUpgradePhaseInitializing          SearchHeadClusterImageUpgradePhase = "Initializing"
	SearchHeadClusterImageUpgradePhaseRollingMembers        SearchHeadClusterImageUpgradePhase = "RollingMembers"
	SearchHeadClusterImageUpgradePhasePendingFinalization   SearchHeadClusterImageUpgradePhase = "PendingFinalization"
	SearchHeadClusterImageUpgradePhaseFinalizing            SearchHeadClusterImageUpgradePhase = "Finalizing"
	SearchHeadClusterImageUpgradePhaseCompleted             SearchHeadClusterImageUpgradePhase = "Completed"
	SearchHeadClusterImageUpgradePhaseBlocked               SearchHeadClusterImageUpgradePhase = "Blocked"
	SearchHeadClusterImageUpgradePhaseFailed                SearchHeadClusterImageUpgradePhase = "Failed"
)

// SearchHeadClusterImageUpgradeReason is a bounded, machine-readable
// explanation for the current cluster-wide image-upgrade state.
type SearchHeadClusterImageUpgradeReason string

const (
	SearchHeadClusterImageUpgradeReasonWorkflowRecorded             SearchHeadClusterImageUpgradeReason = "WorkflowRecorded"
	SearchHeadClusterImageUpgradeReasonInitializationIntentRecorded SearchHeadClusterImageUpgradeReason = "InitializationIntentRecorded"
	SearchHeadClusterImageUpgradeReasonInitializationRetrying       SearchHeadClusterImageUpgradeReason = "InitializationRetrying"
	SearchHeadClusterImageUpgradeReasonInitializationSucceeded      SearchHeadClusterImageUpgradeReason = "InitializationSucceeded"
	SearchHeadClusterImageUpgradeReasonMemberLifecycleInProgress    SearchHeadClusterImageUpgradeReason = "MemberLifecycleInProgress"
	SearchHeadClusterImageUpgradeReasonMemberRecovered              SearchHeadClusterImageUpgradeReason = "MemberRecovered"
	SearchHeadClusterImageUpgradeReasonAllMembersRecovered          SearchHeadClusterImageUpgradeReason = "AllMembersRecovered"
	SearchHeadClusterImageUpgradeReasonFinalizationIntentRecorded   SearchHeadClusterImageUpgradeReason = "FinalizationIntentRecorded"
	SearchHeadClusterImageUpgradeReasonFinalizationRetrying         SearchHeadClusterImageUpgradeReason = "FinalizationRetrying"
	SearchHeadClusterImageUpgradeReasonFinalizationSucceeded        SearchHeadClusterImageUpgradeReason = "FinalizationSucceeded"
	SearchHeadClusterImageUpgradeReasonUnsupportedUpgradePath       SearchHeadClusterImageUpgradeReason = "UnsupportedUpgradePath"
	SearchHeadClusterImageUpgradeReasonUnknownUpgradePath           SearchHeadClusterImageUpgradeReason = "UnknownUpgradePath"
	SearchHeadClusterImageUpgradeReasonImageUpdateIntentMismatch    SearchHeadClusterImageUpgradeReason = "ImageUpdateIntentMismatch"
	SearchHeadClusterImageUpgradeReasonRevisionConflict             SearchHeadClusterImageUpgradeReason = "RevisionConflict"
	SearchHeadClusterImageUpgradeReasonTargetImageConflict          SearchHeadClusterImageUpgradeReason = "TargetImageConflict"
	SearchHeadClusterImageUpgradeReasonReplicaConflict              SearchHeadClusterImageUpgradeReason = "ReplicaConflict"
	SearchHeadClusterImageUpgradeReasonMixedSourceImages            SearchHeadClusterImageUpgradeReason = "MixedSourceImages"
	SearchHeadClusterImageUpgradeReasonConflictingPlannedOperation  SearchHeadClusterImageUpgradeReason = "ConflictingPlannedOperation"
	SearchHeadClusterImageUpgradeReasonClusterNotReady              SearchHeadClusterImageUpgradeReason = "ClusterNotReady"
	SearchHeadClusterImageUpgradeReasonMemberLifecycleBlocked       SearchHeadClusterImageUpgradeReason = "MemberLifecycleBlocked"
	SearchHeadClusterImageUpgradeReasonOperationCompleted           SearchHeadClusterImageUpgradeReason = "OperationCompleted"
)

// SearchHeadClusterImageUpgradeStatus records one cluster-wide image-upgrade
// workflow across all per-member lifecycle operations.
type SearchHeadClusterImageUpgradeStatus struct {
	OperationID     string `json:"operationID"`
	StatefulSetName string `json:"statefulSetName"`
	DesiredRevision string `json:"desiredRevision"`
	SourceImage     string `json:"sourceImage"`
	TargetImage     string `json:"targetImage"`
	TargetReplicas  int32  `json:"targetReplicas"`

	Phase   SearchHeadClusterImageUpgradePhase  `json:"phase"`
	Reason  SearchHeadClusterImageUpgradeReason `json:"reason,omitempty"`
	Message string                              `json:"message,omitempty"`

	StartedAt          *metav1.Time `json:"startedAt,omitempty"`
	PhaseStartedAt     *metav1.Time `json:"phaseStartedAt,omitempty"`
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`

	InitializationIntentAt      *metav1.Time `json:"initializationIntentAt,omitempty"`
	InitializationLastAttemptAt *metav1.Time `json:"initializationLastAttemptAt,omitempty"`
	InitializationSucceededAt   *metav1.Time `json:"initializationSucceededAt,omitempty"`
	InitializationAttemptCount  int32        `json:"initializationAttemptCount,omitempty"`
	CompletedOrdinals           []int32      `json:"completedOrdinals,omitempty"`
	FinalizationIntentAt        *metav1.Time `json:"finalizationIntentAt,omitempty"`
	FinalizationLastAttemptAt   *metav1.Time `json:"finalizationLastAttemptAt,omitempty"`
	FinalizationSucceededAt     *metav1.Time `json:"finalizationSucceededAt,omitempty"`
	FinalizationAttemptCount    int32        `json:"finalizationAttemptCount,omitempty"`
	CompletedAt                 *metav1.Time `json:"completedAt,omitempty"`
}

// SearchHeadClusterLifecycleOperationStatus records enough information to
// resume and diagnose one lifecycle operation across reconciliations.
type SearchHeadClusterLifecycleOperationStatus struct {
	OperationID                string                           `json:"operationID"`
	Intent                     SearchHeadClusterLifecycleIntent `json:"intent"`
	DesiredRevision            string                           `json:"desiredRevision,omitempty"`
	TargetPod                  string                           `json:"targetPod,omitempty"`
	TargetOrdinal              *int32                           `json:"targetOrdinal,omitempty"`
	Stage                      SearchHeadClusterLifecycleStage  `json:"stage"`
	StartedAt                  *metav1.Time                     `json:"startedAt,omitempty"`
	StageStartedAt             *metav1.Time                     `json:"stageStartedAt,omitempty"`
	LastTransitionTime         *metav1.Time                     `json:"lastTransitionTime,omitempty"`
	CompletedOrdinals          []int32                          `json:"completedOrdinals,omitempty"`
	RetryCount                 int32                            `json:"retryCount,omitempty"`
	Reason                     SearchHeadClusterLifecycleReason `json:"reason,omitempty"`
	Message                    string                           `json:"message,omitempty"`
	Captain                    string                           `json:"captain,omitempty"`
	CaptainReady               bool                             `json:"captainReady,omitempty"`
	CaptainTransferTarget      string                           `json:"captainTransferTarget,omitempty"`
	CaptainTransferRequestedAt *metav1.Time                     `json:"captainTransferRequestedAt,omitempty"`
	TargetPodUID               string                           `json:"targetPodUID,omitempty"`
	TargetMemberID             string                           `json:"targetMemberID,omitempty"`
	ReplacementPodUID          string                           `json:"replacementPodUID,omitempty"`
	ReplacementMemberID        string                           `json:"replacementMemberID,omitempty"`
	ReplacementAuthorizedAt    *metav1.Time                     `json:"replacementAuthorizedAt,omitempty"`
	ReplacementPodObservedAt   *metav1.Time                     `json:"replacementPodObservedAt,omitempty"`
	// RecoveryRevision is the last known-good StatefulSet revision used to
	// recover a single unavailable target after its authorized desired revision
	// was withdrawn or superseded. DesiredRevision remains the failed
	// authorization for auditability.
	RecoveryRevision string `json:"recoveryRevision,omitempty"`
	// RevisionWithdrawalStartedAt records the durable barrier before the
	// StatefulSet partition is raised and the withdrawn target is gracefully
	// recycled at RecoveryRevision.
	RevisionWithdrawalStartedAt  *metav1.Time `json:"revisionWithdrawalStartedAt,omitempty"`
	MembershipRemovalRequestedAt *metav1.Time `json:"membershipRemovalRequestedAt,omitempty"`
	MemberRejoinStartedAt        *metav1.Time `json:"memberRejoinStartedAt,omitempty"`
	DetentionRequestedAt         *metav1.Time `json:"detentionRequestedAt,omitempty"`
	DetentionRequestAttemptCount int32        `json:"detentionRequestAttemptCount,omitempty"`
	DetentionReleaseRequestedAt  *metav1.Time `json:"detentionReleaseRequestedAt,omitempty"`
	// SearchDrainContinuationToken is issued only after this operation reaches
	// a search-drain timeout and is required for an exact post-timeout approval.
	SearchDrainContinuationToken string `json:"searchDrainContinuationToken,omitempty"`
	// SearchDrainContinuationApprovedAt records when the controller accepted
	// the matching operation-scoped approval.
	SearchDrainContinuationApprovedAt *metav1.Time `json:"searchDrainContinuationApprovedAt,omitempty"`
	// SearchDrainContinuationApprovalGeneration records the CR generation
	// whose spec contained the accepted approval.
	SearchDrainContinuationApprovalGeneration int64 `json:"searchDrainContinuationApprovalGeneration,omitempty"`
	// ApprovedActiveHistoricalSearches preserves the historical-search count
	// observed when continuation was approved.
	ApprovedActiveHistoricalSearches int32 `json:"approvedActiveHistoricalSearches,omitempty"`
	// ApprovedActiveRealtimeSearches preserves the real-time-search count
	// observed when continuation was approved.
	ApprovedActiveRealtimeSearches int32 `json:"approvedActiveRealtimeSearches,omitempty"`
	// KVStoreNotReadyMembers contains bounded member=status observations from
	// the most recent successful KV Store lifecycle preflight.
	KVStoreNotReadyMembers           []string     `json:"kvStoreNotReadyMembers,omitempty"`
	LastSuccessfulKVStoreObservation *metav1.Time `json:"lastSuccessfulKVStoreObservation,omitempty"`
	ActiveHistoricalSearches         int32        `json:"activeHistoricalSearches,omitempty"`
	ActiveRealtimeSearches           int32        `json:"activeRealtimeSearches,omitempty"`
	LastSuccessfulSHCObservation     *metav1.Time `json:"lastSuccessfulSHCObservation,omitempty"`
}

// SearchHeadClusterStatus defines the observed state of a Splunk Enterprise search head cluster
type SearchHeadClusterStatus struct {
	// current phase of the search head cluster
	Phase Phase `json:"phase"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// It corresponds to the metadata.generation which is updated on spec changes.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the resource's state.
	// Conditions are: Ready, Progressing, Paused
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// current phase of the deployer
	DeployerPhase Phase `json:"deployerPhase"`

	// desired number of search head cluster members
	Replicas int32 `json:"replicas"`

	// current number of ready search head cluster members
	ReadyReplicas int32 `json:"readyReplicas"`

	// last desired replica count for which the search head cluster reached Ready
	// +optional
	LastStableReplicas *int32 `json:"lastStableReplicas,omitempty"`

	// selector for pods, used by HorizontalPodAutoscaler
	Selector string `json:"selector"`

	// name or label of the search head captain
	Captain string `json:"captain"`

	// true if the search head cluster's captain is ready to service requests
	CaptainReady bool `json:"captainReady"`

	// true while the captain is coordinating an SHC rolling restart
	CaptainRollingRestart bool `json:"captainRollingRestart,omitempty"`

	// true when the controller successfully observed every desired member
	// through the captain's authoritative member endpoint
	CaptainMembersObserved bool `json:"captainMembersObserved,omitempty"`

	// durable initial-formation stage used to keep a new SHC out of Service
	// traffic through its required Splunk and deployer-managed restarts
	// +optional
	InitialFormationStage SearchHeadClusterInitialFormationStage `json:"initialFormationStage,omitempty"`

	// true after the controller has requested the supported first-formation
	// rolling restart; this prevents duplicate requests across reconciliations
	// +optional
	InitialFormationRestartInitiated bool `json:"initialFormationRestartInitiated,omitempty"`

	// beginning of the current uninterrupted initial-formation stabilization
	// interval
	// +optional
	InitialFormationStableSince *metav1.Time `json:"initialFormationStableSince,omitempty"`

	// true if the search head cluster has finished initialization
	Initialized bool `json:"initialized"`

	// true if the minimum number of search head cluster members have joined
	MinPeersJoined bool `json:"minPeersJoined"`

	// true if the search head cluster is in maintenance mode
	MaintenanceMode bool `json:"maintenanceMode"`

	// Indicates when the shc_secret has been changed for a peer
	ShcSecretChanged []bool `json:"shcSecretChangedFlag"`

	// Indicates when the admin password has been changed for a peer
	AdminSecretChanged []bool `json:"adminSecretChangedFlag"`

	// Holds secrets whose admin password has changed
	AdminPasswordChangedSecrets map[string]bool `json:"adminPasswordChangedSecrets"`

	// Indicates resource version of namespace scoped secret
	NamespaceSecretResourceVersion string `json:"namespace_scoped_secret_resource_version"`

	// status of each search head cluster member
	Members []SearchHeadClusterMemberStatus `json:"members"`

	// App Framework Context
	AppContext AppDeploymentContext `json:"appContext"`

	// AppFrameworkBundleRevision identifies the exact cluster-scoped App
	// Framework bundle currently being staged or sent. It is derived from the
	// durable app deployment records so an interrupted controller can resume the
	// same work.
	// +optional
	AppFrameworkBundleRevision string `json:"appFrameworkBundleRevision,omitempty"`

	// AppFrameworkRestartObservedRevision identifies the most recent bundle for
	// which the Operator obtained an authoritative captain-members view of the
	// restart requirement. It prevents a no-restart bundle from being observed
	// indefinitely while keeping the observation resumable across controller
	// replacement.
	// +optional
	AppFrameworkRestartObservedRevision string `json:"appFrameworkRestartObservedRevision,omitempty"`

	// AppFrameworkRestartRevision identifies the most recent bundle for which
	// Splunk advertised that Search Head members require a process restart. The
	// Search Head StatefulSet retains this value as a Pod-template annotation so
	// Kubernetes owns the replacement.
	// +optional
	AppFrameworkRestartRevision string `json:"appFrameworkRestartRevision,omitempty"`

	// Telemetry App installation flag
	TelAppInstalled bool `json:"telAppInstalled"`

	// Auxiliary message describing CR status
	Message string `json:"message"`

	UpgradePhase UpgradePhase `json:"upgradePhase"`

	UpgradeStartTimestamp int64 `json:"upgradeStartTimestamp"`

	UpgradeEndTimestamp int64 `json:"upgradeEndTimestamp"`

	// LifecycleOperation is the current operation or most recent terminal
	// result. It is not an unbounded history.
	// +optional
	LifecycleOperation *SearchHeadClusterLifecycleOperationStatus `json:"lifecycleOperation,omitempty"`

	// ImageUpgrade is the current or most recent cluster-wide image-upgrade
	// workflow. Per-member rollout state remains in LifecycleOperation.
	// +optional
	ImageUpgrade *SearchHeadClusterImageUpgradeStatus `json:"imageUpgrade,omitempty"`
}

type UpgradePhase string

const (
	UpgradePhaseUpgrading UpgradePhase = "Upgrading"
	UpgradePhaseUpgraded  UpgradePhase = "Upgraded"
)

// SearchHeadCluster is the Schema for a Splunk Enterprise search head cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:path=searchheadclusters,scope=Namespaced,shortName=shc
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of search head cluster"
// +kubebuilder:printcolumn:name="Deployer",type="string",JSONPath=".status.deployerPhase",description="Status of the deployer"
// +kubebuilder:printcolumn:name="Desired",type="integer",JSONPath=".status.replicas",description="Desired number of search head cluster members"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas",description="Current number of ready search head cluster members"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of search head cluster"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message",description="Auxiliary message describing CR status"
// +kubebuilder:storageversion
type SearchHeadCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SearchHeadClusterSpec   `json:"spec,omitempty"`
	Status SearchHeadClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// SearchHeadClusterList contains a list of SearchHeadCluster
type SearchHeadClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SearchHeadCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SearchHeadCluster{}, &SearchHeadClusterList{})
}
