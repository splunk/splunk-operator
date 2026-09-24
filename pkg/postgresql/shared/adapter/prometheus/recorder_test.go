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
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrometheusRecorderObserveProvisioningDuration(t *testing.T) {
	provisioningDuration.Reset()
	t.Cleanup(provisioningDuration.Reset)

	registry := prometheus.NewRegistry()
	require.NoError(t, Register(registry))

	recorder := NewPrometheusRecorder()
	recorder.ObserveProvisioningDuration(ports.ControllerCluster, 125)
	recorder.ObserveProvisioningDuration(ports.ControllerDatabase, 17)

	metricFamilies, err := registry.Gather()
	require.NoError(t, err)

	metricFamily := findMetricFamily(metricFamilies, "splunk_operator_postgres_provisioning_duration_seconds")
	require.NotNil(t, metricFamily)
	require.Equal(t, dto.MetricType_HISTOGRAM, metricFamily.GetType())

	cluster := histogramForController(t, metricFamily, ports.ControllerCluster)
	assert.Equal(t, uint64(1), cluster.GetSampleCount())
	assert.Equal(t, 125.0, cluster.GetSampleSum())
	assert.Equal(t, uint64(1), bucketCount(cluster, 300))

	database := histogramForController(t, metricFamily, ports.ControllerDatabase)
	assert.Equal(t, uint64(1), database.GetSampleCount())
	assert.Equal(t, 17.0, database.GetSampleSum())
	assert.Equal(t, uint64(0), bucketCount(database, 15))
	assert.Equal(t, uint64(1), bucketCount(database, 30))
}

func findMetricFamily(metricFamilies []*dto.MetricFamily, name string) *dto.MetricFamily {
	for _, metricFamily := range metricFamilies {
		if metricFamily.GetName() == name {
			return metricFamily
		}
	}
	return nil
}

func histogramForController(t *testing.T, metricFamily *dto.MetricFamily, controller string) *dto.Histogram {
	t.Helper()
	for _, metric := range metricFamily.GetMetric() {
		for _, label := range metric.GetLabel() {
			if label.GetName() == "controller" && label.GetValue() == controller {
				return metric.GetHistogram()
			}
		}
	}
	t.Fatalf("histogram metric with controller label %q not found", controller)
	return nil
}

func bucketCount(histogram *dto.Histogram, upperBound float64) uint64 {
	for _, bucket := range histogram.GetBucket() {
		if bucket.GetUpperBound() == upperBound {
			return bucket.GetCumulativeCount()
		}
	}
	return 0
}
