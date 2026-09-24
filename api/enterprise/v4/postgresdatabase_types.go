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

const PostgresDatabaseClusterRefNameField = "spec.clusterRef.name"

// PostgresDatabaseSpec defines the desired state of PostgresDatabase.
// +kubebuilder:validation:XValidation:rule="self.clusterRef == oldSelf.clusterRef",message="clusterRef is immutable"
// +kubebuilder:validation:XValidation:rule="self.clusterRef.name.size() > 0",message="clusterRef.name must not be empty"
type PostgresDatabaseSpec struct {
	// Reference to Postgres Cluster managed by postgresCluster controller
	// +kubebuilder:validation:Required
	ClusterRef corev1.LocalObjectReference `json:"clusterRef"`

	// Databases to provision on the target cluster.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=10
	// +listType=map
	// +listMapKey=name
	Databases []DatabaseDefinition `json:"databases"`
}

type DatabaseMonitoring struct {
	// Ordered database-scoped sources; selector optional fields are unsupported.
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=100
	// +kubebuilder:validation:XValidation:rule="self.all(x, has(x.name) && x.name.size() > 0)",message="name must not be empty"
	// +kubebuilder:validation:XValidation:rule="self.all(x, x.key.size() > 0)",message="key must not be empty"
	// +optional
	CustomQueriesConfigMap []corev1.ConfigMapKeySelector `json:"customQueriesConfigMap,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="(has(self.passwordConfig) == has(oldSelf.passwordConfig))",message="passwordConfig cannot be altered after creation"
// +kubebuilder:validation:XValidation:rule="!has(self.passwordConfig) || self.passwordConfig == oldSelf.passwordConfig",message="passwordConfig is immutable once set"
type DatabaseDefinition struct {
	// Name of the PostgreSQL database to create. It must start with a lowercase
	// letter and contain only lowercase letters and digits because it is also
	// used to derive Kubernetes resource names. Underscores and hyphens are not
	// allowed.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=30
	// +kubebuilder:validation:Pattern=`^[a-z][a-z0-9]*$`
	Name string `json:"name"`
	// PostgreSQL extensions to install in this database (e.g. "pg_trgm", "uuid-ossp").
	Extensions []string `json:"extensions,omitempty"`
	// DeletionPolicy controls what happens to the PostgreSQL database when this resource is deleted.
	// Delete removes the database; Retain leaves it in place.
	// +kubebuilder:validation:Enum=Delete;Retain
	// +kubebuilder:default=Delete
	DeletionPolicy string `json:"deletionPolicy,omitempty"`

	// External Admin and RW secret configuration,
	// if non empty, external secret management is mandatory.
	// +optional
	PasswordConfig *PasswordConfig `json:"passwordConfig,omitempty"`

	// +optional
	Monitoring *DatabaseMonitoring `json:"monitoring,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="self.externalAdminSecretRef.name.size() > 0",message="externalAdminSecretRef.name must not be empty"
// +kubebuilder:validation:XValidation:rule="self.externalRWSecretRef.name.size() > 0",message="externalRWSecretRef.name must not be empty"
// +kubebuilder:validation:XValidation:rule="self.externalAdminSecretRef.name != self.externalRWSecretRef.name",message="externalAdminSecretRef and externalRWSecretRef must reference different Secrets"
type PasswordConfig struct {
	// +kubebuilder:validation:Required
	ExternalAdminSecretRef corev1.LocalObjectReference `json:"externalAdminSecretRef"`
	// +kubebuilder:validation:Required
	ExternalRWSecretRef corev1.LocalObjectReference `json:"externalRWSecretRef"`
}

type DatabaseInfo struct {
	Name  string `json:"name"`
	Ready bool   `json:"ready"`
	// +optional
	Message            string                       `json:"message,omitempty"`
	DatabaseRef        *corev1.LocalObjectReference `json:"databaseRef,omitempty"`
	AdminUserSecretRef *corev1.SecretKeySelector    `json:"adminUserSecretRef,omitempty"`
	RWUserSecretRef    *corev1.SecretKeySelector    `json:"rwUserSecretRef,omitempty"`
	ConfigMapRef       *corev1.LocalObjectReference `json:"configMapRef,omitempty"`
	Roles              []DatabaseRoleInfo           `json:"roles,omitempty"`
}

// DatabaseCustomMetricsContribution is committed database-owned intent.
// Exists=false explicitly declares that the database does not participate.
type DatabaseCustomMetricsContribution struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	DatabaseName string `json:"databaseName"`

	// Digest used for cluster acknowledgement.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Revision string `json:"revision"`

	// False removes previously committed intent.
	// +kubebuilder:validation:Required
	Exists bool `json:"exists"`

	// +listType=atomic
	// +kubebuilder:validation:MaxItems=100
	// +optional
	CustomQueriesConfigMap []corev1.ConfigMapKeySelector `json:"customQueriesConfigMap,omitempty"`
}

// PostgresDatabaseCustomMetricsPublication is the current database-owned
// participation published for the PostgresCluster controller to consume.
type PostgresDatabaseCustomMetricsPublication struct {
	// ObservedGeneration identifies the PostgresDatabase spec generation used
	// to calculate Contributions.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	ObservedGeneration int64 `json:"observedGeneration"`

	// Contributions contains one explicit participation decision per declared
	// database.
	// +kubebuilder:validation:Required
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=10
	Contributions []DatabaseCustomMetricsContribution `json:"contributions"`
}

// DatabaseRoleInfo is the committed credential-ready role surface published by
// the PostgresDatabase controller for the PostgresCluster controller to consume.
// +kubebuilder:validation:XValidation:rule="!self.exists || (has(self.secretRef) && self.secretRef.name.size() > 0)",message="secretRef.name is required when exists is true"
type DatabaseRoleInfo struct {
	// Name is an opaque PostgreSQL role name.
	Name string `json:"name"`

	// SecretRef references the Secret containing the role password. CNPG reads its
	// conventional password key, so only the Secret name crosses this boundary.
	// +optional
	SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`

	// Exists declares whether this role should exist. false is an explicit drop signal.
	Exists bool `json:"exists"`
}

// PostgresDatabaseStatus defines the observed state of PostgresDatabase.
type PostgresDatabaseStatus struct {
	// +optional
	Phase *string `json:"phase,omitempty"`
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// LastTransitionTime is the start time of the active time-to-Ready cycle.
	// It is cleared when the PostgresDatabase successfully reaches Ready.
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`

	// +optional
	Databases []DatabaseInfo `json:"databases,omitempty"`
	// CustomMetricsPublication is committed intent consumed by the
	// PostgresCluster controller. Nil or a stale observed generation means the
	// current participation has not been published yet.
	// +optional
	CustomMetricsPublication *PostgresDatabaseCustomMetricsPublication `json:"customMetricsPublication,omitempty"`
	// ObservedGeneration represents the .metadata.generation that the status was set based upon.
	// +optional
	ObservedGeneration *int64 `json:"observedGeneration,omitempty"`
	// ReconcileFailureType tracks the terminal condition that caused the controller to transition to a failure state.
	// +optional
	// +kubebuilder:validation:Enum=Privileges
	ReconcileFailureType string `json:"reconcileFailureType,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterRef.name`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PostgresDatabase is the Schema for the postgresdatabases API.
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) <= 50",message="name must be 50 characters or fewer to accommodate derived resource names"
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
