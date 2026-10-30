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
	"context"
	"testing"

	v4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/config"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPostgresDatabasePendingPreservesProjectionForWorkloadUpdates(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.PostgresController, true)
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, platformApi.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	f := spltest.NewPostgresDatabaseBundleFixture(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(f.Objects()...).Build()
	cr := &v4.Standalone{Name: "search", Namespace: "ns", UID: "owner", Generation: 1}
	specs := []v4.PostgresDatabaseMountSpec{{Name: "app", Database: "app", PostgresDatabaseRef: corev1.LocalObjectReference{Name: "db"}}}
	previous := &appsv1.StatefulSet{
		Name: "search", Namespace: "ns",
		OwnerReferences: []metav1.OwnerReference{{APIVersion: v4.GroupVersion.String(), Kind: "Standalone", Name: cr.Name, UID: cr.UID, Controller: new(true)}},
		Spec:            appsv1.StatefulSetSpec{Replicas: new(int32(1)), Template: postgresTestTemplate("previous")},
	}
	require.NoError(t, ApplyPostgresDatabaseBundle(t.Context(), c, cr, specs, nil, &cr.Status.Conditions, previous))
	require.NoError(t, c.Create(t.Context(), previous))
	f.Cluster.Status.Phase = new("Provisioning")
	require.NoError(t, c.Update(t.Context(), f.Cluster))
	desired := &appsv1.StatefulSet{Name: previous.Name, Namespace: previous.Namespace, Spec: appsv1.StatefulSetSpec{
		Replicas: new(int32(2)), Template: postgresTestTemplate("security-update"),
	}}
	require.NoError(t, ApplyPostgresDatabaseBundle(t.Context(), c, cr, specs, nil, &cr.Status.Conditions, desired))
	assert.Equal(t, "security-update", desired.Spec.Template.Spec.Containers[0].Image)
	require.Len(t, desired.Spec.Template.Spec.InitContainers, 1)
	assert.Equal(t, "security-update", desired.Spec.Template.Spec.InitContainers[0].Image)
	assert.Equal(t, []corev1.VolumeMount{{Name: "var", MountPath: "/opt/splunk/var"}}, desired.Spec.Template.Spec.InitContainers[0].VolumeMounts)
	assert.Equal(t, int32(2), *desired.Spec.Replicas)
	assert.Equal(t, previous.Spec.Template.Spec.Volumes, desired.Spec.Template.Spec.Volumes)
	assert.Equal(t, previous.Spec.Template.Spec.Containers[0].VolumeMounts, desired.Spec.Template.Spec.Containers[0].VolumeMounts)
	condition := meta.FindStatusCondition(cr.Status.Conditions, string(v4.ConditionPostgresDatabaseResolved))
	require.NotNil(t, condition)
	assert.Equal(t, metav1.ConditionFalse, condition.Status)
	phase, _ := PostgresDatabaseWorkloadPhase(v4.PhaseReady, "", cr.Status.Conditions)
	assert.Equal(t, v4.PhasePending, phase, "retaining mounts must not report the dependency as ready")
	phase, _ = PostgresDatabaseWorkloadPhase(v4.PhaseUpdating, "", cr.Status.Conditions)
	assert.Equal(t, v4.PhaseUpdating, phase)

	for _, tt := range []struct {
		name   string
		change func([]v4.PostgresDatabaseMountSpec, *appsv1.StatefulSet)
		want   error
	}{
		{"different producer", func(specs []v4.PostgresDatabaseMountSpec, _ *appsv1.StatefulSet) {
			specs[0].PostgresDatabaseRef.Name = "other"
		}, configworkflow.ErrPostgresDatabasePending},
		{"different database", func(specs []v4.PostgresDatabaseMountSpec, _ *appsv1.StatefulSet) { specs[0].Database = "other" }, configworkflow.ErrPostgresDatabaseInvalid},
		{"admin opt-in change", func(specs []v4.PostgresDatabaseMountSpec, _ *appsv1.StatefulSet) { specs[0].IncludeAdmin = true }, configworkflow.ErrPostgresDatabasePending},
		{"different home", func(_ []v4.PostgresDatabaseMountSpec, ss *appsv1.StatefulSet) {
			ss.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "SPLUNK_HOME", Value: "/custom/splunk"}}
		}, configworkflow.ErrPostgresDatabasePending},
		{"initial creation", func(_ []v4.PostgresDatabaseMountSpec, ss *appsv1.StatefulSet) { ss.Name = "not-created" }, configworkflow.ErrPostgresDatabasePending},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requested := append([]v4.PostgresDatabaseMountSpec(nil), specs...)
			candidate := &appsv1.StatefulSet{Name: previous.Name, Namespace: previous.Namespace, Spec: appsv1.StatefulSetSpec{
				Replicas: new(int32(2)), Template: postgresTestTemplate("security-update"),
			}}
			tt.change(requested, candidate)
			before := candidate.DeepCopy()
			require.ErrorIs(t, ApplyPostgresDatabaseBundle(t.Context(), c, cr, requested, nil, &cr.Status.Conditions, candidate), tt.want)
			assert.Equal(t, before, candidate)
		})
	}
}

type postgresWarningRecorder struct{ warnings []string }

func (*postgresWarningRecorder) Normal(context.Context, string, string) {}
func (p *postgresWarningRecorder) Warning(_ context.Context, reason, _ string) {
	p.warnings = append(p.warnings, reason)
}

func TestPostgresDatabaseInvalidPublicationEmitsTransitionWarning(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.PostgresController, true)
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, platformApi.AddToScheme(scheme))
	f := spltest.NewPostgresDatabaseBundleFixture(t)
	f.Cluster.Status.ProvisionerRef = &corev1.ObjectReference{Name: "unsupported"}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(f.Objects()...).Build()
	cr := &v4.Standalone{Name: "search", Namespace: "ns", Generation: 1}
	specs := []v4.PostgresDatabaseMountSpec{{Name: "app", Database: "app", PostgresDatabaseRef: corev1.LocalObjectReference{Name: "db"}}}
	publisher := &postgresWarningRecorder{}
	ctx := context.WithValue(t.Context(), splcommon.EventPublisherKey, publisher)
	for range 2 {
		err := ApplyPostgresDatabaseBundle(ctx, c, cr, specs, nil, &cr.Status.Conditions, &appsv1.StatefulSet{})
		require.ErrorIs(t, err, configworkflow.ErrPostgresDatabaseInvalid)
		require.NotErrorIs(t, err, configworkflow.ErrPostgresDatabasePending)
	}
	condition := meta.FindStatusCondition(cr.Status.Conditions, string(v4.ConditionPostgresDatabaseResolved))
	require.NotNil(t, condition)
	assert.Equal(t, postgresDatabaseInvalidReason, condition.Reason)
	assert.Equal(t, []string{postgresDatabaseInvalidReason}, publisher.warnings)
	f.Cluster.Status.ProvisionerRef = nil
	require.NoError(t, c.Update(ctx, f.Cluster))
	desired := &appsv1.StatefulSet{Spec: appsv1.StatefulSetSpec{Template: postgresTestTemplate("splunk:test")}}
	require.NoError(t, ApplyPostgresDatabaseBundle(ctx, c, cr, specs, nil, &cr.Status.Conditions, desired))
	assert.True(t, meta.IsStatusConditionTrue(cr.Status.Conditions, string(v4.ConditionPostgresDatabaseResolved)))
}

func TestPostgresDatabasePendingProjectionIsNotAliased(t *testing.T) {
	// Preservation copies a cached object, not its slice/pointer storage.
	previousTemplate := postgresTestTemplate("splunk:test")
	previous := &previousTemplate
	previous.Annotations = map[string]string{splcommon.PostgresDatabaseBindingAnnotation: "binding"}
	bundle := resources.PostgresDatabaseBundle{{Database: "app", ConfigMapName: "metadata"}}
	require.NoError(t, bundle.Inject(previous))
	desiredTemplate := postgresTestTemplate("splunk:test")
	desired := &desiredTemplate
	kept, err := resources.PreservePostgresDatabaseMounts(previous, desired, "binding")
	require.NoError(t, err)
	require.True(t, kept)
	original := previous.DeepCopy()
	desired.Spec.Volumes[1].Projected.Sources[0].ConfigMap.Name = "changed"
	assert.Equal(t, original, previous)
}
