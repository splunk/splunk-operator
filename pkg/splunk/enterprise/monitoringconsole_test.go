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

package enterprise

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v2"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func TestApplyMonitoringConsole(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Service-test-splunk-stack1-monitoring-console-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-monitoring-console-service"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-monitoring-console-secret-v1"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-monitoring-console"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-monitoring-console"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-monitoring-console"},
	}
	labels := map[string]string{
		"app.kubernetes.io/component":  "versionedSecrets",
		"app.kubernetes.io/managed-by": "splunk-operator",
	}
	listOpts := []client.ListOption{
		client.InNamespace("test"),
		client.MatchingLabels(labels),
	}
	listmockCall := []spltest.MockFuncCall{
		{ListOpts: listOpts}}

	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0], funcCalls[2], funcCalls[3], funcCalls[5], funcCalls[6], funcCalls[8]}, "Update": {funcCalls[0], funcCalls[7]}, "List": {listmockCall[0]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[2], funcCalls[3], funcCalls[4], funcCalls[5], funcCalls[6], funcCalls[7], funcCalls[8]}, "Update": {funcCalls[8]}, "List": {listmockCall[0]}}
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
		_, err := ApplyMonitoringConsole(c, cr.(*enterpriseApi.MonitoringConsole))
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyMonitoringConsole", &current, revised, createCalls, updateCalls, reconcile, true)

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
		_, err := ApplyMonitoringConsole(c, cr.(*enterpriseApi.MonitoringConsole))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)
}

func TestApplyMonitoringConsoleEnvConfigMap(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.ConfigMap-test-splunk-test-monitoring-console"},
	}

	env := []corev1.EnvVar{
		{Name: "A", Value: "a"},
	}

	newURLsAdded := true
	monitoringConsoleRef := "test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyMonitoringConsoleEnvConfigMap(c, "test", "test", monitoringConsoleRef, env, newURLsAdded)
		return err
	}

	//if monitoring-console env configMap doesn't exist, then create one
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls}

	spltest.ReconcileTester(t, "TestApplyMonitoringConsoleEnvConfigMap", "test", "test", createCalls, updateCalls, reconcile, false)

	//if configMap exists then update it
	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": funcCalls}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}

	newURLsAdded = true

	current := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console",
			Namespace: "test",
		},
		Data: map[string]string{"a": "b"},
	}

	spltest.ReconcileTester(t, "TestApplyMonitoringConsoleEnvConfigMap", "test", "test", createCalls, updateCalls, reconcile, false, &current)

	//check for deletion
	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}

	newURLsAdded = false

	current = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console",
			Namespace: "test",
		},
		Data: map[string]string{"a": "b"},
	}

	spltest.ReconcileTester(t, "TestApplyMonitoringConsoleEnvConfigMap", "test", "test", createCalls, updateCalls, reconcile, false, &current)

	//no configMap exist and try to do deletion then just create a empty configMap
	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}

	newURLsAdded = false

	spltest.ReconcileTester(t, "TestApplyMonitoringConsoleEnvConfigMap", "test", "test", createCalls, updateCalls, reconcile, false)

	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}

	current = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console",
			Namespace: "test",
		},
		Data: map[string]string{"A": "a,b"},
	}
	newURLsAdded = false

	spltest.ReconcileTester(t, "TestApplyMonitoringConsoleEnvConfigMap", "test", "test", createCalls, updateCalls, reconcile, false, &current)

	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": funcCalls}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}

	env = []corev1.EnvVar{
		{Name: "A", Value: "test-a"},
	}

	current = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console",
			Namespace: "test",
		},
		Data: map[string]string{"A": "test-a"},
	}
	newURLsAdded = false

	spltest.ReconcileTester(t, "TestApplyMonitoringConsoleEnvConfigMap", "test", "test", createCalls, updateCalls, reconcile, false, &current)

	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": funcCalls}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}

	env = []corev1.EnvVar{
		{Name: "A", Value: "test-a,test-b"},
	}

	current = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console",
			Namespace: "test",
		},
		Data: map[string]string{"A": "test-a"},
	}
	newURLsAdded = true

	spltest.ReconcileTester(t, "TestApplyMonitoringConsoleEnvConfigMap", "test", "test", createCalls, updateCalls, reconcile, false, &current)
}
