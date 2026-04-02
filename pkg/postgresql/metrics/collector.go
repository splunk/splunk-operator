package metrics

import (
	"context"
	"sync"
	"time"

	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const fleetCollectInterval = 2 * time.Second

// FleetCollector recomputes fleet-state gauges from the K8s API (informer cache).
// It is rate-limited to avoid redundant work during burst reconciles.
// Each resource type has its own timestamp so they don't starve each other.
type FleetCollector struct {
	mu               sync.Mutex
	lastClusterCollect  time.Time
	lastDatabaseCollect time.Time
}

// NewFleetCollector returns a new FleetCollector.
func NewFleetCollector() *FleetCollector {
	return &FleetCollector{}
}

// CollectClusterMetrics lists all PostgresCluster resources and updates phase
// gauges, pooler gauges, and managed-user gauges. Skips if called within 2s
// of the last collection.
func (fc *FleetCollector) CollectClusterMetrics(ctx context.Context, c client.Client, recorder Recorder) {
	if !fc.shouldCollectCluster() {
		return
	}

	logger := log.FromContext(ctx)

	var list enterprisev4.PostgresClusterList
	if err := c.List(ctx, &list); err != nil {
		logger.Error(err, "Failed to list PostgresClusters for fleet metrics")
		return
	}

	phases := make(map[string]float64)
	var poolerEnabledCount float64
	managedUserStates := map[string]float64{
		"desired":    0,
		"reconciled": 0,
		"pending":    0,
		"failed":     0,
	}

	for i := range list.Items {
		cluster := &list.Items[i]

		// Phase gauge.
		phase := "Unknown"
		if cluster.Status.Phase != nil {
			phase = *cluster.Status.Phase
		}
		phases[phase]++

		// Pooler-enabled count.
		if cluster.Spec.ConnectionPoolerEnabled != nil && *cluster.Spec.ConnectionPoolerEnabled {
			poolerEnabledCount++
		}

		// Managed users.
		managedUserStates["desired"] += float64(len(cluster.Spec.ManagedRoles))
		if cluster.Status.ManagedRolesStatus != nil {
			managedUserStates["reconciled"] += float64(len(cluster.Status.ManagedRolesStatus.Reconciled))
			managedUserStates["pending"] += float64(len(cluster.Status.ManagedRolesStatus.Pending))
			managedUserStates["failed"] += float64(len(cluster.Status.ManagedRolesStatus.Failed))
		}
	}

	recorder.SetClusterPhases(phases, poolerEnabledCount)
	recorder.SetManagedUsers(ControllerCluster, managedUserStates)
}

// CollectDatabaseMetrics lists all PostgresDatabase resources and updates
// phase gauges. Skips if called within 2s of the last collection.
func (fc *FleetCollector) CollectDatabaseMetrics(ctx context.Context, c client.Client, recorder Recorder) {
	if !fc.shouldCollectDatabase() {
		return
	}

	logger := log.FromContext(ctx)

	var list enterprisev4.PostgresDatabaseList
	if err := c.List(ctx, &list); err != nil {
		logger.Error(err, "Failed to list PostgresDatabases for fleet metrics")
		return
	}

	phases := make(map[string]float64)
	for i := range list.Items {
		db := &list.Items[i]
		phase := "Unknown"
		if db.Status.Phase != nil {
			phase = *db.Status.Phase
		}
		phases[phase]++
	}

	recorder.SetDatabasePhases(phases)
}

func (fc *FleetCollector) shouldCollectCluster() bool {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	now := time.Now()
	if now.Sub(fc.lastClusterCollect) < fleetCollectInterval {
		return false
	}
	fc.lastClusterCollect = now
	return true
}

func (fc *FleetCollector) shouldCollectDatabase() bool {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	now := time.Now()
	if now.Sub(fc.lastDatabaseCollect) < fleetCollectInterval {
		return false
	}
	fc.lastDatabaseCollect = now
	return true
}
