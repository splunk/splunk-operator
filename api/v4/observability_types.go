package v4

// Keep it small and safe-by-default (off unless enabled).
type ObservabilitySpec struct {
	Metrics MetricsSpec `json:"metrics,omitempty"`
}

type MetricsSpec struct {
	Prometheus *PrometheusSpec `json:"prometheus,omitempty"`
	OTEL       *OTELSpec       `json:"otel,omitempty"`
}

type PrometheusSpec struct {
	Enabled                 bool              `json:"enabled"`
	CreateServiceMonitor    bool              `json:"createServiceMonitor,omitempty"`    // default true (controller assumes true if Enabled)
	CreatePodMonitor        bool              `json:"createPodMonitor,omitempty"`        // default false (we prefer ServiceMonitor via Services)
	ServiceMonitorNamespace string            `json:"serviceMonitorNamespace,omitempty"` // default "monitoring"
	AdditionalLabels        map[string]string `json:"additionalLabels,omitempty"`        // e.g. {"release":"prometheus"}
	Interval                string            `json:"interval,omitempty"`                // e.g. "30s"
}

type OTELSpec struct {
	Enabled                bool              `json:"enabled"`
	Mode                   string            `json:"mode,omitempty"`                   // "operator"|"standalone" (today: "operator")
	CreateCollector        bool              `json:"createCollector,omitempty"`        // default true if Enabled
	CollectorNamespace     string            `json:"collectorNamespace,omitempty"`     // default "observability"
	TargetAllocator        bool              `json:"targetAllocator,omitempty"`        // default true
	ServiceMonitorSelector map[string]string `json:"serviceMonitorSelector,omitempty"` // default {"otel/scrape":"true"}
	// Provide your exporter configuration via a Secret (mount/env—out of scope for this POC).
	ExporterConfigSecretRef *SecretRef `json:"exporterConfigSecretRef,omitempty"`
}

type SecretRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// A compact snapshot of observability wiring for this SHC.
type ObservabilitySummary struct {
	// High-level one-liner, good for kubectl columns.
	Message string `json:"message,omitempty"`

	// Prometheus and OpenTelemetry component summaries.
	Prometheus ComponentSummary `json:"prometheus,omitempty"`
	OTEL       ComponentSummary `json:"otel,omitempty"`

	// CNPG-related counts we wired for metrics.
	CNPG CNPGSummary `json:"cnpg,omitempty"`
}

// Generic component mini-summary.
type ComponentSummary struct {
	Enabled   bool   `json:"enabled,omitempty"`
	Installed bool   `json:"installed,omitempty"` // CRDs present (and typically operator running)
	Ready     bool   `json:"ready,omitempty"`     // our best-effort: resources ensured / CR present
	Details   string `json:"details,omitempty"`
}

// Extra details specific to database monitoring.
type CNPGSummary struct {
	MonitoredClusters int `json:"monitoredClusters,omitempty"`
	MonitoredPoolers  int `json:"monitoredPoolers,omitempty"`
}
