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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
)

var (
	statusTransitionsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "splunk_operator_postgres_status_transitions_total",
		Help: "Status condition transitions by controller, condition type, status, and reason.",
	}, []string{"controller", "condition", "status", "reason"})

	provisioningDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_postgres_provisioning_duration_seconds",
		Help:    "Wall-clock time from PostgreSQL resource creation or readiness-affecting operation start to Ready phase.",
		Buckets: []float64{5, 15, 30, 60, 120, 300, 600, 900, 1800},
	}, []string{"controller"})

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
		provisioningDuration,
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

func (p *PrometheusRecorder) ObserveProvisioningDuration(controller string, seconds float64) {
	provisioningDuration.WithLabelValues(controller).Observe(seconds)
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
