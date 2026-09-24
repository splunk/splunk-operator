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
package cnpg

import (
	"context"
	"strings"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	machineryapi "github.com/cloudnative-pg/machinery/pkg/api"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
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

type replaceBeforeDeleteClient struct {
	client.Client
	replacement *corev1.ConfigMap
	replaced    bool
}

func (c *replaceBeforeDeleteClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	cm, ok := obj.(*corev1.ConfigMap)
	if ok && !c.replaced && c.replacement != nil &&
		cm.Namespace == c.replacement.Namespace && cm.Name == c.replacement.Name {
		current := &corev1.ConfigMap{}
		key := types.NamespacedName{Namespace: cm.Namespace, Name: cm.Name}
		if err := c.Client.Get(ctx, key, current); err != nil {
			return err
		}
		if err := c.Client.Delete(ctx, current); err != nil {
			return err
		}
		if err := c.Client.Create(ctx, c.replacement.DeepCopy()); err != nil {
			return err
		}
		c.replaced = true
	}
	return c.Client.Delete(ctx, obj, opts...)
}

func TestSerializeEntries_EmptyYieldsEmptyString(t *testing.T) {
	out, err := SerializeEntries(map[string]MetricEntry{})
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestSerializeEntries_RejectsOversizedOutput(t *testing.T) {
	entries := map[string]MetricEntry{
		"oversized": {
			Query:   "SELECT '" + strings.Repeat("x", corev1.MaxSecretSize) + "' AS value",
			Metrics: []map[string]MetricSpec{{"value": {Usage: "GAUGE"}}},
		},
	}
	_, err := SerializeEntries(entries)
	assert.ErrorIs(t, err, monitoring.ErrGeneratedConfigTooLarge)
	assert.Contains(t, err.Error(), "maximum is 1048576 bytes")
}

func TestBuildMonitoringConfig_SetsFields(t *testing.T) {
	mc := BuildMonitoringConfig("mycluster", "yaml-content")
	assert.Equal(t, "yaml-content", mc.YAML)
	assert.Equal(t, "mycluster-metrics", mc.CMName)
	assert.True(t, strings.HasPrefix(mc.Hash, "sha256:"), "hash must be a sha256 fingerprint")
}

func TestBuildMonitoringConfig_EmptyYAMLIsValid(t *testing.T) {
	mc := BuildMonitoringConfig("mycluster", "")
	assert.Empty(t, mc.YAML)
	assert.Equal(t, "mycluster-metrics", mc.CMName)
	assert.NotEmpty(t, mc.Hash, "hash of empty string is still a valid sha256")
}

func monitoringTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, enterprisev4.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	return scheme
}

func TestPatchClusterSelectorRejectsStaleResourceVersion(t *testing.T) {
	scheme := monitoringTestScheme(t)
	cluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "ns"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()

	stale := &cnpgv1.Cluster{}
	key := types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}
	require.NoError(t, c.Get(t.Context(), key, stale))

	concurrent := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(t.Context(), key, concurrent))
	concurrent.Labels = map[string]string{"concurrent": "update"}
	require.NoError(t, c.Update(t.Context(), concurrent))

	_, err := patchClusterSelector(t.Context(), c, stale, "demo-metrics", true)

	require.Error(t, err)
	assert.True(t, apierrors.IsConflict(err), "expected optimistic-lock conflict, got %v", err)

	current := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(t.Context(), key, current))
	assert.Equal(t, "update", current.Labels["concurrent"])
	assert.Empty(t, current.Spec.Monitoring)
}

func TestApplyMonitoringConfig_DeletePreconditionsPreserveReplacement(t *testing.T) {
	scheme := monitoringTestScheme(t)
	cluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "ns", UID: "provider-uid"},
		Spec: cnpgv1.ClusterSpec{Monitoring: &cnpgv1.MonitoringConfiguration{
			CustomQueriesConfigMap: []machineryapi.ConfigMapKeySelector{{
				LocalObjectReference: machineryapi.LocalObjectReference{Name: "demo-metrics"},
				Key:                  MonitoringCMKey,
			}},
		}},
	}
	active := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-metrics", Namespace: "ns", UID: "active-uid"},
		Data:       map[string]string{MonitoringCMKey: "managed"},
	}
	require.NoError(t, ctrl.SetControllerReference(cluster, active, scheme))
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, active).Build()
	racing := &replaceBeforeDeleteClient{
		Client: base,
		replacement: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "demo-metrics", Namespace: "ns", UID: "replacement-uid"},
			Data:       map[string]string{MonitoringCMKey: "replacement"},
		},
	}

	_, err := ApplyMonitoringConfig(t.Context(), racing, scheme, cluster, BuildMonitoringConfig("demo", ""))

	require.Error(t, err)
	assert.True(t, apierrors.IsConflict(err), "replacement race must remain retryable: %v", err)
	current := &corev1.ConfigMap{}
	require.NoError(t, base.Get(t.Context(), types.NamespacedName{Name: "demo-metrics", Namespace: "ns"}, current))
	assert.Equal(t, types.UID("replacement-uid"), current.UID)
	assert.Equal(t, "replacement", current.Data[MonitoringCMKey])
}

func TestDeleteMonitoringSnapshot_DeletePreconditionsPreserveReplacement(t *testing.T) {
	scheme := monitoringTestScheme(t)
	owner := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "ns", UID: "feature-uid"},
	}
	safety := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-metrics-lkg", Namespace: "ns", UID: "safety-uid"},
		Data:       map[string]string{MonitoringCMKey: "managed"},
	}
	require.NoError(t, ctrl.SetControllerReference(owner, safety, scheme))
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, safety).Build()
	racing := &replaceBeforeDeleteClient{
		Client: base,
		replacement: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "demo-metrics-lkg", Namespace: "ns", UID: "replacement-uid"},
			Data:       map[string]string{MonitoringCMKey: "replacement"},
		},
	}

	_, err := DeleteMonitoringSnapshot(
		t.Context(), racing, owner, enterprisev4.GroupVersion.String(), "PostgresCluster", "demo",
	)

	require.Error(t, err)
	assert.True(t, apierrors.IsConflict(err), "replacement race must remain retryable: %v", err)
	current := &corev1.ConfigMap{}
	require.NoError(t, base.Get(t.Context(), types.NamespacedName{Name: "demo-metrics-lkg", Namespace: "ns"}, current))
	assert.Equal(t, types.UID("replacement-uid"), current.UID)
	assert.Equal(t, "replacement", current.Data[MonitoringCMKey])
}

func TestDeleteMonitoringSnapshot_DeletesOwnedSnapshot(t *testing.T) {
	scheme := monitoringTestScheme(t)
	owner := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "ns", UID: "feature-uid"},
	}
	safety := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-metrics-lkg", Namespace: "ns", UID: "safety-uid"},
	}
	require.NoError(t, ctrl.SetControllerReference(owner, safety, scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, safety).Build()

	changed, err := DeleteMonitoringSnapshot(
		t.Context(), c, owner, enterprisev4.GroupVersion.String(), "PostgresCluster", "demo",
	)

	require.NoError(t, err)
	assert.True(t, changed)
	err = c.Get(t.Context(), types.NamespacedName{Name: "demo-metrics-lkg", Namespace: "ns"}, &corev1.ConfigMap{})
	assert.True(t, apierrors.IsNotFound(err))
}

func TestSaveMonitoringSnapshot_PreservesUnrelatedAnnotations(t *testing.T) {
	scheme := monitoringTestScheme(t)
	owner := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "ns", UID: "feature-uid"},
	}
	current := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "demo-metrics-lkg",
			Namespace:   "ns",
			Annotations: map[string]string{"external.example/owner": "platform"},
		},
	}
	require.NoError(t, ctrl.SetControllerReference(owner, current, scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, current).Build()
	yamlContent, err := SerializeEntries(map[string]MetricEntry{
		"splunk_operator_cluster:test": {
			Name:    "splunk_operator_cluster_test",
			Query:   `SELECT "value" FROM (SELECT 1 AS value) AS splunk_operator_custom_metrics`,
			Metrics: []map[string]MetricSpec{{"value": {Usage: "GAUGE"}}},
		},
	})
	require.NoError(t, err)
	snapshot := MonitoringSnapshot{YAML: yamlContent, Hash: hashContent(yamlContent), QueryCount: 1}

	changed, err := SaveMonitoringSnapshot(
		t.Context(), c, scheme, owner, enterprisev4.GroupVersion.String(), "PostgresCluster", "demo", snapshot,
	)
	require.NoError(t, err)
	assert.True(t, changed)

	got := &corev1.ConfigMap{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: "demo-metrics-lkg", Namespace: "ns"}, got))
	assert.Equal(t, "platform", got.Annotations["external.example/owner"])
	assert.Equal(t, snapshot.Hash, got.Annotations[MonitoringCMHashAnnotation])

	changed, err = SaveMonitoringSnapshot(
		t.Context(), c, scheme, owner, enterprisev4.GroupVersion.String(), "PostgresCluster", "demo", snapshot,
	)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestLoadMonitoringSnapshot_RejectsCorruptOrUnsupportedPayload(t *testing.T) {
	validYAML, err := SerializeEntries(map[string]MetricEntry{
		"splunk_operator_cluster:test": {
			Name:    "splunk_operator_cluster_test",
			Query:   `SELECT "value" FROM (SELECT 1 AS value) AS splunk_operator_custom_metrics`,
			Metrics: []map[string]MetricSpec{{"value": {Usage: "GAUGE"}}},
		},
	})
	require.NoError(t, err)

	tests := map[string]func(*corev1.ConfigMap){
		"hash mismatch": func(cm *corev1.ConfigMap) {
			cm.Annotations[MonitoringCMHashAnnotation] = "sha256:wrong"
		},
		"disabled marker": func(cm *corev1.ConfigMap) {
			cm.Annotations[MonitoringEnabledAnnotation] = "false"
		},
		"negative query count": func(cm *corev1.ConfigMap) {
			cm.Annotations[MonitoringQueryCountAnnotation] = "-1"
		},
		"malformed query count": func(cm *corev1.ConfigMap) {
			cm.Annotations[MonitoringQueryCountAnnotation] = "one"
		},
		"extra data": func(cm *corev1.ConfigMap) {
			cm.Data["extra"] = "value"
		},
		"binary data": func(cm *corev1.ConfigMap) {
			cm.BinaryData = map[string][]byte{"queries.yaml": []byte("value")}
		},
		"unsupported CNPG field with matching hash": func(cm *corev1.ConfigMap) {
			payload := validYAML + "  predicate_query: SELECT true\n"
			cm.Data[MonitoringCMKey] = payload
			cm.Annotations[MonitoringCMHashAnnotation] = hashContent(payload)
		},
		"provider name outside managed namespace with matching hash": func(cm *corev1.ConfigMap) {
			payload := strings.Replace(validYAML, "splunk_operator_cluster_test", "backends", 1)
			cm.Data[MonitoringCMKey] = payload
			cm.Annotations[MonitoringCMHashAnnotation] = hashContent(payload)
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			scheme := monitoringTestScheme(t)
			owner := &enterprisev4.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "ns", UID: "feature-uid"},
			}
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "demo-metrics-lkg",
					Namespace: "ns",
					Annotations: map[string]string{
						MonitoringCMHashAnnotation:     hashContent(validYAML),
						MonitoringEnabledAnnotation:    "true",
						MonitoringQueryCountAnnotation: "1",
					},
				},
				Data: map[string]string{MonitoringCMKey: validYAML},
			}
			require.NoError(t, ctrl.SetControllerReference(owner, cm, scheme))
			mutate(cm)
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, cm).Build()

			_, found, message, err := LoadMonitoringSnapshot(
				t.Context(), c, owner, enterprisev4.GroupVersion.String(), "PostgresCluster", "demo",
			)

			require.NoError(t, err)
			assert.False(t, found)
			assert.NotEmpty(t, message)
		})
	}
}

func TestGetMonitoringFeatureOwner_RequiresMatchingUID(t *testing.T) {
	scheme := monitoringTestScheme(t)
	owner := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "ns", UID: "current-uid"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner).Build()

	_, err := GetMonitoringFeatureOwner(t.Context(), c, "ns", "demo", "replaced-uid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "current-uid")
	assert.Contains(t, err.Error(), "replaced-uid")

	got, err := GetMonitoringFeatureOwner(t.Context(), c, "ns", "demo", "current-uid")
	require.NoError(t, err)
	assert.Equal(t, owner.UID, got.Object.GetUID())
}
