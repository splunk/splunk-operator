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
	// IndexerClusterPausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	IndexerClusterPausedAnnotation = "indexercluster.enterprise.splunk.com/paused"
)

// +kubebuilder:validation:XValidation:rule="has(self.queueRef) == has(self.objectStorageRef)",message="queueRef and objectStorageRef must both be set or both be empty"
// IndexerClusterSpec defines the desired state of a Splunk Enterprise indexer cluster
type IndexerClusterSpec struct {
	CommonSplunkSpec `json:",inline"`

	// LifecyclePolicy configures the Kubernetes traffic-withdrawal barrier
	// used before an Operator-owned indexer decommission.
	// +optional
	LifecyclePolicy *IndexerClusterLifecyclePolicy `json:"lifecyclePolicy,omitempty"`

	// +optional
	// Queue reference. NOTE: part of the index and ingestion separation feature, which is currently in Preview and not recommended for production use.
	QueueRef *corev1.ObjectReference `json:"queueRef,omitempty"`

	// +optional
	// Object Storage reference. NOTE: part of the index and ingestion separation feature, which is currently in Preview and not recommended for production use.
	ObjectStorageRef *corev1.ObjectReference `json:"objectStorageRef,omitempty"`

	// Number of indexer cluster peers
	Replicas int32 `json:"replicas"`
}

// IndexerClusterLifecyclePolicy configures lifecycle timing for an
// Operator-owned indexer Pod update.
type IndexerClusterLifecyclePolicy struct {
	// EndpointWithdrawalDelaySeconds is the minimum quiescence period after
	// the target is no longer routable through the Indexer Service's
	// EndpointSlices and before Splunk decommission begins. It gives
	// kube-proxy and other EndpointSlice consumers time to apply the withdrawal.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=86400
	EndpointWithdrawalDelaySeconds *int64 `json:"endpointWithdrawalDelaySeconds,omitempty"`
}

// IndexerClusterMemberStatus is used to track the status of each indexer cluster peer.
type IndexerClusterMemberStatus struct {
	// Unique identifier or GUID for the peer
	ID string `json:"guid"`

	// Name of the indexer cluster peer
	Name string `json:"name"`

	// Status of the indexer cluster peer
	Status string `json:"status"`

	// The ID of the configuration bundle currently being used by the manager.
	ActiveBundleID string `json:"active_bundle_id"`

	// Count of the number of buckets on this peer, across all indexes.
	BucketCount int64 `json:"bucket_count"`

	// Flag indicating if this peer belongs to the current committed generation and is searchable.
	Searchable bool `json:"is_searchable"`
}

// IndexerClusterPodUpdateStage identifies the durable stage of one
// Operator-owned indexer Pod update.
type IndexerClusterPodUpdateStage string

const (
	IndexerClusterPodUpdateStageTargetSelected                IndexerClusterPodUpdateStage = "TargetSelected"
	IndexerClusterPodUpdateStageWithdrawingReadiness          IndexerClusterPodUpdateStage = "WithdrawingReadiness"
	IndexerClusterPodUpdateStageDecommissioning               IndexerClusterPodUpdateStage = "Decommissioning"
	IndexerClusterPodUpdateStageReadyForReplacement           IndexerClusterPodUpdateStage = "ReadyForReplacement"
	IndexerClusterPodUpdateStageAwaitingSearchPeerConvergence IndexerClusterPodUpdateStage = "AwaitingSearchPeerConvergence"
	IndexerClusterPodUpdateStageCompleted                     IndexerClusterPodUpdateStage = "Completed"
	IndexerClusterPodUpdateStageCancelled                     IndexerClusterPodUpdateStage = "Cancelled"
)

// IndexerClusterPodUpdateStatus records exact ownership of one indexer Pod
// update. It is persisted before decommission so the controller can
// distinguish its deliberately unavailable target from an unrelated failure.
type IndexerClusterPodUpdateStatus struct {
	// Stable operation identity derived from target UID, desired revision, and
	// selection time so a cancelled target can later retry the same revision.
	OperationID string `json:"operationID"`

	// Current lifecycle stage.
	Stage IndexerClusterPodUpdateStage `json:"stage"`

	// Exact target identity.
	TargetPod     string `json:"targetPod"`
	TargetPodUID  string `json:"targetPodUID"`
	TargetOrdinal int32  `json:"targetOrdinal"`

	// StatefulSet revision boundary authorized by this operation.
	SourceRevision  string `json:"sourceRevision"`
	DesiredRevision string `json:"desiredRevision"`

	// When target ownership was first persisted.
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// When the current stage began.
	StageStartedAt *metav1.Time `json:"stageStartedAt,omitempty"`

	// When decommission was requested, or when the controller first recovered
	// an already accepted request from a controlled Cluster Manager peer state.
	DecommissionRequestedAt *metav1.Time `json:"decommissionRequestedAt,omitempty"`

	// Whether the target was observed outside Up after decommission was
	// requested. This prevents an unchanged, eventually consistent Up response
	// from being mistaken for a completed decommission cycle.
	ObservedDecommissioning bool `json:"observedDecommissioning,omitempty"`

	// When Kubernetes first showed both the target Pod as not Ready and no
	// routable target entry in the Indexer Service's EndpointSlices.
	EndpointWithdrawalObservedAt *metav1.Time `json:"endpointWithdrawalObservedAt,omitempty"`

	// Durable end of the propagation delay calculated from the effective
	// lifecycle policy when this withdrawal sequence was observed.
	EndpointWithdrawalDeadline *metav1.Time `json:"endpointWithdrawalDeadline,omitempty"`

	// Exact target UID covered by the endpoint-withdrawal observation.
	EndpointWithdrawalPodUID string `json:"endpointWithdrawalPodUID,omitempty"`

	// Monotonic observation sequence used to prevent a stale status writer
	// from restoring an earlier withdrawal observation.
	EndpointWithdrawalSequence int64 `json:"endpointWithdrawalSequence,omitempty"`

	// Latest endpoint-withdrawal sequence invalidated because the target became
	// routable again before the propagation delay elapsed.
	EndpointWithdrawalInvalidatedSequence int64 `json:"endpointWithdrawalInvalidatedSequence,omitempty"`

	// Most recent stage transition time.
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`

	// Machine-readable reason and human-readable detail for the most recent
	// lifecycle observation.
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`

	// Identity of the accepted replacement and the time this operation reached
	// a terminal Completed or Cancelled stage.
	ReplacementPodUID string       `json:"replacementPodUID,omitempty"`
	FinishedAt        *metav1.Time `json:"finishedAt,omitempty"`

	// When Kubernetes endpoint publication and an independent Pod-to-Pod
	// request first proved that the replacement's enabled HEC path was
	// remotely serving. For HEC-disabled deployments, the proof uses a remote
	// connection to the replacement's declared Splunk-to-Splunk port.
	ServingRecoveryObservedAt *metav1.Time `json:"servingRecoveryObservedAt,omitempty"`

	// Exact replacement UID covered by the serving-recovery observation.
	ServingRecoveryPodUID string `json:"servingRecoveryPodUID,omitempty"`

	// Monotonic observation sequence used to prevent a stale status writer
	// from restoring proof for an earlier replacement UID.
	ServingRecoverySequence int64 `json:"servingRecoverySequence,omitempty"`

	// When every Search Head managed in this namespace for the referenced
	// Cluster Manager first reported exactly one current, enabled, Up entry for
	// the replacement peer GUID and address.
	SearchPeerConvergenceObservedAt *metav1.Time `json:"searchPeerConvergenceObservedAt,omitempty"`

	// Exact replacement UID covered by the Search Head convergence observation.
	SearchPeerConvergencePodUID string `json:"searchPeerConvergencePodUID,omitempty"`

	// Monotonic observation sequence used to prevent a stale status writer
	// from restoring proof for an earlier replacement UID.
	SearchPeerConvergenceSequence int64 `json:"searchPeerConvergenceSequence,omitempty"`

	// Latest Search Head convergence observation sequence invalidated by a
	// subsequent failed observation. Keeping invalidation monotonic preserves
	// the audit trail without allowing a stale status writer to resurrect an
	// earlier successful observation.
	SearchPeerConvergenceInvalidatedSequence int64 `json:"searchPeerConvergenceInvalidatedSequence,omitempty"`
}

// IndexerClusterStatus defines the observed state of a Splunk Enterprise indexer cluster
type IndexerClusterStatus struct {
	// current phase of the indexer cluster
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

	// current phase of the cluster master
	// +optional
	ClusterMasterPhase Phase `json:"clusterMasterPhase,omitempty"`

	// current phase of the cluster manager
	// +optional
	ClusterManagerPhase Phase `json:"clusterManagerPhase,omitempty"`

	// desired number of indexer peers
	Replicas int32 `json:"replicas"`

	// current number of ready indexer peers
	ReadyReplicas int32 `json:"readyReplicas"`

	// selector for pods, used by HorizontalPodAutoscaler
	Selector string `json:"selector"`

	// Indicates if the cluster is initialized.
	Initialized bool `json:"initialized_flag"`

	// Indicates if the cluster is ready for indexing.
	IndexingReady bool `json:"indexing_ready_flag"`

	// Indicates whether the manager is ready to begin servicing, based on whether it is initialized.
	ServiceReady bool `json:"service_ready_flag"`

	// Indicates when the idxc_secret has been changed for a peer
	IndexerSecretChanged []bool `json:"indexer_secret_changed_flag"`

	// Indicates resource version of namespace scoped secret
	NamespaceSecretResourceVersion string `json:"namespace_scoped_secret_resource_version"`

	// Holds secrets whose IDXC password has changed
	IdxcPasswordChangedSecrets map[string]bool `json:"IdxcPasswordChangedSecrets"`

	// Indicates if the cluster is in maintenance mode.
	MaintenanceMode bool `json:"maintenance_mode"`

	// status of each indexer cluster peer
	Peers []IndexerClusterMemberStatus `json:"peers"`

	// Current or most recently completed Operator-owned Pod update.
	// +optional
	PodUpdate *IndexerClusterPodUpdateStatus `json:"podUpdate,omitempty"`

	// Auxiliary message describing CR status
	Message string `json:"message"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// IndexerCluster is the Schema for a Splunk Enterprise indexer cluster
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:path=indexerclusters,scope=Namespaced,shortName=idc;idxc
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of indexer cluster"
// +kubebuilder:printcolumn:name="Master",type="string",JSONPath=".status.clusterMasterPhase",description="Status of cluster master"
// +kubebuilder:printcolumn:name="Manager",type="string",JSONPath=".status.clusterManagerPhase",description="Status of cluster manager"
// +kubebuilder:printcolumn:name="Desired",type="integer",JSONPath=".status.replicas",description="Desired number of indexer peers"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas",description="Current number of ready indexer peers"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of indexer cluster"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message",description="Auxiliary message describing CR status"
// +kubebuilder:storageversion
type IndexerCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IndexerClusterSpec   `json:"spec,omitempty"`
	Status IndexerClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// IndexerClusterList contains a list of IndexerCluster
type IndexerClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IndexerCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IndexerCluster{}, &IndexerClusterList{})
}
