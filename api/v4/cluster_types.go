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

package v4

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterSpec defines the desired state of Cluster.
type ClusterSpec struct {
	// Reference to ClusterClass for default configuration by name.
	// This field is IMMUTABLE after creation.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="class is immutable"
	Class string `json:"class"`

	// Storage overrides the storage size from ClusterClass.
	// Example: "5Gi"
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == null || oldSelf == null || quantity(self).compareTo(quantity(oldSelf)) >= 0",message="storage size can only be increased"
	Storage *resource.Quantity `json:"storage,omitempty"`

	// Instances overrides the number of PostgreSQL instances from ClusterClass.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	Instances *int32 `json:"instances,omitempty"`

	// PostgresVersion overrides the PostgreSQL version from ClusterClass.
	// Example: "16"
	// +optional
	PostgresVersion *string `json:"postgresVersion,omitempty"`

	// Resources overrides CPU/memory resources from ClusterClass.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// PostgreSQL overrides PostgreSQL engine parameters from ClusterClass.
	// Maps to postgresql.conf settings.
	// +optional
	PostgreSQLConfig map[string]string `json:"postgresqlConfig,omitempty"`

	// PgHBA contains pg_hba.conf host-based authentication rules.
	// Defines client authentication and connection security (cluster-wide).
	// Example: ["hostssl all all 0.0.0.0/0 scram-sha-256"]
	// +optional
	PgHBA []string `json:"pgHBA,omitempty"`
}

// ClusterStatus defines the observed state of Cluster.
type ClusterStatus struct {
	// Phase represents the current phase of the Cluster.
	// Values: "Pending", "Provisioning", "Failed", "Ready", "Deleting"
	// +optional
	Phase string `json:"phase,omitempty"`

	// Conditions represent the latest available observations of the Cluster's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ProvisionerRef contains reference to the provisioner resource managing this cluster.
	// Right now, only CNPG is supported.
	// +optional
	ProvisionerRef *corev1.ObjectReference `json:"provisionerRef,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Class",type=string,JSONPath=`.spec.class`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Cluster is the Schema for the clusters API.
type Cluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterSpec   `json:"spec,omitempty"`
	Status ClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterList contains a list of Cluster.
type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Cluster{}, &ClusterList{})
}
