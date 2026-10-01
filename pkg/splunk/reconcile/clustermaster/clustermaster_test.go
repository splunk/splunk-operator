// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package clustermaster

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splstorage "github.com/splunk/splunk-operator/pkg/splunk/client/storage"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/appframework"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/telapp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	runtime "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	testStack1ClusterMasterService             = "Service-test-splunk-stack1-" + splcommon.ClusterManager + "-service"
	testStack1ClusterMasterStatefulSet         = "StatefulSet-test-splunk-stack1-" + splcommon.ClusterManager
	testStack1ClusterMasterConfigMapSmartStore = "ConfigMap-test-splunk-stack1-clustermaster-smartstore"
	testStack1ClusterMasterSmartStore          = "splunk-stack1-clustermaster-smartstore"
	testStack1ClusterMasterID                  = "splunk-stack1-" + splcommon.ClusterManager + "-%s"
)

func init() {
	splutil.GetReadinessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../../tools/k8_probes/readinessProbe.sh")
		return fileLocation
	}
	splutil.GetLivenessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../../tools/k8_probes/livenessProbe.sh")
		return fileLocation
	}
	splutil.GetStartupScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../../tools/k8_probes/startupProbe.sh")
		return fileLocation
	}
}

func TestApplyClusterMaster(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	ctx := context.TODO()
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-cluster-master-stack1-configmap"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-service"},
		{MetaName: "*v1." + testStack1ClusterMasterService},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-master"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-cluster-master-secret-v1"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermaster-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermaster-smartstore"},
		{MetaName: "*v1." + testStack1ClusterMasterStatefulSet},
		{MetaName: "*v1." + testStack1ClusterMasterStatefulSet},
		{MetaName: "*v3.ClusterMaster-test-stack1"},
		{MetaName: "*v3.ClusterMaster-test-stack1"},
	}
	updateFuncCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-cluster-master-stack1-configmap"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-service"},
		{MetaName: "*v1." + testStack1ClusterMasterService},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-master"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-cluster-master-secret-v1"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermaster-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermaster-smartstore"},
		{MetaName: "*v1." + testStack1ClusterMasterStatefulSet},
		{MetaName: "*v1." + testStack1ClusterMasterStatefulSet},
		{MetaName: "*v1." + testStack1ClusterMasterStatefulSet},
		{MetaName: "*v3.ClusterMaster-test-stack1"},
		{MetaName: "*v3.ClusterMaster-test-stack1"},
	}

	labels := map[string]string{
		"app.kubernetes.io/component":  "versionedSecrets",
		"app.kubernetes.io/managed-by": "splunk-operator",
	}
	listOpts := []runtime.ListOption{
		runtime.InNamespace("test"),
		runtime.MatchingLabels(labels),
	}
	listmockCall := []spltest.MockFuncCall{
		{ListOpts: listOpts}}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0], funcCalls[3], funcCalls[4], funcCalls[5], funcCalls[9], funcCalls[11], funcCalls[6]}, "List": {listmockCall[0]}, "Update": {funcCalls[0]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": updateFuncCalls, "Update": {funcCalls[6]}, "List": {listmockCall[0]}}

	current := enterpriseApiV3.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApiV3.ClusterMasterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}
	revised := current.DeepCopy()
	revised.Spec.CommonSplunkSpec.Image = "splunk/test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyClusterMaster(ctx, c, cr.(*enterpriseApiV3.ClusterMaster))
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyClusterMaster", &current, revised, createCalls, updateCalls, reconcile, true)

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
		_, err := ApplyClusterMaster(ctx, c, cr.(*enterpriseApiV3.ClusterMaster))
		return true, err
	}
	spltest.SplunkDeletionTester(t, revised, deleteFunc)
}

func TestGetClusterMasterStatefulSet(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApiV3.ClusterMaster{
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
			if err := validateClusterMasterSpec(ctx, c, &cr); err != nil {
				t.Errorf("validateClusterMasterSpec() returned error: %v", err)
			}
			return getClusterMasterStatefulSet(ctx, c, &cr)
		}
		spltest.ConfigTester(t, "getClusterMasterStatefulSet", f, want)
	}

	test(spltest.LoadFixture(t, "statefulset_stack1_cluster_master_base.json"))

	cr.Spec.LicenseManagerRef.Name = "stack1"
	cr.Spec.LicenseManagerRef.Namespace = "test"
	test(spltest.LoadFixture(t, "statefulset_stack1_cluster_master_base_1.json"))

	cr.Spec.LicenseManagerRef.Name = ""
	cr.Spec.LicenseURL = "/mnt/splunk.lic"
	test(spltest.LoadFixture(t, "statefulset_stack1_cluster_master_base_2.json"))

	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	test(spltest.LoadFixture(t, "statefulset_stack1_cluster_master_with_apps.json"))

	// Create a serviceaccount
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	test(spltest.LoadFixture(t, "statefulset_stack1_cluster_master_with_service_account.json"))

	// Add extraEnv
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	test(spltest.LoadFixture(t, "statefulset_stack1_cluster_master_with_service_account_1.json"))

	// Add additional label to cr metadata to transfer to the statefulset
	cr.ObjectMeta.Labels = make(map[string]string)
	cr.ObjectMeta.Labels["app.kubernetes.io/test-extra-label"] = "test-extra-label-value"
	test(spltest.LoadFixture(t, "statefulset_stack1_cluster_master_with_service_account_2.json"))
}

func TestClusterMasterSpecNotCreatedWithoutGeneralTerms(t *testing.T) {
	// Unset the SPLUNK_GENERAL_TERMS environment variable
	os.Unsetenv("SPLUNK_GENERAL_TERMS")
	ctx := context.TODO()

	// Create a mock cluster master CR
	cm := enterpriseApiV3.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApiV3.ClusterMasterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	// Create a mock client
	c := spltest.NewMockClient()

	// Attempt to apply the cluster master spec
	_, err := ApplyClusterMaster(ctx, c, &cm)

	// Assert that an error is returned
	if err == nil {
		t.Errorf("Expected error when SPLUNK_GENERAL_TERMS is not set, but got none")
	} else if !strings.Contains(err.Error(), "license not accepted") {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestApplyClusterMasterWithSmartstore(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	// Mock VerifyCMasterisMultisite to avoid 5-second HTTP timeout
	// This function tries to connect to Splunk REST API which doesn't exist in unit tests
	savedVerifyCMasterisMultisite := VerifyCMasterisMultisite
	defer func() { VerifyCMasterisMultisite = savedVerifyCMasterisMultisite }()
	VerifyCMasterisMultisite = func(ctx context.Context, cr *enterpriseApiV3.ClusterMaster, namespaceScopedSecret *corev1.Secret) ([]corev1.EnvVar, error) {
		extraEnv := resources.GetClusterMasterExtraEnv(cr)
		return extraEnv, nil
	}

	// Mock performCmasterBundlePush to avoid pod exec operations
	// When Mock=false and NeedToPushMasterApps=true, return error to simulate test expectations
	savedperformCmasterBundlePush := performCmasterBundlePush
	defer func() { performCmasterBundlePush = savedperformCmasterBundlePush }()
	performCmasterBundlePush = func(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApiV3.ClusterMaster) error {
		if !cr.Spec.CommonSplunkSpec.Mock && cr.Status.BundlePushTracker.NeedToPushMasterApps {
			return fmt.Errorf("simulated bundle push error when Mock=false")
		}
		return nil
	}

	ctx := context.TODO()
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1." + testStack1ClusterMasterConfigMapSmartStore},
		{MetaName: "*v1." + testStack1ClusterMasterConfigMapSmartStore},
		{MetaName: "*v1." + testStack1ClusterMasterConfigMapSmartStore},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-cluster-master-stack1-configmap"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-service"},
		{MetaName: "*v1.Service-test-splunk-stack1-cluster-master-service"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-master"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-cluster-master-secret-v1"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermaster-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermaster-smartstore"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-master"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-master"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-master"},
		{MetaName: "*v1.Pod-test-splunk-stack1-cluster-master-0"},
		{MetaName: "*v1.StatefulSet-test-splunk-test-monitoring-console"},
		{MetaName: "*v3.ClusterMaster-test-stack1"},
		{MetaName: "*v3.ClusterMaster-test-stack1"},
	}
	updateFuncCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1." + testStack1ClusterMasterConfigMapSmartStore},
		{MetaName: "*v1." + testStack1ClusterMasterConfigMapSmartStore},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-cluster-master-stack1-configmap"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-service"},
		{MetaName: "*v1.Service-test-splunk-stack1-cluster-master-service"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-master"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-cluster-master-secret-v1"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermaster-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermaster-smartstore"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-master"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-master"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-master"},
		{MetaName: "*v3.ClusterMaster-test-stack1"},
		{MetaName: "*v3.ClusterMaster-test-stack1"},
	}

	labels := map[string]string{
		"app.kubernetes.io/component":  "versionedSecrets",
		"app.kubernetes.io/managed-by": "splunk-operator",
	}
	listOpts := []runtime.ListOption{
		runtime.InNamespace("test"),
		runtime.MatchingLabels(labels),
	}
	listmockCall := []spltest.MockFuncCall{
		{ListOpts: listOpts}}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[7], funcCalls[8], funcCalls[9], funcCalls[13], funcCalls[15]}, "List": {listmockCall[0], listmockCall[0]}, "Update": {funcCalls[0], funcCalls[3], funcCalls[16]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": updateFuncCalls, "Update": {funcCalls[10]}, "List": {listmockCall[0]}}

	current := enterpriseApiV3.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApiV3.ClusterMasterSpec{
			SmartStore: enterpriseApi.SmartStoreSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "splunk-test-secret"},
				},

				IndexList: []enterpriseApi.IndexSpec{
					{Name: "salesdata1", RemotePath: "remotepath1",
						IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
							VolName: "msos_s2s3_vol"},
					},
					{Name: "salesdata2", RemotePath: "remotepath2",
						IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
							VolName: "msos_s2s3_vol"},
					},
					{Name: "salesdata3", RemotePath: "remotepath3",
						IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
							VolName: "msos_s2s3_vol"},
					},
				},
			},
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}
	client := spltest.NewMockClient()

	// Mock some functions for unit tests
	savedAddTelApp := telapp.AddTelApp
	defer func() { telapp.AddTelApp = savedAddTelApp }()
	telapp.AddTelApp = func(ctx context.Context, podExecClient splutil.PodExecClientImpl, replicas int32, cr splcommon.MetaObject) error {
		return nil
	}

	// Without S3 keys, ApplyClusterManager should fail
	current.Kind = "ClusterMaster"
	_, err := ApplyClusterMaster(ctx, client, &current)
	if err == nil {
		t.Errorf("ApplyClusterMaster should fail without S3 secrets configured")
	}

	// Create namespace scoped secret
	secret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Error(err.Error())
	}

	secret.Data[spltest.S3AccessKey] = []byte("abcdJDckRkxhMEdmSk5FekFRRzBFOXV6bGNldzJSWE9IenhVUy80aa")
	secret.Data[spltest.S3SecretKey] = []byte("g4NVp0a29PTzlPdGczWk1vekVUcVBSa0o4NkhBWWMvR1NadDV4YVEy")
	_, err = k8sops.ApplySecret(ctx, client, secret)
	if err != nil {
		t.Error(err.Error())
	}

	smartstoreConfigMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testStack1ClusterMasterSmartStore,
			Namespace: "test",
		},
		Data: map[string]string{"a": "b"},
	}

	revised := current.DeepCopy()
	revised.Spec.CommonSplunkSpec.Image = "splunk/test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyClusterMaster(context.Background(), c, cr.(*enterpriseApiV3.ClusterMaster))
		return err
	}

	client.AddObject(&smartstoreConfigMap)
	ss, _ := getClusterMasterStatefulSet(ctx, client, &current)
	ss.Status.ReadyReplicas = 1

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(testStack1ClusterMasterID, "0"),
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

	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyClusterMasterWithSmartstore-0", &current, revised, createCalls, updateCalls, reconcile, true, secret, &smartstoreConfigMap, ss, pod)

	current.Kind = "ClusterMaster"
	current.Status.BundlePushTracker.NeedToPushMasterApps = true
	if _, err = ApplyClusterMaster(context.Background(), client, &current); err != nil {
		t.Errorf("ApplyClusterMaster() should not have returned error")
	}

	current.Spec.CommonSplunkSpec.EtcVolumeStorageConfig.StorageCapacity = "-abcd"
	if _, err := ApplyClusterMaster(context.Background(), client, &current); err == nil {
		t.Errorf("ApplyClusterMaster() should have returned error")
	}

	var replicas int32 = 3
	current.Spec.CommonSplunkSpec.EtcVolumeStorageConfig.StorageCapacity = ""
	ss.Status.ReadyReplicas = 3
	ss.Spec.Replicas = &replicas
	ss.Spec.Template.Spec.Containers[0].Image = "splunk/splunk"
	client.AddObject(ss)
	if result, err := ApplyClusterMaster(context.Background(), client, &current); err == nil && !result.Requeue {
		t.Errorf("ApplyClusterMaster() should have returned error or result.requeue should have been false")
	}

	ss.Status.ReadyReplicas = 1
	*ss.Spec.Replicas = ss.Status.ReadyReplicas
	objects := []runtime.Object{ss, pod}
	client.AddObjects(objects)
	current.Spec.CommonSplunkSpec.Mock = false

	if _, err := ApplyClusterMaster(context.Background(), client, &current); err == nil {
		t.Errorf("ApplyClusterMaster() should have returned error")
	}
}

func TestAppFrameworkApplyClusterMasterShouldNotFail(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cm := enterpriseApiV3.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		Spec: enterpriseApiV3.ClusterMasterSpec{
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				AppsRepoPollInterval: 60,
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
		},
	}

	// create directory for app framework
	newpath := filepath.Join("/tmp", "appframework")
	_ = os.MkdirAll(newpath, os.ModePerm)
	defer os.RemoveAll(newpath)

	savedGetAppsList := appframework.GetAppsList
	defer func() { appframework.GetAppsList = savedGetAppsList }()
	appframework.GetAppsList = func(ctx context.Context, remoteDataClientMgr appframework.RemoteDataClientManager) (splcommon.RemoteDataListResponse, error) {
		remoteDataListResponse := splcommon.RemoteDataListResponse{}
		return remoteDataListResponse, nil
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

	cm.Kind = "ClusterMaster"
	_, err = ApplyClusterMaster(context.Background(), client, &cm)
	if err != nil {
		t.Errorf("ApplyClusterMaster should not have returned error here.")
	}
}

func TestApplyCLusterMasterDeletion(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cm := enterpriseApiV3.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		Spec: enterpriseApiV3.ClusterMasterSpec{
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
	configmap := spltest.GetMockPerCRConfigMap("splunk-cluster-master-stack1-configmap")
	c.AddObject(&configmap)

	// Create namespace scoped secret
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Error(err.Error())
	}

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	cm.ObjectMeta.DeletionTimestamp = &currentTime
	cm.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}

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

	// create directory for app framework
	newpath := filepath.Join("/tmp", "appframework")
	_ = os.MkdirAll(newpath, os.ModePerm)
	defer os.RemoveAll(newpath)

	savedGetAppsList := appframework.GetAppsList
	defer func() { appframework.GetAppsList = savedGetAppsList }()
	appframework.GetAppsList = func(ctx context.Context, remoteDataClientMgr appframework.RemoteDataClientManager) (splcommon.RemoteDataListResponse, error) {
		remoteDataListResponse := splcommon.RemoteDataListResponse{}
		return remoteDataListResponse, nil
	}

	cm.Kind = "ClusterMaster"
	_, err = ApplyClusterMaster(ctx, c, &cm)
	if err != nil {
		t.Errorf("ApplyClusterMaster should not have returned error here.")
	}
}
func TestClusterMasterGetAppsListForAWSS3ClientShouldNotFail(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	ctx := context.TODO()
	cm := enterpriseApiV3.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApiV3.ClusterMasterSpec{
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
						Path:      "testbucket-rs-london-2",
						SecretRef: "s3-secret",
						Type:      "s3",
						Provider:  "aws",
					},
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

	Etags := []string{"cc707187b036405f095a8ebb43a782c1", "5055a61b3d1b667a4c3279a381a2e7ae", "19779168370b97d8654424e6c9446dd8"}
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

	appFrameworkRef := cm.Spec.AppFrameworkConfig

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

		remoteDataClientMgr := appframework.NewRemoteDataClientManager(client, &cm, &cm.Spec.AppFrameworkConfig, &vol, appSource.Location,
			func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
				cl := spltest.MockAWSS3Client{}
				cl.Objects = mockAwsObjects[index].Objects
				return cl
			},
			func(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject,
				appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec,
				location string, fn splcommon.GetInitFunc) (splstorage.SplunkRemoteDataClient, error) {
				// Get the mock client
				c, err := appframework.GetRemoteStorageClient(ctx, client, cr, appFrameworkRef, vol, location, fn)
				return c, err
			})

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

func TestClusterMasterGetAppsListForAWSS3ClientShouldFail(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	ctx := context.TODO()
	cm := enterpriseApiV3.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApiV3.ClusterMasterSpec{
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

	appFrameworkRef := cm.Spec.AppFrameworkConfig

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

	remoteDataClientMgr := appframework.NewRemoteDataClientManager(client, &cm, &cm.Spec.AppFrameworkConfig, &vol, appSource.Location,
		func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
			// Purposefully return nil here so that we test the error scenario
			return nil
		},
		func(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject,
			appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec,
			location string, fn splcommon.GetInitFunc) (splstorage.SplunkRemoteDataClient, error) {
			// Get the mock client
			c, err := appframework.GetRemoteStorageClient(ctx, client, cr, appFrameworkRef, vol, location, fn)
			return c, err
		})

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
	s3Secret.Data = map[string][]byte{spltest.S3AccessKey: s3AccessKey}
	_, err = remoteDataClientMgr.GetAppsList(ctx)
	if err == nil {
		t.Errorf("GetAppsList should have returned error as S3 secret has empty s3_secret_key")
	}

	s3SecretKey := []byte{'2'}
	s3Secret.Data = map[string][]byte{spltest.S3SecretKey: s3SecretKey}
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

	remoteDataClientMgr = appframework.NewRemoteDataClientManager(client, &cm, &cm.Spec.AppFrameworkConfig, &vol, appSource.Location,
		func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
			// To test the error scenario, do no set the Objects member yet
			cl := spltest.MockAWSS3Client{}
			return cl
		},
		func(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject,
			appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec,
			location string, fn splcommon.GetInitFunc) (splstorage.SplunkRemoteDataClient, error) {
			// Get the mock client
			c, err := appframework.GetRemoteStorageClient(ctx, client, cr, appFrameworkRef, vol, location, fn)
			return c, err
		})

	remoteDataClientResponse, err := remoteDataClientMgr.GetAppsList(ctx)
	if err != nil {
		t.Errorf("GetAppsList should not have returned error since empty appSources are allowed.")
	}
	if len(remoteDataClientResponse.Objects) != 0 {
		t.Errorf("GetAppsList should return an empty response since we have empty objects in MockAWSS3Client")
	}
}

func TestClusterMasterWitReadyState(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	// Mock VerifyCMasterisMultisite to avoid 5-second HTTP timeout
	// This function tries to connect to Splunk REST API which doesn't exist in unit tests
	savedVerifyCMasterisMultisiteForReadyState := VerifyCMasterisMultisite
	defer func() { VerifyCMasterisMultisite = savedVerifyCMasterisMultisiteForReadyState }()
	VerifyCMasterisMultisite = func(ctx context.Context, cr *enterpriseApiV3.ClusterMaster, namespaceScopedSecret *corev1.Secret) ([]corev1.EnvVar, error) {
		extraEnv := resources.GetClusterMasterExtraEnv(cr)
		return extraEnv, nil
	}

	// create directory for app framework
	newpath := filepath.Join("/tmp", "appframework")
	_ = os.MkdirAll(newpath, os.ModePerm)

	// adding getapplist to fix test case
	savedGetAppsListForReadyState := appframework.GetAppsList
	defer func() { appframework.GetAppsList = savedGetAppsListForReadyState }()
	appframework.GetAppsList = func(ctx context.Context, remoteDataClientMgr appframework.RemoteDataClientManager) (splcommon.RemoteDataListResponse, error) {
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
	utilruntime.Must(enterpriseApiV3.AddToScheme(sch))

	builder := spltest.NewFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.LicenseManager{}).
		WithStatusSubresource(&enterpriseApi.ClusterManager{}).
		WithStatusSubresource(&enterpriseApi.Standalone{}).
		WithStatusSubresource(&enterpriseApi.MonitoringConsole{}).
		WithStatusSubresource(&enterpriseApi.IndexerCluster{}).
		WithStatusSubresource(&enterpriseApi.SearchHeadCluster{}).
		WithStatusSubresource(&enterpriseApiV3.ClusterMaster{}).
		WithStatusSubresource(&enterpriseApiV3.LicenseMaster{})
	c := builder.Build()
	utilruntime.Must(enterpriseApiV3.AddToScheme(clientgoscheme.Scheme))
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

	// create clustermaster custom resource
	clustermaster := &enterpriseApiV3.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: enterpriseApiV3.ClusterMasterSpec{
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

	replicas := int32(1)
	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-cluster-master",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: "splunk-test-cluster-master-headless",
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
			Name:      "splunk-test-cluster-master-headless",
			Namespace: "default",
		},
	}

	// simulate service
	c.Create(ctx, service)

	// simulate create stateful set
	c.Create(ctx, statefulset)

	// simulate create clustermaster instance before reconciliation
	c.Create(ctx, clustermaster)

	_, err := ApplyClusterMaster(ctx, c, clustermaster)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for clustermaster with app framework  %v", err)
		debug.PrintStack()
	}
	namespacedName := types.NamespacedName{
		Name:      clustermaster.Name,
		Namespace: clustermaster.Namespace,
	}
	err = c.Get(ctx, namespacedName, clustermaster)
	if err != nil {
		t.Errorf("Unexpected get cluster master %v", err)
		debug.PrintStack()
	}
	// simulate Ready state
	clustermaster.Status.Phase = enterpriseApi.PhaseReady
	clustermaster.Spec.ServiceTemplate.Annotations = map[string]string{
		"traffic.sidecar.istio.io/excludeOutboundPorts": "8089,8191,9997",
		"traffic.sidecar.istio.io/includeInboundPorts":  "8000,8088",
	}
	clustermaster.Spec.ServiceTemplate.Labels = map[string]string{
		"app.kubernetes.io/instance":   "splunk-test-cluster-master",
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "cluster-master",
		"app.kubernetes.io/name":       "cluster-master",
		"app.kubernetes.io/part-of":    "splunk-test-cluster-master",
	}
	err = c.Status().Update(ctx, clustermaster)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for cluster master with app framework  %v", err)
		debug.PrintStack()
	}

	err = c.Get(ctx, namespacedName, clustermaster)
	if err != nil {
		t.Errorf("Unexpected get cluster master %v", err)
		debug.PrintStack()
	}

	// call reconciliation
	_, err = ApplyClusterMaster(ctx, c, clustermaster)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for cluster master with app framework  %v", err)
		debug.PrintStack()
	}

	// create pod
	stpod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-cluster-master-0",
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
		Name:      "splunk-test-cluster-master",
		Namespace: "default",
	}
	err = c.Get(ctx, stNamespacedName, statefulset)
	if err != nil {
		t.Errorf("Unexpected get cluster manager %v", err)
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

	err = c.Get(ctx, namespacedName, clustermaster)
	if err != nil {
		t.Errorf("Unexpected get cluster manager %v", err)
		debug.PrintStack()
	}

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
	err = k8sops.SetStatefulSetOwnerRef(ctx, c, clustermaster, namespacedName)
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
	_, err = ApplyClusterMaster(ctx, c, clustermaster)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for cluster manager with app framework  %v", err)
		debug.PrintStack()
	}
}
