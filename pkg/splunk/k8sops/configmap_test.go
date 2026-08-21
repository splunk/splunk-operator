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

package k8sops

import (
	"context"
	"errors"
	"reflect"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

func TestApplyConfigMap(t *testing.T) {
	ctx := context.TODO()
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.ConfigMap-test-defaults"},
		{MetaName: "*v1.ConfigMap-test-defaults"},
	}

	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0]}, "Update": {funcCalls[0]}}

	current := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	revised := current.DeepCopy()
	revised.Data = map[string]string{"a": "b"}
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyConfigMap(ctx, c, cr.(*corev1.ConfigMap))
		return err
	}
	spltest.ReconcileTester(t, "TestApplyConfigMap", &current, revised, createCalls, updateCalls, reconcile, false)

	// Update owner references test
	c := spltest.NewMockClient()
	c.AddObject(revised)
	revisedWithOr := revised.DeepCopy()
	revisedWithOr.OwnerReferences = append(revised.OwnerReferences, metav1.OwnerReference{
		Name: "DummyOR",
	})
	_, _ = ApplyConfigMap(ctx, c, revisedWithOr)

	// Induce a get error
	errorCm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	c.InduceErrorKind[splcommon.MockClientInduceErrorGet] = k8serrors.NewNotFound(appsv1.Resource("configmap"), errorCm.GetName())
	_, _ = ApplyConfigMap(ctx, c, &errorCm)
}

func TestGetConfigMap(t *testing.T) {
	ctx := context.TODO()
	current := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}

	client := spltest.NewMockClient()
	namespacedName := types.NamespacedName{Namespace: current.GetNamespace(), Name: current.GetName()}

	_, err := GetConfigMap(ctx, client, namespacedName)
	if err == nil {
		t.Errorf("Should return an error, when the configMap doesn't exist")
	}

	_, err = ApplyConfigMap(ctx, client, &current)
	if err != nil {
		t.Errorf("Failed to create the configMap. Error: %s", err.Error())
	}

	_, err = GetConfigMap(ctx, client, namespacedName)
	if err != nil {
		t.Errorf("Should not return an error, when the configMap exists")
	}
}

func TestGetConfigMapResourceVersion(t *testing.T) {
	ctx := context.TODO()
	current := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}

	client := spltest.NewMockClient()
	namespacedName := types.NamespacedName{Namespace: current.GetNamespace(), Name: current.GetName()}

	_, err := GetConfigMap(ctx, client, namespacedName)
	if err == nil {
		t.Errorf("Should return an error, when the configMap doesn't exist")
	}

	_, err = GetConfigMapResourceVersion(ctx, client, namespacedName)
	if err == nil {
		t.Errorf("Should return an error, when the configMap doesn't exist")
	}

	_, err = ApplyConfigMap(ctx, client, &current)
	if err != nil {
		t.Errorf("Failed to create the configMap. Error: %s", err.Error())
	}

	_, err = GetConfigMapResourceVersion(ctx, client, namespacedName)
	if err != nil {
		t.Errorf("Should not return an error, when the configMap exists")
	}
}

func TestGetConfigMapDataHash(t *testing.T) {
	ctx := context.TODO()
	client := spltest.NewMockClient()
	namespacedName := types.NamespacedName{Namespace: "test", Name: "defaults"}

	// Returns error when ConfigMap does not exist.
	_, err := GetConfigMapDataHash(ctx, client, namespacedName, nil)
	if err == nil {
		t.Errorf("expected error when ConfigMap does not exist, got nil")
	}

	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "defaults", Namespace: "test"},
		Data: map[string]string{
			"default.yml": "splunk:\n  key: value\n",
			"extra.conf":  "key = val",
		},
	}
	_, err = ApplyConfigMap(ctx, client, &cm)
	if err != nil {
		t.Fatalf("failed to create ConfigMap: %v", err)
	}

	hash1, err := GetConfigMapDataHash(ctx, client, namespacedName, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hash1) != 16 {
		t.Errorf("expected 16-char hash, got %q (len=%d)", hash1, len(hash1))
	}

	// Same data must produce the same hash (deterministic / no map-iteration variance).
	hash2, err := GetConfigMapDataHash(ctx, client, namespacedName, nil)
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if hash1 != hash2 {
		t.Errorf("hash is not deterministic: first=%q second=%q", hash1, hash2)
	}

	// Changing data must produce a different hash.
	cm2 := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "defaults", Namespace: "test"},
		Data: map[string]string{
			"default.yml": "splunk:\n  key: CHANGED\n",
			"extra.conf":  "key = val",
		},
	}
	_, err = ApplyConfigMap(ctx, client, &cm2)
	if err != nil {
		t.Fatalf("failed to update ConfigMap: %v", err)
	}
	hashChanged, err := GetConfigMapDataHash(ctx, client, namespacedName, nil)
	if err != nil {
		t.Fatalf("unexpected error after data change: %v", err)
	}
	if hashChanged == hash1 {
		t.Errorf("expected hash to change after data update, but got same value %q", hash1)
	}

	// Metadata-only change (simulated by restoring original data) must yield original hash.
	_, err = ApplyConfigMap(ctx, client, &cm)
	if err != nil {
		t.Fatalf("failed to restore ConfigMap: %v", err)
	}
	hashRestored, err := GetConfigMapDataHash(ctx, client, namespacedName, nil)
	if err != nil {
		t.Fatalf("unexpected error after restore: %v", err)
	}
	if hashRestored != hash1 {
		t.Errorf("expected restored hash %q to equal original hash %q", hashRestored, hash1)
	}

	// Items filter: changing an unmounted key must NOT change the hash.
	items := []corev1.KeyToPath{{Key: "default.yml", Path: "default.yml"}}
	hashFiltered, err := GetConfigMapDataHash(ctx, client, namespacedName, items)
	if err != nil {
		t.Fatalf("unexpected error with items filter: %v", err)
	}
	// Now update only "extra.conf" (not in items) — hash must stay the same.
	cm3 := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "defaults", Namespace: "test"},
		Data: map[string]string{
			"default.yml": "splunk:\n  key: value\n",
			"extra.conf":  "CHANGED",
		},
	}
	_, err = ApplyConfigMap(ctx, client, &cm3)
	if err != nil {
		t.Fatalf("failed to update unmounted key: %v", err)
	}
	hashFilteredAfter, err := GetConfigMapDataHash(ctx, client, namespacedName, items)
	if err != nil {
		t.Fatalf("unexpected error after unmounted key change: %v", err)
	}
	if hashFilteredAfter != hashFiltered {
		t.Errorf("hash should not change when only an unmounted key changes: before=%q after=%q", hashFiltered, hashFilteredAfter)
	}
	// Changing the mounted key must produce a different hash.
	cm4 := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "defaults", Namespace: "test"},
		Data: map[string]string{
			"default.yml": "splunk:\n  key: MOUNTED_CHANGED\n",
			"extra.conf":  "CHANGED",
		},
	}
	_, err = ApplyConfigMap(ctx, client, &cm4)
	if err != nil {
		t.Fatalf("failed to update mounted key: %v", err)
	}
	hashFilteredMountedChanged, err := GetConfigMapDataHash(ctx, client, namespacedName, items)
	if err != nil {
		t.Fatalf("unexpected error after mounted key change: %v", err)
	}
	if hashFilteredMountedChanged == hashFiltered {
		t.Errorf("hash should change when a mounted key changes: before=%q after=%q", hashFiltered, hashFilteredMountedChanged)
	}
}

// TestGetConfigMapDataHashNoAmbiguity verifies that the length-delimited framing prevents
// hash collisions between ConfigMaps whose key/value content would be indistinguishable
// under naive "key=value\n" concatenation — e.g. {"a":"x\nb=y"} vs {"a":"x","b":"y"}.
func TestGetConfigMapDataHashNoAmbiguity(t *testing.T) {
	ctx := context.TODO()
	client := spltest.NewMockClient()

	cmAmbig1 := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "ambig", Namespace: "test"},
		Data:       map[string]string{"a": "x\nb=y"},
	}
	_, err := ApplyConfigMap(ctx, client, &cmAmbig1)
	if err != nil {
		t.Fatalf("failed to create ConfigMap: %v", err)
	}
	nn := types.NamespacedName{Namespace: "test", Name: "ambig"}
	hash1, err := GetConfigMapDataHash(ctx, client, nn, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmAmbig2 := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "ambig", Namespace: "test"},
		Data:       map[string]string{"a": "x", "b": "y"},
	}
	_, err = ApplyConfigMap(ctx, client, &cmAmbig2)
	if err != nil {
		t.Fatalf("failed to update ConfigMap: %v", err)
	}
	hash2, err := GetConfigMapDataHash(ctx, client, nn, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hash1 == hash2 {
		t.Errorf("ambiguous framing: {\"a\":\"x\\nb=y\"} and {\"a\":\"x\",\"b\":\"y\"} produced the same hash %q", hash1)
	}
}

func TestGetMCConfigMap(t *testing.T) {
	ctx := context.TODO()
	current := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}

	cr := enterpriseApi.MonitoringConsole{
		TypeMeta: metav1.TypeMeta{
			Kind: "MonitoringConsole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	client := spltest.NewMockClient()
	namespacedName := types.NamespacedName{Namespace: current.GetNamespace(), Name: current.GetName()}

	_, err := GetMCConfigMap(ctx, client, &cr, namespacedName)
	if err != nil {
		t.Errorf("Should never return an error as it should have created a empty configmap")
	}

	_, err = ApplyConfigMap(ctx, client, &current)
	if err != nil {
		t.Errorf("Failed to create the configMap. Error: %s", err.Error())
	}

	_, err = GetMCConfigMap(ctx, client, &cr, namespacedName)
	if err != nil {
		t.Errorf("Should not return an error, when the configMap exists")
	}

	// Error testing
	client.InduceErrorKind[splcommon.MockClientInduceErrorGet] = errors.New(splcommon.Rerr)
	_, err = GetMCConfigMap(ctx, client, &cr, namespacedName)
	if err == nil {
		t.Errorf("Should return an error")
	}

	client.InduceErrorKind[splcommon.MockClientInduceErrorGet] = k8serrors.NewNotFound(appsv1.Resource("configmap"), current.GetName())
	client.InduceErrorKind[splcommon.MockClientInduceErrorCreate] = errors.New(splcommon.Rerr)
	_, err = GetMCConfigMap(ctx, client, &cr, namespacedName)
	if err == nil {
		t.Errorf("Should return an error")
	}

	client.InduceErrorKind[splcommon.MockClientInduceErrorCreate] = nil
	_, err = GetMCConfigMap(ctx, client, &cr, namespacedName)
	if err == nil {
		t.Errorf("Should return an error")
	}
}

func TestSetConfigMapOwnerRef(t *testing.T) {
	ctx := context.TODO()
	current := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	cr := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console",
			Namespace: "test",
		},
	}
	namespacedName := types.NamespacedName{Namespace: "test", Name: "splunk-test-monitoring-console"}

	err := SetConfigMapOwnerRef(ctx, c, &cr, namespacedName)
	if !k8serrors.IsNotFound(err) {
		t.Errorf("Couldn't detect resource %s", current.GetName())
	}

	// Create statefulset
	err = splutil.CreateResource(ctx, c, &cr)
	if err != nil {
		t.Errorf("Failed to create resource  statefulset %s", current.GetName())
	}

	//create configmap
	_, err = ApplyConfigMap(ctx, c, &current)
	if err != nil {
		t.Errorf("Failed to create the configMap. Error: %s", err.Error())
	}

	// Test existing owner reference
	err = SetConfigMapOwnerRef(ctx, c, &cr, namespacedName)
	if err != nil {
		t.Errorf("Couldn't set owner ref for resource configmap %s", current.GetName())
	}

	// Try adding same owner again
	err = SetConfigMapOwnerRef(ctx, c, &cr, namespacedName)
	if err != nil {
		t.Errorf("Couldn't set owner ref for resource configmap %s", current.GetName())
	}
}

func TestPrepareConfigMap(t *testing.T) {
	var configMapName = "testConfgMap"
	var namespace = "testNameSpace"
	expectedCm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
		},
	}

	dataMap := make(map[string]string)
	dataMap["a"] = "x"
	dataMap["b"] = "y"
	dataMap["z"] = "z"
	expectedCm.Data = dataMap

	returnedCM := PrepareConfigMap(configMapName, namespace, dataMap)

	if !reflect.DeepEqual(expectedCm, returnedCM) {
		t.Errorf("configMap preparation failed")
	}
}
