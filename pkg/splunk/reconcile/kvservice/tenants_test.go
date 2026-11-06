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
	"strings"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/config"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
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
	require.NoError(t, enterpriseApi.AddToScheme(scheme))
	require.NoError(t, platformApi.AddToScheme(scheme))
	old := config.DefaultMutableFeatureGate.Enabled(config.PostgresController)
	require.NoError(t, config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): true}))
	t.Cleanup(func() {
		require.NoError(t, config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): old}))
	})
	cr := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "kvs2", Namespace: "shared", UID: "kvs-uid"},
		Spec: enterpriseApi.KVServiceSpec{Postgres: enterpriseApi.KVServicePostgresSpec{ClusterRef: corev1.LocalObjectReference{Name: "pg"}}}}
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
	for _, tenant := range []string{"alpha", "bravo"} {
		db := &platformApi.PostgresDatabase{}
		require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: "shared", Name: tenant + "-database"}, db))
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
	_, ready, message, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	require.False(t, ready)
	require.Contains(t, message, "alpha")
	require.Contains(t, message, "bravo")
	for _, tenant := range []string{"alpha", "bravo"} {
		db := &platformApi.PostgresDatabase{}
		require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: tenant + "-database"}, db))
		require.Empty(t, db.OwnerReferences)
		require.Equal(t, "Retain", db.Spec.Databases[0].DeletionPolicy)
		require.Equal(t, tenant, db.Spec.Databases[0].Name)
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
	_, ready, message, err := ensureTenantPostgresReady(ctx, guarded, cr)
	require.NoError(t, err)
	require.False(t, ready)
	require.Contains(t, message, "waiting for tenant databases")
	for _, tenant := range []string{"alpha", "bravo"} {
		db := &platformApi.PostgresDatabase{}
		require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: tenant + "-database"}, db))
		require.Equal(t, cr.Spec.Postgres.ClusterRef, db.Spec.ClusterRef)
	}
	readyTenantDatabases(t, ctx, c)
	dependencies, ready, message, err := ensureTenantPostgresReady(ctx, guarded, cr)
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
	_, _, _, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	readyTenantDatabases(t, ctx, c)
	dependencies, ready, _, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	require.True(t, ready)
	require.Len(t, dependencies.VolumeMounts, 4)
	var servicePasswordRef *corev1.SecretKeySelector
	for _, env := range dependencies.Environment {
		require.NotEqual(t, "KVSERVICE_SINGLE_TENANT_PASS4_SYMM_KEY", env.Name)
		if env.Name == "KVSERVICE_DB_SERVICE_PASSWORD" && env.ValueFrom != nil {
			servicePasswordRef = env.ValueFrom.SecretKeyRef
		}
	}
	require.NotNil(t, servicePasswordRef, "the reviewed service password setting must survive tenant resolution")
	require.Equal(t, "alpha-owner", servicePasswordRef.Name)
	require.Equal(t, "password", servicePasswordRef.Key)
	encoded, err := json.Marshal(dependencies)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "alpha-db-password")
	require.NotContains(t, string(encoded), "shared-key")
	deployment, err := k8sops.GetKVServiceDeploymentWithPostgresDependencies(ctx, c, cr, dependencies, nil)
	require.NoError(t, err)
	require.Len(t, deployment.Spec.Template.Spec.Volumes, len(dependencies.Volumes))
	require.Equal(t, dependencies.VolumeMounts, deployment.Spec.Template.Spec.Containers[0].VolumeMounts)
	runtimeConfig := &corev1.ConfigMap{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: "splunk-kvs2-kvservice-tenants"}, runtimeConfig))
	require.Equal(t, map[string]string{"alpha": "ns-a", "bravo": "ns-b"}, runtimeConfig.Data)
	require.Equal(t, runtimeConfig.Name, dependencies.Volumes[0].ConfigMap.Name)

	standalone := &enterpriseApi.Standalone{ObjectMeta: metav1.ObjectMeta{Name: "sh", Namespace: "ns-b"}}
	settings, found, err := k8sops.GetKVServiceSettings(ctx, c, standalone, splcommon.SplunkStandalone)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "bravo", settings.Tenant)
	require.Equal(t, "splunk-kvs2-kvservice-service.shared:8066", settings.ConnectionString)
	require.NoError(t, c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "disabled"}}))
	standalone.Namespace = "disabled"
	_, found, err = k8sops.GetKVServiceSettings(ctx, c, standalone, splcommon.SplunkStandalone)
	require.NoError(t, err)
	require.False(t, found)
}

func TestTenantKeyRotationKeepsExactlyCurrentAndPrevious(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	mapping, err := k8sops.DiscoverKVServiceTenants(ctx, c, cr)
	require.NoError(t, err)
	_, err = reconcileKVServiceTenantKeys(ctx, c, cr, mapping)
	require.NoError(t, err)
	source := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: "ns-a", Name: "splunk-ns-a-secret"}, source))
	source.Data["pass4SymmKey"] = []byte("alpha-new")
	require.NoError(t, c.Update(ctx, source))
	ring, err := reconcileKVServiceTenantKeys(ctx, c, cr, mapping)
	require.NoError(t, err)
	var keys []string
	require.NoError(t, json.Unmarshal(ring.Data["alpha"], &keys))
	require.Equal(t, []string{"alpha-new", "alpha-key"}, keys)
	source.Data["pass4SymmKey"] = []byte("alpha-third")
	require.NoError(t, c.Update(ctx, source))
	_, err = reconcileKVServiceTenantKeys(ctx, c, cr, mapping)
	require.ErrorContains(t, err, "unretired")
	ring.Data["alpha"] = []byte(`["alpha-new"]`)
	require.NoError(t, c.Update(ctx, ring))
	ring, err = reconcileKVServiceTenantKeys(ctx, c, cr, mapping)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(ring.Data["alpha"], &keys))
	require.Equal(t, []string{"alpha-third", "alpha-new"}, keys)
}

func TestTenantDatabaseRetainedOnRemovalAndRecreation(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	_, _, _, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	readyTenantDatabases(t, ctx, c)
	namespace := &corev1.Namespace{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "ns-b"}, namespace))
	delete(namespace.Labels, splcommon.LabelKVService)
	require.NoError(t, c.Update(ctx, namespace))
	_, ready, _, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	require.True(t, ready)
	db := &platformApi.PostgresDatabase{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: "shared", Name: "bravo-database"}, db))
	require.Empty(t, db.OwnerReferences)
	ringAfterRemoval := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: splcommon.KVServiceTenantVerificationKeysName}, ringAfterRemoval))
	require.NotContains(t, ringAfterRemoval.Data, "bravo")
	// Kubernetes garbage-collects this owned Secret, but not the retained database.
	ring := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: splcommon.KVServiceTenantVerificationKeysName, Namespace: "shared"}}
	require.NoError(t, c.Delete(ctx, ring))
	require.NoError(t, c.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "splunk-kvs2-kvservice-tenants", Namespace: cr.Namespace}}))
	cr.UID = "recreated-kvs-uid"
	_, ready, _, err = ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	require.True(t, ready)
}

func TestTenantLastNamespaceRemovalSuspendsServingButRetainsDatabases(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	_, _, _, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	readyTenantDatabases(t, ctx, c)
	_, ready, _, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	require.True(t, ready)
	cr.Spec.Replicas = 3
	deployment := resources.NewKVServiceDeployment(cr)
	deployment.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(cr, enterpriseApi.GroupVersion.WithKind("KVService"))}
	require.NoError(t, c.Create(ctx, deployment))
	for _, name := range []string{"ns-a", "ns-b"} {
		require.NoError(t, c.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}))
	}
	_, ready, message, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	require.False(t, ready)
	require.Contains(t, message, "enabled tenant namespaces")
	require.EqualValues(t, 3, cr.Spec.Replicas)
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(deployment), deployment))
	require.Zero(t, *deployment.Spec.Replicas)
	var databases platformApi.PostgresDatabaseList
	require.NoError(t, c.List(ctx, &databases))
	require.Len(t, databases.Items, 2)
	config := &corev1.ConfigMap{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: "splunk-kvs2-kvservice-tenants"}, config))
	require.Empty(t, config.Data)
	ring := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: splcommon.KVServiceTenantVerificationKeysName}, ring))
	require.Empty(t, ring.Data)
	for name, tenant := range map[string]string{"ns-a": "alpha", "ns-b": "bravo"} {
		require.NoError(t, c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{
			splcommon.LabelKVService: "enabled", splcommon.LabelTenant: tenant,
		}, Annotations: map[string]string{splcommon.AnnotationKVServiceAddr: "splunk-kvs2-kvservice-service.shared:8066"}}}))
	}
	dependencies, ready, _, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	require.True(t, ready)
	desired, err := k8sops.GetKVServiceDeploymentWithPostgresDependencies(ctx, c, cr, dependencies, nil)
	require.NoError(t, err)
	require.EqualValues(t, 3, *desired.Spec.Replicas)
}

func TestTenantRemovalRevokesKeysWhileOtherDependenciesAreUnavailable(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	_, _, _, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	require.NoError(t, c.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns-b"}}))
	require.NoError(t, c.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "splunk-ns-a-secret", Namespace: "ns-a"}}))
	cluster := &platformApi.PostgresCluster{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: "pg"}, cluster))
	cluster.Status.Phase = nil
	require.NoError(t, c.Status().Update(ctx, cluster))
	_, ready, _, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	require.False(t, ready)
	ring := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: splcommon.KVServiceTenantVerificationKeysName}, ring))
	require.NotContains(t, ring.Data, "bravo")
	require.Contains(t, ring.Data, "alpha")
}

func TestTenantDiscoveryValidationAndForeignDatabaseProtection(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	foreign := &platformApi.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "alpha-database", Namespace: "shared"}}
	require.NoError(t, c.Create(ctx, foreign))
	_, _, _, err := ensureTenantPostgresReady(ctx, c, cr)
	require.ErrorContains(t, err, "not managed")
}

func TestTenantMissingSecretStillCreatesAllDatabases(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	require.NoError(t, c.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "splunk-ns-a-secret", Namespace: "ns-a"}}))
	_, ready, message, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	require.False(t, ready)
	require.True(t, strings.Contains(message, "authentication"))
	var databases platformApi.PostgresDatabaseList
	require.NoError(t, c.List(ctx, &databases))
	require.Len(t, databases.Items, 2)
}

func TestTenantDiscoveryCreatesNewlyAddedTenantWithoutWaitingForExistingTenant(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	_, _, _, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	require.NoError(t, c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns-c", Labels: map[string]string{
		splcommon.LabelKVService: splcommon.LabelKVServiceEnabled, splcommon.LabelTenant: "charlie",
	}, Annotations: map[string]string{splcommon.AnnotationKVServiceAddr: "splunk-kvs2-kvservice-service.shared:8066"}}}))
	_, ready, _, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	require.False(t, ready)
	db := &platformApi.PostgresDatabase{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: "charlie-database"}, db))
	require.Equal(t, "charlie", db.Spec.Databases[0].Name)
	require.Equal(t, "Retain", db.Spec.Databases[0].DeletionPolicy)
}

func TestTenantSingletonConflictLeavesServingResourcesUnchanged(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	_, _, _, err := ensureTenantPostgresReady(ctx, c, cr)
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
		_, ready, _, err := ensureTenantPostgresReady(ctx, guarded, candidate)
		require.False(t, ready)
		message, terminal := splcommon.TerminalMessage(err)
		require.True(t, terminal)
		require.Contains(t, message, "multiple KVServices")
		require.Contains(t, message, "Keep only one")
	}
	// Exercise the request boundary too: persist a Stalled condition without
	// retrying or changing the running Deployment when admission was bypassed.
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	result, err := Apply(ctx, guarded, client.ObjectKeyFromObject(cr), events.NewFakeRecorder(16))
	require.ErrorContains(t, err, "multiple KVServices")
	require.Zero(t, result.RequeueAfter)
	current := &enterpriseApi.KVService{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(cr), current))
	require.Equal(t, enterpriseApi.PhaseError, current.Status.Phase)
	foundStalled := false
	for _, condition := range current.Status.Conditions {
		if condition.Type == string(enterpriseApi.ConditionStalled) && condition.Status == metav1.ConditionTrue {
			foundStalled = true
			require.Contains(t, condition.Message, "Keep only one")
		}
	}
	require.True(t, foundStalled)
	require.Zero(t, writes, "neither contender may mutate workloads, keys, membership or databases")
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(deployment), deployment))
	require.Equal(t, replicas, *deployment.Spec.Replicas)
}

func TestTenantSecretChangesAlterRevisionWithoutExposingCredentials(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	_, _, _, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	readyTenantDatabases(t, ctx, c)
	before, ready, _, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	require.True(t, ready)
	secret := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: "bravo-owner"}, secret))
	secret.Data["password"] = []byte("rotated-database-credential")
	require.NoError(t, c.Update(ctx, secret))
	after, ready, _, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	require.True(t, ready)
	require.NotEqual(t, before.AdminUserSecretRevision, after.AdminUserSecretRevision)
	data, err := json.Marshal(after)
	require.NoError(t, err)
	require.NotContains(t, string(data), "rotated-database-credential")
}

func TestTenantReplacementReusesRetainedDatabasesAfterPreviousManagerIsGone(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	_, _, _, err := ensureTenantPostgresReady(ctx, c, cr)
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
	_, ready, _, err := ensureTenantPostgresReady(ctx, c, replacement)
	require.NoError(t, err)
	require.True(t, ready)
	db := &platformApi.PostgresDatabase{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: "alpha-database"}, db))
	require.Empty(t, db.OwnerReferences)
	require.Equal(t, "shared/replacement", db.Annotations[splcommon.KVServiceTenantDatabaseOwnerAnnotation])
}

func TestTenantInvalidDiscoveryRevokesServingAndRecovers(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	_, _, _, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	readyTenantDatabases(t, ctx, c)
	_, ready, _, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	require.True(t, ready)
	cr.Spec.Replicas = 2
	cr.Status.ReadyReplicas = 2
	deployment := resources.NewKVServiceDeployment(cr)
	deployment.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(cr, enterpriseApi.GroupVersion.WithKind("KVService"))}
	require.NoError(t, c.Create(ctx, deployment))
	// Revoke bravo and make the remaining selected namespace invalid in the
	// same update window. Invalid metadata must not preserve either old key.
	require.NoError(t, c.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns-b"}}))
	ns := &corev1.Namespace{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "ns-a"}, ns))
	ns.Labels[splcommon.LabelTenant] = "invalid-tenant"
	require.NoError(t, c.Update(ctx, ns))
	_, ready, _, err = ensureTenantPostgresReady(ctx, c, cr)
	require.False(t, ready)
	_, terminal := splcommon.TerminalMessage(err)
	require.True(t, terminal)
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(deployment), deployment))
	require.Zero(t, *deployment.Spec.Replicas)
	require.Zero(t, cr.Status.ReadyReplicas)
	ring := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: splcommon.KVServiceTenantVerificationKeysName}, ring))
	require.Empty(t, ring.Data)
	membership := &corev1.ConfigMap{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: "splunk-kvs2-kvservice-tenants"}, membership))
	require.Empty(t, membership.Data)
	var databases platformApi.PostgresDatabaseList
	require.NoError(t, c.List(ctx, &databases))
	require.Len(t, databases.Items, 2)
	ns.Labels[splcommon.LabelTenant] = "alpha"
	require.NoError(t, c.Update(ctx, ns))
	dependencies, ready, _, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	require.True(t, ready)
	desired, err := k8sops.GetKVServiceDeploymentWithPostgresDependencies(ctx, c, cr, dependencies, nil)
	require.NoError(t, err)
	require.EqualValues(t, 2, *desired.Spec.Replicas)
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(ring), ring))
	require.Contains(t, ring.Data, "alpha")
	require.NotContains(t, ring.Data, "bravo")
}

func TestTenantTransientDiscoveryFailureKeepsLastGoodAuthorization(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	_, _, _, err := ensureTenantPostgresReady(ctx, c, cr)
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
		_, _, _, err = ensureTenantPostgresReady(ctx, guarded, cr)
		require.ErrorIs(t, err, readErr)
		_, terminal := splcommon.TerminalMessage(err)
		require.False(t, terminal)
	}
	ring := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cr.Namespace, Name: splcommon.KVServiceTenantVerificationKeysName}, ring))
	require.Len(t, ring.Data, 2)
}

func TestTenantKeyringRemainsProtectedAfterDuplicateOwnerDeleted(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	_, _, _, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	other := cr.DeepCopy()
	other.Name, other.UID, other.ResourceVersion = "aaa", "competitor-uid", ""
	require.NoError(t, c.Create(ctx, other))
	for _, candidate := range []*enterpriseApi.KVService{cr, other} {
		_, ready, _, err := ensureTenantPostgresReady(ctx, c, candidate)
		require.False(t, ready)
		require.ErrorContains(t, err, "multiple KVServices")
	}
	// Deleting the CR alone does not authorize adopting its remaining keyring.
	require.NoError(t, c.Delete(ctx, cr))
	_, ready, message, err := ensureTenantPostgresReady(ctx, c, other)
	require.NoError(t, err)
	require.False(t, ready)
	require.Contains(t, message, "keyring to be released")
	require.NoError(t, c.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: cr.Namespace, Name: splcommon.KVServiceTenantVerificationKeysName}}))
	_, ready, message, err = ensureTenantPostgresReady(ctx, c, other)
	require.NoError(t, err)
	require.False(t, ready)
	require.Contains(t, message, "enabled tenant namespaces")
}

func TestTenantSingletonConflictRecoversAfterDuplicateDeletion(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	other := cr.DeepCopy()
	other.Name, other.UID, other.ResourceVersion = "aaa", "competitor-uid", ""
	other.Finalizers = []string{"test/cleanup-pending"}
	require.NoError(t, c.Create(ctx, other))
	_, ready, _, err := ensureTenantPostgresReady(ctx, c, cr)
	require.False(t, ready)
	_, terminal := splcommon.TerminalMessage(err)
	require.True(t, terminal)
	var databases platformApi.PostgresDatabaseList
	require.NoError(t, c.List(ctx, &databases))
	require.Empty(t, databases.Items)
	require.NoError(t, c.Delete(ctx, other))
	_, _, _, err = ensureTenantPostgresReady(ctx, c, cr)
	require.ErrorContains(t, err, "multiple KVServices", "a deleting CR still occupies the namespace until cleanup finishes")
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(other), other))
	other.Finalizers = nil
	require.NoError(t, c.Update(ctx, other))
	_, ready, message, err := ensureTenantPostgresReady(ctx, c, cr)
	require.NoError(t, err)
	require.False(t, ready)
	require.Contains(t, message, "waiting for tenant databases")
	require.NoError(t, c.List(ctx, &databases))
	require.Len(t, databases.Items, 2)
}

func TestTenantForeignKeyringNeverAdopted(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	ring := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: cr.Namespace, Name: splcommon.KVServiceTenantVerificationKeysName}, Data: map[string][]byte{"protected": []byte("unchanged")}}
	require.NoError(t, c.Create(ctx, ring))
	_, _, _, err := ensureTenantPostgresReady(ctx, c, cr)
	message, terminal := splcommon.TerminalMessage(err)
	require.True(t, terminal)
	var policy *splcommon.TerminalError
	require.ErrorAs(t, err, &policy)
	require.NotNil(t, policy.Err)
	require.Contains(t, message, cr.Namespace+"/"+ring.Name)
	require.NotContains(t, message, string(cr.UID))
	require.ErrorContains(t, err, string(cr.UID))
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(ring), ring))
	require.Equal(t, []byte("unchanged"), ring.Data["protected"])
	require.Empty(t, ring.OwnerReferences)
}

func TestTenantKeyringCreationRaceRetriesBeforeDatabaseWrites(t *testing.T) {
	ctx, cr, c := tenantFixture(t)
	guarded := interceptor.NewClient(c, interceptor.Funcs{Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
		if _, ok := obj.(*corev1.Secret); ok {
			return apierrors.NewAlreadyExists(schema.GroupResource{Resource: "secrets"}, obj.GetName())
		}
		return c.Create(ctx, obj, opts...)
	}})
	_, ready, message, err := ensureTenantPostgresReady(ctx, guarded, cr)
	require.NoError(t, err)
	require.False(t, ready)
	require.Contains(t, message, "keyring reservation")
	require.Equal(t, enterpriseApi.PhasePending, cr.Status.Phase)
	var databases platformApi.PostgresDatabaseList
	require.NoError(t, c.List(ctx, &databases))
	require.Empty(t, databases.Items)
}

func TestTenantDiscoveryRevocationWriteErrorClassification(t *testing.T) {
	for _, terminalWrite := range []bool{false, true} {
		t.Run(fmt.Sprintf("terminalWrite=%t", terminalWrite), func(t *testing.T) {
			ctx, cr, c := tenantFixture(t)
			_, _, _, err := ensureTenantPostgresReady(ctx, c, cr)
			require.NoError(t, err)
			ns := &corev1.Namespace{}
			require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "ns-a"}, ns))
			ns.Labels[splcommon.LabelTenant] = "invalid-tenant"
			require.NoError(t, c.Update(ctx, ns))
			cause := errors.New("keyring update failure")
			var writeErr error = cause
			if terminalWrite {
				writeErr = tenantTerminal("Resolve the keyring ownership conflict.", cause)
			}
			guarded := interceptor.NewClient(c, interceptor.Funcs{Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*corev1.Secret); ok {
					return writeErr
				}
				return c.Update(ctx, obj, opts...)
			}})
			_, _, _, err = ensureTenantPostgresReady(ctx, guarded, cr)
			require.ErrorIs(t, err, cause)
			require.ErrorContains(t, err, "revoke tenant keys")
			_, terminal := splcommon.TerminalMessage(err)
			require.Equal(t, terminalWrite, terminal, "write errors must retain their classification")
			_, _, _, err = ensureTenantPostgresReady(ctx, c, cr)
			_, terminal = splcommon.TerminalMessage(err)
			require.True(t, terminal, "invalid tenant configuration remains terminal after the write succeeds")
		})
	}
}

func TestTenantMembershipAndKeyringChangesRollDeployment(t *testing.T) {
	for _, change := range []string{"membership", "tenant removal", "key rotation", "keyring metadata", "namespace metadata", "unchanged"} {
		t.Run(change, func(t *testing.T) {
			ctx, cr, c := tenantFixture(t)
			cr.Spec.Replicas = 1
			cr.Spec.Image = "kvstore/kvservice:test"
			cr.Spec.ImagePullPolicy = "IfNotPresent"
			_, _, _, err := ensureTenantPostgresReady(ctx, c, cr)
			require.NoError(t, err)
			readyTenantDatabases(t, ctx, c)

			build := func() *appsv1.Deployment {
				t.Helper()
				dependencies, ready, message, err := ensureTenantPostgresReady(ctx, c, cr)
				require.NoError(t, err)
				require.True(t, ready, message)
				desired, err := k8sops.GetKVServiceDeploymentWithPostgresDependencies(ctx, c, cr, dependencies, nil)
				require.NoError(t, err)
				require.Equal(t, dependencies.TenantKeyringRevision, desired.Spec.Template.Annotations["enterprise.splunk.com/kvservice-pass4-secret"])
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
				require.NotEqual(t, original.Spec.Template.Annotations[changedAnnotation], desired.Spec.Template.Annotations[changedAnnotation])
				if change != "tenant removal" {
					before, after := original.Spec.Template.DeepCopy(), desired.Spec.Template.DeepCopy()
					delete(before.Annotations, changedAnnotation)
					delete(after.Annotations, changedAnnotation)
					require.Equal(t, before, after, "the revision annotation alone must trigger this rollout")
				}
			} else {
				require.Equal(t, original.Spec.Template, desired.Spec.Template)
			}
			// ApplyDeployment normalizes slice order in its input, so preserve the built template.
			expectedTemplate := desired.Spec.Template.DeepCopy()
			phase, err := k8sops.ApplyDeployment(ctx, c, desired)
			require.NoError(t, err)
			require.NoError(t, c.Get(ctx, key, stored))
			if changedAnnotation != "" {
				require.Equal(t, enterpriseApi.PhaseUpdating, phase)
				require.NotEqual(t, version, stored.ResourceVersion)
			} else {
				require.Equal(t, version, stored.ResourceVersion, "unchanged dependencies must not update the Deployment")
			}

			version = stored.ResourceVersion
			repeated := build()
			require.Equal(t, *expectedTemplate, repeated.Spec.Template)
			_, err = k8sops.ApplyDeployment(ctx, c, repeated)
			require.NoError(t, err)
			require.NoError(t, c.Get(ctx, key, stored))
			require.Equal(t, version, stored.ResourceVersion, "reconciliation must converge without another rollout")
		})
	}
}
