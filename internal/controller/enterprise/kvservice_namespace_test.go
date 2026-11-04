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

package controller

import (
	"testing"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestKVServiceNamespaceMapper(t *testing.T) {
	t.Setenv("WATCH_NAMESPACE", "workload")
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, enterpriseApi.AddToScheme(scheme))
	require.NoError(t, enterpriseApiV3.AddToScheme(scheme))
	// The registered storage versions cover the shared v3/v4 kinds; legacy
	// ClusterMaster and LicenseMaster have their own controllers.
	for _, tc := range []struct {
		list   client.ObjectList
		object client.Object
	}{
		{&enterpriseApi.StandaloneList{}, &enterpriseApi.Standalone{}},
		{&enterpriseApi.IndexerClusterList{}, &enterpriseApi.IndexerCluster{}},
		{&enterpriseApi.SearchHeadClusterList{}, &enterpriseApi.SearchHeadCluster{}},
		{&enterpriseApi.ClusterManagerList{}, &enterpriseApi.ClusterManager{}},
		{&enterpriseApi.LicenseManagerList{}, &enterpriseApi.LicenseManager{}},
		{&enterpriseApi.MonitoringConsoleList{}, &enterpriseApi.MonitoringConsole{}},
		{&enterpriseApiV3.ClusterMasterList{}, &enterpriseApiV3.ClusterMaster{}},
		{&enterpriseApiV3.LicenseMasterList{}, &enterpriseApiV3.LicenseMaster{}},
	} {
		tc.object.SetName("consumer")
		tc.object.SetNamespace("workload")
		other := tc.object.DeepCopyObject().(client.Object)
		other.SetNamespace("other")
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.object, other).Build()
		mapper := kvServiceNamespaceMapper(c, tc.list)
		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "workload"}}
		// No enable label: disabling must still enqueue the existing consumers.
		assert.Equal(t, []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: "workload", Name: "consumer"}}}, mapper(t.Context(), namespace))
		assert.Empty(t, mapper(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other"}}))
		assert.Empty(t, mapper(t.Context(), &corev1.ConfigMap{}))
	}
}
