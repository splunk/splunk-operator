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
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestKVServicePostgresDependencyMappings(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, enterpriseApi.AddToScheme(scheme))
	require.NoError(t, platformApi.AddToScheme(scheme))
	consumer := func(name, namespace, resource, logical string) *enterpriseApi.KVService {
		cr := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
		cr.Spec.Postgres = &enterpriseApi.KVServicePostgresSpec{DatabaseName: logical}
		if resource != "" {
			cr.Spec.Postgres.DatabaseRef = &corev1.LocalObjectReference{Name: resource}
		}
		return cr
	}
	database := func(name, logical string) *platformApi.PostgresDatabase {
		return &platformApi.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test"}, Status: platformApi.PostgresDatabaseStatus{Databases: []platformApi.DatabaseInfo{{Name: logical,
			ConfigMapRef: &corev1.LocalObjectReference{Name: "connection"}, AdminUserSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "admin"}, Key: "password"},
		}}}}
	}
	objects := []client.Object{
		consumer("default", "test", "", ""), consumer("custom", "test", "custom-db", "tenantdb"),
		consumer("different-logical", "test", "", "otherdb"), consumer("unrelated", "test", "other-db", ""),
		consumer("other-namespace", "other", "", ""), &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "no-postgres", Namespace: "test"}},
		database("kvstore-database", "kvstore"), database("custom-db", "tenantdb"),
	}
	type mapper func(*KVServicePostgresMapper, context.Context, client.Object) []reconcile.Request
	tests := []struct {
		name     string
		mapEvent mapper
		event    client.Object
		want     []string
	}{
		{name: "default database", mapEvent: (*KVServicePostgresMapper).MapDatabase, event: database("kvstore-database", "kvstore"), want: []string{"default", "different-logical"}},
		{name: "custom database", mapEvent: (*KVServicePostgresMapper).MapDatabase, event: database("custom-db", "tenantdb"), want: []string{"custom"}},
		{name: "ConfigMap", mapEvent: (*KVServicePostgresMapper).MapConfigMap, event: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "connection", Namespace: "test"}}, want: []string{"default", "custom"}},
		{name: "admin Secret", mapEvent: (*KVServicePostgresMapper).MapAdminSecret, event: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "admin", Namespace: "test"}}, want: []string{"default", "custom"}},
		{name: "namespace Secret", mapEvent: (*KVServicePostgresMapper).MapNamespaceSecret, event: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: splcommon.GetNamespaceScopedSecretName("test"), Namespace: "test"}}, want: []string{"default", "custom", "different-logical", "unrelated"}},
		{name: "unrelated ConfigMap", mapEvent: (*KVServicePostgresMapper).MapConfigMap, event: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "test"}}},
		{name: "unrelated Secret", mapEvent: (*KVServicePostgresMapper).MapNamespaceSecret, event: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "test"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			r := &KVServicePostgresMapper{Client: c}
			requests := tt.mapEvent(r, t.Context(), tt.event)
			names := []string{}
			for _, request := range requests {
				assert.Equal(t, "test", request.Namespace)
				names = append(names, request.Name)
			}
			assert.ElementsMatch(t, tt.want, names)
			assert.Empty(t, tt.mapEvent(r, t.Context(), &corev1.Service{}), "other object types must be ignored")
			failing := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
				return errors.New("list failed")
			}}).Build()
			assert.Empty(t, tt.mapEvent(&KVServicePostgresMapper{Client: failing}, t.Context(), tt.event))
		})
	}
}
