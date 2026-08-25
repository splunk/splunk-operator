// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package helpers

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	. "github.com/onsi/gomega"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

type MetricFamilies map[string]*dto.MetricFamily

const postgresMetricsScrapeTimeout = 30 * time.Second

// NewMetricsClient creates the Kubernetes client used to proxy exporter
// metrics directly from a PostgreSQL pod.
func NewMetricsClient() (kubernetes.Interface, error) {
	restConfig, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(restConfig)
}

// ScrapePostgresMetrics reads and parses the current primary's exporter output.
func ScrapePostgresMetrics(
	ctx context.Context,
	kubeClient client.Client,
	metricsClient kubernetes.Interface,
	clusterKey types.NamespacedName,
) (MetricFamilies, error) {
	scrapeCtx, cancel := context.WithTimeout(ctx, postgresMetricsScrapeTimeout)
	defer cancel()

	cluster := &enterprisev4.PostgresCluster{}
	if err := kubeClient.Get(scrapeCtx, clusterKey, cluster); err != nil {
		return nil, fmt.Errorf("getting PostgresCluster primary for metrics scrape: %w", err)
	}
	if cluster.Status.CurrentPrimary == nil || *cluster.Status.CurrentPrimary == "" {
		return nil, fmt.Errorf("PostgresCluster %s has no current primary", clusterKey)
	}

	raw, err := metricsClient.CoreV1().Pods(clusterKey.Namespace).
		ProxyGet("http", *cluster.Status.CurrentPrimary, "9187", "metrics", nil).
		DoRaw(scrapeCtx)
	if err != nil {
		return nil, fmt.Errorf("scraping metrics from primary %s: %w", *cluster.Status.CurrentPrimary, err)
	}
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parsing PostgreSQL metrics response: %w", err)
	}
	return families, nil
}

// MetricSample returns the scalar sample whose labels exactly match expected.
func MetricSample(
	families MetricFamilies,
	familyName string,
	labels map[string]string,
) (float64, bool, error) {
	family, found := families[familyName]
	if !found {
		return 0, false, nil
	}
	for _, metric := range family.Metric {
		if !metricHasLabels(metric, labels) {
			continue
		}
		value, err := metricValue(metric)
		return value, true, err
	}
	return 0, false, nil
}

func metricHasLabels(metric *dto.Metric, expected map[string]string) bool {
	if len(metric.Label) != len(expected) {
		return false
	}
	actual := make(map[string]string, len(metric.Label))
	for _, pair := range metric.Label {
		actual[pair.GetName()] = pair.GetValue()
	}
	for name, value := range expected {
		if actual[name] != value {
			return false
		}
	}
	return true
}

func metricValue(metric *dto.Metric) (float64, error) {
	switch {
	case metric.Gauge != nil:
		return metric.Gauge.GetValue(), nil
	case metric.Counter != nil:
		return metric.Counter.GetValue(), nil
	case metric.Untyped != nil:
		return metric.Untyped.GetValue(), nil
	default:
		return 0, fmt.Errorf("metric sample has no scalar value")
	}
}

// MetricFamilyHasValue reports whether any scalar sample has the expected value.
func MetricFamilyHasValue(families MetricFamilies, familyName string, want float64) bool {
	family, found := families[familyName]
	if !found {
		return false
	}
	for _, metric := range family.Metric {
		value, err := metricValue(metric)
		if err == nil && value == want {
			return true
		}
	}
	return false
}

// ExpectMetricSample asserts one metric family's contract and exact label set.
func ExpectMetricSample(
	g Gomega,
	families MetricFamilies,
	familyName, help string,
	metricType dto.MetricType,
	labels map[string]string,
	want float64,
) {
	family, found := families[familyName]
	g.Expect(found).To(BeTrue(), "metric family %q is missing", familyName)
	if !found {
		return
	}
	g.Expect(family.GetType()).To(Equal(metricType))
	g.Expect(family.GetHelp()).To(Equal(help))
	value, found, err := MetricSample(families, familyName, labels)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue(), "metric family %q has no sample matching %v", familyName, labels)
	g.Expect(value).To(Equal(want))
}

func managedMetricFamilyNames(families MetricFamilies) []string {
	result := make([]string, 0)
	for name := range families {
		if strings.HasPrefix(name, "cnpg_splunk_operator_") {
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result
}
