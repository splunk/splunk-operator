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

// IndexerClusterSpec defines the desired state of a Splunk Enterprise indexer cluster
type IndexerClusterSpec struct {
	CommonSplunkSpec `json:",inline"`

	// Number of search head pods; a search head cluster will be created if > 1
	Replicas int32 `json:"replicas"`
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

// IndexerClusterStatus defines the observed state of a Splunk Enterprise indexer cluster
type IndexerClusterStatus struct {
	// current phase of the indexer cluster
	Phase Phase `json:"phase"`

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

	// Information tracked by the provisioner.
	Provisioning ProvisionStatus `json:"provisioning"`
	// OperationHistory holds information about operations performed
	// on this host.
	OperationHistory OperationHistory `json:"operationHistory,omitempty"`
	// OperationalStatus holds the status of the splunk operations in upgrade
	// +kubebuilder:validation:Enum="";OK;discovered;error;delayed;detached
	OperationalStatus OperationalStatus `json:"operationalStatus"`
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

// SetOperationalStatus updates the OperationalStatus field and returns
// true when a change is made or false when no change is made.
func (icstr *IndexerCluster) SetOperationalStatus(status OperationalStatus) bool {
	if icstr.Status.OperationalStatus != status {
		icstr.Status.OperationalStatus = status
		return true
	}
	return false
}

// OperationMetricForState returns a pointer to the metric for the given
// provisioning state.
func (icstr *IndexerCluster) OperationMetricForState(operation ProvisioningState) (metric *OperationMetric) {
	history := &icstr.Status.OperationHistory
	switch operation {

	case StateIndexerClusterPrepare:
		metric = &history.Prepare

	case StateIndexerClusterRestore:
		metric = &history.Restore

	case StateIndexerClusterBackup:
		metric = &history.Backup

	case StateIndexerClusterUpgrade:
		metric = &history.Upgrade

	case StateIndexerClusterVerification:
		metric = &history.Verification

	case StateIndexerClusterReady:
		metric = &history.Ready

	case StateIndexerClusterError:
		metric = &history.Error
	}
	return
}

// NewEvent creates a new event associated with the object and ready
// to be published to the kubernetes API.
func (icstr *IndexerCluster) NewEvent(eventType, reason, message string) corev1.Event {
	t := metav1.Now()
	return corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: reason + "-",
			Namespace:    icstr.ObjectMeta.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:       "IndexerCluster",
			Namespace:  icstr.Namespace,
			Name:       icstr.Name,
			UID:        icstr.UID,
			APIVersion: GroupVersion.String(),
		},
		Reason:  reason,
		Message: message,
		Source: corev1.EventSource{
			Component: "splunk-indexercluster-controller",
		},
		FirstTimestamp:      t,
		LastTimestamp:       t,
		Count:               1,
		Type:                eventType,
		ReportingController: "enterprise.splunk.com/indexercluster-controller",
	}
}
