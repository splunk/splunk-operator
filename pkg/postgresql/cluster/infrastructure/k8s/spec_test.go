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

package k8s

import (
	"context"
	"testing"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestClusterSpecificationGetSpecificationWithAnnotationsReturnsCopies(t *testing.T) {
	ctx := context.Background()
	k8sClient, key := newSpecificationTestClient(t, &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1",
			Namespace: "default",
			Annotations: map[string]string{
				"existing": "value",
			},
		},
		Spec: enterprisev4.PostgresClusterSpec{
			PostgresVersion: ptr.To("18"),
		},
	})

	spec, annotations, err := NewClusterStateStore(k8sClient, key).GetSpecificationWithAnnotations(ctx)
	require.NoError(t, err)
	require.NotNil(t, spec.PostgresVersion)
	assert.Equal(t, "18", *spec.PostgresVersion)
	assert.Equal(t, "value", annotations["existing"])

	*spec.PostgresVersion = "19"
	annotations["existing"] = "changed"

	cluster := &enterprisev4.PostgresCluster{}
	require.NoError(t, k8sClient.Get(ctx, key, cluster))
	assert.Equal(t, "18", *cluster.Spec.PostgresVersion)
	assert.Equal(t, "value", cluster.Annotations["existing"])
}

func TestClusterSpecificationSetAnnotationsPersistsAnnotations(t *testing.T) {
	ctx := context.Background()
	k8sClient, key := newSpecificationTestClient(t, &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1",
			Namespace: "default",
			Annotations: map[string]string{
				"old": "value",
			},
		},
	})

	err := NewClusterStateStore(k8sClient, key).SetAnnotations(ctx, map[string]string{
		"enterprise.splunk.com/major-upgrade-retry-at": "2026-06-24T10:00:00Z",
	})
	require.NoError(t, err)

	cluster := &enterprisev4.PostgresCluster{}
	require.NoError(t, k8sClient.Get(ctx, key, cluster))
	assert.Equal(t, map[string]string{
		"old": "value",
		"enterprise.splunk.com/major-upgrade-retry-at": "2026-06-24T10:00:00Z",
	}, cluster.Annotations)
}

func newSpecificationTestClient(t *testing.T, cluster *enterprisev4.PostgresCluster) (client.Client, client.ObjectKey) {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, enterprisev4.AddToScheme(scheme))

	key := client.ObjectKeyFromObject(cluster)
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		Build(), key
}
