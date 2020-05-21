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
	"fmt"
	"testing"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

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
		t.Errorf("%s = %s;\nwant %s", method, got, want)
	}
}

func TestGetIndexerStatefulSet(t *testing.T) {
	cr := enterprisev1.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := ValidateIndexerClusterSpec(&cr.Spec); err != nil {
				t.Errorf("ValidateIndexerClusterSpec() returned error: %v", err)
			}
			return GetIndexerStatefulSet(&cr)
		}
		configTester(t, "GetIndexerStatefulSet()", f, want)
	}

	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-indexer","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-indexer-secrets","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"hec","containerPort":8088,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"s2s","containerPort":9997,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_indexer"},{"name":"SPLUNK_INDEXER_URL","value":"splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack1-cluster-master-service"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-indexer"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-indexer-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	// Define additional service port in CR and verified the statefulset has the new port
	cr.Spec.ServiceTemplate.Spec.Ports = []corev1.ServicePort{{Name: "user-defined", Port: 32000, Protocol: "UDP"}}
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-indexer","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-indexer-secrets","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"hec","containerPort":8088,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"s2s","containerPort":9997,"protocol":"TCP"},{"name":"user-defined","containerPort":32000,"protocol":"UDP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_indexer"},{"name":"SPLUNK_INDEXER_URL","value":"splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack1-cluster-master-service"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-indexer"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-indexer-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

}

func TestGetSearchHeadStatefulSet(t *testing.T) {
	cr := enterprisev1.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := ValidateSearchHeadClusterSpec(&cr.Spec); err != nil {
				t.Errorf("ValidateSearchHeadClusterSpec() returned error: %v", err)
			}
			return GetSearchHeadStatefulSet(&cr)
		}
		configTester(t, fmt.Sprintf("GetSearchHeadStatefulSet(Replicas=%d)", cr.Spec.Replicas), f, want)
	}

	cr.Spec.Replicas = 3
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":3,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secrets","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datareceive","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.Replicas = 4
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":4,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secrets","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datareceive","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-3.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.Replicas = 5
	cr.Spec.IndexerClusterRef.Name = "stack1"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":5,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secrets","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datareceive","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-3.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-4.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack1-cluster-master-service"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.Replicas = 6
	cr.Spec.SparkRef.Name = cr.GetIdentifier()
	cr.Spec.IndexerClusterRef.Namespace = "test2"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":6,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secrets","defaultMode":420}},{"name":"mnt-splunk-jdk","emptyDir":{}},{"name":"mnt-splunk-spark","emptyDir":{}}],"initContainers":[{"name":"init","image":"splunk/spark","command":["bash","-c","cp -r /opt/jdk /mnt \u0026\u0026 cp -r /opt/spark /mnt"],"resources":{"limits":{"cpu":"1","memory":"512Mi"},"requests":{"cpu":"250m","memory":"128Mi"}},"volumeMounts":[{"name":"mnt-splunk-jdk","mountPath":"/mnt/jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/spark"}],"imagePullPolicy":"IfNotPresent"}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datareceive","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-3.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-4.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-5.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack1-cluster-master-service.test2.svc.cluster.local"},{"name":"SPLUNK_ENABLE_DFS","value":"true"},{"name":"SPARK_MASTER_HOST","value":"splunk-stack1-spark-master-service"},{"name":"SPARK_MASTER_WEBUI_PORT","value":"8009"},{"name":"SPARK_HOME","value":"/mnt/splunk-spark"},{"name":"JAVA_HOME","value":"/mnt/splunk-jdk"},{"name":"SPLUNK_DFW_NUM_SLOTS_ENABLED","value":"true"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"},{"name":"mnt-splunk-jdk","mountPath":"/mnt/splunk-jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/splunk-spark"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	// Define additional service port in CR and verified the statefulset has the new port
	cr.Spec.ServiceTemplate.Spec.Ports = []corev1.ServicePort{{Name: "user-defined", Port: 32000, Protocol: "UDP"}}
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-search-head","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":6,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secrets","defaultMode":420}},{"name":"mnt-splunk-jdk","emptyDir":{}},{"name":"mnt-splunk-spark","emptyDir":{}}],"initContainers":[{"name":"init","image":"splunk/spark","command":["bash","-c","cp -r /opt/jdk /mnt \u0026\u0026 cp -r /opt/spark /mnt"],"resources":{"limits":{"cpu":"1","memory":"512Mi"},"requests":{"cpu":"250m","memory":"128Mi"}},"volumeMounts":[{"name":"mnt-splunk-jdk","mountPath":"/mnt/jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/spark"}],"imagePullPolicy":"IfNotPresent"}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datareceive","containerPort":19000,"protocol":"TCP"},{"name":"user-defined","containerPort":32000,"protocol":"UDP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-3.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-4.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-5.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_DEPLOYER_URL","value":"splunk-stack1-deployer-service"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack1-cluster-master-service.test2.svc.cluster.local"},{"name":"SPLUNK_ENABLE_DFS","value":"true"},{"name":"SPARK_MASTER_HOST","value":"splunk-stack1-spark-master-service"},{"name":"SPARK_MASTER_WEBUI_PORT","value":"8009"},{"name":"SPARK_HOME","value":"/mnt/splunk-spark"},{"name":"JAVA_HOME","value":"/mnt/splunk-jdk"},{"name":"SPLUNK_DFW_NUM_SLOTS_ENABLED","value":"true"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"},{"name":"mnt-splunk-jdk","mountPath":"/mnt/splunk-jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/splunk-spark"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-search-head"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-search-head-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)
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

	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-standalone","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-standalone-secrets","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"hec","containerPort":8088,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"s2s","containerPort":9997,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datareceive","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_standalone"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-standalone"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-standalone-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.EphemeralStorage = true
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-standalone","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"mnt-splunk-etc","emptyDir":{}},{"name":"mnt-splunk-var","emptyDir":{}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-standalone-secrets","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"hec","containerPort":8088,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"s2s","containerPort":9997,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datareceive","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_standalone"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"mnt-splunk-etc","mountPath":"/opt/splunk/etc"},{"name":"mnt-splunk-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-standalone"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"serviceName":"splunk-stack1-standalone-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.EphemeralStorage = false
	cr.Spec.SparkRef.Name = cr.GetIdentifier()
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-standalone","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-standalone-secrets","defaultMode":420}},{"name":"mnt-splunk-jdk","emptyDir":{}},{"name":"mnt-splunk-spark","emptyDir":{}}],"initContainers":[{"name":"init","image":"splunk/spark","command":["bash","-c","cp -r /opt/jdk /mnt \u0026\u0026 cp -r /opt/spark /mnt"],"resources":{"limits":{"cpu":"1","memory":"512Mi"},"requests":{"cpu":"250m","memory":"128Mi"}},"volumeMounts":[{"name":"mnt-splunk-jdk","mountPath":"/mnt/jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/spark"}],"imagePullPolicy":"IfNotPresent"}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"hec","containerPort":8088,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"s2s","containerPort":9997,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datareceive","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_standalone"},{"name":"SPLUNK_ENABLE_DFS","value":"true"},{"name":"SPARK_MASTER_HOST","value":"splunk-stack1-spark-master-service"},{"name":"SPARK_MASTER_WEBUI_PORT","value":"8009"},{"name":"SPARK_HOME","value":"/mnt/splunk-spark"},{"name":"JAVA_HOME","value":"/mnt/splunk-jdk"},{"name":"SPLUNK_DFW_NUM_SLOTS_ENABLED","value":"false"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"},{"name":"mnt-splunk-jdk","mountPath":"/mnt/splunk-jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/splunk-spark"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-standalone"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-standalone-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.IndexerClusterRef.Name = "stack2"
	cr.Spec.StorageClassName = "gp2"
	cr.Spec.SchedulerName = "custom-scheduler"
	cr.Spec.Defaults = "defaults-string"
	cr.Spec.DefaultsURL = "/mnt/defaults/defaults.yml"
	cr.Spec.Volumes = []corev1.Volume{
		{Name: "defaults"},
	}
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-standalone","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"defaults"},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-standalone-secrets","defaultMode":420}},{"name":"mnt-splunk-defaults","configMap":{"name":"splunk-stack1-standalone-defaults","defaultMode":420}},{"name":"mnt-splunk-jdk","emptyDir":{}},{"name":"mnt-splunk-spark","emptyDir":{}}],"initContainers":[{"name":"init","image":"splunk/spark","command":["bash","-c","cp -r /opt/jdk /mnt \u0026\u0026 cp -r /opt/spark /mnt"],"resources":{"limits":{"cpu":"1","memory":"512Mi"},"requests":{"cpu":"250m","memory":"128Mi"}},"volumeMounts":[{"name":"mnt-splunk-jdk","mountPath":"/mnt/jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/spark"}],"imagePullPolicy":"IfNotPresent"}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"hec","containerPort":8088,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"s2s","containerPort":9997,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datareceive","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml,/mnt/defaults/defaults.yml,/mnt/splunk-defaults/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_standalone"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack2-cluster-master-service"},{"name":"SPLUNK_ENABLE_DFS","value":"true"},{"name":"SPARK_MASTER_HOST","value":"splunk-stack1-spark-master-service"},{"name":"SPARK_MASTER_WEBUI_PORT","value":"8009"},{"name":"SPARK_HOME","value":"/mnt/splunk-spark"},{"name":"JAVA_HOME","value":"/mnt/splunk-jdk"},{"name":"SPLUNK_DFW_NUM_SLOTS_ENABLED","value":"false"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"defaults","mountPath":"/mnt/defaults"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"},{"name":"mnt-splunk-defaults","mountPath":"/mnt/splunk-defaults"},{"name":"mnt-splunk-jdk","mountPath":"/mnt/splunk-jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/splunk-spark"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-standalone"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"custom-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}},"storageClassName":"gp2"},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}},"storageClassName":"gp2"},"status":{}}],"serviceName":"splunk-stack1-standalone-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)
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

	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-license-master","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"license-master","app.kubernetes.io/instance":"splunk-stack1-license-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-master","app.kubernetes.io/part-of":"splunk-stack1-license-master"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-master","app.kubernetes.io/instance":"splunk-stack1-license-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-master","app.kubernetes.io/part-of":"splunk-stack1-license-master"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-license-master-secrets","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_license_master"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-license-master"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-master","app.kubernetes.io/instance":"splunk-stack1-license-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-master","app.kubernetes.io/part-of":"splunk-stack1-license-master"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-master","app.kubernetes.io/instance":"splunk-stack1-license-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-master","app.kubernetes.io/part-of":"splunk-stack1-license-master"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-license-master-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.LicenseURL = "/mnt/splunk.lic"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-license-master","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"license-master","app.kubernetes.io/instance":"splunk-stack1-license-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-master","app.kubernetes.io/part-of":"splunk-stack1-license-master"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-master","app.kubernetes.io/instance":"splunk-stack1-license-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-master","app.kubernetes.io/part-of":"splunk-stack1-license-master"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-license-master-secrets","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_license_master"},{"name":"SPLUNK_LICENSE_URI","value":"/mnt/splunk.lic"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-license-master"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-master","app.kubernetes.io/instance":"splunk-stack1-license-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-master","app.kubernetes.io/part-of":"splunk-stack1-license-master"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-master","app.kubernetes.io/instance":"splunk-stack1-license-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-master","app.kubernetes.io/part-of":"splunk-stack1-license-master"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-license-master-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)
}

func TestGetClusterMasterStatefulSet(t *testing.T) {
	cr := enterprisev1.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := ValidateIndexerClusterSpec(&cr.Spec); err != nil {
				t.Errorf("ValidateSearchHeadClusterSpec() returned error: %v", err)
			}
			return GetClusterMasterStatefulSet(&cr)
		}
		configTester(t, fmt.Sprintf("GetClusterMasterStatefulSet(replicas=%d)", cr.Spec.Replicas), f, want)
	}

	cr.Spec.Replicas = 1
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-master","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-indexer-secrets","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_INDEXER_URL","value":"splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-master"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-master-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.Replicas = 2
	cr.Spec.LicenseMasterRef.Name = "stack1"
	cr.Spec.LicenseMasterRef.Namespace = "test"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-master","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-indexer-secrets","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_LICENSE_MASTER_URL","value":"splunk-stack1-license-master-service.test.svc.cluster.local"},{"name":"SPLUNK_INDEXER_URL","value":"splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local,splunk-stack1-indexer-1.splunk-stack1-indexer-headless.test.svc.cluster.local"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-master"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-master-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.Replicas = 3
	cr.Spec.LicenseMasterRef.Name = ""
	cr.Spec.LicenseURL = "/mnt/splunk.lic"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-master","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-indexer-secrets","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_LICENSE_URI","value":"/mnt/splunk.lic"},{"name":"SPLUNK_INDEXER_URL","value":"splunk-stack1-indexer-0.splunk-stack1-indexer-headless.test.svc.cluster.local,splunk-stack1-indexer-1.splunk-stack1-indexer-headless.test.svc.cluster.local,splunk-stack1-indexer-2.splunk-stack1-indexer-headless.test.svc.cluster.local"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-master"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-master","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-master-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)
}

func TestGetDeployerStatefulSet(t *testing.T) {
	cr := enterprisev1.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	test := func(want string) {
		f := func() (interface{}, error) {
			if err := ValidateSearchHeadClusterSpec(&cr.Spec); err != nil {
				t.Errorf("ValidateSearchHeadClusterSpec() returned error: %v", err)
			}
			return GetDeployerStatefulSet(&cr)
		}
		configTester(t, "GetDeployerStatefulSet()", f, want)
	}

	cr.Spec.Replicas = 3
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-deployer","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-search-head-secrets","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_deployer"},{"name":"SPLUNK_SEARCH_HEAD_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local,splunk-stack1-search-head-2.splunk-stack1-search-head-headless.test.svc.cluster.local"},{"name":"SPLUNK_SEARCH_HEAD_CAPTAIN_URL","value":"splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-deployer"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-deployer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"deployer","app.kubernetes.io/part-of":"splunk-stack1-search-head"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-deployer-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)
}

func TestGetSplunkService(t *testing.T) {
	cr := enterprisev1.IndexerCluster{
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

	test(SplunkIndexer, false, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-indexer-service","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"splunkweb","protocol":"TCP","port":8000,"targetPort":8000},{"name":"hec","protocol":"TCP","port":8088,"targetPort":8088},{"name":"splunkd","protocol":"TCP","port":8089,"targetPort":8089},{"name":"s2s","protocol":"TCP","port":9997,"targetPort":9997}],"selector":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"status":{"loadBalancer":{}}}`)
	test(SplunkIndexer, true, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-indexer-headless","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"splunkweb","protocol":"TCP","port":8000,"targetPort":8000},{"name":"hec","protocol":"TCP","port":8088,"targetPort":8088},{"name":"splunkd","protocol":"TCP","port":8089,"targetPort":8089},{"name":"s2s","protocol":"TCP","port":9997,"targetPort":9997}],"selector":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"clusterIP":"None","type":"ClusterIP"},"status":{"loadBalancer":{}}}`)

	cr.Spec.ServiceTemplate.Spec.Type = "LoadBalancer"
	cr.Spec.ServiceTemplate.ObjectMeta.Labels = map[string]string{"1": "2"}
	cr.ObjectMeta.Labels = map[string]string{"one": "two"}
	cr.ObjectMeta.Annotations = map[string]string{"a": "b"}

	test(SplunkSearchHead, false, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-search-head-service","namespace":"test","creationTimestamp":null,"labels":{"1":"2","app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head","one":"two"},"annotations":{"a":"b"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"splunkweb","protocol":"TCP","port":8000,"targetPort":8000},{"name":"splunkd","protocol":"TCP","port":8089,"targetPort":8089},{"name":"dfsmaster","protocol":"TCP","port":9000,"targetPort":9000},{"name":"dfccontrol","protocol":"TCP","port":17000,"targetPort":17000},{"name":"datareceive","protocol":"TCP","port":19000,"targetPort":19000}],"selector":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"type":"LoadBalancer"},"status":{"loadBalancer":{}}}`)
	test(SplunkSearchHead, true, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-search-head-headless","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head","one":"two"},"annotations":{"a":"b"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"splunkweb","protocol":"TCP","port":8000,"targetPort":8000},{"name":"splunkd","protocol":"TCP","port":8089,"targetPort":8089},{"name":"dfsmaster","protocol":"TCP","port":9000,"targetPort":9000},{"name":"dfccontrol","protocol":"TCP","port":17000,"targetPort":17000},{"name":"datareceive","protocol":"TCP","port":19000,"targetPort":19000}],"selector":{"app.kubernetes.io/component":"search-head","app.kubernetes.io/instance":"splunk-stack1-search-head","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"search-head","app.kubernetes.io/part-of":"splunk-stack1-search-head"},"clusterIP":"None","type":"ClusterIP","publishNotReadyAddresses":true},"status":{"loadBalancer":{}}}`)
}

func TestGetSplunkDefaults(t *testing.T) {
	cr := enterprisev1.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterprisev1.IndexerClusterSpec{
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
	cr := enterprisev1.IndexerCluster{
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

func TestGetService(t *testing.T) {
	cr := enterprisev1.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterprisev1.IndexerClusterSpec{
			Replicas: 3,
			CommonSplunkSpec: enterprisev1.CommonSplunkSpec{
				CommonSpec: enterprisev1.CommonSpec{
					ServiceTemplate: corev1.Service{
						Spec: corev1.ServiceSpec{
							Ports: []corev1.ServicePort{{Name: "user-defined", Port: 32000, TargetPort: intstr.FromInt(6443)}},
						},
					},
				},
			},
		},
	}

	test := func(instanceType InstanceType, want string) {
		f := func() (interface{}, error) {
			return GetSplunkService(&cr, cr.Spec.CommonSpec, instanceType, false), nil
		}
		configTester(t, "GetSplunkService()", f, want)
	}

	test(SplunkIndexer, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-indexer-service","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"ports":[{"name":"user-defined","port":32000,"targetPort":6443},{"name":"splunkweb","protocol":"TCP","port":8000,"targetPort":8000},{"name":"hec","protocol":"TCP","port":8088,"targetPort":8088},{"name":"splunkd","protocol":"TCP","port":8089,"targetPort":8089},{"name":"s2s","protocol":"TCP","port":9997,"targetPort":9997}],"selector":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-indexer","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"indexer","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"status":{"loadBalancer":{}}}`)
}

func TestSetVolumeDefault(t *testing.T) {
	cr := enterprisev1.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	setVolumeDefaults(&cr.Spec.CommonSplunkSpec)
	if cr.Spec.CommonSplunkSpec.Volumes == nil {
		t.Errorf("setVolumeDefaults() returns nil for Volumes")
	}

	mode := int32(644)
	cr.Spec.CommonSplunkSpec.Volumes = []corev1.Volume{
		{
			Name: "vol1",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "top-secret1",
				},
			},
		},
		{
			Name: "vol2",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  "top-secret2",
					DefaultMode: &mode,
				},
			},
		},
		{
			Name: "vol3",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "config3",
					},
				},
			},
		},
	}

	// Make sure the default mode is set correcty
	setVolumeDefaults(&cr.Spec.CommonSplunkSpec)
	if cr.Spec.CommonSplunkSpec.Volumes == nil {
		t.Errorf("setVolumeDefaults() returns nil for Volumes")
	}

	for _, v := range cr.Spec.CommonSplunkSpec.Volumes {
		if v.Name == "vol1" {
			if *v.Secret.DefaultMode != int32(corev1.SecretVolumeSourceDefaultMode) {
				t.Errorf("setVolumeDefaults() did not set defaultMode correctly. Want %d, Got %d", int32(corev1.SecretVolumeSourceDefaultMode), *v.Secret.DefaultMode)
			}
		} else if v.Name == "vol2" {
			if *v.Secret.DefaultMode != mode {
				t.Errorf("setVolumeDefaults() did not set defaultMode correctly. Want %d, Got %d", mode, *v.Secret.DefaultMode)
			}
		} else if v.Name == "vol3" {
			if *v.ConfigMap.DefaultMode != int32(corev1.ConfigMapVolumeSourceDefaultMode) {
				t.Errorf("setVolumeDefaults() did not set defaultMode correctly. Want %d, Got %d", int32(corev1.ConfigMapVolumeSourceDefaultMode), *v.ConfigMap.DefaultMode)
			}
		}
	}
}

func TestSetServiceTemplateDefaults(t *testing.T) {
	cr := enterprisev1.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterprisev1.IndexerClusterSpec{
			CommonSplunkSpec: enterprisev1.CommonSplunkSpec{
				CommonSpec: enterprisev1.CommonSpec{
					ServiceTemplate: corev1.Service{
						Spec: corev1.ServiceSpec{
							Ports: []corev1.ServicePort{
								{
									Name: "http",
									Port: 80,
								},
								{
									Name: "https",
									Port: 443,
									TargetPort: intstr.IntOrString{
										Type:   intstr.Int,
										IntVal: 8443,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	setServiceTemplateDefaults(&cr.Spec.CommonSplunkSpec)
	for _, p := range cr.Spec.CommonSplunkSpec.CommonSpec.ServiceTemplate.Spec.Ports {
		switch p.Name {
		case "http":
			if p.TargetPort.IntValue() != int(p.Port) {
				t.Errorf("setServiceTemplateDefaults() did not set target port correctly. Want %d, Got %d", p.Port, p.TargetPort.IntVal)
			}
			if p.Protocol != corev1.ProtocolTCP {
				t.Errorf("setServiceTemplateDefaults() did not set protocol correctly. Want %s, Got %s", corev1.ProtocolTCP, p.Protocol)
			}
		case "https":
			if p.TargetPort.IntValue() != 8443 {
				t.Errorf("setServiceTemplateDefaults() did not set target port correctly. Want 8443, Got %d", p.TargetPort.IntVal)
			}
		}
	}
}
