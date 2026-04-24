package prometheus

import (
	"context"

	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// FleetCollector recomputes fleet-state gauges from the K8s API (informer cache).
type FleetCollector struct{}

// NewFleetCollector returns a new FleetCollector.
func NewFleetCollector() *FleetCollector {
	return &FleetCollector{}
}

// CollectClusterMetrics lists all PostgresCluster resources and updates phase
// gauges, pooler gauges, and managed-user gauges.
func (fc *FleetCollector) CollectClusterMetrics(ctx context.Context, c client.Client, recorder ports.Recorder) {
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

	recorder.SetClusterPhases(phases)
	recorder.SetPoolerEnabledClusters(poolerEnabledCount)
	recorder.SetManagedUsers(ports.ControllerCluster, managedUserStates)
}

// CollectDatabaseMetrics lists all PostgresDatabase resources and updates
// phase gauges.
func (fc *FleetCollector) CollectDatabaseMetrics(ctx context.Context, c client.Client, recorder ports.Recorder) {
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
