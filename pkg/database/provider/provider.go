package provider

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v4 "github.com/splunk/splunk-operator/api/v4"
)

type ReconcileResult struct {
	Ready                bool
	ConnectionSecretName string
	RWEndpoint           string
	ROEndpoint           string
	Version              string
}

type BackupStatus struct {
	LastBackupTime *metav1.Time
	Healthy        bool
	Message        string
}

type RestoreSpec struct {
	FromBackupRef *string
	PointInTime   *metav1.Time
}

type Provider interface {
	Name() string
	Ensure(ctx context.Context, db *v4.DatabaseCluster) (ReconcileResult, error)
	EnsureBackupPlan(ctx context.Context, db *v4.DatabaseCluster) (BackupStatus, error)
	TriggerAdhocBackup(ctx context.Context, db *v4.DatabaseBackup, src *v4.DatabaseCluster) (string, error)
	Restore(ctx context.Context, db *v4.DatabaseCluster, spec RestoreSpec) error
	ValidateConnectivity(ctx context.Context, db *v4.DatabaseCluster) error
}
