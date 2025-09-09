package provider

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dbv1 "github.com/splunk/splunk-operator/api/database/v1alpha1"
)

type ReconcileResult struct {
	Ready               bool
	ConnectionSecretName string
	RWEndpoint          string
	ROEndpoint          string
	Version             string
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
	Ensure(ctx context.Context, db *dbv1.DatabaseCluster) (ReconcileResult, error)
	Delete(ctx context.Context, db *dbv1.DatabaseCluster) error
	EnsureBackupPlan(ctx context.Context, db *dbv1.DatabaseCluster) (BackupStatus, error)
	TriggerAdhocBackup(ctx context.Context, db *dbv1.DatabaseCluster) error
	Restore(ctx context.Context, db *dbv1.DatabaseCluster, spec RestoreSpec) error
	ValidateConnectivity(ctx context.Context, db *dbv1.DatabaseCluster) error
}
