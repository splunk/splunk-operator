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
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/go-logr/logr"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
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

	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Service-test-splunk-stack1-search-head-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-search-head-service"},
		{MetaName: "*v4.Deployer-test-stack1"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
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
		{MetaName: "*v1.Service-test-splunk-stack1-search-head-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-search-head-service"},
		{MetaName: "*v4.Deployer-test-stack1"},
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

	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[5], funcCalls[0], funcCalls[3], funcCalls[4], funcCalls[8], funcCalls[10], funcCalls[11]}, "Update": {funcCalls[0]}, "List": {listmockCall[0]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": createFuncCalls, "Create": {funcCalls[5]}, "Update": {createFuncCalls[5]}, "List": {listmockCall[0]}}
	shcCr := enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			DeployerRef: corev1.ObjectReference{
				Name: "stack1",
			},
		},
	}

	// Set shc changed to true for testing
	searchHeads := 3
	for i := 0; i < searchHeads; i++ {
		shcCr.Status.ShcSecretChanged = append(shcCr.Status.ShcSecretChanged, true)
	}
	revised := shcCr.DeepCopy()
	revised.Spec.Image = "splunk/test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		deployerCr := enterpriseApi.Deployer{
			TypeMeta: metav1.TypeMeta{
				Kind: "Deployer",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "stack1",
				Namespace: "test",
			},
			Status: enterpriseApi.DeployerStatus{
				Phase: enterpriseApi.PhaseReady,
			},
		}
		c.Create(context.TODO(), &deployerCr)
		_, err := ApplySearchHeadCluster(context.TODO(), c, cr.(*enterpriseApi.SearchHeadCluster))
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplySearchHeadCluster", &shcCr, revised, createCalls, updateCalls, reconcile, true)

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

	// test for updating
	scopedLog := logt.WithName(method)
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
		log:     scopedLog,
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

	wantCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2]}, "Create": {funcCalls[1]}}

	// test API failure
	method := "searchHeadClusterPodManager.Update(API failure)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhasePending, statefulSet, wantCalls, nil, statefulSet)

	// test 1 ready pod
	mockHandlers = []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/member/info?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   `{"links":{},"origin":"https://localhost:8089/services/shcluster/member/info","updated":"2020-03-15T16:30:38+00:00","generator":{"build":"a7f645ddaf91","version":"8.0.2"},"entry":[{"name":"member","id":"https://localhost:8089/services/shcluster/member/info/member","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/services/shcluster/member/info/member","list":"/services/shcluster/member/info/member"},"author":"system","acl":{"app":"","can_list":true,"can_write":true,"modifiable":false,"owner":"system","perms":{"read":["admin","splunk-system-role"],"write":["admin","splunk-system-role"]},"removable":false,"sharing":"system"},"content":{"active_historical_search_count":0,"active_realtime_search_count":0,"adhoc_searchhead":false,"eai:acl":null,"is_registered":true,"last_heartbeat_attempt":1584289836,"maintenance_mode":false,"no_artifact_replications":false,"peer_load_stats_gla_15m":0,"peer_load_stats_gla_1m":0,"peer_load_stats_gla_5m":0,"peer_load_stats_max_runtime":0,"peer_load_stats_num_autosummary":0,"peer_load_stats_num_historical":0,"peer_load_stats_num_realtime":0,"peer_load_stats_num_running":0,"peer_load_stats_total_runtime":0,"restart_state":"NoRestart","status":"Up"}}],"paging":{"total":1,"perPage":30,"offset":0},"messages":[]}`,
		}, {
			Method: "GET",
			URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/captain/info?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   `{"links":{},"origin":"https://localhost:8089/services/shcluster/captain/info","updated":"2020-03-15T16:36:42+00:00","generator":{"build":"a7f645ddaf91","version":"8.0.2"},"entry":[{"name":"captain","id":"https://localhost:8089/services/shcluster/captain/info/captain","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/services/shcluster/captain/info/captain","list":"/services/shcluster/captain/info/captain"},"author":"system","acl":{"app":"","can_list":true,"can_write":true,"modifiable":false,"owner":"system","perms":{"read":["admin","splunk-system-role"],"write":["admin","splunk-system-role"]},"removable":false,"sharing":"system"},"content":{"eai:acl":null,"elected_captain":1584139352,"id":"A9D5FCCF-EB93-4E0A-93E1-45B56483EA7A","initialized_flag":true,"label":"splunk-s2-search-head-0","maintenance_mode":false,"mgmt_uri":"https://splunk-s2-search-head-0.splunk-s2-search-head-headless.splunk.svc.cluster.local:8089","min_peers_joined_flag":true,"peer_scheme_host_port":"https://splunk-s2-search-head-0.splunk-s2-search-head-headless.splunk.svc.cluster.local:8089","rolling_restart_flag":false,"service_ready_flag":true,"start_time":1584139291}}],"paging":{"total":1,"perPage":30,"offset":0},"messages":[]}`,
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
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2], funcCalls[5]}, "Create": {funcCalls[1]}, "List": {listmockCall[0]}}
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhaseReady, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => transition to detention
	mockHandlers = append(mockHandlers, spltest.MockHTTPHandler{
		Method: "POST",
		URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/member/control/control/set_manual_detention?manual_detention=on",
		Status: 200,
		Err:    nil,
		Body:   ``,
	})
	pod.ObjectMeta.Labels["controller-revision-hash"] = "v0"
	method = "searchHeadClusterPodManager.Update(Quarantine Pod)"
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2], funcCalls[5], funcCalls[2], funcCalls[2]}, "Create": {funcCalls[1]}}
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => wait for searches to drain
	mockHandlers = []spltest.MockHTTPHandler{mockHandlers[0], mockHandlers[1]}
	mockHandlers[0].Body = strings.Replace(mockHandlers[0].Body, `"status":"Up"`, `"status":"ManualDetention"`, 1)
	mockHandlers[0].Body = strings.Replace(mockHandlers[0].Body, `"active_historical_search_count":0`, `"active_historical_search_count":1`, 1)
	method = "searchHeadClusterPodManager.Update(Draining Searches)"
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2], funcCalls[5]}, "Create": {funcCalls[1]}}
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => delete pod
	mockHandlers[0].Body = strings.Replace(mockHandlers[0].Body, `"active_historical_search_count":1`, `"active_historical_search_count":0`, 1)
	method = "searchHeadClusterPodManager.Update(Delete Pod)"
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2], funcCalls[5]}, "Create": {funcCalls[1]}, "Delete": {funcCalls[5]}}
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
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2], funcCalls[5], funcCalls[2]}, "Create": {funcCalls[1]}}
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test scale down => remove member
	mockHandlers[2] = spltest.MockHTTPHandler{
		Method: "GET",
		URL:    "https://splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/member/info?count=0&output_mode=json",
		Status: 200,
		Err:    nil,
		Body:   `{"links":{},"origin":"https://localhost:8089/services/shcluster/member/info","updated":"2020-03-15T16:30:38+00:00","generator":{"build":"a7f645ddaf91","version":"8.0.2"},"entry":[{"name":"member","id":"https://localhost:8089/services/shcluster/member/info/member","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/services/shcluster/member/info/member","list":"/services/shcluster/member/info/member"},"author":"system","acl":{"app":"","can_list":true,"can_write":true,"modifiable":false,"owner":"system","perms":{"read":["admin","splunk-system-role"],"write":["admin","splunk-system-role"]},"removable":false,"sharing":"system"},"content":{"active_historical_search_count":0,"active_realtime_search_count":0,"adhoc_searchhead":false,"eai:acl":null,"is_registered":true,"last_heartbeat_attempt":1584289836,"maintenance_mode":false,"no_artifact_replications":false,"peer_load_stats_gla_15m":0,"peer_load_stats_gla_1m":0,"peer_load_stats_gla_5m":0,"peer_load_stats_max_runtime":0,"peer_load_stats_num_autosummary":0,"peer_load_stats_num_historical":0,"peer_load_stats_num_realtime":0,"peer_load_stats_num_running":0,"peer_load_stats_total_runtime":0,"restart_state":"NoRestart","status":"ManualDetention"}}],"paging":{"total":1,"perPage":30,"offset":0},"messages":[]}`,
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
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-1"},
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

func TestApplyShcSecret(t *testing.T) {
	ctx := context.TODO()
	method := "ApplyShcSecret"
	scopedLog := logt.WithName(method)
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
		log:     scopedLog,
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
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}

	mockPodExecReturnContexts[0].Err = nil
	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err == nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
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

	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err.Error() != fmt.Sprintf(splcommon.SecretTokenNotRetrievable, "shc_secret") {
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

	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err.Error() != fmt.Sprintf(splcommon.SecretTokenNotRetrievable, "admin password") {
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

func TestGetSearchHeadStatefulSet(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			DeployerRef: corev1.ObjectReference{
				Name: "stack1",
			},
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
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":3,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)

	cr.Spec.Replicas = 4
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":4,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-3.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)

	cr.Spec.Replicas = 5
	cr.Spec.ClusterManagerRef.Name = "stack1"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":5,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-3.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-4.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack1-cluster-manager-service"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)

	cr.Spec.Replicas = 6

	cr.Spec.ClusterManagerRef.Namespace = "test2"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":6,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-3.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-4.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-5.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack1-cluster-manager-service.test2.svc.cluster.local"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)

	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":6,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-3.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-4.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-5.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack1-cluster-manager-service.test2.svc.cluster.local"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)

	// Define additional service port in CR and verified the statefulset has the new port
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":6,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-3.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-4.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-5.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack1-cluster-manager-service.test2.svc.cluster.local"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)

	// Create a serviceaccount
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":6,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-3.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-4.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-5.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack1-cluster-manager-service.test2.svc.cluster.local"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent"}],"serviceAccountName":"defaults","securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)

	// Add extraEnv
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":6,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-3.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-4.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-5.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack1-cluster-manager-service.test2.svc.cluster.local"},{"name":"TEST_ENV_VAR","value":"test_value"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent"}],"serviceAccountName":"defaults","securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)
}

func TestApplySearchHeadClusterDeletion(t *testing.T) {
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
			DeployerRef: corev1.ObjectReference{
				Name: "stack1",
			},
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
		t.Errorf(err.Error())
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
		t.Errorf("ApplySearchHeadCluster should not have returned error here. Error %v", err)
	}
}

func TestGetSearchHeadClusterList(t *testing.T) {
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

	// mock the verify RF peer funciton
	VerifyRFPeers = func(ctx context.Context, mgr indexerClusterPodManager, client splcommon.ControllerClient) error {
		return nil
	}

	// mock new search pod manager
	newSerachHeadClusterPodManager = func(client splcommon.ControllerClient, log logr.Logger, cr *enterpriseApi.SearchHeadCluster, secret *corev1.Secret, newSplunkClient NewSplunkClientFunc) searchHeadClusterPodManager {
		return searchHeadClusterPodManager{
			log:     log,
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
	GetAppsList = func(ctx context.Context, remoteDataClientMgr RemoteDataClientManager) (splclient.RemoteDataListResponse, error) {
		RemoteDataListResponse := splclient.RemoteDataListResponse{}
		return RemoteDataListResponse, nil
	}

	builder := fake.NewClientBuilder()
	c := builder.Build()
	utilruntime.Must(enterpriseApi.AddToScheme(clientgoscheme.Scheme))
	ctx := context.TODO()

	// create searchheadcluster custom resource
	searchheadcluster := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			DeployerRef: corev1.ObjectReference{
				Name: "stack1",
			},
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

	// simulate create clustermanager instance before reconcilation
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

	/*
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
	*/

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
	err = splctrl.SetStatefulSetOwnerRef(ctx, c, searchheadcluster, namespacedName)
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

	// Mock the addTelApp function for unit tests
	addTelApp = func(ctx context.Context, c splcommon.ControllerClient, podExecClient splutil.PodExecClientImpl, replicas int32, cr splcommon.MetaObject) error {
		return nil
	}

	// call reconciliation
	_, err = ApplySearchHeadCluster(ctx, c, searchheadcluster)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for search head cluster with app framework  %v", err)
		debug.PrintStack()
	}
}
