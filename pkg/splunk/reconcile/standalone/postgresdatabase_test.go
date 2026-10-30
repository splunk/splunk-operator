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

package standalone

import (
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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPostgresDatabaseStandaloneMounts(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.PostgresController, true)
	scheme := runtime.NewScheme()
	require.NoError(t, enterpriseApi.AddToScheme(scheme))
	require.NoError(t, platformApi.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	cr := &enterpriseApi.Standalone{Name: "search", Namespace: "ns", UID: "search-uid", Spec: enterpriseApi.StandaloneSpec{
		Replicas:               1,
		PostgresDatabaseMounts: []enterpriseApi.PostgresDatabaseMountSpec{{Name: "logical-alias", PostgresDatabaseRef: corev1.LocalObjectReference{Name: "db"}, Database: "app"}},
	}}
	resources.SetVolumeDefaults(&cr.Spec.CommonSplunkSpec)
	f := spltest.NewPostgresDatabaseBundleFixture(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(f.Objects()...).Build()
	_, err := k8sops.ApplySplunkConfig(t.Context(), c, cr, cr.Spec.CommonSplunkSpec, splcommon.SplunkStandalone)
	require.NoError(t, err)
	ss, err := GetStandaloneStatefulSet(t.Context(), c, cr)
	require.NoError(t, err)
	assert.Contains(t, ss.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: resources.PostgresDatabaseVolumeName, MountPath: resources.PostgresDatabaseMountRoot, ReadOnly: true})
	require.NoError(t, c.Create(t.Context(), ss))
	f.Cluster.Status.Phase = new("Provisioning")
	require.NoError(t, c.Update(t.Context(), f.Cluster))
	cr.Spec.Image = "splunk/splunk:security-update"
	cr.Spec.Replicas = 2
	pendingUpdate, err := GetStandaloneStatefulSet(t.Context(), c, cr)
	require.NoError(t, err, "temporary PostgreSQL unavailability must not block a workload update")
	assert.Equal(t, cr.Spec.Image, pendingUpdate.Spec.Template.Spec.Containers[0].Image)
	assert.Equal(t, int32(2), *pendingUpdate.Spec.Replicas)
	assert.Contains(t, pendingUpdate.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: resources.PostgresDatabaseVolumeName, MountPath: resources.PostgresDatabaseMountRoot, ReadOnly: true})
	f.Cluster.Status.Phase = new("Ready")
	require.NoError(t, c.Update(t.Context(), f.Cluster))
	cr.Spec.ExtraEnv = []corev1.EnvVar{{Name: "SPLUNK_HOME", Value: "/custom/splunk"}}
	_, err = GetStandaloneStatefulSet(t.Context(), c, cr)
	require.ErrorContains(t, err, `runtime directory "/custom/splunk/var/run/supervisor" requires a writable parent volume`)
	cr.Spec.PostgresDatabaseMounts = nil
	ss, err = GetStandaloneStatefulSet(t.Context(), c, cr)
	require.NoError(t, err)
	for _, volume := range ss.Spec.Template.Spec.Volumes {
		assert.NotEqual(t, resources.PostgresDatabaseVolumeName, volume.Name)
	}
	assert.Empty(t, cr.Status.Conditions)
}
