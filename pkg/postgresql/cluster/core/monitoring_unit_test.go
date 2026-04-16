package core

import (
	"testing"

	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"
)

func TestIsPostgreSQLMetricsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		cluster *enterprisev4.PostgresCluster
		class   *enterprisev4.PostgresClusterClass
		want    bool
	}{
		{
			name: "disabled when class monitoring is absent",
			class: &enterprisev4.PostgresClusterClass{
				Spec: enterprisev4.PostgresClusterClassSpec{
					Config: &enterprisev4.PostgresClusterClassConfig{},
				},
			},
			want: false,
		},
		{
			name:    "enabled when class enables and cluster override is unset",
			cluster: &enterprisev4.PostgresCluster{},
			class: newClassWithMonitoring(
				ptr.To(true),
				nil,
				nil,
			),
			want: true,
		},
		{
			name: "disabled when cluster override disables",
			cluster: &enterprisev4.PostgresCluster{
				Spec: enterprisev4.PostgresClusterSpec{
					Monitoring: &enterprisev4.PostgresClusterMonitoring{
						PostgreSQLMetrics: ptr.To(false),
					},
				},
			},
			class: newClassWithMonitoring(
				ptr.To(true),
				nil,
				nil,
			),
			want: false,
		},
		{
			name: "enabled when cluster overrides class that has it disabled",
			cluster: &enterprisev4.PostgresCluster{
				Spec: enterprisev4.PostgresClusterSpec{
					Monitoring: &enterprisev4.PostgresClusterMonitoring{
						PostgreSQLMetrics: ptr.To(true),
					},
				},
			},
			class: newClassWithMonitoring(
				ptr.To(false),
				nil,
				nil,
			),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPostgreSQLMetricsEnabled(tt.cluster, tt.class)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildScrapeAnnotations(t *testing.T) {
	t.Run("postgres annotations", func(t *testing.T) {
		got := buildPostgresScrapeAnnotations()

		assert.Equal(t, map[string]string{
			prometheusScrapeAnnotation: "true",
			prometheusPathAnnotation:   metricsPath,
			prometheusPortAnnotation:   postgresMetricsPortString,
		}, got)
	})

	t.Run("pooler annotations", func(t *testing.T) {
		got := buildPoolerScrapeAnnotations()

		assert.Equal(t, map[string]string{
			prometheusScrapeAnnotation: "true",
			prometheusPathAnnotation:   metricsPath,
			prometheusPortAnnotation:   poolerMetricsPortString,
		}, got)
	})
}

func TestRemoveScrapeAnnotations(t *testing.T) {
	t.Run("removes only managed scrape keys", func(t *testing.T) {
		annotations := map[string]string{
			prometheusScrapeAnnotation: "true",
			prometheusPathAnnotation:   metricsPath,
			prometheusPortAnnotation:   postgresMetricsPortString,
			"custom":                   "keep-me",
		}

		removeScrapeAnnotations(annotations)

		assert.Equal(t, map[string]string{
			"custom": "keep-me",
		}, annotations)
	})

	t.Run("nil map is safe", func(t *testing.T) {
		removeScrapeAnnotations(nil)
	})
}

func TestIsConnectionPoolerMetricsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		cluster *enterprisev4.PostgresCluster
		class   *enterprisev4.PostgresClusterClass
		want    bool
	}{
		{
			name:    "disabled when class monitoring is absent",
			cluster: &enterprisev4.PostgresCluster{},
			class: &enterprisev4.PostgresClusterClass{
				Spec: enterprisev4.PostgresClusterClassSpec{
					Config: &enterprisev4.PostgresClusterClassConfig{},
				},
			},
			want: false,
		},
		{
			name:    "enabled when class enables and cluster override is unset",
			cluster: &enterprisev4.PostgresCluster{},
			class: newClassWithMonitoring(
				nil,
				ptr.To(true),
				ptr.To(true),
			),
			want: true,
		},
		{
			name: "enabled even when cluster explicitly disables the pooler",
			cluster: &enterprisev4.PostgresCluster{
				Spec: enterprisev4.PostgresClusterSpec{
					ConnectionPoolerEnabled: ptr.To(false),
				},
			},
			class: newClassWithMonitoring(
				nil,
				ptr.To(true),
				ptr.To(true),
			),
			want: true,
		},
		{
			name: "disabled when cluster override disables pgbouncer metrics",
			cluster: &enterprisev4.PostgresCluster{
				Spec: enterprisev4.PostgresClusterSpec{
					Monitoring: &enterprisev4.PostgresClusterMonitoring{
						ConnectionPoolerMetrics: ptr.To(false),
					},
				},
			},
			class: newClassWithMonitoring(
				nil,
				ptr.To(true),
				ptr.To(true),
			),
			want: false,
		},
		{
			name:    "disabled when class disables pgbouncer metrics",
			cluster: &enterprisev4.PostgresCluster{},
			class: newClassWithMonitoring(
				nil,
				ptr.To(true),
				ptr.To(false),
			),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isConnectionPoolerMetricsEnabled(tt.cluster, tt.class)
			assert.Equal(t, tt.want, got)
		})
	}
}

func newClassWithMonitoring(
	postgresEnabled *bool,
	poolerEnabled *bool,
	connectionPoolerMetricsEnabled *bool,
) *enterprisev4.PostgresClusterClass {
	return &enterprisev4.PostgresClusterClass{
		Spec: enterprisev4.PostgresClusterClassSpec{
			Config: &enterprisev4.PostgresClusterClassConfig{
				ConnectionPoolerEnabled: poolerEnabled,
				Monitoring: &enterprisev4.PostgresMonitoringClassConfig{
					PostgreSQLMetrics:       &enterprisev4.MetricsClassConfig{Enabled: postgresEnabled},
					ConnectionPoolerMetrics: &enterprisev4.MetricsClassConfig{Enabled: connectionPoolerMetricsEnabled},
				},
			},
		},
	}
}
