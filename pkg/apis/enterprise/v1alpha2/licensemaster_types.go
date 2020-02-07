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

// LicenseMasterSpec defines the desired state of a Splunk Enterprise license master.
type LicenseMasterSpec struct {
	CommonSplunkSpec `json:",inline"`
}

// LicenseMasterStatus defines the observed state of a Splunk Enterprise license master.
type LicenseMasterStatus struct {
	// current phase of the license master
	// +kubebuilder:validation:Enum=pending;ready;updating
	Phase string `json:"phase"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// LicenseMaster is the Schema for a Splunk Enterprise license master.
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=licensemasters,scope=Namespaced
// +kubebuilder:printcolumn:name="Phase",type="integer",JSONPath=".spec.status.phase",description="Status of license master"
type LicenseMaster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LicenseMasterSpec   `json:"spec,omitempty"`
	Status LicenseMasterStatus `json:"status,omitempty"`
}

// GetIdentifier is a convenience function to return unique identifier for the Splunk enterprise deployment
func (cr *LicenseMaster) GetIdentifier() string {
	return cr.ObjectMeta.Name
}

// GetNamespace is a convenience function to return namespace for a Splunk enterprise deployment
func (cr *LicenseMaster) GetNamespace() string {
	return cr.ObjectMeta.Namespace
}

// GetTypeMeta is a convenience function to return a TypeMeta object
func (cr *LicenseMaster) GetTypeMeta() metav1.TypeMeta {
	return cr.TypeMeta
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// LicenseMasterList contains a list of LicenseMaster
type LicenseMasterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LicenseMaster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LicenseMaster{}, &LicenseMasterList{})
}
