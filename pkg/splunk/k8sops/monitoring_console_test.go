// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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
	"strings"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestGetMonitoringConsoleList(t *testing.T) {
	ctx := context.Background()
	mc := enterpriseApi.MonitoringConsole{}
	listOpts := []client.ListOption{client.InNamespace("test")}
	c := spltest.NewMockClient()
	_, err := GetMonitoringConsoleList(ctx, c, &mc, listOpts)
	require.Error(t, err)

	c.ListObj = &enterpriseApi.MonitoringConsoleList{Items: []enterpriseApi.MonitoringConsole{mc}}

	objectList, err := GetMonitoringConsoleList(ctx, c, &mc, listOpts)
	require.NoError(t, err)
	assert.Len(t, objectList.Items, 1)
}

func TestAddAndDeleteURLsConfigMap(t *testing.T) {
	configMap := &corev1.ConfigMap{Data: map[string]string{
		"SPLUNK_STANDALONE_URL": "splunk-test-cr-standalone-0,splunk-other-cr-standalone-0",
	}}

	AddURLsConfigMap(configMap, "test-cr", []corev1.EnvVar{{Name: "SPLUNK_STANDALONE_URL", Value: "splunk-test-cr-standalone-0,splunk-test-cr-standalone-1"}})
	assert.Contains(t, configMap.Data["SPLUNK_STANDALONE_URL"], "splunk-test-cr-standalone-1")
	assert.Contains(t, configMap.Data["SPLUNK_STANDALONE_URL"], "splunk-other-cr-standalone-0")

	DeleteURLsConfigMap(configMap, "test-cr", []corev1.EnvVar{{Name: "SPLUNK_STANDALONE_URL", Value: "splunk-test-cr-standalone-0"}}, false)
	assert.NotContains(t, configMap.Data["SPLUNK_STANDALONE_URL"], "splunk-test-cr-standalone-1")
	assert.Contains(t, configMap.Data["SPLUNK_STANDALONE_URL"], "splunk-other-cr-standalone-0")
}

func TestValidateMonitoringConsoleRef(t *testing.T) {
	ctx := context.TODO()
	currentCM := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console",
			Namespace: "test",
		},
		Data: map[string]string{"a": "b"},
	}

	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-s1-standalone",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Env: []corev1.EnvVar{
								{
									Name:  "SPLUNK_MONITORING_CONSOLE_REF",
									Value: "test",
								},
							},
						},
					},
				},
			},
		},
	}

	revised := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-s1-standalone",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Env: []corev1.EnvVar{
								{
									Name:  "SPLUNK_MONITORING_CONSOLE_REF",
									Value: "abc",
								},
							},
						},
					},
				},
			},
		},
	}

	client := spltest.NewMockClient()

	//create configmap
	_, err := ApplyConfigMap(ctx, client, &currentCM)
	if err != nil {
		t.Errorf("Failed to create the configMap. Error: %s", err.Error())
	}

	// Create statefulset
	err = splutil.CreateResource(ctx, client, current)
	if err != nil {
		t.Errorf("Failed to create owner reference  %s", current.GetName())
	}

	serviceURLs := []corev1.EnvVar{
		{
			Name:  "A",
			Value: "a",
		},
	}

	err = ValidateMonitoringConsoleRef(ctx, client, revised, serviceURLs)
	if err != nil {
		t.Errorf("Couldn't validate monitoring console ref %s", current.GetName())
	}

	revised = &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-s1-standalone",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Env: []corev1.EnvVar{
								{},
							},
						},
					},
				},
			},
		},
	}

	err = ValidateMonitoringConsoleRef(ctx, client, revised, serviceURLs)
	if err != nil {
		t.Errorf("Couldn't validate monitoring console ref %s", current.GetName())
	}
}

func TestApplyMonitoringConsoleEnvConfigMapLifecycle(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
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
	// Scale-down case: current has two CR-owned URLs (test-a,test-b), new keeps only test-b.
	// The stale "test-a" entry must be removed -> Update is expected.
	env = []corev1.EnvVar{
		{Name: "A", Value: "test-b"},
	}
	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": funcCalls}
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

func TestAddMonitoringConsoleURLsMultipleEnvVars(t *testing.T) {
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

	t.Run("Scale down removes stale CR URL", func(t *testing.T) {
		configMap := &corev1.ConfigMap{
			Data: map[string]string{
				"SPLUNK_STANDALONE_URL": "splunk-test-cr-standalone-0,splunk-test-cr-standalone-1",
			},
		}

		newURLs := []corev1.EnvVar{
			{Name: "SPLUNK_STANDALONE_URL", Value: "splunk-test-cr-standalone-0"},
		}

		AddURLsConfigMap(configMap, "test-cr", newURLs)

		got := configMap.Data["SPLUNK_STANDALONE_URL"]
		want := "splunk-test-cr-standalone-0"
		if got != want {
			t.Errorf("Stale peer not removed on scale-down. Got: %q, Want: %q", got, want)
		}
	})

	t.Run("Scale down preserves URLs from other CRs", func(t *testing.T) {
		configMap := &corev1.ConfigMap{
			Data: map[string]string{
				"SPLUNK_STANDALONE_URL": "splunk-test-cr-standalone-0,splunk-test-cr-standalone-1,splunk-other-cr-standalone-0",
			},
		}

		newURLs := []corev1.EnvVar{
			{Name: "SPLUNK_STANDALONE_URL", Value: "splunk-test-cr-standalone-0"},
		}

		AddURLsConfigMap(configMap, "test-cr", newURLs)

		got := configMap.Data["SPLUNK_STANDALONE_URL"]
		if !strings.Contains(got, "splunk-test-cr-standalone-0") {
			t.Errorf("Surviving CR URL missing. Got: %q", got)
		}
		if strings.Contains(got, "splunk-test-cr-standalone-1") {
			t.Errorf("Stale CR URL not removed. Got: %q", got)
		}
		if !strings.Contains(got, "splunk-other-cr-standalone-0") {
			t.Errorf("Other CR URL was incorrectly removed. Got: %q", got)
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

	// Regression: reconciling a CR whose name is a prefix of another's must
	// not evict the longer-named CR's pod URLs ("search-head" vs "search-head-adhoc").
	t.Run("CR name is prefix of another CR name does not evict peer", func(t *testing.T) {
		configMap := &corev1.ConfigMap{
			Data: map[string]string{
				"SPLUNK_STANDALONE_URL": "splunk-search-head-adhoc-standalone-0",
			},
		}
		newURLs := []corev1.EnvVar{
			{Name: "SPLUNK_STANDALONE_URL", Value: "splunk-search-head-standalone-0"},
		}
		AddURLsConfigMap(configMap, "search-head", newURLs)

		got := configMap.Data["SPLUNK_STANDALONE_URL"]
		if !strings.Contains(got, "splunk-search-head-adhoc-standalone-0") {
			t.Errorf("adhoc CR URL was incorrectly evicted by prefix-named CR. Got: %q", got)
		}
		if !strings.Contains(got, "splunk-search-head-standalone-0") {
			t.Errorf("new CR URL was not added. Got: %q", got)
		}

		// Reverse direction: reconciling the longer-named CR must not touch the sibling.
		adhocURLs := []corev1.EnvVar{
			{Name: "SPLUNK_STANDALONE_URL", Value: "splunk-search-head-adhoc-standalone-0"},
		}
		AddURLsConfigMap(configMap, "search-head-adhoc", adhocURLs)

		got = configMap.Data["SPLUNK_STANDALONE_URL"]
		if !strings.Contains(got, "splunk-search-head-adhoc-standalone-0") {
			t.Errorf("adhoc CR URL missing after self-reconcile. Got: %q", got)
		}
		if !strings.Contains(got, "splunk-search-head-standalone-0") {
			t.Errorf("sibling CR URL was incorrectly evicted by adhoc reconcile. Got: %q", got)
		}
	})

	// Regression: same prefix-name ambiguity for service URLs ("cm" vs "cm-extra").
	t.Run("Service URL with prefix CR name does not evict sibling", func(t *testing.T) {
		configMap := &corev1.ConfigMap{
			Data: map[string]string{
				"SPLUNK_CLUSTER_MANAGER_URL": "splunk-cm-extra-cluster-manager-service",
			},
		}
		newURLs := []corev1.EnvVar{
			{Name: "SPLUNK_CLUSTER_MANAGER_URL", Value: "splunk-cm-cluster-manager-service"},
		}
		AddURLsConfigMap(configMap, "cm", newURLs)

		got := configMap.Data["SPLUNK_CLUSTER_MANAGER_URL"]
		if !strings.Contains(got, "splunk-cm-extra-cluster-manager-service") {
			t.Errorf("sibling service URL was incorrectly evicted. Got: %q", got)
		}
		if !strings.Contains(got, "splunk-cm-cluster-manager-service") {
			t.Errorf("new service URL was not added. Got: %q", got)
		}
	})

	// Regression: a sibling CR whose name embeds the shorter CR's kind segment
	// ("cm" vs "cm-cluster-manager-extra") yields a URL that contains the
	// shorter CR's derived prefix as a substring; ownership must compare
	// derived prefixes for equality, not via substring.
	t.Run("Sibling CR embedding shorter kind segment is not evicted", func(t *testing.T) {
		configMap := &corev1.ConfigMap{
			Data: map[string]string{
				"SPLUNK_CLUSTER_MANAGER_URL": "splunk-cm-cluster-manager-extra-cluster-manager-service",
			},
		}
		newURLs := []corev1.EnvVar{
			{Name: "SPLUNK_CLUSTER_MANAGER_URL", Value: "splunk-cm-cluster-manager-service"},
		}
		AddURLsConfigMap(configMap, "cm", newURLs)

		got := configMap.Data["SPLUNK_CLUSTER_MANAGER_URL"]
		if !strings.Contains(got, "splunk-cm-cluster-manager-extra-cluster-manager-service") {
			t.Errorf("sibling CR URL was incorrectly evicted by prefix substring match. Got: %q", got)
		}
		if !strings.Contains(got, "splunk-cm-cluster-manager-service") {
			t.Errorf("new CR URL was not added. Got: %q", got)
		}
	})
}
