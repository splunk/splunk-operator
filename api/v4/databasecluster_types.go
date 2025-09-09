/*
Copyright ...
*/
package v4

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DatabaseClusterSpec struct {
	// Engine is "Postgres" for now
	Engine    string `json:"engine"`
	Version   string `json:"version,omitempty"`
	ManagedBy string `json:"managedBy"` // CNPG | External | AWSAurora

	// Connection normalization
	Connection *ConnectionSpec `json:"connection,omitempty"`

	// CNPG-specific when ManagedBy=CNPG
	CNPG *CNPGSpec `json:"cnpg,omitempty"`

	// External or CSP managed
	External *ExternalSpec `json:"external,omitempty"`

	// Backup and PITR
	Backup *BackupSpec `json:"backup,omitempty"`

	// Init and schema
	Init *InitSpec `json:"init,omitempty"`

	// Network
	Network *NetworkSpec `json:"network,omitempty"`
}

type ConnectionSpec struct {
	SecretName string   `json:"secretName"`
	SSL        *SSLSpec `json:"ssl,omitempty"`
}

type SSLSpec struct {
	Mode           string `json:"mode,omitempty"` // disable, prefer, require, verify-full
	CABundleSecret string `json:"caBundleSecret,omitempty"`
}

type CNPGSpec struct {
	Instances   int32                        `json:"instances,omitempty"`
	Storage     StorageSpec                  `json:"storage"`
	Resources   *corev1.ResourceRequirements `json:"resources,omitempty"`
	Pooler      *PoolerSpec                  `json:"pooler,omitempty"`
	Maintenance *MaintenanceSpec             `json:"maintenance,omitempty"`
}

type StorageSpec struct {
	Size             string `json:"size"`
	StorageClassName string `json:"storageClassName,omitempty"`
}

type PoolerSpec struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode,omitempty"` // rw or ro
}

type MaintenanceSpec struct {
	Window string `json:"window,omitempty"`
}

type ExternalSpec struct {
	DSNSecretRef  string   `json:"dsnSecretRef,omitempty"`
	Host          string   `json:"host,omitempty"`
	Port          int32    `json:"port,omitempty"`
	DBName        string   `json:"dbName,omitempty"`
	UserSecretRef string   `json:"userSecretRef,omitempty"`
	SSL           *SSLSpec `json:"ssl,omitempty"`
}

type BackupSpec struct {
	Enabled     bool                 `json:"enabled"`
	Schedule    string               `json:"schedule,omitempty"`
	Retention   *BackupRetentionSpec `json:"retention,omitempty"`
	ObjectStore *ObjectStoreSpec     `json:"objectStore,omitempty"`
	PITRecovery *PITRecoverySpec     `json:"pitrecovery,omitempty"`
}

type BackupRetentionSpec struct {
	Full    int32 `json:"full,omitempty"`
	WALDays int32 `json:"walDays,omitempty"`
}

type ObjectStoreSpec struct {
	Provider string  `json:"provider"` // s3, gcs, azure
	S3       *S3Spec `json:"s3,omitempty"`
}

type S3Spec struct {
	Bucket            string `json:"bucket"`
	Path              string `json:"path,omitempty"`
	Region            string `json:"region,omitempty"`
	CredentialsSecret string `json:"credentialsSecret,omitempty"`
}

type PITRecoverySpec struct {
	Enabled           bool   `json:"enabled"`
	MaxRecoveryWindow string `json:"maxRecoveryWindow,omitempty"`
}

type InitSpec struct {
	CreateDatabaseIfMissing bool     `json:"createDatabaseIfMissing,omitempty"`
	DatabaseName            string   `json:"databaseName,omitempty"`
	Owner                   string   `json:"owner,omitempty"`
	Extensions              []string `json:"extensions,omitempty"`
	BootstrapSQLSecret      string   `json:"bootstrapSQLSecret,omitempty"`
}

type NetworkSpec struct {
	AllowedCIDRs        []string `json:"allowedCIDRs,omitempty"`
	AllowFromNamespaces []string `json:"allowFromNamespaces,omitempty"`
}

type DatabaseClusterStatus struct {
	Phase            string             `json:"phase,omitempty"`
	Provider         string             `json:"provider,omitempty"`
	Endpoints        *DatabaseEndpoints `json:"endpoints,omitempty"`
	ConnectionSecret string             `json:"connectionSecret,omitempty"`
	Version          string             `json:"version,omitempty"`
	LastBackupTime   *metav1.Time       `json:"lastBackupTime,omitempty"`
	Conditions       []metav1.Condition `json:"conditions,omitempty"`
}

type DatabaseEndpoints struct {
	ReadWrite string `json:"readWrite,omitempty"`
	ReadOnly  string `json:"readOnly,omitempty"`
	External  string `json:"external,omitempty"`
}

const (
	ConditionReady             = "Ready"
	ConditionSecretsReady      = "SecretsReady"
	ConditionCertificatesReady = "CertificatesReady"
	ConditionBackupHealthy     = "BackupHealthy"
	ConditionRestoreInProgress = "RestoreInProgress"
	ConditionDegraded          = "Degraded"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=dbcl
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=.status.provider
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=.status.phase
// +kubebuilder:printcolumn:name="RW",type=string,JSONPath=.status.endpoints.readWrite
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=.status.version

// DatabaseCluster is the Schema for the databaseclusters API.
type DatabaseCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DatabaseClusterSpec   `json:"spec,omitempty"`
	Status DatabaseClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DatabaseClusterList contains a list of DatabaseCluster.
type DatabaseClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DatabaseCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DatabaseCluster{}, &DatabaseClusterList{})
}
