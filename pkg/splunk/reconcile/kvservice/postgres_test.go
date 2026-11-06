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
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Reuse tenant discovery and credentials, with workload status enabled for Apply.
// Cluster and class objects are deliberately omitted: only tenant DBs gate serving.
func pendingPostgresFixture(t *testing.T) (*enterpriseApi.KVService, client.WithWatch) {
	t.Helper()
	ctx, cr, source := tenantFixture(t)
	cr.Generation = 1
	cr.Spec.Replicas = 1
	cr.Spec.Image = "kvstore/kvservice:test"
	cr.Spec.ImagePullPolicy = string(corev1.PullIfNotPresent)
	builder := fake.NewClientBuilder().WithScheme(source.Scheme()).
		WithStatusSubresource(&enterpriseApi.KVService{}, &appsv1.Deployment{}, &platformApi.PostgresDatabase{}).
		WithObjects(cr)
	for _, list := range []client.ObjectList{&corev1.NamespaceList{}, &corev1.SecretList{}} {
		require.NoError(t, source.List(ctx, list))
		builder.WithLists(list)
	}
	return cr, builder.Build()
}

func postgresReadinessFixture(t *testing.T) (*enterpriseApi.KVService, *platformApi.PostgresDatabase, client.WithWatch) {
	t.Helper()
	cr, c := pendingPostgresFixture(t)
	_, ready, message, err := ensureTenantPostgresReady(t.Context(), c, cr)
	require.NoError(t, err)
	require.False(t, ready)
	require.Contains(t, message, "alpha, bravo")
	readyTenantDatabases(t, t.Context(), c)
	database := &platformApi.PostgresDatabase{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKey{Namespace: cr.Namespace, Name: "alpha-database"}, database))
	return cr, database, c
}

func TestEnsureKVServicePostgresReadiness(t *testing.T) {
	tests := []struct {
		name                   string
		mutate                 func(*platformApi.PostgresDatabase)
		wantMessage, wantError string
		ready                  bool
	}{
		{name: "ready", ready: true},
		{name: "database generation not observed", mutate: func(d *platformApi.PostgresDatabase) { d.Status.ObservedGeneration = nil }, wantMessage: "waiting for tenant databases"},
		{name: "database generation stale", mutate: func(d *platformApi.PostgresDatabase) { stale := d.Generation - 1; d.Status.ObservedGeneration = &stale }, wantMessage: "waiting for tenant databases"},
		{name: "database phase absent", mutate: func(d *platformApi.PostgresDatabase) { d.Status.Phase = nil }, wantMessage: "waiting for tenant databases"},
		{name: "database failed", mutate: func(d *platformApi.PostgresDatabase) { phase := "Failed"; d.Status.Phase = &phase }, wantMessage: "waiting for tenant databases"},
		{name: "database provisioning", mutate: func(d *platformApi.PostgresDatabase) { phase := "Pending"; d.Status.Phase = &phase }, wantMessage: "waiting for tenant databases"},
		{name: "logical database not ready", mutate: func(d *platformApi.PostgresDatabase) { d.Status.Databases[0].Ready = false }, wantMessage: "waiting for connection references for tenant"},
		{name: "logical database status absent", mutate: func(d *platformApi.PostgresDatabase) { d.Status.Databases = nil }, wantMessage: "waiting for connection references for tenant"},
		{name: "connection ConfigMap not published", mutate: func(d *platformApi.PostgresDatabase) { d.Status.Databases[0].ConfigMapRef = nil }, wantMessage: "waiting for connection references for tenant"},
		{name: "admin Secret not published", mutate: func(d *platformApi.PostgresDatabase) { d.Status.Databases[0].AdminUserSecretRef = nil }, wantMessage: "waiting for connection references for tenant"},
		{name: "database missing logical name", mutate: func(d *platformApi.PostgresDatabase) { d.Spec.Databases = nil }, wantError: "incompatible configuration"},
		{name: "database references another cluster", mutate: func(d *platformApi.PostgresDatabase) { d.Spec.ClusterRef.Name = "other-cluster" }, wantError: "incompatible configuration"},
	}
	for _, tenant := range []string{"alpha", "bravo"} {
		for _, tt := range tests {
			t.Run(tenant+"/"+tt.name, func(t *testing.T) {
				cr, _, c := postgresReadinessFixture(t)
				database := &platformApi.PostgresDatabase{}
				require.NoError(t, c.Get(t.Context(), client.ObjectKey{Namespace: cr.Namespace, Name: tenant + "-database"}, database))
				if tt.mutate != nil {
					tt.mutate(database)
				}
				status := database.Status
				require.NoError(t, c.Update(t.Context(), database))
				database.Status = status
				require.NoError(t, c.Status().Update(t.Context(), database))
				before := database.DeepCopy()
				dependencies, ready, message, err := ensureTenantPostgresReady(t.Context(), c, cr)
				if tt.wantError != "" {
					require.ErrorContains(t, err, tt.wantError)
					require.ErrorContains(t, err, "reconcile database "+database.Namespace+"/"+database.Name)
					assert.ErrorIs(t, err, reconcile.TerminalError(nil))
					conditionMessage, terminal := splcommon.TerminalMessage(err)
					assert.True(t, terminal)
					assert.Contains(t, conditionMessage, "Tenant database ")
					assert.NotContains(t, conditionMessage, "ensure tenant databases:")
				} else {
					require.NoError(t, err)
				}
				assert.Equal(t, tt.ready, ready)
				if ready {
					require.NotNil(t, dependencies)
					assert.Empty(t, message)
				} else {
					assert.Nil(t, dependencies)
				}
				if tt.wantMessage != "" {
					assert.Equal(t, enterpriseApi.PhasePending, cr.Status.Phase)
					assert.Contains(t, message, tt.wantMessage)
					assert.Contains(t, message, tenant)
				}
				stored := &platformApi.PostgresDatabase{}
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(database), stored))
				assert.Equal(t, before, stored, "readiness checks must preserve tenant database spec, status, and ownership")
			})
		}
	}
}

func TestEnsureKVServicePostgresDatabaseConsumption(t *testing.T) {
	for _, operation := range []string{"missing database", "existing database", "missing logical database", "foreign database"} {
		t.Run(operation, func(t *testing.T) {
			cr, database, c := postgresReadinessFixture(t)
			switch operation {
			case "missing database":
				require.NoError(t, c.Delete(t.Context(), database))
			case "missing logical database":
				database.Spec.Databases = nil
				require.NoError(t, c.Update(t.Context(), database))
			case "foreign database":
				database.Annotations = nil
				require.NoError(t, c.Update(t.Context(), database))
			}
			before := database.DeepCopy()
			dependencies, ready, message, err := ensureTenantPostgresReady(t.Context(), c, cr)
			stored := &platformApi.PostgresDatabase{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(database), stored))
			switch operation {
			case "missing database":
				require.NoError(t, err)
				assert.False(t, ready)
				assert.Nil(t, dependencies)
				assert.Equal(t, "waiting for tenant databases: alpha", message)
				assert.Equal(t, cr.Spec.Postgres.ClusterRef, stored.Spec.ClusterRef)
				require.Len(t, stored.Spec.Databases, 1)
				assert.Equal(t, "alpha", stored.Spec.Databases[0].Name)
				assert.Equal(t, "Retain", stored.Spec.Databases[0].DeletionPolicy)
				assert.ElementsMatch(t, []string{"pg_stat_statements", "pgcrypto"}, stored.Spec.Databases[0].Extensions)
				assert.Empty(t, stored.OwnerReferences, "retained tenant databases must survive KVService deletion")
				assert.Empty(t, stored.Status.Databases)
			case "existing database":
				require.NoError(t, err)
				assert.True(t, ready)
				assert.NotNil(t, dependencies)
				assert.Equal(t, before, stored, "ready tenant databases must not be rewritten")
			default:
				require.ErrorIs(t, err, reconcile.TerminalError(nil))
				assert.False(t, ready)
				assert.Nil(t, dependencies)
				assert.Equal(t, before, stored, "KVService must not overwrite incompatible or foreign databases")
			}
			bravo := &platformApi.PostgresDatabase{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKey{Namespace: cr.Namespace, Name: "bravo-database"}, bravo))
			assert.True(t, bravo.Status.Databases[0].Ready, "the other tenant must remain ready")
		})
	}
}

func TestEnsureKVServicePostgresGuardsAndClientErrors(t *testing.T) {
	t.Run("disabled feature gate", func(t *testing.T) {
		cr, c := pendingPostgresFixture(t)
		featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.PostgresController, false)
		_, ready, _, err := ensureTenantPostgresReady(t.Context(), c, cr)
		require.ErrorContains(t, err, "feature gate")
		assert.ErrorIs(t, err, reconcile.TerminalError(nil))
		assert.False(t, ready)
	})
	t.Run("get database", func(t *testing.T) {
		cr, _, c := postgresReadinessFixture(t)
		failure := errors.New("API unavailable")
		failing := interceptor.NewClient(c, interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*platformApi.PostgresDatabase); ok {
				return failure
			}
			return c.Get(ctx, key, obj, opts...)
		}})
		dependencies, ready, _, err := ensureTenantPostgresReady(t.Context(), failing, cr)
		require.ErrorIs(t, err, failure)
		require.ErrorContains(t, err, "ensure tenant databases: get tenant database "+cr.Namespace+"/alpha-database")
		assert.NotErrorIs(t, err, reconcile.TerminalError(nil))
		assert.Nil(t, dependencies)
		assert.False(t, ready)
	})
}

// Runs under the existing reconciliation test's legal-terms fixture.
func testKVServicePostgresReconciliationLifecycle(t *testing.T) {
	cr, database, c := postgresReadinessFixture(t)
	database.Status.Databases[0].Ready = false
	require.NoError(t, c.Status().Update(t.Context(), database))
	key := client.ObjectKeyFromObject(cr)
	recorder := events.NewFakeRecorder(20)
	result, err := Apply(t.Context(), c, key, recorder)
	require.NoError(t, err)
	assert.NotZero(t, result.RequeueAfter)
	deploymentKey := types.NamespacedName{Namespace: cr.Namespace, Name: splutil.GetSplunkDeploymentName(splcommon.SplunkKVService, cr.Name)}
	workload := &appsv1.Deployment{}
	assert.True(t, apierrors.IsNotFound(c.Get(t.Context(), deploymentKey, workload)), "workload must not be applied before PostgreSQL is ready")
	database.Status.Databases[0].Ready = true
	require.NoError(t, c.Status().Update(t.Context(), database))
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
	require.NoError(t, c.Delete(t.Context(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "bravo-config", Namespace: cr.Namespace}}))
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
	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.PostgresController, false)
	require.NoError(t, c.Update(t.Context(), cr))
	result, err = Apply(t.Context(), c, key, recorder)
	require.ErrorIs(t, err, reconcile.TerminalError(nil))
	assert.Zero(t, result.RequeueAfter)
	require.NoError(t, c.Get(t.Context(), key, cr))
	stalled := meta.FindStatusCondition(cr.Status.Conditions, string(enterpriseApi.ConditionStalled))
	require.NotNil(t, stalled)
	assert.Equal(t, metav1.ConditionTrue, stalled.Status)
	assert.Equal(t, splcommon.EventReasonValidateSpecFailed, stalled.Reason)
	assert.Contains(t, stalled.Message, "--feature-gates=PostgresController=true")
	assert.Contains(t, stalled.Message, cr.Namespace+"/"+cr.Name)
	assert.NotContains(t, stalled.Message, "PostgreSQL tenant provisioning is disabled")

	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.PostgresController, true)
	require.NoError(t, c.Update(t.Context(), cr))
	_, err = Apply(t.Context(), c, key, recorder)
	require.Error(t, err, "the missing ConfigMap remains retryable")
	assert.NotErrorIs(t, err, reconcile.TerminalError(nil))
	require.NoError(t, c.Get(t.Context(), key, cr))
	stalled = meta.FindStatusCondition(cr.Status.Conditions, string(enterpriseApi.ConditionStalled))
	require.NotNil(t, stalled)
	assert.Equal(t, metav1.ConditionFalse, stalled.Status)
}

// Runs under the existing reconciliation test's legal-terms fixture.
func testKVServicePostgresTLSReconciliation(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.CertManagement, true)
	cr, _, c := postgresReadinessFixture(t)
	cr.Spec.Certs = []enterpriseApi.CertSpec{{SecretRef: corev1.LocalObjectReference{Name: "serving-cert"}, Role: enterpriseApi.CertRoleServer}}
	require.NoError(t, c.Update(t.Context(), cr))
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "serving-cert", Namespace: cr.Namespace},
		Type:       corev1.SecretTypeTLS,
		Data:       map[string][]byte{"tls.crt": []byte("test-cert"), "tls.key": []byte("test-key")},
	}
	require.NoError(t, c.Create(t.Context(), secret))
	_, err := Apply(t.Context(), c, client.ObjectKeyFromObject(cr), events.NewFakeRecorder(20))
	require.NoError(t, err)
	deployment := &appsv1.Deployment{}
	key := types.NamespacedName{Namespace: cr.Namespace, Name: splutil.GetSplunkDeploymentName(splcommon.SplunkKVService, cr.Name)}
	require.NoError(t, c.Get(t.Context(), key, deployment))
	container := deployment.Spec.Template.Spec.Containers[0]
	assert.Equal(t, "/mnt/tls/splunk-server-tls-cert/tls.crt", envValue(container.Env, "TLS_CERTIFICATE"))
	assert.Equal(t, "/mnt/tls/splunk-server-tls-cert/tls.key", envValue(container.Env, "TLS_PRIVATE_KEY"))
	assert.Equal(t, "true", envValue(container.Env, "KVSERVICE_SINGLE_TENANT_K8S_MODE"))
	assert.Equal(t, corev1.URISchemeHTTPS, container.ReadinessProbe.HTTPGet.Scheme)
	assert.Equal(t, corev1.URISchemeHTTPS, container.LivenessProbe.HTTPGet.Scheme)
	assert.NotEmpty(t, deployment.Spec.Template.Annotations["enterprise.splunk.com/kvservice-postgres-config"])
	assert.NotEmpty(t, deployment.Spec.Template.Annotations["enterprise.splunk.com/kvservice-pass4-secret"])
	mounted := false
	for _, volume := range deployment.Spec.Template.Spec.Volumes {
		if volume.Secret != nil && volume.Secret.SecretName == secret.Name {
			mounted = true
		}
	}
	assert.True(t, mounted, "the Postgres-backed workload must mount the TLS Secret")
}
