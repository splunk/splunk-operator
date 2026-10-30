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

package reconcile

import (
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	configworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPostgresDatabaseDependencyCondition(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.PostgresController, true)
	scheme := runtime.NewScheme()
	require.NoError(t, platformApi.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	cr := &enterpriseApi.Standalone{Name: "search", Namespace: "ns", Generation: 3}
	specs := []enterpriseApi.PostgresDatabaseMountSpec{{Name: "app", PostgresDatabaseRef: corev1.LocalObjectReference{Name: "db"}, Database: "app"}}
	err := ApplyPostgresDatabaseBundle(t.Context(), c, cr, specs, nil, &cr.Status.Conditions, nil)
	require.ErrorIs(t, err, configworkflow.ErrPostgresDatabasePending)
	condition := meta.FindStatusCondition(cr.Status.Conditions, string(enterpriseApi.ConditionPostgresDatabaseResolved))
	require.NotNil(t, condition)
	assert.Equal(t, "WaitingForPostgresDatabase", condition.Reason)
	assert.Equal(t, int64(3), condition.ObservedGeneration)
	err = ApplyPostgresDatabaseBundle(t.Context(), c, cr, nil, nil, &cr.Status.Conditions, nil)
	require.NoError(t, err)
	assert.Nil(t, meta.FindStatusCondition(cr.Status.Conditions, string(enterpriseApi.ConditionPostgresDatabaseResolved)))
}

func TestPostgresDatabaseDisabledGate(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.PostgresController, false)
	cr := &enterpriseApi.Standalone{}
	specs := []enterpriseApi.PostgresDatabaseMountSpec{{Name: "app", PostgresDatabaseRef: corev1.LocalObjectReference{Name: "db"}, Database: "app"}}
	err := ApplyPostgresDatabaseBundle(t.Context(), nil, cr, specs, nil, &cr.Status.Conditions, nil)
	require.ErrorContains(t, err, "PostgresController feature gate")
}

func TestPostgresDatabaseMountsAreReadOnlyConsumers(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.PostgresController, true)
	scheme := runtime.NewScheme()
	require.NoError(t, platformApi.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	f := spltest.NewPostgresDatabaseBundleFixture(t)
	f.Metadata.OwnerReferences = []metav1.OwnerReference{{APIVersion: platformApi.GroupVersion.String(), Kind: "PostgresDatabase", Name: f.Database.Name, UID: "producer", Controller: new(true)}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(f.Objects()...).Build()
	var before []client.Object
	for _, object := range f.Objects() {
		snapshot := object.DeepCopyObject().(client.Object)
		require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(object), snapshot))
		before = append(before, snapshot)
	}
	cr := &enterpriseApi.Standalone{Name: "search", Namespace: "ns", UID: "owner"}
	specs := []enterpriseApi.PostgresDatabaseMountSpec{{Name: "alias", PostgresDatabaseRef: corev1.LocalObjectReference{Name: "db"}, Database: "app"}}
	desired := &appsv1.StatefulSet{Spec: appsv1.StatefulSetSpec{Template: postgresTestTemplate("splunk:test")}}
	require.NoError(t, ApplyPostgresDatabaseBundle(t.Context(), c, cr, specs, nil, &cr.Status.Conditions, desired))
	assert.Equal(t, f.Metadata.Name, desired.Spec.Template.Spec.Volumes[1].Projected.Sources[0].ConfigMap.Name)
	for _, snapshot := range before {
		after := snapshot.DeepCopyObject().(client.Object)
		require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(snapshot), after))
		assert.Equal(t, snapshot, after, "consumer must not mutate %T", snapshot)
	}
	var configmaps corev1.ConfigMapList
	require.NoError(t, c.List(t.Context(), &configmaps))
	assert.Len(t, configmaps.Items, 1, "only the producer ConfigMap may exist")
	var secrets corev1.SecretList
	require.NoError(t, c.List(t.Context(), &secrets))
	assert.Len(t, secrets.Items, 3, "no copied credential or CA Secrets")

	// Missing/invalid source data leaves the workload template unchanged.
	f.CA.Data["ca.crt"] = nil
	require.NoError(t, c.Update(t.Context(), f.CA))
	original := desired.DeepCopy()
	require.ErrorIs(t, ApplyPostgresDatabaseBundle(t.Context(), c, cr, specs, nil, &cr.Status.Conditions, desired), configworkflow.ErrPostgresDatabasePending)
	assert.Equal(t, original, desired)
}

func TestPostgresDatabaseInvalidProjectionDoesNotMutate(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.PostgresController, true)
	scheme := runtime.NewScheme()
	require.NoError(t, platformApi.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	f := spltest.NewPostgresDatabaseBundleFixture(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(f.Objects()...).Build()
	cr := &enterpriseApi.Standalone{Name: "search", Namespace: "ns", UID: "owner"}
	specs := []enterpriseApi.PostgresDatabaseMountSpec{{Name: "alias", PostgresDatabaseRef: corev1.LocalObjectReference{Name: "db"}, Database: "app"}}
	desired := &appsv1.StatefulSet{Spec: appsv1.StatefulSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
		Name: "splunk", VolumeMounts: []corev1.VolumeMount{{Name: "collision", MountPath: resources.PostgresDatabaseMountRoot}},
	}}}}}}
	before := desired.DeepCopy()
	require.ErrorContains(t, ApplyPostgresDatabaseBundle(t.Context(), c, cr, specs, nil, &cr.Status.Conditions, desired), "conflicts")
	assert.Equal(t, before, desired)
}

func postgresTestTemplate(image string) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{Spec: corev1.PodSpec{
		SecurityContext: &corev1.PodSecurityContext{RunAsUser: new(int64(41812)), FSGroup: new(int64(41812))},
		Volumes:         []corev1.Volume{{Name: "var", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
		Containers:      []corev1.Container{{Name: "splunk", Image: image, VolumeMounts: []corev1.VolumeMount{{Name: "var", MountPath: "/opt/splunk/var"}}}},
	}}
}
