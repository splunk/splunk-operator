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

// DatabaseRequirements provides provider-agnostic hints about database capacity needs.
// Providers translate these into concrete configurations (e.g., CNPG instance size, Aurora instance class).
type DatabaseRequirements struct {
	// MinMemory is a hint for memory allocation (e.g., "8Gi"). CNPG uses this for pod resources, Aurora for instance class selection.
	MinMemory string `json:"minMemory,omitempty"`

	// MinStorageGB is a hint for storage capacity. CNPG creates a PVC of this size, Aurora ignores it (unlimited storage).
	MinStorageGB int `json:"minStorageGB,omitempty"`

	// MinIOPS affects storage class selection (CNPG) or provisioned IOPS (Aurora).
	MinIOPS int `json:"minIOPS,omitempty"`

	// HighAvailability enables multi-instance/multi-AZ configurations.
	// CNPG: instances=3, Aurora: multi-AZ enabled
	HighAvailability bool `json:"highAvailability,omitempty"`
}

// BackupIntent expresses high-level backup requirements in a provider-agnostic way.
type BackupIntent struct {
	// Enabled controls whether backups are taken.
	Enabled bool `json:"enabled"`

	// RetentionDays specifies how long to keep backups (works for CNPG S3 backups and Aurora snapshots).
	RetentionDays int `json:"retentionDays,omitempty"`

	// Schedule is a cron expression for backup frequency (e.g., "0 2 * * *" for daily at 2am).
	Schedule string `json:"schedule,omitempty"`
}

// ProviderConfig holds provider-specific configuration. Only one provider field should be set.
type ProviderConfig struct {
	// Type identifies the database provider: "CNPG" | "Aurora" | "CloudSQL" | "External"
	Type string `json:"type"`

	// CNPG-specific configuration (for Kubernetes-native PostgreSQL via CloudNativePG operator)
	CNPG *CNPGProviderSpec `json:"cnpg,omitempty"`

	// Aurora-specific configuration (for AWS Aurora PostgreSQL)
	Aurora *AuroraProviderSpec `json:"aurora,omitempty"`

	// External-specific configuration (for pre-existing databases outside Kubernetes)
	External *ExternalProviderSpec `json:"external,omitempty"`
}

// CNPGProviderSpec contains CloudNativePG-specific configuration.
type CNPGProviderSpec struct {
	// Instances is the number of PostgreSQL pods (1 for dev, 3+ for HA).
	Instances int32 `json:"instances,omitempty"`

	// Storage configuration for persistent volumes.
	Storage StorageSpec `json:"storage"`

	// Resources defines CPU/memory requests and limits for PostgreSQL pods.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Pooler configuration for PgBouncer connection pooling.
	Pooler *PoolerSpec `json:"pooler,omitempty"`

	// Maintenance window for automated updates.
	Maintenance *MaintenanceSpec `json:"maintenance,omitempty"`

	// BootstrapInitDB configuration for database initialization.
	BootstrapInitDB *InitSpec `json:"bootstrapInitDB,omitempty"`

	// ObjectStore configuration for Barman backups to S3/GCS/Azure.
	ObjectStore *ObjectStoreSpec `json:"objectStore,omitempty"`

	// ServerCert configuration for PostgreSQL server TLS certificate.
	ServerCert *CertificateSpec `json:"serverCert,omitempty"`

	// ClientCert configuration for client/replication TLS certificate.
	ClientCert *CertificateSpec `json:"clientCert,omitempty"`

	// ImageName specifies the PostgreSQL container image (e.g., "ghcr.io/cloudnative-pg/postgresql:15.3").
	// If not specified, CNPG uses its default image catalog.
	ImageName string `json:"imageName,omitempty"`

	// ImageCatalogRef references a CNPG ClusterImageCatalog for PostgreSQL version management.
	// This allows specifying different PostgreSQL major versions.
	ImageCatalogRef *ImageCatalogRef `json:"imageCatalogRef,omitempty"`

	// ServiceAccountTemplate allows customization of the ServiceAccount created for the CNPG cluster.
	// This is particularly useful for configuring IAM Roles for Service Accounts (IRSA) in AWS EKS.
	ServiceAccountTemplate *ServiceAccountTemplate `json:"serviceAccountTemplate,omitempty"`
}

// CertificateSpec defines certificate configuration for TLS.
type CertificateSpec struct {
	// IssuerRef references a cert-manager Issuer/ClusterIssuer.
	IssuerRef *CertIssuerRef `json:"issuerRef,omitempty"`

	// SecretName for an existing TLS secret (alternative to cert-manager).
	SecretName string `json:"secretName,omitempty"`

	// CASecretName for an existing CA bundle secret.
	CASecretName string `json:"caSecretName,omitempty"`
}

// CertIssuerRef references a cert-manager issuer.
type CertIssuerRef struct {
	// Name of the Issuer/ClusterIssuer.
	Name string `json:"name"`

	// Kind is either "Issuer" or "ClusterIssuer".
	Kind string `json:"kind,omitempty"`

	// Group is the API group (defaults to "cert-manager.io").
	Group string `json:"group,omitempty"`
}

// ImageCatalogRef references a CNPG ClusterImageCatalog resource.
type ImageCatalogRef struct {
	// APIGroup is the API group of the ClusterImageCatalog (typically "postgresql.cnpg.io").
	APIGroup string `json:"apiGroup,omitempty"`

	// Kind is typically "ClusterImageCatalog" or "ImageCatalog".
	Kind string `json:"kind,omitempty"`

	// Name is the name of the ClusterImageCatalog resource.
	Name string `json:"name"`
}

// ServiceAccountTemplate allows customization of the ServiceAccount for CNPG clusters.
type ServiceAccountTemplate struct {
	// Metadata contains labels and annotations for the ServiceAccount.
	// Common use case: adding IRSA annotations for AWS EKS.
	// Example: annotations: {"eks.amazonaws.com/role-arn": "arn:aws:iam::123456789012:role/my-role"}
	Metadata ServiceAccountMetadata `json:"metadata,omitempty"`
}

// ServiceAccountMetadata holds metadata for ServiceAccount customization.
type ServiceAccountMetadata struct {
	// Labels to add to the ServiceAccount.
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations to add to the ServiceAccount.
	// For IRSA: {"eks.amazonaws.com/role-arn": "arn:aws:iam::ACCOUNT:role/ROLE"}
	Annotations map[string]string `json:"annotations,omitempty"`
}

// AuroraProviderSpec contains AWS Aurora-specific configuration.
type AuroraProviderSpec struct {
	// Mode determines how the Aurora cluster is managed: "Provision" | "Reference"
	// - Provision: Create a new Aurora cluster via Crossplane/ACK
	// - Reference: Connect to an existing Aurora cluster
	Mode string `json:"mode"`

	// ProvisionedCluster configuration (used when Mode=Provision).
	ProvisionedCluster *AuroraClusterConfig `json:"provisionedCluster,omitempty"`

	// ClusterIdentifier for existing Aurora cluster (used when Mode=Reference).
	ClusterIdentifier string `json:"clusterIdentifier,omitempty"`

	// Region is the AWS region where the Aurora cluster exists or will be created.
	Region string `json:"region,omitempty"`

	// AWSCredentialsSecret is a Kubernetes secret containing AWS credentials (accessKeyID, secretAccessKey).
	AWSCredentialsSecret string `json:"awsCredentialsSecret,omitempty"`

	// EndpointOverride allows specifying a custom endpoint (e.g., for PrivateLink).
	EndpointOverride string `json:"endpointOverride,omitempty"`
}

// AuroraClusterConfig contains configuration for provisioning a new Aurora cluster.
type AuroraClusterConfig struct {
	// InstanceClass specifies the compute/memory capacity (e.g., "db.r6g.large").
	InstanceClass string `json:"instanceClass"`

	// Engine is the database engine (e.g., "aurora-postgresql").
	Engine string `json:"engine,omitempty"`

	// EngineVersion is the PostgreSQL version (e.g., "15.3").
	EngineVersion string `json:"engineVersion,omitempty"`

	// MinCapacity for Aurora Serverless v2 (Aurora Capacity Units).
	MinCapacity int `json:"minCapacity,omitempty"`

	// MaxCapacity for Aurora Serverless v2 (Aurora Capacity Units).
	MaxCapacity int `json:"maxCapacity,omitempty"`

	// SubnetGroupName identifies the DB subnet group for network placement.
	SubnetGroupName string `json:"subnetGroupName,omitempty"`

	// VPCSecurityGroupIDs are security groups to apply to the cluster.
	VPCSecurityGroupIDs []string `json:"vpcSecurityGroupIDs,omitempty"`

	// EnableIAMAuth enables IAM database authentication.
	EnableIAMAuth bool `json:"enableIAMAuth,omitempty"`

	// StorageEncrypted enables encryption at rest.
	StorageEncrypted bool `json:"storageEncrypted,omitempty"`

	// KMSKeyID for encryption (optional, uses default AWS key if not specified).
	KMSKeyID string `json:"kmsKeyID,omitempty"`
}

// ExternalProviderSpec contains configuration for external (non-Kubernetes) databases.
type ExternalProviderSpec struct {
	// Endpoints are the database connection endpoints (e.g., ["postgres-primary.example.com:5432"]).
	Endpoints []string `json:"endpoints"`

	// CredentialsSecret is a Kubernetes secret containing username and password.
	CredentialsSecret string `json:"credentialsSecret"`

	// DatabaseName is the name of the database to connect to.
	DatabaseName string `json:"databaseName,omitempty"`

	// TLS configuration for secure connections.
	TLS *SSLSpec `json:"tls,omitempty"`
}
