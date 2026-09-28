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

package clustermanager

import (
	"context"
	"fmt"
	"testing"
	"time"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestResetSymbolicLinks(t *testing.T) {
	ctx := context.TODO()
	mockPodExecClient := &spltest.MockPodExecClient{}

	// Test CM
	cmCr := enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
	}

	podExecCommands := []string{
		splcommon.SetSymbolicLinkClusterManager,
	}
	mockPodExecReturnCtxts := []*spltest.MockPodExecReturnContext{
		{
			StdOut: "",
			StdErr: "",
		},
	}

	mockPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnCtxts...)

	// CM should pass
	err := resetSymbolicLinks(ctx, &cmCr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Didn't expect error, err %v", err)
	}

	// ClusterMaster should pass
	clusterMasterCr := enterpriseApiV3.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
	}
	clusterMasterPodExecClient := &spltest.MockPodExecClient{}
	clusterMasterPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnCtxts...)
	err = resetSymbolicLinks(ctx, &clusterMasterCr, 1, clusterMasterPodExecClient)
	if err != nil {
		t.Errorf("Didn't expect error for ClusterMaster, err %v", err)
	}

	// Invalid CR test
	lmCr := enterpriseApi.LicenseManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lm",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "LicenseManager",
		},
	}

	err = resetSymbolicLinks(ctx, &lmCr, 1, mockPodExecClient)
	if err == nil {
		t.Errorf("Expected error")
	}
}

func TestPerformCmBundlePush(t *testing.T) {
	ctx := context.TODO()
	current := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	client := spltest.NewMockClient()

	current.Status.BundlePushTracker.NeedToPushManagerApps = true
	current.Status.BundlePushTracker.LastCheckInterval = time.Now().Unix() - 1
	if err := performCmBundlePush(ctx, client, &current, nil); err == nil {
		t.Errorf("performCmBundlePush() should fail if attempted within 5 seconds interval")
	}

	current.Status.BundlePushTracker.NeedToPushManagerApps = false
	if err := performCmBundlePush(ctx, client, &current, nil); err != nil {
		t.Errorf("performCmBundlePush() should not return an error when bundle push is not required: %v", err)
	}
}

func TestPerformCmBundlePushTargetsClusterManagerPod(t *testing.T) {
	ctx := context.TODO()
	current := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	current.Status.BundlePushTracker.NeedToPushManagerApps = true
	current.Status.BundlePushTracker.LastCheckInterval = time.Now().Unix() - 10

	client := spltest.NewMockClient()
	smartstoreConfigMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-clustermanager-smartstore",
			Namespace: "test",
		},
		Data: map[string]string{splcommon.ConfigToken: "current-token"},
	}
	if _, err := k8sops.ApplyConfigMap(ctx, client, &smartstoreConfigMap); err != nil {
		t.Fatal(err)
	}

	command := fmt.Sprintf("cat /mnt/splunk-operator/local/%s", splcommon.ConfigToken)
	podExecClient := &spltest.MockPodExecClient{TargetPodName: "stale-pod"}
	podExecClient.AddMockPodExecReturnContext(ctx, command, &spltest.MockPodExecReturnContext{StdOut: "stale-token"})

	if err := performCmBundlePush(ctx, client, &current, podExecClient); err == nil {
		t.Fatal("performCmBundlePush() should return an error when the config token has not propagated")
	}

	wantPodName := "splunk-stack1-cluster-manager-0"
	if got := podExecClient.GetTargetPodName(); got != wantPodName {
		t.Errorf("performCmBundlePush() target pod = %q, want %q", got, wantPodName)
	}
}

func TestPushManagerAppsBundle(t *testing.T) {
	ctx := context.TODO()
	current := enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	client := spltest.NewMockClient()

	if err := pushManagerAppsBundle(ctx, client, &current); err == nil {
		t.Fatal("pushManagerAppsBundle() should fail when the namespace-scoped secret is missing")
	}

	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      splcommon.GetNamespaceScopedSecretName("test"),
			Namespace: "test",
		},
		Data: map[string][]byte{},
	}
	if _, err := k8sops.ApplySecret(ctx, client, &secret); err != nil {
		t.Fatal(err)
	}

	if err := pushManagerAppsBundle(ctx, client, &current); err == nil {
		t.Fatal("pushManagerAppsBundle() should fail when the admin password is missing")
	}
}

func TestCheckIfsmartstoreConfigMapUpdatedToPod(t *testing.T) {
	ctx := context.TODO()
	cm := enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
	}

	c := spltest.NewMockClient()
	podExecCommands := []string{
		fmt.Sprintf("cat /mnt/splunk-operator/local/%s", splcommon.ConfigToken),
	}

	smartstoreConfigMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-clustermanager-smartstore",
			Namespace: "test",
		},
		Data: map[string]string{splcommon.ConfigToken: ""},
	}

	mockPodExecReturnContexts := []*spltest.MockPodExecReturnContext{
		{
			StdOut: "",
			StdErr: "",
			Err:    fmt.Errorf("dummy error"),
		},
	}

	mockPodExecClient := &spltest.MockPodExecClient{}
	mockPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnContexts...)

	err := checkIfSmartstoreConfigMapUpdatedToPod(ctx, c, &cm, mockPodExecClient)
	if err == nil {
		t.Errorf("checkIfSmartstoreConfigMapUpdatedToPod() should have returned error")
	}

	mockPodExecReturnContexts[0].Err = nil
	err = checkIfSmartstoreConfigMapUpdatedToPod(ctx, c, &cm, mockPodExecClient)
	if err == nil {
		t.Errorf("checkIfSmartstoreConfigMapUpdatedToPod() should have returned error since we did not add configMap yet")
	}

	c.AddObject(&smartstoreConfigMap)
	err = checkIfSmartstoreConfigMapUpdatedToPod(ctx, c, &cm, mockPodExecClient)
	if err != nil {
		t.Errorf("checkIfSmartstoreConfigMapUpdatedToPod() should not have returned error; err=%v", err)
	}

	mockPodExecClient.CheckPodExecCommands(t, "checkIfSmartstoreConfigMapUpdatedToPod")
}
