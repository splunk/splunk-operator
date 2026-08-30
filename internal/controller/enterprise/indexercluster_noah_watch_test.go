// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestMapNoahClusterToIndexerClusters(t *testing.T) {
	reconciler := newNoahWatchTestReconciler(t,
		noahIndexerCluster("selected", "test", "noah"),
		noahIndexerCluster("other-ref", "test", "other-noah"),
		noahIndexerCluster("other-namespace", "other", "noah"),
		&enterpriseApi.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Name: "classic", Namespace: "test"}},
	)

	requests := reconciler.mapNoahClusterToIndexerClusters(context.Background(), &enterpriseApi.NoahCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "noah", Namespace: "test"},
	})

	assert.ElementsMatch(t, []reconcile.Request{{
		NamespacedName: types.NamespacedName{Name: "selected", Namespace: "test"},
	}}, requests)
	assert.Nil(t, reconciler.mapNoahClusterToIndexerClusters(context.Background(), &corev1.Secret{}))
}

func TestMapNoahAuthSecretToIndexerClusters(t *testing.T) {
	reconciler := newNoahWatchTestReconciler(t,
		&enterpriseApi.NoahCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "noah", Namespace: "test"},
			Spec: enterpriseApi.NoahClusterSpec{
				AuthSecretRef: corev1.LocalObjectReference{Name: "noah-auth"},
			},
		},
		&enterpriseApi.NoahCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "shared-auth", Namespace: "test"},
			Spec: enterpriseApi.NoahClusterSpec{
				AuthSecretRef: corev1.LocalObjectReference{Name: "noah-auth"},
			},
		},
		&enterpriseApi.NoahCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "other-secret", Namespace: "test"},
			Spec: enterpriseApi.NoahClusterSpec{
				AuthSecretRef: corev1.LocalObjectReference{Name: "other-auth"},
			},
		},
		&enterpriseApi.NoahCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "noah", Namespace: "other"},
			Spec: enterpriseApi.NoahClusterSpec{
				AuthSecretRef: corev1.LocalObjectReference{Name: "noah-auth"},
			},
		},
		noahIndexerCluster("selected", "test", "noah"),
		noahIndexerCluster("also-selected", "test", "shared-auth"),
		noahIndexerCluster("other-secret", "test", "other-secret"),
		noahIndexerCluster("other-namespace", "other", "noah"),
	)

	requests := reconciler.mapNoahAuthSecretToIndexerClusters(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noah-auth", Namespace: "test"},
	})

	assert.ElementsMatch(t, []reconcile.Request{
		{NamespacedName: types.NamespacedName{Name: "selected", Namespace: "test"}},
		{NamespacedName: types.NamespacedName{Name: "also-selected", Namespace: "test"}},
	}, requests)
	assert.Nil(t, reconciler.mapNoahAuthSecretToIndexerClusters(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "test"},
	}))
	assert.Nil(t, reconciler.mapNoahAuthSecretToIndexerClusters(context.Background(), &enterpriseApi.NoahCluster{}))
}

func newNoahWatchTestReconciler(t *testing.T, objects ...client.Object) *IndexerClusterReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, enterpriseApi.AddToScheme(scheme))
	return &IndexerClusterReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build(),
		Scheme: scheme,
	}
}

func noahIndexerCluster(name, namespace, noahClusterName string) *enterpriseApi.IndexerCluster {
	return &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: enterpriseApi.IndexerClusterSpec{
			NoahClusterRef: &corev1.LocalObjectReference{Name: noahClusterName},
		},
	}
}
