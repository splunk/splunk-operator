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

package enterprise

import (
	"fmt"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func TestApplySearchHeadCluster(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-secrets"},
		{MetaName: "*v1.Secret-test-splunk-stack1-search-head-secrets"},
		{MetaName: "*v1.Service-test-splunk-stack1-search-head-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-search-head-service"},
		{MetaName: "*v1.Service-test-splunk-stack1-deployer-service"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-deployer"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},
	}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": {funcCalls[5], funcCalls[6]}}
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
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplySearchHeadCluster(c, cr.(*enterprisev1.SearchHeadCluster))
		return err
	}
	spltest.ReconcileTester(t, "TestApplySearchHeadCluster", &statefulSet, revised, createCalls, updateCalls, reconcile)

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
		_, err := ApplySearchHeadCluster(c, cr.(*enterprisev1.SearchHeadCluster))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)
}

func searchHeadClusterPodManagerTester(t *testing.T, method string, mockHandlers []spltest.MockHTTPHandler,
	desiredReplicas int32, wantPhase splcommon.Phase, statefulSet *appsv1.StatefulSet,
	wantCalls map[string][]spltest.MockFuncCall, wantError error, initObjects ...runtime.Object) {

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
		{MetaName: "*v1.Pod-test-splunk-stack1-0"},
	}
	wantCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0]}}

	// test API failure
	method := "searchHeadClusterPodManager.Update(API failure)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, splcommon.PhasePending, statefulSet, wantCalls, nil, statefulSet)

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
	wantCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}
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
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, splcommon.PhaseReady, statefulSet, wantCalls, nil, statefulSet, pod)

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
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, splcommon.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => wait for searches to drain
	mockHandlers = []spltest.MockHTTPHandler{mockHandlers[0], mockHandlers[1]}
	mockHandlers[0].Body = strings.Replace(mockHandlers[0].Body, `"status":"Up"`, `"status":"ManualDetention"`, 1)
	mockHandlers[0].Body = strings.Replace(mockHandlers[0].Body, `"active_historical_search_count":0`, `"active_historical_search_count":1`, 1)
	method = "searchHeadClusterPodManager.Update(Draining Searches)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, splcommon.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => delete pod
	wantCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls, "Delete": {funcCalls[1]}}
	mockHandlers[0].Body = strings.Replace(mockHandlers[0].Body, `"active_historical_search_count":1`, `"active_historical_search_count":0`, 1)
	method = "searchHeadClusterPodManager.Update(Delete Pod)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, splcommon.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod update finished => release from detention
	wantCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}
	pod.ObjectMeta.Labels["controller-revision-hash"] = "v1"
	mockHandlers = append(mockHandlers, spltest.MockHTTPHandler{
		Method: "POST",
		URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/member/control/control/set_manual_detention?manual_detention=off",
		Status: 200,
		Err:    nil,
		Body:   ``,
	})
	method = "searchHeadClusterPodManager.Update(Release Quarantine)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, splcommon.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

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
	funcCalls[1] = spltest.MockFuncCall{MetaName: "*v1.Pod-test-splunk-stack1-0"}
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0]}, "Delete": pvcCalls, "Update": {funcCalls[0]}}
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
	method = "searchHeadClusterPodManager.Update(Remove Member)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, splcommon.PhaseScalingDown, statefulSet, wantCalls, nil, statefulSet, pod, pvcList[0], pvcList[1])
}

func TestGetSearchHeadStatefulSet(t *testing.T) {
	cr := enterprisev1.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := validateSearchHeadClusterSpec(&cr.Spec); err != nil {
				t.Errorf("validateSearchHeadClusterSpec() returned error: %v", err)
			}
			return getSearchHeadStatefulSet(&cr)
		}
		configTester(t, fmt.Sprintf("getSearchHeadStatefulSet(Replicas=%d)", cr.Spec.Replicas), f, want)
	}

	cr.Spec.Replicas = 3
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":3,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secrets","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datareceive","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.Replicas = 4
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":4,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secrets","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datareceive","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-3.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.Replicas = 5
	cr.Spec.IndexerClusterRef.Name = "stack1"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":5,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secrets","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datareceive","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-3.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-4.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack1-cluster-master-service"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.Replicas = 6
	cr.Spec.SparkRef.Name = cr.GetName()
	cr.Spec.IndexerClusterRef.Namespace = "test2"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":6,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secrets","defaultMode":420}},{"name":"mnt-splunk-jdk","emptyDir":{}},{"name":"mnt-splunk-spark","emptyDir":{}}],"initContainers":[{"name":"init","image":"splunk/spark","command":["bash","-c","cp -r /opt/jdk /mnt \u0026\u0026 cp -r /opt/spark /mnt"],"resources":{"limits":{"cpu":"1","memory":"512Mi"},"requests":{"cpu":"250m","memory":"128Mi"}},"volumeMounts":[{"name":"mnt-splunk-jdk","mountPath":"/mnt/jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/spark"}],"imagePullPolicy":"IfNotPresent"}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datareceive","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-3.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-4.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-5.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack1-cluster-master-service.test2.svc.cluster.local"},{"name":"SPLUNK_ENABLE_DFS","value":"true"},{"name":"SPARK_MASTER_HOST","value":"splunk-stack1-spark-master-service"},{"name":"SPARK_MASTER_WEBUI_PORT","value":"8009"},{"name":"SPARK_HOME","value":"/mnt/splunk-spark"},{"name":"JAVA_HOME","value":"/mnt/splunk-jdk"},{"name":"SPLUNK_DFW_NUM_SLOTS_ENABLED","value":"true"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"},{"name":"mnt-splunk-jdk","mountPath":"/mnt/splunk-jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/splunk-spark"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	// Define additional service port in CR and verified the statefulset has the new port
	cr.Spec.ServiceTemplate.Spec.Ports = []corev1.ServicePort{{Name: "user-defined", Port: 32000, Protocol: "UDP"}}
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":6,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secrets","defaultMode":420}},{"name":"mnt-splunk-jdk","emptyDir":{}},{"name":"mnt-splunk-spark","emptyDir":{}}],"initContainers":[{"name":"init","image":"splunk/spark","command":["bash","-c","cp -r /opt/jdk /mnt \u0026\u0026 cp -r /opt/spark /mnt"],"resources":{"limits":{"cpu":"1","memory":"512Mi"},"requests":{"cpu":"250m","memory":"128Mi"}},"volumeMounts":[{"name":"mnt-splunk-jdk","mountPath":"/mnt/jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/spark"}],"imagePullPolicy":"IfNotPresent"}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datareceive","containerPort":19000,"protocol":"TCP"},{"name":"user-defined","containerPort":32000,"protocol":"UDP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-3.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-4.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-5.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack1-cluster-master-service.test2.svc.cluster.local"},{"name":"SPLUNK_ENABLE_DFS","value":"true"},{"name":"SPARK_MASTER_HOST","value":"splunk-stack1-spark-master-service"},{"name":"SPARK_MASTER_WEBUI_PORT","value":"8009"},{"name":"SPARK_HOME","value":"/mnt/splunk-spark"},{"name":"JAVA_HOME","value":"/mnt/splunk-jdk"},{"name":"SPLUNK_DFW_NUM_SLOTS_ENABLED","value":"true"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"},{"name":"mnt-splunk-jdk","mountPath":"/mnt/splunk-jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/splunk-spark"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)
}

func TestGetDeployerStatefulSet(t *testing.T) {
	cr := enterprisev1.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := validateSearchHeadClusterSpec(&cr.Spec); err != nil {
				t.Errorf("validateSearchHeadClusterSpec() returned error: %v", err)
			}
			return getDeployerStatefulSet(&cr)
		}
		configTester(t, "getDeployerStatefulSet()", f, want)
	}

	cr.Spec.Replicas = 3
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-deployer","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secrets","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_deployer"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-deployer"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-deployer-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)
}
