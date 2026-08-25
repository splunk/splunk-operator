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

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"
)

func TestIsPostgreSQLMetricsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		cluster *platformv1alpha1.PostgresCluster
		class   *platformv1alpha1.PostgresClusterClass
		want    bool
	}{
		{
			name: "disabled when class monitoring is absent",
			class: &platformv1alpha1.PostgresClusterClass{
				Spec: platformv1alpha1.PostgresClusterClassSpec{
					Config: &platformv1alpha1.PostgresClusterClassConfig{},
				},
			},
			want: false,
		},
		{
			name:    "enabled when class enables and cluster override is unset",
			cluster: &platformv1alpha1.PostgresCluster{},
			class: newClassWithMonitoring(
				ptr.To(true),
				nil,
				nil,
			),
			want: true,
		},
		{
			name: "disabled when cluster override disables",
			cluster: &platformv1alpha1.PostgresCluster{
				Spec: platformv1alpha1.PostgresClusterSpec{
					Monitoring: &platformv1alpha1.PostgresClusterMonitoring{
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
			cluster: &platformv1alpha1.PostgresCluster{
				Spec: platformv1alpha1.PostgresClusterSpec{
					Monitoring: &platformv1alpha1.PostgresClusterMonitoring{
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
		cluster *platformv1alpha1.PostgresCluster
		class   *platformv1alpha1.PostgresClusterClass
		want    bool
	}{
		{
			name:    "disabled when class monitoring is absent",
			cluster: &platformv1alpha1.PostgresCluster{},
			class: &platformv1alpha1.PostgresClusterClass{
				Spec: platformv1alpha1.PostgresClusterClassSpec{
					Config: &platformv1alpha1.PostgresClusterClassConfig{},
				},
			},
			want: false,
		},
		{
			name:    "enabled when class enables and cluster override is unset",
			cluster: &platformv1alpha1.PostgresCluster{},
			class: newClassWithMonitoring(
				nil,
				ptr.To(true),
				ptr.To(true),
			),
			want: true,
		},
		{
			name: "enabled even when cluster explicitly disables the pooler",
			cluster: &platformv1alpha1.PostgresCluster{
				Spec: platformv1alpha1.PostgresClusterSpec{
					ConnectionPooler: &platformv1alpha1.ConnectionPoolerEnableConfig{
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
			cluster: &platformv1alpha1.PostgresCluster{
				Spec: platformv1alpha1.PostgresClusterSpec{
					Monitoring: &platformv1alpha1.PostgresClusterMonitoring{
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
			cluster: &platformv1alpha1.PostgresCluster{},
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
) *platformv1alpha1.PostgresClusterClass {
	cls := &platformv1alpha1.PostgresClusterClass{
		Spec: platformv1alpha1.PostgresClusterClassSpec{
			Config: &platformv1alpha1.PostgresClusterClassConfig{
				Monitoring: &platformv1alpha1.PostgresMonitoringClassConfig{
					PostgreSQLMetrics:       &platformv1alpha1.MetricsClassConfig{Enabled: postgresEnabled},
					ConnectionPoolerMetrics: &platformv1alpha1.MetricsClassConfig{Enabled: connectionPoolerMetricsEnabled},
				},
			},
		},
	}
	if poolerEnabled != nil {
		cls.Spec.Config.ConnectionPooler = &platformv1alpha1.ConnectionPoolerEnableConfig{Enabled: poolerEnabled}
	}
	return cls
}
