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

package cnpg

import (
	"context"
	"errors"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func retirementDeclaration(t *testing.T) (client.Client, dbtypes.EnvironmentRetirement, *cnpgv1.Database) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	child := corev1.ObjectReference{APIVersion: platformv1alpha1.GroupVersion.String(), Kind: "PostgresDatabase", Namespace: "ns", Name: "child", UID: "child-uid"}
	environment := corev1.ObjectReference{APIVersion: cnpgv1.SchemeGroupVersion.String(), Kind: "Cluster", Namespace: "ns", Name: "retired", UID: "retired-uid"}
	database := &cnpgv1.Database{
		ObjectMeta: metav1.ObjectMeta{Name: "provider", Namespace: "ns", UID: "provider-uid", OwnerReferences: []metav1.OwnerReference{{APIVersion: child.APIVersion, Kind: child.Kind, Name: child.Name, UID: child.UID, Controller: new(true)}}},
		Spec:       cnpgv1.DatabaseSpec{Name: "app", ClusterRef: corev1.LocalObjectReference{Name: environment.Name}, ReclaimPolicy: cnpgv1.DatabaseReclaimDelete},
	}
	request := dbtypes.EnvironmentRetirement{Child: child, Environment: environment, Databases: []dbtypes.DatabaseRetirement{{DatabaseName: "app", Resources: []corev1.ObjectReference{{APIVersion: environment.APIVersion, Kind: "Database", Namespace: "ns", Name: database.Name, UID: database.UID}}}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(database).Build()
	return c, request, database
}

func TestEnvironmentRetirementRejectsConflictingDeclarations(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*dbtypes.EnvironmentRetirement)
	}{
		{"resource replacement", func(r *dbtypes.EnvironmentRetirement) { r.Databases[0].Resources[0].UID = "other" }},
		{"owner replacement", func(r *dbtypes.EnvironmentRetirement) { r.Child.UID = "other" }},
		{"active environment", func(r *dbtypes.EnvironmentRetirement) { r.Environment.Name = "active" }},
		{"different database", func(r *dbtypes.EnvironmentRetirement) { r.Databases[0].DatabaseName = "other" }},
		{"missing namespace", func(r *dbtypes.EnvironmentRetirement) { r.Child.Namespace = "" }},
		{"cross namespace", func(r *dbtypes.EnvironmentRetirement) { r.Databases[0].Resources[0].Namespace = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, request, database := retirementDeclaration(t)
			test.change(&request)
			done, err := NewEnvironmentRetirer(c, c).RetireEnvironment(t.Context(), request, func(context.Context) error { return nil })
			require.Error(t, err)
			assert.False(t, done)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(database), database))
			assert.Equal(t, cnpgv1.DatabaseReclaimDelete, database.Spec.ReclaimPolicy)
			assert.Nil(t, database.DeletionTimestamp)
		})
	}
}

func TestEnvironmentRetirementRechecksIntentBeforeDelete(t *testing.T) {
	c, request, database := retirementDeclaration(t)
	database.Spec.ReclaimPolicy = cnpgv1.DatabaseReclaimRetain
	require.NoError(t, c.Update(t.Context(), database))
	checks := 0
	changed := errors.New("request changed")
	done, err := NewEnvironmentRetirer(c, c).RetireEnvironment(t.Context(), request, func(context.Context) error {
		checks++
		if checks == 2 {
			return changed
		}
		return nil
	})
	require.ErrorIs(t, err, changed)
	assert.False(t, done)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(database), database))
	assert.Nil(t, database.DeletionTimestamp)
	require.Equal(t, 2, checks)
}

func TestEnvironmentRetirementRequiresCompleteInventory(t *testing.T) {
	c, request, database := retirementDeclaration(t)
	request.Databases[0].Resources = nil
	done, err := NewEnvironmentRetirer(c, c).RetireEnvironment(t.Context(), request, func(context.Context) error { return nil })
	require.ErrorContains(t, err, "inventory changed")
	assert.False(t, done)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(database), database))
	assert.Equal(t, cnpgv1.DatabaseReclaimDelete, database.Spec.ReclaimPolicy)
}
