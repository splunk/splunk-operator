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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// default all fields to being optional
// +kubebuilder:validation:Optional

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
// see also https://book.kubebuilder.io/reference/markers/crd.html

// StandaloneSpec defines the desired state of a Splunk Enterprise standalone instances.
type StandaloneSpec struct {
	CommonSplunkSpec `json:",inline"`

	// Number of standalone pods
	Replicas int `json:"replicas"`

	// If this is true, DFS will be installed and enabled on all instances and a spark cluster will be created.
	EnableDFS bool `json:"enableDFS"`

	// Image to use for Spark pod containers (overrides SPARK_IMAGE environment variables)
	SparkImage string `json:"sparkImage"`
}

// StandaloneStatus defines the observed state of a Splunk Enterprise standalone instances.
type StandaloneStatus struct {
	// current phase of the standalone instances
	// +kubebuilder:validation:Enum=pending;ready;scaleup;scaledown;updating
	Phase string `json:"phase"`

	// current number of standalone instances
	Instances int `json:"instances"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Standalone is the Schema for a Splunk Enterprise standalone instances.
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=standalones,scope=Namespaced
// +kubebuilder:printcolumn:name="Phase",type="integer",JSONPath=".spec.status.phase",description="Status of standalone instances"
// +kubebuilder:printcolumn:name="Instances",type="string",JSONPath=".spec.status.instances",description="Number of standalone instances"
type Standalone struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StandaloneSpec   `json:"spec,omitempty"`
	Status StandaloneStatus `json:"status,omitempty"`
}

// GetIdentifier is a convenience function to return unique identifier for the Splunk enterprise deployment
func (cr *Standalone) GetIdentifier() string {
	return cr.ObjectMeta.Name
}

// GetNamespace is a convenience function to return namespace for a Splunk enterprise deployment
func (cr *Standalone) GetNamespace() string {
	return cr.ObjectMeta.Namespace
}

// GetTypeMeta is a convenience function to return a TypeMeta object
func (cr *Standalone) GetTypeMeta() metav1.TypeMeta {
	return cr.TypeMeta
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// StandaloneList contains a list of Standalone
type StandaloneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Standalone `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Standalone{}, &StandaloneList{})
}
