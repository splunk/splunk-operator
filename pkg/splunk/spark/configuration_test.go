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

package spark

import (
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
)

func TestGetSparkDeployment(t *testing.T) {
	cr := enterprisev1.Spark{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterprisev1.SparkSpec{
			Replicas: 3,
		},
	}

	test := func(instanceType InstanceType, want string) {
		err := ValidateSparkSpec(&cr.Spec)
		if err != nil {
			t.Errorf("ValidateSparkSpec() returned error: %v", err)
		}

		deployment, err := GetSparkDeployment(&cr, instanceType)
		if err != nil {
			t.Errorf("GetSparkDeployment() returned error: %v", err)
		}

		got, err := json.Marshal(deployment)
		if err != nil {
			t.Errorf("GetSparkDeployment(\"%s\",%d) failed to marshall: %v", instanceType, cr.Spec.Replicas, err)
		}
		if string(got) != want {
			t.Errorf("GetSparkDeployment(\"%s\",%d) = %s; want %s", instanceType, cr.Spec.Replicas, got, want)
		}
	}

	test(SparkMaster, `{"kind":"Deployment","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-spark-master","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"spark","app.kubernetes.io/instance":"splunk-stack1-spark-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"spark-master","app.kubernetes.io/part-of":"splunk-stack1-spark"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"spark","app.kubernetes.io/instance":"splunk-stack1-spark-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"spark-master","app.kubernetes.io/part-of":"splunk-stack1-spark"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"8009"}},"spec":{"containers":[{"name":"spark","image":"splunk/spark","ports":[{"name":"sparkmaster","containerPort":7777,"protocol":"TCP"},{"name":"sparkwebui","containerPort":8009,"protocol":"TCP"}],"env":[{"name":"SPLUNK_ROLE","value":"splunk_spark_master"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"livenessProbe":{"httpGet":{"path":"/","port":8009},"initialDelaySeconds":30,"timeoutSeconds":10,"periodSeconds":10},"readinessProbe":{"httpGet":{"path":"/","port":8009},"initialDelaySeconds":5,"timeoutSeconds":10,"periodSeconds":10},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"hostname":"splunk-stack1-spark-master-service","affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-spark-master"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"strategy":{}},"status":{}}`)
	test(SparkWorker, `{"kind":"Deployment","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-spark-worker","namespace":"test","creationTimestamp":null,"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":3,"selector":{"matchLabels":{"app.kubernetes.io/component":"spark","app.kubernetes.io/instance":"splunk-stack1-spark-worker","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"spark-worker","app.kubernetes.io/part-of":"splunk-stack1-spark"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"spark","app.kubernetes.io/instance":"splunk-stack1-spark-worker","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"spark-worker","app.kubernetes.io/part-of":"splunk-stack1-spark"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997,7777,9000,17000,17500,19000","traffic.sidecar.istio.io/includeInboundPorts":"7000"}},"spec":{"containers":[{"name":"spark","image":"splunk/spark","ports":[{"name":"workerwebui","containerPort":7000,"protocol":"TCP"},{"name":"dfwreceivedata","containerPort":17500,"protocol":"TCP"}],"env":[{"name":"SPLUNK_ROLE","value":"splunk_spark_worker"},{"name":"SPARK_MASTER_HOSTNAME","value":"splunk-stack1-spark-master-service"},{"name":"SPARK_WORKER_PORT","value":"7777"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"livenessProbe":{"httpGet":{"path":"/","port":7000},"initialDelaySeconds":30,"timeoutSeconds":10,"periodSeconds":10},"readinessProbe":{"httpGet":{"path":"/","port":7000},"initialDelaySeconds":5,"timeoutSeconds":10,"periodSeconds":10},"imagePullPolicy":"IfNotPresent"}],"securityContext":{"runAsUser":41812,"fsGroup":41812},"hostname":"splunk-stack1-spark-worker-service","affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-spark-worker"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"strategy":{}},"status":{}}`)
}

func TestGetSparkService(t *testing.T) {
	cr := enterprisev1.Spark{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Spark",
			APIVersion: "enterprise.splunk.com/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
			UID:       "05db21b4-7244-4022-a844-c131a8747f30",
		},
	}

	test := func(instanceType InstanceType, isHeadless bool, want string) {
		deployment := GetSparkService(&cr, instanceType, isHeadless)
		got, err := json.Marshal(deployment)
		if err != nil {
			t.Errorf("GetSparkService(\"%s\",%t) failed to marshall: %v", instanceType, isHeadless, err)
		}
		if string(got) != want {
			t.Errorf("GetSparkService(\"%s\",%t) = %s; want %s", instanceType, isHeadless, got, want)
		}
	}

	test(SparkMaster, false, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-spark-master-service","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"spark","app.kubernetes.io/instance":"splunk-stack1-spark-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"spark-master","app.kubernetes.io/part-of":"splunk-stack1-spark"},"ownerReferences":[{"apiVersion":"enterprise.splunk.com/v1","kind":"Spark","name":"stack1","uid":"05db21b4-7244-4022-a844-c131a8747f30","controller":true}]},"spec":{"ports":[{"name":"sparkmaster","protocol":"TCP","port":7777,"targetPort":7777},{"name":"sparkwebui","protocol":"TCP","port":8009,"targetPort":8009}],"selector":{"app.kubernetes.io/component":"spark","app.kubernetes.io/instance":"splunk-stack1-spark-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"spark-master","app.kubernetes.io/part-of":"splunk-stack1-spark"}},"status":{"loadBalancer":{}}}`)
	test(SparkMaster, true, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-spark-master-headless","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"spark","app.kubernetes.io/instance":"splunk-stack1-spark-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"spark-master","app.kubernetes.io/part-of":"splunk-stack1-spark"},"ownerReferences":[{"apiVersion":"enterprise.splunk.com/v1","kind":"Spark","name":"stack1","uid":"05db21b4-7244-4022-a844-c131a8747f30","controller":true}]},"spec":{"ports":[{"name":"sparkmaster","protocol":"TCP","port":7777,"targetPort":7777},{"name":"sparkwebui","protocol":"TCP","port":8009,"targetPort":8009}],"selector":{"app.kubernetes.io/component":"spark","app.kubernetes.io/instance":"splunk-stack1-spark-master","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"spark-master","app.kubernetes.io/part-of":"splunk-stack1-spark"},"clusterIP":"None","type":"ClusterIP"},"status":{"loadBalancer":{}}}`)

	cr.Spec.ServiceTemplate.Spec.Type = "LoadBalancer"
	cr.Spec.ServiceTemplate.ObjectMeta.Labels = map[string]string{"1": "2"}
	cr.ObjectMeta.Labels = map[string]string{"one": "two"}
	cr.ObjectMeta.Annotations = map[string]string{"a": "b"}

	test(SparkWorker, false, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-spark-worker-service","namespace":"test","creationTimestamp":null,"labels":{"1":"2","app.kubernetes.io/component":"spark","app.kubernetes.io/instance":"splunk-stack1-spark-worker","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"spark-worker","app.kubernetes.io/part-of":"splunk-stack1-spark","one":"two"},"annotations":{"a":"b"},"ownerReferences":[{"apiVersion":"enterprise.splunk.com/v1","kind":"Spark","name":"stack1","uid":"05db21b4-7244-4022-a844-c131a8747f30","controller":true}]},"spec":{"ports":[{"name":"workerwebui","protocol":"TCP","port":7000,"targetPort":7000},{"name":"dfwreceivedata","protocol":"TCP","port":17500,"targetPort":17500}],"selector":{"app.kubernetes.io/component":"spark","app.kubernetes.io/instance":"splunk-stack1-spark-worker","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"spark-worker","app.kubernetes.io/part-of":"splunk-stack1-spark"},"type":"LoadBalancer"},"status":{"loadBalancer":{}}}`)
	test(SparkWorker, true, `{"kind":"Service","apiVersion":"v1","metadata":{"name":"splunk-stack1-spark-worker-headless","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"spark","app.kubernetes.io/instance":"splunk-stack1-spark-worker","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"spark-worker","app.kubernetes.io/part-of":"splunk-stack1-spark","one":"two"},"annotations":{"a":"b"},"ownerReferences":[{"apiVersion":"enterprise.splunk.com/v1","kind":"Spark","name":"stack1","uid":"05db21b4-7244-4022-a844-c131a8747f30","controller":true}]},"spec":{"ports":[{"name":"workerwebui","protocol":"TCP","port":7000,"targetPort":7000},{"name":"dfwreceivedata","protocol":"TCP","port":17500,"targetPort":17500}],"selector":{"app.kubernetes.io/component":"spark","app.kubernetes.io/instance":"splunk-stack1-spark-worker","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"spark-worker","app.kubernetes.io/part-of":"splunk-stack1-spark"},"clusterIP":"None","type":"ClusterIP"},"status":{"loadBalancer":{}}}`)
}
