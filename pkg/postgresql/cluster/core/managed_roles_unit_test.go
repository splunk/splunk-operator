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

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSyncManagedRolesStatusFromCNPG(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		specRoles  []enterprisev4.ManagedRole
		cnpgStatus cnpgv1.ManagedRoles
		reconciled []string
		pending    []string
		failed     map[string]string
	}{
		{
			name: "marks unreconciled desired role as pending",
			specRoles: []enterprisev4.ManagedRole{
				{Name: "app_user", Exists: true},
			},
			cnpgStatus: cnpgv1.ManagedRoles{},
			reconciled: nil,
			pending:    []string{"app_user"},
			failed:     nil,
		},
		{
			name: "maps reconciled and pending roles from CNPG status",
			specRoles: []enterprisev4.ManagedRole{
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
			specRoles: []enterprisev4.ManagedRole{
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

			cluster := &enterprisev4.PostgresCluster{
				Spec: enterprisev4.PostgresClusterSpec{
					ManagedRoles: tt.specRoles,
				},
			}
			cnpgCluster := &cnpgv1.Cluster{
				Status: cnpgv1.ClusterStatus{
					ManagedRolesStatus: tt.cnpgStatus,
				},
			}

			syncManagedRolesStatusFromCNPG(cluster, cnpgCluster)

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
		specRoles      []enterprisev4.ManagedRole
		expectedState  pgcConstants.State
		expectedReason conditionReasons
		expectErr      bool
		expectPending  []string
		expectFailed   map[string]string
	}{
		{
			name: "returns pending when CNPG cluster is not yet healthy",
			cnpg: makeCNPG(cnpgv1.PhaseFirstPrimary, cnpgv1.ManagedRoles{}),
			specRoles: []enterprisev4.ManagedRole{
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
			specRoles:      []enterprisev4.ManagedRole{{Name: "app_user", Exists: true}},
			expectedState:  pgcConstants.Pending,
			expectedReason: reasonManagedRolesPending,
			expectPending:  []string{"app_user"},
		},
		{
			name: "returns failed when role cannot reconcile",
			cnpg: makeCNPG(cnpgv1.PhaseHealthy, cnpgv1.ManagedRoles{
				CannotReconcile: map[string][]string{"app_user": {"reserved role"}},
			}),
			specRoles:      []enterprisev4.ManagedRole{{Name: "app_user", Exists: true}},
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
			specRoles: []enterprisev4.ManagedRole{
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
			cluster := &enterprisev4.PostgresCluster{
				Spec: enterprisev4.PostgresClusterSpec{ManagedRoles: tt.specRoles},
			}
			contracts := &reconcileContracts{
				CNPGCluster: tt.cnpg,
				Secret:      &corev1.Secret{},
			}
			model := newManagedRolesModel(
				fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.cnpg).Build(), scheme, noopEventEmitter{}, nil, cluster, contracts,
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
	cluster := &enterprisev4.PostgresCluster{
		Spec: enterprisev4.PostgresClusterSpec{
			ManagedRoles: []enterprisev4.ManagedRole{{Name: "app_user", Exists: true}},
		},
	}
	contracts := &reconcileContracts{}
	model := newManagedRolesModel(
		fake.NewClientBuilder().Build(), nil, noopEventEmitter{}, nil, cluster, contracts,
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
	cluster := &enterprisev4.PostgresCluster{
		Spec: enterprisev4.PostgresClusterSpec{
			ManagedRoles: []enterprisev4.ManagedRole{{Name: "app_user", Exists: true}},
		},
	}
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
		fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg).Build(), scheme, events, nil, cluster, contracts,
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
	cluster := &enterprisev4.PostgresCluster{
		Spec: enterprisev4.PostgresClusterSpec{
			ManagedRoles: []enterprisev4.ManagedRole{{Name: "app_user", Exists: true}},
		},
	}
	events := &captureEventEmitter{}
	contracts := &reconcileContracts{
		CNPGCluster: &cnpgv1.Cluster{
			Status: cnpgv1.ClusterStatus{
				Phase: cnpgv1.PhaseHealthy,
				ManagedRolesStatus: cnpgv1.ManagedRoles{
					ByStatus: map[cnpgv1.RoleStatus][]string{
						cnpgv1.RoleStatusReconciled: {"app_user"},
					},
				},
			},
		},
		Secret: &corev1.Secret{},
	}
	model := newManagedRolesModel(fake.NewClientBuilder().Build(), nil, events, nil, cluster, contracts)

	// Act: first Observe — condition is False, event must fire.
	_, err := model.Observe(context.Background(), nil)

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

func TestManagedRolesModelNoOpWhenRolesEmpty(t *testing.T) {
	t.Parallel()

	// Arrange: cluster has no managed roles — reconcileManagedRoles should return nil immediately
	// without patching CNPG, and Observe should report Ready.
	scheme := newTestScheme()
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy},
	}
	cluster := &enterprisev4.PostgresCluster{
		Spec: enterprisev4.PostgresClusterSpec{ManagedRoles: nil},
	}
	contracts := &reconcileContracts{CNPGCluster: cnpg, Secret: &corev1.Secret{}}
	model := newManagedRolesModel(
		fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg).Build(),
		scheme, noopEventEmitter{}, nil, cluster, contracts,
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

	cluster := &enterprisev4.PostgresCluster{
		Spec: enterprisev4.PostgresClusterSpec{
			ManagedRoles: []enterprisev4.ManagedRole{
				{Name: "app_user", Exists: true},
			},
		},
	}
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
		fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg).Build(),
		nil,
		noopEventEmitter{},
		nil,
		cluster,
		contracts,
	)

	reconcileErr := model.Reconcile(context.Background())
	health, err := model.Observe(context.Background(), reconcileErr)
	require.NoError(t, err)
	assert.Equal(t, pgcConstants.Pending, health.State)
}
