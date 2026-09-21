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

package indexercluster

import (
	"context"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestNewPodDisruptionBudget(t *testing.T) {
	for _, tc := range []struct {
		name    string
		noahRef *corev1.LocalObjectReference
	}{
		{name: "classic"},
		{name: "noah", noahRef: &corev1.LocalObjectReference{Name: "noah"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cr := &enterpriseApi.IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "stack", Namespace: "test", UID: "owner-uid"},
				Spec:       enterpriseApi.IndexerClusterSpec{NoahClusterRef: tc.noahRef},
			}

			pdb := newPodDisruptionBudget(cr)

			assert.Equal(t, "splunk-stack-indexer", pdb.Name)
			assert.Equal(t, "test", pdb.Namespace)
			assert.Equal(t, map[string]string{"app.kubernetes.io/instance": "splunk-stack-indexer"}, pdb.Spec.Selector.MatchLabels)
			require.NotNil(t, pdb.Spec.MaxUnavailable)
			assert.Equal(t, indexerClusterMaxUnavailable, pdb.Spec.MaxUnavailable.IntValue())
			require.Len(t, pdb.OwnerReferences, 1)
			assert.Equal(t, "IndexerCluster", pdb.OwnerReferences[0].Kind)
			assert.Equal(t, cr.UID, pdb.OwnerReferences[0].UID)
		})
	}
}

func TestApplyPodDisruptionBudget(t *testing.T) {
	cr := &enterpriseApi.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Name: "stack", Namespace: "test", UID: "owner-uid"}}
	client := spltest.NewMockClient()

	require.NoError(t, applyPodDisruptionBudget(t.Context(), client, cr))
	assert.Len(t, client.Calls["Create"], 1)
}

func TestApplyPodDisruptionBudgetReportsFailure(t *testing.T) {
	cr := &enterpriseApi.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Name: "stack", Namespace: "test", UID: "owner-uid", Generation: 2}}
	client := spltest.NewMockClient()
	client.InduceErrorKind[splcommon.MockClientInduceErrorGet] = assert.AnError

	err := applyPodDisruptionBudget(t.Context(), client, cr)

	require.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, enterpriseApi.PhaseError, cr.Status.Phase)
	assert.Equal(t, cr.Generation, cr.Status.ObservedGeneration)
	ready := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionReady)
	require.NotNil(t, ready)
	assert.Equal(t, metav1.ConditionFalse, ready.Status)
	assert.Equal(t, cr.Generation, ready.ObservedGeneration)
	assert.Equal(t, "Failed to create or update PodDisruptionBudget", ready.Message)
}

func TestApplySkipsPodDisruptionBudgetDuringDeletion(t *testing.T) {
	now := metav1.Now()
	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "stack", Namespace: "test", UID: "owner-uid", DeletionTimestamp: &now},
		Spec: enterpriseApi.IndexerClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{ClusterManagerRef: corev1.ObjectReference{Name: "manager"}},
		},
	}
	client := spltest.NewMockClient()
	client.AddObject(cr)

	original := ApplyIndexerClusterManager
	t.Cleanup(func() { ApplyIndexerClusterManager = original })
	ApplyIndexerClusterManager = func(context.Context, splcommon.ControllerClient, *enterpriseApi.IndexerCluster) (reconcile.Result, error) {
		return reconcile.Result{}, nil
	}

	_, err := apply(t.Context(), client, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, record.NewFakeRecorder(1))

	require.NoError(t, err)
	assert.Empty(t, client.Calls["Create"])
}
