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
	"fmt"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func indexedManagedRolesTestClient(scheme *runtime.Scheme, objs ...client.Object) *fake.ClientBuilder {
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).WithIndex(
		&enterprisev4.PostgresDatabase{},
		enterprisev4.PostgresDatabaseClusterRefNameField,
		func(obj client.Object) []string {
			db, ok := obj.(*enterprisev4.PostgresDatabase)
			if !ok || db.Spec.ClusterRef.Name == "" {
				return nil
			}
			return []string{db.Spec.ClusterRef.Name}
		},
	)
}

func postgresDatabaseWithManagedRoles(name string, roles []managedRole) *enterprisev4.PostgresDatabase {
	dbRoles := make([]enterprisev4.DatabaseRoleInfo, 0, len(roles))
	for _, role := range roles {
		info := enterprisev4.DatabaseRoleInfo{Name: role.Name, Exists: role.Exists}
		if role.Exists {
			secretName := role.Name + "-secret"
			if role.PasswordSecretRef != nil {
				secretName = role.PasswordSecretRef.Name
			}
			info.SecretRef = &corev1.LocalObjectReference{Name: secretName}
		}
		dbRoles = append(dbRoles, info)
	}
	return &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", UID: types.UID("uid-" + name)},
		Spec: enterprisev4.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "pg"},
		},
		Status: enterprisev4.PostgresDatabaseStatus{
			Databases: []enterprisev4.DatabaseInfo{{Name: "app", Roles: dbRoles}},
		},
	}
}

func TestSyncManagedRolesStatusFromCNPG(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		specRoles  []managedRole
		cnpgStatus cnpgv1.ManagedRoles
		reconciled []string
		pending    []string
		failed     map[string]string
	}{
		{
			name: "marks unreconciled desired role as pending",
			specRoles: []managedRole{
				{Name: "app_user", Exists: true},
			},
			cnpgStatus: cnpgv1.ManagedRoles{},
			reconciled: nil,
			pending:    []string{"app_user"},
			failed:     nil,
		},
		{
			name: "maps reconciled and pending roles from CNPG status",
			specRoles: []managedRole{
				{Name: "app_user", Exists: true},
				{Name: "app_rw", Exists: true},
			},
			cnpgStatus: cnpgv1.ManagedRoles{
				ByStatus: map[cnpgv1.RoleStatus][]string{
					cnpgv1.RoleStatusReconciled:            {"app_user"},
					cnpgv1.RoleStatusPendingReconciliation: {"app_rw"},
				},
			},
			reconciled: []string{"app_user"},
			pending:    []string{"app_rw"},
			failed:     nil,
		},
		{
			name: "maps cannot reconcile errors as failed",
			specRoles: []managedRole{
				{Name: "app_user", Exists: true},
			},
			cnpgStatus: cnpgv1.ManagedRoles{
				CannotReconcile: map[string][]string{
					"app_user": {"reserved role"},
				},
			},
			reconciled: nil,
			pending:    nil,
			failed: map[string]string{
				"app_user": "reserved role",
			},
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cluster := &enterprisev4.PostgresCluster{}
			cnpgCluster := &cnpgv1.Cluster{
				Status: cnpgv1.ClusterStatus{
					ManagedRolesStatus: tt.cnpgStatus,
				},
			}

			syncManagedRolesStatusFromCNPG(cluster, cnpgCluster, tt.specRoles, nil, nil)

			require.NotNil(t, cluster.Status.ManagedRolesStatus)
			assert.Equal(t, tt.reconciled, cluster.Status.ManagedRolesStatus.Reconciled)
			assert.Equal(t, tt.pending, cluster.Status.ManagedRolesStatus.Pending)
			assert.Equal(t, tt.failed, cluster.Status.ManagedRolesStatus.Failed)
		})
	}
}

func TestManagedRolesModelConverge(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, enterprisev4.AddToScheme(scheme))

	makeCNPG := func(phase string, managedRoles cnpgv1.ManagedRoles) *cnpgv1.Cluster {
		return &cnpgv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
			Status:     cnpgv1.ClusterStatus{Phase: phase, ManagedRolesStatus: managedRoles},
		}
	}

	tests := []struct {
		name           string
		cnpg           *cnpgv1.Cluster
		specRoles      []managedRole
		expectedState  pgcConstants.State
		expectedReason conditionReasons
		expectErr      bool
		expectPending  []string
		expectFailed   map[string]string
	}{
		{
			name: "returns pending when CNPG cluster is not yet healthy",
			cnpg: makeCNPG(cnpgv1.PhaseFirstPrimary, cnpgv1.ManagedRoles{}),
			specRoles: []managedRole{
				{Name: "app_user", Exists: true},
			},
			expectedState:  pgcConstants.Pending,
			expectedReason: reasonManagedRolesPending,
			expectPending:  []string{"app_user"},
		},
		{
			name: "returns pending when role is still pending reconciliation",
			cnpg: makeCNPG(cnpgv1.PhaseHealthy, cnpgv1.ManagedRoles{
				ByStatus: map[cnpgv1.RoleStatus][]string{
					cnpgv1.RoleStatusPendingReconciliation: {"app_user"},
				},
			}),
			specRoles:      []managedRole{{Name: "app_user", Exists: true}},
			expectedState:  pgcConstants.Pending,
			expectedReason: reasonManagedRolesPending,
			expectPending:  []string{"app_user"},
		},
		{
			name: "returns failed when role cannot reconcile",
			cnpg: makeCNPG(cnpgv1.PhaseHealthy, cnpgv1.ManagedRoles{
				CannotReconcile: map[string][]string{"app_user": {"reserved role"}},
			}),
			specRoles:      []managedRole{{Name: "app_user", Exists: true}},
			expectedState:  pgcConstants.Failed,
			expectedReason: reasonManagedRolesFailed,
			expectErr:      true,
			expectFailed:   map[string]string{"app_user": "reserved role"},
		},
		{
			name: "returns ready when all desired roles are reconciled",
			cnpg: makeCNPG(cnpgv1.PhaseHealthy, cnpgv1.ManagedRoles{
				ByStatus: map[cnpgv1.RoleStatus][]string{
					cnpgv1.RoleStatusReconciled: {"app_user", "app_user_rw"},
				},
			}),
			specRoles: []managedRole{
				{Name: "app_user", Exists: true},
				{Name: "app_user_rw", Exists: true},
			},
			expectedState:  pgcConstants.Ready,
			expectedReason: reasonManagedRolesReady,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Arrange
			cluster := &enterprisev4.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"}}
			postgresDB := postgresDatabaseWithManagedRoles("app-db", tt.specRoles)
			contracts := &reconcileContracts{
				CNPGCluster: tt.cnpg,
				Secret:      &corev1.Secret{},
			}
			model := newManagedRolesModel(
				indexedManagedRolesTestClient(scheme, tt.cnpg, postgresDB).Build(), scheme, noopEventEmitter{}, nil, cluster, contracts, nil,
			)

			// Act
			reconcileErr := model.Reconcile(context.Background())
			health, err := model.Observe(context.Background(), reconcileErr)

			// Assert
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, managedRolesReady, health.Condition)
			assert.Equal(t, tt.expectedState, health.State)
			assert.Equal(t, tt.expectedReason, health.Reason)
			require.NotNil(t, cluster.Status.ManagedRolesStatus)
			assert.Equal(t, tt.expectPending, cluster.Status.ManagedRolesStatus.Pending)
			assert.Equal(t, tt.expectFailed, cluster.Status.ManagedRolesStatus.Failed)
		})
	}
}

func TestManagedRolesContractsNotReadyIsUpstreamPending(t *testing.T) {
	t.Parallel()

	// Arrange: contracts has no CNPGCluster — simulates clusterModel not yet ready.
	cluster := &enterprisev4.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"}}
	contracts := &reconcileContracts{}
	model := newManagedRolesModel(
		fake.NewClientBuilder().Build(), nil, noopEventEmitter{}, nil, cluster, contracts, nil,
	)

	// Act
	reconcileErr := model.CheckContracts()
	health, err := model.Observe(context.Background(), reconcileErr)

	// Assert
	require.ErrorIs(t, reconcileErr, errContractsNotReady)
	require.NoError(t, err)
	assert.Equal(t, managedRolesReady, health.Condition)
	assert.Equal(t, pgcConstants.Pending, health.State)
	assert.Equal(t, reasonUpstreamNotReady, health.Reason)
	assert.True(t, health.Result.RequeueAfter > 0)
}

func TestManagedRolesConvergeDoesNotEmitFailureForPending(t *testing.T) {
	t.Parallel()

	// Arrange
	cluster := &enterprisev4.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"}}
	postgresDB := postgresDatabaseWithManagedRoles("app-db", []managedRole{{Name: "app_user", Exists: true}})
	events := &captureEventEmitter{}
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy, ManagedRolesStatus: cnpgv1.ManagedRoles{}},
	}
	contracts := &reconcileContracts{CNPGCluster: cnpg, Secret: &corev1.Secret{}}
	scheme := runtime.NewScheme()
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, enterprisev4.AddToScheme(scheme))
	model := newManagedRolesModel(
		indexedManagedRolesTestClient(scheme, cnpg, postgresDB).Build(), scheme, events, nil, cluster, contracts, nil,
	)

	// Act
	reconcileErr := model.Reconcile(context.Background())
	_, err := model.Observe(context.Background(), reconcileErr)

	// Assert
	require.NoError(t, err)
	assert.Empty(t, events.warnings)
}

func TestManagedRolesConvergeEmitsReadyEventOnTransition(t *testing.T) {
	t.Parallel()

	// Arrange
	scheme := newTestScheme()
	cluster := &enterprisev4.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"}}
	postgresDB := postgresDatabaseWithManagedRoles("app-db", []managedRole{{Name: "app_user", Exists: true}})
	events := &captureEventEmitter{}
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status: cnpgv1.ClusterStatus{
			Phase: cnpgv1.PhaseHealthy,
			ManagedRolesStatus: cnpgv1.ManagedRoles{
				ByStatus: map[cnpgv1.RoleStatus][]string{
					cnpgv1.RoleStatusReconciled: {"app_user"},
				},
			},
		},
	}
	contracts := &reconcileContracts{CNPGCluster: cnpg, Secret: &corev1.Secret{}}
	model := newManagedRolesModel(indexedManagedRolesTestClient(scheme, cnpg, postgresDB).Build(), scheme, events, nil, cluster, contracts, nil)

	// Act: first Observe — condition is False, event must fire.
	reconcileErr := model.Reconcile(context.Background())
	_, err := model.Observe(context.Background(), reconcileErr)

	// Assert
	require.NoError(t, err)
	require.NotEmpty(t, events.normals)
	assert.Contains(t, events.normals[0], EventManagedRolesReady)

	// Act: second Observe with condition already True — no re-emission.
	cluster.Status.Conditions = []metav1.Condition{{Type: string(managedRolesReady), Status: metav1.ConditionTrue}}
	events.normals = nil
	_, err = model.Observe(context.Background(), nil)

	// Assert
	require.NoError(t, err)
	assert.Empty(t, events.normals)
}

func TestManagedRolesModelRetainsOwnersOnEmptyObservation(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy},
		Spec: cnpgv1.ClusterSpec{Managed: &cnpgv1.ManagedConfiguration{Roles: []cnpgv1.RoleConfiguration{
			{Name: "app_admin", Ensure: cnpgv1.EnsurePresent, Login: true},
		}}},
	}
	owner := enterprisev4.RoleOwnerReference{Name: "app-db", UID: "app-uid"}
	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"},
		Status: enterprisev4.PostgresClusterStatus{ManagedRolesStatus: &enterprisev4.ManagedRolesStatus{
			Reconciled: []string{"app_admin"},
			RoleOwners: map[string]enterprisev4.RoleOwnerReference{"app_admin": owner},
		}},
	}
	contracts := &reconcileContracts{CNPGCluster: cnpg, Secret: &corev1.Secret{}}
	model := newManagedRolesModel(
		indexedManagedRolesTestClient(scheme, cnpg).Build(),
		scheme, noopEventEmitter{}, nil, cluster, contracts, nil,
	)

	require.NoError(t, model.Reconcile(context.Background()))

	assert.Equal(t, owner, model.roleOwners["app_admin"])
	require.Len(t, model.desiredRoles, 1)
	assert.Equal(t, "app_admin", model.desiredRoles[0].Name)
	assert.True(t, model.desiredRoles[0].Exists)
}

func TestManagedRolesModelPreservesCurrentRolesForLegacyDatabaseStatus(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy},
		Spec: cnpgv1.ClusterSpec{Managed: &cnpgv1.ManagedConfiguration{Roles: []cnpgv1.RoleConfiguration{
			{Name: "app_admin", Ensure: cnpgv1.EnsurePresent, Login: true, PasswordSecret: &cnpgv1.LocalObjectReference{Name: "admin-secret"}},
		}}},
	}
	legacyDB := postgresDatabaseWithManagedRoles("legacy-db", nil)
	cluster := &enterprisev4.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"}}
	contracts := &reconcileContracts{CNPGCluster: cnpg, Secret: &corev1.Secret{}}
	model := newManagedRolesModel(
		indexedManagedRolesTestClient(scheme, cnpg, legacyDB).Build(),
		scheme, noopEventEmitter{}, nil, cluster, contracts, nil,
	)

	require.NoError(t, model.Reconcile(context.Background()))

	require.Len(t, model.desiredRoles, 1)
	assert.Equal(t, "app_admin", model.desiredRoles[0].Name)
	assert.True(t, model.desiredRoles[0].Exists)
	assert.Empty(t, model.roleOwners)
}

func TestManagedRolesModelNoOpWhenRolesEmpty(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy},
	}
	cluster := &enterprisev4.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"}}
	contracts := &reconcileContracts{CNPGCluster: cnpg, Secret: &corev1.Secret{}}
	model := newManagedRolesModel(
		indexedManagedRolesTestClient(scheme, cnpg).Build(),
		scheme, noopEventEmitter{}, nil, cluster, contracts, nil,
	)

	// Act
	reconcileErr := model.Reconcile(context.Background())
	health, err := model.Observe(context.Background(), reconcileErr)

	// Assert
	require.NoError(t, err)
	require.NoError(t, reconcileErr)
	assert.Equal(t, pgcConstants.Ready, health.State)
	assert.Equal(t, reasonManagedRolesReady, health.Reason)
}

// TestManagedRolesRuntimeGateHealthMatchesConverge verifies that when CNPG has not yet
// published ManagedRolesStatus (typical during PhaseFirstPrimary), managed roles stays
// Pending rather than surfacing a spurious failure.
func TestManagedRolesRuntimeGateHealthMatchesConverge(t *testing.T) {
	t.Parallel()

	cluster := &enterprisev4.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"}}
	postgresDB := postgresDatabaseWithManagedRoles("app-db", []managedRole{{Name: "app_user", Exists: true}})
	scheme := newTestScheme()
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status: cnpgv1.ClusterStatus{
			Phase:              cnpgv1.PhaseFirstPrimary,
			ManagedRolesStatus: cnpgv1.ManagedRoles{},
		},
	}
	contracts := &reconcileContracts{
		CNPGCluster: cnpg,
		Secret:      &corev1.Secret{},
	}
	model := newManagedRolesModel(
		indexedManagedRolesTestClient(scheme, cnpg, postgresDB).Build(),
		nil,
		noopEventEmitter{},
		nil,
		cluster,
		contracts,
		nil,
	)

	reconcileErr := model.Reconcile(context.Background())
	health, err := model.Observe(context.Background(), reconcileErr)
	require.NoError(t, err)
	assert.Equal(t, pgcConstants.Pending, health.State)
}

// TestManagedRolesNeedsCredentialSweep verifies the gating that makes the post-recovery
// sweep run exactly once: only for restore-bootstrapped clusters, and only until the sweep
// is recorded as completed in status.
func TestManagedRolesNeedsCredentialSweep(t *testing.T) {
	t.Parallel()

	restoreSource := &enterprisev4.BootstrapFrom{
		VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: "snap-1"},
	}

	tests := []struct {
		name    string
		cluster *enterprisev4.PostgresCluster
		want    bool
	}{
		{
			name:    "fresh cluster does not need sweep",
			cluster: &enterprisev4.PostgresCluster{Spec: enterprisev4.PostgresClusterSpec{}},
			want:    false,
		},
		{
			name:    "restore cluster without restore status needs sweep",
			cluster: &enterprisev4.PostgresCluster{Spec: enterprisev4.PostgresClusterSpec{BootstrapFrom: restoreSource}},
			want:    true,
		},
		{
			name: "restore cluster with incomplete sweep status needs sweep",
			cluster: &enterprisev4.PostgresCluster{
				Spec: enterprisev4.PostgresClusterSpec{BootstrapFrom: restoreSource},
				Status: enterprisev4.PostgresClusterStatus{
					Restore: &enterprisev4.RestoreStatus{
						CredentialSweep: enterprisev4.RestoreCredentialSweepStatus{Completed: false},
					},
				},
			},
			want: true,
		},
		{
			name: "restore cluster with completed sweep status does not need sweep",
			cluster: &enterprisev4.PostgresCluster{
				Spec: enterprisev4.PostgresClusterSpec{BootstrapFrom: restoreSource},
				Status: enterprisev4.PostgresClusterStatus{
					Restore: &enterprisev4.RestoreStatus{
						CredentialSweep: enterprisev4.RestoreCredentialSweepStatus{Completed: true},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model := &managedRolesModel{cluster: tt.cluster}
			assert.Equal(t, tt.want, model.needsCredentialSweep())
		})
	}
}

type stubRoleSweeperOK struct{ sweepCalled *bool }

func (s *stubRoleSweeperOK) SweepUnmanagedRolesAfterRestore(_ context.Context) error {
	*s.sweepCalled = true
	return nil
}

// TestManagedRolesCredentialSweepSuccess verifies that on a restore-bootstrapped cluster:
//  1. Reconcile runs the sweep (side-effect) and returns nil on success.
//  2. Observe persists status.restore with the source snapshot and CredentialSweep.Completed,
//     and returns a Provisioning/requeue health so the next reconcile re-enables managed roles.
//  3. The CNPG Cluster managed roles are NOT patched during the sweep pass.
func TestManagedRolesCredentialSweepSuccess(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()

	snapName := "source-pg-backup-20260501"
	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Spec: enterprisev4.PostgresClusterSpec{
			BootstrapFrom: &enterprisev4.BootstrapFrom{
				VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: snapName},
			},
		},
	}
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy},
	}
	contracts := &reconcileContracts{
		CNPGCluster: cnpg,
		Secret: &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret", Namespace: "default"},
			Data:       map[string][]byte{secretKeyPassword: []byte("pw")},
		},
	}

	sweepCalled := false
	stubSweeper := func(_ context.Context, _, _, _ string) (ports.RoleSweeper, error) {
		return &stubRoleSweeperOK{sweepCalled: &sweepCalled}, nil
	}

	events := &captureEventEmitter{}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg).Build()
	model := newManagedRolesModel(c, scheme, events, nil, cluster, contracts, stubSweeper)

	// Act
	reconcileErr := model.Reconcile(context.Background())
	// Boundary: Reconcile must not emit events — that is Observe's job.
	assert.Empty(t, events.normals, "Reconcile must not emit events — boundary violation")
	health, err := model.Observe(context.Background(), reconcileErr)

	// Assert: a successful sweep returns nil from Reconcile; the sweep ran.
	require.NoError(t, reconcileErr)
	assert.True(t, sweepCalled, "SweepUnmanagedRolesAfterRestore must be called")
	require.NoError(t, err)

	// Provisioning requeue so the next reconcile re-enables managed roles.
	assert.Equal(t, pgcConstants.Provisioning, health.State)
	assert.NotZero(t, health.Result.RequeueAfter)

	// Restore status recorded so the sweep is not repeated.
	require.NotNil(t, cluster.Status.Restore)
	assert.True(t, cluster.Status.Restore.CredentialSweep.Completed)
	require.NotNil(t, cluster.Status.Restore.Source.VolumeSnapshot)
	assert.Equal(t, snapName, *cluster.Status.Restore.Source.VolumeSnapshot)

	// CNPG managed roles must not be patched during the sweep pass.
	updatedCNPG := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(cnpg), updatedCNPG))
	assert.Nil(t, updatedCNPG.Spec.Managed)

	// Completion event emitted.
	require.NotEmpty(t, events.normals)
	assert.Contains(t, events.normals[0], EventUnmanagedRolesSweepDone)
}

// TestManagedRolesCredentialSweepConnectWaits verifies that when the restored DB is not
// reachable yet, the sweep does not fail the cluster: Reconcile returns the transient
// errSweepConnect, no status is persisted (so the sweep retries), and Observe returns a
// Provisioning requeue.
func TestManagedRolesCredentialSweepConnectWaits(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Spec: enterprisev4.PostgresClusterSpec{
			BootstrapFrom: &enterprisev4.BootstrapFrom{
				VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: "snap-1"},
			},
		},
	}
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy},
	}
	contracts := &reconcileContracts{
		CNPGCluster: cnpg,
		Secret:      &corev1.Secret{Data: map[string][]byte{secretKeyPassword: []byte("pw")}},
	}

	failingSweeper := func(_ context.Context, _, _, _ string) (ports.RoleSweeper, error) {
		return nil, assert.AnError
	}

	events := &captureEventEmitter{}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg).Build()
	model := newManagedRolesModel(c, scheme, events, nil, cluster, contracts, failingSweeper)

	// Act
	reconcileErr := model.Reconcile(context.Background())
	// Boundary: Reconcile must not emit events — that is Observe's job.
	assert.Empty(t, events.warnings, "Reconcile must not emit warnings — boundary violation")
	health, err := model.Observe(context.Background(), reconcileErr)

	// Assert: a connect failure waits (requeue), it does not fail the cluster.
	require.ErrorIs(t, reconcileErr, errSweepConnect)
	require.NoError(t, err)
	assert.Equal(t, pgcConstants.Provisioning, health.State)
	assert.NotZero(t, health.Result.RequeueAfter)

	// Status must NOT record completion so the sweep retries next reconcile.
	assert.Nil(t, cluster.Status.Restore)

	// A warning event is emitted by Observe for the failed connection.
	require.NotEmpty(t, events.warnings)
	assert.Contains(t, events.warnings[0], EventUnmanagedRolesSweepFailed)
}

// TestManagedRolesCredentialSweepConnectTerminal verifies that a terminal connect failure (the
// sweeper reports ErrSweeperConnectTerminal, e.g. bad superuser credentials) surfaces Failed
// rather than requeuing forever like a transient connect failure does.
func TestManagedRolesCredentialSweepConnectTerminal(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Spec: enterprisev4.PostgresClusterSpec{
			BootstrapFrom: &enterprisev4.BootstrapFrom{
				VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: "snap-1"},
			},
		},
	}
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy},
	}
	contracts := &reconcileContracts{
		CNPGCluster: cnpg,
		Secret:      &corev1.Secret{Data: map[string][]byte{secretKeyPassword: []byte("pw")}},
	}

	terminalSweeper := func(_ context.Context, _, _, _ string) (ports.RoleSweeper, error) {
		return nil, fmt.Errorf("%w: bad credentials", ports.ErrSweeperConnectTerminal)
	}

	events := &captureEventEmitter{}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg).Build()
	model := newManagedRolesModel(c, scheme, events, nil, cluster, contracts, terminalSweeper)

	// Act
	reconcileErr := model.Reconcile(context.Background())
	health, err := model.Observe(context.Background(), reconcileErr)

	// Assert: a terminal connect failure is terminal for the pass, not a requeue.
	require.ErrorIs(t, reconcileErr, errSweepTerminal)
	require.Error(t, err)
	assert.ErrorIs(t, err, errSweepTerminal)
	assert.Equal(t, pgcConstants.Failed, health.State)

	// Status must NOT record completion.
	assert.Nil(t, cluster.Status.Restore)

	// A warning event is emitted by Observe for the failed sweep.
	require.NotEmpty(t, events.warnings)
	assert.Contains(t, events.warnings[0], EventUnmanagedRolesSweepFailed)
}

type stubRoleSweeperExecFails struct{}

func (stubRoleSweeperExecFails) SweepUnmanagedRolesAfterRestore(_ context.Context) error {
	return assert.AnError
}

// TestManagedRolesCredentialSweepExecFails verifies that a failed sweep query surfaces as a
// Failed health with the wrapped error, and — like every other component — the warning is
// emitted from Observe, not Reconcile.
func TestManagedRolesCredentialSweepExecFails(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Spec: enterprisev4.PostgresClusterSpec{
			BootstrapFrom: &enterprisev4.BootstrapFrom{
				VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: "snap-1"},
			},
		},
	}
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy},
	}
	contracts := &reconcileContracts{
		CNPGCluster: cnpg,
		Secret:      &corev1.Secret{Data: map[string][]byte{secretKeyPassword: []byte("pw")}},
	}
	sweeper := func(_ context.Context, _, _, _ string) (ports.RoleSweeper, error) {
		return stubRoleSweeperExecFails{}, nil
	}

	events := &captureEventEmitter{}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg).Build()
	model := newManagedRolesModel(c, scheme, events, nil, cluster, contracts, sweeper)

	// Act
	reconcileErr := model.Reconcile(context.Background())
	// Boundary: Reconcile must not emit events — that is Observe's job.
	assert.Empty(t, events.warnings, "Reconcile must not emit warnings — boundary violation")
	health, err := model.Observe(context.Background(), reconcileErr)

	// Assert: a sweep exec failure is terminal for this pass.
	require.ErrorIs(t, reconcileErr, errSweepTerminal)
	require.Error(t, err)
	assert.ErrorIs(t, err, errSweepTerminal)
	assert.Equal(t, pgcConstants.Failed, health.State)
	assert.Equal(t, reasonManagedRolesFailed, health.Reason)

	// Status must NOT record completion.
	assert.Nil(t, cluster.Status.Restore)

	// A warning event is emitted by Observe for the failed sweep.
	require.NotEmpty(t, events.warnings)
	assert.Contains(t, events.warnings[0], EventUnmanagedRolesSweepFailed)
}

func roleDB(name, uid, cluster, role, secret string, exists bool) enterprisev4.PostgresDatabase {
	return enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(uid)},
		Spec:       enterprisev4.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: cluster}},
		Status: enterprisev4.PostgresDatabaseStatus{Databases: []enterprisev4.DatabaseInfo{{
			Name:  "app",
			Roles: []enterprisev4.DatabaseRoleInfo{{Name: role, SecretRef: &corev1.LocalObjectReference{Name: secret}, Exists: exists}},
		}}},
	}
}

func TestComputeDesiredRolesIncumbentWins(t *testing.T) {
	owner := enterprisev4.RoleOwnerReference{Name: "incumbent", UID: "uid-1"}
	decision := computeDesiredRoles([]enterprisev4.PostgresDatabase{
		roleDB("incumbent", "uid-1", "pg", "app_admin", "inc-secret", true),
		roleDB("newcomer", "uid-2", "pg", "app_admin", "new-secret", true),
	}, map[string]enterprisev4.RoleOwnerReference{"app_admin": owner}, nil, nil)

	assert.Equal(t, owner, decision.RoleOwners["app_admin"])
	assert.Len(t, decision.Roles, 1)
	assert.Equal(t, "inc-secret", decision.Roles[0].PasswordSecretRef.Name)
	assert.Len(t, decision.Conflicts, 1)
	assert.Equal(t, "newcomer", decision.Conflicts[0].AttemptedBy.Name)
}

func TestComputeDesiredRolesSimultaneousFirstClaimWithholdsBoth(t *testing.T) {
	decision := computeDesiredRoles([]enterprisev4.PostgresDatabase{
		roleDB("a", "uid-a", "pg", "shared_rw", "a-secret", true),
		roleDB("b", "uid-b", "pg", "shared_rw", "b-secret", true),
	}, nil, nil, nil)

	assert.Empty(t, decision.Roles)
	assert.Empty(t, decision.RoleOwners)
	assert.Len(t, decision.Conflicts, 2)
}

func TestComputeDesiredRolesExplicitFalseKeepsTombstoneUntilAbsentReconciled(t *testing.T) {
	owner := enterprisev4.RoleOwnerReference{Name: "db", UID: "uid"}
	decision := computeDesiredRoles([]enterprisev4.PostgresDatabase{
		roleDB("db", "uid", "pg", "old_rw", "old-secret", false),
	}, map[string]enterprisev4.RoleOwnerReference{"old_rw": owner}, nil, nil)

	assert.Equal(t, owner, decision.RoleOwners["old_rw"])
	assert.Len(t, decision.Roles, 1)
	assert.Equal(t, "old_rw", decision.Roles[0].Name)
	assert.False(t, decision.Roles[0].Exists)
}

func TestComputeDesiredRolesExplicitFalseRemovesTombstoneAfterAbsentReconciled(t *testing.T) {
	owner := enterprisev4.RoleOwnerReference{Name: "db", UID: "uid"}
	decision := computeDesiredRoles([]enterprisev4.PostgresDatabase{
		roleDB("db", "uid", "pg", "old_rw", "old-secret", false),
	}, map[string]enterprisev4.RoleOwnerReference{"old_rw": owner}, nil, map[string]struct{}{"old_rw": {}})

	assert.Empty(t, decision.RoleOwners)
	assert.Empty(t, decision.Roles)
}

func TestComputeDesiredRolesExplicitFalseDropsOwnedRoleWhenConflictingClaimantAlsoDrops(t *testing.T) {
	owner := enterprisev4.RoleOwnerReference{Name: "incumbent", UID: "uid-1"}
	decision := computeDesiredRoles([]enterprisev4.PostgresDatabase{
		roleDB("incumbent", "uid-1", "pg", "shared_rw", "inc-secret", false),
		roleDB("newcomer", "uid-2", "pg", "shared_rw", "new-secret", false),
	}, map[string]enterprisev4.RoleOwnerReference{"shared_rw": owner}, nil, nil)

	assert.Equal(t, owner, decision.RoleOwners["shared_rw"])
	assert.Len(t, decision.Roles, 1)
	assert.Equal(t, "shared_rw", decision.Roles[0].Name)
	assert.False(t, decision.Roles[0].Exists)
}

func TestComputeDesiredRolesNeverDropsOnMereStatusAbsence(t *testing.T) {
	owner := enterprisev4.RoleOwnerReference{Name: "db", UID: "uid"}
	decision := computeDesiredRoles([]enterprisev4.PostgresDatabase{{
		ObjectMeta: metav1.ObjectMeta{Name: "db", UID: types.UID("uid")},
	}}, map[string]enterprisev4.RoleOwnerReference{"app_rw": owner}, map[string]managedRole{
		"app_rw": {Name: "app_rw", Exists: true, PasswordSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "rw-secret"}, Key: "password"}},
	}, nil)

	assert.Equal(t, owner, decision.RoleOwners["app_rw"])
	assert.Len(t, decision.Roles, 1)
	assert.True(t, decision.Roles[0].Exists)
	assert.Equal(t, "rw-secret", decision.Roles[0].PasswordSecretRef.Name)
}

func TestComputeDesiredRolesOwnerGoneStopsManagingRole(t *testing.T) {
	owner := enterprisev4.RoleOwnerReference{Name: "gone", UID: "uid"}
	decision := computeDesiredRoles(nil, map[string]enterprisev4.RoleOwnerReference{"app_rw": owner}, map[string]managedRole{
		"app_rw": {Name: "app_rw", Exists: true},
	}, nil)

	assert.Empty(t, decision.RoleOwners)
	assert.Empty(t, decision.Roles)
	assert.Empty(t, decision.Conflicts)
}

func TestComputeDesiredRolesNonCollidingRoleProceedsForConflictedDatabase(t *testing.T) {
	incumbent := enterprisev4.RoleOwnerReference{Name: "incumbent", UID: "uid-1"}
	decision := computeDesiredRoles([]enterprisev4.PostgresDatabase{
		roleDB("incumbent", "uid-1", "pg", "shared_admin", "inc-secret", true),
		{
			ObjectMeta: metav1.ObjectMeta{Name: "newcomer", UID: types.UID("uid-2")},
			Status: enterprisev4.PostgresDatabaseStatus{
				Databases: []enterprisev4.DatabaseInfo{{
					Roles: []enterprisev4.DatabaseRoleInfo{
						{Name: "shared_admin", SecretRef: &corev1.LocalObjectReference{Name: "new-shared"}, Exists: true},
						{Name: "private_rw", SecretRef: &corev1.LocalObjectReference{Name: "private-secret"}, Exists: true},
					},
				}},
			},
		},
	}, map[string]enterprisev4.RoleOwnerReference{"shared_admin": incumbent}, nil, nil)

	assert.Equal(t, incumbent, decision.RoleOwners["shared_admin"])
	assert.Equal(t, enterprisev4.RoleOwnerReference{Name: "newcomer", UID: "uid-2"}, decision.RoleOwners["private_rw"])
	assert.Len(t, decision.Conflicts, 1)
	assert.ElementsMatch(t, []string{"shared_admin", "private_rw"}, []string{decision.Roles[0].Name, decision.Roles[1].Name})
}
