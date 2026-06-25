/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"testing"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func dbWithStatusRoles(name, cluster string, roleNames ...string) *enterprisev4.PostgresDatabase {
	roles := make([]enterprisev4.DatabaseRoleInfo, 0, len(roleNames))
	for _, r := range roleNames {
		roles = append(roles, enterprisev4.DatabaseRoleInfo{Name: r, Exists: true})
	}
	return &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"},
		Spec:       enterprisev4.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: cluster}},
		Status:     enterprisev4.PostgresDatabaseStatus{Databases: []enterprisev4.DatabaseInfo{{Name: "app", Roles: roles}}},
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
	assert.False(t, pred.Create(event.CreateEvent{Object: &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "ns"},
		Spec:       enterprisev4.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "pg"}},
	}}))

	assert.True(t, pred.Delete(event.DeleteEvent{Object: dbWithStatusRoles("db", "pg")}))

	old := dbWithStatusRoles("db", "pg", "app_admin")
	updated := dbWithStatusRoles("db", "pg", "app_admin", "app_rw")
	assert.True(t, pred.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: updated}))

	deleting := dbWithStatusRoles("db", "pg", "app_admin")
	now := metav1.Now()
	deleting.DeletionTimestamp = &now
	assert.True(t, pred.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: deleting}))

	assert.False(t, pred.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: dbWithStatusRoles("db", "pg", "app_admin")}))
	assert.False(t, pred.Update(event.UpdateEvent{ObjectOld: &corev1.Secret{}, ObjectNew: &corev1.Secret{}}))
}

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
