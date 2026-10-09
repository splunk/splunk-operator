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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// default all fields to being optional
// +kubebuilder:validation:Optional

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
// see also https://book.kubebuilder.io/reference/markers/crd.html

const (
	// ClusterManagerPausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	ClusterManagerPausedAnnotation = "clustermanager.enterprise.splunk.com/paused"
)

// RecoveryPhase describes the current phase of node failure recovery for a ClusterManager
type RecoveryPhase string

const (
	RecoveryPhaseHealthy               RecoveryPhase = "Healthy"
	RecoveryPhaseWaitingForGracePeriod RecoveryPhase = "WaitingForGracePeriod"
	RecoveryPhaseBlocked               RecoveryPhase = "Blocked"
	RecoveryPhaseRequested             RecoveryPhase = "Requested"
	RecoveryPhaseReplacementPending    RecoveryPhase = "ReplacementPending"
	RecoveryPhaseReadyAfterRecovery    RecoveryPhase = "ReadyAfterRecovery"
	RecoveryPhaseFailed                RecoveryPhase = "Failed"
)

// ClusterManagerSpec defines the desired state of ClusterManager
type ClusterManagerSpec struct {
	CommonSplunkSpec `json:",inline"`

	// Splunk Smartstore configuration. Refer to indexes.conf.spec and server.conf.spec on docs.splunk.com
	// +optional
	SmartStore SmartStoreSpec `json:"smartstore,omitempty"`

	// Splunk Enterprise App repository. Specifies remote App location and scope for Splunk App management
	AppFrameworkConfig AppFrameworkSpec `json:"appRepo,omitempty"`

	// Recovery defines the node failure recovery configuration for this clusterManager
	// +optional
	Recovery *ClusterManagerRecoveryConfig `json:"recovery,omitempty"`
}

// ClusterManagerStatus defines the observed state of ClusterManager
type ClusterManagerStatus struct {
	// current phase of the cluster manager
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

	// selector for pods, used by HorizontalPodAutoscaler
	Selector string `json:"selector"`

	// Splunk Smartstore configuration. Refer to indexes.conf.spec and server.conf.spec on docs.splunk.com
	SmartStore SmartStoreSpec `json:"smartstore,omitempty"`

	// Bundle push status tracker
	BundlePushTracker BundlePushInfo `json:"bundlePushInfo"`

	// Resource Revision tracker
	ResourceRevMap map[string]string `json:"resourceRevMap"`

	// App Framework status
	AppContext AppDeploymentContext `json:"appContext"`

	// Telemetry App installation flag
	TelAppInstalled bool `json:"telAppInstalled"`

	// Auxiliary message describing CR status
	Message string `json:"message"`

	// Recovery describes current observed recovery state of this ClusterManager
	// +optional
	Recovery ClusterManagerRecoveryStatus `json:"recovery,omitempty"`
}

// BundlePushInfo Indicates if bundle push required
type BundlePushInfo struct {
	NeedToPushMasterApps  bool  `json:"needToPushMasterApps"` // NeedToPushMasterApps is an exception needed for dual support
	NeedToPushManagerApps bool  `json:"needToPushManagerApps"`
	LastCheckInterval     int64 `json:"lastCheckInterval"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterManager is the Schema for the cluster manager API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=clustermanagers,scope=Namespaced,shortName=cmanager-idxc
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Phase of the cluster manager"
// +kubebuilder:printcolumn:name="Manager",type="string",JSONPath=".status.clusterManagerPhase",description="Status of cluster manager"
// +kubebuilder:printcolumn:name="Desired",type="integer",JSONPath=".status.replicas",description="Desired number of indexer peers"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas",description="Current number of ready indexer peers"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of cluster manager"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message",description="Auxiliary message describing CR status"
// +kubebuilder:storageversion
type ClusterManager struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterManagerSpec   `json:"spec,omitempty"`
	Status ClusterManagerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterManagerList contains a list of ClusterManager
type ClusterManagerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterManager `json:"items"`
}

// ClusterManagerRecoveryConfig defines the node failure configuration for a ClusterManager
type ClusterManagerRecoveryConfig struct {
	// Enabled activates node failure recovery for ClusterManager.
	// When false, the controller takes no automatic action on node failure.
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// GracePeriodSeconds is the minimum time the controller waits after observing
	// a NotReady node condition before deleting the stale ClusterManager pod
	// +optional
	// +kubebuilder:validation:Minimum=30
	// +kubebuilder:default=300
	GracePeriodSeconds int32 `json:"gracePeriodSeconds,omitempty"`
}

// ClusterManagerRecoveryStatus describes the observed recovery state of a Cluster Manager.
// All fields are derived from live Kubernetes resources on each reconciliation.
type ClusterManagerRecoveryStatus struct {
	// Phase is the current stage of the recovery state machine.
	// +optional
	// +kubebuilder:validation:Enum=Healthy;WaitingForGracePeriod;Blocked;Requested;ReplacementPending;ReadyAfterRecovery;Failed
	Phase RecoveryPhase `json:"phase,omitempty"`

	// NodeName is the name of the node observed as unhealthy.
	// +optional
	NodeName string `json:"nodeName,omitempty"`

	// PodUID is the UID of the ClusterManager pod when the failure was detected.
	// Used to confirm a replacement pod is a genuinely new instance.
	// +optional
	PodUID string `json:"podUID,omitempty"`

	// Message is a human-readable description of the current recovery state.
	// +optional
	Message string `json:"message,omitempty"`

	// TransitionTime is when the current phase was entered.
	// +optional
	TransitionTime *metav1.Time `json:"transitionTime,omitempty"`
}

func init() {
	SchemeBuilder.Register(&ClusterManager{}, &ClusterManagerList{})
}
