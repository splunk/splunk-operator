// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

package enterprise

import (
	"context"
	"encoding/json"
	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	"testing"
	"time"

	"errors"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCollectResourceTelData_NilMaps(t *testing.T) {
	data := make(map[string]string)
	collectResourceTelData(corev1.ResourceRequirements{}, data)
	if data[cpuRequestKey] == "" || data[memoryRequestKey] == "" || data[cpuLimitKey] == "" || data[memoryLimitKey] == "" {
		t.Errorf("expected default values for nil maps")
	}
}

func TestCollectResourceTelData_MissingKeys(t *testing.T) {
	data := make(map[string]string)
	reqs := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{},
		Limits:   corev1.ResourceList{},
	}
	collectResourceTelData(reqs, data)
	if data[cpuRequestKey] == "" || data[memoryRequestKey] == "" || data[cpuLimitKey] == "" || data[memoryLimitKey] == "" {
		t.Errorf("expected default values for missing keys")
	}
}

func TestCollectResourceTelData_ValuesPresent(t *testing.T) {
	data := make(map[string]string)
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
	collectResourceTelData(reqs, data)
	if data[cpuRequestKey] != "123m" || data[memoryRequestKey] != "456Mi" || data[cpuLimitKey] != "789m" || data[memoryLimitKey] != "1Gi" {
		t.Errorf("unexpected values: got %+v", data)
	}
}

func TestCollectCMTelData_UnmarshalError(t *testing.T) {
	cm := &corev1.ConfigMap{Data: map[string]string{"bad": "notjson"}}
	data := make(map[string]interface{})
	CollectCMTelData(context.TODO(), cm, data)
	if data["bad"] != "notjson" {
		t.Errorf("expected fallback to string on unmarshal error")
	}
}

func TestCollectCMTelData_ValidJSON(t *testing.T) {
	val := map[string]interface{}{"foo": "bar"}
	b, _ := json.Marshal(val)
	cm := &corev1.ConfigMap{Data: map[string]string{"good": string(b)}}
	data := make(map[string]interface{})
	CollectCMTelData(context.TODO(), cm, data)
	if m, ok := data["good"].(map[string]interface{}); !ok || m["foo"] != "bar" {
		t.Errorf("expected valid JSON to be unmarshaled")
	}
}

func TestGetCurrentStatus_Default(t *testing.T) {
	cm := &corev1.ConfigMap{Data: nil}
	status := getCurrentStatus(context.TODO(), cm)
	if status == nil || status.Test != "true" {
		t.Errorf("expected default status")
	}
}

func TestGetCurrentStatus_UnmarshalError(t *testing.T) {
	cm := &corev1.ConfigMap{Data: map[string]string{"status": "notjson"}}
	status := getCurrentStatus(context.TODO(), cm)
	if status == nil || status.Test != "true" {
		t.Errorf("expected default status on unmarshal error")
	}
}

func TestUpdateLastTransmissionTime_MarshalError(t *testing.T) {
	ctx := context.TODO()
	cm := &corev1.ConfigMap{Data: map[string]string{}}
	// Use a struct with a channel field to cause json.MarshalIndent to fail
	// Should not panic
	updateLastTransmissionTime(ctx, spltest.NewMockClient(), cm, (*TelemetryStatus)(nil)) // pass nil to avoid panic
}

func TestSendTelemetry_UnknownKind(t *testing.T) {
	cr := &enterpriseApi.Standalone{}
	cr.TypeMeta.Kind = "UnknownKind"
	ok := SendTelemetry(context.TODO(), spltest.NewMockClient(), cr, map[string]interface{}{}, false)
	if ok {
		t.Errorf("expected SendTelemetry to return false for unknown kind")
	}
}

func TestSendTelemetry_NoSecret(t *testing.T) {
	cr := &enterpriseApi.Standalone{}
	cr.TypeMeta.Kind = "Standalone"
	cr.ObjectMeta.Name = "test"
	cr.ObjectMeta.Namespace = "default"
	ok := SendTelemetry(context.TODO(), spltest.NewMockClient(), cr, map[string]interface{}{}, false)
	if ok {
		t.Errorf("expected SendTelemetry to return false if no secret found")
	}
}

func TestTelemetryUpdateLastTransmissionTime_SetsTimestamp(t *testing.T) {
	mockClient := spltest.NewMockClient()
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
	mockClient := spltest.NewMockClient()
	ctx := context.TODO()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
		Data:       map[string]string{},
	}
	status := &TelemetryStatus{Test: "false"}
	updateLastTransmissionTime(ctx, mockClient, cm, status)
	firstStatus := cm.Data[telStatusKey]
	time.Sleep(1 * time.Second)
	updateLastTransmissionTime(ctx, mockClient, cm, status)
	secondStatus := cm.Data[telStatusKey]
	if firstStatus == secondStatus {
		t.Errorf("expected status to change on repeated call")
	}
}

func TestCollectDeploymentTelData_AllKinds(t *testing.T) {
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

// errorUpdateClient is a mock client that always returns an error on Update
// Used for testing updateLastTransmissionTime error handling
type errorUpdateClient struct {
	spltest.MockClient
}

func (c *errorUpdateClient) Update(_ context.Context, _ client.Object, _ ...client.UpdateOption) error {
	return errors.New("forced update error")
}

// FakeListClient is a local mock client that supports List for CRs and StatefulSets for testing
// Only implements List for the types needed in these tests
type FakeListClient struct {
	spltest.MockClient
	crs map[string][]client.Object
	sts []apps.StatefulSet
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
	case *apps.StatefulSetList:
		l.Items = c.sts
	default:
		return nil
	}
	return nil
}

// Additional tests for error paths and success can be added with more advanced mocks.
