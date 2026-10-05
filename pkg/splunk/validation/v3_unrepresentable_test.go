/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	enterpriseApiV4 "github.com/splunk/splunk-operator/api/enterprise/v4"
)

func TestUnrepresentableIndexerClusterFields(t *testing.T) {
	t.Run("reports nothing for a spec using only shared fields", func(t *testing.T) {
		assert.Empty(t, unrepresentableIndexerClusterFields(&enterpriseApiV4.IndexerClusterSpec{
			Replicas: 3,
			CommonSplunkSpec: enterpriseApiV4.CommonSplunkSpec{
				ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
				ServiceAccount:    "splunk-sa",
			},
		}))
	})

	t.Run("reports every v4-only field that is set, in spec order", func(t *testing.T) {
		assert.Equal(t,
			[]string{"spec.queueRef", "spec.objectStorageRef", "spec.noahClusterRef"},
			unrepresentableIndexerClusterFields(&enterpriseApiV4.IndexerClusterSpec{
				QueueRef:         &corev1.ObjectReference{Name: "queue"},
				ObjectStorageRef: &corev1.ObjectReference{Name: "store"},
				NoahClusterRef:   &corev1.LocalObjectReference{Name: "noah"},
			}))
	})

	t.Run("reports noahClusterRef on its own", func(t *testing.T) {
		assert.Equal(t, []string{"spec.noahClusterRef"},
			unrepresentableIndexerClusterFields(&enterpriseApiV4.IndexerClusterSpec{
				NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
			}))
	})
}

func TestUnrepresentableSearchHeadClusterFields(t *testing.T) {
	t.Run("reports nothing for a spec using only shared fields", func(t *testing.T) {
		assert.Empty(t, unrepresentableSearchHeadClusterFields(&enterpriseApiV4.SearchHeadClusterSpec{
			Replicas:                3,
			DetentionTimeoutSeconds: 1800,
			AppFrameworkConfig:      enterpriseApiV4.AppFrameworkSpec{AppsRepoPollInterval: 600},
		}))
	})

	t.Run("reports every v4-only field that is set, in spec order", func(t *testing.T) {
		assert.Equal(t,
			[]string{"spec.noahClusterRef", "spec.deployerResourceSpec", "spec.deployerNodeAffinity"},
			unrepresentableSearchHeadClusterFields(&enterpriseApiV4.SearchHeadClusterSpec{
				NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
				DeployerResourceSpec: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
				},
				DeployerNodeAffinity: &corev1.NodeAffinity{},
			}))
	})

	// deployerResourceSpec is the only v4-only field that is a value type, so
	// "set" cannot be a nil check and an empty-but-non-nil map must not count.
	t.Run("ignores a deployerResourceSpec with no requirements", func(t *testing.T) {
		assert.Empty(t, unrepresentableSearchHeadClusterFields(
			&enterpriseApiV4.SearchHeadClusterSpec{Replicas: 3}))
	})

	t.Run("ignores a deployerResourceSpec whose maps are empty rather than nil", func(t *testing.T) {
		assert.Empty(t, unrepresentableSearchHeadClusterFields(&enterpriseApiV4.SearchHeadClusterSpec{
			DeployerResourceSpec: corev1.ResourceRequirements{
				Limits:   corev1.ResourceList{},
				Requests: corev1.ResourceList{},
			},
		}))
	})

	t.Run("reports a deployerResourceSpec setting only claims", func(t *testing.T) {
		assert.Equal(t, []string{"spec.deployerResourceSpec"},
			unrepresentableSearchHeadClusterFields(&enterpriseApiV4.SearchHeadClusterSpec{
				DeployerResourceSpec: corev1.ResourceRequirements{
					Claims: []corev1.ResourceClaim{{Name: "gpu"}},
				},
			}))
	})
}

func TestRefuseIfUnrepresentable(t *testing.T) {
	t.Run("returns nil when no fields are unrepresentable", func(t *testing.T) {
		assert.NoError(t, refuseIfUnrepresentable("IndexerCluster", "splunk", "idx", nil))
	})

	t.Run("names every offending field in one error", func(t *testing.T) {
		err := refuseIfUnrepresentable("IndexerCluster", "splunk", "idx", []string{"spec.queueRef", "spec.noahClusterRef"})

		assert.EqualError(t, err, "IndexerCluster splunk/idx cannot be represented in apiVersion enterprise.splunk.com/v3: spec.queueRef, spec.noahClusterRef set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource")
	})
}
