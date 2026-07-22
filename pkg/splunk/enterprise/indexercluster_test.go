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
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"errors"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"

	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/splunk/splunk-operator/pkg/logging"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
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

	// Initial run: missing ClusterManagerRef — stalled spec validation failure, returns terminal error (no requeue)
	_, err := ApplyIndexerCluster(ctx, c, &idxCr)
	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Errorf("stalled spec validation failure should return a terminal error, got %v", err)
	}

	rerr := errors.New(splcommon.Rerr)
	c.InduceErrorKind[splcommon.MockClientInduceErrorGet] = rerr
	_, err = ApplyIndexerCluster(ctx, c, &idxCr)
	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Errorf("stalled spec validation failure should return a terminal error, got %v", err)
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
	// GarbageCollectConfigMaps / GarbageCollectSecrets scope their List by the CR-ownership
	// labels server-side, so their ListOpts carry a MatchingLabels selector.
	listOpts2 := []client.ListOption{
		client.InNamespace("test"),
		client.MatchingLabels{resources.LabelCRName: "stack1", resources.LabelCRKind: "IndexerCluster"},
	}
	listmockCall := []spltest.MockFuncCall{
		{ListOpts: listOpts},
		{ListOpts: listOpts1},
		{ListOpts: listOpts2},
		{ListOpts: listOpts2},
	}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0], funcCalls[3], funcCalls[5], funcCalls[6], funcCalls[10], funcCalls[12]}, "Update": {funcCalls[0]}, "List": {listmockCall[0], listmockCall[1], listmockCall[2], listmockCall[3]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": updateFuncCalls, "List": {listmockCall[0], listmockCall[1], listmockCall[2], listmockCall[3]}}

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
	reconcileFn := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyIndexerClusterManager(context.TODO(), c, cr.(*enterpriseApi.IndexerCluster))
		return err
	}
	clusterManagerInitObj := &enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "manager1",
			Namespace: "test",
		},
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyIndexerClusterManager", &current, revised, createCalls, updateCalls, reconcileFn, true, clusterManagerInitObj)

	// // test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
		_, err := ApplyIndexerClusterManager(context.TODO(), c, cr.(*enterpriseApi.IndexerCluster))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)

	// Negative testing: GET error causes ApplySplunkConfig to fail (non-terminal error —
	// spec validation passes because ValidateImagePullSecrets only calls GET when ImagePullSecrets
	// are configured, which they are not here)
	ctx := context.TODO()
	c := spltest.NewMockClient()
	rerr := errors.New(splcommon.Rerr)
	c.InduceErrorKind[splcommon.MockClientInduceErrorGet] = rerr
	_, err := ApplyIndexerClusterManager(ctx, c, &current)
	if err == nil {
		t.Errorf("expected non-nil error when ApplySplunkConfig fails due to GET error")
	}

	// Terminal spec validation: missing ClusterManagerRef causes validateIndexerClusterSpec to
	// return an error → reconciler returns nil (stalled pattern) and sets Stalled condition
	noRefCR := current.DeepCopy()
	noRefCR.Spec.ClusterManagerRef = corev1.ObjectReference{}
	noRefCR.Spec.ClusterMasterRef = corev1.ObjectReference{}
	_, err = ApplyIndexerClusterManager(ctx, spltest.NewMockClient(), noRefCR)
	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Errorf("stalled spec validation failure should return a terminal error, got %v", err)
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

	logger := logging.FromContext(context.TODO()).With("func", "TestGetMonitoringConsoleClient", "name", "stack1", "namespace", "test")

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
		log:     logger,
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
	logger := logging.FromContext(ctx).With("func", "TestGetClusterManagerClient", "name", "stack1", "namespace", "test")

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
		log:     logger,
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
	logger := logging.FromContext(context.TODO()).With("func", method, "name", "stack1", "namespace", "test")

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
		log:     logger,
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

	mockHandlers[1].Body = loadFixture(t, "update_status_invalid_response1.json")

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
			Body:   loadFixture(t, "invalid_peer_status_in_scale_down_info.json"),
		},
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   loadFixture(t, "invalid_peer_status_in_scale_down_peer.json"),
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
			Body:   loadFixture(t, "invalid_peer_in_finish_recycle_info.json"),
		},
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   loadFixture(t, "invalid_peer_in_finish_recycle_peer.json"),
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
			Body:   loadFixture(t, "indexer_cluster_pod_manager_info.json"),
		},
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   loadFixture(t, "indexer_cluster_pod_manager_peer.json"),
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
	pod.ObjectMeta.Name = "splunk-stack1-2"
	replicas = 2
	statefulSet.Status.Replicas = 2
	statefulSet.Status.ReadyReplicas = 2
	statefulSet.Status.UpdatedReplicas = 2
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[4], funcCalls[4], funcCalls[0]}, "Create": {funcCalls[1]}}
	method = "indexerClusterPodManager.Update(Pod Not Found)"
	indexerClusterPodManagerUpdateTester(t, method, mockHandlers, 1, enterpriseApi.PhaseScalingDown, statefulSet, wantCalls, nil, statefulSet, pod)

	// test scale down => decommission pod
	mockHandlers[1].Body = loadFixture(t, "configmap_indexer_smartstore.json")
	mockHandlers = append(mockHandlers, spltest.MockHTTPHandler{
		Method: "POST",
		URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/control/control/remove_peers?peers=D39B1729-E2C5-4273-B9B2-534DA7C2F866",
		Status: 200,
		Err:    nil,
		Body:   ``,
	})
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
		{MetaName: "*v1.StatefulSet-test-splunk-stack1"},
		{MetaName: "*v1.Pod-test-splunk-manager1-cluster-manager-0"},
		{MetaName: "*v1.PersistentVolumeClaim-test-pvc-etc-splunk-stack1-1"},
		{MetaName: "*v1.PersistentVolumeClaim-test-pvc-var-splunk-stack1-1"},
	}
	wantCalls = map[string][]spltest.MockFuncCall{"Get": decommisionFuncCalls, "Create": {funcCalls[1]}, "Delete": pvcCalls, "Update": {funcCalls[0]}}
	//wantCalls["Get"] = append(wantCalls["Get"], pvcCalls...)
	pvcList := []*corev1.PersistentVolumeClaim{
		{ObjectMeta: metav1.ObjectMeta{Name: "pvc-etc-splunk-stack1-1", Namespace: "test"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "pvc-var-splunk-stack1-1", Namespace: "test"}},
	}
	method = "indexerClusterPodManager.Update(Decommission)"
	pod.ObjectMeta.Name = "splunk-stack1-0"
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
	logger := logging.FromContext(context.TODO()).With("func", method, "name", "stack1", "namespace", "test")

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
		log:     logger,
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
	// Empty ClusterManagerRef is caught in validateIndexerClusterSpec — terminal path returns nil (no requeue)
	cr.Spec.ClusterManagerRef.Name = ""
	_, err := ApplyIndexerClusterManager(context.TODO(), c, &cr)
	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Errorf("stalled spec validation failure should return a terminal error, got %v", err)
	}

	cr.Spec.ClusterManagerRef.Name = "manager1"
	// verifyRFPeers should return err here
	if _, err := ApplyIndexerClusterManager(context.TODO(), c, &cr); err == nil {
		t.Errorf("ApplyIndxerCluster() should have returned error")
	}

	cm.Status.Phase = enterpriseApi.PhaseError
	cr.Spec.CommonSplunkSpec.EtcVolumeStorageConfig.StorageCapacity = "-abcd"
	if _, err := ApplyIndexerClusterManager(context.TODO(), c, &cr); err == nil {
		t.Errorf("ApplyIndxerCluster() should have returned error")
	}
}

func TestGetIndexerStatefulSet(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	queue := enterpriseApi.Queue{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Queue",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "queue",
		},
		Spec: enterpriseApi.QueueSpec{
			Provider: "sqs",
			SQS: enterpriseApi.SQSSpec{
				Name:       "test-queue",
				AuthRegion: "us-west-2",
				Endpoint:   "https://sqs.us-west-2.amazonaws.com",
				DLQ:        "sqs-dlq-test",
			},
		},
	}

	cr := enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.IndexerClusterSpec{
			QueueRef: &corev1.ObjectReference{
				Name: queue.Name,
			},
		},
	}

	ctx := context.TODO()

	c := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Errorf("Failed to create namespace scoped object")
	}
	c.AddObject(&enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "manager1",
			Namespace: "test",
		},
	})

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

	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Errorf("stalled spec validation failure should return a terminal error, got %v", err)
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

	// Mock cluster config endpoint for VerifyRFPeers
	type ClusterInfoEntry struct {
		Content splclient.ClusterInfo `json:"content"`
	}
	clusterInfoResponse := struct {
		Entry []ClusterInfoEntry `json:"entry"`
	}{
		Entry: []ClusterInfoEntry{
			{
				Content: splclient.ClusterInfo{
					MultiSite:             "false",
					ReplicationFactor:     3,
					SiteReplicationFactor: "",
				},
			},
		},
	}
	response3, _ := json.Marshal(clusterInfoResponse)

	response1, _ := json.Marshal(apiResponse1)
	response2, _ := json.Marshal(apiResponse2)
	wantRequest1, _ := http.NewRequest("GET", "https://splunk-test-cluster-manager-service.default.svc.cluster.local:8089/services/cluster/manager/info?count=0&output_mode=json", nil)
	wantRequest2, _ := http.NewRequest("GET", "https://splunk-test-cluster-manager-service.default.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json", nil)
	wantRequest3, _ := http.NewRequest("GET", "https://splunk-test-cluster-manager-service.default.svc.cluster.local:8089/services/cluster/config?count=0&output_mode=json", nil)
	mclient.AddHandler(wantRequest1, 200, string(response1), nil)
	mclient.AddHandler(wantRequest2, 200, string(response2), nil)
	mclient.AddHandler(wantRequest3, 200, string(response3), nil)

	savedGetSpecificSecretTokenFromPod := splutil.GetSpecificSecretTokenFromPodMock
	defer func() { splutil.GetSpecificSecretTokenFromPodMock = savedGetSpecificSecretTokenFromPod }()
	splutil.GetSpecificSecretTokenFromPodMock = func(ctx context.Context, c splcommon.ControllerClient, podName string, namespace string, secretToken string) (string, error) {
		return "dummypassword", nil
	}

	savedNewIndexerClusterPodManager := newIndexerClusterPodManager
	defer func() { newIndexerClusterPodManager = savedNewIndexerClusterPodManager }()
	newIndexerClusterPodManager = func(log *slog.Logger, cr *enterpriseApi.IndexerCluster, secret *corev1.Secret, newSplunkClient NewSplunkClientFunc, c splcommon.ControllerClient) indexerClusterPodManager {
		return indexerClusterPodManager{
			log:     log,
			cr:      cr,
			secrets: secret,
			newSplunkClient: func(managementURI, username, password string) *splclient.SplunkClient {
				sc := splclient.NewSplunkClient(managementURI, username, password)
				sc.Client = mclient
				return sc
			},
			c: c,
		}
	}

	// Initialize GlobalResourceTracker to enable app framework
	initGlobalResourceTracker()

	// create directory for app framework
	newpath := filepath.Join("/tmp", "appframework")
	_ = os.MkdirAll(newpath, os.ModePerm)

	// adding getapplist to fix test case
	savedGetAppsList := GetAppsList
	defer func() { GetAppsList = savedGetAppsList }()
	GetAppsList = func(ctx context.Context, remoteDataClientMgr RemoteDataClientManager) (splcommon.RemoteDataListResponse, error) {
		RemoteDataListResponse := splcommon.RemoteDataListResponse{}
		return RemoteDataListResponse, nil
	}

	// Mock GetPodExecClient to return a mock client that simulates pod operations locally
	savedGetPodExecClient := splutil.GetPodExecClient
	splutil.GetPodExecClient = func(client splcommon.ControllerClient, cr splcommon.MetaObject, targetPodName string) splutil.PodExecClientImpl {
		mockClient := &spltest.MockPodExecClient{
			Client:        client,
			Cr:            cr,
			TargetPodName: targetPodName,
		}
		// Add mock responses for common commands
		ctx := context.TODO()
		// Mock mkdir command (used by createDirOnSplunkPods)
		mockClient.AddMockPodExecReturnContext(ctx, "mkdir -p", &spltest.MockPodExecReturnContext{
			StdOut: "",
			StdErr: "",
			Err:    nil,
		})
		return mockClient
	}
	defer func() { splutil.GetPodExecClient = savedGetPodExecClient }()

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

	// simulate create clustermanager instance before reconciliation
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
	_, err = ApplyClusterManager(ctx, c, clustermanager, nil)
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
	_, err = ApplyClusterManager(ctx, c, clustermanager, nil)
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
			Selector: &metav1.LabelSelector{
				MatchLabels: getSplunkLabels("test", SplunkIndexer, "test"),
			},
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

	// simulate create clustermanager instance before reconciliation
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

func buildFormBody(pairs [][]string) string {
	var b strings.Builder
	for i, kv := range pairs {
		if len(kv) < 2 {
			continue
		}
		fmt.Fprintf(&b, "%s=%s", kv[0], kv[1])
		if i < len(pairs)-1 {
			b.WriteByte('&')
		}
	}
	return b.String()
}

func TestPasswordSyncCompleted(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	builder := newFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.ClusterManager{}).
		WithStatusSubresource(&enterpriseApi.IndexerCluster{})

	client := builder.Build()
	ctx := context.TODO()

	// Create a mock event recorder to capture events
	recorder := &mockEventRecorder{events: []mockEvent{}}
	eventPublisher := &K8EventPublisher{recorder: recorder}

	cm := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm",
			Namespace: "test",
		},
	}
	cm.SetGroupVersionKind(enterpriseApi.GroupVersion.WithKind("ClusterManager"))

	err := client.Create(ctx, &cm)
	if err != nil {
		t.Fatalf("Failed to create ClusterManager: %v", err)
	}

	idxc := enterpriseApi.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "idxc",
			Namespace: cm.GetNamespace(),
		},
		Spec: enterpriseApi.IndexerClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				ClusterManagerRef: corev1.ObjectReference{
					Name: cm.GetName(),
				},
			},
		},
	}
	idxc.SetGroupVersionKind(enterpriseApi.GroupVersion.WithKind("IndexerCluster"))

	err = client.Create(ctx, &idxc)
	if err != nil {
		t.Fatalf("Failed to create IndexerCluster: %v", err)
	}

	// Create namespace scoped secret so ApplyIdxcSecret has something to work with
	nsSecret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, cm.GetNamespace())
	if err != nil {
		t.Fatalf("Failed to apply namespace scoped secret: %v", err)
	}

	// Set CR status resource version to a stale value so ApplyIdxcSecret does not early-return
	idxc.Status.NamespaceSecretResourceVersion = nsSecret.ResourceVersion + "-old"

	// Initialize a minimal pod manager for ApplyIdxcSecret
	mgr := &indexerClusterPodManager{
		c:   client,
		log: logging.FromContext(ctx).With("func", "TestPasswordSyncCompleted", "name", idxc.GetName(), "namespace", idxc.GetNamespace()),
		cr:  &idxc,
	}

	// Use a mock PodExec client; replicas will be 0 so it won't be exercised
	var mockPodExecClient *spltest.MockPodExecClient = &spltest.MockPodExecClient{}

	// Add event publisher to context so ApplyIdxcSecret can emit events
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	// Call ApplyIdxcSecret; with 0 replicas it will complete without touching pods,
	// but still emit the PasswordSyncCompleted event
	err = ApplyIdxcSecret(ctx, mgr, 0, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply idxc secret %s", err.Error())
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

func TestClusterQuorumRestoredClusterInitialized(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	builder := newFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.ClusterManager{}).
		WithStatusSubresource(&enterpriseApi.IndexerCluster{})

	client := builder.Build()
	ctx := context.TODO()

	// Create a mock event recorder to capture events
	recorder := &mockEventRecorder{events: []mockEvent{}}
	eventPublisher := &K8EventPublisher{recorder: recorder}

	cm := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "manager1",
			Namespace: "test",
		},
	}
	cm.SetGroupVersionKind(enterpriseApi.GroupVersion.WithKind("ClusterManager"))

	err := client.Create(ctx, &cm)
	if err != nil {
		t.Fatalf("Failed to create ClusterManager: %v", err)
	}

	idxc := enterpriseApi.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "idxc",
			Namespace: cm.GetNamespace(),
		},
		Spec: enterpriseApi.IndexerClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				ClusterManagerRef: corev1.ObjectReference{
					Name: cm.GetName(),
				},
			},
		},
	}
	idxc.SetGroupVersionKind(enterpriseApi.GroupVersion.WithKind("IndexerCluster"))

	err = client.Create(ctx, &idxc)
	if err != nil {
		t.Fatalf("Failed to create IndexerCluster: %v", err)
	}

	// Build mock HTTP handlers for a healthy cluster manager info/peers response
	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/info?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   loadFixture(t, "indexer_cluster_pod_manager_info.json"),
		},
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   loadFixture(t, "indexer_cluster_pod_manager_peer.json"),
		},
	}

	// Create mock Splunk client and indexerClusterPodManager using existing helper
	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)

	mgr := getIndexerClusterPodManager("TestClusterQuorumRestoredClusterInitialized", mockHandlers, mockSplunkClient, 3)
	replicas := int32(3)
	ss := &appsv1.StatefulSet{
		Status: appsv1.StatefulSetStatus{
			Replicas:      replicas,
			ReadyReplicas: replicas,
		},
	}

	// Wire a mock k8s client and event publisher into context
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	// Use a mock k8s client as in other updateStatus tests
	c := spltest.NewMockClient()
	mgr.c = c

	// Ensure initial status is not indexing ready so we see a transition
	mgr.cr.Status.IndexingReady = false

	// Call updateStatus, which should transition to indexing ready and emit the event
	err = mgr.updateStatus(ctx, ss)
	if err != nil {
		t.Fatalf("updateStatus returned unexpected error: %v", err)
	}

	// Check that both ClusterInitialized and ClusterQuorumRestored events were published
	clusterInitialized := false
	quorumRestored := false
	for _, event := range recorder.events {
		if event.reason == "ClusterInitialized" {
			clusterInitialized = true
			if event.eventType != corev1.EventTypeNormal {
				t.Errorf("Expected Normal event type for ClusterInitialized, got %s", event.eventType)
			}
			if quorumRestored {
				break
			}
		}
		if event.reason == "ClusterQuorumRestored" {
			quorumRestored = true
			if event.eventType != corev1.EventTypeNormal {
				t.Errorf("Expected Normal event type for ClusterQuorumRestored, got %s", event.eventType)
			}
			if clusterInitialized {
				break
			}
		}
	}
	if !clusterInitialized {
		t.Errorf("Expected ClusterInitialized event to be published")
	}
	if !quorumRestored {
		t.Errorf("Expected ClusterQuorumRestored event to be published")
	}
}

func TestClusterQuorumLostEvent(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	builder := newFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.ClusterManager{}).
		WithStatusSubresource(&enterpriseApi.IndexerCluster{})

	client := builder.Build()
	ctx := context.TODO()

	recorder := &mockEventRecorder{events: []mockEvent{}}
	eventPublisher := &K8EventPublisher{recorder: recorder}

	cm := enterpriseApi.ClusterManager{
		TypeMeta:   metav1.TypeMeta{Kind: "ClusterManager"},
		ObjectMeta: metav1.ObjectMeta{Name: "manager1", Namespace: "test"},
	}
	cm.SetGroupVersionKind(enterpriseApi.GroupVersion.WithKind("ClusterManager"))
	if err := client.Create(ctx, &cm); err != nil {
		t.Fatalf("Failed to create ClusterManager: %v", err)
	}

	idxc := enterpriseApi.IndexerCluster{
		TypeMeta:   metav1.TypeMeta{Kind: "IndexerCluster"},
		ObjectMeta: metav1.ObjectMeta{Name: "idxc", Namespace: cm.GetNamespace()},
		Spec: enterpriseApi.IndexerClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				ClusterManagerRef: corev1.ObjectReference{Name: cm.GetName()},
			},
		},
	}
	idxc.SetGroupVersionKind(enterpriseApi.GroupVersion.WithKind("IndexerCluster"))
	if err := client.Create(ctx, &idxc); err != nil {
		t.Fatalf("Failed to create IndexerCluster: %v", err)
	}

	// First call: set initial state to indexing ready using healthy cluster response
	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/info?count=0&output_mode=json",
			Status: 200,
			Body:   loadFixture(t, "indexer_cluster_pod_manager_info.json"),
		},
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json",
			Status: 200,
			Body:   loadFixture(t, "indexer_cluster_pod_manager_peer.json"),
		},
	}
	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)

	mgr := getIndexerClusterPodManager("TestClusterQuorumLostEvent", mockHandlers, mockSplunkClient, 3)
	replicas := int32(3)
	ss := &appsv1.StatefulSet{
		Status: appsv1.StatefulSetStatus{Replicas: replicas, ReadyReplicas: replicas},
	}

	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)
	c := spltest.NewMockClient()
	mgr.c = c

	mgr.cr.Status.IndexingReady = false
	mgr.cr.Status.Initialized = false
	err := mgr.updateStatus(ctx, ss)
	if err != nil {
		t.Fatalf("First updateStatus returned unexpected error: %v", err)
	}
	if !mgr.cr.Status.IndexingReady {
		t.Fatal("Expected IndexingReady to be true after first updateStatus")
	}

	// Reset recorder and prepare second call with indexing_ready=false
	recorder.events = []mockEvent{}
	quorumLostInfo := loadFixture(t, "quorum_lost_info.json")
	quorumLostHandlers := []spltest.MockHTTPHandler{
		{Method: "GET", URL: "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/info?count=0&output_mode=json", Status: 200, Body: quorumLostInfo},
		{Method: "GET", URL: "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json", Status: 200, Body: loadFixture(t, "indexer_cluster_pod_manager_peer.json")},
	}
	mockSplunkClient2 := &spltest.MockHTTPClient{}
	mockSplunkClient2.AddHandlers(quorumLostHandlers...)
	mgr.newSplunkClient = func(managementURI, username, password string) *splclient.SplunkClient {
		sc := splclient.NewSplunkClient(managementURI, username, password)
		sc.Client = mockSplunkClient2
		return sc
	}

	err = mgr.updateStatus(ctx, ss)
	if err != nil {
		t.Fatalf("Second updateStatus returned unexpected error: %v", err)
	}

	found := false
	for _, event := range recorder.events {
		if event.reason == "ClusterQuorumLost" {
			found = true
			if event.eventType != corev1.EventTypeWarning {
				t.Errorf("Expected Warning event type for ClusterQuorumLost, got %s", event.eventType)
			}
			if !strings.Contains(event.message, "quorum") {
				t.Errorf("Expected event message to mention quorum, got: %s", event.message)
			}
			break
		}
	}
	if !found {
		t.Errorf("Expected ClusterQuorumLost event to be published")
	}
}

func TestScalingBlockedRFEvent(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	ctx := context.TODO()
	recorder := &mockEventRecorder{events: []mockEvent{}}
	eventPublisher := &K8EventPublisher{recorder: recorder}
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	// Use the same fixture and URL as TestVerifyRFPeers
	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    "https://splunk-manager1-cluster-manager-service.test.svc.cluster.local:8089/services/cluster/config?count=0&output_mode=json",
			Status: 200,
			Body:   loadFixture(t, "service_stack1_indexer_service.json"),
		},
	}
	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)

	// replicas=1 which is less than RF=3 in the fixture
	mgr := getIndexerClusterPodManager("TestScalingBlockedRFEvent", mockHandlers, mockSplunkClient, 1)

	// Use spltest.NewMockClient which handles the Get call for the CM pod
	c := spltest.NewMockClient()
	err := mgr.verifyRFPeers(ctx, c)
	if err != nil {
		t.Fatalf("verifyRFPeers returned unexpected error: %v", err)
	}

	found := false
	for _, event := range recorder.events {
		if event.reason == "ScalingBlockedRF" {
			found = true
			if event.eventType != corev1.EventTypeWarning {
				t.Errorf("Expected Warning event type for ScalingBlockedRF, got %s", event.eventType)
			}
			if !strings.Contains(event.message, "replication factor") {
				t.Errorf("Expected event message to mention replication factor, got: %s", event.message)
			}
			break
		}
	}
	if !found {
		t.Errorf("Expected ScalingBlockedRF event to be published")
	}
	if mgr.cr.Spec.Replicas == 1 {
		t.Errorf("Expected replicas to be adjusted from 1 to replication factor")
	}
}

func TestIdxcScaledUpScaledDownEvent(t *testing.T) {
	ctx := context.TODO()
	recorder := &mockEventRecorder{events: []mockEvent{}}
	eventPublisher := &K8EventPublisher{recorder: recorder}
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	crName := "test-idxc"
	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: crName, Namespace: "test"},
	}

	// Simulate ScaledUp: previousReplicas=1, desiredReplicas=3, phase=PhaseReady, Status.Replicas=3
	previousReplicas := int32(1)
	desiredReplicas := int32(3)
	cr.Status.Replicas = desiredReplicas
	phase := enterpriseApi.PhaseReady

	// Replicate the production conditional from indexerClusterPodManager.Update()
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
			if !strings.Contains(event.message, "3") {
				t.Errorf("Expected event message to contain replica counts, got: %s", event.message)
			}
			break
		}
	}
	if !found {
		t.Errorf("Expected ScaledUp event to be published")
	}

	// Simulate ScaledDown: previousReplicas=3, desiredReplicas=1, phase=PhaseReady, Status.Replicas=1
	recorder.events = []mockEvent{}
	previousReplicas = int32(3)
	desiredReplicas = int32(1)
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

	// Negative: no event when replicas haven't converged
	recorder.events = []mockEvent{}
	phase = enterpriseApi.PhaseReady
	cr.Status.Replicas = int32(2) // not yet at desiredReplicas
	if phase == enterpriseApi.PhaseReady {
		if desiredReplicas < previousReplicas && cr.Status.Replicas == desiredReplicas {
			ep.Normal(ctx, "ScaledDown",
				fmt.Sprintf("Successfully scaled %s down to %d replicas", cr.GetName(), desiredReplicas))
		}
	}
	if len(recorder.events) != 0 {
		t.Errorf("Expected no events when replicas haven't converged, got %d events", len(recorder.events))
	}
}

func TestIdxcPasswordSyncFailedEvent(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	builder := newFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.ClusterManager{}).
		WithStatusSubresource(&enterpriseApi.IndexerCluster{})

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

	idxc := enterpriseApi.IndexerCluster{
		TypeMeta:   metav1.TypeMeta{Kind: "IndexerCluster"},
		ObjectMeta: metav1.ObjectMeta{Name: "idxc", Namespace: "test"},
		Spec: enterpriseApi.IndexerClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
			},
		},
	}
	idxc.SetGroupVersionKind(enterpriseApi.GroupVersion.WithKind("IndexerCluster"))
	// Set stale resource version so ApplyIdxcSecret doesn't early-return
	idxc.Status.NamespaceSecretResourceVersion = nsSecret.ResourceVersion + "-old"
	// Pre-set MaintenanceMode to skip the maintenance mode setup path
	idxc.Status.MaintenanceMode = true
	idxc.Status.IdxcPasswordChangedSecrets = make(map[string]bool)

	// Create the indexer pod with a secret volume mount
	podSecretName := "splunk-idxc-indexer-secret-v1"
	indexerPodName := "splunk-idxc-indexer-0"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: indexerPodName, Namespace: "test"},
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

	// Create the pod's secret with a DIFFERENT idxc_secret than namespace secret
	podSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: podSecretName, Namespace: "test"},
		Data: map[string][]byte{
			"password":    []byte("admin-password"),
			"idxc_secret": []byte("old-idxc-secret"),
		},
	}
	if err := c.Create(ctx, podSecret); err != nil {
		t.Fatalf("Failed to create pod secret: %v", err)
	}

	// Create a mock HTTP client that returns an error on SetIdxcSecret POST
	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(spltest.MockHTTPHandler{
		Method: "POST",
		URL:    fmt.Sprintf("https://splunk-idxc-indexer-0.splunk-idxc-indexer-headless.test.svc.cluster.local:8089/services/cluster/config/config?secret=%s", string(nsSecret.Data["idxc_secret"])),
		Status: 500,
		Err:    fmt.Errorf("mock SetIdxcSecret failure"),
	})

	mgr := &indexerClusterPodManager{
		c:   c,
		log: logging.FromContext(ctx).With("func", "TestIdxcPasswordSyncFailedEvent", "name", idxc.GetName(), "namespace", idxc.GetNamespace()),
		cr:  &idxc,
		newSplunkClient: func(managementURI, username, password string) *splclient.SplunkClient {
			sc := splclient.NewSplunkClient(managementURI, username, password)
			sc.Client = mockSplunkClient
			return sc
		},
	}

	mockPodExecClient := &spltest.MockPodExecClient{}

	// Call ApplyIdxcSecret — should fail at SetIdxcSecret and emit PasswordSyncFailed
	err = ApplyIdxcSecret(ctx, mgr, 1, mockPodExecClient)
	if err == nil {
		t.Errorf("Expected error from ApplyIdxcSecret when SetIdxcSecret fails")
	}

	found := false
	for _, event := range recorder.events {
		if event.reason == "PasswordSyncFailed" {
			found = true
			if event.eventType != corev1.EventTypeWarning {
				t.Errorf("Expected Warning event type for PasswordSyncFailed, got %s", event.eventType)
			}
			if !strings.Contains(event.message, indexerPodName) {
				t.Errorf("Expected event message to contain pod name '%s', got: %s", indexerPodName, event.message)
			}
			break
		}
	}
	if !found {
		t.Errorf("Expected PasswordSyncFailed event to be published")
	}
}

// mockEvent stores event details for testing
type mockEvent struct {
	eventType string
	reason    string
	message   string
}

// mockEventRecorder implements record.EventRecorder for testing
type mockEventRecorder struct {
	events []mockEvent
}

func (m *mockEventRecorder) Event(object pkgruntime.Object, eventType, reason, message string) {
	m.events = append(m.events, mockEvent{eventType: eventType, reason: reason, message: message})
}

func (m *mockEventRecorder) Eventf(object pkgruntime.Object, eventType, reason, messageFmt string, args ...interface{}) {
	m.events = append(m.events, mockEvent{eventType: eventType, reason: reason, message: fmt.Sprintf(messageFmt, args...)})
}

func (m *mockEventRecorder) AnnotatedEventf(object pkgruntime.Object, annotations map[string]string, eventType, reason, messageFmt string, args ...interface{}) {
	m.events = append(m.events, mockEvent{eventType: eventType, reason: reason, message: fmt.Sprintf(messageFmt, args...)})
}

// --- Declarative SmartBus credential delivery -------------------------------
//
// SmartBus queue/pipeline config (structural) is delivered through a
// content-addressed ConfigMap, and the static access_key/secret_key credentials
// through a separate content-addressed Secret. Both are mounted and joined into
// SPLUNK_DEFAULTS_URL, so a change to either produces a new resource name and the
// StatefulSet update path rolls the pods. The tests below assert that declarative
// behavior, replacing the old imperative REST/restart path.

// newQueueOSFixture creates a Queue, ObjectStorage, and the referenced credentials
// Secret in the fake client, returning them for use by the reconciler tests.
func newQueueOSFixture(t *testing.T, ctx context.Context, c client.Client, queueName, credsSecretName string) (*enterpriseApi.Queue, *enterpriseApi.ObjectStorage) {
	t.Helper()

	credsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: credsSecretName, Namespace: "test"},
		Data: map[string][]byte{
			"s3_access_key": []byte("AKIAEXAMPLE"),
			"s3_secret_key": []byte("shhh-secret"),
		},
	}
	require.NoError(t, c.Create(ctx, credsSecret))

	queue := &enterpriseApi.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: queueName, Namespace: "test"},
		Spec: enterpriseApi.QueueSpec{
			Provider: "sqs",
			SQS: enterpriseApi.SQSSpec{
				Name:       "test-queue",
				AuthRegion: "us-west-2",
				Endpoint:   "https://sqs.us-west-2.amazonaws.com",
				DLQ:        "sqs-dlq-test",
				VolList:    []enterpriseApi.SQSVolumeSpec{{SecretRef: credsSecretName}},
			},
		},
	}
	require.NoError(t, c.Create(ctx, queue))

	os := &enterpriseApi.ObjectStorage{
		ObjectMeta: metav1.ObjectMeta{Name: "os", Namespace: "test"},
		Spec: enterpriseApi.ObjectStorageSpec{
			Provider: "s3",
			S3: enterpriseApi.S3Spec{
				Endpoint: "https://s3.us-west-2.amazonaws.com",
				Path:     "bucket/key",
			},
		},
	}
	require.NoError(t, c.Create(ctx, os))

	return queue, os
}

// listCredsSecrets returns the SOK credentials Secrets owned by the given IndexerCluster.
func listCredsSecrets(t *testing.T, ctx context.Context, c client.Client, crName string) []corev1.Secret {
	t.Helper()
	var all corev1.SecretList
	require.NoError(t, c.List(ctx, &all, client.InNamespace("test")))
	var owned []corev1.Secret
	for _, s := range all.Items {
		if s.Labels[resources.LabelCRKind] == "IndexerCluster" &&
			s.Labels[resources.LabelCRName] == crName {
			owned = append(owned, s)
		}
	}
	return owned
}

// TestEnsureIndexerCredentialsSecret_CreatesMountsAndRotates exercises the declarative
// credentials path directly: a queueRef with a static credentials secret yields a
// content-addressed Secret that mounts into the StatefulSet and is joined into
// SPLUNK_DEFAULTS_URL; rotating the source credentials yields a new Secret name.
func TestEnsureIndexerCredentialsSecret_CreatesMountsAndRotates(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(appsv1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))
	c := newFakeClientBuilder(sch).Build()

	queue, os := newQueueOSFixture(t, ctx, c, "queue", "queue-secrets")

	cr := &enterpriseApi.IndexerCluster{
		// Kind mirrors the reconciler, which sets cr.Kind before calling
		// ensureIndexerDefaults; the defaults resource names embed it.
		TypeMeta:   metav1.TypeMeta{Kind: "IndexerCluster"},
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas:         3,
			QueueRef:         &corev1.ObjectReference{Name: queue.Name},
			ObjectStorageRef: &corev1.ObjectReference{Name: os.Name},
		},
	}

	// A queueRef with static credentials produces a non-empty, content-addressed Secret.
	_, credsSecret, err := ensureIndexerDefaults(ctx, c, cr)
	require.NoError(t, err)
	require.NotEmpty(t, credsSecret.Name, "credentials Secret should be created when static creds are present")
	assert.Regexp(t, regexp.MustCompile(`^sok-indexercluster-creds-[0-9a-f]{6}$`), credsSecret.Name)

	var stored corev1.Secret
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: "test", Name: credsSecret.Name}, &stored))
	require.NotNil(t, stored.Immutable)
	assert.True(t, *stored.Immutable, "credentials Secret must be immutable")

	// The Secret mounts into the pod and joins SPLUNK_DEFAULTS_URL.
	ss := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "splunk"}}},
			},
		},
	}
	credsSecret.AsStatefulSetOption()(ss)
	require.Len(t, ss.Spec.Template.Spec.Volumes, 1)
	require.NotNil(t, ss.Spec.Template.Spec.Volumes[0].Secret)
	assert.Equal(t, credsSecret.Name, ss.Spec.Template.Spec.Volumes[0].Secret.SecretName)
	require.Len(t, ss.Spec.Template.Spec.Containers[0].VolumeMounts, 1)

	var defaultsURL string
	for _, e := range ss.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "SPLUNK_DEFAULTS_URL" {
			defaultsURL = e.Value
		}
	}
	assert.Contains(t, defaultsURL, resources.SecretMountPath(), "creds mount path must be joined into SPLUNK_DEFAULTS_URL")

	// Rotating the source credentials produces a different Secret name (rolls pods).
	rotated := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: "test", Name: "queue-secrets"}, rotated))
	rotated.Data["s3_secret_key"] = []byte("rotated-secret")
	require.NoError(t, c.Update(ctx, rotated))

	_, rotatedSecret, err := ensureIndexerDefaults(ctx, c, cr)
	require.NoError(t, err)
	assert.NotEqual(t, credsSecret.Name, rotatedSecret.Name, "rotated credentials must produce a new Secret name")
}

// TestEnsureIndexerCredentialsSecret_NoQueueRef verifies no Secret is produced when
// SmartBus is not configured.
func TestEnsureIndexerCredentialsSecret_NoQueueRef(t *testing.T) {
	ctx := context.TODO()

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))
	c := newFakeClientBuilder(sch).Build()

	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
		Spec:       enterpriseApi.IndexerClusterSpec{Replicas: 1},
	}

	_, credsSecret, err := ensureIndexerDefaults(ctx, c, cr)
	require.NoError(t, err)
	assert.Empty(t, credsSecret.Name, "no queueRef → no credentials Secret")
}

// TestEnsureIndexerCredentialsSecret_IRSAProducesNoStaticCreds verifies that when the Queue
// has no VolList (IRSA / workload identity), ResolveQueueAndObjectStorage leaves the keys
// empty and no static-credential Secret is produced.
func TestEnsureIndexerCredentialsSecret_IRSAProducesNoStaticCreds(t *testing.T) {
	ctx := context.TODO()

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(appsv1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))
	c := newFakeClientBuilder(sch).Build()

	// Queue with no VolList — simulates IRSA / workload identity where no static creds exist.
	irsaQueue := &enterpriseApi.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "irsa-queue", Namespace: "test"},
		Spec: enterpriseApi.QueueSpec{
			Provider: "sqs",
			SQS: enterpriseApi.SQSSpec{
				Name:       "test-queue",
				AuthRegion: "us-west-2",
				Endpoint:   "https://sqs.us-west-2.amazonaws.com",
				DLQ:        "sqs-dlq-test",
				// VolList intentionally empty — IRSA uses pod identity, not static creds.
			},
		},
	}
	require.NoError(t, c.Create(ctx, irsaQueue))

	objStorage := &enterpriseApi.ObjectStorage{
		ObjectMeta: metav1.ObjectMeta{Name: "irsa-os", Namespace: "test"},
		Spec: enterpriseApi.ObjectStorageSpec{
			Provider: "s3",
			S3: enterpriseApi.S3Spec{
				Endpoint: "https://s3.us-west-2.amazonaws.com",
				Path:     "bucket/key",
			},
		},
	}
	require.NoError(t, c.Create(ctx, objStorage))

	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test"},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas: 1,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				ServiceAccount: "irsa-sa",
			},
			QueueRef:         &corev1.ObjectReference{Name: irsaQueue.Name},
			ObjectStorageRef: &corev1.ObjectReference{Name: objStorage.Name},
		},
	}

	_, credsSecret, err := ensureIndexerDefaults(ctx, c, cr)
	require.NoError(t, err)
	assert.Empty(t, credsSecret.Name, "no VolList → no static credentials Secret")
}

// TestApplyIndexerClusterManager_QueueCredsSecretLifecycle drives the full manager
// reconciler and asserts that (1) a credentials Secret is created and mounted on the
// indexer StatefulSet, and (2) rotating the source credentials creates a new Secret and
// garbage-collects the stale one — the declarative replacement for the old
// QueueConfigUpdated/IndexersRestarted imperative path.
func TestApplyIndexerClusterManager_QueueCredsSecretLifecycle(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	ctx := context.TODO()
	recorder := &mockEventRecorder{events: []mockEvent{}}
	eventPublisher := &K8EventPublisher{recorder: recorder}
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	oldVerifyRFPeers := VerifyRFPeers
	defer func() { VerifyRFPeers = oldVerifyRFPeers }()
	VerifyRFPeers = func(ctx context.Context, mgr indexerClusterPodManager, client splcommon.ControllerClient) error {
		return nil
	}

	oldGetCMInfo := GetClusterManagerInfoCall
	oldGetCMPeers := GetClusterManagerPeersCall
	defer func() {
		GetClusterManagerInfoCall = oldGetCMInfo
		GetClusterManagerPeersCall = oldGetCMPeers
	}()
	GetClusterManagerInfoCall = func(ctx context.Context, mgr *indexerClusterPodManager) (*splclient.ClusterManagerInfo, error) {
		return &splclient.ClusterManagerInfo{Initialized: true, IndexingReady: true, ServiceReady: true, MaintenanceMode: false}, nil
	}
	GetClusterManagerPeersCall = func(ctx context.Context, mgr *indexerClusterPodManager) (map[string]splclient.ClusterManagerPeerInfo, error) {
		peers := map[string]splclient.ClusterManagerPeerInfo{}
		for i := int32(0); i < 3; i++ {
			peerName := GetSplunkStatefulsetPodName(SplunkIndexer, mgr.cr.GetName(), i)
			peers[peerName] = splclient.ClusterManagerPeerInfo{ID: fmt.Sprintf("peer-%d", i), Status: "Up", Searchable: true}
		}
		return peers, nil
	}

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(appsv1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	c := newFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.ClusterManager{}).
		WithStatusSubresource(&enterpriseApi.IndexerCluster{}).
		Build()

	probeConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-test-probe-configmap", Namespace: "test"},
	}
	require.NoError(t, c.Create(ctx, probeConfigMap))

	cm := &enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{Name: "manager1", Namespace: "test"},
		Status:     enterpriseApi.ClusterManagerStatus{Phase: enterpriseApi.PhaseReady},
	}
	require.NoError(t, c.Create(ctx, cm))
	require.NoError(t, c.Status().Update(ctx, cm))

	queue, os := newQueueOSFixture(t, ctx, c, "queue", "queue-secrets")

	passwordSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test-secrets", Namespace: "test"},
		Data:       map[string][]byte{"password": []byte("dummy")},
	}
	require.NoError(t, c.Create(ctx, passwordSecret))

	cmPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-manager1-cluster-manager-0", Namespace: "test"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:latest"}},
			Volumes: []corev1.Volume{
				{Name: "mnt-splunk-secrets", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "test-secrets"}}},
			},
		},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{Ready: true}},
		},
	}
	require.NoError(t, c.Create(ctx, cmPod))

	cmReplicas := int32(1)
	cmSts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: GetSplunkStatefulsetName(SplunkClusterManager, "manager1"), Namespace: "test"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &cmReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:latest"}}},
			},
		},
	}
	require.NoError(t, c.Create(ctx, cmSts))

	crName := "stack1"
	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: crName, Namespace: "test"},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas: 3,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock:              true,
				ClusterManagerRef: corev1.ObjectReference{Name: "manager1"},
			},
			QueueRef:         &corev1.ObjectReference{Name: queue.Name},
			ObjectStorageRef: &corev1.ObjectReference{Name: os.Name},
		},
		Status: enterpriseApi.IndexerClusterStatus{ReadyReplicas: 0},
	}
	require.NoError(t, c.Create(ctx, cr))

	threeReplicas := int32(3)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(SplunkIndexer, cr.GetName()),
			Namespace: cr.GetNamespace(),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &threeReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:latest"}}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			Replicas: threeReplicas, ReadyReplicas: 0,
			CurrentRevision: "v1", UpdateRevision: "v1",
		},
	}
	require.NoError(t, c.Create(ctx, sts))

	basePod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:latest"}},
			Volumes: []corev1.Volume{
				{Name: "mnt-splunk-secrets", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "test-secrets"}}},
			},
		},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{Ready: true}},
		},
	}
	for i := int32(0); i < threeReplicas; i++ {
		pod := basePod.DeepCopy()
		pod.ObjectMeta = metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetPodName(SplunkIndexer, cr.GetName(), i),
			Namespace: cr.GetNamespace(),
			Labels: map[string]string{
				"app.kubernetes.io/instance": GetSplunkStatefulsetName(SplunkIndexer, cr.GetName()),
				"controller-revision-hash":   "v1",
			},
		}
		require.NoError(t, c.Create(ctx, pod))
	}

	// --- Pass 1: reconcile creates the credentials Secret and mounts it ---
	_, err := ApplyIndexerClusterManager(ctx, c, cr)
	require.NoError(t, err)

	credsList := listCredsSecrets(t, ctx, c, crName)
	require.Len(t, credsList, 1, "reconcile must create exactly one credentials Secret")
	firstName := credsList[0].Name
	assert.Regexp(t, regexp.MustCompile(`^sok-indexercluster-creds-[0-9a-f]{6}$`), firstName)

	// The indexer StatefulSet mounts the credentials Secret and joins SPLUNK_DEFAULTS_URL.
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: sts.GetName(), Namespace: sts.GetNamespace()}, sts))
	var mounted bool
	for _, v := range sts.Spec.Template.Spec.Volumes {
		if v.Secret != nil && v.Secret.SecretName == firstName {
			mounted = true
		}
	}
	assert.True(t, mounted, "indexer StatefulSet must mount the credentials Secret")

	var defaultsURL string
	for _, e := range sts.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "SPLUNK_DEFAULTS_URL" {
			defaultsURL = e.Value
		}
	}
	assert.Contains(t, defaultsURL, resources.SecretMountPath(), "SPLUNK_DEFAULTS_URL must include the creds mount path")

	// The declarative path emits no imperative queue-config / restart events.
	for _, event := range recorder.events {
		assert.NotEqual(t, "QueueConfigUpdated", event.reason, "declarative path must not emit QueueConfigUpdated")
		assert.NotEqual(t, "IndexersRestarted", event.reason, "declarative path must not emit IndexersRestarted")
	}

	// --- Pass 2: rotate credentials → new Secret name, stale one garbage-collected ---
	rotated := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: "test", Name: "queue-secrets"}, rotated))
	rotated.Data["s3_secret_key"] = []byte("rotated-secret")
	require.NoError(t, c.Update(ctx, rotated))

	_, err = ApplyIndexerClusterManager(ctx, c, cr)
	require.NoError(t, err)

	credsList = listCredsSecrets(t, ctx, c, crName)
	require.Len(t, credsList, 1, "stale credentials Secret must be garbage-collected after rotation")
	assert.NotEqual(t, firstName, credsList[0].Name, "rotated credentials must produce a new Secret name")
}

// listCredsConfigMaps returns the SOK defaults ConfigMaps owned by the given IndexerCluster.
func listCredsConfigMaps(t *testing.T, ctx context.Context, c client.Client, crName string) []corev1.ConfigMap {
	t.Helper()
	var all corev1.ConfigMapList
	require.NoError(t, c.List(ctx, &all, client.InNamespace("test")))
	var owned []corev1.ConfigMap
	for _, cm := range all.Items {
		if cm.Labels[resources.LabelCRKind] == "IndexerCluster" &&
			cm.Labels[resources.LabelCRName] == crName {
			owned = append(owned, cm)
		}
	}
	return owned
}

// TestIdxcQueueRefChangeRollsPodsDeclarative verifies the declarative replacement for the
// old QueueConfigUpdated/IndexersRestarted imperative path: swapping QueueRef to a
// different queue produces new content-addressed ConfigMap and Secret names (which causes
// Kubernetes to roll pods via the StatefulSet template hash), and GC removes the stale ones.
func TestIdxcQueueRefChangeRollsPodsDeclarative(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	ctx := context.TODO()
	recorder := &mockEventRecorder{events: []mockEvent{}}
	eventPublisher := &K8EventPublisher{recorder: recorder}
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	oldVerifyRFPeers := VerifyRFPeers
	defer func() { VerifyRFPeers = oldVerifyRFPeers }()
	VerifyRFPeers = func(ctx context.Context, mgr indexerClusterPodManager, client splcommon.ControllerClient) error {
		return nil
	}

	oldGetCMInfo := GetClusterManagerInfoCall
	oldGetCMPeers := GetClusterManagerPeersCall
	defer func() {
		GetClusterManagerInfoCall = oldGetCMInfo
		GetClusterManagerPeersCall = oldGetCMPeers
	}()
	GetClusterManagerInfoCall = func(ctx context.Context, mgr *indexerClusterPodManager) (*splclient.ClusterManagerInfo, error) {
		return &splclient.ClusterManagerInfo{Initialized: true, IndexingReady: true, ServiceReady: true, MaintenanceMode: false}, nil
	}
	GetClusterManagerPeersCall = func(ctx context.Context, mgr *indexerClusterPodManager) (map[string]splclient.ClusterManagerPeerInfo, error) {
		peers := map[string]splclient.ClusterManagerPeerInfo{}
		for i := int32(0); i < 3; i++ {
			peerName := GetSplunkStatefulsetPodName(SplunkIndexer, mgr.cr.GetName(), i)
			peers[peerName] = splclient.ClusterManagerPeerInfo{ID: fmt.Sprintf("peer-%d", i), Status: "Up", Searchable: true}
		}
		return peers, nil
	}

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(appsv1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	c := newFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.ClusterManager{}).
		WithStatusSubresource(&enterpriseApi.IndexerCluster{}).
		Build()

	require.NoError(t, c.Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-test-probe-configmap", Namespace: "test"},
	}))

	cm := &enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{Name: "manager1", Namespace: "test"},
		Status:     enterpriseApi.ClusterManagerStatus{Phase: enterpriseApi.PhaseReady},
	}
	require.NoError(t, c.Create(ctx, cm))
	require.NoError(t, c.Status().Update(ctx, cm))

	// Two queues with distinct config so their content-addressed names differ.
	credsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "queue-secrets", Namespace: "test"},
		Data: map[string][]byte{
			"s3_access_key": []byte("AKIAEXAMPLE"),
			"s3_secret_key": []byte("shhh-secret"),
		},
	}
	require.NoError(t, c.Create(ctx, credsSecret))

	queueOld := &enterpriseApi.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "queue-old", Namespace: "test"},
		Spec: enterpriseApi.QueueSpec{
			Provider: "sqs",
			SQS: enterpriseApi.SQSSpec{
				Name: "old-queue", AuthRegion: "us-west-2",
				Endpoint: "https://sqs.us-west-2.amazonaws.com", DLQ: "old-dlq",
				VolList: []enterpriseApi.SQSVolumeSpec{{SecretRef: "queue-secrets"}},
			},
		},
	}
	require.NoError(t, c.Create(ctx, queueOld))

	queueNew := &enterpriseApi.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "queue-new", Namespace: "test"},
		Spec: enterpriseApi.QueueSpec{
			Provider: "sqs",
			SQS: enterpriseApi.SQSSpec{
				Name: "new-queue", AuthRegion: "us-east-1",
				Endpoint: "https://sqs.us-east-1.amazonaws.com", DLQ: "new-dlq",
				VolList: []enterpriseApi.SQSVolumeSpec{{SecretRef: "queue-secrets"}},
			},
		},
	}
	require.NoError(t, c.Create(ctx, queueNew))

	objStorage := &enterpriseApi.ObjectStorage{
		ObjectMeta: metav1.ObjectMeta{Name: "os", Namespace: "test"},
		Spec: enterpriseApi.ObjectStorageSpec{
			Provider: "s3",
			S3:       enterpriseApi.S3Spec{Endpoint: "https://s3.us-west-2.amazonaws.com", Path: "bucket/key"},
		},
	}
	require.NoError(t, c.Create(ctx, objStorage))

	nsSecretName := splcommon.GetNamespaceScopedSecretName("test")
	nsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: nsSecretName, Namespace: "test"},
		Data: map[string][]byte{
			"hec_token":    []byte("ABCDEF01-2345-6789-ABCD-EF0123456789"),
			"password":     []byte("dummyPasswordLongEnough"),
			"pass4SymmKey": []byte("dummyPass4SymmKeyLong"),
			"idxc_secret":  []byte("dummyIdxcSecretLongEn"),
			"shc_secret":   []byte("dummyShcSecretLongEno"),
		},
	}
	require.NoError(t, c.Create(ctx, nsSecret))

	require.NoError(t, c.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test-secrets", Namespace: "test"},
		Data: map[string][]byte{
			"password":    []byte("dummyPasswordLongEnough"),
			"idxc_secret": []byte("dummyIdxcSecretLongEn"),
		},
	}))

	require.NoError(t, c.Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-manager1-cluster-manager-0", Namespace: "test"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:latest"}},
			Volumes: []corev1.Volume{
				{Name: "mnt-splunk-secrets", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "test-secrets"}}},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}},
	}))

	cmReplicas := int32(1)
	require.NoError(t, c.Create(ctx, &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: GetSplunkStatefulsetName(SplunkClusterManager, "manager1"), Namespace: "test"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &cmReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:latest"}}},
			},
		},
	}))

	crName := "test"
	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: crName, Namespace: "test"},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas: 3,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock:              true,
				ClusterManagerRef: corev1.ObjectReference{Name: "manager1"},
			},
			QueueRef:         &corev1.ObjectReference{Name: queueOld.Name},
			ObjectStorageRef: &corev1.ObjectReference{Name: objStorage.Name},
		},
		// Pre-set to the NS secret's ResourceVersion so ApplyIdxcSecret sees a
		// matching version and skips the pod exec loop (which fails in fake clients).
		Status: enterpriseApi.IndexerClusterStatus{
			NamespaceSecretResourceVersion: nsSecret.ResourceVersion,
		},
	}
	require.NoError(t, c.Create(ctx, cr))

	threeReplicas := int32(3)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: GetSplunkStatefulsetName(SplunkIndexer, crName), Namespace: "test"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &threeReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:latest"}}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			Replicas: threeReplicas, ReadyReplicas: threeReplicas,
			CurrentRevision: "v1", UpdateRevision: "v1",
		},
	}
	require.NoError(t, c.Create(ctx, sts))

	basePod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:latest"}},
			Volumes: []corev1.Volume{
				{Name: "mnt-splunk-secrets", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "test-secrets"}}},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}},
	}
	for i := int32(0); i < threeReplicas; i++ {
		pod := basePod.DeepCopy()
		pod.ObjectMeta = metav1.ObjectMeta{
			Name: GetSplunkStatefulsetPodName(SplunkIndexer, crName, i), Namespace: "test",
			Labels: map[string]string{
				"app.kubernetes.io/instance": GetSplunkStatefulsetName(SplunkIndexer, crName),
				"controller-revision-hash":   "v1",
			},
		}
		require.NoError(t, c.Create(ctx, pod))
	}

	// --- Pass 1: reconcile with old queue ---
	_, err := ApplyIndexerClusterManager(ctx, c, cr)
	require.NoError(t, err)

	cmListOld := listCredsConfigMaps(t, ctx, c, crName)
	secretListOld := listCredsSecrets(t, ctx, c, crName)
	require.Len(t, cmListOld, 1, "pass 1 must create exactly one defaults ConfigMap")
	require.Len(t, secretListOld, 1, "pass 1 must create exactly one credentials Secret")
	oldCMName := cmListOld[0].Name
	oldSecretName := secretListOld[0].Name
	assert.Regexp(t, regexp.MustCompile(`^sok-indexercluster-defaults-[0-9a-f]{6}$`), oldCMName)
	assert.Regexp(t, regexp.MustCompile(`^sok-indexercluster-creds-[0-9a-f]{6}$`), oldSecretName)

	// The declarative path emits no imperative queue-config / restart events.
	for _, event := range recorder.events {
		assert.NotEqual(t, "QueueConfigUpdated", event.reason, "declarative path must not emit QueueConfigUpdated")
		assert.NotEqual(t, "IndexersRestarted", event.reason, "declarative path must not emit IndexersRestarted")
	}

	// --- Pass 2: swap QueueRef to a queue with different config ---
	recorder.events = []mockEvent{}
	cr.Spec.QueueRef = &corev1.ObjectReference{Name: queueNew.Name}

	_, err = ApplyIndexerClusterManager(ctx, c, cr)
	require.NoError(t, err)

	// New queue config → new content-addressed names.
	cmListNew := listCredsConfigMaps(t, ctx, c, crName)
	secretListNew := listCredsSecrets(t, ctx, c, crName)
	require.Len(t, cmListNew, 1, "stale defaults ConfigMap must be garbage-collected after queue ref change")
	require.Len(t, secretListNew, 1, "stale credentials Secret must be garbage-collected after queue ref change")
	assert.NotEqual(t, oldCMName, cmListNew[0].Name, "new queue config must produce a new ConfigMap name")
	assert.NotEqual(t, oldSecretName, secretListNew[0].Name, "new queue config must produce a new Secret name")

	// Still no imperative events on the ref-change pass.
	for _, event := range recorder.events {
		assert.NotEqual(t, "QueueConfigUpdated", event.reason, "declarative path must not emit QueueConfigUpdated on ref change")
		assert.NotEqual(t, "IndexersRestarted", event.reason, "declarative path must not emit IndexersRestarted on ref change")
	}
}

// TestIdxcQueueRefRemovedGCsResources verifies that clearing QueueRef (setting it to nil)
// after a queue was previously configured garbage-collects the stale ConfigMap and Secret
// without creating new ones.
func TestIdxcQueueRefRemovedGCsResources(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	ctx := context.TODO()
	recorder := &mockEventRecorder{events: []mockEvent{}}
	eventPublisher := &K8EventPublisher{recorder: recorder}
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	oldVerifyRFPeers := VerifyRFPeers
	defer func() { VerifyRFPeers = oldVerifyRFPeers }()
	VerifyRFPeers = func(ctx context.Context, mgr indexerClusterPodManager, client splcommon.ControllerClient) error {
		return nil
	}

	oldGetCMInfo := GetClusterManagerInfoCall
	oldGetCMPeers := GetClusterManagerPeersCall
	defer func() {
		GetClusterManagerInfoCall = oldGetCMInfo
		GetClusterManagerPeersCall = oldGetCMPeers
	}()
	GetClusterManagerInfoCall = func(ctx context.Context, mgr *indexerClusterPodManager) (*splclient.ClusterManagerInfo, error) {
		return &splclient.ClusterManagerInfo{Initialized: true, IndexingReady: true, ServiceReady: true, MaintenanceMode: false}, nil
	}
	GetClusterManagerPeersCall = func(ctx context.Context, mgr *indexerClusterPodManager) (map[string]splclient.ClusterManagerPeerInfo, error) {
		peers := map[string]splclient.ClusterManagerPeerInfo{}
		for i := int32(0); i < 3; i++ {
			peerName := GetSplunkStatefulsetPodName(SplunkIndexer, mgr.cr.GetName(), i)
			peers[peerName] = splclient.ClusterManagerPeerInfo{ID: fmt.Sprintf("peer-%d", i), Status: "Up", Searchable: true}
		}
		return peers, nil
	}

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(appsv1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	c := newFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.ClusterManager{}).
		WithStatusSubresource(&enterpriseApi.IndexerCluster{}).
		Build()

	require.NoError(t, c.Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-test-probe-configmap", Namespace: "test"},
	}))

	cm := &enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{Name: "manager1", Namespace: "test"},
		Status:     enterpriseApi.ClusterManagerStatus{Phase: enterpriseApi.PhaseReady},
	}
	require.NoError(t, c.Create(ctx, cm))
	require.NoError(t, c.Status().Update(ctx, cm))

	credsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "queue-secrets", Namespace: "test"},
		Data: map[string][]byte{
			"s3_access_key": []byte("AKIAEXAMPLE"),
			"s3_secret_key": []byte("shhh-secret"),
		},
	}
	require.NoError(t, c.Create(ctx, credsSecret))

	queue := &enterpriseApi.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "queue-to-remove", Namespace: "test"},
		Spec: enterpriseApi.QueueSpec{
			Provider: "sqs",
			SQS: enterpriseApi.SQSSpec{
				Name: "my-queue", AuthRegion: "us-west-2",
				Endpoint: "https://sqs.us-west-2.amazonaws.com", DLQ: "my-dlq",
				VolList: []enterpriseApi.SQSVolumeSpec{{SecretRef: "queue-secrets"}},
			},
		},
	}
	require.NoError(t, c.Create(ctx, queue))

	objStorage := &enterpriseApi.ObjectStorage{
		ObjectMeta: metav1.ObjectMeta{Name: "os", Namespace: "test"},
		Spec: enterpriseApi.ObjectStorageSpec{
			Provider: "s3",
			S3:       enterpriseApi.S3Spec{Endpoint: "https://s3.us-west-2.amazonaws.com", Path: "bucket/key"},
		},
	}
	require.NoError(t, c.Create(ctx, objStorage))

	nsSecretName := splcommon.GetNamespaceScopedSecretName("test")
	nsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: nsSecretName, Namespace: "test"},
		Data: map[string][]byte{
			"hec_token":    []byte("ABCDEF01-2345-6789-ABCD-EF0123456789"),
			"password":     []byte("dummyPasswordLongEnough"),
			"pass4SymmKey": []byte("dummyPass4SymmKeyLong"),
			"idxc_secret":  []byte("dummyIdxcSecretLongEn"),
			"shc_secret":   []byte("dummyShcSecretLongEno"),
		},
	}
	require.NoError(t, c.Create(ctx, nsSecret))

	require.NoError(t, c.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test-secrets", Namespace: "test"},
		Data: map[string][]byte{
			"password":    []byte("dummyPasswordLongEnough"),
			"idxc_secret": []byte("dummyIdxcSecretLongEn"),
		},
	}))

	require.NoError(t, c.Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-manager1-cluster-manager-0", Namespace: "test"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:latest"}},
			Volumes: []corev1.Volume{
				{Name: "mnt-splunk-secrets", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "test-secrets"}}},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}},
	}))

	cmReplicas := int32(1)
	require.NoError(t, c.Create(ctx, &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: GetSplunkStatefulsetName(SplunkClusterManager, "manager1"), Namespace: "test"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &cmReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:latest"}}},
			},
		},
	}))

	crName := "test"
	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: crName, Namespace: "test"},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas: 3,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock:              true,
				ClusterManagerRef: corev1.ObjectReference{Name: "manager1"},
			},
			QueueRef:         &corev1.ObjectReference{Name: queue.Name},
			ObjectStorageRef: &corev1.ObjectReference{Name: objStorage.Name},
		},
		Status: enterpriseApi.IndexerClusterStatus{
			NamespaceSecretResourceVersion: nsSecret.ResourceVersion,
		},
	}
	require.NoError(t, c.Create(ctx, cr))

	threeReplicas := int32(3)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: GetSplunkStatefulsetName(SplunkIndexer, crName), Namespace: "test"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &threeReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:latest"}}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			Replicas: threeReplicas, ReadyReplicas: threeReplicas,
			CurrentRevision: "v1", UpdateRevision: "v1",
		},
	}
	require.NoError(t, c.Create(ctx, sts))

	basePod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:latest"}},
			Volumes: []corev1.Volume{
				{Name: "mnt-splunk-secrets", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "test-secrets"}}},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}},
	}
	for i := int32(0); i < threeReplicas; i++ {
		pod := basePod.DeepCopy()
		pod.ObjectMeta = metav1.ObjectMeta{
			Name: GetSplunkStatefulsetPodName(SplunkIndexer, crName, i), Namespace: "test",
			Labels: map[string]string{
				"app.kubernetes.io/instance": GetSplunkStatefulsetName(SplunkIndexer, crName),
				"controller-revision-hash":   "v1",
			},
		}
		require.NoError(t, c.Create(ctx, pod))
	}

	// --- Pass 1: reconcile with queue set ---
	_, err := ApplyIndexerClusterManager(ctx, c, cr)
	require.NoError(t, err)

	require.Len(t, listCredsConfigMaps(t, ctx, c, crName), 1, "pass 1 must create exactly one defaults ConfigMap")
	require.Len(t, listCredsSecrets(t, ctx, c, crName), 1, "pass 1 must create exactly one credentials Secret")

	// --- Pass 2: remove QueueRef ---
	cr.Spec.QueueRef = nil
	cr.Spec.ObjectStorageRef = nil

	_, err = ApplyIndexerClusterManager(ctx, c, cr)
	require.NoError(t, err)

	assert.Empty(t, listCredsConfigMaps(t, ctx, c, crName), "removing QueueRef must GC the stale defaults ConfigMap")
	assert.Empty(t, listCredsSecrets(t, ctx, c, crName), "removing QueueRef must GC the stale credentials Secret")

	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: sts.GetName(), Namespace: sts.GetNamespace()}, sts))
	var defaultsURL string
	for _, e := range sts.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "SPLUNK_DEFAULTS_URL" {
			defaultsURL = e.Value
		}
	}
	assert.NotContains(t, defaultsURL, resources.DefaultsMountPath(), "SPLUNK_DEFAULTS_URL must not reference the removed defaults ConfigMap mount")
	assert.NotContains(t, defaultsURL, resources.SecretMountPath(), "SPLUNK_DEFAULTS_URL must not reference the removed credentials Secret mount")
}
