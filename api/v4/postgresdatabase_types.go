/*
Copyright 2026.

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

// PostgresDatabaseSpec defines the desired state of PostgresDatabase.
// +kubebuilder:validation:XValidation:rule="self.clusterRef == oldSelf.clusterRef",message="clusterRef is immutable"
type PostgresDatabaseSpec struct {
	// +kubebuilder:validation:Required
	ClusterRef corev1.LocalObjectReference `json:"clusterRef"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=10
	// +kubebuilder:validation:XValidation:rule="self.all(x, self.filter(y, y.name == x.name).size() == 1)",message="database names must be unique"
	Databases []DatabaseDefinition `json:"databases"`
}

type DatabaseDefinition struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=30
	Name       string   `json:"name"`
	Extensions []string `json:"extensions,omitempty"`
	// +kubebuilder:validation:Enum=Delete;Retain
	// +kubebuilder:default=Delete
	DeletionPolicy string `json:"deletionPolicy,omitempty"`
}

type DatabaseInfo struct {
	Name               string                       `json:"name"`
	Ready              bool                         `json:"ready"`
	DatabaseRef        *corev1.LocalObjectReference `json:"databaseRef,omitempty"`
	AdminUserSecretRef *corev1.LocalObjectReference `json:"adminUserSecretRef,omitempty"`
	RWSecretRef        *corev1.LocalObjectReference `json:"rwSecretRef,omitempty"`
	ConfigMapRef       *corev1.LocalObjectReference `json:"configMap,omitempty"`
}

// PostgresDatabaseStatus defines the observed state of PostgresDatabase.
type PostgresDatabaseStatus struct {
	// +optional
	Phase string `json:"phase,omitempty"`
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +optional
	Databases []DatabaseInfo `json:"databases,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterRef.name`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PostgresDatabase is the Schema for the postgresdatabases API.
type PostgresDatabase struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PostgresDatabaseSpec   `json:"spec,omitempty"`
	Status PostgresDatabaseStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PostgresDatabaseList contains a list of PostgresDatabase.
type PostgresDatabaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PostgresDatabase `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PostgresDatabase{}, &PostgresDatabaseList{})
}
