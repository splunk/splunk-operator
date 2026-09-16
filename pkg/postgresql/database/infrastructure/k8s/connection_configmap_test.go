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
	"errors"
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func connectionConfigMapScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	return scheme
}

func connectionConfigMapOwner() *platformv1alpha1.PostgresDatabase {
	return &platformv1alpha1.PostgresDatabase{
		TypeMeta:   metav1.TypeMeta{APIVersion: platformv1alpha1.GroupVersion.String(), Kind: "PostgresDatabase"},
		ObjectMeta: metav1.ObjectMeta{Name: "tenant", Namespace: "dbs", UID: types.UID("database-uid")},
	}
}

func desiredConnectionConfigMap() DesiredConnectionConfigMap {
	return DesiredConnectionConfigMap{
		Name: "tenant-payments-config", Namespace: "dbs",
		Labels: map[string]string{"app.kubernetes.io/managed-by": "splunk-operator"},
		Data:   map[string]string{"DATABASE_NAME": "payments"},
	}
}

func TestApplyConnectionConfigMapCreatesAndConverges(t *testing.T) {
	scheme := connectionConfigMapScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	desired := desiredConnectionConfigMap()
	owner := connectionConfigMapOwner()

	_, err := ApplyConnectionConfigMap(t.Context(), c, scheme, owner, desired)
	require.NoError(t, err)

	actual := &corev1.ConfigMap{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKey{Name: desired.Name, Namespace: desired.Namespace}, actual))
	assert.Equal(t, desired.Data, actual.Data)
	assert.Equal(t, "splunk-operator", actual.Labels["app.kubernetes.io/managed-by"])
	assert.True(t, metav1.IsControlledBy(actual, owner))
	resourceVersion := actual.ResourceVersion

	_, err = ApplyConnectionConfigMap(t.Context(), c, scheme, owner, desired)
	require.NoError(t, err)
	require.NoError(t, c.Get(t.Context(), client.ObjectKey{Name: desired.Name, Namespace: desired.Namespace}, actual))
	assert.Equal(t, resourceVersion, actual.ResourceVersion)
}

func TestApplyConnectionConfigMapRepairsDataAndPreservesExistingMetadata(t *testing.T) {
	scheme := connectionConfigMapScheme(t)
	owner := connectionConfigMapOwner()
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-payments-config", Namespace: "dbs",
			Annotations:     map[string]string{"keep": "true"},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(owner, platformv1alpha1.GroupVersion.WithKind("PostgresDatabase"))},
		},
		Data: map[string]string{"DATABASE_NAME": "stale", "UNRELATED": "removed"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()

	_, err := ApplyConnectionConfigMap(t.Context(), c, scheme, owner, desiredConnectionConfigMap())
	require.NoError(t, err)

	actual := &corev1.ConfigMap{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(existing), actual))
	assert.Equal(t, map[string]string{"DATABASE_NAME": "payments"}, actual.Data)
	assert.Equal(t, "true", actual.Annotations["keep"])
	assert.NotContains(t, actual.Labels, "app.kubernetes.io/managed-by", "existing label drift is not repaired by the current contract")
}

func TestApplyConnectionConfigMapAdoptsRetainedAndUnownedObjects(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		wantAdopted bool
	}{
		{name: "retained", annotations: map[string]string{retainedFromAnnotation: "tenant", "keep": "true"}, wantAdopted: true},
		{name: "unowned", annotations: map[string]string{"keep": "true"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := connectionConfigMapScheme(t)
			owner := connectionConfigMapOwner()
			existing := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
				Name: "tenant-payments-config", Namespace: "dbs", Annotations: tt.annotations,
			}}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()

			reAdopted, err := ApplyConnectionConfigMap(t.Context(), c, scheme, owner, desiredConnectionConfigMap())
			require.NoError(t, err)
			assert.Equal(t, tt.wantAdopted, reAdopted)

			actual := &corev1.ConfigMap{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(existing), actual))
			assert.True(t, metav1.IsControlledBy(actual, owner))
			assert.Equal(t, "true", actual.Annotations["keep"])
			assert.NotContains(t, actual.Annotations, retainedFromAnnotation)
		})
	}
}

func TestApplyConnectionConfigMapRejectsForeignController(t *testing.T) {
	scheme := connectionConfigMapScheme(t)
	owner := connectionConfigMapOwner()
	controller := true
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-payments-config", Namespace: "dbs",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: "Deployment", Name: "foreign", UID: types.UID("foreign-uid"), Controller: &controller,
			}},
		},
		Data: map[string]string{"DATABASE_NAME": "foreign"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()

	_, err := ApplyConnectionConfigMap(t.Context(), c, scheme, owner, desiredConnectionConfigMap())
	require.Error(t, err)
	var owned *controllerutil.AlreadyOwnedError
	assert.ErrorAs(t, err, &owned)

	actual := &corev1.ConfigMap{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(existing), actual))
	assert.Equal(t, map[string]string{"DATABASE_NAME": "foreign"}, actual.Data)
}

func TestApplyConnectionConfigMapPreservesAPIErrorIdentity(t *testing.T) {
	t.Run("create conflict", func(t *testing.T) {
		scheme := connectionConfigMapScheme(t)
		conflict := apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, "tenant-payments-config", errors.New("write conflict"))
		c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Create: func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
				return conflict
			},
		}).Build()

		_, err := ApplyConnectionConfigMap(t.Context(), c, scheme, connectionConfigMapOwner(), desiredConnectionConfigMap())
		assert.ErrorIs(t, err, conflict)
	})

	t.Run("update conflict", func(t *testing.T) {
		scheme := connectionConfigMapScheme(t)
		owner := connectionConfigMapOwner()
		existing := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-payments-config", Namespace: "dbs",
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(owner, platformv1alpha1.GroupVersion.WithKind("PostgresDatabase"))},
		}, Data: map[string]string{"DATABASE_NAME": "stale"}}
		conflict := apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, existing.Name, errors.New("write conflict"))
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).WithInterceptorFuncs(interceptor.Funcs{
			Update: func(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error {
				return conflict
			},
		}).Build()

		_, err := ApplyConnectionConfigMap(t.Context(), c, scheme, owner, desiredConnectionConfigMap())
		assert.ErrorIs(t, err, conflict)
	})

	t.Run("read failure", func(t *testing.T) {
		scheme := connectionConfigMapScheme(t)
		readErr := apierrors.NewTimeoutError("apiserver timeout", 1)
		c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
				return readErr
			},
		}).Build()

		_, err := ApplyConnectionConfigMap(t.Context(), c, scheme, connectionConfigMapOwner(), desiredConnectionConfigMap())
		assert.ErrorIs(t, err, readErr)
	})
}

func TestApplyConnectionConfigMapRecreatesDeletedManagedObject(t *testing.T) {
	scheme := connectionConfigMapScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	desired := desiredConnectionConfigMap()
	owner := connectionConfigMapOwner()

	_, err := ApplyConnectionConfigMap(t.Context(), c, scheme, owner, desired)
	require.NoError(t, err)
	actual := &corev1.ConfigMap{}
	key := client.ObjectKey{Name: desired.Name, Namespace: desired.Namespace}
	require.NoError(t, c.Get(t.Context(), key, actual))
	require.NoError(t, c.Delete(t.Context(), actual))

	_, err = ApplyConnectionConfigMap(t.Context(), c, scheme, owner, desired)
	require.NoError(t, err)
	require.NoError(t, c.Get(t.Context(), key, actual))
	assert.Equal(t, desired.Data, actual.Data)
	assert.True(t, metav1.IsControlledBy(actual, owner))
}
