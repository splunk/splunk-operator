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

/*
Package test includes common code used for testing other modules.
This package has no dependencies outside of the standard go and kubernetes libraries,
and the splunk.common package.
*/
package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	// ReadinessScriptLocation is the repository-relative readiness probe path used by tests.
	ReadinessScriptLocation = "tools/k8_probes/readinessProbe.sh"
	// LivenessScriptLocation is the repository-relative liveness probe path used by tests.
	LivenessScriptLocation = "tools/k8_probes/livenessProbe.sh"
	// StartupScriptLocation is the repository-relative startup probe path used by tests.
	StartupScriptLocation = "tools/k8_probes/startupProbe.sh"

	// S3AccessKey is the secret data key used by Splunk S3-backed tests.
	S3AccessKey = "s3_access_key"
	// S3SecretKey is the secret data key used by Splunk S3-backed tests.
	S3SecretKey = "s3_secret_key"
)

// GetMockPerCRConfigMap returns per cr configmap
func GetMockPerCRConfigMap(name string) corev1.ConfigMap {
	// Create S3 secret
	cfg := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test",
		},
		Data: map[string]string{
			"manualUpdate": "false",
		},
	}
	return cfg
}

// GetMockS3SecretKeys returns S3 secret keys
func GetMockS3SecretKeys(name string) corev1.Secret {
	accessKey := []byte{'1'}
	secretKey := []byte{'2'}

	// Create S3 secret
	s3Secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test",
		},
		Data: map[string][]byte{
			S3AccessKey: accessKey,
			S3SecretKey: secretKey,
		},
	}
	return s3Secret
}

// CreatePods creates or updates a pod and marks it as running for tests.
func CreatePods(t *testing.T, ctx context.Context, client splcommon.ControllerClient, crtype, name, namespace, image string) {
	stpod := &corev1.Pod{}
	namespacesName := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	err := client.Get(ctx, namespacesName, stpod)
	if err != nil && k8serrors.IsNotFound(err) {
		// create pod
		stpod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "splunk-operator",
					"app.kubernetes.io/component":  crtype,
					"app.kubernetes.io/name":       crtype,
					"app.kubernetes.io/part-of":    fmt.Sprintf("splunk-test-%s", crtype),
					"app.kubernetes.io/instance":   fmt.Sprintf("splunk-test-%s", crtype),
				},
				Annotations: map[string]string{
					"traffic.sidecar.istio.io/excludeOutboundPorts": "8089,8191,9997",
					"traffic.sidecar.istio.io/includeInboundPorts":  "8000",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "splunk",
						Image: image,
						Env: []corev1.EnvVar{
							{
								Name:  "test",
								Value: "test",
							},
						},
						Ports: []corev1.ContainerPort{
							{
								Name:          "http-splunkweb",
								HostPort:      0,
								ContainerPort: 8000,
								Protocol:      "TCP",
								HostIP:        "",
							},
							{
								Name:          "https-splunkd",
								HostPort:      0,
								ContainerPort: 8089,
								Protocol:      "TCP",
								HostIP:        "",
							},
						},
					},
				},
			},
		}
		// simulate create stateful set
		err := client.Create(ctx, stpod)
		if err != nil {
			t.Errorf("Unexpected create pod failed %v", err)
			debug.PrintStack()
		}
	} else if err != nil {
		t.Errorf("Unexpected erro while get pod  %v", err)
		debug.PrintStack()
	}
	if stpod.Spec.Containers[0].Image != image {
		stpod.Spec.Containers[0].Image = image
		err := client.Update(ctx, stpod)
		if err != nil {
			t.Errorf("Unexpected create pod failed %v", err)
			debug.PrintStack()
		}
	}

	// update statefulset
	stpod.Status.Phase = corev1.PodRunning
	stpod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Image: image,
			Name:  "splunk",
			Ready: true,
		},
	}
	err = client.Status().Update(ctx, stpod)
	if err != nil {
		t.Errorf("Unexpected update pod  %v", err)
		debug.PrintStack()
	}
}

// UpdateStatefulSetsInTest marks a StatefulSet as ready for tests.
func UpdateStatefulSetsInTest(t *testing.T, ctx context.Context, client splcommon.ControllerClient, replicas int32, name, namespace string) {
	stNamespacedName := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	statefulset := &appsv1.StatefulSet{}
	err := client.Get(ctx, stNamespacedName, statefulset)
	if err != nil {
		t.Errorf("Unexpected get cluster manager %v", err)
		debug.PrintStack()
	}
	// update statefulset
	statefulset.Status.ReadyReplicas = replicas
	statefulset.Status.Replicas = replicas
	statefulset.Status.CurrentReplicas = replicas
	statefulset.Status.AvailableReplicas = replicas
	err = client.Status().Update(ctx, statefulset)
	if err != nil {
		t.Errorf("Unexpected update statefulset  %v", err)
		debug.PrintStack()
	}
}

// MockPodExecReturnContext stores the return values for each podExec command
type MockPodExecReturnContext struct {
	StdOut string
	StdErr string
	Err    error
}

// MockPodExecClient mocks the PodExecClient
type MockPodExecClient struct {
	Client             splcommon.ControllerClient
	Cr                 splcommon.MetaObject
	TargetPodName      string
	WantCmdList        []string
	GotCmdList         []string
	MockReturnContexts map[string]*MockPodExecReturnContext
}

// AddMockPodExecReturnContext adds the MockPodExecReturnContext object for a command
func (client *MockPodExecClient) AddMockPodExecReturnContext(ctx context.Context, cmd string, mockPodExecReturnContext *MockPodExecReturnContext) {
	client.WantCmdList = append(client.WantCmdList, cmd)
	if client.MockReturnContexts == nil {
		client.MockReturnContexts = make(map[string]*MockPodExecReturnContext)
	}
	client.MockReturnContexts[cmd] = mockPodExecReturnContext
}

// AddMockPodExecReturnContexts adds mockPodExecReturnContexts for the corresponding commands
func (client *MockPodExecClient) AddMockPodExecReturnContexts(ctx context.Context, podExecCmds []string, mockPodExecReturnContexts ...*MockPodExecReturnContext) {
	for n := range mockPodExecReturnContexts {
		client.AddMockPodExecReturnContext(ctx, podExecCmds[n], mockPodExecReturnContexts[n])
	}
}

// GetMockPodExecReturnContextAndKey returns the mockPodExecReturnContext object and the corresponding command
func (client *MockPodExecClient) GetMockPodExecReturnContextAndKey(ctx context.Context, cmd string) (*MockPodExecReturnContext, string) {
	for key := range client.MockReturnContexts {
		if strings.Contains(cmd, key) {
			return client.MockReturnContexts[key], key
		}
	}
	return nil, ""
}

// CheckPodExecCommands method for MockPodExecClient checks if got commands are same as received commands
func (client *MockPodExecClient) CheckPodExecCommands(t *testing.T, testMethod string) {
	if len(client.GotCmdList) != len(client.WantCmdList) {
		t.Fatalf("%s got %d number of commands; want %d number of commands", testMethod, len(client.GotCmdList), len(client.WantCmdList))
	}
	for n := range client.GotCmdList {
		if client.GotCmdList[n] != client.WantCmdList[n] {
			t.Errorf("%s GotCmdList[%d]=%s, want %s;", testMethod, n, client.GotCmdList[n], client.WantCmdList[n])
		}
	}
}

// GetCR returns the CR from the MockPodExecClient
func (client *MockPodExecClient) GetCR() splcommon.MetaObject {
	return client.Cr
}

// SetCR sets the CR
func (client *MockPodExecClient) SetCR(cr splcommon.MetaObject) {
	client.Cr = cr
}

// RunPodExecCommand returns the dummy values for mockPodExecClient
func (client *MockPodExecClient) RunPodExecCommand(ctx context.Context, streamOptions *remotecommand.StreamOptions, baseCmd []string) (string, string, error) {

	var mockPodExecReturnContext *MockPodExecReturnContext = &MockPodExecReturnContext{}
	var command string
	// This is to prevent the crash in the case where streamOptions.Stdin is anything other than *strings.Reader
	// In most of the cases the base command will be /bin/sh but if it is something else, it can be reading from
	// a io.Reader pipe. For e.g. tarring a file, writing to a write pipe and then untarring it on the pod by reading
	// from the reader pipe.
	if baseCmd[0] == "/bin/sh" {
		var cmdStr string
		streamOptionsCmd := streamOptions.Stdin.(*strings.Reader)
		for i := 0; i < int(streamOptionsCmd.Size()); i++ {
			cmd, _, _ := streamOptionsCmd.ReadRune()
			cmdStr = cmdStr + string(cmd)
		}

		mockPodExecReturnContext, command = client.GetMockPodExecReturnContextAndKey(ctx, cmdStr)
		if mockPodExecReturnContext == nil {
			err := fmt.Errorf("mockPodExecReturnContext is nil")
			return "", "", err
		}
	}

	// check if the command is already added or not in the list of GotCmdList
	var found bool
	for i := range client.GotCmdList {
		if command == client.GotCmdList[i] {
			found = true
			break
		}
	}
	if !found {
		client.GotCmdList = append(client.GotCmdList, command)
	}

	return mockPodExecReturnContext.StdOut, mockPodExecReturnContext.StdErr, mockPodExecReturnContext.Err
}

// SetTargetPodName sets the targetPodName for MockPodExecClient
func (client *MockPodExecClient) SetTargetPodName(ctx context.Context, targetPodName string) {
	client.TargetPodName = targetPodName
}

// GetTargetPodName returns dummy target pod name for mockPodExecClient
func (client *MockPodExecClient) GetTargetPodName() string {
	return client.TargetPodName
}

// GetClient returns the ControllerClient from MockPodExecClient
func (client *MockPodExecClient) GetClient() splcommon.ControllerClient {
	return client.Client
}

// LoadFixture reads a JSON fixture file from testdata/fixtures relative to the
// package being tested and returns its compacted JSON string.
func LoadFixture(t *testing.T, filename string) string {
	t.Helper()
	path := filepath.Join("testdata", "fixtures", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to load fixture %s: %v", filename, err)
	}

	var compactJSON bytes.Buffer
	if err := json.Compact(&compactJSON, data); err != nil {
		t.Fatalf("Failed to compact JSON from fixture %s: %v", filename, err)
	}
	return compactJSON.String()
}

// NewFakeClientBuilder returns a fake client builder that preserves object GVKs
// and uses the classic object tracker for CR specs used in reconcile tests.
func NewFakeClientBuilder(scheme *runtime.Scheme) *fake.ClientBuilder {
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjectTracker(clienttesting.NewObjectTracker(
			scheme,
			serializer.NewCodecFactory(scheme).UniversalDecoder(),
		)).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				err := c.Get(ctx, key, obj, opts...)
				if err != nil {
					return err
				}
				gvk, err := apiutil.GVKForObject(obj, scheme)
				if err == nil {
					obj.GetObjectKind().SetGroupVersionKind(gvk)
				}
				return nil
			},
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				gvk := obj.GetObjectKind().GroupVersionKind()
				err := c.Create(ctx, obj, opts...)
				obj.GetObjectKind().SetGroupVersionKind(gvk)
				return err
			},
		})
}

// ConfigTester runs f, marshals the result and compares it to want (normalized).
func ConfigTester(t *testing.T, method string, f func() (interface{}, error), want string) {
	result, err := f()
	if err != nil {
		t.Errorf("%s returned error: %v", method, err)
	}

	marshalAndCompare(t, result, method, want)
}

// ConfigTester2 runs f, marshals the result and compares it to want (normalized).
func ConfigTester2(t *testing.T, method string, f func() (interface{}, error), want string) {
	result, err := f()
	if err != nil {
		t.Errorf("%s returned error: %v", method, err)
	}

	marshalAndCompare2(t, result, method, want)
}

func marshalAndCompare(t *testing.T, compare interface{}, method string, want string) {
	t.Helper()
	got, err := json.Marshal(compare)
	if err != nil {
		t.Errorf("%s failed to marshall", err)
	}

	require.JSONEq(t, normalizeGeneratedConfigJSON(t, want), normalizeGeneratedConfigJSON(t, string(got)))
}

func marshalAndCompare2(t *testing.T, compare interface{}, method string, want string) {
	t.Helper()
	got, err := json.Marshal(compare)
	if err != nil {
		t.Errorf("%s failed to marshall", err)
	}

	gotJSON := normalizeGeneratedConfigJSON(t, string(got))
	wantJSON := normalizeGeneratedConfigJSON(t, want)
	if gotJSON != wantJSON {
		t.Errorf("Method %s, got = %s;\nwant %s", method, got, want)
	}
	require.JSONEq(t, wantJSON, gotJSON)
}

func normalizeGeneratedConfigJSON(t *testing.T, data string) string {
	t.Helper()

	var value interface{}
	require.NoError(t, json.Unmarshal([]byte(data), &value))
	dropNilCreationTimestamp(value)

	normalized, err := json.Marshal(value)
	require.NoError(t, err)

	return string(normalized)
}

func dropNilCreationTimestamp(value interface{}) {
	switch typed := value.(type) {
	case map[string]interface{}:
		if creationTimestamp, ok := typed["creationTimestamp"]; ok && creationTimestamp == nil {
			delete(typed, "creationTimestamp")
		}
		for _, child := range typed {
			dropNilCreationTimestamp(child)
		}
	case []interface{}:
		for _, child := range typed {
			dropNilCreationTimestamp(child)
		}
	}
}

// SplunkDeletionTester exercises the deletion path for a CR and verifies the
// expected set of client calls.
func SplunkDeletionTester(t *testing.T, cr splcommon.MetaObject, delete func(splcommon.MetaObject, splcommon.ControllerClient) (bool, error)) {
	var component string
	switch cr.GetObjectKind().GroupVersionKind().Kind {
	case "Standalone":
		component = "standalone"
	case "LicenseManager":
		component = "license-manager"
	case "LicenseMaster":
		component = "license-master"
	case "SearchHeadCluster":
		component = "search-head"
	case "IndexerCluster":
		component = "indexer"
	case "ClusterManager":
		component = "cluster-manager"
	case "ClusterMaster":
		component = "cluster-master"
	case "MonitoringConsole":
		component = "monitoring-console"
	case "IngestorCluster":
		component = "ingestor"
	}

	labelsB := map[string]string{
		"app.kubernetes.io/instance": fmt.Sprintf("splunk-%s-%s", cr.GetName(), component),
	}

	listOptsB := []client.ListOption{
		client.InNamespace(cr.GetNamespace()),
		client.MatchingLabels(labelsB),
	}

	pvclist := corev1.PersistentVolumeClaimList{
		Items: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "splunk-pvc-stack1-var",
					Namespace: "test",
				},
			},
		},
	}
	mockCalls := make(map[string][]MockFuncCall)
	wantDeleted := false
	if cr.GetObjectMeta().GetDeletionTimestamp() != nil {
		wantDeleted = true
		apiVersion, _ := schema.ParseGroupVersion(enterpriseApi.APIVersion)
		if component == "cluster-master" || component == "license-master" {
			apiVersion, _ = schema.ParseGroupVersion("enterprise.splunk.com/v3")
		}
		mockCalls["Update"] = []MockFuncCall{
			{MetaName: fmt.Sprintf("*%s.%s-%s-%s", apiVersion.Version, cr.GetObjectKind().GroupVersionKind().Kind, cr.GetNamespace(), cr.GetName())},
		}
		if cr.GetObjectKind().GroupVersionKind().Kind != "IndexerCluster" {
			mockCalls["Update"] = []MockFuncCall{
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: fmt.Sprintf("*%s.%s-%s-%s", apiVersion.Version, cr.GetObjectKind().GroupVersionKind().Kind, cr.GetNamespace(), cr.GetName())},
			}
			mockCalls["Delete"] = []MockFuncCall{
				{MetaName: "*v1.PersistentVolumeClaim-test-splunk-pvc-stack1-var"},
			}
			mockCalls["List"] = []MockFuncCall{
				{ListOpts: listOptsB},
			}
			// account for extra calls in the shc case due to the deployer
			if component == "search-head" {
				labelsC := map[string]string{
					"app.kubernetes.io/instance": fmt.Sprintf("splunk-%s-%s", cr.GetName(), "deployer"),
				}
				listOptsC := []client.ListOption{
					client.InNamespace(cr.GetNamespace()),
					client.MatchingLabels(labelsC),
				}
				mockCalls["Delete"] = append(mockCalls["Delete"], MockFuncCall{MetaName: "*v1.PersistentVolumeClaim-test-splunk-pvc-stack1-var"})
				mockCalls["List"] = append(mockCalls["List"], MockFuncCall{ListOpts: listOptsC})
			}
			mockCalls["Get"] = []MockFuncCall{
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
			}
			mockCalls["Create"] = []MockFuncCall{
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
			}
			if component == "monitoring-console" {
				mockCalls["Create"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
				}
				mockCalls["Get"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
				}
				mockCalls["Update"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: fmt.Sprintf("*%s.%s-%s-%s", apiVersion.Version, cr.GetObjectKind().GroupVersionKind().Kind, cr.GetNamespace(), cr.GetName())},
				}
				mockCalls["Delete"] = []MockFuncCall{
					{MetaName: "*v1.PersistentVolumeClaim-test-splunk-pvc-stack1-var"},
				}
			}

			switch cr.GetObjectKind().GroupVersionKind().Kind {
			case "Standalone":
				mockCalls["Get"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-standalone-stack1-configmap"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.StatefulSet-test-splunk-stack1-standalone"},
					{MetaName: "*v4.Standalone-test-stack1"},
					{MetaName: "*v4.Standalone-test-stack1"},
				}
				mockCalls["Create"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-standalone-stack1-configmap"},
				}

			case "LicenseMaster":
				mockCalls["Get"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-license-master-stack1-configmap"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.StatefulSet-test-splunk-stack1-license-master"},
					{MetaName: "*v3.LicenseMaster-test-stack1"},
					{MetaName: "*v3.LicenseMaster-test-stack1"},
				}
				mockCalls["Create"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-license-master-stack1-configmap"},
				}

			case "LicenseManager":
				mockCalls["Get"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-license-manager-stack1-configmap"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.StatefulSet-test-splunk-stack1-license-manager"},
					{MetaName: "*v4.LicenseManager-test-stack1"},
					{MetaName: "*v4.LicenseManager-test-stack1"},
				}
				mockCalls["Create"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-license-manager-stack1-configmap"},
				}

			case "SearchHeadCluster":
				mockCalls["Get"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-search-head-stack1-configmap"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},
					{MetaName: "*v4.SearchHeadCluster-test-stack1"},
					{MetaName: "*v4.SearchHeadCluster-test-stack1"},
				}
				mockCalls["Create"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-search-head-stack1-configmap"},
				}

			case "IndexerCluster":
				mockCalls["Create"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-indexer-stack1-configmap"},
				}

			case "ClusterManager":
				mockCalls["Get"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-cluster-manager-stack1-configmap"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-manager"},
					{MetaName: "*v4.ClusterManager-test-stack1"},
					{MetaName: "*v4.ClusterManager-test-stack1"},
				}
				mockCalls["Create"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-cluster-manager-stack1-configmap"},
				}

				listOptsTest := []client.ListOption{
					client.InNamespace(cr.GetNamespace()),
				}

				mockCalls["List"] = append(mockCalls["List"], []MockFuncCall{
					{ListOpts: listOptsTest},
					{ListOpts: listOptsTest},
					{ListOpts: listOptsTest},
					{ListOpts: listOptsTest},
				}...)
				mockCalls["List"][0], mockCalls["List"][len(mockCalls["List"])-1] = mockCalls["List"][len(mockCalls["List"])-1], mockCalls["List"][0]
			case "ClusterMaster":
				mockCalls["Get"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-cluster-master-stack1-configmap"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-master"},
					{MetaName: "*v3.ClusterMaster-test-stack1"},
					{MetaName: "*v3.ClusterMaster-test-stack1"},
				}
				mockCalls["Create"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-cluster-master-stack1-configmap"},
				}
			case "MonitoringConsole":
				mockCalls["Get"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-monitoring-console-stack1-configmap"},
					{MetaName: "*v4.MonitoringConsole-test-stack1"},
					{MetaName: "*v4.MonitoringConsole-test-stack1"},
				}
				mockCalls["Create"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-monitoring-console-stack1-configmap"},
				}
			}
		} else {
			mockCalls["Update"] = []MockFuncCall{
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: fmt.Sprintf("*%s.%s-%s-%s", apiVersion.Version, cr.GetObjectKind().GroupVersionKind().Kind, cr.GetNamespace(), cr.GetName())},
			}
			mockCalls["Delete"] = []MockFuncCall{
				{MetaName: "*v1.PersistentVolumeClaim-test-splunk-pvc-stack1-var"},
			}
			mockCalls["List"] = []MockFuncCall{
				{ListOpts: listOptsB},
			}
			mockCalls["Create"] = []MockFuncCall{
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
			}
			mockCalls["Get"] = []MockFuncCall{
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v4.ClusterManager-test-manager1"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.StatefulSet-test-splunk-stack1-indexer"},
				{MetaName: "*v4.IndexerCluster-test-stack1"},
				{MetaName: "*v4.IndexerCluster-test-stack1"},
			}
			switch cr.GetObjectKind().GroupVersionKind().Kind {
			case "IndexerCluster":
				mockCalls["Create"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-indexer-stack1-configmap"},
				}
				mockCalls["Get"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-indexer-stack1-configmap"},
					{MetaName: "*v4.ClusterManager-test-manager1"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.StatefulSet-test-splunk-stack1-indexer"},
					{MetaName: "*v4.IndexerCluster-test-stack1"},
					{MetaName: "*v4.IndexerCluster-test-stack1"},
				}
			case "IngestorCluster":
				mockCalls["Create"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-ingestor-stack1-configmap"},
				}
				mockCalls["Get"] = []MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-ingestor-stack1-configmap"},
					{MetaName: "*v4.IngestorCluster-test-stack1"},
					{MetaName: "*v4.IngestorCluster-test-stack1"},
				}
			}
		}
	}

	c := NewMockClient()
	c.ListObj = &pvclist
	var err error
	deleted, err := delete(cr, c)
	if deleted != wantDeleted || err != nil {
		t.Errorf("k8sops.CheckForDeletion() returned %t, %v; want %t, nil", deleted, err, wantDeleted)
	}
	c.CheckCalls(t, "Testk8sops.CheckForDeletion", mockCalls)
}
