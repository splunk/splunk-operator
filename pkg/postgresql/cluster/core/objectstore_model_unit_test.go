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
package core

import (
	"context"
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestObjectStoreModel_Reconcile_DeletesWhenBackupDisabled(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	cfg := newTestMergedConfigBarman("0 2 * * *")
	cfg.Spec.Backup.Enabled = ptr.To(false)
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(ObjectStoreGVK)
	existing.SetName(objectStoreName(cluster.Name))
	existing.SetNamespace(cluster.Namespace)
	require.NoError(t, ctrl.SetControllerReference(cluster, existing, scheme))

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	model := newObjectStoreModel(c, scheme, noopEventEmitter{}, noopHealthUpdater, cluster, cfg)

	reconcileErr := model.Reconcile(context.Background())
	health, err := model.Observe(context.Background(), reconcileErr)

	require.NoError(t, reconcileErr)
	require.NoError(t, err)
	assert.Equal(t, reasonObjectStoreDisabled, health.Reason)
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(ObjectStoreGVK)
	err = c.Get(context.Background(), types.NamespacedName{Name: objectStoreName(cluster.Name), Namespace: cluster.Namespace}, got)
	assert.True(t, apierrors.IsNotFound(err), "ObjectStore must be removed when backup is disabled")
}

func TestObjectStoreModel_Reconcile_CreatesWhenBackupEnabled(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	cfg := newTestMergedConfigBarman("0 2 * * *")
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	model := newObjectStoreModel(c, scheme, noopEventEmitter{}, noopHealthUpdater, cluster, cfg)

	reconcileErr := model.Reconcile(context.Background())
	health, err := model.Observe(context.Background(), reconcileErr)

	require.NoError(t, reconcileErr)
	require.NoError(t, err)
	assert.Equal(t, reasonObjectStoreConfigured, health.Reason)
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(ObjectStoreGVK)
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: objectStoreName(cluster.Name), Namespace: cluster.Namespace}, got))
}

// foreignObjectStore builds an ObjectStore with the deterministic name but owned by a
// different controller, simulating a pre-existing or orphaned object the operator must not touch.
func foreignObjectStore(cluster *platformv1alpha1.PostgresCluster) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(ObjectStoreGVK)
	obj.SetName(objectStoreName(cluster.Name))
	obj.SetNamespace(cluster.Namespace)
	obj.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Name:       "someone-else",
		UID:        "foreign-uid",
		Controller: ptr.To(true),
	}})
	obj.Object["spec"] = map[string]interface{}{"configuration": map[string]interface{}{"destinationPath": "s3://foreign"}}
	return obj
}

func TestObjectStoreModel_Reconcile_DoesNotDeleteForeignObjectStore(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	cfg := newTestMergedConfigBarman("0 2 * * *")
	cfg.Spec.Backup.Enabled = ptr.To(false)
	existing := foreignObjectStore(cluster)

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	model := newObjectStoreModel(c, scheme, noopEventEmitter{}, noopHealthUpdater, cluster, cfg)

	require.NoError(t, model.Reconcile(context.Background()))

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(ObjectStoreGVK)
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: objectStoreName(cluster.Name), Namespace: cluster.Namespace}, got),
		"foreign ObjectStore must not be deleted")
}

func TestObjectStoreModel_Reconcile_DoesNotMutateForeignObjectStore(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	cfg := newTestMergedConfigBarman("0 2 * * *")
	existing := foreignObjectStore(cluster)

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	model := newObjectStoreModel(c, scheme, noopEventEmitter{}, noopHealthUpdater, cluster, cfg)

	err := model.Reconcile(context.Background())
	require.Error(t, err, "reconcile must fail rather than mutate a foreign ObjectStore")

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(ObjectStoreGVK)
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: objectStoreName(cluster.Name), Namespace: cluster.Namespace}, got))
	cfgField, _, _ := unstructured.NestedString(got.Object, "spec", "configuration", "destinationPath")
	assert.Equal(t, "s3://foreign", cfgField, "foreign ObjectStore spec must be left untouched")
}
