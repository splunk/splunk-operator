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

package majorupgradeadapter

import (
	"context"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const teardownFinalizer = "test.cnpg.io/reclaim"

type teardownSQLFake struct {
	slotsGone        bool
	slotCalls        []string
	slotInventories  []teardownSlotInventory
	slotErr          error
	roleCalls        []teardownRoleCall
	roleErr          error
	missingDatabase  bool
	publicationCalls []string
	publicationErr   error
	stopCalls        []string
}

func (s *teardownSQLFake) StopReplication(_ context.Context, source *cnpgv1.Cluster, _ string, _ []string) error {
	s.stopCalls = append(s.stopCalls, source.Name)
	return nil
}

func (s *teardownSQLFake) RemovePublications(_ context.Context, source *cnpgv1.Cluster, _ string, databases []string) error {
	s.publicationCalls = append(s.publicationCalls, databases...)
	return s.publicationErr
}

func (s *teardownSQLFake) DatabaseExists(context.Context, *cnpgv1.Cluster, string) (bool, error) {
	return !s.missingDatabase, nil
}

type teardownRoleCall struct {
	cluster   string
	databases []string
	roles     []string
}

func (s *teardownSQLFake) RemoveSourceSlots(_ context.Context, source *cnpgv1.Cluster, slots teardownSlotInventory) (bool, error) {
	s.slotCalls = append(s.slotCalls, source.Name)
	s.slotInventories = append(s.slotInventories, slots)
	return s.slotsGone, s.slotErr
}

func (s *teardownSQLFake) DropAttemptRoles(_ context.Context, cluster *cnpgv1.Cluster, databases, roles []string) error {
	s.roleCalls = append(s.roleCalls, teardownRoleCall{cluster: cluster.Name, databases: databases, roles: roles})
	return s.roleErr
}

type teardownFixture struct {
	t        *testing.T
	client   client.Client
	teardown *BlueGreenReplicationTeardown
	sql      *teardownSQLFake
	parent   *platformv1alpha1.PostgresCluster
	status   *platformv1alpha1.PostgresBlueGreenUpgradeStatus
}

func healthyTeardownCluster(name, uid string, owner metav1.OwnerReference, labels map[string]string) *cnpgv1.Cluster {
	cluster := &cnpgv1.Cluster{Name: name, Namespace: "default", UID: k8stypes.UID(uid), Labels: labels, OwnerReferences: []metav1.OwnerReference{owner}}
	cluster.Status.Phase = cnpgv1.PhaseHealthy
	cluster.Status.CurrentPrimary = name + "-1"
	cluster.Status.InstancesStatus = map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {name + "-1"}}
	return cluster
}

func newTeardownFixture(t *testing.T, extra ...client.Object) *teardownFixture {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	parent := &platformv1alpha1.PostgresCluster{Name: "orders", Namespace: "default", UID: "parent-uid"}
	owner := *metav1.NewControllerRef(parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))
	attempt := map[string]string{blueGreenAttemptLabel: "attempt"}
	blue := healthyTeardownCluster("blue", "blue-uid", owner, nil)
	green := healthyTeardownCluster("green", "green-uid", owner, attempt)
	green.Spec.ExternalClusters = []cnpgv1.ExternalCluster{{Name: candidateImportExternalClusterName}, {Name: candidateSourceExternalClusterName}}
	owned := func(name, uid string) metav1.ObjectMeta {
		return metav1.ObjectMeta{Name: name, Namespace: "default", UID: k8stypes.UID(uid), Labels: attempt, OwnerReferences: []metav1.OwnerReference{owner}, Finalizers: []string{teardownFinalizer}}
	}
	// CNPG has reconciled both, so its finalizers own their SQL objects.
	subscription := &cnpgv1.Subscription{ObjectMeta: owned("orders-sub", "subscription-uid"), Spec: cnpgv1.SubscriptionSpec{
		ClusterRef: corev1.LocalObjectReference{Name: "green"}, Name: subscriptionName(parent.Name, "orders"), DBName: "orders",
		ReclaimPolicy: cnpgv1.SubscriptionReclaimRetain, Parameters: map[string]string{"slot_name": replicationSlotName(parent.Name, "orders")}}}
	subscription.Generation, subscription.Status.ObservedGeneration = 1, 1
	publication := &cnpgv1.Publication{ObjectMeta: owned("orders-pub", "publication-uid"), Spec: desiredPublicationSpec("blue", "orders", publicationName(parent.Name, "orders"))}
	publication.Spec.ReclaimPolicy = cnpgv1.PublicationReclaimRetain
	publication.Generation, publication.Status.ObservedGeneration = 1, 1
	publication.Status.Applied = new(true)
	objects := append([]client.Object{parent, blue, green, subscription, publication,
		&corev1.Secret{Name: replicationSecretName(parent.Name, "attempt"), Namespace: "default", UID: "replication-secret", Labels: attempt, OwnerReferences: []metav1.OwnerReference{owner}},
		&corev1.Secret{Name: controlSecretName(parent.Name, "attempt"), Namespace: "default", UID: "control-secret", Labels: attempt, OwnerReferences: []metav1.OwnerReference{owner}},
	}, extra...)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	f := &teardownFixture{t: t, client: c, sql: &teardownSQLFake{slotsGone: true}, parent: parent}
	f.teardown = &BlueGreenReplicationTeardown{client: c, reader: c, key: client.ObjectKeyFromObject(parent), owner: parent, sql: f.sql}
	f.status = &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt",
		Blue:                &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: blue.Name, UID: blue.UID}},
		Green:               &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: green.Name, UID: green.UID}},
		DatabaseReplication: []platformv1alpha1.BlueGreenDatabaseReplicationStatus{{DatabaseName: "orders"}},
	}
	return f
}

// release simulates CNPG finishing its reclaim.
func (f *teardownFixture) release(object client.Object) {
	f.t.Helper()
	require.NoError(f.t, f.client.Get(f.t.Context(), client.ObjectKeyFromObject(object), object))
	require.NotNil(f.t, object.GetDeletionTimestamp(), "the teardown must have requested deletion")
	object.SetFinalizers(nil)
	require.NoError(f.t, f.client.Update(f.t.Context(), object))
}

func (f *teardownFixture) exists(object client.Object) bool {
	f.t.Helper()
	err := f.client.Get(f.t.Context(), client.ObjectKeyFromObject(object), object)
	if apierrors.IsNotFound(err) {
		return false
	}
	require.NoError(f.t, err)
	return true
}

func (f *teardownFixture) removeReplication() bool {
	f.t.Helper()
	done, err := f.teardown.RemoveReplication(f.t.Context(), f.status)
	require.NoError(f.t, err)
	return done
}

func TestTeardownRemovesReplicationInDependencyOrder(t *testing.T) {
	f := newTeardownFixture(t)
	subscription := &cnpgv1.Subscription{Name: "orders-sub", Namespace: "default"}
	publication := &cnpgv1.Publication{Name: "orders-pub", Namespace: "default"}

	assert.False(t, f.removeReplication())
	require.True(t, f.exists(publication))
	assert.Nil(t, publication.DeletionTimestamp, "publications wait for every subscription to be dropped")
	assert.False(t, f.removeReplication(), "a subscription still finalizing keeps teardown pending")
	f.release(subscription)

	assert.False(t, f.removeReplication())
	assert.Empty(t, f.sql.slotCalls, "slots are verified only after publications are gone")
	f.release(publication)

	f.sql.slotsGone = false
	assert.False(t, f.removeReplication())
	assert.Equal(t, []string{"blue"}, f.sql.slotCalls)
	green := &cnpgv1.Cluster{Name: "green", Namespace: "default"}
	require.True(t, f.exists(green))
	assert.Len(t, green.Spec.ExternalClusters, 2, "green keeps its replication source until blue has no attempt slot")

	f.sql.slotsGone = true
	assert.False(t, f.removeReplication())
	require.True(t, f.exists(green))
	assert.Equal(t, []cnpgv1.ExternalCluster{{Name: candidateImportExternalClusterName}}, green.Spec.ExternalClusters)
	assert.True(t, f.removeReplication())
	assert.True(t, f.removeReplication(), "a completed teardown stays complete")
}

func TestTeardownRemovesCredentialsAfterReplication(t *testing.T) {
	f := newTeardownFixture(t)
	_, err := f.teardown.RemoveCredentials(t.Context(), f.status)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed, "the replication role must outlive every subscription")
	assert.Empty(t, f.sql.roleCalls)

	for !f.removeReplication() {
		for _, object := range []client.Object{&cnpgv1.Subscription{Name: "orders-sub", Namespace: "default"}, &cnpgv1.Publication{Name: "orders-pub", Namespace: "default"}} {
			if f.exists(object) && object.GetDeletionTimestamp() != nil {
				f.release(object)
			}
		}
	}

	done, err := f.teardown.RemoveCredentials(t.Context(), f.status)
	require.NoError(t, err)
	assert.False(t, done, "Secret deletion is confirmed by a later pass")
	assert.Equal(t, []teardownRoleCall{{cluster: "blue", databases: []string{"orders"},
		roles: []string{replicationRoleName("attempt"), cutoverRoleName("attempt")}}}, f.sql.roleCalls, "retained green is left for cleanup")
	for _, name := range []string{replicationSecretName("orders", "attempt"), controlSecretName("orders", "attempt")} {
		assert.False(t, f.exists(&corev1.Secret{Name: name, Namespace: "default"}), name)
	}

	done, err = f.teardown.RemoveCredentials(t.Context(), f.status)
	require.NoError(t, err)
	assert.True(t, done)
}

func TestTeardownNeverConnectsToGreen(t *testing.T) {
	for name, configure := range map[string]func(*teardownFixture){
		// A window replaced by a retry no longer records green's fence, but
		// green's CNPG spec still rejects the superuser.
		"fenced by an earlier window": func(f *teardownFixture) {
			f.status.Switchover = &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 2}
		},
		"never created": func(f *teardownFixture) { f.status.Green = nil },
		"deleted": func(f *teardownFixture) {
			require.NoError(t, f.client.Delete(t.Context(), &cnpgv1.Cluster{Name: "green", Namespace: "default"}))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newTeardownFixture(t)
			for _, object := range []client.Object{&cnpgv1.Subscription{Name: "orders-sub", Namespace: "default"}, &cnpgv1.Publication{Name: "orders-pub", Namespace: "default"}} {
				require.NoError(t, f.client.Delete(t.Context(), object))
				f.release(object)
			}
			configure(f)
			for !f.removeReplication() {
			}
			_, err := f.teardown.RemoveCredentials(t.Context(), f.status)
			require.NoError(t, err)
			for _, cluster := range f.sql.slotCalls {
				assert.Equal(t, "blue", cluster)
			}
			require.Len(t, f.sql.roleCalls, 1)
			assert.Equal(t, "blue", f.sql.roleCalls[0].cluster)
		})
	}
}

func TestTeardownOfAnAttemptWithoutBlue(t *testing.T) {
	f := newTeardownFixture(t)
	for _, object := range []client.Object{&cnpgv1.Subscription{Name: "orders-sub", Namespace: "default"}, &cnpgv1.Publication{Name: "orders-pub", Namespace: "default"}} {
		require.NoError(t, f.client.Delete(t.Context(), object))
		f.release(object)
	}
	f.status.Blue, f.status.Green = nil, nil
	assert.True(t, f.removeReplication(), "an attempt that never recorded blue has no blue artifacts")
	_, err := f.teardown.RemoveCredentials(t.Context(), f.status)
	require.NoError(t, err)
	assert.Empty(t, f.sql.slotCalls)
	assert.Empty(t, f.sql.roleCalls)
}

func TestTeardownWaitsForAuthoritativeBluePrimary(t *testing.T) {
	f := newTeardownFixture(t)
	blue := &cnpgv1.Cluster{Name: "blue", Namespace: "default"}
	require.True(t, f.exists(blue))
	blue.Status.CurrentPrimary = ""
	require.NoError(t, f.client.Update(t.Context(), blue))
	for _, object := range []client.Object{&cnpgv1.Subscription{Name: "orders-sub", Namespace: "default"}, &cnpgv1.Publication{Name: "orders-pub", Namespace: "default"}} {
		require.NoError(t, f.client.Delete(t.Context(), object))
		f.release(object)
	}

	assert.False(t, f.removeReplication())
	assert.Empty(t, f.sql.slotCalls)
	done, err := f.teardown.RemoveCredentials(t.Context(), f.status)
	require.NoError(t, err)
	assert.False(t, done)
	assert.Empty(t, f.sql.roleCalls)
	assert.True(t, f.exists(&corev1.Secret{Name: replicationSecretName("orders", "attempt"), Namespace: "default"}))
}

func TestTeardownFailsClosedOnForeignArtifacts(t *testing.T) {
	foreign := &platformv1alpha1.PostgresCluster{Name: "other", Namespace: "default", UID: "other-uid"}
	foreignOwner := []metav1.OwnerReference{*metav1.NewControllerRef(foreign, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}
	attempt := map[string]string{blueGreenAttemptLabel: "attempt"}

	t.Run("subscription", func(t *testing.T) {
		f := newTeardownFixture(t, &cnpgv1.Subscription{Name: "foreign-sub", Namespace: "default", UID: "foreign", Labels: attempt, OwnerReferences: foreignOwner})
		_, err := f.teardown.RemoveReplication(t.Context(), f.status)
		require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
		assert.True(t, f.exists(&cnpgv1.Subscription{Name: "foreign-sub", Namespace: "default"}))
	})
	t.Run("secret", func(t *testing.T) {
		f := newTeardownFixture(t)
		secret := &corev1.Secret{Name: controlSecretName("orders", "attempt"), Namespace: "default"}
		require.True(t, f.exists(secret))
		secret.OwnerReferences = foreignOwner
		require.NoError(t, f.client.Update(t.Context(), secret))
		_, err := f.teardown.deleteSecrets(t.Context(), "attempt")
		require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
		assert.True(t, f.exists(secret))
	})
	t.Run("replaced green", func(t *testing.T) {
		f := newTeardownFixture(t)
		f.status.Green.Ref.UID = "previous-green"
		_, err := f.teardown.environment(t.Context(), f.status.Green)
		require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	})
	t.Run("missing blue", func(t *testing.T) {
		f := newTeardownFixture(t)
		require.NoError(t, f.client.Delete(t.Context(), &cnpgv1.Cluster{Name: "blue", Namespace: "default"}))
		for _, object := range []client.Object{&cnpgv1.Subscription{Name: "orders-sub", Namespace: "default"}, &cnpgv1.Publication{Name: "orders-pub", Namespace: "default"}} {
			require.NoError(t, f.client.Delete(t.Context(), object))
			f.release(object)
		}
		_, err := f.teardown.RemoveReplication(t.Context(), f.status)
		require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed, "blue's artifacts cannot be proved removed without blue")
	})
}

func TestTeardownRejectsCommittedAttempt(t *testing.T) {
	f := newTeardownFixture(t)
	f.status.CommitReceipt = &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1}
	_, err := f.teardown.RemoveReplication(t.Context(), f.status)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	_, err = f.teardown.RemoveCredentials(t.Context(), f.status)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	assert.True(t, f.exists(&cnpgv1.Subscription{Name: "orders-sub", Namespace: "default"}))
}

func TestNewBlueGreenReplicationTeardownRequiresPersistedParent(t *testing.T) {
	f := newTeardownFixture(t)
	_, err := NewBlueGreenReplicationTeardown(f.client, f.client, f.teardown.key, &platformv1alpha1.PostgresCluster{Name: "orders", Namespace: "default"})
	require.Error(t, err)
	teardown, err := NewBlueGreenReplicationTeardown(f.client, f.client, f.teardown.key, f.parent)
	require.NoError(t, err)
	assert.NotNil(t, teardown.sql)
}

func TestTeardownRetainsSQLBeforeDeletingRegardlessOfStaleStatus(t *testing.T) {
	for _, observed := range []int64{0, 1} {
		f := newTeardownFixture(t)
		publication := &cnpgv1.Publication{Name: "orders-pub", Namespace: "default"}
		require.True(t, f.exists(publication))
		publication.Spec.ReclaimPolicy = cnpgv1.PublicationReclaimDelete
		publication.Status.ObservedGeneration = observed
		publication.Status.Applied = new(false) // stale failure: SQL creation may have succeeded afterward
		require.NoError(t, f.client.Update(t.Context(), publication))
		require.NoError(t, f.client.Delete(t.Context(), &cnpgv1.Subscription{Name: "orders-sub", Namespace: "default"}))
		f.release(&cnpgv1.Subscription{Name: "orders-sub", Namespace: "default"})
		assert.False(t, f.removeReplication())
		require.True(t, f.exists(publication))
		assert.Equal(t, cnpgv1.PublicationReclaimRetain, publication.Spec.ReclaimPolicy)
		assert.Nil(t, publication.DeletionTimestamp, "retain is persisted before deletion")
		assert.False(t, f.removeReplication(), "wait for the retry's apply acknowledgement")
		require.True(t, f.exists(publication))
		assert.Nil(t, publication.DeletionTimestamp)
		publication.Status.ObservedGeneration = publication.Generation
		publication.Status.Applied = new(true)
		require.NoError(t, f.client.Update(t.Context(), publication))
		assert.False(t, f.removeReplication())
		require.True(t, f.exists(publication))
		require.NotNil(t, publication.DeletionTimestamp)
		f.release(publication)
		assert.False(t, f.removeReplication())
		assert.Equal(t, []string{"orders"}, f.sql.publicationCalls, "SQL is reclaimed even without a successful creation receipt")
	}
}

func TestTeardownRetainsSubscriptionWithOrWithoutSourceSlot(t *testing.T) {
	for _, missing := range []bool{false, true} {
		f := newTeardownFixture(t)
		f.sql.slotsGone = missing
		subscription := &cnpgv1.Subscription{Name: "orders-sub", Namespace: "default"}
		require.True(t, f.exists(subscription))
		subscription.Spec.ReclaimPolicy = cnpgv1.SubscriptionReclaimDelete
		require.NoError(t, f.client.Update(t.Context(), subscription))
		assert.False(t, f.removeReplication())
		require.True(t, f.exists(subscription))
		assert.Equal(t, cnpgv1.SubscriptionReclaimRetain, subscription.Spec.ReclaimPolicy)
		assert.Nil(t, subscription.DeletionTimestamp)
		assert.False(t, f.removeReplication())
		require.True(t, f.exists(subscription))
		require.NotNil(t, subscription.DeletionTimestamp)
	}
}

func TestTeardownValidatesEnvironmentsBeforeAnyMutation(t *testing.T) {
	for _, which := range []string{"blue", "green"} {
		t.Run(which, func(t *testing.T) {
			f := newTeardownFixture(t)
			if which == "blue" {
				f.status.Blue.Ref.UID = "old-blue"
			} else {
				f.status.Green.Ref.UID = "old-green"
			}
			_, err := f.teardown.RemoveReplication(t.Context(), f.status)
			require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
			for _, object := range []client.Object{&cnpgv1.Subscription{Name: "orders-sub", Namespace: "default"}, &cnpgv1.Publication{Name: "orders-pub", Namespace: "default"}} {
				require.True(t, f.exists(object))
				assert.Nil(t, object.GetDeletionTimestamp())
			}
			assert.Empty(t, f.sql.stopCalls)
		})
	}
}

func TestTeardownMissingGreenWaitsForExactOldInstances(t *testing.T) {
	f := newTeardownFixture(t)
	green := &cnpgv1.Cluster{Name: "green", Namespace: "default"}
	require.True(t, f.exists(green))
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "old-green", Namespace: "default", UID: "old-pod", OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(green, cnpgv1.SchemeGroupVersion.WithKind("Cluster"))}}}
	require.NoError(t, f.client.Create(t.Context(), pod))
	require.NoError(t, f.client.Delete(t.Context(), green))
	subscription := &cnpgv1.Subscription{Name: "orders-sub", Namespace: "default"}
	require.True(t, f.exists(subscription))
	subscription.Finalizers = []string{cnpgutils.SubscriptionFinalizerName, "other/finalizer"}
	require.NoError(t, f.client.Update(t.Context(), subscription))
	assert.False(t, f.removeReplication())
	require.True(t, f.exists(subscription))
	assert.Nil(t, subscription.DeletionTimestamp)
	assert.Empty(t, f.sql.stopCalls)
	require.NoError(t, f.client.Delete(t.Context(), pod))
	assert.False(t, f.removeReplication()) // delete the subscription
	assert.False(t, f.removeReplication()) // release only CNPG's finalizer
	require.True(t, f.exists(subscription))
	assert.Equal(t, []string{"other/finalizer"}, subscription.Finalizers)
	f.release(subscription)
	assert.False(t, f.removeReplication())
	f.release(&cnpgv1.Publication{Name: "orders-pub", Namespace: "default"})
	assert.True(t, f.removeReplication())
	assert.NotEmpty(t, f.sql.slotCalls)
}

func TestRecoverGreenRecordsIdentityBeforeExternalSteps(t *testing.T) {
	f := newTeardownFixture(t)
	green := healthyTeardownCluster(candidateClusterName(f.parent.Name, f.status.AttemptID), "unrecorded", *metav1.NewControllerRef(f.parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster")), map[string]string{blueGreenAttemptLabel: f.status.AttemptID})
	require.NoError(t, f.client.Create(t.Context(), green))
	f.status.Green = nil
	changed, err := f.teardown.RecoverGreen(t.Context(), f.status)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, green.UID, f.status.Green.Ref.UID)
	changed, err = f.teardown.RecoverGreen(t.Context(), f.status)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Empty(t, f.sql.stopCalls)
}

func TestTeardownFindsAnUnrecordedGreen(t *testing.T) {
	parent := &platformv1alpha1.PostgresCluster{Name: "orders", Namespace: "default", UID: "parent-uid"}
	owner := *metav1.NewControllerRef(parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))
	name := candidateClusterName(parent.Name, "attempt")
	for test, labels := range map[string]map[string]string{
		"attempt green": {blueGreenAttemptLabel: "attempt"},
		"foreign green": {blueGreenAttemptLabel: "other-attempt"},
		"never created": nil,
	} {
		t.Run(test, func(t *testing.T) {
			var extra []client.Object
			if labels != nil {
				green := healthyTeardownCluster(name, "unrecorded-uid", owner, labels)
				green.Spec.ExternalClusters = []cnpgv1.ExternalCluster{{Name: candidateSourceExternalClusterName}}
				extra = append(extra, green)
			}
			f := newTeardownFixture(t, extra...)
			// The green reference was never persisted.
			f.status.Green = nil
			green, err := f.teardown.AttemptGreen(t.Context(), f.status)
			switch test {
			case "foreign green":
				require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
			case "never created":
				require.NoError(t, err)
				assert.Nil(t, green)
			default:
				require.NoError(t, err)
				require.NotNil(t, green)
				for _, object := range []client.Object{&cnpgv1.Subscription{Name: "orders-sub", Namespace: "default"}, &cnpgv1.Publication{Name: "orders-pub", Namespace: "default"}} {
					require.NoError(t, f.client.Delete(t.Context(), object))
					f.release(object)
				}
				for !f.removeReplication() {
				}
				require.True(t, f.exists(green))
				assert.NotContains(t, green.Spec.ExternalClusters, cnpgv1.ExternalCluster{Name: candidateSourceExternalClusterName},
					"an unrecorded green still loses its replication source")
			}
		})
	}
}

func TestTeardownMissingDatabaseConvergesWithoutAWorkingCNPGFinalizer(t *testing.T) {
	f := newTeardownFixture(t)
	sub := &cnpgv1.Subscription{Name: "orders-sub", Namespace: "default"}
	pub := &cnpgv1.Publication{Name: "orders-pub", Namespace: "default"}
	require.True(t, f.exists(sub))
	require.True(t, f.exists(pub))
	sub.Finalizers = []string{cnpgutils.SubscriptionFinalizerName}
	sub.Spec.ReclaimPolicy = cnpgv1.SubscriptionReclaimDelete
	pub.Finalizers = []string{cnpgutils.PublicationFinalizerName}
	pub.Spec.ReclaimPolicy = cnpgv1.PublicationReclaimDelete
	pub.Status.Applied = new(false)
	pub.Status.ObservedGeneration = 0
	require.NoError(t, f.client.Update(t.Context(), sub))
	require.NoError(t, f.client.Update(t.Context(), pub))
	f.sql.missingDatabase = true
	done := false
	for range 12 {
		done = f.removeReplication()
		if done {
			break
		}
	}
	require.True(t, done, "teardown must release retained CNPG finalizers without manual intervention")
	assert.False(t, f.exists(pub))
	assert.False(t, f.exists(sub))
	assert.NotEmpty(t, f.sql.publicationCalls)
	assert.NotEmpty(t, f.sql.slotCalls)
	for range 3 {
		done, err := f.teardown.RemoveCredentials(t.Context(), f.status)
		require.NoError(t, err)
		if done {
			return
		}
	}
	t.Fatal("credential cleanup did not converge")
}

func TestTeardownDeleteRejectsConcurrentReclaimPolicyChange(t *testing.T) {
	f := newTeardownFixture(t)
	stale := &cnpgv1.Publication{Name: "orders-pub", Namespace: "default"}
	require.True(t, f.exists(stale))
	concurrent := stale.DeepCopy()
	concurrent.Spec.ReclaimPolicy = cnpgv1.PublicationReclaimDelete
	require.NoError(t, f.client.Update(t.Context(), concurrent))
	require.Error(t, f.teardown.deleteExact(t.Context(), stale), "the delete must reject a new resource version with SQL reclamation reenabled")
	require.True(t, f.exists(concurrent))
	assert.Nil(t, concurrent.DeletionTimestamp)
}

func TestTeardownPersistsSubscriberIdentityForMissingGreenRecovery(t *testing.T) {
	f := newTeardownFixture(t)
	green := &cnpgv1.Cluster{Name: "green", Namespace: "default"}
	require.True(t, f.exists(green))
	green.Status.SystemID = "111"
	require.NoError(t, f.client.Update(t.Context(), green))
	inventory, recorded, err := f.teardown.sourceSlotInventory(t.Context(), f.status, green)
	require.NoError(t, err)
	assert.Equal(t, "111", inventory.systemID)
	secret := &corev1.Secret{Name: replicationSecretName(f.parent.Name, f.status.AttemptID), Namespace: "default"}
	require.True(t, f.exists(secret))
	assert.True(t, recorded, "persist identity before external cleanup")
	parent := &platformv1alpha1.PostgresCluster{Name: f.parent.Name, Namespace: f.parent.Namespace}
	require.True(t, f.exists(parent))
	assert.Equal(t, f.status.AttemptID+"/green-uid/111", parent.Annotations[teardownSubscriberAnnotation])
	require.NoError(t, f.client.Delete(t.Context(), green))
	inventory, recorded, err = f.teardown.sourceSlotInventory(t.Context(), f.status, nil)
	require.NoError(t, err)
	assert.Equal(t, "111", inventory.systemID, "retries retain ownership proof after green disappears")
	assert.False(t, recorded, "the existing parent receipt needs no further write")
	f.status.Green.Ref.UID = "replacement"
	_, _, err = f.teardown.sourceSlotInventory(t.Context(), f.status, nil)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
}

func TestTeardownInterruptedInitialCopyRemovesSlotsBeforeCredentials(t *testing.T) {
	f := newTeardownFixture(t)
	h := newTeardownSQLHarness(t, false)
	green := &cnpgv1.Cluster{Name: "green", Namespace: "default"}
	require.True(t, f.exists(green))
	green.Status.SystemID = "111"
	require.NoError(t, f.client.Update(t.Context(), green))
	syncSlot := "pg_10_sync_20_111"
	mainSlot := replicationSlotName(f.parent.Name, "orders")
	h.primary.slots = map[string]bool{mainSlot: false, syncSlot: false}
	h.primary.syncSlots = map[string]string{syncSlot: "orders"}
	h.primary.roles = map[string]bool{replicationRoleName(f.status.AttemptID): false, cutoverRoleName(f.status.AttemptID): false}
	f.teardown.sql = h.sql
	for _, object := range []client.Object{&cnpgv1.Subscription{Name: "orders-sub", Namespace: "default"}, &cnpgv1.Publication{Name: "orders-pub", Namespace: "default"}} {
		require.True(t, f.exists(object))
		if object.GetName() == "orders-sub" {
			object.SetFinalizers([]string{cnpgutils.SubscriptionFinalizerName})
		} else {
			object.SetFinalizers([]string{cnpgutils.PublicationFinalizerName})
		}
		require.NoError(t, f.client.Update(t.Context(), object))
	}
	done := false
	for range 12 {
		if done = f.removeReplication(); done {
			break
		}
	}
	require.True(t, done)
	assert.Empty(t, h.primary.slots, "no synchronization slot may remain after replication teardown")
	// Recheck before deleting credentials: a late-discovered copy slot must also be removed.
	h.primary.slots[syncSlot] = false
	done, err := f.teardown.RemoveCredentials(t.Context(), f.status)
	require.NoError(t, err)
	assert.False(t, done)
	assert.Empty(t, h.primary.slots)
	assert.Empty(t, h.primary.roles)
	assert.False(t, f.exists(&corev1.Secret{Name: replicationSecretName(f.parent.Name, f.status.AttemptID), Namespace: "default"}))
}

func TestTeardownPreservesCredentialsWhileSynchronizationSlotsAreUnproved(t *testing.T) {
	f := newTeardownFixture(t)
	sub := &cnpgv1.Subscription{Name: "orders-sub", Namespace: "default"}
	require.NoError(t, f.client.Delete(t.Context(), sub))
	f.release(sub)
	f.sql.slotErr = mvutypes.ErrUpgradeFlowFailed
	done, err := f.teardown.RemoveCredentials(t.Context(), f.status)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	assert.False(t, done)
	assert.Empty(t, f.sql.roleCalls)
	assert.True(t, f.exists(&corev1.Secret{Name: replicationSecretName(f.parent.Name, f.status.AttemptID), Namespace: "default"}))
}

func TestTeardownCredentialConfirmationPreservesForeignSyncSlotsWithoutGreen(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "parent receipt"
		if legacy {
			name = "legacy Secret receipt"
		}
		t.Run(name, func(t *testing.T) {
			f := newTeardownFixture(t)
			h := newTeardownSQLHarness(t, false)
			green := &cnpgv1.Cluster{Name: "green", Namespace: "default"}
			require.True(t, f.exists(green))
			green.Status.SystemID = "111"
			require.NoError(t, f.client.Update(t.Context(), green))
			secret := &corev1.Secret{Name: replicationSecretName(f.parent.Name, f.status.AttemptID), Namespace: "default"}
			if legacy {
				require.True(t, f.exists(secret))
				secret.Annotations = map[string]string{teardownSubscriberAnnotation: "green-uid/111"}
				require.NoError(t, f.client.Update(t.Context(), secret))
			} else {
				_, recorded, err := f.teardown.sourceSlotInventory(t.Context(), f.status, green)
				require.NoError(t, err)
				require.True(t, recorded)
			}
			require.NoError(t, f.client.Delete(t.Context(), green))
			for _, object := range []client.Object{&cnpgv1.Subscription{Name: "orders-sub", Namespace: "default"}, &cnpgv1.Publication{Name: "orders-pub", Namespace: "default"}} {
				require.NoError(t, f.client.Delete(t.Context(), object))
				f.release(object)
			}
			own, foreign := "pg_10_sync_20_111", "pg_10_sync_20_222"
			h.primary.slots = map[string]bool{own: false, foreign: false}
			h.primary.syncSlots = map[string]string{own: "orders", foreign: "orders"}
			h.primary.roles = map[string]bool{replicationRoleName(f.status.AttemptID): false, cutoverRoleName(f.status.AttemptID): false}
			f.teardown.sql = h.sql
			if legacy {
				done, err := f.teardown.RemoveCredentials(t.Context(), f.status)
				require.NoError(t, err)
				assert.False(t, done, "persist the migrated receipt and return before cleanup")
				assert.True(t, f.exists(secret))
				assert.NotEmpty(t, h.primary.roles)
				assert.Contains(t, h.primary.slots, own)
			}
			done, err := f.teardown.RemoveCredentials(t.Context(), f.status)
			require.NoError(t, err)
			assert.False(t, done, "this pass deletes Secrets and requires confirmation")
			assert.NotContains(t, h.primary.slots, own)
			assert.Contains(t, h.primary.slots, foreign)
			assert.Empty(t, h.primary.roles)
			assert.False(t, f.exists(secret))
			// A fresh adapter must recover from the API, without a retained owner object.
			recovered := *f.teardown
			recovered.owner = f.parent.DeepCopy()
			done, err = recovered.RemoveCredentials(t.Context(), f.status)
			require.NoError(t, err)
			assert.True(t, done, "confirmation retains identity after Secret deletion")
			assert.Contains(t, h.primary.slots, foreign)
		})
	}
}

func TestTeardownSubscriberReceiptIsScopedToParentAndAttempt(t *testing.T) {
	for _, replaced := range []bool{false, true} {
		f := newTeardownFixture(t)
		parent := &platformv1alpha1.PostgresCluster{Name: f.parent.Name, Namespace: f.parent.Namespace}
		require.True(t, f.exists(parent))
		parent.Annotations = map[string]string{teardownSubscriberAnnotation: "other-attempt/green-uid/111"}
		if replaced {
			require.NoError(t, f.client.Delete(t.Context(), parent))
			parent.UID = "replacement-parent"
			parent.ResourceVersion = ""
			require.NoError(t, f.client.Create(t.Context(), parent))
		} else {
			require.NoError(t, f.client.Update(t.Context(), parent))
		}
		inventory, recorded, err := f.teardown.sourceSlotInventory(t.Context(), f.status, nil)
		assert.False(t, recorded)
		if replaced {
			require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
		} else {
			require.NoError(t, err)
			assert.Empty(t, inventory.systemID, "another attempt's receipt cannot authorize slot cleanup")
		}
	}
}
