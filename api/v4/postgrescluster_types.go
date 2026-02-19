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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PostgresClusterSpec defines the desired state of PostgresCluster.
// Validation rules ensure immutability of Class, and that Storage and PostgresVersion can only be set once and cannot be removed or downgraded.
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.postgresVersion) || semver(self.postgresVersion, true).compareTo(semver(oldSelf.postgresVersion, true)) >= 0",message="Postgres version cannot be downgraded"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.storage) || (has(self.storage) && quantity(self.storage).compareTo(quantity(oldSelf.storage)) >= 0)",message="Storage size cannot be removed and can only be increased"
type PostgresClusterSpec struct {
	// This field is IMMUTABLE after creation.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="class is immutable"
	Class string `json:"class"`

	// Storage overrides the storage size from ClusterClass.
	// Example: "5Gi"
	// +optional
	Storage *resource.Quantity `json:"storage,omitempty"`

	// Instances overrides the number of PostgreSQL instances from ClusterClass.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	Instances *int32 `json:"instances,omitempty"`

	// PostgresVersion is the PostgreSQL version (major or major.minor).
	// Examples: "18" (latest 18.x), "18.1" (specific minor), "17", "16"
	// +kubebuilder:validation:Pattern=`^[0-9]+(\.[0-9]+)?$`
	// +kubebuilder:default="18"
	// +optional
	PostgresVersion *string `json:"postgresVersion,omitempty"`

	// Resources overrides CPU/memory resources from ClusterClass.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// PostgreSQL overrides PostgreSQL engine parameters from ClusterClass.
	// Maps to postgresql.conf settings.
	// Default empty map prevents panic.
	// Example: {"shared_buffers": "128MB", "log_min_duration_statement": "500ms"}
	// +optional
	// +kubebuilder:default={}
	PostgreSQLConfig map[string]string `json:"postgresqlConfig,omitempty"`

	// PgHBA contains pg_hba.conf host-based authentication rules.
	// Defines client authentication and connection security (cluster-wide).
	// Maps to pg_hba.conf settings.
	// Default empty array prevents panic.
	// Example: ["hostssl all all 0.0.0.0/0 scram-sha-256"]
	// +optional
	// +kubebuilder:default={}
	PgHBA []string `json:"pgHBA,omitempty"`

	// ConnectionPoolerEnabled controls whether PgBouncer connection pooling is deployed for this cluster.
	// When set, takes precedence over the class-level connectionPoolerEnabled value.
	// +kubebuilder:default=false
	// +optional
	ConnectionPoolerEnabled *bool `json:"connectionPoolerEnabled,omitempty"`
}

// PostgresClusterStatus defines the observed state of PostgresCluster.
type PostgresClusterStatus struct {
	// Phase represents the current phase of the PostgresCluster.
	// Values: "Pending", "Provisioning", "Failed", "Ready", "Deleting"
	// +optional
	Phase string `json:"phase,omitempty"`

	// Conditions represent the latest available observations of the PostgresCluster's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ProvisionerRef contains reference to the provisioner resource managing this PostgresCluster.
	// Right now, only CNPG is supported.
	// +optional
	ProvisionerRef *corev1.ObjectReference `json:"provisionerRef,omitempty"`

	// ConnectionPoolerStatus contains the observed state of the connection pooler.
	// Only populated when connection pooler is enabled in the PostgresClusterClass.
	// +optional
	ConnectionPoolerStatus *ConnectionPoolerStatus `json:"connectionPoolerStatus,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Class",type=string,JSONPath=`.spec.class`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PostgresCluster is the Schema for the postgresclusters API.
type PostgresCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PostgresClusterSpec   `json:"spec,omitempty"`
	Status PostgresClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PostgresClusterList contains a list of PostgresCluster.
type PostgresClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PostgresCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PostgresCluster{}, &PostgresClusterList{})
}
