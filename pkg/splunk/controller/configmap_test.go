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

package controller

import (
	"context"
	"errors"
	"reflect"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

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
	client.InduceErrorKind[splcommon.MockClientInduceErrorGet] = errors.New("randomerror")
	_, err = GetMCConfigMap(ctx, client, &cr, namespacedName)
	if err == nil {
		t.Errorf("Should return an error")
	}

	client.InduceErrorKind[splcommon.MockClientInduceErrorGet] = k8serrors.NewNotFound(appsv1.Resource("configmap"), current.GetName())
	client.InduceErrorKind[splcommon.MockClientInduceErrorCreate] = errors.New("randomerror")
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
