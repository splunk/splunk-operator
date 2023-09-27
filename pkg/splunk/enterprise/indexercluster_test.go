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

	"github.com/jinzhu/copier"
	"github.com/pkg/errors"
	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-logr/logr"
	manager "github.com/splunk/splunk-operator/pkg/splunk"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	managermodel "github.com/splunk/splunk-operator/pkg/splunk/model"
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

func setCredsIdx(t *testing.T, c splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) manager.SplunkManager {
	ctx := context.TODO()
	clusterManager := enterpriseApi.ClusterManager{}
	clusterManager.Name = "test"
	info := &managermodel.ReconcileInfo{
		Kind:       cr.Kind,
		CommonSpec: cr.Spec.CommonSplunkSpec,
		Client:     c,
		Log:        log.Log,
		Namespace:  cr.Namespace,
		Name:       cr.Name,
	}
	copier.Copy(info.MetaObject, cr.ObjectMeta)
	publisher := func(ctx context.Context, eventType, reason, message string) {}
	mg := NewManagerFactory(true)
	manager, err := mg.NewManager(ctx, info, publisher)
	if err != nil {
		return nil
	}
	return manager
}

func TestApplyIndexerClusterOld(t *testing.T) {
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
	_, err = ApplyIndexerCluster(ctx, c, &idxCr)

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
	_, err = ApplyIndexerCluster(ctx, c, &idxCr)

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
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v4.ClusterManager-test-manager1"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-service"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-indexer"},
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
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0], funcCalls[4], funcCalls[5], funcCalls[8], funcCalls[10]}, "Update": {funcCalls[0]}, "List": {listmockCall[0], listmockCall[1]}}
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
		manager := setCredsIdx(t, c, cr.(*enterpriseApi.IndexerCluster))
		_, err := manager.ApplyIndexerClusterManager(context.Background(), c, cr.(*enterpriseApi.IndexerCluster))
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyIndexerClusterManager", &current, revised, createCalls, updateCalls, reconcile, true)

	// // test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
		manager := setCredsIdx(t, c, cr.(*enterpriseApi.IndexerCluster))
		_, err := manager.ApplyIndexerClusterManager(context.Background(), c, cr.(*enterpriseApi.IndexerCluster))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)

	// Negative testing
	ctx := context.TODO()
	c := spltest.NewMockClient()
	rerr := errors.New(splcommon.Rerr)
	c.InduceErrorKind[splcommon.MockClientInduceErrorGet] = rerr
	manager := setCredsIdx(t, c, &current)
	_, err := manager.ApplyIndexerClusterManager(ctx, c, &current)
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
	manager = setCredsIdx(t, c, &current)
	_, err = manager.ApplyIndexerClusterManager(ctx, c, &current)
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
	manager = setCredsIdx(t, c, &current)
	_, err = manager.ApplyIndexerClusterManager(ctx, newc, &current)
	if err == nil {
		t.Errorf("Expected error")
	}
}

func TestGetMonitoringConsoleClient(t *testing.T) {
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

	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)

	mgr := getIndexerClusterPodManager(method, mockHandlers, mockSplunkClient, replicas)
	indexerClusterPodManagerVerifyRFPeersTester(t, method, mgr, desiredReplicas, wantPhase, wantCalls, wantError)
	mockSplunkClient.CheckRequests(t, method)
}

func TestVerifyRFPeers(t *testing.T) {

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
			Body:   `{"links":{"_reload":"/services/cluster/config/_reload","_acl":"/services/cluster/config/_acl"},"origin":"https://localhost:8089/services/cluster/config","updated":"2020-10-28T21:37:07+00:00","generator":{"build":"152fb4b2bb96","version":"8.0.6"},"entry":[{"name":"config","id":"https://localhost:8089/services/cluster/config/config","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/services/cluster/config/config","list":"/services/cluster/config/config","_reload":"/services/cluster/config/config/_reload","edit":"/services/cluster/config/config","disable":"/services/cluster/config/config/disable"},"author":"system","acl":{"app":"","can_list":true,"can_write":true,"modifiable":false,"owner":"system","perms":{"read":["admin","splunk-system-role"],"write":["admin","splunk-system-role"]},"removable":false,"sharing":"system"},"content":{"access_logging_for_heartbeats":false,"auto_rebalance_primaries":true,"buckets_to_summarize":"primaries","cluster_label":"idxc_label","cxn_timeout":60,"decommission_force_finish_idle_time":0,"decommission_force_timeout":180,"disabled":false,"eai:acl":null,"forwarderdata_rcv_port":0,"forwarderdata_use_ssl":false,"frozen_notifications_per_batch":10,"guid":"F643BA71-0D3C-4D63-A0BC-A1604AC928E3","heartbeat_period":18446744073709552000,"heartbeat_timeout":60,"master_uri":"https://127.0.0.1:8089","max_auto_service_interval":30,"max_fixup_time_ms":5000,"max_peer_build_load":2,"max_peer_rep_load":5,"max_peer_sum_rep_load":5,"max_peers_to_download_bundle":5,"max_primary_backups_per_service":10,"mode":"master","multisite":"false","notify_buckets_period":10,"notify_scan_min_period":10,"notify_scan_period":10,"percent_peers_to_restart":10,"ping_flag":true,"quiet_period":60,"rcv_timeout":60,"rebalance_primaries_execution_limit_ms":0,"rebalance_threshold":0.9,"register_forwarder_address":"","register_replication_address":"","register_search_address":"","remote_storage_upload_timeout":60,"rep_cxn_timeout":60,"rep_max_rcv_timeout":180,"rep_max_send_timeout":180,"rep_rcv_timeout":60,"rep_send_timeout":60,"replication_factor":3,"replication_port":null,"replication_use_ssl":false,"report_remote_storage_bucket_upload_to_targets":false,"reporting_delay_period":30,"restart_inactivity_timeout":600,"restart_timeout":60,"rolling_restart":"restart","search_factor":3,"search_files_retry_timeout":600,"secret":"********","send_timeout":60,"service_interval":0,"site":"default","site_by_site":true,"summary_replication":"false","use_batch_discard":"true","use_batch_mask_changes":"true","use_batch_remote_rep_changes":"false"}}],"paging":{"total":1,"perPage":10000000,"offset":0},"messages":[]}`,
		},
	}

	method := "indexerClusterPodManager.verifyRFPeers(All pods ready)"
	// test for singlesite i.e. with replication_factor=3(on ClusterManager) and replicas=3(on IndexerCluster)
	indexerClusterPodManagerReplicasTester(t, method, mockHandlers, 3 /*replicas*/, 3 /*desired replicas*/, enterpriseApi.PhaseReady, wantCalls, nil)

	// test for singlesite i.e. with replication_factor=3(on ClusterManager) and replicas=1(on IndexerCluster)
	indexerClusterPodManagerReplicasTester(t, method, mockHandlers, 1 /*replicas*/, 3 /*desired replicas*/, enterpriseApi.PhaseReady, wantCalls, nil)

	// Now test for multi-site too
	mockHandlers[0].Body = `{"links":{"_reload":"/services/cluster/config/_reload","_acl":"/services/cluster/config/_acl"},"origin":"https://localhost:8089/services/cluster/config","updated":"2020-10-28T21:37:07+00:00","generator":{"build":"152fb4b2bb96","version":"8.0.6"},"entry":[{"name":"config","id":"https://localhost:8089/services/cluster/config/config","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/services/cluster/config/config","list":"/services/cluster/config/config","_reload":"/services/cluster/config/config/_reload","edit":"/services/cluster/config/config","disable":"/services/cluster/config/config/disable"},"author":"system","acl":{"app":"","can_list":true,"can_write":true,"modifiable":false,"owner":"system","perms":{"read":["admin","splunk-system-role"],"write":["admin","splunk-system-role"]},"removable":false,"sharing":"system"},"content":{"access_logging_for_heartbeats":false,"auto_rebalance_primaries":true,"buckets_to_summarize":"primaries","cluster_label":"idxc_label","cxn_timeout":60,"decommission_force_finish_idle_time":0,"decommission_force_timeout":180,"disabled":false,"eai:acl":null,"forwarderdata_rcv_port":0,"forwarderdata_use_ssl":false,"frozen_notifications_per_batch":10,"guid":"F643BA71-0D3C-4D63-A0BC-A1604AC928E3","heartbeat_period":18446744073709552000,"heartbeat_timeout":60,"master_uri":"https://127.0.0.1:8089","max_auto_service_interval":30,"max_fixup_time_ms":5000,"max_peer_build_load":2,"max_peer_rep_load":5,"max_peer_sum_rep_load":5,"max_peers_to_download_bundle":5,"max_primary_backups_per_service":10,"mode":"master","multisite":"true","notify_buckets_period":10,"notify_scan_min_period":10,"notify_scan_period":10,"percent_peers_to_restart":10,"ping_flag":true,"quiet_period":60,"rcv_timeout":60,"rebalance_primaries_execution_limit_ms":0,"rebalance_threshold":0.9,"register_forwarder_address":"","register_replication_address":"","register_search_address":"","remote_storage_upload_timeout":60,"rep_cxn_timeout":60,"rep_max_rcv_timeout":180,"rep_max_send_timeout":180,"rep_rcv_timeout":60,"rep_send_timeout":60,"replication_factor":3,"replication_port":null,"replication_use_ssl":false,"report_remote_storage_bucket_upload_to_targets":false,"reporting_delay_period":30,"restart_inactivity_timeout":600,"restart_timeout":60,"rolling_restart":"restart","search_factor":3,"search_files_retry_timeout":600,"secret":"********","send_timeout":60,"service_interval":0,"site":"site1","site_by_site":true,"site_replication_factor":"{ origin:2, total:2 }","site_search_factor":"{ origin:2, total:2 }","summary_replication":"false","use_batch_discard":"true","use_batch_mask_changes":"true","use_batch_remote_rep_changes":"false"}}],"paging":{"total":1,"perPage":10000000,"offset":0},"messages":[]}`

	//test for multisite i.e. with site_replication_factor=origin:2,total:2(on ClusterManager) and replicas=2(on IndexerCluster)
	indexerClusterPodManagerReplicasTester(t, method, mockHandlers, 2 /*replicas*/, 2 /*desired replicas*/, enterpriseApi.PhaseReady, wantCalls, nil)

	//test for multisite i.e. with site_replication_factor=origin:2,total:2(on ClusterManager) and replicas=1(on IndexerCluster)
	indexerClusterPodManagerReplicasTester(t, method, mockHandlers, 1 /*replicas*/, 2 /*desired replicas*/, enterpriseApi.PhaseReady, wantCalls, nil)
}

func checkResponseFromUpdateStatus(t *testing.T, method string, mockHandlers []spltest.MockHTTPHandler, replicas int32, statefulSet *appsv1.StatefulSet, retry bool) error {
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

	mockHandlers[0].Body = `{"links":{},"origin":"https://localhost:8089/services/cluster/manager/info","updated":"2020-03-18T01:04:53+00:00","generator":{"build":"a7f645ddaf91","version":"8.0.2"},"entry":[{"name":"master","id":"https://localhost:8089/services/cluster/manager/info/master","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/services/cluster/manager/info/master","list":"/services/cluster/manager/info/master"},"author":"system","acl":{"app":"","can_list":true,"can_write":true,"modifiable":false,"owner":"system","perms":{"read":["admin","splunk-system-role"],"write":["admin","splunk-system-role"]},"removable":false,"sharing":"system"},"content":{"active_bundle":{"bundle_path":"/opt/splunk/var/run/splunk/cluster/remote-bundle/506c58d5aeda1dd6017889e3186e7337-1583870198.bundle","checksum":"14310A4AABD23E85BBD4559C4A3B59F8","timestamp":1583870198},"apply_bundle_status":{"invalid_bundle":{"bundle_path":"","bundle_validation_errors_on_master":[],"checksum":"","timestamp":0},"reload_bundle_issued":false,"status":"None"},"backup_and_restore_primaries":false,"controlled_rolling_restart_flag":false,"eai:acl":null,"indexing_ready_flag":true,"initialized_flag":true,"label":"splunk-stack1-cluster-manager-0","last_check_restart_bundle_result":false,"last_dry_run_bundle":{"bundle_path":"","checksum":"","timestamp":0},"last_validated_bundle":{"bundle_path":"/opt/splunk/var/run/splunk/cluster/remote-bundle/0af7c0e95f313f7be3b0cb1d878df9a1-1583948640.bundle","checksum":"14310A4AABD23E85BBD4559C4A3B59F8","is_valid_bundle":true,"timestamp":1583948640},"latest_bundle":{"bundle_path":"/opt/splunk/var/run/splunk/cluster/remote-bundle/506c58d5aeda1dd6017889e3186e7337-1583870198.bundle","checksum":"14310A4AABD23E85BBD4559C4A3B59F8","timestamp":1583870198},"maintenance_mode":false,"multisite":false,"previous_active_bundle":{"bundle_path":"","checksum":"","timestamp":0},"primaries_backup_status":"No on-going (or) completed primaries backup yet. Check back again in few minutes if you expect a backup.","quiet_period_flag":false,"rolling_restart_flag":false,"rolling_restart_or_upgrade":false,"service_ready_flag":true,"start_time":1583948636,"summary_replication":"false"}}],"paging":{"total":1,"perPage":30,"offset":0},"messages":[]}`

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
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Pod-test-splunk-stack1-indexer-0"},
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

	wantCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[4], funcCalls[5]}, "Create": {funcCalls[1]}, "List": {listmockCall[0]}}

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
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[4]}, "Create": {funcCalls[1]}}
	method = "indexerClusterPodManager.Update(Pod Not Found)"
	indexerClusterPodManagerUpdateTester(t, method, mockHandlers, 1, enterpriseApi.PhaseScalingDown, statefulSet, wantCalls, nil, statefulSet, pod)

	// test scale down => decommission pod
	mockHandlers[1].Body = `{"entry":[{"name":"aa45bf46-7f46-47af-a760-590d5c606d10","content":{"status":"Up","label":"splunk-stack1-indexer-0"}},{"name":"D39B1729-E2C5-4273-B9B2-534DA7C2F866","content":{"status":"GracefulShutdown","label":"splunk-stack1-indexer-1"}}]}`
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
}

func TestInvalidIndexerClusterSpec(t *testing.T) {

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
	manager := setCredsIdx(t, c, &cr)
	if _, err := manager.ApplyIndexerClusterManager(context.Background(), c, &cr); err == nil {
		t.Errorf("ApplyIndxerCluster() should have returned error")
	}

	cr.Spec.ClusterManagerRef.Name = "manager1"
	// verifyRFPeers should return err here
	manager = setCredsIdx(t, c, &cr)
	if _, err := manager.ApplyIndexerClusterManager(context.Background(), c, &cr); err == nil {
		t.Errorf("ApplyIndxerCluster() should have returned error")
	}

	cm.Status.Phase = enterpriseApi.PhaseError
	cr.Spec.CommonSplunkSpec.EtcVolumeStorageConfig.StorageCapacity = "-abcd"
	manager = setCredsIdx(t, c, &cr)
	if _, err := manager.ApplyIndexerClusterManager(context.Background(), c, &cr); err == nil {
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
	_ = splutil.CreateResource(ctx, c, &current)
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

	cr.Spec.ClusterManagerRef.Namespace = "other"
	if err := validateIndexerClusterSpec(ctx, c, &cr); err == nil {
		t.Errorf("validateIndexerClusterSpec() error expected on multisite IndexerCluster referencing a cluster manager located in a different namespace")
	}
}

func TestGetIndexerClusterList(t *testing.T) {
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

	// mock the verify RF peer funciton
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

	builder := fake.NewClientBuilder()
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
	err := c.Status().Update(ctx, clustermanager)
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
	manager := setCreds(t, c, clustermanager, clustermanager.Spec.CommonSplunkSpec)
	_, err = manager.ApplyClusterManager(ctx, c, clustermanager)
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
	_, err = manager.ApplyClusterManager(ctx, c, clustermanager)
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

	manager = setCredsIdx(t, c, indexercluster)
	_, err = manager.ApplyIndexerClusterManager(ctx, c, indexercluster)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for indexer cluster %v", err)
		debug.PrintStack()
	}

	namespacedName = types.NamespacedName{
		Name:      indexercluster.Name,
		Namespace: indexercluster.Namespace,
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
	manager = setCredsIdx(t, c, indexercluster)
	_, err = manager.ApplyIndexerClusterManager(ctx, c, indexercluster)
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
	manager = setCredsIdx(t, c, indexercluster)
	_, err = manager.ApplyIndexerClusterManager(ctx, c, indexercluster)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for indexer cluster with app framework  %v", err)
		debug.PrintStack()
	}
}
