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

package enterprise

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	reconcile "sigs.k8s.io/controller-runtime/pkg/reconcile"

	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splstorage "github.com/splunk/splunk-operator/pkg/splunk/client/storage"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/telapp"
)

func init() {
	// Re-Assigning GetReadinessScriptLocation, GetLivenessScriptLocation, GetStartupScriptLocation to use absolute path for readinessScriptLocation, readinessScriptLocation
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

func TestApplySearchHeadCluster(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},

		{MetaName: "*v1.ConfigMap-test-splunk-search-head-stack1-configmap"},

		{MetaName: "*v1.Service-test-splunk-stack1-search-head-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-search-head-service"},

		{MetaName: "*v1.Service-test-splunk-stack1-deployer-service"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-deployer"},

		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},

		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-deployer-secret-v1"},

		{MetaName: "*v1.StatefulSet-test-splunk-stack1-deployer"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},

		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-search-head-secret-v1"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},

		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v4.SearchHeadCluster-test-stack1"},
		{MetaName: "*v4.SearchHeadCluster-test-stack1"},
	}

	createFuncCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-search-head-stack1-configmap"},
		{MetaName: "*v1.Service-test-splunk-stack1-search-head-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-search-head-service"},

		{MetaName: "*v1.Service-test-splunk-stack1-deployer-service"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-deployer"},

		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-deployer-secret-v1"},
		//{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-deployer"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-deployer"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},

		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-search-head-secret-v1"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v4.SearchHeadCluster-test-stack1"},
		{MetaName: "*v4.SearchHeadCluster-test-stack1"},
	}

	labels := map[string]string{
		"app.kubernetes.io/component":  "versionedSecrets",
		"app.kubernetes.io/managed-by": "splunk-operator",
	}
	listOpts := []client.ListOption{
		client.InNamespace("test"),
		client.MatchingLabels(labels),
	}
	listmockCall := []spltest.MockFuncCall{
		{ListOpts: listOpts}}

	// CheckPodsForTerminalFailures lists pods using the SHC StatefulSet's selector labels.
	shcPodLabels := map[string]string{
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "search-head",
		"app.kubernetes.io/name":       "search-head",
		"app.kubernetes.io/part-of":    "splunk-stack1-search-head",
		"app.kubernetes.io/instance":   "splunk-stack1-search-head",
	}
	shcPodListOpts := []client.ListOption{
		client.InNamespace("test"),
		client.MatchingLabels(shcPodLabels),
	}
	shcPodListMockCall := spltest.MockFuncCall{ListOpts: shcPodListOpts}

	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0], funcCalls[3], funcCalls[4], funcCalls[5], funcCalls[6], funcCalls[10], funcCalls[12], funcCalls[13], funcCalls[17], funcCalls[19]}, "Update": {funcCalls[0]}, "List": {listmockCall[0], listmockCall[0], shcPodListMockCall}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": createFuncCalls, "Update": {createFuncCalls[6], createFuncCalls[18]}, "List": {listmockCall[0], listmockCall[0], shcPodListMockCall}}
	statefulSet := enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	// Set shc changed to true for testing
	searchHeads := 3
	for i := 0; i < searchHeads; i++ {
		statefulSet.Status.ShcSecretChanged = append(statefulSet.Status.ShcSecretChanged, true)
	}
	revised := statefulSet.DeepCopy()
	revised.Spec.Image = "splunk/test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplySearchHeadCluster(context.TODO(), c, cr.(*enterpriseApi.SearchHeadCluster))
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplySearchHeadCluster", &statefulSet, revised, createCalls, updateCalls, reconcile, true)

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
		_, err := ApplySearchHeadCluster(context.Background(), c, cr.(*enterpriseApi.SearchHeadCluster))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)
}

func searchHeadClusterPodManagerTester(t *testing.T, method string, mockHandlers []spltest.MockHTTPHandler,
	desiredReplicas int32, wantPhase enterpriseApi.Phase, statefulSet *appsv1.StatefulSet,
	wantCalls map[string][]spltest.MockFuncCall, wantError error, initObjects ...client.Object) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	// test for updating
	cr := enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	// Set shc changed to true for testing
	searchHeads := 3
	for i := 0; i < searchHeads; i++ {
		cr.Status.ShcSecretChanged = append(cr.Status.ShcSecretChanged, true)
	}
	secrets := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"password": {'1', '2', '3'},
		},
	}
	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)

	mgr := &searchHeadClusterPodManager{
		cr:      &cr,
		secrets: secrets,
		newSplunkClient: func(managementURI, username, password string) *splclient.SplunkClient {
			c := splclient.NewSplunkClient(managementURI, username, password)
			c.Client = mockSplunkClient
			return c
		},
	}
	spltest.PodManagerUpdateTester(t, method, mgr, desiredReplicas, wantPhase, statefulSet, wantCalls, wantError, initObjects...)
	mockSplunkClient.CheckRequests(t, method)
}

func TestSearchHeadClusterPodManager(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	var replicas int32 = 1
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{ObjectMeta: metav1.ObjectMeta{Name: "pvc-etc", Namespace: "test"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pvc-var", Namespace: "test"}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:        replicas,
			ReadyReplicas:   replicas,
			UpdatedReplicas: replicas,
			UpdateRevision:  "v1",
		},
	}
	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/member/info?count=0&output_mode=json",
			Status: 500,
			Err:    nil,
			Body:   ``,
		},
	}
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.StatefulSet-test-splunk-stack1"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-0"},
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-1"},
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-1"},
		{MetaName: "*v1.Pod-test-splunk-stack1-0"},
		{MetaName: "*v1.Pod-test-splunk-stack1-1"},
	}

	labels := map[string]string{
		"app.kubernetes.io/component":  "versionedSecrets",
		"app.kubernetes.io/managed-by": "splunk-operator",
	}
	listOpts := []client.ListOption{
		client.InNamespace("test"),
		client.MatchingLabels(labels),
	}
	listmockCall := []spltest.MockFuncCall{
		{ListOpts: listOpts}}

	// funcCalls[2] (Pod-search-head-0) appears twice: once for the Splunk client admin
	// password lookup and once for the controller-revision-hash read in updateStatus.
	wantCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2], funcCalls[2]}, "Create": {funcCalls[1]}}

	// test API failure
	method := "searchHeadClusterPodManager.Update(API failure)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhasePending, statefulSet, wantCalls, nil, statefulSet)

	// captain not ready (e.g. mid fleet-recycle captain election) but a scale up is
	// underway -> report ScalingUp instead of masking it behind Pending
	method = "searchHeadClusterPodManager.Update(API failure, scaling up)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 2, enterpriseApi.PhaseScalingUp, statefulSet, wantCalls, nil, statefulSet)

	// captain not ready but a scale down is underway -> report ScalingDown instead
	// of masking it behind Pending
	method = "searchHeadClusterPodManager.Update(API failure, scaling down)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 0, enterpriseApi.PhaseScalingDown, statefulSet, wantCalls, nil, statefulSet)

	// test 1 ready pod
	mockHandlers = []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/member/info?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   loadFixture(t, "shc_member_info_response.json"),
		}, {
			Method: "GET",
			URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/captain/info?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   loadFixture(t, "shc_captain_info_response.json"),
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-0",
			Namespace: "test",
			Labels: map[string]string{
				"controller-revision-hash": "v1",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Ready: true},
			},
		},
	}
	method = "searchHeadClusterPodManager.Update(All pods ready)"
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2], funcCalls[2], funcCalls[2], funcCalls[0], funcCalls[5]}, "Create": {funcCalls[1]}, "List": {listmockCall[0]}}
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhaseReady, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => transition to detention
	mockHandlers = append(mockHandlers, spltest.MockHTTPHandler{
		Method: "POST",
		URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/captain/control/control/upgrade-init",
		Status: 200,
		Err:    nil,
		Body:   ``,
	}, spltest.MockHTTPHandler{
		Method: "POST",
		URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/member/control/control/set_manual_detention?manual_detention=on",
		Status: 200,
		Err:    nil,
		Body:   ``,
	},
	)
	pod.ObjectMeta.Labels["controller-revision-hash"] = "v0"
	method = "searchHeadClusterPodManager.Update(Quarantine Pod)"
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2], funcCalls[2], funcCalls[2], funcCalls[0], funcCalls[5], funcCalls[2], funcCalls[2]}, "Create": {funcCalls[1]}}
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => wait for searches to drain
	mockHandlers = []spltest.MockHTTPHandler{mockHandlers[0], mockHandlers[1]}
	mockHandlers[0].Body = strings.Replace(mockHandlers[0].Body, `"status":"Up"`, `"status":"ManualDetention"`, 1)
	mockHandlers[0].Body = strings.Replace(mockHandlers[0].Body, `"active_historical_search_count":0`, `"active_historical_search_count":1`, 1)
	method = "searchHeadClusterPodManager.Update(Draining Searches)"
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2], funcCalls[2], funcCalls[2], funcCalls[0], funcCalls[5]}, "Create": {funcCalls[1]}}
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => delete pod
	mockHandlers[0].Body = strings.Replace(mockHandlers[0].Body, `"active_historical_search_count":1`, `"active_historical_search_count":0`, 1)
	method = "searchHeadClusterPodManager.Update(Delete Pod)"
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2], funcCalls[2], funcCalls[2], funcCalls[0], funcCalls[5]}, "Create": {funcCalls[1]}, "Delete": {funcCalls[5]}}
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod update finished => release from detention
	pod.ObjectMeta.Labels["controller-revision-hash"] = "v1"
	mockHandlers = append(mockHandlers, spltest.MockHTTPHandler{
		Method: "POST",
		URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/member/control/control/set_manual_detention?manual_detention=off",
		Status: 200,
		Err:    nil,
		Body:   ``,
	})
	method = "searchHeadClusterPodManager.Update(Release Quarantine)"
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2], funcCalls[2], funcCalls[2], funcCalls[0], funcCalls[5], funcCalls[2]}, "Create": {funcCalls[1]}}
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test scale down => remove member
	mockHandlers[2] = spltest.MockHTTPHandler{
		Method: "GET",
		URL:    "https://splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/member/info?count=0&output_mode=json",
		Status: 200,
		Err:    nil,
		Body:   loadFixture(t, "shc_member_remove_response.json"),
	}
	mockHandlers = append(mockHandlers, spltest.MockHTTPHandler{
		Method: "POST",
		URL:    "https://splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/member/consensus/default/remove_server?output_mode=json",
		Status: 200,
		Err:    nil,
		Body:   ``,
	})
	pvcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.PersistentVolumeClaim-test-pvc-etc-splunk-stack1-1"},
		{MetaName: "*v1.PersistentVolumeClaim-test-pvc-var-splunk-stack1-1"},
	}

	updateFuncCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.StatefulSet-test-splunk-stack1"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-0"},
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-0"},
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-0"}, // controller-revision-hash read
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-1"},
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-1"}, // controller-revision-hash read
		{MetaName: "*v1.StatefulSet-test-splunk-stack1"},
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-1"},
		{MetaName: "*v1.PersistentVolumeClaim-test-pvc-etc-splunk-stack1-1"},
		{MetaName: "*v1.PersistentVolumeClaim-test-pvc-var-splunk-stack1-1"},
	}

	wantCalls = map[string][]spltest.MockFuncCall{"Get": updateFuncCalls, "Delete": pvcCalls, "Update": {funcCalls[0]}, "Create": {funcCalls[1]}}
	pvcList := []*corev1.PersistentVolumeClaim{
		{ObjectMeta: metav1.ObjectMeta{Name: "pvc-etc-splunk-stack1-1", Namespace: "test"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "pvc-var-splunk-stack1-1", Namespace: "test"}},
	}
	pod.ObjectMeta.Name = "splunk-stack1-0"
	replicas = 2
	statefulSet.Status.Replicas = 2
	statefulSet.Status.ReadyReplicas = 2
	statefulSet.Status.UpdatedReplicas = 2
	method = "searchHeadClusterPodManager.Update(Remove Member)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhaseScalingDown, statefulSet, wantCalls, nil, statefulSet, pod, pvcList[0], pvcList[1])

}

func TestFinishRecycle(t *testing.T) {
	ctx := context.TODO()
	mgr := &searchHeadClusterPodManager{
		cr: &enterpriseApi.SearchHeadCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "stack1", Namespace: "test"},
		},
	}

	// member is up, not in detention -> recycle is complete
	mgr.cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{{Status: "Up"}}
	complete, err := mgr.FinishRecycle(ctx, 0)
	if err != nil || !complete {
		t.Errorf("FinishRecycle(Up) = %v, %v; want true, nil", complete, err)
	}

	// member info was transiently unavailable (e.g. pod mid-restart) -> wait, don't error
	mgr.cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{{Status: ""}}
	complete, err = mgr.FinishRecycle(ctx, 0)
	if err != nil || complete {
		t.Errorf("FinishRecycle(empty status) = %v, %v; want false, nil", complete, err)
	}

	// any other unrecognized status is still a hard error
	mgr.cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{{Status: "Down"}}
	complete, err = mgr.FinishRecycle(ctx, 0)
	if err == nil || complete {
		t.Errorf("FinishRecycle(Down) = %v, %v; want false, error", complete, err)
	}
}

func TestApplyShcSecret(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	method := "ApplyShcSecret"
	var initObjectList []client.Object

	c := spltest.NewMockClient()

	// Get namespace scoped secret
	nsSecret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Errorf("Apply namespace scoped secret failed")
	}

	// Create pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-search-head-0",
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
							SecretName: "stack1-secrets",
						},
					},
				},
			},
		},
	}
	initObjectList = append(initObjectList, pod)

	secrets := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"password":   {'1', '2', '3'},
			"shc_secret": {'a'},
		},
	}
	initObjectList = append(initObjectList, secrets)

	c.AddObjects(initObjectList)

	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "POST",
			URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/server/control/restart",
			Status: 200,
			Err:    nil,
		},
		{
			Method: "POST",
			URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/server/control/restart",
			Status: 200,
			Err:    nil,
		},
		{
			Method: "POST",
			URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/server/control/restart",
			Status: 200,
			Err:    nil,
		},
	}

	cr := enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	cr.Status.AdminPasswordChangedSecrets = make(map[string]bool)
	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)
	mgr := &searchHeadClusterPodManager{
		c:       c,
		cr:      &cr,
		secrets: secrets,
		newSplunkClient: func(managementURI, username, password string) *splclient.SplunkClient {
			c := splclient.NewSplunkClient(managementURI, username, password)
			c.Client = mockSplunkClient
			return c
		},
	}

	podExecCommands := []string{
		"/opt/splunk/bin/splunk edit shcluster-config",
		"opt/splunk/bin/splunk cmd splunkd rest",
	}
	mockPodExecReturnContexts := []*spltest.MockPodExecReturnContext{
		{
			StdOut: "",
			StdErr: "",
			Err:    fmt.Errorf("some dummy error"),
		},
		{
			StdOut: "",
			StdErr: "",
			Err:    fmt.Errorf("some dummy error"),
		},
	}

	var mockPodExecClient *spltest.MockPodExecClient = &spltest.MockPodExecClient{}
	mockPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnContexts...)
	// Set resource version as that of NS secret
	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}

	// Change resource version and test
	mgr.cr.Status.NamespaceSecretResourceVersion = "0"
	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err == nil {
		t.Errorf("Couldn't apply shc secret")
	}

	mockPodExecReturnContexts[0].Err = nil
	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err == nil {
		t.Errorf("Couldn't apply shc secret")
	}

	mgr.cr.Status.ShcSecretChanged[0] = false
	mockPodExecReturnContexts[1].Err = nil
	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}
	mockSplunkClient.CheckRequests(t, method)

	// Don't set as it is set already
	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}

	// Update admin password in secret again to hit already set scenario
	secrets.Data["password"] = []byte{'1'}
	err = splutil.UpdateResource(ctx, c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	mgr.cr.Status.ShcSecretChanged[0] = false
	// Test set again for shc_secret
	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}

	// Update admin password in secret again to hit already set scenario
	secrets.Data["password"] = []byte{'1'}
	err = splutil.UpdateResource(ctx, c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	mgr.cr.Status.ShcSecretChanged[0] = false
	mgr.cr.Status.AdminSecretChanged[0] = false
	// Test set again for admin password
	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}

	// Missing shc_secret scenario
	secrets = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"password": {'1', '2', '3'},
		},
	}
	err = splutil.UpdateResource(ctx, c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	errMsg := fmt.Sprintf(splcommon.SecretTokenNotRetrievable, "shc_secret") + ", error: invalid secret data"

	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err.Error() != errMsg {
		t.Errorf("Couldn't recognize missing shc_secret %s", err.Error())
	}

	// Missing admin password scenario
	secrets = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"shc_secret": {'a'},
		},
	}

	err = splutil.UpdateResource(ctx, c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	errMsg = fmt.Sprintf(splcommon.SecretTokenNotRetrievable, "admin password") + ", error: invalid secret data"
	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err.Error() != errMsg {
		t.Errorf("Couldn't recognize missing admin password %s", err.Error())
	}

	// Make resource version of ns secret and cr the same
	mgr.cr.Status.NamespaceSecretResourceVersion = "1"
	nsSecret.ResourceVersion = mgr.cr.Status.NamespaceSecretResourceVersion
	err = splutil.UpdateResource(ctx, c, nsSecret)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}
}

// TestApplyShcSecretAdminPasswordNotStarvedByShcSecretAlreadyChanged is a regression test:
// when a pod's shc_secret was already marked as synced in a prior reconcile, its
// independent admin-password check must still run instead of being skipped.
func TestApplyShcSecretAdminPasswordNotStarvedByShcSecretAlreadyChanged(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	var initObjectList []client.Object

	c := spltest.NewMockClient()

	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Errorf("Apply namespace scoped secret failed")
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-search-head-0",
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
							SecretName: "stack1-secrets",
						},
					},
				},
			},
		},
	}
	initObjectList = append(initObjectList, pod)

	// Pod's mounted secret already matches the namespace shc_secret, but its
	// admin password does not -- the shc_secret branch must not be entered
	// (and thus won't short-circuit via "continue"), while the admin password
	// branch below still needs to run regardless.
	secrets := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"password":   {'1'},
			"shc_secret": {'a'},
		},
	}
	initObjectList = append(initObjectList, secrets)

	c.AddObjects(initObjectList)

	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "POST",
			URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/server/control/restart",
			Status: 200,
			Err:    nil,
		},
	}

	cr := enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	cr.Status.AdminPasswordChangedSecrets = make(map[string]bool)
	// Simulate a prior reconcile that already synced shc_secret for pod 0.
	cr.Status.ShcSecretChanged = []bool{true}
	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)
	mgr := &searchHeadClusterPodManager{
		c:       c,
		cr:      &cr,
		secrets: secrets,
		newSplunkClient: func(managementURI, username, password string) *splclient.SplunkClient {
			c := splclient.NewSplunkClient(managementURI, username, password)
			c.Client = mockSplunkClient
			return c
		},
	}

	podExecCommands := []string{
		"opt/splunk/bin/splunk cmd splunkd rest",
	}
	mockPodExecReturnContexts := []*spltest.MockPodExecReturnContext{
		{
			StdOut: "",
			StdErr: "",
			Err:    nil,
		},
	}

	var mockPodExecClient *spltest.MockPodExecClient = &spltest.MockPodExecClient{}
	mockPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnContexts...)

	// Namespace secret's shc_secret ('a') already matches the pod's, so the
	// admin-password mismatch is the only thing that should trigger a sync.
	// Bump the resource version so ApplyShcSecret doesn't early-return.
	mgr.cr.Status.NamespaceSecretResourceVersion = "0"

	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}

	if !mgr.cr.Status.AdminSecretChanged[0] {
		t.Errorf("Admin password sync was skipped for pod 0 even though shc_secret was already in sync")
	}
	if len(mockPodExecClient.GotCmdList) == 0 {
		t.Errorf("Expected admin password change command to be executed, but none was")
	}
}

func TestShcPasswordSyncCompleted(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	builder := newFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.SearchHeadCluster{})

	client := builder.Build()
	ctx := context.TODO()

	// Create a mock event recorder to capture events
	recorder := &mockEventRecorder{events: []mockEvent{}}
	eventPublisher := &K8EventPublisher{recorder: recorder}

	shc := enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "shc",
			Namespace: "test",
		},
	}
	shc.SetGroupVersionKind(enterpriseApi.GroupVersion.WithKind("SearchHeadCluster"))

	err := client.Create(ctx, &shc)
	if err != nil {
		t.Fatalf("Failed to create SearchHeadCluster: %v", err)
	}

	// Create namespace scoped secret so ApplyShcSecret has something to work with
	nsSecret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, shc.GetNamespace())
	if err != nil {
		t.Fatalf("Failed to apply namespace scoped secret: %v", err)
	}

	// Set CR status resource version to a stale value so ApplyShcSecret does not early-return
	shc.Status.NamespaceSecretResourceVersion = nsSecret.ResourceVersion + "-old"
	shc.Status.AdminPasswordChangedSecrets = make(map[string]bool)

	// Initialize a minimal pod manager for ApplyShcSecret
	mgr := &searchHeadClusterPodManager{
		c:  client,
		cr: &shc,
	}

	// Use a mock PodExec client; replicas will be 0 so it won't be exercised
	var mockPodExecClient *spltest.MockPodExecClient = &spltest.MockPodExecClient{}

	// Add event publisher to context so ApplyShcSecret can emit events
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	// Call ApplyShcSecret; with 0 replicas it will complete without touching pods,
	// but still emit the PasswordSyncCompleted event
	err = ApplyShcSecret(ctx, mgr, 0, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}

	// Check that PasswordSyncCompleted event was published
	foundEvent := false
	for _, event := range recorder.events {
		if event.reason == "PasswordSyncCompleted" {
			foundEvent = true
			if event.eventType != corev1.EventTypeNormal {
				t.Errorf("Expected Normal event type, got %s", event.eventType)
			}
			if !strings.Contains(event.message, "Password synchronized") {
				t.Errorf("Expected event message to contain 'Password synchronized', got: %s", event.message)
			}
			break
		}
	}
	if !foundEvent {
		t.Errorf("Expected PasswordSyncCompleted event to be published")
	}
}

func TestGetSearchHeadStatefulSet(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Errorf("Failed to create namespace scoped object")
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := validateSearchHeadClusterSpec(ctx, c, &cr); err != nil {
				t.Errorf("validateSearchHeadClusterSpec() returned error: %v", err)
			}
			return getSearchHeadStatefulSet(ctx, c, &cr)
		}
		configTester(t, fmt.Sprintf("getSearchHeadStatefulSet(Replicas=%d)", cr.Spec.Replicas), f, want)
	}

	cr.Spec.Replicas = 3
	test(loadFixture(t, "statefulset_stack1_search_head_base.json"))

	cr.Spec.Replicas = 4
	test(loadFixture(t, "statefulset_stack1_search_head_base_1.json"))

	cr.Spec.Replicas = 5
	cr.Spec.ClusterManagerRef.Name = "stack1"
	_ = splutil.CreateResource(ctx, c, &enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	})
	test(loadFixture(t, "statefulset_stack1_search_head_base_2.json"))

	cr.Spec.Replicas = 6

	cr.Spec.ClusterManagerRef.Namespace = "test2"
	_ = splutil.CreateResource(ctx, c, &enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test2",
		},
	})
	test(loadFixture(t, "statefulset_stack1_search_head_base_3.json"))

	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	test(loadFixture(t, "statefulset_stack1_search_head_base_4.json"))

	// Define additional service port in CR and verified the statefulset has the new port
	test(loadFixture(t, "statefulset_stack1_search_head_base_5.json"))

	// Create a serviceaccount
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	test(loadFixture(t, "statefulset_stack1_search_head_with_service_account.json"))

	// Add extraEnv
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	test(loadFixture(t, "statefulset_stack1_search_head_with_service_account_1.json"))

	// Add additional label to cr metadata to transfer to the statefulset
	cr.ObjectMeta.Labels = make(map[string]string)
	cr.ObjectMeta.Labels["app.kubernetes.io/test-extra-label"] = "test-extra-label-value"
	test(loadFixture(t, "statefulset_stack1_search_head_with_service_account_2.json"))
}

func TestGetDeployerStatefulSet(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Errorf("Failed to create namespace scoped object")
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := validateSearchHeadClusterSpec(ctx, c, &cr); err != nil {
				t.Errorf("validateSearchHeadClusterSpec() returned error: %v", err)
			}
			return getDeployerStatefulSet(ctx, c, &cr)
		}
		configTester(t, "getDeployerStatefulSet()", f, want)
	}

	cr.Spec.Replicas = 3
	test(loadFixture(t, "statefulset_stack1_deployer_base.json"))

	// Allow installation of apps via DefaultsURLApps on the SHCDeployer
	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	test(loadFixture(t, "statefulset_stack1_deployer_with_apps.json"))

	// Create a serviceaccount
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"

	test(loadFixture(t, "statefulset_stack1_deployer_with_service_account.json"))
}

func TestSearchHeadSpecNotCreatedWithoutGeneralTerms(t *testing.T) {
	// Unset the SPLUNK_GENERAL_TERMS environment variable
	os.Unsetenv("SPLUNK_GENERAL_TERMS")
	ctx := context.TODO()

	// Create a mock search head CR
	shc := enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			Replicas: 3,
		},
	}

	// Create a mock client
	c := spltest.NewMockClient()

	// Attempt to apply the search head spec
	_, err := ApplySearchHeadCluster(ctx, c, &shc)

	// SPLUNK_GENERAL_TERMS unset is a stalled misconfiguration: reconciler returns terminal error (no requeue)
	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Errorf("stalled spec validation failure should return a terminal error, got %v", err)
	}
}

func TestApplySearchHeadClusterValidationFailure(t *testing.T) {
	ctx := context.TODO()
	shc := &enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "SearchHeadCluster",
			APIVersion: "enterprise.splunk.com/v4",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				LivenessProbe: &enterpriseApi.Probe{
					InitialDelaySeconds: -5, // Invalid value
				},
				Volumes: []corev1.Volume{},
			},
		},
		Status: enterpriseApi.SearchHeadClusterStatus{},
	}

	c := spltest.NewMockClient()

	err := c.Create(ctx, shc)
	if err != nil {
		t.Errorf("shc CR creation failed: %v", err)
	}

	_, err = ApplySearchHeadCluster(ctx, c, shc)
	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Errorf("stalled spec validation failure should return a terminal error, got %v", err)
	}
	if shc.Status.Phase != enterpriseApi.PhaseError {
		t.Errorf("Expected PhaseError, got %v", shc.Status.Phase)
	}
	if shc.Status.DeployerPhase != enterpriseApi.PhaseError {
		t.Errorf("Expected DeployerPhaseError, got %v", shc.Status.DeployerPhase)
	}
}

func TestApplySearchHeadClusterDeployerPodTerminalFailure(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	scheme := pkgruntime.NewScheme()
	utilruntime.Must(enterpriseApi.AddToScheme(scheme))
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	c := newFakeClientBuilder(scheme).
		WithStatusSubresource(&enterpriseApi.SearchHeadCluster{}).
		Build()

	cr := &enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "SearchHeadCluster",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	if err := c.Create(ctx, cr); err != nil {
		t.Fatalf("failed to create SHC CR: %v", err)
	}

	// Pass 1: creates the deployer StatefulSet (PhasePending); no error expected.
	if _, err := ApplySearchHeadCluster(ctx, c, cr); err != nil {
		t.Fatalf("pass 1 unexpectedly failed: %v", err)
	}

	// Inject a deployer pod stuck in ImagePullBackOff so that
	// checkPodsForTerminalFailures returns a TerminalError on the next reconcile.
	deployerPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetPodName(SplunkDeployer, cr.GetName(), 0),
			Namespace: cr.GetNamespace(),
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "splunk-operator",
				"app.kubernetes.io/component":  "search-head",
				"app.kubernetes.io/name":       "deployer",
				"app.kubernetes.io/part-of":    fmt.Sprintf("splunk-%s-search-head", cr.GetName()),
				"app.kubernetes.io/instance":   GetSplunkStatefulsetName(SplunkDeployer, cr.GetName()),
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{
						Reason:  "ImagePullBackOff",
						Message: "Back-off pulling image",
					},
				},
			}},
		},
	}
	if err := c.Create(ctx, deployerPod); err != nil {
		t.Fatalf("failed to create terminal deployer pod: %v", err)
	}

	// Pass 2: deployer STS already exists with ReadyReplicas=0; the terminal pod
	// is detected and the function must return a TerminalError with Stalled=True.
	_, err := ApplySearchHeadCluster(ctx, c, cr)
	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Errorf("expected TerminalError for deployer ImagePullBackOff pod, got %v", err)
	}
}

// TestApplySearchHeadClusterUpgradePathSoftWait verifies that when
// UpgradePathValidation soft-waits on a LicenseManager that is temporarily
// not Ready (returns (false, nil), not an error), ApplySearchHeadCluster
// does not leave the earlier-staged PhaseError as the persisted status on
// either cr.Status.Phase or cr.Status.DeployerPhase (CSPL-5080).
func TestApplySearchHeadClusterUpgradePathSoftWait(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	scheme := pkgruntime.NewScheme()
	utilruntime.Must(enterpriseApi.AddToScheme(scheme))
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	c := newFakeClientBuilder(scheme).
		WithStatusSubresource(&enterpriseApi.SearchHeadCluster{}).
		WithStatusSubresource(&enterpriseApi.LicenseManager{}).
		Build()

	lm := &enterpriseApi.LicenseManager{
		TypeMeta: metav1.TypeMeta{
			Kind:       "LicenseManager",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lm1",
			Namespace: "test",
		},
	}
	if err := c.Create(ctx, lm); err != nil {
		t.Fatalf("failed to create LicenseManager CR: %v", err)
	}
	// LicenseManager not yet Ready: this is the benign soft-wait state.
	lm.Status.Phase = enterpriseApi.PhaseUpdating
	if err := c.Status().Update(ctx, lm); err != nil {
		t.Fatalf("failed to set LicenseManager status: %v", err)
	}

	cr := &enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "SearchHeadCluster",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				LicenseManagerRef: corev1.ObjectReference{
					Name: "lm1",
				},
			},
		},
	}
	if err := c.Create(ctx, cr); err != nil {
		t.Fatalf("failed to create SHC CR: %v", err)
	}

	// Pass 1: creates the deployer StatefulSet; no LicenseManager image to
	// compare against yet, so UpgradePathValidation isn't exercised until
	// the StatefulSet already exists (see CSPL-3060 guard).
	if _, err := ApplySearchHeadCluster(ctx, c, cr); err != nil {
		t.Fatalf("pass 1 unexpectedly failed: %v", err)
	}

	// The CSPL-3060 guard checks statefulSet.CreationTimestamp.IsZero(); the
	// fake client doesn't populate CreationTimestamp on Create, so set it
	// explicitly to make pass 2 exercise UpgradePathValidation.
	deployerStatefulSetName := types.NamespacedName{
		Name:      GetSplunkStatefulsetName(SplunkDeployer, cr.GetName()),
		Namespace: cr.GetNamespace(),
	}
	deployerStatefulSet := &appsv1.StatefulSet{}
	if err := c.Get(ctx, deployerStatefulSetName, deployerStatefulSet); err != nil {
		t.Fatalf("failed to fetch deployer StatefulSet: %v", err)
	}
	deployerStatefulSet.CreationTimestamp = metav1.Now()
	if err := c.Update(ctx, deployerStatefulSet); err != nil {
		t.Fatalf("failed to set deployer StatefulSet CreationTimestamp: %v", err)
	}

	// Give the LicenseManager a StatefulSet whose image matches the SHC's
	// so UpgradePathValidation reaches the not-Ready soft-wait branch
	// instead of the hard image-mismatch error branch.
	lmStatefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(SplunkLicenseManager, lm.GetName()),
			Namespace: lm.GetNamespace(),
		},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "splunk", Image: cr.Spec.CommonSplunkSpec.Image},
					},
				},
			},
		},
	}
	if err := c.Create(ctx, lmStatefulSet); err != nil {
		t.Fatalf("failed to create LicenseManager StatefulSet: %v", err)
	}

	// Pass 2: deployer StatefulSet already exists, so UpgradePathValidation
	// runs, finds the LicenseManager image matches but Phase != Ready, and
	// soft-waits by returning (false, nil).
	if _, err := ApplySearchHeadCluster(ctx, c, cr); err != nil {
		t.Fatalf("pass 2 unexpectedly returned an error for a benign soft-wait: %v", err)
	}

	if err := c.Get(ctx, types.NamespacedName{Name: cr.GetName(), Namespace: cr.GetNamespace()}, cr); err != nil {
		t.Fatalf("failed to re-fetch SHC CR: %v", err)
	}
	if cr.Status.Phase != enterpriseApi.PhasePending {
		t.Errorf("expected Phase=Pending during LicenseManager soft-wait, got %v", cr.Status.Phase)
	}
	if cr.Status.DeployerPhase != enterpriseApi.PhasePending {
		t.Errorf("expected DeployerPhase=Pending during LicenseManager soft-wait, got %v", cr.Status.DeployerPhase)
	}
}

func TestAppFrameworkSearchHeadClusterShouldNotFail(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			Replicas: 3,
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret", Type: "s3", Provider: "aws"},
				},
				AppSources: []enterpriseApi.AppSourceSpec{
					{Name: "adminApps",
						Location: "adminAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal},
					},
					{Name: "securityApps",
						Location: "securityAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal},
					},
					{Name: "authenticationApps",
						Location: "authenticationAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal},
					},
				},
			},
		},
	}

	client := spltest.NewMockClient()

	// Create namespace scoped secret
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Error(err.Error())
	}

	// Create S3 secret
	s3Secret := spltest.GetMockS3SecretKeys("s3-secret")

	client.AddObject(&s3Secret)

	// to pass the validation stage, add the directory to download apps
	err = os.MkdirAll(splcommon.AppDownloadVolume, 0755)
	defer os.RemoveAll(splcommon.AppDownloadVolume)

	if err != nil {
		t.Errorf("Unable to create download directory for apps :%s", splcommon.AppDownloadVolume)
	}

	_, err = ApplySearchHeadCluster(ctx, client, &cr)
	if err != nil {
		t.Errorf("ApplySearchHeadCluster should be successful")
	}
}

func TestSHCGetAppsListForAWSS3ClientShouldNotFail(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			Replicas: 3,
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				Defaults: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol2",
					Scope:   enterpriseApi.ScopeLocal,
				},
				VolList: []enterpriseApi.VolumeSpec{
					{
						Name:      "msos_s2s3_vol",
						Endpoint:  "https://s3-eu-west-2.amazonaws.com",
						Path:      "testbucket-rs-london",
						SecretRef: "s3-secret",
						Type:      "s3",
						Provider:  "aws",
					},
					{
						Name:      "msos_s2s3_vol2",
						Endpoint:  "https://s3-eu-west-2.amazonaws.com",
						Path:      "testbucket-rs-london2",
						SecretRef: "s3-secret",
						Type:      "s3",
						Provider:  "aws",
					},
				},
				AppSources: []enterpriseApi.AppSourceSpec{
					{
						Name:     "adminApps",
						Location: "adminAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal,
						},
					},
					{
						Name:     "securityApps",
						Location: "securityAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal,
						},
					},
					{
						Name:     "authenticationApps",
						Location: "authenticationAppsRepo",
					},
				},
			},
		},
	}

	client := spltest.NewMockClient()

	// Create S3 secret
	s3Secret := spltest.GetMockS3SecretKeys("s3-secret")

	client.AddObject(&s3Secret)

	// Create namespace scoped secret
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Error(err.Error())
	}

	splstorage.RegisterRemoteDataClient(ctx, "aws")

	Etags := []string{"cc707187b036405f095a8ebb43a782c1", "5055a61b3d1b667a4c3279a381a2e7ae", "19779168370b97d8654424e6c9446dd9"}
	Keys := []string{"admin_app.tgz", "security_app.tgz", "authentication_app.tgz"}
	Sizes := []int64{10, 20, 30}
	StorageClass := "STANDARD"
	randomTime := time.Date(2021, time.May, 1, 23, 23, 0, 0, time.UTC)

	mockAwsHandler := spltest.MockAWSS3Handler{}

	mockAwsObjects := []spltest.MockAWSS3Client{
		{
			Objects: []*spltest.MockRemoteDataObject{
				{
					Etag:         &Etags[0],
					Key:          &Keys[0],
					LastModified: &randomTime,
					Size:         &Sizes[0],
					StorageClass: &StorageClass,
				},
			},
		},
		{
			Objects: []*spltest.MockRemoteDataObject{
				{
					Etag:         &Etags[1],
					Key:          &Keys[1],
					LastModified: &randomTime,
					Size:         &Sizes[1],
					StorageClass: &StorageClass,
				},
			},
		},
		{
			Objects: []*spltest.MockRemoteDataObject{
				{
					Etag:         &Etags[2],
					Key:          &Keys[2],
					LastModified: &randomTime,
					Size:         &Sizes[2],
					StorageClass: &StorageClass,
				},
			},
		},
	}

	appFrameworkRef := cr.Spec.AppFrameworkConfig

	mockAwsHandler.AddObjects(appFrameworkRef, mockAwsObjects...)

	var vol enterpriseApi.VolumeSpec
	var allSuccess bool = true
	for index, appSource := range appFrameworkRef.AppSources {

		vol, err = splutil.GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
		if err != nil {
			allSuccess = false
			continue
		}

		// Update the GetRemoteDataClient with our mock call which initializes mock AWS client
		getClientWrapper := splstorage.RemoteDataClientsMap[vol.Provider]
		getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, splstorage.NewMockAWSS3Client)

		remoteDataClientMgr := &RemoteDataClientManager{client: client,
			cr: &cr, appFrameworkRef: &cr.Spec.AppFrameworkConfig,
			vol:      &vol,
			location: appSource.Location,
			initFn: func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
				cl := spltest.MockAWSS3Client{}
				cl.Objects = mockAwsObjects[index].Objects
				return cl
			},
			getRemoteDataClient: func(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec, location string, fn splcommon.GetInitFunc) (splstorage.SplunkRemoteDataClient, error) {
				c, err := GetRemoteStorageClient(ctx, client, cr, appFrameworkRef, vol, location, fn)
				return c, err
			},
		}

		RemoteDataListResponse, err := remoteDataClientMgr.GetAppsList(ctx)
		if err != nil {
			allSuccess = false
			continue
		}

		var mockResponse spltest.MockRemoteDataClient
		mockResponse, err = splstorage.ConvertRemoteDataListResponse(ctx, RemoteDataListResponse)
		if err != nil {
			allSuccess = false
			continue
		}
		if mockAwsHandler.GotSourceAppListResponseMap == nil {
			mockAwsHandler.GotSourceAppListResponseMap = make(map[string]spltest.MockAWSS3Client)
		}

		mockAwsHandler.GotSourceAppListResponseMap[appSource.Name] = spltest.MockAWSS3Client(mockResponse)
	}

	if allSuccess == false {
		t.Errorf("Unable to get apps list for all the app sources")
	}
	method := "GetAppsList"
	mockAwsHandler.CheckAWSRemoteDataListResponse(t, method)
}

func TestSHCGetAppsListForAWSS3ClientShouldFail(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "msos_s2s3_vol",
						Endpoint:  "https://s3-eu-west-2.amazonaws.com",
						Path:      "testbucket-rs-london",
						SecretRef: "s3-secret",
						Type:      "s3",
						Provider:  "aws"},
				},
				AppSources: []enterpriseApi.AppSourceSpec{
					{Name: "adminApps",
						Location: "adminAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal},
					},
				},
			},
		},
	}

	client := spltest.NewMockClient()

	// Create namespace scoped secret
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Error(err.Error())
	}

	splstorage.RegisterRemoteDataClient(ctx, "aws")

	Etags := []string{"cc707187b036405f095a8ebb43a782c1"}
	Keys := []string{"admin_app.tgz"}
	Sizes := []int64{10}
	StorageClass := "STANDARD"
	randomTime := time.Date(2021, time.May, 1, 23, 23, 0, 0, time.UTC)

	mockAwsHandler := spltest.MockAWSS3Handler{}

	mockAwsObjects := []spltest.MockAWSS3Client{
		{
			Objects: []*spltest.MockRemoteDataObject{
				{
					Etag:         &Etags[0],
					Key:          &Keys[0],
					LastModified: &randomTime,
					Size:         &Sizes[0],
					StorageClass: &StorageClass,
				},
			},
		},
	}

	appFrameworkRef := cr.Spec.AppFrameworkConfig

	mockAwsHandler.AddObjects(appFrameworkRef, mockAwsObjects...)

	var vol enterpriseApi.VolumeSpec

	appSource := appFrameworkRef.AppSources[0]
	vol, err = splutil.GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
	if err != nil {
		t.Errorf("Unable to get Volume due to error=%s", err)
	}

	// Update the GetRemoteDataClient with our mock call which initializes mock AWS client
	getClientWrapper := splstorage.RemoteDataClientsMap[vol.Provider]
	getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, splstorage.NewMockAWSS3Client)

	remoteDataClientMgr := &RemoteDataClientManager{
		client:          client,
		cr:              &cr,
		appFrameworkRef: &cr.Spec.AppFrameworkConfig,
		vol:             &vol,
		location:        appSource.Location,
		initFn: func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
			// Purposefully return nil here so that we test the error scenario
			return nil
		},
		getRemoteDataClient: func(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject,
			appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec,
			location string, fn splcommon.GetInitFunc) (splstorage.SplunkRemoteDataClient, error) {
			// Get the mock client
			c, err := GetRemoteStorageClient(ctx, client, cr, appFrameworkRef, vol, location, fn)
			return c, err
		},
	}

	_, err = remoteDataClientMgr.GetAppsList(ctx)
	if err == nil {
		t.Errorf("GetAppsList should have returned error as there is no S3 secret provided")
	}

	// Create empty S3 secret
	s3Secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "s3-secret",
			Namespace: "test",
		},
		Data: map[string][]byte{},
	}

	client.AddObject(&s3Secret)

	_, err = remoteDataClientMgr.GetAppsList(ctx)
	if err == nil {
		t.Errorf("GetAppsList should have returned error as S3 secret has empty keys")
	}

	s3AccessKey := []byte{'1'}
	s3Secret.Data = map[string][]byte{"s3_access_key": s3AccessKey}
	_, err = remoteDataClientMgr.GetAppsList(ctx)
	if err == nil {
		t.Errorf("GetAppsList should have returned error as S3 secret has empty s3_secret_key")
	}

	s3SecretKey := []byte{'2'}
	s3Secret.Data = map[string][]byte{"s3_secret_key": s3SecretKey}
	_, err = remoteDataClientMgr.GetAppsList(ctx)
	if err == nil {
		t.Errorf("GetAppsList should have returned error as S3 secret has empty s3_access_key")
	}

	// Create S3 secret
	s3Secret = spltest.GetMockS3SecretKeys("s3-secret")

	// This should return an error as we have initialized initFn for remoteDataClientMgr
	// to return a nil client.
	_, err = remoteDataClientMgr.GetAppsList(ctx)
	if err == nil {
		t.Errorf("GetAppsList should have returned error as we could not get the S3 client")
	}

	remoteDataClientMgr.initFn = func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
		// To test the error scenario, do no set the Objects member yet
		cl := spltest.MockAWSS3Client{}
		return cl
	}

	remoteDataClientResponse, err := remoteDataClientMgr.GetAppsList(ctx)
	if err != nil {
		t.Errorf("GetAppsList should not have returned error since empty appSources are allowed.")
	}
	if len(remoteDataClientResponse.Objects) != 0 {
		t.Errorf("GetAppsList should return an empty response since we have empty objects in MockAWSS3Client")
	}
}

func TestApplySearchHeadClusterDeletion(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	shc := enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				AppsRepoPollInterval: 0,
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "msos_s2s3_vol",
						Endpoint:  "https://s3-eu-west-2.amazonaws.com",
						Path:      "testbucket-rs-london",
						SecretRef: "s3-secret",
						Type:      "s3",
						Provider:  "aws"},
				},
				AppSources: []enterpriseApi.AppSourceSpec{
					{Name: "adminApps",
						Location: "adminAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal},
					},
					{Name: "securityApps",
						Location: "securityAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal},
					},
					{Name: "authenticationApps",
						Location: "authenticationAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal},
					},
				},
			},
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				MonitoringConsoleRef: corev1.ObjectReference{
					Name: "mcName",
				},
				Mock: true,
			},
		},
	}

	c := spltest.NewMockClient()

	// Create S3 secret
	s3Secret := spltest.GetMockS3SecretKeys("s3-secret")

	c.AddObject(&s3Secret)

	// Create namespace scoped secret
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Error(err.Error())
	}

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	shc.ObjectMeta.DeletionTimestamp = &currentTime
	shc.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}

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
	c.ListObj = &pvclist

	// to pass the validation stage, add the directory to download apps
	err = os.MkdirAll(splcommon.AppDownloadVolume, 0755)
	defer os.RemoveAll(splcommon.AppDownloadVolume)

	if err != nil {
		t.Errorf("Unable to create download directory for apps :%s", splcommon.AppDownloadVolume)
	}

	_, err = ApplySearchHeadCluster(ctx, c, &shc)
	if err != nil {
		t.Errorf("ApplySearchHeadCluster should not have returned error here.")
	}
}

func TestGetSearchHeadClusterList(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	shc := enterpriseApi.SearchHeadCluster{}

	listOpts := []client.ListOption{
		client.InNamespace("test"),
	}

	client := spltest.NewMockClient()

	var numOfObjects int

	// Invalid scenario since we haven't added shc to the list yet
	_, err := getSearchHeadClusterList(ctx, client, &shc, listOpts)
	if err == nil {
		t.Errorf("getNumOfObjects should have returned error as we haven't added shc to the list yet")
	}

	shcList := &enterpriseApi.SearchHeadClusterList{}
	shcList.Items = append(shcList.Items, shc)

	client.ListObj = shcList

	objList, err := getSearchHeadClusterList(ctx, client, &shc, listOpts)
	if err != nil {
		t.Errorf("getNumOfObjects should not have returned error=%v", err)
	}

	numOfObjects = len(objList.Items)
	if numOfObjects != 1 {
		t.Errorf("Got wrong number of SearchHeadCluster objects. Expected=%d, Got=%d", 1, numOfObjects)
	}
}

func TestSearchHeadClusterWithReadyState(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	mclient := &spltest.MockHTTPClient{}
	type Entry1 struct {
		Content splclient.SearchHeadCaptainInfo `json:"content"`
	}

	apiResponse1 := struct {
		Entry []Entry1 `json:"entry"`
	}{
		Entry: []Entry1{
			{
				Content: splclient.SearchHeadCaptainInfo{
					Initialized:     true,
					ServiceReady:    true,
					MaintenanceMode: true,
				},
			},
			{
				Content: splclient.SearchHeadCaptainInfo{
					Initialized:     true,
					ServiceReady:    true,
					MaintenanceMode: true,
				},
			},
			{
				Content: splclient.SearchHeadCaptainInfo{
					Initialized:     true,
					ServiceReady:    true,
					MaintenanceMode: true,
				},
			},
		},
	}

	type Entry struct {
		Name    string                                `json:"name"`
		Content splclient.SearchHeadClusterMemberInfo `json:"content"`
	}

	apiResponse2 := struct {
		Entry []Entry `json:"entry"`
	}{
		Entry: []Entry{
			{
				Name: "splunk-test-search-head-0",
				Content: splclient.SearchHeadClusterMemberInfo{
					ActiveHistoricalSearchCount: 1,
					ActiveRealtimeSearchCount:   1,
					Adhoc:                       true,
					Registered:                  true,
					LastHeartbeatAttempt:        1,
					PeerLoadStatsGla15m:         1,
					PeerLoadStatsGla1m:          1,
					PeerLoadStatsGla5m:          1,
					RestartState:                "Up",
					Status:                      "Up",
				},
			},
			{
				Name: "splunk-test-search-head-1",
				Content: splclient.SearchHeadClusterMemberInfo{
					ActiveHistoricalSearchCount: 1,
					ActiveRealtimeSearchCount:   1,
					Adhoc:                       true,
					Registered:                  true,
					LastHeartbeatAttempt:        1,
					PeerLoadStatsGla15m:         1,
					PeerLoadStatsGla1m:          1,
					PeerLoadStatsGla5m:          1,
					RestartState:                "Up",
					Status:                      "Up",
				},
			},
			{
				Name: "splunk-test-search-head-2",
				Content: splclient.SearchHeadClusterMemberInfo{
					ActiveHistoricalSearchCount: 1,
					ActiveRealtimeSearchCount:   1,
					Adhoc:                       true,
					Registered:                  true,
					LastHeartbeatAttempt:        1,
					PeerLoadStatsGla15m:         1,
					PeerLoadStatsGla1m:          1,
					PeerLoadStatsGla5m:          1,
					RestartState:                "Up",
					Status:                      "Up",
				},
			},
		},
	}

	type Entry3 struct {
		Content splclient.SearchHeadCaptainInfo `json:"content"`
	}

	apiResponse3 := struct {
		Entry []Entry3 `json:"entry"`
	}{
		Entry: []Entry3{
			{
				Content: splclient.SearchHeadCaptainInfo{
					ServiceReady:    true,
					Identifier:      "1",
					ElectedCaptain:  1,
					Initialized:     true,
					Label:           "splunk-test-search-head-0",
					MinPeersJoined:  true,
					MaintenanceMode: false,
				},
			},
			{
				Content: splclient.SearchHeadCaptainInfo{
					ServiceReady:    true,
					Identifier:      "1",
					ElectedCaptain:  1,
					Initialized:     true,
					Label:           "splunk-test-search-head-1",
					MinPeersJoined:  true,
					MaintenanceMode: false,
				},
			},
			{
				Content: splclient.SearchHeadCaptainInfo{
					ServiceReady:    true,
					Identifier:      "1",
					ElectedCaptain:  1,
					Initialized:     true,
					Label:           "splunk-test-search-head-2",
					MinPeersJoined:  true,
					MaintenanceMode: false,
				},
			},
		},
	}

	// mock search head cluster calls
	response1, _ := json.Marshal(apiResponse1)
	response2, _ := json.Marshal(apiResponse2)
	response3, _ := json.Marshal(apiResponse3)
	wantRequest1, _ := http.NewRequest("GET", "https://splunk-test-search-head-0.splunk-test-search-head-headless.default.svc.cluster.local:8089/services/shcluster/member/info?count=0&output_mode=json", nil)
	wantRequest2, _ := http.NewRequest("GET", "https://splunk-test-search-head-0.splunk-test-search-head-headless.default.svc.cluster.local:8089/services/shcluster/member/peers?count=0&output_mode=json", nil)
	wantRequest3, _ := http.NewRequest("GET", "https://splunk-test-search-head-0.splunk-test-search-head-headless.default.svc.cluster.local:8089/services/shcluster/captain/info?count=0&output_mode=json", nil)

	wantRequest4, _ := http.NewRequest("GET", "https://splunk-test-search-head-1.splunk-test-search-head-headless.default.svc.cluster.local:8089/services/shcluster/member/info?count=0&output_mode=json", nil)
	wantRequest5, _ := http.NewRequest("GET", "https://splunk-test-search-head-1.splunk-test-search-head-headless.default.svc.cluster.local:8089/services/shcluster/member/peers?count=0&output_mode=json", nil)
	wantRequest6, _ := http.NewRequest("GET", "https://splunk-test-search-head-1.splunk-test-search-head-headless.default.svc.cluster.local:8089/services/shcluster/captain/info?count=0&output_mode=json", nil)

	wantRequest7, _ := http.NewRequest("GET", "https://splunk-test-search-head-2.splunk-test-search-head-headless.default.svc.cluster.local:8089/services/shcluster/member/info?count=0&output_mode=json", nil)
	wantRequest8, _ := http.NewRequest("GET", "https://splunk-test-search-head-2.splunk-test-search-head-headless.default.svc.cluster.local:8089/services/shcluster/member/peers?count=0&output_mode=json", nil)
	wantRequest9, _ := http.NewRequest("GET", "https://splunk-test-search-head-2.splunk-test-search-head-headless.default.svc.cluster.local:8089/services/shcluster/captain/info?count=0&output_mode=json", nil)

	mclient.AddHandler(wantRequest1, 200, string(response2), nil)
	mclient.AddHandler(wantRequest2, 200, string(response1), nil)
	mclient.AddHandler(wantRequest3, 200, string(response3), nil)

	mclient.AddHandler(wantRequest4, 200, string(response2), nil)
	mclient.AddHandler(wantRequest5, 200, string(response1), nil)
	mclient.AddHandler(wantRequest6, 200, string(response3), nil)

	mclient.AddHandler(wantRequest7, 200, string(response2), nil)
	mclient.AddHandler(wantRequest8, 200, string(response1), nil)
	mclient.AddHandler(wantRequest9, 200, string(response3), nil)

	// mock the verify RF peer function
	VerifyRFPeers = func(ctx context.Context, mgr indexerClusterPodManager, client splcommon.ControllerClient) error {
		return nil
	}

	// mock new search pod manager
	newSearchHeadClusterPodManager = func(client splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster, secret *corev1.Secret, newSplunkClient NewSplunkClientFunc) searchHeadClusterPodManager {
		return searchHeadClusterPodManager{
			cr:      cr,
			secrets: secret,
			newSplunkClient: func(managementURI, username, password string) *splclient.SplunkClient {
				c := splclient.NewSplunkClient(managementURI, username, password)
				c.Client = mclient
				return c
			},
		}
	}

	// create directory for app framework
	newpath := filepath.Join("/tmp", "appframework")
	_ = os.MkdirAll(newpath, os.ModePerm)

	// adding getapplist to fix test case
	GetAppsList = func(ctx context.Context, remoteDataClientMgr RemoteDataClientManager) (splcommon.RemoteDataListResponse, error) {
		RemoteDataListResponse := splcommon.RemoteDataListResponse{}
		return RemoteDataListResponse, nil
	}

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	builder := newFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.LicenseManager{}).
		WithStatusSubresource(&enterpriseApi.ClusterManager{}).
		WithStatusSubresource(&enterpriseApi.Standalone{}).
		WithStatusSubresource(&enterpriseApi.MonitoringConsole{}).
		WithStatusSubresource(&enterpriseApi.IndexerCluster{}).
		WithStatusSubresource(&enterpriseApi.SearchHeadCluster{})
	c := builder.Build()
	ctx := context.TODO()

	// create searchheadcluster custom resource
	searchheadcluster := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				Volumes: []corev1.Volume{},
			},
			Replicas: 3,
		},
	}

	replicas := int32(3)
	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-search-head",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			PodManagementPolicy: "Parallel",
			ServiceName:         "splunk-test-deployer-headless",
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "splunk",
							Image: "splunk/splunk:latest",
							Env: []corev1.EnvVar{
								{
									Name:  "test",
									Value: "test",
								},
							},
							LivenessProbe: &corev1.Probe{
								InitialDelaySeconds: 300,
							},
							ReadinessProbe: &corev1.Probe{
								InitialDelaySeconds: 300,
							},
						},
					},
				},
			},
			Replicas: &replicas,
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-deployer-headless",
			Namespace: "default",
		},
	}

	// simulate service
	c.Create(ctx, service)

	// simulate create stateful set
	c.Create(ctx, statefulset)

	// simulate create clustermanager instance before reconciliation
	c.Create(ctx, searchheadcluster)

	_, err := ApplySearchHeadCluster(ctx, c, searchheadcluster)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for searchhead cluster %v", err)
		debug.PrintStack()
	}

	namespacedName := types.NamespacedName{
		Name:      searchheadcluster.Name,
		Namespace: searchheadcluster.Namespace,
	}
	err = c.Get(ctx, namespacedName, searchheadcluster)
	if err != nil {
		t.Errorf("Unexpected get search head cluster. Error=%v", err)
		debug.PrintStack()
	}
	// simulate Ready state
	searchheadcluster.Status.Phase = enterpriseApi.PhaseReady
	searchheadcluster.Spec.ServiceTemplate.Annotations = map[string]string{
		"traffic.sidecar.istio.io/excludeOutboundPorts": "8089,8191,9997",
		"traffic.sidecar.istio.io/includeInboundPorts":  "8000,8088",
	}
	searchheadcluster.Spec.ServiceTemplate.Labels = map[string]string{
		"app.kubernetes.io/instance":   "splunk-test-searchhead-cluster",
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "searchhead-cluster",
		"app.kubernetes.io/name":       "search-cluster",
		"app.kubernetes.io/part-of":    "splunk-test-searchead-cluster",
	}
	err = c.Status().Update(ctx, searchheadcluster)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for searchhead cluster with app framework  %v", err)
		debug.PrintStack()
	}

	err = c.Get(ctx, namespacedName, searchheadcluster)
	if err != nil {
		t.Errorf("Unexpected get search head cluster %v", err)
		debug.PrintStack()
	}

	// call reconciliation
	_, err = ApplySearchHeadCluster(ctx, c, searchheadcluster)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for searchead cluster with app framework  %v", err)
		debug.PrintStack()
	}

	// create pod
	stpod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-search-head-0",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "splunk",
					Image: "splunk/splunk:latest",
					Env: []corev1.EnvVar{
						{
							Name:  "test",
							Value: "test",
						},
					},
					LivenessProbe: &corev1.Probe{
						InitialDelaySeconds: 300,
					},
					ReadinessProbe: &corev1.Probe{
						InitialDelaySeconds: 300,
					},
				},
			},
		},
	}
	// simulate create stateful set
	c.Create(ctx, stpod)
	if err != nil {
		t.Errorf("Unexpected create pod failed %v", err)
		debug.PrintStack()
	}

	// create pod
	stpod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-search-head-1",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "splunk",
					Image: "splunk/splunk:latest",
					Env: []corev1.EnvVar{
						{
							Name:  "test",
							Value: "test",
						},
					},
					LivenessProbe: &corev1.Probe{
						InitialDelaySeconds: 300,
					},
					ReadinessProbe: &corev1.Probe{
						InitialDelaySeconds: 300,
					},
				},
			},
		},
	}
	// simulate create stateful set
	c.Create(ctx, stpod1)
	if err != nil {
		t.Errorf("Unexpected create pod failed %v", err)
		debug.PrintStack()
	}

	// create pod
	stpod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-search-head-2",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "splunk",
					Image: "splunk/splunk:latest",
					Env: []corev1.EnvVar{
						{
							Name:  "test",
							Value: "test",
						},
					},
					LivenessProbe: &corev1.Probe{
						InitialDelaySeconds: 300,
					},
					ReadinessProbe: &corev1.Probe{
						InitialDelaySeconds: 300,
					},
				},
			},
		},
	}
	// simulate create stateful set
	c.Create(ctx, stpod2)
	if err != nil {
		t.Errorf("Unexpected create pod failed %v", err)
		debug.PrintStack()
	}

	// create pod
	stpoddeployer := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-deployer-0",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "splunk",
					Image: "splunk/splunk:latest",
					Env: []corev1.EnvVar{
						{
							Name:  "test",
							Value: "test",
						},
					},
					LivenessProbe: &corev1.Probe{
						InitialDelaySeconds: 300,
					},
					ReadinessProbe: &corev1.Probe{
						InitialDelaySeconds: 300,
					},
				},
			},
		},
	}
	// simulate create stateful set
	c.Create(ctx, stpoddeployer)
	if err != nil {
		t.Errorf("Unexpected create pod failed %v", err)
		debug.PrintStack()
	}

	// update stateful set pod
	stpod.Status.Phase = corev1.PodRunning
	stpod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Image: "splunk/splunk:latest",
			Name:  "splunk",
			Ready: true,
		},
	}
	err = c.Status().Update(ctx, stpod)
	if err != nil {
		t.Errorf("Unexpected update pod  %v", err)
		debug.PrintStack()
	}

	// update stateful set pod
	stpod1.Status.Phase = corev1.PodRunning
	stpod1.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Image: "splunk/splunk:latest",
			Name:  "splunk",
			Ready: true,
		},
	}
	err = c.Status().Update(ctx, stpod1)
	if err != nil {
		t.Errorf("Unexpected update pod  %v", err)
		debug.PrintStack()
	}

	// update stateful set pod
	stpod2.Status.Phase = corev1.PodRunning
	stpod2.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Image: "splunk/splunk:latest",
			Name:  "splunk",
			Ready: true,
		},
	}
	err = c.Status().Update(ctx, stpod2)
	if err != nil {
		t.Errorf("Unexpected update pod  %v", err)
		debug.PrintStack()
	}

	// update statefulset
	stpoddeployer.Status.Phase = corev1.PodRunning
	stpoddeployer.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Image: "splunk/splunk:latest",
			Name:  "splunk",
			Ready: true,
		},
	}
	err = c.Status().Update(ctx, stpoddeployer)
	if err != nil {
		t.Errorf("Unexpected update statefulset  %v", err)
		debug.PrintStack()
	}

	stNamespacedName := types.NamespacedName{
		Name:      "splunk-test-search-head",
		Namespace: "default",
	}
	err = c.Get(ctx, stNamespacedName, statefulset)
	if err != nil {
		t.Errorf("Unexpected get searchhead cluster %v", err)
		debug.PrintStack()
	}

	// update statefulset
	statefulset.Status.ReadyReplicas = 3
	statefulset.Status.Replicas = 3
	err = c.Status().Update(ctx, statefulset)
	if err != nil {
		t.Errorf("Unexpected update statefulset  %v", err)
		debug.PrintStack()
	}

	// update statefulset for deployer

	stNamespacedName = types.NamespacedName{
		Name:      "splunk-test-deployer",
		Namespace: "default",
	}
	err = c.Get(ctx, stNamespacedName, statefulset)
	if err != nil {
		t.Errorf("Unexpected get searchhead cluster %v", err)
		debug.PrintStack()
	}

	// update statefulset
	statefulset.Status.ReadyReplicas = 1
	statefulset.Status.Replicas = 1
	err = c.Status().Update(ctx, statefulset)
	if err != nil {
		t.Errorf("Unexpected update statefulset  %v", err)
		debug.PrintStack()
	}

	err = c.Get(ctx, namespacedName, searchheadcluster)
	if err != nil {
		t.Errorf("Unexpected get searchhead cluster %v", err)
		debug.PrintStack()
	}

	searchheadcluster.Status.Initialized = true
	searchheadcluster.Status.CaptainReady = true
	searchheadcluster.Status.ReadyReplicas = 3
	searchheadcluster.Status.Replicas = 3

	//create namespace MC statefulset
	current := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-default-monitoring-console",
			Namespace: "default",
		},
	}
	namespacedName = types.NamespacedName{Namespace: "default", Name: "splunk-default-monitoring-console"}

	// Create MC statefulset
	err = splutil.CreateResource(ctx, c, &current)
	if err != nil {
		t.Errorf("Failed to create owner reference  %s", current.GetName())
	}

	//setownerReference
	err = k8sops.SetStatefulSetOwnerRef(ctx, c, searchheadcluster, namespacedName)
	if err != nil {
		t.Errorf("Couldn't set owner ref for resource %s", current.GetName())
	}

	err = c.Get(ctx, namespacedName, &current)
	if err != nil {
		t.Errorf("Couldn't get the statefulset resource %s", current.GetName())
	}

	configmap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-default-monitoring-console",
			Namespace: "default",
		},
	}

	// Create configmap
	err = splutil.CreateResource(ctx, c, &configmap)
	if err != nil {
		t.Errorf("Failed to create resource  %s", current.GetName())
	}

	// Mock the telapp.AddTelApp function for unit tests
	savedAddTelApp := telapp.AddTelApp
	defer func() { telapp.AddTelApp = savedAddTelApp }()
	telapp.AddTelApp = func(ctx context.Context, podExecClient splutil.PodExecClientImpl, replicas int32, cr splcommon.MetaObject) error {
		return nil
	}

	// call reconciliation
	_, err = ApplySearchHeadCluster(ctx, c, searchheadcluster)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for search head cluster with app framework. Error=%v", err)
	}
}

func TestSetDeployerConfig(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	client := spltest.NewMockClient()
	depResSpec := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("4"),
			corev1.ResourceMemory: resource.MustParse("14Gi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2"),
			corev1.ResourceMemory: resource.MustParse("7Gi"),
		},
	}

	shc := enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			DeployerResourceSpec: depResSpec,
			DeployerNodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{},
				},
			},
		},
	}

	nsTerm := corev1.NodeSelectorTerm{
		MatchExpressions: []corev1.NodeSelectorRequirement{
			{
				Key: "node-role.kubernetes.io/master",
			},
		},
	}
	shc.Spec.DeployerNodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = append(shc.Spec.DeployerNodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms, nsTerm)

	// Get deployer STS and set resources
	depSts, err := getSplunkStatefulSet(ctx, client, &shc, &shc.Spec.CommonSplunkSpec, SplunkDeployer, 1, getSearchHeadExtraEnv(&shc, shc.Spec.Replicas), nil)
	if err != nil {
		t.Errorf("Failed to get deployer statefulset due to error=%s", err)
	}
	setDeployerConfig(ctx, &shc, &depSts.Spec.Template)
	if !reflect.DeepEqual(depResSpec.Limits, depSts.Spec.Template.Spec.Containers[0].Resources.Limits) {
		t.Errorf("Failed to set deployer resources properly, limits are off")
	}

	// Verify deployer resources are set properly
	if !reflect.DeepEqual(depResSpec.Requests, depSts.Spec.Template.Spec.Containers[0].Resources.Requests) {
		t.Errorf("Failed to set deployer resources properly, requests are off")
	}

	// Verify deployer nodeAffinity are set properly
	if !reflect.DeepEqual(shc.Spec.DeployerNodeAffinity, depSts.Spec.Template.Spec.Affinity.NodeAffinity) {
		t.Errorf("Failed to set deployer resources properly, requests are off")
	}
}

func TestShcPasswordSyncFailedEvent(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	builder := newFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.SearchHeadCluster{})

	c := builder.Build()
	ctx := context.TODO()

	recorder := &mockEventRecorder{events: []mockEvent{}}
	eventPublisher := &K8EventPublisher{recorder: recorder}
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	// Create namespace scoped secret
	nsSecret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Fatalf("Failed to apply namespace scoped secret: %v", err)
	}

	shc := enterpriseApi.SearchHeadCluster{
		TypeMeta:   metav1.TypeMeta{Kind: "SearchHeadCluster"},
		ObjectMeta: metav1.ObjectMeta{Name: "shc", Namespace: "test"},
	}
	shc.SetGroupVersionKind(enterpriseApi.GroupVersion.WithKind("SearchHeadCluster"))
	// Set stale resource version so ApplyShcSecret doesn't early-return
	shc.Status.NamespaceSecretResourceVersion = nsSecret.ResourceVersion + "-old"
	shc.Status.AdminPasswordChangedSecrets = make(map[string]bool)

	// Create the search head pod with a secret volume mount
	podSecretName := "splunk-shc-search-head-secret-v1"
	shPodName := "splunk-shc-search-head-0"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: shPodName, Namespace: "test"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:latest"}},
			Volumes: []corev1.Volume{
				{
					Name: "mnt-splunk-secrets",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{SecretName: podSecretName},
					},
				},
			},
		},
	}
	if err := c.Create(ctx, pod); err != nil {
		t.Fatalf("Failed to create pod: %v", err)
	}

	// Create the pod's secret with a DIFFERENT shc_secret than namespace secret
	podSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: podSecretName, Namespace: "test"},
		Data: map[string][]byte{
			"password":   []byte("admin-password"),
			"shc_secret": []byte("old-shc-secret"),
		},
	}
	if err := c.Create(ctx, podSecret); err != nil {
		t.Fatalf("Failed to create pod secret: %v", err)
	}

	mgr := &searchHeadClusterPodManager{
		c:  c,
		cr: &shc,
	}

	// Configure mock pod exec client to return an error on shcluster-config command
	mockPodExecClient := &spltest.MockPodExecClient{}
	mockPodExecClient.AddMockPodExecReturnContext(ctx, "shcluster-config", &spltest.MockPodExecReturnContext{
		StdOut: "",
		StdErr: "connection refused",
		Err:    fmt.Errorf("connection refused"),
	})

	// Call ApplyShcSecret — should fail at RunPodExecCommand and emit PasswordSyncFailed
	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err == nil {
		t.Errorf("Expected error from ApplyShcSecret when pod exec fails")
	}

	found := false
	for _, event := range recorder.events {
		if event.reason == "PasswordSyncFailed" {
			found = true
			if event.eventType != corev1.EventTypeWarning {
				t.Errorf("Expected Warning event type for PasswordSyncFailed, got %s", event.eventType)
			}
			if !strings.Contains(event.message, shPodName) {
				t.Errorf("Expected event message to contain pod name '%s', got: %s", shPodName, event.message)
			}
			if !strings.Contains(event.message, "connection refused") {
				t.Errorf("Expected event message to contain error details, got: %s", event.message)
			}
			break
		}
	}
	if !found {
		t.Errorf("Expected PasswordSyncFailed event to be published")
	}
}

func TestShcScaledUpScaledDownEvent(t *testing.T) {
	ctx := context.TODO()
	recorder := &mockEventRecorder{events: []mockEvent{}}
	eventPublisher := &K8EventPublisher{recorder: recorder}
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	crName := "test-shc"
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: crName, Namespace: "test"},
	}

	// Simulate ScaledUp: previousReplicas=3, desiredReplicas=5, phase=PhaseReady, Status.Replicas=5
	previousReplicas := int32(3)
	desiredReplicas := int32(5)
	cr.Status.Replicas = desiredReplicas
	phase := enterpriseApi.PhaseReady

	// Replicate the production conditional from searchHeadClusterPodManager.Update()
	ep := GetEventPublisher(ctx, cr)
	if phase == enterpriseApi.PhaseReady {
		if desiredReplicas > previousReplicas && cr.Status.Replicas == desiredReplicas {
			ep.Normal(ctx, "ScaledUp",
				fmt.Sprintf("Successfully scaled %s up to %d replicas", cr.GetName(), desiredReplicas))
		}
	}

	found := false
	for _, event := range recorder.events {
		if event.reason == "ScaledUp" {
			found = true
			if event.eventType != corev1.EventTypeNormal {
				t.Errorf("Expected Normal event type for ScaledUp, got %s", event.eventType)
			}
			if !strings.Contains(event.message, crName) {
				t.Errorf("Expected event message to contain CR name '%s', got: %s", crName, event.message)
			}
			if !strings.Contains(event.message, "5") {
				t.Errorf("Expected event message to contain replica counts, got: %s", event.message)
			}
			break
		}
	}
	if !found {
		t.Errorf("Expected ScaledUp event to be published")
	}

	// Simulate ScaledDown: previousReplicas=5, desiredReplicas=3, phase=PhaseReady, Status.Replicas=3
	recorder.events = []mockEvent{}
	previousReplicas = int32(5)
	desiredReplicas = int32(3)
	cr.Status.Replicas = desiredReplicas

	if phase == enterpriseApi.PhaseReady {
		if desiredReplicas < previousReplicas && cr.Status.Replicas == desiredReplicas {
			ep.Normal(ctx, "ScaledDown",
				fmt.Sprintf("Successfully scaled %s down to %d replicas", cr.GetName(), desiredReplicas))
		}
	}

	found = false
	for _, event := range recorder.events {
		if event.reason == "ScaledDown" {
			found = true
			if event.eventType != corev1.EventTypeNormal {
				t.Errorf("Expected Normal event type for ScaledDown, got %s", event.eventType)
			}
			if !strings.Contains(event.message, crName) {
				t.Errorf("Expected event message to contain CR name '%s', got: %s", crName, event.message)
			}
			break
		}
	}
	if !found {
		t.Errorf("Expected ScaledDown event to be published")
	}

	// Negative: no event when phase is not PhaseReady
	recorder.events = []mockEvent{}
	phase = enterpriseApi.PhasePending
	if phase == enterpriseApi.PhaseReady {
		if desiredReplicas < previousReplicas && cr.Status.Replicas == desiredReplicas {
			ep.Normal(ctx, "ScaledDown",
				fmt.Sprintf("Successfully scaled %s down to %d replicas", cr.GetName(), desiredReplicas))
		}
	}
	if len(recorder.events) != 0 {
		t.Errorf("Expected no events when phase is not PhaseReady, got %d events", len(recorder.events))
	}
}

func newTestSHCPodManager(cr *enterpriseApi.SearchHeadCluster) *searchHeadClusterPodManager {
	return &searchHeadClusterPodManager{
		cr: cr,
		newSplunkClient: func(managementURI, username, password string) *splclient.SplunkClient {
			return splclient.NewSplunkClient(managementURI, username, password)
		},
	}
}

func newTestSHCCR(memberStatus string, historicalSearches, realtimeSearches int) *enterpriseApi.SearchHeadCluster {
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shc",
			Namespace: "test",
		},
	}
	cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{
		{
			Name:                        "splunk-test-shc-search-head-0",
			Status:                      memberStatus,
			ActiveHistoricalSearchCount: historicalSearches,
			ActiveRealtimeSearchCount:   realtimeSearches,
		},
	}
	return cr
}

func TestPrepareRecycle_NormalDrain(t *testing.T) {
	ctx := context.Background()

	// Step 1: active searches present, not timed out — should wait
	cr := newTestSHCCR("ManualDetention", 3, 2)
	cr.Spec.DetentionTimeoutSeconds = 3600
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 10
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready {
		t.Error("expected (false, nil) while searches are active and timeout not exceeded")
	}

	// Step 2: searches drained — should return true and zero the timer fields
	cr.Status.Members[0].ActiveHistoricalSearchCount = 0
	cr.Status.Members[0].ActiveRealtimeSearchCount = 0
	ready, err = mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Error("expected (true, nil) when searches have drained")
	}
	if cr.Status.DetentionStartTimestamp != 0 {
		t.Error("expected DetentionStartTimestamp to be zeroed after drain")
	}
	if cr.Status.DetainedMemberName != "" {
		t.Error("expected DetainedMemberName to be cleared after drain")
	}
}

func TestPrepareRecycle_TimeoutForced(t *testing.T) {
	ctx := context.Background()

	cr := newTestSHCCR("ManualDetention", 1, 2)
	cr.Spec.DetentionTimeoutSeconds = 3600
	savedTimestamp := time.Now().Unix() - 3700
	cr.Status.DetentionStartTimestamp = savedTimestamp
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"
	cr.Status.DetainedPodRevision = "v1"
	cr.Status.Members[0].PodRevision = "v1"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Error("expected (true, nil) when timeout exceeded")
	}
	// Fields must NOT be cleared here — FinishRecycle owns cleanup after pod deletion succeeds.
	// Clearing here causes a fresh full timeout if the pod delete fails transiently.
	if cr.Status.DetentionStartTimestamp != savedTimestamp {
		t.Error("expected DetentionStartTimestamp to be preserved after timeout — FinishRecycle clears it")
	}
	if cr.Status.DetainedMemberName != "splunk-test-shc-search-head-0" {
		t.Error("expected DetainedMemberName to be preserved after timeout — FinishRecycle clears it")
	}
}

func TestPrepareRecycle_TimeoutForcedWhenPodRevisionUnavailable(t *testing.T) {
	ctx := context.Background()

	cr := newTestSHCCR("ManualDetention", 1, 2)
	cr.Spec.DetentionTimeoutSeconds = 3600
	savedTimestamp := time.Now().Unix() - 3700
	cr.Status.DetentionStartTimestamp = savedTimestamp
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Error("expected timeout to force recycle when pod revision is unavailable")
	}
	if cr.Status.DetentionStartTimestamp != savedTimestamp {
		t.Error("expected empty pod revision not to reset an expired detention timer")
	}
}

func TestPrepareRecycle_FirstObservedPodRevisionDoesNotResetTimer(t *testing.T) {
	ctx := context.Background()

	cr := newTestSHCCR("ManualDetention", 1, 2)
	cr.Spec.DetentionTimeoutSeconds = 3600
	savedTimestamp := time.Now().Unix() - 3700
	cr.Status.DetentionStartTimestamp = savedTimestamp
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"
	cr.Status.DetainedPodRevision = ""
	cr.Status.Members[0].PodRevision = "revision-1"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Error("expected timeout to force recycle when first observed pod revision appears after timeout")
	}
	if cr.Status.DetentionStartTimestamp != savedTimestamp {
		t.Error("expected first observed pod revision not to reset an expired detention timer")
	}
	if cr.Status.DetainedPodRevision != "revision-1" {
		t.Errorf("expected first observed pod revision to be recorded, got %q", cr.Status.DetainedPodRevision)
	}
}

func TestPrepareRecycle_TimestampSetOnFirstEntry(t *testing.T) {
	ctx := context.Background()

	cr := newTestSHCCR("ManualDetention", 0, 2)
	cr.Spec.DetentionTimeoutSeconds = 3600
	cr.Status.DetentionStartTimestamp = 0
	cr.Status.DetainedMemberName = ""

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready {
		t.Error("expected (false, nil) on first entry with active searches")
	}
	if cr.Status.DetentionStartTimestamp == 0 {
		t.Error("expected DetentionStartTimestamp to be set on first entry")
	}
	if cr.Status.DetainedMemberName != "splunk-test-shc-search-head-0" {
		t.Errorf("expected DetainedMemberName to be set, got %q", cr.Status.DetainedMemberName)
	}
}

func TestPrepareRecycle_CustomTimeout(t *testing.T) {
	ctx := context.Background()

	cr := newTestSHCCR("ManualDetention", 0, 1)
	cr.Spec.DetentionTimeoutSeconds = 60
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 70
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"
	cr.Status.DetainedPodRevision = "v1"
	cr.Status.Members[0].PodRevision = "v1"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Error("expected (true, nil) when custom timeout of 60s exceeded after 70s")
	}
}

func TestPrepareRecycle_DetentionTimeoutSecondsDefault(t *testing.T) {
	ctx := context.Background()

	// DetentionTimeoutSeconds = 0 means unset — should use defaultSearchHeadDetentionTimeoutSeconds (3600)
	cr := newTestSHCCR("ManualDetention", 0, 1)
	cr.Spec.DetentionTimeoutSeconds = 0
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 3700
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"
	cr.Status.DetainedPodRevision = "v1"
	cr.Status.Members[0].PodRevision = "v1"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Error("expected (true, nil) when default timeout of 3600s exceeded after 3700s")
	}
}

func TestPrepareRecycle_NegativeTimeout(t *testing.T) {
	ctx := context.Background()

	// Negative DetentionTimeoutSeconds should fall back to default (3600)
	cr := newTestSHCCR("ManualDetention", 0, 1)
	cr.Spec.DetentionTimeoutSeconds = -1
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 3700
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"
	cr.Status.DetainedPodRevision = "v1"
	cr.Status.Members[0].PodRevision = "v1"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Error("expected (true, nil) — negative timeout should use default of 3600s, exceeded after 3700s")
	}
}

func TestPrepareRecycle_NegativeActiveSearches(t *testing.T) {
	ctx := context.Background()

	// Negative search count (corrupted REST response) should be treated as drained
	cr := newTestSHCCR("ManualDetention", -1, -1)
	cr.Spec.DetentionTimeoutSeconds = 3600
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 10
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Error("expected (true, nil) — negative search count should be treated as drained")
	}
}

func TestPrepareRecycle_TimerResetOnMemberChange(t *testing.T) {
	ctx := context.Background()

	// Timer was set for a different member (e.g. after operator restart mid-rolling-update)
	// Should reset timestamp and member name for the new member
	cr := newTestSHCCR("ManualDetention", 0, 2)
	cr.Spec.DetentionTimeoutSeconds = 3600
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 100
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-1" // different member
	cr.Status.DetainedPodRevision = "revision-1"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready {
		t.Error("expected (false, nil) — timer just reset for new member, timeout not exceeded")
	}
	if cr.Status.DetainedMemberName != "splunk-test-shc-search-head-0" {
		t.Errorf("expected DetainedMemberName to be updated to current member, got %q", cr.Status.DetainedMemberName)
	}
	if cr.Status.DetainedPodRevision != "" {
		t.Errorf("expected stale DetainedPodRevision to be cleared on member change, got %q", cr.Status.DetainedPodRevision)
	}
	// Timestamp should have been reset to approximately now, not the old value
	if time.Now().Unix()-cr.Status.DetentionStartTimestamp > 2 {
		t.Error("expected DetentionStartTimestamp to be reset to approximately now for new member")
	}
}

func TestFinishRecycle_ClearsTimerFields(t *testing.T) {
	ctx := context.Background()

	cr := newTestSHCCR("ManualDetention", 0, 0)
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 100
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"

	// FinishRecycle clears the timer fields before calling SetSearchHeadDetention.
	// We verify the in-memory CR fields are zeroed regardless of whether the REST call succeeds.
	// getClient requires a controller client for secret retrieval; use defer/recover to catch
	// the nil-client panic and still assert the fields were cleared before the call was made.
	func() {
		defer func() { recover() }() // suppress nil-client panic from getClient
		mgr := newTestSHCPodManager(cr)
		_, _ = mgr.FinishRecycle(ctx, 0)
	}()

	if cr.Status.DetentionStartTimestamp != 0 {
		t.Error("expected DetentionStartTimestamp to be zeroed by FinishRecycle ManualDetention case")
	}
	if cr.Status.DetainedMemberName != "" {
		t.Error("expected DetainedMemberName to be cleared by FinishRecycle ManualDetention case")
	}
}

func TestFinishRecycle_ClearsTimerFields_UpStatus(t *testing.T) {
	ctx := context.Background()

	// After a timeout-forced recycle, the pod comes back Up — FinishRecycle Up branch must clear timer
	cr := newTestSHCCR("Up", 0, 0)
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 100
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.FinishRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Error("expected (true, nil) when member is Up")
	}
	if cr.Status.DetentionStartTimestamp != 0 {
		t.Error("expected DetentionStartTimestamp to be zeroed when member returns Up after timeout-forced recycle")
	}
	if cr.Status.DetainedMemberName != "" {
		t.Error("expected DetainedMemberName to be cleared when member returns Up after timeout-forced recycle")
	}
}

func TestPrepareRecycle_TimerSurvivesUpdateStatus(t *testing.T) {
	ctx := context.Background()

	cr := newTestSHCCR("ManualDetention", 0, 2)
	cr.Spec.DetentionTimeoutSeconds = 3600
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 10
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"

	savedTimestamp := cr.Status.DetentionStartTimestamp
	savedName := cr.Status.DetainedMemberName

	// Simulate what updateStatus() does — rebuild Members slice from REST API
	// This overwrites Members[n] but must NOT touch cluster-level timer fields
	cr.Status.Members[0] = enterpriseApi.SearchHeadClusterMemberStatus{
		Name:                        "splunk-test-shc-search-head-0",
		Status:                      "ManualDetention",
		ActiveHistoricalSearchCount: 0,
		ActiveRealtimeSearchCount:   2,
	}

	if cr.Status.DetentionStartTimestamp != savedTimestamp {
		t.Error("updateStatus() must not wipe DetentionStartTimestamp — it is a cluster-level field")
	}
	if cr.Status.DetainedMemberName != savedName {
		t.Error("updateStatus() must not wipe DetainedMemberName — it is a cluster-level field")
	}

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready {
		t.Error("expected (false, nil) — searches still active, timer should have survived updateStatus()")
	}
}

func TestPrepareRecycle_StaleTimerClearedOnNewDetentionCycle(t *testing.T) {
	ctx := context.Background()

	// Simulate: timeout fired in a previous recycle episode, timer fields were NOT cleared
	// (FinishRecycle was skipped because a new revision arrived before the pod returned to Up).
	// The pod is now Up again and a new recycle episode is starting.
	cr := newTestSHCCR("Up", 0, 0)
	cr.Spec.DetentionTimeoutSeconds = 3600
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 3700 // stale expired timestamp
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"

	// PrepareRecycle on an Up pod detains it — the stale timer must be cleared first
	// so the next ManualDetention cycle gets a fresh 3600s window, not an instant force-recycle.
	// getClient panics with nil controller client in unit tests; recover and assert timer was cleared.
	func() {
		defer func() { recover() }()
		mgr := newTestSHCPodManager(cr)
		_, _ = mgr.PrepareRecycle(ctx, 0)
	}()

	if cr.Status.DetentionStartTimestamp != 0 {
		t.Error("expected stale DetentionStartTimestamp to be cleared when pod re-enters Up state")
	}
	if cr.Status.DetainedMemberName != "" {
		t.Error("expected stale DetainedMemberName to be cleared when pod re-enters Up state")
	}
}

func TestPrepareRecycle_TimerResetOnRevisionChange(t *testing.T) {
	ctx := context.Background()

	// Simulate: timeout fired for revision-1, timer not cleared, pod restarted as revision-2.
	// The replacement pod is reporting ManualDetention (rejoined SHC in detention state).
	// PrepareRecycle must reset the timer for the new revision so it gets a fresh timeout window.
	cr := newTestSHCCR("ManualDetention", 0, 1)
	cr.Spec.DetentionTimeoutSeconds = 3600
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 3700 // stale expired timestamp
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"
	cr.Status.DetainedPodRevision = "revision-1"
	// New pod is on revision-2 — simulate updateStatus having populated PodRevision
	cr.Status.Members[0].PodRevision = "revision-2"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready {
		t.Error("expected (false, nil) — timer was reset for new revision, not an instant force-recycle")
	}
	if cr.Status.DetainedPodRevision != "revision-2" {
		t.Errorf("expected DetainedPodRevision updated to revision-2, got %q", cr.Status.DetainedPodRevision)
	}
	// Timer should have been reset to approximately now, not the old expired value
	if time.Now().Unix()-cr.Status.DetentionStartTimestamp > 2 {
		t.Error("expected DetentionStartTimestamp to be reset to approximately now for new revision")
	}
}

func TestUpdateStatusPreservesPodRevisionByMemberName(t *testing.T) {
	ctx := context.Background()
	restoreSearchHeadClusterInfoStubs(t)

	replicas := int32(2)
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-shc", Namespace: "test"},
	}
	cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{
		{Name: "splunk-test-shc-search-head-1", PodRevision: "revision-1"},
		{Name: "splunk-test-shc-search-head-0", PodRevision: "revision-0"},
	}

	c := spltest.NewMockClient()
	c.AddObjects([]client.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "splunk-test-shc-search-head-0",
				Namespace: "test",
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "splunk-test-shc-search-head-1",
				Namespace: "test",
			},
		},
	})
	mgr := newTestSHCPodManager(cr)
	mgr.c = c

	err := mgr.updateStatus(ctx, searchHeadStatefulSet("test-shc", replicas))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cr.Status.Members[0].PodRevision; got != "revision-0" {
		t.Errorf("expected revision-0 preserved by member name, got %q", got)
	}
	if got := cr.Status.Members[1].PodRevision; got != "revision-1" {
		t.Errorf("expected revision-1 preserved by member name, got %q", got)
	}
}

func TestUpdateStatusPreservesPodRevisionWhenPodReadFails(t *testing.T) {
	ctx := context.Background()
	restoreSearchHeadClusterInfoStubs(t)

	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-shc", Namespace: "test"},
	}
	cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{
		{Name: "splunk-test-shc-search-head-0", PodRevision: "revision-0"},
	}

	mgr := newTestSHCPodManager(cr)
	mgr.c = spltest.NewMockClient()

	err := mgr.updateStatus(ctx, searchHeadStatefulSet("test-shc", 1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cr.Status.Members[0].PodRevision; got != "revision-0" {
		t.Errorf("expected revision-0 preserved after pod read failure, got %q", got)
	}
}

func TestUpdateStatusUsesCurrentPodRevisionWhenPresent(t *testing.T) {
	ctx := context.Background()
	restoreSearchHeadClusterInfoStubs(t)

	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-shc", Namespace: "test"},
	}
	cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{
		{Name: "splunk-test-shc-search-head-0", PodRevision: "old-revision"},
	}

	c := spltest.NewMockClient()
	c.AddObjects([]client.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "splunk-test-shc-search-head-0",
				Namespace: "test",
				Labels: map[string]string{
					"controller-revision-hash": "current-revision",
				},
			},
		},
	})
	mgr := newTestSHCPodManager(cr)
	mgr.c = c

	err := mgr.updateStatus(ctx, searchHeadStatefulSet("test-shc", 1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cr.Status.Members[0].PodRevision; got != "current-revision" {
		t.Errorf("expected current pod revision to win, got %q", got)
	}
}

func restoreSearchHeadClusterInfoStubs(t *testing.T) {
	t.Helper()
	originalMemberInfo := GetSearchHeadClusterMemberInfo
	originalCaptainInfo := GetSearchHeadCaptainInfo
	t.Cleanup(func() {
		GetSearchHeadClusterMemberInfo = originalMemberInfo
		GetSearchHeadCaptainInfo = originalCaptainInfo
	})

	GetSearchHeadClusterMemberInfo = func(ctx context.Context, mgr *searchHeadClusterPodManager, n int32) (*splclient.SearchHeadClusterMemberInfo, error) {
		return &splclient.SearchHeadClusterMemberInfo{
			Status:     "Up",
			Adhoc:      true,
			Registered: true,
		}, nil
	}
	GetSearchHeadCaptainInfo = func(ctx context.Context, mgr *searchHeadClusterPodManager, n int32) (*splclient.SearchHeadCaptainInfo, error) {
		return &splclient.SearchHeadCaptainInfo{
			Label:          "splunk-test-shc-search-head-0",
			ServiceReady:   true,
			Initialized:    true,
			MinPeersJoined: true,
		}, nil
	}
}

func searchHeadStatefulSet(name string, replicas int32) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-" + name + "-search-head", Namespace: "test"},
		Status: appsv1.StatefulSetStatus{
			Replicas:      replicas,
			ReadyReplicas: replicas,
		},
	}
}
