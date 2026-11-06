// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package k8sops

import (
	"context"
	"testing"

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
)

func TestKVServiceTenantDependencyWatches(t *testing.T) {
	t.Setenv("WATCH_NAMESPACE", "")
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, enterpriseApi.AddToScheme(scheme))
	require.NoError(t, platformApi.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	cr := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "kvs", Namespace: "shared"}, Spec: enterpriseApi.KVServiceSpec{Postgres: enterpriseApi.KVServicePostgresSpec{ClusterRef: corev1.LocalObjectReference{Name: "pg"}}}}
	nsA := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns-a", Labels: map[string]string{splcommon.LabelKVService: "enabled", splcommon.LabelTenant: "alpha"}, Annotations: map[string]string{splcommon.AnnotationKVServiceAddr: "splunk-kvs-kvservice-service.shared:8066"}}}
	nsB := nsA.DeepCopy()
	nsB.Name = "ns-b"
	nsB.Labels[splcommon.LabelTenant] = "bravo"
	db := &platformApi.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "bravo-database", Namespace: "shared", Annotations: map[string]string{splcommon.KVServiceTenantDatabaseOwnerAnnotation: "shared/kvs"}}, Status: platformApi.PostgresDatabaseStatus{Databases: []platformApi.DatabaseInfo{{Name: "bravo", ConfigMapRef: &corev1.LocalObjectReference{Name: "bravo-config"}, AdminUserSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "bravo-owner"}, Key: "password"}}}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cr, nsA, nsB, db).WithIndex(&platformApi.PostgresDatabase{}, KVServiceDatabaseDependencyIndex, KVServiceDatabaseDependencies).WithInterceptorFuncs(interceptor.Funcs{List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
		if _, ok := list.(*corev1.NamespaceList); ok {
			t.Fatal("watch mappers must not repeat namespace discovery")
		}
		return c.List(ctx, list, opts...)
	}}).Build()
	mapper := KVServicePostgresMapper{Client: c}
	for _, namespace := range []string{"ns-a", "ns-b"} {
		requests := mapper.MapTenantNamespaceSecret(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "splunk-" + namespace + "-secret", Namespace: namespace}})
		require.Len(t, requests, 1)
		require.Equal(t, "shared", requests[0].Namespace)
	}
	require.Empty(t, mapper.MapTenantNamespaceSecret(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "splunk-unrelated-secret", Namespace: "unrelated"}}))
	require.Len(t, mapper.MapTenantNamespace(ctx, nsA), 1)
	// An address change must wake the old backend and stop mapping its source Secret.
	nsA.Annotations[splcommon.AnnotationKVServiceAddr] = "other.shared:8066"
	require.NoError(t, c.Update(ctx, nsA))
	require.Len(t, mapper.MapTenantNamespace(ctx, nsA), 1)
	require.Empty(t, mapper.MapTenantNamespaceSecret(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "splunk-ns-a-secret", Namespace: "ns-a"}}))
	nsA.Annotations[splcommon.AnnotationKVServiceAddr] = "splunk-kvs-kvservice-service.shared:8066"
	require.NoError(t, c.Update(ctx, nsA))
	// Removed labels must still wake the old backend; discovery then excludes it.
	nsA.Labels = nil
	require.NoError(t, c.Update(ctx, nsA))
	require.Len(t, mapper.MapTenantNamespace(ctx, nsA), 1)
	require.Empty(t, mapper.MapTenantNamespaceSecret(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "splunk-ns-a-secret", Namespace: "ns-a"}}))
	require.Len(t, mapper.MapTenantNamespace(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "deleted"}}), 1)
	require.Empty(t, mapper.MapTenantNamespace(ctx, &corev1.ConfigMap{}))
	require.Len(t, mapper.MapDatabase(ctx, db), 1)
	require.Len(t, mapper.MapConfigMap(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "bravo-config", Namespace: "shared"}}), 1)
	require.Len(t, mapper.MapAdminSecret(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "bravo-owner", Namespace: "shared"}}), 1)
}
