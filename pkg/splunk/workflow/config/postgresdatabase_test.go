// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"context"
	"errors"
	"fmt"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestResolvePostgresDatabaseMounts(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, platformApi.AddToScheme(scheme))
	ref := enterpriseApi.PostgresDatabaseMountSpec{Name: "logical-alias", PostgresDatabaseRef: corev1.LocalObjectReference{Name: "db"}, Database: "app", IncludeAdmin: true}
	for _, tt := range []struct {
		name      string
		mutate    func(*spltest.PostgresDatabaseBundleFixture)
		missing   string
		namespace string
		pending   bool
		invalid   bool
	}{
		{name: "complete bundle"},
		{name: "stale database", mutate: func(f *spltest.PostgresDatabaseBundleFixture) { f.Database.Status.ObservedGeneration = new(int64(1)) }, pending: true},
		{name: "database not ready", mutate: func(f *spltest.PostgresDatabaseBundleFixture) { f.Database.Status.Databases[0].Ready = false }, pending: true},
		{name: "tombstone", mutate: func(f *spltest.PostgresDatabaseBundleFixture) { f.Database.Spec.Databases = nil }, invalid: true},
		{name: "incomplete refs", mutate: func(f *spltest.PostgresDatabaseBundleFixture) {
			f.Database.Status.Databases[0].AdminUserSecretRef = nil
		}, pending: true},
		{name: "cluster not ready", mutate: func(f *spltest.PostgresDatabaseBundleFixture) { f.Cluster.Status.Phase = new("Provisioning") }, pending: true},
		{name: "stale cluster", mutate: func(f *spltest.PostgresDatabaseBundleFixture) { f.Cluster.Generation++ }, pending: true},
		{name: "different provider name", mutate: func(f *spltest.PostgresDatabaseBundleFixture) {
			f.Cluster.Status.ProvisionerRef = &corev1.ObjectReference{Name: "other"}
		}, invalid: true},
		{name: "empty cluster ref", mutate: func(f *spltest.PostgresDatabaseBundleFixture) { f.Database.Spec.ClusterRef.Name = "" }, invalid: true},
		{name: "missing CA", missing: "primary-ca", pending: true},
		{name: "missing admin", missing: "published-admin", pending: true},
		{name: "missing RW", missing: "external-rw", pending: true},
		{name: "missing cluster", missing: "primary", pending: true},
		{name: "missing database", missing: "db", pending: true},
		{name: "missing configmap", missing: "published-config", pending: true},
		{name: "missing password key", mutate: func(f *spltest.PostgresDatabaseBundleFixture) { delete(f.RW.Data, "custom-password") }, invalid: true},
		{name: "empty admin password", mutate: func(f *spltest.PostgresDatabaseBundleFixture) { f.Admin.Data["password"] = nil }, invalid: true},
		{name: "invalid CA", mutate: func(f *spltest.PostgresDatabaseBundleFixture) { f.CA.Data["ca.crt"] = []byte("not a certificate") }, invalid: true},
		{name: "wrong username", mutate: func(f *spltest.PostgresDatabaseBundleFixture) { f.RW.Data["username"] = []byte("do-not-log-this") }, invalid: true},
		{name: "wrong database", mutate: func(f *spltest.PostgresDatabaseBundleFixture) { f.Metadata.Data["DATABASE_NAME"] = "different" }, invalid: true},
		{name: "localhost", mutate: func(f *spltest.PostgresDatabaseBundleFixture) { f.Metadata.Data["CLUSTER_RW_ENDPOINT"] = "localhost" }, invalid: true},
		{name: "loopback", mutate: func(f *spltest.PostgresDatabaseBundleFixture) { f.Metadata.Data["CLUSTER_RW_ENDPOINT"] = "127.0.0.1" }, invalid: true},
		{name: "bad port", mutate: func(f *spltest.PostgresDatabaseBundleFixture) { f.Metadata.Data["DEFAULT_CLUSTER_PORT"] = "0" }, invalid: true},
		{name: "cross namespace", namespace: "other", pending: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := spltest.NewPostgresDatabaseBundleFixture(t)
			if tt.mutate != nil {
				tt.mutate(&f)
			}
			var objects []client.Object
			for _, object := range f.Objects() {
				if object.GetName() != tt.missing {
					objects = append(objects, object)
				}
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			namespace := tt.namespace
			if namespace == "" {
				namespace = "ns"
			}
			mounts, err := ResolvePostgresDatabaseMounts(t.Context(), c, namespace, []enterpriseApi.PostgresDatabaseMountSpec{ref})
			if tt.pending || tt.invalid {
				want := ErrPostgresDatabasePending
				if tt.invalid {
					want = ErrPostgresDatabaseInvalid
				}
				require.ErrorIs(t, err, want)
				assert.Empty(t, mounts)
				assert.NotContains(t, err.Error(), "do-not-log-this")
				assert.NotContains(t, err.Error(), "test-admin-password")
				assert.NotContains(t, err.Error(), "test-rw-password")
				return
			}
			require.NoError(t, err)
			require.Len(t, mounts, 1)
			assert.Equal(t, "primary-ca", mounts[0].CASecretName)
			assert.Equal(t, f.Database.Status.Databases[0].AdminUserSecretRef, mounts[0].AdminPassword)
			assert.Equal(t, *f.Database.Status.Databases[0].RWUserSecretRef, mounts[0].RWPassword)
			assert.Equal(t, "app", mounts[0].Database)
			assert.Equal(t, f.Metadata.Name, mounts[0].ConfigMapName)
			result := fmt.Sprintf("%+v", mounts)
			assert.NotContains(t, result, "test-admin-password")
			assert.NotContains(t, result, "test-rw-password")
			assert.NotContains(t, result, "BEGIN CERTIFICATE")
		})
	}
	for _, key := range []string{"CLUSTER_RW_ENDPOINT", "DEFAULT_CLUSTER_PORT", "DATABASE_NAME", "ADMIN_USER_NAME", "RW_USER_NAME"} {
		t.Run("missing "+key, func(t *testing.T) {
			f := spltest.NewPostgresDatabaseBundleFixture(t)
			delete(f.Metadata.Data, key)
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(f.Objects()...).Build()
			_, err := ResolvePostgresDatabaseMounts(t.Context(), c, "ns", []enterpriseApi.PostgresDatabaseMountSpec{ref})
			require.ErrorIs(t, err, ErrPostgresDatabaseInvalid)
			assert.Contains(t, err.Error(), key)
		})
	}
	t.Run("API failure is not pending", func(t *testing.T) {
		failure := errors.New("API unavailable")
		c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
				return failure
			},
		}).Build()
		_, err := ResolvePostgresDatabaseMounts(t.Context(), c, "ns", []enterpriseApi.PostgresDatabaseMountSpec{ref})
		assert.ErrorIs(t, err, failure)
		assert.NotErrorIs(t, err, ErrPostgresDatabasePending)
	})
}
