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
package cnpgmonitoring

import (
	"context"
	"strings"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	machineryapi "github.com/cloudnative-pg/machinery/pkg/api"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	cnpginfra "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/cnpg"
	monitoring "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	enterprisev4.AddToScheme(scheme)
	cnpgv1.AddToScheme(scheme)
	corev1.AddToScheme(scheme)
	return scheme
}

func newTestCNPGCluster(name, ns string) *cnpgv1.Cluster {
	return &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, UID: "cnpg-uid-1"},
	}
}

func gaugeQuery(name string) monitoring.ResolvedQuery {
	return monitoring.ResolvedQuery{
		PlatformQuery: monitoring.PlatformQuery{
			Name:   name,
			Type:   monitoring.MetricTypeGauge,
			Help:   "help for " + name,
			SQL:    "SELECT count(*) AS value FROM pg_stat_activity",
			Value:  "value",
			Labels: []string{"state"},
		},
	}
}

func testTarget() monitoring.Target {
	return monitoring.Target{
		Namespace:    "ns",
		FeatureName:  "demo",
		FeatureUID:   "feature-uid-1",
		ProviderName: "demo",
	}
}

func newFeatureOwner() *enterprisev4.PostgresCluster {
	return &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "ns", UID: "feature-uid-1"},
	}
}

func newTestAdapter(c client.Client, scheme *runtime.Scheme) *Adapter {
	return New(c, scheme, testTarget())
}

func confirmed(expected monitoring.ExpectedState) monitoring.ConfirmedState {
	return monitoring.ConfirmedState{
		Revision: expected.Revision, Enabled: expected.Enabled, QueryCount: expected.QueryCount,
	}
}

func applyMetrics(t *testing.T, adapter *Adapter, cfg monitoring.AggregatedConfig) monitoring.ExpectedState {
	t.Helper()
	result, err := adapter.Apply(t.Context(), cfg)
	require.NoError(t, err)
	acknowledgeExpectedState(t, adapter, result)
	return result
}

func acknowledgeExpectedState(t *testing.T, adapter *Adapter, expected monitoring.ExpectedState) {
	t.Helper()
	cluster := &cnpgv1.Cluster{}
	key := types.NamespacedName{Name: adapter.target.ProviderName, Namespace: adapter.target.Namespace}
	require.NoError(t, adapter.client.Get(t.Context(), key, cluster))
	cluster.Status.ConfigMapResourceVersion.Metrics = map[string]string{}
	if expected.Enabled {
		cm := &corev1.ConfigMap{}
		require.NoError(t, adapter.client.Get(t.Context(), types.NamespacedName{
			Name: adapter.target.ProviderName + "-metrics", Namespace: adapter.target.Namespace,
		}, cm))
		cluster.Status.ConfigMapResourceVersion.Metrics[cm.Name] = cm.ResourceVersion
	}
	require.NoError(t, adapter.client.Update(t.Context(), cluster))
}

func TestApply_OversizedConfigDoesNotWrite(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCNPGCluster("demo", "ns")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	a := newTestAdapter(c, scheme)
	q := gaugeQuery("oversized")
	q.SQL = "SELECT '" + strings.Repeat("x", corev1.MaxSecretSize) + "' AS value"

	_, err := a.Apply(t.Context(), monitoring.AggregatedConfig{ClusterQueries: []monitoring.ResolvedQuery{q}})

	assert.ErrorIs(t, err, monitoring.ErrGeneratedConfigTooLarge)
	cm := &corev1.ConfigMap{}
	err = c.Get(t.Context(), types.NamespacedName{Name: "demo-metrics", Namespace: "ns"}, cm)
	assert.True(t, apierrors.IsNotFound(err), "generated ConfigMap must not be written")
	got := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: "demo", Namespace: "ns"}, got))
	assert.Nil(t, got.Spec.Monitoring, "CNPG selector must not be written")
}

func TestApply_OversizedConfigPreservesLastKnownGood(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCNPGCluster("demo", "ns")
	cluster.Spec.Monitoring = &cnpgv1.MonitoringConfiguration{
		CustomQueriesConfigMap: []machineryapi.ConfigMapKeySelector{{
			LocalObjectReference: machineryapi.LocalObjectReference{Name: "demo-metrics"},
			Key:                  cnpginfra.MonitoringCMKey,
		}},
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-metrics", Namespace: "ns"},
		Data:       map[string]string{cnpginfra.MonitoringCMKey: "last-known-good"},
	}
	require.NoError(t, ctrl.SetControllerReference(cluster, cm, scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, cm).Build()
	a := newTestAdapter(c, scheme)
	q := gaugeQuery("oversized")
	q.SQL = "SELECT '" + strings.Repeat("x", corev1.MaxSecretSize) + "' AS value"

	_, err := a.Apply(t.Context(), monitoring.AggregatedConfig{ClusterQueries: []monitoring.ResolvedQuery{q}})

	assert.ErrorIs(t, err, monitoring.ErrGeneratedConfigTooLarge)
	gotCM := &corev1.ConfigMap{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: "demo-metrics", Namespace: "ns"}, gotCM))
	assert.Equal(t, "last-known-good", gotCM.Data[cnpginfra.MonitoringCMKey])
	gotCluster := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: "demo", Namespace: "ns"}, gotCluster))
	require.Len(t, gotCluster.Spec.Monitoring.CustomQueriesConfigMap, 1)
	assert.Equal(t, "demo-metrics", gotCluster.Spec.Monitoring.CustomQueriesConfigMap[0].Name)
}

func TestApply_CreatesConfigMapAndPatchesCluster(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCNPGCluster("demo", "ns")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	a := newTestAdapter(c, scheme)

	cfg := monitoring.AggregatedConfig{ClusterQueries: []monitoring.ResolvedQuery{gaugeQuery("pg_conns")}}
	applyMetrics(t, a, cfg)

	cm := &corev1.ConfigMap{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "demo-metrics", Namespace: "ns"}, cm))
	assert.Contains(t, cm.Data[cnpginfra.MonitoringCMKey], "splunk_operator_cluster:pg_conns:")
	assert.Contains(t, cm.Data[cnpginfra.MonitoringCMKey], "name: splunk_operator_cluster_pg_conns")
	assert.True(t, strings.HasPrefix(cm.Annotations[cnpginfra.MonitoringCMHashAnnotation], "sha256:"))
	require.Len(t, cm.OwnerReferences, 1)
	assert.Equal(t, "demo", cm.OwnerReferences[0].Name)

	got := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "demo", Namespace: "ns"}, got))
	require.NotNil(t, got.Spec.Monitoring)
	require.Len(t, got.Spec.Monitoring.CustomQueriesConfigMap, 1)
	assert.Equal(t, "demo-metrics", got.Spec.Monitoring.CustomQueriesConfigMap[0].Name)
	assert.Equal(t, cnpginfra.MonitoringCMKey, got.Spec.Monitoring.CustomQueriesConfigMap[0].Key)
}

func TestApply_HashGateNoOpOnUnchanged(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCNPGCluster("demo", "ns")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	a := newTestAdapter(c, scheme)

	cfg := monitoring.AggregatedConfig{ClusterQueries: []monitoring.ResolvedQuery{gaugeQuery("pg_conns")}}
	applyMetrics(t, a, cfg)

	cm := &corev1.ConfigMap{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "demo-metrics", Namespace: "ns"}, cm))
	rvBefore := cm.ResourceVersion

	got := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "demo", Namespace: "ns"}, got))
	applyMetrics(t, a, cfg)

	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "demo-metrics", Namespace: "ns"}, cm))
	assert.Equal(t, rvBefore, cm.ResourceVersion, "identical config must not rewrite the ConfigMap")
}

func TestApply_PreservesUnrelatedSelectors(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCNPGCluster("demo", "ns")
	cluster.Spec.Monitoring = &cnpgv1.MonitoringConfiguration{
		CustomQueriesConfigMap: []machineryapi.ConfigMapKeySelector{{
			LocalObjectReference: machineryapi.LocalObjectReference{Name: "cnpg-default-monitoring"},
			Key:                  "default.yaml",
		}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	cfg := monitoring.AggregatedConfig{ClusterQueries: []monitoring.ResolvedQuery{gaugeQuery("pg_conns")}}
	applyMetrics(t, newTestAdapter(c, scheme), cfg)

	got := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "demo", Namespace: "ns"}, got))
	require.Len(t, got.Spec.Monitoring.CustomQueriesConfigMap, 2)
	assert.Equal(t, "cnpg-default-monitoring", got.Spec.Monitoring.CustomQueriesConfigMap[0].Name)
	assert.Equal(t, "demo-metrics", got.Spec.Monitoring.CustomQueriesConfigMap[1].Name)
}

func TestApply_EmptyRemovesOnlyOwnedConfiguration(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCNPGCluster("demo", "ns")
	cluster.Spec.Monitoring = &cnpgv1.MonitoringConfiguration{
		CustomQueriesConfigMap: []machineryapi.ConfigMapKeySelector{{
			LocalObjectReference: machineryapi.LocalObjectReference{Name: "cnpg-default-monitoring"},
			Key:                  "default.yaml",
		}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	a := newTestAdapter(c, scheme)
	applyMetrics(t, a,
		monitoring.AggregatedConfig{ClusterQueries: []monitoring.ResolvedQuery{gaugeQuery("pg_conns")}})

	current := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "demo", Namespace: "ns"}, current))
	applyMetrics(t, a, monitoring.AggregatedConfig{})

	cm := &corev1.ConfigMap{}
	err := c.Get(context.Background(), types.NamespacedName{Name: "demo-metrics", Namespace: "ns"}, cm)
	assert.Error(t, err)
	got := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "demo", Namespace: "ns"}, got))
	require.Len(t, got.Spec.Monitoring.CustomQueriesConfigMap, 1)
	assert.Equal(t, "cnpg-default-monitoring", got.Spec.Monitoring.CustomQueriesConfigMap[0].Name)
}

func TestApply_RepairsDataDriftWithMatchingHash(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCNPGCluster("demo", "ns")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	a := newTestAdapter(c, scheme)
	cfg := monitoring.AggregatedConfig{ClusterQueries: []monitoring.ResolvedQuery{gaugeQuery("pg_conns")}}
	applyMetrics(t, a, cfg)

	cm := &corev1.ConfigMap{}
	key := types.NamespacedName{Name: "demo-metrics", Namespace: "ns"}
	require.NoError(t, c.Get(context.Background(), key, cm))
	desired := cm.Data[cnpginfra.MonitoringCMKey]
	cm.Data[cnpginfra.MonitoringCMKey] = "drifted"
	require.NoError(t, c.Update(context.Background(), cm))

	current := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "demo", Namespace: "ns"}, current))
	applyMetrics(t, a, cfg)
	require.NoError(t, c.Get(context.Background(), key, cm))
	assert.Equal(t, desired, cm.Data[cnpginfra.MonitoringCMKey])
}

func TestApply_DisabledPreservesForeignConfigMapAndMatchingSelector(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCNPGCluster("demo", "ns")
	cluster.Spec.Monitoring = &cnpgv1.MonitoringConfiguration{
		CustomQueriesConfigMap: []machineryapi.ConfigMapKeySelector{
			{LocalObjectReference: machineryapi.LocalObjectReference{Name: "external"}, Key: "queries.yaml"},
			{LocalObjectReference: machineryapi.LocalObjectReference{Name: "demo-metrics"}, Key: cnpginfra.MonitoringCMKey},
		},
	}
	expectedSelectors := append([]machineryapi.ConfigMapKeySelector(nil), cluster.Spec.Monitoring.CustomQueriesConfigMap...)
	foreign := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-metrics", Namespace: "ns"},
		Data:       map[string]string{cnpginfra.MonitoringCMKey: "consumer-owned"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, foreign).Build()

	_, err := newTestAdapter(c, scheme).Apply(context.Background(), monitoring.AggregatedConfig{})
	require.Error(t, err)
	assert.ErrorIs(t, err, monitoring.ErrGeneratedResourceOwnershipConflict)

	gotCM := &corev1.ConfigMap{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "demo-metrics", Namespace: "ns"}, gotCM))
	assert.Equal(t, "consumer-owned", gotCM.Data[cnpginfra.MonitoringCMKey])

	gotCluster := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "demo", Namespace: "ns"}, gotCluster))
	require.NotNil(t, gotCluster.Spec.Monitoring)
	assert.Equal(t, expectedSelectors, gotCluster.Spec.Monitoring.CustomQueriesConfigMap)
}

func TestApply_DisabledIgnoresForeignConfigMapWithoutMatchingSelector(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCNPGCluster("demo", "ns")
	cluster.Spec.Monitoring = &cnpgv1.MonitoringConfiguration{
		CustomQueriesConfigMap: []machineryapi.ConfigMapKeySelector{
			{LocalObjectReference: machineryapi.LocalObjectReference{Name: "external"}, Key: "queries.yaml"},
		},
	}
	foreign := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-metrics", Namespace: "ns"},
		Data:       map[string]string{cnpginfra.MonitoringCMKey: "consumer-owned"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, foreign).Build()

	result, err := newTestAdapter(c, scheme).Apply(context.Background(), monitoring.AggregatedConfig{})
	require.NoError(t, err)
	assert.False(t, result.Enabled)

	gotCM := &corev1.ConfigMap{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "demo-metrics", Namespace: "ns"}, gotCM))
	assert.Equal(t, "consumer-owned", gotCM.Data[cnpginfra.MonitoringCMKey])

	gotCluster := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "demo", Namespace: "ns"}, gotCluster))
	require.NotNil(t, gotCluster.Spec.Monitoring)
	require.Len(t, gotCluster.Spec.Monitoring.CustomQueriesConfigMap, 1)
	assert.Equal(t, "external", gotCluster.Spec.Monitoring.CustomQueriesConfigMap[0].Name)
}

func TestApply_DisabledRemovesStaleManagedSelectorWhenConfigMapIsAbsent(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCNPGCluster("demo", "ns")
	cluster.Spec.Monitoring = &cnpgv1.MonitoringConfiguration{
		CustomQueriesConfigMap: []machineryapi.ConfigMapKeySelector{
			{LocalObjectReference: machineryapi.LocalObjectReference{Name: "external"}, Key: "queries.yaml"},
			{LocalObjectReference: machineryapi.LocalObjectReference{Name: "demo-metrics"}, Key: cnpginfra.MonitoringCMKey},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()

	result, err := newTestAdapter(c, scheme).Apply(t.Context(), monitoring.AggregatedConfig{})

	require.NoError(t, err)
	assert.False(t, result.Enabled)
	current := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: "demo", Namespace: "ns"}, current))
	require.Len(t, current.Spec.Monitoring.CustomQueriesConfigMap, 1)
	assert.Equal(t, "external", current.Spec.Monitoring.CustomQueriesConfigMap[0].Name)
}

func TestApply_RejectsForeignConfigMapWithoutMutation(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCNPGCluster("demo", "ns")
	cluster.Spec.Monitoring = &cnpgv1.MonitoringConfiguration{
		CustomQueriesConfigMap: []machineryapi.ConfigMapKeySelector{
			{LocalObjectReference: machineryapi.LocalObjectReference{Name: "external"}, Key: "queries.yaml"},
			{LocalObjectReference: machineryapi.LocalObjectReference{Name: "demo-metrics"}, Key: cnpginfra.MonitoringCMKey},
		},
	}
	foreign := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-metrics", Namespace: "ns"},
		Data:       map[string]string{cnpginfra.MonitoringCMKey: "consumer-owned"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, foreign).Build()
	_, err := newTestAdapter(c, scheme).Apply(context.Background(),
		monitoring.AggregatedConfig{ClusterQueries: []monitoring.ResolvedQuery{gaugeQuery("pg_conns")}})
	require.Error(t, err)
	assert.ErrorIs(t, err, monitoring.ErrGeneratedResourceOwnershipConflict)
	assert.Contains(t, err.Error(), "is not controlled by CNPG Cluster")

	gotCM := &corev1.ConfigMap{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "demo-metrics", Namespace: "ns"}, gotCM))
	assert.Equal(t, "consumer-owned", gotCM.Data[cnpginfra.MonitoringCMKey])
	gotCluster := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "demo", Namespace: "ns"}, gotCluster))
	require.NotNil(t, gotCluster.Spec.Monitoring)
	require.Len(t, gotCluster.Spec.Monitoring.CustomQueriesConfigMap, 1)
	assert.Equal(t, "external", gotCluster.Spec.Monitoring.CustomQueriesConfigMap[0].Name)
}

func TestObserve_RequiresExactDesiredState(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCNPGCluster("demo", "ns")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	a := newTestAdapter(c, scheme)

	expected := monitoring.ExpectedState{Revision: "missing", Enabled: true, QueryCount: 1}
	observation, err := a.Observe(context.Background(), expected)
	require.NoError(t, err)
	assert.Equal(t, monitoring.ObservationPending, observation.State)

	applied := applyMetrics(t, a, monitoring.AggregatedConfig{ClusterQueries: []monitoring.ResolvedQuery{gaugeQuery("pg_conns")}})
	observation, err = a.Observe(context.Background(), applied)
	require.NoError(t, err)
	assert.Equal(t, monitoring.ObservationReady, observation.State)
}

func TestObserve_WaitsForCNPGResourceVersionAcknowledgement(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCNPGCluster("demo", "ns")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	a := newTestAdapter(c, scheme)
	expected, err := a.Apply(t.Context(), monitoring.AggregatedConfig{
		ClusterQueries: []monitoring.ResolvedQuery{gaugeQuery("pg_conns")},
	})
	require.NoError(t, err)

	observation, err := a.Observe(t.Context(), expected)
	require.NoError(t, err)
	assert.Equal(t, monitoring.ObservationPending, observation.State)
	assert.Contains(t, observation.Message, "waiting for CNPG")

	current := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: "demo", Namespace: "ns"}, current))
	current.Status.ConfigMapResourceVersion.Metrics = map[string]string{"demo-metrics": "stale"}
	require.NoError(t, c.Update(t.Context(), current))
	observation, err = a.Observe(t.Context(), expected)
	require.NoError(t, err)
	assert.Equal(t, monitoring.ObservationPending, observation.State)

	acknowledgeExpectedState(t, a, expected)
	observation, err = a.Observe(t.Context(), expected)
	require.NoError(t, err)
	assert.Equal(t, monitoring.ObservationReady, observation.State)
}

func TestObserve_DisablementWaitsForCNPGStatusRemoval(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCNPGCluster("demo", "ns")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	a := newTestAdapter(c, scheme)
	applyMetrics(t, a, monitoring.AggregatedConfig{
		ClusterQueries: []monitoring.ResolvedQuery{gaugeQuery("pg_conns")},
	})

	disabled, err := a.Apply(t.Context(), monitoring.AggregatedConfig{})
	require.NoError(t, err)
	observation, err := a.Observe(t.Context(), disabled)
	require.NoError(t, err)
	assert.Equal(t, monitoring.ObservationPending, observation.State)

	acknowledgeExpectedState(t, a, disabled)
	observation, err = a.Observe(t.Context(), disabled)
	require.NoError(t, err)
	assert.Equal(t, monitoring.ObservationReady, observation.State)
}

func TestObserve_DisabledIgnoresNameOnlyCNPGStatusForForeignDifferentKey(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCNPGCluster("demo", "ns")
	cluster.Spec.Monitoring = &cnpgv1.MonitoringConfiguration{
		CustomQueriesConfigMap: []machineryapi.ConfigMapKeySelector{{
			LocalObjectReference: machineryapi.LocalObjectReference{Name: "demo-metrics"},
			Key:                  "consumer.yaml",
		}},
	}
	cluster.Status.ConfigMapResourceVersion.Metrics = map[string]string{"demo-metrics": "foreign-rv"}
	foreign := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-metrics", Namespace: "ns"},
		Data:       map[string]string{"consumer.yaml": "consumer-owned"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, foreign).Build()
	a := newTestAdapter(c, scheme)

	observation, err := a.Observe(t.Context(), monitoring.ExpectedState{
		Revision: cnpginfra.BuildMonitoringConfig("demo", "").Hash,
		Enabled:  false,
	})

	require.NoError(t, err)
	assert.Equal(t, monitoring.ObservationReady, observation.State)
	require.NotNil(t, observation.Confirmed)
	assert.False(t, observation.Confirmed.Enabled)
}

func TestObserve_RejectsPayloadHashBinaryDataAndSelectorDrift(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(t *testing.T, c client.Client)
	}{
		{
			name: "payload",
			mutate: func(t *testing.T, c client.Client) {
				cm := &corev1.ConfigMap{}
				require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: "demo-metrics", Namespace: "ns"}, cm))
				cm.Data[cnpginfra.MonitoringCMKey] = "drifted"
				require.NoError(t, c.Update(t.Context(), cm))
			},
		},
		{
			name: "hash",
			mutate: func(t *testing.T, c client.Client) {
				cm := &corev1.ConfigMap{}
				require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: "demo-metrics", Namespace: "ns"}, cm))
				cm.Annotations[cnpginfra.MonitoringCMHashAnnotation] = "drifted"
				require.NoError(t, c.Update(t.Context(), cm))
			},
		},
		{
			name: "binary data",
			mutate: func(t *testing.T, c client.Client) {
				cm := &corev1.ConfigMap{}
				require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: "demo-metrics", Namespace: "ns"}, cm))
				cm.BinaryData = map[string][]byte{"unexpected": []byte("drifted")}
				require.NoError(t, c.Update(t.Context(), cm))
			},
		},
		{
			name: "selector",
			mutate: func(t *testing.T, c client.Client) {
				cluster := &cnpgv1.Cluster{}
				require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: "demo", Namespace: "ns"}, cluster))
				cluster.Spec.Monitoring.CustomQueriesConfigMap = nil
				require.NoError(t, c.Update(t.Context(), cluster))
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			scheme := newTestScheme()
			cluster := newTestCNPGCluster("demo", "ns")
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
			a := newTestAdapter(c, scheme)
			applied := applyMetrics(t, a, monitoring.AggregatedConfig{
				ClusterQueries: []monitoring.ResolvedQuery{gaugeQuery("pg_conns")},
			})
			test.mutate(t, c)

			observation, err := a.Observe(t.Context(), applied)

			require.NoError(t, err)
			assert.Equal(t, monitoring.ObservationPending, observation.State)
			assert.Nil(t, observation.Confirmed)
		})
	}
}

func TestSafety_SuccessfulApplyThenDeletionOrDataDriftRollsBackConfirmedPayload(t *testing.T) {
	for _, test := range []struct {
		name  string
		drift func(t *testing.T, c client.Client, cm *corev1.ConfigMap)
	}{
		{
			name: "generated ConfigMap deletion",
			drift: func(t *testing.T, c client.Client, cm *corev1.ConfigMap) {
				t.Helper()
				require.NoError(t, c.Delete(t.Context(), cm))
			},
		},
		{
			name: "generated ConfigMap data drift",
			drift: func(t *testing.T, c client.Client, cm *corev1.ConfigMap) {
				t.Helper()
				cm.Data[cnpginfra.MonitoringCMKey] = "drifted"
				require.NoError(t, c.Update(t.Context(), cm))
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			scheme := newTestScheme()
			cluster := newTestCNPGCluster("demo", "ns")
			featureOwner := newFeatureOwner()
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, featureOwner).Build()
			a := newTestAdapter(c, scheme)
			cfg := monitoring.AggregatedConfig{
				ClusterQueries: []monitoring.ResolvedQuery{gaugeQuery("pg_conns")},
			}

			applied := applyMetrics(t, a, cfg)
			observation, err := a.Observe(t.Context(), applied)
			require.NoError(t, err)
			require.Equal(t, monitoring.ObservationReady, observation.State)
			_, err = a.Save(t.Context(), confirmed(applied))
			require.NoError(t, err)

			safety := &corev1.ConfigMap{}
			require.NoError(t, c.Get(t.Context(), types.NamespacedName{
				Name: "demo-metrics-lkg", Namespace: "ns",
			}, safety))
			require.Len(t, safety.OwnerReferences, 1)
			assert.Equal(t, featureOwner.UID, safety.OwnerReferences[0].UID)

			active := &corev1.ConfigMap{}
			activeKey := types.NamespacedName{Name: "demo-metrics", Namespace: "ns"}
			require.NoError(t, c.Get(t.Context(), activeKey, active))
			expectedYAML := active.Data[cnpginfra.MonitoringCMKey]
			test.drift(t, c, active)

			restored, err := a.Rollback(t.Context())
			require.NoError(t, err)
			require.True(t, restored.Available)
			assert.True(t, restored.Changed)
			acknowledgeExpectedState(t, a, restored.Expected)
			observation, err = a.Observe(t.Context(), restored.Expected)
			require.NoError(t, err)
			assert.Equal(t, monitoring.ObservationReady, observation.State)

			require.NoError(t, c.Get(t.Context(), activeKey, active))
			assert.Equal(t, expectedYAML, active.Data[cnpginfra.MonitoringCMKey])
		})
	}
}

func TestSafety_RefusesForeignSnapshotWithoutMutation(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCNPGCluster("demo", "ns")
	featureOwner := newFeatureOwner()
	foreign := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-metrics-lkg", Namespace: "ns"},
		Data:       map[string]string{cnpginfra.MonitoringCMKey: "foreign"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, featureOwner, foreign).Build()
	a := newTestAdapter(c, scheme)
	applied := applyMetrics(t, a, monitoring.AggregatedConfig{
		ClusterQueries: []monitoring.ResolvedQuery{gaugeQuery("pg_conns")},
	})
	observation, err := a.Observe(t.Context(), applied)
	require.NoError(t, err)
	require.Equal(t, monitoring.ObservationReady, observation.State)

	_, err = a.Save(t.Context(), *observation.Confirmed)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not controlled by PostgresCluster")
	current := &corev1.ConfigMap{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{
		Name: "demo-metrics-lkg", Namespace: "ns",
	}, current))
	assert.Equal(t, foreign.Data, current.Data)
	assert.Empty(t, current.OwnerReferences)
}
