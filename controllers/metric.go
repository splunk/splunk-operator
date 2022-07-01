package controllers

import (
	"github.com/prometheus/client_golang/prometheus"
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

func init() {
	metrics.Registry.MustRegister(
		reconcileCounters,
		reconcileErrorCounter,
		actionFailureCounters,
		apiTotalTimeMetricEvents,
	)
}
