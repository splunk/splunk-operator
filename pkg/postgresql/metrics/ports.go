package metrics

// Result labels for counters that track success/error outcomes.
const (
	ResultSuccess = "success"
	ResultError   = "error"
)

// Controller name labels.
const (
	ControllerCluster  = "postgrescluster"
	ControllerDatabase = "postgresdatabase"
)

// Validation failure reason labels.
const (
	ReasonClassNotFound      = "class_not_found"
	ReasonInvalidConfig      = "invalid_configuration"
	ReasonClusterNotFound    = "cluster_not_found"
	ReasonClusterNotReady    = "cluster_not_ready"
	ReasonRoleConflict       = "role_conflict"
	ReasonPoolerConfigMissing = "pooler_config_missing"
)

// User action labels.
const (
	ActionSecretReconcile    = "secret_reconcile"
	ActionConfigMapReconcile = "configmap_reconcile"
	ActionRolePatch          = "role_patch"
	ActionDatabaseReconcile  = "database_reconcile"
	ActionPrivilegeGrant     = "privilege_grant"
)

// Owned resource operation labels.
const (
	OpCreate = "create"
	OpUpdate = "update"
)

// Owned resource kind labels.
const (
	ResourceSecret    = "Secret"
	ResourceCluster   = "Cluster"
	ResourcePooler    = "Pooler"
	ResourceConfigMap = "ConfigMap"
	ResourceDatabase  = "Database"
)

// Recorder is the port for all PostgreSQL controller metrics.
// Core service packages depend on this interface, never on Prometheus directly.
// Adapters (PrometheusRecorder, NoopRecorder) live in this package.
//
// Reconcile-level metrics (total count, duration, error count) are handled
// automatically by controller-runtime — see controller_runtime_reconcile_total,
// controller_runtime_reconcile_time_seconds, controller_runtime_reconcile_errors_total.
// This interface covers domain-specific metrics only.
type Recorder interface {
	// IncValidationFailure records a validation or configuration failure.
	IncValidationFailure(controller string, reason string)

	// SetClusterPhases sets gauge values for cluster counts by phase.
	// The phases map keys are phase strings (Ready, Pending, etc.) with counts as values.
	SetClusterPhases(phases map[string]float64, poolerEnabledCount float64)

	// SetDatabasePhases sets gauge values for database counts by phase.
	SetDatabasePhases(phases map[string]float64)

	// SetManagedUsers sets the gauge for managed user states.
	// The states map keys are state strings (desired, reconciled, pending, failed).
	SetManagedUsers(controller string, states map[string]float64)

	// IncUserAction increments the user action counter.
	IncUserAction(action string, result string)

	// SetPoolers sets pooler gauge values by type and state.
	SetPoolers(poolerType string, state string, count float64)

	// SetPoolerInstances sets pooler instance gauge by type.
	SetPoolerInstances(poolerType string, count float64)

	// IncFinalizerOp increments the finalizer operations counter.
	IncFinalizerOp(controller string, result string)

	// IncOwnedResourceOp increments the owned resource operations counter.
	IncOwnedResourceOp(controller string, resourceKind string, operation string, result string)
}
