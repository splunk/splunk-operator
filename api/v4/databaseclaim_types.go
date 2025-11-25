package v4

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:validation:Enum=Pending;Bound;Lost
type ClaimPhase string

const (
	ClaimPhasePending ClaimPhase = "Pending"
	ClaimPhaseBound   ClaimPhase = "Bound"
	ClaimPhaseLost    ClaimPhase = "Lost"
)

// +kubebuilder:validation:Enum=Delete;Retain
type ReclaimPolicy string

const (
	ReclaimDelete ReclaimPolicy = "Delete"
	ReclaimRetain ReclaimPolicy = "Retain"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=dbclaim
// +kubebuilder:printcolumn:name="Class",type=string,JSONPath=.spec.className
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=.status.phase
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=.status.boundRef.name
type DatabaseClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DatabaseClaimSpec   `json:"spec,omitempty"`
	Status            DatabaseClaimStatus `json:"status,omitempty"`
}

type DatabaseClaimSpec struct {
	ClassName            string                  `json:"className"`
	Parameters           map[string]string       `json:"parameters,omitempty"` // optional overrides allowed by Class.Policy
	ConnectionSecretName string                  `json:"connectionSecretName,omitempty"`
	ReclaimPolicy        ReclaimPolicy           `json:"reclaimPolicy,omitempty"`
	Overrides            *DatabaseClaimOverrides `json:"overrides,omitempty"` // Overrides for specific configuration like S3 backup bucket
}

// DatabaseClaimOverrides allows per-claim customization of DatabaseClass settings.
// This enables simple APIs like SearchHeadCluster.spec.database.s3BackupBucket to override
// the default S3 configuration from the DatabaseClass.
type DatabaseClaimOverrides struct {
	BackupConfig *BackupOverride `json:"backupConfig,omitempty"`
}

// BackupOverride allows overriding backup configuration from the DatabaseClass.
type BackupOverride struct {
	Enabled *bool       `json:"enabled,omitempty"`
	S3      *S3Override `json:"s3,omitempty"`
}

// S3Override allows overriding S3 configuration for backups.
type S3Override struct {
	Bucket string `json:"bucket,omitempty"`
	Path   string `json:"path,omitempty"`
}

type LocalRef struct {
	Name string `json:"name"`
}

type DatabaseClaimStatus struct {
	Phase      ClaimPhase         `json:"phase,omitempty"`
	BoundRef   *LocalRef          `json:"boundRef,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
type DatabaseClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DatabaseClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DatabaseClaim{}, &DatabaseClaimList{})
}
