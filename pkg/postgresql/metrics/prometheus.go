package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	reconcileTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "splunk_operator_postgres_reconcile_total",
		Help: "Total reconcile attempts for PostgreSQL controllers.",
	}, []string{"controller", "result"})

	reconcileDurationSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_postgres_reconcile_duration_seconds",
		Help:    "End-to-end reconcile duration for PostgreSQL controllers.",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 15, 30},
	}, []string{"controller", "result"})

	reconcileErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "splunk_operator_postgres_reconcile_errors_total",
		Help: "Reconcile failures grouped by stable error class.",
	}, []string{"controller", "error_class"})

	reconcileRequeuesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "splunk_operator_postgres_reconcile_requeues_total",
		Help: "Requeues caused by waiting, conflicts, or dependency state.",
	}, []string{"controller", "reason"})

	validationFailuresTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "splunk_operator_postgres_validation_failures_total",
		Help: "Validation and configuration failures.",
	}, []string{"controller", "reason"})

	clusters = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "splunk_operator_postgres_clusters",
		Help: "Current number of PostgresCluster resources by status phase.",
	}, []string{"phase", "pooler_enabled"})

	databases = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "splunk_operator_postgres_databases",
		Help: "Current number of PostgresDatabase resources by status phase.",
	}, []string{"phase"})

	managedUsers = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "splunk_operator_postgres_managed_users",
		Help: "Current counts of managed users by state.",
	}, []string{"controller", "state"})

	userActionsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "splunk_operator_postgres_user_actions_total",
		Help: "User-management actions such as secret reconcile, role patch, privilege grant.",
	}, []string{"action", "result"})

	poolers = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "splunk_operator_postgres_poolers",
		Help: "Current number of PgBouncer poolers by type and readiness state.",
	}, []string{"type", "state"})

	poolerInstances = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "splunk_operator_postgres_pooler_instances",
		Help: "Current observed pooler instance count.",
	}, []string{"type"})

	finalizerOperationsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "splunk_operator_postgres_finalizer_operations_total",
		Help: "Finalizer success and cleanup failures.",
	}, []string{"controller", "result"})

	ownedResourceOperationsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "splunk_operator_postgres_owned_resource_operations_total",
		Help: "Create/update/delete outcomes for owned resources.",
	}, []string{"controller", "resource_kind", "operation", "result"})

	allCollectors = []prometheus.Collector{
		reconcileTotal,
		reconcileDurationSeconds,
		reconcileErrorsTotal,
		reconcileRequeuesTotal,
		validationFailuresTotal,
		clusters,
		databases,
		managedUsers,
		userActionsTotal,
		poolers,
		poolerInstances,
		finalizerOperationsTotal,
		ownedResourceOperationsTotal,
	}
)

// Register registers all PostgreSQL metrics with the given registerer.
// Call once at startup from cmd/main.go.
func Register(registerer prometheus.Registerer) error {
	for _, c := range allCollectors {
		if err := registerer.Register(c); err != nil {
			return err
		}
	}
	return nil
}

// PrometheusRecorder implements Recorder using Prometheus client_golang.
type PrometheusRecorder struct{}

// NewPrometheusRecorder returns a new PrometheusRecorder.
func NewPrometheusRecorder() *PrometheusRecorder {
	return &PrometheusRecorder{}
}

func (p *PrometheusRecorder) ObserveReconcile(controller string, result string, duration time.Duration) {
	reconcileTotal.WithLabelValues(controller, result).Inc()
	reconcileDurationSeconds.WithLabelValues(controller, result).Observe(duration.Seconds())
}

func (p *PrometheusRecorder) IncReconcileError(controller string, errorClass string) {
	reconcileErrorsTotal.WithLabelValues(controller, errorClass).Inc()
}

func (p *PrometheusRecorder) IncRequeue(controller string, reason string) {
	reconcileRequeuesTotal.WithLabelValues(controller, reason).Inc()
}

func (p *PrometheusRecorder) IncValidationFailure(controller string, reason string) {
	validationFailuresTotal.WithLabelValues(controller, reason).Inc()
}

func (p *PrometheusRecorder) SetClusterPhases(phases map[string]float64, poolerEnabledCount float64) {
	// Reset all phase gauges before setting new values to avoid stale entries.
	clusters.Reset()
	for phase, count := range phases {
		clusters.WithLabelValues(phase, "false").Set(count)
	}
	// pooler_enabled count is tracked separately — it's a cross-cutting dimension.
	if poolerEnabledCount > 0 {
		clusters.WithLabelValues("", "true").Set(poolerEnabledCount)
	}
}

func (p *PrometheusRecorder) SetDatabasePhases(phases map[string]float64) {
	databases.Reset()
	for phase, count := range phases {
		databases.WithLabelValues(phase).Set(count)
	}
}

func (p *PrometheusRecorder) SetManagedUsers(controller string, states map[string]float64) {
	for state, count := range states {
		managedUsers.WithLabelValues(controller, state).Set(count)
	}
}

func (p *PrometheusRecorder) IncUserAction(action string, result string) {
	userActionsTotal.WithLabelValues(action, result).Inc()
}

func (p *PrometheusRecorder) SetPoolers(poolerType string, state string, count float64) {
	poolers.WithLabelValues(poolerType, state).Set(count)
}

func (p *PrometheusRecorder) SetPoolerInstances(poolerType string, count float64) {
	poolerInstances.WithLabelValues(poolerType).Set(count)
}

func (p *PrometheusRecorder) IncFinalizerOp(controller string, result string) {
	finalizerOperationsTotal.WithLabelValues(controller, result).Inc()
}

func (p *PrometheusRecorder) IncOwnedResourceOp(controller string, resourceKind string, operation string, result string) {
	ownedResourceOperationsTotal.WithLabelValues(controller, resourceKind, operation, result).Inc()
}

// Compile-time interface check.
var _ Recorder = (*PrometheusRecorder)(nil)
