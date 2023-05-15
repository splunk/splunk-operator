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

package util

import (
	"context"
	"errors"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/flowcontrol"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

// Faking APIs

// Faking Rest GetConfig()
var fakePodExecGetConfig = func() (*rest.Config, error) {
	restConfig := rest.Config{}
	return &restConfig, errors.New("fakeerror")
}

// Faking RESTClientForGVK
var fakePodExecRESTClientForGVK = func(gvk schema.GroupVersionKind, isUnstructured bool, baseConfig *rest.Config, codecs serializer.CodecFactory) (rest.Interface, error) {
	return &fakeRestInterface{}, errors.New("fakeerror")
}

type fakeRestInterface struct {
	name string
}

func (fri fakeRestInterface) GetRateLimiter() flowcontrol.RateLimiter {
	return flowcontrol.NewFakeAlwaysRateLimiter()
}

func (fri fakeRestInterface) Verb(verb string) *rest.Request {
	return &rest.Request{}
}

func (fri fakeRestInterface) Post() *rest.Request {
	return &rest.Request{}
}

func (fri fakeRestInterface) Put() *rest.Request {
	return &rest.Request{}
}

func (fri fakeRestInterface) Patch(pt types.PatchType) *rest.Request {
	return &rest.Request{}
}

func (fri fakeRestInterface) Get() *rest.Request {
	return &rest.Request{}
}

func (fri fakeRestInterface) Delete() *rest.Request {
	return &rest.Request{}
}

func (fri fakeRestInterface) APIVersion() schema.GroupVersion {
	return schema.GroupVersion{}
}

// Faking SPDY executor

var fakePodExecNewSPDYExecutor = func(config *rest.Config, method string, url *url.URL) (remotecommand.Executor, error) {
	return &fakeExecutor{}, errors.New("FakeError")
}

type fakeExecutor struct{}

func (f *fakeExecutor) Stream(options remotecommand.StreamOptions) error {
	return nil
}

func (f *fakeExecutor) StreamWithContext(ctx context.Context, options remotecommand.StreamOptions) error {
	return nil
}

func TestCreateResource(t *testing.T) {
	ctx := context.TODO()
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret",
			Namespace: "test",
		},
		Data: map[string][]byte{"one": []byte("value1")},
	}

	c := spltest.NewMockClient()
	err := CreateResource(ctx, c, &secret)
	if err != nil {
		t.Errorf("CreateResource() returned %v; want nil", err)
	}
	c.CheckCalls(t, "TestCreateResource", map[string][]spltest.MockFuncCall{
		"Create": {
			{CTX: context.TODO(), Obj: &secret},
		},
	})
}

func TestUpdateResource(t *testing.T) {
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret",
			Namespace: "test",
		},
		Data: map[string][]byte{"one": []byte("value1")},
	}

	c := spltest.NewMockClient()
	err := UpdateResource(context.TODO(), c, &secret)
	if err != nil {
		t.Errorf("UpdateResource() returned %v; want nil", err)
	}
	c.CheckCalls(t, "TestUpdateResource", map[string][]spltest.MockFuncCall{
		"Update": {
			{CTX: context.TODO(), Obj: &secret},
		},
	})
}

func TestDeleteResource(t *testing.T) {
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret",
			Namespace: "test",
		},
		Data: map[string][]byte{"one": []byte("value1")},
	}

	c := spltest.NewMockClient()
	err := DeleteResource(context.TODO(), c, &secret)
	if err != nil {
		t.Errorf("DeleteResource() returned %v; want nil", err)
	}
	c.CheckCalls(t, "TestUpdateResource", map[string][]spltest.MockFuncCall{
		"Delete": {
			{CTX: context.TODO(), Obj: &secret},
		},
	})
}

func TestDeepCopyInto(t *testing.T) {
	cr := TestResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	copy := cr.DeepCopy()

	if copy.Name != cr.Name {
		t.Errorf("TestResource copy.Name = %s; want %s", copy.Name, cr.Name)
	}

	if copy.Namespace != cr.Namespace {
		t.Errorf("TestResource copy.Namespace = %s; want %s", copy.Namespace, cr.Namespace)
	}
}

func TestDeepCopyObject(t *testing.T) {
	cr := TestResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	copy := cr.DeepCopyObject()

	if !reflect.DeepEqual(copy, &cr) {
		t.Errorf("TestResource \n got = %+v; \n want %+v \n", copy, cr)
	}

	crPtr := &cr
	crPtr = nil
	copy = crPtr.DeepCopyObject()
}

func TestDeepCopy(t *testing.T) {
	var cr *TestResource
	cr = nil
	_ = cr.DeepCopy()
}

func TestPodExecCommand(t *testing.T) {
	ctx := context.TODO()
	// Create pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-0",
			Namespace: "test",
			Labels: map[string]string{
				"controller-revision-hash": "v0",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					VolumeMounts: []corev1.VolumeMount{
						{
							MountPath: "/mnt/splunk-secrets",
							Name:      "mnt-splunk-secrets",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "mnt-splunk-secrets",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "test-secret",
						},
					},
				},
			},
		},
	}

	// Create client and add object
	c := spltest.NewMockClient()
	streamOptions := &remotecommand.StreamOptions{
		Stdin: strings.NewReader("ls -ltr"),
	}

	mockKubPath := os.Getenv("PWD") + "/kubeconfig"
	_, _, _ = PodExecCommand(ctx, c, "splunk-stack1-0", "test", []string{"/bin/sh"}, streamOptions, false, true, mockKubPath)

	// Add object
	c.AddObject(pod)
	_, _, _ = PodExecCommand(ctx, c, "splunk-stack1-0", "test", []string{"/bin/sh"}, streamOptions, false, true, mockKubPath)

	// Hit some error legs
	_, _, _ = PodExecCommand(ctx, c, "splunk-stack1-0", "test", []string{"/bin/sh"}, streamOptions, false, false, "")

	// Negative testing
	streamOptions.Stdin = nil
	_, _, err := PodExecCommand(ctx, c, "splunk-stack1-0", "test", []string{"/bin/sh"}, streamOptions, false, false, "")
	if err == nil {
		t.Errorf("Expected error")
	}

	_, _, err = PodExecCommand(ctx, c, "splunk-stack1-0", "test", []string{"/bin/sh"}, streamOptions, false, true, "fakepath")
	if err == nil {
		t.Errorf("Expected error")
	}

	// Faking functions

	// Faking GetConfig()
	podExecGetConfig = fakePodExecGetConfig
	_, _, err = PodExecCommand(ctx, c, "splunk-stack1-0", "test", []string{"/bin/sh"}, streamOptions, false, false, "")
	if err == nil {
		t.Errorf("Expected error")
	}

	podExecGetConfig = config.GetConfig

	// Faking RestClientForGVK
	podExecRESTClientForGVK = fakePodExecRESTClientForGVK
	_, _, err = PodExecCommand(ctx, c, "splunk-stack1-0", "test", []string{"/bin/sh"}, streamOptions, false, false, "")
	if err == nil {
		t.Errorf("Expected error")
	}

	// Faking SPDY executor
	podExecRESTClientForGVK = apiutil.RESTClientForGVK
	podExecNewSPDYExecutor = fakePodExecNewSPDYExecutor
	_, _, err = PodExecCommand(ctx, c, "splunk-stack1-0", "test", []string{"/bin/sh"}, streamOptions, false, false, "")
	if err == nil {
		t.Errorf("Expected error")
	}
}

func TestRunPodExecCommand(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "clusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	targetPodName := "splunk-cm-cluster-manager-0"
	podExecClient := GetPodExecClient(c, &cr, targetPodName)
	dummyCmd := "dummyCmd"
	streamOptions := &remotecommand.StreamOptions{
		Stdin: strings.NewReader(dummyCmd),
	}
	stdOut, stdErr, _ := podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
	if stdOut != "" && stdErr != "" {
		t.Errorf("expected stdOut and stdErr to be empty since it is a dummy podExec call")
	}
}

func TestNewStreamOptionsObject(t *testing.T) {
	command := "dummyCmd"

	streamOptions := NewStreamOptionsObject(command)
	var gotCmd string
	streamOptionsCmd := streamOptions.Stdin.(*strings.Reader)
	for i := 0; i < int(streamOptionsCmd.Size()); i++ {
		cmd, _, _ := streamOptionsCmd.ReadRune()
		gotCmd = gotCmd + string(cmd)
	}

	if gotCmd != command {
		t.Errorf("got invalid command, expected: %s, got %s", command, gotCmd)
	}
}

func TestGetSetTargetPodName(t *testing.T) {
	ctx := context.TODO()
	podName := "pod-0"

	var podExecClient PodExecClient = PodExecClient{}

	podExecClient.SetTargetPodName(ctx, podName)

	gotPodName := podExecClient.GetTargetPodName()
	if gotPodName != podName {
		t.Errorf("invalid targetPodName, expected: %s, got: %s", podName, gotPodName)
	}
}

func TestGetSetCR(t *testing.T) {
	cr := TestResource{}

	var podExecClient PodExecClient = PodExecClient{}
	podExecClient.SetCR(&cr)

	if podExecClient.GetCR() != &cr {
		t.Errorf("Retrieved CR is not the same")
	}
}

func TestResetStringReader(t *testing.T) {
	sop := remotecommand.StreamOptions{
		Tty: true,
	}
	ResetStringReader(&sop, "testcommand")

	sop = remotecommand.StreamOptions{
		Tty:   true,
		Stdin: strings.NewReader("test"),
	}
	ResetStringReader(&sop, "testcommand")
}

func TestSuppressHarmlessErrorMessages(t *testing.T) {
	var finalStr string

	// Create string containing all suppression strings
	for _, replStr := range splunkCliSuppressionStrings {
		finalStr += replStr
	}

	// Test replacement of strings
	suppressHarmlessErrorMessages(&finalStr)
	if finalStr != "" {
		t.Errorf("Known messages did not get suppressed.")
	}
}
