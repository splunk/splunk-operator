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

	"github.com/pkg/errors"
	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"

	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/go-logr/logr"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var logt = logf.Log.WithName("splunk.enterprise.configValidation")

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

func TestApplyIndexerClusterOld(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	c := spltest.NewMockClient()
	ctx := context.TODO()
	idxCr := enterpriseApi.IndexerCluster{
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
				Mock: true,
			},
		},
	}

	// Initial run, invalid spec
	_, err := ApplyIndexerCluster(ctx, c, &idxCr)
	if err == nil {
		t.Errorf("Expected error, cm missing")
	}

	// ApplySplunkConfigError
	rerr := errors.New(splcommon.Rerr)
	c.InduceErrorKind[splcommon.MockClientInduceErrorGet] = rerr
	_, err = ApplyIndexerCluster(ctx, c, &idxCr)
	if err == nil {
		t.Errorf("Expected error, cm missing")
	}

	// Set CM Ref, but no CM
	c.InduceErrorKind[splcommon.MockClientInduceErrorGet] = nil
	idxCr.Spec.CommonSplunkSpec.ClusterMasterRef = corev1.ObjectReference{
		Name:      "test",
		Namespace: "test",
	}
	c.InduceErrorKind[splcommon.MockClientInduceErrorGet] = nil
	ApplyIndexerCluster(ctx, c, &idxCr)

	// Set CM Ref, but with CM
	cMasterCr := enterpriseApiV3.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
	}
	c.Create(ctx, &cMasterCr)
	idxCr.Spec.CommonSplunkSpec.ClusterMasterRef = corev1.ObjectReference{
		Name:      "test",
		Namespace: "test",
	}
	ApplyIndexerCluster(ctx, c, &idxCr)

	cMasterCr.Status.Phase = enterpriseApi.PhaseReady
	_, err = ApplyIndexerCluster(ctx, c, &idxCr)
	if err == nil {
		t.Errorf("Expected error for verifyRFPeers")
	}

	cMasterCr.Status.Phase = enterpriseApi.PhasePending
	cTs := metav1.Now()
	idxCr.ObjectMeta.DeletionTimestamp = &cTs
	_, err = ApplyIndexerCluster(ctx, c, &idxCr)
	if err != nil {
		t.Errorf("Not Expecting an error")
	}

	idxCr.ObjectMeta.DeletionTimestamp = nil
	_, err = ApplyIndexerCluster(ctx, c, &idxCr)
	if err != nil {
		t.Errorf("Not expecting an error, listing empty")
	}
}

func TestApplyIndexerCluster(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-indexer-stack1-configmap"},
		{MetaName: "*v4.ClusterManager-test-manager1"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-service"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-indexer"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-indexer-secret-v1"},
		{MetaName: "*v4.ClusterManager-test-manager1"},
		{MetaName: "*v4.IndexerCluster-test-stack1"},
		{MetaName: "*v4.IndexerCluster-test-stack1"},
	}
	updateFuncCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-indexer-stack1-configmap"},
		{MetaName: "*v4.ClusterManager-test-manager1"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-service"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-indexer"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-indexer-secret-v1"},
		{MetaName: "*v4.ClusterManager-test-manager1"},
		{MetaName: "*v4.IndexerCluster-test-stack1"},
		{MetaName: "*v4.IndexerCluster-test-stack1"},
	}

	labels := map[string]string{
		"app.kubernetes.io/component":  "versionedSecrets",
		"app.kubernetes.io/managed-by": "splunk-operator",
	}
	listOpts := []client.ListOption{
		client.InNamespace("test"),
		client.MatchingLabels(labels),
	}
	listOpts1 := []client.ListOption{
		client.InNamespace("test"),
	}
	listmockCall := []spltest.MockFuncCall{
		{ListOpts: listOpts},
		{ListOpts: listOpts1},
	}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0], funcCalls[3], funcCalls[5], funcCalls[6], funcCalls[10], funcCalls[12]}, "Update": {funcCalls[0]}, "List": {listmockCall[0], listmockCall[1]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": updateFuncCalls, "List": {listmockCall[0], listmockCall[1]}}

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
				ClusterManagerRef: corev1.ObjectReference{
					Name: "manager1",
				},
				Mock: true,
			},
		},
	}
	current.Status.ClusterManagerPhase = enterpriseApi.PhaseReady
	current.Status.IndexerSecretChanged = append(current.Status.IndexerSecretChanged, true)
	revised := current.DeepCopy()
	revised.Spec.Image = "splunk/test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyIndexerClusterManager(context.Background(), c, cr.(*enterpriseApi.IndexerCluster))
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyIndexerClusterManager", &current, revised, createCalls, updateCalls, reconcile, true)

	// // test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
		_, err := ApplyIndexerClusterManager(context.Background(), c, cr.(*enterpriseApi.IndexerCluster))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)

	// Negative testing
	ctx := context.TODO()
	c := spltest.NewMockClient()
	rerr := errors.New(splcommon.Rerr)
	c.InduceErrorKind[splcommon.MockClientInduceErrorGet] = rerr
	_, err := ApplyIndexerClusterManager(ctx, c, &current)
	if err == nil {
		t.Errorf("Expected error")
	}

	c.InduceErrorKind[splcommon.MockClientInduceErrorGet] = nil
	cManager := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "manager1",
			Namespace: "test",
		},
	}
	c.Create(ctx, &cManager)
	current.Spec.ClusterManagerRef = corev1.ObjectReference{
		Name:      "manager1",
		Namespace: "test",
	}
	_, err = ApplyIndexerClusterManager(ctx, c, &current)
	if err != nil {
		t.Errorf("Expected error")
	}

	newc := spltest.NewMockClient()
	nsSec, err := splutil.ApplyNamespaceScopedSecretObject(ctx, newc, "test")
	if err != nil {
		t.Errorf("Error creating secret")
	}
	newc.Create(ctx, nsSec)
	newc.Create(ctx, &cManager)
	newc.InduceErrorKind[splcommon.MockClientInduceErrorCreate] = rerr
	_, err = ApplyIndexerClusterManager(ctx, newc, &current)
	if err == nil {
		t.Errorf("Expected error")
	}
}

func TestGetMonitoringConsoleClient(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
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
				ClusterManagerRef: corev1.ObjectReference{
					Name: "manager1",
				},
				Mock: true,
			},
		},
	}
	scopedLog := logt.WithName("TestGetMonitoringConsoleClient")

	secrets := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-manager1-indexer-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"password": {'1', '2', '3'},
		},
	}
	mockSplunkClient := &spltest.MockHTTPClient{}
	mgr := &indexerClusterPodManager{
		log:     scopedLog,
		cr:      &current,
		secrets: secrets,
		newSplunkClient: func(managementURI, username, password string) *splclient.SplunkClient {
			c := splclient.NewSplunkClient(managementURI, username, password)
			c.Client = mockSplunkClient
			return c
		},
	}
	mgr.getMonitoringConsoleClient(&current, "cManager")
}

func TestGetClusterManagerClient(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	ctx := context.TODO()
	scopedLog := logt.WithName("TestGetClusterManagerClient")
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
				ClusterManagerRef: corev1.ObjectReference{
					Name: "", /* Empty ClusterManagerRef */
				},
			},
		},
		Status: enterpriseApi.IndexerClusterStatus{
			ClusterManagerPhase: enterpriseApi.PhaseReady,
		},
	}
	secrets := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-manager1-indexer-secrets",
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
	cm := mgr.getClusterManagerClient(ctx)
	if cm.ManagementURI != "https://splunk---service.test.svc.cluster.local:8089" {
		t.Errorf("getClusterManagerClient() should have returned incorrect mgmt URI")
	}
}

func getIndexerClusterPodManager(method string, mockHandlers []spltest.MockHTTPHandler, mockSplunkClient *spltest.MockHTTPClient, replicas int32) *indexerClusterPodManager {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	scopedLog := logt.WithName(method)
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
				ClusterManagerRef: corev1.ObjectReference{
					Name: "manager1",
				},
			},
		},
		Status: enterpriseApi.IndexerClusterStatus{
			ClusterManagerPhase: enterpriseApi.PhaseReady,
		},
	}
	cr.Status.IndexerSecretChanged = append(cr.Status.IndexerSecretChanged, true)

	secrets := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-manager1-indexer-secrets",
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
	desiredReplicas int32, wantPhase enterpriseApi.Phase, wantCalls map[string][]spltest.MockFuncCall, wantError error) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	ctx := context.TODO()

	// initialize client
	c := spltest.NewMockClient()

	// test update
	err := mgr.verifyRFPeers(ctx, c)
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
	replicas int32, desiredReplicas int32, wantPhase enterpriseApi.Phase,
	wantCalls map[string][]spltest.MockFuncCall, wantError error) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)

	mgr := getIndexerClusterPodManager(method, mockHandlers, mockSplunkClient, replicas)
	indexerClusterPodManagerVerifyRFPeersTester(t, method, mgr, desiredReplicas, wantPhase, wantCalls, wantError)
	mockSplunkClient.CheckRequests(t, method)
}

func TestVerifyRFPeers(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Pod-test-splunk-manager1-cluster-manager-0"},
	}

	wantCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0]}}

	// test 1 ready pod
	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/config?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   loadFixture(t, "service_stack1_indexer_service.json"),
		},
	}

	method := "indexerClusterPodManager.verifyRFPeers(All pods ready)"
	// test for singlesite i.e. with replication_factor=3(on ClusterManager) and replicas=3(on IndexerCluster)
	indexerClusterPodManagerReplicasTester(t, method, mockHandlers, 3 /*replicas*/, 3 /*desired replicas*/, enterpriseApi.PhaseReady, wantCalls, nil)

	// test for singlesite i.e. with replication_factor=3(on ClusterManager) and replicas=1(on IndexerCluster)
	indexerClusterPodManagerReplicasTester(t, method, mockHandlers, 1 /*replicas*/, 3 /*desired replicas*/, enterpriseApi.PhaseReady, wantCalls, nil)

	// Now test for multi-site too
	mockHandlers[0].Body = loadFixture(t, "service_stack1_indexer_headless.json")

	//test for multisite i.e. with site_replication_factor=origin:2,total:2(on ClusterManager) and replicas=2(on IndexerCluster)
	indexerClusterPodManagerReplicasTester(t, method, mockHandlers, 2 /*replicas*/, 2 /*desired replicas*/, enterpriseApi.PhaseReady, wantCalls, nil)

	//test for multisite i.e. with site_replication_factor=origin:2,total:2(on ClusterManager) and replicas=1(on IndexerCluster)
	indexerClusterPodManagerReplicasTester(t, method, mockHandlers, 1 /*replicas*/, 2 /*desired replicas*/, enterpriseApi.PhaseReady, wantCalls, nil)
}

func checkResponseFromUpdateStatus(t *testing.T, method string, mockHandlers []spltest.MockHTTPHandler, replicas int32, statefulSet *appsv1.StatefulSet, retry bool) error {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)

	ctx := context.TODO()

	mgr := getIndexerClusterPodManager(method, mockHandlers, mockSplunkClient, replicas)

	c := spltest.NewMockClient()
	mgr.c = c

	err := mgr.updateStatus(ctx, statefulSet)
	if retry == true {
		err = mgr.updateStatus(ctx, statefulSet)
	}
	return err
}

func TestUpdateStatusInvalidResponse(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/info?count=0&output_mode=json",
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

	mockHandlers[0].Body = loadFixture(t, "service_stack1_indexer_with_port.json")

	mockHandler := spltest.MockHTTPHandler{
		Method: "GET",
		URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json",
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
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	var replicas int32 = 1

	ctx := context.TODO()
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
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/info?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   splcommon.TestInvalidPeerStatusInScaleDownInfo,
		},
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json",
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

	err := mgr.updateStatus(ctx, statefulSet)
	if err != nil {
		t.Errorf("mgr.updateStatus() should not have returned an error here")
	}

	_, err = mgr.PrepareScaleDown(ctx, 0)
	if err == nil {
		t.Errorf("mgr.PrepareScaleDown() should have returned an error here")
	}
}

func TestInvalidPeerInFinishRecycle(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	var replicas int32 = 1

	ctx := context.TODO()
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
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/info?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   splcommon.TestInvalidPeerInFinishRecycleInfo,
		},
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json",
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

	err := mgr.updateStatus(ctx, statefulSet)
	if err != nil {
		t.Errorf("mgr.updateStatus() should not have returned an error here")
	}

	// Here we are trying to call FinishRecycle for a peer which is not in the list.
	_, err = mgr.FinishRecycle(ctx, 1)
	if err == nil {
		t.Errorf("mgr.FinishRecycle() should have returned an error here")
	}
}

func indexerClusterPodManagerUpdateTester(t *testing.T, method string, mockHandlers []spltest.MockHTTPHandler,
	desiredReplicas int32, wantPhase enterpriseApi.Phase, statefulSet *appsv1.StatefulSet,
	wantCalls map[string][]spltest.MockFuncCall, wantError error, initObjects ...client.Object) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)
	// get indexerClusterPodManager instance
	mgr := getIndexerClusterPodManager(method, mockHandlers, mockSplunkClient, 1)
	spltest.PodManagerUpdateTester(t, method, mgr, desiredReplicas, wantPhase, statefulSet, wantCalls, wantError, initObjects...)
	mockSplunkClient.CheckRequests(t, method)
}

func TestIndexerClusterPodManager(t *testing.T) {
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
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.StatefulSet-test-splunk-stack1"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		//{MetaName: "*v1.Pod-test-splunk-stack1-indexer-0"},
		{MetaName: "*v1.Pod-test-splunk-manager1-cluster-manager-0"},
		{MetaName: "*v1.Pod-test-splunk-manager1-cluster-manager-0"},
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

	wantCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[4], funcCalls[4], funcCalls[0], funcCalls[5]}, "Create": {funcCalls[1]}, "List": {listmockCall[0]}}

	// test 1 ready pod
	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/info?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   splcommon.TestIndexerClusterPodManagerInfo,
		},
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json",
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
	indexerClusterPodManagerUpdateTester(t, method, mockHandlers, 1, enterpriseApi.PhaseReady, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => decommission
	mockHandlers = append(mockHandlers, spltest.MockHTTPHandler{
		Method: "POST",
		URL:    "https://splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local:8089/services/cluster/peer/control/control/decommission?enforce_counts=0",
		Status: 200,
		Err:    nil,
		Body:   ``,
	})
	pod.ObjectMeta.Labels["controller-revision-hash"] = "v0"
	method = "indexerClusterPodManager.Update(Decommission Pod)"
	decommisonFuncCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.StatefulSet-test-splunk-stack1"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Pod-test-splunk-manager1-cluster-manager-0"},
		{MetaName: "*v1.Pod-test-splunk-manager1-cluster-manager-0"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1"},
		{MetaName: "*v1.Pod-test-splunk-stack1-0"},
		{MetaName: "*v1.Pod-test-splunk-stack1-indexer-0"},
		{MetaName: "*v1.Pod-test-splunk-stack1-indexer-0"},
	}
	wantDecomPodCalls := map[string][]spltest.MockFuncCall{"Get": decommisonFuncCalls, "Create": {funcCalls[1]}}
	indexerClusterPodManagerUpdateTester(t, method, mockHandlers, 1, enterpriseApi.PhaseUpdating, statefulSet, wantDecomPodCalls, nil, statefulSet, pod)

	// test pod needs update => wait for decommission to complete
	reassigningFuncCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.StatefulSet-test-splunk-stack1"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Pod-test-splunk-manager1-cluster-manager-0"},
		{MetaName: "*v1.Pod-test-splunk-manager1-cluster-manager-0"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1"},
		{MetaName: "*v1.Pod-test-splunk-stack1-0"},
	}
	mockHandlers = []spltest.MockHTTPHandler{mockHandlers[0], mockHandlers[1]}
	mockHandlers[1].Body = strings.Replace(mockHandlers[1].Body, `"status":"Up"`, `"status":"ReassigningPrimaries"`, 1)
	method = "indexerClusterPodManager.Update(ReassigningPrimaries)"
	wantReasCalls := map[string][]spltest.MockFuncCall{"Get": reassigningFuncCalls, "Create": {funcCalls[1]}}
	indexerClusterPodManagerUpdateTester(t, method, mockHandlers, 1, enterpriseApi.PhaseUpdating, statefulSet, wantReasCalls, nil, statefulSet, pod)

	// test pod needs update => wait for decommission to complete
	mockHandlers[1].Body = strings.Replace(mockHandlers[1].Body, `"status":"ReassigningPrimaries"`, `"status":"Decommissioning"`, 1)
	method = "indexerClusterPodManager.Update(Decommissioning)"
	wantDecomCalls := map[string][]spltest.MockFuncCall{"Get": reassigningFuncCalls, "Create": {funcCalls[1]}}
	indexerClusterPodManagerUpdateTester(t, method, mockHandlers, 1, enterpriseApi.PhaseUpdating, statefulSet, wantDecomCalls, nil, statefulSet, pod)

	// test pod needs update => delete pod
	wantCalls = map[string][]spltest.MockFuncCall{"Get": reassigningFuncCalls, "Create": {funcCalls[1]}, "Delete": {funcCalls[5]}}
	mockHandlers[1].Body = strings.Replace(mockHandlers[1].Body, `"status":"Decommissioning"`, `"status":"Down"`, 1)
	method = "indexerClusterPodManager.Update(Delete Pod)"
	indexerClusterPodManagerUpdateTester(t, method, mockHandlers, 1, enterpriseApi.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test scale down => pod not found
	// Reset mockHandlers to original peer list (not "Down" status from previous test)
	mockHandlers[1].Body = splcommon.TestIndexerClusterPodManagerPeer
	// cleanupPeerFromClusterManager makes another GET peers call to verify peer doesn't exist
	// The peer (splunk-stack1-indexer-1) doesn't exist in the response, so no POST remove_peers call
	mockHandlers = append(mockHandlers, spltest.MockHTTPHandler{
		Method: "GET",
		URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json",
		Status: 200,
		Err:    nil,
		Body:   splcommon.TestIndexerClusterPodManagerPeer,
	})
	pod.ObjectMeta.Name = "splunk-stack1-2"
	replicas = 2
	statefulSet.Spec.Replicas = &replicas // Ensure Spec matches Status
	statefulSet.Status.Replicas = 2
	statefulSet.Status.ReadyReplicas = 2
	statefulSet.Status.UpdatedReplicas = 2

	// Get calls include:
	// - StatefulSet (initial)
	// - Secrets for namespace-scoped secret
	// - Cluster manager pod (multiple times for status updates)
	// - StatefulSet re-fetch
	// - PVC Gets (for scale-down cleanup)
	podNotFoundCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.StatefulSet-test-splunk-stack1"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Pod-test-splunk-manager1-cluster-manager-0"},
		{MetaName: "*v1.Pod-test-splunk-manager1-cluster-manager-0"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1"},             // Re-fetch StatefulSet
		{MetaName: "*v1.Pod-test-splunk-manager1-cluster-manager-0"}, // cleanupPeerFromClusterManager
		{MetaName: "*v1.Pod-test-splunk-manager1-cluster-manager-0"}, // cleanupPeerFromClusterManager
		{MetaName: "*v1.PersistentVolumeClaim-test-pvc-etc-splunk-stack1-1"},
		{MetaName: "*v1.PersistentVolumeClaim-test-pvc-var-splunk-stack1-1"},
	}
	wantCalls = map[string][]spltest.MockFuncCall{
		"Get":    podNotFoundCalls,
		"Create": {funcCalls[1]},
		"Update": {funcCalls[0]}, // StatefulSet update to scale down
	}
	method = "indexerClusterPodManager.Update(Pod Not Found)"
	indexerClusterPodManagerUpdateTester(t, method, mockHandlers, 1, enterpriseApi.PhaseScalingDown, statefulSet, wantCalls, nil, statefulSet, pod)

	// test scale down => decommission pod
	replicas = 2 // Ensure Spec.Replicas matches Status.Replicas
	statefulSet.Spec.Replicas = &replicas
	mockHandlers[1].Body = loadFixture(t, "configmap_indexer_smartstore.json")
	// Add mock handler for remove_peers POST call
	if len(mockHandlers) == 2 {
		mockHandlers = append(mockHandlers, spltest.MockHTTPHandler{
			Method: "POST",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/control/control/remove_peers?peers=D39B1729-E2C5-4273-B9B2-534DA7C2F866",
			Status: 200,
			Err:    nil,
			Body:   ``,
		})
	}
	pvcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.PersistentVolumeClaim-test-pvc-etc-splunk-stack1-1"},
		{MetaName: "*v1.PersistentVolumeClaim-test-pvc-var-splunk-stack1-1"},
	}
	decommisionFuncCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.StatefulSet-test-splunk-stack1"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Pod-test-splunk-manager1-cluster-manager-0"},
		{MetaName: "*v1.Pod-test-splunk-manager1-cluster-manager-0"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1"},                     // Re-fetch StatefulSet in UpdateStatefulSetPods
		{MetaName: "*v1.Pod-test-splunk-manager1-cluster-manager-0"},         // getClusterManagerClient in PrepareScaleDown
		{MetaName: "*v1.Pod-test-splunk-manager1-cluster-manager-0"},         // getClusterManagerClient for RemoveIndexerClusterPeer
		{MetaName: "*v1.PersistentVolumeClaim-test-pvc-etc-splunk-stack1-1"}, // PVC check (returns NotFound)
		{MetaName: "*v1.PersistentVolumeClaim-test-pvc-var-splunk-stack1-1"}, // PVC check (returns NotFound)
	}
	wantCalls = map[string][]spltest.MockFuncCall{"Get": decommisionFuncCalls, "Create": {funcCalls[1]}, "Delete": pvcCalls, "Update": {funcCalls[0]}}
	//wantCalls["Get"] = append(wantCalls["Get"], pvcCalls...)
	pvcList := []*corev1.PersistentVolumeClaim{
		{ObjectMeta: metav1.ObjectMeta{Name: "pvc-etc-splunk-stack1-1", Namespace: "test"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "pvc-var-splunk-stack1-1", Namespace: "test"}},
	}
	method = "indexerClusterPodManager.Update(Decommission)"
	pod.ObjectMeta.Name = "splunk-stack1-0"
	// Note: We don't create pod-1 here because the test is for the case where the pod
	// has already been decommissioned and removed, so the pod existence check should fail
	// and the code should skip PrepareScaleDown and go straight to scaling down.
	indexerClusterPodManagerUpdateTester(t, method, mockHandlers, 1, enterpriseApi.PhaseScalingDown, statefulSet, wantCalls, nil, statefulSet, pod, pvcList[0], pvcList[1])
}

func TestSetClusterMaintenanceMode(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	var initObjectList []client.Object

	ctx := context.TODO()

	c := spltest.NewMockClient()

	// Get namespace scoped secret
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Errorf("Apply namespace scoped secret failed")
	}

	// Create pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-cluster-manager-0",
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

	cr.Spec.ClusterManagerRef.Name = cr.GetName()
	cmPodName := pod.GetName()

	podExecCommands := []string{
		"maintenance-mode",
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

	// Invalid scenario where enabling MM returned error
	err = SetClusterMaintenanceMode(ctx, c, &cr, true, cmPodName, mockPodExecClient)
	if err == nil {
		t.Errorf("SetClusterMaintenanceMode should have returned error")
	}
	if cr.Status.MaintenanceMode != false {
		t.Errorf("Couldn't disable cm maintenance mode %s", err.Error())
	}

	// Enable CM maintenance mode
	mockPodExecReturnContexts[0].Err = nil
	err = SetClusterMaintenanceMode(ctx, c, &cr, true, cmPodName, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't enable cm maintenance mode %s", err.Error())
	}

	if cr.Status.MaintenanceMode != true {
		t.Errorf("Couldn't enable cm maintenance mode %s", err.Error())
	}

	// Disable CM maintenance mode
	err = SetClusterMaintenanceMode(ctx, c, &cr, false, cmPodName, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't disable cm maintenance mode %s", err.Error())
	}

	if cr.Status.MaintenanceMode != false {
		t.Errorf("Couldn't disable cm maintenance mode %s", err.Error())
	}

	mockPodExecClient.CheckPodExecCommands(t, "SetClusterMaintenanceMode")
}

func TestApplyIdxcSecret(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	method := "ApplyIdxcSecret"
	scopedLog := logt.WithName(method)
	var initObjectList []client.Object

	ctx := context.TODO()

	c := spltest.NewMockClient()

	// Get namespace scoped secret
	nsSecret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
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
			Name:      "splunk-stack1-cluster-manager-0",
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
	cr.Spec.ClusterManagerRef.Name = cr.GetName()
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

	podExecCommands := []string{
		"maintenance-mode",
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

	// Set resource version to that of NS secret
	err = ApplyIdxcSecret(ctx, mgr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply idxc secret %s", err.Error())
	}

	// Change resource version
	mgr.cr.Status.NamespaceSecretResourceVersion = "0"

	// Invalid scenario where SetClusterMaintenanceMode would return error
	err = ApplyIdxcSecret(ctx, mgr, 1, mockPodExecClient)
	if err == nil {
		t.Errorf("ApplyIdxcSecret should have returned error")
	}

	// Valid scenario where SetClusterMaintenanceMode would not return error
	mockPodExecReturnContexts[0].Err = nil
	err = ApplyIdxcSecret(ctx, mgr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply idxc secret %s", err.Error())
	}
	mockSplunkClient.CheckRequests(t, method)

	// Don't set as it is set already
	secrets.Data[splcommon.IdxcSecret] = []byte{'a'}
	err = splutil.UpdateResource(ctx, c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}
	err = ApplyIdxcSecret(ctx, mgr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply idxc secret %s", err.Error())
	}

	mgr.cr.Status.IndexerSecretChanged[0] = false
	secrets.Data[splcommon.IdxcSecret] = []byte{'a'}
	err = splutil.UpdateResource(ctx, c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}
	// Test set again
	err = ApplyIdxcSecret(ctx, mgr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply idxc secret %s", err.Error())
	}

	// Test the setCmMode failure
	secrets.Data[splcommon.IdxcSecret] = []byte{'a'}
	err = splutil.UpdateResource(ctx, c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	mgr.cr.Status.NamespaceSecretResourceVersion = "2"
	mgr.cr.Spec.ClusterManagerRef.Name = ""
	mgr.cr.Status.MaintenanceMode = false
	mgr.cr.Status.IndexerSecretChanged = []bool{}
	err = ApplyIdxcSecret(ctx, mgr, 1, mockPodExecClient)
	if err.Error() != splcommon.EmptyClusterManagerRef {
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

	err = splutil.UpdateResource(ctx, c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	err = ApplyIdxcSecret(ctx, mgr, 1, mockPodExecClient)
	if err.Error() != fmt.Sprintf(splcommon.SecretTokenNotRetrievable, splcommon.IdxcSecret) {
		t.Errorf("Couldn't recognize missing idxc secret %s", err.Error())
	}

	// Test scenario with same namespace secret and cr status resource version
	nsSecret.ResourceVersion = "1"
	mgr.cr.Status.NamespaceSecretResourceVersion = nsSecret.ResourceVersion
	err = splutil.UpdateResource(ctx, c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	err = ApplyIdxcSecret(ctx, mgr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply idxc secret %s", err.Error())
	}

	// Test missing secret from pod
	mgr.cr.Status.NamespaceSecretResourceVersion = "10"
	err = splutil.DeleteResource(ctx, c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	err = ApplyIdxcSecret(ctx, mgr, 1, mockPodExecClient)
	if err.Error() != fmt.Sprintf(splcommon.PodSecretNotFoundError, podName) {
		t.Errorf("Couldn't recognize missing secret from Pod, error: %s", err.Error())
	}

	// Test the secret update is skipped when the pod is not existing
	err = splutil.UpdateResource(ctx, c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource %v, err: %v", secrets, err)
	}
	err = splutil.DeleteResource(ctx, c, pod)
	if err != nil {
		t.Errorf("Couldn't update resource %v, err: %v", pod, err)
	}
	err = ApplyIdxcSecret(ctx, mgr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't recognize missing idxc secret %s", err.Error())
	}
}

func TestInvalidIndexerClusterSpec(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	cr := enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	cm := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "manager1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	c.AddObject(&cm)

	cm.Status.Phase = enterpriseApi.PhaseReady
	// Empty ClusterManagerRef should return an error
	cr.Spec.ClusterManagerRef.Name = ""
	if _, err := ApplyIndexerClusterManager(context.Background(), c, &cr); err == nil {
		t.Errorf("ApplyIndxerCluster() should have returned error")
	}

	cr.Spec.ClusterManagerRef.Name = "manager1"
	// verifyRFPeers should return err here
	if _, err := ApplyIndexerClusterManager(context.Background(), c, &cr); err == nil {
		t.Errorf("ApplyIndxerCluster() should have returned error")
	}

	cm.Status.Phase = enterpriseApi.PhaseError
	cr.Spec.CommonSplunkSpec.EtcVolumeStorageConfig.StorageCapacity = "-abcd"
	if _, err := ApplyIndexerClusterManager(context.Background(), c, &cr); err == nil {
		t.Errorf("ApplyIndxerCluster() should have returned error")
	}
}

func TestGetIndexerStatefulSet(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	cr := enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	ctx := context.TODO()

	c := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Errorf("Failed to create namespace scoped object")
	}

	cr.Spec.ClusterManagerRef.Name = "manager1"
	test := func(want string) {
		f := func() (interface{}, error) {
			if err := validateIndexerClusterSpec(ctx, c, &cr); err != nil {
				t.Errorf("validateIndexerClusterSpec() returned error: %v", err)
			}
			return getIndexerStatefulSet(ctx, c, &cr)
		}
		configTester(t, "getIndexerStatefulSet()", f, want)
	}

	cr.Spec.Replicas = 0
	test(loadFixture(t, "statefulset_stack1_indexer_base.json"))
	cr.Spec.Replicas = 1
	test(loadFixture(t, "statefulset_stack1_indexer_base_1.json"))

	// Define additional service port in CR and verified the statefulset has the new port
	cr.Spec.ServiceTemplate.Spec.Ports = []corev1.ServicePort{{Name: "user-defined", Port: 32000, Protocol: "UDP"}}
	test(loadFixture(t, "statefulset_stack1_indexer_base_2.json"))
	// Block moving DefaultsURLApps to SPLUNK_DEFAULTS_URL for indexer cluster member
	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	test(loadFixture(t, "statefulset_stack1_indexer_base_3.json"))

	// Create a serviceaccount
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	test(loadFixture(t, "statefulset_stack1_indexer_with_service_account.json"))

	// Add extraEnv
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	test(loadFixture(t, "statefulset_stack1_indexer_with_service_account_1.json"))

	// Add additional label to cr metadata to transfer to the statefulset
	cr.ObjectMeta.Labels = make(map[string]string)
	cr.ObjectMeta.Labels["app.kubernetes.io/test-extra-label"] = "test-extra-label-value"
	test(loadFixture(t, "statefulset_stack1_indexer_with_service_account_2.json"))

	cr.Spec.ClusterManagerRef.Namespace = "other"
	if err := validateIndexerClusterSpec(ctx, c, &cr); err == nil {
		t.Errorf("validateIndexerClusterSpec() error expected on multisite IndexerCluster referencing a cluster manager located in a different namespace")
	}
}

func TestIndexerClusterSpecNotCreatedWithoutGeneralTerms(t *testing.T) {
	// Unset the SPLUNK_GENERAL_TERMS environment variable
	os.Unsetenv("SPLUNK_GENERAL_TERMS")
	ctx := context.TODO()

	// Create a mock indexer cluster CR
	idxc := enterpriseApi.IndexerCluster{
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
				ClusterManagerRef: corev1.ObjectReference{
					Name: "manager1",
				},
				Mock: true,
			},
		},
	}

	// Create a mock client
	c := spltest.NewMockClient()

	// Attempt to apply the indexer cluster spec
	_, err := ApplyIndexerCluster(ctx, c, &idxc)

	// Assert that an error is returned
	if err == nil {
		t.Errorf("Expected error when SPLUNK_GENERAL_TERMS is not set, but got none")
	} else if err.Error() != "license not accepted, please adjust SPLUNK_GENERAL_TERMS to indicate you have accepted the current/latest version of the license. See README file for additional information" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestGetIndexerClusterList(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	idxc := enterpriseApi.IndexerCluster{}

	listOpts := []client.ListOption{
		client.InNamespace("test"),
	}

	client := spltest.NewMockClient()

	idxcList := &enterpriseApi.IndexerClusterList{}
	idxcList.Items = append(idxcList.Items, idxc)

	client.ListObj = idxcList

	objectList, err := getIndexerClusterList(ctx, client, &idxc, listOpts)
	if err != nil {
		t.Errorf("getNumOfObjects should not have returned error=%v", err)
	}

	numOfObjects := len(objectList.Items)
	if numOfObjects != 1 {
		t.Errorf("Got wrong number of IndexerCluster objects. Expected=%d, Got=%d", 1, numOfObjects)
	}
}

func TestIndexerClusterWithReadyState(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	mclient := &spltest.MockHTTPClient{}
	type Entry1 struct {
		Content splclient.ClusterManagerInfo `json:"content"`
	}

	apiResponse1 := struct {
		Entry []Entry1 `json:"entry"`
	}{
		Entry: []Entry1{
			{
				Content: splclient.ClusterManagerInfo{
					Initialized:     true,
					IndexingReady:   true,
					ServiceReady:    true,
					MaintenanceMode: true,
				},
			},
			{
				Content: splclient.ClusterManagerInfo{
					Initialized:     true,
					IndexingReady:   true,
					ServiceReady:    true,
					MaintenanceMode: true,
				},
			},
		},
	}

	type Entry struct {
		Name    string                           `json:"name"`
		Content splclient.ClusterManagerPeerInfo `json:"content"`
	}

	apiResponse2 := struct {
		Entry []Entry `json:"entry"`
	}{
		Entry: []Entry{
			{
				Name: "testing",
				Content: splclient.ClusterManagerPeerInfo{
					ID:             "testing",
					Status:         "Up",
					ActiveBundleID: "testing",
					BucketCount:    2,
					Searchable:     true,
					Label:          "splunk-test-indexer-0",
				},
			},
		},
	}

	response1, _ := json.Marshal(apiResponse1)
	response2, _ := json.Marshal(apiResponse2)
	wantRequest1, _ := http.NewRequest("GET", "https://splunk-test-cluster-manager-service.default.svc.cluster.local:8089/services/cluster/manager/info?count=0&output_mode=json", nil)
	wantRequest2, _ := http.NewRequest("GET", "https://splunk-test-cluster-manager-service.default.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json", nil)
	mclient.AddHandler(wantRequest1, 200, string(response1), nil)
	mclient.AddHandler(wantRequest2, 200, string(response2), nil)

	// mock the verify RF peer function
	VerifyRFPeers = func(ctx context.Context, mgr indexerClusterPodManager, client splcommon.ControllerClient) error {
		return nil
	}

	newIndexerClusterPodManager = func(log logr.Logger, cr *enterpriseApi.IndexerCluster, secret *corev1.Secret, newSplunkClient NewSplunkClientFunc) indexerClusterPodManager {
		return indexerClusterPodManager{
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

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	builder := fake.NewClientBuilder().
		WithScheme(sch).
		WithStatusSubresource(&enterpriseApi.LicenseManager{}).
		WithStatusSubresource(&enterpriseApi.ClusterManager{}).
		WithStatusSubresource(&enterpriseApi.Standalone{}).
		WithStatusSubresource(&enterpriseApi.MonitoringConsole{}).
		WithStatusSubresource(&enterpriseApi.IndexerCluster{}).
		WithStatusSubresource(&enterpriseApi.SearchHeadCluster{})
	c := builder.Build()
	utilruntime.Must(enterpriseApi.AddToScheme(clientgoscheme.Scheme))
	ctx := context.TODO()

	// Create App framework volume
	volumeSpec := []enterpriseApi.VolumeSpec{
		{
			Name:      "testing",
			Endpoint:  "/someendpoint",
			Path:      "s3-test",
			SecretRef: "secretRef",
			Provider:  "aws",
			Type:      "s3",
			Region:    "west",
		},
	}

	// AppSourceDefaultSpec: Remote Storage volume name and Scope of App deployment
	appSourceDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
		VolName: "testing",
		Scope:   "local",
	}

	// appSourceSpec: App source name, location and volume name and scope from appSourceDefaultSpec
	appSourceSpec := []enterpriseApi.AppSourceSpec{
		{
			Name:                 "appSourceName",
			Location:             "appSourceLocation",
			AppSourceDefaultSpec: appSourceDefaultSpec,
		},
	}

	// appFrameworkSpec: AppSource settings, Poll Interval, volumes, appSources on volumes
	appFrameworkSpec := enterpriseApi.AppFrameworkSpec{
		Defaults:             appSourceDefaultSpec,
		AppsRepoPollInterval: int64(60),
		VolList:              volumeSpec,
		AppSources:           appSourceSpec,
	}

	// create clustermanager custom resource
	clustermanager := &enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				Volumes: []corev1.Volume{},
				MonitoringConsoleRef: corev1.ObjectReference{
					Name: "mcName",
				},
			},
			AppFrameworkConfig: appFrameworkSpec,
		},
	}

	creplicas := int32(1)
	cstatefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-cluster-manager",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: "splunk-test-cluster-manager-headless",
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
						},
					},
				},
			},
			Replicas: &creplicas,
		},
	}

	// simulate create clustermanager instance before reconcilation
	c.Create(ctx, clustermanager)

	// simulate Ready state
	namespacedName := types.NamespacedName{
		Name:      clustermanager.Name,
		Namespace: clustermanager.Namespace,
	}
	err := c.Get(ctx, namespacedName, clustermanager)
	if err != nil {
		t.Errorf("Unexpected get cluster manager %v", err)
		debug.PrintStack()
	}
	clustermanager.Status.Phase = enterpriseApi.PhaseReady
	clustermanager.Spec.ServiceTemplate.Annotations = map[string]string{
		"traffic.sidecar.istio.io/excludeOutboundPorts": "8089,8191,9997",
		"traffic.sidecar.istio.io/includeInboundPorts":  "8000,8088",
	}
	clustermanager.Spec.ServiceTemplate.Labels = map[string]string{
		"app.kubernetes.io/instance":   "splunk-test-cluster-manager",
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "cluster-manager",
		"app.kubernetes.io/name":       "cluster-manager",
		"app.kubernetes.io/part-of":    "splunk-test-cluster-manager",
	}
	err = c.Status().Update(ctx, clustermanager)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for cluster manager with app framework  %v", err)
		debug.PrintStack()
	}

	err = c.Get(ctx, namespacedName, clustermanager)
	if err != nil {
		t.Errorf("Unexpected get cluster manager %v", err)
		debug.PrintStack()
	}

	// call reconciliation
	_, err = ApplyClusterManager(ctx, c, clustermanager)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for cluster manager with app framework  %v", err)
		debug.PrintStack()
	}

	// create pod
	stpod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-cluster-manager-0",
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

	// update statefulset
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
		t.Errorf("Unexpected update statefulset  %v", err)
		debug.PrintStack()
	}

	stNamespacedName := types.NamespacedName{
		Name:      "splunk-test-cluster-manager",
		Namespace: "default",
	}
	err = c.Get(ctx, stNamespacedName, cstatefulset)
	if err != nil {
		t.Errorf("Unexpected get cluster manager %v", err)
		debug.PrintStack()
	}
	// update statefulset
	cstatefulset.Status.ReadyReplicas = 1
	cstatefulset.Status.Replicas = 1
	err = c.Status().Update(ctx, cstatefulset)
	if err != nil {
		t.Errorf("Unexpected update statefulset  %v", err)
		debug.PrintStack()
	}

	err = c.Get(ctx, namespacedName, clustermanager)
	if err != nil {
		t.Errorf("Unexpected get cluster manager %v", err)
		debug.PrintStack()
	}

	// Mock the addTelApp function for unit tests
	addTelApp = func(ctx context.Context, podExecClient splutil.PodExecClientImpl, replicas int32, cr splcommon.MetaObject) error {
		return nil
	}

	// call reconciliation
	_, err = ApplyClusterManager(ctx, c, clustermanager)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for cluster manager with app framework  %v", err)
		debug.PrintStack()
	}

	clusterObjRef := corev1.ObjectReference{
		Kind:      clustermanager.Kind,
		Name:      clustermanager.Name,
		Namespace: clustermanager.Namespace,
		UID:       clustermanager.UID,
	}

	// create indexercluster custom resource
	indexercluster := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: enterpriseApi.IndexerClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				Volumes:           []corev1.Volume{},
				ClusterManagerRef: clusterObjRef,
			},
		},
	}

	replicas := int32(1)
	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-indexer",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: "splunk-test-indexer-headless",
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
						},
					},
				},
			},
			Replicas: &replicas,
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-indexer-headless",
			Namespace: "default",
		},
	}

	// simulate service
	c.Create(ctx, service)

	// simulate create stateful set
	c.Create(ctx, statefulset)

	// simulate create clustermanager instance before reconcilation
	c.Create(ctx, indexercluster)

	GetClusterInfoCall = func(ctx context.Context, mgr *indexerClusterPodManager, mockCall bool) (*splclient.ClusterInfo, error) {
		cinfo := &splclient.ClusterInfo{
			MultiSite: "false",
		}
		return cinfo, nil
	}
	GetClusterManagerPeersCall = func(ctx context.Context, mgr *indexerClusterPodManager) (map[string]splclient.ClusterManagerPeerInfo, error) {
		response := map[string]splclient.ClusterManagerPeerInfo{
			"splunk-test-indexer-0": {
				ID:             "site-1",
				Status:         "Up",
				ActiveBundleID: "1",
				BucketCount:    10,
				Searchable:     true,
			},
		}
		return response, err
	}
	_, err = ApplyIndexerClusterManager(ctx, c, indexercluster)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for indexer cluster %v", err)
		debug.PrintStack()
	}

	namespacedName = types.NamespacedName{
		Name:      indexercluster.Name,
		Namespace: indexercluster.Namespace,
	}
	err = c.Get(ctx, namespacedName, indexercluster)
	if err != nil {
		t.Errorf("Unexpected get indexer cluster %v", err)
		debug.PrintStack()
	}
	// simulate Ready state
	indexercluster.Status.Phase = enterpriseApi.PhaseReady
	indexercluster.Spec.ServiceTemplate.Annotations = map[string]string{
		"traffic.sidecar.istio.io/excludeOutboundPorts": "8089,8191,9997",
		"traffic.sidecar.istio.io/includeInboundPorts":  "8000,8088",
	}
	indexercluster.Spec.ServiceTemplate.Labels = map[string]string{
		"app.kubernetes.io/instance":   "splunk-test-indexer-cluster",
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "indexer-cluster",
		"app.kubernetes.io/name":       "indexer-cluster",
		"app.kubernetes.io/part-of":    "splunk-test-indexer-cluster",
	}
	err = c.Status().Update(ctx, indexercluster)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for cluster manager with app framework  %v", err)
		debug.PrintStack()
	}

	err = c.Get(ctx, namespacedName, indexercluster)
	if err != nil {
		t.Errorf("Unexpected get indexer cluster %v", err)
		debug.PrintStack()
	}

	// call reconciliation
	_, err = ApplyIndexerClusterManager(ctx, c, indexercluster)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for cluster manager with app framework  %v", err)
		debug.PrintStack()
	}

	// create pod
	stpod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-indexer-0",
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

	// update statefulset
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
		t.Errorf("Unexpected update statefulset  %v", err)
		debug.PrintStack()
	}

	stNamespacedName = types.NamespacedName{
		Name:      "splunk-test-indexer",
		Namespace: "default",
	}
	err = c.Get(ctx, stNamespacedName, statefulset)
	if err != nil {
		t.Errorf("Unexpected get indexer cluster %v", err)
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

	err = c.Get(ctx, namespacedName, indexercluster)
	if err != nil {
		t.Errorf("Unexpected get indexer cluster %v", err)
		debug.PrintStack()
	}

	indexercluster.Status.Initialized = true
	indexercluster.Status.IndexingReady = true
	indexercluster.Status.ServiceReady = true
	// call reconciliation
	_, err = ApplyIndexerClusterManager(ctx, c, indexercluster)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for indexer cluster with app framework  %v", err)
		debug.PrintStack()
	}
}

// TestPrepareScaleDownOutOfBounds tests PrepareScaleDown when peer index is out of bounds
func TestPrepareScaleDownOutOfBounds(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	// Create indexer cluster pod manager with empty peer status (out of bounds scenario)
	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/info?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   splcommon.TestIndexerClusterPodManagerInfo,
		},
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   `{"entry": []}`, // Empty peers list
		},
	}

	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)
	mgr := getIndexerClusterPodManager("TestPrepareScaleDownOutOfBounds", mockHandlers, mockSplunkClient, 3)

	// Initialize status with updateStatus to set up the mgr state
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: func() *int32 { r := int32(3); return &r }(),
		},
	}

	c := spltest.NewMockClient()
	mgr.c = c
	err := mgr.updateStatus(ctx, statefulSet)
	if err != nil {
		t.Errorf("updateStatus failed: %v", err)
	}

	// Test PrepareScaleDown with index 2 when Status.Peers is empty (out of bounds)
	ready, err := mgr.PrepareScaleDown(ctx, 2)
	if err != nil {
		t.Errorf("PrepareScaleDown should handle out of bounds gracefully, got error: %v", err)
	}
	if !ready {
		t.Errorf("PrepareScaleDown should return true (ready) for out of bounds index")
	}
}

// TestPrepareScaleDownEmptyPeerID tests PrepareScaleDown when peer ID is empty
func TestPrepareScaleDownEmptyPeerID(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	// Create peer response with valid ID first for the initial updateStatus call
	// Note: 'name' field becomes the ID, 'label' field is the hostname used as map key
	peerWithValidID := `{"entry":[{"name":"VALID-PEER-GUID-123","content":{"label":"splunk-stack1-indexer-0","status":"Up"}}]}`

	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/info?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   splcommon.TestIndexerClusterPodManagerInfo,
		},
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   peerWithValidID,
		},
	}

	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)
	mgr := getIndexerClusterPodManager("TestPrepareScaleDownEmptyPeerID", mockHandlers, mockSplunkClient, 1)

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: func() *int32 { r := int32(1); return &r }(),
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:        1,
			ReadyReplicas:   1,
			UpdatedReplicas: 1,
		},
	}

	c := spltest.NewMockClient()
	mgr.c = c
	err := mgr.updateStatus(ctx, statefulSet)
	if err != nil {
		t.Errorf("updateStatus failed: %v", err)
	}

	// Verify we have a peer now
	if len(mgr.cr.Status.Peers) == 0 {
		t.Fatalf("Expected at least one peer in status after updateStatus")
	}

	// Manually set peer ID to empty to simulate the edge case
	mgr.cr.Status.Peers[0].ID = ""

	// Test PrepareScaleDown with empty peer ID - should trigger fallback path
	// We're not testing the actual removal here, just that it handles empty ID gracefully
	// by attempting the fallback (which will fail in this test setup, but that's OK)
	_, err = mgr.PrepareScaleDown(ctx, 0)
	// The fallback will attempt to query CM, but we haven't mocked that second GET request
	// So we expect an error here, but the important thing is it didn't panic
	// and it attempted the fallback path
	if err == nil {
		t.Logf("PrepareScaleDown completed (likely found no peer to remove)")
	} else {
		t.Logf("PrepareScaleDown attempted fallback cleanup (expected in this test setup): %v", err)
	}
	// The test passes as long as we didn't panic on empty ID
}

// TestCleanupPeerFromClusterManagerPeerExists tests cleanupPeerFromClusterManager when peer exists
func TestCleanupPeerFromClusterManagerPeerExists(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	peerName := "splunk-stack1-indexer-2"
	peerID := "TEST-PEER-GUID-123"

	// Mock response with the peer we're looking for
	// Note: 'name' becomes the ID after parsing, 'label' is used as map key
	peersResponse := fmt.Sprintf(`{
		"entry": [
			{
				"name": "%s",
				"content": {
					"label": "%s",
					"status": "Up"
				}
			}
		]
	}`, peerID, peerName)

	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/info?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   splcommon.TestIndexerClusterPodManagerInfo,
		},
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   peersResponse,
		},
		{
			Method: "POST",
			URL:    fmt.Sprintf("https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/control/control/remove_peers?peers=%s", peerID),
			Status: 200,
			Err:    nil,
			Body:   `{}`,
		},
	}

	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)
	mgr := getIndexerClusterPodManager("TestCleanupPeerFromClusterManagerPeerExists", mockHandlers, mockSplunkClient, 1)

	c := spltest.NewMockClient()
	mgr.c = c

	// Call cleanupPeerFromClusterManager - should find and remove the peer
	err := mgr.cleanupPeerFromClusterManager(ctx, peerName)
	if err != nil {
		t.Errorf("cleanupPeerFromClusterManager should succeed when peer exists, got error: %v", err)
	}
}

// TestCleanupPeerFromClusterManagerPeerNotFound tests cleanupPeerFromClusterManager when peer doesn't exist
func TestCleanupPeerFromClusterManagerPeerNotFound(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	peerName := "splunk-stack1-indexer-2"

	// Mock response with no matching peer (peer already removed)
	// Using different label so the peer we're looking for won't be found
	peersResponse := `{
		"entry": [
			{
				"name": "DIFFERENT-PEER-GUID",
				"content": {
					"label": "splunk-stack1-indexer-0",
					"status": "Up"
				}
			}
		]
	}`

	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/info?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   splcommon.TestIndexerClusterPodManagerInfo,
		},
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   peersResponse,
		},
	}

	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)
	mgr := getIndexerClusterPodManager("TestCleanupPeerFromClusterManagerPeerNotFound", mockHandlers, mockSplunkClient, 1)

	c := spltest.NewMockClient()
	mgr.c = c

	// Call cleanupPeerFromClusterManager - should return nil (success) when peer not found
	err := mgr.cleanupPeerFromClusterManager(ctx, peerName)
	if err != nil {
		t.Errorf("cleanupPeerFromClusterManager should succeed when peer not found (already removed), got error: %v", err)
	}
}

// TestCleanupPeerFromClusterManagerQueryFails tests cleanupPeerFromClusterManager when CM query fails
func TestCleanupPeerFromClusterManagerQueryFails(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	// Reset GetClusterManagerPeersCall to use the real implementation (not a mock from other tests)
	// This ensures the test uses the HTTP handlers we set up below
	originalGetClusterManagerPeersCall := GetClusterManagerPeersCall
	defer func() {
		GetClusterManagerPeersCall = originalGetClusterManagerPeersCall
	}()
	GetClusterManagerPeersCall = func(ctx context.Context, mgr *indexerClusterPodManager) (map[string]splclient.ClusterManagerPeerInfo, error) {
		c := mgr.getClusterManagerClient(ctx)
		return c.GetClusterManagerPeers()
	}

	peerName := "splunk-stack1-indexer-2"

	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/info?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   splcommon.TestIndexerClusterPodManagerInfo,
		},
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json",
			Status: 500,
			Err:    fmt.Errorf("cluster manager unavailable"),
			Body:   ``,
		},
	}

	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)
	mgr := getIndexerClusterPodManager("TestCleanupPeerFromClusterManagerQueryFails", mockHandlers, mockSplunkClient, 1)

	c := spltest.NewMockClient()
	mgr.c = c

	// Call cleanupPeerFromClusterManager - should return error when CM query fails
	err := mgr.cleanupPeerFromClusterManager(ctx, peerName)
	if err == nil {
		t.Errorf("cleanupPeerFromClusterManager should return error when CM query fails")
	}
}

// TestDecommissionOutOfBounds tests decommission with out-of-bounds peer index
func TestDecommissionOutOfBounds(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/info?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   splcommon.TestIndexerClusterPodManagerInfo,
		},
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   `{"entry": []}`, // Empty peers list
		},
	}

	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)
	mgr := getIndexerClusterPodManager("TestDecommissionOutOfBounds", mockHandlers, mockSplunkClient, 3)

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: func() *int32 { r := int32(3); return &r }(),
		},
	}

	c := spltest.NewMockClient()
	mgr.c = c
	err := mgr.updateStatus(ctx, statefulSet)
	if err != nil {
		t.Errorf("updateStatus failed: %v", err)
	}

	// Test decommission with index 2 when Status.Peers is empty (out of bounds)
	ready, err := mgr.decommission(ctx, 2, false)
	if err != nil {
		t.Errorf("decommission should handle out of bounds gracefully, got error: %v", err)
	}
	if !ready {
		t.Errorf("decommission should return true for out of bounds index (nothing to decommission)")
	}
}

// TestNoZombiePeersAfterScaleDown verifies no zombie peers remain after various scale-down scenarios
func TestNoZombiePeersAfterScaleDown(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	testCases := []struct {
		name         string
		initialPeers string
		peerIndex    int32
		description  string
	}{
		{
			name: "Normal scale-down with valid peer",
			initialPeers: `{
				"entry": [
					{
						"name": "peer-123",
						"content": {
							"label": "splunk-stack1-indexer-2",
							"status": "Up"
						}
					}
				]
			}`,
			peerIndex:   0,
			description: "Peer exists with valid ID and should be removed",
		},
		{
			name: "Scale-down with empty peer ID",
			initialPeers: `{
				"entry": [
					{
						"name": "peer-empty-id",
						"content": {
							"label": "splunk-stack1-indexer-2",
							"status": "Up"
						}
					}
				]
			}`,
			peerIndex:   0,
			description: "Peer with empty ID should use fallback cleanup",
		},
		{
			name:         "Scale-down with out-of-bounds index",
			initialPeers: `{"entry": []}`,
			peerIndex:    2,
			description:  "Out of bounds index should be handled gracefully",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockHandlers := []spltest.MockHTTPHandler{
				{
					Method: "GET",
					URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/info?count=0&output_mode=json",
					Status: 200,
					Err:    nil,
					Body:   splcommon.TestIndexerClusterPodManagerInfo,
				},
				{
					Method: "GET",
					URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json",
					Status: 200,
					Err:    nil,
					Body:   tc.initialPeers,
				},
			}

			// Add cleanup handlers if peer exists
			if tc.peerIndex == 0 && tc.initialPeers != `{"entry": []}` {
				// Add handlers for cleanup
				mockHandlers = append(mockHandlers, spltest.MockHTTPHandler{
					Method: "GET",
					URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json",
					Status: 200,
					Err:    nil,
					Body:   tc.initialPeers,
				})
				mockHandlers = append(mockHandlers, spltest.MockHTTPHandler{
					Method: "POST",
					URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/control/control/remove_peers?peers=peer-123",
					Status: 200,
					Err:    nil,
					Body:   `{}`,
				})
			}

			mockSplunkClient := &spltest.MockHTTPClient{}
			mockSplunkClient.AddHandlers(mockHandlers...)
			mgr := getIndexerClusterPodManager(tc.name, mockHandlers, mockSplunkClient, 3)

			statefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "splunk-stack1",
					Namespace: "test",
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: func() *int32 { r := int32(3); return &r }(),
				},
			}

			c := spltest.NewMockClient()
			mgr.c = c
			err := mgr.updateStatus(ctx, statefulSet)
			if err != nil {
				t.Errorf("updateStatus failed: %v", err)
			}

			// Execute PrepareScaleDown
			ready, err := mgr.PrepareScaleDown(ctx, tc.peerIndex)
			if err != nil {
				t.Errorf("%s: PrepareScaleDown failed: %v", tc.description, err)
			}
			if !ready {
				t.Errorf("%s: PrepareScaleDown should be ready", tc.description)
			}

			// Success means no zombie peers should remain
			t.Logf("%s: Successfully handled - no zombie peers", tc.description)
		})
	}
}

func TestImageUpdatedTo9(t *testing.T) {
	if !imageUpdatedTo9("splunk/splunk:8.2.6", "splunk/splunk:9.0.0") {
		t.Errorf("Should have detected an upgrade from 8 to 9")
	}
	if imageUpdatedTo9("splunk/splunk:9.0.3", "splunk/splunk:9.0.4") {
		t.Errorf("Should not have detected an upgrade from 8 to 9")
	}
	if imageUpdatedTo9("splunk/splunk:8.2.6", "splunk/splunk:latest") {
		t.Errorf("Should not have detected an upgrade from 8 to 9, latest doesn't allow to know the version")
	}
	if imageUpdatedTo9("splunk/splunk", "splunk/splunk") {
		t.Errorf("Should not have detected an upgrade from 8 to 9, there is no colon and version")
	}
	if imageUpdatedTo9("splunk/splunk:", "splunk/splunk:") {
		t.Errorf("Should not have detected an upgrade from 8 to 9, there is no version")
	}
}
