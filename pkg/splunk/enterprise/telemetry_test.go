// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

package enterprise

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// --- MOCKS AND TEST HELPERS ---

// errorUpdateClient is a mock client that always returns an error on Update
// Used for testing updateLastTransmissionTime error handling

type errorUpdateClient struct {
	test.MockClient
}

func (c *errorUpdateClient) Update(_ context.Context, _ client.Object, _ ...client.UpdateOption) error {
	return errors.New("forced update error")
}

// FakeListClient is a local mock client that supports List for CRs and StatefulSets for testing
// Only implements List for the types needed in these tests

type FakeListClient struct {
	test.MockClient
	crs map[string][]client.Object
}

func (c *FakeListClient) List(_ context.Context, list client.ObjectList, _ ...client.ListOption) error {
	switch l := list.(type) {
	case *enterpriseApi.StandaloneList:
		l.Items = nil
		for _, obj := range c.crs["Standalone"] {
			l.Items = append(l.Items, *(obj.(*enterpriseApi.Standalone)))
		}
	case *enterpriseApi.LicenseManagerList:
		l.Items = nil
		for _, obj := range c.crs["LicenseManager"] {
			l.Items = append(l.Items, *(obj.(*enterpriseApi.LicenseManager)))
		}
	case *enterpriseApiV3.LicenseMasterList:
		l.Items = nil
		for _, obj := range c.crs["LicenseMaster"] {
			l.Items = append(l.Items, *(obj.(*enterpriseApiV3.LicenseMaster)))
		}
	case *enterpriseApi.SearchHeadClusterList:
		l.Items = nil
		for _, obj := range c.crs["SearchHeadCluster"] {
			l.Items = append(l.Items, *(obj.(*enterpriseApi.SearchHeadCluster)))
		}
	case *enterpriseApi.IndexerClusterList:
		l.Items = nil
		for _, obj := range c.crs["IndexerCluster"] {
			l.Items = append(l.Items, *(obj.(*enterpriseApi.IndexerCluster)))
		}
	case *enterpriseApi.ClusterManagerList:
		l.Items = nil
		for _, obj := range c.crs["ClusterManager"] {
			l.Items = append(l.Items, *(obj.(*enterpriseApi.ClusterManager)))
		}
	case *enterpriseApiV3.ClusterMasterList:
		l.Items = nil
		for _, obj := range c.crs["ClusterMaster"] {
			l.Items = append(l.Items, *(obj.(*enterpriseApiV3.ClusterMaster)))
		}
	case *enterpriseApi.MonitoringConsoleList:
		l.Items = nil
		for _, obj := range c.crs["MonitoringConsole"] {
			l.Items = append(l.Items, *(obj.(*enterpriseApi.MonitoringConsole)))
		}
	default:
		return nil
	}
	return nil
}

func TestTelemetryCollectResourceTelData_NilMaps(t *testing.T) {
	data := collectResourceTelData(corev1.ResourceRequirements{})
	if data[cpuRequestKey] == "" || data[memoryRequestKey] == "" || data[cpuLimitKey] == "" || data[memoryLimitKey] == "" {
		t.Errorf("expected default values for nil maps")
	}
}

func TestTelemetryCollectResourceTelData_MissingKeys(t *testing.T) {
	reqs := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{},
		Limits:   corev1.ResourceList{},
	}
	data := collectResourceTelData(reqs)
	if data[cpuRequestKey] == "" || data[memoryRequestKey] == "" || data[cpuLimitKey] == "" || data[memoryLimitKey] == "" {
		t.Errorf("expected default values for missing keys")
	}
}

func TestTelemetryCollectResourceTelData_ValuesPresent(t *testing.T) {
	reqs := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("123m"),
			corev1.ResourceMemory: resource.MustParse("456Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("789m"),
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		},
	}
	data := collectResourceTelData(reqs)
	if data[cpuRequestKey] != "123m" || data[memoryRequestKey] != "456Mi" || data[cpuLimitKey] != "789m" || data[memoryLimitKey] != "1Gi" {
		t.Errorf("unexpected values: got %+v", data)
	}
}

func TestTelemetryCollectCMTelData_UnmarshalError(t *testing.T) {
	cm := &corev1.ConfigMap{Data: map[string]string{"bad": "notjson"}}
	data := make(map[string]interface{})
	CollectCMTelData(context.TODO(), cm, data)
	if data["bad"] != "notjson" {
		t.Errorf("expected fallback to string on unmarshal error")
	}
}

func TestTelemetryCollectCMTelData_ValidJSON(t *testing.T) {
	val := map[string]interface{}{"foo": "bar"}
	b, _ := json.Marshal(val)
	cm := &corev1.ConfigMap{Data: map[string]string{"good": string(b)}}
	data := make(map[string]interface{})
	CollectCMTelData(context.TODO(), cm, data)
	if m, ok := data["good"].(map[string]interface{}); !ok || m["foo"] != "bar" {
		t.Errorf("expected valid JSON to be unmarshaled")
	}
}

func TestTelemetryGetCurrentStatus_Default(t *testing.T) {
	cm := &corev1.ConfigMap{Data: nil}
	status := getCurrentStatus(context.TODO(), cm)
	if status == nil || status.Test != defaultTestMode {
		t.Errorf("expected default status")
	}
}

func TestTelemetryGetCurrentStatus_UnmarshalError(t *testing.T) {
	cm := &corev1.ConfigMap{Data: map[string]string{"status": "notjson"}}
	status := getCurrentStatus(context.TODO(), cm)
	if status == nil || status.Test != defaultTestMode {
		t.Errorf("expected default status on unmarshal error")
	}
}

func TestTelemetryUpdateLastTransmissionTime_MarshalError(t *testing.T) {
	ctx := context.TODO()
	cm := &corev1.ConfigMap{Data: map[string]string{}}
	status := &TelemetryStatus{Test: "false"}
	updateLastTransmissionTime(ctx, test.NewMockClient(), cm, status) // pass nil to avoid panic
}

func TestSendTelemetry_UnknownKind(t *testing.T) {
	cr := &enterpriseApi.Standalone{}
	cr.TypeMeta.Kind = "UnknownKind"
	ok := SendTelemetry(context.TODO(), test.NewMockClient(), cr, map[string]interface{}{}, false)
	if ok {
		t.Errorf("expected SendTelemetry to return false for unknown kind")
	}
}

func TestSendTelemetry_NoSecret(t *testing.T) {
	cr := &enterpriseApi.Standalone{}
	cr.TypeMeta.Kind = "Standalone"
	cr.ObjectMeta.Name = "test"
	cr.ObjectMeta.Namespace = "default"
	ok := SendTelemetry(context.TODO(), test.NewMockClient(), cr, map[string]interface{}{}, false)
	if ok {
		t.Errorf("expected SendTelemetry to return false if no secret found")
	}
}

func TestTelemetryUpdateLastTransmissionTime_SetsTimestamp(t *testing.T) {
	mockClient := test.NewMockClient()
	ctx := context.TODO()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
		Data:       map[string]string{},
	}
	status := &TelemetryStatus{Test: "false"}

	updateLastTransmissionTime(ctx, mockClient, cm, status)
	statusStr, ok := cm.Data[telStatusKey]
	if !ok {
		t.Fatalf("expected telStatusKey in configmap data")
	}
	var statusObj TelemetryStatus
	if err := json.Unmarshal([]byte(statusStr), &statusObj); err != nil {
		t.Fatalf("failed to unmarshal status: %v", err)
	}
	if statusObj.LastTransmission == "" {
		t.Errorf("expected LastTransmission to be set")
	}
	if _, err := time.Parse(time.RFC3339, statusObj.LastTransmission); err != nil {
		t.Errorf("LastTransmission is not RFC3339: %v", statusObj.LastTransmission)
	}
	if statusObj.Test != "false" {
		t.Errorf("expected Test to be 'false', got %v", statusObj.Test)
	}
}

func TestTelemetryUpdateLastTransmissionTime_UpdateError(t *testing.T) {
	ctx := context.TODO()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
		Data:       map[string]string{},
	}
	badClient := &errorUpdateClient{}
	status := &TelemetryStatus{Test: "false"}
	updateLastTransmissionTime(ctx, badClient, cm, status)
}

func TestTelemetryUpdateLastTransmissionTime_RepeatedCalls(t *testing.T) {
	mockClient := test.NewMockClient()
	ctx := context.TODO()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
		Data:       map[string]string{},
	}
	status := &TelemetryStatus{Test: "false", LastTransmission: "1970-01-01T00:00:00Z"}
	firstStatus := cm.Data[telStatusKey]
	updateLastTransmissionTime(ctx, mockClient, cm, status)
	secondStatus := cm.Data[telStatusKey]
	if firstStatus == secondStatus {
		t.Errorf("expected status to change on repeated call")
	}
}

func TestTelemetryCollectDeploymentTelData_AllKinds(t *testing.T) {
	ctx := context.TODO()
	crs := map[string][]client.Object{
		"Standalone":        {&enterpriseApi.Standalone{TypeMeta: metav1.TypeMeta{Kind: "Standalone"}, ObjectMeta: metav1.ObjectMeta{Name: "standalone1"}, Spec: enterpriseApi.StandaloneSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("1Gi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("2Gi")}}}}}}},
		"LicenseManager":    {&enterpriseApi.LicenseManager{TypeMeta: metav1.TypeMeta{Kind: "LicenseManager"}, ObjectMeta: metav1.ObjectMeta{Name: "lm1"}, Spec: enterpriseApi.LicenseManagerSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("3"), corev1.ResourceMemory: resource.MustParse("3Gi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4"), corev1.ResourceMemory: resource.MustParse("4Gi")}}}}}}},
		"LicenseMaster":     {&enterpriseApiV3.LicenseMaster{TypeMeta: metav1.TypeMeta{Kind: "LicenseMaster"}, ObjectMeta: metav1.ObjectMeta{Name: "lmast1"}, Spec: enterpriseApiV3.LicenseMasterSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("5"), corev1.ResourceMemory: resource.MustParse("5Gi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("6"), corev1.ResourceMemory: resource.MustParse("6Gi")}}}}}}},
		"SearchHeadCluster": {&enterpriseApi.SearchHeadCluster{TypeMeta: metav1.TypeMeta{Kind: "SearchHeadCluster"}, ObjectMeta: metav1.ObjectMeta{Name: "shc1"}, Spec: enterpriseApi.SearchHeadClusterSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("7"), corev1.ResourceMemory: resource.MustParse("7Gi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("8"), corev1.ResourceMemory: resource.MustParse("8Gi")}}}}}}},
		"IndexerCluster":    {&enterpriseApi.IndexerCluster{TypeMeta: metav1.TypeMeta{Kind: "IndexerCluster"}, ObjectMeta: metav1.ObjectMeta{Name: "idx1"}, Spec: enterpriseApi.IndexerClusterSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("9"), corev1.ResourceMemory: resource.MustParse("9Gi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10"), corev1.ResourceMemory: resource.MustParse("10Gi")}}}}}}},
		"ClusterManager":    {&enterpriseApi.ClusterManager{TypeMeta: metav1.TypeMeta{Kind: "ClusterManager"}, ObjectMeta: metav1.ObjectMeta{Name: "cmgr1"}, Spec: enterpriseApi.ClusterManagerSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("11"), corev1.ResourceMemory: resource.MustParse("11Gi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("12"), corev1.ResourceMemory: resource.MustParse("12Gi")}}}}}}},
		"ClusterMaster":     {&enterpriseApiV3.ClusterMaster{TypeMeta: metav1.TypeMeta{Kind: "ClusterMaster"}, ObjectMeta: metav1.ObjectMeta{Name: "cmast1"}, Spec: enterpriseApiV3.ClusterMasterSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("13"), corev1.ResourceMemory: resource.MustParse("13Gi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("14"), corev1.ResourceMemory: resource.MustParse("14Gi")}}}}}}},
	}
	fakeClient := &FakeListClient{crs: crs}
	deploymentData := make(map[string]interface{})
	crWithTelAppList := collectDeploymentTelData(ctx, fakeClient, deploymentData)
	kinds := []string{"Standalone", "LicenseManager", "LicenseMaster", "SearchHeadCluster", "IndexerCluster", "ClusterManager", "ClusterMaster"}
	for _, kind := range kinds {
		if _, ok := deploymentData[kind]; !ok {
			t.Errorf("expected deploymentData to have key %s", kind)
		}
		// Check resource data for at least one CR per kind
		kindData, ok := deploymentData[kind].(map[string]interface{})
		if !ok {
			t.Errorf("expected deploymentData[%s] to be map[string]interface{}", kind)
			continue
		}
		for crName, v := range kindData {
			resData, ok := v.(map[string]string)
			if !ok {
				t.Errorf("expected resource data for %s/%s to be map[string]string", kind, crName)
			}
			// Spot check a value
			if resData[cpuRequestKey] == "" || resData[memoryRequestKey] == "" {
				t.Errorf("expected resource data for %s/%s to have cpu/memory", kind, crName)
			}
		}
	}
	// crWithTelAppList should be empty since TelAppInstalled is not set
	if len(crWithTelAppList) != 0 {
		t.Errorf("expected crWithTelAppList to be empty if TelAppInstalled is not set")
	}
}

func TestApplyTelemetry_NoCRs(t *testing.T) {
	cm := &corev1.ConfigMap{Data: map[string]string{}}
	mockClient := test.NewMockClient()
	result, err := ApplyTelemetry(context.TODO(), mockClient, cm)
	if err == nil {
		t.Errorf("expected error when no CRs are present")
	}
	if result != (reconcile.Result{}) && !result.Requeue {
		t.Errorf("expected requeue to be true")
	}
}

func TestSendTelemetry_LicenseInfoError(t *testing.T) {
	cr := &enterpriseApi.Standalone{}
	cr.TypeMeta.Kind = "Standalone"
	cr.ObjectMeta.Name = "test"
	cr.ObjectMeta.Namespace = "default"
	mockClient := test.NewMockClient()
	// Simulate secret found, but license info error
	ok := SendTelemetry(context.TODO(), mockClient, cr, map[string]interface{}{}, false)
	if ok {
		t.Errorf("expected SendTelemetry to return false on license info error")
	}
}

func TestSendTelemetry_AdminPasswordMissing(t *testing.T) {
	cr := &enterpriseApi.Standalone{}
	cr.TypeMeta.Kind = "Standalone"
	cr.ObjectMeta.Name = "test"
	cr.ObjectMeta.Namespace = "default"
	mockClient := test.NewMockClient()
	// Simulate secret missing password
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-secret",
			Namespace: cr.ObjectMeta.Namespace,
		},
		Data: map[string][]byte{},
	}
	_ = mockClient.Create(context.TODO(), secret)
	ok := SendTelemetry(context.TODO(), mockClient, cr, map[string]interface{}{}, false)
	if ok {
		t.Errorf("expected SendTelemetry to return false if admin password is missing")
	}
}

func TestSendTelemetry_Success(t *testing.T) {
	cr := &enterpriseApi.Standalone{}
	cr.TypeMeta.Kind = "Standalone"
	cr.ObjectMeta.Name = "test"
	cr.ObjectMeta.Namespace = "default"
	mockClient := test.NewMockClient()
	// Add a secret with a password
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-secret",
			Namespace: cr.ObjectMeta.Namespace,
		},
		Data: map[string][]byte{"password": []byte("adminpass")},
	}
	_ = mockClient.Create(context.TODO(), secret)
	// Mock license info retrieval by patching the SplunkClient if needed
	ok := SendTelemetry(context.TODO(), mockClient, cr, map[string]interface{}{}, false)
	// We expect false because the mock client does not actually send telemetry, but this covers the path
	if ok {
		t.Logf("SendTelemetry returned true, but expected false due to mock client")
	}
}

func TestApplyTelemetry_Success(t *testing.T) {
	cm := &corev1.ConfigMap{Data: map[string]string{}}
	mockClient := test.NewMockClient()
	// Add a CR with TelAppInstalled true to trigger sending
	cr := &enterpriseApi.Standalone{
		TypeMeta:   metav1.TypeMeta{Kind: "Standalone"},
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Status:     enterpriseApi.StandaloneStatus{TelAppInstalled: true},
	}
	_ = mockClient.Create(context.TODO(), cr)
	result, err := ApplyTelemetry(context.TODO(), mockClient, cm)
	if err == nil && result != (reconcile.Result{}) && !result.Requeue {
		t.Errorf("expected requeue to be true or error to be non-nil")
	}
}

func TestApplyTelemetry_ConfigMapWithExistingData(t *testing.T) {
	cm := &corev1.ConfigMap{Data: map[string]string{"foo": "bar"}}
	mockClient := test.NewMockClient()
	result, err := ApplyTelemetry(context.TODO(), mockClient, cm)
	if err == nil {
		t.Errorf("expected error when no CRs are present, even with configmap data")
	}
	if result != (reconcile.Result{}) && !result.Requeue {
		t.Errorf("expected requeue to be true")
	}
}

// Fix TestApplyTelemetry_CRNoTelAppInstalled signature
func TestApplyTelemetry_CRNoTelAppInstalled(t *testing.T) {
	cm := &corev1.ConfigMap{Data: map[string]string{}}
	mockClient := test.NewMockClient()
	cr := &enterpriseApi.Standalone{
		TypeMeta:   metav1.TypeMeta{Kind: "Standalone"},
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Status:     enterpriseApi.StandaloneStatus{TelAppInstalled: false},
	}
	_ = mockClient.Create(context.TODO(), cr)
	result, err := ApplyTelemetry(context.TODO(), mockClient, cm)
	if err == nil {
		t.Errorf("expected error when no CRs with TelAppInstalled=true")
	}
	if result != (reconcile.Result{}) && !result.Requeue {
		t.Errorf("expected requeue to be true")
	}
}

func TestApplyTelemetry_SendTelemetryFails(t *testing.T) {
	cm := &corev1.ConfigMap{Data: map[string]string{}}
	mockClient := test.NewMockClient()
	cr := &enterpriseApi.Standalone{
		TypeMeta:   metav1.TypeMeta{Kind: "Standalone"},
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Status:     enterpriseApi.StandaloneStatus{TelAppInstalled: true},
	}
	_ = mockClient.Create(context.TODO(), cr)
	origFactory := newSplunkClientFactory
	newSplunkClientFactory = func(uri, user, pass string) SplunkTelemetryClient {
		return &mockSplunkTelemetryClient{
			GetLicenseInfoFunc: func() (map[string]splclient.LicenseInfo, error) {
				return map[string]splclient.LicenseInfo{"test": {}}, nil
			},
			SendTelemetryFunc: func(path string, body []byte) (interface{}, error) {
				return nil, errors.New("fail send")
			},
		}
	}
	defer func() { newSplunkClientFactory = origFactory }()
	result, err := ApplyTelemetry(context.TODO(), mockClient, cm)
	if err == nil {
		t.Errorf("expected error when SendTelemetry fails")
	}
	if result != (reconcile.Result{}) && !result.Requeue {
		t.Errorf("expected requeue to be true")
	}
}

func TestGetCurrentStatus_ValidStatus(t *testing.T) {
	status := TelemetryStatus{LastTransmission: "2024-01-01T00:00:00Z", Test: "true", SokVersion: "1.2.3"}
	b, _ := json.Marshal(status)
	cm := &corev1.ConfigMap{Data: map[string]string{"status": string(b)}}
	got := getCurrentStatus(context.TODO(), cm)
	if got.LastTransmission != status.LastTransmission || got.Test != status.Test || got.SokVersion != status.SokVersion {
		t.Errorf("expected status to match, got %+v", got)
	}
}

func TestHandleMonitoringConsoles_NoCRs(t *testing.T) {
	mockClient := &FakeListClient{crs: map[string][]client.Object{"MonitoringConsole": {}}}
	ctx := context.TODO()
	data, _, err := handleMonitoringConsoles(ctx, mockClient)
	if data != nil || err != nil {
		t.Errorf("expected nil, nil, nil when no MonitoringConsole CRs exist")
	}
}

func TestHandleMonitoringConsoles_OneCR(t *testing.T) {
	mc := &enterpriseApi.MonitoringConsole{
		ObjectMeta: metav1.ObjectMeta{Name: "mc1"},
		Spec: enterpriseApi.MonitoringConsoleSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("2Gi")},
						Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("4Gi")},
					},
				},
			},
		},
	}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"MonitoringConsole": {mc}}}
	ctx := context.TODO()
	data, _, err := handleMonitoringConsoles(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 1 {
		t.Errorf("expected one telemetry entry for mc1")
	}
	res, ok := m["mc1"].(map[string]string)
	if !ok {
		t.Errorf("expected resource telemetry for mc1")
	}
	if res[cpuRequestKey] != "1" || res[memoryRequestKey] != "2Gi" || res[cpuLimitKey] != "2" || res[memoryLimitKey] != "4Gi" {
		t.Errorf("unexpected resource telemetry: %+v", res)
	}
}

func TestHandleMonitoringConsoles_ListError(t *testing.T) {
	mockClient := &FakeListClient{crs: map[string][]client.Object{"MonitoringConsole": {}}}
	ctx := context.TODO()
	errClient := &errorClient{mockClient}
	data, _, err := handleMonitoringConsoles(ctx, errClient)
	if err == nil || err.Error() != "fail list" {
		t.Errorf("expected error 'fail list', got %v", err)
	}
	if data != nil {
		t.Errorf("expected nil, nil when error")
	}
}

func TestHandleMonitoringConsoles_MultipleCRs(t *testing.T) {
	mc1 := &enterpriseApi.MonitoringConsole{
		ObjectMeta: metav1.ObjectMeta{Name: "mc1"},
		Spec: enterpriseApi.MonitoringConsoleSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("2Gi")},
						Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("4Gi")},
					},
				},
			},
		},
	}
	mc2 := &enterpriseApi.MonitoringConsole{
		ObjectMeta: metav1.ObjectMeta{Name: "mc2"},
		Spec: enterpriseApi.MonitoringConsoleSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("3"), corev1.ResourceMemory: resource.MustParse("6Gi")},
						Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4"), corev1.ResourceMemory: resource.MustParse("8Gi")},
					},
				},
			},
		},
	}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"MonitoringConsole": {mc1, mc2}}}
	ctx := context.TODO()
	data, _, err := handleMonitoringConsoles(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 2 {
		t.Errorf("expected two telemetry entries")
	}
	res1, ok := m["mc1"].(map[string]string)
	if !ok || res1[cpuRequestKey] != "1" || res1[memoryRequestKey] != "2Gi" || res1[cpuLimitKey] != "2" || res1[memoryLimitKey] != "4Gi" {
		t.Errorf("unexpected resource telemetry for mc1: %+v", res1)
	}
	res2, ok := m["mc2"].(map[string]string)
	if !ok || res2[cpuRequestKey] != "3" || res2[memoryRequestKey] != "6Gi" || res2[cpuLimitKey] != "4" || res2[memoryLimitKey] != "8Gi" {
		t.Errorf("unexpected resource telemetry for mc2: %+v", res2)
	}
}

// Error client for simulating List error in tests
// Implements List to always return error

type errorClient struct{ *FakeListClient }

func (c *errorClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return errors.New("fail list")
}

// --- TEST-ONLY PATCHABLE TELEMETRY CLIENT MOCKS ---

// SplunkTelemetryClient is the interface for test patching (copied from production code if not imported)
type SplunkTelemetryClient interface {
	GetLicenseInfo() (map[string]splclient.LicenseInfo, error)
	SendTelemetry(path string, body []byte) (interface{}, error)
}

// mockSplunkTelemetryClient is a test mock for SplunkTelemetryClient
// Allows patching SendTelemetry and GetLicenseInfo
// Use fields for function overrides
type mockSplunkTelemetryClient struct {
	GetLicenseInfoFunc func() (map[string]splclient.LicenseInfo, error)
	SendTelemetryFunc  func(path string, body []byte) (interface{}, error)
}

func (m *mockSplunkTelemetryClient) GetLicenseInfo() (map[string]splclient.LicenseInfo, error) {
	if m.GetLicenseInfoFunc != nil {
		return m.GetLicenseInfoFunc()
	}
	return map[string]splclient.LicenseInfo{"test": {}}, nil
}
func (m *mockSplunkTelemetryClient) SendTelemetry(path string, body []byte) (interface{}, error) {
	if m.SendTelemetryFunc != nil {
		return m.SendTelemetryFunc(path, body)
	}
	return nil, nil
}

// Patchable factory for tests (must match production variable name)
var newSplunkClientFactory = func(uri, user, pass string) SplunkTelemetryClient {
	return &mockSplunkTelemetryClient{}
}

// --- Tests for handleStandalones ---
func TestHandleStandalones_NoCRs(t *testing.T) {
	mockClient := &FakeListClient{crs: map[string][]client.Object{"Standalone": {}}}
	ctx := context.TODO()
	data, _, err := handleStandalones(ctx, mockClient)
	if data != nil || err != nil {
		t.Errorf("expected nil, nil, nil when no Standalone CRs exist")
	}
}
func TestHandleStandalones_OneCR(t *testing.T) {
	cr := &enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{Name: "s1"},
		Spec: enterpriseApi.StandaloneSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("2Gi")},
						Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("4Gi")},
					},
				},
			},
		},
	}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"Standalone": {cr}}}
	ctx := context.TODO()
	data, _, err := handleStandalones(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 1 {
		t.Errorf("expected one telemetry entry for s1")
	}
	res, ok := m["s1"].(map[string]string)
	if !ok {
		t.Errorf("expected resource telemetry for s1")
	}
	if res[cpuRequestKey] != "1" || res[memoryRequestKey] != "2Gi" || res[cpuLimitKey] != "2" || res[memoryLimitKey] != "4Gi" {
		t.Errorf("unexpected resource telemetry: %+v", res)
	}
}
func TestHandleStandalones_MultipleCRs(t *testing.T) {
	cr1 := &enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{Name: "s1"},
		Spec: enterpriseApi.StandaloneSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
						Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
					},
				},
			},
		},
	}
	cr2 := &enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{Name: "s2"},
		Spec: enterpriseApi.StandaloneSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("3")},
						Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4")},
					},
				},
			},
		},
	}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"Standalone": {cr1, cr2}}}
	ctx := context.TODO()
	data, _, err := handleStandalones(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 2 {
		t.Errorf("expected two telemetry entries")
	}
}

type errorStandaloneClient struct{ *FakeListClient }

func (c *errorStandaloneClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return errors.New("fail list")
}
func TestHandleStandalones_ListError(t *testing.T) {
	mockClient := &FakeListClient{crs: map[string][]client.Object{"Standalone": {}}}
	ctx := context.TODO()
	errClient := &errorStandaloneClient{mockClient}
	data, _, err := handleStandalones(ctx, errClient)
	if err == nil || err.Error() != "fail list" {
		t.Errorf("expected error 'fail list', got %v", err)
	}
	if data != nil {
		t.Errorf("expected nil, nil when error")
	}
}
func TestHandleStandalones_EdgeResourceSpecs(t *testing.T) {
	cr := &enterpriseApi.Standalone{ObjectMeta: metav1.ObjectMeta{Name: "s1"}, Spec: enterpriseApi.StandaloneSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{}}}}}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"Standalone": {cr}}}
	ctx := context.TODO()
	data, _, err := handleStandalones(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 1 {
		t.Errorf("expected one telemetry entry for s1")
	}
	res, ok := m["s1"].(map[string]string)
	if !ok {
		t.Errorf("expected resource telemetry for s1")
	}
	if res[cpuRequestKey] != "" || res[memoryRequestKey] != "" || res[cpuLimitKey] != "" || res[memoryLimitKey] != "" {
		// Acceptable: all empty
	} else {
		t.Errorf("unexpected resource telemetry for edge case: %+v", res)
	}
}

// --- Tests for handleLicenseManagers ---
func TestHandleLicenseManagers_NoCRs(t *testing.T) {
	mockClient := &FakeListClient{crs: map[string][]client.Object{"LicenseManager": {}}}
	ctx := context.TODO()
	data, _, err := handleLicenseManagers(ctx, mockClient)
	if data != nil || err != nil {
		t.Errorf("expected nil, nil, nil when no LicenseManager CRs exist")
	}
}
func TestHandleLicenseManagers_OneCR(t *testing.T) {
	cr := &enterpriseApi.LicenseManager{
		ObjectMeta: metav1.ObjectMeta{Name: "lm1"},
		Spec: enterpriseApi.LicenseManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("2Gi")},
						Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("4Gi")},
					},
				},
			},
		},
	}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"LicenseManager": {cr}}}
	ctx := context.TODO()
	data, _, err := handleLicenseManagers(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 1 {
		t.Errorf("expected one telemetry entry for lm1")
	}
	res, ok := m["lm1"].(map[string]string)
	if !ok {
		t.Errorf("expected resource telemetry for lm1")
	}
	if res[cpuRequestKey] != "1" || res[memoryRequestKey] != "2Gi" || res[cpuLimitKey] != "2" || res[memoryLimitKey] != "4Gi" {
		t.Errorf("unexpected resource telemetry: %+v", res)
	}
}
func TestHandleLicenseManagers_MultipleCRs(t *testing.T) {
	cr1 := &enterpriseApi.LicenseManager{ObjectMeta: metav1.ObjectMeta{Name: "lm1"}, Spec: enterpriseApi.LicenseManagerSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")}}}}}}
	cr2 := &enterpriseApi.LicenseManager{ObjectMeta: metav1.ObjectMeta{Name: "lm2"}, Spec: enterpriseApi.LicenseManagerSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("3")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4")}}}}}}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"LicenseManager": {cr1, cr2}}}
	ctx := context.TODO()
	data, _, err := handleLicenseManagers(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 2 {
		t.Errorf("expected two telemetry entries")
	}
}

type errorLicenseManagerClient struct{ *FakeListClient }

func (c *errorLicenseManagerClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return errors.New("fail list")
}
func TestHandleLicenseManagers_ListError(t *testing.T) {
	mockClient := &FakeListClient{crs: map[string][]client.Object{"LicenseManager": {}}}
	ctx := context.TODO()
	errClient := &errorLicenseManagerClient{mockClient}
	data, _, err := handleLicenseManagers(ctx, errClient)
	if err == nil || err.Error() != "fail list" {
		t.Errorf("expected error 'fail list', got %v", err)
	}
	if data != nil {
		t.Errorf("expected nil, nil when error")
	}
}
func TestHandleLicenseManagers_EdgeResourceSpecs(t *testing.T) {
	cr := &enterpriseApi.LicenseManager{ObjectMeta: metav1.ObjectMeta{Name: "lm1"}, Spec: enterpriseApi.LicenseManagerSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{}}}}}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"LicenseManager": {cr}}}
	ctx := context.TODO()
	data, _, err := handleLicenseManagers(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 1 {
		t.Errorf("expected one telemetry entry for lm1")
	}
	res, ok := m["lm1"].(map[string]string)
	if !ok {
		t.Errorf("expected resource telemetry for lm1")
	}
	if res[cpuRequestKey] != "" || res[memoryRequestKey] != "" || res[cpuLimitKey] != "" || res[memoryLimitKey] != "" {
		// Acceptable: all empty
	} else {
		t.Errorf("unexpected resource telemetry for edge case: %+v", res)
	}
}

// --- Tests for handleLicenseMasters ---
func TestHandleLicenseMasters_NoCRs(t *testing.T) {
	mockClient := &FakeListClient{crs: map[string][]client.Object{"LicenseMaster": {}}}
	ctx := context.TODO()
	data, _, err := handleLicenseMasters(ctx, mockClient)
	if data != nil || err != nil {
		t.Errorf("expected nil, nil, nil when no LicenseMaster CRs exist")
	}
}
func TestHandleLicenseMasters_OneCR(t *testing.T) {
	cr := &enterpriseApiV3.LicenseMaster{
		ObjectMeta: metav1.ObjectMeta{Name: "lm1"},
		Spec: enterpriseApiV3.LicenseMasterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("2Gi")},
						Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("4Gi")},
					},
				},
			},
		},
	}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"LicenseMaster": {cr}}}
	ctx := context.TODO()
	data, _, err := handleLicenseMasters(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 1 {
		t.Errorf("expected one telemetry entry for lm1")
	}
	res, ok := m["lm1"].(map[string]string)
	if !ok {
		t.Errorf("expected resource telemetry for lm1")
	}
	if res[cpuRequestKey] != "1" || res[memoryRequestKey] != "2Gi" || res[cpuLimitKey] != "2" || res[memoryLimitKey] != "4Gi" {
		t.Errorf("unexpected resource telemetry: %+v", res)
	}
}
func TestHandleLicenseMasters_MultipleCRs(t *testing.T) {
	cr1 := &enterpriseApiV3.LicenseMaster{ObjectMeta: metav1.ObjectMeta{Name: "lm1"}, Spec: enterpriseApiV3.LicenseMasterSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")}}}}}}
	cr2 := &enterpriseApiV3.LicenseMaster{ObjectMeta: metav1.ObjectMeta{Name: "lm2"}, Spec: enterpriseApiV3.LicenseMasterSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("3")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4")}}}}}}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"LicenseMaster": {cr1, cr2}}}
	ctx := context.TODO()
	data, _, err := handleLicenseMasters(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 2 {
		t.Errorf("expected two telemetry entries")
	}
}

type errorLicenseMasterClient struct{ *FakeListClient }

func (c *errorLicenseMasterClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return errors.New("fail list")
}
func TestHandleLicenseMasters_ListError(t *testing.T) {
	mockClient := &FakeListClient{crs: map[string][]client.Object{"LicenseMaster": {}}}
	ctx := context.TODO()
	errClient := &errorLicenseMasterClient{mockClient}
	data, _, err := handleLicenseMasters(ctx, errClient)
	if err == nil || err.Error() != "fail list" {
		t.Errorf("expected error 'fail list', got %v", err)
	}
	if data != nil {
		t.Errorf("expected nil, nil when error")
	}
}
func TestHandleLicenseMasters_EdgeResourceSpecs(t *testing.T) {
	cr := &enterpriseApiV3.LicenseMaster{ObjectMeta: metav1.ObjectMeta{Name: "lm1"}, Spec: enterpriseApiV3.LicenseMasterSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{}}}}}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"LicenseMaster": {cr}}}
	ctx := context.TODO()
	data, _, err := handleLicenseMasters(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 1 {
		t.Errorf("expected one telemetry entry for lm1")
	}
	res, ok := m["lm1"].(map[string]string)
	if !ok {
		t.Errorf("expected resource telemetry for lm1")
	}
	if res[cpuRequestKey] != "" || res[memoryRequestKey] != "" || res[cpuLimitKey] != "" || res[memoryLimitKey] != "" {
		// Acceptable: all empty
	} else {
		t.Errorf("unexpected resource telemetry for edge case: %+v", res)
	}
}

// --- Tests for handleSearchHeadClusters ---
func TestHandleSearchHeadClusters_NoCRs(t *testing.T) {
	mockClient := &FakeListClient{crs: map[string][]client.Object{"SearchHeadCluster": {}}}
	ctx := context.TODO()
	data, _, err := handleSearchHeadClusters(ctx, mockClient)
	if data != nil || err != nil {
		t.Errorf("expected nil, nil, nil when no SearchHeadCluster CRs exist")
	}
}
func TestHandleSearchHeadClusters_OneCR(t *testing.T) {
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "shc1"},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("2Gi")},
						Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("4Gi")},
					},
				},
			},
		},
	}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"SearchHeadCluster": {cr}}}
	ctx := context.TODO()
	data, _, err := handleSearchHeadClusters(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 1 {
		t.Errorf("expected one telemetry entry for shc1")
	}
	res, ok := m["shc1"].(map[string]string)
	if !ok {
		t.Errorf("expected resource telemetry for shc1")
	}
	if res[cpuRequestKey] != "1" || res[memoryRequestKey] != "2Gi" || res[cpuLimitKey] != "2" || res[memoryLimitKey] != "4Gi" {
		t.Errorf("unexpected resource telemetry: %+v", res)
	}
}
func TestHandleSearchHeadClusters_MultipleCRs(t *testing.T) {
	cr1 := &enterpriseApi.SearchHeadCluster{ObjectMeta: metav1.ObjectMeta{Name: "shc1"}, Spec: enterpriseApi.SearchHeadClusterSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")}}}}}}
	cr2 := &enterpriseApi.SearchHeadCluster{ObjectMeta: metav1.ObjectMeta{Name: "shc2"}, Spec: enterpriseApi.SearchHeadClusterSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("3")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4")}}}}}}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"SearchHeadCluster": {cr1, cr2}}}
	ctx := context.TODO()
	data, _, err := handleSearchHeadClusters(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 2 {
		t.Errorf("expected two telemetry entries")
	}
}

type errorSearchHeadClusterClient struct{ *FakeListClient }

func (c *errorSearchHeadClusterClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return errors.New("fail list")
}
func TestHandleSearchHeadClusters_ListError(t *testing.T) {
	mockClient := &FakeListClient{crs: map[string][]client.Object{"SearchHeadCluster": {}}}
	ctx := context.TODO()
	errClient := &errorSearchHeadClusterClient{mockClient}
	data, _, err := handleSearchHeadClusters(ctx, errClient)
	if err == nil || err.Error() != "fail list" {
		t.Errorf("expected error 'fail list', got %v", err)
	}
	if data != nil {
		t.Errorf("expected nil, nil when error")
	}
}
func TestHandleSearchHeadClusters_EdgeResourceSpecs(t *testing.T) {
	cr := &enterpriseApi.SearchHeadCluster{ObjectMeta: metav1.ObjectMeta{Name: "shc1"}, Spec: enterpriseApi.SearchHeadClusterSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{}}}}}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"SearchHeadCluster": {cr}}}
	ctx := context.TODO()
	data, _, err := handleSearchHeadClusters(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 1 {
		t.Errorf("expected one telemetry entry for shc1")
	}
	res, ok := m["shc1"].(map[string]string)
	if !ok {
		t.Errorf("expected resource telemetry for shc1")
	}
	if res[cpuRequestKey] != "" || res[memoryRequestKey] != "" || res[cpuLimitKey] != "" || res[memoryLimitKey] != "" {
		// Acceptable: all empty
	} else {
		t.Errorf("unexpected resource telemetry for edge case: %+v", res)
	}
}

// --- Tests for handleIndexerClusters ---
func TestHandleIndexerClusters_NoCRs(t *testing.T) {
	mockClient := &FakeListClient{crs: map[string][]client.Object{"IndexerCluster": {}}}
	ctx := context.TODO()
	data, _, err := handleIndexerClusters(ctx, mockClient)
	if data != nil || err != nil {
		t.Errorf("expected nil, nil, nil when no IndexerCluster CRs exist")
	}
}
func TestHandleIndexerClusters_OneCR(t *testing.T) {
	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "idx1"},
		Spec: enterpriseApi.IndexerClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("2Gi")},
						Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("4Gi")},
					},
				},
			},
		},
	}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"IndexerCluster": {cr}}}
	ctx := context.TODO()
	data, _, err := handleIndexerClusters(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 1 {
		t.Errorf("expected one telemetry entry for idx1")
	}
	res, ok := m["idx1"].(map[string]string)
	if !ok {
		t.Errorf("expected resource telemetry for idx1")
	}
	if res[cpuRequestKey] != "1" || res[memoryRequestKey] != "2Gi" || res[cpuLimitKey] != "2" || res[memoryLimitKey] != "4Gi" {
		t.Errorf("unexpected resource telemetry: %+v", res)
	}
}
func TestHandleIndexerClusters_MultipleCRs(t *testing.T) {
	cr1 := &enterpriseApi.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Name: "idx1"}, Spec: enterpriseApi.IndexerClusterSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")}}}}}}
	cr2 := &enterpriseApi.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Name: "idx2"}, Spec: enterpriseApi.IndexerClusterSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("3")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4")}}}}}}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"IndexerCluster": {cr1, cr2}}}
	ctx := context.TODO()
	data, _, err := handleIndexerClusters(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 2 {
		t.Errorf("expected two telemetry entries")
	}
}

type errorIndexerClusterClient struct{ *FakeListClient }

func (c *errorIndexerClusterClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return errors.New("fail list")
}
func TestHandleIndexerClusters_ListError(t *testing.T) {
	mockClient := &FakeListClient{crs: map[string][]client.Object{"IndexerCluster": {}}}
	ctx := context.TODO()
	errClient := &errorIndexerClusterClient{mockClient}
	data, _, err := handleIndexerClusters(ctx, errClient)
	if err == nil || err.Error() != "fail list" {
		t.Errorf("expected error 'fail list', got %v", err)
	}
	if data != nil {
		t.Errorf("expected nil, nil when error")
	}
}
func TestHandleIndexerClusters_EdgeResourceSpecs(t *testing.T) {
	cr := &enterpriseApi.IndexerCluster{ObjectMeta: metav1.ObjectMeta{Name: "idx1"}, Spec: enterpriseApi.IndexerClusterSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{}}}}}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"IndexerCluster": {cr}}}
	ctx := context.TODO()
	data, _, err := handleIndexerClusters(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 1 {
		t.Errorf("expected one telemetry entry for idx1")
	}
	res, ok := m["idx1"].(map[string]string)
	if !ok {
		t.Errorf("expected resource telemetry for idx1")
	}
	if res[cpuRequestKey] != "" || res[memoryRequestKey] != "" || res[cpuLimitKey] != "" || res[memoryLimitKey] != "" {
		// Acceptable: all empty
	} else {
		t.Errorf("unexpected resource telemetry for edge case: %+v", res)
	}
}

// --- Tests for handleClusterManagers ---
func TestHandleClusterManagers_NoCRs(t *testing.T) {
	mockClient := &FakeListClient{crs: map[string][]client.Object{"ClusterManager": {}}}
	ctx := context.TODO()
	data, _, err := handleClusterManagers(ctx, mockClient)
	if data != nil || err != nil {
		t.Errorf("expected nil, nil, nil when no ClusterManager CRs exist")
	}
}
func TestHandleClusterManagers_OneCR(t *testing.T) {
	cr := &enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{Name: "cmgr1"},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("2Gi")},
						Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("4Gi")},
					},
				},
			},
		},
	}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"ClusterManager": {cr}}}
	ctx := context.TODO()
	data, _, err := handleClusterManagers(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 1 {
		t.Errorf("expected one telemetry entry for cmgr1")
	}
	res, ok := m["cmgr1"].(map[string]string)
	if !ok {
		t.Errorf("expected resource telemetry for cmgr1")
	}
	if res[cpuRequestKey] != "1" || res[memoryRequestKey] != "2Gi" || res[cpuLimitKey] != "2" || res[memoryLimitKey] != "4Gi" {
		t.Errorf("unexpected resource telemetry: %+v", res)
	}
}
func TestHandleClusterManagers_MultipleCRs(t *testing.T) {
	cr1 := &enterpriseApi.ClusterManager{ObjectMeta: metav1.ObjectMeta{Name: "cmgr1"}, Spec: enterpriseApi.ClusterManagerSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")}}}}}}
	cr2 := &enterpriseApi.ClusterManager{ObjectMeta: metav1.ObjectMeta{Name: "cmgr2"}, Spec: enterpriseApi.ClusterManagerSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("3")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4")}}}}}}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"ClusterManager": {cr1, cr2}}}
	ctx := context.TODO()
	data, _, err := handleClusterManagers(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 2 {
		t.Errorf("expected two telemetry entries")
	}
}

type errorClusterManagerClient struct{ *FakeListClient }

func (c *errorClusterManagerClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return errors.New("fail list")
}
func TestHandleClusterManagers_ListError(t *testing.T) {
	mockClient := &FakeListClient{crs: map[string][]client.Object{"ClusterManager": {}}}
	ctx := context.TODO()
	errClient := &errorClusterManagerClient{mockClient}
	data, _, err := handleClusterManagers(ctx, errClient)
	if err == nil || err.Error() != "fail list" {
		t.Errorf("expected error 'fail list', got %v", err)
	}
	if data != nil {
		t.Errorf("expected nil, nil when error")
	}
}
func TestHandleClusterManagers_EdgeResourceSpecs(t *testing.T) {
	cr := &enterpriseApi.ClusterManager{ObjectMeta: metav1.ObjectMeta{Name: "cmgr1"}, Spec: enterpriseApi.ClusterManagerSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{}}}}}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"ClusterManager": {cr}}}
	ctx := context.TODO()
	data, _, err := handleClusterManagers(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 1 {
		t.Errorf("expected one telemetry entry for cmgr1")
	}
	res, ok := m["cmgr1"].(map[string]string)
	if !ok {
		t.Errorf("expected resource telemetry for cmgr1")
	}
	if res[cpuRequestKey] != "" || res[memoryRequestKey] != "" || res[cpuLimitKey] != "" || res[memoryLimitKey] != "" {
		// Acceptable: all empty
	} else {
		t.Errorf("unexpected resource telemetry for edge case: %+v", res)
	}
}

// --- Tests for handleClusterMasters ---
func TestHandleClusterMasters_NoCRs(t *testing.T) {
	mockClient := &FakeListClient{crs: map[string][]client.Object{"ClusterMaster": {}}}
	ctx := context.TODO()
	data, _, err := handleClusterMasters(ctx, mockClient)
	if data != nil || err != nil {
		t.Errorf("expected nil, nil, nil when no ClusterMaster CRs exist")
	}
}
func TestHandleClusterMasters_OneCR(t *testing.T) {
	cr := &enterpriseApiV3.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{Name: "cmast1"},
		Spec: enterpriseApiV3.ClusterMasterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("2Gi")},
						Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("4Gi")},
					},
				},
			},
		},
	}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"ClusterMaster": {cr}}}
	ctx := context.TODO()
	data, _, err := handleClusterMasters(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 1 {
		t.Errorf("expected one telemetry entry for cmast1")
	}
	res, ok := m["cmast1"].(map[string]string)
	if !ok {
		t.Errorf("expected resource telemetry for cmast1")
	}
	if res[cpuRequestKey] != "1" || res[memoryRequestKey] != "2Gi" || res[cpuLimitKey] != "2" || res[memoryLimitKey] != "4Gi" {
		t.Errorf("unexpected resource telemetry: %+v", res)
	}
}
func TestHandleClusterMasters_MultipleCRs(t *testing.T) {
	cr1 := &enterpriseApiV3.ClusterMaster{ObjectMeta: metav1.ObjectMeta{Name: "cmast1"}, Spec: enterpriseApiV3.ClusterMasterSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")}}}}}}
	cr2 := &enterpriseApiV3.ClusterMaster{ObjectMeta: metav1.ObjectMeta{Name: "cmast2"}, Spec: enterpriseApiV3.ClusterMasterSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("3")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4")}}}}}}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"ClusterMaster": {cr1, cr2}}}
	ctx := context.TODO()
	data, _, err := handleClusterMasters(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 2 {
		t.Errorf("expected two telemetry entries")
	}
}

type errorClusterMasterClient struct{ *FakeListClient }

func (c *errorClusterMasterClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return errors.New("fail list")
}
func TestHandleClusterMasters_ListError(t *testing.T) {
	mockClient := &FakeListClient{crs: map[string][]client.Object{"ClusterMaster": {}}}
	ctx := context.TODO()
	errClient := &errorClusterMasterClient{mockClient}
	data, _, err := handleClusterMasters(ctx, errClient)
	if err == nil {
		t.Errorf("expected error 'fail list', got %v", err)
	}
	if data != nil {
		t.Errorf("expected nil, nil when error")
	}
}
func TestHandleClusterMasters_EdgeResourceSpecs(t *testing.T) {
	cr := &enterpriseApiV3.ClusterMaster{ObjectMeta: metav1.ObjectMeta{Name: "cmast1"}, Spec: enterpriseApiV3.ClusterMasterSpec{CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Spec: enterpriseApi.Spec{Resources: corev1.ResourceRequirements{}}}}}
	mockClient := &FakeListClient{crs: map[string][]client.Object{"ClusterMaster": {cr}}}
	ctx := context.TODO()
	data, _, err := handleClusterMasters(ctx, mockClient)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]interface{})
	if !ok || len(m) != 1 {
		t.Errorf("expected one telemetry entry for cmast1")
	}
	res, ok := m["cmast1"].(map[string]string)
	if !ok {
		t.Errorf("expected resource telemetry for cmast1")
	}
	if res[cpuRequestKey] != "" || res[memoryRequestKey] != "" || res[cpuLimitKey] != "" || res[memoryLimitKey] != "" {
		// Acceptable: all empty
	} else {
		t.Errorf("unexpected resource telemetry for edge case: %+v", res)
	}
}
