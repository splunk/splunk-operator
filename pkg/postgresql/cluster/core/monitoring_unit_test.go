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
	"testing"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
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
					ConnectionPooler: &enterprisev4.ConnectionPoolerEnableConfig{
						Enabled: ptr.To(false),
					},
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
	cls := &enterprisev4.PostgresClusterClass{
		Spec: enterprisev4.PostgresClusterClassSpec{
			Config: &enterprisev4.PostgresClusterClassConfig{
				Monitoring: &enterprisev4.PostgresMonitoringClassConfig{
					PostgreSQLMetrics:       &enterprisev4.MetricsClassConfig{Enabled: postgresEnabled},
					ConnectionPoolerMetrics: &enterprisev4.MetricsClassConfig{Enabled: connectionPoolerMetricsEnabled},
				},
			},
		},
	}
	if poolerEnabled != nil {
		cls.Spec.Config.ConnectionPooler = &enterprisev4.ConnectionPoolerEnableConfig{Enabled: poolerEnabled}
	}
	return cls
}
