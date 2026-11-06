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
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestClassifyTenantDatabaseRetentionAndOwnership(t *testing.T) {
	cr := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "kvs", Namespace: "shared", UID: "current"}, Spec: enterpriseApi.KVServiceSpec{Postgres: enterpriseApi.KVServicePostgresSpec{ClusterRef: corev1.LocalObjectReference{Name: "pg"}}}}
	for _, tc := range []struct {
		name     string
		modify   func(*platformApi.PostgresDatabase)
		services []enterpriseApi.KVService
		adopt    bool
		err      string
	}{
		{name: "current manager unchanged"},
		{name: "retained previous manager gone", modify: func(db *platformApi.PostgresDatabase) {
			db.Annotations[splcommon.KVServiceTenantDatabaseOwnerAnnotation] = "shared/previous"
		}, adopt: true},
		{name: "previous manager still exists", modify: func(db *platformApi.PostgresDatabase) {
			db.Annotations[splcommon.KVServiceTenantDatabaseOwnerAnnotation] = "shared/previous"
		}, services: []enterpriseApi.KVService{{ObjectMeta: metav1.ObjectMeta{Name: "previous"}}}, err: "not managed"},
		{name: "foreign namespace annotation", modify: func(db *platformApi.PostgresDatabase) {
			db.Annotations[splcommon.KVServiceTenantDatabaseOwnerAnnotation] = "foreign/previous"
		}, err: "not managed"},
		{name: "unmanaged database", modify: func(db *platformApi.PostgresDatabase) { db.Annotations = nil }, err: "not managed"},
		{name: "labels alone do not authorize reuse", modify: func(db *platformApi.PostgresDatabase) {
			db.Annotations = nil
			db.Labels = map[string]string{"app.kubernetes.io/managed-by": "splunk-operator", "app.kubernetes.io/instance": "splunk-kvs-kvservice"}
		}, err: "not managed"},
		{name: "labels cannot override another live manager", modify: func(db *platformApi.PostgresDatabase) {
			db.Annotations[splcommon.KVServiceTenantDatabaseOwnerAnnotation] = "shared/previous"
			db.Labels = map[string]string{"app.kubernetes.io/managed-by": "splunk-operator", "app.kubernetes.io/instance": "splunk-kvs-kvservice"}
		}, services: []enterpriseApi.KVService{{ObjectMeta: metav1.ObjectMeta{Name: "previous"}}}, err: "not managed"},
		{name: "different cluster", modify: func(db *platformApi.PostgresDatabase) { db.Spec.ClusterRef.Name = "other" }, err: "incompatible"},
		{name: "wrong database", modify: func(db *platformApi.PostgresDatabase) { db.Spec.Databases[0].Name = "other" }, err: "incompatible"},
		{name: "delete policy", modify: func(db *platformApi.PostgresDatabase) { db.Spec.Databases[0].DeletionPolicy = "Delete" }, err: "incompatible"},
		{name: "foreign controller", modify: func(db *platformApi.PostgresDatabase) {
			db.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(&enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "other", UID: "other"}}, enterpriseApi.GroupVersion.WithKind("KVService"))}
		}, err: "controller reference"},
		{name: "current GC owner is not repaired", modify: func(db *platformApi.PostgresDatabase) {
			db.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(cr, enterpriseApi.GroupVersion.WithKind("KVService"))}
		}, err: "controller reference"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := resources.NewKVServiceTenantDatabase(cr, "alpha")
			if tc.modify != nil {
				tc.modify(db)
			}
			before := db.DeepCopy()
			adopt, err := classifyTenantDatabase(db, cr, "alpha", tc.services)
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				require.False(t, adopt)
				if tc.err != "incompatible" {
					message, terminal := splcommon.TerminalMessage(err)
					require.True(t, terminal)
					require.Contains(t, message, `Tenant "alpha" requires PostgresDatabase shared/alpha-database`)
					require.Contains(t, message, splcommon.AnnotationKVServiceAddr)
					require.Contains(t, message, splcommon.LabelTenant)
					require.Contains(t, message, "existing database is left unchanged")
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.adopt, adopt)
			}
			require.Equal(t, before, db, "classification must never mutate a retained database")
		})
	}
}
