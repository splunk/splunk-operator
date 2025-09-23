package observability

import (
	"context"
	"fmt"

	v4 "github.com/splunk/splunk-operator/api/v4"
	obs "github.com/splunk/splunk-operator/pkg/observability"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ReconcileObservability wires Prometheus ServiceMonitors and optional OpenTelemetryCollector.
// Safe to call on every SHC reconcile (idempotent).
// ReconcileObservability wires Prometheus SMs/Services and optional OTel Collector,
// and returns a compact status summary to persist on SHC.status.
func ReconcileObservability(ctx context.Context, c client.Client, shc *v4.SearchHeadCluster) (v4.ObservabilitySummary, error) {
	sum := v4.ObservabilitySummary{}
	cfg := shc.Spec.Observability.Metrics

	// Detect CRDs/operators present
	promInstalled := obs.APIAvailable("monitoring.coreos.com/v1", "ServiceMonitor")
	otelInstalled := obs.APIAvailable("opentelemetry.io/v1beta1", "OpenTelemetryCollector")

	sum.Prometheus.Installed = promInstalled
	sum.OTEL.Installed = otelInstalled
	sum.Prometheus.Enabled = cfg.Prometheus != nil && cfg.Prometheus.Enabled
	sum.OTEL.Enabled = cfg.OTEL != nil && cfg.OTEL.Enabled

	// --- PROMETHEUS ---
	if sum.Prometheus.Enabled && promInstalled {
		pm := cfg.Prometheus
		interval := pm.Interval
		smNS := pm.ServiceMonitorNamespace
		if smNS == "" {
			smNS = "monitoring"
		}
		labels := map[string]string{"app.kubernetes.io/managed-by": "splunk-operator"}
		for k, v := range pm.AdditionalLabels {
			labels[k] = v
		}

		// Ensure SM for Splunk operator (cross-ns, no ownerRef)
		_ = obs.EnsureServiceMonitor(ctx, c, smNS, "splunk-operator",
			map[string]string{"control-plane": "controller-manager"},
			false, []string{"splunk-operator-system"},
			obs.SMEndpoint{
				Port:            "https",
				Interval:        interval,
				Scheme:          "https",
				TLSSkipVerify:   true,
				BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
			}, labels, nil)

		// Ensure SM for CNPG operator (cross-ns)
		_ = obs.EnsureServiceMonitor(ctx, c, smNS, "cnpg-operator",
			map[string]string{"control-plane": "controller-manager"},
			false, []string{"cnpg-system"},
			obs.SMEndpoint{
				Port:            "https",
				Interval:        interval,
				Scheme:          "https",
				TLSSkipVerify:   true,
				BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
			}, labels, nil)

		// Headless metrics Service + SM for each CNPG cluster in the SHC namespace
		clusters, _ := listCNPGClusters(ctx, c, shc.Namespace)
		sum.CNPG.MonitoredClusters = len(clusters)

		owner := metav1.OwnerReference{
			APIVersion: v4.GroupVersion.String(),
			Kind:       "SearchHeadCluster",
			Name:       shc.Name,
			UID:        shc.UID,
			Controller: ptr(true),
		}
		for _, name := range clusters {
			sel := map[string]string{"cnpg.io/cluster": name}
			_ = obs.EnsureHeadlessMetricsService(ctx, c, shc.Namespace, fmt.Sprintf("%s-metrics", name), sel, 9187, owner)
			_ = obs.EnsureServiceMonitor(ctx, c, smNS, fmt.Sprintf("cnpg-%s", name),
				sel, false, []string{shc.Namespace},
				obs.SMEndpoint{Port: "metrics", Interval: interval}, labels, nil)
		}

		// Count poolers (for summary)
		poolers, _ := listCNPGPoolers(ctx, c, shc.Namespace)
		sum.CNPG.MonitoredPoolers = len(poolers)

		sum.Prometheus.Ready = true // we ensured SMs/Services; we can't verify Prometheus server health here
		sum.Prometheus.Details = fmt.Sprintf("ServiceMonitors ensured; %d clusters, %d poolers", sum.CNPG.MonitoredClusters, sum.CNPG.MonitoredPoolers)
	}

	// --- OTel (operator mode) ---
	if sum.OTEL.Enabled && otelInstalled {
		ot := cfg.OTEL
		if ot.Mode == "" {
			ot.Mode = "operator"
		}
		if ot.CreateCollector {
			otelNS := ot.CollectorNamespace
			if otelNS == "" {
				otelNS = "observability"
			}
			selector := ot.ServiceMonitorSelector
			if selector == nil {
				selector = map[string]string{"otel/scrape": "true"}
			}
			name := fmt.Sprintf("otel-%s", shc.Namespace)

			_ = obs.EnsureOTelCollector(ctx, c, otelNS, name, selector, nil)

			// Best-effort Ready: check that the CR now exists
			if exists, _ := getOTelCR(ctx, c, otelNS, name); exists {
				sum.OTEL.Ready = true
				sum.OTEL.Details = fmt.Sprintf("Collector %q ensured (TargetAllocator on)", name)
			} else {
				sum.OTEL.Ready = false
				sum.OTEL.Details = "Collector not observed after ensure"
			}
		}
	}

	// One-liner message
	sum.Message = fmt.Sprintf(
		"Prometheus:%s/%s, OTel:%s/%s, CNPG:%d clusters, %d poolers",
		onOff(sum.Prometheus.Enabled), installed(sum.Prometheus.Installed),
		onOff(sum.OTEL.Enabled), installed(sum.OTEL.Installed),
		sum.CNPG.MonitoredClusters, sum.CNPG.MonitoredPoolers,
	)

	return sum, nil
}

func listCNPGClusters(ctx context.Context, c client.Client, namespace string) ([]string, error) {
	uList := &unstructured.UnstructuredList{}
	uList.SetGroupVersionKind(schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "ClusterList"})
	if err := c.List(ctx, uList, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(uList.Items))
	for _, it := range uList.Items {
		out = append(out, it.GetName())
	}
	return out, nil
}

func listCNPGPoolers(ctx context.Context, c client.Client, namespace string) ([]string, error) {
	uList := &unstructured.UnstructuredList{}
	uList.SetGroupVersionKind(schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "PoolerList"})
	if err := c.List(ctx, uList, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(uList.Items))
	for _, it := range uList.Items {
		out = append(out, it.GetName())
	}
	return out, nil
}

func getOTelCR(ctx context.Context, c client.Client, ns, name string) (bool, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: "opentelemetry.io", Version: "v1beta1", Kind: "OpenTelemetryCollector"})
	u.SetNamespace(ns)
	u.SetName(name)
	if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, u); err != nil {
		return false, err
	}
	return true, nil
}

func ptr[T any](v T) *T { return &v }

func onOff(b bool) string {
	if b {
		return "On"
	}
	return "Off"
}
func installed(b bool) string {
	if b {
		return "Installed"
	}
	return "Missing"
}
