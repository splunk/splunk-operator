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

package monitoringconsole

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splstorage "github.com/splunk/splunk-operator/pkg/splunk/client/storage"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/appframework"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clienttesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	reconcile "sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func init() {
	// The tests run from this package directory, so resolve probe fixtures from
	// the repository root as the standalone/indexer tests do.
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

type remoteDataClientManager struct {
	client              splcommon.ControllerClient
	cr                  splcommon.MetaObject
	appFrameworkRef     *enterpriseApi.AppFrameworkSpec
	vol                 *enterpriseApi.VolumeSpec
	location            string
	initFn              splcommon.GetInitFunc
	getRemoteDataClient func(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec, location string, fn splcommon.GetInitFunc) (splstorage.SplunkRemoteDataClient, error)
}

func (m *remoteDataClientManager) GetAppsList(ctx context.Context) (splcommon.RemoteDataListResponse, error) {
	c, err := m.getRemoteDataClient(ctx, m.client, m.cr, m.appFrameworkRef, m.vol, m.location, m.initFn)
	if err != nil {
		return splcommon.RemoteDataListResponse{}, err
	}
	return c.Client.GetAppsList(ctx)
}

func TestMonitoringConsoleGetAppsListForAWSS3ClientShouldNotFail(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.MonitoringConsole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "monitoringConsole",
			Namespace: "test",
		},
		Spec: enterpriseApi.MonitoringConsoleSpec{
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
	Etags := []string{"cc707187b036405f095a8ebb43a782c1", "5055a61b3d1b667a4c3279a381a2e7ae", "19779168370b97d8654424e6c9446dd9"}
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
	appFrameworkRef := cr.Spec.AppFrameworkConfig
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
		remoteDataClientMgr := &remoteDataClientManager{client: client,
			cr: &cr, appFrameworkRef: &cr.Spec.AppFrameworkConfig,
			vol:      &vol,
			location: appSource.Location,
			initFn: func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
				cl := spltest.MockAWSS3Client{}
				cl.Objects = mockAwsObjects[index].Objects
				return cl
			},
			getRemoteDataClient: func(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec, location string, fn splcommon.GetInitFunc) (splstorage.SplunkRemoteDataClient, error) {
				c, err := appframework.GetRemoteStorageClient(ctx, client, cr, appFrameworkRef, vol, location, fn)
				return c, err
			},
		}
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

func TestMonitoringConsoleGetAppsListForAWSS3ClientShouldFail(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.MonitoringConsole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.MonitoringConsoleSpec{
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
	appFrameworkRef := cr.Spec.AppFrameworkConfig
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
	remoteDataClientMgr := &remoteDataClientManager{
		client:          client,
		cr:              &cr,
		appFrameworkRef: &cr.Spec.AppFrameworkConfig,
		vol:             &vol,
		location:        appSource.Location,
		initFn: func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
			// Purposefully return nil here so that we test the error scenario
			return nil
		},
		getRemoteDataClient: func(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject,
			appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec,
			location string, fn splcommon.GetInitFunc) (splstorage.SplunkRemoteDataClient, error) {
			// Get the mock client
			c, err := appframework.GetRemoteStorageClient(ctx, client, cr, appFrameworkRef, vol, location, fn)
			return c, err
		},
	}
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
	s3Secret.Data = map[string][]byte{"s3_access_key": s3AccessKey}
	_, err = remoteDataClientMgr.GetAppsList(ctx)
	if err == nil {
		t.Errorf("GetAppsList should have returned error as S3 secret has empty s3_secret_key")
	}
	s3SecretKey := []byte{'2'}
	s3Secret.Data = map[string][]byte{"s3_secret_key": s3SecretKey}
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
	remoteDataClientMgr.initFn = func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
		// To test the error scenario, do no set the Objects member yet
		cl := spltest.MockAWSS3Client{}
		return cl
	}
	remoteDataClientResponse, err := remoteDataClientMgr.GetAppsList(ctx)
	if err != nil {
		t.Errorf("GetAppsList should not have returned error since empty appSources are allowed.")
	}
	if len(remoteDataClientResponse.Objects) != 0 {
		t.Errorf("GetAppsList should return an empty response since we have empty objects in MockAWSS3Client")
	}
}

func newFakeClientBuilder(scheme *pkgruntime.Scheme) *fake.ClientBuilder {
	// The controller-runtime fake client defaults to a managed-fields tracker,
	// which rejects the uint64 fields used by Splunk CR specs.
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjectTracker(clienttesting.NewObjectTracker(
			scheme,
			serializer.NewCodecFactory(scheme).UniversalDecoder(),
		)).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				err := c.Get(ctx, key, obj, opts...)
				if err != nil {
					return err
				}
				gvk, err := apiutil.GVKForObject(obj, scheme)
				if err == nil {
					obj.GetObjectKind().SetGroupVersionKind(gvk)
				}
				return nil
			},
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				gvk := obj.GetObjectKind().GroupVersionKind()
				err := c.Create(ctx, obj, opts...)
				obj.GetObjectKind().SetGroupVersionKind(gvk)
				return err
			},
		})
}

func TestGetMonitoringConsoleStatefulSet(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.Background()
	cr := enterpriseApi.MonitoringConsole{ObjectMeta: metav1.ObjectMeta{Name: "stack1", Namespace: "test"}}
	c := spltest.NewMockClient()
	if _, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test"); err != nil {
		t.Fatalf("failed to create namespace scoped object: %v", err)
	}
	c.AddObject(&enterpriseApi.ClusterManager{ObjectMeta: metav1.ObjectMeta{Name: "stack2", Namespace: "test"}})

	test := func(want string) {
		t.Helper()
		f := func() (interface{}, error) {
			if err := validateMonitoringConsoleSpec(ctx, c, &cr); err != nil {
				t.Errorf("validateMonitoringConsoleSpec() returned error: %v", err)
			}
			return getMonitoringConsoleStatefulSet(ctx, c, &cr)
		}
		configTester(t, "getMonitoringConsoleStatefulSet()", f, want)
	}

	test(loadFixture(t, "statefulset_stack1_monitoring_console_with_defaults.json"))
	cr.Spec.EtcVolumeStorageConfig.EphemeralStorage = true
	cr.Spec.VarVolumeStorageConfig.EphemeralStorage = true
	test(loadFixture(t, "statefulset_stack1_monitoring_console_with_apps.json"))
	cr.Spec.EtcVolumeStorageConfig.EphemeralStorage = false
	cr.Spec.VarVolumeStorageConfig.EphemeralStorage = false
	cr.Spec.ClusterManagerRef.Name = "stack2"
	cr.Spec.EtcVolumeStorageConfig.StorageClassName = "gp2"
	cr.Spec.VarVolumeStorageConfig.StorageClassName = "gp2"
	cr.Spec.SchedulerName = "custom-scheduler"
	cr.Spec.Defaults = "defaults-string"
	cr.Spec.DefaultsURL = "/mnt/defaults/defaults.yml"
	cr.Spec.Volumes = []corev1.Volume{{Name: "defaults"}}
	test(loadFixture(t, "statefulset_stack1_monitoring_console_with_service_account_2.json"))
	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	test(loadFixture(t, "statefulset_stack1_monitoring_console_base.json"))

	current := corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "defaults", Namespace: "test"}}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	test(loadFixture(t, "statefulset_stack1_monitoring_console_base_1.json"))
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{{Name: "TEST_ENV_VAR", Value: "test_value"}}
	test(loadFixture(t, "statefulset_stack1_monitoring_console_with_service_account.json"))
	cr.ObjectMeta.Labels = map[string]string{"app.kubernetes.io/test-extra-label": "test-extra-label-value"}
	test(loadFixture(t, "statefulset_stack1_monitoring_console_with_service_account_1.json"))
}

func configTester(t *testing.T, method string, f func() (interface{}, error), want string) {
	t.Helper()
	result, err := f()
	require.NoError(t, err, method)
	got, err := json.Marshal(result)
	require.NoError(t, err, method)
	require.JSONEq(t, normalizeGeneratedConfigJSON(t, want), normalizeGeneratedConfigJSON(t, string(got)), method)
}

func normalizeGeneratedConfigJSON(t *testing.T, data string) string {
	t.Helper()
	var value interface{}
	require.NoError(t, json.Unmarshal([]byte(data), &value))
	dropNilCreationTimestamp(value)
	normalized, err := json.Marshal(value)
	require.NoError(t, err)
	return string(normalized)
}

func dropNilCreationTimestamp(value interface{}) {
	switch typed := value.(type) {
	case map[string]interface{}:
		if creationTimestamp, ok := typed["creationTimestamp"]; ok && creationTimestamp == nil {
			delete(typed, "creationTimestamp")
		}
		for _, child := range typed {
			dropNilCreationTimestamp(child)
		}
	case []interface{}:
		for _, child := range typed {
			dropNilCreationTimestamp(child)
		}
	}
}

func loadFixture(t *testing.T, filename string) string {
	t.Helper()
	path := filepath.Join("testdata", "fixtures", filename)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "failed to load fixture %s", filename)

	var compactJSON bytes.Buffer
	require.NoError(t, json.Compact(&compactJSON, data), "failed to compact fixture %s", filename)
	return compactJSON.String()
}

func TestApplyPaused(t *testing.T) {
	ctx := context.Background()
	scheme := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(enterpriseApi.AddToScheme(scheme))

	c := newFakeClientBuilder(scheme).
		WithStatusSubresource(&enterpriseApi.MonitoringConsole{}).
		Build()
	cr := &enterpriseApi.MonitoringConsole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "monitoring-console",
			Namespace: "test",
			Annotations: map[string]string{
				enterpriseApi.MonitoringConsolePausedAnnotation: "true",
			},
		},
	}
	if err := c.Create(ctx, cr); err != nil {
		t.Fatalf("failed to create MonitoringConsole: %v", err)
	}

	result, err := Apply(ctx, c, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, nil)
	if err != nil {
		t.Fatalf("Apply() returned error: %v", err)
	}
	if !result.Requeue || result.RequeueAfter != splcommon.PauseRetryDelay {
		t.Fatalf("Apply() result = %+v; want requeue after %s", result, splcommon.PauseRetryDelay)
	}

	updated := &enterpriseApi.MonitoringConsole{}
	if err := c.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, updated); err != nil {
		t.Fatalf("failed to get updated MonitoringConsole: %v", err)
	}
	paused := meta.FindStatusCondition(updated.Status.Conditions, string(enterpriseApi.ConditionPaused))
	if paused == nil {
		t.Fatal("expected Paused condition")
	}
	if paused.Status != metav1.ConditionTrue {
		t.Fatalf("Paused condition status = %s; want %s", paused.Status, metav1.ConditionTrue)
	}
}

func TestApplyMonitoringConsole(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-monitoring-console-stack1-configmap"},
		{MetaName: "*v1.Service-test-splunk-stack1-monitoring-console-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-monitoring-console-service"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-monitoring-console"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-monitoring-console-secret-v1"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-monitoring-console"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-monitoring-console"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-monitoring-console"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-monitoring-console"},
		{MetaName: "*v4.MonitoringConsole-test-stack1"},
		{MetaName: "*v4.MonitoringConsole-test-stack1"},
	}
	updateFuncCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-monitoring-console-stack1-configmap"},
		{MetaName: "*v1.Service-test-splunk-stack1-monitoring-console-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-monitoring-console-service"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-monitoring-console"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-monitoring-console-secret-v1"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-monitoring-console"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-monitoring-console"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-monitoring-console"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-monitoring-console"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-monitoring-console"},
		{MetaName: "*v4.MonitoringConsole-test-stack1"},
		{MetaName: "*v4.MonitoringConsole-test-stack1"},
	}
	labels := map[string]string{
		"app.kubernetes.io/component":  "versionedSecrets",
		"app.kubernetes.io/managed-by": "splunk-operator",
	}
	listOpts := []client.ListOption{
		client.InNamespace("test"),
		client.MatchingLabels(labels),
	}
	listOpts2 := []client.ListOption{
		client.InNamespace("test"),
	}
	listmockCall := []spltest.MockFuncCall{
		{ListOpts: listOpts},
		{ListOpts: listOpts2},
	}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0], funcCalls[3], funcCalls[4], funcCalls[5], funcCalls[9], funcCalls[11], funcCalls[12], funcCalls[6]}, "Update": {funcCalls[0], funcCalls[12]}, "List": {listmockCall[0]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": updateFuncCalls, "Update": {updateFuncCalls[5]}, "List": {listmockCall[0]}}
	current := enterpriseApi.MonitoringConsole{
		TypeMeta: metav1.TypeMeta{
			Kind: "MonitoringConsole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	revised := current.DeepCopy()
	revised.Spec.Image = "splunk/test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyMonitoringConsole(context.Background(), c, cr.(*enterpriseApi.MonitoringConsole))
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyMonitoringConsole", &current, revised, createCalls, updateCalls, reconcile, true)
}

func TestAppFrameworkApplyMonitoringConsoleShouldNotFail(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.MonitoringConsole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "monitoringConsole",
			Namespace: "test",
		},
		Spec: enterpriseApi.MonitoringConsoleSpec{
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
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Error(err.Error())
	}
	// Create S3 secret
	s3Secret := spltest.GetMockS3SecretKeys("s3-secret")
	client.AddObject(&s3Secret)
	configmap := spltest.GetMockPerCRConfigMap("splunk-monitoring-console-monitoringConsole-configmap")
	client.AddObject(&configmap)
	// to pass the validation stage, add the directory to download apps
	_ = os.MkdirAll(splcommon.AppDownloadVolume, 0755)
	defer os.RemoveAll(splcommon.AppDownloadVolume)
	_, err = ApplyMonitoringConsole(ctx, client, &cr)
	if err != nil {
		t.Errorf("ApplyMonitoringConsole should be successful")
	}
}

func TestMonitoringConsoleSpecNotCreatedWithoutGeneralTerms(t *testing.T) {
	// Unset the SPLUNK_GENERAL_TERMS environment variable
	os.Unsetenv("SPLUNK_GENERAL_TERMS")
	ctx := context.TODO()
	// Create a mock monitoring console CR
	mc := enterpriseApi.MonitoringConsole{
		TypeMeta: metav1.TypeMeta{
			Kind: "MonitoringConsole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	// Create a mock client
	c := spltest.NewMockClient()
	// Attempt to apply the monitoring console spec
	_, err := ApplyMonitoringConsole(ctx, c, &mc)
	// SPLUNK_GENERAL_TERMS unset is a stalled misconfiguration: reconciler returns terminal error (no requeue)
	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Errorf("stalled spec validation failure should return a terminal error, got %v", err)
	}
}

func TestMonitoringConsoleWithReadyState(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
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
	ctx := context.TODO()
	// create monitoringconsole custom resource
	monitoringconsole := &enterpriseApi.MonitoringConsole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: enterpriseApi.MonitoringConsoleSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				Volumes: []corev1.Volume{},
			},
		},
	}
	replicas := int32(1)
	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: "splunk-test-monitoring-console-headless",
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
			Name:      "splunk-test-monitoring-console-headless",
			Namespace: "default",
		},
	}
	// simulate service
	c.Create(ctx, service)
	// simulate create stateful set
	c.Create(ctx, statefulset)
	// simulate create clustermanager instance before reconciliation
	c.Create(ctx, monitoringconsole)
	_, err := ApplyMonitoringConsole(ctx, c, monitoringconsole)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for indexer cluster %v", err)
		debug.PrintStack()
	}
	namespacedName := types.NamespacedName{
		Name:      monitoringconsole.Name,
		Namespace: monitoringconsole.Namespace,
	}
	err = c.Get(ctx, namespacedName, monitoringconsole)
	if err != nil {
		t.Errorf("Unexpected get monitoring console. Error = %v", err)
		debug.PrintStack()
	}
	// simulate Ready state
	monitoringconsole.Status.Phase = enterpriseApi.PhaseReady
	monitoringconsole.Spec.ServiceTemplate.Annotations = map[string]string{
		"traffic.sidecar.istio.io/excludeOutboundPorts": "8089,8191,9997",
		"traffic.sidecar.istio.io/includeInboundPorts":  "8000,8088",
	}
	monitoringconsole.Spec.ServiceTemplate.Labels = map[string]string{
		"app.kubernetes.io/instance":   "splunk-test-monitoring-console",
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "monitoring-console",
		"app.kubernetes.io/name":       "monitoring-console",
		"app.kubernetes.io/part-of":    "splunk-test-monitoring-console",
	}
	err = c.Status().Update(ctx, monitoringconsole)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for cluster master with app framework  %v", err)
		debug.PrintStack()
	}
	err = c.Get(ctx, namespacedName, monitoringconsole)
	if err != nil {
		t.Errorf("Unexpected get monitoring console %v", err)
		debug.PrintStack()
	}
	// call reconciliation
	_, err = ApplyMonitoringConsole(ctx, c, monitoringconsole)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for monitoring console with app framework  %v", err)
		debug.PrintStack()
	}
	// create pod
	stpod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console-0",
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
		Name:      "splunk-test-monitoring-console",
		Namespace: "default",
	}
	err = c.Get(ctx, stNamespacedName, statefulset)
	if err != nil {
		t.Errorf("Unexpected get monitoring console %v", err)
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
	err = c.Get(ctx, namespacedName, monitoringconsole)
	if err != nil {
		t.Errorf("Unexpected get monitoring console %v", err)
		debug.PrintStack()
	}
	// call reconciliation
	_, err = ApplyMonitoringConsole(ctx, c, monitoringconsole)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for monitoring console with app framework  %v", err)
		debug.PrintStack()
	}
}

func TestApplyMonitoringConsoleDeletion(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	mc := enterpriseApi.MonitoringConsole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "monitoring-console",
		},
		Spec: enterpriseApi.MonitoringConsoleSpec{
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
				Mock: true,
			},
		},
	}
	c := spltest.NewMockClient()
	// Create S3 secret
	s3Secret := spltest.GetMockS3SecretKeys("s3-secret")
	c.AddObject(&s3Secret)
	configmap := spltest.GetMockPerCRConfigMap("splunk-monitoring-console-stack1-configmap")
	c.AddObject(&configmap)
	// Create namespace scoped secret
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Error(err.Error())
	}
	// test deletion
	currentTime := metav1.NewTime(time.Now())
	mc.ObjectMeta.DeletionTimestamp = &currentTime
	mc.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
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
	_, err = ApplyMonitoringConsole(ctx, c, &mc)
	if err != nil {
		t.Errorf("ApplyMonitoringConsole should not have returned error here.")
	}
}
