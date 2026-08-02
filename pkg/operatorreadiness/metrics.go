package operatorreadiness

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const metricLabelCheck = "check"

var (
	managerReadinessStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "splunk_operator_manager_readiness_status",
			Help: "Whether each prerequisite for Operator reconciliation participation is ready (1) or not ready (0).",
		},
		[]string{metricLabelCheck},
	)
	managerReadinessTransitions = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "splunk_operator_manager_readiness_transitions_total",
			Help: "The number of Operator reconciliation-readiness result transitions.",
		},
		[]string{"state", "reason"},
	)
	managerReadinessLastTransition = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "splunk_operator_manager_readiness_last_transition_timestamp_seconds",
			Help: "Unix timestamp of the most recent Operator reconciliation-readiness result transition.",
		},
	)
)

type telemetryRecorder interface {
	SetCheck(name string, ready bool)
	RecordTransition(ready bool, reason string)
}

type prometheusTelemetry struct{}

func (prometheusTelemetry) SetCheck(name string, ready bool) {
	value := 0.0
	if ready {
		value = 1
	}
	managerReadinessStatus.WithLabelValues(name).Set(value)
}

func (prometheusTelemetry) RecordTransition(ready bool, reason string) {
	state := "not_ready"
	if ready {
		state = "ready"
	}
	managerReadinessTransitions.WithLabelValues(state, reason).Inc()
	managerReadinessLastTransition.Set(float64(time.Now().Unix()))
}

func init() {
	crmetrics.Registry.MustRegister(
		managerReadinessStatus,
		managerReadinessTransitions,
		managerReadinessLastTransition,
	)
}
