package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
)

var (
	statusTransitionsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "splunk_operator_postgres_status_transitions_total",
		Help: "Status condition transitions by controller, condition type, status, and reason.",
	}, []string{"controller", "condition", "status", "reason"})

	clusters = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "splunk_operator_postgres_clusters",
		Help: "Current number of PostgresCluster resources by status phase.",
	}, []string{"phase"})

	poolerEnabledClusters = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "splunk_operator_postgres_clusters_pooler_enabled",
		Help: "Current number of PostgresCluster resources with connection pooling enabled.",
	})

	databases = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "splunk_operator_postgres_databases",
		Help: "Current number of PostgresDatabase resources by status phase.",
	}, []string{"phase"})

	managedUsers = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "splunk_operator_postgres_managed_users",
		Help: "Current counts of managed users by state.",
	}, []string{"controller", "state"})

	allCollectors = []prometheus.Collector{
		statusTransitionsTotal,
		clusters,
		poolerEnabledClusters,
		databases,
		managedUsers,
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

// PrometheusRecorder implements shared.Recorder using Prometheus client_golang.
type PrometheusRecorder struct{}

// NewPrometheusRecorder returns a new PrometheusRecorder.
func NewPrometheusRecorder() *PrometheusRecorder {
	return &PrometheusRecorder{}
}

func (p *PrometheusRecorder) IncStatusTransition(controller, condition, status, reason string) {
	statusTransitionsTotal.WithLabelValues(controller, condition, status, reason).Inc()
}

func (p *PrometheusRecorder) SetClusterPhases(phases map[string]float64) {
	clusters.Reset() // drop stale label combinations before re-populating
	for phase, count := range phases {
		clusters.WithLabelValues(phase).Set(count)
	}
}

func (p *PrometheusRecorder) SetPoolerEnabledClusters(count float64) {
	poolerEnabledClusters.Set(count)
}

func (p *PrometheusRecorder) SetDatabasePhases(phases map[string]float64) {
	databases.Reset() // drop stale label combinations before re-populating
	for phase, count := range phases {
		databases.WithLabelValues(phase).Set(count)
	}
}

func (p *PrometheusRecorder) SetManagedUsers(controller string, states map[string]float64) {
	managedUsers.Reset() // drop stale label combinations before re-populating
	for state, count := range states {
		managedUsers.WithLabelValues(controller, state).Set(count)
	}
}

// Compile-time interface check.
var _ ports.Recorder = (*PrometheusRecorder)(nil)
