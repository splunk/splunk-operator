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
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCleanupPreservesSharedCAsAndRecoverySnapshots(t *testing.T) {
	f := newCleanupFixture(t)
	f.active.Spec.Certificates.ClientCASecret = "client-ca"
	require.NoError(t, f.client.Update(t.Context(), f.active))
	owner := *metav1.NewControllerRef(f.retired, cnpgv1.SchemeGroupVersion.WithKind("Cluster"))
	unrelated := metav1.OwnerReference{APIVersion: "v1", Kind: "ConfigMap", Name: "audit", UID: "audit-uid"}
	for _, name := range []string{"shared-ca", "client-ca"} {
		require.NoError(t, f.client.Create(t.Context(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns", UID: types.UID(name), OwnerReferences: []metav1.OwnerReference{owner, unrelated}}, Data: map[string][]byte{"ca.crt": []byte("keep"), "ca.key": []byte("private")}}))
	}
	for _, name := range []string{"data", "wal", "tablespace"} {
		require.NoError(t, f.client.Create(t.Context(), &snapshotv1.VolumeSnapshot{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns", UID: types.UID(name), OwnerReferences: []metav1.OwnerReference{owner, unrelated}}}))
	}
	foreign := owner
	foreign.UID = "replacement"
	for _, name := range []string{"foreign", "backup-owned", "ownerless"} {
		owners := []metav1.OwnerReference{foreign}
		if name == "backup-owned" {
			owners = []metav1.OwnerReference{{APIVersion: cnpgv1.SchemeGroupVersion.String(), Kind: "Backup", Name: "anchor", UID: "backup-uid"}}
		} else if name == "ownerless" {
			owners = nil
		}
		require.NoError(t, f.client.Create(t.Context(), &snapshotv1.VolumeSnapshot{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns", UID: types.UID(name), OwnerReferences: owners}}))
	}
	deleted := false
	for range 12 {
		f.retire(t)
		if apierrors.IsNotFound(f.client.Get(t.Context(), client.ObjectKeyFromObject(f.retired), &cnpgv1.Cluster{})) {
			deleted = true
			break
		}
	}
	require.True(t, deleted)
	for _, name := range []string{"shared-ca", "client-ca"} {
		secret := &corev1.Secret{}
		require.NoError(t, f.client.Get(t.Context(), client.ObjectKey{Namespace: "ns", Name: name}, secret))
		assert.True(t, metav1.IsControlledBy(secret, f.active))
		assert.Contains(t, secret.OwnerReferences, unrelated)
		assert.Equal(t, map[string][]byte{"ca.crt": []byte("keep"), "ca.key": []byte("private")}, secret.Data)
	}
	for _, name := range []string{"data", "wal", "tablespace", "foreign", "backup-owned", "ownerless"} {
		snapshot := &snapshotv1.VolumeSnapshot{}
		require.NoError(t, f.client.Get(t.Context(), client.ObjectKey{Namespace: "ns", Name: name}, snapshot))
		switch name {
		case "data", "wal", "tablespace":
			assert.Equal(t, []metav1.OwnerReference{unrelated}, snapshot.OwnerReferences, "Cluster deletion must follow snapshot preservation")
		case "foreign":
			assert.Equal(t, []metav1.OwnerReference{foreign}, snapshot.OwnerReferences)
		case "backup-owned":
			assert.Equal(t, "Backup", snapshot.OwnerReferences[0].Kind)
		case "ownerless":
			assert.Empty(t, snapshot.OwnerReferences)
		}
	}
}

func TestCleanupSnapshotPreservationChecksFreshAuthority(t *testing.T) {
	f := newCleanupFixture(t)
	owner := *metav1.NewControllerRef(f.retired, cnpgv1.SchemeGroupVersion.WithKind("Cluster"))
	snapshot := &snapshotv1.VolumeSnapshot{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "ns", UID: "data-uid", OwnerReferences: []metav1.OwnerReference{owner}}}
	require.NoError(t, f.client.Create(t.Context(), snapshot))
	done, err := preserveClusterSnapshots(t.Context(), f.client, f.client, testResourceReference(t, f.retired), func(context.Context) error { return errors.New("authority changed") })
	assert.False(t, done)
	require.ErrorContains(t, err, "authority changed")
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(snapshot), snapshot))
	assert.Equal(t, []metav1.OwnerReference{owner}, snapshot.OwnerReferences)
}
