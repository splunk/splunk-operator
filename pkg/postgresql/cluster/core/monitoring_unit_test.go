package core

import (
	"encoding/json"
	"errors"
	"testing"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
			name: "disabled when class observability is absent",
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
			class: newClassWithObservability(
				ptr.To(true),
				nil,
				nil,
				nil,
			),
			want: true,
		},
		{
			name: "disabled when cluster override disables",
			cluster: &enterprisev4.PostgresCluster{
				Spec: enterprisev4.PostgresClusterSpec{
					Observability: &enterprisev4.PostgresObservabilityOverride{
						PostgreSQL: &enterprisev4.FeatureDisableOverride{Disabled: ptr.To(true)},
					},
				},
			},
			class: newClassWithObservability(
				ptr.To(true),
				nil,
				nil,
				nil,
			),
			want: false,
		},
		{
			name: "disabled when class disables even if cluster has override struct",
			cluster: &enterprisev4.PostgresCluster{
				Spec: enterprisev4.PostgresClusterSpec{
					Observability: &enterprisev4.PostgresObservabilityOverride{
						PostgreSQL: &enterprisev4.FeatureDisableOverride{Disabled: ptr.To(false)},
					},
				},
			},
			class: newClassWithObservability(
				ptr.To(false),
				nil,
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

func TestIsConnectionPoolerEnabled(t *testing.T) {
	tests := []struct {
		name    string
		cluster *enterprisev4.PostgresCluster
		class   *enterprisev4.PostgresClusterClass
		want    bool
	}{
		{
			name:  "disabled when class config is absent",
			class: &enterprisev4.PostgresClusterClass{},
			want:  false,
		},
		{
			name:    "inherits enabled class setting when cluster override is unset",
			cluster: &enterprisev4.PostgresCluster{},
			class: &enterprisev4.PostgresClusterClass{
				Spec: enterprisev4.PostgresClusterClassSpec{
					Config: &enterprisev4.PostgresClusterClassConfig{
						ConnectionPoolerEnabled: ptr.To(true),
					},
				},
			},
			want: true,
		},
		{
			name: "cluster can disable class enabled pooler",
			cluster: &enterprisev4.PostgresCluster{
				Spec: enterprisev4.PostgresClusterSpec{
					ConnectionPoolerEnabled: ptr.To(false),
				},
			},
			class: &enterprisev4.PostgresClusterClass{
				Spec: enterprisev4.PostgresClusterClassSpec{
					Config: &enterprisev4.PostgresClusterClassConfig{
						ConnectionPoolerEnabled: ptr.To(true),
					},
				},
			},
			want: false,
		},
		{
			name: "class disabled wins",
			cluster: &enterprisev4.PostgresCluster{
				Spec: enterprisev4.PostgresClusterSpec{
					ConnectionPoolerEnabled: ptr.To(true),
				},
			},
			class: &enterprisev4.PostgresClusterClass{
				Spec: enterprisev4.PostgresClusterClassSpec{
					Config: &enterprisev4.PostgresClusterClassConfig{
						ConnectionPoolerEnabled: ptr.To(false),
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isConnectionPoolerEnabled(tt.cluster, tt.class)
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
			name:    "disabled when pooler itself is disabled",
			cluster: &enterprisev4.PostgresCluster{},
			class: newClassWithObservability(
				nil,
				ptr.To(true),
				nil,
				ptr.To(false),
			),
			want: false,
		},
		{
			name:    "enabled when pooler and pgbouncer metrics are enabled",
			cluster: &enterprisev4.PostgresCluster{},
			class: newClassWithObservability(
				nil,
				ptr.To(true),
				ptr.To(true),
				ptr.To(true),
			),
			want: true,
		},
		{
			name: "disabled when cluster override disables pgbouncer metrics",
			cluster: &enterprisev4.PostgresCluster{
				Spec: enterprisev4.PostgresClusterSpec{
					Observability: &enterprisev4.PostgresObservabilityOverride{
						PgBouncer: &enterprisev4.FeatureDisableOverride{Disabled: ptr.To(true)},
					},
				},
			},
			class: newClassWithObservability(
				nil,
				ptr.To(true),
				ptr.To(true),
				ptr.To(true),
			),
			want: false,
		},
		{
			name:    "disabled when class disables pgbouncer metrics",
			cluster: &enterprisev4.PostgresCluster{},
			class: newClassWithObservability(
				nil,
				ptr.To(true),
				ptr.To(false),
				ptr.To(true),
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

func TestIsGrafanaDashboardEnabled(t *testing.T) {
	tests := []struct {
		name    string
		cluster *enterprisev4.PostgresCluster
		class   *enterprisev4.PostgresClusterClass
		want    bool
	}{
		{
			name:    "enabled when class enables and cluster override is unset",
			cluster: &enterprisev4.PostgresCluster{},
			class: newClassWithObservability(
				nil,
				nil,
				nil,
				ptr.To(true),
			),
			want: true,
		},
		{
			name: "disabled when cluster override disables dashboard",
			cluster: &enterprisev4.PostgresCluster{
				Spec: enterprisev4.PostgresClusterSpec{
					Observability: &enterprisev4.PostgresObservabilityOverride{
						GrafanaDashboard: &enterprisev4.FeatureDisableOverride{Disabled: ptr.To(true)},
					},
				},
			},
			class: newClassWithObservability(
				nil,
				nil,
				nil,
				ptr.To(true),
			),
			want: false,
		},
		{
			name:    "disabled when class disables dashboard",
			cluster: &enterprisev4.PostgresCluster{},
			class: newClassWithObservability(
				nil,
				nil,
				nil,
				ptr.To(false),
			),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isGrafanaDashboardEnabled(tt.cluster, tt.class)
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

func TestBuildGrafanaDashboardConfigMap(t *testing.T) {
	scheme := newMonitoringTestScheme(t)
	cluster := newTestMonitoringCluster()

	cm, err := buildGrafanaDashboardConfigMap(scheme, cluster)
	require.NoError(t, err)

	assert.Equal(t, "postgresql-cluster-dev-grafana-dashboard", cm.Name)
	assert.Equal(t, "grafana-dashboard", cm.Labels[labelObservabilityComponent])
	assert.Equal(t, grafanaDashboardLabelValue, cm.Labels[grafanaDashboardLabelKey])
	assert.Contains(t, cm.Data, "dashboard.json")
	assert.NotContains(t, cm.Data["dashboard.json"], "__CLUSTER_NAME__")
	assert.Contains(t, cm.Data["dashboard.json"], cluster.Name)
	assert.Contains(t, cm.Data["dashboard.json"], cluster.Namespace)
	assert.Contains(t, cm.Data["dashboard.json"], cluster.Name+postgresMetricsServiceSuffix)
	assert.Contains(t, cm.Data["dashboard.json"], poolerMetricsServiceName(cluster.Name, readWriteEndpoint))
	assert.Contains(t, cm.Data["dashboard.json"], poolerMetricsServiceName(cluster.Name, readOnlyEndpoint))

	var dashboard map[string]any
	require.NoError(t, json.Unmarshal([]byte(cm.Data["dashboard.json"]), &dashboard))
	assertMonitoringOwnerRef(t, cm.OwnerReferences, cluster)
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

func TestIsServiceMonitorUnavailable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "not found error",
			err:  apierrors.NewNotFound(schema.GroupResource{Group: "monitoring.coreos.com", Resource: "servicemonitors"}, "test"),
			want: true,
		},
		{
			name: "kind match string error",
			err:  errors.New("no matches for kind \"ServiceMonitor\" in version \"monitoring.coreos.com/v1\""),
			want: true,
		},
		{
			name: "resource string error",
			err:  errors.New("servicemonitors.monitoring.coreos.com not found"),
			want: true,
		},
		{
			name: "unrelated error",
			err:  errors.New("boom"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isServiceMonitorUnavailable(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
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

func newClassWithObservability(
	postgresEnabled *bool,
	poolerEnabled *bool,
	pgBouncerMetricsEnabled *bool,
	grafanaEnabled *bool,
) *enterprisev4.PostgresClusterClass {
	return &enterprisev4.PostgresClusterClass{
		Spec: enterprisev4.PostgresClusterClassSpec{
			Config: &enterprisev4.PostgresClusterClassConfig{
				ConnectionPoolerEnabled: poolerEnabled,
				Observability: &enterprisev4.PostgresObservabilityClassConfig{
					PostgreSQL:       &enterprisev4.MetricsClassConfig{Enabled: postgresEnabled},
					PgBouncer:        &enterprisev4.MetricsClassConfig{Enabled: pgBouncerMetricsEnabled},
					GrafanaDashboard: &enterprisev4.GrafanaDashboardClassConfig{Enabled: grafanaEnabled},
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
