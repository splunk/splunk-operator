/*
Copyright 2025.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package enterprise

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
	busConfig := &enterpriseApi.BusConfiguration{
		TypeMeta: metav1.TypeMeta{
			Kind:       "BusConfiguration",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "busConfig",
			Namespace: "test",
		},
		Spec: enterpriseApi.BusConfigurationSpec{
			Type: "sqs_smartbus",
			SQS: enterpriseApi.SQSSpec{
				QueueName:                 "test-queue",
				AuthRegion:                "us-west-2",
				Endpoint:                  "https://sqs.us-west-2.amazonaws.com",
				LargeMessageStorePath:     "s3://ingestion/smartbus-test",
				LargeMessageStoreEndpoint: "https://s3.us-west-2.amazonaws.com",
				DeadLetterQueueName:       "sqs-dlq-test",
			},
		},
	}
	c.Create(ctx, busConfig)

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
				Mock: true,
			},
			BusConfigurationRef: corev1.ObjectReference{
				Name:      busConfig.Name,
				Namespace: busConfig.Namespace,
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

	// Ensure stored StatefulSet status reflects readiness after any reconcile modifications
	fetched := &appsv1.StatefulSet{}
	_ = c.Get(ctx, types.NamespacedName{Name: "splunk-test-ingestor", Namespace: "test"}, fetched)
	fetched.Status.Replicas = replicas
	fetched.Status.ReadyReplicas = replicas
	fetched.Status.UpdatedReplicas = replicas
	if fetched.Status.UpdateRevision == "" {
		fetched.Status.UpdateRevision = "v1"
	}
	c.Update(ctx, fetched)

	// Guarantee all pods have matching revision label
	for _, pn := range []string{"splunk-test-ingestor-0", "splunk-test-ingestor-1", "splunk-test-ingestor-2"} {
		p := &corev1.Pod{}
		if err := c.Get(ctx, types.NamespacedName{Name: pn, Namespace: "test"}, p); err == nil {
			if p.Labels == nil {
				p.Labels = map[string]string{}
			}
			p.Labels["controller-revision-hash"] = fetched.Status.UpdateRevision
			c.Update(ctx, p)
		}
	}

	// outputs.conf
	origNew := newIngestorClusterPodManager
	mockHTTPClient := &spltest.MockHTTPClient{}
	newIngestorClusterPodManager = func(l logr.Logger, cr *enterpriseApi.IngestorCluster, secret *corev1.Secret, _ NewSplunkClientFunc) ingestorClusterPodManager {
		return ingestorClusterPodManager{
			log: l, cr: cr, secrets: secret,
			newSplunkClient: func(uri, user, pass string) *splclient.SplunkClient {
				return &splclient.SplunkClient{ManagementURI: uri, Username: user, Password: pass, Client: mockHTTPClient}
			},
		}
	}
	defer func() { newIngestorClusterPodManager = origNew }()

	propertyKVList := [][]string{
		{fmt.Sprintf("remote_queue.%s.encoding_format", busConfig.Spec.Type), "s2s"},
		{fmt.Sprintf("remote_queue.%s.auth_region", busConfig.Spec.Type), busConfig.Spec.SQS.AuthRegion},
		{fmt.Sprintf("remote_queue.%s.endpoint", busConfig.Spec.Type), busConfig.Spec.SQS.Endpoint},
		{fmt.Sprintf("remote_queue.%s.large_message_store.endpoint", busConfig.Spec.Type), busConfig.Spec.SQS.LargeMessageStoreEndpoint},
		{fmt.Sprintf("remote_queue.%s.large_message_store.path", busConfig.Spec.Type), busConfig.Spec.SQS.LargeMessageStorePath},
		{fmt.Sprintf("remote_queue.%s.dead_letter_queue.name", busConfig.Spec.Type), busConfig.Spec.SQS.DeadLetterQueueName},
		{fmt.Sprintf("remote_queue.%s.max_count.max_retries_per_part", busConfig.Spec.Type), "4"},
		{fmt.Sprintf("remote_queue.%s.retry_policy", busConfig.Spec.Type), "max_count"},
		{fmt.Sprintf("remote_queue.%s.send_interval", busConfig.Spec.Type), "5s"},
	}

	body := buildFormBody(propertyKVList)
	addRemoteQueueHandlersForIngestor(mockHTTPClient, cr, busConfig, cr.Status.ReadyReplicas, "conf-outputs", body)

	// default-mode.conf
	propertyKVList = [][]string{
		{"pipeline:remotequeueruleset", "disabled", "false"},
		{"pipeline:ruleset", "disabled", "true"},
		{"pipeline:remotequeuetyping", "disabled", "false"},
		{"pipeline:remotequeueoutput", "disabled", "false"},
		{"pipeline:typing", "disabled", "true"},
		{"pipeline:indexerPipe", "disabled", "true"},
	}

	for i := 0; i < int(cr.Status.ReadyReplicas); i++ {
		podName := fmt.Sprintf("splunk-test-ingestor-%d", i)
		baseURL := fmt.Sprintf("https://%s.splunk-%s-ingestor-headless.%s.svc.cluster.local:8089/servicesNS/nobody/system/configs/conf-default-mode", podName, cr.GetName(), cr.GetNamespace())

		for _, field := range propertyKVList {
			req, _ := http.NewRequest("POST", baseURL, strings.NewReader(fmt.Sprintf("name=%s", field[0])))
			mockHTTPClient.AddHandler(req, 200, "", nil)

			updateURL := fmt.Sprintf("%s/%s", baseURL, field[0])
			req, _ = http.NewRequest("POST", updateURL, strings.NewReader(fmt.Sprintf("%s=%s", field[1], field[2])))
			mockHTTPClient.AddHandler(req, 200, "", nil)
		}
	}

	// Second reconcile should now yield Ready
	cr.Status.TelAppInstalled = true
	result, err = ApplyIngestorCluster(ctx, c, cr)
	assert.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseReady, cr.Status.Phase)
}

func TestGetIngestorStatefulSet(t *testing.T) {
	// Object definitions
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	busConfig := enterpriseApi.BusConfiguration{
		TypeMeta: metav1.TypeMeta{
			Kind:       "BusConfiguration",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "busConfig",
		},
		Spec: enterpriseApi.BusConfigurationSpec{
			Type: "sqs_smartbus",
			SQS: enterpriseApi.SQSSpec{
				QueueName:                 "test-queue",
				AuthRegion:                "us-west-2",
				Endpoint:                  "https://sqs.us-west-2.amazonaws.com",
				LargeMessageStorePath:     "s3://ingestion/smartbus-test",
				LargeMessageStoreEndpoint: "https://s3.us-west-2.amazonaws.com",
				DeadLetterQueueName:       "sqs-dlq-test",
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
			Replicas: 2,
			BusConfigurationRef: corev1.ObjectReference{
				Name: busConfig.Name,
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
	test(loadFixture(t, "statefulset_stack1_ingestor_base_1.json"))

	// Create a service account
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	test(loadFixture(t, "statefulset_stack1_ingestor_base_2.json"))

	// Add extraEnv
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	test(loadFixture(t, "statefulset_stack1_ingestor_base_3.json"))

	// Add additional label to cr metadata to transfer to the statefulset
	cr.ObjectMeta.Labels = make(map[string]string)
	cr.ObjectMeta.Labels["app.kubernetes.io/test-extra-label"] = "test-extra-label-value"
	test(loadFixture(t, "statefulset_stack1_ingestor_base_4.json"))
}

func TestGetChangedBusFieldsForIngestor(t *testing.T) {
	busConfig := enterpriseApi.BusConfiguration{
		TypeMeta: metav1.TypeMeta{
			Kind:       "BusConfiguration",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "busConfig",
		},
		Spec: enterpriseApi.BusConfigurationSpec{
			Type: "sqs_smartbus",
			SQS: enterpriseApi.SQSSpec{
				QueueName:                 "test-queue",
				AuthRegion:                "us-west-2",
				Endpoint:                  "https://sqs.us-west-2.amazonaws.com",
				LargeMessageStorePath:     "s3://ingestion/smartbus-test",
				LargeMessageStoreEndpoint: "https://s3.us-west-2.amazonaws.com",
				DeadLetterQueueName:       "sqs-dlq-test",
			},
		},
	}

	newCR := &enterpriseApi.IngestorCluster{
		Spec: enterpriseApi.IngestorClusterSpec{
			BusConfigurationRef: corev1.ObjectReference{
				Name: busConfig.Name,
			},
		},
		Status: enterpriseApi.IngestorClusterStatus{},
	}

	busChangedFields, pipelineChangedFields := getChangedBusFieldsForIngestor(&busConfig, newCR, false)

	assert.Equal(t, 10, len(busChangedFields))
	assert.Equal(t, [][]string{
		{"remote_queue.type", busConfig.Spec.Type},
		{fmt.Sprintf("remote_queue.%s.auth_region", busConfig.Spec.Type), busConfig.Spec.SQS.AuthRegion},
		{fmt.Sprintf("remote_queue.%s.endpoint", busConfig.Spec.Type), busConfig.Spec.SQS.Endpoint},
		{fmt.Sprintf("remote_queue.%s.large_message_store.endpoint", busConfig.Spec.Type), busConfig.Spec.SQS.LargeMessageStoreEndpoint},
		{fmt.Sprintf("remote_queue.%s.large_message_store.path", busConfig.Spec.Type), busConfig.Spec.SQS.LargeMessageStorePath},
		{fmt.Sprintf("remote_queue.%s.dead_letter_queue.name", busConfig.Spec.Type), busConfig.Spec.SQS.DeadLetterQueueName},
		{fmt.Sprintf("remote_queue.%s.encoding_format", busConfig.Spec.Type), "s2s"},
		{fmt.Sprintf("remote_queue.%s.max_count.max_retries_per_part", busConfig.Spec.Type), "4"},
		{fmt.Sprintf("remote_queue.%s.retry_policy", busConfig.Spec.Type), "max_count"},
		{fmt.Sprintf("remote_queue.%s.send_interval", busConfig.Spec.Type), "5s"},
	}, busChangedFields)

	assert.Equal(t, 6, len(pipelineChangedFields))
	assert.Equal(t, [][]string{
		{"pipeline:remotequeueruleset", "disabled", "false"},
		{"pipeline:ruleset", "disabled", "true"},
		{"pipeline:remotequeuetyping", "disabled", "false"},
		{"pipeline:remotequeueoutput", "disabled", "false"},
		{"pipeline:typing", "disabled", "true"},
		{"pipeline:indexerPipe", "disabled", "true"},
	}, pipelineChangedFields)
}

func TestHandlePushBusChange(t *testing.T) {
	// Object definitions
	busConfig := enterpriseApi.BusConfiguration{
		TypeMeta: metav1.TypeMeta{
			Kind:       "BusConfiguration",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "busConfig",
		},
		Spec: enterpriseApi.BusConfigurationSpec{
			Type: "sqs_smartbus",
			SQS: enterpriseApi.SQSSpec{
				QueueName:                 "test-queue",
				AuthRegion:                "us-west-2",
				Endpoint:                  "https://sqs.us-west-2.amazonaws.com",
				LargeMessageStorePath:     "s3://ingestion/smartbus-test",
				LargeMessageStoreEndpoint: "https://s3.us-west-2.amazonaws.com",
				DeadLetterQueueName:       "sqs-dlq-test",
			},
		},
	}

	newCR := &enterpriseApi.IngestorCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IngestorCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: enterpriseApi.IngestorClusterSpec{
			BusConfigurationRef: corev1.ObjectReference{
				Name: busConfig.Name,
			},
		},
		Status: enterpriseApi.IngestorClusterStatus{
			Replicas:      3,
			ReadyReplicas: 3,
		},
	}

	pod0 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-ingestor-0",
			Namespace: "test",
			Labels: map[string]string{
				"app.kubernetes.io/instance": "splunk-test-ingestor",
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

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"password": []byte("dummy"),
		},
	}

	// Mock pods
	c := spltest.NewMockClient()
	ctx := context.TODO()
	c.Create(ctx, pod0)
	c.Create(ctx, pod1)
	c.Create(ctx, pod2)

	// Negative test case: secret not found
	mgr := &ingestorClusterPodManager{}

	err := mgr.handlePushBusChange(ctx, newCR, busConfig, c)
	assert.NotNil(t, err)

	// Mock secret
	c.Create(ctx, secret)

	mockHTTPClient := &spltest.MockHTTPClient{}

	// Negative test case: failure in creating remote queue stanza
	mgr = newTestPushBusPipelineManager(mockHTTPClient)

	err = mgr.handlePushBusChange(ctx, newCR, busConfig, c)
	assert.NotNil(t, err)

	// outputs.conf
	propertyKVList := [][]string{
		{fmt.Sprintf("remote_queue.%s.encoding_format", busConfig.Spec.Type), "s2s"},
		{fmt.Sprintf("remote_queue.%s.auth_region", busConfig.Spec.Type), busConfig.Spec.SQS.AuthRegion},
		{fmt.Sprintf("remote_queue.%s.endpoint", busConfig.Spec.Type), busConfig.Spec.SQS.Endpoint},
		{fmt.Sprintf("remote_queue.%s.large_message_store.endpoint", busConfig.Spec.Type), busConfig.Spec.SQS.LargeMessageStoreEndpoint},
		{fmt.Sprintf("remote_queue.%s.large_message_store.path", busConfig.Spec.Type), busConfig.Spec.SQS.LargeMessageStorePath},
		{fmt.Sprintf("remote_queue.%s.dead_letter_queue.name", busConfig.Spec.Type), busConfig.Spec.SQS.DeadLetterQueueName},
		{fmt.Sprintf("remote_queue.max_count.%s.max_retries_per_part", busConfig.Spec.Type), "4"},
		{fmt.Sprintf("remote_queue.%s.retry_policy", busConfig.Spec.Type), "max_count"},
		{fmt.Sprintf("remote_queue.%s.send_interval", busConfig.Spec.Type), "5s"},
	}

	body := buildFormBody(propertyKVList)
	addRemoteQueueHandlersForIngestor(mockHTTPClient, newCR, &busConfig, newCR.Status.ReadyReplicas, "conf-outputs", body)

	// Negative test case: failure in creating remote queue stanza
	mgr = newTestPushBusPipelineManager(mockHTTPClient)

	err = mgr.handlePushBusChange(ctx, newCR, busConfig, c)
	assert.NotNil(t, err)

	// default-mode.conf
	propertyKVList = [][]string{
		{"pipeline:remotequeueruleset", "disabled", "false"},
		{"pipeline:ruleset", "disabled", "true"},
		{"pipeline:remotequeuetyping", "disabled", "false"},
		{"pipeline:remotequeueoutput", "disabled", "false"},
		{"pipeline:typing", "disabled", "true"},
		{"pipeline:indexerPipe", "disabled", "true"},
	}

	for i := 0; i < int(newCR.Status.ReadyReplicas); i++ {
		podName := fmt.Sprintf("splunk-test-ingestor-%d", i)
		baseURL := fmt.Sprintf("https://%s.splunk-%s-ingestor-headless.%s.svc.cluster.local:8089/servicesNS/nobody/system/configs/conf-default-mode", podName, newCR.GetName(), newCR.GetNamespace())

		for _, field := range propertyKVList {
			req, _ := http.NewRequest("POST", baseURL, strings.NewReader(fmt.Sprintf("name=%s", field[0])))
			mockHTTPClient.AddHandler(req, 200, "", nil)

			updateURL := fmt.Sprintf("%s/%s", baseURL, field[0])
			req, _ = http.NewRequest("POST", updateURL, strings.NewReader(fmt.Sprintf("%s=%s", field[1], field[2])))
			mockHTTPClient.AddHandler(req, 200, "", nil)
		}
	}

	mgr = newTestPushBusPipelineManager(mockHTTPClient)

	err = mgr.handlePushBusChange(ctx, newCR, busConfig, c)
	assert.Nil(t, err)
}

func addRemoteQueueHandlersForIngestor(mockHTTPClient *spltest.MockHTTPClient, cr *enterpriseApi.IngestorCluster, busConfig *enterpriseApi.BusConfiguration, replicas int32, confName, body string) {
	for i := 0; i < int(replicas); i++ {
		podName := fmt.Sprintf("splunk-%s-ingestor-%d", cr.GetName(), i)
		baseURL := fmt.Sprintf(
			"https://%s.splunk-%s-ingestor-headless.%s.svc.cluster.local:8089/servicesNS/nobody/system/configs/%s",
			podName, cr.GetName(), cr.GetNamespace(), confName,
		)

		createReqBody := fmt.Sprintf("name=%s", fmt.Sprintf("remote_queue:%s", busConfig.Spec.SQS.QueueName))
		reqCreate, _ := http.NewRequest("POST", baseURL, strings.NewReader(createReqBody))
		mockHTTPClient.AddHandler(reqCreate, 200, "", nil)

		updateURL := fmt.Sprintf("%s/%s", baseURL, fmt.Sprintf("remote_queue:%s", busConfig.Spec.SQS.QueueName))
		reqUpdate, _ := http.NewRequest("POST", updateURL, strings.NewReader(body))
		mockHTTPClient.AddHandler(reqUpdate, 200, "", nil)
	}
}

func newTestPushBusPipelineManager(mockHTTPClient *spltest.MockHTTPClient) *ingestorClusterPodManager {
	newSplunkClientForPushBusPipeline := func(uri, user, pass string) *splclient.SplunkClient {
		return &splclient.SplunkClient{
			ManagementURI: uri,
			Username:      user,
			Password:      pass,
			Client:        mockHTTPClient,
		}
	}
	return &ingestorClusterPodManager{
		newSplunkClient: newSplunkClientForPushBusPipeline,
	}
}
