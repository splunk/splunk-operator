// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
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
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

func TestMapUserSecretToIngestorClusters(t *testing.T) {
	objects := []client.Object{
		usersIngestorCluster("selected", "test", "observability-user"),
		usersIngestorCluster("also-selected", "test", "automation-user", "observability-user"),
		usersIngestorCluster("same-secret-twice", "test", "observability-user", "observability-user"),
		usersIngestorCluster("other-secret", "test", "automation-user"),
		usersIngestorCluster("other-namespace", "other", "observability-user"),
		&enterpriseApi.IngestorCluster{ObjectMeta: metav1.ObjectMeta{Name: "no-users", Namespace: "test"}},
	}

	tests := []struct {
		name string
		obj  client.Object
		want []reconcile.Request
	}{
		{
			name: "requeues each same-namespace IngestorCluster referencing the Secret once",
			obj:  &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "observability-user", Namespace: "test"}},
			want: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "selected", Namespace: "test"}},
				{NamespacedName: types.NamespacedName{Name: "also-selected", Namespace: "test"}},
				{NamespacedName: types.NamespacedName{Name: "same-secret-twice", Namespace: "test"}},
			},
		},
		{
			name: "ignores a Secret no IngestorCluster references",
			obj:  &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "test"}},
		},
		{
			name: "ignores a non-Secret object",
			obj:  &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "observability-user", Namespace: "test"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reconciler := &IngestorClusterReconciler{Client: usersWatchClient(t, interceptor.Funcs{}, objects...)}
			assert.ElementsMatch(t, tt.want, reconciler.mapUserSecretToIngestorClusters(context.Background(), tt.obj))
		})
	}
}

func TestMapUserSecretToIngestorClustersListError(t *testing.T) {
	reconciler := &IngestorClusterReconciler{Client: usersWatchClient(t, interceptor.Funcs{
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			return errors.New("cache unavailable")
		},
	}, usersIngestorCluster("selected", "test", "observability-user"))}

	assert.Nil(t, reconciler.mapUserSecretToIngestorClusters(context.Background(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "observability-user", Namespace: "test"}}))
}

func TestExtractIngestorClusterUserSecretNames(t *testing.T) {
	tests := []struct {
		name string
		obj  client.Object
		want []string
	}{
		{
			name: "one user",
			obj:  usersIngestorCluster("ic", "test", "observability-user"),
			want: []string{"observability-user"},
		},
		{
			name: "several users",
			obj:  usersIngestorCluster("ic", "test", "automation-user", "observability-user"),
			want: []string{"automation-user", "observability-user"},
		},
		{
			name: "users sharing a secret",
			obj:  usersIngestorCluster("ic", "test", "shared-user", "shared-user"),
			want: []string{"shared-user", "shared-user"},
		},
		{
			name: "no users",
			obj:  usersIngestorCluster("ic", "test"),
		},
		{
			name: "not an IngestorCluster",
			obj:  &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "observability-user", Namespace: "test"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractIngestorClusterUserSecretNames(tt.obj))
		})
	}
}

func usersWatchClient(t *testing.T, funcs interceptor.Funcs, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, enterpriseApi.AddToScheme(scheme))
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithIndex(&enterpriseApi.IngestorCluster{}, indexIngestorClusterUserSecrets, extractIngestorClusterUserSecretNames).
		WithInterceptorFuncs(funcs).
		Build()
}

func usersIngestorCluster(name, namespace string, secretNames ...string) *enterpriseApi.IngestorCluster {
	ic := &enterpriseApi.IngestorCluster{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	for i, secretName := range secretNames {
		ic.Spec.Users = append(ic.Spec.Users, enterpriseApi.SplunkUser{
			Name:              fmt.Sprintf("internal_%d", i),
			Roles:             []string{"user"},
			PasswordSecretRef: corev1.LocalObjectReference{Name: secretName},
		})
	}
	return ic
}
