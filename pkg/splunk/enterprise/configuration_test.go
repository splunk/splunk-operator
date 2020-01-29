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
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAppendSplunkDfsOverrides(t *testing.T) {
	var overrides map[string]string
	identifier := "test"
	searchHeads := 1

	test := func(want map[string]string) {
		got := AppendSplunkDfsOverrides(overrides, identifier, searchHeads)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("AppendSplunkDfsOverrides(%v,\"%s\",%d) = %v; want %v",
				overrides, identifier, searchHeads, got, want)
		}
	}

	test(map[string]string{
		"SPLUNK_ENABLE_DFS":            "true",
		"SPARK_MASTER_HOST":            "splunk-test-spark-master-service",
		"SPARK_MASTER_WEBUI_PORT":      "8009",
		"SPARK_HOME":                   "/mnt/splunk-spark",
		"JAVA_HOME":                    "/mnt/splunk-jdk",
		"SPLUNK_DFW_NUM_SLOTS_ENABLED": "false",
	})

	searchHeads = 3
	overrides = map[string]string{"a": "b"}
	test(map[string]string{
		"a":                            "b",
		"SPLUNK_ENABLE_DFS":            "true",
		"SPARK_MASTER_HOST":            "splunk-test-spark-master-service",
		"SPARK_MASTER_WEBUI_PORT":      "8009",
		"SPARK_HOME":                   "/mnt/splunk-spark",
		"JAVA_HOME":                    "/mnt/splunk-jdk",
		"SPLUNK_DFW_NUM_SLOTS_ENABLED": "true",
	})
}

func TestGetSplunkConfiguration(t *testing.T) {
	overrides := map[string]string{"A": "a"}
	defaults := "defaults_string"
	defaultsURL := "http://defaults.example.com"

	got := GetSplunkConfiguration(overrides, defaults, defaultsURL)
	want := []corev1.EnvVar{
		{
			Name:  "SPLUNK_HOME",
			Value: "/opt/splunk",
		},
		{
			Name:  "SPLUNK_START_ARGS",
			Value: "--accept-license",
		},
		{
			Name:  "SPLUNK_DEFAULTS_URL",
			Value: "/mnt/splunk-secrets/default.yml,http://defaults.example.com,/mnt/splunk-defaults/default.yml",
		},
		{
			Name:  "SPLUNK_HOME_OWNERSHIP_ENFORCEMENT",
			Value: "false",
		},
		{
			Name:  "A",
			Value: "a",
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetSplunkConfiguration(%v,\"%s\",\"%s\") = %v; want %v",
			overrides, defaults, defaultsURL, got, want)
	}
}

func TestGetSplunkClusterConfiguration(t *testing.T) {
	cr := v1alpha1.SplunkEnterprise{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: v1alpha1.SplunkEnterpriseSpec{
			Topology: v1alpha1.SplunkTopologySpec{
				Indexers:    3,
				SearchHeads: 3,
			},
		},
	}
	searchHeadCluster := true
	overrides := map[string]string{"A": "a"}

	got := GetSplunkClusterConfiguration(&cr, searchHeadCluster, overrides)
	want := []corev1.EnvVar{
		{
			Name:  "SPLUNK_CLUSTER_MASTER_URL",
			Value: "splunk-stack1-cluster-master-service",
		}, {
			Name:  "SPLUNK_INDEXER_URL",
			Value: "splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local,splunk-stack1-indexer-1.splunk-stack1-indexer-headless.test.svc.cluster.local,splunk-stack1-indexer-2.splunk-stack1-indexer-headless.test.svc.cluster.local",
		}, {
			Name:  "SPLUNK_LICENSE_MASTER_URL",
			Value: "splunk-stack1-license-master-service",
		},
		{
			Name:  "SPLUNK_SEARCH_HEAD_URL",
			Value: "splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local",
		},
		{
			Name:  "SPLUNK_SEARCH_HEAD_CAPTAIN_URL",
			Value: "splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local",
		},
		{
			Name:  "SPLUNK_DEPLOYER_URL",
			Value: "splunk-stack1-deployer-service",
		},
		{
			Name:  "SPLUNK_HOME",
			Value: "/opt/splunk",
		},
		{
			Name:  "SPLUNK_START_ARGS",
			Value: "--accept-license",
		},
		{
			Name:  "SPLUNK_DEFAULTS_URL",
			Value: "/mnt/splunk-secrets/default.yml",
		},
		{
			Name:  "SPLUNK_HOME_OWNERSHIP_ENFORCEMENT",
			Value: "false",
		},
		{
			Name:  "A",
			Value: "a",
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetSplunkClusterConfiguration(,%t,%v) = %v; want %v",
			searchHeadCluster, overrides, got, want)
	}
}

func configTester(t *testing.T, method string, f func() (interface{}, error), want string) {
	result, err := f()
	if err != nil {
		t.Errorf("%s() returned error: %v", method, err)
	}
	got, err := json.Marshal(result)
	if err != nil {
		t.Errorf("%s() failed to marshall: %v", method, err)
	}
	if string(got) != want {
		t.Errorf("%s() = %s; want %s", method, got, want)
	}
}

func TestGetSplunkStatefulSet(t *testing.T) {
	cr := v1alpha1.SplunkEnterprise{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	instanceType := SplunkIndexer
	replicas := 3
	var envVariables []corev1.EnvVar

	test := func(want string) {
		f := func() (interface{}, error) {
			return GetSplunkStatefulSet(&cr, instanceType, replicas, envVariables)
		}
		configTester(t, "GetSplunkStatefulSet", f, want)
	}

	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-indexer","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":3,"selector":{"matchLabels":{"app":"splunk","for":"stack1","type":"indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app":"splunk","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"splunk-stack1","app.kubernetes.io/part-of":"splunk","for":"stack1","type":"indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-secrets"}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"s2s","containerPort":9997,"protocol":"TCP"},{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"hec","containerPort":8088,"protocol":"TCP"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5}}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-indexer"]}]},"topologyKey":"kubernetes.io/hostname"}}]}}}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app":"splunk","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"splunk-stack1","app.kubernetes.io/part-of":"splunk","for":"stack1","type":"indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"1Gi"}},"dataSource":null},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app":"splunk","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"splunk-stack1","app.kubernetes.io/part-of":"splunk","for":"stack1","type":"indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"200Gi"}},"dataSource":null},"status":{}}],"serviceName":"splunk-stack1-indexer-headless","podManagementPolicy":"Parallel","updateStrategy":{}},"status":{"replicas":0}}`)
}

func TestGetSplunkService(t *testing.T) {
	cr := v1alpha1.SplunkEnterprise{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	instanceType := SplunkIndexer
	isHeadless := false

	test := func(want string) {
		f := func() (interface{}, error) {
			return GetSplunkService(&cr, instanceType, isHeadless), nil
		}
		configTester(t, "GetSplunkService", f, want)
	}

	test(`{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-indexer-service","namespace":"test","creationTimestamp":null,"labels":{"app":"splunk","app.kubernetes.io/instance":"splunk-stack1-indexer-service","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"splunk-stack1","app.kubernetes.io/part-of":"splunk","for":"stack1","type":"indexer-service"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"splunkweb","protocol":"TCP","port":8000,"targetPort":0},{"name":"splunkd","protocol":"TCP","port":8089,"targetPort":0},{"name":"hec","protocol":"TCP","port":8088,"targetPort":0},{"name":"s2s","protocol":"TCP","port":9997,"targetPort":0}],"selector":{"app":"splunk","for":"stack1","type":"indexer"}},"status":{"loadBalancer":{}}}`)
}

func TestValidateSplunkCustomResource(t *testing.T) {
	cr := v1alpha1.SplunkEnterprise{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	test := func(want error) {
		got := ValidateSplunkCustomResource(&cr)
		if got == nil {
			if want != nil {
				t.Errorf("ValidateSplunkCustomResource(%v) = nil; want %v", cr, want)
			}
		} else {
			if want == nil {
				t.Errorf("ValidateSplunkCustomResource(%v) = %v; want nil", cr, got)
			}
		}
	}

	test(nil)

	cr.Spec = v1alpha1.SplunkEnterpriseSpec{
		Topology: v1alpha1.SplunkTopologySpec{
			Indexers:    0,
			SearchHeads: 3,
		},
	}
	test(errors.New("You must specify how many indexers the cluster should have"))

	cr.Spec = v1alpha1.SplunkEnterpriseSpec{
		Topology: v1alpha1.SplunkTopologySpec{
			Indexers:    3,
			SearchHeads: 0,
		},
	}
	test(errors.New("ou must specify how many search heads the cluster should have"))

	cr.Spec = v1alpha1.SplunkEnterpriseSpec{
		Topology: v1alpha1.SplunkTopologySpec{
			Indexers:    3,
			SearchHeads: 3,
		},
	}
	test(errors.New("You must provide a license to create a cluster"))
}

func TestGetSplunkDefaults(t *testing.T) {
	cr := v1alpha1.SplunkEnterprise{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: v1alpha1.SplunkEnterpriseSpec{
			Defaults: "defaults_string",
		},
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			return GetSplunkDefaults(&cr), nil
		}
		configTester(t, "GetSplunkDefaults", f, want)
	}

	test(`{"metadata":{"name":"splunk-stack1-defaults","namespace":"test","creationTimestamp":null},"data":{"default.yml":"defaults_string"}}`)
}

func TestGetSplunkSecrets(t *testing.T) {
	cr := v1alpha1.SplunkEnterprise{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	got := GetSplunkSecrets(&cr)

	if len(got.Data["hec_token"]) != 36 {
		t.Errorf("GetSplunkSecrets() hec_token len = %d; want %d", len(got.Data["hec_token"]), 36)
	}

	if len(got.Data["password"]) != 24 {
		t.Errorf("GetSplunkSecrets() password len = %d; want %d", len(got.Data["password"]), 24)
	}

	if len(got.Data["idc_secret"]) != 24 {
		t.Errorf("GetSplunkSecrets() password len = %d; want %d", len(got.Data["password"]), 24)
	}

	if len(got.Data["password"]) != 24 {
		t.Errorf("GetSplunkSecrets() password len = %d; want %d", len(got.Data["password"]), 24)
	}

	if len(got.Data["shc_secret"]) != 24 {
		t.Errorf("GetSplunkSecrets() password len = %d; want %d", len(got.Data["password"]), 24)
	}

	if len(got.Data["default.yml"]) == 0 {
		t.Errorf("GetSplunkSecrets() has empty default.yml")
	}
}
