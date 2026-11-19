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
	"bytes"
	"context"
	"crypto/x509"
	"encoding/asn1"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestKVServiceAutoscalingReconciliation(t *testing.T) {
	// Existing unit-test fixture value; this test only uses a fake Kubernetes client.
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := t.Context()
	cr, _, c := postgresReadinessFixture(t)
	cr.Spec.Replicas = ptr.To(int32(2))
	cr.Spec.Autoscaling = &enterpriseApi.KVServiceAutoscalingSpec{
		MinReplicas: ptr.To(int32(2)), MaxReplicas: ptr.To(int32(10)), TargetAverageUtilizationCPU: ptr.To(int32(60)), TargetAverageUtilizationMemory: ptr.To(int32(60)),
	}
	require.NoError(t, c.Update(ctx, cr))
	key := types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name}
	recorder := events.NewFakeRecorder(100)
	_, err := Apply(ctx, c, key, recorder)
	assert.NoError(t, err)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	hpaKey := types.NamespacedName{Namespace: cr.Namespace, Name: "splunk-" + cr.Name + "-kvservice-hpa"}
	require.NoError(t, c.Get(ctx, hpaKey, hpa))
	assert.Equal(t, "KVService", hpa.Spec.ScaleTargetRef.Kind)
	assert.Equal(t, cr.Name, hpa.Spec.ScaleTargetRef.Name)
	if assert.NotNil(t, hpa.Spec.MinReplicas) {
		assert.Equal(t, int32(2), *hpa.Spec.MinReplicas)
	}
	assert.Equal(t, int32(10), hpa.Spec.MaxReplicas)

	// The HPA writes spec.replicas through /scale. Reconciliation must use that value.
	require.NoError(t, c.Get(ctx, key, cr))
	cr.Spec.Replicas = ptr.To(int32(4))
	require.NoError(t, c.Update(ctx, cr))
	_, err = Apply(ctx, c, key, recorder)
	assert.NoError(t, err)
	deployment := &appsv1.Deployment{}
	deploymentKey := types.NamespacedName{Namespace: cr.Namespace, Name: "splunk-" + cr.Name + "-kvservice"}
	require.NoError(t, c.Get(ctx, deploymentKey, deployment))
	if assert.NotNil(t, deployment.Spec.Replicas) {
		assert.Equal(t, int32(4), *deployment.Spec.Replicas)
	}
	require.NoError(t, c.Get(ctx, key, cr))
	assert.Equal(t, int32(4), cr.Status.Replicas)

	cr.Spec.Autoscaling = nil
	require.NoError(t, c.Update(ctx, cr))
	_, err = Apply(ctx, c, key, recorder)
	assert.NoError(t, err)
	assert.True(t, apierrors.IsNotFound(c.Get(ctx, hpaKey, hpa)))
	require.NoError(t, c.Get(ctx, key, cr))
	assert.Equal(t, ptr.To(int32(4)), cr.Spec.Replicas, "disabling autoscaling retains the last selected replica count")

	// An HPA ownership conflict must prevent a Ready status.
	foreign := &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: hpaKey.Name, Namespace: hpaKey.Namespace}}
	require.NoError(t, c.Create(ctx, foreign))
	cr.Spec.Autoscaling = &enterpriseApi.KVServiceAutoscalingSpec{
		MinReplicas: ptr.To(int32(2)), MaxReplicas: ptr.To(int32(10)), TargetAverageUtilizationCPU: ptr.To(int32(60)), TargetAverageUtilizationMemory: ptr.To(int32(60)),
	}
	require.NoError(t, c.Update(ctx, cr))
	_, err = Apply(ctx, c, key, recorder)
	assert.ErrorContains(t, err, "not controlled by KVService")
	require.NoError(t, c.Get(ctx, key, cr))
	assert.Equal(t, enterpriseApi.PhaseError, cr.Status.Phase)
	ready := meta.FindStatusCondition(cr.Status.Conditions, string(enterpriseApi.ConditionReady))
	if assert.NotNil(t, ready) {
		assert.Equal(t, metav1.ConditionFalse, ready.Status)
	}
}

func TestKVServiceDisablesAutoscalingWhileServiceDatabaseIsUnready(t *testing.T) {
	// Existing unit-test fixture value; this test only uses a fake Kubernetes client.
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := t.Context()
	cr, database, c := postgresReadinessFixture(t)
	cr.Spec.Replicas = ptr.To(int32(4))
	cr.Spec.Autoscaling = &enterpriseApi.KVServiceAutoscalingSpec{
		MinReplicas: ptr.To(int32(2)), MaxReplicas: ptr.To(int32(10)), TargetAverageUtilizationCPU: ptr.To(int32(60)), TargetAverageUtilizationMemory: ptr.To(int32(60)),
	}
	require.NoError(t, c.Update(ctx, cr))
	key := types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name}
	recorder := events.NewFakeRecorder(100)
	_, err := Apply(ctx, c, key, recorder)
	assert.NoError(t, err)
	hpaKey := types.NamespacedName{Namespace: cr.Namespace, Name: "splunk-" + cr.Name + "-kvservice-hpa"}
	require.NoError(t, c.Get(ctx, hpaKey, &autoscalingv2.HorizontalPodAutoscaler{}))
	deploymentKey := types.NamespacedName{Namespace: cr.Namespace, Name: "splunk-" + cr.Name + "-kvservice"}
	deployment := &appsv1.Deployment{}
	require.NoError(t, c.Get(ctx, deploymentKey, deployment))
	before := deployment.DeepCopy()

	// Database readiness must still gate workload changes, but not HPA removal.
	database.Status.Phase = nil
	require.NoError(t, c.Status().Update(ctx, database))
	require.NoError(t, c.Get(ctx, key, cr))
	cr.Spec.Autoscaling = nil
	cr.Spec.Replicas = ptr.To(int32(3))
	require.NoError(t, c.Update(ctx, cr))
	result, err := Apply(ctx, c, key, recorder)
	assert.NoError(t, err)
	assert.True(t, apierrors.IsNotFound(c.Get(ctx, hpaKey, &autoscalingv2.HorizontalPodAutoscaler{})), "disabling autoscaling must delete the owned HPA while the service database is unready")
	assert.NotZero(t, result.RequeueAfter, "an unready service database must still be retried")
	require.NoError(t, c.Get(ctx, key, cr))
	assert.Equal(t, ptr.To(int32(3)), cr.Spec.Replicas, "cleanup must preserve the manually selected replica count")
	assert.Equal(t, enterpriseApi.PhasePending, cr.Status.Phase)
	ready := meta.FindStatusCondition(cr.Status.Conditions, string(enterpriseApi.ConditionReady))
	if assert.NotNil(t, ready) {
		assert.Equal(t, metav1.ConditionFalse, ready.Status)
		assert.Contains(t, ready.Message, "waiting for service database kvservicesystem")
	}
	require.NoError(t, c.Get(ctx, deploymentKey, deployment))
	assert.Equal(t, before.Spec, deployment.Spec, "an unready database must still block Deployment updates")
}

func TestKVServiceReadinessAndConfigurationFailure(t *testing.T) {
	// Existing unit-test fixture value; this test only uses a fake Kubernetes client.
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	cr, _, c := postgresReadinessFixture(t)
	ctx := t.Context()
	cr.Generation = 7
	cr.Spec.PodAnnotations = map[string]string{"sidecar.istio.io/inject": "false"}
	require.NoError(t, c.Update(ctx, cr))
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
	for _, secret := range secrets.Items {
		if secret.Namespace == cr.Namespace {
			assert.Contains(t, []string{"alpha-owner", "bravo-owner", "kvservicesystem-owner", splcommon.KVServiceTenantVerificationKeysName}, secret.Name)
		}
	}
	configMaps := &corev1.ConfigMapList{}
	require.NoError(t, c.List(ctx, configMaps))
	assert.Len(t, configMaps.Items, 4, "service and tenant connection maps plus membership are needed")
	for _, configMap := range configMaps.Items {
		assert.NotContains(t, configMap.Data, "default.yml", "KVService must not create Enterprise defaults")
	}
	assert.NotContains(t, deployment.Spec.Template.Annotations, "defaultConfigRev")
	assert.Equal(t, splcommon.KVServiceSystemDatabase, envValue(deployment.Spec.Template.Spec.Containers[0].Env, "KVSERVICE_CMPK_SERVICE_DATABASE"))
	assert.Empty(t, envValue(deployment.Spec.Template.Spec.Containers[0].Env, "SPLUNK_HOME"))
	assert.Empty(t, envValue(deployment.Spec.Template.Spec.Containers[0].Env, "SPLUNK_DEFAULTS_URL"))
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
	assert.NoError(t, err)
	require.NoError(t, c.Get(ctx, deploymentKey, deployment))
	assert.Equal(t, splcommon.KVServiceSystemDatabase, envValue(deployment.Spec.Template.Spec.Containers[0].Env, "KVSERVICE_CMPK_SERVICE_DATABASE"))
	assert.Empty(t, envValue(deployment.Spec.Template.Spec.Containers[0].Env, "SPLUNK_HOME"))
	assert.Empty(t, envValue(deployment.Spec.Template.Spec.Containers[0].Env, "SPLUNK_DEFAULTS_URL"))
	assert.Len(t, deployment.Spec.Template.Spec.Volumes, 4, "tenant map, verification keys, and both tenant password Secrets must be restored")
	assert.Len(t, deployment.Spec.Template.Spec.Containers[0].VolumeMounts, 4)
	for _, mount := range deployment.Spec.Template.Spec.Containers[0].VolumeMounts {
		assert.NotEqual(t, "/mnt/splunk-secrets", mount.MountPath)
	}
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
				t.Errorf("pause reconciliation failed: %v", err)
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

func TestKVServicePostgresPreservesTenantPendingMessage(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	cr, c := pendingPostgresFixture(t)
	ctx := t.Context()
	result, err := Apply(ctx, c, types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name}, events.NewFakeRecorder(10))
	assert.NoError(t, err)
	assert.NotZero(t, result.RequeueAfter, "KVService waiting for tenant databases should be retried")
	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{Namespace: cr.Namespace, Name: splcommon.GetNamespaceScopedSecretName(cr.Namespace)}
	assert.True(t, apierrors.IsNotFound(c.Get(ctx, secretKey, secret)), "multi-tenant KVService must not generate a shared Enterprise namespace Secret")
	for _, tenant := range []string{"alpha", "bravo"} {
		database := &platformApi.PostgresDatabase{}
		require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: tenant + "-database"}, database))
		assert.Equal(t, cr.Spec.Postgres.ClusterRef, database.Spec.ClusterRef)
	}
	current := &enterpriseApi.KVService{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name}, current))
	assert.Equal(t, enterpriseApi.PhasePending, current.Status.Phase)
	ready := meta.FindStatusCondition(current.Status.Conditions, string(enterpriseApi.ConditionReady))
	if assert.NotNil(t, ready) {
		assert.Equal(t, metav1.ConditionFalse, ready.Status)
		assert.Equal(t, "waiting for service database kvservicesystem and its connection credentials", ready.Message)
	}
	t.Run("PostgreSQL reconciliation lifecycle", testKVServicePostgresReconciliationLifecycle)
	t.Run("PostgreSQL with TLS", testKVServicePostgresTLSReconciliation)
}

func TestEnsurePostgresReadyWaitsForDeletingDatabase(t *testing.T) {
	cr, database, c := postgresReadinessFixture(t)
	database.Finalizers = []string{"test-finalizer"}
	require.NoError(t, c.Update(t.Context(), database))
	require.NoError(t, c.Delete(t.Context(), database))
	postgres, err := ensureTenantPostgresReady(t.Context(), c, cr)
	require.NoError(t, err)
	assert.False(t, postgres.ready)
	assert.Nil(t, postgres.dependencies)
	assert.Contains(t, postgres.message, "waiting for service database kvservicesystem")
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
		assert.NoError(t, config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.CertManagement): previous}))
	})
	cr, _, c := postgresReadinessFixture(t)
	ctx := t.Context()
	cr.Spec.Scheme = "https"
	cr.Spec.Certs = []enterpriseApi.CertSpec{{SecretRef: corev1.LocalObjectReference{Name: "customer-tls"}}}
	require.NoError(t, c.Update(ctx, cr))
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "customer-tls", Namespace: cr.Namespace},
		Data:       servingCertificateData(t, cr.Name, cr.Namespace),
	}
	require.NoError(t, c.Create(ctx, secret))
	key := types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}
	_, err := Apply(ctx, c, key, nil)
	assert.NoError(t, err)
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
	assert.NotEmpty(t, certVolume, "certificate Secret must be mounted")
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
	if assert.Len(t, requests, 1, "certificate Secret updates must enqueue KVService") {
		assert.Equal(t, key, requests[0].NamespacedName)
	}

	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}, secret))
	secret.Data = servingCertificateData(t, cr.Name, cr.Namespace)
	require.NoError(t, c.Update(ctx, secret))
	_, err = Apply(ctx, c, key, nil)
	assert.NoError(t, err)
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
		{name: "malformed PEM", missingKey: "matching PEM", data: map[string][]byte{"tls.crt": []byte("invalid-certificate"), "tls.key": []byte("invalid-key")}},
		{name: "mismatched pair", missingKey: "matching PEM", data: map[string][]byte{"tls.crt": secret.Data["tls.crt"], "tls.key": servingCertificateData(t, cr.Name, cr.Namespace)["tls.key"]}},
		{name: "expired", missingKey: "not currently valid", data: servingCertificateData(t, cr.Name, cr.Namespace, func(c *x509.Certificate) { c.NotAfter = time.Now().Add(-time.Minute) })},
		{name: "not yet valid", missingKey: "not currently valid", data: servingCertificateData(t, cr.Name, cr.Namespace, func(c *x509.Certificate) { c.NotBefore = time.Now().Add(time.Hour) })},
		{name: "full DNS only", missingKey: "must cover hostname", data: servingCertificateData(t, cr.Name, cr.Namespace, func(c *x509.Certificate) {
			c.DNSNames = []string{splcommon.GetServiceFQDN(cr.Namespace, splcommon.GetSplunkServiceName(splcommon.SplunkKVService, cr.Name, false))}
		})},
		{name: "client only", missingKey: "server authentication", data: servingCertificateData(t, cr.Name, cr.Namespace, func(c *x509.Certificate) { c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth} })},
		{name: "unknown usage only", missingKey: "server authentication", data: servingCertificateData(t, cr.Name, cr.Namespace, func(c *x509.Certificate) {
			c.ExtKeyUsage = nil
			c.UnknownExtKeyUsage = []asn1.ObjectIdentifier{{1, 3, 6, 1, 4, 1, 55555, 1}}
		})},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			secret.Data = invalid.data
			require.NoError(t, c.Update(ctx, secret))
			_, err = Apply(ctx, c, key, nil)
			assert.ErrorContains(t, err, invalid.missingKey)
			require.NoError(t, c.Get(ctx, deploymentKey, deployment))
			assert.Equal(t, before, &deployment.Spec.Template, "invalid certificates must preserve the working Deployment")
		})
	}

	// A corrected Secret recovers, and switching to HTTP removes the TLS wiring.
	secret.Data = servingCertificateData(t, cr.Name, cr.Namespace)
	require.NoError(t, c.Update(ctx, secret))
	_, err = Apply(ctx, c, key, nil)
	require.NoError(t, err)
	require.NoError(t, c.Get(ctx, key, cr))
	cr.Spec.Scheme = "http"
	require.NoError(t, c.Update(ctx, cr))
	_, err = Apply(ctx, c, key, nil)
	require.NoError(t, err)
	require.NoError(t, c.Get(ctx, deploymentKey, deployment))
	assert.Empty(t, envValue(deployment.Spec.Template.Spec.Containers[0].Env, "TLS_CERTIFICATE"))
	for _, volume := range deployment.Spec.Template.Spec.Volumes {
		if volume.Secret != nil {
			assert.NotEqual(t, secret.Name, volume.Secret.SecretName)
		}
	}
}

func TestKVServiceHTTPIgnoresCertificatesOnEveryReconcile(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.CertManagement, false)
	cr, _, c := postgresReadinessFixture(t)
	// An absent certificate Secret must neither block HTTP nor be mounted.
	cr.Spec.Scheme = "http"
	cr.Spec.Certs = []enterpriseApi.CertSpec{{SecretRef: corev1.LocalObjectReference{Name: "absent"}}}
	require.NoError(t, c.Update(t.Context(), cr))
	var logs bytes.Buffer
	ctx := logging.WithLogger(t.Context(), slog.New(slog.NewTextHandler(&logs, nil)))
	for range 2 {
		_, err := Apply(ctx, c, client.ObjectKeyFromObject(cr), nil)
		require.NoError(t, err)
	}
	assert.Equal(t, 2, strings.Count(logs.String(), "spec.certs is ignored"))
	deployment := &appsv1.Deployment{}
	key := types.NamespacedName{Namespace: cr.Namespace, Name: splutil.GetSplunkDeploymentName(splcommon.SplunkKVService, cr.Name)}
	require.NoError(t, c.Get(ctx, key, deployment))
	assert.Empty(t, envValue(deployment.Spec.Template.Spec.Containers[0].Env, "TLS_CERTIFICATE"))
	for _, volume := range deployment.Spec.Template.Spec.Volumes {
		if volume.Secret != nil {
			assert.NotEqual(t, "absent", volume.Secret.SecretName)
		}
	}
}
