// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func TestApplyMonitoringConsole(t *testing.T) {
	//create secrets, take dummy URL pass it to my function
	standaloneCR := enterprisev1.Standalone{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-monitoring-console-secret-v1"},
		{MetaName: "*v1.Service-test-splunk-test-monitoring-console-service"},
		{MetaName: "*v1.Service-test-splunk-test-monitoring-console-headless"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-monitoring-console-defaults"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-monitoring-console-defaults"},
		{MetaName: "*v1.Deployment-test-splunk-test-monitoring-console"},
	}
	listOpts := []client.ListOption{
		client.InNamespace("test"),
	}
	listmockCall := []spltest.MockFuncCall{
		{ListOpts: listOpts}}

	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[1], funcCalls[2], funcCalls[3], funcCalls[4], funcCalls[6]}, "List": {listmockCall[0]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": {funcCalls[6]}, "List": {listmockCall[0]}}
	c := spltest.NewMockClient()

	// Create namespace scoped secret
	namespacescopedsecret, err := ApplyNamespaceScopedSecretObject(c, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	standaloneCR.Spec.Defaults = "defaults-yaml"
	standaloneRevised := standaloneCR.DeepCopy()
	standaloneRevised.Spec.Image = "splunk/test"
	env := []corev1.EnvVar{
		{Name: "A", Value: "a"},
	}
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		obj := cr.(*enterprisev1.Standalone)
		err := ApplyMonitoringConsole(c, obj, obj.Spec.CommonSplunkSpec, env)
		return err
	}
	spltest.ReconcileTester(t, "TestApplyMonitoringConsole", &standaloneCR, standaloneRevised, createCalls, updateCalls, reconcile, true, namespacescopedsecret)
}

func TestApplyMonitoringConsoleEnvConfigMap(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.ConfigMap-test-splunk-test-monitoring-console-defaults"},
	}

	env := []corev1.EnvVar{
		{Name: "A", Value: "a"},
	}

	newURLsAdded := true
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyMonitoringConsoleEnvConfigMap(c, "test", env, newURLsAdded)
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
			Name:      "splunk-test-monitoring-console-defaults",
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
			Name:      "splunk-test-monitoring-console-defaults",
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

	//extra check for "SPLUNK_SEARCH_HEAD_URL"

	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": funcCalls}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}

	env = []corev1.EnvVar{
		{Name: "SPLUNK_SEARCH_HEAD_URL", Value: "splunk-test-search-head-0.splunk-test-search-head-headless.default.svc.cluster.local"},
	}
	newURLsAdded = true

	current = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console-defaults",
			Namespace: "test",
		},
		Data: map[string]string{"SPLUNK_SEARCH_HEAD_URL": "splunk-test-search-head-1.splunk-test-search-head-headless.default.svc.cluster.local,splunk-test-search-head-0.splunk-test-search-head-headless.default.svc.cluster.local,splunk-test-search-head-2.splunk-test-search-head-headless.default.svc.cluster.local"},
	}
	spltest.ReconcileTester(t, "TestApplyMonitoringConsoleEnvConfigMap", "test", "test", createCalls, updateCalls, reconcile, false, &current)

	//more SH scenarios
	//extra check for "SPLUNK_SEARCH_HEAD_URL"

	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": funcCalls}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}

	env = []corev1.EnvVar{
		{Name: "SPLUNK_SEARCH_HEAD_URL", Value: "splunk-test-search-head-1.splunk-test-search-head-headless.default.svc.cluster.local,splunk-test-search-head-0.splunk-test-search-head-headless.default.svc.cluster.local,splunk-test-search-head-2.splunk-test-search-head-headless.default.svc.cluster.local"},
	}
	newURLsAdded = true

	current = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console-defaults",
			Namespace: "test",
		},
		Data: map[string]string{"SPLUNK_SEARCH_HEAD_URL": "splunk-test-search-head-0.splunk-test-search-head-headless.default.svc.cluster.local"},
	}
	spltest.ReconcileTester(t, "TestApplyMonitoringConsoleEnvConfigMap", "test", "test", createCalls, updateCalls, reconcile, false, &current)

	//extra check for "SPLUNK_SEARCH_HEAD_URL"

	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": funcCalls}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}

	env = []corev1.EnvVar{
		{Name: "SPLUNK_SEARCH_HEAD_URL", Value: "splunk-test-search-head-1.splunk-test-search-head-headless.default.svc.cluster.local,splunk-test-search-head-0.splunk-test-search-head-headless.default.svc.cluster.local,splunk-test-search-head-2.splunk-test-search-head-headless.default.svc.cluster.local"},
	}
	newURLsAdded = true

	current = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console-defaults",
			Namespace: "test",
		},
		Data: map[string]string{"SPLUNK_SEARCH_HEAD_URL": "splunk-test-search-head-0.splunk-test-search-head-headless.default.svc.cluster.local"},
	}
	spltest.ReconcileTester(t, "TestApplyMonitoringConsoleEnvConfigMap", "test", "test", createCalls, updateCalls, reconcile, false, &current)

	//test if url.Value contains any prefix/suffix/,,
	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": funcCalls}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}

	newURLsAdded = false

	current = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console-defaults",
			Namespace: "test",
		},
		Data: map[string]string{"a": "b,c"},
	}

	env = []corev1.EnvVar{
		{Name: "a", Value: "b"},
	}
	spltest.ReconcileTester(t, "TestApplyMonitoringConsoleEnvConfigMap", "test", "test", createCalls, updateCalls, reconcile, false, &current)

	//check more Deletion scenarios
	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": funcCalls}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}

	newURLsAdded = false

	current = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console-defaults",
			Namespace: "test",
		},
		Data: map[string]string{"a": "b,a,c"},
	}

	env = []corev1.EnvVar{
		{Name: "a", Value: "b"},
	}
	spltest.ReconcileTester(t, "TestApplyMonitoringConsoleEnvConfigMap", "test", "test", createCalls, updateCalls, reconcile, false, &current)

	//check more Deletion scenarios
	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": funcCalls}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}

	newURLsAdded = false

	current = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console-defaults",
			Namespace: "test",
		},
		Data: map[string]string{"a": "b,a,c"},
	}

	env = []corev1.EnvVar{
		{Name: "a", Value: "c"},
	}
	spltest.ReconcileTester(t, "TestApplyMonitoringConsoleEnvConfigMap", "test", "test", createCalls, updateCalls, reconcile, false, &current)
}
