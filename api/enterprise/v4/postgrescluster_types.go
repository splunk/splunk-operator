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
// Exactly one of VolumeSnapshot or ObjectStorage must be set.
// +kubebuilder:validation:XValidation:rule="has(self.volumeSnapshot) != has(self.objectStorage)",message="exactly one of volumeSnapshot or objectStorage must be set"
type BootstrapFrom struct {
	// VolumeSnapshot restores from Kubernetes VolumeSnapshot resources.
	// Mutually exclusive with objectStorage.
	// +optional
	VolumeSnapshot *VolumeSnapshotSource `json:"volumeSnapshot,omitempty"`

	// ObjectStorage restores from an object storage backup (e.g. S3) managed by the
	// PostgresClusterClass. Bucket path and credentials are resolved from the class.
	// Mutually exclusive with volumeSnapshot.
	// +optional
	ObjectStorage *ObjectStorageSource `json:"objectStorage,omitempty"`

	// RecoveryTarget defines an optional point-in-time recovery (PITR) target.
	// When omitted, recovery replays all available WAL (recovery to latest).
	// For a volumeSnapshot source, setting recoveryTarget requires volumeSnapshot.walArchive
	// so WAL segments can be replayed past the snapshot point.
	// +optional
	RecoveryTarget *RecoveryTarget `json:"recoveryTarget,omitempty"`
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

	// WalArchive identifies the object store WAL archive to replay WAL segments after the
	// snapshot. Bucket path and credentials are resolved from the class backup config.
	// Required when RecoveryTarget is set (PITR); optional for a plain snapshot restore.
	// +optional
	WalArchive *ObjectStorageSource `json:"walArchive,omitempty"`
}

// ObjectStorageSource identifies a backup in an object store managed by the PostgresClusterClass.
// Bucket path, credentials, and endpoint config are resolved from the class — not specified here.
type ObjectStorageSource struct {
	// ServerName is the identifier of the source cluster in the object store.
	// Must match the server name used when the backup was written.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	ServerName string `json:"serverName"`
}

// RecoveryTargetType enumerates the kinds of point-in-time recovery target a user can request.
// +kubebuilder:validation:Enum=time;lsn;xid;name;immediate
type RecoveryTargetType string

const (
	// RecoveryTargetTime recovers up to a timestamp (RFC 3339), carried in RecoveryTarget.Value.
	RecoveryTargetTime RecoveryTargetType = "time"
	// RecoveryTargetLSN recovers up to a WAL log sequence number, carried in RecoveryTarget.Value.
	RecoveryTargetLSN RecoveryTargetType = "lsn"
	// RecoveryTargetXID recovers up to a transaction ID, carried in RecoveryTarget.Value.
	RecoveryTargetXID RecoveryTargetType = "xid"
	// RecoveryTargetName recovers to a named restore point (pg_create_restore_point), carried in
	// RecoveryTarget.Value.
	RecoveryTargetName RecoveryTargetType = "name"
	// RecoveryTargetImmediate ends recovery as soon as a consistent state is reached. It takes no
	// value — RecoveryTarget.Value must be empty for this type.
	RecoveryTargetImmediate RecoveryTargetType = "immediate"
)

// RecoveryTarget defines the point-in-time recovery target as a discriminated (type, value) pair.
// This mirrors PostgreSQL's own model — a single recovery_target_* is chosen — so the API cannot
// express two conflicting targets at once, and core can dispatch on a single Type field.
//   - type=time|lsn|xid|name require a non-empty value (the timestamp / LSN / XID / restore-point name).
//   - type=immediate takes no value; value must be empty.
//
// +kubebuilder:validation:XValidation:rule="self.type == 'immediate' ? !has(self.value) || size(self.value) == 0 : has(self.value) && size(self.value) > 0",message="value is required for target types time, lsn, xid, and name, and must be empty for type immediate"
type RecoveryTarget struct {
	// Type selects which kind of recovery target Value carries.
	// +kubebuilder:validation:Required
	Type RecoveryTargetType `json:"type"`

	// Value is the target's value, interpreted according to Type:
	// an RFC 3339 timestamp (time), a WAL LSN (lsn), a transaction ID (xid), or a restore-point name
	// (name). It must be empty when Type is immediate.
	// +optional
	Value string `json:"value,omitempty"`

	// Exclusive stops recovery just before the target rather than just after (default: false).
	// Ignored for type immediate.
	// +optional
	Exclusive *bool `json:"exclusive,omitempty"`
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

	// PostgresMajorUpgradeConfig sets up the upgrade flow according to backup, version and strategy requirements.
	// +optional
	PostgresMajorUpgradeConfig *PostgresMajorUpgradeConfig `json:"postgresMajorUpgradeConfig,omitempty"`
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

	// Ordered cluster-scoped sources; selector optional fields are unsupported.
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=100
	// +kubebuilder:validation:XValidation:rule="self.all(x, has(x.name) && x.name.size() > 0)",message="name must not be empty"
	// +kubebuilder:validation:XValidation:rule="self.all(x, x.key.size() > 0)",message="key must not be empty"
	// +optional
	CustomQueriesConfigMap []corev1.ConfigMapKeySelector `json:"customQueriesConfigMap,omitempty"`
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

type PostgresMajorUpgradeConfig struct {
	// Allow permits the operator to execute a PostgreSQL major-version
	// upgrade when spec.postgresVersion crosses a major-version boundary.
	// +optional
	Allow *bool `json:"allow,omitempty"`

	// Strategy selects the major-upgrade implementation.
	// For now only pgUpgrade is supported.
	// +kubebuilder:validation:Enum=pgUpgrade
	// +kubebuilder:default=pgUpgrade
	// +optional
	Strategy *string `json:"strategy,omitempty"`
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

	// LastTransitionTime is the start time of the active time-to-Ready cycle.
	// It is cleared when the PostgresCluster successfully reaches Ready.
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`

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

	// Per-database decisions consumed by database readiness gates.
	// +optional
	CustomMetricsStatus *CustomMetricsStatus `json:"customMetricsStatus,omitempty"`

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

	// PostgresMajorUpgradeStatus contains the information
	// about upgrade completion and any needed rollback/backup information
	// shall the upgrade be reverted manually.
	// It's a list to support a case of multi version upgrade.
	// +optional
	PostgresMajorUpgradeStatus []PostgresMajorUpgradeStatus `json:"postgresMajorUpgradeStatus,omitempty"`

	// CurrentPgVersion is the PostgreSQL major version currently running against
	// the data directory, as reported by CNPG (PGDataImageInfo.MajorVersion).
	// Written on every reconcile; used as the source-version baseline for the
	// major-version upgrade use case when no prior upgrade status entries exist.
	// +optional
	CurrentPgVersion string `json:"currentPgVersion,omitempty"`
}

// CustomMetricsStatus crosses the cluster-to-database acknowledgement boundary.
type CustomMetricsStatus struct {
	// +listType=map
	// +listMapKey=postgresDatabaseName
	// +listMapKey=postgresDatabaseUID
	// +listMapKey=databaseName
	// +optional
	DatabaseContributions []DatabaseCustomMetricsStatus `json:"databaseContributions,omitempty"`
}

type DatabaseCustomMetricsStatus struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	PostgresDatabaseName string `json:"postgresDatabaseName"`
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	PostgresDatabaseUID string `json:"postgresDatabaseUID"`
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	DatabaseName string `json:"databaseName"`
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	DesiredRevision string `json:"desiredRevision"`
	// +optional
	AppliedRevision string `json:"appliedRevision,omitempty"`
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=True;False;Unknown
	Status metav1.ConditionStatus `json:"status"`
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Reason string `json:"reason"`
	// +kubebuilder:validation:Required
	Message string `json:"message"`
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

	// ObjectStorage is the server name of the object storage backup the cluster was restored from.
	// +optional
	ObjectStorage *string `json:"objectStorage,omitempty"`

	// RequestedRecoveryTarget echoes the point-in-time recovery target requested in spec.bootstrapFrom,
	// if any. It is derived from the desired spec, not observed from the provider, so it records what
	// the restore was asked to recover to (not a confirmation of where recovery actually stopped).
	// Nil for recovery to the latest available WAL.
	// +optional
	RequestedRecoveryTarget *RecoveryTargetStatus `json:"requestedRecoveryTarget,omitempty"`
}

// RecoveryTargetStatus is the structured echo of a requested recovery target, mirroring the
// spec RecoveryTarget shape so status consumers do not have to parse a formatted string.
type RecoveryTargetStatus struct {
	// Type is the kind of recovery target that was requested.
	Type RecoveryTargetType `json:"type"`

	// Value is the target's value (empty for type immediate).
	// +optional
	Value string `json:"value,omitempty"`

	// Exclusive reports whether recovery was requested to stop just before the target (true) rather
	// than just after (false/omitted).
	// +optional
	Exclusive *bool `json:"exclusive,omitempty"`
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

type PostgresMajorUpgradeStatus struct {
	Phase           *string            `json:"phase,omitempty"`
	Strategy        *string            `json:"strategy,omitempty"`
	SourcePgVersion *string            `json:"sourcePgVersion,omitempty"`
	TargetPgVersion *string            `json:"targetPgVersion,omitempty"`
	StartedAt       *metav1.Time       `json:"startedAt,omitempty"`
	CompletedAt     *metav1.Time       `json:"completedAt,omitempty"`
	Conditions      []metav1.Condition `json:"conditions,omitempty"`
	BackupStatus    *BackupStatus      `json:"backupStatus,omitempty"`
	// BackupNames holds the names of the CNPG Backup objects created as
	// rollback baselines for this upgrade hop. Use these to locate the
	// backup and any provider-specific references needed for manual recovery.
	// +optional
	BackupNames *UpgradeBackupNames `json:"backupNames,omitempty"`
}

// UpgradeBackupNames records the CNPG Backup object names created at each
// gate of a major-version upgrade hop.
type UpgradeBackupNames struct {
	// PreUpgrade is the backup taken before the pg_upgrade run.
	// +optional
	PreUpgrade *string `json:"preUpgrade,omitempty"`
	// PostUpgrade is the backup taken after the upgraded cluster is verified.
	// +optional
	PostUpgrade *string `json:"postUpgrade,omitempty"`
}

func init() {
	SchemeBuilder.Register(&PostgresCluster{}, &PostgresClusterList{})
}
