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

package majorupgradeadapter

import (
	"context"
	"errors"
	"slices"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	tlsport "github.com/splunk/splunk-operator/pkg/postgresql/cluster/ports/tls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestCleanupMutationGuardPreservesReplicationCR(t *testing.T) {
	f := newCleanupFixture(t)
	bg := f.retirementStatus(t)
	publication := &cnpgv1.Publication{ObjectMeta: metav1.ObjectMeta{Name: "publication", Namespace: "ns", UID: "pub-uid", Labels: map[string]string{blueGreenAttemptLabel: bg.AttemptID}, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(f.parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}}, Spec: cnpgv1.PublicationSpec{ClusterRef: corev1.LocalObjectReference{Name: f.retired.Name}}}
	require.NoError(t, f.client.Create(t.Context(), publication))
	bg.Cleanup.RetiredResources = []corev1.ObjectReference{testResourceReference(t, publication)}
	checks := 0
	_, err := f.runtime.retireResources(t.Context(), f.parent, bg, f.active, func(context.Context) error {
		checks++
		if checks == 2 {
			return errors.New("changed intent")
		}
		return nil
	})
	require.ErrorContains(t, err, "changed intent")
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(publication), publication))
	assert.Empty(t, publication.Spec.ReclaimPolicy)
	_, err = f.runtime.retireResources(t.Context(), f.parent, bg, f.active, func(context.Context) error { return nil })
	require.NoError(t, err)
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(publication), publication))
	assert.Equal(t, cnpgv1.PublicationReclaimRetain, publication.Spec.ReclaimPolicy)
}

func TestCleanupWaitsForTerminatingReplicationWriters(t *testing.T) {
	for _, kind := range []string{"Subscription", "Publication"} {
		t.Run(kind, func(t *testing.T) {
			f := newCleanupFixture(t)
			bg := f.retirementStatus(t)
			var object client.Object
			var finalizer, parameter string
			parameters := func() map[string]string {
				if sub, ok := object.(*cnpgv1.Subscription); ok {
					return sub.Spec.Parameters
				}
				return object.(*cnpgv1.Publication).Spec.Parameters
			}
			if kind == "Subscription" {
				object = &cnpgv1.Subscription{Spec: cnpgv1.SubscriptionSpec{ClusterRef: corev1.LocalObjectReference{Name: f.retired.Name}}}
				finalizer, parameter = cnpgutils.SubscriptionFinalizerName, "enabled"
			} else {
				object = &cnpgv1.Publication{Spec: cnpgv1.PublicationSpec{ClusterRef: corev1.LocalObjectReference{Name: f.retired.Name}}}
				finalizer, parameter = cnpgutils.PublicationFinalizerName, "publish_via_partition_root"
			}
			object.SetName("replication")
			object.SetNamespace(f.parent.Namespace)
			object.SetUID("replication-uid")
			object.SetGeneration(1)
			object.SetLabels(map[string]string{blueGreenAttemptLabel: bg.AttemptID})
			object.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(f.parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))})
			object.SetFinalizers([]string{finalizer, "example.com/other"})
			require.NoError(t, f.client.Create(t.Context(), object))
			assert.False(t, f.retire(t))
			require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(object), object))
			assert.False(t, retainReplicationDeclaration(object), "Retain must already be persisted")
			assert.Nil(t, object.GetDeletionTimestamp())
			assert.Empty(t, parameters(), "never wake an applicable declaration")
			// Model CNPG acknowledging the Retain generation before deletion.
			object.SetGeneration(2)
			if sub, ok := object.(*cnpgv1.Subscription); ok {
				sub.Status.ObservedGeneration = 2
			} else {
				object.(*cnpgv1.Publication).Status.ObservedGeneration = 2
			}
			require.NoError(t, f.client.Update(t.Context(), object))
			assert.False(t, f.retire(t))
			require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(object), object))
			require.NotNil(t, object.GetDeletionTimestamp())
			guardErr := errors.New("retirement intent changed")
			_, err := f.runtime.retireResources(t.Context(), f.parent, f.retirementStatus(t), f.active, func(context.Context) error { return guardErr })
			require.ErrorIs(t, err, guardErr)
			require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(object), object))
			assert.Empty(t, parameters())
			assert.False(t, f.retire(t))
			require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(object), object))
			assert.Equal(t, "false", parameters()[parameter])
			assert.Equal(t, []string{finalizer, "example.com/other"}, object.GetFinalizers())
			require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.retired), &cnpgv1.Cluster{}))
			object.SetGeneration(3)
			require.NoError(t, f.client.Update(t.Context(), object))
			version := object.GetResourceVersion()
			assert.False(t, f.retire(t))
			require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(object), object))
			assert.Equal(t, version, object.GetResourceVersion(), "wait while CNPG processes the new generation")
			// Model provider acknowledgement; an unrelated finalizer must still block.
			object.SetFinalizers(slices.DeleteFunc(object.GetFinalizers(), func(v string) bool { return v == finalizer }))
			require.NoError(t, f.client.Update(t.Context(), object))
			assert.False(t, f.retire(t))
			require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.retired), &cnpgv1.Cluster{}))
			object.SetFinalizers(nil)
			require.NoError(t, f.client.Update(t.Context(), object))
			assert.False(t, f.retire(t))
			assert.True(t, f.retire(t))
		})
	}
}

func TestCleanupRejectsDeletingReplicationWithDeletePolicy(t *testing.T) {
	for _, object := range []client.Object{&cnpgv1.Subscription{}, &cnpgv1.Publication{}} {
		t.Run(testResourceReference(t, object).Kind, func(t *testing.T) {
			f := newCleanupFixture(t)
			object.SetName("replication")
			object.SetNamespace(f.parent.Namespace)
			object.SetUID("replication-uid")
			object.SetLabels(map[string]string{blueGreenAttemptLabel: f.parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen.AttemptID})
			object.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(f.parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))})
			object.SetFinalizers([]string{"example.com/keep"})
			switch value := object.(type) {
			case *cnpgv1.Subscription:
				value.Spec.ClusterRef.Name = f.retired.Name
				value.Spec.ReclaimPolicy = cnpgv1.SubscriptionReclaimDelete
			case *cnpgv1.Publication:
				value.Spec.ClusterRef.Name = f.retired.Name
				value.Spec.ReclaimPolicy = cnpgv1.PublicationReclaimDelete
			}
			require.NoError(t, f.client.Create(t.Context(), object))
			require.NoError(t, f.client.Delete(t.Context(), object))
			_, err := f.runtime.retireResources(t.Context(), f.parent, f.retirementStatus(t), f.active, func(context.Context) error { return nil })
			require.ErrorContains(t, err, "deleting Retain intent")
			require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(object), object))
			assert.Equal(t, []string{"example.com/keep"}, object.GetFinalizers())
		})
	}
}

func TestCleanupEngineSnapshotErrorsProtectCluster(t *testing.T) {
	for _, test := range []struct {
		name          string
		err           error
		permitsDelete bool
	}{
		{"CRD absent", &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: snapshotv1.GroupName, Kind: "VolumeSnapshot"}}, true},
		{"forbidden", apierrors.NewForbidden(schema.GroupResource{Group: snapshotv1.GroupName, Resource: "volumesnapshots"}, "", errors.New("denied")), false},
		{"unavailable", errors.New("snapshot API unavailable"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newCleanupFixture(t)
			bg := f.retirementStatus(t)
			lists := 0
			f.runtime.reader = interceptor.NewClient(f.client.(client.WithWatch), interceptor.Funcs{List: func(ctx context.Context, next client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*snapshotv1.VolumeSnapshotList); ok {
					lists++
					return test.err
				}
				return next.List(ctx, list, opts...)
			}})
			_, err := f.runtime.retireResources(t.Context(), f.parent, bg, f.active, func(context.Context) error { return nil })
			require.Equal(t, 1, lists)
			liveErr := f.client.Get(t.Context(), client.ObjectKeyFromObject(f.retired), f.retired)
			if test.permitsDelete {
				require.NoError(t, err)
				assert.True(t, apierrors.IsNotFound(liveErr))
			} else {
				require.ErrorIs(t, err, test.err)
				require.NoError(t, liveErr)
				assert.Nil(t, f.retired.DeletionTimestamp)
			}
		})
	}
}

func TestCleanupEnginePreservesBackupAndWaitsForStorage(t *testing.T) {
	f := newCleanupFixture(t)
	owner := []metav1.OwnerReference{*metav1.NewControllerRef(f.retired, cnpgv1.SchemeGroupVersion.WithKind("Cluster"))}
	backup := &cnpgv1.Backup{ObjectMeta: metav1.ObjectMeta{Name: "source-anchor", Namespace: "ns", UID: "anchor-uid", OwnerReferences: owner}, Spec: cnpgv1.BackupSpec{Cluster: cnpgv1.LocalObjectReference{Name: f.retired.Name}}, Status: cnpgv1.BackupStatus{Phase: cnpgv1.BackupPhaseCompleted}}
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "old-storage", Namespace: "ns", UID: "pvc-uid", OwnerReferences: owner}}
	require.NoError(t, f.client.Create(t.Context(), backup))
	require.NoError(t, f.client.Create(t.Context(), pvc))
	assert.False(t, f.retire(t))
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(backup), backup))
	assert.Empty(t, backup.OwnerReferences)
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.retired), &cnpgv1.Cluster{}))
	assert.False(t, f.retire(t))
	assert.False(t, f.retire(t), "storage absence is independent of Cluster deletion")
	require.NoError(t, f.client.Delete(t.Context(), pvc))
	assert.True(t, f.retire(t))
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(backup), backup))
}

func TestCleanupTLSLeavesProtectActiveDependencies(t *testing.T) {
	f := newCleanupFixture(t)
	bg := f.retirementStatus(t)
	bg.Cleanup.RetiredResources = append(bg.Cleanup.RetiredResources, corev1.ObjectReference{Kind: "Certificate", UID: "old-cert-uid"})
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "old-tls", Namespace: "ns", UID: "old-tls-uid", Labels: map[string]string{tlsport.ClusterUIDLabel: string(f.parent.UID), tlsport.PurposeLabel: tlsport.ServerTLSPurpose}}, Type: corev1.SecretTypeTLS}
	for _, owner := range []types.UID{"", f.parent.UID, "old-cert-uid"} {
		secret.OwnerReferences = nil
		if owner != "" {
			secret.OwnerReferences = []metav1.OwnerReference{{UID: owner, Controller: new(true)}}
		}
		require.NoError(t, validateCleanupRoot(f.parent, bg, f.active, nil, secret))
	}
	secret.OwnerReferences = []metav1.OwnerReference{{UID: "foreign", Controller: new(true)}}
	require.Error(t, validateCleanupRoot(f.parent, bg, f.active, nil, secret))
	secret.OwnerReferences = nil
	f.active.Spec.Certificates.ServerTLSSecret = secret.Name
	require.Error(t, validateCleanupRoot(f.parent, bg, f.active, nil, secret))
	f.active.Spec.Certificates.ServerTLSSecret = "active-tls"
	require.Error(t, validateCleanupRoot(f.parent, bg, f.active, map[string]bool{secret.Name: true}, secret))
}
