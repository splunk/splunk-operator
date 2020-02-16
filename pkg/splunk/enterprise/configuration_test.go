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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
)

// getSplunkEnterprise returns a complete SplunkEnterprise object
func getSplunkEnterprise() *enterprisev1.SplunkEnterprise {
	return &enterprisev1.SplunkEnterprise{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterprisev1.SplunkEnterpriseSpec{
			SplunkImage:      "test/splunk",
			SparkImage:       "test/spark",
			ImagePullPolicy:  "Always",
			StorageClassName: "gp2",
			SchedulerName:    "default",
			Defaults:         "defaults_string",
			DefaultsURL:      "http://defaults.example.com",
			LicenseURL:       "/mnt/splunk.lic",
			Topology: enterprisev1.SplunkTopologySpec{
				Indexers:     3,
				SearchHeads:  3,
				SparkWorkers: 3,
			},
			Resources: enterprisev1.SplunkResourcesSpec{
				SplunkCPURequest:     "0.1",
				SplunkCPULimit:       "1.0",
				SplunkMemoryRequest:  "4Gi",
				SplunkMemoryLimit:    "32Gi",
				SparkCPURequest:      "0.2",
				SparkCPULimit:        "2.0",
				SparkMemoryRequest:   "2Gi",
				SparkMemoryLimit:     "64Gi",
				SplunkEtcStorage:     "4Gi",
				SplunkVarStorage:     "20Gi",
				SplunkIndexerStorage: "100Gi",
			},
		},
	}
}

func testCommonSpec(t *testing.T, method string, dst *enterprisev1.CommonSpec, src *enterprisev1.SplunkEnterpriseSpec, isSpark bool) {
	if dst.Image != src.SplunkImage {
		t.Errorf("%s() Spec.Image = %s; want %s", method, dst.Image, src.SplunkImage)
	}
	if dst.ImagePullPolicy != src.ImagePullPolicy {
		t.Errorf("%s() Spec.ImagePullPolicy = %s; want %s", method, dst.ImagePullPolicy, src.ImagePullPolicy)
	}
	if dst.SchedulerName != src.SchedulerName {
		t.Errorf("%s() Spec.SchedulerName = %s; want %s", method, dst.SchedulerName, src.SchedulerName)
	}
	if !reflect.DeepEqual(dst.Affinity, src.Affinity) {
		t.Errorf("%s() Spec.Affinity = %v; want %v", method, dst.Affinity, src.Affinity)
	}

	var want corev1.ResourceList
	if isSpark {
		want = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(src.Resources.SparkCPURequest),
			corev1.ResourceMemory: resource.MustParse(src.Resources.SparkMemoryRequest),
		}
	} else {
		want = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(src.Resources.SplunkCPURequest),
			corev1.ResourceMemory: resource.MustParse(src.Resources.SplunkMemoryRequest),
		}
	}
	if !reflect.DeepEqual(dst.Resources.Requests, want) {
		t.Errorf("%s(isSpark=%t) Spec.Resources.Requests = %v; want %v", method, isSpark, dst.Resources.Requests, want)
	}

	if isSpark {
		want = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(src.Resources.SparkCPULimit),
			corev1.ResourceMemory: resource.MustParse(src.Resources.SparkMemoryLimit),
		}
	} else {
		want = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(src.Resources.SplunkCPULimit),
			corev1.ResourceMemory: resource.MustParse(src.Resources.SplunkMemoryLimit),
		}
	}
	if !reflect.DeepEqual(dst.Resources.Limits, want) {
		t.Errorf("%s(isSpark=%t) Spec.Resources.Requests = %v; want %v", method, isSpark, dst.Resources.Limits, want)
	}
}

func testCommonSplunkSpec(t *testing.T, method string, dst *enterprisev1.CommonSplunkSpec, src *enterprisev1.SplunkEnterprise, isIndexer bool) {
	testCommonSpec(t, method, &dst.CommonSpec, &src.Spec, false)
	if dst.StorageClassName != src.Spec.StorageClassName {
		t.Errorf("%s() Spec.StorageClassName = %s; want %s", method, dst.StorageClassName, src.Spec.StorageClassName)
	}
	if dst.EtcStorage != src.Spec.Resources.SplunkEtcStorage {
		t.Errorf("%s() Spec.EtcStorage = %s; want %s", method, dst.EtcStorage, src.Spec.Resources.SplunkEtcStorage)
	}
	if isIndexer {
		if dst.VarStorage != src.Spec.Resources.SplunkIndexerStorage {
			t.Errorf("%s() Spec.VarStorage = %s; want %s", method, dst.VarStorage, src.Spec.Resources.SplunkIndexerStorage)
		}
	} else {
		if dst.VarStorage != src.Spec.Resources.SplunkVarStorage {
			t.Errorf("%s() Spec.VarStorage = %s; want %s", method, dst.VarStorage, src.Spec.Resources.SplunkVarStorage)
		}
	}
	if len(dst.Volumes) != len(src.Spec.SplunkVolumes) {
		t.Errorf("%s() len(Spec.Volumes) = %d; want %d", method, len(dst.Volumes), len(src.Spec.SplunkVolumes))
	}
	for n := range dst.Volumes {
		if dst.Volumes[n] != src.Spec.SplunkVolumes[n] {
			t.Errorf("%s() Spec.Volumes[%d] = %v; want %v", method, n, dst.Volumes[n], src.Spec.SplunkVolumes[n])
		}
	}
	if dst.Defaults != src.Spec.Defaults {
		t.Errorf("%s() Spec.Defaults = %s; want %s", method, dst.Defaults, src.Spec.Defaults)
	}
	if dst.DefaultsURL != src.Spec.DefaultsURL {
		t.Errorf("%s() Spec.DefaultsURL = %s; want %s", method, dst.DefaultsURL, src.Spec.DefaultsURL)
	}
	if dst.LicenseURL != src.Spec.LicenseURL {
		t.Errorf("%s() Spec.LicenseURL = %s; want %s", method, dst.LicenseURL, src.Spec.LicenseURL)
	}
	if dst.LicenseURL != "" {
		if dst.LicenseMasterRef.Name != src.GetIdentifier() {
			t.Errorf("%s() Spec.LicenseMasterRef.Name = %s; want %s", method, dst.LicenseMasterRef.Name, src.GetIdentifier())
		}
		if dst.LicenseMasterRef.Namespace != src.GetNamespace() {
			t.Errorf("%s() Spec.LicenseMasterRef.Namespace = %s; want %s", method, dst.LicenseMasterRef.Namespace, src.GetNamespace())
		}
	}
}

func testOwnerReferences(t *testing.T, method string, cr enterprisev1.MetaObject, owner enterprisev1.MetaObject) {
	owners := cr.GetObjectMeta().GetOwnerReferences()
	if len(owners) != 1 {
		t.Fatalf("%s() OwnerReferences len = %d; want %d", method, len(owners), 1)
	}
	if owners[0].Name != owner.GetIdentifier() {
		t.Errorf("%s() OwnerReferences[0].Name = %s; want %s", method, owners[0].Name, owner.GetIdentifier())
	}
}

func TestGetLicenseMasterResource(t *testing.T) {
	src := getSplunkEnterprise()
	got, err := GetLicenseMasterResource(src)
	if err != nil {
		t.Errorf("GetLicenseMasterResource() returned %v; want nil", err)
	}
	testCommonSplunkSpec(t, "TestGetLicenseMasterResource", &got.Spec.CommonSplunkSpec, src, false)
	testOwnerReferences(t, "TestGetLicenseMasterResource", got, src)
}

func TestGetStandaloneResource(t *testing.T) {
	src := getSplunkEnterprise()
	src.Spec.EnableDFS = true
	got, err := GetStandaloneResource(src)
	if err != nil {
		t.Errorf("GetStandaloneResource() returned %v; want nil", err)
	}
	testCommonSplunkSpec(t, "TestGetStandaloneResource", &got.Spec.CommonSplunkSpec, src, false)
	if got.Spec.Replicas != src.Spec.Topology.Standalones {
		t.Errorf("GetStandaloneResource() Spec.Replicas = %d; want %d", got.Spec.Replicas, src.Spec.Topology.Standalones)
	}
	if got.Spec.SparkImage != src.Spec.SparkImage {
		t.Errorf("GetStandaloneResource() Spec.SparkImage = %s; want %s", got.Spec.SparkImage, src.Spec.SparkImage)
	}
	if got.Spec.SparkRef.Name != src.GetIdentifier() {
		t.Errorf("GetStandaloneResource() got.Spec.SparkRef.Name = %s; want %s", got.Spec.SparkRef.Name, src.GetIdentifier())
	}
	if got.Spec.SparkRef.Namespace != src.GetNamespace() {
		t.Errorf("GetStandaloneResource() got.Spec.SparkRef.Namespace = %s; want %s", got.Spec.SparkRef.Namespace, src.GetNamespace())
	}
	testOwnerReferences(t, "TestGetStandaloneResource", got, src)
}

func TestGetSearchHeadResource(t *testing.T) {
	src := getSplunkEnterprise()
	src.Spec.EnableDFS = true
	got, err := GetSearchHeadResource(src)
	if err != nil {
		t.Errorf("GetSearchHeadResource() returned %v; want nil", err)
	}
	testCommonSplunkSpec(t, "TestGetSearchHeadResource", &got.Spec.CommonSplunkSpec, src, false)
	if got.Spec.Replicas != src.Spec.Topology.SearchHeads {
		t.Errorf("GetSearchHeadResource() Spec.Replicas = %d; want %d", got.Spec.Replicas, src.Spec.Topology.SearchHeads)
	}
	if got.Spec.SparkImage != src.Spec.SparkImage {
		t.Errorf("GetSearchHeadResource() Spec.SparkImage = %s; want %s", got.Spec.SparkImage, src.Spec.SparkImage)
	}
	if got.Spec.SparkRef.Name != src.GetIdentifier() {
		t.Errorf("GetStandaloneResource() got.Spec.SparkRef.Name = %s; want %s", got.Spec.SparkRef.Name, src.GetIdentifier())
	}
	if got.Spec.SparkRef.Namespace != src.GetNamespace() {
		t.Errorf("GetStandaloneResource() got.Spec.SparkRef.Namespace = %s; want %s", got.Spec.SparkRef.Namespace, src.GetNamespace())
	}
	testOwnerReferences(t, "TestGetSearchHeadResource", got, src)
}

func TestGetIndexerResource(t *testing.T) {
	src := getSplunkEnterprise()
	got, err := GetIndexerResource(src)
	if err != nil {
		t.Errorf("GetIndexerResource() returned %v; want nil", err)
	}
	testCommonSplunkSpec(t, "TestGetIndexerResource", &got.Spec.CommonSplunkSpec, src, true)
	if got.Spec.Replicas != src.Spec.Topology.Indexers {
		t.Errorf("GetIndexerResource() Spec.Replicas = %d; want %d", got.Spec.Replicas, src.Spec.Topology.Indexers)
	}
	testOwnerReferences(t, "TestGetIndexerResource", got, src)
}

func TestGetSparkResource(t *testing.T) {
	src := getSplunkEnterprise()
	got, err := GetSparkResource(src)
	if err != nil {
		t.Errorf("GetSparkResource() returned %v; want nil", err)
	}
	testCommonSpec(t, "TestGetSparkResource", &got.Spec.CommonSpec, &src.Spec, true)
	if got.Spec.Replicas != src.Spec.Topology.SearchHeads {
		t.Errorf("GetSparkResource() Spec.Replicas = %d; want %d", got.Spec.Replicas, src.Spec.Topology.SearchHeads)
	}
	testOwnerReferences(t, "TestGetSparkResource", got, src)
}

func configTester(t *testing.T, method string, f func() (interface{}, error), want string) {
	result, err := f()
	if err != nil {
		t.Errorf("%s returned error: %v", method, err)
	}
	got, err := json.Marshal(result)
	if err != nil {
		t.Errorf("%s failed to marshall: %v", method, err)
	}
	if string(got) != want {
		t.Errorf("%s = %s; want %s", method, got, want)
	}
}

func TestGetIndexerStatefulSet(t *testing.T) {
	cr := enterprisev1.Indexer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := ValidateIndexerSpec(&cr.Spec); err != nil {
				t.Errorf("ValidateIndexerSpec() returned error: %v", err)
			}
			return GetIndexerStatefulSet(&cr)
		}
		configTester(t, "GetIndexerStatefulSet()", f, want)
	}

	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-indexer","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-indexer-secrets"}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"hec","containerPort":8088,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"s2s","containerPort":9997,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_indexer"},{"name":"SPLUNK_INDEXER_URL","value":"splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack1-cluster-master-service"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-indexer"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"1Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"200Gi"}}},"status":{}}],"serviceName":"splunk-stack1-indexer-headless","podManagementPolicy":"Parallel","updateStrategy":{}},"status":{"replicas":0}}`)
}

func TestGetSearchHeadStatefulSet(t *testing.T) {
	cr := enterprisev1.SearchHead{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := ValidateSearchHeadSpec(&cr.Spec); err != nil {
				t.Errorf("ValidateSearchHeadSpec() returned error: %v", err)
			}
			return GetSearchHeadStatefulSet(&cr)
		}
		configTester(t, fmt.Sprintf("GetSearchHeadStatefulSet(Replicas=%d)", cr.Spec.Replicas), f, want)
	}

	cr.Spec.Replicas = 3
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":3,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secrets"}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datarecieve","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"1Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"200Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{}},"status":{"replicas":0}}`)

	cr.Spec.Replicas = 4
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":4,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secrets"}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datarecieve","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-3.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"1Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"200Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{}},"status":{"replicas":0}}`)

	cr.Spec.Replicas = 5
	cr.Spec.IndexerRef.Name = "stack1"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":5,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secrets"}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datarecieve","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-3.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-4.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack1-cluster-master-service"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"1Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"200Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{}},"status":{"replicas":0}}`)

	cr.Spec.Replicas = 6
	cr.Spec.SparkRef.Name = cr.GetIdentifier()
	cr.Spec.IndexerRef.Namespace = "test2"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":6,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secrets"}},{"name":"mnt-splunk-jdk","emptyDir":{}},{"name":"mnt-splunk-spark","emptyDir":{}}],"initContainers":[{"name":"init","image":"splunk/spark","command":["bash","-c","cp -r /opt/jdk /mnt \u0026\u0026 cp -r /opt/spark /mnt"],"resources":{"limits":{"cpu":"1","memory":"512Mi"},"requests":{"cpu":"250m","memory":"128Mi"}},"volumeMounts":[{"name":"mnt-splunk-jdk","mountPath":"/mnt/jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/spark"}],"imagePullPolicy":"IfNotPresent"}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datarecieve","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-3.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-4.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-5.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack1-cluster-master-service.test2.svc.cluster.local"},{"name":"SPLUNK_ENABLE_DFS","value":"true"},{"name":"SPARK_MASTER_HOST","value":"splunk-stack1-spark-master-service"},{"name":"SPARK_MASTER_WEBUI_PORT","value":"8009"},{"name":"SPARK_HOME","value":"/mnt/splunk-spark"},{"name":"JAVA_HOME","value":"/mnt/splunk-jdk"},{"name":"SPLUNK_DFW_NUM_SLOTS_ENABLED","value":"true"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"},{"name":"mnt-splunk-jdk","mountPath":"/mnt/splunk-jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/splunk-spark"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"1Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"200Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{}},"status":{"replicas":0}}`)
}

func TestGetStandaloneStatefulSet(t *testing.T) {
	cr := enterprisev1.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := ValidateStandaloneSpec(&cr.Spec); err != nil {
				t.Errorf("ValidateStandaloneSpec() returned error: %v", err)
			}
			return GetStandaloneStatefulSet(&cr)
		}
		configTester(t, "GetStandaloneStatefulSet()", f, want)
	}

	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-standalone","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-standalone-secrets"}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"hec","containerPort":8088,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"s2s","containerPort":9997,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datarecieve","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_standalone"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-standalone"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"1Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"200Gi"}}},"status":{}}],"serviceName":"splunk-stack1-standalone-headless","podManagementPolicy":"Parallel","updateStrategy":{}},"status":{"replicas":0}}`)

	cr.Spec.SparkRef.Name = cr.GetIdentifier()
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-standalone","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-standalone-secrets"}},{"name":"mnt-splunk-jdk","emptyDir":{}},{"name":"mnt-splunk-spark","emptyDir":{}}],"initContainers":[{"name":"init","image":"splunk/spark","command":["bash","-c","cp -r /opt/jdk /mnt \u0026\u0026 cp -r /opt/spark /mnt"],"resources":{"limits":{"cpu":"1","memory":"512Mi"},"requests":{"cpu":"250m","memory":"128Mi"}},"volumeMounts":[{"name":"mnt-splunk-jdk","mountPath":"/mnt/jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/spark"}],"imagePullPolicy":"IfNotPresent"}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"hec","containerPort":8088,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"s2s","containerPort":9997,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datarecieve","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_standalone"},{"name":"SPLUNK_ENABLE_DFS","value":"true"},{"name":"SPARK_MASTER_HOST","value":"splunk-stack1-spark-master-service"},{"name":"SPARK_MASTER_WEBUI_PORT","value":"8009"},{"name":"SPARK_HOME","value":"/mnt/splunk-spark"},{"name":"JAVA_HOME","value":"/mnt/splunk-jdk"},{"name":"SPLUNK_DFW_NUM_SLOTS_ENABLED","value":"false"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"},{"name":"mnt-splunk-jdk","mountPath":"/mnt/splunk-jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/splunk-spark"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-standalone"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"1Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"200Gi"}}},"status":{}}],"serviceName":"splunk-stack1-standalone-headless","podManagementPolicy":"Parallel","updateStrategy":{}},"status":{"replicas":0}}`)

	cr.Spec.IndexerRef.Name = "stack2"
	cr.Spec.StorageClassName = "gp2"
	cr.Spec.SchedulerName = "custom-scheduler"
	cr.Spec.Defaults = "defaults-string"
	cr.Spec.DefaultsURL = "/mnt/defaults/defaults.yml"
	cr.Spec.Volumes = []corev1.Volume{
		{Name: "defaults"},
	}
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-standalone","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"defaults"},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-standalone-secrets"}},{"name":"mnt-splunk-defaults","configMap":{"name":"splunk-stack1-standalone-defaults"}},{"name":"mnt-splunk-jdk","emptyDir":{}},{"name":"mnt-splunk-spark","emptyDir":{}}],"initContainers":[{"name":"init","image":"splunk/spark","command":["bash","-c","cp -r /opt/jdk /mnt \u0026\u0026 cp -r /opt/spark /mnt"],"resources":{"limits":{"cpu":"1","memory":"512Mi"},"requests":{"cpu":"250m","memory":"128Mi"}},"volumeMounts":[{"name":"mnt-splunk-jdk","mountPath":"/mnt/jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/spark"}],"imagePullPolicy":"IfNotPresent"}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"hec","containerPort":8088,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"s2s","containerPort":9997,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datarecieve","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml,/mnt/defaults/defaults.yml,/mnt/splunk-defaults/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack2-cluster-master-service"},{"name":"SPLUNK_ENABLE_DFS","value":"true"},{"name":"SPARK_MASTER_HOST","value":"splunk-stack1-spark-master-service"},{"name":"SPARK_MASTER_WEBUI_PORT","value":"8009"},{"name":"SPARK_HOME","value":"/mnt/splunk-spark"},{"name":"JAVA_HOME","value":"/mnt/splunk-jdk"},{"name":"SPLUNK_DFW_NUM_SLOTS_ENABLED","value":"false"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"defaults","mountPath":"/mnt/defaults"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"},{"name":"mnt-splunk-defaults","mountPath":"/mnt/splunk-defaults"},{"name":"mnt-splunk-jdk","mountPath":"/mnt/splunk-jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/splunk-spark"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-standalone"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"custom-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"1Gi"}},"storageClassName":"gp2"},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"200Gi"}},"storageClassName":"gp2"},"status":{}}],"serviceName":"splunk-stack1-standalone-headless","podManagementPolicy":"Parallel","updateStrategy":{}},"status":{"replicas":0}}`)
}

func TestGetLicenseMasterStatefulSet(t *testing.T) {
	cr := enterprisev1.LicenseMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := ValidateLicenseMasterSpec(&cr.Spec); err != nil {
				t.Errorf("ValidateLicenseMasterSpec() returned error: %v", err)
			}
			return GetLicenseMasterStatefulSet(&cr)
		}
		configTester(t, "GetLicenseMasterStatefulSet()", f, want)
	}

	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-license-master","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"license-master","app.kubernetes.io/instance":"splunk-stack1-license-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-master","app.kubernetes.io/part-of":"splunk-stack1-license-master"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-master","app.kubernetes.io/instance":"splunk-stack1-license-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-master","app.kubernetes.io/part-of":"splunk-stack1-license-master"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-license-master-secrets"}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_license_master"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-license-master"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-master","app.kubernetes.io/instance":"splunk-stack1-license-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-master","app.kubernetes.io/part-of":"splunk-stack1-license-master"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"1Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-master","app.kubernetes.io/instance":"splunk-stack1-license-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-master","app.kubernetes.io/part-of":"splunk-stack1-license-master"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"200Gi"}}},"status":{}}],"serviceName":"splunk-stack1-license-master-headless","podManagementPolicy":"Parallel","updateStrategy":{}},"status":{"replicas":0}}`)

	cr.Spec.LicenseURL = "/mnt/splunk.lic"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-license-master","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"license-master","app.kubernetes.io/instance":"splunk-stack1-license-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-master","app.kubernetes.io/part-of":"splunk-stack1-license-master"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-master","app.kubernetes.io/instance":"splunk-stack1-license-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-master","app.kubernetes.io/part-of":"splunk-stack1-license-master"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-license-master-secrets"}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_license_master"},{"name":"SPLUNK_LICENSE_URI","value":"/mnt/splunk.lic"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-license-master"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-master","app.kubernetes.io/instance":"splunk-stack1-license-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-master","app.kubernetes.io/part-of":"splunk-stack1-license-master"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"1Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-master","app.kubernetes.io/instance":"splunk-stack1-license-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-master","app.kubernetes.io/part-of":"splunk-stack1-license-master"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"200Gi"}}},"status":{}}],"serviceName":"splunk-stack1-license-master-headless","podManagementPolicy":"Parallel","updateStrategy":{}},"status":{"replicas":0}}`)
}

func TestGetClusterMasterStatefulSet(t *testing.T) {
	cr := enterprisev1.Indexer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := ValidateIndexerSpec(&cr.Spec); err != nil {
				t.Errorf("ValidateSearchHeadSpec() returned error: %v", err)
			}
			return GetClusterMasterStatefulSet(&cr)
		}
		configTester(t, fmt.Sprintf("GetClusterMasterStatefulSet(replicas=%d)", cr.Spec.Replicas), f, want)
	}

	cr.Spec.Replicas = 1
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-master","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-indexer-secrets"}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_INDEXER_URL","value":"splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-master"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"1Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"200Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-master-headless","podManagementPolicy":"Parallel","updateStrategy":{}},"status":{"replicas":0}}`)

	cr.Spec.Replicas = 2
	cr.Spec.LicenseMasterRef.Name = "stack1"
	cr.Spec.LicenseMasterRef.Namespace = "test"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-master","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-indexer-secrets"}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_LICENSE_MASTER_URL","value":"splunk-stack1-license-master-service.test.svc.cluster.local"},{"name":"SPLUNK_INDEXER_URL","value":"splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local,splunk-stack1-indexer-1.splunk-stack1-indexer-headless.test.svc.cluster.local"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-master"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"1Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"200Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-master-headless","podManagementPolicy":"Parallel","updateStrategy":{}},"status":{"replicas":0}}`)

	cr.Spec.Replicas = 3
	cr.Spec.LicenseMasterRef.Name = ""
	cr.Spec.LicenseURL = "/mnt/splunk.lic"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-master","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-indexer-secrets"}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_LICENSE_URI","value":"/mnt/splunk.lic"},{"name":"SPLUNK_INDEXER_URL","value":"splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local,splunk-stack1-indexer-1.splunk-stack1-indexer-headless.test.svc.cluster.local,splunk-stack1-indexer-2.splunk-stack1-indexer-headless.test.svc.cluster.local"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-master"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"1Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"200Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-master-headless","podManagementPolicy":"Parallel","updateStrategy":{}},"status":{"replicas":0}}`)
}

func TestGetDeployerStatefulSet(t *testing.T) {
	cr := enterprisev1.SearchHead{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := ValidateSearchHeadSpec(&cr.Spec); err != nil {
				t.Errorf("ValidateSearchHeadSpec() returned error: %v", err)
			}
			return GetDeployerStatefulSet(&cr)
		}
		configTester(t, "GetDeployerStatefulSet()", f, want)
	}

	cr.Spec.Replicas = 3
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-deployer","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secrets"}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_deployer"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-deployer"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"1Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"200Gi"}}},"status":{}}],"serviceName":"splunk-stack1-deployer-headless","podManagementPolicy":"Parallel","updateStrategy":{}},"status":{"replicas":0}}`)
}

func TestGetSplunkService(t *testing.T) {
	cr := enterprisev1.Indexer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	test := func(instanceType InstanceType, isHeadless bool, want string) {
		f := func() (interface{}, error) {
			return GetSplunkService(&cr, cr.Spec.CommonSpec, instanceType, isHeadless), nil
		}
		configTester(t, fmt.Sprintf("GetSplunkService(\"%s\",%t)", instanceType, isHeadless), f, want)
	}

	test(SplunkIndexer, false, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-indexer-service","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"splunkweb","protocol":"TCP","port":8000,"targetPort":0},{"name":"hec","protocol":"TCP","port":8088,"targetPort":0},{"name":"splunkd","protocol":"TCP","port":8089,"targetPort":0},{"name":"s2s","protocol":"TCP","port":9997,"targetPort":0}],"selector":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"status":{"loadBalancer":{}}}`)
	test(SplunkIndexer, true, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-indexer-headless","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"splunkweb","protocol":"TCP","port":8000,"targetPort":0},{"name":"hec","protocol":"TCP","port":8088,"targetPort":0},{"name":"splunkd","protocol":"TCP","port":8089,"targetPort":0},{"name":"s2s","protocol":"TCP","port":9997,"targetPort":0}],"selector":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"clusterIP":"None"},"status":{"loadBalancer":{}}}`)

	cr.Spec.ServiceTemplate.Spec.Type = "LoadBalancer"
	cr.Spec.ServiceTemplate.ObjectMeta.Labels = map[string]string{"1": "2"}
	cr.ObjectMeta.Labels = map[string]string{"one": "two"}
	cr.ObjectMeta.Annotations = map[string]string{"a": "b"}

	test(SplunkSearchHead, false, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-search-head-service","namespace":"test","creationTimestamp":null,"labels":{"1":"2","app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head","one":"two"},"annotations":{"a":"b"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"splunkweb","protocol":"TCP","port":8000,"targetPort":0},{"name":"splunkd","protocol":"TCP","port":8089,"targetPort":0},{"name":"dfsmaster","protocol":"TCP","port":9000,"targetPort":0},{"name":"dfccontrol","protocol":"TCP","port":17000,"targetPort":0},{"name":"datarecieve","protocol":"TCP","port":19000,"targetPort":0}],"selector":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"type":"LoadBalancer"},"status":{"loadBalancer":{}}}`)
	test(SplunkSearchHead, true, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-search-head-headless","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head","one":"two"},"annotations":{"a":"b"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"splunkweb","protocol":"TCP","port":8000,"targetPort":0},{"name":"splunkd","protocol":"TCP","port":8089,"targetPort":0},{"name":"dfsmaster","protocol":"TCP","port":9000,"targetPort":0},{"name":"dfccontrol","protocol":"TCP","port":17000,"targetPort":0},{"name":"datarecieve","protocol":"TCP","port":19000,"targetPort":0}],"selector":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"clusterIP":"None","publishNotReadyAddresses":true},"status":{"loadBalancer":{}}}`)
}

func TestValidateSplunkEnterpriseSpec(t *testing.T) {
	cr := enterprisev1.SplunkEnterprise{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	test := func(want error) {
		got := ValidateSplunkEnterpriseSpec(&cr.Spec)
		if got == nil {
			if want != nil {
				t.Errorf("ValidateSplunkEnterpriseSpec(%v) = nil; want %v", cr, want)
			}
		} else {
			if want == nil {
				t.Errorf("ValidateSplunkEnterpriseSpec(%v) = %v; want nil", cr, got)
			}
		}
	}

	test(nil)

	cr.Spec = enterprisev1.SplunkEnterpriseSpec{
		Topology: enterprisev1.SplunkTopologySpec{
			Indexers:    0,
			SearchHeads: 3,
		},
	}
	test(errors.New("You must specify how many indexers the cluster should have"))

	cr.Spec = enterprisev1.SplunkEnterpriseSpec{
		Topology: enterprisev1.SplunkTopologySpec{
			Indexers:    3,
			SearchHeads: 0,
		},
	}
	test(errors.New("ou must specify how many search heads the cluster should have"))

	cr.Spec = enterprisev1.SplunkEnterpriseSpec{
		Topology: enterprisev1.SplunkTopologySpec{
			Indexers:    3,
			SearchHeads: 3,
		},
	}
	test(nil)
}

func TestGetSplunkDefaults(t *testing.T) {
	cr := enterprisev1.Indexer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterprisev1.IndexerSpec{
			CommonSplunkSpec: enterprisev1.CommonSplunkSpec{Defaults: "defaults_string"},
		},
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			return GetSplunkDefaults(cr.GetIdentifier(), cr.GetNamespace(), SplunkIndexer, cr.Spec.Defaults), nil
		}
		configTester(t, "GetSplunkDefaults()", f, want)
	}

	test(`{"metadata":{"name":"splunk-stack1-indexer-defaults","namespace":"test","creationTimestamp":null},"data":{"default.yml":"defaults_string"}}`)
}

func TestGetSplunkSecrets(t *testing.T) {
	cr := enterprisev1.Indexer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	got := GetSplunkSecrets(&cr, SplunkIndexer, nil, nil)

	if len(got.Data["hec_token"]) != 36 {
		t.Errorf("GetSplunkSecrets() hec_token len = %d; want %d", len(got.Data["hec_token"]), 36)
	}

	if len(got.Data["password"]) != 24 {
		t.Errorf("GetSplunkSecrets() password len = %d; want %d", len(got.Data["password"]), 24)
	}

	if len(got.Data["pass4SymmKey"]) != 24 {
		t.Errorf("GetSplunkSecrets() pass4SymmKey len = %d; want %d", len(got.Data["pass4SymmKey"]), 24)
	}

	if len(got.Data["idxc_secret"]) != 24 {
		t.Errorf("GetSplunkSecrets() idxc_secret len = %d; want %d", len(got.Data["idxc_secret"]), 24)
	}

	if len(got.Data["shc_secret"]) != 24 {
		t.Errorf("GetSplunkSecrets() shc_secret len = %d; want %d", len(got.Data["shc_secret"]), 24)
	}

	if len(got.Data["default.yml"]) == 0 {
		t.Errorf("GetSplunkSecrets() has empty default.yml")
	}

	idxcSecret := []byte{'a', 'b'}
	pass4SymmKey := []byte{'a', 'b'}
	got = GetSplunkSecrets(&cr, SplunkIndexer, idxcSecret, pass4SymmKey)

	if !bytes.Equal(got.Data["idxc_secret"], idxcSecret) {
		t.Errorf("GetSplunkSecrets() idxc_secret = %v; want %v", got.Data["idxc_secret"], idxcSecret)
	}

	if !bytes.Equal(got.Data["pass4SymmKey"], pass4SymmKey) {
		t.Errorf("GetSplunkSecrets() pass4SymmKey = %v; want %v", got.Data["pass4SymmKey"], idxcSecret)
	}
}
