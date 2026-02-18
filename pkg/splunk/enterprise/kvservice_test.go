// Copyright (c) 2024 Splunk Inc. All rights reserved.
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

package enterprise

import (
	"context"
	"os"
	"testing"

	"github.com/pkg/errors"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestApplyKVService(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Service-test-splunk-stack1-kvservice-service"},
		{MetaName: "*v1.Deployment-test-splunk-stack1-kvservice"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-kvservice-secret-v1"},
		{MetaName: "*v1.Deployment-test-splunk-stack1-kvservice"},
		{MetaName: "*v4.KVService-test-stack1"},
		{MetaName: "*v4.KVService-test-stack1"},
	}
	updateFuncCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Service-test-splunk-stack1-kvservice-service"},
		{MetaName: "*v1.Deployment-test-splunk-stack1-kvservice"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-kvservice-secret-v1"},
		{MetaName: "*v1.Deployment-test-splunk-stack1-kvservice"},
		{MetaName: "*v4.KVService-test-stack1"},
		{MetaName: "*v4.KVService-test-stack1"},
	}

	labels := map[string]string{
		"app.kubernetes.io/component":  "versionedSecrets",
		"app.kubernetes.io/managed-by": "splunk-operator",
	}
	listOpts := []client.ListOption{
		client.InNamespace("test"),
		client.MatchingLabels(labels),
	}
	listmockCall := []spltest.MockFuncCall{
		{ListOpts: listOpts},
	}

	createCalls := map[string][]spltest.MockFuncCall{
		"Get":    funcCalls,
		"Create": {funcCalls[0], funcCalls[2], funcCalls[4], funcCalls[1]},
		"List":   {listmockCall[0]},
	}
	updateCalls := map[string][]spltest.MockFuncCall{
		"Get":    updateFuncCalls,
		"Update": {updateFuncCalls[4]},
		"List":   {listmockCall[0]},
	}

	current := enterpriseApi.KVService{
		TypeMeta: metav1.TypeMeta{
			Kind: "KVService",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	revised := current.DeepCopy()
	revised.Spec.Replicas = 2

	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		recorder := record.NewFakeRecorder(16)
		_, err := ApplyKVService(context.Background(), c, recorder, cr.(*enterpriseApi.KVService))
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyKVService", &current, revised, createCalls, updateCalls, reconcile, true)

	c := spltest.NewMockClient()
	ctx := context.TODO()
	recorder := record.NewFakeRecorder(16)
	kvGvk := schema.GroupVersionKind{
		Group:   "enterprise.splunk.com",
		Version: "v4",
		Kind:    "KVService",
	}
	current.SetGroupVersionKind(kvGvk)
	current.Spec.LivenessInitialDelaySeconds = -1
	_, err := ApplyKVService(ctx, c, recorder, &current)
	if err == nil {
		t.Errorf("Expected error")
	}

	rerr := errors.New(splcommon.Rerr)
	current.Spec.LivenessInitialDelaySeconds = 5
	c.InduceErrorKind[splcommon.MockClientInduceErrorGet] = rerr
	_, err = ApplyKVService(ctx, c, recorder, &current)
	if err == nil {
		t.Errorf("Expected error")
	}
}

func TestGetStandardK8sServiceForKVServiceDeployment(t *testing.T) {
	cr := &enterpriseApi.KVService{
		TypeMeta: metav1.TypeMeta{
			Kind: "KVService",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
			Labels: map[string]string{
				"parent": "true",
			},
			Annotations: map[string]string{
				"note": "from-parent",
			},
		},
	}
	cr.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "enterprise.splunk.com",
		Version: "v4",
		Kind:    "KVService",
	})

	spec := &enterpriseApi.KVServiceSpec{
		Spec: enterpriseApi.Spec{
			ServiceTemplate: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"existing": "label",
					},
					Annotations: map[string]string{
						"existing-annotation": "true",
					},
				},
			},
		},
	}

	service := getStandardK8sServiceForKVServiceDeployment(context.Background(), cr, spec, SplunkKVService)
	if service.Name != GetSplunkServiceName(SplunkKVService, cr.GetName(), false) {
		t.Errorf("service name=%s; want %s", service.Name, GetSplunkServiceName(SplunkKVService, cr.GetName(), false))
	}
	if service.Namespace != cr.GetNamespace() {
		t.Errorf("service namespace=%s; want %s", service.Namespace, cr.GetNamespace())
	}
	if service.Spec.Selector["app.kubernetes.io/instance"] != "splunk-stack1-kvservice" {
		t.Errorf("service selector instance=%s; want %s", service.Spec.Selector["app.kubernetes.io/instance"], "splunk-stack1-kvservice")
	}
	if service.Labels["parent"] != "true" {
		t.Errorf("service label parent missing")
	}
	if service.Labels["existing"] != "label" {
		t.Errorf("service label existing missing")
	}
	if service.Annotations["note"] != "from-parent" {
		t.Errorf("service annotation note missing")
	}
	if service.Annotations["existing-annotation"] != "true" {
		t.Errorf("service annotation existing-annotation missing")
	}
	if len(service.OwnerReferences) != 1 {
		t.Errorf("owner references=%d; want 1", len(service.OwnerReferences))
	}
	foundPort := false
	for _, port := range service.Spec.Ports {
		if port.Port == 8066 {
			foundPort = true
			break
		}
	}
	if !foundPort {
		t.Errorf("service port 8066 not found")
	}
}

func TestGetSplunkKVServiceImage(t *testing.T) {
	specImage := ""
	t.Setenv("RELATED_IMAGE_SPLUNK_KVSERVICE", "")

	test := func(want string) {
		got := GetSplunkKVServiceImage(specImage)
		if got != want {
			t.Errorf("GetSplunkKVServiceImage() = %s; want %s", got, want)
		}
	}

	test(defaultKVServiceImage)

	t.Setenv("RELATED_IMAGE_SPLUNK_KVSERVICE", "kvstore/kvservice:env")
	test("kvstore/kvservice:env")

	specImage = "kvstore/kvservice:spec"
	test("kvstore/kvservice:spec")
}

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

	deployment, err := getKVServiceDeployment(ctx, c, cr)
	if err != nil {
		t.Fatalf("getKVServiceDeployment error=%v", err)
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
	if container.LivenessProbe.HTTPGet.Port.IntValue() != defaultKVServicePort {
		t.Errorf("liveness port=%d; want %d", container.LivenessProbe.HTTPGet.Port.IntValue(), defaultKVServicePort)
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
	if container.ReadinessProbe.HTTPGet.Port.IntValue() != defaultKVServicePort {
		t.Errorf("readiness port=%d; want %d", container.ReadinessProbe.HTTPGet.Port.IntValue(), defaultKVServicePort)
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

	if len(deployment.OwnerReferences) == 0 {
		t.Errorf("deployment owner references not set")
	}
}

func TestValidateKVServiceSpec(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	c := spltest.NewMockClient()
	cr := &enterpriseApi.KVService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	err := validateKVServiceSpec(ctx, c, cr)
	if err != nil {
		t.Fatalf("validateKVServiceSpec error=%v", err)
	}
	if cr.Spec.Replicas != 1 {
		t.Errorf("replicas=%d; want 1", cr.Spec.Replicas)
	}
	if cr.Spec.Image != defaultKVServiceImage {
		t.Errorf("image=%s; want %s", cr.Spec.Image, defaultKVServiceImage)
	}
	if cr.Spec.Volumes == nil {
		t.Errorf("volumes should be non-nil")
	}
	if len(cr.Spec.ImagePullSecrets) != 0 {
		t.Errorf("imagePullSecrets len=%d; want 0", len(cr.Spec.ImagePullSecrets))
	}

	invalid := &enterpriseApi.KVService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack2",
			Namespace: "test",
		},
	}
	invalid.Spec.LivenessInitialDelaySeconds = -1
	err = validateKVServiceSpec(ctx, c, invalid)
	if err == nil {
		t.Errorf("Expected error")
	}
}

func TestSetKVServiceVolumeDefaults(t *testing.T) {
	spec := &enterpriseApi.KVServiceSpec{}
	setKVServiceVolumeDefaults(spec)
	if spec.Volumes == nil {
		t.Errorf("volumes should be non-nil")
	}

	secretVolume := corev1.Volume{
		Name: "secret",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{},
		},
	}
	configMapVolume := corev1.Volume{
		Name: "config",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{},
		},
	}
	spec.Volumes = []corev1.Volume{secretVolume, configMapVolume}
	setKVServiceVolumeDefaults(spec)

	if spec.Volumes[0].Secret.DefaultMode == nil || *spec.Volumes[0].Secret.DefaultMode != int32(corev1.SecretVolumeSourceDefaultMode) {
		t.Errorf("secret default mode not set")
	}
	if spec.Volumes[1].ConfigMap.DefaultMode == nil || *spec.Volumes[1].ConfigMap.DefaultMode != int32(corev1.ConfigMapVolumeSourceDefaultMode) {
		t.Errorf("configmap default mode not set")
	}
}

func TestValidateKVServiceImagePullSecrets(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()
	cr := &enterpriseApi.KVService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	spec := &enterpriseApi.KVServiceSpec{}
	err := ValidateKVServiceImagePullSecrets(ctx, c, cr, spec)
	if err != nil {
		t.Fatalf("ValidateKVServiceImagePullSecrets error=%v", err)
	}
	if len(spec.ImagePullSecrets) != 0 {
		t.Errorf("imagePullSecrets len=%d; want 0", len(spec.ImagePullSecrets))
	}

	pullSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pull-secret",
			Namespace: "test",
		},
	}
	c.AddObject(pullSecret)
	spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "pull-secret"}}
	err = ValidateKVServiceImagePullSecrets(ctx, c, cr, spec)
	if err != nil {
		t.Fatalf("ValidateKVServiceImagePullSecrets error=%v", err)
	}
	if spec.ImagePullSecrets[0].Name != "pull-secret" {
		t.Errorf("imagePullSecrets name=%s; want %s", spec.ImagePullSecrets[0].Name, "pull-secret")
	}
}

func TestGetKVServiceList(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()
	cr := &enterpriseApi.KVService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	listOpts := []client.ListOption{
		client.InNamespace("test"),
	}
	c.ListObj = &enterpriseApi.KVServiceList{
		Items: []enterpriseApi.KVService{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kv1",
					Namespace: "test",
				},
			},
		},
	}
	list, err := getKVServiceList(ctx, c, cr, listOpts)
	if err != nil {
		t.Fatalf("getKVServiceList error=%v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Name != "kv1" {
		t.Errorf("list items=%v; want kv1", list.Items)
	}

	c.InduceErrorKind[splcommon.MockClientInduceErrorList] = errors.New("list error")
	_, err = getKVServiceList(ctx, c, cr, listOpts)
	if err == nil {
		t.Errorf("Expected error")
	}
}
