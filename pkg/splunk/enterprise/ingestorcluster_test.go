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
	provider := "sqs_smartbus"

	bus := &enterpriseApi.Bus{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Bus",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bus",
			Namespace: "test",
		},
		Spec: enterpriseApi.BusSpec{
			Provider: "sqs",
			SQS: enterpriseApi.SQSSpec{
				Name:     "test-queue",
				Region:   "us-west-2",
				Endpoint: "https://sqs.us-west-2.amazonaws.com",
				DLQ:      "sqs-dlq-test",
			},
		},
	}
	c.Create(ctx, bus)

	lms := enterpriseApi.LargeMessageStore{
		TypeMeta: metav1.TypeMeta{
			Kind:       "LargeMessageStore",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lms",
			Namespace: "test",
		},
		Spec: enterpriseApi.LargeMessageStoreSpec{
			Provider: "s3",
			S3: enterpriseApi.S3Spec{
				Endpoint: "https://s3.us-west-2.amazonaws.com",
				Path:     "s3://bucket/key",
			},
		},
	}
	c.Create(ctx, &lms)

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
			BusRef: corev1.ObjectReference{
				Name:      bus.Name,
				Namespace: bus.Namespace,
			},
			LargeMessageStoreRef: corev1.ObjectReference{
				Name:      lms.Name,
				Namespace: lms.Namespace,
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
		{fmt.Sprintf("remote_queue.%s.encoding_format", provider), "s2s"},
		{fmt.Sprintf("remote_queue.%s.auth_region", provider), bus.Spec.SQS.Region},
		{fmt.Sprintf("remote_queue.%s.endpoint", provider), bus.Spec.SQS.Endpoint},
		{fmt.Sprintf("remote_queue.%s.large_message_store.endpoint", provider), lms.Spec.S3.Endpoint},
		{fmt.Sprintf("remote_queue.%s.large_message_store.path", provider), lms.Spec.S3.Path},
		{fmt.Sprintf("remote_queue.%s.dead_letter_queue.name", provider), bus.Spec.SQS.DLQ},
		{fmt.Sprintf("remote_queue.%s.max_count.max_retries_per_part", provider), "4"},
		{fmt.Sprintf("remote_queue.%s.retry_policy", provider), "max_count"},
		{fmt.Sprintf("remote_queue.%s.send_interval", provider), "5s"},
	}

	body := buildFormBody(propertyKVList)
	addRemoteQueueHandlersForIngestor(mockHTTPClient, cr, bus, cr.Status.ReadyReplicas, "conf-outputs", body)

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

	bus := enterpriseApi.Bus{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Bus",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "bus",
		},
		Spec: enterpriseApi.BusSpec{
			Provider: "sqs",
			SQS: enterpriseApi.SQSSpec{
				Name:     "test-queue",
				Region:   "us-west-2",
				Endpoint: "https://sqs.us-west-2.amazonaws.com",
				DLQ:      "sqs-dlq-test",
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
			BusRef: corev1.ObjectReference{
				Name: bus.Name,
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
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-test-ingestor","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"ingestor","app.kubernetes.io/instance":"splunk-test-ingestor","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"ingestor","app.kubernetes.io/part-of":"splunk-test-ingestor"},"ownerReferences":[{"apiVersion":"","kind":"IngestorCluster","name":"test","uid":"","controller":true}]},"spec":{"replicas":3,"selector":{"matchLabels":{"app.kubernetes.io/component":"ingestor","app.kubernetes.io/instance":"splunk-test-ingestor","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"ingestor","app.kubernetes.io/part-of":"splunk-test-ingestor"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"ingestor","app.kubernetes.io/instance":"splunk-test-ingestor","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"ingestor","app.kubernetes.io/part-of":"splunk-test-ingestor"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-test-ingestor-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"http-hec","containerPort":8088,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"},{"name":"tcp-s2s","containerPort":9997,"protocol":"TCP"},{"name":"user-defined","containerPort":32000,"protocol":"UDP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_standalone"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_GENERAL_TERMS","value":"--accept-sgt-current-at-splunk-com"},{"name":"SPLUNK_SKIP_CLUSTER_BUNDLE_PUSH","value":"true"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent","securityContext":{"capabilities":{"add":["NET_BIND_SERVICE"],"drop":["ALL"]},"privileged":false,"runAsUser":41812,"runAsNonRoot":true,"allowPrivilegeEscalation":false,"seccompProfile":{"type":"RuntimeDefault"}}}],"securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812,"fsGroupChangePolicy":"OnRootMismatch"},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-test-ingestor"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"ingestor","app.kubernetes.io/instance":"splunk-test-ingestor","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"ingestor","app.kubernetes.io/part-of":"splunk-test-ingestor"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"ingestor","app.kubernetes.io/instance":"splunk-test-ingestor","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"ingestor","app.kubernetes.io/part-of":"splunk-test-ingestor"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-test-ingestor-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)

	// Create a service account
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-test-ingestor","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"ingestor","app.kubernetes.io/instance":"splunk-test-ingestor","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"ingestor","app.kubernetes.io/part-of":"splunk-test-ingestor"},"ownerReferences":[{"apiVersion":"","kind":"IngestorCluster","name":"test","uid":"","controller":true}]},"spec":{"replicas":3,"selector":{"matchLabels":{"app.kubernetes.io/component":"ingestor","app.kubernetes.io/instance":"splunk-test-ingestor","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"ingestor","app.kubernetes.io/part-of":"splunk-test-ingestor"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"ingestor","app.kubernetes.io/instance":"splunk-test-ingestor","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"ingestor","app.kubernetes.io/part-of":"splunk-test-ingestor"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-test-ingestor-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"http-hec","containerPort":8088,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"},{"name":"tcp-s2s","containerPort":9997,"protocol":"TCP"},{"name":"user-defined","containerPort":32000,"protocol":"UDP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_standalone"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_GENERAL_TERMS","value":"--accept-sgt-current-at-splunk-com"},{"name":"SPLUNK_SKIP_CLUSTER_BUNDLE_PUSH","value":"true"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent","securityContext":{"capabilities":{"add":["NET_BIND_SERVICE"],"drop":["ALL"]},"privileged":false,"runAsUser":41812,"runAsNonRoot":true,"allowPrivilegeEscalation":false,"seccompProfile":{"type":"RuntimeDefault"}}}],"serviceAccountName":"defaults","securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812,"fsGroupChangePolicy":"OnRootMismatch"},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-test-ingestor"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"ingestor","app.kubernetes.io/instance":"splunk-test-ingestor","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"ingestor","app.kubernetes.io/part-of":"splunk-test-ingestor"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"ingestor","app.kubernetes.io/instance":"splunk-test-ingestor","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"ingestor","app.kubernetes.io/part-of":"splunk-test-ingestor"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-test-ingestor-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)

	// Add extraEnv
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-test-ingestor","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"ingestor","app.kubernetes.io/instance":"splunk-test-ingestor","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"ingestor","app.kubernetes.io/part-of":"splunk-test-ingestor"},"ownerReferences":[{"apiVersion":"","kind":"IngestorCluster","name":"test","uid":"","controller":true}]},"spec":{"replicas":3,"selector":{"matchLabels":{"app.kubernetes.io/component":"ingestor","app.kubernetes.io/instance":"splunk-test-ingestor","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"ingestor","app.kubernetes.io/part-of":"splunk-test-ingestor"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"ingestor","app.kubernetes.io/instance":"splunk-test-ingestor","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"ingestor","app.kubernetes.io/part-of":"splunk-test-ingestor"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-test-ingestor-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"http-hec","containerPort":8088,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"},{"name":"tcp-s2s","containerPort":9997,"protocol":"TCP"},{"name":"user-defined","containerPort":32000,"protocol":"UDP"}],"env":[{"name":"TEST_ENV_VAR","value":"test_value"},{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_standalone"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_GENERAL_TERMS","value":"--accept-sgt-current-at-splunk-com"},{"name":"SPLUNK_SKIP_CLUSTER_BUNDLE_PUSH","value":"true"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent","securityContext":{"capabilities":{"add":["NET_BIND_SERVICE"],"drop":["ALL"]},"privileged":false,"runAsUser":41812,"runAsNonRoot":true,"allowPrivilegeEscalation":false,"seccompProfile":{"type":"RuntimeDefault"}}}],"serviceAccountName":"defaults","securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812,"fsGroupChangePolicy":"OnRootMismatch"},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-test-ingestor"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"ingestor","app.kubernetes.io/instance":"splunk-test-ingestor","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"ingestor","app.kubernetes.io/part-of":"splunk-test-ingestor"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"ingestor","app.kubernetes.io/instance":"splunk-test-ingestor","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"ingestor","app.kubernetes.io/part-of":"splunk-test-ingestor"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-test-ingestor-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)

	// Add additional label to cr metadata to transfer to the statefulset
	cr.ObjectMeta.Labels = make(map[string]string)
	cr.ObjectMeta.Labels["app.kubernetes.io/test-extra-label"] = "test-extra-label-value"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-test-ingestor","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"ingestor","app.kubernetes.io/instance":"splunk-test-ingestor","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"ingestor","app.kubernetes.io/part-of":"splunk-test-ingestor","app.kubernetes.io/test-extra-label":"test-extra-label-value"},"ownerReferences":[{"apiVersion":"","kind":"IngestorCluster","name":"test","uid":"","controller":true}]},"spec":{"replicas":3,"selector":{"matchLabels":{"app.kubernetes.io/component":"ingestor","app.kubernetes.io/instance":"splunk-test-ingestor","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"ingestor","app.kubernetes.io/part-of":"splunk-test-ingestor"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"ingestor","app.kubernetes.io/instance":"splunk-test-ingestor","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"ingestor","app.kubernetes.io/part-of":"splunk-test-ingestor","app.kubernetes.io/test-extra-label":"test-extra-label-value"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-test-ingestor-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"http-hec","containerPort":8088,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"},{"name":"tcp-s2s","containerPort":9997,"protocol":"TCP"},{"name":"user-defined","containerPort":32000,"protocol":"UDP"}],"env":[{"name":"TEST_ENV_VAR","value":"test_value"},{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_standalone"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_GENERAL_TERMS","value":"--accept-sgt-current-at-splunk-com"},{"name":"SPLUNK_SKIP_CLUSTER_BUNDLE_PUSH","value":"true"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent","securityContext":{"capabilities":{"add":["NET_BIND_SERVICE"],"drop":["ALL"]},"privileged":false,"runAsUser":41812,"runAsNonRoot":true,"allowPrivilegeEscalation":false,"seccompProfile":{"type":"RuntimeDefault"}}}],"serviceAccountName":"defaults","securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812,"fsGroupChangePolicy":"OnRootMismatch"},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-test-ingestor"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"ingestor","app.kubernetes.io/instance":"splunk-test-ingestor","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"ingestor","app.kubernetes.io/part-of":"splunk-test-ingestor","app.kubernetes.io/test-extra-label":"test-extra-label-value"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"ingestor","app.kubernetes.io/instance":"splunk-test-ingestor","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"ingestor","app.kubernetes.io/part-of":"splunk-test-ingestor","app.kubernetes.io/test-extra-label":"test-extra-label-value"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-test-ingestor-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)
}

func TestGetChangedBusFieldsForIngestor(t *testing.T) {
	provider := "sqs_smartbus"

	bus := enterpriseApi.Bus{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Bus",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "bus",
		},
		Spec: enterpriseApi.BusSpec{
			Provider: "sqs",
			SQS: enterpriseApi.SQSSpec{
				Name:     "test-queue",
				Region:   "us-west-2",
				Endpoint: "https://sqs.us-west-2.amazonaws.com",
				DLQ:      "sqs-dlq-test",
			},
		},
	}

	lms := enterpriseApi.LargeMessageStore{
		TypeMeta: metav1.TypeMeta{
			Kind:       "LargeMessageStore",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "lms",
		},
		Spec: enterpriseApi.LargeMessageStoreSpec{
			Provider: "s3",
			S3: enterpriseApi.S3Spec{
				Endpoint: "https://s3.us-west-2.amazonaws.com",
				Path:     "s3://bucket/key",
			},
		},
	}

	newCR := &enterpriseApi.IngestorCluster{
		Spec: enterpriseApi.IngestorClusterSpec{
			BusRef: corev1.ObjectReference{
				Name: bus.Name,
			},
			LargeMessageStoreRef: corev1.ObjectReference{
				Name: lms.Name,
			},
		},
		Status: enterpriseApi.IngestorClusterStatus{},
	}

	busChangedFields, pipelineChangedFields := getChangedBusFieldsForIngestor(&bus, &lms, newCR, false)

	assert.Equal(t, 10, len(busChangedFields))
	assert.Equal(t, [][]string{
		{"remote_queue.type", provider},
		{fmt.Sprintf("remote_queue.%s.auth_region", provider), bus.Spec.SQS.Region},
		{fmt.Sprintf("remote_queue.%s.endpoint", provider), bus.Spec.SQS.Endpoint},
		{fmt.Sprintf("remote_queue.%s.large_message_store.endpoint", provider), lms.Spec.S3.Endpoint},
		{fmt.Sprintf("remote_queue.%s.large_message_store.path", provider), lms.Spec.S3.Path},
		{fmt.Sprintf("remote_queue.%s.dead_letter_queue.name", provider), bus.Spec.SQS.DLQ},
		{fmt.Sprintf("remote_queue.%s.encoding_format", provider), "s2s"},
		{fmt.Sprintf("remote_queue.%s.max_count.max_retries_per_part", provider), "4"},
		{fmt.Sprintf("remote_queue.%s.retry_policy", provider), "max_count"},
		{fmt.Sprintf("remote_queue.%s.send_interval", provider), "5s"},
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
	provider := "sqs_smartbus"

	bus := enterpriseApi.Bus{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Bus",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "bus",
		},
		Spec: enterpriseApi.BusSpec{
			Provider: "sqs",
			SQS: enterpriseApi.SQSSpec{
				Name:     "test-queue",
				Region:   "us-west-2",
				Endpoint: "https://sqs.us-west-2.amazonaws.com",
				DLQ:      "sqs-dlq-test",
			},
		},
	}

	lms := enterpriseApi.LargeMessageStore{
		TypeMeta: metav1.TypeMeta{
			Kind:       "LargeMessageStore",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "lms",
		},
		Spec: enterpriseApi.LargeMessageStoreSpec{
			Provider: "s3",
			S3: enterpriseApi.S3Spec{
				Endpoint: "https://s3.us-west-2.amazonaws.com",
				Path:     "s3://bucket/key",
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
			BusRef: corev1.ObjectReference{
				Name: bus.Name,
			},
			LargeMessageStoreRef: corev1.ObjectReference{
				Name: lms.Name,
			},
		},
		Status: enterpriseApi.IngestorClusterStatus{
			Replicas:          3,
			ReadyReplicas:     3,
			Bus:               &enterpriseApi.BusSpec{},
			LargeMessageStore: &enterpriseApi.LargeMessageStoreSpec{},
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

	err := mgr.handlePushBusChange(ctx, newCR, bus, lms, c)
	assert.NotNil(t, err)

	// Mock secret
	c.Create(ctx, secret)

	mockHTTPClient := &spltest.MockHTTPClient{}

	// Negative test case: failure in creating remote queue stanza
	mgr = newTestPushBusPipelineManager(mockHTTPClient)

	err = mgr.handlePushBusChange(ctx, newCR, bus, lms, c)
	assert.NotNil(t, err)

	// outputs.conf
	propertyKVList := [][]string{
		{fmt.Sprintf("remote_queue.%s.encoding_format", provider), "s2s"},
		{fmt.Sprintf("remote_queue.%s.auth_region", provider), bus.Spec.SQS.Region},
		{fmt.Sprintf("remote_queue.%s.endpoint", provider), bus.Spec.SQS.Endpoint},
		{fmt.Sprintf("remote_queue.%s.large_message_store.endpoint", provider), lms.Spec.S3.Endpoint},
		{fmt.Sprintf("remote_queue.%s.large_message_store.path", provider), lms.Spec.S3.Path},
		{fmt.Sprintf("remote_queue.%s.dead_letter_queue.name", provider), bus.Spec.SQS.DLQ},
		{fmt.Sprintf("remote_queue.max_count.%s.max_retries_per_part", provider), "4"},
		{fmt.Sprintf("remote_queue.%s.retry_policy", provider), "max_count"},
		{fmt.Sprintf("remote_queue.%s.send_interval", provider), "5s"},
	}

	body := buildFormBody(propertyKVList)
	addRemoteQueueHandlersForIngestor(mockHTTPClient, newCR, &bus, newCR.Status.ReadyReplicas, "conf-outputs", body)

	// Negative test case: failure in creating remote queue stanza
	mgr = newTestPushBusPipelineManager(mockHTTPClient)

	err = mgr.handlePushBusChange(ctx, newCR, bus, lms, c)
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

	err = mgr.handlePushBusChange(ctx, newCR, bus, lms, c)
	assert.Nil(t, err)
}

func addRemoteQueueHandlersForIngestor(mockHTTPClient *spltest.MockHTTPClient, cr *enterpriseApi.IngestorCluster, bus *enterpriseApi.Bus, replicas int32, confName, body string) {
	for i := 0; i < int(replicas); i++ {
		podName := fmt.Sprintf("splunk-%s-ingestor-%d", cr.GetName(), i)
		baseURL := fmt.Sprintf(
			"https://%s.splunk-%s-ingestor-headless.%s.svc.cluster.local:8089/servicesNS/nobody/system/configs/%s",
			podName, cr.GetName(), cr.GetNamespace(), confName,
		)

		createReqBody := fmt.Sprintf("name=%s", fmt.Sprintf("remote_queue:%s", bus.Spec.SQS.Name))
		reqCreate, _ := http.NewRequest("POST", baseURL, strings.NewReader(createReqBody))
		mockHTTPClient.AddHandler(reqCreate, 200, "", nil)

		updateURL := fmt.Sprintf("%s/%s", baseURL, fmt.Sprintf("remote_queue:%s", bus.Spec.SQS.Name))
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
