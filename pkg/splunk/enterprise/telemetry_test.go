// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

package enterprise

import (
	"context"
	"encoding/json"
	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetAllCustomResources_Empty(t *testing.T) {
	mockClient := spltest.NewMockClient()
	ctx := context.TODO()
	crMap := getAllCustomResources(ctx, mockClient)
	if len(crMap) != 0 {
		t.Errorf("expected no CRs, got %d", len(crMap))
	}
}

func TestCollectCRTelData_WithMockCR(t *testing.T) {
	mockClient := spltest.NewMockClient()
	ctx := context.TODO()
	cr := &enterpriseApi.Standalone{}
	cr.TypeMeta.Kind = "Standalone"
	cr.ObjectMeta.Name = "test-standalone"
	crList := map[string][]splcommon.MetaObject{"Standalone": {cr}}
	data := make(map[string]interface{})
	collectCRTelData(ctx, mockClient, crList, data)
	if _, ok := data["Standalone"]; !ok {
		t.Errorf("expected Standalone key in data map")
	}
}

func TestApplyTelemetry_ConfigMapNoData(t *testing.T) {
	mockClient := spltest.NewMockClient()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
		Data:       map[string]string{},
	}
	ctx := context.TODO()
	result, err := ApplyTelemetry(ctx, mockClient, cm)
	if err == nil {
		t.Errorf("expected error when no CRs present, got nil")
	}
	if !result.Requeue {
		t.Errorf("expected requeue to be true, got false")
	}
}

func TestCollectCMTelData_UnmarshalError(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
		Data:       map[string]string{"bad": "notjson"},
	}
	ctx := context.TODO()
	data := make(map[string]interface{})
	CollectCMTelData(ctx, cm, data)
	if data["bad"] != "notjson" {
		t.Errorf("expected fallback to string on unmarshal error")
	}
}

func TestCollectCMTelData_ValidJSON(t *testing.T) {
	val := map[string]interface{}{"foo": "bar"}
	b, _ := json.Marshal(val)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
		Data:       map[string]string{"good": string(b)},
	}
	ctx := context.TODO()
	data := make(map[string]interface{})
	CollectCMTelData(ctx, cm, data)
	if m, ok := data["good"].(map[string]interface{}); !ok || m["foo"] != "bar" {
		t.Errorf("expected valid JSON to be unmarshaled")
	}
}

func TestSendTelemetry_UnknownKind(t *testing.T) {
	cr := &enterpriseApi.Standalone{}
	cr.TypeMeta.Kind = "UnknownKind"
	ok := SendTelemetry(context.TODO(), spltest.NewMockClient(), cr, map[string]interface{}{})
	if ok {
		t.Errorf("expected SendTelemetry to return false for unknown kind")
	}
}

func TestSendTelemetry_NoSecret(t *testing.T) {
	cr := &enterpriseApi.Standalone{}
	cr.TypeMeta.Kind = "Standalone"
	cr.ObjectMeta.Name = "test"
	cr.ObjectMeta.Namespace = "default"
	ok := SendTelemetry(context.TODO(), spltest.NewMockClient(), cr, map[string]interface{}{})
	if ok {
		t.Errorf("expected SendTelemetry to return false if no secret found")
	}
}

func TestGetAllCustomResources_AllKinds(t *testing.T) {
	ctx := context.TODO()
	mockClient := spltest.NewMockClient()

	// Standalone
	standalone := &enterpriseApi.Standalone{}
	standalone.TypeMeta.Kind = "Standalone"
	standalone.ObjectMeta.Name = "test-standalone"
	mockClient.AddObject(standalone)

	// LicenseManager
	licenseManager := &enterpriseApi.LicenseManager{}
	licenseManager.TypeMeta.Kind = "LicenseManager"
	licenseManager.ObjectMeta.Name = "test-licensemanager"
	mockClient.AddObject(licenseManager)

	// LicenseMaster (v3)
	licenseMaster := &enterpriseApiV3.LicenseMaster{}
	licenseMaster.TypeMeta.Kind = "LicenseMaster"
	licenseMaster.ObjectMeta.Name = "test-licensemaster"
	mockClient.AddObject(licenseMaster)

	// SearchHeadCluster
	shc := &enterpriseApi.SearchHeadCluster{}
	shc.TypeMeta.Kind = "SearchHeadCluster"
	shc.ObjectMeta.Name = "test-shc"
	mockClient.AddObject(shc)

	// IndexerCluster
	idx := &enterpriseApi.IndexerCluster{}
	idx.TypeMeta.Kind = "IndexerCluster"
	idx.ObjectMeta.Name = "test-idx"
	mockClient.AddObject(idx)

	// ClusterManager
	cmanager := &enterpriseApi.ClusterManager{}
	cmanager.TypeMeta.Kind = "ClusterManager"
	cmanager.ObjectMeta.Name = "test-cmanager"
	mockClient.AddObject(cmanager)

	// ClusterMaster (v3)
	cmaster := &enterpriseApiV3.ClusterMaster{}
	cmaster.TypeMeta.Kind = "ClusterMaster"
	cmaster.ObjectMeta.Name = "test-cmaster"
	mockClient.AddObject(cmaster)

	crMap := getAllCustomResources(ctx, mockClient)
	kinds := []string{"Standalone", "LicenseManager", "LicenseMaster", "SearchHeadCluster", "IndexerCluster", "ClusterManager", "ClusterMaster"}
	for _, kind := range kinds {
		if _, ok := crMap[kind]; !ok {
			t.Errorf("expected kind %s in CR map", kind)
		}
	}
}

// Test for resource extraction from StatefulSet (integration style, not a pure unit test)
func TestCollectCRTelData_ResourceData(t *testing.T) {
	mockClient := spltest.NewMockClient()
	ctx := context.TODO()
	cr := &enterpriseApi.Standalone{}
	cr.TypeMeta.Kind = "Standalone"
	cr.ObjectMeta.Name = "test-standalone"
	cr.ObjectMeta.Namespace = "default"
	crList := map[string][]splcommon.MetaObject{"Standalone": {cr}}
	data := make(map[string]interface{})

	// Create a fake StatefulSet owned by the CR with resource settings
	sts := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-standalone-sts-0",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				UID: cr.GetUID(),
			}},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "test-container",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
				},
			}},
		},
	}
	// Add the pod to the fake client (simulate the pod as a member of the StatefulSet)
	mockClient.AddObject(sts)

	// Run the function under test
	collectCRTelData(ctx, mockClient, crList, data)

	standaloneData, ok := data["Standalone"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected Standalone data map")
	}
	crData, ok := standaloneData["test-standalone"].([]map[string]string)
	if !ok || len(crData) == 0 {
		t.Fatalf("expected resource data slice")
	}
	container := crData[0]
	if container["cpu_request"] != "500m" || container["memory_request"] != "128Mi" || container["cpu_limit"] != "1" || container["memory_limit"] != "256Mi" {
		t.Errorf("unexpected resource values: got %+v", container)
	}
}

func TestCollectCMTelData_SetsDataCorrectly(t *testing.T) {
	ctx := context.TODO()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
		Data: map[string]string{
			"json":  "{\"foo\":\"bar\"}",
			"plain": "baz",
		},
	}
	data := make(map[string]interface{})
	CollectCMTelData(ctx, cm, data)

	// JSON key should be unmarshaled
	if m, ok := data["json"].(map[string]interface{}); !ok || m["foo"] != "bar" {
		t.Errorf("expected 'json' key to be unmarshaled to map with foo=bar, got: %v", data["json"])
	}
	// Plain key should be set as string
	if s, ok := data["plain"].(string); !ok || s != "baz" {
		t.Errorf("expected 'plain' key to be set as string 'baz', got: %v", data["plain"])
	}
}

// Additional tests for error paths and success can be added with more advanced mocks.
