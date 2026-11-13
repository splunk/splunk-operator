// Copyright (c) 2026 Splunk Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"fmt"
	"strings"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestGeneratedNameCollisionPreservesOriginalResources(t *testing.T) {
	for _, state := range []string{"owned", "retained", "unowned", "unowned-shared-roles", "legacy-unowned-shared-roles"} {
		t.Run(state, func(t *testing.T) {
			scheme := testScheme(t)
			c := testClient(t, scheme)
			owner := &platformv1alpha1.PostgresDatabase{
				Name: "a-b", Namespace: "dbs", UID: "owner-uid",
				Spec: platformv1alpha1.PostgresDatabaseSpec{
					ClusterRef: corev1.LocalObjectReference{Name: "cluster"},
					Databases:  []platformv1alpha1.DatabaseDefinition{{Name: "c"}},
				},
			}
			other := &platformv1alpha1.PostgresDatabase{
				Name: "a", Namespace: "dbs", UID: "other-uid",
				Spec: platformv1alpha1.PostgresDatabaseSpec{
					ClusterRef: owner.Spec.ClusterRef,
					Databases:  []platformv1alpha1.DatabaseDefinition{{Name: "b-c"}},
				},
			}
			if strings.HasSuffix(state, "shared-roles") {
				other.Spec.Databases[0].AdminRoleName = "c_admin"
				other.Spec.Databases[0].RWRoleName = "c_rw"
			}
			endpoints := clusterEndpoints{RWHost: "cluster-rw", ROHost: "cluster-ro", RHost: "cluster-r"}
			require.NoError(t, reconcileRoleSecrets(t.Context(), c, scheme, owner, nil))
			require.NoError(t, reconcileRoleConfigMaps(t.Context(), c, scheme, owner, endpoints))
			provisioner := testDatabaseProvisioner(c, scheme)
			_, err := reconcileDatabaseProvisioning(t.Context(), provisioner, owner, "cluster", "")
			require.NoError(t, err)
			objects := []client.Object{
				&corev1.Secret{Name: "a-b-c-admin", Namespace: "dbs"},
				&corev1.Secret{Name: "a-b-c-rw", Namespace: "dbs"},
				&corev1.ConfigMap{Name: "a-b-c-config", Namespace: "dbs"},
				&cnpgv1.Database{Name: "a-b-c", Namespace: "dbs"},
			}
			for _, obj := range objects {
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
				if state != "owned" {
					obj.SetOwnerReferences(nil)
					if state == "legacy-unowned-shared-roles" {
						delete(obj.GetAnnotations(), annotationCredentialOwner)
					}
					if state == "retained" {
						obj.SetAnnotations(map[string]string{annotationRetainedFrom: owner.Name})
					}
					require.NoError(t, c.Update(t.Context(), obj))
					require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
				}
			}
			assert.Error(t, reconcileRoleSecrets(t.Context(), c, scheme, other, nil))
			assert.Error(t, reconcileRoleConfigMaps(t.Context(), c, scheme, other, endpoints))
			_, err = reconcileDatabaseProvisioning(t.Context(), provisioner, other, "cluster", "")
			assert.Error(t, err)
			for range 2 {
				require.NoError(t, deleteCNPGDatabases(t.Context(), c, other, other.Spec.Databases))
				require.NoError(t, deleteConfigMaps(t.Context(), c, other, other.Spec.Databases))
				require.NoError(t, deleteSecrets(t.Context(), c, other, other.Spec.Databases))
				require.NoError(t, orphanCNPGDatabases(t.Context(), c, other, other.Spec.Databases))
				require.NoError(t, orphanConfigMaps(t.Context(), c, other, other.Spec.Databases))
				require.NoError(t, orphanSecrets(t.Context(), c, other, other.Spec.Databases))
			}
			for _, before := range objects {
				after := before.DeepCopyObject().(client.Object)
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(before), after))
				assert.Equal(t, before, after, "collision must not mutate %T %s", before, before.GetName())
			}
		})
	}
}

func TestHyphenatedDatabaseIdentityAndRetainRecreate(t *testing.T) {
	scheme := testScheme(t)
	c := testClient(t, scheme)
	owner := &platformv1alpha1.PostgresDatabase{
		Name: "tenant", Namespace: "dbs", UID: "first-uid",
		Spec: platformv1alpha1.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "cluster"},
			Databases:  []platformv1alpha1.DatabaseDefinition{{Name: "tenant--abc", DeletionPolicy: "Retain"}},
		},
	}
	endpoints := clusterEndpoints{RWHost: "cluster-rw", ROHost: "cluster-ro", RHost: "cluster-r"}
	provisioner := testDatabaseProvisioner(c, scheme)
	for range 2 {
		require.NoError(t, reconcileRoleSecrets(t.Context(), c, scheme, owner, nil))
		require.NoError(t, reconcileRoleConfigMaps(t.Context(), c, scheme, owner, endpoints))
		_, err := reconcileDatabaseProvisioning(t.Context(), provisioner, owner, "cluster", "")
		require.NoError(t, err)
	}
	adminName, rwName := ResolveSecretNames(owner.Name, owner.Spec.Databases[0])
	admin := &corev1.Secret{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKey{Namespace: owner.Namespace, Name: adminName}, admin))
	password := string(admin.Data[secretKeyPassword])
	assert.Equal(t, "tenant--abc_admin", string(admin.Data[secretKeyUsername]))
	cm := &corev1.ConfigMap{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKey{Namespace: owner.Namespace, Name: configMapName(owner.Name, "tenant--abc")}, cm))
	assert.Equal(t, "tenant--abc", cm.Data[ConfigMapKeyDatabaseName])
	assert.Equal(t, "tenant--abc_admin", cm.Data[ConfigMapKeyAdminUser])
	assert.Equal(t, "tenant--abc_rw", cm.Data[ConfigMapKeyRWUser])
	for _, name := range []string{adminName, rwName, cm.Name, cnpgDatabaseName(owner.Name, "tenant--abc")} {
		assert.Empty(t, validation.IsDNS1123Subdomain(name))
	}
	for range 2 {
		require.NoError(t, orphanCNPGDatabases(t.Context(), c, owner, owner.Spec.Databases))
		require.NoError(t, orphanConfigMaps(t.Context(), c, owner, owner.Spec.Databases))
		require.NoError(t, orphanSecrets(t.Context(), c, owner, owner.Spec.Databases))
	}
	owner.UID = types.UID("recreated-uid")
	require.NoError(t, reconcileRoleSecrets(t.Context(), c, scheme, owner, nil))
	require.NoError(t, reconcileRoleConfigMaps(t.Context(), c, scheme, owner, endpoints))
	_, err := reconcileDatabaseProvisioning(t.Context(), provisioner, owner, "cluster", "")
	require.NoError(t, err)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(admin), admin))
	assert.Equal(t, password, string(admin.Data[secretKeyPassword]))
	assert.True(t, metav1.IsControlledBy(admin, owner))
	assert.NotContains(t, admin.Annotations, annotationRetainedFrom)
	// The same generated names must still be cleaned up by the recreated owner.
	require.NoError(t, deleteCNPGDatabases(t.Context(), c, owner, owner.Spec.Databases))
	require.NoError(t, deleteConfigMaps(t.Context(), c, owner, owner.Spec.Databases))
	require.NoError(t, deleteSecrets(t.Context(), c, owner, owner.Spec.Databases))
	assert.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKeyFromObject(admin), admin)))
}

func TestCleanupIgnoresSameNameDifferentOwnerUID(t *testing.T) {
	scheme := testScheme(t)
	oldOwner := &platformv1alpha1.PostgresDatabase{Name: "tenant", Namespace: "dbs", UID: "old"}
	newOwner := oldOwner.DeepCopy()
	newOwner.UID = "new"
	secret := &corev1.Secret{Name: "tenant-app-rw", Namespace: "dbs", Data: map[string][]byte{"password": []byte("preserve")}}
	require.NoError(t, controllerutil.SetControllerReference(newOwner, secret, scheme))
	c := testClient(t, scheme, secret)
	require.NoError(t, deleteSecrets(t.Context(), c, oldOwner, []platformv1alpha1.DatabaseDefinition{{Name: "app"}}))
	got := &corev1.Secret{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(secret), got))
	assert.Equal(t, secret, got)
}

func TestDeleteOwnedResourceProtectsConcurrentOwnershipChange(t *testing.T) {
	scheme := testScheme(t)
	owner := &platformv1alpha1.PostgresDatabase{Name: "tenant", Namespace: "dbs", UID: "owner"}
	secret := &corev1.Secret{Name: "tenant-app-rw", Namespace: "dbs", UID: "secret-uid"}
	require.NoError(t, controllerutil.SetControllerReference(owner, secret, scheme))
	deleteCalled := false
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).WithInterceptorFuncs(interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			deleteCalled = true
			options := &client.DeleteOptions{}
			for _, opt := range opts {
				opt.ApplyToDelete(options)
			}
			require.NotNil(t, options.Preconditions)
			require.NotNil(t, options.Preconditions.UID)
			require.NotNil(t, options.Preconditions.ResourceVersion)
			assert.Equal(t, secret.UID, *options.Preconditions.UID)
			current := &corev1.Secret{}
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(obj), current))
			current.OwnerReferences[0].UID = "new-owner"
			require.NoError(t, c.Update(ctx, current))
			return c.Delete(ctx, obj, opts...)
		},
	}).Build()
	err := deleteOwnedResource(t.Context(), c, owner, secret)
	assert.True(t, deleteCalled)
	assert.True(t, apierrors.IsConflict(err), "ownership changed after observation: %v", err)
	got := &corev1.Secret{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(secret), got))
	assert.Equal(t, types.UID("new-owner"), got.OwnerReferences[0].UID)
}

func TestRoleSecretCreateRaceChecksTheWinningOwner(t *testing.T) {
	scheme := testScheme(t)
	owner := &platformv1alpha1.PostgresDatabase{Name: "a", Namespace: "dbs", UID: "owner"}
	other := &platformv1alpha1.PostgresDatabase{Name: "a-b", Namespace: "dbs", UID: "other"}
	winner := buildPasswordSecret(other, "a-b-c-admin", "c_admin", "preserve")
	require.NoError(t, controllerutil.SetControllerReference(other, winner, scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			require.NoError(t, c.Create(ctx, winner))
			return c.Create(ctx, obj, opts...)
		},
	}).Build()
	err := createRoleSecret(t.Context(), c, scheme, owner, "b-c_admin", winner.Name)
	require.Error(t, err)
	got := &corev1.Secret{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(winner), got))
	assert.True(t, metav1.IsControlledBy(got, other))
	assert.Equal(t, winner.Data, got.Data)
}

func TestGeneratedNamesAtSupportedLengthLimits(t *testing.T) {
	owner := &platformv1alpha1.PostgresDatabase{Name: strings.Repeat("a", 50)}
	databaseName := "a" + strings.Repeat("-", 28) + "1"
	for _, name := range []string{
		roleSecretName(owner.Name, databaseName, secretRoleAdmin),
		roleSecretName(owner.Name, databaseName, secretRoleRW),
		configMapName(owner.Name, databaseName),
		cnpgDatabaseName(owner.Name, databaseName),
		cnpgDatabaseResourceName(owner, databaseName, strings.Repeat("b", 63)),
	} {
		assert.Empty(t, validation.IsDNS1123Subdomain(name), name)
	}
}

func TestLegacyPublishedCredentialsRecoverOwnershipWithoutChangingData(t *testing.T) {
	scheme := testScheme(t)
	owner := &platformv1alpha1.PostgresDatabase{
		Name: "tenant", Namespace: "dbs", UID: "owner",
		Status: platformv1alpha1.PostgresDatabaseStatus{Databases: []platformv1alpha1.DatabaseInfo{{
			Name: "tenant--abc", AdminUserSecretRef: &corev1.SecretKeySelector{Name: "tenant-tenant--abc-admin"},
		}}},
	}
	secret := &corev1.Secret{Name: "tenant-tenant--abc-admin", Namespace: "dbs",
		Data: map[string][]byte{secretKeyUsername: []byte("tenant--abc_admin"), secretKeyPassword: []byte("preserve")}}
	c := testClient(t, scheme, secret)
	require.NoError(t, ensureProvisionedSecret(t.Context(), c, scheme, owner, "tenant--abc_admin", secret.Name))
	got := &corev1.Secret{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(secret), got))
	assert.True(t, metav1.IsControlledBy(got, owner))
	assert.Equal(t, owner.Name, got.Annotations[annotationCredentialOwner])
	assert.Equal(t, secret.Data, got.Data)
}

func TestProviderNameCollisionWithinOneOwnerPreservesDatabaseIdentity(t *testing.T) {
	scheme := testScheme(t)
	c := testClient(t, scheme)
	const environmentName = "primary-bg-0123456789"
	owner := &platformv1alpha1.PostgresDatabase{
		Name: "app", Namespace: "dbs", UID: "owner",
		Spec: platformv1alpha1.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "primary"},
			Databases: []platformv1alpha1.DatabaseDefinition{
				{Name: "c"}, {Name: environmentName + "-c"},
			},
		},
	}
	provisioner := testDatabaseProvisioner(c, scheme)
	_, err := reconcileDatabaseProvisioning(t.Context(), provisioner, owner, "primary", "")
	require.NoError(t, err)
	key := client.ObjectKey{Namespace: owner.Namespace, Name: cnpgDatabaseName(owner.Name, environmentName+"-c")}
	require.Equal(t, key.Name, cnpgDatabaseResourceName(owner, "c", environmentName))
	original := &cnpgv1.Database{}
	require.NoError(t, c.Get(t.Context(), key, original))
	for range 2 {
		_, err := reconcileDatabaseProvisioning(t.Context(), provisioner, owner, environmentName, environmentName)
		require.ErrorContains(t, err, "conflicting database identity")
		got := &cnpgv1.Database{}
		require.NoError(t, c.Get(t.Context(), key, got))
		assert.Equal(t, original, got)
	}
}

func TestSameOwnerCleanupKeepsDeletionPoliciesSeparate(t *testing.T) {
	const environment = "primary-bg-0123456789"
	scheme := testScheme(t)
	c := testClient(t, scheme)
	owner := &platformv1alpha1.PostgresDatabase{
		Name: "app", Namespace: "dbs", UID: "owner",
		Spec: platformv1alpha1.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "primary"},
			Databases: []platformv1alpha1.DatabaseDefinition{
				{Name: "c", DeletionPolicy: "Retain"},
				{Name: environment + "-c", DeletionPolicy: "Delete"},
			},
		},
	}
	_, err := reconcileDatabaseProvisioning(t.Context(), testDatabaseProvisioner(c, scheme), owner, "primary", "")
	require.NoError(t, err)
	plan := buildDeletionPlan(owner.Spec.Databases)
	require.NoError(t, orphanRetainedResources(t.Context(), c, owner, plan.retained))
	require.NoError(t, deleteRemovedResources(t.Context(), c, owner, plan.deleted))
	retained := &cnpgv1.Database{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKey{Name: "app-c", Namespace: "dbs"}, retained))
	assert.False(t, metav1.IsControlledBy(retained, owner))
	assert.Equal(t, owner.Name, retained.Annotations[annotationRetainedFrom])
	deleted := &cnpgv1.Database{}
	err = c.Get(t.Context(), client.ObjectKey{Name: "app-" + environment + "-c", Namespace: "dbs"}, deleted)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("Delete-policy database was incorrectly retained: get=%v, database=%q, retained-from=%q, owners=%v", err, deleted.Spec.Name, deleted.Annotations[annotationRetainedFrom], deleted.OwnerReferences)
	}
}

func TestCNPGCleanupDoesNotDeleteSameOwnerReplacement(t *testing.T) {
	scheme := testScheme(t)
	owner := &platformv1alpha1.PostgresDatabase{Name: "app", Namespace: "dbs", UID: "owner"}
	database := &cnpgv1.Database{Name: "app-c", Namespace: "dbs", UID: "original",
		Spec: cnpgv1.DatabaseSpec{Name: "c"}}
	require.NoError(t, controllerutil.SetControllerReference(owner, database, scheme))
	replacement := database.DeepCopy()
	replacement.UID = "replacement"
	replacement.Spec.Name = "other"
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(database).WithInterceptorFuncs(interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if err := c.List(ctx, list, opts...); err != nil {
				return err
			}
			// Replace the object after cleanup has observed its logical identity.
			require.NoError(t, c.Delete(ctx, database))
			require.NoError(t, c.Create(ctx, replacement))
			return nil
		},
	}).Build()
	err := deleteCNPGDatabases(t.Context(), c, owner, []platformv1alpha1.DatabaseDefinition{{Name: "c"}})
	require.True(t, apierrors.IsConflict(err), "replacement must invalidate the observed delete: %v", err)
	got := &cnpgv1.Database{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(replacement), got))
	assert.Equal(t, replacement.UID, got.UID)
	assert.Equal(t, "other", got.Spec.Name)
}

func TestCNPGCleanupSkipsSameOwnerAliasOfAnotherDatabase(t *testing.T) {
	for _, retain := range []bool{false, true} {
		t.Run(fmt.Sprintf("retain=%t", retain), func(t *testing.T) {
			scheme := testScheme(t)
			owner := &platformv1alpha1.PostgresDatabase{Name: "app", Namespace: "dbs", UID: "owner"}
			database := &cnpgv1.Database{Name: "app-green-c", Namespace: "dbs",
				Spec: cnpgv1.DatabaseSpec{Name: "green-c"}}
			require.NoError(t, controllerutil.SetControllerReference(owner, database, scheme))
			c := testClient(t, scheme, database)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(database), database))
			before := database.DeepCopy()
			if retain {
				require.NoError(t, orphanCNPGDatabases(t.Context(), c, owner, []platformv1alpha1.DatabaseDefinition{{Name: "c"}}))
			} else {
				require.NoError(t, deleteCNPGDatabases(t.Context(), c, owner, []platformv1alpha1.DatabaseDefinition{{Name: "c"}}))
			}
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(database), database))
			assert.Equal(t, before, database)
		})
	}
}
