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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestNoahClusterRefMutability asserts noahClusterRef is immutable on both
// workload kinds. Clearing it makes NoahEnabled report false, which routes a
// running Noah cluster down the Cluster Manager path; repointing it at another
// NoahCluster can change tenant. Both are silent, so admission has to refuse
// them.
func TestNoahClusterRefMutability(t *testing.T) {
	apiClient := requireAPIServer(t)

	t.Run("IndexerCluster", func(t *testing.T) {
		t.Run("should reject clearing noahClusterRef", func(t *testing.T) {
			indexerCluster := &IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "idx-ref-cleared", Namespace: "default"},
				Spec: IndexerClusterSpec{
					Replicas:       3,
					NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
				},
			}
			require.NoError(t, apiClient.Create(t.Context(), indexerCluster))

			indexerCluster.Spec.NoahClusterRef = nil
			assert.ErrorContains(t, apiClient.Update(t.Context(), indexerCluster),
				"noahClusterRef cannot be added or removed after creation")
		})

		t.Run("should reject repointing noahClusterRef at another NoahCluster", func(t *testing.T) {
			indexerCluster := &IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "idx-ref-repointed", Namespace: "default"},
				Spec: IndexerClusterSpec{
					Replicas:       3,
					NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
				},
			}
			require.NoError(t, apiClient.Create(t.Context(), indexerCluster))

			indexerCluster.Spec.NoahClusterRef = &corev1.LocalObjectReference{Name: "other-noah"}
			assert.ErrorContains(t, apiClient.Update(t.Context(), indexerCluster),
				"noahClusterRef.name is immutable once created")
		})

		t.Run("should reject adding noahClusterRef to a classic IndexerCluster", func(t *testing.T) {
			indexerCluster := &IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "idx-ref-added", Namespace: "default"},
				Spec: IndexerClusterSpec{
					Replicas: 3,
					CommonSplunkSpec: CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
					},
				},
			}
			require.NoError(t, apiClient.Create(t.Context(), indexerCluster))

			indexerCluster.Spec.ClusterManagerRef = corev1.ObjectReference{}
			indexerCluster.Spec.NoahClusterRef = &corev1.LocalObjectReference{Name: "noah"}
			assert.ErrorContains(t, apiClient.Update(t.Context(), indexerCluster),
				"noahClusterRef cannot be added or removed after creation")
		})

		t.Run("should accept an unrelated spec change", func(t *testing.T) {
			indexerCluster := &IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "idx-ref-unchanged", Namespace: "default"},
				Spec: IndexerClusterSpec{
					Replicas:       3,
					NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
				},
			}
			require.NoError(t, apiClient.Create(t.Context(), indexerCluster))

			indexerCluster.Spec.Replicas = 5
			assert.NoError(t, apiClient.Update(t.Context(), indexerCluster))
		})
	})

	t.Run("SearchHeadCluster", func(t *testing.T) {
		t.Run("should reject clearing noahClusterRef", func(t *testing.T) {
			searchHeadCluster := &SearchHeadCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "shc-ref-cleared", Namespace: "default"},
				Spec: SearchHeadClusterSpec{
					Replicas:       3,
					NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
				},
			}
			require.NoError(t, apiClient.Create(t.Context(), searchHeadCluster))

			searchHeadCluster.Spec.NoahClusterRef = nil
			assert.ErrorContains(t, apiClient.Update(t.Context(), searchHeadCluster),
				"noahClusterRef cannot be added or removed after creation")
		})

		t.Run("should reject repointing noahClusterRef at another NoahCluster", func(t *testing.T) {
			searchHeadCluster := &SearchHeadCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "shc-ref-repointed", Namespace: "default"},
				Spec: SearchHeadClusterSpec{
					Replicas:       3,
					NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
				},
			}
			require.NoError(t, apiClient.Create(t.Context(), searchHeadCluster))

			searchHeadCluster.Spec.NoahClusterRef = &corev1.LocalObjectReference{Name: "other-noah"}
			assert.ErrorContains(t, apiClient.Update(t.Context(), searchHeadCluster),
				"noahClusterRef.name is immutable once created")
		})

		t.Run("should accept an unrelated spec change", func(t *testing.T) {
			searchHeadCluster := &SearchHeadCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "shc-ref-unchanged", Namespace: "default"},
				Spec: SearchHeadClusterSpec{
					Replicas:       3,
					NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
				},
			}
			require.NoError(t, apiClient.Create(t.Context(), searchHeadCluster))

			searchHeadCluster.Spec.Replicas = 5
			assert.NoError(t, apiClient.Update(t.Context(), searchHeadCluster))
		})
	})
}
