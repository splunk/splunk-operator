package v4

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +kubebuilder:validation:Enum=Auto;ManagedRef;External
type DatabaseMode string

const (
	DatabaseModeAuto       DatabaseMode = "Auto"
	DatabaseModeManagedRef DatabaseMode = "ManagedRef"
	DatabaseModeExternal   DatabaseMode = "External"
)

type SearchHeadClusterDatabasePolicy struct {
	// +kubebuilder:validation:Enum=BlockRollout;WarnOnly
	RolloutGate string `json:"rolloutGate,omitempty"`
}

type SearchHeadClusterExternalDB struct {
	// Secret exposes: host,port,dbname,username,password,sslmode,(optional)sslrootcert
	ConnectionSecretRef LocalObjectReference `json:"connectionSecretRef"`
}

type LocalObjectReference struct {
	Name string `json:"name"`
}

type SearchHeadClusterDatabaseSpec struct {
	Mode              DatabaseMode                     `json:"mode,omitempty"`
	ManagedRef        *LocalObjectReference            `json:"managedRef,omitempty"`
	External          *SearchHeadClusterExternalDB     `json:"external,omitempty"`
	Policy            *SearchHeadClusterDatabasePolicy `json:"policy,omitempty"`
	DatabaseClassName string                           `json:"databaseClassName,omitempty"`
	AutoNameSuffix    string                           `json:"autoNameSuffix,omitempty"`
	// S3BackupBucket allows specifying a custom S3 bucket for database backups.
	// This overrides the default S3 bucket from the DatabaseClass.
	// Only used in Auto mode.
	S3BackupBucket string `json:"s3BackupBucket,omitempty"`
}

type DatabaseSummary struct {
	Provider   string       `json:"provider,omitempty"`
	Phase      string       `json:"phase,omitempty"`
	Endpoint   string       `json:"endpoint,omitempty"`
	Version    string       `json:"version,omitempty"`
	LastBackup *metav1.Time `json:"lastBackup,omitempty"`
}
