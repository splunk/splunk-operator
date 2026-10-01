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

package clustermaster

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPerformCmasterBundlePush(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	ctx := context.TODO()
	current := enterpriseApiV3.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApiV3.ClusterMasterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	client := spltest.NewMockClient()

	// When the secret object is not present, should return an error
	current.Status.BundlePushTracker.NeedToPushMasterApps = true
	err := performCmasterBundlePush(ctx, client, &current)
	if err == nil {
		t.Errorf("Should return error, when the secret object is not present")
	}

	secret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Error(err.Error())
	}

	_, err = k8sops.ApplySecret(ctx, client, secret)
	if err != nil {
		t.Error(err.Error())
	}

	smartstoreConfigMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testStack1ClusterMasterSmartStore,
			Namespace: "test",
		},
		Data: map[string]string{splcommon.ConfigToken: ""},
	}

	_, err = k8sops.ApplyConfigMap(ctx, client, &smartstoreConfigMap)
	if err != nil {
		t.Error(err.Error())
	}

	current.Status.BundlePushTracker.NeedToPushMasterApps = true

	//Re-attempting to push the CM bundle in less than 5 seconds should return an error
	current.Status.BundlePushTracker.LastCheckInterval = time.Now().Unix() - 1
	err = performCmasterBundlePush(ctx, client, &current)
	if err == nil {
		t.Errorf("Bundle Push Should fail, if attempted to push within 5 seconds interval")
	}

	//Re-attempting to push the CM bundle after 5 seconds passed, should not return an error
	current.Status.BundlePushTracker.LastCheckInterval = time.Now().Unix() - 10
	err = performCmasterBundlePush(ctx, client, &current)
	if err != nil && strings.HasPrefix(err.Error(), "Will re-attempt to push the bundle after the 5 seconds") {
		t.Errorf("Bundle Push Should not fail if reattempted after 5 seconds interval passed. Error: %s", err.Error())
	}

	// When the CM Bundle push is not pending, should not return an error
	current.Status.BundlePushTracker.NeedToPushMasterApps = false
	err = performCmasterBundlePush(ctx, client, &current)
	if err != nil {
		t.Errorf("Should not return an error when the Bundle push is not required. Error: %s", err.Error())
	}
}

func TestPushMasterAppsBundle(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	ctx := context.TODO()
	current := enterpriseApiV3.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApiV3.ClusterMasterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	client := spltest.NewMockClient()

	//Without global secret object, should return an error
	err := pushMasterAppsBundle(ctx, client, &current)
	if err == nil {
		t.Errorf("Bundle push should fail, when the secret object is not found")
	}

	secret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Error(err.Error())
	}

	_, err = k8sops.ApplySecret(ctx, client, secret)
	if err != nil {
		t.Error(err.Error())
	}

	err = pushMasterAppsBundle(ctx, client, &current)
	if err == nil {
		t.Errorf("Bundle push should fail, when the password is not found")
	}

	//Without password, should return an error
	delete(secret.Data, "password")
	err = pushMasterAppsBundle(ctx, client, &current)
	if err == nil {
		t.Errorf("Bundle push should fail, when the password is not found")
	}
}

func TestCheckIfMastersmartstoreConfigMapUpdatedToPod(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cm := enterpriseApiV3.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "clustermaster",
		},
	}

	c := spltest.NewMockClient()
	podExecCommands := []string{
		fmt.Sprintf("cat /mnt/splunk-operator/local/%s", splcommon.ConfigToken),
	}

	smartstoreConfigMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testStack1ClusterMasterSmartStore,
			Namespace: "test",
		},
		Data: map[string]string{"a": "b"},
	}

	mockPodExecReturnContexts := []*spltest.MockPodExecReturnContext{
		{
			StdOut: "",
			StdErr: "",
			Err:    fmt.Errorf("dummy error"),
		},
	}

	var mockPodExecClient *spltest.MockPodExecClient = &spltest.MockPodExecClient{}
	mockPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnContexts...)

	err := checkIfMastersmartstoreConfigMapUpdatedToPod(ctx, c, &cm, mockPodExecClient)
	if err == nil {
		t.Errorf("checkIfMastersmartstoreConfigMapUpdatedToPod should have returned error")
	}

	mockPodExecReturnContexts[0].Err = nil
	err = checkIfMastersmartstoreConfigMapUpdatedToPod(ctx, c, &cm, mockPodExecClient)
	if err == nil {
		t.Errorf("checkIfMastersmartstoreConfigMapUpdatedToPod should have returned error since we did not add configMap yet.")
	}

	c.AddObject(&smartstoreConfigMap)
	err = checkIfMastersmartstoreConfigMapUpdatedToPod(ctx, c, &cm, mockPodExecClient)
	if err != nil {
		t.Errorf("checkIfMastersmartstoreConfigMapUpdatedToPod should not have returned error; err=%v", err)
	}

	mockPodExecClient.CheckPodExecCommands(t, "checkIfMastersmartstoreConfigMapUpdatedToPod")
}
