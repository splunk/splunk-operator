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
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func TestApplyIndexerCluster(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-service"},
		{MetaName: "*v1.Service-test-splunk-stack1-cluster-master-service"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-cluster-master-secret-v1"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-master"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-indexer-secret-v1"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-indexer"},
	}
	listOpts := []client.ListOption{
		client.InNamespace("test"),
	}
	listmockCall := []spltest.MockFuncCall{
		{ListOpts: listOpts}}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0], funcCalls[1], funcCalls[2], funcCalls[3], funcCalls[5], funcCalls[6], funcCalls[8], funcCalls[9]}, "List": {listmockCall[0], listmockCall[0]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": {funcCalls[6], funcCalls[9]}, "List": {listmockCall[0], listmockCall[0]}}

	current := enterprisev1.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterprisev1.IndexerClusterSpec{
			Replicas: 1,
		},
	}
	revised := current.DeepCopy()
	revised.Spec.Image = "splunk/test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyIndexerCluster(c, cr.(*enterprisev1.IndexerCluster))
		return err
	}
	spltest.ReconcileTester(t, "TestApplyIndexerCluster", &current, revised, createCalls, updateCalls, reconcile, true)

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
		_, err := ApplyIndexerCluster(c, cr.(*enterprisev1.IndexerCluster))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)
}

func TestApplyIndexerClusterMasterOnlyWithSmartstore(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-indexercluster-smartstore"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-service"},
		{MetaName: "*v1.Service-test-splunk-stack1-cluster-master-service"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-cluster-master-secret-v1"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-master"},
	}
	listOpts := []client.ListOption{
		client.InNamespace("test"),
	}
	listmockCall := []spltest.MockFuncCall{
		{ListOpts: listOpts}}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[1], funcCalls[3], funcCalls[4], funcCalls[5], funcCalls[7], funcCalls[8]}, "List": {listmockCall[0]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": {funcCalls[8]}, "List": {listmockCall[0]}}

	current := enterprisev1.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterprisev1.IndexerClusterSpec{
			Replicas: 0,
			SmartStore: enterprisev1.SmartStoreSpec{
				VolList: []enterprisev1.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london"},
				},

				IndexList: []enterprisev1.IndexSpec{
					{Name: "salesdata1", VolName: "msos_s2s3_vol"},
					{Name: "salesdata2", RemotePath: "salesdata2", VolName: "msos_s2s3_vol"},
					{Name: "salesdata3", RemotePath: "", VolName: "msos_s2s3_vol"},
				},
			},
		},
	}
	client := spltest.NewMockClient()

	// Without S3 keys, ApplyStandalone should fail
	_, err := ApplyIndexerCluster(client, &current)
	if err == nil {
		t.Errorf("ApplyIndexerCluster should fail without S3 secrets configured")
	}

	// Create namespace scoped secret
	secret, err := ApplyNamespaceScopedSecretObject(client, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	secret.Data[s3AccessKey] = []byte("abcdJDckRkxhMEdmSk5FekFRRzBFOXV6bGNldzJSWE9IenhVUy80aa")
	secret.Data[s3SecretKey] = []byte("g4NVp0a29PTzlPdGczWk1vekVUcVBSa0o4NkhBWWMvR1NadDV4YVEy")
	_, err = splctrl.ApplySecret(client, secret)
	if err != nil {
		t.Errorf(err.Error())
	}

	revised := current.DeepCopy()
	revised.Spec.Image = "splunk/test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyIndexerCluster(c, cr.(*enterprisev1.IndexerCluster))
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyIndexerClusterMasterOnlyWithSmartstore", &current, revised, createCalls, updateCalls, reconcile, true, secret)

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
		_, err := ApplyIndexerCluster(c, cr.(*enterprisev1.IndexerCluster))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)
}

func TestApplyIndexerClusterMasterOnly(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-service"},
		{MetaName: "*v1.Service-test-splunk-stack1-cluster-master-service"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-cluster-master-secret-v1"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-master"},
	}
	listOpts := []client.ListOption{
		client.InNamespace("test"),
	}
	listmockCall := []spltest.MockFuncCall{
		{ListOpts: listOpts}}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0], funcCalls[1], funcCalls[2], funcCalls[3], funcCalls[5], funcCalls[6]}, "List": {listmockCall[0]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": {funcCalls[6]}, "List": {listmockCall[0]}}

	current := enterprisev1.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterprisev1.IndexerClusterSpec{
			Replicas: 0,
		},
	}
	revised := current.DeepCopy()
	revised.Spec.Image = "splunk/test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyIndexerCluster(c, cr.(*enterprisev1.IndexerCluster))
		return err
	}
	spltest.ReconcileTester(t, "TestApplyIndexerClusterMasterOnly", &current, revised, createCalls, updateCalls, reconcile, true)

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
		_, err := ApplyIndexerCluster(c, cr.(*enterprisev1.IndexerCluster))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)
}

func TestApplyIndexerClusterMultipart(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-service"},
		{MetaName: "*v1alpha3.IndexerCluster-test-master1"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-indexer-secret-v1"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-indexer"},
	}
	listOpts := []client.ListOption{
		client.InNamespace("test"),
	}
	listmockCall := []spltest.MockFuncCall{
		{ListOpts: listOpts}}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0], funcCalls[1], funcCalls[2], funcCalls[5], funcCalls[6]}, "List": {listmockCall[0]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": {funcCalls[6]}, "List": {listmockCall[0]}}

	current := enterprisev1.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterprisev1.IndexerClusterSpec{
			Replicas: 1,
			CommonSplunkSpec: enterprisev1.CommonSplunkSpec{
				IndexerClusterRef: corev1.ObjectReference{
					Name: "master1",
				},
			},
		},
	}
	revised := current.DeepCopy()
	revised.Spec.Image = "splunk/test"
	secret := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind: "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-master1-indexer-secrets",
			Namespace: "test",
		},
	}
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		c.AddObject(&secret)
		_, err := ApplyIndexerCluster(c, cr.(*enterprisev1.IndexerCluster))
		return err
	}
	spltest.ReconcileTester(t, "TestApplyIndexerClusterMultipart", &current, revised, createCalls, updateCalls, reconcile, true)

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
		_, err := ApplyIndexerCluster(c, cr.(*enterprisev1.IndexerCluster))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)
}

func indexerClusterPodManagerTester(t *testing.T, method string, mockHandlers []spltest.MockHTTPHandler,
	desiredReplicas int32, wantPhase splcommon.Phase, statefulSet *appsv1.StatefulSet,
	wantCalls map[string][]spltest.MockFuncCall, wantError error, initObjects ...runtime.Object) {

	// test for updating
	scopedLog := log.WithName(method)
	cr := enterprisev1.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Status: enterprisev1.IndexerClusterStatus{
			ClusterMasterPhase: splcommon.PhaseReady,
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
		{MetaName: "*v1.Pod-test-splunk-stack1-0"},
	}
	wantCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0]}}

	// test 1 ready pod
	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    "https://splunk-stack1-cluster-master-service.test.svc.cluster.local:8089/services/cluster/master/info?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   `{"links":{},"origin":"https://localhost:8089/services/cluster/master/info","updated":"2020-03-18T01:04:53+00:00","generator":{"build":"a7f645ddaf91","version":"8.0.2"},"entry":[{"name":"master","id":"https://localhost:8089/services/cluster/master/info/master","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/services/cluster/master/info/master","list":"/services/cluster/master/info/master"},"author":"system","acl":{"app":"","can_list":true,"can_write":true,"modifiable":false,"owner":"system","perms":{"read":["admin","splunk-system-role"],"write":["admin","splunk-system-role"]},"removable":false,"sharing":"system"},"content":{"active_bundle":{"bundle_path":"/opt/splunk/var/run/splunk/cluster/remote-bundle/506c58d5aeda1dd6017889e3186e7337-1583870198.bundle","checksum":"14310A4AABD23E85BBD4559C4A3B59F8","timestamp":1583870198},"apply_bundle_status":{"invalid_bundle":{"bundle_path":"","bundle_validation_errors_on_master":[],"checksum":"","timestamp":0},"reload_bundle_issued":false,"status":"None"},"backup_and_restore_primaries":false,"controlled_rolling_restart_flag":false,"eai:acl":null,"indexing_ready_flag":true,"initialized_flag":true,"label":"splunk-stack1-cluster-master-0","last_check_restart_bundle_result":false,"last_dry_run_bundle":{"bundle_path":"","checksum":"","timestamp":0},"last_validated_bundle":{"bundle_path":"/opt/splunk/var/run/splunk/cluster/remote-bundle/0af7c0e95f313f7be3b0cb1d878df9a1-1583948640.bundle","checksum":"14310A4AABD23E85BBD4559C4A3B59F8","is_valid_bundle":true,"timestamp":1583948640},"latest_bundle":{"bundle_path":"/opt/splunk/var/run/splunk/cluster/remote-bundle/506c58d5aeda1dd6017889e3186e7337-1583870198.bundle","checksum":"14310A4AABD23E85BBD4559C4A3B59F8","timestamp":1583870198},"maintenance_mode":false,"multisite":false,"previous_active_bundle":{"bundle_path":"","checksum":"","timestamp":0},"primaries_backup_status":"No on-going (or) completed primaries backup yet. Check back again in few minutes if you expect a backup.","quiet_period_flag":false,"rolling_restart_flag":false,"rolling_restart_or_upgrade":false,"service_ready_flag":true,"start_time":1583948636,"summary_replication":"false"}}],"paging":{"total":1,"perPage":30,"offset":0},"messages":[]}`,
		},
		{
			Method: "GET",
			URL:    "https://splunk-stack1-cluster-master-service.test.svc.cluster.local:8089/services/cluster/master/peers?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   `{"links":{"create":"/services/cluster/master/peers/_new"},"origin":"https://localhost:8089/services/cluster/master/peers","updated":"2020-03-18T01:08:53+00:00","generator":{"build":"a7f645ddaf91","version":"8.0.2"},"entry":[{"name":"D39B1729-E2C5-4273-B9B2-534DA7C2F866","id":"https://localhost:8089/services/cluster/master/peers/D39B1729-E2C5-4273-B9B2-534DA7C2F866","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/services/cluster/master/peers/D39B1729-E2C5-4273-B9B2-534DA7C2F866","list":"/services/cluster/master/peers/D39B1729-E2C5-4273-B9B2-534DA7C2F866","edit":"/services/cluster/master/peers/D39B1729-E2C5-4273-B9B2-534DA7C2F866"},"author":"system","acl":{"app":"","can_list":true,"can_write":true,"modifiable":false,"owner":"system","perms":{"read":["admin","splunk-system-role"],"write":["admin","splunk-system-role"]},"removable":false,"sharing":"system"},"content":{"active_bundle_id":"14310A4AABD23E85BBD4559C4A3B59F8","apply_bundle_status":{"invalid_bundle":{"bundle_validation_errors":[],"invalid_bundle_id":""},"reasons_for_restart":[],"restart_required_for_apply_bundle":false,"status":"None"},"base_generation_id":26,"bucket_count":73,"bucket_count_by_index":{"_audit":24,"_internal":45,"_telemetry":4},"buckets_rf_by_origin_site":{"default":73},"buckets_sf_by_origin_site":{"default":73},"delayed_buckets_to_discard":[],"eai:acl":null,"fixup_set":[],"heartbeat_started":true,"host_port_pair":"10.36.0.6:8089","indexing_disk_space":210707374080,"is_searchable":true,"is_valid_bundle":true,"label":"splunk-stack1-indexer-0","last_dry_run_bundle":"","last_heartbeat":1584493732,"last_validated_bundle":"14310A4AABD23E85BBD4559C4A3B59F8","latest_bundle_id":"14310A4AABD23E85BBD4559C4A3B59F8","peer_registered_summaries":true,"pending_builds":[],"pending_job_count":0,"primary_count":73,"primary_count_remote":0,"register_search_address":"10.36.0.6:8089","replication_count":0,"replication_port":9887,"replication_use_ssl":false,"restart_required_for_applying_dry_run_bundle":false,"search_state_counter":{"PendingSearchable":0,"Searchable":73,"SearchablePendingMask":0,"Unsearchable":0},"site":"default","splunk_version":"8.0.2","status":"Up","status_counter":{"Complete":69,"NonStreamingTarget":0,"StreamingSource":4,"StreamingTarget":0},"summary_replication_count":0}}],"paging":{"total":1,"perPage":30,"offset":0},"messages":[]}`,
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
	method := "indexerClusterPodManager.Update(All pods ready)"
	indexerClusterPodManagerTester(t, method, mockHandlers, 1, splcommon.PhaseReady, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => decommission
	mockHandlers = append(mockHandlers, spltest.MockHTTPHandler{
		Method: "POST",
		URL:    "https://splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local:8089/services/cluster/slave/control/control/decommission?enforce_counts=0",
		Status: 200,
		Err:    nil,
		Body:   ``,
	})
	pod.ObjectMeta.Labels["controller-revision-hash"] = "v0"
	method = "indexerClusterPodManager.Update(Decommission Pod)"
	indexerClusterPodManagerTester(t, method, mockHandlers, 1, splcommon.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => wait for decommission to complete
	mockHandlers = []spltest.MockHTTPHandler{mockHandlers[0], mockHandlers[1]}
	mockHandlers[1].Body = strings.Replace(mockHandlers[1].Body, `"status":"Up"`, `"status":"ReassigningPrimaries"`, 1)
	method = "indexerClusterPodManager.Update(ReassigningPrimaries)"
	indexerClusterPodManagerTester(t, method, mockHandlers, 1, splcommon.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => wait for decommission to complete
	mockHandlers[1].Body = strings.Replace(mockHandlers[1].Body, `"status":"ReassigningPrimaries"`, `"status":"Decommissioning"`, 1)
	method = "indexerClusterPodManager.Update(Decommissioning)"
	indexerClusterPodManagerTester(t, method, mockHandlers, 1, splcommon.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => delete pod
	wantCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls, "Delete": {funcCalls[1]}}
	mockHandlers[1].Body = strings.Replace(mockHandlers[1].Body, `"status":"Decommissioning"`, `"status":"Down"`, 1)
	method = "indexerClusterPodManager.Update(Delete Pod)"
	indexerClusterPodManagerTester(t, method, mockHandlers, 1, splcommon.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test scale down => pod not found
	pod.ObjectMeta.Name = "splunk-stack1-2"
	replicas = 2
	statefulSet.Status.Replicas = 2
	statefulSet.Status.ReadyReplicas = 2
	statefulSet.Status.UpdatedReplicas = 2
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0]}}
	method = "indexerClusterPodManager.Update(Pod Not Found)"
	indexerClusterPodManagerTester(t, method, mockHandlers, 1, splcommon.PhaseScalingDown, statefulSet, wantCalls, nil, statefulSet, pod)

	// test scale down => decommission pod
	mockHandlers[1].Body = `{"entry":[{"name":"aa45bf46-7f46-47af-a760-590d5c606d10","content":{"status":"Up","label":"splunk-stack1-indexer-0"}},{"name":"D39B1729-E2C5-4273-B9B2-534DA7C2F866","content":{"status":"GracefulShutdown","label":"splunk-stack1-indexer-1"}}]}`
	mockHandlers = append(mockHandlers, spltest.MockHTTPHandler{
		Method: "POST",
		URL:    "https://splunk-stack1-cluster-master-service.test.svc.cluster.local:8089/services/cluster/master/control/control/remove_peers?peers=D39B1729-E2C5-4273-B9B2-534DA7C2F866",
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
	method = "indexerClusterPodManager.Update(Decommission)"
	indexerClusterPodManagerTester(t, method, mockHandlers, 1, splcommon.PhaseScalingDown, statefulSet, wantCalls, nil, statefulSet, pod, pvcList[0], pvcList[1])
}

func TestGetIndexerStatefulSet(t *testing.T) {
	cr := enterprisev1.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	_, err := ApplyNamespaceScopedSecretObject(c, "test")
	if err != nil {
		t.Errorf("Failed to create namespace scoped object")
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := validateIndexerClusterSpec(&cr); err != nil {
				t.Errorf("validateIndexerClusterSpec() returned error: %v", err)
			}
			return getIndexerStatefulSet(c, &cr)
		}
		configTester(t, "getIndexerStatefulSet()", f, want)
	}

	cr.Spec.Replicas = 1
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-indexer","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-indexer-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"hec","containerPort":8088,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"s2s","containerPort":9997,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_indexer"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_INDEXER_URL","value":"splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack1-cluster-master-service"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-indexer"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-indexer-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	// Define additional service port in CR and verified the statefulset has the new port
	cr.Spec.ServiceTemplate.Spec.Ports = []corev1.ServicePort{{Name: "user-defined", Port: 32000, Protocol: "UDP"}}
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-indexer","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-indexer-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"hec","containerPort":8088,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"s2s","containerPort":9997,"protocol":"TCP"},{"name":"user-defined","containerPort":32000,"protocol":"UDP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_indexer"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_INDEXER_URL","value":"splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack1-cluster-master-service"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-indexer"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-indexer-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.ServiceTemplate.Spec.Ports = nil
	cr.Spec.IndexerClusterRef.Name = "master1"
	// Multi-part differences: part-of labels / selector, secret name, SPLUNK_CLUSTER_MASTER_URL variable
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-indexer","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-master1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-master1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-indexer-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"hec","containerPort":8088,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"s2s","containerPort":9997,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_indexer"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_INDEXER_URL","value":"splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-master1-cluster-master-service"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-indexer"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-master1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-master1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-indexer-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.IndexerClusterRef.Namespace = "other"
	if err := validateIndexerClusterSpec(&cr); err == nil {
		t.Errorf("validateIndexerClusterSpec() error expected on multisite IndexerCluster referencing a cluster master located in a different namespace")
	}
}

func TestGetClusterMasterStatefulSet(t *testing.T) {
	cr := enterprisev1.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	_, err := ApplyNamespaceScopedSecretObject(c, "test")
	if err != nil {
		t.Errorf("Failed to create namespace scoped object")
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := validateIndexerClusterSpec(&cr); err != nil {
				t.Errorf("validateIndexerClusterSpec() returned error: %v", err)
			}
			return getClusterMasterStatefulSet(c, &cr)
		}
		configTester(t, fmt.Sprintf("getClusterMasterStatefulSet(replicas=%d)", cr.Spec.Replicas), f, want)
	}

	cr.Spec.Replicas = 0
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-master","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-cluster-master-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_INDEXER_URL"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"localhost"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-master"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-master-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.Replicas = 1
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-master","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-cluster-master-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_INDEXER_URL","value":"splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"localhost"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-master"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-master-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.Replicas = 2
	cr.Spec.LicenseMasterRef.Name = "stack1"
	cr.Spec.LicenseMasterRef.Namespace = "test"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-master","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-cluster-master-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_LICENSE_MASTER_URL","value":"splunk-stack1-license-master-service.test.svc.cluster.local"},{"name":"SPLUNK_INDEXER_URL","value":"splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local,splunk-stack1-indexer-1.splunk-stack1-indexer-headless.test.svc.cluster.local"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"localhost"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-master"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-master-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.Replicas = 3
	cr.Spec.LicenseMasterRef.Name = ""
	cr.Spec.LicenseURL = "/mnt/splunk.lic"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-master","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-cluster-master-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_LICENSE_URI","value":"/mnt/splunk.lic"},{"name":"SPLUNK_INDEXER_URL","value":"splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local,splunk-stack1-indexer-1.splunk-stack1-indexer-headless.test.svc.cluster.local,splunk-stack1-indexer-2.splunk-stack1-indexer-headless.test.svc.cluster.local"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"localhost"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-master"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-master-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)
}
