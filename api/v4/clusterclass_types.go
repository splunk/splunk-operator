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

// ClusterClassSpec defines the desired state of ClusterClass.
// ClusterClass is immutable after creation - it serves as a template for Cluster CRs.
type ClusterClassSpec struct {
	// Provisioner identifies which database provisioner to use.
	// Currently supported: "postgresql.cnpg.io" (CloudNativePG)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=postgresql.cnpg.io
	Provisioner string `json:"provisioner"`

	// ClusterConfig contains cluster-level configuration.
	// These settings apply to database cluster infrastructure.
	// Can be overridden in Cluster CR.
	// +kubebuilder:default={}
	// +optional
	ClusterConfig ClusterConfig `json:"clusterConfig,omitempty"`

	// CNPG contains CloudNativePG-specific configuration and policies.
	// Only used when Provisioner is "postgresql.cnpg.io"
	// These settings CANNOT be overridden in Cluster CR (platform policy).
	// +optional
	CNPG *CNPGConfig `json:"cnpg,omitempty"`
}

// ClusterConfig contains provider-agnostic cluster configuration.
// These fields define database cluster infrastructure and can be overridden in Cluster CR.
type ClusterConfig struct {
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

// ClusterClassStatus defines the observed state of ClusterClass.
type ClusterClassStatus struct {
	// Conditions represent the latest available observations of the ClusterClass state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase represents the current phase of the ClusterClass.
	// Valid phases: "Ready", "Invalid"
	// +optional
	Phase string `json:"phase,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Provisioner",type=string,JSONPath=`.spec.provisioner`
// +kubebuilder:printcolumn:name="Instances",type=integer,JSONPath=`.spec.clusterConfig.instances`
// +kubebuilder:printcolumn:name="Storage",type=string,JSONPath=`.spec.clusterConfig.storage`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.clusterConfig.postgresVersion`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ClusterClass is the Schema for the clusterclasses API.
// ClusterClass defines a reusable template and policy for database cluster provisioning.
type ClusterClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterClassSpec   `json:"spec,omitempty"`
	Status ClusterClassStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterClassList contains a list of ClusterClass.
type ClusterClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterClass `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterClass{}, &ClusterClassList{})
}
