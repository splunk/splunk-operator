package v4

import (
	corev1 "k8s.io/api/core/v1"
)

// +kubebuilder:validation:Optional

type SSLSpec struct {
	// disable | prefer | require | verify-full
	Mode           string `json:"mode,omitempty"`
	CABundleSecret string `json:"caBundleSecret,omitempty"`
}

type ConnectionSpec struct {
	SecretName       string   `json:"secretName"`
	SSL              *SSLSpec `json:"ssl,omitempty"`
	ClientCertSecret string   `json:"clientCertSecret,omitempty"`
}

type StorageSpec struct {
	Size             string `json:"size"`
	StorageClassName string `json:"storageClassName,omitempty"`
}

type PoolerSpec struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode,omitempty"` // rw|ro
}

type MaintenanceSpec struct {
	Window string `json:"window,omitempty"`
}

type CNPGSpec struct {
	Instances   int32                        `json:"instances,omitempty"`
	Storage     StorageSpec                  `json:"storage"`
	Resources   *corev1.ResourceRequirements `json:"resources,omitempty"`
	Pooler      *PoolerSpec                  `json:"pooler,omitempty"`
	Maintenance *MaintenanceSpec             `json:"maintenance,omitempty"`
}

type BackupRetentionSpec struct {
	Full    int32 `json:"full,omitempty"`
	WALDays int32 `json:"walDays,omitempty"`
}

type S3Spec struct {
	Bucket            string `json:"bucket"`
	Path              string `json:"path,omitempty"`
	Region            string `json:"region,omitempty"`
	CredentialsSecret string `json:"credentialsSecret,omitempty"`
}

type ObjectStoreSpec struct {
	Provider string  `json:"provider"` // s3|gcs|azure
	S3       *S3Spec `json:"s3,omitempty"`
}

type PITRecoverySpec struct {
	Enabled           bool   `json:"enabled"`
	MaxRecoveryWindow string `json:"maxRecoveryWindow,omitempty"`
}

type BackupSpec struct {
	Enabled     bool                 `json:"enabled"`
	Schedule    string               `json:"schedule,omitempty"`
	Retention   *BackupRetentionSpec `json:"retention,omitempty"`
	ObjectStore *ObjectStoreSpec     `json:"objectStore,omitempty"`
	PITRecovery *PITRecoverySpec     `json:"pitrecovery,omitempty"`
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
