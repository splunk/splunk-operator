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
	"context"
	"errors"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"testing"
)

func TestKVServicePostgresDependencyMappings(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, enterpriseApi.AddToScheme(scheme))
	require.NoError(t, platformApi.AddToScheme(scheme))
	database := func(name, owner string) *platformApi.PostgresDatabase {
		return &platformApi.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "shared", Annotations: map[string]string{splcommon.KVServiceTenantDatabaseOwnerAnnotation: "shared/" + owner}},
			Status: platformApi.PostgresDatabaseStatus{Databases: []platformApi.DatabaseInfo{{Name: name, ConfigMapRef: &corev1.LocalObjectReference{Name: "connection"}, AdminUserSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "password"}, Key: "password"}}}}}
	}
	alpha, bravo := database("alpha-database", "kvs"), database("bravo-database", "kvs")
	foreign := database("foreign-database", "other")
	var gets, lists int
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(alpha, bravo, foreign).
		WithIndex(&platformApi.PostgresDatabase{}, KVServiceDatabaseDependencyIndex, KVServiceDatabaseDependencies).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
				gets++
				return errors.New("unexpected Get")
			},
			List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				lists++
				return c.List(ctx, list, opts...)
			},
		}).Build()
	mapper := KVServicePostgresMapper{Client: c}
	requests := mapper.MapDatabase(t.Context(), alpha)
	require.Len(t, requests, 1)
	require.Equal(t, "kvs", requests[0].Name)
	require.Zero(t, gets)
	require.Equal(t, 1, lists, "database events list only the hosting namespace consumer set")
	for _, event := range []client.Object{&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "connection", Namespace: "shared"}}, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "password", Namespace: "shared"}}} {
		lists = 0
		if _, ok := event.(*corev1.ConfigMap); ok {
			requests = mapper.MapConfigMap(t.Context(), event)
		} else {
			requests = mapper.MapAdminSecret(t.Context(), event)
		}
		require.Len(t, requests, 2, "deduplicate multiple tenant databases for the same KVService")
		require.ElementsMatch(t, []string{"kvs", "other"}, []string{requests[0].Name, requests[1].Name})
		require.Equal(t, 1, lists, "dependency events use one indexed list, not per-tenant reads")
		require.Zero(t, gets)
	}
	require.Empty(t, mapper.MapConfigMap(t.Context(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "shared"}}))
	require.Empty(t, mapper.MapAdminSecret(t.Context(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "password", Namespace: "elsewhere"}}))
	require.Empty(t, mapper.MapDatabase(t.Context(), &corev1.Service{}))
	require.Empty(t, mapper.MapDatabase(t.Context(), &platformApi.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "unmanaged", Namespace: "shared"}}))
	unannotated := &platformApi.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "retained", Namespace: "shared", Labels: map[string]string{"app.kubernetes.io/managed-by": "splunk-operator", "app.kubernetes.io/instance": "splunk-old-kvservice"}}}
	require.Empty(t, mapper.MapDatabase(t.Context(), unannotated), "labels alone must not assign database management")
	require.Empty(t, mapper.MapConfigMap(t.Context(), &corev1.Secret{}))
	require.Empty(t, mapper.MapAdminSecret(t.Context(), &corev1.Service{}))
}

func TestKVServiceDependencyIndexTracksStatusChanges(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, platformApi.AddToScheme(scheme))
	db := &platformApi.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "alpha-database", Namespace: "shared", Annotations: map[string]string{splcommon.KVServiceTenantDatabaseOwnerAnnotation: "shared/kvs"}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(db).WithStatusSubresource(db).WithIndex(db, KVServiceDatabaseDependencyIndex, KVServiceDatabaseDependencies).Build()
	mapper := KVServicePostgresMapper{Client: c}
	event := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "password", Namespace: "shared"}}
	require.Empty(t, mapper.MapAdminSecret(t.Context(), event))
	db.Status.Databases = []platformApi.DatabaseInfo{{Name: "alpha", AdminUserSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "password"}}}}
	require.NoError(t, c.Status().Update(t.Context(), db))
	require.Len(t, mapper.MapAdminSecret(t.Context(), event), 1)
	db.Status.Databases[0].AdminUserSecretRef.Name = "replacement"
	require.NoError(t, c.Status().Update(t.Context(), db))
	require.Empty(t, mapper.MapAdminSecret(t.Context(), event))
}

func TestKVServiceDependencyMappingHandlesListFailure(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, platformApi.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
		return errors.New("cache unavailable")
	}}).Build()
	mapper := KVServicePostgresMapper{Client: c}
	require.Empty(t, mapper.MapAdminSecret(t.Context(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "password", Namespace: "shared"}}))
	require.Empty(t, mapper.MapTenantNamespace(t.Context(), &corev1.Namespace{}))
}

func TestDatabaseCollisionRepairWakesBlockedConsumer(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, enterpriseApi.AddToScheme(scheme))
	require.NoError(t, platformApi.AddToScheme(scheme))
	blocked := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "blocked", Namespace: "shared"}}
	other := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "elsewhere", Namespace: "other"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(blocked, other).Build()
	mapper := KVServicePostgresMapper{Client: c}
	for _, annotation := range []string{"", "shared/previous-manager"} {
		db := &platformApi.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "alpha-database", Namespace: "shared", Annotations: map[string]string{splcommon.KVServiceTenantDatabaseOwnerAnnotation: annotation}}}
		requests := mapper.MapDatabase(t.Context(), db)
		names := []string{}
		for _, request := range requests {
			require.Equal(t, "shared", request.Namespace)
			names = append(names, request.Name)
		}
		require.Contains(t, names, "blocked", "repair/deletion must wake the stalled consumer without ownership metadata")
		require.NotContains(t, names, "elsewhere")
	}
}
