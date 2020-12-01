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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1beta1"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

func TestApplyStandalone(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Service-test-splunk-stack1-standalone-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-standalone-service"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-standalone-secret-v1"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-standalone-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-standalone-smartstore"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-standalone"},
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

	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0], funcCalls[2], funcCalls[3], funcCalls[5], funcCalls[8]}, "Update": {funcCalls[0]}, "List": {listmockCall[0]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[2], funcCalls[3], funcCalls[4], funcCalls[5], funcCalls[6], funcCalls[7], funcCalls[8]}, "Update": {funcCalls[8]}, "List": {listmockCall[0]}}
	current := enterprisev1.Standalone{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	revised := current.DeepCopy()
	revised.Spec.Image = "splunk/test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyStandalone(c, cr.(*enterprisev1.Standalone))
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyStandalone", &current, revised, createCalls, updateCalls, reconcile, true)

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
		_, err := ApplyStandalone(c, cr.(*enterprisev1.Standalone))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)
}

func TestApplyStandaloneWithSmartstore(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-standalone-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-standalone-smartstore"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Service-test-splunk-stack1-standalone-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-standalone-service"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-standalone-secret-v1"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-standalone-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-standalone-smartstore"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-standalone"},
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

	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[2], funcCalls[6], funcCalls[7], funcCalls[9], funcCalls[12]}, "Update": {funcCalls[0]}, "List": {listmockCall[0]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[2], funcCalls[3], funcCalls[4], funcCalls[5], funcCalls[6], funcCalls[7], funcCalls[8], funcCalls[9], funcCalls[10], funcCalls[11], funcCalls[12]}, "Update": {funcCalls[11], funcCalls[12]}, "List": {listmockCall[0]}}

	current := enterprisev1.Standalone{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterprisev1.StandaloneSpec{
			Replicas: 1,
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
		},
	}

	client := spltest.NewMockClient()

	// Without S3 keys, ApplyStandalone should fail
	_, err := ApplyStandalone(client, &current)
	if err == nil {
		t.Errorf("ApplyStandalone should fail without S3 secrets configured")
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

	revised := current.DeepCopy()
	revised.Spec.Image = "splunk/test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyStandalone(c, cr.(*enterprisev1.Standalone))
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyStandaloneWithSmartstore", &current, revised, createCalls, updateCalls, reconcile, true, secret)
}

func TestGetStandaloneStatefulSet(t *testing.T) {
	cr := enterprisev1.Standalone{
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
			if err := validateStandaloneSpec(&cr.Spec); err != nil {
				t.Errorf("validateStandaloneSpec() returned error: %v", err)
			}
			return getStandaloneStatefulSet(c, &cr)
		}
		configTester(t, "getStandaloneStatefulSet()", f, want)
	}

	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-standalone","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-standalone-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"hec","containerPort":8088,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"s2s","containerPort":9997,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datareceive","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_standalone"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-standalone"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-standalone-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.EphemeralStorage = true
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-standalone","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"mnt-splunk-etc","emptyDir":{}},{"name":"mnt-splunk-var","emptyDir":{}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-standalone-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"hec","containerPort":8088,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"s2s","containerPort":9997,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datareceive","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_standalone"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"mnt-splunk-etc","mountPath":"/opt/splunk/etc"},{"name":"mnt-splunk-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-standalone"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"serviceName":"splunk-stack1-standalone-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.EphemeralStorage = false
	cr.Spec.SparkRef.Name = cr.GetName()
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-standalone","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-standalone-secret-v1","defaultMode":420}},{"name":"mnt-splunk-jdk","emptyDir":{}},{"name":"mnt-splunk-spark","emptyDir":{}}],"initContainers":[{"name":"init","image":"splunk/spark","command":["bash","-c","cp -r /opt/jdk /mnt \u0026\u0026 cp -r /opt/spark /mnt"],"resources":{"limits":{"cpu":"1","memory":"512Mi"},"requests":{"cpu":"250m","memory":"128Mi"}},"volumeMounts":[{"name":"mnt-splunk-jdk","mountPath":"/mnt/jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/spark"}],"imagePullPolicy":"IfNotPresent"}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"hec","containerPort":8088,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"s2s","containerPort":9997,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datareceive","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_standalone"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_ENABLE_DFS","value":"true"},{"name":"SPARK_MASTER_HOST","value":"splunk-stack1-spark-master-service"},{"name":"SPARK_MASTER_WEBUI_PORT","value":"8009"},{"name":"SPARK_HOME","value":"/mnt/splunk-spark"},{"name":"JAVA_HOME","value":"/mnt/splunk-jdk"},{"name":"SPLUNK_DFW_NUM_SLOTS_ENABLED","value":"false"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"},{"name":"mnt-splunk-jdk","mountPath":"/mnt/splunk-jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/splunk-spark"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-standalone"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-standalone-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)

	cr.Spec.ClusterMasterRef.Name = "stack2"
	cr.Spec.StorageClassName = "gp2"
	cr.Spec.SchedulerName = "custom-scheduler"
	cr.Spec.Defaults = "defaults-string"
	cr.Spec.DefaultsURL = "/mnt/defaults/defaults.yml"
	cr.Spec.Volumes = []corev1.Volume{
		{Name: "defaults"},
	}
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-standalone","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8000,8088"}},"spec":{"volumes":[{"name":"defaults"},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-standalone-secret-v1","defaultMode":420}},{"name":"mnt-splunk-defaults","configMap":{"name":"splunk-stack1-standalone-defaults","defaultMode":420}},{"name":"mnt-splunk-jdk","emptyDir":{}},{"name":"mnt-splunk-spark","emptyDir":{}}],"initContainers":[{"name":"init","image":"splunk/spark","command":["bash","-c","cp -r /opt/jdk /mnt \u0026\u0026 cp -r /opt/spark /mnt"],"resources":{"limits":{"cpu":"1","memory":"512Mi"},"requests":{"cpu":"250m","memory":"128Mi"}},"volumeMounts":[{"name":"mnt-splunk-jdk","mountPath":"/mnt/jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/spark"}],"imagePullPolicy":"IfNotPresent"}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"hec","containerPort":8088,"protocol":"TCP"},{"name":"splunkd","containerPort":8089,"protocol":"TCP"},{"name":"dfsmaster","containerPort":9000,"protocol":"TCP"},{"name":"s2s","containerPort":9997,"protocol":"TCP"},{"name":"dfccontrol","containerPort":17000,"protocol":"TCP"},{"name":"datareceive","containerPort":19000,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-defaults/default.yml,/mnt/defaults/defaults.yml,/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_search_head"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"splunk-stack2-cluster-master-service"},{"name":"SPLUNK_ENABLE_DFS","value":"true"},{"name":"SPARK_MASTER_HOST","value":"splunk-stack1-spark-master-service"},{"name":"SPARK_MASTER_WEBUI_PORT","value":"8009"},{"name":"SPARK_HOME","value":"/mnt/splunk-spark"},{"name":"JAVA_HOME","value":"/mnt/splunk-jdk"},{"name":"SPLUNK_DFW_NUM_SLOTS_ENABLED","value":"false"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"defaults","mountPath":"/mnt/defaults"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"},{"name":"mnt-splunk-defaults","mountPath":"/mnt/splunk-defaults"},{"name":"mnt-splunk-jdk","mountPath":"/mnt/splunk-jdk"},{"name":"mnt-splunk-spark","mountPath":"/mnt/splunk-spark"}],"livenessProbe":{"exec":{"command":["/sbin/checkstate.sh"]},"initialDelaySeconds":300,"timeoutSeconds":30,"periodSeconds":30},"readinessProbe":{"exec":{"command":["/bin/grep","started","/opt/container_artifact/splunk-container.state"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-standalone"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"custom-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}},"storageClassName":"gp2"},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"standalone","app.kubernetes.io/instance":"splunk-stack1-standalone","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"standalone","app.kubernetes.io/part-of":"splunk-stack1-standalone"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}},"storageClassName":"gp2"},"status":{}}],"serviceName":"splunk-stack1-standalone-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0}}`)
}

func TestApplyStandaloneSmartstoreKeyChangeDetection(t *testing.T) {
	current := enterprisev1.Standalone{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterprisev1.StandaloneSpec{
			Replicas: 1,
			SmartStore: enterprisev1.SmartStoreSpec{
				VolList: []enterprisev1.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "splunk-test-secret"},
				},
				IndexList: []enterprisev1.IndexSpec{
					{Name: "salesdata1", RemotePath: "remotepath1",
						IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{
							VolName: "msos_s2s3_vol"},
					},
				},
			},
		},
	}
	client := spltest.NewMockClient()

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

	_, err = ApplyStandalone(client, &current)
	if err != nil {
		t.Errorf("ApplyStandalone should not fail with full configuration")
	}

	// Now change the secret key
	secret.Data[s3AccessKey] = []byte("changed")
	current.Status.ResourceRevMap["splunk-test-secret"] = "3456"

	_, err = splctrl.ApplySecret(client, secret)
	if err != nil {
		t.Errorf(err.Error())
	}

	changed := AreRemoteVolumeKeysChanged(client, &current, SplunkStandalone, &current.Spec.SmartStore, current.Status.ResourceRevMap, &err)

	if !changed {
		t.Errorf("Key change was not detected %v", err)
	}
}
