package metrics

import (
	"bytes"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
)

// Collector captures metrics for E2E runs.
type Collector struct {
	registry     *prometheus.Registry
	testsTotal   *prometheus.CounterVec
	stepsTotal   *prometheus.CounterVec
	testDuration *prometheus.HistogramVec
	stepDuration *prometheus.HistogramVec
	testInfo     *prometheus.GaugeVec
}

// NewCollector initializes a new metrics registry.
func NewCollector() *Collector {
	registry := prometheus.NewRegistry()
	collector := &Collector{
		registry: registry,
		testsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "e2e_tests_total", Help: "Total number of tests"},
			[]string{"status"},
		),
		stepsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "e2e_steps_total", Help: "Total number of steps"},
			[]string{"status"},
		),
		testDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "e2e_test_duration_seconds",
				Help:    "Test duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"test", "status", "topology"},
		),
		stepDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "e2e_step_duration_seconds",
				Help:    "Step duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"test", "action", "status"},
		),
		testInfo: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "e2e_test_info",
				Help: "E2E test metadata for traceability",
			},
			[]string{"test", "status", "topology", "operator_image", "splunk_image", "cluster_provider", "k8s_version", "node_os", "container_runtime"},
		),
	}

	registry.MustRegister(collector.testsTotal, collector.stepsTotal, collector.testDuration, collector.stepDuration, collector.testInfo)
	return collector
}

// ObserveTest records a test outcome.
func (c *Collector) ObserveTest(status string, duration time.Duration) {
	c.testsTotal.WithLabelValues(status).Inc()
	c.testDuration.WithLabelValues("all", status, "unknown").Observe(duration.Seconds())
}

// ObserveStep records a step outcome.
func (c *Collector) ObserveStep(testName, action, status string, duration time.Duration) {
	c.stepsTotal.WithLabelValues(status).Inc()
	c.stepDuration.WithLabelValues(testName, action, status).Observe(duration.Seconds())
}

// ObserveTestDetail records per-test metrics.
func (c *Collector) ObserveTestDetail(testName, status, topology string, duration time.Duration) {
	c.testDuration.WithLabelValues(testName, status, topology).Observe(duration.Seconds())
}

// ObserveTestInfo records metadata for a test.
func (c *Collector) ObserveTestInfo(info TestInfo) {
	c.testInfo.WithLabelValues(info.Test, info.Status, info.Topology, info.OperatorImage, info.SplunkImage, info.ClusterProvider, info.KubernetesVersion, info.NodeOSImage, info.ContainerRuntime).Set(1)
}

// TestInfo is a structured view of test metadata for metrics.
type TestInfo struct {
	Test              string
	Status            string
	Topology          string
	OperatorImage     string
	SplunkImage       string
	ClusterProvider   string
	KubernetesVersion string
	NodeOSImage       string
	ContainerRuntime  string
}

// Write writes all metrics to a Prometheus text file.
func (c *Collector) Write(path string) error {
	metricFamilies, err := c.registry.Gather()
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := expfmt.NewEncoder(&buf, expfmt.NewFormat(expfmt.TypeTextPlain))
	for _, family := range metricFamilies {
		if err := enc.Encode(family); err != nil {
			return err
		}
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
