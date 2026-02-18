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

package v4

// ConnectionPoolerMode defines the PgBouncer connection pooling strategy.
// +kubebuilder:validation:Enum=session;transaction;statement
type ConnectionPoolerMode string

const (
	// ConnectionPoolerModeSession assigns a connection for the entire client session (most compatible).
	ConnectionPoolerModeSession ConnectionPoolerMode = "session"

	// ConnectionPoolerModeTransaction returns the connection after each transaction (recommended).
	ConnectionPoolerModeTransaction ConnectionPoolerMode = "transaction"

	// ConnectionPoolerModeStatement returns the connection after each statement (limited compatibility).
	ConnectionPoolerModeStatement ConnectionPoolerMode = "statement"
)

// ConnectionPoolerConfig defines PgBouncer connection pooler configuration.
// When enabled, creates RW and RO pooler deployments for clusters using this class.
type ConnectionPoolerConfig struct {
	// Enabled controls whether PgBouncer connection pooling is deployed.
	// When true, creates RW and RO pooler deployments for the cluster.
	// +kubebuilder:default=false
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Instances is the number of PgBouncer pod replicas.
	// Higher values provide better availability and load distribution.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:default=3
	// +optional
	Instances *int32 `json:"instances,omitempty"`

	// Mode defines the connection pooling strategy.
	// +kubebuilder:default="transaction"
	// +optional
	Mode *ConnectionPoolerMode `json:"mode,omitempty"`

	// Config contains PgBouncer configuration parameters.
	// Passed directly to CNPG Pooler spec.pgbouncer.parameters.
	// See: https://cloudnative-pg.io/docs/1.28/connection_pooling/#pgbouncer-configuration-options
	// +optional
	Config map[string]string `json:"config,omitempty"`
}

// ConnectionPoolerEnable defines connection pooler settings available at the PostgresCluster level.
// Only the enabled toggle is exposed; all other pooler settings are controlled by the PostgresClusterClass.
type ConnectionPoolerEnable struct {
	// Enabled controls whether PgBouncer connection pooling is deployed for this cluster.
	// When set, takes precedence over the class-level connectionPooler.enabled value.
	// +kubebuilder:default=false
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}

// ConnectionPoolerStatus contains the observed state of the connection pooler.
type ConnectionPoolerStatus struct {
	// Enabled indicates whether pooler is active for this cluster.
	Enabled bool `json:"enabled"`
}
