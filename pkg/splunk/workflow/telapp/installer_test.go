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

package telapp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetTelAppNameExtension(t *testing.T) {
	crKinds := map[string]string{
		"Standalone":        "stdaln",
		"LicenseMaster":     "lmaster",
		"LicenseManager":    "lmanager",
		"SearchHeadCluster": "shc",
		"ClusterMaster":     "cmaster",
		"ClusterManager":    "cmanager",
		"IngestorCluster":   "ingestor",
	}

	for kind, expectedExtension := range crKinds {
		extension, _ := getTelAppNameExtension(kind)
		if expectedExtension != extension {
			t.Errorf("Invalid extension crkind %v, extension %v", kind, expectedExtension)
		}
	}

	_, err := getTelAppNameExtension("incorrect value")
	if err == nil {
		t.Errorf("Expected error")
	}
}

func TestRedactSplunkAuthRedactsRawAndShellQuotedPassword(t *testing.T) {
	adminPwd := "a'b"
	cmd := fmt.Sprintf("raw=%s quoted=%s", adminPwd, shellQuote(adminPwd))

	redacted := redactSplunkAuth(cmd, adminPwd)

	if strings.Contains(redacted, adminPwd) {
		t.Errorf("redacted command contains raw password: %s", redacted)
	}
	if strings.Contains(redacted, shellQuote(adminPwd)) {
		t.Errorf("redacted command contains shell-quoted password: %s", redacted)
	}
	if strings.Count(redacted, "****") != 2 {
		t.Errorf("redacted command = %s; want both password forms redacted", redacted)
	}
}

func TestRedactSplunkAuthPreservesCommandWithEmptyPassword(t *testing.T) {
	cmd := "splunk list app"

	redacted := redactSplunkAuth(cmd, "")

	if redacted != cmd {
		t.Errorf("redactSplunkAuth() = %s; want %s", redacted, cmd)
	}
}

func TestAddTelAppCMaster(t *testing.T) {
	ctx := context.TODO()

	mockClient := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, mockClient, "")
	if err != nil {
		t.Fatalf("failed to create namespace-scoped secret: %v", err)
	}

	cmCr := &enterpriseApiV3.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
	}

	shcCr := &enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
	}

	podExecCommands := []string{
		fmt.Sprintf(createTelAppNonShcString, telAppConfString, telAppDefMetaConfString),
		"curl -k -u admin:",
	}

	mockPodExecReturnContexts := []*spltest.MockPodExecReturnContext{
		{
			StdOut: "",
		},
		{
			StdOut: "",
		},
	}

	mockPodExecClient := &spltest.MockPodExecClient{Cr: cmCr, Client: mockClient}
	mockPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnContexts...)

	err = AddTelApp(ctx, mockPodExecClient, 1, cmCr)
	if err != nil {
		t.Errorf("Tel app not added successfully, error: %v", err)
	}

	podExecCommands = []string{
		fmt.Sprintf(createTelAppShcString, shcAppsLocationOnDeployer, shcAppsLocationOnDeployer, telAppConfString, shcAppsLocationOnDeployer, telAppDefMetaConfString, shcAppsLocationOnDeployer),
		fmt.Sprintf("/opt/splunk/bin/splunk apply shcluster-bundle -target https://%s:8089 -auth admin:", getSplunkStatefulsetURL(shcCr.GetNamespace(), splunkSearchHead, shcCr.GetName(), 0, false)),
	}

	mockPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnContexts...)
	mockPodExecClient.Cr = shcCr

	err = AddTelApp(ctx, mockPodExecClient, 1, shcCr)
	if err != nil {
		t.Errorf("Tel app not added successfully, error: %v", err)
	}

	podExecCommandsError := []string{
		fmt.Sprintf(createTelAppNonShcString, telAppConfString, telAppDefMetaConfString),
	}

	mockPodExecReturnContextsError := []*spltest.MockPodExecReturnContext{
		{
			StdOut: "",
		},
	}

	mockPodExecClientError1 := &spltest.MockPodExecClient{Cr: cmCr, Client: mockClient}
	mockPodExecClientError1.AddMockPodExecReturnContexts(ctx, podExecCommandsError, mockPodExecReturnContextsError...)

	err = AddTelApp(ctx, mockPodExecClientError1, 1, cmCr)
	if err == nil {
		t.Errorf("Expected error")
	}

	podExecCommandsError = []string{
		fmt.Sprintf(createTelAppNonShcString, telAppConfString, telAppDefMetaConfString),
	}
	mockPodExecClientError2 := &spltest.MockPodExecClient{Cr: cmCr, Client: mockClient}
	mockPodExecClientError2.AddMockPodExecReturnContexts(ctx, podExecCommandsError, mockPodExecReturnContextsError...)

	err = AddTelApp(ctx, mockPodExecClientError2, 1, cmCr)
	if err == nil {
		t.Errorf("Expected error")
	}

	podExecCommandsError = []string{
		fmt.Sprintf(createTelAppShcString, shcAppsLocationOnDeployer, shcAppsLocationOnDeployer, telAppConfString, shcAppsLocationOnDeployer, telAppDefMetaConfString, shcAppsLocationOnDeployer),
	}

	mockPodExecClientError3 := &spltest.MockPodExecClient{Cr: shcCr, Client: mockClient}
	mockPodExecClientError3.AddMockPodExecReturnContexts(ctx, podExecCommandsError, mockPodExecReturnContextsError...)

	err = AddTelApp(ctx, mockPodExecClientError3, 1, shcCr)
	if err == nil {
		t.Errorf("Expected error")
	}

	podExecCommandsError = []string{
		fmt.Sprintf(createTelAppShcString, shcAppsLocationOnDeployer, shcAppsLocationOnDeployer, telAppConfString, shcAppsLocationOnDeployer, telAppDefMetaConfString, shcAppsLocationOnDeployer),
	}
	mockPodExecClientError4 := &spltest.MockPodExecClient{Cr: shcCr, Client: mockClient}
	mockPodExecClientError4.AddMockPodExecReturnContexts(ctx, podExecCommandsError, mockPodExecReturnContextsError...)

	err = AddTelApp(ctx, mockPodExecClientError4, 1, shcCr)
	if err == nil {
		t.Errorf("Expected error")
	}
}

func TestAddTelAppCManager(t *testing.T) {
	ctx := context.TODO()
	mockClient := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, mockClient, "")
	if err != nil {
		t.Fatalf("failed to create namespace-scoped secret: %v", err)
	}

	cmCr := &enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
	}

	shcCr := &enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
	}

	podExecCommands := []string{
		fmt.Sprintf(createTelAppNonShcString, telAppConfString, telAppDefMetaConfString),
		"curl -k -u admin:",
	}

	mockPodExecReturnContexts := []*spltest.MockPodExecReturnContext{
		{
			StdOut: "",
		},
		{
			StdOut: "",
		},
	}

	mockPodExecClient := &spltest.MockPodExecClient{Cr: cmCr, Client: mockClient}
	mockPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnContexts...)

	err = AddTelApp(ctx, mockPodExecClient, 1, cmCr)
	if err != nil {
		t.Errorf("Tel app not added successfully, error: %v", err)
	}

	podExecCommands = []string{
		fmt.Sprintf(createTelAppShcString, shcAppsLocationOnDeployer, shcAppsLocationOnDeployer, telAppConfString, shcAppsLocationOnDeployer, telAppDefMetaConfString, shcAppsLocationOnDeployer),
		fmt.Sprintf("/opt/splunk/bin/splunk apply shcluster-bundle -target https://%s:8089 -auth admin:", getSplunkStatefulsetURL(shcCr.GetNamespace(), splunkSearchHead, shcCr.GetName(), 0, false)),
	}

	mockPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnContexts...)
	mockPodExecClient.Cr = shcCr

	err = AddTelApp(ctx, mockPodExecClient, 1, shcCr)
	if err != nil {
		t.Errorf("Tel app not added successfully, error: %v", err)
	}

	podExecCommandsError := []string{
		fmt.Sprintf(createTelAppNonShcString, telAppConfString, telAppDefMetaConfString),
	}

	mockPodExecReturnContextsError := []*spltest.MockPodExecReturnContext{
		{
			StdOut: "",
		},
	}

	mockPodExecClientError1 := &spltest.MockPodExecClient{Cr: cmCr, Client: mockClient}
	mockPodExecClientError1.AddMockPodExecReturnContexts(ctx, podExecCommandsError, mockPodExecReturnContextsError...)

	err = AddTelApp(ctx, mockPodExecClientError1, 1, cmCr)
	if err == nil {
		t.Errorf("Expected error")
	}

	podExecCommandsError = []string{
		fmt.Sprintf(createTelAppNonShcString, telAppConfString, telAppDefMetaConfString),
	}
	mockPodExecClientError2 := &spltest.MockPodExecClient{Cr: cmCr, Client: mockClient}
	mockPodExecClientError2.AddMockPodExecReturnContexts(ctx, podExecCommandsError, mockPodExecReturnContextsError...)

	err = AddTelApp(ctx, mockPodExecClientError2, 1, cmCr)
	if err == nil {
		t.Errorf("Expected error")
	}

	podExecCommandsError = []string{
		fmt.Sprintf(createTelAppShcString, shcAppsLocationOnDeployer, shcAppsLocationOnDeployer, telAppConfString, shcAppsLocationOnDeployer, telAppDefMetaConfString, shcAppsLocationOnDeployer),
	}

	mockPodExecClientError3 := &spltest.MockPodExecClient{Cr: shcCr, Client: mockClient}
	mockPodExecClientError3.AddMockPodExecReturnContexts(ctx, podExecCommandsError, mockPodExecReturnContextsError...)

	err = AddTelApp(ctx, mockPodExecClientError3, 1, shcCr)
	if err == nil {
		t.Errorf("Expected error")
	}

	podExecCommandsError = []string{
		fmt.Sprintf(createTelAppShcString, shcAppsLocationOnDeployer, shcAppsLocationOnDeployer, telAppConfString, shcAppsLocationOnDeployer, telAppDefMetaConfString, shcAppsLocationOnDeployer),
	}
	mockPodExecClientError4 := &spltest.MockPodExecClient{Cr: shcCr, Client: mockClient}
	mockPodExecClientError4.AddMockPodExecReturnContexts(ctx, podExecCommandsError, mockPodExecReturnContextsError...)

	err = AddTelApp(ctx, mockPodExecClientError4, 1, shcCr)
	if err == nil {
		t.Errorf("Expected error")
	}

	crNew := enterpriseApi.MonitoringConsole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mc",
			Namespace: "test",
		},
	}
	AddTelApp(ctx, mockPodExecClient, 2, &crNew)
}
