// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package k8sops

import (
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgdatabase "github.com/splunk/splunk-operator/pkg/postgresql/database/core"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func postgresDeploymentFixture(t *testing.T, custom bool) (*enterpriseApi.KVService, *platformApi.PostgresDatabase, client.Client) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, enterpriseApi.AddToScheme(scheme))
	require.NoError(t, platformApi.AddToScheme(scheme))
	cr := &enterpriseApi.KVService{
		TypeMeta:   metav1.TypeMeta{APIVersion: enterpriseApi.GroupVersion.String(), Kind: "KVService"},
		ObjectMeta: metav1.ObjectMeta{Name: "kv", Namespace: "test", UID: types.UID("kv-id")},
		Spec:       enterpriseApi.KVServiceSpec{Replicas: 1, Postgres: &enterpriseApi.KVServicePostgresSpec{}},
	}
	cr.Spec.Image = "kvstore/kvservice:test"
	cr.Spec.ImagePullPolicy = "IfNotPresent"
	resourceName, logicalName := "kvstore-database", "kvstore"
	if custom {
		resourceName, logicalName = "tenant-database", "tenantdb"
		cr.Spec.Postgres.DatabaseRef = &corev1.LocalObjectReference{Name: resourceName}
		cr.Spec.Postgres.DatabaseName = logicalName
	}
	database := &platformApi.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: cr.Namespace},
		Status: platformApi.PostgresDatabaseStatus{Databases: []platformApi.DatabaseInfo{{Name: logicalName, Ready: true,
			ConfigMapRef:       &corev1.LocalObjectReference{Name: "pg-connection"},
			AdminUserSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "pg-admin"}, Key: "password"},
		}}},
	}
	connection := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "pg-connection", Namespace: cr.Namespace}, Data: map[string]string{
		pgconninfo.KeyClusterRWEndpoint: "postgres-rw.test.svc", pgconninfo.KeyDefaultClusterPort: "5432", pgdatabase.ConfigMapKeyAdminUser: "kv-admin",
	}}
	admin := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg-admin", Namespace: cr.Namespace}, Data: map[string][]byte{"password": []byte("test-db-password")}}
	auth := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: splcommon.GetNamespaceScopedSecretName(cr.Namespace), Namespace: cr.Namespace}, Data: map[string][]byte{}}
	for _, key := range splcommon.GetSplunkSecretTokenTypes() {
		auth.Data[key] = []byte("test-" + key)
	}
	auth.Data["hec_token"] = []byte("ABCDEF01-2345-6789-ABCD-EF0123456789")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cr, database, connection, admin, auth).Build()
	return cr, database, c
}

func TestKVServicePostgresDeploymentConfiguration(t *testing.T) {
	for _, custom := range []bool{false, true} {
		name := "default names"
		if custom {
			name = "custom names"
		}
		t.Run(name, func(t *testing.T) {
			cr, database, c := postgresDeploymentFixture(t, custom)
			assert.Equal(t, database.Name, KVServicePostgresDatabaseResourceName(cr))
			assert.Equal(t, database.Status.Databases[0].Name, KVServicePostgresLogicalDatabaseName(cr))
			deployment, err := GetKVServiceDeployment(t.Context(), c, cr, nil)
			require.NoError(t, err)
			env := map[string]corev1.EnvVar{}
			for _, value := range deployment.Spec.Template.Spec.Containers[0].Env {
				env[value.Name] = value
			}
			assert.Equal(t, "true", env["KVSERVICE_SINGLE_TENANT_K8S_MODE"].Value)
			for _, name := range []string{"KVSERVICE_DB_HOST", "KVSERVICE_SINGLE_TENANT_PRIMARY_HOST", "KVSERVICE_SINGLE_TENANT_REPLICA_HOST"} {
				require.NotNil(t, env[name].ValueFrom, name)
				assert.Equal(t, &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "pg-connection"}, Key: pgconninfo.KeyClusterRWEndpoint}, env[name].ValueFrom.ConfigMapKeyRef)
			}
			for _, name := range []string{"KVSERVICE_DB_PORT", "KVSERVICE_SINGLE_TENANT_PRIMARY_PORT", "KVSERVICE_SINGLE_TENANT_REPLICA_PORT"} {
				require.NotNil(t, env[name].ValueFrom, name)
				assert.Equal(t, pgconninfo.KeyDefaultClusterPort, env[name].ValueFrom.ConfigMapKeyRef.Key)
			}
			for _, name := range []string{"KVSERVICE_DB_ROOT_USER", "KVSERVICE_DB_SERVICE_USER"} {
				require.NotNil(t, env[name].ValueFrom, name)
				assert.Equal(t, pgdatabase.ConfigMapKeyAdminUser, env[name].ValueFrom.ConfigMapKeyRef.Key)
			}
			for _, name := range []string{"KVSERVICE_DB_PASSWORD", "KVSERVICE_DB_SERVICE_PASSWORD"} {
				require.NotNil(t, env[name].ValueFrom, name)
				assert.Empty(t, env[name].Value, "credentials must remain Secret references")
				assert.Equal(t, database.Status.Databases[0].AdminUserSecretRef, env[name].ValueFrom.SecretKeyRef)
			}
			auth := env["KVSERVICE_SINGLE_TENANT_PASS4_SYMM_KEY"]
			require.NotNil(t, auth.ValueFrom)
			assert.Equal(t, &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: splcommon.GetNamespaceScopedSecretName(cr.Namespace)}, Key: "pass4SymmKey"}, auth.ValueFrom.SecretKeyRef)
			assert.Len(t, deployment.Spec.Template.Annotations["enterprise.splunk.com/kvservice-postgres-config"], 64)
			for _, key := range []string{"enterprise.splunk.com/kvservice-postgres-admin-secret", "enterprise.splunk.com/kvservice-pass4-secret"} {
				value := deployment.Spec.Template.Annotations[key]
				assert.Contains(t, value, cr.Namespace+"/")
				assert.NotContains(t, value, "test-db-password")
				assert.NotContains(t, value, "test-pass4SymmKey")
			}

			dependencies, ready, err := ResolveKVServicePostgresDependencies(t.Context(), c, cr, database, database.Status.Databases[0].Name)
			require.NoError(t, err)
			require.True(t, ready)
			require.NoError(t, c.Delete(t.Context(), database))
			snapshot, err := GetKVServiceDeploymentWithPostgresDependencies(t.Context(), c, cr, dependencies, nil)
			require.NoError(t, err, "the validated snapshot must not reread the PostgresDatabase")
			assert.Equal(t, deployment.Spec.Template, snapshot.Spec.Template)
			_, err = GetKVServiceDeploymentWithPostgresDependencies(t.Context(), c, cr, nil, nil)
			require.ErrorContains(t, err, "validated PostgreSQL dependencies are required")
			_, err = GetKVServiceDeployment(t.Context(), c, cr, nil)
			require.Error(t, err, "the direct resolver must report a missing database")
		})
	}
}

func TestKVServicePostgresDependencyChangesRollDeployment(t *testing.T) {
	tests := []struct {
		name, kind, key, value string
		changedAnnotation      string
	}{
		{name: "endpoint", kind: "config", key: pgconninfo.KeyClusterRWEndpoint, value: "new-postgres-rw.test.svc", changedAnnotation: "enterprise.splunk.com/kvservice-postgres-config"},
		{name: "port", kind: "config", key: pgconninfo.KeyDefaultClusterPort, value: "6432", changedAnnotation: "enterprise.splunk.com/kvservice-postgres-config"},
		{name: "username", kind: "config", key: pgdatabase.ConfigMapKeyAdminUser, value: "new-admin", changedAnnotation: "enterprise.splunk.com/kvservice-postgres-config"},
		{name: "database password", kind: "admin", key: "password", value: "new-password", changedAnnotation: "enterprise.splunk.com/kvservice-postgres-admin-secret"},
		{name: "authentication key", kind: "auth", key: "pass4SymmKey", value: "new-pass4-secret", changedAnnotation: "enterprise.splunk.com/kvservice-pass4-secret"},
		{name: "unrelated ConfigMap key", kind: "config", key: "unrelated", value: "ignored"},
		{name: "unrelated admin key changes revision", kind: "admin", key: "unrelated", value: "ignored", changedAnnotation: "enterprise.splunk.com/kvservice-postgres-admin-secret"},
		{name: "unrelated authentication key changes revision", kind: "auth", key: "unrelated", value: "ignored", changedAnnotation: "enterprise.splunk.com/kvservice-pass4-secret"},
		{name: "ConfigMap insertion order", kind: "reorder"},
		{name: "ConfigMap metadata", kind: "metadata"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr, _, c := postgresDeploymentFixture(t, false)
			original, err := GetKVServiceDeployment(t.Context(), c, cr, nil)
			require.NoError(t, err)
			initialAnnotations := original.Spec.Template.DeepCopy().Annotations
			_, err = ApplyDeployment(t.Context(), c, original)
			require.NoError(t, err)
			stored := &appsv1.Deployment{}
			key := types.NamespacedName{Namespace: cr.Namespace, Name: original.Name}
			require.NoError(t, c.Get(t.Context(), key, stored))
			version := stored.ResourceVersion
			if tt.kind == "admin" || tt.kind == "auth" {
				name := "pg-admin"
				if tt.kind == "auth" {
					name = splcommon.GetNamespaceScopedSecretName(cr.Namespace)
				}
				secret := &corev1.Secret{}
				require.NoError(t, c.Get(t.Context(), types.NamespacedName{Namespace: cr.Namespace, Name: name}, secret))
				secret.Data[tt.key] = []byte(tt.value)
				require.NoError(t, c.Update(t.Context(), secret))
			} else {
				cm := &corev1.ConfigMap{}
				require.NoError(t, c.Get(t.Context(), types.NamespacedName{Namespace: cr.Namespace, Name: "pg-connection"}, cm))
				switch tt.kind {
				case "config":
					cm.Data[tt.key] = tt.value
				case "reorder":
					cm.Data = map[string]string{pgdatabase.ConfigMapKeyAdminUser: cm.Data[pgdatabase.ConfigMapKeyAdminUser], pgconninfo.KeyDefaultClusterPort: cm.Data[pgconninfo.KeyDefaultClusterPort], pgconninfo.KeyClusterRWEndpoint: cm.Data[pgconninfo.KeyClusterRWEndpoint]}
				case "metadata":
					cm.Annotations = map[string]string{"note": "changed"}
				}
				require.NoError(t, c.Update(t.Context(), cm))
			}
			desired, err := GetKVServiceDeployment(t.Context(), c, cr, nil)
			require.NoError(t, err)
			for annotation, value := range initialAnnotations {
				if annotation == tt.changedAnnotation {
					assert.NotEqual(t, value, desired.Spec.Template.Annotations[annotation])
				} else {
					assert.Equal(t, value, desired.Spec.Template.Annotations[annotation], annotation)
				}
			}
			phase, err := ApplyDeployment(t.Context(), c, desired)
			require.NoError(t, err)
			require.NoError(t, c.Get(t.Context(), key, stored))
			if tt.changedAnnotation != "" {
				assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
				assert.NotEqual(t, version, stored.ResourceVersion)
			} else {
				assert.Equal(t, version, stored.ResourceVersion, "unrelated changes must not roll the workload")
			}
		})
	}
}

func TestKVServicePostgresDependencyReadinessAndReadErrors(t *testing.T) {
	for _, name := range []string{"no database info", "not ready", "no ConfigMap reference", "no Secret reference", "missing ConfigMap", "missing admin Secret", "missing namespace Secret"} {
		t.Run(name, func(t *testing.T) {
			cr, database, c := postgresDeploymentFixture(t, false)
			switch name {
			case "no database info":
				database.Status.Databases = nil
			case "not ready":
				database.Status.Databases[0].Ready = false
			case "no ConfigMap reference":
				database.Status.Databases[0].ConfigMapRef = nil
			case "no Secret reference":
				database.Status.Databases[0].AdminUserSecretRef = nil
			case "missing ConfigMap":
				require.NoError(t, c.Delete(t.Context(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "pg-connection", Namespace: cr.Namespace}}))
			case "missing admin Secret":
				require.NoError(t, c.Delete(t.Context(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg-admin", Namespace: cr.Namespace}}))
			case "missing namespace Secret":
				require.NoError(t, c.Delete(t.Context(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: splcommon.GetNamespaceScopedSecretName(cr.Namespace), Namespace: cr.Namespace}}))
			}
			dependencies, ready, err := ResolveKVServicePostgresDependencies(t.Context(), c, cr, database, "kvstore")
			assert.Nil(t, dependencies)
			assert.False(t, ready)
			if name == "missing ConfigMap" || name == "missing admin Secret" || name == "missing namespace Secret" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, c.Update(t.Context(), database))
			deployment, err := GetKVServiceDeployment(t.Context(), c, cr, nil)
			require.Error(t, err, "incomplete PostgreSQL dependencies must block Deployment construction")
			assert.Nil(t, deployment)
		})
	}
}
