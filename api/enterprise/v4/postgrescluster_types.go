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

// BootstrapFrom defines the source from which a PostgresCluster is bootstrapped via recovery.
type BootstrapFrom struct {
	// VolumeSnapshot restores from Kubernetes VolumeSnapshot resources.
	// Required whenever bootstrapFrom is set — it is the only supported recovery source.
	// +kubebuilder:validation:Required
	VolumeSnapshot *VolumeSnapshotSource `json:"volumeSnapshot"`
}

// VolumeSnapshotSource identifies the VolumeSnapshot resources to use as the base backup.
type VolumeSnapshotSource struct {
	// Storage is the name of the VolumeSnapshot containing the PGDATA directory.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Storage string `json:"storage"`

	// WalStorage is the name of the VolumeSnapshot containing the pg_wal directory.
	// Required only when the source cluster had a separate WAL volume.
	// +optional
	// +kubebuilder:validation:MinLength=1
	WalStorage *string `json:"walStorage,omitempty"`
}

// PostgresClusterSpec defines the desired state of PostgresCluster.
// Validation rules: Class is immutable; Storage cannot decrease and
// PostgresVersion's major version cannot be downgraded once set, and neither
// can be removed once set. Instances can be raised, lowered, or cleared
// (clearing returns to the class default) subject to the scaling rules in
// scaling-out.md, which the admission webhook enforces against the merged
// effective instance count.
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.postgresVersion) || (has(self.postgresVersion) && int(self.postgresVersion.split('.')[0]) >= int(oldSelf.postgresVersion.split('.')[0]))",messageExpression="!has(self.postgresVersion) ? 'postgresVersion cannot be removed once set (was: ' + oldSelf.postgresVersion + ')' : 'postgresVersion major version cannot be downgraded (from: ' + oldSelf.postgresVersion + ', to: ' + self.postgresVersion + ')'"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.storage) || (has(self.storage) && quantity(self.storage).compareTo(quantity(oldSelf.storage)) >= 0)",messageExpression="!has(self.storage) ? 'storage cannot be removed once set (was: ' + string(oldSelf.storage) + ')' : 'storage size cannot be decreased (from: ' + string(oldSelf.storage) + ', to: ' + string(self.storage) + ')'"
// +kubebuilder:validation:XValidation:rule="(has(self.passwordConfig) == has(oldSelf.passwordConfig))",message="passwordConfig cannot be altered after creation"
// +kubebuilder:validation:XValidation:rule="!has(self.passwordConfig) || self.passwordConfig == oldSelf.passwordConfig",message="passwordConfig is immutable once set"

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
	// +optional
	PostgresVersion *string `json:"postgresVersion,omitempty"`

	// Resources overrides CPU/memory resources from ClusterClass.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// PostgreSQLConfig overrides PostgreSQL engine parameters from ClusterClass.
	// Maps to postgresql.conf settings.
	// Example: {"shared_buffers": "128MB", "log_min_duration_statement": "500ms"}
	// +optional
	PostgreSQLConfig map[string]string `json:"postgresqlConfig,omitempty"`

	// PgHBA contains pg_hba.conf host-based authentication rules.
	// Defines client authentication and connection security (cluster-wide).
	// Example: ["hostssl all all 0.0.0.0/0 scram-sha-256"]
	// +optional
	PgHBA []string `json:"pgHBA,omitempty"`

	// ConnectionPooler controls whether PgBouncer connection pooling is deployed for this cluster.
	// Sub-fields override the matching values from the class-level connectionPooler config.
	// +optional
	ConnectionPooler *ConnectionPoolerEnableConfig `json:"connectionPooler,omitempty"`

	// ClusterDeletionPolicy controls the deletion behavior of the underlying CNPG Cluster when the PostgresCluster is deleted.
	// +kubebuilder:validation:Enum=Delete;Retain
	// +kubebuilder:default=Retain
	// +optional
	ClusterDeletionPolicy *string `json:"clusterDeletionPolicy,omitempty"`

	// Monitoring contains configuration for metrics exposure features.
	// +optional
	Monitoring *PostgresClusterMonitoring `json:"monitoring,omitempty"`

	// Backup overrides backup settings from ClusterClass.
	// Only generic fields (enabled, schedule) can be overridden.
	// +optional
	Backup *BackupConfig `json:"backup,omitempty"`

	// BootstrapFrom configures recovery-based bootstrapping from an existing backup.
	// When set, the cluster is initialized from the specified backup source instead of a fresh initdb.
	// This field is immutable after creation.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="bootstrapFrom is immutable"
	BootstrapFrom *BootstrapFrom `json:"bootstrapFrom,omitempty"`

	// External superuser secret configuration,
	// if non empty, external secret management is mandatory.
	// +optional
	PasswordConfig *SuperuserPasswordConfig `json:"passwordConfig,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="self.superuserExternalSecretRef.name.size() > 0",message="superuserExternalSecretRef.name must not be empty"
type SuperuserPasswordConfig struct {
	// +kubebuilder:validation:Required
	SuperuserExternalSecretRef corev1.LocalObjectReference `json:"superuserExternalSecretRef"`
}

// PostgresClusterMonitoring overrides monitoring configuration options for PostgresClusterClass.
// Set a field to false to disable a metric target that is enabled in the class.
type PostgresClusterMonitoring struct {
	// PostgreSQLMetrics overrides whether PostgreSQL metrics scraping is enabled.
	// When unset, the class-level setting applies.
	// +optional
	PostgreSQLMetrics *bool `json:"postgresqlMetrics,omitempty"`

	// ConnectionPoolerMetrics overrides whether connection pooler metrics scraping is enabled.
	// When unset, the class-level setting applies.
	// +optional
	ConnectionPoolerMetrics *bool `json:"connectionPoolerMetrics,omitempty"`
}

// PostgresClusterResources defines references to Kubernetes resources related to the PostgresCluster, such as ConfigMaps and Secrets.
type PostgresClusterResources struct {
	// ConfigMapRef references the ConfigMap with connection endpoints.
	// Contains: CLUSTER_ENDPOINTS, POOLER_ENDPOINTS (if connection pooler enabled)
	// +optional
	ConfigMapRef *corev1.LocalObjectReference `json:"configMapRef,omitempty"`

	// SuperUserSecretRef references the Secret containing the superuser credentials.
	// +optional
	SuperUserSecretRef *corev1.SecretKeySelector `json:"superUserSecretRef,omitempty"`
}

// PostgresClusterStatus defines the observed state of PostgresCluster.
type PostgresClusterStatus struct {
	// Phase represents the current phase of the PostgresCluster.
	// Values: "Pending", "Provisioning", "Failed", "Ready", "Deleting"
	// +optional
	Phase *string `json:"phase,omitempty"`

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

	// ManagedRolesStatus tracks the reconciliation status of managed roles.
	// +optional
	ManagedRolesStatus *ManagedRolesStatus `json:"managedRolesStatus,omitempty"`

	// Resources contains references to related Kubernetes resources like ConfigMaps and Secrets.
	// +optional
	Resources *PostgresClusterResources `json:"resources,omitempty"`

	// ObservedGeneration represents the .metadata.generation that the status was set based upon.
	// +optional
	ObservedGeneration *int64 `json:"observedGeneration,omitempty"`

	// BackupStatus contains the observed state of backup configuration.
	// +optional
	BackupStatus *BackupStatus `json:"backupStatus,omitempty"`

	// Restore contains the observed state of a recovery-bootstrapped cluster.
	// +optional
	Restore *RestoreStatus `json:"restore,omitempty"`

	// Instances is the declared instance count reported by the underlying
	// provisioner.
	// +optional
	Instances *int32 `json:"instances,omitempty"`

	// ReadyInstances is the number of instances reported as ready by the
	// underlying provisioner.
	// +optional
	ReadyInstances *int32 `json:"readyInstances,omitempty"`

	// CurrentPrimary is the name of the pod currently hosting the primary.
	// +optional
	CurrentPrimary *string `json:"currentPrimary,omitempty"`
}

// ManagedRolesStatus tracks the state of managed PostgreSQL roles.
type ManagedRolesStatus struct {
	// Reconciled contains roles that have been successfully created and are ready.
	// +optional
	Reconciled []string `json:"reconciled,omitempty"`

	// Pending contains roles that are being created but not yet ready.
	// +optional
	Pending []string `json:"pending,omitempty"`

	// Failed contains roles that failed to reconcile with error messages.
	// +optional
	Failed map[string]string `json:"failed,omitempty"`

	// RoleOwners is the durable incumbency map from role name to owning PostgresDatabase.
	// +optional
	RoleOwners map[string]RoleOwnerReference `json:"roleOwners,omitempty"`

	// Conflicts contains non-fatal role ownership conflicts detected while computing desired roles.
	// +optional
	Conflicts []RoleConflict `json:"conflicts,omitempty"`
}

// RoleOwnerReference identifies the PostgresDatabase that owns a managed role.
type RoleOwnerReference struct {
	// Name is the owning PostgresDatabase name.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// UID is the owning PostgresDatabase UID.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	UID string `json:"uid"`
}

// RoleConflict records a role-level ownership conflict.
type RoleConflict struct {
	// Role is the contested PostgreSQL role name.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Role string `json:"role"`

	// ClaimedBy is the incumbent or winner, when one exists.
	// +optional
	ClaimedBy *RoleOwnerReference `json:"claimedBy,omitempty"`

	// AttemptedBy is the PostgresDatabase whose claim was withheld.
	AttemptedBy RoleOwnerReference `json:"attemptedBy"`
}

// ConnectionPoolerStatus contains the observed state of the connection pooler.
type ConnectionPoolerStatus struct {
	// Enabled indicates whether pooler is active for this cluster.
	Enabled bool `json:"enabled"`

	// ReadWriteEnabled is true when the RW pooler resource is reconciled by the operator.
	// +optional
	ReadWriteEnabled bool `json:"readWriteEnabled,omitempty"`

	// ReadOnlyEnabled is true when the RO pooler resource is reconciled by the operator.
	// Independent consumers should mirror this gate before advertising RO pooler endpoints.
	// +optional
	ReadOnlyEnabled bool `json:"readOnlyEnabled,omitempty"`
}

// BackupStatus contains the observed state of backup configuration.
type BackupStatus struct {
	// VolumeSnapshot contains status for volume snapshot backups.
	// +optional
	VolumeSnapshot *VolumeSnapshotBackupStatus `json:"volumeSnapshot,omitempty"`

	// ObjectStore contains status for barman object storage backups.
	// +optional
	ObjectStore *ObjectStoreBackupStatus `json:"objectStore,omitempty"`
}

// ObjectStoreBackupStatus contains the observed state of barman object storage backups.
type ObjectStoreBackupStatus struct {
	// Enabled indicates whether object store backups are active.
	Enabled bool `json:"enabled"`

	// LastScheduleTime is when the last backup was scheduled.
	// +optional
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`

	// NextScheduleTime is the next scheduled backup time.
	// +optional
	NextScheduleTime *metav1.Time `json:"nextScheduleTime,omitempty"`
}

// VolumeSnapshotBackupStatus contains the observed state of volume snapshot backups.
type VolumeSnapshotBackupStatus struct {
	// Enabled indicates whether volume snapshot backups are active.
	Enabled bool `json:"enabled"`

	// LastScheduleTime is when the last backup was scheduled.
	// +optional
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`

	// NextScheduleTime is the next scheduled backup time.
	// +optional
	NextScheduleTime *metav1.Time `json:"nextScheduleTime,omitempty"`
}

// RestoreStatus contains the observed state of a recovery-bootstrapped cluster.
type RestoreStatus struct {
	// Source identifies the backup the cluster was restored from.
	// +optional
	Source RestoreSourceStatus `json:"source,omitempty"`

	// CredentialSweep tracks the post-recovery credential sweep.
	// +optional
	CredentialSweep RestoreCredentialSweepStatus `json:"credentialSweep,omitempty"`
}

// RestoreSourceStatus identifies the backup source used during recovery.
type RestoreSourceStatus struct {
	// VolumeSnapshot is the name of the VolumeSnapshot the cluster was restored from.
	// +optional
	VolumeSnapshot *string `json:"volumeSnapshot,omitempty"`
}

// RestoreCredentialSweepStatus tracks whether the post-recovery credential sweep has run.
type RestoreCredentialSweepStatus struct {
	// Completed is true once the credential sweep has run successfully.
	// +optional
	Completed bool `json:"completed,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Class",type=string,JSONPath=`.spec.class`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Instances",type=integer,JSONPath=`.status.instances`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyInstances`
// +kubebuilder:printcolumn:name="Primary",type=string,JSONPath=`.status.currentPrimary`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PostgresCluster is the Schema for the postgresclusters API.
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) <= 50",message="name must be 50 characters or fewer to accommodate derived resource names"
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
