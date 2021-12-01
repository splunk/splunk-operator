package controllers

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	labelNamespace       = "namespace"
	labelName            = "name"
	labelErrorType       = "error_type"
	labelMethodName      = "api"
	labelModuleName      = "module"
	labelResourceVersion = "resource_version"
)

var reconcileCounters = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "splunk_operator_reconcile_total",
	Help: "The number of times reconciled by this controller",
}, []string{labelNamespace, labelName})

var reconcileErrorCounter = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "splunk_operator_reconcile_error_total",
	Help: "The number of times the operator has failed to reconcile",
})

var actionFailureCounters = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "splunk_operator_error_total",
	Help: "The number of times standlaone has entered an error state",
}, []string{labelErrorType})

var apiTotalTimeMetricEvents = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "splunk_operator_module_duration_in_milliseconds",
	Help: "the time it takes to complete each call in standlaone (in milliseconds)",
}, []string{labelNamespace, labelName, labelModuleName, labelMethodName})

func getPrometheusLabels(request reconcile.Request) prometheus.Labels {
	return prometheus.Labels{
		labelNamespace: request.Namespace,
		labelName:      request.Name,
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
