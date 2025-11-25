package provider

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v4 "github.com/splunk/splunk-operator/api/v4"
)

// ReconcileResult contains the outcome of a provider's Ensure operation
type ReconcileResult struct {
	// Ready indicates whether the database is ready to accept connections
	Ready bool

	// ConnectionSecretData contains the normalized connection information
	// Required keys: host, port, dbname, username, password, sslmode
	ConnectionSecretData map[string][]byte

	// Endpoints contains service endpoints for read-write and read-only connections
	Endpoints Endpoints

	// Version is the actual database version running
	Version string

	// RequeueAfter indicates when to reconcile again (0 = no requeue)
	RequeueAfter time.Duration
}

// Endpoints contains database connection endpoints
type Endpoints struct {
	ReadWrite string
	ReadOnly  string
	External  string
}

// Status contains high-level status information
type Status struct {
	Phase      string // "Provisioning" | "Ready" | "Degraded" | "Failed"
	Message    string
	Conditions []metav1.Condition
}

// BackupStatus contains backup plan status
type BackupStatus struct {
	LastBackupTime *metav1.Time
	NextBackupTime *metav1.Time
	BackupCount    int
	Healthy        bool
	Message        string
}

// Provider defines the interface that all database providers must implement
type Provider interface {
	// Name returns the provider name (e.g., "CNPG", "Aurora")
	Name() string

	// Ensure creates or updates the database cluster and returns normalized connection data
	Ensure(ctx context.Context, db *v4.DatabaseCluster) (ReconcileResult, error)

	// Delete removes the database cluster (if the provider manages lifecycle)
	Delete(ctx context.Context, db *v4.DatabaseCluster) error

	// GetStatus returns the current status of the database cluster
	GetStatus(ctx context.Context, db *v4.DatabaseCluster) (Status, error)

	// TriggerBackup initiates an ad-hoc backup and returns the backup identifier
	TriggerBackup(ctx context.Context, backup *v4.DatabaseBackup, src *v4.DatabaseCluster) (string, error)

	// GetBackupStatus retrieves the status of a backup
	GetBackupStatus(ctx context.Context, ns, backupID string) (phase string, completed *metav1.Time, message string, err error)

	// RestoreBackup creates a new cluster from a backup
	RestoreBackup(ctx context.Context, src *v4.DatabaseCluster, restore *v4.DatabaseRestore) error

	// EnsurePooler creates or updates a connection pooler
	EnsurePooler(ctx context.Context, pooler *v4.DatabasePooler) (serviceName string, ready bool, err error)
}
