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

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestClusterStateStoreRejectsInvalidPortsAtConstruction(t *testing.T) {
	c := fake.NewClientBuilder().Build()
	key := client.ObjectKey{Namespace: "default", Name: "orders"}
	for _, test := range []struct {
		name   string
		writer client.Client
		reader client.Reader
		key    client.ObjectKey
	}{
		{"nil writer", nil, c, key},
		{"nil reader", c, nil, key},
		{"missing name", c, c, client.ObjectKey{Namespace: key.Namespace}},
		{"missing namespace", c, c, client.ObjectKey{Name: key.Name}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := NewClusterStateStoreWithReader(test.writer, test.reader, test.key)
			assert.Error(t, err)
			assert.Nil(t, store)
		})
	}
	var zero ClusterStateStore
	_, err := zero.GetSourcePgVersion(t.Context())
	assert.Error(t, err)
}

// GetSourcePgVersion reads the running major straight from the live CNPG cluster
// (PGDataImageInfo.MajorVersion), not the provisioner's status.CurrentPgVersion
// projection — decoupling the major-upgrade use case from the provisioner.
func TestGetSourcePgVersionReadsLiveCNPGMajorVersion(t *testing.T) {
	ctx := t.Context()
	key := client.ObjectKey{Name: "pg1", Namespace: "default"}
	c := newCNPGTestClient(t, &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
		Status: cnpgv1.ClusterStatus{
			PGDataImageInfo: &cnpgv1.ImageInfo{
				Image:        "ghcr.io/cloudnative-pg/postgresql:15.10",
				MajorVersion: 15,
			},
		},
	})

	version, err := newTestClusterStateStore(t, c, key).GetSourcePgVersion(ctx)
	require.NoError(t, err)
	assert.Equal(t, "15", version)
}

// A not-yet-created CNPG cluster reports no source version (not an error): the
// upgrade simply has nothing to act on yet and Prerequisites defers it.
func TestGetSourcePgVersionReturnsEmptyWhenCNPGMissing(t *testing.T) {
	ctx := t.Context()
	key := client.ObjectKey{Name: "pg1", Namespace: "default"}
	c := newCNPGTestClient(t)

	version, err := newTestClusterStateStore(t, c, key).GetSourcePgVersion(ctx)
	require.NoError(t, err)
	assert.Empty(t, version)
}

// A CNPG cluster that has not yet published PGDataImageInfo reports no source
// version rather than a spurious "0".
func TestGetSourcePgVersionReturnsEmptyWhenImageInfoUnset(t *testing.T) {
	ctx := t.Context()
	key := client.ObjectKey{Name: "pg1", Namespace: "default"}
	c := newCNPGTestClient(t, &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
	})

	version, err := newTestClusterStateStore(t, c, key).GetSourcePgVersion(ctx)
	require.NoError(t, err)
	assert.Empty(t, version)
}

func TestGetReadyToStartRequiresReadyParentAndFullSourceTopology(t *testing.T) {
	key := client.ObjectKey{Name: "orders", Namespace: "default"}
	phase := "Ready"
	generation := int64(3)
	parent := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace, Generation: generation},
		Status: platformv1alpha1.PostgresClusterStatus{
			Phase: &phase, ObservedGeneration: &generation, Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllComponentsReady", LastTransitionTime: metav1.Now()}},
			ProvisionerRef: &corev1.ObjectReference{Name: "blue", Namespace: key.Namespace, UID: "blue-uid"},
		},
	}
	source := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "blue", Namespace: key.Namespace, UID: "blue-uid"},
		Spec:       cnpgv1.ClusterSpec{Instances: 3},
		Status: cnpgv1.ClusterStatus{
			Phase: cnpgv1.PhaseHealthy, Instances: 3, ReadyInstances: 3, CurrentPrimary: "blue-1",
			InstancesStatus: map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"blue-1", "blue-2", "blue-3"}},
		},
	}
	ready, err := newTestClusterStateStore(t, newCNPGTestClient(t, parent, source), key).GetReadyToStart(t.Context())
	require.NoError(t, err)
	assert.True(t, ready)

	degraded := source.DeepCopy()
	degraded.Status.ReadyInstances = 1
	degraded.Status.InstancesStatus[cnpgv1.PodHealthy] = []string{"blue-1"}
	ready, err = newTestClusterStateStore(t, newCNPGTestClient(t, parent, degraded), key).GetReadyToStart(t.Context())
	require.NoError(t, err)
	assert.False(t, ready)

	notReady := parent.DeepCopy()
	notReady.Status.Conditions[0].Status = metav1.ConditionFalse
	ready, err = newTestClusterStateStore(t, newCNPGTestClient(t, notReady, source), key).GetReadyToStart(t.Context())
	require.NoError(t, err)
	assert.False(t, ready)

	stale := parent.DeepCopy()
	stale.Generation++
	ready, err = newTestClusterStateStore(t, newCNPGTestClient(t, stale, source), key).GetReadyToStart(t.Context())
	require.NoError(t, err)
	assert.False(t, ready)
}

func newCNPGTestClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()
}

func TestUpdateMajorUpgradeStatusRechecksCommitAfterConcurrentWrite(t *testing.T) {
	key := client.ObjectKey{Name: "orders", Namespace: "default"}
	parent := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
		Status: platformv1alpha1.PostgresClusterStatus{
			ProvisionerRef: &corev1.ObjectReference{Name: "blue"},
			PostgresMajorUpgradeStatus: []platformv1alpha1.PostgresMajorUpgradeStatus{{
				BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt"},
			}},
		},
	}
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	base := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(parent).WithObjects(parent).Build()
	concurrentWrite := false
	c := interceptor.NewClient(base, interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, c client.Client, subResource string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			if !concurrentWrite {
				concurrentWrite = true
				latest := new(platformv1alpha1.PostgresCluster)
				if err := c.Get(ctx, key, latest); err != nil {
					return err
				}
				latest.Status.ProvisionerRef = &corev1.ObjectReference{Name: "green"}
				latest.Status.PostgresMajorUpgradeStatus[0].BlueGreen.CommitReceipt = &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1}
				if err := c.Status().Update(ctx, latest); err != nil {
					return err
				}
			}
			return c.SubResource(subResource).Update(ctx, obj, opts...)
		},
	})
	merges := 0
	err := newTestClusterStateStore(t, c, key).UpdateMajorUpgradeStatus(t.Context(), func(entries []platformv1alpha1.PostgresMajorUpgradeStatus) ([]platformv1alpha1.PostgresMajorUpgradeStatus, error) {
		merges++
		entries[0].Phase = new("SwitchingOver")
		return entries, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, merges)
	latest := new(platformv1alpha1.PostgresCluster)
	require.NoError(t, base.Get(t.Context(), key, latest))
	assert.Equal(t, "green", latest.Status.ProvisionerRef.Name)
	assert.Equal(t, int64(1), latest.Status.PostgresMajorUpgradeStatus[0].BlueGreen.CommitReceipt.Sequence)
	assert.Equal(t, "SwitchingOver", *latest.Status.PostgresMajorUpgradeStatus[0].Phase)
}

func newTestClusterStateStore(t *testing.T, writer client.Client, key client.ObjectKey) *ClusterStateStore {
	t.Helper()
	store, err := NewClusterStateStore(writer, key)
	require.NoError(t, err)
	return store
}
