package v4

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=dbclass
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=.spec.managedBy
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=.spec.version
type DatabaseClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DatabaseClassSpec `json:"spec,omitempty"`
}

type ClassPolicy struct {
	TLSRequired          bool     `json:"tlsRequired,omitempty"`
	AllowInlineOverrides []string `json:"allowInlineOverrides,omitempty"`
}

type DatabaseClassSpec struct {
	// Engine specifies the database engine (e.g., "Postgres", "MySQL")
	Engine string `json:"engine,omitempty"`

	// Provider configuration (CNPG, Aurora, CloudSQL, External)
	Provider ProviderConfig `json:"provider"`

	// Requirements are provider-agnostic capacity hints
	Requirements *DatabaseRequirements `json:"requirements,omitempty"`

	// Backup intent for this class
	Backup *BackupIntent `json:"backup,omitempty"`

	// Policy controls what claim parameters can override
	Policy *ClassPolicy `json:"policy,omitempty"`
}

// +kubebuilder:object:root=true
type DatabaseClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DatabaseClass `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DatabaseClass{}, &DatabaseClassList{})
}
