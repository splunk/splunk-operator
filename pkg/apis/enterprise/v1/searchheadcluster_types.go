// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

// default all fields to being optional
// +kubebuilder:validation:Optional

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
// see also https://book.kubebuilder.io/reference/markers/crd.html

// SearchHeadClusterSpec defines the desired state of a Splunk Enterprise search head cluster
type SearchHeadClusterSpec struct {
	CommonSplunkSpec `json:",inline"`

	// Number of search head pods; a search head cluster will be created if > 1
	Replicas int32 `json:"replicas"`
}

// SearchHeadClusterMemberStatus is used to track the status of each search head cluster member
type SearchHeadClusterMemberStatus struct {
	// Name of the search head cluster member
	Name string `json:"name"`

	// Indicates the status of the member.
	Status string `json:"status"`

	// Flag that indicates if this member can run scheduled searches.
	Adhoc bool `json:"adhoc_searchhead"`

	// Indicates if this member is registered with the searchhead cluster captain.
	Registered bool `json:"is_registered"`

	// Number of currently running historical searches.
	ActiveHistoricalSearchCount int `json:"active_historical_search_count"`

	// Number of currently running realtime searches.
	ActiveRealtimeSearchCount int `json:"active_realtime_search_count"`
}

// SearchHeadClusterStatus defines the observed state of a Splunk Enterprise search head cluster
type SearchHeadClusterStatus struct {
	// current phase of the search head cluster
	Phase splcommon.Phase `json:"phase"`

	// current phase of the deployer
	DeployerPhase splcommon.Phase `json:"deployerPhase"`

	// desired number of search head cluster members
	Replicas int32 `json:"replicas"`

	// current number of ready search head cluster members
	ReadyReplicas int32 `json:"readyReplicas"`

	// selector for pods, used by HorizontalPodAutoscaler
	Selector string `json:"selector"`

	// name or label of the search head captain
	Captain string `json:"captain"`

	// true if the search head cluster's captain is ready to service requests
	CaptainReady bool `json:"captainReady"`

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
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SearchHeadCluster is the Schema for a Splunk Enterprise search head cluster
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:path=searchheadclusters,scope=Namespaced,shortName=shc
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of search head cluster"
// +kubebuilder:printcolumn:name="Deployer",type="string",JSONPath=".status.deployerPhase",description="Status of the deployer"
// +kubebuilder:printcolumn:name="Desired",type="integer",JSONPath=".status.replicas",description="Desired number of search head cluster members"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas",description="Current number of ready search head cluster members"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of search head cluster"
// +kubebuilder:storageversion
type SearchHeadCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SearchHeadClusterSpec   `json:"spec,omitempty"`
	Status SearchHeadClusterStatus `json:"status,omitempty"`
}

// blank assignment to verify that SearchHeadCluster implements splcommon.MetaObject
var _ splcommon.MetaObject = &SearchHeadCluster{}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SearchHeadClusterList contains a list of SearcHead
type SearchHeadClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SearchHeadCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SearchHeadCluster{}, &SearchHeadClusterList{})
}
