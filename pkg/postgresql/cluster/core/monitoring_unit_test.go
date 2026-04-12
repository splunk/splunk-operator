package core

import (
	"testing"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
						PostgreSQLMetrics: &enterprisev4.FeatureDisableOverride{Disabled: ptr.To(true)},
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
			name: "disabled when class disables even if cluster has override struct",
			cluster: &enterprisev4.PostgresCluster{
				Spec: enterprisev4.PostgresClusterSpec{
					Monitoring: &enterprisev4.PostgresClusterMonitoring{
						PostgreSQLMetrics: &enterprisev4.FeatureDisableOverride{Disabled: ptr.To(false)},
					},
				},
			},
			class: newClassWithMonitoring(
				ptr.To(false),
				nil,
				nil,
			),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPostgreSQLMetricsEnabled(tt.cluster, tt.class)
			assert.Equal(t, tt.want, got)
		})
	}
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
						ConnectionPoolerMetrics: &enterprisev4.FeatureDisableOverride{Disabled: ptr.To(true)},
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

func TestBuildPostgreSQLMetricsService(t *testing.T) {
	scheme := newMonitoringTestScheme(t)
	cluster := newTestMonitoringCluster()

	svc, err := buildPostgreSQLMetricsService(scheme, cluster)
	require.NoError(t, err)

	assert.Equal(t, "postgresql-cluster-dev-postgres-metrics", svc.Name)
	assert.Equal(t, cluster.Namespace, svc.Namespace)
	assert.Equal(t, "postgresql-metrics", svc.Labels[labelObservabilityComponent])
	assert.Equal(t, cluster.Name, svc.Labels[cnpgClusterLabelName])
	assert.Equal(t, cluster.Name, svc.Spec.Selector[cnpgClusterLabelName])
	assert.Equal(t, cnpgPodRoleInstance, svc.Spec.Selector[cnpgPodRoleLabelName])
	require.Len(t, svc.Spec.Ports, 1)
	assert.Equal(t, postgresMetricsPortName, svc.Spec.Ports[0].Name)
	assert.Equal(t, postgresMetricsPort, svc.Spec.Ports[0].Port)
	assert.Equal(t, postgresMetricsPortName, svc.Spec.Ports[0].TargetPort.StrVal)
	assertMonitoringOwnerRef(t, svc.OwnerReferences, cluster)
}

func TestBuildConnectionPoolerMetricsService(t *testing.T) {
	scheme := newMonitoringTestScheme(t)
	cluster := newTestMonitoringCluster()

	svc, err := buildConnectionPoolerMetricsService(scheme, cluster, readWriteEndpoint)
	require.NoError(t, err)

	assert.Equal(t, "postgresql-cluster-dev-pooler-rw-metrics", svc.Name)
	assert.Equal(t, "pgbouncer-metrics", svc.Labels[labelObservabilityComponent])
	assert.Equal(t, poolerResourceName(cluster.Name, readWriteEndpoint), svc.Labels[cnpgPoolerNameLabel])
	assert.Equal(t, poolerResourceName(cluster.Name, readWriteEndpoint), svc.Spec.Selector[cnpgPoolerNameLabel])
	require.Len(t, svc.Spec.Ports, 1)
	assert.Equal(t, poolerMetricsPortName, svc.Spec.Ports[0].Name)
	assert.Equal(t, poolerMetricsPort, svc.Spec.Ports[0].Port)
	assert.Equal(t, poolerMetricsPortName, svc.Spec.Ports[0].TargetPort.StrVal)
	assertMonitoringOwnerRef(t, svc.OwnerReferences, cluster)
}

func TestBuildPostgreSQLMetricsServiceMonitor(t *testing.T) {
	scheme := newMonitoringTestScheme(t)
	cluster := newTestMonitoringCluster()

	sm, err := buildPostgreSQLMetricsServiceMonitor(scheme, cluster)
	require.NoError(t, err)

	assert.Equal(t, "postgresql-cluster-dev-postgres-metrics-monitor", sm.Name)
	assert.Equal(t, "postgresql-metrics", sm.Labels[labelObservabilityComponent])
	assert.Equal(t, cluster.Name, sm.Spec.Selector.MatchLabels[cnpgClusterLabelName])
	require.Len(t, sm.Spec.Endpoints, 1)
	assert.Equal(t, postgresMetricsPortName, sm.Spec.Endpoints[0].Port)
	assert.Equal(t, "/metrics", sm.Spec.Endpoints[0].Path)
	assert.Equal(t, "http", sm.Spec.Endpoints[0].Scheme)
	assertMonitoringOwnerRef(t, sm.OwnerReferences, cluster)
}

func TestBuildConnectionPoolerMetricsServiceMonitor(t *testing.T) {
	scheme := newMonitoringTestScheme(t)
	cluster := newTestMonitoringCluster()

	sm, err := buildConnectionPoolerMetricsServiceMonitor(scheme, cluster, readOnlyEndpoint)
	require.NoError(t, err)

	assert.Equal(t, "postgresql-cluster-dev-pooler-ro-metrics-monitor", sm.Name)
	assert.Equal(t, "pgbouncer-metrics", sm.Labels[labelObservabilityComponent])
	assert.Equal(t, poolerResourceName(cluster.Name, readOnlyEndpoint), sm.Labels[cnpgPoolerNameLabel])
	assert.Equal(t, poolerResourceName(cluster.Name, readOnlyEndpoint), sm.Spec.Selector.MatchLabels[cnpgPoolerNameLabel])
	require.Len(t, sm.Spec.Endpoints, 1)
	assert.Equal(t, poolerMetricsPortName, sm.Spec.Endpoints[0].Port)
	assert.Equal(t, "/metrics", sm.Spec.Endpoints[0].Path)
	assert.Equal(t, "http", sm.Spec.Endpoints[0].Scheme)
	assertMonitoringOwnerRef(t, sm.OwnerReferences, cluster)
}

func newMonitoringTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, monitoringv1.AddToScheme(scheme))
	require.NoError(t, enterprisev4.AddToScheme(scheme))

	return scheme
}

func newTestMonitoringCluster() *enterprisev4.PostgresCluster {
	return &enterprisev4.PostgresCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: enterprisev4.GroupVersion.String(),
			Kind:       "PostgresCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "postgresql-cluster-dev",
			Namespace: "test",
			UID:       "cluster-uid",
		},
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

func assertMonitoringOwnerRef(t *testing.T, ownerRefs []metav1.OwnerReference, cluster *enterprisev4.PostgresCluster) {
	t.Helper()

	require.Len(t, ownerRefs, 1)
	assert.Equal(t, cluster.APIVersion, ownerRefs[0].APIVersion)
	assert.Equal(t, cluster.Kind, ownerRefs[0].Kind)
	assert.Equal(t, cluster.Name, ownerRefs[0].Name)
	assert.Equal(t, cluster.UID, ownerRefs[0].UID)
	require.NotNil(t, ownerRefs[0].Controller)
	assert.True(t, *ownerRefs[0].Controller)
}
