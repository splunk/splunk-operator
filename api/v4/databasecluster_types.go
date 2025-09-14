package v4

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Condition types
const (
	ConditionReady             = "Ready"
	ConditionSecretsReady      = "SecretsReady"
	ConditionCertificatesReady = "CertificatesReady"
	ConditionBackupHealthy     = "BackupHealthy"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=dbcl
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=.status.provider
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=.status.phase
// +kubebuilder:printcolumn:name="RW",type=string,JSONPath=.status.endpoints.readWrite
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=.status.version
type DatabaseCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DatabaseClusterSpec   `json:"spec,omitempty"`
	Status            DatabaseClusterStatus `json:"status,omitempty"`
}

type DatabaseClusterSpec struct {
	Engine     string          `json:"engine"`
	Version    string          `json:"version,omitempty"`
	ManagedBy  string          `json:"managedBy"`
	Connection *ConnectionSpec `json:"connection,omitempty"`

	CNPG     *CNPGSpec     `json:"cnpg,omitempty"`
	External *ExternalSpec `json:"external,omitempty"`

	Backup  *BackupSpec  `json:"backup,omitempty"`
	Init    *InitSpec    `json:"init,omitempty"`
	Network *NetworkSpec `json:"network,omitempty"`

	// +kubebuilder:validation:Enum=Delete;Retain
	ReclaimPolicy string `json:"reclaimPolicy,omitempty"`
}

type ExternalSpec struct {
	DSNSecretRef  string   `json:"dsnSecretRef,omitempty"`
	Host          string   `json:"host,omitempty"`
	Port          int32    `json:"port,omitempty"`
	DBName        string   `json:"dbName,omitempty"`
	UserSecretRef string   `json:"userSecretRef,omitempty"`
	SSL           *SSLSpec `json:"ssl,omitempty"`
}

type DatabaseEndpoints struct {
	ReadWrite string `json:"readWrite,omitempty"`
	ReadOnly  string `json:"readOnly,omitempty"`
	External  string `json:"external,omitempty"`
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

// +kubebuilder:object:root=true
type DatabaseClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DatabaseCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DatabaseCluster{}, &DatabaseClusterList{})
}
