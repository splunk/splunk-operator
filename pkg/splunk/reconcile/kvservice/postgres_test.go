// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kvservice

import (
	"context"
	"errors"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/config"
	pgdatabase "github.com/splunk/splunk-operator/pkg/postgresql/database/core"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func postgresReadinessFixture(t *testing.T, custom bool) (*enterpriseApi.KVService, *platformApi.PostgresCluster, *platformApi.PostgresDatabase, client.WithWatch) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, enterpriseApi.AddToScheme(scheme))
	require.NoError(t, platformApi.AddToScheme(scheme))
	cr := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "kv", Namespace: "test", UID: "kv-id", Generation: 1}, Spec: enterpriseApi.KVServiceSpec{Replicas: 1, Postgres: &enterpriseApi.KVServicePostgresSpec{}}}
	cr.Spec.Image = "kvstore/kvservice:test"
	cr.Spec.ImagePullPolicy = "IfNotPresent"
	resource, logical := "kvstore-database", "kvstore"
	if custom {
		resource, logical = "tenant-database", "tenantdb"
		cr.Spec.Postgres.DatabaseRef = &corev1.LocalObjectReference{Name: resource}
		cr.Spec.Postgres.DatabaseName = logical
	}
	generation := int64(1)
	phase := "Ready"
	cluster := &platformApi.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: cr.Namespace, Generation: generation}, Status: platformApi.PostgresClusterStatus{ObservedGeneration: &generation, Phase: &phase}}
	database := &platformApi.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: resource, Namespace: cr.Namespace, Generation: generation, UID: "database-id"},
		Spec:       platformApi.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: cluster.Name}, Databases: []platformApi.DatabaseDefinition{{Name: logical, DeletionPolicy: "Retain", Extensions: []string{"pg_stat_statements", "pgcrypto"}}}},
		Status:     platformApi.PostgresDatabaseStatus{ObservedGeneration: &generation, Phase: &phase, Databases: []platformApi.DatabaseInfo{{Name: logical, Ready: true, ConfigMapRef: &corev1.LocalObjectReference{Name: "pg-connection"}, AdminUserSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "pg-admin"}, Key: "password"}}}},
	}
	connection := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "pg-connection", Namespace: cr.Namespace}, Data: map[string]string{pgconninfo.KeyClusterRWEndpoint: "postgres-rw.test.svc", pgconninfo.KeyDefaultClusterPort: "5432", pgdatabase.ConfigMapKeyAdminUser: "kv-admin"}}
	admin := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg-admin", Namespace: cr.Namespace}, Data: map[string][]byte{"password": []byte("test-db-password")}}
	auth := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: splcommon.GetNamespaceScopedSecretName(cr.Namespace), Namespace: cr.Namespace}, Data: map[string][]byte{}}
	for _, key := range splcommon.GetSplunkSecretTokenTypes() {
		auth.Data[key] = []byte("test-" + key)
	}
	auth.Data["hec_token"] = []byte("ABCDEF01-2345-6789-ABCD-EF0123456789")
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&enterpriseApi.KVService{}, &appsv1.Deployment{}).WithObjects(cr, cluster, database, connection, admin, auth).Build()
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(cluster), cluster))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(database), database))
	return cr, cluster, database, c
}

func TestEnsureKVServicePostgresReadiness(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.PostgresController, true)
	tests := []struct {
		name                   string
		mutate                 func(*platformApi.PostgresCluster, *platformApi.PostgresDatabase)
		wantPhase              enterpriseApi.Phase
		wantMessage, wantError string
		ready                  bool
	}{
		{name: "ready", ready: true},
		{name: "cluster readiness is not inspected", mutate: func(c *platformApi.PostgresCluster, _ *platformApi.PostgresDatabase) {
			c.Status.ObservedGeneration = nil
			c.Status.Phase = nil
		}, ready: true},
		{name: "database generation not observed", mutate: func(_ *platformApi.PostgresCluster, d *platformApi.PostgresDatabase) {
			d.Status.ObservedGeneration = nil
		}, wantPhase: enterpriseApi.PhasePending, wantMessage: "observe generation"},
		{name: "database missing logical name", mutate: func(_ *platformApi.PostgresCluster, d *platformApi.PostgresDatabase) { d.Spec.Databases = nil }, wantError: "does not define logical database"},
		{name: "database failed", mutate: func(_ *platformApi.PostgresCluster, d *platformApi.PostgresDatabase) {
			phase := "Failed"
			d.Status.Phase = &phase
			d.Status.Databases[0].Ready = false
		}, wantPhase: enterpriseApi.PhaseError, wantMessage: "failed while provisioning"},
		{name: "logical database not ready", mutate: func(_ *platformApi.PostgresCluster, d *platformApi.PostgresDatabase) {
			d.Status.Databases[0].Ready = false
		}, wantPhase: enterpriseApi.PhasePending, wantMessage: "to become ready"},
		{name: "logical database status absent", mutate: func(_ *platformApi.PostgresCluster, d *platformApi.PostgresDatabase) { d.Status.Databases = nil }, wantPhase: enterpriseApi.PhasePending, wantMessage: "to become ready"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr, cluster, database, c := postgresReadinessFixture(t, false)
			if tt.mutate != nil {
				tt.mutate(cluster, database)
			}
			require.NoError(t, c.Update(t.Context(), cluster))
			require.NoError(t, c.Update(t.Context(), database))
			before := database.DeepCopy()
			dependencies, ready, message, err := ensurePostgresReady(t.Context(), c, cr)
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.ready, ready)
			if ready {
				assert.NotNil(t, dependencies)
				assert.Empty(t, message)
			} else {
				assert.Nil(t, dependencies)
			}
			if tt.wantPhase != "" {
				assert.Equal(t, tt.wantPhase, cr.Status.Phase)
			}
			if tt.wantMessage != "" {
				assert.Contains(t, message, tt.wantMessage)
			}
			stored := &platformApi.PostgresDatabase{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(database), stored))
			assert.Equal(t, before.Spec, stored.Spec, "readiness checks must not alter an adopted database")
		})
	}
}

func TestEnsureKVServicePostgresDatabaseConsumption(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.PostgresController, true)
	for _, custom := range []bool{false, true} {
		for _, operation := range []string{"missing database", "existing database", "missing logical database"} {
			name := operation + " default names"
			if custom {
				name = operation + " custom names"
			}
			t.Run(name, func(t *testing.T) {
				cr, _, database, c := postgresReadinessFixture(t, custom)
				if operation == "missing database" {
					require.NoError(t, c.Delete(t.Context(), database))
				}
				if operation == "missing logical database" {
					database.Spec.Databases = nil
					require.NoError(t, c.Update(t.Context(), database))
				}
				if operation != "missing database" {
					require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(database), database))
				}
				before := database.DeepCopy()
				dependencies, ready, message, err := ensurePostgresReady(t.Context(), c, cr)
				stored := &platformApi.PostgresDatabase{}
				readErr := c.Get(t.Context(), client.ObjectKeyFromObject(database), stored)
				switch operation {
				case "missing database":
					require.NoError(t, err)
					assert.False(t, ready)
					assert.Nil(t, dependencies)
					assert.Contains(t, message, "waiting for PostgresDatabase")
					assert.True(t, apierrors.IsNotFound(readErr), "KVService must not create the database")
				case "existing database":
					require.NoError(t, err)
					require.NoError(t, readErr)
					assert.True(t, ready)
					assert.NotNil(t, dependencies)
					assert.Equal(t, before, stored, "KVService must not change database spec, metadata, or ownership")
				case "missing logical database":
					require.ErrorIs(t, err, reconcile.TerminalError(nil))
					require.NoError(t, readErr)
					assert.False(t, ready)
					assert.Equal(t, before, stored, "KVService must not add a logical database")
				}
			})
		}
	}
}

func TestEnsureKVServicePostgresGuardsAndClientErrors(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.PostgresController, true)
	t.Run("disabled feature gate", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.PostgresController, false)
		cr, _, _, c := postgresReadinessFixture(t, false)
		_, ready, _, err := ensurePostgresReady(t.Context(), c, cr)
		require.ErrorContains(t, err, "feature gate")
		assert.ErrorIs(t, err, reconcile.TerminalError(nil))
		assert.False(t, ready)
	})
	t.Run("empty database reference", func(t *testing.T) {
		cr, _, _, c := postgresReadinessFixture(t, false)
		cr.Spec.Postgres.DatabaseRef = &corev1.LocalObjectReference{}
		_, ready, _, err := ensurePostgresReady(t.Context(), c, cr)
		require.ErrorContains(t, err, "databaseRef.name")
		assert.ErrorIs(t, err, reconcile.TerminalError(nil))
		assert.False(t, ready)
	})
	t.Run("get database", func(t *testing.T) {
		cr, _, _, c := postgresReadinessFixture(t, false)
		failure := errors.New("API unavailable")
		failing := interceptor.NewClient(c, interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*platformApi.PostgresDatabase); ok {
				return failure
			}
			return c.Get(ctx, key, obj, opts...)
		}})
		dependencies, ready, _, err := ensurePostgresReady(t.Context(), failing, cr)
		require.ErrorIs(t, err, failure)
		assert.Nil(t, dependencies)
		assert.False(t, ready)
	})
}

// Runs under the existing reconciliation test's legal-terms fixture.
func testKVServicePostgresReconciliationLifecycle(t *testing.T) {
	cr, _, database, c := postgresReadinessFixture(t, false)
	database.Status.Databases[0].Ready = false
	require.NoError(t, c.Update(t.Context(), database))
	key := client.ObjectKeyFromObject(cr)
	recorder := events.NewFakeRecorder(20)
	result, err := Apply(t.Context(), c, key, recorder)
	require.NoError(t, err)
	assert.NotZero(t, result.RequeueAfter)
	deploymentKey := types.NamespacedName{Namespace: cr.Namespace, Name: splutil.GetSplunkDeploymentName(splcommon.SplunkKVService, cr.Name)}
	workload := &appsv1.Deployment{}
	assert.True(t, apierrors.IsNotFound(c.Get(t.Context(), deploymentKey, workload)), "workload must not be applied before PostgreSQL is ready")
	database.Status.Databases[0].Ready = true
	require.NoError(t, c.Update(t.Context(), database))
	_, err = Apply(t.Context(), c, key, recorder)
	require.NoError(t, err)
	require.NoError(t, c.Get(t.Context(), deploymentKey, workload))
	assert.NotEmpty(t, workload.Spec.Template.Annotations["enterprise.splunk.com/kvservice-postgres-config"])
	workload.Status = appsv1.DeploymentStatus{Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1}
	require.NoError(t, c.Status().Update(t.Context(), workload))
	result, err = Apply(t.Context(), c, key, recorder)
	require.NoError(t, err)
	assert.Zero(t, result.RequeueAfter)
	require.NoError(t, c.Get(t.Context(), key, cr))
	condition := meta.FindStatusCondition(cr.Status.Conditions, string(enterpriseApi.ConditionReady))
	require.NotNil(t, condition)
	assert.Equal(t, metav1.ConditionTrue, condition.Status)
	assert.Equal(t, enterpriseApi.PhaseReady, cr.Status.Phase)
	require.NoError(t, c.Get(t.Context(), deploymentKey, workload))
	version := workload.ResourceVersion
	require.NoError(t, c.Delete(t.Context(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "pg-connection", Namespace: cr.Namespace}}))
	_, err = Apply(t.Context(), c, key, recorder)
	require.Error(t, err)
	require.NoError(t, c.Get(t.Context(), deploymentKey, workload))
	assert.Equal(t, version, workload.ResourceVersion, "invalid dependencies must not replace the existing workload")
	require.NoError(t, c.Get(t.Context(), key, cr))
	condition = meta.FindStatusCondition(cr.Status.Conditions, string(enterpriseApi.ConditionReady))
	require.NotNil(t, condition)
	assert.Equal(t, metav1.ConditionFalse, condition.Status)
	assert.Equal(t, enterpriseApi.PhaseError, cr.Status.Phase)

	// Invalid configuration must stop retries and publish an actionable condition.
	cr.Spec.Postgres.DatabaseRef = &corev1.LocalObjectReference{}
	require.NoError(t, c.Update(t.Context(), cr))
	result, err = Apply(t.Context(), c, key, recorder)
	require.ErrorIs(t, err, reconcile.TerminalError(nil))
	assert.Zero(t, result.RequeueAfter)
	require.NoError(t, c.Get(t.Context(), key, cr))
	stalled := meta.FindStatusCondition(cr.Status.Conditions, string(enterpriseApi.ConditionStalled))
	require.NotNil(t, stalled)
	assert.Equal(t, metav1.ConditionTrue, stalled.Status)
	assert.Equal(t, splcommon.EventReasonValidateSpecFailed, stalled.Reason)
	assert.Equal(t, "spec.postgres.databaseRef.name must not be empty when databaseRef is specified", stalled.Message)

	cr.Spec.Postgres.DatabaseRef = nil
	require.NoError(t, c.Update(t.Context(), cr))
	_, err = Apply(t.Context(), c, key, recorder)
	require.Error(t, err, "the missing ConfigMap remains retryable")
	assert.NotErrorIs(t, err, reconcile.TerminalError(nil))
	require.NoError(t, c.Get(t.Context(), key, cr))
	stalled = meta.FindStatusCondition(cr.Status.Conditions, string(enterpriseApi.ConditionStalled))
	require.NotNil(t, stalled)
	assert.Equal(t, metav1.ConditionFalse, stalled.Status)
}
