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
	ManagedBy string       `json:"managedBy"` // CNPG|External|AWSAurora (future)
	Version   string       `json:"version,omitempty"`
	CNPG      *CNPGSpec    `json:"cnpg,omitempty"`
	Backup    *BackupSpec  `json:"backup,omitempty"`
	Policy    *ClassPolicy `json:"policy,omitempty"`
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
