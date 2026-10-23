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

package k8sops

import (
	"context"
	"strings"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetKVServiceDeployment(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kvservice-sa",
			Namespace: "test",
		},
	}
	c.AddObject(serviceAccount)

	cr := &enterpriseApi.KVService{
		TypeMeta: metav1.TypeMeta{
			Kind: "KVService",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	cr.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "enterprise.splunk.com",
		Version: "v4",
		Kind:    "KVService",
	})
	cr.Spec.Image = "kvstore/kvservice:test"
	cr.Spec.ImagePullPolicy = "IfNotPresent"
	cr.Spec.ServiceAccount = "kvservice-sa"
	cr.Spec.Replicas = 1

	deployment, err := GetKVServiceDeployment(ctx, c, cr)
	if err != nil {
		t.Fatalf("GetKVServiceDeployment error=%v", err)
	}

	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
		t.Errorf("deployment replicas=%v; want 1", deployment.Spec.Replicas)
	}

	if deployment.Spec.Template.Spec.ServiceAccountName != "kvservice-sa" {
		t.Errorf("service account=%s; want %s", deployment.Spec.Template.Spec.ServiceAccountName, "kvservice-sa")
	}

	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		t.Fatalf("deployment containers empty")
	}
	container := deployment.Spec.Template.Spec.Containers[0]
	if container.Image != "kvstore/kvservice:test" {
		t.Errorf("container image=%s; want %s", container.Image, "kvstore/kvservice:test")
	}

	if container.LivenessProbe == nil || container.LivenessProbe.HTTPGet == nil {
		t.Fatalf("liveness probe not set")
	}
	if container.LivenessProbe.HTTPGet.Path != "/service/liveness" {
		t.Errorf("liveness path=%s; want %s", container.LivenessProbe.HTTPGet.Path, "/service/liveness")
	}
	if container.LivenessProbe.HTTPGet.Port.IntValue() != splcommon.KVServicePort {
		t.Errorf("liveness port=%d; want %d", container.LivenessProbe.HTTPGet.Port.IntValue(), splcommon.KVServicePort)
	}
	if container.LivenessProbe.InitialDelaySeconds != 30 {
		t.Errorf("liveness initial delay=%d; want %d", container.LivenessProbe.InitialDelaySeconds, 30)
	}
	if container.LivenessProbe.TimeoutSeconds != 1 {
		t.Errorf("liveness timeout=%d; want %d", container.LivenessProbe.TimeoutSeconds, 1)
	}
	if container.LivenessProbe.PeriodSeconds != 5 {
		t.Errorf("liveness period=%d; want %d", container.LivenessProbe.PeriodSeconds, 5)
	}
	if container.LivenessProbe.FailureThreshold != 3 {
		t.Errorf("liveness failure threshold=%d; want %d", container.LivenessProbe.FailureThreshold, 3)
	}
	if container.LivenessProbe.SuccessThreshold != 1 {
		t.Errorf("liveness success threshold=%d; want %d", container.LivenessProbe.SuccessThreshold, 1)
	}

	if container.ReadinessProbe == nil || container.ReadinessProbe.HTTPGet == nil {
		t.Fatalf("readiness probe not set")
	}
	if container.ReadinessProbe.HTTPGet.Path != "/service/readiness" {
		t.Errorf("readiness path=%s; want %s", container.ReadinessProbe.HTTPGet.Path, "/service/readiness")
	}
	if container.ReadinessProbe.HTTPGet.Port.IntValue() != splcommon.KVServicePort {
		t.Errorf("readiness port=%d; want %d", container.ReadinessProbe.HTTPGet.Port.IntValue(), splcommon.KVServicePort)
	}
	if container.ReadinessProbe.InitialDelaySeconds != 30 {
		t.Errorf("readiness initial delay=%d; want %d", container.ReadinessProbe.InitialDelaySeconds, 30)
	}
	if container.ReadinessProbe.TimeoutSeconds != 1 {
		t.Errorf("readiness timeout=%d; want %d", container.ReadinessProbe.TimeoutSeconds, 1)
	}
	if container.ReadinessProbe.PeriodSeconds != 5 {
		t.Errorf("readiness period=%d; want %d", container.ReadinessProbe.PeriodSeconds, 5)
	}
	if container.ReadinessProbe.FailureThreshold != 3 {
		t.Errorf("readiness failure threshold=%d; want %d", container.ReadinessProbe.FailureThreshold, 3)
	}
	if container.ReadinessProbe.SuccessThreshold != 1 {
		t.Errorf("readiness success threshold=%d; want %d", container.ReadinessProbe.SuccessThreshold, 1)
	}
	if container.StartupProbe != nil {
		t.Errorf("KVService must not configure a startup probe: %+v", container.StartupProbe)
	}

	if len(deployment.OwnerReferences) == 0 {
		t.Errorf("deployment owner references not set")
	}
}

func TestKVServiceConfigurationAndConfigMapRollouts(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, enterpriseApi.AddToScheme(scheme))
	cr := &enterpriseApi.KVService{
		TypeMeta:   metav1.TypeMeta{Kind: "KVService", APIVersion: enterpriseApi.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "test", UID: "kvservice-id"},
	}
	cr.Spec.Defaults = "splunk:\n  hec:\n    enable: false\n"
	cr.Spec.ExtraEnv = []corev1.EnvVar{{Name: "SPLUNK_ROLE", Value: "custom-role"}, {Name: "DB_HOST", Value: "postgres"}}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "db-config", Namespace: cr.Namespace}, Data: map[string]string{"used": "first", "unused": "ignore"}}
	cr.Spec.Volumes = []corev1.Volume{
		{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: cm.Name}, Items: []corev1.KeyToPath{{Key: "used", Path: "db"}},
		}}},
		{Name: strings.Repeat("p", 63), VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{Sources: []corev1.VolumeProjection{{ConfigMap: &corev1.ConfigMapProjection{
			LocalObjectReference: corev1.LocalObjectReference{Name: cm.Name}, Items: []corev1.KeyToPath{{Key: "used", Path: "db"}},
		}}}}}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cr, cm).Build()
	require.NoError(t, ApplyKVServiceConfig(ctx, c, cr))
	before := cr.DeepCopy()
	deployment, err := GetKVServiceDeployment(ctx, c, cr)
	require.NoError(t, err)
	assert.Equal(t, before, cr, "resolving configuration must not mutate the CR")
	require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
	container := deployment.Spec.Template.Spec.Containers[0]
	assert.Contains(t, container.Env, corev1.EnvVar{Name: "SPLUNK_DEFAULTS_URL", Value: "/mnt/splunk-defaults/default.yml,/mnt/splunk-secrets/default.yml"})
	assert.Contains(t, container.Env, corev1.EnvVar{Name: "SPLUNK_ROLE", Value: "custom-role"})
	assert.Contains(t, container.Env, corev1.EnvVar{Name: "DB_HOST", Value: "postgres"})
	roleCount := 0
	for _, env := range container.Env {
		if env.Name == "SPLUNK_ROLE" {
			roleCount++
		}
	}
	assert.Equal(t, 1, roleCount, "user environment overrides must not create duplicate entries")
	assert.Contains(t, container.VolumeMounts, corev1.VolumeMount{Name: "mnt-splunk-secrets", MountPath: "/mnt/splunk-secrets"})
	assert.Contains(t, container.VolumeMounts, corev1.VolumeMount{Name: "mnt-splunk-defaults", MountPath: "/mnt/splunk-defaults"})
	first := deployment.Spec.Template.Annotations
	assert.NotEmpty(t, first["defaultConfigRev"])
	assert.NotEmpty(t, first[splcommon.ConfigMapRevAnnotationPrefix+"config"])
	projectedKey := ""
	for key := range first {
		if strings.HasPrefix(key, splcommon.ConfigMapRevAnnotationPrefix+"p.") {
			projectedKey = key
		}
	}
	require.NotEmpty(t, projectedKey, "long projected volume names need a valid restart annotation")
	assert.LessOrEqual(t, len(strings.SplitN(projectedKey, "/", 2)[1]), 63)
	cmKey := types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}
	require.NoError(t, c.Get(ctx, cmKey, cm))
	cm.Labels = map[string]string{"metadata": "updated"}
	cm.Data["unused"] = "changed"
	require.NoError(t, c.Update(ctx, cm))
	unchanged, err := GetKVServiceDeployment(ctx, c, cr)
	require.NoError(t, err)
	assert.Equal(t, first, unchanged.Spec.Template.Annotations, "metadata and unmounted keys must not trigger a rollout")
	cm.Data["used"] = "second"
	require.NoError(t, c.Update(ctx, cm))
	changed, err := GetKVServiceDeployment(ctx, c, cr)
	require.NoError(t, err)
	assert.NotEqual(t, first[splcommon.ConfigMapRevAnnotationPrefix+"config"], changed.Spec.Template.Annotations[splcommon.ConfigMapRevAnnotationPrefix+"config"])
	assert.NotEqual(t, first[projectedKey], changed.Spec.Template.Annotations[projectedKey])
	cm.Annotations = map[string]string{splcommon.ConfigMapRestartOptOutAnnotation: "false"}
	require.NoError(t, c.Update(ctx, cm))
	optOut, err := GetKVServiceDeployment(ctx, c, cr)
	require.NoError(t, err)
	assert.NotContains(t, optOut.Spec.Template.Annotations, splcommon.ConfigMapRevAnnotationPrefix+"config")
	assert.NotContains(t, optOut.Spec.Template.Annotations, projectedKey)
}
