package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	LabelNamespace       = "namespace"
	LabelName            = "name"
	LabelKind            = "kind"
	LabelErrorType       = "error_type"
	LabelMethodName      = "api"
	LabelModuleName      = "module"
	LabelResourceVersion = "resource_version"
)

var (
	upgradeStartTimestamp int64
	upgradeEndTimestamp   int64
)

var ReconcileCounters = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "splunk_operator_reconcile_total",
	Help: "The number of times reconciled by this controller",
}, []string{LabelNamespace, LabelName, LabelKind})

var ReconcileErrorCounter = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_reconcile_error_total",
	Help: "The number of times the operator has failed to reconcile",
})

var ActionFailureCounters = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "splunk_operator_error_total",
	Help: "The number of times operator has entered an error state",
}, []string{LabelErrorType})

var ApiTotalTimeMetricEvents = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "splunk_operator_module_duration_in_milliseconds",
	Help: "The time it takes to complete each call in standalone (in milliseconds)",
}, []string{LabelNamespace, LabelName, LabelKind, LabelModuleName, LabelMethodName})

var (
	UpgradeStartTime = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "splunk_upgrade_start_time",
		Help: "Unix timestamp when the SHC upgrade started",
	})
	UpgradeEndTime = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "splunk_upgrade_end_time",
		Help: "Unix timestamp when the SHC upgrade ended",
	},
	)
	ShortSearchSuccessCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "splunk_short_search_success_total",
			Help: "Total number of successful short searches per search head",
		},
		[]string{"sh_name"},
	)
	ShortSearchFailureCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "splunk_short_search_failure_total",
			Help: "Total number of failed short searches per search head",
		},
		[]string{"sh_name"},
	)
	TotalSearchSuccessCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "splunk_total_search_success_total",
			Help: "Total number of successful total searches per search head",
		},
		[]string{"sh_name"},
	)
	TotalSearchFailureCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "splunk_total_search_failure_total",
			Help: "Total number of failed total searches per search head",
		},
		[]string{"sh_name"},
	)
)

func GetPrometheusLabels(request reconcile.Request, kind string) prometheus.Labels {
	return prometheus.Labels{
		LabelNamespace: request.Namespace,
		LabelName:      request.Name,
		LabelKind:      kind,
	}
}

func RecordUpgradeStartTime() {
	upgradeStartTimestamp = time.Now().Unix()
	UpgradeStartTime.Set(float64(upgradeStartTimestamp))
}

func RecordUpgradeEndTime() {
	upgradeEndTimestamp = time.Now().Unix()
	UpgradeEndTime.Set(float64(upgradeEndTimestamp))
}

func init() {
	metrics.Registry.MustRegister(
		ReconcileCounters,
		ReconcileErrorCounter,
		ActionFailureCounters,
		ApiTotalTimeMetricEvents,
		UpgradeStartTime,
		UpgradeEndTime,
		ShortSearchSuccessCounter,
		ShortSearchFailureCounter,
		TotalSearchSuccessCounter,
		TotalSearchFailureCounter,
	)
}
