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

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestPostgresDatabasePredicator(t *testing.T) {
	t.Parallel()
	pred := postgresDatabasePredicator()

	base := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "db1",
			Namespace:  "default",
			Generation: 1,
			Finalizers: []string{postgresDatabaseFinalizer},
		},
	}

	deletionTime := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	cases := []struct {
		name   string
		mutate func(c *enterprisev4.PostgresDatabase)
		want   bool
	}{
		{name: "no change", mutate: func(c *enterprisev4.PostgresDatabase) {}, want: false},
		{name: "generation change", mutate: func(c *enterprisev4.PostgresDatabase) { c.Generation = 2 }, want: true},
		{name: "deletion timestamp change", mutate: func(c *enterprisev4.PostgresDatabase) { c.DeletionTimestamp = &deletionTime }, want: true},
		{name: "finalizer change", mutate: func(c *enterprisev4.PostgresDatabase) { c.Finalizers = nil }, want: true},
		{name: "annotation change", mutate: func(c *enterprisev4.PostgresDatabase) { c.Annotations = map[string]string{"some-annotation": "new"} }, want: false},
		{name: "label change", mutate: func(c *enterprisev4.PostgresDatabase) { c.Labels = map[string]string{"app": "database"} }, want: false},
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

func TestPostgresDatabasePredicatorIgnoresUnchangedWrongType(t *testing.T) {
	t.Parallel()
	pred := postgresDatabasePredicator()

	got := pred.Update(event.UpdateEvent{
		ObjectOld: &corev1.ConfigMap{},
		ObjectNew: &corev1.ConfigMap{},
	})

	assert.False(t, got)
}

func TestDatabaseSecretPredicator(t *testing.T) {
	t.Parallel()
	pred := databaseSecretPredicator()

	assert.False(t, pred.Create(event.CreateEvent{Object: &corev1.Secret{}}))
	assert.False(t, pred.Update(event.UpdateEvent{
		ObjectOld: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "1"}},
		ObjectNew: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "2"}},
	}))
	assert.True(t, pred.Delete(event.DeleteEvent{Object: &corev1.Secret{}}))
}
