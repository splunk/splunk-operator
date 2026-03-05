// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package enterprise

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func init() {
	GetReadinessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../" + readinessScriptLocation)
		return fileLocation
	}
	GetLivenessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../" + livenessScriptLocation)
		return fileLocation
	}
	GetStartupScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../" + startupScriptLocation)
		return fileLocation
	}
}

func TestApplyIngestorCluster(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	ctx := context.TODO()

	scheme := runtime.NewScheme()
	_ = enterpriseApi.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Object definitions
	queue := &enterpriseApi.Queue{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Queue",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "queue",
			Namespace: "test",
		},
		Spec: enterpriseApi.QueueSpec{
			Provider: "sqs",
			SQS: enterpriseApi.SQSSpec{
				Name:       "test-queue",
				AuthRegion: "us-west-2",
				Endpoint:   "https://sqs.us-west-2.amazonaws.com",
				DLQ:        "sqs-dlq-test",
			},
		},
	}
	c.Create(ctx, queue)

	os := &enterpriseApi.ObjectStorage{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ObjectStorage",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "os",
			Namespace: "test",
		},
		Spec: enterpriseApi.ObjectStorageSpec{
			Provider: "s3",
			S3: enterpriseApi.S3Spec{
				Endpoint: "https://s3.us-west-2.amazonaws.com",
				Path:     "bucket/key",
			},
		},
	}
	c.Create(ctx, os)

	cr := &enterpriseApi.IngestorCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "IngestorCluster",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: enterpriseApi.IngestorClusterSpec{
			Replicas: 3,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock:           true,
				ServiceAccount: "sa",
			},
			QueueRef: corev1.ObjectReference{
				Name:      queue.Name,
				Namespace: queue.Namespace,
			},
			ObjectStorageRef: corev1.ObjectReference{
				Name:      os.Name,
				Namespace: os.Namespace,
			},
		},
	}
	c.Create(ctx, cr)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{"password": []byte("dummy")},
	}
	c.Create(ctx, secret)

	probeConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-probe-configmap",
			Namespace: "test",
		},
	}
	c.Create(ctx, probeConfigMap)

	replicas := int32(3)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-ingestor",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/instance": "splunk-test-ingestor",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/instance": "splunk-test-ingestor",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "splunk-test-ingestor",
							Image: "splunk/splunk:latest",
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 8080,
								},
							},
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:        replicas,
			ReadyReplicas:   replicas,
			UpdatedReplicas: replicas,
			CurrentRevision: "v1",
			UpdateRevision:  "v1",
		},
	}
	c.Create(ctx, sts)

	pod0 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-ingestor-0",
			Namespace: "test",
			Labels: map[string]string{
				"app.kubernetes.io/instance": "splunk-test-ingestor",
				"controller-revision-hash":   "v1",
			},
		},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: "dummy-volume",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "mnt-splunk-secrets",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "test-secrets",
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Ready: true},
			},
		},
	}

	pod1 := pod0.DeepCopy()
	pod1.ObjectMeta.Name = "splunk-test-ingestor-1"

	pod2 := pod0.DeepCopy()
	pod2.ObjectMeta.Name = "splunk-test-ingestor-2"

	c.Create(ctx, pod0)
	c.Create(ctx, pod1)
	c.Create(ctx, pod2)

	// ApplyIngestorCluster
	cr.Spec.Replicas = replicas
	cr.Status.ReadyReplicas = cr.Spec.Replicas

	result, err := ApplyIngestorCluster(ctx, c, cr)
	assert.NoError(t, err)
	assert.True(t, result.Requeue)
	assert.NotEqual(t, enterpriseApi.PhaseError, cr.Status.Phase)

	// Second reconcile with telemetry already installed should yield Ready
	cr.Status.TelAppInstalled = true
	result, err = ApplyIngestorCluster(ctx, c, cr)
	assert.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseReady, cr.Status.Phase)
}

func TestGetIngestorStatefulSet(t *testing.T) {
	// Object definitions
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	queue := enterpriseApi.Queue{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Queue",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "queue",
		},
		Spec: enterpriseApi.QueueSpec{
			Provider: "sqs",
			SQS: enterpriseApi.SQSSpec{
				Name:       "test-queue",
				AuthRegion: "us-west-2",
				Endpoint:   "https://sqs.us-west-2.amazonaws.com",
				DLQ:        "sqs-dlq-test",
			},
		},
	}

	cr := enterpriseApi.IngestorCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IngestorCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: enterpriseApi.IngestorClusterSpec{
			Replicas: 0,
			QueueRef: corev1.ObjectReference{
				Name: queue.Name,
			},
		},
	}

	ctx := context.TODO()

	c := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Errorf("Failed to create namespace scoped object")
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := validateIngestorClusterSpec(ctx, c, &cr); err != nil {
				t.Errorf("validateIngestorClusterSpec() returned error: %v", err)
			}
			return getIngestorStatefulSet(ctx, c, &cr)
		}
		// Use configTester2 (no space-stripping) because the init container command string
		// contains meaningful spaces that must be preserved in comparison.
		configTester2(t, "getIngestorStatefulSet()", f, want)
	}

	// Define additional service port in CR and verify the statefulset has the new port
	cr.Spec.ServiceTemplate.Spec.Ports = []corev1.ServicePort{{Name: "user-defined", Port: 32000, Protocol: "UDP"}}
	test(loadFixture(t, "statefulset_ingestor.json"))

	// Create a service account
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	test(loadFixture(t, "statefulset_ingestor_with_serviceaccount.json"))

	// Add extraEnv
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	test(loadFixture(t, "statefulset_ingestor_with_extraenv.json"))

	// Add additional label to cr metadata to transfer to the statefulset
	cr.ObjectMeta.Labels = make(map[string]string)
	cr.ObjectMeta.Labels["app.kubernetes.io/test-extra-label"] = "test-extra-label-value"
	test(loadFixture(t, "statefulset_ingestor_with_labels.json"))
}

func TestComputeIngestorConfChecksum(t *testing.T) {
	checksum := computeIngestorConfChecksum("outputs", "defaultmode")
	// SHA-256 produces 64 hex chars
	if len(checksum) != 64 {
		t.Errorf("expected 64-char hex, got %d: %s", len(checksum), checksum)
	}

	// Deterministic
	if computeIngestorConfChecksum("outputs", "defaultmode") != checksum {
		t.Error("checksum is not deterministic")
	}

	// Sensitive to content change
	if computeIngestorConfChecksum("outputs2", "defaultmode") == checksum {
		t.Error("checksum did not change when outputs changed")
	}
}

func TestGenerateIngestorOutputsConf(t *testing.T) {
	queue := enterpriseApi.QueueSpec{
		Provider: "sqs",
		SQS: enterpriseApi.SQSSpec{
			Name:       "test-queue",
			AuthRegion: "us-west-2",
			Endpoint:   "https://sqs.us-west-2.amazonaws.com",
			DLQ:        "dlq",
		},
	}
	os := enterpriseApi.ObjectStorageSpec{
		Provider: "s3",
		S3: enterpriseApi.S3Spec{
			Endpoint: "https://s3.amazonaws.com",
			Path:     "bucket/key",
		},
	}

	// IRSA: no credentials embedded
	conf := generateIngestorOutputsConf(&queue, &os, "", "")
	assert.Contains(t, conf, "[remote_queue:test-queue]")
	assert.NotContains(t, conf, "access_key")

	// Static creds: credentials embedded
	conf = generateIngestorOutputsConf(&queue, &os, "AKID", "secret")
	assert.Contains(t, conf, "access_key")
}

func TestGenerateIngestorDefaultModeConf(t *testing.T) {
	conf := generateIngestorDefaultModeConf()
	for _, stanza := range []string{
		"pipeline:remotequeueruleset",
		"pipeline:ruleset",
		"pipeline:remotequeuetyping",
		"pipeline:remotequeueoutput",
		"pipeline:typing",
		"pipeline:indexerPipe",
	} {
		assert.Contains(t, conf, stanza)
	}
}

func TestGenerateIngestorAppConf(t *testing.T) {
	conf := generateQueueConfigAppConf("Splunk Operator Ingestor Queue Config")
	assert.Contains(t, conf, "[install]")
	assert.Contains(t, conf, "state = enabled")
	assert.Contains(t, conf, "[package]")
	assert.Contains(t, conf, "[ui]")
}

func TestGenerateIngestorLocalMeta(t *testing.T) {
	conf := generateQueueConfigLocalMeta()
	// install_source_checksum is not a valid local.meta field; it was removed to prevent parse errors.
	assert.NotContains(t, conf, "install_source_checksum")
	assert.Contains(t, conf, "export = system")
	assert.Contains(t, conf, "access = read : [ * ], write : [ admin ]")
}
