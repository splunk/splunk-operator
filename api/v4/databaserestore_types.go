package v4

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:validation:Enum=NewCluster;InPlace
type RestoreMode string

const (
	RestoreNewCluster RestoreMode = "NewCluster"
	RestoreInPlace    RestoreMode = "InPlace" // WARNING: not recommended; we will reject in controller for now
)

// +kubebuilder:validation:Enum=Pending;Running;Completed;Failed
type RestorePhase string

const (
	RestorePending   RestorePhase = "Pending"
	RestoreRunning   RestorePhase = "Running"
	RestoreCompleted RestorePhase = "Completed"
	RestoreFailed    RestorePhase = "Failed"
)

type RestoreFrom struct {
	// Either reference a DatabaseBackup resource…
	DatabaseBackupRef *LocalRef `json:"databaseBackupRef,omitempty"`
	// …or reference a CNPG Backup name directly
	CNPGBackupName string `json:"cnpgBackupName,omitempty"`
	// (Future) PITR: add fields {targetTime,targetLSN} etc.
}

type NewClusterSpec struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	// Minimal knobs to stand up storage; rest inherits from source cluster defaults
	Instances        *int32 `json:"instances,omitempty"`
	StorageClassName string `json:"storageClassName,omitempty"`
	StorageSize      string `json:"storageSize,omitempty"`
}

type DatabaseRestoreSpec struct {
	// Source database cluster we’re restoring FROM (for object store & defaults)
	SourceClusterRef LocalRef    `json:"sourceClusterRef"`
	Mode             RestoreMode `json:"mode"`
	From             RestoreFrom `json:"from"`

	// Only for NewCluster mode
	NewCluster *NewClusterSpec `json:"newCluster,omitempty"`
}

type DatabaseRestoreStatus struct {
	Phase       RestorePhase       `json:"phase,omitempty"`
	RestoredRef *LocalRef          `json:"restoredRef,omitempty"` // the new DatabaseCluster (or CNPG Cluster) name
	Message     string             `json:"message,omitempty"`
	Conditions  []metav1.Condition `json:"conditions,omitempty"`
	StartedAt   *metav1.Time       `json:"startedAt,omitempty"`
	CompletedAt *metav1.Time       `json:"completedAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=dbrestore
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=.spec.mode
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=.status.phase
// +kubebuilder:storageversion
type DatabaseRestore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DatabaseRestoreSpec   `json:"spec,omitempty"`
	Status            DatabaseRestoreStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type DatabaseRestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DatabaseRestore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DatabaseRestore{}, &DatabaseRestoreList{})
}
