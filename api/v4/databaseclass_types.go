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

// DatabaseClassSpec defines the desired state of DatabaseClass.
// DatabaseClass is immutable after creation - it's a template/policy for database provisioning.
type DatabaseClassSpec struct {
	// Provisioner identifies which database provisioner to use.
	// Currently supported: "postgresql.cnpg.io" (CloudNativePG)
	// Future: "aws.rds.io", "gcp.cloudsql.io"
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=postgresql.cnpg.io
	Provisioner string `json:"provisioner"`

	// Config contains the generic database configuration (provider-agnostic).
	// These settings work across all provisioners.
	// +kubebuilder:default={}	
	// +optional
	Config DatabaseConfig `json:"config,omitempty"`

	// CNPG contains CloudNativePG-specific configuration.
	// Only used when Provisioner is "postgresql.cnpg.io"
	// +optional
	CNPG *CNPGConfig `json:"cnpg,omitempty"`
}

// DatabaseConfig contains provider-agnostic database configuration.
// These fields can be overridden in Database CR.
type DatabaseConfig struct {
	// Instances is the number of PostgreSQL instances (1 primary + N replicas).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:default=3
	// +optional
	Instances *int32 `json:"instances,omitempty"`

	// Storage is the size of persistent volume for each instance.
	// Example: "100Gi", "1Ti"
	// +kubebuilder:default="10Gi"	
	// +optional
	Storage *resource.Quantity `json:"storage,omitempty"`

	// PostgresVersion is the PostgreSQL major version.
	// Example: "17", "18"
	// +kubebuilder:default="18"
	// +optional
	PostgresVersion *string `json:"postgresVersion,omitempty"`

	// Resources defines CPU and memory requests/limits per instance.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// PostgreSQL contains PostgreSQL engine configuration parameters.
	// Maps to postgresql.conf settings.
	// Example: {"max_connections": "200", "shared_buffers": "2GB"}
	// +optional
  	// +kubebuilder:default={}
	PostgreSQL map[string]string `json:"postgresql,omitempty"`

	// Extensions is a list of PostgreSQL extensions to enable.
	// Default: []
	// Example: ["pg_stat_statements", "pgcrypto"]
	// +kubebuilder:default={}
	// +optional
	Extensions []string `json:"extensions,omitempty"`
	// Databases is a list of PostgreSQL databases to create within cluster.
	// Default: []
	// +kubebuilder:default={}
	// +optional
	Databases []string `json:"databases,omitempty"`
}

// CNPGConfig contains CloudNativePG-specific configuration.
// These fields CANNOT be overridden in Database CR (infrastructure policy).
type CNPGConfig struct {
	// PrimaryUpdateMethod determines how the primary instance is updated.
	// "restart" - tolerate brief downtime (good for dev)
	// "switchover" - minimal downtime via failover (good for prod)
	// +optional
	// +kubebuilder:validation:Enum=restart;switchover
	PrimaryUpdateMethod string `json:"primaryUpdateMethod,omitempty"`

	// TODO: Add more CNPG-specific fields as needed
	// Examples: backup config, monitoring, connection pooler
}

// DatabaseClassStatus defines the observed state of DatabaseClass.
type DatabaseClassStatus struct {
	// Conditions represent the latest available observations of the DatabaseClass state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase represents the current phase of the DatabaseClass.
	// Valid phases: "Ready", "Invalid"
	// +optional
	Phase string `json:"phase,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Provisioner",type=string,JSONPath=`.spec.provisioner`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// DatabaseClass is the Schema for the databaseclasses API.
type DatabaseClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DatabaseClassSpec   `json:"spec,omitempty"`
	Status DatabaseClassStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DatabaseClassList contains a list of DatabaseClass.
type DatabaseClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DatabaseClass `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DatabaseClass{}, &DatabaseClassList{})
}
