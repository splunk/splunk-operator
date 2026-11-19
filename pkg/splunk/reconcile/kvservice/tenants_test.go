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

package kvservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/config"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func tenantFixture(t *testing.T) (context.Context, *enterpriseApi.KVService, client.WithWatch) {
	t.Helper()
	t.Setenv("WATCH_NAMESPACE", "")
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, autoscalingv2.AddToScheme(scheme))
	require.NoError(t, enterpriseApi.AddToScheme(scheme))
	require.NoError(t, platformApi.AddToScheme(scheme))
	old := config.DefaultMutableFeatureGate.Enabled(config.PostgresController)
	require.NoError(t, config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): true}))
	t.Cleanup(func() {
		assert.NoError(t, config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): old}))
	})
	cr := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "kvs2", Namespace: "shared", UID: "kvs-uid"},
		Spec: enterpriseApi.KVServiceSpec{Scheme: "http", Postgres: enterpriseApi.KVServicePostgresSpec{ClusterRef: corev1.LocalObjectReference{Name: "pg"}}}}
	ready, generation := "Ready", int64(1)
	objects := []client.Object{cr,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns-a", Labels: map[string]string{
			splcommon.LabelKVService: splcommon.LabelKVServiceEnabled, splcommon.LabelTenant: "alpha",
		}, Annotations: map[string]string{splcommon.AnnotationKVServiceAddr: "splunk-kvs2-kvservice-service.shared:8066"}}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns-b", Labels: map[string]string{
			splcommon.LabelKVService: splcommon.LabelKVServiceEnabled, splcommon.LabelTenant: "bravo",
		}, Annotations: map[string]string{splcommon.AnnotationKVServiceAddr: "splunk-kvs2-kvservice-service.shared:8066"}}},
		&platformApi.PostgresClusterClass{ObjectMeta: metav1.ObjectMeta{Name: "dev"}},
		&platformApi.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "shared", Generation: 1}, Spec: platformApi.PostgresClusterSpec{Class: "dev"}, Status: platformApi.PostgresClusterStatus{Phase: &ready, ObservedGeneration: &generation}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "splunk-ns-a-secret", Namespace: "ns-a"}, Data: map[string][]byte{"pass4SymmKey": []byte("alpha-key")}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "splunk-ns-b-secret", Namespace: "ns-b"}, Data: map[string][]byte{"pass4SymmKey": []byte("bravo-key")}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&platformApi.PostgresDatabase{}, &platformApi.PostgresCluster{}, &enterpriseApi.KVService{}).WithObjects(objects...).Build()
	return ctx, cr, c
}

func readyTenantDatabases(t *testing.T, ctx context.Context, c client.Client) {
	t.Helper()
	for _, tenant := range []string{"alpha", "bravo", splcommon.KVServiceSystemDatabase} {
		db := &platformApi.PostgresDatabase{}
		name := tenant + "-database"
		if tenant == splcommon.KVServiceSystemDatabase {
			name = splcommon.KVServiceSystemDatabaseResource
		}
		require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: "shared", Name: name}, db))
		ready := "Ready"
		generation := db.Generation
		db.Status = platformApi.PostgresDatabaseStatus{Phase: &ready, ObservedGeneration: &generation, Databases: []platformApi.DatabaseInfo{{Name: tenant, Ready: true,
			ConfigMapRef: &corev1.LocalObjectReference{Name: tenant + "-config"}, AdminUserSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: tenant + "-owner"}, Key: "password"}}}}
		require.NoError(t, c.Status().Update(ctx, db))
		require.NoError(t, c.Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: tenant + "-config", Namespace: "shared"}, Data: map[string]string{
			"CLUSTER_RW_ENDPOINT": "pg-rw.shared", "DEFAULT_CLUSTER_PORT": "5432", "ADMIN_USER_NAME": tenant + "_admin",
		}}))
		require.NoError(t, c.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: tenant + "-owner", Namespace: "shared"}, Data: map[string][]byte{"password": []byte(tenant + "-db-password")}}))
	}
}

func TestTenantDiscoveryCreatesEveryDatabaseBeforeWaiting(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	postgres, err := ensureTenantPostgresReady(ctx, c, cr)
	ready, message := postgres.ready, postgres.message
	require.NoError(t, err)
	require.False(t, ready)
	require.Contains(t, message, "service database kvservicesystem")
	for _, tenant := range []string{"alpha", "bravo"} {
		db := &platformApi.PostgresDatabase{}
		require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: tenant + "-database"}, db))
		assert.Empty(t, db.OwnerReferences)
		if assert.NotEmpty(t, db.Spec.Databases) {
			assert.Equal(t, "Retain", db.Spec.Databases[0].DeletionPolicy)
			assert.Equal(t, tenant, db.Spec.Databases[0].Name)
		}
	}
}

func TestTenantDatabaseReadinessDoesNotDependOnClusterReads(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	require.NoError(t, c.Delete(ctx, &platformApi.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: cr.Namespace}}))
	require.NoError(t, c.Delete(ctx, &platformApi.PostgresClusterClass{ObjectMeta: metav1.ObjectMeta{Name: "dev"}}))
	guarded := interceptor.NewClient(c, interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
		switch obj.(type) {
		case *platformApi.PostgresCluster, *platformApi.PostgresClusterClass:
			t.Errorf("KVService must delegate cluster/class dependencies to the PostgreSQL controller")
			return fmt.Errorf("cluster reads are forbidden")
		default:
			return c.Get(ctx, key, obj, opts...)
		}
	}})
	postgres, err := ensureTenantPostgresReady(ctx, guarded, cr)
	ready, message := postgres.ready, postgres.message
	require.NoError(t, err)
	require.False(t, ready)
	require.Contains(t, message, "waiting for service database kvservicesystem")
	for _, tenant := range []string{"alpha", "bravo"} {
		db := &platformApi.PostgresDatabase{}
		require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: tenant + "-database"}, db))
		assert.Equal(t, cr.Spec.Postgres.ClusterRef, db.Spec.ClusterRef)
	}
	readyTenantDatabases(t, ctx, c)
	nextPostgres, err := ensureTenantPostgresReady(ctx, guarded, cr)
	dependencies, ready, message := nextPostgres.dependencies, nextPostgres.ready, nextPostgres.message
	require.NoError(t, err)
	require.True(t, ready)
	require.NotNil(t, dependencies)
	require.Empty(t, message)
}

func TestTenantDeploymentUsesScopedCredentialsAndNoFixedRequestKey(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	// Reused request keys must also allow full dependency preparation to succeed.
	changeTenantSource(t, ctx, c, "ns-a", "shared-key")
	changeTenantSource(t, ctx, c, "ns-b", "shared-key")
	_, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	readyTenantDatabases(t, ctx, c)
	postgres, err := ensureTenantPostgresReady(ctx, c, cr)
	dependencies, ready := postgres.dependencies, postgres.ready
	require.NoError(t, err)
	require.True(t, ready)
	assert.Len(t, dependencies.VolumeMounts, 4)
	var servicePasswordRef *corev1.SecretKeySelector
	for _, env := range dependencies.Environment {
		assert.NotEqual(t, "KVSERVICE_SINGLE_TENANT_PASS4_SYMM_KEY", env.Name)
		if env.Name == "KVSERVICE_DB_SERVICE_PASSWORD" && env.ValueFrom != nil {
			servicePasswordRef = env.ValueFrom.SecretKeyRef
		}
	}
	require.NotNil(t, servicePasswordRef, "the reviewed service password setting must survive tenant resolution")
	require.Equal(t, "kvservicesystem-owner", servicePasswordRef.Name)
	require.Equal(t, "password", servicePasswordRef.Key)
	encoded, err := json.Marshal(dependencies)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "alpha-db-password")
	assert.NotContains(t, string(encoded), "shared-key")
	deployment, err := k8sops.GetKVServiceDeploymentWithPostgresDependencies(ctx, c, cr, dependencies, nil)
	require.NoError(t, err)
	assert.Len(t, deployment.Spec.Template.Spec.Volumes, len(dependencies.Volumes))
	if assert.NotEmpty(t, deployment.Spec.Template.Spec.Containers) {
		assert.Equal(t, dependencies.VolumeMounts, deployment.Spec.Template.Spec.Containers[0].VolumeMounts)
	}
	runtimeConfig := &corev1.ConfigMap{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: "splunk-kvs2-kvservice-tenants"}, runtimeConfig))
	assert.Equal(t, map[string]string{"alpha": "ns-a", "bravo": "ns-b"}, runtimeConfig.Data)
	if assert.NotEmpty(t, dependencies.Volumes) && assert.NotNil(t, dependencies.Volumes[0].ConfigMap) {
		assert.Equal(t, runtimeConfig.Name, dependencies.Volumes[0].ConfigMap.Name)
	}

	standalone := &enterpriseApi.Standalone{ObjectMeta: metav1.ObjectMeta{Name: "sh", Namespace: "ns-b"}}
	settings, found, err := k8sops.GetKVServiceSettings(ctx, c, standalone, splcommon.SplunkStandalone)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "bravo", settings.Tenant)
	assert.Equal(t, "splunk-kvs2-kvservice-service.shared:8066", settings.ConnectionString)
	require.NoError(t, c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "disabled"}}))
	standalone.Namespace = "disabled"
	_, found, err = k8sops.GetKVServiceSettings(ctx, c, standalone, splcommon.SplunkStandalone)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestTenantKeyRotationKeepsExactlyCurrentAndPrevious(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	discovery, err := k8sops.DiscoverKVServiceTenants(ctx, c, cr)
	require.NoError(t, err)
	_, failures, err := reconcileKVServiceTenantKeys(ctx, c, cr, discovery.Members)
	require.NoError(t, err)
	require.Empty(t, failures)
	source := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: "ns-a", Name: "splunk-ns-a-secret"}, source))
	source.Data["pass4SymmKey"] = []byte("alpha-new")
	require.NoError(t, c.Update(ctx, source))
	ring, failures, err := reconcileKVServiceTenantKeys(ctx, c, cr, discovery.Members)
	require.NoError(t, err)
	require.Empty(t, failures)
	var keys []string
	require.NoError(t, json.Unmarshal(ring.Data["alpha"], &keys))
	assert.Equal(t, []string{"alpha-new", "alpha-key"}, keys)
	source.Data["pass4SymmKey"] = []byte("alpha-third")
	require.NoError(t, c.Update(ctx, source))
	_, failures, err = reconcileKVServiceTenantKeys(ctx, c, cr, discovery.Members)
	require.NoError(t, err)
	require.ErrorContains(t, failures["alpha"], "unretired")
	ring.Data["alpha"] = []byte(`["alpha-new"]`)
	require.NoError(t, c.Update(ctx, ring))
	ring, failures, err = reconcileKVServiceTenantKeys(ctx, c, cr, discovery.Members)
	require.NoError(t, err)
	require.Empty(t, failures)
	require.NoError(t, json.Unmarshal(ring.Data["alpha"], &keys))
	assert.Equal(t, []string{"alpha-third", "alpha-new"}, keys)
}

func TestTenantDatabaseRetainedOnRemovalAndRecreation(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	_, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	readyTenantDatabases(t, ctx, c)
	namespace := &corev1.Namespace{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "ns-b"}, namespace))
	delete(namespace.Labels, splcommon.LabelKVService)
	require.NoError(t, c.Update(ctx, namespace))
	postgres, err := ensureTenantPostgresReady(ctx, c, cr)
	ready := postgres.ready
	require.NoError(t, err)
	assert.True(t, ready)
	db := &platformApi.PostgresDatabase{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: "shared", Name: "bravo-database"}, db))
	assert.Empty(t, db.OwnerReferences)
	ringAfterRemoval := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: splcommon.KVServiceTenantVerificationKeysName}, ringAfterRemoval))
	assert.NotContains(t, ringAfterRemoval.Data, "bravo")
	// Kubernetes garbage-collects this owned Secret, but not the retained database.
	ring := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: splcommon.KVServiceTenantVerificationKeysName, Namespace: "shared"}}
	require.NoError(t, c.Delete(ctx, ring))
	require.NoError(t, c.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "splunk-kvs2-kvservice-tenants", Namespace: cr.Namespace}}))
	cr.UID = "recreated-kvs-uid"
	nextPostgres, err := ensureTenantPostgresReady(ctx, c, cr)
	ready = nextPostgres.ready
	require.NoError(t, err)
	assert.True(t, ready)
}

func TestTenantLastNamespaceRemovalKeepsServiceRunningAndRetainsDatabases(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	_, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	readyTenantDatabases(t, ctx, c)
	postgres, err := ensureTenantPostgresReady(ctx, c, cr)
	ready := postgres.ready
	require.NoError(t, err)
	require.True(t, ready)
	cr.Spec.Replicas = ptr.To(int32(3))
	deployment := resources.NewKVServiceDeployment(cr)
	deployment.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(cr, enterpriseApi.GroupVersion.WithKind("KVService"))}
	require.NoError(t, c.Create(ctx, deployment))
	for _, name := range []string{"ns-a", "ns-b"} {
		require.NoError(t, c.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}))
	}
	nextPostgres, err := ensureTenantPostgresReady(ctx, c, cr)
	ready, message := nextPostgres.ready, nextPostgres.message
	require.NoError(t, err)
	require.True(t, ready)
	require.Empty(t, message)
	assert.Equal(t, ptr.To(int32(3)), cr.Spec.Replicas)
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(deployment), deployment))
	require.EqualValues(t, 3, *deployment.Spec.Replicas)
	var databases platformApi.PostgresDatabaseList
	require.NoError(t, c.List(ctx, &databases))
	require.Len(t, databases.Items, 3)
	config := &corev1.ConfigMap{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: "splunk-kvs2-kvservice-tenants"}, config))
	assert.Empty(t, config.Data)
	ring := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: splcommon.KVServiceTenantVerificationKeysName}, ring))
	assert.Empty(t, ring.Data)
	for name, tenant := range map[string]string{"ns-a": "alpha", "ns-b": "bravo"} {
		require.NoError(t, c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{
			splcommon.LabelKVService: "enabled", splcommon.LabelTenant: tenant,
		}, Annotations: map[string]string{splcommon.AnnotationKVServiceAddr: "splunk-kvs2-kvservice-service.shared:8066"}}}))
	}
	finalPostgres, err := ensureTenantPostgresReady(ctx, c, cr)
	dependencies, ready := finalPostgres.dependencies, finalPostgres.ready
	require.NoError(t, err)
	require.True(t, ready)
	desired, err := k8sops.GetKVServiceDeploymentWithPostgresDependencies(ctx, c, cr, dependencies, nil)
	require.NoError(t, err)
	assert.EqualValues(t, 3, *desired.Spec.Replicas)
}

func TestTenantRemovalRevokesKeysWhileOtherDependenciesAreUnavailable(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	_, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	require.NoError(t, c.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns-b"}}))
	require.NoError(t, c.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "splunk-ns-a-secret", Namespace: "ns-a"}}))
	cluster := &platformApi.PostgresCluster{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: "pg"}, cluster))
	cluster.Status.Phase = nil
	require.NoError(t, c.Status().Update(ctx, cluster))
	postgres, err := ensureTenantPostgresReady(ctx, c, cr)
	ready := postgres.ready
	require.NoError(t, err)
	assert.False(t, ready)
	ring := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: splcommon.KVServiceTenantVerificationKeysName}, ring))
	assert.NotContains(t, ring.Data, "bravo")
	assert.Contains(t, ring.Data, "alpha")
}

func TestTenantDiscoveryValidationAndForeignDatabaseProtection(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	foreign := &platformApi.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "alpha-database", Namespace: "shared"}}
	require.NoError(t, c.Create(ctx, foreign))
	_, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	require.EqualValues(t, "DatabaseConflict", cr.Status.Tenants[0].Reason)
	require.Contains(t, cr.Status.Tenants[0].Message, "not managed")
	stored := &platformApi.PostgresDatabase{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(foreign), stored))
	require.Empty(t, stored.Spec.Databases)
}

func TestTenantMissingSecretStillCreatesAllDatabases(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	require.NoError(t, c.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "splunk-ns-a-secret", Namespace: "ns-a"}}))
	postgres, err := ensureTenantPostgresReady(ctx, c, cr)
	ready, message := postgres.ready, postgres.message
	require.NoError(t, err)
	require.False(t, ready)
	require.Contains(t, message, "service database kvservicesystem")
	require.EqualValues(t, "AuthenticationPending", cr.Status.Tenants[0].Reason)
	var databases platformApi.PostgresDatabaseList
	require.NoError(t, c.List(ctx, &databases))
	require.Len(t, databases.Items, 3)
}

func TestTenantDiscoveryCreatesNewlyAddedTenantWithoutWaitingForExistingTenant(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	_, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	require.NoError(t, c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns-c", Labels: map[string]string{
		splcommon.LabelKVService: splcommon.LabelKVServiceEnabled, splcommon.LabelTenant: "charlie",
	}, Annotations: map[string]string{splcommon.AnnotationKVServiceAddr: "splunk-kvs2-kvservice-service.shared:8066"}}}))
	postgres, err := ensureTenantPostgresReady(ctx, c, cr)
	ready := postgres.ready
	require.NoError(t, err)
	assert.False(t, ready)
	db := &platformApi.PostgresDatabase{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: "charlie-database"}, db))
	if assert.NotEmpty(t, db.Spec.Databases) {
		assert.Equal(t, "charlie", db.Spec.Databases[0].Name)
		assert.Equal(t, "Retain", db.Spec.Databases[0].DeletionPolicy)
	}
}

func TestTenantSingletonConflictLeavesServingResourcesUnchanged(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	_, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	replicas := int32(3)
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "splunk-kvs2-kvservice", Namespace: cr.Namespace}, Spec: appsv1.DeploymentSpec{Replicas: &replicas}}
	require.NoError(t, c.Create(ctx, deployment))
	other := cr.DeepCopy()
	other.Name, other.UID, other.ResourceVersion = "earlier", "other", ""
	require.NoError(t, c.Create(ctx, other))
	writes := 0
	guarded := interceptor.NewClient(c, interceptor.Funcs{
		Create: func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
			writes++
			return fmt.Errorf("unexpected create")
		},
		Update: func(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error {
			writes++
			return fmt.Errorf("unexpected update")
		},
		Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
			writes++
			return fmt.Errorf("unexpected patch")
		},
		Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
			writes++
			return fmt.Errorf("unexpected delete")
		},
	})
	for _, candidate := range []*enterpriseApi.KVService{cr, other} {
		candidate.Status.Phase = enterpriseApi.PhaseReady
		postgres, err := ensureTenantPostgresReady(ctx, guarded, candidate)
		ready := postgres.ready
		require.False(t, ready)
		message, terminal := splcommon.TerminalMessage(err)
		assert.True(t, terminal)
		assert.Contains(t, message, "multiple KVServices")
		assert.Contains(t, message, "Keep only one")
	}
	// Exercise the request boundary too: persist a Stalled condition without
	// retrying or changing the running Deployment when admission was bypassed.
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	result, err := Apply(ctx, guarded, client.ObjectKeyFromObject(cr), events.NewFakeRecorder(16))
	assert.ErrorContains(t, err, "multiple KVServices")
	assert.Zero(t, result.RequeueAfter)
	current := &enterpriseApi.KVService{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(cr), current))
	assert.Equal(t, enterpriseApi.PhaseError, current.Status.Phase)
	foundStalled := false
	for _, condition := range current.Status.Conditions {
		if condition.Type == string(enterpriseApi.ConditionStalled) && condition.Status == metav1.ConditionTrue {
			foundStalled = true
			assert.Contains(t, condition.Message, "Keep only one")
		}
	}
	assert.True(t, foundStalled)
	assert.Zero(t, writes, "neither contender may mutate workloads, keys, membership or databases")
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(deployment), deployment))
	assert.Equal(t, replicas, *deployment.Spec.Replicas)
}

func TestTenantSecretChangesAlterRevisionWithoutExposingCredentials(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	_, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	readyTenantDatabases(t, ctx, c)
	beforePostgres, err := ensureTenantPostgresReady(ctx, c, cr)
	before, ready := beforePostgres.dependencies, beforePostgres.ready
	require.NoError(t, err)
	require.True(t, ready)
	secret := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: "bravo-owner"}, secret))
	secret.Data["password"] = []byte("rotated-database-credential")
	require.NoError(t, c.Update(ctx, secret))
	afterPostgres, err := ensureTenantPostgresReady(ctx, c, cr)
	after, ready := afterPostgres.dependencies, afterPostgres.ready
	require.NoError(t, err)
	require.True(t, ready)
	assert.NotEqual(t, before.AdminUserSecretRevision, after.AdminUserSecretRevision)
	data, err := json.Marshal(after)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "rotated-database-credential")
}

func TestTenantReplacementReusesRetainedDatabasesAfterPreviousManagerIsGone(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	_, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	readyTenantDatabases(t, ctx, c)
	require.NoError(t, c.Delete(ctx, cr))
	require.NoError(t, c.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: splcommon.KVServiceTenantVerificationKeysName, Namespace: cr.Namespace}}))
	replacement := cr.DeepCopy()
	replacement.Name = "replacement"
	replacement.UID = "replacement-uid"
	replacement.ResourceVersion = ""
	require.NoError(t, c.Create(ctx, replacement))
	for _, name := range []string{"ns-a", "ns-b"} {
		namespace := &corev1.Namespace{}
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: name}, namespace))
		namespace.Annotations[splcommon.AnnotationKVServiceAddr] = "splunk-replacement-kvservice-service.shared:8066"
		require.NoError(t, c.Update(ctx, namespace))
	}
	postgres, err := ensureTenantPostgresReady(ctx, c, replacement)
	ready := postgres.ready
	require.NoError(t, err)
	assert.True(t, ready)
	db := &platformApi.PostgresDatabase{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: "alpha-database"}, db))
	assert.Empty(t, db.OwnerReferences)
	assert.Equal(t, "shared/replacement", db.Annotations[splcommon.KVServiceTenantDatabaseOwnerAnnotation])
}

func TestTenantInvalidDiscoveryIsolatesOnlyAffectedTenantAndRecovers(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	_, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	readyTenantDatabases(t, ctx, c)
	namespace := &corev1.Namespace{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "ns-a"}, namespace))
	namespace.Labels[splcommon.LabelTenant] = "invalid-name"
	require.NoError(t, c.Update(ctx, namespace))
	postgres, err := ensureTenantPostgresReady(ctx, c, cr)
	dependencies, ready := postgres.dependencies, postgres.ready
	require.NoError(t, err)
	require.True(t, ready)
	require.Len(t, dependencies.VolumeMounts, 3)
	config := &corev1.ConfigMap{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: "splunk-kvs2-kvservice-tenants"}, config))
	require.Equal(t, map[string]string{"bravo": "ns-b"}, config.Data)
	require.EqualValues(t, "InvalidTenantName", cr.Status.Tenants[0].Reason)
	namespace.Labels[splcommon.LabelTenant] = "alpha"
	require.NoError(t, c.Update(ctx, namespace))
	nextPostgres, err := ensureTenantPostgresReady(ctx, c, cr)
	ready = nextPostgres.ready
	require.NoError(t, err)
	require.True(t, ready)
	require.True(t, cr.Status.Tenants[0].Ready)
	require.True(t, cr.Status.Tenants[1].Ready)
}

func TestTenantTransientDiscoveryFailureKeepsLastGoodAuthorization(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	_, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	readErr := errors.New("temporary API failure")
	failServices := false
	guarded := interceptor.NewClient(c, interceptor.Funcs{List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
		if _, ok := list.(*enterpriseApi.KVServiceList); ok && failServices {
			return readErr
		}
		if _, ok := list.(*corev1.NamespaceList); ok {
			return readErr
		}
		return c.List(ctx, list, opts...)
	}})
	for _, listServices := range []bool{false, true} {
		failServices = listServices
		_, err = ensureTenantPostgresReady(ctx, guarded, cr)
		require.ErrorIs(t, err, readErr)
		_, terminal := splcommon.TerminalMessage(err)
		assert.False(t, terminal)
	}
	ring := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: splcommon.KVServiceTenantVerificationKeysName}, ring))
	assert.Len(t, ring.Data, 2)
}

func TestTenantKeyringRemainsProtectedAfterDuplicateOwnerDeleted(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	_, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	other := cr.DeepCopy()
	other.Name, other.UID, other.ResourceVersion = "aaa", "competitor-uid", ""
	require.NoError(t, c.Create(ctx, other))
	for _, candidate := range []*enterpriseApi.KVService{cr, other} {
		postgres, err := ensureTenantPostgresReady(ctx, c, candidate)
		ready := postgres.ready
		require.False(t, ready)
		require.ErrorContains(t, err, "multiple KVServices")
	}
	// Deleting the CR alone does not authorize adopting its remaining keyring.
	require.NoError(t, c.Delete(ctx, cr))
	nextPostgres, err := ensureTenantPostgresReady(ctx, c, other)
	ready, message := nextPostgres.ready, nextPostgres.message
	require.NoError(t, err)
	assert.False(t, ready)
	assert.Contains(t, message, "keyring to be released")
	require.NoError(t, c.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: cr.Namespace, Name: splcommon.KVServiceTenantVerificationKeysName}}))
	finalPostgres, err := ensureTenantPostgresReady(ctx, c, other)
	ready, message = finalPostgres.ready, finalPostgres.message
	require.NoError(t, err)
	require.False(t, ready)
	require.Contains(t, message, "service database kvservicesystem")
}

func TestTenantSingletonConflictRecoversAfterDuplicateDeletion(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	other := cr.DeepCopy()
	other.Name, other.UID, other.ResourceVersion = "aaa", "competitor-uid", ""
	other.Finalizers = []string{"test/cleanup-pending"}
	require.NoError(t, c.Create(ctx, other))
	postgres, err := ensureTenantPostgresReady(ctx, c, cr)
	ready := postgres.ready
	require.False(t, ready)
	_, terminal := splcommon.TerminalMessage(err)
	assert.True(t, terminal)
	var databases platformApi.PostgresDatabaseList
	require.NoError(t, c.List(ctx, &databases))
	assert.Empty(t, databases.Items)
	require.NoError(t, c.Delete(ctx, other))
	_, err = ensureTenantPostgresReady(ctx, c, cr)
	require.ErrorContains(t, err, "multiple KVServices", "a deleting CR still occupies the namespace until cleanup finishes")
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(other), other))
	other.Finalizers = nil
	require.NoError(t, c.Update(ctx, other))
	nextPostgres, err := ensureTenantPostgresReady(ctx, c, cr)
	ready, message := nextPostgres.ready, nextPostgres.message
	require.NoError(t, err)
	require.False(t, ready)
	require.Contains(t, message, "waiting for service database kvservicesystem")
	require.NoError(t, c.List(ctx, &databases))
	require.Len(t, databases.Items, 3)
}

func TestTenantForeignKeyringNeverAdopted(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	ring := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: cr.Namespace, Name: splcommon.KVServiceTenantVerificationKeysName}, Data: map[string][]byte{"protected": []byte("unchanged")}}
	require.NoError(t, c.Create(ctx, ring))
	_, err := ensureTenantPostgresReady(ctx, c, cr)
	message, terminal := splcommon.TerminalMessage(err)
	assert.True(t, terminal)
	var policy *splcommon.TerminalError
	require.ErrorAs(t, err, &policy)
	assert.NotNil(t, policy.Err)
	assert.Contains(t, message, cr.Namespace+"/"+ring.Name)
	assert.NotContains(t, message, string(cr.UID))
	assert.ErrorContains(t, err, string(cr.UID))
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(ring), ring))
	assert.Equal(t, []byte("unchanged"), ring.Data["protected"])
	assert.Empty(t, ring.OwnerReferences)
}

func TestTenantKeyringCreationRaceRetriesBeforeDatabaseWrites(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	guarded := interceptor.NewClient(c, interceptor.Funcs{Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
		if _, ok := obj.(*corev1.Secret); ok {
			return apierrors.NewAlreadyExists(schema.GroupResource{Resource: "secrets"}, obj.GetName())
		}
		return c.Create(ctx, obj, opts...)
	}})
	postgres, err := ensureTenantPostgresReady(ctx, guarded, cr)
	ready, message := postgres.ready, postgres.message
	require.NoError(t, err)
	assert.False(t, ready)
	assert.Contains(t, message, "keyring reservation")
	assert.Equal(t, enterpriseApi.PhasePending, cr.Status.Phase)
	var databases platformApi.PostgresDatabaseList
	require.NoError(t, c.List(ctx, &databases))
	assert.Empty(t, databases.Items)
}

func TestTenantMembershipWriteErrorPreservesClassification(t *testing.T) {
	for _, terminalWrite := range []bool{false, true} {
		t.Run(fmt.Sprint(terminalWrite), func(t *testing.T) {
			ctx, cr, c := tenantFixture(t)
			cause := errors.New("membership update failed")
			var failure error = cause
			if terminalWrite {
				failure = tenantTerminal("correct shared membership ownership", cause)
			}
			failing := interceptor.NewClient(c, interceptor.Funcs{Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*corev1.ConfigMap); ok {
					return failure
				}
				return c.Create(ctx, obj, opts...)
			}})
			_, err := ensureTenantPostgresReady(ctx, failing, cr)
			require.ErrorIs(t, err, cause)
			_, terminal := splcommon.TerminalMessage(err)
			require.Equal(t, terminalWrite, terminal)
		})
	}
}

func TestTenantMembershipAndKeyringChangesRollDeployment(t *testing.T) {
	for _, change := range []string{"membership", "tenant removal", "key rotation", "keyring metadata", "namespace metadata", "unchanged"} {
		t.Run(change, func(t *testing.T) {
			ctx, cr, c := tenantFixture(t)
			cr.Spec.Replicas = ptr.To(int32(1))
			cr.Spec.Image = "kvstore/kvservice:test"
			cr.Spec.ImagePullPolicy = "IfNotPresent"
			_, err := ensureTenantPostgresReady(ctx, c, cr)
			require.NoError(t, err)
			readyTenantDatabases(t, ctx, c)

			build := func() *appsv1.Deployment {
				t.Helper()
				postgres, err := ensureTenantPostgresReady(ctx, c, cr)
				dependencies, ready, message := postgres.dependencies, postgres.ready, postgres.message
				require.NoError(t, err)
				require.True(t, ready, message)
				desired, err := k8sops.GetKVServiceDeploymentWithPostgresDependencies(ctx, c, cr, dependencies, nil)
				require.NoError(t, err)
				assert.Equal(t, dependencies.TenantKeyringRevision, desired.Spec.Template.Annotations["enterprise.splunk.com/kvservice-pass4-secret"])
				return desired
			}
			original := build()
			_, err = k8sops.ApplyDeployment(ctx, c, original)
			require.NoError(t, err)
			stored := &appsv1.Deployment{}
			key := client.ObjectKeyFromObject(original)
			require.NoError(t, c.Get(ctx, key, stored))
			version := stored.ResourceVersion

			changedAnnotation := ""
			switch change {
			case "membership":
				// Preserve the tenant, database and keys; only its namespace mapping changes.
				ns := &corev1.Namespace{}
				require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "ns-b"}, ns))
				require.NoError(t, c.Delete(ctx, ns))
				ns.Name, ns.UID, ns.ResourceVersion = "ns-c", "", ""
				require.NoError(t, c.Create(ctx, ns))
				require.NoError(t, c.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "splunk-ns-c-secret", Namespace: "ns-c"}, Data: map[string][]byte{"pass4SymmKey": []byte("bravo-key")}}))
				changedAnnotation = "enterprise.splunk.com/kvservice-postgres-config"
			case "tenant removal":
				require.NoError(t, c.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns-b"}}))
				changedAnnotation = "enterprise.splunk.com/kvservice-postgres-config"
			case "key rotation":
				changeTenantSource(t, ctx, c, "ns-a", "alpha-new")
				changedAnnotation = "enterprise.splunk.com/kvservice-pass4-secret"
			case "keyring metadata":
				ring := &corev1.Secret{}
				require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: splcommon.KVServiceTenantVerificationKeysName}, ring))
				ring.Annotations = map[string]string{"note": "updated"}
				require.NoError(t, c.Update(ctx, ring))
				changedAnnotation = "enterprise.splunk.com/kvservice-pass4-secret"
			case "namespace metadata":
				ns := &corev1.Namespace{}
				require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "ns-b"}, ns))
				ns.Annotations["note"] = "updated"
				require.NoError(t, c.Update(ctx, ns))
			}

			desired := build()
			if changedAnnotation != "" {
				assert.NotEqual(t, original.Spec.Template.Annotations[changedAnnotation], desired.Spec.Template.Annotations[changedAnnotation])
				if change != "tenant removal" {
					before, after := original.Spec.Template.DeepCopy(), desired.Spec.Template.DeepCopy()
					delete(before.Annotations, changedAnnotation)
					delete(after.Annotations, changedAnnotation)
					assert.Equal(t, before, after, "the revision annotation alone must trigger this rollout")
				}
			} else {
				assert.Equal(t, original.Spec.Template, desired.Spec.Template)
			}
			// ApplyDeployment normalizes slice order in its input, so preserve the built template.
			expectedTemplate := desired.Spec.Template.DeepCopy()
			phase, err := k8sops.ApplyDeployment(ctx, c, desired)
			require.NoError(t, err)
			require.NoError(t, c.Get(ctx, key, stored))
			if changedAnnotation != "" {
				assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
				assert.NotEqual(t, version, stored.ResourceVersion)
			} else {
				assert.Equal(t, version, stored.ResourceVersion, "unchanged dependencies must not update the Deployment")
			}

			version = stored.ResourceVersion
			repeated := build()
			assert.Equal(t, *expectedTemplate, repeated.Spec.Template)
			_, err = k8sops.ApplyDeployment(ctx, c, repeated)
			require.NoError(t, err)
			require.NoError(t, c.Get(ctx, key, stored))
			assert.Equal(t, version, stored.ResourceVersion, "reconciliation must converge without another rollout")
		})
	}
}
