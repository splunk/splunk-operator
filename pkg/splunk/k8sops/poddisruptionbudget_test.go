// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package k8sops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func TestApplyPodDisruptionBudget(t *testing.T) {
	desired := testPodDisruptionBudget("owner-uid", 1)
	client := spltest.NewMockClient()

	require.NoError(t, ApplyPodDisruptionBudget(t.Context(), client, desired.DeepCopy()))
	assert.Len(t, client.Calls["Create"], 1)

	require.NoError(t, ApplyPodDisruptionBudget(t.Context(), client, desired.DeepCopy()))
	assert.Empty(t, client.Calls["Update"])

	current := &policyv1.PodDisruptionBudget{}
	require.NoError(t, client.Get(t.Context(), types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, current))
	current.Spec.MaxUnavailable = new(intstr.FromInt32(2))
	current.Spec.Selector.MatchLabels["app"] = "stale"
	current.Labels["managed"] = "false"
	current.Labels["preserved"] = "true"
	require.NoError(t, client.Update(t.Context(), current))
	client.Calls["Update"] = nil

	require.NoError(t, ApplyPodDisruptionBudget(t.Context(), client, desired.DeepCopy()))
	assert.Len(t, client.Calls["Update"], 1)
	require.NoError(t, client.Get(t.Context(), types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, current))
	require.NotNil(t, current.Spec.MaxUnavailable)
	assert.Equal(t, 1, current.Spec.MaxUnavailable.IntValue())
	assert.Equal(t, desired.Spec.Selector, current.Spec.Selector)
	assert.Equal(t, "true", current.Labels["managed"])
	assert.Equal(t, "true", current.Labels["preserved"])
}

func TestApplyPodDisruptionBudgetRejectsForeignController(t *testing.T) {
	desired := testPodDisruptionBudget("desired-owner", 1)
	foreign := testPodDisruptionBudget("foreign-owner", 1)
	client := spltest.NewMockClient()
	client.AddObject(foreign)

	err := ApplyPodDisruptionBudget(t.Context(), client, desired)
	require.ErrorContains(t, err, "not controlled by the desired owner")
	assert.Empty(t, client.Calls["Update"])
}

func testPodDisruptionBudget(ownerUID string, maxUnavailable int32) *policyv1.PodDisruptionBudget {
	controller := true
	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack-indexer",
			Namespace: "test",
			Labels:    map[string]string{"managed": "true"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "enterprise.splunk.com/v4",
				Kind:       "IndexerCluster",
				Name:       "stack",
				UID:        types.UID(ownerUID),
				Controller: &controller,
			}},
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MaxUnavailable: new(intstr.FromInt32(maxUnavailable)),
			Selector:       &metav1.LabelSelector{MatchLabels: map[string]string{"app": "indexer"}},
		},
	}
}
