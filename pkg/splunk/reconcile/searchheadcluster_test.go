// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
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

package reconcile

import (
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func TestApplySearchHeadCluster(t *testing.T) {
	funcCalls := []mockFuncCall{
		{metaName: "*v1.Secret-test-splunk-stack1-search-head-secrets"},
		{metaName: "*v1.Service-test-splunk-stack1-search-head-headless"},
		{metaName: "*v1.Service-test-splunk-stack1-search-head-service"},
		{metaName: "*v1.Service-test-splunk-stack1-deployer-service"},
		{metaName: "*v1.StatefulSet-test-splunk-stack1-deployer"},
		{metaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},
	}
	createCalls := map[string][]mockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls := map[string][]mockFuncCall{"Get": funcCalls, "Update": []mockFuncCall{funcCalls[4], funcCalls[5]}}
	statefulSet := enterprisev1.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	revised := statefulSet.DeepCopy()
	revised.Spec.Image = "splunk/test"
	reconcile := func(c *mockClient, cr interface{}) error {
		_, err := ApplySearchHeadCluster(c, cr.(*enterprisev1.SearchHeadCluster))
		return err
	}
	reconcileTester(t, "TestApplySearchHeadCluster", &statefulSet, revised, createCalls, updateCalls, reconcile)

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr enterprisev1.MetaObject, c ControllerClient) (bool, error) {
		_, err := ApplySearchHeadCluster(c, cr.(*enterprisev1.SearchHeadCluster))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)
}

func searchHeadClusterPodManagerTester(t *testing.T, method string, mockHandlers []spltest.MockHTTPHandler,
	desiredReplicas int32, wantPhase enterprisev1.ResourcePhase, statefulSet *appsv1.StatefulSet,
	wantCalls map[string][]mockFuncCall, wantError error, initObjects ...runtime.Object) {

	// test for updating
	scopedLog := log.WithName(method)
	cr := enterprisev1.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	secrets := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"password": []byte{'1', '2', '3'},
		},
	}
	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)
	mgr := &SearchHeadClusterPodManager{
		log:     scopedLog,
		cr:      &cr,
		secrets: secrets,
		newSplunkClient: func(managementURI, username, password string) *splclient.SplunkClient {
			c := splclient.NewSplunkClient(managementURI, username, password)
			c.Client = mockSplunkClient
			return c
		},
	}
	podManagerUpdateTester(t, method, mgr, desiredReplicas, wantPhase, statefulSet, wantCalls, wantError, initObjects...)
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
	funcCalls := []mockFuncCall{
		{metaName: "*v1.StatefulSet-test-splunk-stack1"},
		{metaName: "*v1.Pod-test-splunk-stack1-0"},
	}
	wantCalls := map[string][]mockFuncCall{"Get": {funcCalls[0]}}

	// test API failure
	method := "SearchHeadClusterPodManager.Update(API failure)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterprisev1.PhasePending, statefulSet, wantCalls, nil, statefulSet)

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
	wantCalls = map[string][]mockFuncCall{"Get": funcCalls}
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
	method = "SearchHeadClusterPodManager.Update(All pods ready)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterprisev1.PhaseReady, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => transition to detention
	mockHandlers = append(mockHandlers, spltest.MockHTTPHandler{
		Method: "POST",
		URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/member/control/control/set_manual_detention?manual_detention=on",
		Status: 200,
		Err:    nil,
		Body:   ``,
	})
	pod.ObjectMeta.Labels["controller-revision-hash"] = "v0"
	method = "SearchHeadClusterPodManager.Update(Quarantine Pod)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterprisev1.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => wait for searches to drain
	mockHandlers = []spltest.MockHTTPHandler{mockHandlers[0], mockHandlers[1]}
	mockHandlers[0].Body = strings.Replace(mockHandlers[0].Body, `"status":"Up"`, `"status":"ManualDetention"`, 1)
	mockHandlers[0].Body = strings.Replace(mockHandlers[0].Body, `"active_historical_search_count":0`, `"active_historical_search_count":1`, 1)
	method = "SearchHeadClusterPodManager.Update(Draining Searches)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterprisev1.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => delete pod
	wantCalls = map[string][]mockFuncCall{"Get": funcCalls, "Delete": {funcCalls[1]}}
	mockHandlers[0].Body = strings.Replace(mockHandlers[0].Body, `"active_historical_search_count":1`, `"active_historical_search_count":0`, 1)
	method = "SearchHeadClusterPodManager.Update(Delete Pod)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterprisev1.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod update finished => release from detention
	wantCalls = map[string][]mockFuncCall{"Get": funcCalls}
	pod.ObjectMeta.Labels["controller-revision-hash"] = "v1"
	mockHandlers = append(mockHandlers, spltest.MockHTTPHandler{
		Method: "POST",
		URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/member/control/control/set_manual_detention?manual_detention=off",
		Status: 200,
		Err:    nil,
		Body:   ``,
	})
	method = "SearchHeadClusterPodManager.Update(Release Quarantine)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterprisev1.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

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
	pvcCalls := []mockFuncCall{
		{metaName: "*v1.PersistentVolumeClaim-test-pvc-etc-splunk-stack1-1"},
		{metaName: "*v1.PersistentVolumeClaim-test-pvc-var-splunk-stack1-1"},
	}
	funcCalls[1] = mockFuncCall{metaName: "*v1.Pod-test-splunk-stack1-0"}
	wantCalls = map[string][]mockFuncCall{"Get": {funcCalls[0]}, "Delete": pvcCalls, "Update": {funcCalls[0]}}
	wantCalls["Get"] = append(wantCalls["Get"], pvcCalls...)
	pvcList := []*corev1.PersistentVolumeClaim{
		{ObjectMeta: metav1.ObjectMeta{Name: "pvc-etc-splunk-stack1-1", Namespace: "test"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "pvc-var-splunk-stack1-1", Namespace: "test"}},
	}
	pod.ObjectMeta.Name = "splunk-stack1-2"
	replicas = 2
	statefulSet.Status.Replicas = 2
	statefulSet.Status.ReadyReplicas = 2
	statefulSet.Status.UpdatedReplicas = 2
	method = "SearchHeadClusterPodManager.Update(Remove Member)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterprisev1.PhaseScalingDown, statefulSet, wantCalls, nil, statefulSet, pod, pvcList[0], pvcList[1])
}
