// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"testing"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/remotecommand"
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
			"s3_access_key": accessKey,
			"s3_secret_key": secretKey,
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
