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
package ports

// Controller name labels.
const (
	ControllerCluster  = "postgrescluster"
	ControllerDatabase = "postgresdatabase"
)

// Recorder is the port for all PostgreSQL controller metrics.
// Core service packages depend on this interface, never on Prometheus directly.
//
// Reconcile-level metrics (total count, duration, error count) are handled
// automatically by controller-runtime — see controller_runtime_reconcile_total,
// controller_runtime_reconcile_time_seconds, controller_runtime_reconcile_errors_total.
//
// Domain-specific business metrics are emitted automatically via IncStatusTransition
// every time a status condition is written. Fleet-level gauges are populated by the
// collector on each reconcile.
type Recorder interface {
	// IncStatusTransition increments the status transition counter.
	// Called automatically by persistStatus/setStatus — no manual calls needed in service code.
	IncStatusTransition(controller, condition, status, reason string)

	// SetClusterPhases sets gauge values for cluster counts by phase.
	SetClusterPhases(phases map[string]float64)

	// SetPoolerEnabledClusters sets the gauge for clusters with connection pooling enabled.
	SetPoolerEnabledClusters(count float64)

	// SetDatabasePhases sets gauge values for database counts by phase.
	SetDatabasePhases(phases map[string]float64)

	// SetManagedUsers sets the gauge for managed user states.
	SetManagedUsers(controller string, states map[string]float64)
}
