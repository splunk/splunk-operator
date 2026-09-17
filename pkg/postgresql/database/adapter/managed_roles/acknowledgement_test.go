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

package managedroles

import (
	"context"
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestAcknowledgementReaderMapsAndCopiesClusterStatus(t *testing.T) {
	scheme := managedRolesScheme(t)
	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "primary", Namespace: "dbs"},
		Status: platformv1alpha1.PostgresClusterStatus{ManagedRolesStatus: &platformv1alpha1.ManagedRolesStatus{
			Reconciled: []string{"payments_admin"},
			Failed:     map[string]string{"payments_rw": "provider rejected role"},
			RoleOwners: map[string]platformv1alpha1.RoleOwnerReference{
				"payments_admin": {Name: "tenant", UID: "database-uid"},
			},
			Conflicts: []platformv1alpha1.RoleConflict{{
				Role: "payments_rw", AttemptedBy: platformv1alpha1.RoleOwnerReference{Name: "tenant", UID: "database-uid"},
			}},
		}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()

	acknowledgement, err := NewAcknowledgementReader(c).Read(
		t.Context(),
		dbtypes.ManagedRoleAcknowledgementTarget{Namespace: "dbs", ClusterName: "primary"},
	)
	require.NoError(t, err)
	assert.True(t, acknowledgement.Published)
	assert.Equal(t, []string{"payments_admin"}, acknowledgement.Reconciled)
	assert.Equal(t, map[string]string{"payments_rw": "provider rejected role"}, acknowledgement.Failed)
	assert.Equal(t, dbtypes.ManagedRoleParticipant{Name: "tenant", UID: "database-uid"}, acknowledgement.Owners["payments_admin"])
	assert.Equal(t, []dbtypes.ManagedRoleConflict{{
		Role: "payments_rw", AttemptedBy: dbtypes.ManagedRoleParticipant{Name: "tenant", UID: "database-uid"},
	}}, acknowledgement.Conflicts)

	acknowledgement.Reconciled[0] = "mutated"
	acknowledgement.Failed["payments_rw"] = "mutated"
	acknowledgement.Owners["payments_admin"] = dbtypes.ManagedRoleParticipant{Name: "mutated"}
	assert.Equal(t, "payments_admin", cluster.Status.ManagedRolesStatus.Reconciled[0])
	assert.Equal(t, "provider rejected role", cluster.Status.ManagedRolesStatus.Failed["payments_rw"])
	assert.Equal(t, "tenant", cluster.Status.ManagedRolesStatus.RoleOwners["payments_admin"].Name)
}

func TestAcknowledgementReaderTreatsMissingStatusAsUnpublished(t *testing.T) {
	scheme := managedRolesScheme(t)
	cluster := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "primary", Namespace: "dbs"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()

	acknowledgement, err := NewAcknowledgementReader(c).Read(
		t.Context(),
		dbtypes.ManagedRoleAcknowledgementTarget{Namespace: "dbs", ClusterName: "primary"},
	)

	require.NoError(t, err)
	assert.False(t, acknowledgement.Published)
}

func TestAcknowledgementReaderClassifiesAndPreservesReadError(t *testing.T) {
	scheme := managedRolesScheme(t)
	readErr := apierrors.NewTimeoutError("apiserver unavailable", 1)
	c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
			return readErr
		},
	}).Build()

	_, err := NewAcknowledgementReader(c).Read(
		t.Context(),
		dbtypes.ManagedRoleAcknowledgementTarget{Namespace: "dbs", ClusterName: "primary"},
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, dbtypes.ErrManagedRoleAcknowledgementRead)
	assert.ErrorIs(t, err, readErr)
	assert.True(t, apierrors.IsTimeout(err))
}
