// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

package enterprise

import (
	"context"
	"encoding/json"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetAllCustomResources_Empty(t *testing.T) {
	client := spltest.NewMockClient()
	ctx := context.TODO()
	crMap := getAllCustomResources(ctx, client)
	if len(crMap) != 0 {
		t.Errorf("expected no CRs, got %d", len(crMap))
	}
}

func TestCollectCRTelData_WithMockCR(t *testing.T) {
	client := spltest.NewMockClient()
	ctx := context.TODO()
	cr := &enterpriseApi.Standalone{}
	cr.TypeMeta.Kind = "Standalone"
	cr.ObjectMeta.Name = "test-standalone"
	crList := map[string][]splcommon.MetaObject{"Standalone": {cr}}
	data := make(map[string]interface{})
	collectCRTelData(ctx, client, crList, data)
	if _, ok := data["Standalone"]; !ok {
		t.Errorf("expected Standalone key in data map")
	}
}

func TestApplyTelemetry_ConfigMapNoData(t *testing.T) {
	client := spltest.NewMockClient()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "default"},
		Data:       map[string]string{},
	}
	ctx := context.TODO()
	result, err := ApplyTelemetry(ctx, client, cm)
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

// Additional tests for error paths and success can be added with more advanced mocks.
