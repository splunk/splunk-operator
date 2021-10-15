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

func TestApplyIndexerCluster(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v3.ClusterMaster-test-master1"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-service"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-indexer-secret-v1"},
		{MetaName: "*v3.ClusterMaster-test-master1"},
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
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0], funcCalls[3], funcCalls[4], funcCalls[6]}, "Update": {funcCalls[0]}, "List": {listmockCall[0]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "List": {listmockCall[0]}}

	current := enterpriseApi.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas: 1,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				ClusterMasterRef: corev1.ObjectReference{
					Name: "master1",
				},
				Mock: true,
			},
		},
	}
	current.Status.ClusterMasterPhase = splcommon.PhaseReady
	current.Status.IndexerSecretChanged = append(current.Status.IndexerSecretChanged, true)
	revised := current.DeepCopy()
	revised.Spec.Image = "splunk/test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyIndexerCluster(c, cr.(*enterpriseApi.IndexerCluster))
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyIndexerCluster", &current, revised, createCalls, updateCalls, reconcile, true)

	// // test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
		_, err := ApplyIndexerCluster(c, cr.(*enterpriseApi.IndexerCluster))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)
}

func TestGetClusterMasterClient(t *testing.T) {
	scopedLog := log.WithName("TestGetClusterMasterClient")
	cr := enterpriseApi.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas: 1,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				ClusterMasterRef: corev1.ObjectReference{
					Name: "", /* Empty ClusterMasterRef */
				},
			},
		},
		Status: enterpriseApi.IndexerClusterStatus{
			ClusterMasterPhase: splcommon.PhaseReady,
		},
	}
	secrets := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-master1-indexer-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"password": {'1', '2', '3'},
		},
	}
	mockSplunkClient := &spltest.MockHTTPClient{}
	mgr := &indexerClusterPodManager{
		log:     scopedLog,
		cr:      &cr,
		secrets: secrets,
		newSplunkClient: func(managementURI, username, password string) *splclient.SplunkClient {
			c := splclient.NewSplunkClient(managementURI, username, password)
			c.Client = mockSplunkClient
			return c
		},
	}
	c := spltest.NewMockClient()
	mgr.c = c
	cm := mgr.getClusterMasterClient()
	if cm.ManagementURI != splcommon.TestServiceURLClusterManagerMgmtPort {
		t.Errorf("getClusterMasterClient() should have returned incorrect mgmt URI")
	}
}

func getIndexerClusterPodManager(method string, mockHandlers []spltest.MockHTTPHandler, mockSplunkClient *spltest.MockHTTPClient, replicas int32) *indexerClusterPodManager {
	scopedLog := log.WithName(method)
	cr := enterpriseApi.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas: replicas,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				ClusterMasterRef: corev1.ObjectReference{
					Name: "master1",
				},
			},
		},
		Status: enterpriseApi.IndexerClusterStatus{
			ClusterMasterPhase: splcommon.PhaseReady,
		},
	}
	cr.Status.IndexerSecretChanged = append(cr.Status.IndexerSecretChanged, true)

	secrets := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-master1-indexer-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"password": {'1', '2', '3'},
		},
	}

	mgr := &indexerClusterPodManager{
		log:     scopedLog,
		cr:      &cr,
		secrets: secrets,
		newSplunkClient: func(managementURI, username, password string) *splclient.SplunkClient {
			c := splclient.NewSplunkClient(managementURI, username, password)
			c.Client = mockSplunkClient
			return c
		},
	}
	return mgr
}

// indexerClusterpodManagerVerifyRFPeersTester is used to verify replicas against RF using a indexerClusterPodManager
func indexerClusterPodManagerVerifyRFPeersTester(t *testing.T, method string, mgr *indexerClusterPodManager,
	desiredReplicas int32, wantPhase splcommon.Phase, wantCalls map[string][]spltest.MockFuncCall, wantError error) {

	// initialize client
	c := spltest.NewMockClient()

	// test update
	err := mgr.verifyRFPeers(c)
	if (err == nil && wantError != nil) ||
		(err != nil && wantError == nil) ||
		(err != nil && wantError != nil && err.Error() != wantError.Error()) {
		t.Errorf("%s returned error %v; want %v", method, err, wantError)
	}

	if mgr.cr.Spec.Replicas != desiredReplicas {
		t.Errorf("spec has replicas as %d ; want %d", mgr.cr.Spec.Replicas, desiredReplicas)
	}
	// check calls
	c.CheckCalls(t, method, wantCalls)
}

func indexerClusterPodManagerReplicasTester(t *testing.T, method string, mockHandlers []spltest.MockHTTPHandler,
	replicas int32, desiredReplicas int32, wantPhase splcommon.Phase,
	wantCalls map[string][]spltest.MockFuncCall, wantError error) {

	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)

	mgr := getIndexerClusterPodManager(method, mockHandlers, mockSplunkClient, replicas)
	indexerClusterPodManagerVerifyRFPeersTester(t, method, mgr, desiredReplicas, wantPhase, wantCalls, wantError)
	mockSplunkClient.CheckRequests(t, method)
}

func TestVerifyRFPeers(t *testing.T) {

	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1." + fmt.Sprintf(splcommon.TestClusterManagerPod, "0")},
	}

	wantCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0]}}

	// test 1 ready pod
	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    splcommon.TestServiceURLClusterManagerClusterConfig,
			Status: 200,
			Err:    nil,
			Body:   splcommon.TestVerifyRFPeers,
		},
	}

	method := "indexerClusterPodManager.verifyRFPeers(All pods ready)"
	// test for singlesite i.e. with replication_factor=3(on ClusterMaster) and replicas=3(on IndexerCluster)
	indexerClusterPodManagerReplicasTester(t, method, mockHandlers, 3 /*replicas*/, 3 /*desired replicas*/, splcommon.PhaseReady, wantCalls, nil)

	// test for singlesite i.e. with replication_factor=3(on ClusterMaster) and replicas=1(on IndexerCluster)
	indexerClusterPodManagerReplicasTester(t, method, mockHandlers, 1 /*replicas*/, 3 /*desired replicas*/, splcommon.PhaseReady, wantCalls, nil)

	// Now test for multi-site too
	mockHandlers[0].Body = splcommon.TestVerifyRFPeersMultiSite

	//test for multisite i.e. with site_replication_factor=origin:2,total:2(on ClusterMaster) and replicas=2(on IndexerCluster)
	indexerClusterPodManagerReplicasTester(t, method, mockHandlers, 2 /*replicas*/, 2 /*desired replicas*/, splcommon.PhaseReady, wantCalls, nil)

	//test for multisite i.e. with site_replication_factor=origin:2,total:2(on ClusterMaster) and replicas=1(on IndexerCluster)
	indexerClusterPodManagerReplicasTester(t, method, mockHandlers, 1 /*replicas*/, 2 /*desired replicas*/, splcommon.PhaseReady, wantCalls, nil)
}

func checkResponseFromUpdateStatus(t *testing.T, method string, mockHandlers []spltest.MockHTTPHandler, replicas int32, statefulSet *appsv1.StatefulSet, retry bool) error {
	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)

	mgr := getIndexerClusterPodManager(method, mockHandlers, mockSplunkClient, replicas)

	c := spltest.NewMockClient()
	mgr.c = c

	err := mgr.updateStatus(statefulSet)
	if retry == true {
		err = mgr.updateStatus(statefulSet)
	}
	return err
}

func TestUpdateStatusInvalidResponse(t *testing.T) {
	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    splcommon.TestServiceURLClusterManagerGetInfo,
			Status: 200,
			Err:    nil,
			Body:   ``,
		},
	}
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

	method := "indexerClusterPodManager.UpdateStatus(Invalid response)"
	err := checkResponseFromUpdateStatus(t, method, mockHandlers, 1, statefulSet, false)
	if err == nil {
		t.Errorf("mgr.updateStatus() should have returned an error here")
	}

	mockHandlers[0].Body = splcommon.TestUpdateStatusInvalidResponse0

	mockHandler := spltest.MockHTTPHandler{
		Method: "GET",
		URL:    splcommon.TestServiceURLClusterManagerGetPeers,
		Status: 200,
		Err:    nil,
		Body:   ``,
	}

	mockHandlers = append(mockHandlers, mockHandler)
	err = checkResponseFromUpdateStatus(t, method, mockHandlers, 1, statefulSet, false)
	if err == nil {
		t.Errorf("mgr.updateStatus() should have returned an error here")
	}

	mockHandlers[1].Body = splcommon.TestUpdateStatusInvalidResponse1

	// We would like to call mgr.updateStatus() here twice just to mimic calling reconcile twice,
	// so that the first call fill the field `mgr.cr.Status.Peers` and the next call can use that.
	err = checkResponseFromUpdateStatus(t, method, mockHandlers, 1, statefulSet, true)
	if err != nil {
		t.Errorf("mgr.updateStatus() should not have returned an error here")
	}
}

func TestInvalidPeerStatusInScaleDown(t *testing.T) {
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

	// Create a mock handler that returns an invalid peer status as response
	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    splcommon.TestServiceURLClusterManagerGetInfo,
			Status: 200,
			Err:    nil,
			Body:   splcommon.TestInvalidPeerStatusInScaleDownInfo,
		},
		{
			Method: "GET",
			URL:    splcommon.TestServiceURLClusterManagerGetPeers,
			Status: 200,
			Err:    nil,
			Body:   splcommon.TestInvalidPeerStatusInScaleDownPeer,
		},
	}

	method := "indexerClusterPodManager.decommission"
	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)

	mgr := getIndexerClusterPodManager(method, mockHandlers, mockSplunkClient, replicas)

	c := spltest.NewMockClient()
	mgr.c = c

	err := mgr.updateStatus(statefulSet)
	if err != nil {
		t.Errorf("mgr.updateStatus() should not have returned an error here")
	}

	_, err = mgr.PrepareScaleDown(0)
	if err == nil {
		t.Errorf("mgr.PrepareScaleDown() should have returned an error here")
	}
}

func TestInvalidPeerInFinishRecycle(t *testing.T) {
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
			URL:    splcommon.TestServiceURLClusterManagerGetInfo,
			Status: 200,
			Err:    nil,
			Body:   splcommon.TestInvalidPeerInFinishRecycleInfo,
		},
		{
			Method: "GET",
			URL:    splcommon.TestServiceURLClusterManagerGetPeers,
			Status: 200,
			Err:    nil,
			Body:   splcommon.TestInvalidPeerInFinishRecyclePeer,
		},
	}

	method := "indexerClusterPodManager.FinishRecycle"
	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)

	mgr := getIndexerClusterPodManager(method, mockHandlers, mockSplunkClient, replicas)

	c := spltest.NewMockClient()
	mgr.c = c

	err := mgr.updateStatus(statefulSet)
	if err != nil {
		t.Errorf("mgr.updateStatus() should not have returned an error here")
	}

	// Here we are trying to call FinishRecycle for a peer which is not in the list.
	_, err = mgr.FinishRecycle(1)
	if err == nil {
		t.Errorf("mgr.FinishRecycle() should have returned an error here")
	}
}

func indexerClusterPodManagerUpdateTester(t *testing.T, method string, mockHandlers []spltest.MockHTTPHandler,
	desiredReplicas int32, wantPhase splcommon.Phase, statefulSet *appsv1.StatefulSet,
	wantCalls map[string][]spltest.MockFuncCall, wantError error, initObjects ...runtime.Object) {
	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)
	// get indexerClusterPodManager instance
	mgr := getIndexerClusterPodManager(method, mockHandlers, mockSplunkClient, 1)
	spltest.PodManagerUpdateTester(t, method, mgr, desiredReplicas, wantPhase, statefulSet, wantCalls, wantError, initObjects...)
	mockSplunkClient.CheckRequests(t, method)
}

func TestIndexerClusterPodManager(t *testing.T) {
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
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.StatefulSet-test-splunk-stack1"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Pod-test-splunk-stack1-indexer-0"},
		{MetaName: "*v1." + fmt.Sprintf(splcommon.TestClusterManagerPod, "0")},
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

	wantCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[3], funcCalls[4]}, "Create": {funcCalls[1]}, "List": {listmockCall[0]}}

	// test 1 ready pod
	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    splcommon.TestServiceURLClusterManagerGetInfo,
			Status: 200,
			Err:    nil,
			Body:   splcommon.TestIndexerClusterPodManagerInfo,
		},
		{
			Method: "GET",
			URL:    splcommon.TestServiceURLClusterManagerGetPeers,
			Status: 200,
			Err:    nil,
			Body:   splcommon.TestIndexerClusterPodManagerPeer,
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
	method := "indexerClusterPodManager.Update(All pods ready)"
	indexerClusterPodManagerUpdateTester(t, method, mockHandlers, 1, splcommon.PhaseReady, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => decommission
	mockHandlers = append(mockHandlers, spltest.MockHTTPHandler{
		Method: "POST",
		URL:    splcommon.TestURLPeerHeadlessDecommission + "enforce_counts=0",
		Status: 200,
		Err:    nil,
		Body:   ``,
	})
	pod.ObjectMeta.Labels["controller-revision-hash"] = "v0"
	method = "indexerClusterPodManager.Update(Decommission Pod)"
	wantDecomPodCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[3], funcCalls[4], funcCalls[2]}, "Create": {funcCalls[1]}}
	indexerClusterPodManagerUpdateTester(t, method, mockHandlers, 1, splcommon.PhaseUpdating, statefulSet, wantDecomPodCalls, nil, statefulSet, pod)

	// test pod needs update => wait for decommission to complete
	mockHandlers = []spltest.MockHTTPHandler{mockHandlers[0], mockHandlers[1]}
	mockHandlers[1].Body = strings.Replace(mockHandlers[1].Body, `"status":"Up"`, `"status":"ReassigningPrimaries"`, 1)
	method = "indexerClusterPodManager.Update(ReassigningPrimaries)"
	wantReasCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[3], funcCalls[4]}, "Create": {funcCalls[1]}}
	indexerClusterPodManagerUpdateTester(t, method, mockHandlers, 1, splcommon.PhaseUpdating, statefulSet, wantReasCalls, nil, statefulSet, pod)

	// test pod needs update => wait for decommission to complete
	mockHandlers[1].Body = strings.Replace(mockHandlers[1].Body, `"status":"ReassigningPrimaries"`, `"status":"Decommissioning"`, 1)
	method = "indexerClusterPodManager.Update(Decommissioning)"
	wantDecomCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[3], funcCalls[4]}, "Create": {funcCalls[1]}}
	indexerClusterPodManagerUpdateTester(t, method, mockHandlers, 1, splcommon.PhaseUpdating, statefulSet, wantDecomCalls, nil, statefulSet, pod)

	// test pod needs update => delete pod
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[3], funcCalls[4]}, "Create": {funcCalls[1]}, "Delete": {funcCalls[4]}}
	mockHandlers[1].Body = strings.Replace(mockHandlers[1].Body, `"status":"Decommissioning"`, `"status":"Down"`, 1)
	method = "indexerClusterPodManager.Update(Delete Pod)"
	indexerClusterPodManagerUpdateTester(t, method, mockHandlers, 1, splcommon.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test scale down => pod not found
	pod.ObjectMeta.Name = "splunk-stack1-2"
	replicas = 2
	statefulSet.Status.Replicas = 2
	statefulSet.Status.ReadyReplicas = 2
	statefulSet.Status.UpdatedReplicas = 2
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[3]}, "Create": {funcCalls[1]}}
	method = "indexerClusterPodManager.Update(Pod Not Found)"
	indexerClusterPodManagerUpdateTester(t, method, mockHandlers, 1, splcommon.PhaseScalingDown, statefulSet, wantCalls, nil, statefulSet, pod)

	// test scale down => decommission pod
	mockHandlers[1].Body = `{"entry":[{"name":"aa45bf46-7f46-47af-a760-590d5c606d10","content":{"status":"Up","label":"splunk-stack1-indexer-0"}},{"name":"D39B1729-E2C5-4273-B9B2-534DA7C2F866","content":{"status":"GracefulShutdown","label":"splunk-stack1-indexer-1"}}]}`
	mockHandlers = append(mockHandlers, spltest.MockHTTPHandler{
		Method: "POST",
		URL:    splcommon.TestServiceURLClusterManagerRemovePeers + "peers=D39B1729-E2C5-4273-B9B2-534DA7C2F866",
		Status: 200,
		Err:    nil,
		Body:   ``,
	})
	pvcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.PersistentVolumeClaim-test-pvc-etc-splunk-stack1-1"},
		{MetaName: "*v1.PersistentVolumeClaim-test-pvc-var-splunk-stack1-1"},
	}
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[3], funcCalls[3]}, "Create": {funcCalls[1]}, "Delete": pvcCalls, "Update": {funcCalls[0]}}
	wantCalls["Get"] = append(wantCalls["Get"], pvcCalls...)
	pvcList := []*corev1.PersistentVolumeClaim{
		{ObjectMeta: metav1.ObjectMeta{Name: "pvc-etc-splunk-stack1-1", Namespace: "test"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "pvc-var-splunk-stack1-1", Namespace: "test"}},
	}
	method = "indexerClusterPodManager.Update(Decommission)"
	pod.ObjectMeta.Name = "splunk-stack1-0"
	indexerClusterPodManagerUpdateTester(t, method, mockHandlers, 1, splcommon.PhaseScalingDown, statefulSet, wantCalls, nil, statefulSet, pod, pvcList[0], pvcList[1])
}

func TestSetClusterMaintenanceMode(t *testing.T) {
	var initObjectList []runtime.Object

	c := spltest.NewMockClient()

	// Get namespace scoped secret
	_, err := splutil.ApplyNamespaceScopedSecretObject(c, "test")
	if err != nil {
		t.Errorf("Apply namespace scoped secret failed")
	}

	// Create pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(splcommon.TestStack1ClusterManagerID, "0"),
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
			"password": {'1', '2', '3'},
		},
	}
	initObjectList = append(initObjectList, secrets)

	c.AddObjects(initObjectList)

	cr := enterpriseApi.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	cr.Spec.ClusterMasterRef.Name = cr.GetName()
	// Enable CM maintenance mode
	err = SetClusterMaintenanceMode(c, &cr, true, true)
	if err != nil {
		t.Errorf("Couldn't enable cm maintenance mode %s", err.Error())
	}

	if cr.Status.MaintenanceMode != true {
		t.Errorf("Couldn't enable cm maintenance mode %s", err.Error())
	}

	// Disable CM maintenance mode
	err = SetClusterMaintenanceMode(c, &cr, false, true)
	if err != nil {
		t.Errorf("Couldn't enable cm maintenance mode %s", err.Error())
	}

	if cr.Status.MaintenanceMode != false {
		t.Errorf("Couldn't enable cm maintenance mode %s", err.Error())
	}

	// Disable CM maintenance mode
	cr.Spec.ClusterMasterRef.Name = "random"
	err = SetClusterMaintenanceMode(c, &cr, false, true)
	if err.Error() != splcommon.PodNotFoundError {
		t.Errorf("Couldn't enable cm maintenance mode %s", err.Error())
	}

	// Empty clusterMaster reference
	cr.Spec.ClusterMasterRef.Name = ""
	err = SetClusterMaintenanceMode(c, &cr, false, true)
	if err.Error() != splcommon.EmptyClusterMasterRef {
		t.Errorf("Couldn't detect empty Cluster Master reference %s", err.Error())
	}
}

func TestApplyIdxcSecret(t *testing.T) {
	method := "ApplyIdxcSecret"
	scopedLog := log.WithName(method)
	var initObjectList []runtime.Object

	c := spltest.NewMockClient()

	// Get namespace scoped secret
	nsSecret, err := splutil.ApplyNamespaceScopedSecretObject(c, "test")
	if err != nil {
		t.Errorf("Apply namespace scoped secret failed")
	}

	podName := "splunk-stack1-indexer-0"
	// Create pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
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

	cmPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(splcommon.TestStack1ClusterManagerID, "0"),
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
	initObjectList = append(initObjectList, cmPod)

	secrets := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"password":           {'1', '2', '3'},
			splcommon.IdxcSecret: {'a'},
		},
	}
	initObjectList = append(initObjectList, secrets)

	c.AddObjects(initObjectList)

	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "POST",
			URL:    fmt.Sprintf("https://splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local:8089/services/cluster/config/config?secret=%s", string(nsSecret.Data[splcommon.IdxcSecret])),
			Status: 200,
			Err:    nil,
		},
		{
			Method: "POST",
			URL:    "https://splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local:8089/services/server/control/restart",
			Status: 200,
			Err:    nil,
		},
	}

	cr := enterpriseApi.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	cr.Status.IdxcPasswordChangedSecrets = make(map[string]bool)
	cr.Spec.ClusterMasterRef.Name = cr.GetName()
	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)
	mgr := &indexerClusterPodManager{
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

	// Set resource version to that of NS secret
	err = ApplyIdxcSecret(mgr, 1, true)
	if err != nil {
		t.Errorf("Couldn't apply idxc secret %s", err.Error())
	}

	// Change resource version
	mgr.cr.Status.NamespaceSecretResourceVersion = "0"
	err = ApplyIdxcSecret(mgr, 1, true)
	if err != nil {
		t.Errorf("Couldn't apply idxc secret %s", err.Error())
	}
	mockSplunkClient.CheckRequests(t, method)

	// Don't set as it is set already
	secrets.Data[splcommon.IdxcSecret] = []byte{'a'}
	err = splutil.UpdateResource(c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}
	err = ApplyIdxcSecret(mgr, 1, true)
	if err != nil {
		t.Errorf("Couldn't apply idxc secret %s", err.Error())
	}

	mgr.cr.Status.IndexerSecretChanged[0] = false
	secrets.Data[splcommon.IdxcSecret] = []byte{'a'}
	err = splutil.UpdateResource(c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}
	// Test set again
	err = ApplyIdxcSecret(mgr, 1, true)
	if err != nil {
		t.Errorf("Couldn't apply idxc secret %s", err.Error())
	}

	// Test the setCmMode failure
	secrets.Data[splcommon.IdxcSecret] = []byte{'a'}
	err = splutil.UpdateResource(c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	mgr.cr.Status.NamespaceSecretResourceVersion = "2"
	mgr.cr.Spec.ClusterMasterRef.Name = ""
	mgr.cr.Status.MaintenanceMode = false
	mgr.cr.Status.IndexerSecretChanged = []bool{}
	err = ApplyIdxcSecret(mgr, 1, true)
	if err.Error() != splcommon.EmptyClusterMasterRef {
		t.Errorf("Couldn't apply idxc secret %s", err.Error())
	}

	// Remove idxc secret
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

	err = ApplyIdxcSecret(mgr, 1, true)
	if err.Error() != fmt.Sprintf(splcommon.SecretTokenNotRetrievable, splcommon.IdxcSecret) {
		t.Errorf("Couldn't recognize missing idxc secret %s", err.Error())
	}

	// Test scenario with same namespace secret and cr status resource version
	nsSecret.ResourceVersion = "1"
	mgr.cr.Status.NamespaceSecretResourceVersion = nsSecret.ResourceVersion
	err = splutil.UpdateResource(c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	err = ApplyIdxcSecret(mgr, 1, true)
	if err != nil {
		t.Errorf("Couldn't apply idxc secret %s", err.Error())
	}

	// Test missing secret from pod
	mgr.cr.Status.NamespaceSecretResourceVersion = "10"
	err = splutil.DeleteResource(c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	err = ApplyIdxcSecret(mgr, 1, true)
	if err.Error() != fmt.Sprintf(splcommon.PodSecretNotFoundError, podName) {
		t.Errorf("Couldn't recognize missing secret from Pod, error: %s", err.Error())
	}
}

func TestInvalidIndexerClusterSpec(t *testing.T) {

	cr := enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	cm := enterpriseApi.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "master1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	c.AddObject(&cm)

	cm.Status.Phase = splcommon.PhaseReady
	// Empty ClusterMasterRef should return an error
	cr.Spec.ClusterMasterRef.Name = ""
	if _, err := ApplyIndexerCluster(c, &cr); err == nil {
		t.Errorf("ApplyIndxerCluster() should have returned error")
	}

	cr.Spec.ClusterMasterRef.Name = "master1"
	// verifyRFPeers should return err here
	if _, err := ApplyIndexerCluster(c, &cr); err == nil {
		t.Errorf("ApplyIndxerCluster() should have returned error")
	}

	cm.Status.Phase = splcommon.PhaseError
	cr.Spec.CommonSplunkSpec.EtcVolumeStorageConfig.StorageCapacity = "-abcd"
	if _, err := ApplyIndexerCluster(c, &cr); err == nil {
		t.Errorf("ApplyIndxerCluster() should have returned error")
	}
}

func TestGetIndexerStatefulSet(t *testing.T) {
	cr := enterpriseApi.IndexerCluster{
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

	cr.Spec.ClusterMasterRef.Name = "master1"
	test := func(want string) {
		f := func() (interface{}, error) {
			if err := validateIndexerClusterSpec(&cr); err != nil {
				t.Errorf("validateIndexerClusterSpec() returned error: %v", err)
			}
			return getIndexerStatefulSet(c, &cr)
		}
		configTester(t, "getIndexerStatefulSet()", f, want)
	}

	cr.Spec.Replicas = 0
	test(splcommon.TestGetIndexerStatefulSettest0)

	cr.Spec.Replicas = 1
	test(splcommon.TestGetIndexerStatefulSettest1)

	// Define additional service port in CR and verified the statefulset has the new port
	cr.Spec.ServiceTemplate.Spec.Ports = []corev1.ServicePort{{Name: "user-defined", Port: 32000, Protocol: "UDP"}}
	test(splcommon.TestGetIndexerStatefulSettest2)

	// Block moving DefaultsURLApps to SPLUNK_DEFAULTS_URL for indexer cluster member
	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	test(splcommon.TestGetIndexerStatefulSettest3)

	// Create a serviceaccount
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(c, &current)
	cr.Spec.ServiceAccount = "defaults"
	test(splcommon.TestGetIndexerStatefulSettest4)

	// Add extraEnv
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	test(splcommon.TestGetIndexerStatefulSettest5)

	cr.Spec.ClusterMasterRef.Namespace = "other"
	if err := validateIndexerClusterSpec(&cr); err == nil {
		t.Errorf("validateIndexerClusterSpec() error expected on multisite IndexerCluster referencing a cluster master located in a different namespace")
	}
}
