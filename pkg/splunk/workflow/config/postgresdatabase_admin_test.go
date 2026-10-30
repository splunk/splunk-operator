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
	"testing"

	v4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPostgresDatabaseDefaultDoesNotRequireAdmin(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, platformApi.AddToScheme(scheme))
	f := spltest.NewPostgresDatabaseBundleFixture(t)
	f.Database.Status.Databases[0].AdminUserSecretRef = nil
	var objects []client.Object
	for _, object := range f.Objects() {
		if object != f.Admin {
			objects = append(objects, object)
		}
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	mounts, err := ResolvePostgresDatabaseMounts(t.Context(), c, "ns", []v4.PostgresDatabaseMountSpec{{
		Name: "app", Database: "app", PostgresDatabaseRef: corev1.LocalObjectReference{Name: "db"},
	}})
	require.NoError(t, err)
	require.Len(t, mounts, 1)
	assert.Empty(t, mounts[0].AdminPassword)
}
