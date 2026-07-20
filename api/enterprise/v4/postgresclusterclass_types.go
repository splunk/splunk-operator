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

// +kubebuilder:validation:XValidation:rule="!has(self.cnpg) || self.provisioner == 'postgresql.cnpg.io'",message="cnpg config can only be set when provisioner is postgresql.cnpg.io"
// +kubebuilder:validation:XValidation:rule="self.provisioner != 'postgresql.cnpg.io' || has(self.cnpg)",message="cnpg config is required when provisioner is postgresql.cnpg.io"
// +kubebuilder:validation:XValidation:rule="!has(self.config) || !has(self.config.connectionPooler) || !has(self.config.connectionPooler.enabled) || !self.config.connectionPooler.enabled || (has(self.cnpg) && has(self.cnpg.connectionPooler))",message="cnpg.connectionPooler must be set when config.connectionPooler.enabled is true"
// +kubebuilder:validation:XValidation:rule="!has(self.config) || !has(self.config.backup) || !has(self.config.backup.enabled) || !self.config.backup.enabled || (has(self.cnpg) && has(self.cnpg.backup) && (has(self.cnpg.backup.volumeSnapshot) || has(self.cnpg.backup.barmanObjectStore)))",message="cnpg.backup.volumeSnapshot or cnpg.backup.barmanObjectStore must be set when config.backup.enabled is true"
// +kubebuilder:validation:XValidation:rule="!has(self.config) || !has(self.config.backup) || !has(self.config.backup.enabled) || !self.config.backup.enabled || (has(self.config.backup.schedule) && self.config.backup.schedule != '')",message="config.backup.schedule is required when config.backup.enabled is true"
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="PostgresClusterClass is immutable after creation"
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
	Config *PostgresClusterClassConfig `json:"config,omitempty"`

	// CNPG contains CloudNativePG-specific configuration and policies.
	// Only used when Provisioner is "postgresql.cnpg.io"
	// These settings CANNOT be overridden in PostgresCluster CR (platform policy).
	// +optional
	CNPG *CNPGConfig `json:"cnpg,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!has(self.monitoring) || !has(self.monitoring.connectionPoolerMetrics) || !has(self.monitoring.connectionPoolerMetrics.enabled) || !self.monitoring.connectionPoolerMetrics.enabled || (has(self.connectionPooler) && has(self.connectionPooler.enabled) && self.connectionPooler.enabled)",message="connectionPooler.enabled must be true when monitoring.connectionPoolerMetrics.enabled is true"
// PostgresClusterClassConfig contains provider-agnostic cluster configuration.
// These fields define PostgresCluster infrastructure and can be overridden in PostgresCluster CR.
type PostgresClusterClassConfig struct {
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

	// ConnectionPooler controls whether PgBouncer connection pooling is deployed
	// and which endpoints get a pooler. Sub-fields can be overridden per cluster.
	// +optional
	ConnectionPooler *ConnectionPoolerEnableConfig `json:"connectionPooler,omitempty"`

	// Monitoring contains configuration for metrics exposure.
	// When enabled, creates metrics resources for clusters using this class.
	// Can be overridden in PostgresCluster CR.
	// +kubebuilder:default={}
	// +optional
	Monitoring *PostgresMonitoringClassConfig `json:"monitoring,omitempty"`

	// Backup contains provider-agnostic backup configuration.
	// Can be overridden in PostgresCluster CR.
	// +optional
	Backup *BackupConfig `json:"backup,omitempty"`
}

// ConnectionPoolerEnableConfig controls whether PgBouncer connection pooling is
// deployed and which endpoints get a pooler. Sub-fields are only consulted when
// Enabled is true.
//
// This type is shared by the class config and the per-cluster spec, so its
// fields intentionally carry NO +kubebuilder:default markers: a CRD default
// would be materialized onto the stored cluster object by the apiserver, which
// would overwrite the nil ("inherit from class") sentinel that
// mergeConnectionPoolerEnable relies on for per-field overrides. Defaulting for
// omitted fields is owned by the Go layer instead — see isPoolerEnabled
// (nil enabled → false), poolerReadWriteWanted and poolerReadOnlyWanted
// (nil → true).
type ConnectionPoolerEnableConfig struct {
	// Enabled controls whether the connection pooler is deployed at all.
	// When omitted the pooler is treated as disabled (see isPoolerEnabled).
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// ReadWrite controls whether the RW pooler is reconciled when Enabled is true.
	// When omitted it is treated as true (see poolerReadWriteWanted).
	// +optional
	ReadWrite *bool `json:"readWrite,omitempty"`

	// ReadOnly controls whether the RO pooler is reconciled when Enabled is true.
	// The RO pooler additionally requires the cluster to run with instances >= 2;
	// the admission webhook rejects readOnly=true with instances<2.
	// When omitted it is treated as true (see poolerReadOnlyWanted).
	// +optional
	ReadOnly *bool `json:"readOnly,omitempty"`
}

// ConnectionPoolerMode defines the PgBouncer connection pooling strategy.
// +kubebuilder:validation:Enum=session;transaction;statement
type ConnectionPoolerMode string

const (
	// ConnectionPoolerModeSession assigns a connection for the entire client session (most compatible).
	ConnectionPoolerModeSession ConnectionPoolerMode = "session"

	// ConnectionPoolerModeTransaction returns the connection after each transaction (recommended).
	ConnectionPoolerModeTransaction ConnectionPoolerMode = "transaction"

	// ConnectionPoolerModeStatement returns the connection after each statement (limited compatibility).
	ConnectionPoolerModeStatement ConnectionPoolerMode = "statement"
)

// ConnectionPoolerConfig defines PgBouncer connection pooler configuration.
// When enabled, creates RW and RO pooler deployments for clusters using this class.
type ConnectionPoolerConfig struct {
	// Instances is the number of PgBouncer pod replicas.
	// Higher values provide better availability and load distribution.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:default=3
	// +optional
	Instances *int32 `json:"instances,omitempty"`

	// Mode defines the connection pooling strategy.
	// +kubebuilder:default="transaction"
	// +optional
	Mode *ConnectionPoolerMode `json:"mode,omitempty"`

	// Config contains PgBouncer configuration parameters.
	// Passed directly to CNPG Pooler spec.pgbouncer.parameters.
	// See: https://cloudnative-pg.io/docs/1.30/connection_pooling/#pgbouncer-configuration-options
	// +optional
	Config map[string]string `json:"config,omitempty"`
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
	// +kubebuilder:default=restart
	// +optional
	PrimaryUpdateMethod *string `json:"primaryUpdateMethod,omitempty"`

	// ConnectionPooler contains PgBouncer connection pooler configuration.
	// When enabled, creates RW and RO pooler deployments for clusters using this class.
	// +optional
	ConnectionPooler *ConnectionPoolerConfig `json:"connectionPooler,omitempty"`

	// Backup contains CNPG-specific backup configuration.
	// Cannot be overridden in PostgresCluster CR (platform policy).
	// +optional
	Backup *CNPGBackupConfig `json:"backup,omitempty"`
}

// BackupConfig contains provider-agnostic backup settings.
// These fields can be overridden in PostgresCluster CR.
type BackupConfig struct {
	// Enabled controls whether automated backups are active.
	// When true, schedule is required and the class must define a provider backup implementation policy.
	// When unset on PostgresCluster, the class-level value is inherited.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Schedule is a standard 5-field cron expression for backup timing.
	// Required when enabled is true.
	// Example: "0 2 * * *" (daily at 2am)
	// +kubebuilder:validation:Pattern=`^(\S+\s+){4}\S+$`
	// +optional
	Schedule *string `json:"schedule,omitempty"`
}

// CNPGBackupConfig contains CNPG-specific backup configuration.
// Cannot be overridden in PostgresCluster CR (platform policy).
type CNPGBackupConfig struct {
	// VolumeSnapshot configures CSI volume snapshot backups.
	// +optional
	VolumeSnapshot *CNPGVolumeSnapshotConfig `json:"volumeSnapshot,omitempty"`

	// BarmanObjectStore configures object storage backups via the barman-cloud CNPG plugin.
	// The operator creates and manages a barmancloud.cnpg.io/v1 ObjectStore from this config.
	// +optional
	BarmanObjectStore *CNPGBarmanObjectStoreConfig `json:"barmanObjectStore,omitempty"`

	// Target selects which instance performs backups.
	// +kubebuilder:validation:Enum=primary;prefer-standby
	// +kubebuilder:default="prefer-standby"
	// +optional
	Target *string `json:"target,omitempty"`
}

// CNPGBarmanObjectStoreConfig contains the configuration for object storage backups
// via the barman-cloud CNPG plugin. The operator creates and manages a
// barmancloud.cnpg.io/v1 ObjectStore resource in the cluster namespace from this config.
// Users only need to create the referenced credentials Secret.
type CNPGBarmanObjectStoreConfig struct {
	// DestinationPath is the S3-compatible object storage path for backups.
	// Example: "s3://my-bucket/postgres/clusters/"
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	DestinationPath string `json:"destinationPath"`

	// EndpointURL is the S3-compatible endpoint URL.
	// Defaults to AWS S3 if omitted.
	// Example: "https://s3.us-east-1.amazonaws.com"
	// +optional
	EndpointURL *string `json:"endpointURL,omitempty"`

	// S3Credentials contains the references to the Kubernetes Secret holding AWS credentials.
	// The referenced Secret must exist in the same namespace as the PostgresCluster.
	// +kubebuilder:validation:Required
	S3Credentials CNPGBarmanS3Credentials `json:"s3Credentials"`

	// WAL contains WAL archiving configuration.
	// +optional
	WAL *CNPGBarmanWALConfig `json:"wal,omitempty"`

	// RetentionPolicy defines how long backups are retained.
	// Format: positive number followed by 'd' (days). Example: "30d".
	// +kubebuilder:validation:Pattern=`^[1-9][0-9]*d$`
	// +optional
	RetentionPolicy *string `json:"retentionPolicy,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="size(self.accessKeyId.name) > 0",message="accessKeyId.name must not be empty"
// +kubebuilder:validation:XValidation:rule="size(self.accessKeyId.key) > 0",message="accessKeyId.key must not be empty"
// +kubebuilder:validation:XValidation:rule="size(self.secretAccessKey.name) > 0",message="secretAccessKey.name must not be empty"
// +kubebuilder:validation:XValidation:rule="size(self.secretAccessKey.key) > 0",message="secretAccessKey.key must not be empty"
// CNPGBarmanS3Credentials references Kubernetes Secret keys for AWS S3 credentials.
type CNPGBarmanS3Credentials struct {
	// AccessKeyId references the Secret key containing the AWS access key ID.
	// +kubebuilder:validation:Required
	AccessKeyId corev1.SecretKeySelector `json:"accessKeyId"`

	// SecretAccessKey references the Secret key containing the AWS secret access key.
	// +kubebuilder:validation:Required
	SecretAccessKey corev1.SecretKeySelector `json:"secretAccessKey"`
}

// CNPGBarmanWALConfig contains WAL archiving configuration for barman.
type CNPGBarmanWALConfig struct {
	// Compression algorithm for WAL files.
	// +kubebuilder:validation:Enum=gzip;bzip2;snappy
	// +optional
	Compression *string `json:"compression,omitempty"`

	// Encryption algorithm for WAL files.
	// +kubebuilder:validation:Enum=AES256;"aws:kms"
	// +optional
	Encryption *string `json:"encryption,omitempty"`
}

// CNPGVolumeSnapshotConfig contains volume snapshot backup settings.
type CNPGVolumeSnapshotConfig struct {
	// ClassName is the VolumeSnapshotClass for PG_DATA PVC.
	// +optional
	ClassName *string `json:"className,omitempty"`

	// WalClassName is the VolumeSnapshotClass for PG_WAL PVC.
	// +optional
	WalClassName *string `json:"walClassName,omitempty"`

	// SnapshotOwnerReference controls ownership of VolumeSnapshot resources.
	// "none" - snapshots persist independently, require manual cleanup
	// "cluster" - snapshots are garbage collected when the CNPG Cluster is deleted
	// +kubebuilder:validation:Enum=none;cluster
	// +kubebuilder:default="none"
	// +optional
	SnapshotOwnerReference *string `json:"snapshotOwnerReference,omitempty"`

	// Online controls whether snapshots are taken online (hot) or offline (cold).
	// +kubebuilder:default=true
	// +optional
	Online *bool `json:"online,omitempty"`

	// Labels for VolumeSnapshot resources.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations for VolumeSnapshot resources.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// PostgresClusterClassStatus defines the observed state of PostgresClusterClass.
type PostgresClusterClassStatus struct {
	// Conditions represent the latest available observations of the PostgresClusterClass state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase represents the current phase of the PostgresClusterClass.
	// Valid phases: "Ready", "Invalid"
	// +optional
	Phase *string `json:"phase,omitempty"`
}

type PostgresMonitoringClassConfig struct {
	// +optional
	PostgreSQLMetrics *MetricsClassConfig `json:"postgresqlMetrics,omitempty"`
	// +optional
	ConnectionPoolerMetrics *MetricsClassConfig `json:"connectionPoolerMetrics,omitempty"`
}

type MetricsClassConfig struct {
	// Enabled controls whether metrics resources should be created for this target.
	// +kubebuilder:default=false
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Provisioner",type=string,JSONPath=`.spec.provisioner`
// +kubebuilder:printcolumn:name="Instances",type=integer,JSONPath=`.spec.config.instances`
// +kubebuilder:printcolumn:name="Storage",type=string,JSONPath=`.spec.config.storage`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.config.postgresVersion`
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
