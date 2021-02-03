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
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1beta1"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

func TestApplyClusterMaster(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-service"},
		{MetaName: "*v1.Service-test-splunk-stack1-cluster-master-service"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-cluster-master-secret-v1"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermaster-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermaster-smartstore"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-master"},
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
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0], funcCalls[2], funcCalls[3], funcCalls[5], funcCalls[8]}, "List": {listmockCall[0]}, "Update": {funcCalls[0]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[0], funcCalls[2], funcCalls[3], funcCalls[4], funcCalls[5], funcCalls[6], funcCalls[7], funcCalls[8]}, "Update": {funcCalls[8]}, "List": {listmockCall[0]}}

	current := enterprisev1.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterprisev1.ClusterMasterSpec{
			CommonSplunkSpec: enterprisev1.CommonSplunkSpec{
				Mock: true,
			},
		},
	}
	revised := current.DeepCopy()
	revised.Spec.Image = "splunk/test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyClusterMaster(c, cr.(*enterprisev1.ClusterMaster))
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyClusterMaster", &current, revised, createCalls, updateCalls, reconcile, true)

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
		_, err := ApplyClusterMaster(c, cr.(*enterprisev1.ClusterMaster))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)
}

func TestGetClusterMasterStatefulSet(t *testing.T) {
	cr := enterprisev1.ClusterMaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(c, "test")
	if err != nil {
		t.Errorf("Failed to create namespace scoped object")
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := validateClusterMasterSpec(&cr); err != nil {
				t.Errorf("validateClusterMasterSpec() returned error: %v", err)
			}
			return getClusterMasterStatefulSet(c, &cr)
		}
		configTester(t, fmt.Sprintf("getClusterMasterStatefulSet"), f, want)
	}

	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-master","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-cluster-master-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"localhost"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-master"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-master-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.LicenseMasterRef.Name = "stack1"
	cr.Spec.LicenseMasterRef.Namespace = "test"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-master","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-cluster-master-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_LICENSE_MASTER_URL","value":"https://splunk-stack1-license-master-service.test.svc.cluster.local:8089"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"localhost"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-master"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-master-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.LicenseMasterRef.Name = ""
	cr.Spec.LicenseURL = "/mnt/splunk.lic"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-master","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-cluster-master-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_LICENSE_URI","value":"/mnt/splunk.lic"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"localhost"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-master"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-master-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-master","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-cluster-master-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/apps/apps.yml,/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_LICENSE_URI","value":"/mnt/splunk.lic"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"localhost"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-master"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-master-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	// Create a serviceaccount
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(c, &current)
	cr.Spec.ServiceAccount = "defaults"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-master","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-cluster-master-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/apps/apps.yml,/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_LICENSE_URI","value":"/mnt/splunk.lic"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"localhost"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"serviceAccountName":"defaults","securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-master"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-master-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)
}

func TestApplyClusterMasterWithSmartstore(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermaster-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermaster-smartstore"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Service-test-splunk-stack1-indexer-service"},
		{MetaName: "*v1.Service-test-splunk-stack1-cluster-master-service"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-cluster-master-secret-v1"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermaster-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermaster-smartstore"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-master"},
		{MetaName: "*v1.Pod-test-splunk-stack1-cluster-master-0"},
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
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[6], funcCalls[7], funcCalls[9], funcCalls[15], funcCalls[16], funcCalls[17], funcCalls[18], funcCalls[20]}, "List": {listmockCall[0], listmockCall[0], listmockCall[0]}, "Update": {funcCalls[0], funcCalls[3], funcCalls[20]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[2], funcCalls[3], funcCalls[5], funcCalls[5], funcCalls[6], funcCalls[7], funcCalls[8], funcCalls[9], funcCalls[11], funcCalls[11], funcCalls[12]}, "Update": {funcCalls[10], funcCalls[12]}, "List": {listmockCall[0]}}

	current := enterprisev1.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterprisev1.ClusterMasterSpec{
			SmartStore: enterprisev1.SmartStoreSpec{
				VolList: []enterprisev1.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "splunk-test-secret"},
				},

				IndexList: []enterprisev1.IndexSpec{
					{Name: "salesdata1", RemotePath: "remotepath1",
						IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
							VolName: "msos_s2s3_vol"},
					},
					{Name: "salesdata2", RemotePath: "remotepath2",
						IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
							VolName: "msos_s2s3_vol"},
					},
					{Name: "salesdata3", RemotePath: "remotepath3",
						IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
							VolName: "msos_s2s3_vol"},
					},
				},
			},
			CommonSplunkSpec: enterprisev1.CommonSplunkSpec{
				Mock: true,
			},
		},
	}
	client := spltest.NewMockClient()

	// Without S3 keys, ApplyClusterMaster should fail
	_, err := ApplyClusterMaster(client, &current)
	if err == nil {
		t.Errorf("ApplyClusterMaster should fail without S3 secrets configured")
	}

	// Create namespace scoped secret
	secret, err := splutil.ApplyNamespaceScopedSecretObject(client, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	secret.Data[s3AccessKey] = []byte("abcdJDckRkxhMEdmSk5FekFRRzBFOXV6bGNldzJSWE9IenhVUy80aa")
	secret.Data[s3SecretKey] = []byte("g4NVp0a29PTzlPdGczWk1vekVUcVBSa0o4NkhBWWMvR1NadDV4YVEy")
	_, err = splctrl.ApplySecret(client, secret)
	if err != nil {
		t.Errorf(err.Error())
	}

	smartstoreConfigMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-clustermaster-smartstore",
			Namespace: "test",
		},
		Data: map[string]string{"a": "b"},
	}

	revised := current.DeepCopy()
	revised.Spec.Image = "splunk/test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyClusterMaster(c, cr.(*enterprisev1.ClusterMaster))
		return err
	}

	client.AddObject(&smartstoreConfigMap)
	ss, _ := getClusterMasterStatefulSet(client, &current)
	ss.Status.ReadyReplicas = 1

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-cluster-master-0",
			Namespace: "test",
			Labels: map[string]string{
				"controller-revision-hash": "v1",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Ready: true},
			},
		},
	}

	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyClusterMasterWithSmartstore-0", &current, revised, createCalls, updateCalls, reconcile, true, secret, &smartstoreConfigMap, ss, pod)

	current.Status.BundlePushTracker.NeedToPushMasterApps = true
	if _, err = ApplyClusterMaster(client, &current); err != nil {
		t.Errorf("ApplyClusterMaster() should not have returned error")
	}

	current.Spec.CommonSplunkSpec.EtcVolumeStorageConfig.StorageCapacity = "-abcd"
	if _, err := ApplyClusterMaster(client, &current); err == nil {
		t.Errorf("ApplyClusterMaster() should have returned error")
	}

	var replicas int32 = 3
	current.Spec.CommonSplunkSpec.EtcVolumeStorageConfig.StorageCapacity = ""
	ss.Status.ReadyReplicas = 3
	ss.Spec.Replicas = &replicas
	ss.Spec.Template.Spec.Containers[0].Image = "splunk/splunk"
	client.AddObject(ss)
	if result, err := ApplyClusterMaster(client, &current); err == nil && !result.Requeue {
		t.Errorf("ApplyClusterMaster() should have returned error or result.requeue should have been false")
	}

	ss.Status.ReadyReplicas = 1
	*ss.Spec.Replicas = ss.Status.ReadyReplicas
	objects := []runtime.Object{ss, pod}
	client.AddObjects(objects)
	current.Spec.CommonSplunkSpec.Mock = false

	// This should fail at ApplyMonitoringConsole
	if _, err := ApplyClusterMaster(client, &current); err == nil {
		t.Errorf("ApplyClusterMaster() should have returned error")
	}
}

func TestPerformCmBundlePush(t *testing.T) {

	current := enterprisev1.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterprisev1.ClusterMasterSpec{
			CommonSplunkSpec: enterprisev1.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	client := spltest.NewMockClient()

	// When the secret object is not present, should return an error
	current.Status.BundlePushTracker.NeedToPushMasterApps = true
	err := PerformCmBundlePush(client, &current)
	if err == nil {
		t.Errorf("Should return error, when the secret object is not present")
	}

	secret, err := splutil.ApplyNamespaceScopedSecretObject(client, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	_, err = splctrl.ApplySecret(client, secret)
	if err != nil {
		t.Errorf(err.Error())
	}

	smartstoreConfigMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-clustermaster-smartstore",
			Namespace: "test",
		},
		Data: map[string]string{configToken: ""},
	}

	_, err = splctrl.ApplyConfigMap(client, &smartstoreConfigMap)
	if err != nil {
		t.Errorf(err.Error())
	}

	current.Status.BundlePushTracker.NeedToPushMasterApps = true

	//Re-attempting to push the CM bundle in less than 5 seconds should return an error
	current.Status.BundlePushTracker.LastCheckInterval = time.Now().Unix() - 1
	err = PerformCmBundlePush(client, &current)
	if err == nil {
		t.Errorf("Bundle Push Should fail, if attempted to push within 5 seconds interval")
	}

	//Re-attempting to push the CM bundle after 5 seconds passed, should not return an error
	current.Status.BundlePushTracker.LastCheckInterval = time.Now().Unix() - 10
	err = PerformCmBundlePush(client, &current)
	if err != nil && strings.HasPrefix(err.Error(), "Will re-attempt to push the bundle after the 5 seconds") {
		t.Errorf("Bundle Push Should not fail if reattempted after 5 seconds interval passed. Error: %s", err.Error())
	}

	// When the CM Bundle push is not pending, should not return an error
	current.Status.BundlePushTracker.NeedToPushMasterApps = false
	err = PerformCmBundlePush(client, &current)
	if err != nil {
		t.Errorf("Should not return an error when the Bundle push is not required. Error: %s", err.Error())
	}
}

func TestPushMasterAppsBundle(t *testing.T) {

	current := enterprisev1.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterprisev1.ClusterMasterSpec{
			CommonSplunkSpec: enterprisev1.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	client := spltest.NewMockClient()

	//Without global secret object, should return an error
	err := PushMasterAppsBundle(client, &current)
	if err == nil {
		t.Errorf("Bundle push should fail, when the secret object is not found")
	}

	secret, err := splutil.ApplyNamespaceScopedSecretObject(client, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	_, err = splctrl.ApplySecret(client, secret)
	if err != nil {
		t.Errorf(err.Error())
	}

	err = PushMasterAppsBundle(client, &current)
	if err == nil {
		t.Errorf("Bundle push should fail, when the password is not found")
	}

	//Without password, should return an error
	delete(secret.Data, "password")
	err = PushMasterAppsBundle(client, &current)
	if err == nil {
		t.Errorf("Bundle push should fail, when the password is not found")
	}
}
