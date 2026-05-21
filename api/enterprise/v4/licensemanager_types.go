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
	// LicenseManagerPausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	LicenseManagerPausedAnnotation = "licensemanager.enterprise.splunk.com/paused"
)

// LicenseManagerSpec defines the desired state of a Splunk Enterprise license manager.
type LicenseManagerSpec struct {
	CommonSplunkSpec `json:",inline"`

	// Splunk enterprise App repository. Specifies remote App location and scope for Splunk App management
	AppFrameworkConfig AppFrameworkSpec `json:"appRepo,omitempty"`
}

// LicenseManagerStatus defines the observed state of a Splunk Enterprise license manager.
type LicenseManagerStatus struct {
	// current phase of the license manager
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

	// App Framework Context
	AppContext AppDeploymentContext `json:"appContext"`

	// Telemetry App installation flag
	TelAppInstalled bool `json:"telAppInstalled"`

	// Auxiliary message describing CR status
	Message string `json:"message"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// LicenseManager is the Schema for a Splunk Enterprise license manager.
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=licensemanagers,scope=Namespaced,shortName=lmanager
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of license manager"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of license manager"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message",description="Auxiliary message describing CR status"
// +kubebuilder:storageversion
type LicenseManager struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LicenseManagerSpec   `json:"spec,omitempty"`
	Status LicenseManagerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// LicenseManagerList contains a list of LicenseManager
type LicenseManagerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LicenseManager `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LicenseManager{}, &LicenseManagerList{})
}
