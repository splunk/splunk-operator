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

// DatabaseSpec defines the desired state of Database.
type DatabaseSpec struct {
	// Class references a DatabaseClass (cluster-scoped) by name.
	// This field is IMMUTABLE after creation.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Class string `json:"class"`

	// Storage overrides the storage size from DatabaseClass.
	// Example: "500Gi"
	// +optional
	Storage *resource.Quantity `json:"storage,omitempty"`

	// Instances overrides the number of PostgreSQL instances from DatabaseClass.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	Instances *int32 `json:"instances,omitempty"`

	// PostgresVersion overrides the PostgreSQL version from DatabaseClass.
	// Example: "16"
	// +optional
	PostgresVersion *string `json:"postgresVersion,omitempty"`

	// Resources overrides CPU/memory resources from DatabaseClass.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// PostgreSQL overrides PostgreSQL engine parameters from DatabaseClass.
	// Maps to postgresql.conf settings.
	// +optional
	PostgreSQL map[string]string `json:"postgresql,omitempty"`

	// Extensions overrides PostgreSQL extensions from DatabaseClass.
	// +optional
	Extensions []string `json:"extensions,omitempty"`

	// Databases is a list of logical database names to create within the cluster.
	// Each gets its own user and password.
	// If empty, one default database is created.
	// Example: ["shc_kvstore", "shc_dmx", "shc_spl2"]
	// +optional
	Databases []string `json:"databases,omitempty"`

	// ClusterDeletionPolicy controls what happens when Database CR is deleted.
	// "Delete" (default) - PostgreSQL cluster is deleted
	// "Retain" - PostgreSQL cluster is preserved (orphaned)
	// +optional
	// +kubebuilder:validation:Enum=Delete;Retain
	// +kubebuilder:default=Delete
	ClusterDeletionPolicy string `json:"clusterDeletionPolicy,omitempty"`

	// DatabaseDeletionPolicy controls what happens when a database is removed from Databases list.
	// "Retain" (default) - Database not dropped, credentials remain
	// "Delete" - Database dropped, credentials removed
	// +optional
	// +kubebuilder:validation:Enum=Delete;Retain
	// +kubebuilder:default=Retain
	DatabaseDeletionPolicy string `json:"databaseDeletionPolicy,omitempty"`
}

// DatabaseStatus defines the observed state of Database.
type DatabaseStatus struct {
	// Phase represents the current phase of the Database.
	// Valid phases: "Pending", "Provisioning", "Ready", "Failed"
	// +optional
	Phase string `json:"phase,omitempty"`

	// Conditions represent the latest available observations of the Database state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Resources contains references to generated ConfigMap and Secret.
	// +optional
	Resources *DatabaseResources `json:"resources,omitempty"`

	// ProvisionerRef references the underlying provisioner resource (e.g., CNPG Cluster).
	// +optional
	ProvisionerRef *corev1.ObjectReference `json:"provisionerRef,omitempty"`
}

// DatabaseResources contains references to connection resources.
type DatabaseResources struct {
	// ConfigMapRef references the ConfigMap with connection endpoints.
	// Contains: DB_SERVICE_RW, DB_SERVICE_RO, DB_PORT, database list, usernames
	// +optional
	ConfigMapRef *corev1.LocalObjectReference `json:"configMapRef,omitempty"`

	// SecretRef references the Secret with database credentials.
	// Contains: passwords for each database user
	// +optional
	SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Class",type=string,JSONPath=`.spec.class`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Database is the Schema for the databases API.
type Database struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DatabaseSpec   `json:"spec,omitempty"`
	Status DatabaseStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DatabaseList contains a list of Database.
type DatabaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Database `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Database{}, &DatabaseList{})
}
