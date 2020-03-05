// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
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

package v1alpha2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// default all fields to being optional
// +kubebuilder:validation:Optional

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
// see also https://book.kubebuilder.io/reference/markers/crd.html

// SearchHeadSpec defines the desired state of a Splunk Enterprise standalone search head or cluster of search heads
type SearchHeadSpec struct {
	CommonSplunkSpec `json:",inline"`

	// Number of search head pods; a search head cluster will be created if > 1
	Replicas int32 `json:"replicas"`

	// SparkRef refers to a Spark cluster managed by the operator within Kubernetes
	// When defined, Data Fabric Search (DFS) will be enabled and configured to use the Spark cluster.
	SparkRef corev1.ObjectReference `json:"sparkRef"`

	// Image to use for Spark pod containers (overrides SPARK_IMAGE environment variables)
	SparkImage string `json:"sparkImage"`
}

// SearchHeadMemberStatus is used to track the status of each search head cluster member
type SearchHeadMemberStatus struct {
	// Name of the search head cluster member
	Name string `json:"name"`

	// Status of the search head cluster member
	Status string `json:"status"`

	// true if this member is registered with the search head captain
	Registered bool `json:"registered"`

	// total number of active historical + realtime searches
	ActiveSearches int `json:"activeSearches"`
}

// SearchHeadStatus defines the observed state of a Splunk Enterprise standalone search head or cluster of search heads
type SearchHeadStatus struct {
	// current phase of the search head cluster
	Phase ResourcePhase `json:"phase"`

	// current phase of the deployer
	DeployerPhase ResourcePhase `json:"deployerPhase"`

	// number of desired search head replicas
	Replicas int32 `json:"replicas"`

	// current number of ready search head replicas
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

	// status of each search head cluster member
	Members []SearchHeadMemberStatus `json:"members"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SearchHead is the Schema for a Splunk Enterprise standalone search head or cluster of search heads
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:path=searchheads,scope=Namespaced,shortName=search;sh
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of search head cluster"
// +kubebuilder:printcolumn:name="Deployer",type="string",JSONPath=".status.deployerPhase",description="Status of the deployer"
// +kubebuilder:printcolumn:name="Desired",type="integer",JSONPath=".status.replicas",description="Number of desired search head replicas"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas",description="Current number of ready search head replicas"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of search head cluster"
type SearchHead struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SearchHeadSpec   `json:"spec,omitempty"`
	Status SearchHeadStatus `json:"status,omitempty"`
}

// GetIdentifier is a convenience function to return unique identifier for the Splunk enterprise deployment
func (cr *SearchHead) GetIdentifier() string {
	return cr.ObjectMeta.Name
}

// GetNamespace is a convenience function to return namespace for a Splunk enterprise deployment
func (cr *SearchHead) GetNamespace() string {
	return cr.ObjectMeta.Namespace
}

// GetTypeMeta is a convenience function to return a TypeMeta object
func (cr *SearchHead) GetTypeMeta() metav1.TypeMeta {
	return cr.TypeMeta
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SearchHeadList contains a list of SearcHead
type SearchHeadList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SearchHead `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SearchHead{}, &SearchHeadList{})
}
