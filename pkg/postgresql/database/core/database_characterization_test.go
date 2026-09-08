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

// Characterization tests for status-write behavior captured before the
// step-based pipeline refactor.

import (
	"context"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// buildFreshDatabaseObjects returns enough objects for PostgresDatabaseService
// to converge a fresh database without test-side status bookkeeping.
func buildFreshDatabaseObjects(requestName types.NamespacedName, dbName string) []client.Object {
	postgresDB := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{
			Name:       requestName.Name,
			Namespace:  requestName.Namespace,
			UID:        types.UID("postgresdb-uid"),
			Generation: 1,
			Finalizers: []string{postgresDatabaseFinalizerName},
		},
		Spec: platformv1alpha1.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "primary-cluster"},
			Databases:  []platformv1alpha1.DatabaseDefinition{{Name: dbName}},
		},
	}

	roleOwners := map[string]platformv1alpha1.RoleOwnerReference{
		adminRoleName(dbName): {Name: postgresDB.Name, UID: string(postgresDB.UID)},
		rwRoleName(dbName):    {Name: postgresDB.Name, UID: string(postgresDB.UID)},
	}

	postgresCluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "primary-cluster", Namespace: requestName.Namespace},
		Status: platformv1alpha1.PostgresClusterStatus{
			Phase: strPtr(string(ClusterReady)),
			ProvisionerRef: &corev1.ObjectReference{
				APIVersion: cnpgv1.SchemeGroupVersion.String(), Kind: "Cluster",
				Name: "primary-cnpg", Namespace: requestName.Namespace,
			},
			Resources: &platformv1alpha1.PostgresClusterResources{
				SuperUserSecretRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "primary-superuser"},
					Key:                  secretKeyPassword,
				},
			},
			ManagedRolesStatus: &platformv1alpha1.ManagedRolesStatus{
				Reconciled: []string{adminRoleName(dbName), rwRoleName(dbName)},
				RoleOwners: roleOwners,
			},
		},
	}

	cnpgCluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "primary-cnpg", Namespace: requestName.Namespace},
		Status: cnpgv1.ClusterStatus{
			ManagedRolesStatus: cnpgv1.ManagedRoles{
				ByStatus: map[cnpgv1.RoleStatus][]string{cnpgv1.RoleStatusReconciled: {adminRoleName(dbName), rwRoleName(dbName)}},
			},
			WriteService: "primary-rw",
			ReadService:  "primary-ro",
		},
	}

	superSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "primary-superuser", Namespace: requestName.Namespace},
		Data:       map[string][]byte{secretKeyPassword: []byte(dbName)},
	}

	cnpgDatabase := &cnpgv1.Database{
		ObjectMeta: metav1.ObjectMeta{
			Name:       cnpgDatabaseName(postgresDB.Name, dbName),
			Namespace:  requestName.Namespace,
			UID:        types.UID("cnpg-database-uid"),
			Generation: 1,
		},
		Spec: cnpgv1.DatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "primary-cnpg"},
			Name:       dbName,
			Owner:      adminRoleName(dbName),
		},
		Status: cnpgv1.DatabaseStatus{Applied: boolPtr(true), ObservedGeneration: 1},
	}

	return []client.Object{postgresDB, postgresCluster, cnpgCluster, superSecret, cnpgDatabase}
}

func TestCharacterization_SteadyStateReadyReconcileWriteCount(t *testing.T) {
	scheme := testScheme(t)
	ctx := context.Background()
	requestName := types.NamespacedName{Name: "primary", Namespace: "dbs"}
	dbName := "payments"

	statusWrites := 0
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&platformv1alpha1.PostgresDatabase{}).
		WithObjects(buildFreshDatabaseObjects(requestName, dbName)...).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, cl client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				if subResourceName == "status" {
					if _, ok := obj.(*platformv1alpha1.PostgresDatabase); ok {
						statusWrites++
					}
				}
				return cl.Status().Update(ctx, obj, opts...)
			},
		}).
		Build()

	newDBRepo := func(_ context.Context, _, _, _ string) (ports.DBRepo, error) { return &stubDBRepo{}, nil }
	metrics := &captureMetricsRecorder{}
	rc := &ReconcileContext{
		Client:              c,
		Scheme:              scheme,
		Recorder:            record.NewFakeRecorder(20),
		Metrics:             metrics,
		DatabaseProvisioner: testDatabaseProvisioner(c, scheme),
	}

	// Settle first; this test pins only the steady-state pass below.
	settled := &platformv1alpha1.PostgresDatabase{}
	for i := 0; i < 8; i++ {
		require.NoError(t, c.Get(ctx, requestName, settled))
		if settled.Status.Phase != nil && *settled.Status.Phase == string(readyDBPhase) {
			break
		}
		_, err := PostgresDatabaseService(ctx, rc, settled, newDBRepo)
		require.NoError(t, err)
	}
	require.NoError(t, c.Get(ctx, requestName, settled))
	require.NotNil(t, settled.Status.Phase)
	require.Equal(t, string(readyDBPhase), *settled.Status.Phase, "must reach Ready before the steady-state pass under test")

	statusWrites = 0
	metrics.provisioningDurations = nil
	result, err := PostgresDatabaseService(ctx, rc, settled, newDBRepo)
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)

	// This captures today's Ready steady-state flicker: several intermediate
	// status writes touch Provisioning/condition state before Ready is restored.
	// A future pipeline may reduce this, but it should do so deliberately.
	require.Equal(t, 4, statusWrites, "steady-state Ready reconcile status write count changed")
	require.Empty(t, metrics.provisioningDurations,
		"a steady-state pass that was already Ready must not start a new readiness cycle, "+
			"so no provisioning-duration observation is recorded")
}

func TestCharacterization_CurrentTerminalFailureWritesNoStatus(t *testing.T) {
	scheme := testScheme(t)
	ctx := context.Background()
	requestName := types.NamespacedName{Name: "primary", Namespace: "dbs"}

	postgresDB := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{
			Name:       requestName.Name,
			Namespace:  requestName.Namespace,
			UID:        types.UID("postgresdb-uid"),
			Generation: 5,
			Finalizers: []string{postgresDatabaseFinalizerName},
		},
		Spec: platformv1alpha1.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "primary-cluster"},
			Databases:  []platformv1alpha1.DatabaseDefinition{{Name: "payments"}},
		},
		Status: platformv1alpha1.PostgresDatabaseStatus{
			Phase:                strPtr(string(failedDBPhase)),
			ObservedGeneration:   int64Ptr(5),
			ReconcileFailureType: reconcileFailurePrivileges,
			Conditions: []metav1.Condition{
				{
					Type:               string(privilegesReady),
					Status:             metav1.ConditionFalse,
					Reason:             string(reasonPrivilegesTerminalFailure),
					Message:            "Failed to grant RW role privileges. Manual intervention required.",
					ObservedGeneration: 5,
				},
			},
		},
	}

	statusWrites := 0
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&platformv1alpha1.PostgresDatabase{}).
		WithObjects(postgresDB).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, cl client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				if subResourceName == "status" {
					statusWrites++
				}
				return cl.Status().Update(ctx, obj, opts...)
			},
		}).
		Build()

	rc := &ReconcileContext{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10)}
	newDBRepo := func(_ context.Context, _, _, _ string) (ports.DBRepo, error) { return &stubDBRepo{}, nil }

	// The first pass may publish custom metrics; the repeated pass below is the
	// sticky failure path under test.
	before := &platformv1alpha1.PostgresDatabase{}
	require.NoError(t, c.Get(ctx, requestName, before))
	result, err := PostgresDatabaseService(ctx, rc, before, newDBRepo)
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)

	statusWrites = 0
	current := &platformv1alpha1.PostgresDatabase{}
	require.NoError(t, c.Get(ctx, requestName, current))
	result, err = PostgresDatabaseService(ctx, rc, current, newDBRepo)
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
	require.Zero(t, statusWrites, "the sticky current-terminal-failure short-circuit must not write status at all once custom-metrics publication has converged")
}
