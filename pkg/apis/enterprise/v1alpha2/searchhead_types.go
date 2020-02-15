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
	Replicas int `json:"replicas"`

	// SparkRef refers to a Spark cluster managed by the operator within Kubernetes
	// When defined, Data Fabric Search (DFS) will be enabled and configured to use the Spark cluster.
	SparkRef corev1.ObjectReference `json:"sparkRef"`

	// Image to use for Spark pod containers (overrides SPARK_IMAGE environment variables)
	SparkImage string `json:"sparkImage"`
}

// SearchHeadStatus defines the observed state of a Splunk Enterprise standalone search head or cluster of search heads
type SearchHeadStatus struct {
	// current phase of the search head cluster
	// +kubebuilder:validation:Enum=pending;ready;scaleup;scaledown;updating
	Phase string `json:"phase"`

	// current number of search head instances
	Instances int `json:"instances"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SearchHead is the Schema for a Splunk Enterprise standalone search head or cluster of search heads
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=searchheads,scope=Namespaced,shortName=search;sh
// +kubebuilder:printcolumn:name="Phase",type="integer",JSONPath=".spec.status.phase",description="Status of search head cluster"
// +kubebuilder:printcolumn:name="Instances",type="string",JSONPath=".spec.status.instances",description="Number of search heads"
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
