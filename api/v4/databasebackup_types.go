package v4

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed
type BackupPhase string

const (
	BackupPending   BackupPhase = "Pending"
	BackupRunning   BackupPhase = "Running"
	BackupSucceeded BackupPhase = "Succeeded"
	BackupFailed    BackupPhase = "Failed"
)

type DatabaseBackupSpec struct {
	// Target DatabaseCluster to back up
	TargetRef LocalRef `json:"targetRef"`
	// Optional label to tag the underlying CNPG Backup
	BackupLabel string `json:"backupLabel,omitempty"`
	// If true, create the backup immediately (default true)
	Immediate *bool `json:"immediate,omitempty"`
	// Optional: override object store from the cluster defaults
	// (kept simple; advanced overrides can be added later)
}

type DatabaseBackupStatus struct {
	Phase         BackupPhase        `json:"phase,omitempty"`
	CNPGBackupRef *LocalRef          `json:"cnpgBackupRef,omitempty"` // name of postgresql.cnpg.io Backup
	BackupID      string             `json:"backupId,omitempty"`
	CompletedAt   *metav1.Time       `json:"completedAt,omitempty"`
	Message       string             `json:"message,omitempty"`
	Conditions    []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=dbbkp
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=.spec.targetRef.name
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=.status.phase
// +kubebuilder:printcolumn:name="CNPG",type=string,JSONPath=.status.cnpgBackupRef.name
// +kubebuilder:storageversion
type DatabaseBackup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DatabaseBackupSpec   `json:"spec,omitempty"`
	Status            DatabaseBackupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type DatabaseBackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DatabaseBackup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DatabaseBackup{}, &DatabaseBackupList{})
}
