// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

package enterprise

import (
	"context"
	"encoding/json"
	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
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

func TestTelemetryGetAllCustomResources_Empty(t *testing.T) {
	mockClient := spltest.NewMockClient()
	ctx := context.TODO()
	crMap := getAllCustomResources(ctx, mockClient)
	if len(crMap) != 0 {
		t.Errorf("expected no CRs, got %d", len(crMap))
	}
}

func TestTelemetryCollectCRTelData_WithMockCR(t *testing.T) {
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

func TestTelemetryCollectCMTelData_UnmarshalError(t *testing.T) {
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

func TestTelemetryCollectCMTelData_ValidJSON(t *testing.T) {
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

func TestTelemetryGetAllCustomResources_AllKinds(t *testing.T) {
	ctx := context.TODO()
	fakeClient := &FakeListClient{
		crs: map[string][]client.Object{
			"Standalone":        {&enterpriseApi.Standalone{TypeMeta: metav1.TypeMeta{Kind: "Standalone"}, ObjectMeta: metav1.ObjectMeta{Name: "test-standalone"}}},
			"LicenseManager":    {&enterpriseApi.LicenseManager{TypeMeta: metav1.TypeMeta{Kind: "LicenseManager"}, ObjectMeta: metav1.ObjectMeta{Name: "test-licensemanager"}}},
			"LicenseMaster":     {&enterpriseApiV3.LicenseMaster{TypeMeta: metav1.TypeMeta{Kind: "LicenseMaster"}, ObjectMeta: metav1.ObjectMeta{Name: "test-licensemaster"}}},
			"SearchHeadCluster": {&enterpriseApi.SearchHeadCluster{TypeMeta: metav1.TypeMeta{Kind: "SearchHeadCluster"}, ObjectMeta: metav1.ObjectMeta{Name: "test-shc"}}},
			"ClusterManager":    {&enterpriseApi.ClusterManager{TypeMeta: metav1.TypeMeta{Kind: "ClusterManager"}, ObjectMeta: metav1.ObjectMeta{Name: "test-cmanager"}}},
			"ClusterMaster":     {&enterpriseApiV3.ClusterMaster{TypeMeta: metav1.TypeMeta{Kind: "ClusterMaster"}, ObjectMeta: metav1.ObjectMeta{Name: "test-cmaster"}}},
		},
		sts: []apps.StatefulSet{}, // ensure all keys are present
	}
	crMap := getAllCustomResources(ctx, fakeClient)
	kinds := []string{"Standalone", "LicenseManager", "LicenseMaster", "SearchHeadCluster", "ClusterManager", "ClusterMaster"}
	for _, kind := range kinds {
		if _, ok := crMap[kind]; !ok {
			t.Errorf("expected kind %s in CR map", kind)
		}
	}
}

func TestTelemetryCollectCRTelData_StandaloneData(t *testing.T) {
	ctx := context.TODO()
	cr := &enterpriseApi.Standalone{}
	cr.TypeMeta.Kind = "Standalone"
	cr.ObjectMeta.Name = "test-standalone"
	cr.ObjectMeta.Namespace = "default"
	crList := map[string][]splcommon.MetaObject{"Standalone": {cr}}
	sts := apps.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-standalone-sts",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				UID: cr.GetUID(),
			}},
		},
		Spec: apps.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
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
			},
		},
	}
	fakeClient := &FakeListClient{
		sts: []apps.StatefulSet{sts},
	}
	data := make(map[string]interface{})
	collectCRTelData(ctx, fakeClient, crList, data)
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

func TestTelemetryCollectCRTelData_LicenseManagerData(t *testing.T) {
	ctx := context.TODO()
	cr := &enterpriseApi.LicenseManager{}
	cr.TypeMeta.Kind = "LicenseManager"
	cr.ObjectMeta.Name = "test-licensemanager"
	cr.ObjectMeta.Namespace = "default"
	crList := map[string][]splcommon.MetaObject{"LicenseManager": {cr}}
	sts := apps.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-licensemanager-sts",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				UID: cr.GetUID(),
			}},
		},
		Spec: apps.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "test-container",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("600m"),
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("2"),
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
						},
					}},
				},
			},
		},
	}
	fakeClient := &FakeListClient{
		sts: []apps.StatefulSet{sts},
	}
	data := make(map[string]interface{})
	collectCRTelData(ctx, fakeClient, crList, data)
	lmData, ok := data["LicenseManager"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected LicenseManager data map")
	}
	crData, ok := lmData["test-licensemanager"].([]map[string]string)
	if !ok || len(crData) == 0 {
		t.Fatalf("expected resource data slice")
	}
	container := crData[0]
	if container["cpu_request"] != "600m" || container["memory_request"] != "256Mi" || container["cpu_limit"] != "2" || container["memory_limit"] != "512Mi" {
		t.Errorf("unexpected resource values: got %+v", container)
	}
}

func TestTelemetryCollectCRTelData_LicenseMasterData(t *testing.T) {
	ctx := context.TODO()
	cr := &enterpriseApiV3.LicenseMaster{}
	cr.TypeMeta.Kind = "LicenseMaster"
	cr.ObjectMeta.Name = "test-licensemaster"
	cr.ObjectMeta.Namespace = "default"
	crList := map[string][]splcommon.MetaObject{"LicenseMaster": {cr}}
	sts := apps.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-licensemaster-sts",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				UID: cr.GetUID(),
			}},
		},
		Spec: apps.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "test-container",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("700m"),
								corev1.ResourceMemory: resource.MustParse("384Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("3"),
								corev1.ResourceMemory: resource.MustParse("768Mi"),
							},
						},
					}},
				},
			},
		},
	}
	fakeClient := &FakeListClient{
		sts: []apps.StatefulSet{sts},
	}
	data := make(map[string]interface{})
	collectCRTelData(ctx, fakeClient, crList, data)
	lmData, ok := data["LicenseMaster"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected LicenseMaster data map")
	}
	crData, ok := lmData["test-licensemaster"].([]map[string]string)
	if !ok || len(crData) == 0 {
		t.Fatalf("expected resource data slice")
	}
	container := crData[0]
	if container["cpu_request"] != "700m" || container["memory_request"] != "384Mi" || container["cpu_limit"] != "3" || container["memory_limit"] != "768Mi" {
		t.Errorf("unexpected resource values: got %+v", container)
	}
}

func TestTelemetryCollectCRTelData_SearchHeadClusterData(t *testing.T) {
	ctx := context.TODO()
	cr := &enterpriseApi.SearchHeadCluster{}
	cr.TypeMeta.Kind = "SearchHeadCluster"
	cr.ObjectMeta.Name = "test-shc"
	cr.ObjectMeta.Namespace = "default"
	crList := map[string][]splcommon.MetaObject{"SearchHeadCluster": {cr}}
	sts := apps.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shc-sts",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				UID: cr.GetUID(),
			}},
		},
		Spec: apps.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "test-container",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("800m"),
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("4"),
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
					}},
				},
			},
		},
	}
	fakeClient := &FakeListClient{
		sts: []apps.StatefulSet{sts},
	}
	data := make(map[string]interface{})
	collectCRTelData(ctx, fakeClient, crList, data)
	shcData, ok := data["SearchHeadCluster"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected SearchHeadCluster data map")
	}
	crData, ok := shcData["test-shc"].([]map[string]string)
	if !ok || len(crData) == 0 {
		t.Fatalf("expected resource data slice")
	}
	container := crData[0]
	if container["cpu_request"] != "800m" || container["memory_request"] != "512Mi" || container["cpu_limit"] != "4" || container["memory_limit"] != "1Gi" {
		t.Errorf("unexpected resource values: got %+v", container)
	}
}

func TestTelemetryCollectCRTelData_ClusterManagerData(t *testing.T) {
	ctx := context.TODO()
	cr := &enterpriseApi.ClusterManager{}
	cr.TypeMeta.Kind = "ClusterManager"
	cr.ObjectMeta.Name = "test-cmanager"
	cr.ObjectMeta.Namespace = "default"
	crList := map[string][]splcommon.MetaObject{"ClusterManager": {cr}}
	sts := apps.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cmanager-sts",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				UID: cr.GetUID(),
			}},
		},
		Spec: apps.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "test-container",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("900m"),
								corev1.ResourceMemory: resource.MustParse("640Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("5"),
								corev1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					}},
				},
			},
		},
	}
	fakeClient := &FakeListClient{
		sts: []apps.StatefulSet{sts},
	}
	data := make(map[string]interface{})
	collectCRTelData(ctx, fakeClient, crList, data)
	cmData, ok := data["ClusterManager"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected ClusterManager data map")
	}
	crData, ok := cmData["test-cmanager"].([]map[string]string)
	if !ok || len(crData) == 0 {
		t.Fatalf("expected resource data slice")
	}
	container := crData[0]
	if container["cpu_request"] != "900m" || container["memory_request"] != "640Mi" || container["cpu_limit"] != "5" || container["memory_limit"] != "2Gi" {
		t.Errorf("unexpected resource values: got %+v", container)
	}
}

func TestTelemetryCollectCRTelData_ClusterMasterData(t *testing.T) {
	ctx := context.TODO()
	cr := &enterpriseApiV3.ClusterMaster{}
	cr.TypeMeta.Kind = "ClusterMaster"
	cr.ObjectMeta.Name = "test-cmaster"
	cr.ObjectMeta.Namespace = "default"
	crList := map[string][]splcommon.MetaObject{"ClusterMaster": {cr}}
	sts := apps.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cmaster-sts",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				UID: cr.GetUID(),
			}},
		},
		Spec: apps.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "test-container",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1000m"),
								corev1.ResourceMemory: resource.MustParse("768Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("6"),
								corev1.ResourceMemory: resource.MustParse("4Gi"),
							},
						},
					}},
				},
			},
		},
	}
	fakeClient := &FakeListClient{
		sts: []apps.StatefulSet{sts},
	}
	data := make(map[string]interface{})
	collectCRTelData(ctx, fakeClient, crList, data)
	cmData, ok := data["ClusterMaster"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected ClusterMaster data map")
	}
	crData, ok := cmData["test-cmaster"].([]map[string]string)
	if !ok || len(crData) == 0 {
		t.Fatalf("expected resource data slice")
	}
	container := crData[0]
	if container["cpu_request"] != "1" || container["memory_request"] != "768Mi" || container["cpu_limit"] != "6" || container["memory_limit"] != "4Gi" {
		t.Errorf("unexpected resource values: got %+v", container)
	}
}

func TestTelemetryUpdateLastTransmissionTime_SetsTimestamp(t *testing.T) {
	mockClient := spltest.NewMockClient()
	ctx := context.TODO()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
		Data:       map[string]string{},
	}

	err := updateLastTransmissionTime(ctx, mockClient, cm)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	statusStr, ok := cm.Data[telStatusKey]
	if !ok {
		t.Fatalf("expected telStatusKey in configmap data")
	}
	var status TelemetryStatus
	if err := json.Unmarshal([]byte(statusStr), &status); err != nil {
		t.Fatalf("failed to unmarshal status: %v", err)
	}
	if status.LastTransmission == "" {
		t.Errorf("expected LastTransmission to be set")
	}
	if _, err := time.Parse(time.RFC3339, status.LastTransmission); err != nil {
		t.Errorf("LastTransmission is not RFC3339: %v", status.LastTransmission)
	}
}

func TestTelemetryUpdateLastTransmissionTime_UpdateError(t *testing.T) {
	ctx := context.TODO()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
		Data:       map[string]string{},
	}
	badClient := &errorUpdateClient{}
	err := updateLastTransmissionTime(ctx, badClient, cm)
	if err == nil {
		t.Errorf("expected error from client.Update, got nil")
	}
}

func TestTelemetryUpdateLastTransmissionTime_RepeatedCalls(t *testing.T) {
	mockClient := spltest.NewMockClient()
	ctx := context.TODO()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
		Data:       map[string]string{},
	}
	err := updateLastTransmissionTime(ctx, mockClient, cm)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	firstStatus := cm.Data[telStatusKey]
	time.Sleep(1 * time.Second)
	err = updateLastTransmissionTime(ctx, mockClient, cm)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	secondStatus := cm.Data[telStatusKey]
	if firstStatus == secondStatus {
		t.Errorf("expected status to change on repeated call")
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
