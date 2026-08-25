/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	clustercore "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestObjectStorePredicator(t *testing.T) {
	t.Parallel()
	pred := objectStorePredicator()

	newObjectStore := func(generation int64) *unstructured.Unstructured {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(clustercore.ObjectStoreGVK)
		obj.SetName("c1-object-store")
		obj.SetNamespace("default")
		obj.SetGeneration(generation)
		return obj
	}

	t.Run("no spec change is ignored", func(t *testing.T) {
		t.Parallel()
		got := pred.Update(event.UpdateEvent{ObjectOld: newObjectStore(1), ObjectNew: newObjectStore(1)})
		assert.False(t, got)
	})

	t.Run("generation change fires", func(t *testing.T) {
		t.Parallel()
		got := pred.Update(event.UpdateEvent{ObjectOld: newObjectStore(1), ObjectNew: newObjectStore(2)})
		assert.True(t, got)
	})

	t.Run("delete fires", func(t *testing.T) {
		t.Parallel()
		got := pred.Delete(event.DeleteEvent{Object: newObjectStore(1)})
		assert.True(t, got)
	})
}

func dbWithStatusRoles(name, cluster string, roleNames ...string) *platformv1alpha1.PostgresDatabase {
	roles := make([]platformv1alpha1.DatabaseRoleInfo, 0, len(roleNames))
	for _, r := range roleNames {
		roles = append(roles, platformv1alpha1.DatabaseRoleInfo{Name: r, Exists: true})
	}
	return &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns", Generation: 1},
		Spec:       platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: cluster}},
		Status:     platformv1alpha1.PostgresDatabaseStatus{Databases: []platformv1alpha1.DatabaseInfo{{Name: "app", Roles: roles}}},
	}
}

func TestMapDatabaseToCluster(t *testing.T) {
	ctx := t.Context()
	reqs := mapDatabaseToCluster(ctx, dbWithStatusRoles("db", "pg"))
	if assert.Len(t, reqs, 1) {
		assert.Equal(t, "pg", reqs[0].Name)
		assert.Equal(t, "ns", reqs[0].Namespace)
	}

	assert.Empty(t, mapDatabaseToCluster(ctx, dbWithStatusRoles("db", "")))
	assert.Empty(t, mapDatabaseToCluster(ctx, &corev1.Secret{}))
}

func TestExtractPostgresDatabaseClusterRefName(t *testing.T) {
	assert.Equal(t, []string{"pg"}, extractPostgresDatabaseClusterRefName(dbWithStatusRoles("db", "pg")))
	assert.Nil(t, extractPostgresDatabaseClusterRefName(dbWithStatusRoles("db", "")))
	assert.Nil(t, extractPostgresDatabaseClusterRefName(&corev1.Secret{}))
}

func TestPostgresDatabaseForClusterPredicator(t *testing.T) {
	pred := postgresDatabaseForClusterPredicator()

	assert.True(t, pred.Create(event.CreateEvent{Object: dbWithStatusRoles("db", "pg", "app_admin")}))
	assert.False(t, pred.Create(event.CreateEvent{Object: &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "ns"},
		Spec:       platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "pg"}},
	}}))
	published := dbWithStatusRoles("db", "pg")
	published.Status.CustomMetricsPublication = &platformv1alpha1.PostgresDatabaseCustomMetricsPublication{
		ObservedGeneration: 1,
	}
	assert.True(t, pred.Create(event.CreateEvent{Object: published}))

	assert.True(t, pred.Delete(event.DeleteEvent{Object: dbWithStatusRoles("db", "pg")}))

	old := dbWithStatusRoles("db", "pg", "app_admin")
	updated := dbWithStatusRoles("db", "pg", "app_admin", "app_rw")
	assert.True(t, pred.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: updated}))

	oldSpec := dbWithStatusRoles("db", "pg", "app_admin")
	oldSpec.Generation = 1
	specUpdated := dbWithStatusRoles("db", "pg", "app_admin")
	specUpdated.Generation = 2
	assert.False(t, pred.Update(event.UpdateEvent{ObjectOld: oldSpec, ObjectNew: specUpdated}),
		"raw database spec changes must be interpreted by the database controller first")

	statusUpdated := dbWithStatusRoles("db", "pg", "app_admin")
	statusUpdated.Status.CustomMetricsPublication = &platformv1alpha1.PostgresDatabaseCustomMetricsPublication{
		ObservedGeneration: 1,
		Contributions: []platformv1alpha1.DatabaseCustomMetricsContribution{{
			DatabaseName: "app",
			Revision:     "revision",
			Exists:       true,
		}},
	}
	assert.True(t, pred.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: statusUpdated}))

	deleting := dbWithStatusRoles("db", "pg", "app_admin")
	now := metav1.Now()
	deleting.DeletionTimestamp = &now
	assert.True(t, pred.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: deleting}))

	assert.False(t, pred.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: dbWithStatusRoles("db", "pg", "app_admin")}))
	assert.False(t, pred.Update(event.UpdateEvent{ObjectOld: &corev1.Secret{}, ObjectNew: &corev1.Secret{}}))
}

func TestExtractDatabaseCustomQueryConfigMapNamesUsesCommittedStatus(t *testing.T) {
	db := dbWithStatusRoles("db", "pg")
	db.Spec.Databases = []platformv1alpha1.DatabaseDefinition{{
		Name: "app",
		Monitoring: &platformv1alpha1.DatabaseMonitoring{
			CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{selectorForTest("spec-only")},
		},
	}}
	assert.Nil(t, extractDatabaseCustomQueryConfigMapNames(db))

	db.Status.CustomMetricsPublication = &platformv1alpha1.PostgresDatabaseCustomMetricsPublication{
		ObservedGeneration: db.Generation,
		Contributions: []platformv1alpha1.DatabaseCustomMetricsContribution{{
			DatabaseName: "app",
			Revision:     "revision",
			Exists:       true,
			CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{
				selectorForTest("published"),
				selectorForTest("published"),
			},
		}},
	}
	assert.Equal(t, []string{"published"}, extractDatabaseCustomQueryConfigMapNames(db))

	db.Status.CustomMetricsPublication.Contributions[0].Exists = false
	assert.Nil(t, extractDatabaseCustomQueryConfigMapNames(db))

	db.Status.CustomMetricsPublication.Contributions[0].Exists = true
	db.Status.CustomMetricsPublication.ObservedGeneration = db.Generation - 1
	assert.Nil(t, extractDatabaseCustomQueryConfigMapNames(db))
}

func selectorForTest(name string) corev1.ConfigMapKeySelector {
	return corev1.ConfigMapKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: name},
		Key:                  "queries.yaml",
	}
}

func TestGeneratedMetricsConfigMapMapsToPostgresCluster(t *testing.T) {
	controller := true
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name:      "pg-metrics",
		Namespace: "ns",
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: cnpgv1.SchemeGroupVersion.String(),
			Kind:       "Cluster",
			Name:       "pg",
			Controller: &controller,
		}},
	}}

	reqs := (&PostgresClusterReconciler{}).enqueueClustersForCustomMetricsConfigMap(t.Context(), cm)
	if assert.Len(t, reqs, 1) {
		assert.Equal(t, "ns", reqs[0].Namespace)
		assert.Equal(t, "pg", reqs[0].Name)
	}

	pred := customMetricsConfigMapPredicate()
	updated := cm.DeepCopy()
	updated.Data = map[string]string{generatedCMKeyForTest: "changed"}
	assert.True(t, pred.Update(event.UpdateEvent{ObjectOld: cm, ObjectNew: updated}))

	ownerRemoved := cm.DeepCopy()
	ownerRemoved.OwnerReferences = nil
	assert.True(t, pred.Update(event.UpdateEvent{ObjectOld: cm, ObjectNew: ownerRemoved}))
	assert.False(t, pred.Update(event.UpdateEvent{ObjectOld: cm, ObjectNew: cm.DeepCopy()}))

	assert.True(t, pred.Delete(event.DeleteEvent{Object: cm}))

	hashDrift := cm.DeepCopy()
	hashDrift.Annotations = map[string]string{
		"platform.splunk.com/monitoring-config-hash": "drifted",
	}
	assert.True(t, pred.Update(event.UpdateEvent{ObjectOld: cm, ObjectNew: hashDrift}))
}

func TestForeignGeneratedMetricsConfigMapMapsToIntendedPostgresCluster(t *testing.T) {
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name:      "pg-metrics",
		Namespace: "ns",
	}}

	reqs := (&PostgresClusterReconciler{}).enqueueClustersForCustomMetricsConfigMap(t.Context(), cm)

	require.Len(t, reqs, 1)
	assert.Equal(t, "ns", reqs[0].Namespace)
	assert.Equal(t, "pg", reqs[0].Name)
	assert.True(t, customMetricsConfigMapPredicate().Delete(event.DeleteEvent{Object: cm}))
}

func TestGeneratedMetricsConfigMapOwnedByAnotherPostgresClusterMapsToIntendedCluster(t *testing.T) {
	controller := true
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name:      "intended-metrics",
		Namespace: "ns",
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: platformv1alpha1.GroupVersion.String(),
			Kind:       "PostgresCluster",
			Name:       "other",
			Controller: &controller,
		}},
	}}

	pred := customMetricsConfigMapPredicate()
	assert.True(t, pred.Create(event.CreateEvent{Object: cm}))
	assert.True(t, pred.Delete(event.DeleteEvent{Object: cm}))
	reqs := (&PostgresClusterReconciler{}).enqueueClustersForCustomMetricsConfigMap(t.Context(), cm)
	require.Len(t, reqs, 1)
	assert.Equal(t, "intended", reqs[0].Name)
}

func TestCustomMetricsConfigMapCreatePredicateSeparatesSourcesFromOwnedResources(t *testing.T) {
	controller := true
	pred := customMetricsConfigMapPredicate()
	source := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: "ns"}}
	owned := source.DeepCopy()
	owned.Name = "owned"
	owned.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: platformv1alpha1.GroupVersion.String(),
		Kind:       "PostgresCluster",
		Name:       "pg",
		Controller: &controller,
	}}

	assert.True(t, pred.Create(event.CreateEvent{Object: source}))
	assert.False(t, pred.Create(event.CreateEvent{Object: owned}))
}

func TestCustomMetricsConfigMapMapperFansOutBeyondControllerOwner(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	consumer := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "consumer", Namespace: "ns"},
		Spec: platformv1alpha1.PostgresClusterSpec{Monitoring: &platformv1alpha1.PostgresClusterMonitoring{
			CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{selectorForTest("shared-source")},
		}},
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(consumer).
		WithIndex(&platformv1alpha1.PostgresCluster{}, indexClusterCustomQueryConfigMaps, extractClusterCustomQueryConfigMapNames).
		Build()
	controller := true
	source := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name:      "shared-source",
		Namespace: "ns",
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: platformv1alpha1.GroupVersion.String(),
			Kind:       "PostgresCluster",
			Name:       "producer",
			Controller: &controller,
		}},
	}}

	reqs := (&PostgresClusterReconciler{Client: c}).enqueueClustersForCustomMetricsConfigMap(t.Context(), source)

	require.Len(t, reqs, 2)
	assert.ElementsMatch(t, []string{"producer", "consumer"}, []string{reqs[0].Name, reqs[1].Name})
}

func TestCustomMetricsConfigMapMapperFansOutCNPGOwnedDatabaseSource(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	database := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "databases", Namespace: "ns", Generation: 2},
		Spec:       platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "consumer"}},
		Status: platformv1alpha1.PostgresDatabaseStatus{
			CustomMetricsPublication: &platformv1alpha1.PostgresDatabaseCustomMetricsPublication{
				ObservedGeneration: 2,
				Contributions: []platformv1alpha1.DatabaseCustomMetricsContribution{{
					DatabaseName: "orders",
					Revision:     "revision",
					Exists:       true,
					CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{
						selectorForTest("provider-source"),
					},
				}},
			},
		},
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(database).
		WithIndex(&platformv1alpha1.PostgresDatabase{}, indexDatabaseCustomQueryConfigMaps, extractDatabaseCustomQueryConfigMapNames).
		Build()
	controller := true
	source := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name:      "provider-source",
		Namespace: "ns",
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: cnpgv1.SchemeGroupVersion.String(),
			Kind:       "Cluster",
			Name:       "provider-owner",
			Controller: &controller,
		}},
	}}

	reqs := (&PostgresClusterReconciler{Client: c}).enqueueClustersForCustomMetricsConfigMap(t.Context(), source)

	require.Len(t, reqs, 2)
	assert.ElementsMatch(t, []string{"provider-owner", "consumer"}, []string{reqs[0].Name, reqs[1].Name})
}

func TestCNPGOwnedConfigMapUsesOwnerNameWithoutGeneratedNameConvention(t *testing.T) {
	controller := true
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name:      "arbitrary-provider-config",
		Namespace: "ns",
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: cnpgv1.SchemeGroupVersion.String(),
			Kind:       "Cluster",
			Name:       "pg",
			Controller: &controller,
		}},
	}}

	reqs := (&PostgresClusterReconciler{}).enqueueClustersForCustomMetricsConfigMap(t.Context(), cm)

	require.Len(t, reqs, 1)
	assert.Equal(t, "pg", reqs[0].Name)
}

func TestOwnedConfigMapPredicateObservesSafetyDrift(t *testing.T) {
	pred := configMapPredicator()
	base := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name:        "pg-metrics-lkg",
		Annotations: map[string]string{"platform.splunk.com/monitoring-config-hash": "revision"},
	}}

	metadataDrift := base.DeepCopy()
	metadataDrift.Annotations["platform.splunk.com/monitoring-config-hash"] = "drifted"
	assert.True(t, pred.Update(event.UpdateEvent{ObjectOld: base, ObjectNew: metadataDrift}))

	binaryDrift := base.DeepCopy()
	binaryDrift.BinaryData = map[string][]byte{"queries.yaml": []byte("drifted")}
	assert.True(t, pred.Update(event.UpdateEvent{ObjectOld: base, ObjectNew: binaryDrift}))
}

type failIndexedListClient struct {
	client.Client
}

func (c *failIndexedListClient) List(
	ctx context.Context,
	list client.ObjectList,
	opts ...client.ListOption,
) error {
	options := &client.ListOptions{}
	options.ApplyOptions(opts)
	if options.FieldSelector != nil && !options.FieldSelector.Empty() {
		return errors.New("injected indexed list failure")
	}
	return c.Client.List(ctx, list, opts...)
}

func TestCustomMetricsConfigMapMapperFallsBackWhenIndexedListsFail(t *testing.T) {
	scheme := runtime.NewScheme()
	assert.NoError(t, platformv1alpha1.AddToScheme(scheme))
	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-source", Namespace: "ns"},
		Spec: platformv1alpha1.PostgresClusterSpec{
			Monitoring: &platformv1alpha1.PostgresClusterMonitoring{
				CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{selectorForTest("source")},
			},
		},
	}
	database := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "databases", Namespace: "ns", Generation: 3},
		Spec: platformv1alpha1.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "database-source"},
		},
		Status: platformv1alpha1.PostgresDatabaseStatus{
			CustomMetricsPublication: &platformv1alpha1.PostgresDatabaseCustomMetricsPublication{
				ObservedGeneration: 3,
				Contributions: []platformv1alpha1.DatabaseCustomMetricsContribution{{
					DatabaseName:           "orders",
					Revision:               "revision",
					Exists:                 true,
					CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{selectorForTest("source")},
				}},
			},
		},
	}
	duplicateTarget := database.DeepCopy()
	duplicateTarget.Name = "databases-same-target"
	duplicateTarget.Spec.ClusterRef.Name = cluster.Name
	unrelatedCluster := cluster.DeepCopy()
	unrelatedCluster.Name = "unrelated-cluster"
	unrelatedCluster.Spec.Monitoring.CustomQueriesConfigMap = []corev1.ConfigMapKeySelector{
		selectorForTest("other-source"),
	}
	unrelatedDatabase := database.DeepCopy()
	unrelatedDatabase.Name = "unrelated-database"
	unrelatedDatabase.Spec.ClusterRef.Name = "unrelated-database-source"
	unrelatedDatabase.Status.CustomMetricsPublication.Contributions[0].CustomQueriesConfigMap =
		[]corev1.ConfigMapKeySelector{selectorForTest("other-source")}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		cluster,
		database,
		duplicateTarget,
		unrelatedCluster,
		unrelatedDatabase,
	).Build()
	r := &PostgresClusterReconciler{Client: &failIndexedListClient{Client: base}}

	reqs := r.enqueueClustersForCustomMetricsConfigMap(t.Context(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: "ns"},
	})

	require.Len(t, reqs, 2)
	assert.ElementsMatch(t, []string{"cluster-source", "database-source"}, []string{
		reqs[0].Name,
		reqs[1].Name,
	})
}

const generatedCMKeyForTest = "queries.yaml"

func TestCNPGClusterPredicator(t *testing.T) {
	t.Parallel()
	pred := cnpgClusterPredicator()

	base := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status: cnpgv1.ClusterStatus{
			Phase:          cnpgv1.PhaseHealthy,
			Instances:      3,
			ReadyInstances: 3,
			CurrentPrimary: "pg1-1",
		},
	}

	cases := []struct {
		name   string
		mutate func(c *cnpgv1.Cluster)
		want   bool
	}{
		{name: "no change", mutate: func(c *cnpgv1.Cluster) {}, want: false},
		{name: "phase change", mutate: func(c *cnpgv1.Cluster) { c.Status.Phase = cnpgv1.PhaseSwitchover }, want: true},
		{name: "instances change", mutate: func(c *cnpgv1.Cluster) { c.Status.Instances = 4 }, want: true},
		{name: "ready instances change", mutate: func(c *cnpgv1.Cluster) { c.Status.ReadyInstances = 2 }, want: true},
		{name: "current primary change", mutate: func(c *cnpgv1.Cluster) { c.Status.CurrentPrimary = "pg1-2" }, want: true},
		{name: "resizing pvc added", mutate: func(c *cnpgv1.Cluster) { c.Status.ResizingPVC = []string{"pg1-1"} }, want: true},
		{name: "resizing pvc count reduced", mutate: func(c *cnpgv1.Cluster) { c.Status.ResizingPVC = []string{"pg1-1", "pg1-2"} }, want: true},
		{
			name: "metrics ConfigMap resource version change",
			mutate: func(c *cnpgv1.Cluster) {
				c.Status.ConfigMapResourceVersion.Metrics = map[string]string{"pg1-metrics": "2"}
			},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			oldObj := base.DeepCopy()
			newObj := base.DeepCopy()
			tc.mutate(newObj)
			got := pred.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj})
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCNPGClusterPredicatorRejectsWrongType(t *testing.T) {
	t.Parallel()
	pred := cnpgClusterPredicator()
	got := pred.Update(event.UpdateEvent{
		ObjectOld: &corev1.ConfigMap{},
		ObjectNew: &corev1.ConfigMap{},
	})
	assert.False(t, got)
}

func TestCNPGPoolerPredicator(t *testing.T) {
	t.Parallel()
	pred := cnpgPoolerPredicator()

	base := &cnpgv1.Pooler{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Status:     cnpgv1.PoolerStatus{Instances: 2},
	}

	t.Run("no change", func(t *testing.T) {
		t.Parallel()
		got := pred.Update(event.UpdateEvent{ObjectOld: base.DeepCopy(), ObjectNew: base.DeepCopy()})
		assert.False(t, got)
	})

	t.Run("instances change", func(t *testing.T) {
		t.Parallel()
		newObj := base.DeepCopy()
		newObj.Status.Instances = 3
		got := pred.Update(event.UpdateEvent{ObjectOld: base.DeepCopy(), ObjectNew: newObj})
		assert.True(t, got)
	})

	t.Run("rejects wrong type", func(t *testing.T) {
		t.Parallel()
		got := pred.Update(event.UpdateEvent{ObjectOld: &corev1.ConfigMap{}, ObjectNew: &corev1.ConfigMap{}})
		assert.False(t, got)
	})
}

func TestScheduledBackupPredicator(t *testing.T) {
	t.Parallel()
	pred := scheduledBackupPredicator()

	last := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	next := metav1.NewTime(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	base := &cnpgv1.ScheduledBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "sb1", Namespace: "default"},
		Status: cnpgv1.ScheduledBackupStatus{
			LastScheduleTime: &last,
			NextScheduleTime: &next,
		},
	}

	t.Run("no change", func(t *testing.T) {
		t.Parallel()
		got := pred.Update(event.UpdateEvent{ObjectOld: base.DeepCopy(), ObjectNew: base.DeepCopy()})
		assert.False(t, got)
	})

	t.Run("last schedule time change", func(t *testing.T) {
		t.Parallel()
		newObj := base.DeepCopy()
		bumped := metav1.NewTime(last.Add(time.Hour))
		newObj.Status.LastScheduleTime = &bumped
		got := pred.Update(event.UpdateEvent{ObjectOld: base.DeepCopy(), ObjectNew: newObj})
		assert.True(t, got)
	})

	t.Run("next schedule time change", func(t *testing.T) {
		t.Parallel()
		newObj := base.DeepCopy()
		bumped := metav1.NewTime(next.Add(time.Hour))
		newObj.Status.NextScheduleTime = &bumped
		got := pred.Update(event.UpdateEvent{ObjectOld: base.DeepCopy(), ObjectNew: newObj})
		assert.True(t, got)
	})

	t.Run("delete fires", func(t *testing.T) {
		t.Parallel()
		got := pred.Delete(event.DeleteEvent{Object: base.DeepCopy()})
		assert.True(t, got)
	})

	t.Run("rejects wrong type", func(t *testing.T) {
		t.Parallel()
		got := pred.Update(event.UpdateEvent{ObjectOld: &corev1.ConfigMap{}, ObjectNew: &corev1.ConfigMap{}})
		assert.False(t, got)
	})
}
