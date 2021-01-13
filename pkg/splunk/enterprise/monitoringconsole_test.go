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
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1beta1"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

func TestApplyMonitoringConsole(t *testing.T) {
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
		{MetaName: "*v1.ConfigMap-test-splunk-test-monitoring-console"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-monitoring-console"},
		{MetaName: "*v1.StatefulSet-test-splunk-test-monitoring-console"},
		{MetaName: "*v1.StatefulSet-test-splunk-test-monitoring-console"},
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

	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[1], funcCalls[2], funcCalls[3], funcCalls[5]}, "Update": {funcCalls[6], funcCalls[6]}, "List": {listmockCall[0]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": {funcCalls[7]}, "List": {listmockCall[0]}}
	c := spltest.NewMockClient()

	// Create namespace scoped secret
	namespacescopedsecret, err := splutil.ApplyNamespaceScopedSecretObject(c, "test")
	if err != nil {
		t.Errorf(err.Error())
	}
	standaloneCR.Spec.Defaults = "defaults-yaml"
	standaloneRevised := standaloneCR.DeepCopy()
	standaloneRevised.Spec.Image = "splunk/test"
	env := []corev1.EnvVar{
		{Name: "A", Value: "a"},
	}

	statefulset := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console",
			Namespace: "test",
		},
	}
	_, err = splctrl.ApplyStatefulSet(c, &statefulset)
	if err != nil {
		return
	}

	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		obj := cr.(*enterprisev1.Standalone)
		err := ApplyMonitoringConsole(c, obj, obj.Spec.CommonSplunkSpec, env)
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyMonitoringConsole", &standaloneCR, standaloneRevised, createCalls, updateCalls, reconcile, true, namespacescopedsecret, &statefulset)
}

func TestApplyMonitoringConsoleEnvConfigMap(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.ConfigMap-test-splunk-test-monitoring-console"},
	}

	env := []corev1.EnvVar{
		{Name: "A", Value: "a"},
	}

	newURLsAdded := true
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyMonitoringConsoleEnvConfigMap(c, "test", "test", env, newURLsAdded)
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

func TestGetMonitoringConsoleStatefulSet(t *testing.T) {
	cr := enterprisev1.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterprisev1.SearchHeadClusterSpec{
			Replicas: 3,
			CommonSplunkSpec: enterprisev1.CommonSplunkSpec{
				ClusterMasterRef: corev1.ObjectReference{
					Name: "stack1",
				},
			},
		},
	}

	mcConfigMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console",
			Namespace: "test",
		},
		Data: map[string]string{"a": "b"},
	}

	c := spltest.NewMockClient()
	c.AddObject(&mcConfigMap)
	_, err := splutil.ApplyNamespaceScopedSecretObject(c, "test")
	if err != nil {
		t.Errorf("Failed to create namespace scoped object")
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := validateSearchHeadClusterSpec(&cr.Spec); err != nil {
				t.Errorf("validateSearchHeadClusterSpec() returned error: %v", err)
			}
			return getMonitoringConsoleStatefulSet(c, &cr, &cr.Spec.CommonSplunkSpec, SplunkMonitoringConsole, "splunk-test-secret")
		}
		configTester(t, fmt.Sprintf("getMonitoringConsoleStatefulSet"), f, want)
	}

	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-test-monitoring-console","namespace":"test","creationTimestamp":null},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"monitoring-console","app.kubernetes.io/instance":"splunk-test-monitoring-console","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"monitoring-console","app.kubernetes.io/part-of":"splunk-test-monitoring-console"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"monitoring-console","app.kubernetes.io/instance":"splunk-test-monitoring-console","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"monitoring-console","app.kubernetes.io/part-of":"splunk-test-monitoring-console"},"annotations":{"monitoringConsoleConfigRev":"","traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"mnt-splunk-etc","emptyDir":{}},{"name":"mnt-splunk-var","emptyDir":{}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-test-secret","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"hec","containerPort":8088,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"s2s","containerPort":9997,"protocol":"TCP"}],"envFrom":[{"configMapRef":{"name":"splunk-test-monitoring-console"}}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_monitor"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack1-cluster-master-service"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"mnt-splunk-etc","mountPath":"/opt/splunk/etc"},{"name":"mnt-splunk-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-test-monitoring-console"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"serviceName":"splunk-test-monitoring-console-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)
}
