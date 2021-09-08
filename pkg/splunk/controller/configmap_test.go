// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func TestApplyConfigMap(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{{MetaName: "*v1.ConfigMap-test-defaults"}}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": funcCalls}
	current := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	revised := current.DeepCopy()
	revised.Data = map[string]string{"a": "b"}
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyConfigMap(c, cr.(*corev1.ConfigMap), false)
		return err
	}
	spltest.ReconcileTester(t, "TestApplyConfigMap", &current, revised, createCalls, updateCalls, reconcile, false)
}

func TestAppyConfigMapForce(t *testing.T) {
	current := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	current.Data = map[string]string{"a": "b"}

	client := spltest.NewMockClient()
	namespacedName := types.NamespacedName{Namespace: current.GetNamespace(), Name: current.GetName()}

	_, err := GetConfigMap(client, namespacedName)
	if err == nil {
		t.Errorf("Should return an error, when the configMap doesn't exist")
	}

	// Initial configmap, no revision label required
	_, err = ApplyConfigMap(client, &current, true)
	if err != nil {
		t.Errorf("Failed to create the configMap. Error: %s", err.Error())
	}
	var firstConfigMap *corev1.ConfigMap
	firstConfigMap, err = GetConfigMap(client, namespacedName)
	if err != nil {
		t.Errorf("Should return an error, when the configMap doesn't exist")
	}
	firstLabels := firstConfigMap.GetLabels()
	if origVal, ok := firstLabels["revision"]; ok || origVal != "" {
		t.Errorf("Failed. Should not have a revision label in configMap. revision=%s", origVal)
	}

	// No data change. Update to configmap, should add revision label
	_, err = ApplyConfigMap(client, &current, true)
	if err != nil {
		t.Errorf("Failed to create the configMap. Error: %s", err.Error())
	}

	var newConfigMap *corev1.ConfigMap
	newConfigMap, err = GetConfigMap(client, namespacedName)
	if err != nil {
		t.Errorf("Should return an error, when the configMap doesn't exist")
	}
	labels := newConfigMap.GetLabels()
	if val, ok := labels["revision"]; !ok || val != "1" {
		t.Errorf("Failed to have correct revision label in configMap. label=%s", val)
	}

	// No data change.  Update to configmap again, should update revision label
	_, err = ApplyConfigMap(client, &current, true)
	if err != nil {
		t.Errorf("Failed to create the configMap. Error: %s", err.Error())
	}

	var updatedConfigMap *corev1.ConfigMap
	updatedConfigMap, err = GetConfigMap(client, namespacedName)
	if err != nil {
		t.Errorf("Should return an error, when the configMap doesn't exist")
	}

	updatedLabels := updatedConfigMap.GetLabels()
	if updateVal, ok := updatedLabels["revision"]; !ok || updateVal != "2" {
		t.Errorf("Failed to update the revision label in configMap. label=%s", updateVal)
	}
}

func TestGetConfigMap(t *testing.T) {
	current := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}

	client := spltest.NewMockClient()
	namespacedName := types.NamespacedName{Namespace: current.GetNamespace(), Name: current.GetName()}

	_, err := GetConfigMap(client, namespacedName)
	if err == nil {
		t.Errorf("Should return an error, when the configMap doesn't exist")
	}

	_, err = ApplyConfigMap(client, &current, false)
	if err != nil {
		t.Errorf("Failed to create the configMap. Error: %s", err.Error())
	}

	_, err = GetConfigMap(client, namespacedName)
	if err != nil {
		t.Errorf("Should not return an error, when the configMap exists")
	}
}

func TestGetConfigMapResourceVersion(t *testing.T) {
	current := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}

	client := spltest.NewMockClient()
	namespacedName := types.NamespacedName{Namespace: current.GetNamespace(), Name: current.GetName()}

	_, err := GetConfigMap(client, namespacedName)
	if err == nil {
		t.Errorf("Should return an error, when the configMap doesn't exist")
	}

	_, err = GetConfigMapResourceVersion(client, namespacedName)
	if err == nil {
		t.Errorf("Should return an error, when the configMap doesn't exist")
	}

	_, err = ApplyConfigMap(client, &current, false)
	if err != nil {
		t.Errorf("Failed to create the configMap. Error: %s", err.Error())
	}

	_, err = GetConfigMapResourceVersion(client, namespacedName)
	if err != nil {
		t.Errorf("Should not return an error, when the configMap exists")
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
