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
package core

import (
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
)

const (
	prometheusScrapeAnnotation = "prometheus.io/scrape"
	prometheusPathAnnotation   = "prometheus.io/path"
	prometheusPortAnnotation   = "prometheus.io/port"

	metricsPath               = "/metrics"
	postgresMetricsPortString = "9187"
	poolerMetricsPortString   = "9127"
)

func buildScrapeAnnotations(port string) map[string]string {
	return map[string]string{
		prometheusScrapeAnnotation: "true",
		prometheusPathAnnotation:   metricsPath,
		prometheusPortAnnotation:   port,
	}
}

func buildPostgresScrapeAnnotations() map[string]string {
	return buildScrapeAnnotations(postgresMetricsPortString)
}

func buildPoolerScrapeAnnotations() map[string]string {
	return buildScrapeAnnotations(poolerMetricsPortString)
}

func isPostgreSQLMetricsEnabled(cluster *enterprisev4.PostgresCluster, class *enterprisev4.PostgresClusterClass) bool {
	classEnabled := class != nil &&
		class.Spec.Config != nil &&
		class.Spec.Config.Monitoring != nil &&
		class.Spec.Config.Monitoring.PostgreSQLMetrics != nil &&
		class.Spec.Config.Monitoring.PostgreSQLMetrics.Enabled != nil &&
		*class.Spec.Config.Monitoring.PostgreSQLMetrics.Enabled

	if cluster != nil && cluster.Spec.Monitoring != nil && cluster.Spec.Monitoring.PostgreSQLMetrics != nil {
		return *cluster.Spec.Monitoring.PostgreSQLMetrics
	}

	return classEnabled
}

func isConnectionPoolerMetricsEnabled(cluster *enterprisev4.PostgresCluster, class *enterprisev4.PostgresClusterClass) bool {
	classEnabled := class != nil &&
		class.Spec.Config != nil &&
		class.Spec.Config.Monitoring != nil &&
		class.Spec.Config.Monitoring.ConnectionPoolerMetrics != nil &&
		class.Spec.Config.Monitoring.ConnectionPoolerMetrics.Enabled != nil &&
		*class.Spec.Config.Monitoring.ConnectionPoolerMetrics.Enabled

	if cluster != nil && cluster.Spec.Monitoring != nil && cluster.Spec.Monitoring.ConnectionPoolerMetrics != nil {
		return *cluster.Spec.Monitoring.ConnectionPoolerMetrics
	}

	return classEnabled
}
