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
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const fleetCollectionInterval = 30 * time.Second

// FleetCollector periodically recomputes fleet-state gauges from the manager cache.
type FleetCollector struct {
	client   client.Client
	recorder ports.Recorder
	clock    clock.WithTicker
	interval time.Duration
}

// NewFleetCollector returns a new FleetCollector.
func NewFleetCollector(c client.Client, recorder ports.Recorder) *FleetCollector {
	return newFleetCollector(c, recorder, clock.RealClock{}, fleetCollectionInterval)
}

func newFleetCollector(c client.Client, recorder ports.Recorder, clock clock.WithTicker, interval time.Duration) *FleetCollector {
	return &FleetCollector{client: c, recorder: recorder, clock: clock, interval: interval}
}

// Start collects immediately, then refreshes the gauges without overlapping cycles.
func (fc *FleetCollector) Start(ctx context.Context) error {
	fc.collect(ctx)
	if ctx.Err() != nil {
		return nil
	}

	ticker := fc.clock.NewTicker(fc.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C():
			fc.collect(ctx)
		}
	}
}

// NeedLeaderElection makes one active manager responsible for fleet gauges.
func (*FleetCollector) NeedLeaderElection() bool {
	return true
}

func (fc *FleetCollector) collect(ctx context.Context) {
	logger := log.FromContext(ctx)
	if err := fc.collectClusterMetrics(ctx); err != nil && ctx.Err() == nil {
		logger.Error(err, "Failed to collect PostgresCluster fleet metrics")
	}
	if ctx.Err() != nil {
		return
	}
	if err := fc.collectDatabaseMetrics(ctx); err != nil && ctx.Err() == nil {
		logger.Error(err, "Failed to collect PostgresDatabase fleet metrics")
	}
}

func (fc *FleetCollector) collectClusterMetrics(ctx context.Context) error {
	var list platformv1alpha1.PostgresClusterList
	if err := fc.client.List(ctx, &list); err != nil {
		return err
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

	fc.recorder.SetClusterPhases(phases)
	fc.recorder.SetPoolerEnabledClusters(poolerEnabledCount)
	fc.recorder.SetManagedUsers(ports.ControllerCluster, managedUserStates)
	return nil
}

func (fc *FleetCollector) collectDatabaseMetrics(ctx context.Context) error {
	var list platformv1alpha1.PostgresDatabaseList
	if err := fc.client.List(ctx, &list); err != nil {
		return err
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

	fc.recorder.SetDatabasePhases(phases)
	return nil
}
