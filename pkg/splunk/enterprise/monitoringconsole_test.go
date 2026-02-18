// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package enterprise

import (
	"context"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func init() {
	// Re-Assigning GetReadinessScriptLocation, GetLivenessScriptLocation, GetStartupScriptLocation to use absolute path for readinessScriptLocation, readinessScriptLocation
	GetReadinessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../" + readinessScriptLocation)
		return fileLocation
	}
	GetLivenessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../" + livenessScriptLocation)
		return fileLocation
	}
	GetStartupScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../" + startupScriptLocation)
		return fileLocation
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
	// test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
		_, err := ApplyMonitoringConsole(context.Background(), c, cr.(*enterpriseApi.MonitoringConsole))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)
}
func TestApplyMonitoringConsoleEnvConfigMap(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.ConfigMap-test-splunk-test-monitoring-console"},
	}
	env := []corev1.EnvVar{
		{Name: "A", Value: "a"},
	}
	newURLsAdded := true
	monitoringConsoleRef := "test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyMonitoringConsoleEnvConfigMap(ctx, c, "test", "test", monitoringConsoleRef, env, newURLsAdded)
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
		Data: map[string]string{"A": "test-a,test-b"},
	}
	newURLsAdded = false
	spltest.ReconcileTester(t, "TestApplyMonitoringConsoleEnvConfigMap", "test", "test", createCalls, updateCalls, reconcile, false, &current)
	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": funcCalls}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}
	env = []corev1.EnvVar{
		{Name: "A", Value: "test-b"},
	}
	current = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console",
			Namespace: "test",
		},
		Data: map[string]string{"A": "test-a,test-b"},
	}
	newURLsAdded = false
	spltest.ReconcileTester(t, "TestApplyMonitoringConsoleEnvConfigMap", "test", "test", createCalls, updateCalls, reconcile, false, &current)
	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": funcCalls}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}
	env = []corev1.EnvVar{
		{Name: "SPLUNK_MULTISITE_MASTER", Value: "test-a"},
	}
	current = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console",
			Namespace: "test",
		},
		Data: map[string]string{"SPLUNK_MULTISITE_MASTER": "test-a", "SPLUNK_SITE": "abc"},
	}
	newURLsAdded = false
	spltest.ReconcileTester(t, "TestApplyMonitoringConsoleEnvConfigMap", "test", "test", createCalls, updateCalls, reconcile, false, &current)
	env = []corev1.EnvVar{
		{Name: "A", Value: "test-a,test-b"},
	}
	current = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console",
			Namespace: "test",
		},
		Data: map[string]string{"A": "test-a,test-b,test-c"},
	}
	newURLsAdded = false
	spltest.ReconcileTester(t, "TestApplyMonitoringConsoleEnvConfigMap", "test", "test", createCalls, updateCalls, reconcile, false, &current)
	env = []corev1.EnvVar{
		{Name: "A", Value: "test-b"},
	}
	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}
	current = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console",
			Namespace: "test",
		},
		Data: map[string]string{"A": "test-a,test-b"},
	}
	newURLsAdded = true
	spltest.ReconcileTester(t, "TestApplyMonitoringConsoleEnvConfigMap", "test", "test", createCalls, updateCalls, reconcile, false, &current)
}

func TestAddURLsConfigMapMultipleEnvVars(t *testing.T) {
	// Scenario: Multiple URL types need to be added/updated
	// The first URL already exists and matches, but subsequent URLs need processing

	t.Run("All URLs processed when first matches", func(t *testing.T) {
		configMap := &corev1.ConfigMap{
			Data: map[string]string{
				"SPLUNK_CLUSTER_MANAGER_URL": "splunk-cm-cluster-manager-service",
				"SPLUNK_INDEXER_URL":         "splunk-idx-indexer-0,splunk-idx-indexer-1",
			},
		}

		newURLs := []corev1.EnvVar{
			{Name: "SPLUNK_CLUSTER_MANAGER_URL", Value: "splunk-cm-cluster-manager-service"},                      // matches
			{Name: "SPLUNK_INDEXER_URL", Value: "splunk-idx-indexer-0,splunk-idx-indexer-1,splunk-idx-indexer-2"}, // scale up
			{Name: "SPLUNK_SEARCH_HEAD_URL", Value: "splunk-sh-search-head-0,splunk-sh-search-head-1"},            // new
		}

		AddURLsConfigMap(configMap, "test-cr", newURLs)

		if configMap.Data["SPLUNK_CLUSTER_MANAGER_URL"] != "splunk-cm-cluster-manager-service" {
			t.Errorf("SPLUNK_CLUSTER_MANAGER_URL was modified, expected unchanged")
		}

		expectedIndexerURL := "splunk-idx-indexer-0,splunk-idx-indexer-1,splunk-idx-indexer-2"
		if configMap.Data["SPLUNK_INDEXER_URL"] != expectedIndexerURL {
			t.Errorf("SPLUNK_INDEXER_URL not updated correctly. Got: %s, Want: %s",
				configMap.Data["SPLUNK_INDEXER_URL"], expectedIndexerURL)
		}

		expectedSearchHeadURL := "splunk-sh-search-head-0,splunk-sh-search-head-1"
		if configMap.Data["SPLUNK_SEARCH_HEAD_URL"] != expectedSearchHeadURL {
			t.Errorf("SPLUNK_SEARCH_HEAD_URL not added. Got: %s, Want: %s",
				configMap.Data["SPLUNK_SEARCH_HEAD_URL"], expectedSearchHeadURL)
		}
	})

	t.Run("Multiple matching URLs all preserved", func(t *testing.T) {
		configMap := &corev1.ConfigMap{
			Data: map[string]string{
				"SPLUNK_CLUSTER_MANAGER_URL": "splunk-cm-cluster-manager-service",
				"SPLUNK_INDEXER_URL":         "splunk-idx-indexer-0,splunk-idx-indexer-1",
				"SPLUNK_SEARCH_HEAD_URL":     "splunk-sh-search-head-0",
				"SPLUNK_LICENSE_MANAGER_URL": "splunk-lm-license-manager-service",
			},
		}

		newURLs := []corev1.EnvVar{
			{Name: "SPLUNK_CLUSTER_MANAGER_URL", Value: "splunk-cm-cluster-manager-service"},
			{Name: "SPLUNK_INDEXER_URL", Value: "splunk-idx-indexer-0,splunk-idx-indexer-1"},
			{Name: "SPLUNK_SEARCH_HEAD_URL", Value: "splunk-sh-search-head-0"},
			{Name: "SPLUNK_LICENSE_MANAGER_URL", Value: "splunk-lm-license-manager-service"},
		}

		AddURLsConfigMap(configMap, "test-cr", newURLs)

		for _, url := range newURLs {
			if val, ok := configMap.Data[url.Name]; !ok {
				t.Errorf("URL %s was removed from ConfigMap (bug manifestation)", url.Name)
			} else if val != url.Value {
				t.Errorf("URL %s was modified. Got: %s, Want: %s", url.Name, val, url.Value)
			}
		}
	})

	t.Run("First URL matches, second is new, third scales up", func(t *testing.T) {
		configMap := &corev1.ConfigMap{
			Data: map[string]string{
				"SPLUNK_CLUSTER_MANAGER_URL": "splunk-cm-cluster-manager-service",
				"SPLUNK_INDEXER_URL":         "splunk-idx-indexer-0",
			},
		}

		newURLs := []corev1.EnvVar{
			{Name: "SPLUNK_CLUSTER_MANAGER_URL", Value: "splunk-cm-cluster-manager-service"}, // matches
			{Name: "SPLUNK_LICENSE_MANAGER_URL", Value: "splunk-lm-license-manager-service"}, // new
			{Name: "SPLUNK_INDEXER_URL", Value: "splunk-idx-indexer-0,splunk-idx-indexer-1"}, // scale up
		}

		AddURLsConfigMap(configMap, "test-cr", newURLs)

		if _, ok := configMap.Data["SPLUNK_CLUSTER_MANAGER_URL"]; !ok {
			t.Errorf("SPLUNK_CLUSTER_MANAGER_URL was removed")
		}

		if _, ok := configMap.Data["SPLUNK_LICENSE_MANAGER_URL"]; !ok {
			t.Errorf("SPLUNK_LICENSE_MANAGER_URL was not added (bug: loop exited early)")
		}

		expectedIndexerURL := "splunk-idx-indexer-0,splunk-idx-indexer-1"
		if configMap.Data["SPLUNK_INDEXER_URL"] != expectedIndexerURL {
			t.Errorf("SPLUNK_INDEXER_URL not scaled up. Got: %s, Want: %s",
				configMap.Data["SPLUNK_INDEXER_URL"], expectedIndexerURL)
		}
	})

	t.Run("Empty ConfigMap with multiple URLs", func(t *testing.T) {
		configMap := &corev1.ConfigMap{
			Data: map[string]string{},
		}

		newURLs := []corev1.EnvVar{
			{Name: "SPLUNK_CLUSTER_MANAGER_URL", Value: "splunk-cm-cluster-manager-service"},
			{Name: "SPLUNK_INDEXER_URL", Value: "splunk-idx-indexer-0,splunk-idx-indexer-1"},
			{Name: "SPLUNK_SEARCH_HEAD_URL", Value: "splunk-sh-search-head-0"},
		}

		AddURLsConfigMap(configMap, "test-cr", newURLs)

		for _, url := range newURLs {
			if val, ok := configMap.Data[url.Name]; !ok {
				t.Errorf("URL %s was not added to empty ConfigMap", url.Name)
			} else if val != url.Value {
				t.Errorf("URL %s has wrong value. Got: %s, Want: %s", url.Name, val, url.Value)
			}
		}
	})
}

func TestGetMonitoringConsoleStatefulSet(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.MonitoringConsole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	c := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Errorf("Failed to create namespace scoped object")
	}
	test := func(want string) {
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
	cr.Spec.Volumes = []corev1.Volume{
		{Name: "defaults"},
	}
	test(loadFixture(t, "statefulset_stack1_monitoring_console_with_service_account_2.json"))
	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	test(loadFixture(t, "statefulset_stack1_monitoring_console_base.json"))
	// Create a serviceaccount
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	test(loadFixture(t, "statefulset_stack1_monitoring_console_base_1.json"))
	// Add extraEnv
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	test(loadFixture(t, "statefulset_stack1_monitoring_console_with_service_account.json"))
	// Add additional label to cr metadata to transfer to the statefulset
	cr.ObjectMeta.Labels = make(map[string]string)
	cr.ObjectMeta.Labels["app.kubernetes.io/test-extra-label"] = "test-extra-label-value"
	test(loadFixture(t, "statefulset_stack1_monitoring_console_with_service_account_1.json"))
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
	// Assert that an error is returned
	if err == nil {
		t.Errorf("Expected error when SPLUNK_GENERAL_TERMS is not set, but got none")
	} else if err.Error() != "license not accepted, please adjust SPLUNK_GENERAL_TERMS to indicate you have accepted the current/latest version of the license. See README file for additional information" {
		t.Errorf("Unexpected error message: %v", err)
	}
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
	splclient.RegisterRemoteDataClient(ctx, "aws")
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
		vol, err = splclient.GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
		if err != nil {
			allSuccess = false
			continue
		}
		// Update the GetRemoteDataClient with our mock call which initializes mock AWS client
		getClientWrapper := splclient.RemoteDataClientsMap[vol.Provider]
		getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, splclient.NewMockAWSS3Client)
		remoteDataClientMgr := &RemoteDataClientManager{client: client,
			cr: &cr, appFrameworkRef: &cr.Spec.AppFrameworkConfig,
			vol:      &vol,
			location: appSource.Location,
			initFn: func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
				cl := spltest.MockAWSS3Client{}
				cl.Objects = mockAwsObjects[index].Objects
				return cl
			},
			getRemoteDataClient: func(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec, location string, fn splclient.GetInitFunc) (splclient.SplunkRemoteDataClient, error) {
				c, err := GetRemoteStorageClient(ctx, client, cr, appFrameworkRef, vol, location, fn)
				return c, err
			},
		}
		RemoteDataListResponse, err := remoteDataClientMgr.GetAppsList(ctx)
		if err != nil {
			allSuccess = false
			continue
		}
		var mockResponse spltest.MockRemoteDataClient
		mockResponse, err = splclient.ConvertRemoteDataListResponse(ctx, RemoteDataListResponse)
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
	splclient.RegisterRemoteDataClient(ctx, "aws")
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
	vol, err = splclient.GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
	if err != nil {
		t.Errorf("Unable to get Volume due to error=%s", err)
	}
	// Update the GetRemoteDataClient with our mock call which initializes mock AWS client
	getClientWrapper := splclient.RemoteDataClientsMap[vol.Provider]
	getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, splclient.NewMockAWSS3Client)
	remoteDataClientMgr := &RemoteDataClientManager{
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
			location string, fn splclient.GetInitFunc) (splclient.SplunkRemoteDataClient, error) {
			// Get the mock client
			c, err := GetRemoteStorageClient(ctx, client, cr, appFrameworkRef, vol, location, fn)
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
func TestMonitoringConsoleWithReadyState(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))
	builder := fake.NewClientBuilder().
		WithScheme(sch).
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
	// simulate create clustermanager instance before reconcilation
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
	// to pass the validation stage, add the directory to download apps
	err = os.MkdirAll(splcommon.AppDownloadVolume, 0755)
	defer os.RemoveAll(splcommon.AppDownloadVolume)
	if err != nil {
		t.Errorf("Unable to create download directory for apps :%s", splcommon.AppDownloadVolume)
	}
	_, err = ApplyMonitoringConsole(ctx, c, &mc)
	if err != nil {
		t.Errorf("ApplyMonitoringConsole should not have returned error here.")
	}
}
func TestGetMonitoringConsoleList(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	mc := enterpriseApi.MonitoringConsole{}
	listOpts := []client.ListOption{
		client.InNamespace("test"),
	}
	client := spltest.NewMockClient()
	mcList := &enterpriseApi.MonitoringConsoleList{}
	mcList.Items = append(mcList.Items, mc)
	client.ListObj = mcList
	objectList, err := getMonitoringConsoleList(ctx, client, &mc, listOpts)
	if err != nil {
		t.Errorf("getNumOfObjects should not have returned error=%v", err)
	}
	numOfObjects := len(objectList.Items)
	if numOfObjects != 1 {
		t.Errorf("Got wrong number of IndexerCluster objects. Expected=%d, Got=%d", 1, numOfObjects)
	}
}
func TestChangeMonitoringConsoleAnnotations(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))
	builder := fake.NewClientBuilder().
		WithScheme(sch).
		WithStatusSubresource(&enterpriseApi.LicenseManager{}).
		WithStatusSubresource(&enterpriseApi.ClusterManager{}).
		WithStatusSubresource(&enterpriseApi.Standalone{}).
		WithStatusSubresource(&enterpriseApi.MonitoringConsole{}).
		WithStatusSubresource(&enterpriseApi.IndexerCluster{}).
		WithStatusSubresource(&enterpriseApi.SearchHeadCluster{})
	client := builder.Build()
	utilruntime.Must(enterpriseApi.AddToScheme(clientgoscheme.Scheme))
	// define CM and MC
	cm := &enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				Volumes: []corev1.Volume{},
			},
		},
	}
	mc := &enterpriseApi.MonitoringConsole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: enterpriseApi.MonitoringConsoleSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				Volumes: []corev1.Volume{},
				ClusterManagerRef: corev1.ObjectReference{
					Name: "test",
				},
			},
		},
	}
	cm.Spec.Image = "splunk/splunk:latest"
	// Create the instances
	client.Create(ctx, cm)
	_, err := ApplyClusterManager(ctx, client, cm)
	if err != nil {
		t.Errorf("applyClusterManager should not have returned error; err=%v", err)
	}
	namespacedName := types.NamespacedName{
		Name:      cm.Name,
		Namespace: cm.Namespace,
	}
	err = client.Get(ctx, namespacedName, cm)
	if err != nil {
		t.Errorf("changeMonitoringConsoleAnnotations should not have returned error=%v", err)
	}
	cm.Status.Phase = enterpriseApi.PhaseReady
	err = client.Status().Update(ctx, cm)
	if err != nil {
		t.Errorf("Unexpected update pod  %v", err)
		debug.PrintStack()
	}
	client.Create(ctx, mc)
	_, err = ApplyMonitoringConsole(ctx, client, mc)
	if err != nil {
		t.Errorf("applyMonitoringConsole should not have returned error; err=%v", err)
	}
	err = changeMonitoringConsoleAnnotations(ctx, client, cm)
	if err != nil {
		t.Errorf("changeMonitoringConsoleAnnotations should not have returned error=%v", err)
	}
	monitoringConsole := &enterpriseApi.MonitoringConsole{}
	namespacedName = types.NamespacedName{
		Name:      cm.Name,
		Namespace: cm.Namespace,
	}
	err = client.Get(ctx, namespacedName, monitoringConsole)
	if err != nil {
		t.Errorf("changeMonitoringConsoleAnnotations should not have returned error=%v", err)
	}
	annotations := monitoringConsole.GetAnnotations()
	if annotations["splunk/image-tag"] != cm.Spec.Image {
		t.Errorf("changeMonitoringConsoleAnnotations should have set the checkUpdateImage annotation field to the current image")
	}
}
