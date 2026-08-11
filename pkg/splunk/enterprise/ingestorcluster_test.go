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
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/splkcontroller"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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

func TestApplyIngestorClusterTerminalFailures(t *testing.T) {
	ctx := context.TODO()

	// Case 1: spec validation failure (empty queueRef.name) is a terminal failure.
	t.Run("empty queueRef is terminal", func(t *testing.T) {
		os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

		scheme := runtime.NewScheme()
		_ = enterpriseApi.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		_ = appsv1.AddToScheme(scheme)
		c := newFakeClientBuilder(scheme).Build()

		cr := &enterpriseApi.IngestorCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
			Spec: enterpriseApi.IngestorClusterSpec{
				Replicas:         1,
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Mock: true},
				ObjectStorageRef: corev1.ObjectReference{Name: "os"},
				// QueueRef.Name intentionally empty → validation fails
			},
		}

		_, err := ApplyIngestorCluster(ctx, c, cr)
		assert.True(t, errors.Is(err, reconcile.TerminalError(nil)), "expected TerminalError, got %v", err)
	})

	// Case 2: SPLUNK_GENERAL_TERMS unset is a terminal failure.
	t.Run("SPLUNK_GENERAL_TERMS unset is terminal", func(t *testing.T) {
		os.Unsetenv("SPLUNK_GENERAL_TERMS")
		defer os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

		scheme := runtime.NewScheme()
		_ = enterpriseApi.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		_ = appsv1.AddToScheme(scheme)
		c := newFakeClientBuilder(scheme).Build()

		cr := &enterpriseApi.IngestorCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
			Spec: enterpriseApi.IngestorClusterSpec{
				Replicas:         1,
				QueueRef:         corev1.ObjectReference{Name: "queue"},
				ObjectStorageRef: corev1.ObjectReference{Name: "os"},
			},
		}

		_, err := ApplyIngestorCluster(ctx, c, cr)
		assert.True(t, errors.Is(err, reconcile.TerminalError(nil)), "expected TerminalError, got %v", err)
	})

	// Case 3: Queue CR not found is a terminal failure.
	// ensureIngestorDefaults runs unconditionally on every reconcile, so a single
	// call is sufficient — no need to reach PhaseReady first.
	t.Run("Queue CR not found is terminal", func(t *testing.T) {
		os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

		scheme := runtime.NewScheme()
		_ = enterpriseApi.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		_ = appsv1.AddToScheme(scheme)
		_ = policyv1.AddToScheme(scheme)
		c := newFakeClientBuilder(scheme).Build()

		// Create the Queue CR so only the ObjectStorage CR is missing.
		_ = c.Create(ctx, &enterpriseApi.Queue{
			ObjectMeta: metav1.ObjectMeta{Name: "queue", Namespace: "test"},
			Spec: enterpriseApi.QueueSpec{
				Provider: "sqs",
				SQS: enterpriseApi.SQSSpec{
					Name: "test-queue", AuthRegion: "us-west-2",
					Endpoint: "https://sqs.us-west-2.amazonaws.com", DLQ: "dlq",
				},
			},
		})

		cr := &enterpriseApi.IngestorCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
			Spec: enterpriseApi.IngestorClusterSpec{
				Replicas:         1,
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Mock: true},
				QueueRef:         corev1.ObjectReference{Name: "queue", Namespace: "test"},
				ObjectStorageRef: corev1.ObjectReference{Name: "nonexistent-os", Namespace: "test"},
			},
		}

		_, err := ApplyIngestorCluster(ctx, c, cr)
		assert.True(t, errors.Is(err, reconcile.TerminalError(nil)), "expected TerminalError, got %v", err)
	})
}

func TestApplyIngestorCluster(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	ctx := context.TODO()

	scheme := pkgruntime.NewScheme()
	_ = enterpriseApi.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = policyv1.AddToScheme(scheme)
	c := newFakeClientBuilder(scheme).Build()

	queue := &enterpriseApi.Queue{
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

	objStorage := &enterpriseApi.ObjectStorage{
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
	c.Create(ctx, objStorage)

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
				Name:      objStorage.Name,
				Namespace: objStorage.Namespace,
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

	// ApplyIngestorCluster — declarative path: config is delivered via ConfigMap/Secret,
	// no HTTP push or restart. The reconcile settles to Ready without a PhaseUpdating flip.
	cr.Spec.Replicas = replicas
	cr.Status.ReadyReplicas = cr.Spec.Replicas
	cr.Status.TelAppInstalled = true

	result, err := ApplyIngestorCluster(ctx, c, cr)
	assert.NoError(t, err)
	assert.NotZero(t, result.RequeueAfter)
	assert.NotEqual(t, enterpriseApi.PhaseError, cr.Status.Phase)
	// No QueueConfigUpdated / IngestorsRestarted events expected: config is declarative.
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
			ObjectStorageRef: corev1.ObjectReference{
				Name: "objectstorage",
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
		configTester(t, "getIngestorStatefulSet()", f, want)
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

// TestGetIngestorStatefulSet_ConfigMapVolAnnotation reproduces CSPL-4611 CI failure locally:
// a user-supplied ConfigMap volume in spec.Volumes should stamp a
// splcommon.ConfigMapRevAnnotationPrefix+<vol-name> annotation on the IngestorCluster pod
// template, mirroring TestConfigMapVolAnnotationStamped for Standalone.
func TestGetIngestorStatefulSet_ConfigMapVolAnnotation(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	cr := enterpriseApi.IngestorCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IngestorCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: enterpriseApi.IngestorClusterSpec{
			Replicas: 1,
			QueueRef: corev1.ObjectReference{
				Name: "queue",
			},
			ObjectStorageRef: corev1.ObjectReference{
				Name: "objectstorage",
			},
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Volumes: []corev1.Volume{
					{
						Name: "my-defaults",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "my-defaults-cm",
								},
							},
						},
					},
				},
			},
		},
	}

	c := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	require.NoError(t, err)

	cmData := map[string]string{"default.yml": "splunk:\n  conf: value1"}
	cm := splctrl.PrepareConfigMap("my-defaults-cm", "test", cmData)
	require.NoError(t, splutil.CreateResource(ctx, c, cm))

	require.NoError(t, validateIngestorClusterSpec(ctx, c, &cr))

	ss, err := getIngestorStatefulSet(ctx, c, &cr)
	require.NoError(t, err)

	annotations := ss.Spec.Template.ObjectMeta.Annotations
	annotationKey := splcommon.ConfigMapRevAnnotationPrefix + "my-defaults"
	hash, ok := annotations[annotationKey]
	if !ok {
		t.Fatalf("expected annotation %q on IngestorCluster pod template, got annotations: %v", annotationKey, annotations)
	}
	if hash == "" {
		t.Errorf("expected annotation %q to be non-empty, got empty string", annotationKey)
	}
}

// newIngestorQueueOSFixture creates a Queue, ObjectStorage, and the referenced credentials
// Secret in the fake client, returning them for use by the reconciler tests.
func newIngestorQueueOSFixture(t *testing.T, ctx context.Context, c client.Client, queueName, credsSecretName string) (*enterpriseApi.Queue, *enterpriseApi.ObjectStorage) {
	t.Helper()

	credsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: credsSecretName, Namespace: "test"},
		Data: map[string][]byte{
			"s3_access_key": []byte("AKIAEXAMPLE"),
			"s3_secret_key": []byte("shhh-secret"),
		},
	}
	require.NoError(t, c.Create(ctx, credsSecret))

	queue := &enterpriseApi.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: queueName, Namespace: "test"},
		Spec: enterpriseApi.QueueSpec{
			Provider: "sqs",
			SQS: enterpriseApi.SQSSpec{
				Name:       "test-queue",
				AuthRegion: "us-west-2",
				Endpoint:   "https://sqs.us-west-2.amazonaws.com",
				DLQ:        "sqs-dlq-test",
				SecretKeyRef: &enterpriseApi.SQSSecretKeyRef{
					AwsAccessKey: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: credsSecretName}, Key: "s3_access_key"},
					AwsSecretKey: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: credsSecretName}, Key: "s3_secret_key"},
				},
			},
		},
	}
	require.NoError(t, c.Create(ctx, queue))

	os := &enterpriseApi.ObjectStorage{
		ObjectMeta: metav1.ObjectMeta{Name: "os", Namespace: "test"},
		Spec: enterpriseApi.ObjectStorageSpec{
			Provider: "s3",
			S3: enterpriseApi.S3Spec{
				Endpoint: "https://s3.us-west-2.amazonaws.com",
				Path:     "bucket/key",
			},
		},
	}
	require.NoError(t, c.Create(ctx, os))

	return queue, os
}

// listIngestorCredsSecrets returns the SOK credentials Secrets owned by the given IngestorCluster.
func listIngestorCredsConfigMaps(t *testing.T, ctx context.Context, c client.Client, crName string) []corev1.ConfigMap {
	t.Helper()
	var all corev1.ConfigMapList
	require.NoError(t, c.List(ctx, &all, client.InNamespace("test")))
	var owned []corev1.ConfigMap
	for _, cm := range all.Items {
		if cm.Labels[resources.LabelCRKind] == "IngestorCluster" &&
			cm.Labels[resources.LabelCRName] == crName {
			owned = append(owned, cm)
		}
	}
	return owned
}

func listIngestorCredsSecrets(t *testing.T, ctx context.Context, c client.Client, crName string) []corev1.Secret {
	t.Helper()
	var all corev1.SecretList
	require.NoError(t, c.List(ctx, &all, client.InNamespace("test")))
	var owned []corev1.Secret
	for _, s := range all.Items {
		if s.Labels[resources.LabelCRKind] == "IngestorCluster" &&
			s.Labels[resources.LabelCRName] == crName {
			owned = append(owned, s)
		}
	}
	return owned
}

// TestEnsureIngestorCredentialsSecret_CreatesMountsAndRotates exercises the declarative
// credentials path directly: a queueRef with a static credentials secret yields a
// content-addressed Secret that mounts into the StatefulSet and is joined into
// SPLUNK_DEFAULTS_URL; rotating the source credentials yields a new Secret name.
func TestEnsureIngestorCredentialsSecret_CreatesMountsAndRotates(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(appsv1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))
	c := newFakeClientBuilder(sch).Build()

	queue, objStorage := newIngestorQueueOSFixture(t, ctx, c, "queue", "queue-secrets")

	cr := &enterpriseApi.IngestorCluster{
		// Kind mirrors the reconciler, which sets cr.Kind before calling
		// ensureIngestorDefaults; the defaults resource names embed it.
		TypeMeta:   metav1.TypeMeta{Kind: "IngestorCluster"},
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
		Spec: enterpriseApi.IngestorClusterSpec{
			Replicas:         1,
			QueueRef:         corev1.ObjectReference{Name: queue.Name, Namespace: queue.Namespace},
			ObjectStorageRef: corev1.ObjectReference{Name: objStorage.Name, Namespace: objStorage.Namespace},
		},
	}

	// A queueRef with static credentials produces a non-empty, content-addressed Secret.
	_, credsSecret, err := ensureIngestorDefaults(ctx, c, cr)
	require.NoError(t, err)
	require.NotEmpty(t, credsSecret.Name, "credentials Secret should be created when static creds are present")
	assert.Regexp(t, regexp.MustCompile(`^sok-ingestorcluster-creds-[0-9a-f]{6}$`), credsSecret.Name)

	var stored corev1.Secret
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: "test", Name: credsSecret.Name}, &stored))
	require.NotNil(t, stored.Immutable)
	assert.True(t, *stored.Immutable, "credentials Secret must be immutable")

	// The Secret mounts into the pod and joins SPLUNK_DEFAULTS_URL.
	ss := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "splunk"}}},
			},
		},
	}
	credsSecret.AsStatefulSetOption()(ss)
	require.Len(t, ss.Spec.Template.Spec.Volumes, 1)
	require.NotNil(t, ss.Spec.Template.Spec.Volumes[0].Secret)
	assert.Equal(t, credsSecret.Name, ss.Spec.Template.Spec.Volumes[0].Secret.SecretName)
	require.Len(t, ss.Spec.Template.Spec.Containers[0].VolumeMounts, 1)

	var defaultsURL string
	for _, e := range ss.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "SPLUNK_DEFAULTS_URL" {
			defaultsURL = e.Value
		}
	}
	assert.Contains(t, defaultsURL, resources.SecretMountPath(), "creds mount path must be joined into SPLUNK_DEFAULTS_URL")

	// Rotating the source credentials produces a different Secret name (rolls pods).
	rotated := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: "test", Name: "queue-secrets"}, rotated))
	rotated.Data["s3_secret_key"] = []byte("rotated-secret")
	require.NoError(t, c.Update(ctx, rotated))

	_, rotatedSecret, err := ensureIngestorDefaults(ctx, c, cr)
	require.NoError(t, err)
	assert.NotEqual(t, credsSecret.Name, rotatedSecret.Name, "rotated credentials must produce a new Secret name")
}

// TestEnsureIngestorCredentialsSecret_NoQueueRef verifies no Secret is produced when
// SmartBus is not configured.
func TestEnsureIngestorCredentialsSecret_NoQueueRef(t *testing.T) {
	ctx := context.TODO()

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))
	c := newFakeClientBuilder(sch).Build()

	cr := &enterpriseApi.IngestorCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
		Spec:       enterpriseApi.IngestorClusterSpec{Replicas: 1},
	}

	_, credsSecret, err := ensureIngestorDefaults(ctx, c, cr)
	require.NoError(t, err)
	assert.Empty(t, credsSecret.Name, "no queueRef → no credentials Secret")
}

// TestEnsureIngestorCredentialsSecret_IRSAProducesNoStaticCreds verifies that when the Queue
// has no VolList (IRSA / workload identity), ResolveQueueAndObjectStorage leaves the keys
// empty and no static-credential Secret is produced.
func TestEnsureIngestorCredentialsSecret_IRSAProducesNoStaticCreds(t *testing.T) {
	ctx := context.TODO()

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(appsv1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))
	c := newFakeClientBuilder(sch).Build()

	// Queue with no VolList — simulates IRSA / workload identity where no static creds exist.
	irsaQueue := &enterpriseApi.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "irsa-queue", Namespace: "test"},
		Spec: enterpriseApi.QueueSpec{
			Provider: "sqs",
			SQS: enterpriseApi.SQSSpec{
				Name:       "test-queue",
				AuthRegion: "us-west-2",
				Endpoint:   "https://sqs.us-west-2.amazonaws.com",
				DLQ:        "sqs-dlq-test",
				// VolList intentionally empty — IRSA uses pod identity, not static creds.
			},
		},
	}
	require.NoError(t, c.Create(ctx, irsaQueue))

	objStorage := &enterpriseApi.ObjectStorage{
		ObjectMeta: metav1.ObjectMeta{Name: "irsa-os", Namespace: "test"},
		Spec: enterpriseApi.ObjectStorageSpec{
			Provider: "s3",
			S3: enterpriseApi.S3Spec{
				Endpoint: "https://s3.us-west-2.amazonaws.com",
				Path:     "bucket/key",
			},
		},
	}
	require.NoError(t, c.Create(ctx, objStorage))

	cr := &enterpriseApi.IngestorCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
		Spec: enterpriseApi.IngestorClusterSpec{
			Replicas: 1,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				ServiceAccount: "irsa-sa",
			},
			QueueRef:         corev1.ObjectReference{Name: irsaQueue.Name, Namespace: irsaQueue.Namespace},
			ObjectStorageRef: corev1.ObjectReference{Name: objStorage.Name, Namespace: objStorage.Namespace},
		},
	}

	_, credsSecret, err := ensureIngestorDefaults(ctx, c, cr)
	require.NoError(t, err)
	assert.Empty(t, credsSecret.Name, "no VolList → no static credentials Secret")
}

// TestApplyIngestorCluster_QueueCredsSecretLifecycle drives the full ingestor reconciler
// and asserts that (1) a credentials Secret is created and mounted on the ingestor
// StatefulSet, and (2) rotating the source credentials creates a new Secret and
// garbage-collects the stale one — the declarative replacement for the old
// QueueConfigUpdated/IngestorsRestarted imperative path.
func TestApplyIngestorCluster_QueueCredsSecretLifecycle(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	ctx := context.TODO()
	recorder := &mockEventRecorder{events: []mockEvent{}}
	eventPublisher := &K8EventPublisher{recorder: recorder}
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(appsv1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	c := newFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.IngestorCluster{}).
		Build()

	probeConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-test-probe-configmap", Namespace: "test"},
	}
	require.NoError(t, c.Create(ctx, probeConfigMap))

	queue, objStorage := newIngestorQueueOSFixture(t, ctx, c, "queue", "queue-secrets")

	passwordSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test-secrets", Namespace: "test"},
		Data:       map[string][]byte{"password": []byte("dummy")},
	}
	require.NoError(t, c.Create(ctx, passwordSecret))

	crName := "ing1"
	cr := &enterpriseApi.IngestorCluster{
		ObjectMeta: metav1.ObjectMeta{Name: crName, Namespace: "test"},
		Spec: enterpriseApi.IngestorClusterSpec{
			Replicas: 3,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
			QueueRef:         corev1.ObjectReference{Name: queue.Name, Namespace: queue.Namespace},
			ObjectStorageRef: corev1.ObjectReference{Name: objStorage.Name, Namespace: objStorage.Namespace},
		},
		Status: enterpriseApi.IngestorClusterStatus{ReadyReplicas: 0, TelAppInstalled: true},
	}
	require.NoError(t, c.Create(ctx, cr))

	threeReplicas := int32(3)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(SplunkIngestor, cr.GetName()),
			Namespace: cr.GetNamespace(),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &threeReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:latest"}}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			Replicas: threeReplicas, ReadyReplicas: threeReplicas,
			CurrentRevision: "v1", UpdateRevision: "v1",
		},
	}
	require.NoError(t, c.Create(ctx, sts))

	basePod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:latest"}},
			Volumes: []corev1.Volume{
				{Name: "mnt-splunk-secrets", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "test-secrets"}}},
			},
		},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{Ready: true}},
		},
	}
	for i := int32(0); i < threeReplicas; i++ {
		pod := basePod.DeepCopy()
		pod.ObjectMeta = metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetPodName(SplunkIngestor, cr.GetName(), i),
			Namespace: cr.GetNamespace(),
			Labels: map[string]string{
				"app.kubernetes.io/instance": GetSplunkStatefulsetName(SplunkIngestor, cr.GetName()),
				"controller-revision-hash":   "v1",
			},
		}
		require.NoError(t, c.Create(ctx, pod))
	}

	// --- Pass 1: reconcile creates the credentials Secret and mounts it ---
	_, err := ApplyIngestorCluster(ctx, c, cr)
	require.NoError(t, err)

	credsList := listIngestorCredsSecrets(t, ctx, c, crName)
	require.Len(t, credsList, 1, "reconcile must create exactly one credentials Secret")
	firstName := credsList[0].Name
	assert.Regexp(t, regexp.MustCompile(`^sok-ingestorcluster-creds-[0-9a-f]{6}$`), firstName)

	// The ingestor StatefulSet mounts the credentials Secret and joins SPLUNK_DEFAULTS_URL.
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: sts.GetName(), Namespace: sts.GetNamespace()}, sts))
	var mounted bool
	for _, v := range sts.Spec.Template.Spec.Volumes {
		if v.Secret != nil && v.Secret.SecretName == firstName {
			mounted = true
		}
	}
	assert.True(t, mounted, "ingestor StatefulSet must mount the credentials Secret")

	var defaultsURL string
	for _, e := range sts.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "SPLUNK_DEFAULTS_URL" {
			defaultsURL = e.Value
		}
	}
	assert.Contains(t, defaultsURL, resources.SecretMountPath(), "SPLUNK_DEFAULTS_URL must include the creds mount path")

	// The declarative path emits no imperative queue-config / restart events.
	for _, event := range recorder.events {
		assert.NotEqual(t, "QueueConfigUpdated", event.reason, "declarative path must not emit QueueConfigUpdated")
		assert.NotEqual(t, "IngestorsRestarted", event.reason, "declarative path must not emit IngestorsRestarted")
	}

	// --- Pass 2: rotate credentials → new Secret name, stale one garbage-collected ---
	rotated := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: "test", Name: "queue-secrets"}, rotated))
	rotated.Data["s3_secret_key"] = []byte("rotated-secret")
	require.NoError(t, c.Update(ctx, rotated))

	_, err = ApplyIngestorCluster(ctx, c, cr)
	require.NoError(t, err)

	credsList = listIngestorCredsSecrets(t, ctx, c, crName)
	require.Len(t, credsList, 1, "stale credentials Secret must be garbage-collected after rotation")
	assert.NotEqual(t, firstName, credsList[0].Name, "rotated credentials must produce a new Secret name")
}

// TestIngScaledUpScaledDownEvents checks that scale-up/down events are emitted
// after the StatefulSet reaches the desired replica count.
func TestIngScaledUpScaledDownEvents(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	ctx := context.TODO()
	recorder := &mockEventRecorder{events: []mockEvent{}}
	eventPublisher := &K8EventPublisher{recorder: recorder}
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	scheme := pkgruntime.NewScheme()
	_ = enterpriseApi.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = policyv1.AddToScheme(scheme)
	c := newFakeClientBuilder(scheme).Build()

	queue := &enterpriseApi.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "queue", Namespace: "test"},
		Spec: enterpriseApi.QueueSpec{
			Provider: "sqs",
			SQS: enterpriseApi.SQSSpec{
				Name: "test-queue", AuthRegion: "us-west-2",
				Endpoint: "https://sqs.us-west-2.amazonaws.com", DLQ: "sqs-dlq-test",
			},
		},
	}
	_ = c.Create(ctx, queue)

	objStorage := &enterpriseApi.ObjectStorage{
		ObjectMeta: metav1.ObjectMeta{Name: "os", Namespace: "test"},
		Spec: enterpriseApi.ObjectStorageSpec{
			Provider: "s3",
			S3:       enterpriseApi.S3Spec{Endpoint: "https://s3.us-west-2.amazonaws.com", Path: "bucket/key"},
		},
	}
	_ = c.Create(ctx, objStorage)

	probeConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-test-probe-configmap", Namespace: "test"},
	}
	_ = c.Create(ctx, probeConfigMap)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test-secrets", Namespace: "test"},
		Data:       map[string][]byte{"password": []byte("dummy")},
	}
	_ = c.Create(ctx, secret)

	cr := &enterpriseApi.IngestorCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ingestor", Namespace: "test"},
		Spec: enterpriseApi.IngestorClusterSpec{
			Replicas: 1,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock:           true,
				ServiceAccount: "sa",
			},
			QueueRef:         corev1.ObjectReference{Name: queue.Name, Namespace: queue.Namespace},
			ObjectStorageRef: corev1.ObjectReference{Name: objStorage.Name, Namespace: objStorage.Namespace},
		},
		Status: enterpriseApi.IngestorClusterStatus{
			Replicas: 1, TelAppInstalled: true,
		},
	}
	_ = c.Create(ctx, cr)

	oneReplica := int32(1)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(SplunkIngestor, cr.GetName()),
			Namespace: cr.GetNamespace(),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &oneReplica,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:latest"}}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			Replicas: oneReplica, ReadyReplicas: oneReplica,
			CurrentRevision: "v1", UpdateRevision: "v1",
		},
	}
	_ = c.Create(ctx, sts)

	basePod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{Name: "mnt-splunk-secrets", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "test-secrets"}}},
			},
		},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{Ready: true}},
		},
	}
	pod := basePod.DeepCopy()
	pod.ObjectMeta = metav1.ObjectMeta{
		Name: GetSplunkStatefulsetPodName(SplunkIngestor, cr.GetName(), 0), Namespace: cr.GetNamespace(),
		Labels: map[string]string{
			"app.kubernetes.io/instance": GetSplunkStatefulsetName(SplunkIngestor, cr.GetName()),
			"controller-revision-hash":   "v1",
		},
	}
	_ = c.Create(ctx, pod)

	_, err := ApplyIngestorCluster(ctx, c, cr)
	assert.NoError(t, err)

	// ===== Scale up =====
	threeReplicas := int32(3)
	cr.Spec.Replicas = threeReplicas
	_ = c.Update(ctx, sts)
	for i := int32(1); i < threeReplicas; i++ {
		p := basePod.DeepCopy()
		p.ObjectMeta = metav1.ObjectMeta{
			Name: GetSplunkStatefulsetPodName(SplunkIngestor, cr.GetName(), i), Namespace: cr.GetNamespace(),
			Labels: map[string]string{
				"app.kubernetes.io/instance": GetSplunkStatefulsetName(SplunkIngestor, cr.GetName()),
				"controller-revision-hash":   "v1",
			},
		}
		_ = c.Create(ctx, p)
	}

	_, err = ApplyIngestorCluster(ctx, c, cr)
	assert.NoError(t, err)

	_ = c.Get(ctx, client.ObjectKey{Name: GetSplunkStatefulsetName(SplunkIngestor, cr.GetName()), Namespace: cr.GetNamespace()}, sts)
	sts.Status.Replicas = threeReplicas
	sts.Status.ReadyReplicas = threeReplicas
	_ = c.Status().Update(ctx, sts)

	_, err = ApplyIngestorCluster(ctx, c, cr)
	assert.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseReady, cr.Status.Phase)

	scaledUp := false
	for _, event := range recorder.events {
		if event.reason == "ScaledUp" {
			scaledUp = true
		}
	}
	assert.True(t, scaledUp)

	// ===== Scale down =====
	recorder.events = []mockEvent{}
	cr.Spec.Replicas = oneReplica
	cr.Status.Replicas = threeReplicas
	cr.Status.ReadyReplicas = threeReplicas

	// Read the current STS from the fake client (it has the lifecycle fields injected by earlier reconciles)
	// and only update the replica count, so we don't overwrite the spec with the stale original sts.
	currentSts := &appsv1.StatefulSet{}
	_ = c.Get(ctx, client.ObjectKey{Name: sts.Name, Namespace: sts.Namespace}, currentSts)
	currentSts.Spec.Replicas = &oneReplica
	_ = c.Update(ctx, currentSts)
	currentSts.Status.Replicas = oneReplica
	currentSts.Status.ReadyReplicas = oneReplica
	_ = c.Status().Update(ctx, currentSts)
	for i := int32(1); i < threeReplicas; i++ {
		_ = c.Delete(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: GetSplunkStatefulsetPodName(SplunkIngestor, cr.GetName(), i), Namespace: cr.GetNamespace()}})
	}

	_, err = ApplyIngestorCluster(ctx, c, cr)
	assert.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseReady, cr.Status.Phase)
	assert.Equal(t, oneReplica, cr.Status.ReadyReplicas)

	scaledDown := false
	for _, event := range recorder.events {
		if event.reason == "ScaledDown" {
			scaledDown = true
		}
	}
	assert.True(t, scaledDown)
}

// TestIngQueueRefChangeRollsPodsDeclarative verifies the declarative replacement for the
// old QueueConfigUpdated/IngestorsRestarted imperative path: swapping QueueRef to a
// different queue produces new content-addressed ConfigMap and Secret names (which causes
// Kubernetes to roll pods via the StatefulSet template hash), and GC removes the stale ones.
func TestIngQueueRefChangeRollsPodsDeclarative(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	ctx := context.TODO()
	recorder := &mockEventRecorder{events: []mockEvent{}}
	eventPublisher := &K8EventPublisher{recorder: recorder}
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(appsv1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	c := newFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.IngestorCluster{}).
		Build()

	require.NoError(t, c.Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-test-probe-configmap", Namespace: "test"},
	}))

	// Two queues with distinct config so their content-addressed names differ.
	credsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "queue-secrets", Namespace: "test"},
		Data: map[string][]byte{
			"s3_access_key": []byte("AKIAEXAMPLE"),
			"s3_secret_key": []byte("shhh-secret"),
		},
	}
	require.NoError(t, c.Create(ctx, credsSecret))

	queueOld := &enterpriseApi.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "queue-old", Namespace: "test"},
		Spec: enterpriseApi.QueueSpec{
			Provider: "sqs",
			SQS: enterpriseApi.SQSSpec{
				Name: "old-queue", AuthRegion: "us-west-2",
				Endpoint: "https://sqs.us-west-2.amazonaws.com", DLQ: "old-dlq",
				SecretKeyRef: &enterpriseApi.SQSSecretKeyRef{
					AwsAccessKey: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "queue-secrets"}, Key: "s3_access_key"},
					AwsSecretKey: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "queue-secrets"}, Key: "s3_secret_key"},
				},
			},
		},
	}
	require.NoError(t, c.Create(ctx, queueOld))

	queueNew := &enterpriseApi.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "queue-new", Namespace: "test"},
		Spec: enterpriseApi.QueueSpec{
			Provider: "sqs",
			SQS: enterpriseApi.SQSSpec{
				Name: "new-queue", AuthRegion: "us-east-1",
				Endpoint: "https://sqs.us-east-1.amazonaws.com", DLQ: "new-dlq",
				SecretKeyRef: &enterpriseApi.SQSSecretKeyRef{
					AwsAccessKey: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "queue-secrets"}, Key: "s3_access_key"},
					AwsSecretKey: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "queue-secrets"}, Key: "s3_secret_key"},
				},
			},
		},
	}
	require.NoError(t, c.Create(ctx, queueNew))

	objStorage := &enterpriseApi.ObjectStorage{
		ObjectMeta: metav1.ObjectMeta{Name: "os", Namespace: "test"},
		Spec: enterpriseApi.ObjectStorageSpec{
			Provider: "s3",
			S3:       enterpriseApi.S3Spec{Endpoint: "https://s3.us-west-2.amazonaws.com", Path: "bucket/key"},
		},
	}
	require.NoError(t, c.Create(ctx, objStorage))

	require.NoError(t, c.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test-secrets", Namespace: "test"},
		Data:       map[string][]byte{"password": []byte("dummy")},
	}))

	crName := "ing-ref-test"
	cr := &enterpriseApi.IngestorCluster{
		ObjectMeta: metav1.ObjectMeta{Name: crName, Namespace: "test"},
		Spec: enterpriseApi.IngestorClusterSpec{
			Replicas: 1,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
			QueueRef:         corev1.ObjectReference{Name: queueOld.Name, Namespace: "test"},
			ObjectStorageRef: corev1.ObjectReference{Name: objStorage.Name, Namespace: "test"},
		},
		Status: enterpriseApi.IngestorClusterStatus{ReadyReplicas: 1, TelAppInstalled: true},
	}
	require.NoError(t, c.Create(ctx, cr))

	oneReplica := int32(1)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: GetSplunkStatefulsetName(SplunkIngestor, crName), Namespace: "test"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &oneReplica,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:latest"}}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			Replicas: oneReplica, ReadyReplicas: oneReplica,
			CurrentRevision: "v1", UpdateRevision: "v1",
		},
	}
	require.NoError(t, c.Create(ctx, sts))

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: GetSplunkStatefulsetPodName(SplunkIngestor, crName, 0), Namespace: "test",
			Labels: map[string]string{
				"app.kubernetes.io/instance": GetSplunkStatefulsetName(SplunkIngestor, crName),
				"controller-revision-hash":   "v1",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:latest"}},
			Volumes: []corev1.Volume{
				{Name: "mnt-splunk-secrets", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "test-secrets"}}},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}},
	}
	require.NoError(t, c.Create(ctx, pod))

	// --- Pass 1: reconcile with old queue ---
	_, err := ApplyIngestorCluster(ctx, c, cr)
	require.NoError(t, err)

	cmListOld := listIngestorCredsConfigMaps(t, ctx, c, crName)
	secretListOld := listIngestorCredsSecrets(t, ctx, c, crName)
	require.Len(t, cmListOld, 1, "pass 1 must create exactly one defaults ConfigMap")
	require.Len(t, secretListOld, 1, "pass 1 must create exactly one credentials Secret")
	oldCMName := cmListOld[0].Name
	oldSecretName := secretListOld[0].Name
	assert.Regexp(t, regexp.MustCompile(`^sok-ingestorcluster-defaults-[0-9a-f]{6}$`), oldCMName)
	assert.Regexp(t, regexp.MustCompile(`^sok-ingestorcluster-creds-[0-9a-f]{6}$`), oldSecretName)

	// The declarative path emits no imperative queue-config / restart events.
	for _, event := range recorder.events {
		assert.NotEqual(t, "QueueConfigUpdated", event.reason, "declarative path must not emit QueueConfigUpdated")
		assert.NotEqual(t, "IngestorsRestarted", event.reason, "declarative path must not emit IngestorsRestarted")
	}

	// --- Pass 2: swap QueueRef to a queue with different config ---
	recorder.events = []mockEvent{}
	cr.Spec.QueueRef = corev1.ObjectReference{Name: queueNew.Name, Namespace: "test"}

	_, err = ApplyIngestorCluster(ctx, c, cr)
	require.NoError(t, err)

	// New queue config → new content-addressed names.
	cmListNew := listIngestorCredsConfigMaps(t, ctx, c, crName)
	secretListNew := listIngestorCredsSecrets(t, ctx, c, crName)
	require.Len(t, cmListNew, 1, "stale defaults ConfigMap must be garbage-collected after queue ref change")
	require.Len(t, secretListNew, 1, "stale credentials Secret must be garbage-collected after queue ref change")
	assert.NotEqual(t, oldCMName, cmListNew[0].Name, "new queue config must produce a new ConfigMap name")
	assert.NotEqual(t, oldSecretName, secretListNew[0].Name, "new queue config must produce a new Secret name")

	// Still no imperative events on the ref-change pass.
	for _, event := range recorder.events {
		assert.NotEqual(t, "QueueConfigUpdated", event.reason, "declarative path must not emit QueueConfigUpdated on ref change")
		assert.NotEqual(t, "IngestorsRestarted", event.reason, "declarative path must not emit IngestorsRestarted on ref change")
	}
}
func TestGetIngestorStatefulSetPreStop(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	cr := enterpriseApi.IngestorCluster{
		TypeMeta: metav1.TypeMeta{Kind: "IngestorCluster"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: enterpriseApi.IngestorClusterSpec{
			Replicas:         1,
			QueueRef:         corev1.ObjectReference{Name: "queue", Namespace: "test"},
			ObjectStorageRef: corev1.ObjectReference{Name: "os", Namespace: "test"},
		},
	}

	c := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Fatalf("ApplyNamespaceScopedSecretObject: %v", err)
	}
	if err := validateIngestorClusterSpec(ctx, c, &cr); err != nil {
		t.Fatalf("validateIngestorClusterSpec: %v", err)
	}

	ss, err := getIngestorStatefulSet(ctx, c, &cr)
	if err != nil {
		t.Fatalf("getIngestorStatefulSet: %v", err)
	}

	if ss.Spec.Template.Spec.TerminationGracePeriodSeconds == nil {
		t.Fatal("TerminationGracePeriodSeconds is nil")
	}
	if *ss.Spec.Template.Spec.TerminationGracePeriodSeconds != 60 {
		t.Errorf("TerminationGracePeriodSeconds = %d; want 60", *ss.Spec.Template.Spec.TerminationGracePeriodSeconds)
	}

	for i, c := range ss.Spec.Template.Spec.Containers {
		if c.Lifecycle == nil {
			t.Errorf("container[%d] Lifecycle is nil", i)
			continue
		}
		if c.Lifecycle.PreStop == nil {
			t.Errorf("container[%d] PreStop is nil", i)
			continue
		}
		if c.Lifecycle.PreStop.Exec == nil {
			t.Errorf("container[%d] PreStop.Exec is nil", i)
			continue
		}
		wantCmd := []string{"/bin/sh", "-c", "/opt/splunk/bin/splunk stop"}
		cmd := c.Lifecycle.PreStop.Exec.Command
		if len(cmd) != len(wantCmd) {
			t.Errorf("container[%d] Command = %v; want %v", i, cmd, wantCmd)
			continue
		}
		for j := range wantCmd {
			if cmd[j] != wantCmd[j] {
				t.Errorf("container[%d] Command[%d] = %q; want %q", i, j, cmd[j], wantCmd[j])
			}
		}
	}
}
