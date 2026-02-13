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

// PostgresClusterClassSpec defines the desired state of PostgresClusterClass.
// PostgresClusterClass is immutable after creation - it serves as a template for Cluster CRs.
type PostgresClusterClassSpec struct {
	// Provisioner identifies which database provisioner to use.
	// Currently supported: "postgresql.cnpg.io" (CloudNativePG)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=postgresql.cnpg.io
	Provisioner string `json:"provisioner"`

	// PostgresClusterConfig contains cluster-level configuration.
	// These settings apply to PostgresCluster infrastructure.
	// Can be overridden in PostgresCluster CR.
	// +kubebuilder:default={}
	// +optional
	Config PosgresClusterClassConfig `json:"config,omitempty"`

	// ConnectionPooler contains PgBouncer connection pooler configuration.
	// When enabled, creates RW and RO pooler deployments for clusters using this class.
	// +optional
	ConnectionPooler *ConnectionPoolerConfig `json:"connectionPooler,omitempty"`

	// CNPG contains CloudNativePG-specific configuration and policies.
	// Only used when Provisioner is "postgresql.cnpg.io"
	// These settings CANNOT be overridden in PostgresCluster CR (platform policy).
	// +optional
	CNPG *CNPGConfig `json:"cnpg,omitempty"`
}

// PosgresClusterClassConfig contains provider-agnostic cluster configuration.
// These fields define PostgresCluster infrastructure and can be overridden in PostgresCluster CR.
type PosgresClusterClassConfig struct {
	// Instances is the number of database instances (1 primary + N replicas).
	// Single instance (1) is suitable for development.
	// High availability requires at least 3 instances (1 primary + 2 replicas).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:default=1
	// +optional
	Instances *int32 `json:"instances,omitempty"`

	// Storage is the size of persistent volume for each instance.
	// Cannot be decreased after cluster creation (PostgreSQL limitation).
	// Recommended minimum: 10Gi for production viability.
	// Example: "50Gi", "100Gi", "1Ti"
	// +kubebuilder:default="50Gi"
	// +optional
	Storage *resource.Quantity `json:"storage,omitempty"`

	// PostgresVersion is the PostgreSQL version (major or major.minor).
	// Examples: "18" (latest 18.x), "18.1" (specific minor), "17", "16"
	// +kubebuilder:validation:Pattern=`^[0-9]+(\.[0-9]+)?$`
	// +kubebuilder:default="18"
	// +optional
	PostgresVersion *string `json:"postgresVersion,omitempty"`

	// Resources defines CPU and memory requests/limits per instance.
	// All instances in the cluster have the same resources.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// PostgreSQLConfig contains PostgreSQL engine configuration parameters.
	// Maps to postgresql.conf settings (cluster-wide).
	// Example: {"max_connections": "200", "shared_buffers": "2GB"}
	// +optional
	PostgreSQLConfig map[string]string `json:"postgresqlConfig,omitempty"`

	// PgHBA contains pg_hba.conf host-based authentication rules.
	// Defines client authentication and connection security (cluster-wide).
	// Example: ["hostssl all all 0.0.0.0/0 scram-sha-256"]
	// +optional
	PgHBA []string `json:"pgHBA,omitempty"`
}

// CNPGConfig contains CloudNativePG-specific configuration.
// These fields control CNPG operator behavior and enforce platform policies.
// Cannot be overridden in Cluster CR.
type CNPGConfig struct {
	// PrimaryUpdateMethod determines how the primary instance is updated.
	// "restart" - tolerate brief downtime (suitable for development)
	// "switchover" - minimal downtime via automated failover (production-grade)
	//
	// NOTE: When using "switchover", ensure clusterConfig.instances > 1.
	// Switchover requires at least one replica to fail over to.
	// +kubebuilder:validation:Enum=restart;switchover
	// +kubebuilder:default=switchover
	// +optional
	PrimaryUpdateMethod string `json:"primaryUpdateMethod,omitempty"`
}

// PostgresClusterClassStatus defines the observed state of PostgresClusterClass.
type PostgresClusterClassStatus struct {
	// Conditions represent the latest available observations of the PostgresClusterClass state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase represents the current phase of the PostgresClusterClass.
	// Valid phases: "Ready", "Invalid"
	// +optional
	Phase string `json:"phase,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Provisioner",type=string,JSONPath=`.spec.provisioner`
// +kubebuilder:printcolumn:name="Instances",type=integer,JSONPath=`.spec.postgresClusterConfig.instances`
// +kubebuilder:printcolumn:name="Storage",type=string,JSONPath=`.spec.postgresClusterConfig.storage`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.postgresClusterConfig.postgresVersion`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PostgresClusterClass is the Schema for the postgresclusterclasses API.
// PostgresClusterClass defines a reusable template and policy for postgres cluster provisioning.
type PostgresClusterClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PostgresClusterClassSpec   `json:"spec,omitempty"`
	Status PostgresClusterClassStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PostgresClusterClassList contains a list of PostgresClusterClass.
type PostgresClusterClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PostgresClusterClass `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PostgresClusterClass{}, &PostgresClusterClassList{})
}
