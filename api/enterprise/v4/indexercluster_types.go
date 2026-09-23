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
	k8stypes "k8s.io/apimachinery/pkg/types"
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
// +kubebuilder:validation:XValidation:rule="!has(self.noahClusterRef) || ((!has(self.clusterManagerRef) || !has(self.clusterManagerRef.name) || self.clusterManagerRef.name == \"\") && (!has(self.clusterMasterRef) || !has(self.clusterMasterRef.name) || self.clusterMasterRef.name == \"\"))",message="noahClusterRef is mutually exclusive with clusterManagerRef and clusterMasterRef"
// +kubebuilder:validation:XValidation:rule="has(self.noahClusterRef) == has(oldSelf.noahClusterRef)",message="noahClusterRef cannot be added or removed after creation"
// +kubebuilder:validation:XValidation:rule="!has(self.noahClusterRef) || self.noahClusterRef.name == oldSelf.noahClusterRef.name",message="noahClusterRef.name is immutable once created"
// IndexerClusterSpec defines the desired state of a Splunk Enterprise indexer cluster
type IndexerClusterSpec struct {
	CommonSplunkSpec `json:",inline"`

	// +optional
	// Queue reference. NOTE: part of the index and ingestion separation feature, which is currently in Preview and not recommended for production use.
	QueueRef *corev1.ObjectReference `json:"queueRef,omitempty"`

	// +optional
	// Object Storage reference. NOTE: part of the index and ingestion separation feature, which is currently in Preview and not recommended for production use.
	ObjectStorageRef *corev1.ObjectReference `json:"objectStorageRef,omitempty"`

	// Number of indexer cluster peers
	Replicas int32 `json:"replicas"`

	// NoahClusterRef selects the Noah configuration used by this IndexerCluster.
	// The referenced NoahCluster must be in the same namespace.
	// +optional
	// +kubebuilder:validation:XValidation:rule="has(self.name) && self.name != ''",message="noahClusterRef.name must not be empty"
	NoahClusterRef *corev1.LocalObjectReference `json:"noahClusterRef,omitempty"`
}

// NoahEnabled reports whether this spec selects Noah mode.
func (s *IndexerClusterSpec) NoahEnabled() bool {
	return s != nil && s.NoahClusterRef != nil
}

// IndexerClusterLifecycleStatus records one restart-safe lifecycle operation.
// It is controller-owned and also acts as the lock that prevents concurrent
// IndexerCluster lifecycle actions.
type IndexerClusterLifecycleStatus struct {
	// Kind identifies the desired-state change being executed.
	// +kubebuilder:validation:Required
	Kind IndexerClusterLifecycleKind `json:"kind"`

	// Checkpoint identifies the durable point reached by the operation.
	// +kubebuilder:validation:Required
	Checkpoint IndexerClusterLifecycleCheckpoint `json:"checkpoint"`

	// Generation is the IndexerCluster generation that initiated the operation.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	Generation int64 `json:"generation"`

	// Target identifies the exact StatefulSet incarnation, replica and revision
	// boundaries, and peers covered by the operation.
	// +kubebuilder:validation:Required
	Target IndexerClusterLifecycleTarget `json:"target"`

	// PendingAction is the action currently authorized for execution. It is
	// cleared after its effect has been accepted.
	// +optional
	PendingAction *IndexerClusterLifecyclePendingAction `json:"pendingAction,omitempty"`

	// StartedAt records when the operation was authorized.
	// +kubebuilder:validation:Required
	StartedAt metav1.Time `json:"startedAt"`

	// LastTransitionTime records the last durable checkpoint or action change.
	// +kubebuilder:validation:Required
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`

	// CompletedAt records when all operation postconditions became true.
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`
}

// IndexerClusterLifecycleKind identifies the desired-state change being
// executed by the IndexerCluster lifecycle controller.
// +kubebuilder:validation:Enum=Rollout;ScaleIn;ScaleOut
type IndexerClusterLifecycleKind string

const (
	// IndexerClusterLifecycleRollout replaces peers at a new StatefulSet
	// revision.
	IndexerClusterLifecycleRollout IndexerClusterLifecycleKind = "Rollout"
	// IndexerClusterLifecycleScaleIn gracefully removes the highest ordinal.
	IndexerClusterLifecycleScaleIn IndexerClusterLifecycleKind = "ScaleIn"
	// IndexerClusterLifecycleScaleOut adds a contiguous batch of new ordinals.
	IndexerClusterLifecycleScaleOut IndexerClusterLifecycleKind = "ScaleOut"
)

// IndexerClusterLifecycleCheckpoint identifies the durable point reached by a
// lifecycle operation.
// +kubebuilder:validation:Enum=ActionPending;WaitingForMembership;Completed;Failed
type IndexerClusterLifecycleCheckpoint string

const (
	// IndexerClusterLifecycleActionPending means PendingAction is durably
	// authorized for execution.
	IndexerClusterLifecycleActionPending IndexerClusterLifecycleCheckpoint = "ActionPending"
	// IndexerClusterLifecycleWaitingForMembership means Kubernetes has converged
	// and the operation is waiting for membership-provider evidence.
	IndexerClusterLifecycleWaitingForMembership IndexerClusterLifecycleCheckpoint = "WaitingForMembership"
	// IndexerClusterLifecycleCompleted means all operation postconditions hold.
	IndexerClusterLifecycleCompleted IndexerClusterLifecycleCheckpoint = "Completed"
	// IndexerClusterLifecycleFailed means the operation cannot automatically
	// progress.
	IndexerClusterLifecycleFailed IndexerClusterLifecycleCheckpoint = "Failed"
)

// IndexerClusterLifecycleTarget identifies the exact workload and peers to
// which a lifecycle operation applies.
type IndexerClusterLifecycleTarget struct {
	// StatefulSetUID identifies the exact StatefulSet incarnation targeted by
	// the operation. A different live UID invalidates the operation instead of
	// transferring its authorization to a replacement StatefulSet.
	// +kubebuilder:validation:Required
	StatefulSetUID k8stypes.UID `json:"statefulSetUID"`

	// SourceReplicas is the StatefulSet replica count observed when the
	// operation was authorized.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=0
	SourceReplicas int32 `json:"sourceReplicas"`

	// TargetReplicas is the StatefulSet replica count requested by a scaling
	// action. It equals SourceReplicas for operations that do not scale.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=0
	TargetReplicas int32 `json:"targetReplicas"`

	// SourceRevision is the StatefulSet revision observed when the operation
	// was planned.
	// +optional
	SourceRevision string `json:"sourceRevision,omitempty"`

	// TargetRevision is the StatefulSet revision authorized when the operation
	// was planned. A ready replacement at a newer live revision is also accepted.
	// +optional
	TargetRevision string `json:"targetRevision,omitempty"`

	// Peers contains the exact peer identities covered by the operation. The
	// operation's policy determines whether one action addresses one peer
	// or a batch. The list is keyed by ordinal and its order is not significant.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +listType=map
	// +listMapKey=ordinal
	Peers []IndexerClusterLifecyclePeerTarget `json:"peers"`
}

// IndexerClusterLifecyclePeerTarget identifies one indexer peer affected by a
// lifecycle operation.
type IndexerClusterLifecyclePeerTarget struct {
	// Ordinal is the peer's StatefulSet ordinal.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=0
	Ordinal int32 `json:"ordinal"`

	// PeerID is the stable peer identifier expected from the membership
	// provider for the ordinal.
	// +kubebuilder:validation:Required
	PeerID string `json:"peerID"`

	// PodName is the Kubernetes Pod name expected for the ordinal.
	// +kubebuilder:validation:Required
	PodName string `json:"podName"`

	// SourcePodUID is the immutable identity fence for the Pod incarnation
	// against which a disruptive action is authorized. It is absent for a new
	// scale-out peer.
	// +optional
	SourcePodUID k8stypes.UID `json:"sourcePodUID,omitempty"`

	// TargetPodUID identifies the latest persisted new or replacement Pod
	// incarnation that must satisfy the operation's postconditions. It is
	// populated after that Pod is observed and is absent for a removed scale-in
	// peer.
	// +optional
	TargetPodUID k8stypes.UID `json:"targetPodUID,omitempty"`
}

// IndexerClusterLifecyclePendingAction is the single action durably authorized
// for execution. Its target and parameters come from the containing lifecycle
// record.
type IndexerClusterLifecyclePendingAction struct {
	// Type identifies the external action authorized by this record. Its
	// parameters and exact targets come from the containing lifecycle status.
	// +kubebuilder:validation:Required
	Type IndexerClusterLifecycleActionType `json:"type"`
}

// IndexerClusterLifecycleActionType identifies one externally meaningful
// action authorized by a persisted lifecycle operation.
// +kubebuilder:validation:Enum=SetReplicas;DeletePod
type IndexerClusterLifecycleActionType string

const (
	// IndexerClusterLifecycleSetReplicas changes the target StatefulSet to
	// TargetReplicas.
	IndexerClusterLifecycleSetReplicas IndexerClusterLifecycleActionType = "SetReplicas"
	// IndexerClusterLifecycleDeletePod deletes the exact source Pod selected for
	// a rollout.
	IndexerClusterLifecycleDeletePod IndexerClusterLifecycleActionType = "DeletePod"
)

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

	// Auxiliary message describing CR status
	Message string `json:"message"`

	// Lifecycle records the active or retained terminal lifecycle operation. It
	// is controller-owned and is absent when no operation is retained.
	// +optional
	Lifecycle *IndexerClusterLifecycleStatus `json:"lifecycle,omitempty"`
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

// Hub marks v4 as the conversion hub for IndexerCluster
func (*IndexerCluster) Hub() {}

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
