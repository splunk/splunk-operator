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

package searchheadcluster

import (
	"maps"
	"strings"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/config"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPostgresDatabaseMountsExcludeDeployer(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.PostgresController, true)
	scheme := runtime.NewScheme()
	require.NoError(t, enterpriseApi.AddToScheme(scheme))
	require.NoError(t, platformApi.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	cr := &enterpriseApi.SearchHeadCluster{Name: "search", Namespace: "ns", UID: "search-uid", Spec: enterpriseApi.SearchHeadClusterSpec{
		Replicas:               3,
		PostgresDatabaseMounts: []enterpriseApi.PostgresDatabaseMountSpec{{Name: "logical-alias", PostgresDatabaseRef: corev1.LocalObjectReference{Name: "db"}, Database: "app"}},
	}}
	resources.SetVolumeDefaults(&cr.Spec.CommonSplunkSpec)
	f := spltest.NewPostgresDatabaseBundleFixture(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(f.Objects()...).Build()
	_, err := k8sops.ApplySplunkConfig(t.Context(), c, cr, cr.Spec.CommonSplunkSpec, splcommon.SplunkSearchHead)
	require.NoError(t, err)
	members, err := getSearchHeadStatefulSet(t.Context(), c, cr)
	require.NoError(t, err)
	assert.Contains(t, members.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: resources.PostgresDatabaseVolumeName, MountPath: resources.PostgresDatabaseMountRoot, ReadOnly: true})
	deployer, err := getDeployerStatefulSet(t.Context(), c, cr)
	require.NoError(t, err)
	for _, volume := range deployer.Spec.Template.Spec.Volumes {
		assert.NotEqual(t, resources.PostgresDatabaseVolumeName, volume.Name)
	}

	projection := func(ss *appsv1.StatefulSet) *corev1.ProjectedVolumeSource {
		for _, volume := range ss.Spec.Template.Spec.Volumes {
			if volume.Name == resources.PostgresDatabaseVolumeName {
				return volume.Projected
			}
		}
		t.Fatal("missing PostgresDatabase projected volume")
		return nil
	}
	assert.Equal(t, f.Metadata.Name, projection(members).Sources[0].ConfigMap.Name)

	require.NoError(t, c.Create(t.Context(), members))
	f.Cluster.Status.Phase = new("Provisioning")
	require.NoError(t, c.Update(t.Context(), f.Cluster))
	cr.Spec.Image = "splunk/splunk:security-update"
	cr.Spec.Replicas = 4
	pendingUpdate, err := getSearchHeadStatefulSet(t.Context(), c, cr)
	require.NoError(t, err, "temporary PostgreSQL unavailability must not block SHC updates")
	assert.Equal(t, cr.Spec.Image, pendingUpdate.Spec.Template.Spec.Containers[0].Image)
	assert.Equal(t, int32(4), *pendingUpdate.Spec.Replicas)
	assert.Equal(t, projection(members), projection(pendingUpdate))
	f.Cluster.Status.Phase = new("Ready")
	require.NoError(t, c.Update(t.Context(), f.Cluster))
	members = pendingUpdate

	// Producer data refreshes mounted files without a pod-template change.
	f.Metadata.Data["CLUSTER_RW_ENDPOINT"] = "updated.ns.svc.cluster.local"
	require.NoError(t, c.Update(t.Context(), f.Metadata))
	f.RW.Data["custom-password"] = []byte("rotated-test-password")
	require.NoError(t, c.Update(t.Context(), f.RW))
	reloaded, err := getSearchHeadStatefulSet(t.Context(), c, cr)
	require.NoError(t, err)
	assert.Equal(t, members.Spec.Template, reloaded.Spec.Template)

	// A replacement publication changes the member's references. The old
	// producer objects remain untouched and available to old pods.
	f.Database.Status.Databases[0].RWUserSecretRef.Name = "rotated-rw"
	f.Database.Status.Databases[0].ConfigMapRef.Name = "replacement-config"
	require.NoError(t, c.Update(t.Context(), f.Database))
	replacement := maps.Clone(f.Metadata.Data)
	replacement["CLUSTER_RW_ENDPOINT"] = "replacement.ns.svc.cluster.local"
	require.NoError(t, c.Create(t.Context(), &corev1.ConfigMap{Name: "replacement-config", Namespace: "ns", Data: replacement}))
	require.NoError(t, c.Create(t.Context(), &corev1.Secret{Name: "rotated-rw", Namespace: "ns", Data: f.RW.Data}))
	updated, err := getSearchHeadStatefulSet(t.Context(), c, cr)
	require.NoError(t, err)
	assert.NotEqual(t, members.Spec.Template, updated.Spec.Template)
	assert.Equal(t, "replacement-config", projection(updated).Sources[0].ConfigMap.Name)
	assert.Equal(t, "rotated-rw", projection(updated).Sources[1].Secret.Name)
	metadata := &corev1.ConfigMap{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(f.Metadata), metadata))
	assert.Equal(t, "updated.ns.svc.cluster.local", metadata.Data["CLUSTER_RW_ENDPOINT"])
	assert.Empty(t, metadata.OwnerReferences)
	secret := &corev1.Secret{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(f.RW), secret))
	assert.Empty(t, secret.OwnerReferences)
	var configmaps corev1.ConfigMapList
	require.NoError(t, c.List(t.Context(), &configmaps))
	for _, cm := range configmaps.Items {
		assert.False(t, strings.HasPrefix(cm.Name, "splunk-postgresok-"), "no consumer-owned metadata maps")
	}
}
