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
	LabelAction          = "action"
	LabelReason          = "reason"
	LabelStage           = "stage"
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

var UpgradeStartTime = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "splunk_upgrade_start_time",
	Help: "Unix timestamp when the SHC upgrade started",
})

var UpgradeEndTime = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "splunk_upgrade_end_time",
	Help: "Unix timestamp when the SHC upgrade ended",
})

var ActiveHistoricalSearchCount = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "splunk_active_historical_search_count",
		Help: "Total number of active historical search count",
	}, []string{"sh_name"})

var ActiveRealtimeSearchCount = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "splunk_active_realtime_search_count",
		Help: "Total number of active realtime search count",
	}, []string{"sh_name"})

var SHCRolloutDecisionCounters = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "splunk_operator_shc_rollout_decision_total",
		Help: "The number of transitions between bounded Search Head Cluster rollout decisions",
	},
	[]string{LabelAction, LabelReason},
)

var SHCRolloutPartitionAdvanceCounter = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "splunk_operator_shc_rollout_partition_advance_total",
		Help: "The number of authorized Search Head Cluster partition changes",
	},
)

var SHCSearchDrainContinuationApprovalCounter = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "splunk_operator_shc_search_drain_continuation_approval_total",
		Help: "The number of operation-scoped approvals to continue after a Search Head Cluster search-drain timeout",
	},
)

var SHCAuthorizedRevisionWithdrawalCounter = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "splunk_operator_shc_authorized_revision_withdrawal_total",
		Help: "The number of durable withdrawals of failed, already-authorized Search Head Cluster revisions",
	},
)

var SHCAuthorizedRevisionRecoveryCounter = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "splunk_operator_shc_authorized_revision_recovery_total",
		Help: "The number of Search Head Cluster members recovered at a last known-good revision after authorized revision withdrawal",
	},
)

var IndexerLifecycleTransitionCounters = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "splunk_operator_indexer_lifecycle_transition_total",
		Help: "The number of durable Indexer Pod lifecycle stage transitions",
	},
	[]string{LabelStage, LabelReason},
)

var IndexerLifecycleStageDurationSeconds = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "splunk_operator_indexer_lifecycle_stage_duration_seconds",
		Help:    "Time spent in each completed durable Indexer Pod lifecycle stage",
		Buckets: prometheus.ExponentialBuckets(1, 2, 12),
	},
	[]string{LabelStage},
)

var IndexerEndpointWithdrawalCounters = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "splunk_operator_indexer_endpoint_withdrawal_total",
		Help: "The number of durable Indexer endpoint-withdrawal observations and invalidations",
	},
	[]string{LabelAction},
)

var SearchHeadEndpointWithdrawalCounters = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "splunk_operator_search_head_endpoint_withdrawal_total",
		Help: "The number of durable Search Head endpoint-withdrawal observations and invalidations",
	},
	[]string{LabelAction},
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
		ActiveHistoricalSearchCount,
		ActiveRealtimeSearchCount,
		SHCRolloutDecisionCounters,
		SHCRolloutPartitionAdvanceCounter,
		SHCSearchDrainContinuationApprovalCounter,
		SHCAuthorizedRevisionWithdrawalCounter,
		SHCAuthorizedRevisionRecoveryCounter,
		IndexerLifecycleTransitionCounters,
		IndexerLifecycleStageDurationSeconds,
		IndexerEndpointWithdrawalCounters,
		SearchHeadEndpointWithdrawalCounters,
	)
}
