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

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	dbcnpgadapter "github.com/splunk/splunk-operator/pkg/postgresql/database/adapter/cnpg"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func childRetirementFixture(t *testing.T) (client.Client, *ReconcileContext, *platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresDatabase, *cnpgv1.Database) {
	t.Helper()
	now := metav1.Now()
	parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "ns", UID: "parent-uid"}}
	owner := []metav1.OwnerReference{*metav1.NewControllerRef(parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}
	active := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "blue", Namespace: "ns", UID: "blue-uid", OwnerReferences: owner}, Status: cnpgv1.ClusterStatus{WriteService: "blue-rw"}}
	retired := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "green", Namespace: "ns", UID: "green-uid", OwnerReferences: owner}}
	ref := func(cluster *cnpgv1.Cluster) corev1.ObjectReference {
		return corev1.ObjectReference{APIVersion: cnpgv1.SchemeGroupVersion.String(), Kind: "Cluster", Name: cluster.Name, Namespace: cluster.Namespace, UID: cluster.UID}
	}
	blueRef, greenRef := ref(active), ref(retired)
	parent.Status.ProvisionerRef = &blueRef
	parent.Status.Resources = &platformv1alpha1.PostgresClusterResources{ConfigMapRef: &corev1.LocalObjectReference{Name: "canonical"}}
	child := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: "ns", UID: "child-uid", Generation: 2},
		Spec:       platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: parent.Name}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "app"}}},
		Status:     platformv1alpha1.PostgresDatabaseStatus{Phase: new("Ready")},
	}
	childOwner := []metav1.OwnerReference{*metav1.NewControllerRef(child, platformv1alpha1.GroupVersion.WithKind("PostgresDatabase"))}
	declaration := &cnpgv1.Database{
		ObjectMeta: metav1.ObjectMeta{Name: "arbitrary-old-name", Namespace: "ns", UID: "old-database-uid", OwnerReferences: childOwner, Finalizers: []string{"cnpg.io/database"}},
		Spec:       cnpgv1.DatabaseSpec{Name: "app", ClusterRef: corev1.LocalObjectReference{Name: retired.Name}, ReclaimPolicy: cnpgv1.DatabaseReclaimDelete},
	}
	current := declaration.DeepCopy()
	current.Name = "active-db"
	current.UID = "active-database-uid"
	current.Spec.ClusterRef.Name = active.Name
	bg := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		AttemptID: "attempt", Blue: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: blueRef}, Green: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: greenRef},
		Gates: &platformv1alpha1.BlueGreenGateStatus{
			Cleanup:      &platformv1alpha1.BlueGreenGateConsumptionStatus{Generation: 1, ConsumedAt: &now},
			Cancellation: &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: &now, SourceAccessRestoredAt: &now, ReplicationRemovedAt: &now, CredentialsRemovedAt: &now, GreenFencedAt: &now, CompletedAt: &now},
		},
		Cleanup: &platformv1alpha1.BlueGreenCleanupStatus{
			State: platformv1alpha1.BlueGreenCleanupStateCleaning, StartedAt: &now, RetirementGeneration: new(int64(1)), EnvironmentRef: &greenRef,
			RetirementAcknowledgements: []platformv1alpha1.BlueGreenRetirementAcknowledgementStatus{{
				PostgresDatabaseName: child.Name, PostgresDatabaseUID: string(child.UID), PostgresDatabaseGeneration: child.Generation, DatabaseName: "app",
				Resources: []corev1.ObjectReference{{APIVersion: cnpgv1.SchemeGroupVersion.String(), Kind: "Database", Namespace: "ns", Name: declaration.Name, UID: declaration.UID}},
			}},
		},
	}
	parent.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{Phase: new("Cancelled"), Strategy: new("blueGreen"), BlueGreen: bg}}
	canonical := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "canonical", Namespace: "ns", OwnerReferences: owner}, Data: map[string]string{pgconninfo.KeyClusterRWEndpoint: "blue-rw.ns.svc.cluster.local"}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "application", Namespace: "ns", UID: "secret-uid", OwnerReferences: childOwner}}
	scheme := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(parent, child).WithObjects(parent, child, active, retired, declaration, current, canonical, secret).Build()
	rc := &ReconcileContext{Client: c, APIReader: c, Scheme: scheme, EnvironmentRetirer: dbcnpgadapter.NewEnvironmentRetirer(c, c)}
	return c, rc, parent, child, declaration
}

func TestChildRetirementWaitsForAbsenceAndPreservesActiveResources(t *testing.T) {
	c, rc, parent, child, declaration := childRetirementFixture(t)
	pass := func() {
		t.Helper()
		require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(child), child))
		result, err, handled := (&databaseReconciliation{rc: rc, db: child}).reconcileRetirement(t.Context())
		require.NoError(t, err)
		require.True(t, handled)
		assert.Positive(t, result.RequeueAfter)
	}
	pass()
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(declaration), declaration))
	assert.Equal(t, cnpgv1.DatabaseReclaimRetain, declaration.Spec.ReclaimPolicy)
	assert.Nil(t, declaration.DeletionTimestamp)
	pass()
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(declaration), declaration))
	require.NotNil(t, declaration.DeletionTimestamp)
	pass()
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(child), child))
	assert.Nil(t, child.Status.EnvironmentRetirement)
	declaration.Finalizers = nil
	require.NoError(t, c.Update(t.Context(), declaration))
	pass()
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(child), child))
	proof := child.Status.EnvironmentRetirement
	require.NotNil(t, proof)
	assert.Equal(t, parent.UID, proof.ParentUID)
	assert.Equal(t, "attempt", proof.AttemptID)
	assert.Equal(t, child.Generation, proof.ObservedDatabaseGeneration)
	require.Len(t, proof.Databases, 1)
	assert.Equal(t, declaration.UID, proof.Databases[0].Resources[0].UID)
	assert.NotNil(t, proof.Databases[0].AcknowledgedAt)
	assert.Equal(t, "Ready", *child.Status.Phase)
	version := child.ResourceVersion
	_, err, handled := (&databaseReconciliation{rc: rc, db: child}).reconcileRetirement(t.Context())
	require.NoError(t, err)
	assert.False(t, handled)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(child), child))
	assert.Equal(t, version, child.ResourceVersion)
	assert.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKeyFromObject(declaration), &cnpgv1.Database{})))
	active := &cnpgv1.Database{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKey{Namespace: "ns", Name: "active-db"}, active))
	assert.Equal(t, cnpgv1.DatabaseReclaimDelete, active.Spec.ReclaimPolicy)
	require.NoError(t, c.Get(t.Context(), client.ObjectKey{Namespace: "ns", Name: "application"}, &corev1.Secret{}))
}

func TestChildRetirementCannotMutateAfterIntentChanges(t *testing.T) {
	c, rc, parent, child, declaration := childRetirementFixture(t)
	parentReads := 0
	guarded := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
		Get: func(ctx context.Context, next client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*platformv1alpha1.PostgresCluster); ok {
				parentReads++
				if parentReads == 2 {
					current := &platformv1alpha1.PostgresCluster{}
					require.NoError(t, next.Get(ctx, client.ObjectKeyFromObject(parent), current))
					*current.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Cleanup.RetirementGeneration++
					require.NoError(t, next.Status().Update(ctx, current))
				}
			}
			return next.Get(ctx, key, obj, opts...)
		},
	})
	rc.APIReader = guarded
	rc.EnvironmentRetirer = dbcnpgadapter.NewEnvironmentRetirer(c, guarded)
	_, err, handled := (&databaseReconciliation{rc: rc, db: child}).reconcileRetirement(t.Context())
	require.True(t, handled)
	require.ErrorContains(t, err, "intent changed")
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(declaration), declaration))
	assert.Equal(t, cnpgv1.DatabaseReclaimDelete, declaration.Spec.ReclaimPolicy)
	assert.Nil(t, declaration.DeletionTimestamp)
}

func TestChildRetirementRejectsReclaimPolicyChangeBeforeDelete(t *testing.T) {
	c, rc, _, child, declaration := childRetirementFixture(t)
	_, err, handled := (&databaseReconciliation{rc: rc, db: child}).reconcileRetirement(t.Context())
	require.NoError(t, err)
	require.True(t, handled)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(declaration), declaration))
	require.Equal(t, cnpgv1.DatabaseReclaimRetain, declaration.Spec.ReclaimPolicy)
	retainedVersion := declaration.ResourceVersion
	deletes := 0
	racing := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
		Delete: func(ctx context.Context, next client.WithWatch, object client.Object, opts ...client.DeleteOption) error {
			if object.GetUID() == declaration.UID {
				deletes++
				live := &cnpgv1.Database{}
				require.NoError(t, next.Get(ctx, client.ObjectKeyFromObject(object), live))
				live.Spec.ReclaimPolicy = cnpgv1.DatabaseReclaimDelete
				require.NoError(t, next.Update(ctx, live))
				assert.NotEqual(t, retainedVersion, live.ResourceVersion)
			}
			return next.Delete(ctx, object, opts...)
		},
	})
	rc.EnvironmentRetirer = dbcnpgadapter.NewEnvironmentRetirer(racing, c)
	_, err, handled = (&databaseReconciliation{rc: rc, db: child}).reconcileRetirement(t.Context())
	require.True(t, handled)
	require.True(t, apierrors.IsConflict(err), "the backing client must reject stale deletion preconditions: %v", err)
	assert.Equal(t, 1, deletes)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(declaration), declaration))
	assert.Equal(t, cnpgv1.DatabaseReclaimDelete, declaration.Spec.ReclaimPolicy)
	assert.Nil(t, declaration.DeletionTimestamp)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(child), child))
	assert.Nil(t, child.Status.EnvironmentRetirement)
}

func TestChildRetirementGuardRejectsDrift(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(context.Context, client.Client, *platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresDatabase) error
	}{
		{"child generation", func(ctx context.Context, c client.Client, p *platformv1alpha1.PostgresCluster, d *platformv1alpha1.PostgresDatabase) error {
			d.Generation++
			return c.Update(ctx, d)
		}},
		{"authority", func(ctx context.Context, c client.Client, p *platformv1alpha1.PostgresCluster, d *platformv1alpha1.PostgresDatabase) error {
			p.Status.ProvisionerRef = p.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Green.Ref.DeepCopy()
			return c.Status().Update(ctx, p)
		}},
		{"routing", func(ctx context.Context, c client.Client, p *platformv1alpha1.PostgresCluster, d *platformv1alpha1.PostgresDatabase) error {
			cm := &corev1.ConfigMap{}
			if err := c.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: "canonical"}, cm); err != nil {
				return err
			}
			cm.Data[pgconninfo.KeyClusterRWEndpoint] = "green-rw.ns.svc.cluster.local"
			return c.Update(ctx, cm)
		}},
		{"environment replacement", func(ctx context.Context, c client.Client, p *platformv1alpha1.PostgresCluster, d *platformv1alpha1.PostgresDatabase) error {
			cluster := &cnpgv1.Cluster{}
			if err := c.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: "green"}, cluster); err != nil {
				return err
			}
			if err := c.Delete(ctx, cluster); err != nil {
				return err
			}
			cluster.ResourceVersion = ""
			cluster.UID = "replacement"
			return c.Create(ctx, cluster)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, rc, parent, child, _ := childRetirementFixture(t)
			originalChild := child.DeepCopy()
			entry := *parent.Status.PostgresMajorUpgradeStatus[0].DeepCopy()
			require.NoError(t, test.change(t.Context(), c, parent, child))
			_, err := verifyRetirementRequest(t.Context(), rc.APIReader, parent, originalChild, entry)
			require.Error(t, err)
		})
	}
}
