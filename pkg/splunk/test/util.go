// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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
	"fmt"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/remotecommand"
)

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

// MockPodExecClient to mock the PodExecClient
type MockPodExecClient struct {
	StdOut string
	StdErr string
	Err    error
}

// MockPodExecClientHandler handles the MockPodExecClient
type MockPodExecClientHandler struct {
	WantMockPodExecClient []*MockPodExecClient
	GotMockPodExecClient  []*MockPodExecClient
	MockClients           map[string]*MockPodExecClient
}

// AddPodExecClient adds the podExecClient object for a command
func (handler *MockPodExecClientHandler) AddPodExecClient(cmd string, mockPodExecClient *MockPodExecClient) {
	handler.WantMockPodExecClient = append(handler.WantMockPodExecClient, mockPodExecClient)
	if handler.MockClients == nil {
		handler.MockClients = make(map[string]*MockPodExecClient)
	}
	handler.MockClients[cmd] = mockPodExecClient
}

// AddPodExecClients adds podExecClients for the corresponding commands
func (handler *MockPodExecClientHandler) AddPodExecClients(podExecCmds []string, mockPodExecClients ...*MockPodExecClient) {
	for n := range mockPodExecClients {
		handler.AddPodExecClient(podExecCmds[n], mockPodExecClients[n])
	}
}

// GetMockPodExecClient returns the mockPodExecClient object for the corresponding command
func (handler *MockPodExecClientHandler) GetMockPodExecClient(cmd string) *MockPodExecClient {
	for key := range handler.MockClients {
		if strings.Contains(cmd, key) {
			return handler.MockClients[key]
		}
	}
	return nil
}

// CheckPodExecClients method for MockPodExecClientHandler checks if podExecClient fields received matches fields that we want
func (handler *MockPodExecClientHandler) CheckPodExecClients(t *testing.T, testMethod string) {
	if len(handler.GotMockPodExecClient) != len(handler.WantMockPodExecClient) {
		t.Fatalf("%s got %d number of mockPodExecClients; want %d number of mockPodExecClients", testMethod, len(handler.GotMockPodExecClient), len(handler.WantMockPodExecClient))
	}
	for n := range handler.GotMockPodExecClient {
		if !reflect.DeepEqual(handler.GotMockPodExecClient[n], handler.WantMockPodExecClient[n]) {
			t.Errorf("%s GotMockPodExecClient.StdOut[%d]=%s, want %s; GotMockPodExecClient.StdErr[%d]=%s; want %s",
				testMethod, n, handler.GotMockPodExecClient[n].StdOut, handler.WantMockPodExecClient[n].StdOut, n, handler.GotMockPodExecClient[n].StdErr, handler.WantMockPodExecClient[n].StdErr)
		}
	}
}

// RunPodExecCommand returns the dummy values for mockPodExecClient
func (handler *MockPodExecClientHandler) RunPodExecCommand(streamOptions *remotecommand.StreamOptions, baseCmd []string) (string, string, error) {

	var mockPodExecClient *MockPodExecClient
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

		mockPodExecClient = handler.GetMockPodExecClient(cmdStr)
		if mockPodExecClient == nil {
			err := fmt.Errorf("mockPodExecClient is nil")
			return "", "", err
		}
	} else {
		// use a dummy mockPodExecClient
		mockPodExecClient = &MockPodExecClient{}
	}
	// check if the mockPodExecClient is already added or not in the list of mockPodExecClients
	var found bool
	for i := range handler.GotMockPodExecClient {
		if reflect.DeepEqual(handler.GotMockPodExecClient[i], mockPodExecClient) {
			found = true
			break
		}
	}
	if !found {
		handler.GotMockPodExecClient = append(handler.GotMockPodExecClient, mockPodExecClient)
	}

	return mockPodExecClient.StdOut, mockPodExecClient.StdErr, mockPodExecClient.Err
}

// SetTargetPodName is a dummy function for mockPodExecClient
func (handler *MockPodExecClientHandler) SetTargetPodName(targetPodName string) {}

// GetTargetPodName returns dummy target pod name for mockPodExecClient
func (handler *MockPodExecClientHandler) GetTargetPodName() string {
	return ""
}
