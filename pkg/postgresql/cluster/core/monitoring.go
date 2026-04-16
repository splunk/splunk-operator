package core

import (
	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
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

func removeScrapeAnnotations(annotations map[string]string) {
	delete(annotations, prometheusScrapeAnnotation)
	delete(annotations, prometheusPathAnnotation)
	delete(annotations, prometheusPortAnnotation)
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
