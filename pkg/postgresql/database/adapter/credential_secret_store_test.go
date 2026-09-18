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
package adapter

import (
	"context"
	"errors"
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	dbcredentials "github.com/splunk/splunk-operator/pkg/postgresql/database/core/components/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func credentialAdapterScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	return scheme
}

func credentialAdapterOwner() *platformv1alpha1.PostgresDatabase {
	return &platformv1alpha1.PostgresDatabase{
		TypeMeta:   metav1.TypeMeta{APIVersion: platformv1alpha1.GroupVersion.String(), Kind: "PostgresDatabase"},
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "dbs", UID: types.UID("orders-uid")},
	}
}

func TestCredentialSecretOperationsUsesBoundOwnerIdentity(t *testing.T) {
	scheme := credentialAdapterScheme(t)
	owner := credentialAdapterOwner()
	operations := NewCredentialSecretOperations(fake.NewClientBuilder().WithScheme(scheme).Build(), scheme, owner)
	assert.Equal(t, dbcredentials.OwnerIdentity{Name: owner.Name, UID: string(owner.UID), Kind: owner.Kind}, operations.OwnerIdentity())
}

func TestCredentialSecretOperationsMapsNotFoundAndPreservesCause(t *testing.T) {
	scheme := credentialAdapterScheme(t)
	_, err := NewCredentialSecretOperations(fake.NewClientBuilder().WithScheme(scheme).Build(), scheme, credentialAdapterOwner()).
		Read(t.Context(), dbcredentials.SecretRef{Namespace: "dbs", Name: "missing"})
	assert.ErrorIs(t, err, dbcredentials.ErrSecretNotFound)
	assert.True(t, apierrors.IsNotFound(err))
}

func TestCredentialSecretOperationsMapsCreateRaceAndPreservesCause(t *testing.T) {
	scheme := credentialAdapterScheme(t)
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "orders-admin", Namespace: "dbs"}}
	operations := NewCredentialSecretOperations(fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build(), scheme, credentialAdapterOwner())

	err := operations.CreateGenerated(t.Context(), dbcredentials.SecretRef{Namespace: "dbs", Name: "orders-admin"}, "orders_admin")
	assert.ErrorIs(t, err, dbcredentials.ErrSecretAlreadyExists)
	assert.True(t, apierrors.IsAlreadyExists(err))
}

func TestCredentialSecretOperationsPreservesTransientReadError(t *testing.T) {
	scheme := credentialAdapterScheme(t)
	transient := errors.New("apiserver unavailable")
	c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
			return transient
		},
	}).Build()

	_, err := NewCredentialSecretOperations(c, scheme, credentialAdapterOwner()).Read(t.Context(), dbcredentials.SecretRef{Namespace: "dbs", Name: "orders-admin"})
	assert.ErrorIs(t, err, transient)
}

func TestCredentialSecretOperationsTranslatesSecretSnapshot(t *testing.T) {
	scheme := credentialAdapterScheme(t)
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name: "orders-admin", Namespace: "dbs",
		Labels:      map[string]string{"cnpg.io/reload": "true"},
		Annotations: map[string]string{"platform.splunk.com/retained-from": "orders"},
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: platformv1alpha1.GroupVersion.String(), Kind: "PostgresCluster", Name: "primary", UID: types.UID("primary-uid"), Controller: ptr.To(true),
		}},
	}, Data: map[string][]byte{
		"username": []byte("orders_admin"),
		"password": []byte("not-exposed"),
	}}
	operations := NewCredentialSecretOperations(fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build(), scheme, credentialAdapterOwner())

	facts, err := operations.Read(t.Context(), dbcredentials.SecretRef{Namespace: secret.Namespace, Name: secret.Name})
	require.NoError(t, err)
	assert.True(t, facts.DataDefined)
	assert.Equal(t, "orders_admin", facts.Username)
	assert.True(t, facts.UsernamePresent)
	assert.True(t, facts.PasswordPresent)
	assert.True(t, facts.ReloadEnabled)
	assert.Equal(t, "orders", facts.RetainedFrom)
	require.NotNil(t, facts.Controller)
	assert.Equal(t, "PostgresCluster", facts.Controller.Kind)
	assert.Equal(t, "primary", facts.Controller.Name)
	assert.Equal(t, "primary-uid", facts.Controller.UID)
}

func TestCredentialSecretOperationsMapsAdoptionConflict(t *testing.T) {
	scheme := credentialAdapterScheme(t)
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "orders-admin", Namespace: "dbs"}}
	conflict := apierrors.NewConflict(schema.GroupResource{Resource: "secrets"}, secret.Name, errors.New("resource version changed"))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).WithInterceptorFuncs(interceptor.Funcs{
		Update: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.UpdateOption) error {
			return conflict
		},
	}).Build()
	operations := NewCredentialSecretOperations(c, scheme, credentialAdapterOwner())
	ref := dbcredentials.SecretRef{Namespace: secret.Namespace, Name: secret.Name}
	facts, err := operations.Read(t.Context(), ref)
	require.NoError(t, err)

	err = operations.Adopt(t.Context(), ref, facts.ResourceVersion)
	assert.ErrorIs(t, err, dbcredentials.ErrSecretConflict)
	assert.True(t, apierrors.IsConflict(err))
}
