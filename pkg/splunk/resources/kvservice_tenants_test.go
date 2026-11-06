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

package resources

import (
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestKVServiceTenantResourceSafety(t *testing.T) {
	cr := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "backend", Namespace: "shared", UID: "backend-uid"}, Spec: enterpriseApi.KVServiceSpec{
		Postgres: enterpriseApi.KVServicePostgresSpec{ClusterRef: corev1.LocalObjectReference{Name: "pg"}},
	}}
	db := NewKVServiceTenantDatabase(cr, "tenant1")
	require.Equal(t, "tenant1-database", db.Name)
	require.Empty(t, db.OwnerReferences, "retained database metadata must not be garbage collected with KVService")
	require.Equal(t, "Retain", db.Spec.Databases[0].DeletionPolicy)
	require.Equal(t, "tenant1", db.Spec.Databases[0].Name)
	require.Equal(t, cr.Spec.Postgres.ClusterRef, db.Spec.ClusterRef)
	require.Equal(t, "shared/backend", db.Annotations[common.KVServiceTenantDatabaseOwnerAnnotation])
	ring := NewKVServiceTenantKeyring(cr)
	require.Equal(t, cr.UID, metav1.GetControllerOf(ring).UID)
	require.Equal(t, corev1.SecretTypeOpaque, ring.Type)
	wiring := NewKVServiceTenantRuntime("membership", ring.Name, "tenant1", []KVServiceTenantCredential{{Tenant: "tenant1", SecretRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "tenant-owner"}, Key: "db-password"}}})
	require.Len(t, wiring.Volumes, 3)
	for _, volume := range wiring.Volumes {
		if volume.Secret != nil {
			require.EqualValues(t, 0644, *volume.Secret.DefaultMode)
		}
		if volume.ConfigMap != nil {
			require.EqualValues(t, 0644, *volume.ConfigMap.DefaultMode)
		}
	}
	require.Equal(t, []corev1.KeyToPath{{Key: "db-password", Path: "password"}}, wiring.Volumes[2].Secret.Items)
	require.Equal(t, "tenant-owner", wiring.Volumes[2].Secret.SecretName)
	require.Equal(t, "/var/run/kvservice/tenant-passwords/tenant1", wiring.VolumeMounts[2].MountPath)
	for _, mount := range wiring.VolumeMounts {
		require.True(t, mount.ReadOnly)
	}
	require.Len(t, wiring.Environment, 4, "password values must not be injected into environment variables")
}
