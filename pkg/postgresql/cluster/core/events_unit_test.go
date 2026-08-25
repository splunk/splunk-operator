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
package core

import (
	"context"
	"testing"
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestEmitClusterPhaseTransitionEmitsReadyEvent(t *testing.T) {
	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "pg1",
			Namespace:         "default",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Minute)),
		},
	}
	rc := &ReconcileContext{Recorder: record.NewFakeRecorder(2)}

	rc.emitClusterPhaseTransition(cluster, string(provisioningClusterPhase), string(readyClusterPhase), "", "")
	rc.emitClusterPhaseTransition(cluster, string(readyClusterPhase), string(readyClusterPhase), "", "")

	select {
	case event := <-rc.Recorder.(*record.FakeRecorder).Events:
		assert.Contains(t, event, EventClusterReady)
	default:
		t.Fatal("expected ClusterReady event")
	}
}

func TestSetPhaseStatusCompletesReadinessCycleOnce(t *testing.T) {
	ctx := context.Background()
	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "pg1",
			Namespace:         "default",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Minute)),
		},
	}
	c := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithStatusSubresource(&platformv1alpha1.PostgresCluster{}).
		WithObjects(cluster).
		Build()

	require.NoError(t, setStatus(
		ctx,
		c,
		nil,
		cluster,
		cluster.Status.DeepCopy(),
		clusterReady,
		metav1.ConditionFalse,
		reasonCNPGProvisioning,
		"initial provisioning",
		provisioningClusterPhase,
	))
	require.NotNil(t, cluster.Status.LastTransitionTime)
	lastTransitionTime := *cluster.Status.LastTransitionTime

	started := &platformv1alpha1.PostgresCluster{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, started))
	require.NotNil(t, started.Status.LastTransitionTime)
	assert.Equal(t, lastTransitionTime, *started.Status.LastTransitionTime)

	duration, completedReadinessCycle, err := setPhaseStatus(ctx, c, started, readyClusterPhase)
	require.NoError(t, err)
	require.True(t, completedReadinessCycle)
	assert.Positive(t, duration)
	assert.Nil(t, started.Status.LastTransitionTime)

	stored := &platformv1alpha1.PostgresCluster{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, stored))
	assert.Nil(t, stored.Status.LastTransitionTime)

	duration, completedReadinessCycle, err = setPhaseStatus(ctx, c, stored, readyClusterPhase)
	require.NoError(t, err)
	assert.False(t, completedReadinessCycle)
	assert.Zero(t, duration)
}

func TestStartReadinessCycleForActiveUseCase(t *testing.T) {
	ctx := context.Background()
	ready := string(readyClusterPhase)
	generation := int64(1)
	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "pg1",
			Namespace:         "default",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Minute)),
			Generation:        2,
		},
		Status: platformv1alpha1.PostgresClusterStatus{
			Phase:              &ready,
			ObservedGeneration: &generation,
		},
	}
	c := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithStatusSubresource(&platformv1alpha1.PostgresCluster{}).
		WithObjects(cluster).
		Build()

	require.NoError(t, startReadinessCycle(ctx, c, cluster))
	require.NotNil(t, cluster.Status.LastTransitionTime)
	assert.Equal(t, ready, *cluster.Status.Phase)

	stored := &platformv1alpha1.PostgresCluster{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, stored))
	require.NotNil(t, stored.Status.LastTransitionTime)
	assert.Equal(t, ready, *stored.Status.Phase)
}

func TestSetPhaseStatusDoesNotCompleteReadinessCycleWhenStatusWriteFails(t *testing.T) {
	ctx := context.Background()
	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "pg1",
			Namespace:         "default",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Minute)),
		},
	}
	baseClient := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithStatusSubresource(&platformv1alpha1.PostgresCluster{}).
		WithObjects(cluster).
		Build()

	require.NoError(t, setStatus(
		ctx,
		baseClient,
		nil,
		cluster,
		cluster.Status.DeepCopy(),
		clusterReady,
		metav1.ConditionFalse,
		reasonCNPGProvisioning,
		"initial provisioning",
		provisioningClusterPhase,
	))

	started := &platformv1alpha1.PostgresCluster{}
	require.NoError(t, baseClient.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, started))
	lastTransitionTime := *started.Status.LastTransitionTime

	failingClient := interceptor.NewClient(baseClient, interceptor.Funcs{
		SubResourceUpdate: func(_ context.Context, _ client.Client, subResourceName string, _ client.Object, _ ...client.SubResourceUpdateOption) error {
			if subResourceName == "status" {
				return assert.AnError
			}
			return nil
		},
	})

	duration, completedReadinessCycle, err := setPhaseStatus(ctx, failingClient, started, readyClusterPhase)
	require.ErrorIs(t, err, assert.AnError)
	assert.Zero(t, duration)
	assert.False(t, completedReadinessCycle, "the caller must not observe a failed Ready status write")

	persisted := &platformv1alpha1.PostgresCluster{}
	require.NoError(t, baseClient.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, persisted))
	require.NotNil(t, persisted.Status.LastTransitionTime)
	assert.Equal(t, lastTransitionTime, *persisted.Status.LastTransitionTime)
}
