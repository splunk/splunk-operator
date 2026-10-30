// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestPostgresDatabaseWatch(t *testing.T) {
	for _, kind := range []string{"Standalone", "SearchHeadCluster"} {
		t.Run(kind, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, enterpriseApi.AddToScheme(scheme))
			require.NoError(t, platformApi.AddToScheme(scheme))
			require.NoError(t, corev1.AddToScheme(scheme))
			consumer := func(name, namespace, reference string) client.Object {
				mounts := []enterpriseApi.PostgresDatabaseMountSpec{{Name: "app", PostgresDatabaseRef: corev1.LocalObjectReference{Name: reference}, Database: "app"}}
				metadata := metav1.ObjectMeta{Name: name, Namespace: namespace}
				if kind == "Standalone" {
					return &enterpriseApi.Standalone{ObjectMeta: metadata, Spec: enterpriseApi.StandaloneSpec{PostgresDatabaseMounts: mounts}}
				}
				return &enterpriseApi.SearchHeadCluster{ObjectMeta: metadata, Spec: enterpriseApi.SearchHeadClusterSpec{PostgresDatabaseMounts: mounts}}
			}
			selected := consumer("selected", "ns", "db")
			db := &platformApi.PostgresDatabase{Name: "db", Namespace: "ns", Spec: platformApi.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "primary"}}, Status: platformApi.PostgresDatabaseStatus{Databases: []platformApi.DatabaseInfo{{Name: "app", ConfigMapRef: &corev1.LocalObjectReference{Name: "config"}, RWUserSecretRef: &corev1.SecretKeySelector{Name: "external-rw"}}}}}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(selected, consumer("other", "ns", "other"), consumer("cross-namespace", "other", "db"), db).
				WithIndex(selected, postgresDatabaseRefIndex, func(object client.Object) []string {
					return []string{postgresMountSpecs(object)[0].PostgresDatabaseRef.Name}
				}).Build()
			newList := func() client.ObjectList { return &enterpriseApi.StandaloneList{} }
			if kind == "SearchHeadCluster" {
				newList = func() client.ObjectList { return &enterpriseApi.SearchHeadClusterList{} }
			}
			watch := postgresDatabaseWatch{client: c, newList: newList}
			expected := []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(selected)}}
			assert.Equal(t, expected, watch.mapDatabase(t.Context(), db))
			assert.Equal(t, expected, watch.mapResource(t.Context(), &corev1.Secret{Name: "primary-ca", Namespace: "ns"}))
			assert.Empty(t, watch.mapResource(t.Context(), &corev1.Secret{Name: "primary-ca", Namespace: "other"}))
			cluster := &platformApi.PostgresCluster{Name: "primary", Namespace: "ns"}
			assert.Equal(t, expected, watch.mapResource(t.Context(), cluster))
			updatedCluster := cluster.DeepCopy()
			updatedCluster.Status.Phase = new("Ready")
			assert.True(t, postgresDatabaseChangedPredicate().Update(event.UpdateEvent{ObjectOld: cluster, ObjectNew: updatedCluster}))
			assert.Equal(t, expected, watch.mapResource(t.Context(), &corev1.Secret{Name: "external-rw", Namespace: "ns"}))
			assert.Equal(t, expected, watch.mapResource(t.Context(), &corev1.ConfigMap{Name: "config", Namespace: "ns"}))
			assert.Empty(t, watch.mapResource(t.Context(), &corev1.Secret{Name: "unrelated", Namespace: "ns"}))
			assert.Empty(t, watch.mapResource(t.Context(), &corev1.Secret{Name: "external-rw", Namespace: "other"}))
			updated := db.DeepCopy()
			updated.Status.ObservedGeneration = new(int64(2))
			assert.True(t, postgresDatabaseChangedPredicate().Update(event.UpdateEvent{ObjectOld: db, ObjectNew: updated}), "status-only publications must not be filtered out")
		})
	}
}
