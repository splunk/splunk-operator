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
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	cnpginfra "github.com/splunk/splunk-operator/pkg/postgresql/database/infrastructure/cnpg"
	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBuildDatabaseSpecMapsEveryOption(t *testing.T) {
	desired := dbtypes.DesiredDatabase{
		Name:       "payments",
		Owner:      "tenant_owner",
		Reclaim:    dbtypes.ReclaimRetain,
		Extensions: []string{"pg_trgm"},
		Creation: dbtypes.DatabaseCreationOptions{
			Template: "template0", Encoding: "UTF8", Locale: "en_US.UTF-8",
			LocaleProvider: "icu", LocaleCollate: "en_US.UTF-8", LocaleCType: "en_US.UTF-8",
			ICULocale: "en-US", ICURules: "&V << w", CollationVersion: "153.120",
		},
		Mutable: dbtypes.DatabaseMutableOptions{
			IsTemplate: ptr.To(false), AllowConnections: ptr.To(true),
			ConnectionLimit: ptr.To(int32(0)), Tablespace: "fastspace",
		},
	}

	got, err := buildDatabaseSpec("cnpg-primary", desired, cnpgv1.Database{Spec: cnpgv1.DatabaseSpec{
		Extensions: []cnpgv1.ExtensionSpec{{
			DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "unaccent", Ensure: cnpgv1.EnsurePresent},
		}},
	}})
	require.NoError(t, err)

	assert.Equal(t, "payments", got.Name)
	assert.Equal(t, "tenant_owner", got.Owner)
	assert.Equal(t, corev1.LocalObjectReference{Name: "cnpg-primary"}, got.ClusterRef)
	assert.Equal(t, cnpgv1.DatabaseReclaimRetain, got.ReclaimPolicy)
	assert.Equal(t, "template0", got.Template)
	assert.Equal(t, "UTF8", got.Encoding)
	assert.Equal(t, "en_US.UTF-8", got.Locale)
	assert.Equal(t, "icu", got.LocaleProvider)
	assert.Equal(t, "en_US.UTF-8", got.LcCollate)
	assert.Equal(t, "en_US.UTF-8", got.LcCtype)
	assert.Equal(t, "en-US", got.IcuLocale)
	assert.Equal(t, "&V << w", got.IcuRules)
	assert.Equal(t, "153.120", got.CollationVersion)
	require.NotNil(t, got.IsTemplate)
	assert.False(t, *got.IsTemplate)
	require.NotNil(t, got.AllowConnections)
	assert.True(t, *got.AllowConnections)
	require.NotNil(t, got.ConnectionLimit)
	assert.Zero(t, *got.ConnectionLimit)
	assert.Equal(t, "fastspace", got.Tablespace)
	assert.Equal(t, []cnpgv1.ExtensionSpec{
		{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "pg_trgm", Ensure: cnpgv1.EnsurePresent}},
		{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "unaccent", Ensure: cnpgv1.EnsureAbsent}},
	}, got.Extensions)
}

func TestBuildDatabaseSpecMapsBuiltinLocaleAndOmittedMutableValues(t *testing.T) {
	got, err := buildDatabaseSpec("cnpg-primary", dbtypes.DesiredDatabase{
		Name: "analytics", Owner: "analytics_admin",
		Creation: dbtypes.DatabaseCreationOptions{LocaleProvider: "builtin", BuiltinLocale: "C.UTF-8"},
	}, cnpgv1.Database{})
	require.NoError(t, err)

	assert.Equal(t, "builtin", got.LocaleProvider)
	assert.Equal(t, "C.UTF-8", got.BuiltinLocale)
	assert.Nil(t, got.IsTemplate)
	assert.Nil(t, got.AllowConnections)
	assert.Nil(t, got.ConnectionLimit)
	assert.Nil(t, got.Extensions)
}

func TestBuildDatabaseSpecRejectsDesiredClosedDatabaseWithManagedExtensions(t *testing.T) {
	_, err := buildDatabaseSpec("cnpg-primary", dbtypes.DesiredDatabase{
		Name: "payments", Extensions: []string{"pg_trgm"},
		Mutable: dbtypes.DatabaseMutableOptions{AllowConnections: ptr.To(false)},
	}, cnpgv1.Database{})

	require.ErrorIs(t, err, errClosedDatabaseWithManagedExtensions)
}

func TestBuildDatabaseSpecStagesExtensionRemovalBeforeClose(t *testing.T) {
	desired := dbtypes.DesiredDatabase{
		Name:    "payments",
		Mutable: dbtypes.DatabaseMutableOptions{AllowConnections: ptr.To(false)},
	}
	database := cnpgv1.Database{
		ObjectMeta: metav1.ObjectMeta{Generation: 1},
		Spec: cnpgv1.DatabaseSpec{
			AllowConnections: ptr.To(true),
			Extensions: []cnpgv1.ExtensionSpec{{
				DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "pg_trgm", Ensure: cnpgv1.EnsurePresent},
			}},
		},
		Status: cnpgv1.DatabaseStatus{Applied: ptr.To(true), ObservedGeneration: 1},
	}
	wantAbsent := []cnpgv1.ExtensionSpec{{
		DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "pg_trgm", Ensure: cnpgv1.EnsureAbsent},
	}}

	removing, err := buildDatabaseSpec("cnpg-primary", desired, database)
	require.NoError(t, err)
	require.NotNil(t, removing.AllowConnections)
	assert.True(t, *removing.AllowConnections)
	assert.Equal(t, wantAbsent, removing.Extensions)

	database.Generation = 2
	database.Spec = removing
	waiting, err := buildDatabaseSpec("cnpg-primary", desired, database)
	require.NoError(t, err)
	require.NotNil(t, waiting.AllowConnections)
	assert.True(t, *waiting.AllowConnections)
	assert.Equal(t, wantAbsent, waiting.Extensions)

	database.Status.ObservedGeneration = database.Generation
	closing, err := buildDatabaseSpec("cnpg-primary", desired, database)
	require.NoError(t, err)
	require.NotNil(t, closing.AllowConnections)
	assert.False(t, *closing.AllowConnections)
	assert.Empty(t, closing.Extensions)
}

func TestDatabaseProvisionerAppliesAndObservesExactGeneration(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&cnpgv1.Database{}).Build()
	provisioner := NewDatabaseProvisioner(c, scheme)
	target := dbtypes.ProvisionerTarget{
		Namespace: "dbs", PostgresDatabaseName: "primary",
		PostgresDatabaseUID: "owner-uid", ProviderClusterName: "cnpg-primary",
	}

	applied, err := provisioner.Apply(t.Context(), target, []dbtypes.DesiredDatabase{{
		ResourceName: "primary-payments", Name: "payments", Owner: "payments_admin",
	}})
	require.NoError(t, err)
	require.Len(t, applied.Expected, 1)

	database := &cnpgv1.Database{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "primary-payments", Namespace: "dbs"}, database))
	require.Len(t, database.OwnerReferences, 1)
	assert.Equal(t, types.UID("owner-uid"), database.OwnerReferences[0].UID)
	database.Status.Applied = ptr.To(true)
	database.Status.ObservedGeneration = applied.Expected[0].Generation
	require.NoError(t, c.Status().Update(t.Context(), database))

	observation, err := provisioner.Observe(t.Context(), target, applied.Expected)
	require.NoError(t, err)
	require.Len(t, observation.Databases, 1)
	assert.True(t, observation.Databases[0].Found)
	assert.Equal(t, string(database.UID), observation.Databases[0].UID)
	assert.True(t, observation.Databases[0].Applied)
	assert.Equal(t, applied.Expected[0].Generation, observation.Databases[0].ObservedGeneration)
}

func TestDatabaseProvisionerObservationPreservesStaleGeneration(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	database := &cnpgv1.Database{
		ObjectMeta: metav1.ObjectMeta{Name: "primary-payments", Namespace: "dbs", Generation: 2},
		Status:     cnpgv1.DatabaseStatus{Applied: ptr.To(true), ObservedGeneration: 1},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&cnpgv1.Database{}).WithObjects(database).Build()
	provisioner := NewDatabaseProvisioner(c, scheme)

	observation, err := provisioner.Observe(t.Context(), dbtypes.ProvisionerTarget{Namespace: "dbs"}, []dbtypes.ExpectedDatabase{{
		Name: "payments", ResourceName: "primary-payments", Generation: 2,
	}})

	require.NoError(t, err)
	require.Len(t, observation.Databases, 1)
	assert.True(t, observation.Databases[0].Applied)
	assert.Equal(t, int64(1), observation.Databases[0].ObservedGeneration)
}

func TestDatabaseProvisionerReAdoptsRetainedDatabase(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	database := &cnpgv1.Database{
		ObjectMeta: metav1.ObjectMeta{
			Name: "primary-payments", Namespace: "dbs",
			Annotations: map[string]string{cnpginfra.AnnotationRetainedFrom: "primary"},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(database).Build()
	provisioner := NewDatabaseProvisioner(c, scheme)
	target := dbtypes.ProvisionerTarget{
		Namespace: "dbs", PostgresDatabaseName: "primary",
		PostgresDatabaseUID: "owner-uid", ProviderClusterName: "cnpg-primary",
	}

	result, err := provisioner.Apply(t.Context(), target, []dbtypes.DesiredDatabase{{
		ResourceName: "primary-payments", Name: "payments", Owner: "payments_admin",
	}})

	require.NoError(t, err)
	assert.Equal(t, []string{"payments"}, result.Adopted)
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: database.Name, Namespace: database.Namespace}, database))
	assert.NotContains(t, database.Annotations, cnpginfra.AnnotationRetainedFrom)
	require.Len(t, database.OwnerReferences, 1)
	assert.Equal(t, types.UID("owner-uid"), database.OwnerReferences[0].UID)
}
