package controllers

import (
	"github.com/prometheus/client_golang/prometheus"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	labelNamespace       = "namespace"
	labelName            = "name"
	labelKind            = "kind"
	labelErrorType       = "error_type"
	labelMethodName      = "api"
	labelModuleName      = "module"
	labelResourceVersion = "resource_version"
	labelPrevState       = "prev_state"
	labelNewState        = "new_state"
)

var reconcileCounters = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "splunk_operator_reconcile_total",
	Help: "The number of times reconciled by this controller",
}, []string{labelNamespace, labelName, labelKind})

var reconcileErrorCounter = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_reconcile_error_total",
	Help: "The number of times the operator has failed to reconcile",
})

var actionFailureCounters = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "splunk_operator_error_total",
	Help: "The number of times operator has entered an error state",
}, []string{labelErrorType})

var apiTotalTimeMetricEvents = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "splunk_operator_module_duration_in_milliseconds",
	Help: "The time it takes to complete each call in standalone (in milliseconds)",
}, []string{labelNamespace, labelName, labelKind, labelModuleName, labelMethodName})

func getPrometheusLabels(request reconcile.Request, kind string) prometheus.Labels {
	return prometheus.Labels{
		labelNamespace: request.Namespace,
		labelName:      request.Name,
		labelKind:      kind,
	}
}

var upgradeStandalonePrepare = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeStandaloneBackup = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeStandaloneRestore = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeStandaloneUpgrade = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeStandaloneVerification = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeStandaloneReady = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeStandaloneError = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeLicenseManagerPrepare = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeLicenseManagerBackup = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeLicenseManagerRestore = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeLicenseManagerUpgrade = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeLicenseManagerVerification = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeLicenseManagerReady = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeLicenseManagerError = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeClusterManagerPrepare = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeClusterManagerBackup = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeClusterManagerRestore = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeClusterManagerUpgrade = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeClusterManagerVerification = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeClusterManagerReady = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeClusterManagerError = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeMonitoringConsolePrepare = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeMonitoringConsoleBackup = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeMonitoringConsoleRestore = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeMonitoringConsoleUpgrade = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeMonitoringConsoleVerification = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeMonitoringConsoleReady = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeMonitoringConsoleError = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeSearchHeadClusterPrepare = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeSearchHeadClusterBackup = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeSearchHeadClusterRestore = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeSearchHeadClusterUpgrade = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeSearchHeadClusterVerification = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeSearchHeadClusterReady = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeSearchHeadClusterError = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeIndexerClusterPrepare = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeIndexerClusterBackup = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeIndexerClusterRestore = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeIndexerClusterUpgrade = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeIndexerClusterVerification = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeIndexerClusterReady = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

var upgradeIndexerClusterError = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_standalone_prepare_total",
	Help: "Number of times a standalone is found to be in prepare for upgrade state",
})

func crMetricLabels(request ctrl.Request) prometheus.Labels {
	return prometheus.Labels{
		labelNamespace: request.Namespace,
		labelName:      request.Name,
	}
}

var slowOperationBuckets = []float64{30, 90, 180, 360, 720, 1440}

var stateTime = map[enterpriseApi.ProvisioningState]*prometheus.HistogramVec{
	enterpriseApi.StateStandalonePrepare: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "splunk_operator_standalone_prepare_state_duration_seconds",
		Help: "Length of time cr in prepare state",
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateStandaloneBackup: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in backup state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateStandaloneRestore: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in restore state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateStandaloneUpgrade: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateStandaloneVerification: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateStandaloneReady: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateStandaloneError: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),

	enterpriseApi.StateLicenseManagerPrepare: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "splunk_operator_operation_register_duration_seconds",
		Help: "Length of time cr in prepare state",
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateLicenseManagerBackup: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_operation_inspect_duration_seconds",
		Help:    "Length of time cr in backup state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateLicenseManagerRestore: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_operation_provision_duration_seconds",
		Help:    "Length of time cr in restore state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateLicenseManagerUpgrade: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateLicenseManagerVerification: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateLicenseManagerReady: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateLicenseManagerError: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),

	enterpriseApi.StateClusterManagerPrepare: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "splunk_operator_operation_register_duration_seconds",
		Help: "Length of time cr in prepare state",
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateClusterManagerBackup: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_operation_inspect_duration_seconds",
		Help:    "Length of time cr in backup state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateClusterManagerRestore: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_operation_provision_duration_seconds",
		Help:    "Length of time cr in restore state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateClusterManagerUpgrade: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateClusterManagerVerification: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateClusterManagerReady: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateClusterManagerError: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),

	enterpriseApi.StateMonitoringConsolePrepare: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "splunk_operator_operation_register_duration_seconds",
		Help: "Length of time cr in prepare state",
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateMonitoringConsoleBackup: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_operation_inspect_duration_seconds",
		Help:    "Length of time cr in backup state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateMonitoringConsoleRestore: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_operation_provision_duration_seconds",
		Help:    "Length of time cr in restore state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateMonitoringConsoleUpgrade: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in upgrade state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateMonitoringConsoleVerification: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateMonitoringConsoleReady: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in ready state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateMonitoringConsoleError: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),

	enterpriseApi.StateSearchHeadClusterPrepare: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "splunk_operator_operation_register_duration_seconds",
		Help: "Length of time cr in prepare state",
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateSearchHeadClusterBackup: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_operation_inspect_duration_seconds",
		Help:    "Length of time cr in backup state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateSearchHeadClusterRestore: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_operation_provision_duration_seconds",
		Help:    "Length of time cr in restore state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateSearchHeadClusterUpgrade: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateSearchHeadClusterVerification: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateSearchHeadClusterReady: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateSearchHeadClusterError: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),

	enterpriseApi.StateIndexerClusterPrepare: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "splunk_operator_operation_register_duration_seconds",
		Help: "Length of time cr in prepare state",
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateIndexerClusterBackup: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_operation_inspect_duration_seconds",
		Help:    "Length of time cr in backup state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateIndexerClusterRestore: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_operation_provision_duration_seconds",
		Help:    "Length of time cr in restore state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateIndexerClusterUpgrade: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateIndexerClusterVerification: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateIndexerClusterReady: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
	enterpriseApi.StateIndexerClusterError: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "splunk_operator_standalone_prepare_state_duration_seconds",
		Help:    "Length of time cr in verification state",
		Buckets: slowOperationBuckets,
	}, []string{labelNamespace, labelName}),
}

var stateChanges = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "splunk_operator_upgrade_state_change_total",
	Help: "Number of times a state transition has occurred",
}, []string{labelPrevState, labelNewState})

func stateChangeMetricLabels(prevState, newState enterpriseApi.ProvisioningState) prometheus.Labels {
	return prometheus.Labels{
		labelPrevState: string(prevState),
		labelNewState:  string(newState),
	}
}

func init() {
	metrics.Registry.MustRegister(
		reconcileCounters,
		reconcileErrorCounter,
		actionFailureCounters,
		apiTotalTimeMetricEvents,
	)
}
