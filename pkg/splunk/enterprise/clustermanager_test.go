// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"k8s.io/apimachinery/pkg/runtime/schema"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	runtime "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

func TestApplyClusterManager(t *testing.T) {

	// redefining cpmakeTar to return nil always
	cpMakeTar = func(src localPath, dest remotePath, writer io.Writer) error {
		return nil
	}

	ctx := context.TODO()
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-cluster-manager-stack1-configmap"},
		{MetaName: "*v1.Service-test-splunk-stack1-cluster-manager-service"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-manager"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-cluster-manager-secret-v1"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermanager-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermanager-smartstore"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-manager"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-manager"},
		{MetaName: "*v4.ClusterManager-test-stack1"},
		{MetaName: "*v4.ClusterManager-test-stack1"},
	}
	updateFuncCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-cluster-manager-stack1-configmap"},
		{MetaName: "*v1.Service-test-splunk-stack1-cluster-manager-service"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-manager"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-cluster-manager-secret-v1"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermanager-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermanager-smartstore"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-manager"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-manager"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-manager"},
		{MetaName: "*v4.ClusterManager-test-stack1"},
		{MetaName: "*v4.ClusterManager-test-stack1"},
	}

	labels := map[string]string{
		"app.kubernetes.io/component":  "versionedSecrets",
		"app.kubernetes.io/managed-by": "splunk-operator",
	}
	listOpts := []runtime.ListOption{
		runtime.InNamespace("test"),
		runtime.MatchingLabels(labels),
	}
	listmockCall := []spltest.MockFuncCall{
		{ListOpts: listOpts}}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0], funcCalls[3], funcCalls[4], funcCalls[6], funcCalls[9], funcCalls[5]}, "List": {listmockCall[0]}, "Update": {funcCalls[0]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": updateFuncCalls, "Update": {funcCalls[5]}, "List": {listmockCall[0]}}

	current := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterManager",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}
	// Define GroupVersionKind
	gvk := schema.GroupVersionKind{
		Group:   "enterprise.splunk.com",
		Version: "v4",
		Kind:    "ClusterManager",
	}
	current.SetGroupVersionKind(gvk)
	revised := current.DeepCopy()
	revised.Spec.Image = "splunk/test"
	revised.SetGroupVersionKind(gvk)
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyClusterManager(ctx, c, cr.(*enterpriseApi.ClusterManager))
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyClusterManager", &current, revised, createCalls, updateCalls, reconcile, true)

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
		_, err := ApplyClusterManager(ctx, c, cr.(*enterpriseApi.ClusterManager))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)

	// Negative testing
	current.Spec.CommonSplunkSpec.LivenessProbe = &enterpriseApi.Probe{
		InitialDelaySeconds: -1,
	}
	c := spltest.NewMockClient()
	_ = errors.New(splcommon.Rerr)
	current.Kind = "ClusterManager"
	_, err := ApplyClusterManager(ctx, c, &current)
	if err == nil {
		t.Errorf("Expected error")
	}

	// Smartstore spec
	current.Spec.CommonSplunkSpec.LivenessProbe = &enterpriseApi.Probe{
		InitialDelaySeconds: 5,
	}
	current.Spec.SmartStore = enterpriseApi.SmartStoreSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{
				Name:      "msos_s2s3_vol",
				Endpoint:  "https://s3-eu-west-2.amazonaws.com",
				Path:      "testbucket-rs-london",
				SecretRef: "splunk-test-secret",
			},
		},

		IndexList: []enterpriseApi.IndexSpec{
			{Name: "salesdata1", RemotePath: "remotepath1",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata3", RemotePath: "remotepath3",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
		},
	}

	current.Status.SmartStore = enterpriseApi.SmartStoreSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{
				Name:      "msos_s2s3_vol",
				Endpoint:  "https://s3-eu-west-2.amazonaws.com",
				Path:      "testbucket-rs-london",
				SecretRef: "splunk-test-secret",
			},
		},

		IndexList: []enterpriseApi.IndexSpec{
			{Name: "salesdata1", RemotePath: "remotepath1",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata2", RemotePath: "remotepath2",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
			{Name: "salesdata3", RemotePath: "remotepath3",
				IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
					VolName: "msos_s2s3_vol"},
			},
		},
		Defaults: enterpriseApi.IndexConfDefaultsSpec{
			IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
				VolName: "msos_s2s3_vol",
			},
		},
	}

	current.Kind = "ClusterManager"
	_, err = ApplyClusterManager(ctx, c, &current)
	if err == nil {
		t.Errorf("Expected error")
	}

	sec := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "s3-secret",
			Namespace:       "test",
			ResourceVersion: "v1",
		},
	}
	c.Create(ctx, &sec)
	current.Spec.SmartStore.VolList[0].SecretRef = "s3-secret"
	current.Status.SmartStore.VolList[0].SecretRef = "s3-secret"
	current.Status.ResourceRevMap["s3-secret"] = "v2"
	current.Kind = "ClusterManager"
	_, err = ApplyClusterManager(ctx, c, &current)
	if err == nil {
		t.Errorf("Expected error")
	}

	cmap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-clustermanager-smartstore",
			Namespace: "test",
		},
	}
	c.Create(ctx, &cmap)
	current.Spec.SmartStore.VolList[0].SecretRef = ""
	current.Spec.SmartStore.Defaults.IndexAndGlobalCommonSpec.VolName = "msos_s2s3_vol"
	current.Kind = "ClusterManager"
	_, err = ApplyClusterManager(ctx, c, &current)
	if err != nil {
		t.Errorf("Don't expected error here")
	}

	current.Spec.AppFrameworkConfig = enterpriseApi.AppFrameworkSpec{
		AppsRepoPollInterval: 60,
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
				Name:      "msos_s2s3_vol",
				Endpoint:  "https://s3-eu-west-2.amazonaws.com",
				Path:      "testbucket-rs-london",
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
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   enterpriseApi.ScopeLocal},
			},
		},
	}
	current.Status.AppContext.AppFrameworkConfig = current.Spec.AppFrameworkConfig
	current.Status.AppContext.Version = 0
	current.Status.AppContext.AppsSrcDeployStatus = make(map[string]enterpriseApi.AppSrcDeployInfo)
	current.Status.AppContext.AppsSrcDeployStatus["key"] = enterpriseApi.AppSrcDeployInfo{
		AppDeploymentInfoList: []enterpriseApi.AppDeploymentInfo{
			{
				AppName: "app1.tgz",
			},
		},
	}
	current.Kind = "ClusterManager"
	_, err = ApplyClusterManager(ctx, c, &current)
	if err == nil {
		t.Errorf("Expected error")
	}

	current.Spec.AppFrameworkConfig.VolList = []enterpriseApi.VolumeSpec{
		{
			Name:      "msos_s2s3_vol",
			Endpoint:  "https://s3-eu-west-2.amazonaws.com",
			Path:      "testbucket-rs-london",
			SecretRef: "s3-secret",
			Type:      "s3",
			Provider:  "aws",
		},
	}
	rerr := errors.New(splcommon.Rerr)
	c.InduceErrorKind[splcommon.MockClientInduceErrorGet] = rerr
	current.Kind = "ClusterManager"
	_, err = ApplyClusterManager(ctx, c, &current)
	if err == nil {
		t.Errorf("Expected error")
	}
}

func TestValidateClusterManagerSpec(t *testing.T) {
	ctx := context.TODO()
	current := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}
	current.Spec.AppFrameworkConfig = enterpriseApi.AppFrameworkSpec{
		AppsRepoPollInterval: 60,
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
				Name:      "msos_s2s3_vol",
				Endpoint:  "https://s3-eu-west-2.amazonaws.com",
				Path:      "testbucket-rs-london",
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
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   enterpriseApi.ScopeLocal},
			},
		},
	}
	current.Status.AppContext.AppFrameworkConfig = enterpriseApi.AppFrameworkSpec{
		AppsRepoPollInterval: 60,
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
				Name:      "msos_s2s3_vol",
				Endpoint:  "https://s3-eu-west-2.amazonaws.com",
				Path:      "testbucket-rs-london",
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
			{Name: "securityApps2",
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
	}
	c := spltest.NewMockClient()
	err := validateClusterManagerSpec(ctx, c, &current)
	if err == nil {
		t.Errorf("Didn't detect incorrect appframework config")
	}
}

func TestGetClusterManagerStatefulSet(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApi.ClusterManager{
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
			if err := validateClusterManagerSpec(ctx, c, &cr); err != nil {
				t.Errorf("validateClusterManagerSpec() returned error: %v", err)
			}
			return getClusterManagerStatefulSet(ctx, c, &cr)
		}
		configTester(t, "getClusterManagerStatefulSet", f, want)
	}
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-manager","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-cluster-manager-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"localhost"},{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent","securityContext":{"capabilities":{"add":["NET_BIND_SERVICE"],"drop":["ALL"]},"privileged":false,"runAsUser":41812,"runAsNonRoot":true,"allowPrivilegeEscalation":false,"seccompProfile":{"type":"RuntimeDefault"}}}],"securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812,"fsGroupChangePolicy":"OnRootMismatch"},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-manager"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-manager-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)

	cr.Spec.LicenseManagerRef.Name = "stack1"
	cr.Spec.LicenseManagerRef.Namespace = "test"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-manager","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-cluster-manager-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"localhost"},{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_LICENSE_MASTER_URL","value":"splunk-stack1-license-manager-service.test.svc.cluster.local"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent","securityContext":{"capabilities":{"add":["NET_BIND_SERVICE"],"drop":["ALL"]},"privileged":false,"runAsUser":41812,"runAsNonRoot":true,"allowPrivilegeEscalation":false,"seccompProfile":{"type":"RuntimeDefault"}}}],"securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812,"fsGroupChangePolicy":"OnRootMismatch"},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-manager"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-manager-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)

	cr.Spec.LicenseManagerRef.Name = ""
	cr.Spec.LicenseURL = "/mnt/splunk.lic"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-manager","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-cluster-manager-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"localhost"},{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_LICENSE_URI","value":"/mnt/splunk.lic"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent","securityContext":{"capabilities":{"add":["NET_BIND_SERVICE"],"drop":["ALL"]},"privileged":false,"runAsUser":41812,"runAsNonRoot":true,"allowPrivilegeEscalation":false,"seccompProfile":{"type":"RuntimeDefault"}}}],"securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812,"fsGroupChangePolicy":"OnRootMismatch"},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-manager"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-manager-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)

	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-manager","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-cluster-manager-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"localhost"},{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/apps/apps.yml,/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_LICENSE_URI","value":"/mnt/splunk.lic"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent","securityContext":{"capabilities":{"add":["NET_BIND_SERVICE"],"drop":["ALL"]},"privileged":false,"runAsUser":41812,"runAsNonRoot":true,"allowPrivilegeEscalation":false,"seccompProfile":{"type":"RuntimeDefault"}}}],"securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812,"fsGroupChangePolicy":"OnRootMismatch"},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-manager"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-manager-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)

	// Create a serviceaccount
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-manager","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-cluster-manager-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"localhost"},{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/apps/apps.yml,/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_LICENSE_URI","value":"/mnt/splunk.lic"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent","securityContext":{"capabilities":{"add":["NET_BIND_SERVICE"],"drop":["ALL"]},"privileged":false,"runAsUser":41812,"runAsNonRoot":true,"allowPrivilegeEscalation":false,"seccompProfile":{"type":"RuntimeDefault"}}}],"serviceAccountName":"defaults","securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812,"fsGroupChangePolicy":"OnRootMismatch"},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-manager"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-manager-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)

	// Add extraEnv
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-manager","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-cluster-manager-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"TEST_ENV_VAR","value":"test_value"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"localhost"},{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/apps/apps.yml,/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_LICENSE_URI","value":"/mnt/splunk.lic"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent","securityContext":{"capabilities":{"add":["NET_BIND_SERVICE"],"drop":["ALL"]},"privileged":false,"runAsUser":41812,"runAsNonRoot":true,"allowPrivilegeEscalation":false,"seccompProfile":{"type":"RuntimeDefault"}}}],"serviceAccountName":"defaults","securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812,"fsGroupChangePolicy":"OnRootMismatch"},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-manager"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-manager-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)

	// Add additional label to cr metadata to transfer to the statefulset
	cr.ObjectMeta.Labels = make(map[string]string)
	cr.ObjectMeta.Labels["app.kubernetes.io/test-extra-label"] = "test-extra-label-value"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-cluster-manager","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer","app.kubernetes.io/test-extra-label":"test-extra-label-value"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer","app.kubernetes.io/test-extra-label":"test-extra-label-value"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-cluster-manager-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"TEST_ENV_VAR","value":"test_value"},{"name":"SPLUNK_CLUSTER_MASTER_URL","value":"localhost"},{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/apps/apps.yml,/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_cluster_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_LICENSE_URI","value":"/mnt/splunk.lic"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent","securityContext":{"capabilities":{"add":["NET_BIND_SERVICE"],"drop":["ALL"]},"privileged":false,"runAsUser":41812,"runAsNonRoot":true,"allowPrivilegeEscalation":false,"seccompProfile":{"type":"RuntimeDefault"}}}],"serviceAccountName":"defaults","securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812,"fsGroupChangePolicy":"OnRootMismatch"},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-cluster-manager"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer","app.kubernetes.io/test-extra-label":"test-extra-label-value"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"indexer","app.kubernetes.io/instance":"splunk-stack1-cluster-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"cluster-manager","app.kubernetes.io/part-of":"splunk-stack1-indexer","app.kubernetes.io/test-extra-label":"test-extra-label-value"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-cluster-manager-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)
}

func TestApplyClusterManagerWithSmartstore(t *testing.T) {
	ctx := context.TODO()
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermanager-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermanager-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermanager-smartstore"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-cluster-manager-stack1-configmap"},
		{MetaName: "*v1.Service-test-splunk-stack1-cluster-manager-service"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-manager"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-cluster-manager-secret-v1"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermanager-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermanager-smartstore"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-manager"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-manager"},
		{MetaName: "*v1.Pod-test-splunk-stack1-cluster-manager-0"},
		{MetaName: "*v1.StatefulSet-test-splunk-test-monitoring-console"},
		{MetaName: "*v4.ClusterManager-test-stack1"},
		{MetaName: "*v4.ClusterManager-test-stack1"},
	}
	updateFuncCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermanager-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermanager-smartstore"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-cluster-manager-stack1-configmap"},
		{MetaName: "*v1.Service-test-splunk-stack1-cluster-manager-service"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-manager"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-cluster-manager-secret-v1"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermanager-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermanager-smartstore"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-manager"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-manager"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-manager"},
		{MetaName: "*v4.ClusterManager-test-stack1"},
		{MetaName: "*v4.ClusterManager-test-stack1"},
	}

	labels := map[string]string{
		"app.kubernetes.io/component":  "versionedSecrets",
		"app.kubernetes.io/managed-by": "splunk-operator",
	}
	listOpts := []runtime.ListOption{
		runtime.InNamespace("test"),
		runtime.MatchingLabels(labels),
	}
	listOpts1 := []runtime.ListOption{
		runtime.InNamespace("test"),
	}
	listmockCall := []spltest.MockFuncCall{
		{ListOpts: listOpts},
		{ListOpts: listOpts1},
	}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[7], funcCalls[8], funcCalls[11], funcCalls[13]}, "List": {listmockCall[0], listmockCall[0], listmockCall[1]}, "Update": {funcCalls[0], funcCalls[3], funcCalls[14]}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": updateFuncCalls, "Update": {funcCalls[9]}, "List": {listmockCall[0]}}

	current := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			SmartStore: enterpriseApi.SmartStoreSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "splunk-test-secret"},
				},

				IndexList: []enterpriseApi.IndexSpec{
					{Name: "salesdata1", RemotePath: "remotepath1",
						IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
							VolName: "msos_s2s3_vol"},
					},
					{Name: "salesdata2", RemotePath: "remotepath2",
						IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
							VolName: "msos_s2s3_vol"},
					},
					{Name: "salesdata3", RemotePath: "remotepath3",
						IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
							VolName: "msos_s2s3_vol"},
					},
				},
			},
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}
	client := spltest.NewMockClient()

	// Mock some functions for unit tests
	addTelApp = func(ctx context.Context, podExecClient splutil.PodExecClientImpl, replicas int32, cr splcommon.MetaObject) error {
		return nil
	}

	// Without S3 keys, ApplyClusterManager should fail
	current.Kind = "ClusterManager"
	_, err := ApplyClusterManager(ctx, client, &current)
	if err == nil {
		t.Errorf("ApplyClusterManager should fail without S3 secrets configured")
	}

	// Create namespace scoped secret
	secret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	secret.Data[s3AccessKey] = []byte("abcdJDckRkxhMEdmSk5FekFRRzBFOXV6bGNldzJSWE9IenhVUy80aa")
	secret.Data[s3SecretKey] = []byte("g4NVp0a29PTzlPdGczWk1vekVUcVBSa0o4NkhBWWMvR1NadDV4YVEy")
	_, err = splctrl.ApplySecret(ctx, client, secret)
	if err != nil {
		t.Errorf(err.Error())
	}

	smartstoreConfigMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-clustermanager-smartstore",
			Namespace: "test",
		},
		Data: map[string]string{"a": "b"},
	}

	revised := current.DeepCopy()
	revised.Spec.Image = "splunk/test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		current.Kind = "ClusterManager"
		_, err := ApplyClusterManager(context.Background(), c, cr.(*enterpriseApi.ClusterManager))
		return err
	}

	client.AddObject(&smartstoreConfigMap)
	ss, _ := getClusterManagerStatefulSet(ctx, client, &current)
	ss.Status.ReadyReplicas = 1

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-cluster-manager-0",
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

	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyClusterManagerWithSmartstore-0", &current, revised, createCalls, updateCalls, reconcile, true, secret, &smartstoreConfigMap, ss, pod)

	current.Status.BundlePushTracker.NeedToPushManagerApps = true
	current.Kind = "ClusterManager"
	if _, err = ApplyClusterManager(context.Background(), client, &current); err != nil {
		t.Errorf("ApplyClusterManager() should not have returned error")
	}

	current.Spec.CommonSplunkSpec.EtcVolumeStorageConfig.StorageCapacity = "-abcd"
	if _, err := ApplyClusterManager(context.Background(), client, &current); err == nil {
		t.Errorf("ApplyClusterManager() should have returned error")
	}

	var replicas int32 = 3
	current.Spec.CommonSplunkSpec.EtcVolumeStorageConfig.StorageCapacity = ""
	ss.Status.ReadyReplicas = 3
	ss.Spec.Replicas = &replicas
	ss.Spec.Template.Spec.Containers[0].Image = "splunk/splunk"
	client.AddObject(ss)
	if result, err := ApplyClusterManager(context.Background(), client, &current); err == nil && !result.Requeue {
		t.Errorf("ApplyClusterManager() should have returned error or result.requeue should have been false")
	}

	ss.Status.ReadyReplicas = 1
	*ss.Spec.Replicas = ss.Status.ReadyReplicas
	objects := []runtime.Object{ss, pod}
	client.AddObjects(objects)
	current.Spec.CommonSplunkSpec.Mock = false

	if _, err := ApplyClusterManager(context.Background(), client, &current); err == nil {
		t.Errorf("ApplyClusterManager() should have returned error")
	}
}

func TestPerformCmBundlePush(t *testing.T) {

	ctx := context.TODO()
	current := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	client := spltest.NewMockClient()

	// When the secret object is not present, should return an error
	current.Status.BundlePushTracker.NeedToPushManagerApps = true
	err := PerformCmBundlePush(ctx, client, &current)
	if err == nil {
		t.Errorf("Should return error, when the secret object is not present")
	}

	secret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	_, err = splctrl.ApplySecret(ctx, client, secret)
	if err != nil {
		t.Errorf(err.Error())
	}

	smartstoreConfigMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-clustermanager-smartstore",
			Namespace: "test",
		},
		Data: map[string]string{configToken: ""},
	}

	_, err = splctrl.ApplyConfigMap(ctx, client, &smartstoreConfigMap)
	if err != nil {
		t.Errorf(err.Error())
	}

	current.Status.BundlePushTracker.NeedToPushManagerApps = true

	//Re-attempting to push the CM bundle in less than 5 seconds should return an error
	current.Status.BundlePushTracker.LastCheckInterval = time.Now().Unix() - 1
	err = PerformCmBundlePush(ctx, client, &current)
	if err == nil {
		t.Errorf("Bundle Push Should fail, if attempted to push within 5 seconds interval")
	}

	//Re-attempting to push the CM bundle after 5 seconds passed, should not return an error
	current.Status.BundlePushTracker.LastCheckInterval = time.Now().Unix() - 10
	err = PerformCmBundlePush(ctx, client, &current)
	if err != nil && strings.HasPrefix(err.Error(), "Will re-attempt to push the bundle after the 5 seconds") {
		t.Errorf("Bundle Push Should not fail if reattempted after 5 seconds interval passed. Error: %s", err.Error())
	}

	// When the CM Bundle push is not pending, should not return an error
	current.Status.BundlePushTracker.NeedToPushManagerApps = false
	err = PerformCmBundlePush(ctx, client, &current)
	if err != nil {
		t.Errorf("Should not return an error when the Bundle push is not required. Error: %s", err.Error())
	}

	// Negative testing
	current.Status.BundlePushTracker.NeedToPushManagerApps = true
	err = PerformCmBundlePush(ctx, client, &current)
	if err != nil && strings.HasPrefix(err.Error(), "Will re-attempt to push the bundle after the 5 seconds") {
		t.Errorf("Bundle Push Should not fail if reattempted after 5 seconds interval passed. Error: %s", err.Error())
	}
}

func TestPushManagerAppsBundle(t *testing.T) {

	ctx := context.TODO()
	current := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	client := spltest.NewMockClient()

	//Without global secret object, should return an error
	err := PushManagerAppsBundle(ctx, client, &current)
	if err == nil {
		t.Errorf("Bundle push should fail, when the secret object is not found")
	}

	secret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	_, err = splctrl.ApplySecret(ctx, client, secret)
	if err != nil {
		t.Errorf(err.Error())
	}

	err = PushManagerAppsBundle(ctx, client, &current)
	if err == nil {
		t.Errorf("Bundle push should fail, when the password is not found")
	}

	//Without password, should return an error
	delete(secret.Data, "password")
	err = PushManagerAppsBundle(ctx, client, &current)
	if err == nil {
		t.Errorf("Bundle push should fail, when the password is not found")
	}
}

func TestAppFrameworkApplyClusterManagerShouldNotFail(t *testing.T) {
	initGlobalResourceTracker()
	ctx := context.TODO()
	cm := enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				AppsRepoPollInterval: 60,
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
		},
	}

	// to pass the validation stage, add the directory to download apps
	err := os.MkdirAll(splcommon.AppDownloadVolume, 0755)
	defer os.RemoveAll(splcommon.AppDownloadVolume)

	if err != nil {
		t.Errorf("Unable to create download directory for apps :%s", splcommon.AppDownloadVolume)
	}

	client := spltest.NewMockClient()

	// Create S3 secret
	s3Secret := spltest.GetMockS3SecretKeys("s3-secret")

	client.AddObject(&s3Secret)

	// Create namespace scoped secret
	_, err = splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	cm.Kind = "ClusterManager"
	_, err = ApplyClusterManager(context.Background(), client, &cm)
	if err != nil {
		t.Errorf("ApplyClusterManager should not have returned error here.")
	}
}

func TestApplyClusterManagerDeletion(t *testing.T) {
	ctx := context.TODO()
	cm := enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
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
				MonitoringConsoleRef: corev1.ObjectReference{
					Name: "mcName",
				},
				Mock: true,
			},
		},
	}

	c := spltest.NewMockClient()

	// Create S3 secret
	s3Secret := spltest.GetMockS3SecretKeys("s3-secret")
	c.AddObject(&s3Secret)
	configmap := spltest.GetMockPerCRConfigMap("splunk-cluster-manager-stack1-configmap")
	c.AddObject(&configmap)

	// Create namespace scoped secret
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	cm.ObjectMeta.DeletionTimestamp = &currentTime
	cm.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}

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
	cm.Kind = "ClusterManager"
	_, err = ApplyClusterManager(ctx, c, &cm)
	if err != nil {
		t.Errorf("ApplyClusterManager should not have returned error here.")
	}
}
func TestClusterManagerGetAppsListForAWSS3ClientShouldNotFail(t *testing.T) {

	ctx := context.TODO()
	cm := enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
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
						Path:      "testbucket-rs-london-2",
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
					{
						Name:     "authenticationApps",
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
		t.Errorf(err.Error())
	}

	splclient.RegisterRemoteDataClient(ctx, "aws")

	Etags := []string{"cc707187b036405f095a8ebb43a782c1", "5055a61b3d1b667a4c3279a381a2e7ae", "19779168370b97d8654424e6c9446dd8"}
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

	appFrameworkRef := cm.Spec.AppFrameworkConfig

	mockAwsHandler.AddObjects(appFrameworkRef, mockAwsObjects...)

	var vol enterpriseApi.VolumeSpec
	var allSuccess bool = true
	for index, appSource := range appFrameworkRef.AppSources {

		vol, err = splclient.GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
		if err != nil {
			allSuccess = false
			continue
		}

		// Update the GetS3Client with our mock call which initializes mock AWS client
		getClientWrapper := splclient.RemoteDataClientsMap[vol.Provider]
		getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, splclient.NewMockAWSS3Client)

		remoteDataClientMgr := &RemoteDataClientManager{
			client:          client,
			cr:              &cm,
			appFrameworkRef: &cm.Spec.AppFrameworkConfig,
			vol:             &vol,
			location:        appSource.Location,
			initFn: func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
				cl := spltest.MockAWSS3Client{}
				cl.Objects = mockAwsObjects[index].Objects
				return cl
			},
			getRemoteDataClient: func(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject,
				appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec,
				location string, fn splclient.GetInitFunc) (splclient.SplunkRemoteDataClient, error) {
				// Get the mock client
				c, err := GetRemoteStorageClient(ctx, client, cr, appFrameworkRef, vol, location, fn)
				return c, err
			},
		}

		s3Response, err := remoteDataClientMgr.GetAppsList(ctx)
		if err != nil {
			allSuccess = false
			continue
		}

		var mockResponse spltest.MockRemoteDataClient
		mockResponse, err = splclient.ConvertRemoteDataListResponse(ctx, s3Response)
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

func TestClusterManagerGetAppsListForAWSS3ClientShouldFail(t *testing.T) {

	ctx := context.TODO()
	cm := enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
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
		t.Errorf(err.Error())
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

	appFrameworkRef := cm.Spec.AppFrameworkConfig

	mockAwsHandler.AddObjects(appFrameworkRef, mockAwsObjects...)

	var vol enterpriseApi.VolumeSpec

	appSource := appFrameworkRef.AppSources[0]
	vol, err = splclient.GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
	if err != nil {
		t.Errorf("Unable to get Volume due to error=%s", err)
	}

	// Update the GetS3Client with our mock call which initializes mock AWS client
	getClientWrapper := splclient.RemoteDataClientsMap[vol.Provider]
	getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, splclient.NewMockAWSS3Client)

	remoteDataClientMgr := &RemoteDataClientManager{
		client:          client,
		cr:              &cm,
		appFrameworkRef: &cm.Spec.AppFrameworkConfig,
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

	s3Resp, err := remoteDataClientMgr.GetAppsList(ctx)
	if err != nil {
		t.Errorf("GetAppsList should not have returned error since empty appSources are allowed.")
	}
	if len(s3Resp.Objects) != 0 {
		t.Errorf("GetAppsList should return an empty response since we have empty objects in MockAWSS3Client")
	}
}

func TestGetClusterManagerList(t *testing.T) {
	ctx := context.TODO()
	cm := enterpriseApi.ClusterManager{}

	listOpts := []runtime.ListOption{
		runtime.InNamespace("test"),
	}

	client := spltest.NewMockClient()

	var numOfObjects int
	// Invalid scenario since we haven't added clustermanager to the list yet
	_, err := getClusterManagerList(ctx, client, &cm, listOpts)
	if err == nil {
		t.Errorf("getNumOfObjects should have returned error as we haven't added cluster manager to the list yet")
	}

	cmList := &enterpriseApi.ClusterManagerList{}
	cmList.Items = append(cmList.Items, cm)

	client.ListObj = cmList

	numOfObjects, err = getClusterManagerList(ctx, client, &cm, listOpts)
	if err != nil {
		t.Errorf("getNumOfObjects should not have returned error=%v", err)
	}

	if numOfObjects != 1 {
		t.Errorf("Got wrong number of ClusterManager objects. Expected=%d, Got=%d", 1, numOfObjects)
	}
}

func TestCheckIfsmartstoreConfigMapUpdatedToPod(t *testing.T) {
	ctx := context.TODO()
	cm := enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "clustermanager",
		},
	}

	c := spltest.NewMockClient()
	podExecCommands := []string{
		"cat /mnt/splunk-operator/local/",
	}

	smartstoreConfigMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-clustermanager-smartstore",
			Namespace: "test",
		},
		Data: map[string]string{"a": "b"},
	}

	mockPodExecReturnContexts := []*spltest.MockPodExecReturnContext{
		{
			StdOut: "",
			StdErr: "",
			Err:    fmt.Errorf("dummy error"),
		},
	}

	var mockPodExecClient *spltest.MockPodExecClient = &spltest.MockPodExecClient{}
	mockPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnContexts...)

	err := CheckIfsmartstoreConfigMapUpdatedToPod(ctx, c, &cm, mockPodExecClient)
	if err == nil {
		t.Errorf("CheckIfsmartstoreConfigMapUpdatedToPod should have returned error")
	}

	mockPodExecReturnContexts[0].Err = nil
	err = CheckIfsmartstoreConfigMapUpdatedToPod(ctx, c, &cm, mockPodExecClient)
	if err == nil {
		t.Errorf("CheckIfsmartstoreConfigMapUpdatedToPod should have returned error since we did not add configMap yet.")
	}

	c.AddObject(&smartstoreConfigMap)
	err = CheckIfsmartstoreConfigMapUpdatedToPod(ctx, c, &cm, mockPodExecClient)
	if err != nil {
		t.Errorf("CheckIfsmartstoreConfigMapUpdatedToPod should not have returned error; err=%v", err)
	}

	mockPodExecClient.CheckPodExecCommands(t, "CheckIfsmartstoreConfigMapUpdatedToPod")
}

func TestIsClusterManagerReadyForUpgrade(t *testing.T) {
	ctx := context.TODO()

	builder := fake.NewClientBuilder()
	client := builder.Build()
	utilruntime.Must(enterpriseApi.AddToScheme(clientgoscheme.Scheme))

	// Create License Manager
	lm := enterpriseApi.LicenseManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: enterpriseApi.LicenseManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
					Image:           "splunk/splunk:latest",
				},
				Volumes: []corev1.Volume{},
				ClusterManagerRef: corev1.ObjectReference{
					Name: "test",
				},
			},
		},
	}

	client.Create(ctx, &lm)
	_, err := ApplyLicenseManager(ctx, client, &lm)
	if err != nil {
		t.Errorf("applyLicenseManager should not have returned error; err=%v", err)
	}
	namespacedName := types.NamespacedName{
		Name:      "test",
		Namespace: "test",
	}
	err = client.Get(ctx, namespacedName, &lm)
	if err != nil {
		t.Errorf("get should not have returned error; err=%v", err)
	}
	lm.Status.Phase = enterpriseApi.PhaseReady
	err = client.Status().Update(ctx, &lm)
	if err != nil {
		t.Errorf("Unexpected status update  %v", err)
		debug.PrintStack()
	}

	// Create Cluster Manager
	cm := enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
					Image:           "splunk/splunk:latest",
				},
				Volumes: []corev1.Volume{},
				LicenseManagerRef: corev1.ObjectReference{
					Name: "test",
				},
			},
		},
	}

	cm.Kind = "ClusterManager"
	client.Create(ctx, &cm)
	_, err = ApplyClusterManager(ctx, client, &cm)
	if err != nil {
		t.Errorf("applyClusterManager should not have returned error; err=%v", err)
	}

	// create pods for license manager
	lm.Status.TelAppInstalled = true
	lm.Spec.Image = "splunk2"
	createPods(t, ctx, client, "license-manager", fmt.Sprintf("splunk-%s-license-manager-0", lm.Name), lm.Namespace, lm.Spec.Image)
	updateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-license-manager", lm.Name), lm.Namespace)
	// now the statefulset image in spec is updated to splunk2
	ApplyLicenseManager(ctx, client, &lm)

	// now the statefulset and license manager both should be in ready state
	ApplyLicenseManager(ctx, client, &lm)

	clusterManager := &enterpriseApi.ClusterManager{}
	namespacedName = types.NamespacedName{
		Name:      cm.Name,
		Namespace: cm.Namespace,
	}
	err = client.Get(ctx, namespacedName, clusterManager)
	if err != nil {
		t.Errorf("changeClusterManagerAnnotations should not have returned error=%v", err)
	}
	clusterManager.Spec.Image = "splunk2"
	err = client.Update(ctx, clusterManager)
	if err != nil {
		t.Errorf("update should not have returned error; err=%v", err)
	}

	check, err := UpgradePathValidation(ctx, client, clusterManager, clusterManager.Spec.CommonSplunkSpec, nil)

	if err != nil {
		t.Errorf("Unexpected upgradeScenario error %v", err)
	}

	if !check {
		t.Errorf("isClusterManagerReadyForUpgrade: CM should be ready for upgrade")
	}
}

func TestChangeClusterManagerAnnotations(t *testing.T) {
	ctx := context.TODO()

	// define LM and CM
	lm := &enterpriseApi.LicenseManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-lm",
			Namespace: "test",
		},
		Spec: enterpriseApi.LicenseManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					Image:           "splunk/splunk:latest",
					ImagePullPolicy: "Always",
				},
				Volumes: []corev1.Volume{},
			},
		},
	}

	cm := &enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					Image:           "splunk/splunk:latest",
					ImagePullPolicy: "Always",
				},
				Volumes: []corev1.Volume{},
				LicenseManagerRef: corev1.ObjectReference{
					Name: "test-lm",
				},
			},
		},
	}
	lm.Spec.Image = "splunk/splunk:latest"

	builder := fake.NewClientBuilder()
	client := builder.Build()
	utilruntime.Must(enterpriseApi.AddToScheme(clientgoscheme.Scheme))

	// Create the instances
	client.Create(ctx, lm)
	_, err := ApplyLicenseManager(ctx, client, lm)
	if err != nil {
		t.Errorf("applyLicenseManager should not have returned error; err=%v", err)
	}

	namespacedName := types.NamespacedName{
		Name:      lm.Name,
		Namespace: lm.Namespace,
	}
	err = client.Get(ctx, namespacedName, lm)
	if err != nil {
		t.Errorf("changeLicenseManagerAnnotations should not have returned error=%v", err)
	}

	// create pods for license manager
	createPods(t, ctx, client, "license-manager", fmt.Sprintf("splunk-%s-license-manager-0", lm.Name), lm.Namespace, lm.Spec.Image)
	updateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-license-manager", lm.Name), lm.Namespace)
	lm.Status.TelAppInstalled = true
	// create license manager statefulset
	_, err = ApplyLicenseManager(ctx, client, lm)
	if err != nil {
		t.Errorf("ApplyLicenseManager should not have returned error; err=%v", err)
	}

	err = client.Get(ctx, namespacedName, lm)
	if err != nil {
		t.Errorf("changeLicenseManagerAnnotations should not have returned error=%v", err)
	}

	lm.Status.Phase = enterpriseApi.PhaseReady
	err = client.Status().Update(ctx, lm)
	if err != nil {
		t.Errorf("Unexpected update pod  %v", err)
		debug.PrintStack()
	}

	VerifyCMisMultisiteCall = func(ctx context.Context, cr *enterpriseApi.ClusterManager, namespaceScopedSecret *corev1.Secret) ([]corev1.EnvVar, error) {
		extraEnv := getClusterManagerExtraEnv(cr, &cr.Spec.CommonSplunkSpec)
		return extraEnv, err
	}

	cm.Kind = "ClusterManager"
	client.Create(ctx, cm)
	_, err = ApplyClusterManager(ctx, client, cm)
	if err != nil {
		t.Errorf("applyClusterManager should not have returned error; err=%v", err)
	}

	err = changeClusterManagerAnnotations(ctx, client, lm)
	if err != nil {
		t.Errorf("changeClusterManagerAnnotations should not have returned error=%v", err)
	}
	clusterManager := &enterpriseApi.ClusterManager{}
	namespacedName = types.NamespacedName{
		Name:      cm.Name,
		Namespace: cm.Namespace,
	}
	err = client.Get(ctx, namespacedName, clusterManager)
	if err != nil {
		t.Errorf("changeClusterManagerAnnotations should not have returned error=%v", err)
	}

	annotations := clusterManager.GetAnnotations()
	if annotations["splunk/image-tag"] != lm.Spec.Image {
		t.Errorf("changeClusterManagerAnnotations should have set the checkUpdateImage annotation field to the current image")
	}
}

func TestClusterManagerWitReadyState(t *testing.T) {
	// create directory for app framework
	newpath := filepath.Join("/tmp", "appframework")
	_ = os.MkdirAll(newpath, os.ModePerm)

	// adding getapplist to fix test case
	GetAppsList = func(ctx context.Context, remoteDataClientMgr RemoteDataClientManager) (splclient.RemoteDataListResponse, error) {
		remoteDataListResponse := splclient.RemoteDataListResponse{}
		return remoteDataListResponse, nil
	}

	builder := fake.NewClientBuilder()
	c := builder.Build()
	utilruntime.Must(enterpriseApi.AddToScheme(clientgoscheme.Scheme))
	ctx := context.TODO()

	// Create App framework volume
	volumeSpec := []enterpriseApi.VolumeSpec{
		{
			Name:      "testing",
			Endpoint:  "/someendpoint",
			Path:      "s3-test",
			SecretRef: "secretRef",
			Provider:  "aws",
			Type:      "s3",
			Region:    "west",
		},
	}

	// AppSourceDefaultSpec: Remote Storage volume name and Scope of App deployment
	appSourceDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
		VolName: "testing",
		Scope:   "local",
	}

	// appSourceSpec: App source name, location and volume name and scope from appSourceDefaultSpec
	appSourceSpec := []enterpriseApi.AppSourceSpec{
		{
			Name:                 "appSourceName",
			Location:             "appSourceLocation",
			AppSourceDefaultSpec: appSourceDefaultSpec,
		},
	}

	// appFrameworkSpec: AppSource settings, Poll Interval, volumes, appSources on volumes
	appFrameworkSpec := enterpriseApi.AppFrameworkSpec{
		Defaults:             appSourceDefaultSpec,
		AppsRepoPollInterval: int64(60),
		VolList:              volumeSpec,
		AppSources:           appSourceSpec,
	}

	// create clustermanager custom resource
	clustermanager := &enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				Volumes: []corev1.Volume{},
				MonitoringConsoleRef: corev1.ObjectReference{
					Name: "mcName",
				},
			},
			AppFrameworkConfig: appFrameworkSpec,
		},
	}

	replicas := int32(1)
	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-cluster-manager",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: "splunk-test-cluster-manager-headless",
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
			Name:      "splunk-test-cluster-manager-headless",
			Namespace: "default",
		},
	}

	// simulate service
	c.Create(ctx, service)

	// simulate create stateful set
	c.Create(ctx, statefulset)

	clustermanager.Kind = "ClusterManager"
	// simulate create clustermanager instance before reconcilation
	c.Create(ctx, clustermanager)

	_, err := ApplyClusterManager(ctx, c, clustermanager)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for clustermanager with app framework  %v", err)
		debug.PrintStack()
	}
	namespacedName := types.NamespacedName{
		Name:      clustermanager.Name,
		Namespace: clustermanager.Namespace,
	}

	// cluster manager
	err = c.Get(ctx, namespacedName, clustermanager)
	if err != nil {
		t.Errorf("get should not have returned error; err=%v", err)
	}

	// simulate Ready state
	clustermanager.Status.Phase = enterpriseApi.PhaseReady
	clustermanager.Spec.ServiceTemplate.Annotations = map[string]string{
		"traffic.sidecar.istio.io/excludeOutboundPorts": "8089,8191,9997",
		"traffic.sidecar.istio.io/includeInboundPorts":  "8000,8088",
	}
	clustermanager.Spec.ServiceTemplate.Labels = map[string]string{
		"app.kubernetes.io/instance":   "splunk-test-cluster-manager",
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "cluster-manager",
		"app.kubernetes.io/name":       "cluster-manager",
		"app.kubernetes.io/part-of":    "splunk-test-cluster-manager",
	}
	err = c.Status().Update(ctx, clustermanager)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for cluster manager with app framework  %v", err)
		debug.PrintStack()
	}

	err = c.Get(ctx, namespacedName, clustermanager)
	if err != nil {
		t.Errorf("Unexpected get cluster manager %v", err)
		debug.PrintStack()
	}

	// call reconciliation
	clustermanager.Kind = "ClusterManager"
	_, err = ApplyClusterManager(ctx, c, clustermanager)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for cluster manager with app framework  %v", err)
		debug.PrintStack()
	}

	// create pod
	stpod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-cluster-manager-0",
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
		Name:      "splunk-test-cluster-manager",
		Namespace: "default",
	}
	err = c.Get(ctx, stNamespacedName, statefulset)
	if err != nil {
		t.Errorf("Unexpected get cluster manager %v", err)
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

	err = c.Get(ctx, namespacedName, clustermanager)
	if err != nil {
		t.Errorf("Unexpected get cluster manager %v", err)
		debug.PrintStack()
	}

	//create namespace MC statefulset
	current := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-default-monitoring-console",
			Namespace: "default",
		},
	}
	namespacedName = types.NamespacedName{Namespace: "default", Name: "splunk-default-monitoring-console"}

	// Create MC statefulset
	err = splutil.CreateResource(ctx, c, &current)
	if err != nil {
		t.Errorf("Failed to create owner reference  %s", current.GetName())
	}

	//setownerReference
	err = splctrl.SetStatefulSetOwnerRef(ctx, c, clustermanager, namespacedName)
	if err != nil {
		t.Errorf("Couldn't set owner ref for resource %s", current.GetName())
	}

	err = c.Get(ctx, namespacedName, &current)
	if err != nil {
		t.Errorf("Couldn't get the statefulset resource %s", current.GetName())
	}

	configmap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-default-monitoring-console",
			Namespace: "default",
		},
	}

	// Create configmap
	err = splutil.CreateResource(ctx, c, &configmap)
	if err != nil {
		t.Errorf("Failed to create resource  %s", current.GetName())
	}

	// Mock the addTelApp function for unit tests
	addTelApp = func(ctx context.Context, podExecClient splutil.PodExecClientImpl, replicas int32, cr splcommon.MetaObject) error {
		return nil
	}

	// call reconciliation
	clustermanager.Kind = "ClusterManager"
	_, err = ApplyClusterManager(ctx, c, clustermanager)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for cluster manager with app framework  %v", err)
		debug.PrintStack()
	}
}
