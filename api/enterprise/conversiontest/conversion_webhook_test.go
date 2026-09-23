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

package conversiontest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	v4 "github.com/splunk/splunk-operator/api/enterprise/v4"
)

func TestNoahObjectIsUnreadableAtV3(t *testing.T) {
	apiClient := requireAPIServer(t)
	namespace := freshNamespace(t, apiClient, "noah-read")

	noahCluster := &v4.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "idx-noah", Namespace: namespace},
		Spec: v4.IndexerClusterSpec{
			Replicas:       3,
			NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
		},
	}
	require.NoError(t, apiClient.Create(t.Context(), noahCluster))

	t.Run("v4 GET works", func(t *testing.T) {
		fetched := &v4.IndexerCluster{}
		require.NoError(t, apiClient.Get(t.Context(),
			client.ObjectKey{Namespace: namespace, Name: "idx-noah"}, fetched))
		require.NotNil(t, fetched.Spec.NoahClusterRef)
		assert.Equal(t, "noah", fetched.Spec.NoahClusterRef.Name)
	})

	t.Run("v3 GET is refused", func(t *testing.T) {
		atV3 := &v3.IndexerCluster{}
		err := apiClient.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: "idx-noah"}, atV3)

		// The whole refusal is asserted, but not with EqualError: the API server
		// prefixes it with a StorageError quoting a storage revision that varies
		// between runs.
		assert.ErrorContains(t, err, "IndexerCluster noah-read-ns/idx-noah cannot be represented in apiVersion enterprise.splunk.com/v3: spec.noahClusterRef set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource")
	})

	t.Run("v3 UPDATE does not strip noahClusterRef", func(t *testing.T) {
		// The write path fetches the stored object and converts it to the request
		// version, so refusing the read also refuses the read-modify-write.
		atV3 := &unstructured.Unstructured{}
		atV3.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "enterprise.splunk.com", Version: "v3", Kind: "IndexerCluster",
		})
		atV3.SetNamespace(namespace)
		atV3.SetName("idx-noah")
		require.NoError(t, unstructured.SetNestedField(atV3.Object, int64(5), "spec", "replicas"))

		assert.Error(t, apiClient.Update(t.Context(), atV3))

		stored := &v4.IndexerCluster{}
		require.NoError(t, apiClient.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: "idx-noah"}, stored))
		require.NotNil(t, stored.Spec.NoahClusterRef, "noahClusterRef must survive a v3 write attempt")
		assert.Equal(t, "noah", stored.Spec.NoahClusterRef.Name)
	})
}

func TestClassicObjectsStillConvertToV3(t *testing.T) {
	apiClient := requireAPIServer(t)
	namespace := freshNamespace(t, apiClient, "classic")

	classic := &v4.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "idx-classic", Namespace: namespace},
		Spec: v4.IndexerClusterSpec{
			Replicas: 3,
			CommonSplunkSpec: v4.CommonSplunkSpec{
				ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
			},
		},
	}
	require.NoError(t, apiClient.Create(t.Context(), classic))

	atV3 := &v3.IndexerCluster{}
	require.NoError(t, apiClient.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: "idx-classic"}, atV3), "a classic object must remain readable at v3")
	assert.Equal(t, int32(3), atV3.Spec.Replicas)
	assert.Equal(t, "cm", atV3.Spec.ClusterManagerRef.Name)

	atV3.Spec.Replicas = 5
	require.NoError(t, apiClient.Update(t.Context(), atV3), "a classic v3 write must succeed")

	stored := &v4.IndexerCluster{}
	require.NoError(t, apiClient.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: "idx-classic"}, stored))
	assert.Equal(t, int32(5), stored.Spec.Replicas)
}

func TestSearchHeadClusterConversionAtV3(t *testing.T) {
	apiClient := requireAPIServer(t)
	namespace := freshNamespace(t, apiClient, "shc")

	require.NoError(t, apiClient.Create(t.Context(), &v4.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "shc-noah", Namespace: namespace},
		Spec: v4.SearchHeadClusterSpec{
			Replicas:       3,
			NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
		},
	}))
	require.NoError(t, apiClient.Create(t.Context(), &v4.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "shc-classic", Namespace: namespace},
		Spec: v4.SearchHeadClusterSpec{
			Replicas: 3,
			CommonSplunkSpec: v4.CommonSplunkSpec{
				ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
			},
		},
	}))

	t.Run("Noah search head is refused at v3", func(t *testing.T) {
		atV3 := &v3.SearchHeadCluster{}
		err := apiClient.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: "shc-noah"}, atV3)

		require.Error(t, err)
		assert.ErrorContains(t, err, "SearchHeadCluster shc-ns/shc-noah cannot be represented in apiVersion enterprise.splunk.com/v3: spec.noahClusterRef set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource")
	})

	t.Run("classic search head converts successfully", func(t *testing.T) {
		atV3 := &v3.SearchHeadCluster{}
		require.NoError(t, apiClient.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: "shc-classic"}, atV3))
		assert.Equal(t, "cm", atV3.Spec.ClusterManagerRef.Name)
	})

	t.Run("deployer tuning is refused at v3", func(t *testing.T) {
		require.NoError(t, apiClient.Create(t.Context(), &v4.SearchHeadCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "shc-deployer", Namespace: namespace},
			Spec: v4.SearchHeadClusterSpec{
				Replicas: 3,
				CommonSplunkSpec: v4.CommonSplunkSpec{
					ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
				},
				DeployerResourceSpec: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
				},
			},
		}))

		atV3 := &v3.SearchHeadCluster{}
		err := apiClient.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: "shc-deployer"}, atV3)

		require.Error(t, err)
		assert.ErrorContains(t, err, "SearchHeadCluster shc-ns/shc-deployer cannot be represented in apiVersion enterprise.splunk.com/v3: spec.deployerResourceSpec set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource")
	})
}

func freshNamespace(t *testing.T, apiClient client.Client, prefix string) string {
	t.Helper()
	name := prefix + "-ns"
	err := apiClient.Create(t.Context(),
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})
	if !apierrors.IsAlreadyExists(err) {
		require.NoError(t, err)
	}
	return name
}
