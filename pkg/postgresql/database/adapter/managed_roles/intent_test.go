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

package managedroles

import (
	"context"
	"errors"
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestIntentPublisherUpdatesRoleProjectionWithoutAdvancingObservedGeneration(t *testing.T) {
	scheme := managedRolesScheme(t)
	database := managedRolesDatabase()
	observedGeneration := int64(2)
	database.Status.ObservedGeneration = &observedGeneration
	database.Status.Databases = []platformv1alpha1.DatabaseInfo{fullyPopulatedDatabaseInfo("payments")}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&platformv1alpha1.PostgresDatabase{}).
		WithObjects(database).
		Build()

	err := NewIntentPublisher(c, database).Publish(t.Context(), managedRolePublication())
	require.NoError(t, err)

	stored := &platformv1alpha1.PostgresDatabase{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(database), stored))
	require.NotNil(t, stored.Status.ObservedGeneration)
	assert.Equal(t, observedGeneration, *stored.Status.ObservedGeneration)
	require.Len(t, stored.Status.Databases, 2)
	payments := stored.Status.Databases[0]
	assert.False(t, payments.Ready)
	assert.Equal(t, "keep", payments.Message)
	assert.Equal(t, "tenant-payments", payments.DatabaseRef.Name)
	assert.Equal(t, "admin-secret", payments.AdminUserSecretRef.Name)
	assert.Equal(t, "rw-secret", payments.RWUserSecretRef.Name)
	assert.Equal(t, "connection", payments.ConfigMapRef.Name)
	assert.Equal(t, []platformv1alpha1.DatabaseRoleInfo{
		{Name: "payments_admin", SecretRef: &corev1.LocalObjectReference{Name: "tenant-payments-admin"}, Exists: true},
		{Name: "payments_rw", SecretRef: &corev1.LocalObjectReference{Name: "tenant-payments-rw"}, Exists: true},
	}, payments.Roles)
}

func TestIntentPublisherReplacesPopulatedRemovedDatabaseWithMinimalTombstone(t *testing.T) {
	scheme := managedRolesScheme(t)
	database := managedRolesDatabase()
	database.Status.Databases = []platformv1alpha1.DatabaseInfo{
		fullyPopulatedDatabaseInfo("payments"),
		fullyPopulatedDatabaseInfo("removed"),
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&platformv1alpha1.PostgresDatabase{}).
		WithObjects(database).
		Build()

	err := NewIntentPublisher(c, database).Publish(t.Context(), managedRolePublication())
	require.NoError(t, err)

	stored := &platformv1alpha1.PostgresDatabase{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(database), stored))
	require.Len(t, stored.Status.Databases, 2)
	assert.Equal(t, platformv1alpha1.DatabaseInfo{
		Name: "removed",
		Roles: []platformv1alpha1.DatabaseRoleInfo{{
			Name: "removed_rw", SecretRef: &corev1.LocalObjectReference{Name: "tenant-removed-rw"}, Exists: false,
		}},
	}, stored.Status.Databases[1])
}

func TestIntentPublisherUsesPublicationOrder(t *testing.T) {
	scheme := managedRolesScheme(t)
	database := managedRolesDatabase()
	database.Status.Databases = []platformv1alpha1.DatabaseInfo{
		fullyPopulatedDatabaseInfo("removed"),
		fullyPopulatedDatabaseInfo("payments"),
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&platformv1alpha1.PostgresDatabase{}).
		WithObjects(database).
		Build()

	err := NewIntentPublisher(c, database).Publish(t.Context(), managedRolePublication())
	require.NoError(t, err)

	stored := &platformv1alpha1.PostgresDatabase{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(database), stored))
	require.Len(t, stored.Status.Databases, 2)
	assert.Equal(t, "payments", stored.Status.Databases[0].Name)
	assert.Equal(t, "removed", stored.Status.Databases[1].Name)
}

func TestIntentPublisherSkipsNoopStatusWrite(t *testing.T) {
	scheme := managedRolesScheme(t)
	database := managedRolesDatabase()
	generation := database.Generation
	database.Status.ObservedGeneration = &generation
	database.Status.Databases = []platformv1alpha1.DatabaseInfo{
		{Name: "payments", Roles: []platformv1alpha1.DatabaseRoleInfo{
			{Name: "payments_admin", SecretRef: &corev1.LocalObjectReference{Name: "tenant-payments-admin"}, Exists: true},
			{Name: "payments_rw", SecretRef: &corev1.LocalObjectReference{Name: "tenant-payments-rw"}, Exists: true},
		}},
		{Name: "removed", Roles: []platformv1alpha1.DatabaseRoleInfo{
			{Name: "removed_rw", SecretRef: &corev1.LocalObjectReference{Name: "tenant-removed-rw"}, Exists: false},
		}},
	}
	statusWrites := 0
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&platformv1alpha1.PostgresDatabase{}).
		WithObjects(database).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, c client.Client, subresource string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				statusWrites++
				return c.SubResource(subresource).Update(ctx, obj, opts...)
			},
		}).
		Build()

	err := NewIntentPublisher(c, database).Publish(t.Context(), managedRolePublication())

	require.NoError(t, err)
	assert.Zero(t, statusWrites)
}

func TestIntentPublisherPreservesConflictAndDoesNotMutateBoundObject(t *testing.T) {
	scheme := managedRolesScheme(t)
	database := managedRolesDatabase()
	before := database.DeepCopy()
	conflict := apierrors.NewConflict(
		schema.GroupResource{Group: platformv1alpha1.GroupVersion.Group, Resource: "postgresdatabases"},
		database.Name,
		errors.New("write conflict"),
	)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&platformv1alpha1.PostgresDatabase{}).
		WithObjects(database).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(context.Context, client.Client, string, client.Object, ...client.SubResourceUpdateOption) error {
				return conflict
			},
		}).
		Build()

	err := NewIntentPublisher(c, database).Publish(t.Context(), managedRolePublication())

	require.Error(t, err)
	assert.ErrorIs(t, err, dbtypes.ErrManagedRoleIntentConflict)
	assert.ErrorIs(t, err, conflict)
	assert.True(t, apierrors.IsConflict(err))
	assert.Equal(t, before.Status, database.Status)
	assert.Equal(t, before.ResourceVersion, database.ResourceVersion)
}

func TestIntentPublisherPreservesOtherAPIErrorsAndDoesNotMutateBoundObject(t *testing.T) {
	scheme := managedRolesScheme(t)
	database := managedRolesDatabase()
	before := database.DeepCopy()
	writeErr := apierrors.NewTimeoutError("apiserver unavailable", 1)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&platformv1alpha1.PostgresDatabase{}).
		WithObjects(database).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(context.Context, client.Client, string, client.Object, ...client.SubResourceUpdateOption) error {
				return writeErr
			},
		}).
		Build()

	err := NewIntentPublisher(c, database).Publish(t.Context(), managedRolePublication())

	require.Error(t, err)
	assert.NotErrorIs(t, err, dbtypes.ErrManagedRoleIntentConflict)
	assert.ErrorIs(t, err, writeErr)
	assert.True(t, apierrors.IsTimeout(err))
	assert.Equal(t, before.Status, database.Status)
	assert.Equal(t, before.ResourceVersion, database.ResourceVersion)
}

func TestIntentPublisherRejectsMismatchedTarget(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*dbtypes.ManagedRolePublication)
		want   string
	}{
		{
			name:   "identity",
			mutate: func(publication *dbtypes.ManagedRolePublication) { publication.Target.UID = "other-uid" },
			want:   "does not match PostgresDatabase",
		},
		{
			name:   "generation",
			mutate: func(publication *dbtypes.ManagedRolePublication) { publication.Target.Generation++ },
			want:   "does not match PostgresDatabase generation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := managedRolesDatabase()
			publication := managedRolePublication()
			tt.mutate(&publication)

			err := NewIntentPublisher(nil, database).Publish(t.Context(), publication)

			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestIntentFromStatusCopiesRoleProjection(t *testing.T) {
	database := managedRolesDatabase()
	database.Status.Databases = []platformv1alpha1.DatabaseInfo{
		{Name: "payments", Roles: []platformv1alpha1.DatabaseRoleInfo{{
			Name: "payments_admin", SecretRef: &corev1.LocalObjectReference{Name: "tenant-payments-admin"}, Exists: true,
		}}},
		{Name: "legacy-without-roles"},
	}

	intent := IntentFromStatus(database)

	assert.Equal(t, []dbtypes.DatabaseRoleIntent{{
		Database: "payments",
		Roles: []dbtypes.ManagedRoleIntent{{
			Name: "payments_admin", SecretName: "tenant-payments-admin", Exists: true,
		}},
	}}, intent)
	intent[0].Roles[0].Name = "mutated"
	assert.Equal(t, "payments_admin", database.Status.Databases[0].Roles[0].Name)
}

func fullyPopulatedDatabaseInfo(name string) platformv1alpha1.DatabaseInfo {
	return platformv1alpha1.DatabaseInfo{
		Name: name, Ready: true, Message: "keep",
		DatabaseRef:        &corev1.LocalObjectReference{Name: "tenant-" + name},
		DatabaseUID:        types.UID(name + "-uid"),
		AdminUserSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "admin-secret"}, Key: "password"},
		RWUserSecretRef:    &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "rw-secret"}, Key: "password"},
		ConfigMapRef:       &corev1.LocalObjectReference{Name: "connection"},
		Roles:              []platformv1alpha1.DatabaseRoleInfo{{Name: "stale", Exists: true}},
	}
}

func managedRolesScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	return scheme
}

func managedRolesDatabase() *platformv1alpha1.PostgresDatabase {
	return &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant", Namespace: "dbs", UID: types.UID("database-uid"), Generation: 4, ResourceVersion: "1",
		},
	}
}

func managedRolePublication() dbtypes.ManagedRolePublication {
	return dbtypes.ManagedRolePublication{
		Target: dbtypes.ManagedRolePublicationTarget{Name: "tenant", Namespace: "dbs", UID: "database-uid", Generation: 4},
		Databases: []dbtypes.DatabaseRoleIntent{
			{Database: "payments", Roles: []dbtypes.ManagedRoleIntent{
				{Name: "payments_admin", SecretName: "tenant-payments-admin", Exists: true},
				{Name: "payments_rw", SecretName: "tenant-payments-rw", Exists: true},
			}},
			{Database: "removed", Roles: []dbtypes.ManagedRoleIntent{
				{Name: "removed_rw", SecretName: "tenant-removed-rw", Exists: false},
			}},
		},
	}
}
