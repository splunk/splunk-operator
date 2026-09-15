// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
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

package v4

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestClassicV3UpgradesToV4(t *testing.T) {
	tests := []struct {
		name   string
		kind   string
		crName string
		spec   map[string]any
	}{
		{
			name:   "IndexerCluster",
			kind:   "IndexerCluster",
			crName: "classic-upgrade-idx",
			spec: map[string]any{
				"replicas":                int64(3),
				"clusterManagerRef":       map[string]any{"name": "cm"},
				"licenseManagerRef":       map[string]any{"name": "lm"},
				"monitoringConsoleRef":    map[string]any{"name": "mc"},
				"image":                   "splunk/splunk:9.0.0",
				"imagePullPolicy":         "Never",
				"schedulerName":           "my-scheduler",
				"defaults":                "default-secret",
				"disableResourceDefaults": false,
			},
		},
		{
			name:   "SearchHeadCluster",
			kind:   "SearchHeadCluster",
			crName: "classic-upgrade-shc",
			spec: map[string]any{
				"replicas":                int64(3),
				"clusterManagerRef":       map[string]any{"name": "cm"},
				"licenseManagerRef":       map[string]any{"name": "lm"},
				"monitoringConsoleRef":    map[string]any{"name": "mc"},
				"image":                   "splunk/splunk:9.0.0",
				"imagePullPolicy":         "Never",
				"schedulerName":           "my-scheduler",
				"defaults":                "default-secret",
				"disableResourceDefaults": false,
				"detentionTimeoutSeconds": int64(120),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			k := requireAPIServer(t)

			v3 := &unstructured.Unstructured{}
			v3.SetGroupVersionKind(schema.GroupVersionKind{
				Group: "enterprise.splunk.com", Version: "v3", Kind: test.kind,
			})
			v3.SetName(test.crName)
			v3.SetNamespace("default")
			v3.SetLabels(map[string]string{"team": "sok"})
			v3.SetAnnotations(map[string]string{"note": "classic"})
			require.NoError(t, unstructured.SetNestedMap(v3.Object, test.spec, "spec"))
			require.NoError(t, k.Create(t.Context(), v3))
			t.Cleanup(func() { _ = k.Delete(t.Context(), v3) })

			upgraded := &unstructured.Unstructured{}
			upgraded.SetGroupVersionKind(schema.GroupVersionKind{
				Group: "enterprise.splunk.com", Version: "v4", Kind: test.kind,
			})
			require.NoError(t, k.Get(t.Context(), client.ObjectKey{Name: test.crName, Namespace: "default"}, upgraded))

			assert.Equal(t, map[string]string{"team": "sok"}, upgraded.GetLabels())
			assert.Equal(t, "classic", upgraded.GetAnnotations()["note"])

			spec, found, err := unstructured.NestedMap(upgraded.Object, "spec")
			require.NoError(t, err)
			require.True(t, found)
			for field, want := range test.spec {
				assert.Equal(t, want, spec[field], "spec.%s must survive the upgrade", field)
			}

			_, noahRef, err := unstructured.NestedMap(upgraded.Object, "spec", "noahClusterRef")
			require.NoError(t, err)
			assert.False(t, noahRef, "upgrading a classic resource must not introduce a Noah reference")
		})
	}
}

func TestClassicV3UpgradePreservesStatus(t *testing.T) {
	k := requireAPIServer(t)
	const name = "classic-upgrade-status"

	v3 := &unstructured.Unstructured{}
	v3.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "enterprise.splunk.com", Version: "v3", Kind: "IndexerCluster",
	})
	v3.SetName(name)
	v3.SetNamespace("default")
	require.NoError(t, unstructured.SetNestedMap(v3.Object, map[string]any{
		"replicas":          int64(1),
		"clusterManagerRef": map[string]any{"name": "cm"},
	}, "spec"))
	require.NoError(t, k.Create(t.Context(), v3))
	t.Cleanup(func() { _ = k.Delete(t.Context(), v3) })

	require.NoError(t, unstructured.SetNestedMap(v3.Object, map[string]any{
		"phase":         "Ready",
		"replicas":      int64(1),
		"readyReplicas": int64(1),
	}, "status"))
	require.NoError(t, k.Status().Update(t.Context(), v3))

	upgraded := &unstructured.Unstructured{}
	upgraded.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "enterprise.splunk.com", Version: "v4", Kind: "IndexerCluster",
	})
	require.NoError(t, k.Get(t.Context(), client.ObjectKey{Name: name, Namespace: "default"}, upgraded))

	status, found, err := unstructured.NestedMap(upgraded.Object, "status")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "Ready", status["phase"])
	assert.Equal(t, int64(1), status["readyReplicas"])
}

func TestClassicV3UpgradePreservesExplicitZeroValues(t *testing.T) {
	k := requireAPIServer(t)
	const name = "classic-upgrade-zero"

	v3 := &unstructured.Unstructured{}
	v3.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "enterprise.splunk.com", Version: "v3", Kind: "IndexerCluster",
	})
	v3.SetName(name)
	v3.SetNamespace("default")
	require.NoError(t, unstructured.SetNestedMap(v3.Object, map[string]any{
		"replicas":                int64(0),
		"clusterManagerRef":       map[string]any{"name": "cm"},
		"disableResourceDefaults": false,
		"schedulerName":           "",
	}, "spec"))
	require.NoError(t, k.Create(t.Context(), v3))
	t.Cleanup(func() { _ = k.Delete(t.Context(), v3) })

	upgraded := &unstructured.Unstructured{}
	upgraded.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "enterprise.splunk.com", Version: "v4", Kind: "IndexerCluster",
	})
	require.NoError(t, k.Get(t.Context(), client.ObjectKey{Name: name, Namespace: "default"}, upgraded))

	spec, _, err := unstructured.NestedMap(upgraded.Object, "spec")
	require.NoError(t, err)
	assert.Equal(t, int64(0), spec["replicas"], "an explicit zero must not become a default")
	assert.Equal(t, false, spec["disableResourceDefaults"])
	assert.Equal(t, "", spec["schedulerName"])
}
