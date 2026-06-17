/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package prometheus

import (
	"context"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
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
		"conflicts":  0,
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
		if cluster.Status.ConnectionPoolerStatus != nil && cluster.Status.ConnectionPoolerStatus.Enabled {
			poolerEnabledCount++
		}

		// Managed users. Desired includes owned roles plus distinct conflicted roles
		// withheld from ownership, so conflicts remain visible in fleet metrics.
		if cluster.Status.ManagedRolesStatus != nil {
			conflictRoles := make(map[string]struct{}, len(cluster.Status.ManagedRolesStatus.Conflicts))
			for _, conflict := range cluster.Status.ManagedRolesStatus.Conflicts {
				conflictRoles[conflict.Role] = struct{}{}
			}
			managedUserStates["desired"] += float64(len(cluster.Status.ManagedRolesStatus.RoleOwners) + len(conflictRoles))
			managedUserStates["conflicts"] += float64(len(cluster.Status.ManagedRolesStatus.Conflicts))
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
