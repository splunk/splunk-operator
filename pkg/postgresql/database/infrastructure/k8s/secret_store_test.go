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

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func secretStoreScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	return scheme
}

func secretStoreOwner() *platformv1alpha1.PostgresDatabase {
	return &platformv1alpha1.PostgresDatabase{
		TypeMeta:   metav1.TypeMeta{APIVersion: platformv1alpha1.GroupVersion.String(), Kind: "PostgresDatabase"},
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "dbs", UID: types.UID(uuid.NewUUID())},
	}
}

func TestSecretStoreReadReturnsNonSensitiveFacts(t *testing.T) {
	scheme := secretStoreScheme(t)
	owner := secretStoreOwner()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "orders-admin", Namespace: "dbs",
			Labels:          map[string]string{labelCNPGReload: "true"},
			Annotations:     map[string]string{annotationRetainedFrom: owner.Name},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: platformv1alpha1.GroupVersion.String(), Kind: "PostgresDatabase", Name: owner.Name, UID: owner.UID, Controller: ptr.To(true)}},
		},
		Data: map[string][]byte{
			secretKeyUsername: []byte("orders_admin"),
			secretKeyPassword: []byte("not-exposed"),
		},
	}

	facts, err := NewSecretStore(fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build(), scheme, owner).
		Read(t.Context(), secret.Namespace, secret.Name)
	require.NoError(t, err)
	assert.True(t, facts.DataDefined)
	assert.Equal(t, "orders_admin", facts.Username)
	assert.True(t, facts.UsernamePresent)
	assert.True(t, facts.PasswordPresent)
	assert.True(t, facts.ReloadEnabled)
	assert.Equal(t, owner.Name, facts.RetainedFrom)
	require.NotNil(t, facts.Controller)
	assert.Equal(t, owner.UID, facts.Controller.UID)
	assert.Equal(t, "PostgresDatabase", facts.Controller.Kind)
}

func TestSecretStoreReadReportsForeignController(t *testing.T) {
	scheme := secretStoreScheme(t)
	owner := secretStoreOwner()
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name: "orders-admin", Namespace: "dbs",
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: platformv1alpha1.GroupVersion.String(), Kind: "PostgresCluster", Name: "primary", UID: types.UID("primary-uid"), Controller: ptr.To(true),
		}},
	}}

	facts, err := NewSecretStore(fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build(), scheme, owner).
		Read(t.Context(), secret.Namespace, secret.Name)
	require.NoError(t, err)
	require.NotNil(t, facts.Controller)
	assert.Equal(t, "PostgresCluster", facts.Controller.Kind)
	assert.Equal(t, "primary", facts.Controller.Name)
	assert.Equal(t, types.UID("primary-uid"), facts.Controller.UID)
}

func TestSecretStoreCreateGeneratedSetsOnlyNewCredentialData(t *testing.T) {
	scheme := secretStoreScheme(t)
	owner := secretStoreOwner()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	store := NewSecretStore(c, scheme, owner)

	require.NoError(t, store.CreateGenerated(t.Context(), "dbs", "orders-admin", "orders_admin"))
	created := &corev1.Secret{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKey{Namespace: "dbs", Name: "orders-admin"}, created))
	assert.Equal(t, "orders_admin", string(created.Data[secretKeyUsername]))
	assert.NotEmpty(t, created.Data[secretKeyPassword])
	assert.Equal(t, "splunk-operator", created.Labels[labelManagedBy])
	assert.Equal(t, "true", created.Labels[labelCNPGReload])
	assert.Equal(t, corev1.SecretTypeOpaque, created.Type)
	assert.True(t, metav1.IsControlledBy(created, owner))
}

func TestSecretStoreAdoptPreservesImmutableData(t *testing.T) {
	scheme := secretStoreScheme(t)
	owner := secretStoreOwner()
	originalData := map[string][]byte{
		secretKeyUsername: []byte("orders_admin"),
		secretKeyPassword: []byte("do-not-rotate"),
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "orders-admin", Namespace: "dbs",
			Annotations: map[string]string{annotationRetainedFrom: owner.Name, "keep": "true"},
		},
		Immutable: ptr.To(true),
		Data:      originalData,
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	store := NewSecretStore(c, scheme, owner)
	snapshot, err := store.Read(t.Context(), secret.Namespace, secret.Name)
	require.NoError(t, err)
	require.NoError(t, store.Adopt(t.Context(), secret.Namespace, secret.Name, snapshot.ResourceVersion))
	updated := &corev1.Secret{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(secret), updated))
	assert.Equal(t, originalData, updated.Data)
	assert.True(t, *updated.Immutable)
	assert.True(t, metav1.IsControlledBy(updated, owner))
	assert.NotContains(t, updated.Annotations, annotationRetainedFrom)
	assert.Equal(t, "true", updated.Annotations["keep"])
}

func TestSecretStoreAdoptRejectsStaleRead(t *testing.T) {
	scheme := secretStoreScheme(t)
	owner := secretStoreOwner()
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "orders-admin", Namespace: "dbs"}}
	store := NewSecretStore(fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build(), scheme, owner)

	snapshot, err := store.Read(t.Context(), secret.Namespace, secret.Name)
	require.NoError(t, err)
	err = store.Adopt(t.Context(), secret.Namespace, secret.Name, snapshot.ResourceVersion+"-stale")
	assert.True(t, apierrors.IsConflict(err))
}

func TestSecretStoreReadPreservesNotFound(t *testing.T) {
	scheme := secretStoreScheme(t)
	_, err := NewSecretStore(fake.NewClientBuilder().WithScheme(scheme).Build(), scheme, secretStoreOwner()).Read(context.Background(), "dbs", "missing")
	assert.True(t, apierrors.IsNotFound(err))
}
