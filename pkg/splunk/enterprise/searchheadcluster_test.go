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
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v3"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

func TestApplySearchHeadCluster(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Service-test-splunk-stack1-search-head-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-search-head-service"},
		{MetaName: "*v1.Service-test-splunk-stack1-deployer-service"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-deployer-secret-v1"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-searchheadcluster-app-list"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-deployer"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-search-head-secret-v1"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
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

	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0], funcCalls[2], funcCalls[3], funcCalls[4], funcCalls[6], funcCalls[8], funcCalls[10], funcCalls[11]}, "Update": {funcCalls[0]}, "List": {listmockCall[0], listmockCall[0]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": {funcCalls[8], funcCalls[11]}, "List": {listmockCall[0], listmockCall[0]}}
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
		_, err := ApplySearchHeadCluster(c, cr.(*enterpriseApi.SearchHeadCluster))
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplySearchHeadCluster", &statefulSet, revised, createCalls, updateCalls, reconcile, true)

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
		_, err := ApplySearchHeadCluster(c, cr.(*enterpriseApi.SearchHeadCluster))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)
}

func searchHeadClusterPodManagerTester(t *testing.T, method string, mockHandlers []spltest.MockHTTPHandler,
	desiredReplicas int32, wantPhase splcommon.Phase, statefulSet *appsv1.StatefulSet,
	wantCalls map[string][]spltest.MockFuncCall, wantError error, initObjects ...runtime.Object) {

	// test for updating
	scopedLog := log.WithName(method)
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
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-0"},
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-0"},
		{MetaName: "*v1.Pod-test-splunk-stack1-0"},
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

	wantCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[2]}, "Create": {funcCalls[1]}}

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
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[2], funcCalls[5]}, "Create": {funcCalls[1]}, "List": {listmockCall[0]}}
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
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[2], funcCalls[5], funcCalls[2]}, "Create": {funcCalls[1]}}
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, splcommon.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => wait for searches to drain
	mockHandlers = []spltest.MockHTTPHandler{mockHandlers[0], mockHandlers[1]}
	mockHandlers[0].Body = strings.Replace(mockHandlers[0].Body, `"status":"Up"`, `"status":"ManualDetention"`, 1)
	mockHandlers[0].Body = strings.Replace(mockHandlers[0].Body, `"active_historical_search_count":0`, `"active_historical_search_count":1`, 1)
	method = "searchHeadClusterPodManager.Update(Draining Searches)"
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[2], funcCalls[5]}, "Create": {funcCalls[1]}}
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, splcommon.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => delete pod
	mockHandlers[0].Body = strings.Replace(mockHandlers[0].Body, `"active_historical_search_count":1`, `"active_historical_search_count":0`, 1)
	method = "searchHeadClusterPodManager.Update(Delete Pod)"
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[2], funcCalls[5]}, "Create": {funcCalls[1]}, "Delete": {funcCalls[5]}}
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, splcommon.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

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
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[2], funcCalls[5], funcCalls[2]}, "Create": {funcCalls[1]}}
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
	extraCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-1"},
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-1"},
	}

	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[2]}, "Delete": pvcCalls, "Update": {funcCalls[0]}, "Create": {funcCalls[1]}}
	wantCalls["Get"] = append(wantCalls["Get"], extraCalls...)
	wantCalls["Get"] = append(wantCalls["Get"], pvcCalls...)
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
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, splcommon.PhaseScalingDown, statefulSet, wantCalls, nil, statefulSet, pod, pvcList[0], pvcList[1])
}

func TestApplyShcSecret(t *testing.T) {
	method := "ApplyShcSecret"
	scopedLog := log.WithName(method)
	var initObjectList []runtime.Object

	c := spltest.NewMockClient()

	// Get namespace scoped secret
	nsSecret, err := splutil.ApplyNamespaceScopedSecretObject(c, "test")
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

	// Set resource version as that of NS secret
	err = ApplyShcSecret(mgr, 1, true)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}

	// Change resource version and test
	mgr.cr.Status.NamespaceSecretResourceVersion = "0"
	err = ApplyShcSecret(mgr, 1, true)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}
	mockSplunkClient.CheckRequests(t, method)

	// Don't set as it is set already
	err = ApplyShcSecret(mgr, 1, true)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}

	// Update admin password in secret again to hit already set scenario
	secrets.Data["password"] = []byte{'1'}
	err = splutil.UpdateResource(c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	mgr.cr.Status.ShcSecretChanged[0] = false
	// Test set again for shc_secret
	err = ApplyShcSecret(mgr, 1, true)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}

	// Update admin password in secret again to hit already set scenario
	secrets.Data["password"] = []byte{'1'}
	err = splutil.UpdateResource(c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	mgr.cr.Status.ShcSecretChanged[0] = false
	mgr.cr.Status.AdminSecretChanged[0] = false
	// Test set again for admin password
	err = ApplyShcSecret(mgr, 1, true)
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
	err = splutil.UpdateResource(c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	err = ApplyShcSecret(mgr, 1, true)
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

	err = splutil.UpdateResource(c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	err = ApplyShcSecret(mgr, 1, true)
	if err.Error() != fmt.Sprintf(splcommon.SecretTokenNotRetrievable, "admin password") {
		t.Errorf("Couldn't recognize missing admin password %s", err.Error())
	}

	// Make resource version of ns secret and cr the same
	mgr.cr.Status.NamespaceSecretResourceVersion = "1"
	nsSecret.ResourceVersion = mgr.cr.Status.NamespaceSecretResourceVersion
	err = splutil.UpdateResource(c, nsSecret)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	err = ApplyShcSecret(mgr, 1, true)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}
}

func TestGetSearchHeadStatefulSet(t *testing.T) {
	cr := enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(c, "test")
	if err != nil {
		t.Errorf("Failed to create namespace scoped object")
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := validateSearchHeadClusterSpec(&cr); err != nil {
				t.Errorf("validateSearchHeadClusterSpec() returned error: %v", err)
			}
			return getSearchHeadStatefulSet(c, &cr)
		}
		configTester(t, fmt.Sprintf("getSearchHeadStatefulSet(Replicas=%d)", cr.Spec.Replicas), f, want)
	}

	cr.Spec.Replicas = 3
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":3,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.Replicas = 4
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":4,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-3.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.Replicas = 5
	cr.Spec.ClusterMasterRef.Name = "stack1"
	test(splcommon.TestGetSearchHeadStatefulSet)

	cr.Spec.Replicas = 6

	cr.Spec.ClusterMasterRef.Namespace = "test2"
	test(splcommon.TestGetSearchHeadStatefulSetT2)

	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	test(splcommon.TestGetSearchHeadStatefulSetApps)

	// Define additional service port in CR and verified the statefulset has the new port
	test(splcommon.TestGetSearchHeadStatefulSetNewPort)

	// Create a serviceaccount
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(c, &current)
	cr.Spec.ServiceAccount = "defaults"
	test(splcommon.TestGetSearchHeadStatefulSetT3)

	// Add extraEnv
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	test(splcommon.TestGetSearchHeadStatefulSetT4)
}

func TestGetDeployerStatefulSet(t *testing.T) {
	cr := enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(c, "test")
	if err != nil {
		t.Errorf("Failed to create namespace scoped object")
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := validateSearchHeadClusterSpec(&cr); err != nil {
				t.Errorf("validateSearchHeadClusterSpec() returned error: %v", err)
			}
			return getDeployerStatefulSet(c, &cr)
		}
		configTester(t, "getDeployerStatefulSet()", f, want)
	}

	cr.Spec.Replicas = 3
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-deployer","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-deployer-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_deployer"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-deployer"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-deployer-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	// Allow installation of apps via DefaultsURLApps on the SHCDeployer
	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-deployer","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-deployer-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/apps/apps.yml,/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_deployer"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-deployer"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-deployer-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	// Create a serviceaccount
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(c, &current)
	cr.Spec.ServiceAccount = "defaults"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-deployer","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-deployer-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/apps/apps.yml,/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_deployer"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"serviceAccountName":"defaults","securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-deployer"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-deployer-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)
}

func TestAppFrameworkSearchHeadClusterShouldNotFail(t *testing.T) {
	cr := enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
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
	_, err := splutil.ApplyNamespaceScopedSecretObject(client, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	// Create S3 secret
	s3Secret := spltest.GetMockS3SecretKeys("s3-secret")

	client.AddObject(&s3Secret)

	_, err = ApplySearchHeadCluster(client, &cr)
	if err != nil {
		t.Errorf("ApplySearchHeadCluster should be successful")
	}
}

func TestSHCGetAppsListForAWSS3ClientShouldNotFail(t *testing.T) {
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
	_, err := splutil.ApplyNamespaceScopedSecretObject(client, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	splclient.RegisterS3Client("aws")

	Etags := []string{"cc707187b036405f095a8ebb43a782c1", "5055a61b3d1b667a4c3279a381a2e7ae", "19779168370b97d8654424e6c9446dd9"}
	Keys := []string{"admin_app.tgz", "security_app.tgz", "authentication_app.tgz"}
	Sizes := []int64{10, 20, 30}
	StorageClass := "STANDARD"
	randomTime := time.Date(2021, time.May, 1, 23, 23, 0, 0, time.UTC)

	mockAwsHandler := spltest.MockAWSS3Handler{}

	mockAwsObjects := []spltest.MockAWSS3Client{
		{
			Objects: []*spltest.MockAWSS3Object{
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
			Objects: []*spltest.MockAWSS3Object{
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
			Objects: []*spltest.MockAWSS3Object{
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

		vol, err = splclient.GetAppSrcVolume(appSource, &appFrameworkRef)
		if err != nil {
			allSuccess = false
			continue
		}

		// Update the GetS3Client with our mock call which initializes mock AWS client
		getClientWrapper := splclient.S3Clients[vol.Provider]
		getClientWrapper.SetS3ClientFuncPtr(vol.Provider, splclient.NewMockAWSS3Client)

		s3ClientMgr := &S3ClientManager{client: client,
			cr: &cr, appFrameworkRef: &cr.Spec.AppFrameworkConfig,
			vol:      &vol,
			location: appSource.Location,
			initFn: func(region, accessKeyID, secretAccessKey string) interface{} {
				cl := spltest.MockAWSS3Client{}
				cl.Objects = mockAwsObjects[index].Objects
				return cl
			},
			getS3Client: func(client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec, location string, fn splclient.GetInitFunc) (splclient.SplunkS3Client, error) {
				c, err := GetRemoteStorageClient(client, cr, appFrameworkRef, vol, location, fn)
				return c, err
			},
		}

		s3Response, err := s3ClientMgr.GetAppsList()
		if err != nil {
			allSuccess = false
			continue
		}

		var mockResponse spltest.MockAWSS3Client
		mockResponse, err = splclient.ConvertS3Response(s3Response)
		if err != nil {
			allSuccess = false
			continue
		}
		if mockAwsHandler.GotSourceAppListResponseMap == nil {
			mockAwsHandler.GotSourceAppListResponseMap = make(map[string]spltest.MockAWSS3Client)
		}

		mockAwsHandler.GotSourceAppListResponseMap[appSource.Name] = mockResponse
	}

	if allSuccess == false {
		t.Errorf("Unable to get apps list for all the app sources")
	}
	method := "GetAppsList"
	mockAwsHandler.CheckAWSS3Response(t, method)
}

func TestSHCGetAppsListForAWSS3ClientShouldFail(t *testing.T) {
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
	_, err := splutil.ApplyNamespaceScopedSecretObject(client, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	splclient.RegisterS3Client("aws")

	Etags := []string{"cc707187b036405f095a8ebb43a782c1"}
	Keys := []string{"admin_app.tgz"}
	Sizes := []int64{10}
	StorageClass := "STANDARD"
	randomTime := time.Date(2021, time.May, 1, 23, 23, 0, 0, time.UTC)

	mockAwsHandler := spltest.MockAWSS3Handler{}

	mockAwsObjects := []spltest.MockAWSS3Client{
		{
			Objects: []*spltest.MockAWSS3Object{
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
	vol, err = splclient.GetAppSrcVolume(appSource, &appFrameworkRef)
	if err != nil {
		t.Errorf("Unable to get Volume due to error=%s", err)
	}

	// Update the GetS3Client with our mock call which initializes mock AWS client
	getClientWrapper := splclient.S3Clients[vol.Provider]
	getClientWrapper.SetS3ClientFuncPtr(vol.Provider, splclient.NewMockAWSS3Client)

	s3ClientMgr := &S3ClientManager{
		client:          client,
		cr:              &cr,
		appFrameworkRef: &cr.Spec.AppFrameworkConfig,
		vol:             &vol,
		location:        appSource.Location,
		initFn: func(region, accessKeyID, secretAccessKey string) interface{} {
			// Purposefully return nil here so that we test the error scenario
			return nil
		},
		getS3Client: func(client splcommon.ControllerClient, cr splcommon.MetaObject,
			appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec,
			location string, fn splclient.GetInitFunc) (splclient.SplunkS3Client, error) {
			// Get the mock client
			c, err := GetRemoteStorageClient(client, cr, appFrameworkRef, vol, location, fn)
			return c, err
		},
	}

	_, err = s3ClientMgr.GetAppsList()
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

	_, err = s3ClientMgr.GetAppsList()
	if err == nil {
		t.Errorf("GetAppsList should have returned error as S3 secret has empty keys")
	}

	s3AccessKey := []byte{'1'}
	s3Secret.Data = map[string][]byte{"s3_access_key": s3AccessKey}
	_, err = s3ClientMgr.GetAppsList()
	if err == nil {
		t.Errorf("GetAppsList should have returned error as S3 secret has empty s3_secret_key")
	}

	s3SecretKey := []byte{'2'}
	s3Secret.Data = map[string][]byte{"s3_secret_key": s3SecretKey}
	_, err = s3ClientMgr.GetAppsList()
	if err == nil {
		t.Errorf("GetAppsList should have returned error as S3 secret has empty s3_access_key")
	}

	// Create S3 secret
	s3Secret = spltest.GetMockS3SecretKeys("s3-secret")

	// This should return an error as we have initialized initFn for s3ClientMgr
	// to return a nil client.
	_, err = s3ClientMgr.GetAppsList()
	if err == nil {
		t.Errorf("GetAppsList should have returned error as we could not get the S3 client")
	}

	s3ClientMgr.initFn = func(region, accessKeyID, secretAccessKey string) interface{} {
		// To test the error scenario, do no set the Objects member yet
		cl := spltest.MockAWSS3Client{}
		return cl
	}

	s3Resp, err := s3ClientMgr.GetAppsList()
	if err != nil {
		t.Errorf("GetAppsList should not have returned error since empty appSources are allowed.")
	}
	if len(s3Resp.Objects) != 0 {
		t.Errorf("GetAppsList should return an empty response since we have empty objects in MockAWSS3Client")
	}
}
