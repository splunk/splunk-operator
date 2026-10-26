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
	"context"
	"reflect"
	"strings"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestKVServiceReadinessAndConfigurationFailure(t *testing.T) {
	// Existing unit-test fixture value; this test only uses a fake Kubernetes client.
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := enterpriseApi.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	cr := &enterpriseApi.KVService{
		TypeMeta:   metav1.TypeMeta{APIVersion: enterpriseApi.GroupVersion.String(), Kind: "KVService"},
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "test", UID: "kvservice-id", Generation: 7},
	}
	cr.Spec.PodAnnotations = map[string]string{"sidecar.istio.io/inject": "false"}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&enterpriseApi.KVService{}, &appsv1.Deployment{}).WithObjects(cr).Build()
	key := types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}
	recorder := events.NewFakeRecorder(100)
	result, err := Apply(ctx, c, key, recorder)
	if err != nil {
		t.Fatal(err)
	}
	if result.RequeueAfter == 0 {
		t.Error("pending Deployment should be retried")
	}
	deployment := &appsv1.Deployment{}
	deploymentKey := types.NamespacedName{Namespace: cr.Namespace, Name: splutil.GetSplunkDeploymentName(splcommon.SplunkKVService, cr.Name)}
	if err := c.Get(ctx, deploymentKey, deployment); err != nil {
		t.Fatal(err)
	}
	secrets := &corev1.SecretList{}
	require.NoError(t, c.List(ctx, secrets))
	assert.Empty(t, secrets.Items, "KVService must not generate Enterprise namespace or versioned secrets")
	configMaps := &corev1.ConfigMapList{}
	require.NoError(t, c.List(ctx, configMaps))
	assert.Empty(t, configMaps.Items, "KVService must not create default.yml or App Framework ConfigMaps")
	assert.NotContains(t, deployment.Spec.Template.Annotations, "defaultConfigRev")
	assert.Empty(t, deployment.Spec.Template.Spec.Containers[0].Env)
	// Reconciling an older Deployment must remove the inherited Enterprise setup.
	deployment.Spec.Template.Spec.Containers[0].Env = append(deployment.Spec.Template.Spec.Containers[0].Env,
		corev1.EnvVar{Name: "SPLUNK_HOME", Value: "/opt/splunk"},
		corev1.EnvVar{Name: "SPLUNK_DEFAULTS_URL", Value: "/mnt/splunk-secrets/default.yml"})
	deployment.Spec.Template.Spec.Volumes = []corev1.Volume{{Name: "legacy-splunk-secret", VolumeSource: corev1.VolumeSource{
		Secret: &corev1.SecretVolumeSource{SecretName: "legacy-versioned-secret"},
	}}}
	deployment.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "legacy-splunk-secret", MountPath: "/mnt/splunk-secrets"}}
	require.NoError(t, c.Update(ctx, deployment))
	_, err = Apply(ctx, c, key, recorder)
	require.NoError(t, err)
	require.NoError(t, c.Get(ctx, deploymentKey, deployment))
	assert.Empty(t, deployment.Spec.Template.Spec.Containers[0].Env)
	assert.Empty(t, deployment.Spec.Template.Spec.Volumes)
	assert.Empty(t, deployment.Spec.Template.Spec.Containers[0].VolumeMounts)
	deployment.Status = appsv1.DeploymentStatus{Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1}
	if err := c.Status().Update(ctx, deployment); err != nil {
		t.Fatal(err)
	}
	result, err = Apply(ctx, c, key, recorder)
	if err != nil {
		t.Error(err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("ready Deployment still polls: %v", result)
	}
	current := &enterpriseApi.KVService{}
	if err := c.Get(ctx, key, current); err != nil {
		t.Fatal(err)
	}
	ready := meta.FindStatusCondition(current.Status.Conditions, string(enterpriseApi.ConditionReady))
	if ready == nil || ready.Status != metav1.ConditionTrue || ready.ObservedGeneration != 7 || current.Status.ObservedGeneration != 7 {
		t.Errorf("ready status was not persisted: %+v", current.Status)
	}

	for _, annotation := range []struct {
		name   string
		value  string
		remove bool
	}{
		{name: "false", value: "false"},
		{name: "empty", value: ""},
		{name: "removed", remove: true},
	} {
		t.Run("resume with annotation "+annotation.name, func(t *testing.T) {
			if err := c.Get(ctx, key, current); err != nil {
				t.Fatal(err)
			}
			current.Annotations = map[string]string{enterpriseApi.KVServicePausedAnnotation: "true"}
			if err := c.Update(ctx, current); err != nil {
				t.Fatal(err)
			}
			result, err := Apply(ctx, c, key, recorder)
			if err != nil {
				t.Fatalf("pause reconciliation failed: %v", err)
			}
			if result.RequeueAfter != splcommon.PauseRetryDelay {
				t.Errorf("true annotation did not pause reconciliation: result=%+v", result)
			}
			if err := c.Get(ctx, key, current); err != nil {
				t.Fatal(err)
			}
			paused := meta.FindStatusCondition(current.Status.Conditions, string(enterpriseApi.ConditionPaused))
			if paused == nil || paused.Status != metav1.ConditionTrue {
				t.Errorf("paused status missing: %+v", current.Status)
			}
			if annotation.remove {
				delete(current.Annotations, enterpriseApi.KVServicePausedAnnotation)
			} else {
				current.Annotations[enterpriseApi.KVServicePausedAnnotation] = annotation.value
			}
			if err := c.Update(ctx, current); err != nil {
				t.Fatal(err)
			}
			result, err = Apply(ctx, c, key, recorder)
			if err != nil {
				t.Errorf("resource did not resume: %v", err)
			}
			if err := c.Get(ctx, key, current); err != nil {
				t.Fatal(err)
			}
			paused = meta.FindStatusCondition(current.Status.Conditions, string(enterpriseApi.ConditionPaused))
			if paused == nil || paused.Status != metav1.ConditionFalse {
				t.Errorf("resumed status missing: %+v", current.Status)
			}
		})
	}

	// A missing configuration dependency must not replace a working pod template.
	if err := c.Get(ctx, deploymentKey, deployment); err != nil {
		t.Fatal(err)
	}
	before := deployment.Spec.Template.DeepCopy()
	current.Spec.ServiceAccount = "missing-service-account"
	current.Generation = 8
	if err := c.Update(ctx, current); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, c, key, recorder); !apierrors.IsNotFound(err) {
		t.Errorf("missing dependency error was lost: %v", err)
	}
	if err := c.Get(ctx, deploymentKey, deployment); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, &deployment.Spec.Template) {
		t.Error("incomplete pod configuration replaced the working Deployment")
	}
	if err := c.Get(ctx, key, current); err != nil {
		t.Fatal(err)
	}
	ready = meta.FindStatusCondition(current.Status.Conditions, string(enterpriseApi.ConditionReady))
	if ready == nil || ready.Status != metav1.ConditionFalse || current.Status.Phase != enterpriseApi.PhaseError || !strings.Contains(ready.Message, "missing-service-account") || current.Status.ObservedGeneration != 8 {
		t.Errorf("failed generation was not reflected in status: %+v", current.Status)
	}
}

func TestKVServicePausedStatus(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := enterpriseApi.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	cr := &enterpriseApi.KVService{ObjectMeta: metav1.ObjectMeta{Name: "paused", Namespace: "test", Generation: 9, Annotations: map[string]string{enterpriseApi.KVServicePausedAnnotation: "true"}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(cr).WithObjects(cr).Build()
	key := types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}
	result, err := Apply(ctx, c, key, events.NewFakeRecorder(10))
	if err != nil {
		t.Error(err)
	}
	if result.RequeueAfter != splcommon.PauseRetryDelay {
		t.Errorf("pause delay changed: %v", result)
	}
	if err := c.Get(ctx, key, cr); err != nil {
		t.Fatal(err)
	}
	paused := meta.FindStatusCondition(cr.Status.Conditions, string(enterpriseApi.ConditionPaused))
	if paused == nil || paused.Status != metav1.ConditionTrue || cr.Status.Phase != enterpriseApi.PhasePending || cr.Status.ObservedGeneration != 9 {
		t.Errorf("paused status missing: %+v", cr.Status)
	}
}

func TestKVServiceCertificateMountsAndRotation(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	previous := config.DefaultMutableFeatureGate.Enabled(config.CertManagement)
	require.NoError(t, config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.CertManagement): true}))
	t.Cleanup(func() {
		require.NoError(t, config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.CertManagement): previous}))
	})
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, enterpriseApi.AddToScheme(scheme))
	cr := &enterpriseApi.KVService{
		ObjectMeta: metav1.ObjectMeta{Name: "certs", Namespace: "test", UID: "kvservice-certs"},
		Spec: enterpriseApi.KVServiceSpec{
			CommonWorkloadSpec: enterpriseApi.CommonWorkloadSpec{
				Certs: []enterpriseApi.CertSpec{{SecretRef: corev1.LocalObjectReference{Name: "customer-tls"}}},
			},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "customer-tls", Namespace: cr.Namespace},
		Data:       map[string][]byte{"tls.crt": []byte("test-cert-v1"), "tls.key": []byte("test-private-key"), "ca.crt": []byte("test-ca")},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&enterpriseApi.KVService{}, &appsv1.Deployment{}).
		WithObjects(cr, secret).Build()
	key := types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}
	_, err := Apply(ctx, c, key, nil)
	require.NoError(t, err)
	deploymentKey := types.NamespacedName{Namespace: cr.Namespace,
		Name: splutil.GetSplunkDeploymentName(splcommon.SplunkKVService, cr.Name)}
	deployment := &appsv1.Deployment{}
	require.NoError(t, c.Get(ctx, deploymentKey, deployment))
	var certVolume string
	for _, volume := range deployment.Spec.Template.Spec.Volumes {
		if volume.Secret != nil && volume.Secret.SecretName == secret.Name {
			certVolume = volume.Name
		}
	}
	require.NotEmpty(t, certVolume, "certificate Secret must be mounted")
	found := false
	for _, mount := range deployment.Spec.Template.Spec.Containers[0].VolumeMounts {
		if mount.Name == certVolume {
			found = true
			assert.Equal(t, "/mnt/tls/customer-tls", mount.MountPath)
			assert.True(t, mount.ReadOnly)
		}
	}
	assert.True(t, found, "KVService container must receive the certificate mount")
	before := deployment.Spec.Template.DeepCopy()

	requests := certs.CertSecretMapper(c, &enterpriseApi.KVServiceList{})(ctx, secret)
	require.Len(t, requests, 1, "certificate Secret updates must enqueue KVService")
	assert.Equal(t, key, requests[0].NamespacedName)
	assert.Equal(t, []string{splcommon.GetServiceFQDN(cr.Namespace,
		splcommon.GetSplunkServiceName(splcommon.SplunkKVService, cr.Name, false))},
		certs.AutoDNSNames(splcommon.SplunkKVService, cr.Name, cr.Namespace, 3))

	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}, secret))
	secret.Data["tls.crt"] = []byte("test-cert-v2")
	require.NoError(t, c.Update(ctx, secret))
	_, err = Apply(ctx, c, key, nil)
	require.NoError(t, err)
	require.NoError(t, c.Get(ctx, deploymentKey, deployment))
	assert.NotEqual(t, before.Annotations, deployment.Spec.Template.Annotations,
		"certificate rotation must change the pod template to trigger a rollout")

	before = deployment.Spec.Template.DeepCopy()
	for _, invalid := range []struct {
		name, missingKey string
		data             map[string][]byte
	}{
		{name: "CA only", missingKey: "tls.crt", data: map[string][]byte{"ca.crt": []byte("test-ca")}},
		{name: "missing private key", missingKey: "tls.key", data: map[string][]byte{"tls.crt": []byte("missing-private-key")}},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			secret.Data = invalid.data
			require.NoError(t, c.Update(ctx, secret))
			_, err = Apply(ctx, c, key, nil)
			require.ErrorContains(t, err, invalid.missingKey)
			require.NoError(t, c.Get(ctx, deploymentKey, deployment))
			assert.Equal(t, before, &deployment.Spec.Template, "invalid certificates must preserve the working Deployment")
		})
	}
}
