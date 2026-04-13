/*
Copyright 2021.

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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type AppSourceS3Spec struct {
	// +required
	Endpoint string `json:"endpoint"`

	// +optional
	Region string `json:"region,omitempty"`

	// +optional
	Bucket string `json:"bucket,omitempty"`

	// +optional
	Path string `json:"path,omitempty"`
}

type AppSourceGitSpec struct {
	// +required
	Repo string `json:"repo"`

	// +optional
	// +kubebuilder:default="main"
	Ref string `json:"ref,omitempty"`
}

type AppSourceAuth struct {
	// +required
	SecretRef corev1.LocalObjectReference `json:"secretRef"`
}

// +kubebuilder:validation:XValidation:rule="self.type != 's3' || has(self.s3)",message="s3 configuration is required when type is s3"
// +kubebuilder:validation:XValidation:rule="self.type != 'git' || has(self.git)",message="git configuration is required when type is git"
// +kubebuilder:validation:XValidation:rule="[has(self.s3), has(self.git)].filter(x, x == true).size() == 1",message="exactly one of s3 or git must be specified"
// AppSourceSpec defines the desired state of AppSource.
type AppSourceSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Type of the App Source
	// Valid values are "git", "s3", "gcp", "azure"
	// +kubebuilder:validation:Enum="git";"s3";"gcp";"azure"
	// +required
	Type string `json:"type"`

	// S3 specific configuration
	// +optional
	S3 *AppSourceS3Spec `json:"s3,omitempty"`

	// Git specific configuration
	// +optional
	Git *AppSourceGitSpec `json:"git,omitempty"`

	// GCP and Azure specific configuration
	// TODO: Add GCP and Azure specific configuration

	// TODO: Add SplunkBase support

	// Authentication configuration
	// +required
	Auth AppSourceAuth `json:"auth"`

	// PollIntervalSeconds is the interval in seconds to poll remote repository
	// +kubebuilder:default=60
	// +optional
	PollIntervalSeconds *int32 `json:"pollIntervalSeconds,omitempty"`
}

// AppSourceStatus defines the observed state of AppSource.
type AppSourceStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Conditions represent the current state of the AppSource
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"condition,omitempty"`

	// ObservedGeneration represents the most recent generation observed for this AppSource
	// This will be used to determine if the AppSource needs to be reconciled
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// LastPolledTime represents the last time the AppSource was polled
	// +optional
	LastPolledTime *metav1.Time `json:"lastPolledTime,omitempty"`
}

const (
	TypeAppSourceConditionPending = "Pending"
	TypeAppSourceConditionSyncing = "Syncing"
	TypeAppSourceConditionReady   = "Ready"
	TypeAppSourceConditionFailed  = "Failed"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// AppSource is the Schema for the appsources API.
type AppSource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AppSourceSpec   `json:"spec,omitempty"`
	Status AppSourceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AppSourceList contains a list of AppSource.
type AppSourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AppSource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AppSource{}, &AppSourceList{})
}
