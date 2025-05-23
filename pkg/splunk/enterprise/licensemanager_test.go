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
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"
	"time"

	"github.com/pkg/errors"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

func TestApplyLicenseManager(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-license-manager-stack1-configmap"},
		{MetaName: "*v1.Service-test-splunk-stack1-license-manager-service"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-license-manager"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-license-manager-secret-v1"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-license-manager"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-license-manager"},
		{MetaName: "*v4.LicenseManager-test-stack1"},
		{MetaName: "*v4.LicenseManager-test-stack1"},
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
		{ListOpts: listOpts},
	}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0], funcCalls[3], funcCalls[4], funcCalls[6], funcCalls[9], funcCalls[10]}, "Update": {funcCalls[0]}, "List": {listmockCall[0]}}
	updateFuncCalls := []spltest.MockFuncCall{funcCalls[0], funcCalls[1], funcCalls[3], funcCalls[4], funcCalls[5], funcCalls[6], funcCalls[8], funcCalls[9], funcCalls[10], funcCalls[11], funcCalls[10], funcCalls[12], funcCalls[12]}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": updateFuncCalls, "Update": {funcCalls[5]}, "List": {listmockCall[0]}}
	current := enterpriseApi.LicenseManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "LicenseManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	revised := current.DeepCopy()
	revised.Spec.Image = "splunk/test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyLicenseManager(context.Background(), c, cr.(*enterpriseApi.LicenseManager))
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyLicenseManager", &current, revised, createCalls, updateCalls, reconcile, true)

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
		_, err := ApplyLicenseManager(context.Background(), c, cr.(*enterpriseApi.LicenseManager))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)

	// Negative testing
	c := spltest.NewMockClient()
	ctx := context.TODO()
	current.Spec.LivenessInitialDelaySeconds = -1
	_, err := ApplyLicenseManager(ctx, c, &current)
	if err == nil {
		t.Errorf("Expected error")
	}

	rerr := errors.New(splcommon.Rerr)
	current.Spec.LivenessInitialDelaySeconds = 5
	c.InduceErrorKind[splcommon.MockClientInduceErrorGet] = rerr
	_, err = ApplyLicenseManager(ctx, c, &current)
	if err == nil {
		t.Errorf("Expected error")
	}

	c.InduceErrorKind[splcommon.MockClientInduceErrorGet] = nil
	nsSec, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Errorf("Creating secret shouldn't fail")
	}
	c.Create(ctx, nsSec)
	c.InduceErrorKind[splcommon.MockClientInduceErrorCreate] = rerr
	_, err = ApplyLicenseManager(ctx, c, &current)
	if err == nil {
		t.Errorf("Expected error")
	}
}

func TestGetLicenseManagerStatefulSet(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApi.LicenseManager{
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
			if err := validateLicenseManagerSpec(ctx, c, &cr); err != nil {
				t.Errorf("validateLicenseManagerSpec() returned error: %v", err)
			}
			return getLicenseManagerStatefulSet(ctx, c, &cr)
		}
		configTester(t, "getLicenseManagerStatefulSet()", f, want)
	}

	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-license-manager","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-license-manager-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_license_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent","securityContext":{"capabilities":{"add":["NET_BIND_SERVICE"],"drop":["ALL"]},"privileged":false,"runAsUser":41812,"runAsNonRoot":true,"allowPrivilegeEscalation":false,"seccompProfile":{"type":"RuntimeDefault"}}}],"securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812,"fsGroupChangePolicy":"OnRootMismatch"},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-license-manager"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-license-manager-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)

	cr.Spec.LicenseURL = "/mnt/splunk.lic"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-license-manager","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-license-manager-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_license_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_LICENSE_URI","value":"/mnt/splunk.lic"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent","securityContext":{"capabilities":{"add":["NET_BIND_SERVICE"],"drop":["ALL"]},"privileged":false,"runAsUser":41812,"runAsNonRoot":true,"allowPrivilegeEscalation":false,"seccompProfile":{"type":"RuntimeDefault"}}}],"securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812,"fsGroupChangePolicy":"OnRootMismatch"},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-license-manager"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-license-manager-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)

	// Allow installing apps via DefaultsURLApps for Licence Manager
	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-license-manager","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-license-manager-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/apps/apps.yml,/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_license_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_LICENSE_URI","value":"/mnt/splunk.lic"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent","securityContext":{"capabilities":{"add":["NET_BIND_SERVICE"],"drop":["ALL"]},"privileged":false,"runAsUser":41812,"runAsNonRoot":true,"allowPrivilegeEscalation":false,"seccompProfile":{"type":"RuntimeDefault"}}}],"securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812,"fsGroupChangePolicy":"OnRootMismatch"},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-license-manager"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-license-manager-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)

	// Create a serviceaccount
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-license-manager","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-license-manager-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/apps/apps.yml,/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_license_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_LICENSE_URI","value":"/mnt/splunk.lic"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent","securityContext":{"capabilities":{"add":["NET_BIND_SERVICE"],"drop":["ALL"]},"privileged":false,"runAsUser":41812,"runAsNonRoot":true,"allowPrivilegeEscalation":false,"seccompProfile":{"type":"RuntimeDefault"}}}],"serviceAccountName":"defaults","securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812,"fsGroupChangePolicy":"OnRootMismatch"},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-license-manager"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-license-manager-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)

	// Add extraEnv
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-license-manager","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-license-manager-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"TEST_ENV_VAR","value":"test_value"},{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/apps/apps.yml,/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_license_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_LICENSE_URI","value":"/mnt/splunk.lic"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent","securityContext":{"capabilities":{"add":["NET_BIND_SERVICE"],"drop":["ALL"]},"privileged":false,"runAsUser":41812,"runAsNonRoot":true,"allowPrivilegeEscalation":false,"seccompProfile":{"type":"RuntimeDefault"}}}],"serviceAccountName":"defaults","securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812,"fsGroupChangePolicy":"OnRootMismatch"},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-license-manager"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-license-manager-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)

	// Add additional label to cr metadata to transfer to the statefulset
	cr.ObjectMeta.Labels = make(map[string]string)
	cr.ObjectMeta.Labels["app.kubernetes.io/test-extra-label"] = "test-extra-label-value"
	test(`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"splunk-stack1-license-manager","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager","app.kubernetes.io/test-extra-label":"test-extra-label-value"},"ownerReferences":[{"apiVersion":"","kind":"","name":"stack1","uid":"","controller":true}]},"spec":{"replicas":1,"selector":{"matchLabels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager","app.kubernetes.io/test-extra-label":"test-extra-label-value"},"annotations":{"traffic.sidecar.istio.io/excludeOutboundPorts":"8089,8191,9997","traffic.sidecar.istio.io/includeInboundPorts":"8000"}},"spec":{"volumes":[{"name":"splunk-test-probe-configmap","configMap":{"name":"splunk-test-probe-configmap","defaultMode":365}},{"name":"mnt-splunk-secrets","secret":{"secretName":"splunk-stack1-license-manager-secret-v1","defaultMode":420}}],"containers":[{"name":"splunk","image":"splunk/splunk","ports":[{"name":"http-splunkweb","containerPort":8000,"protocol":"TCP"},{"name":"https-splunkd","containerPort":8089,"protocol":"TCP"}],"env":[{"name":"TEST_ENV_VAR","value":"test_value"},{"name":"SPLUNK_HOME","value":"/opt/splunk"},{"name":"SPLUNK_START_ARGS","value":"--accept-license"},{"name":"SPLUNK_DEFAULTS_URL","value":"/mnt/apps/apps.yml,/mnt/splunk-secrets/default.yml"},{"name":"SPLUNK_HOME_OWNERSHIP_ENFORCEMENT","value":"false"},{"name":"SPLUNK_ROLE","value":"splunk_license_master"},{"name":"SPLUNK_DECLARATIVE_ADMIN_PASSWORD","value":"true"},{"name":"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH","value":"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"},{"name":"SPLUNK_LICENSE_URI","value":"/mnt/splunk.lic"}],"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"100m","memory":"512Mi"}},"volumeMounts":[{"name":"pvc-etc","mountPath":"/opt/splunk/etc"},{"name":"pvc-var","mountPath":"/opt/splunk/var"},{"name":"splunk-test-probe-configmap","mountPath":"/mnt/probes"},{"name":"mnt-splunk-secrets","mountPath":"/mnt/splunk-secrets"}],"livenessProbe":{"exec":{"command":["/mnt/probes/livenessProbe.sh"]},"initialDelaySeconds":30,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":3},"readinessProbe":{"exec":{"command":["/mnt/probes/readinessProbe.sh"]},"initialDelaySeconds":10,"timeoutSeconds":5,"periodSeconds":5,"failureThreshold":3},"startupProbe":{"exec":{"command":["/mnt/probes/startupProbe.sh"]},"initialDelaySeconds":40,"timeoutSeconds":30,"periodSeconds":30,"failureThreshold":12},"imagePullPolicy":"IfNotPresent","securityContext":{"capabilities":{"add":["NET_BIND_SERVICE"],"drop":["ALL"]},"privileged":false,"runAsUser":41812,"runAsNonRoot":true,"allowPrivilegeEscalation":false,"seccompProfile":{"type":"RuntimeDefault"}}}],"serviceAccountName":"defaults","securityContext":{"runAsUser":41812,"runAsNonRoot":true,"fsGroup":41812,"fsGroupChangePolicy":"OnRootMismatch"},"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{"labelSelector":{"matchExpressions":[{"key":"app.kubernetes.io/instance","operator":"In","values":["splunk-stack1-license-manager"]}]},"topologyKey":"kubernetes.io/hostname"}}]}},"schedulerName":"default-scheduler"}},"volumeClaimTemplates":[{"metadata":{"name":"pvc-etc","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager","app.kubernetes.io/test-extra-label":"test-extra-label-value"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},"status":{}},{"metadata":{"name":"pvc-var","namespace":"test","creationTimestamp":null,"labels":{"app.kubernetes.io/component":"license-manager","app.kubernetes.io/instance":"splunk-stack1-license-manager","app.kubernetes.io/managed-by":"splunk-operator","app.kubernetes.io/name":"license-manager","app.kubernetes.io/part-of":"splunk-stack1-license-manager","app.kubernetes.io/test-extra-label":"test-extra-label-value"}},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"100Gi"}}},"status":{}}],"serviceName":"splunk-stack1-license-manager-headless","podManagementPolicy":"Parallel","updateStrategy":{"type":"OnDelete"}},"status":{"replicas":0,"availableReplicas":0}}`)
}

func TestAppFrameworkApplyLicenseManagerShouldNotFail(t *testing.T) {

	ctx := context.TODO()
	cr := enterpriseApi.LicenseManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "LicenseManager",
		},
		Spec: enterpriseApi.LicenseManagerSpec{
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret", Type: "s3", Provider: "aws"},
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

	client := spltest.NewMockClient()

	// Create namespace scoped secret
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	// Create S3 secret
	s3Secret := spltest.GetMockS3SecretKeys("s3-secret")
	client.AddObject(&s3Secret)
	configmap := spltest.GetMockPerCRConfigMap("splunk-license-manager-stack1-configmap")
	client.AddObject(&configmap)

	// to pass the validation stage, add the directory to download apps
	err = os.MkdirAll(splcommon.AppDownloadVolume, 0755)
	defer os.RemoveAll(splcommon.AppDownloadVolume)

	if err != nil {
		t.Errorf("Unable to create download directory for apps :%s", splcommon.AppDownloadVolume)
	}

	_, err = ApplyLicenseManager(ctx, client, &cr)

	if err != nil {
		t.Errorf("ApplyLicenseManager should be successful")
	}
}

func TestLicensemanagerGetAppsListForAWSS3ClientShouldNotFail(t *testing.T) {

	ctx := context.TODO()
	cr := enterpriseApi.LicenseManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.LicenseManagerSpec{
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
						Path:      "testbucket-rs-london2",
						SecretRef: "s3-secret",
						Type:      "s3",
						Provider:  "aws",
					},
				},
				AppSources: []enterpriseApi.AppSourceSpec{
					{
						Name:     "adminApps",
						Location: "adminAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal,
						},
					},
					{
						Name:     "securityApps",
						Location: "securityAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "msos_s2s3_vol",
							Scope:   enterpriseApi.ScopeLocal,
						},
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

	Etags := []string{"cc707187b036405f095a8ebb43a782c1", "5055a61b3d1b667a4c3279a381a2e7ae", "19779168370b97d8654424e6c9446dd9"}
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

	appFrameworkRef := cr.Spec.AppFrameworkConfig

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

		s3ClientMgr := &RemoteDataClientManager{client: client,
			cr: &cr, appFrameworkRef: &cr.Spec.AppFrameworkConfig,
			vol:      &vol,
			location: appSource.Location,
			initFn: func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
				cl := spltest.MockAWSS3Client{}
				cl.Objects = mockAwsObjects[index].Objects
				return cl
			},
			getRemoteDataClient: func(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec, location string, fn splclient.GetInitFunc) (splclient.SplunkRemoteDataClient, error) {
				c, err := GetRemoteStorageClient(ctx, client, cr, appFrameworkRef, vol, location, fn)
				return c, err
			},
		}

		s3Response, err := s3ClientMgr.GetAppsList(ctx)
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

func TestLicenseManagerGetAppsListForAWSS3ClientShouldFail(t *testing.T) {

	ctx := context.TODO()
	lm := enterpriseApi.LicenseManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.LicenseManagerSpec{
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

	appFrameworkRef := lm.Spec.AppFrameworkConfig

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

	s3ClientMgr := &RemoteDataClientManager{
		client:          client,
		cr:              &lm,
		appFrameworkRef: &lm.Spec.AppFrameworkConfig,
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

	_, err = s3ClientMgr.GetAppsList(ctx)
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

	_, err = s3ClientMgr.GetAppsList(ctx)
	if err == nil {
		t.Errorf("GetAppsList should have returned error as S3 secret has empty keys")
	}

	s3AccessKey := []byte{'1'}
	s3Secret.Data = map[string][]byte{"s3_access_key": s3AccessKey}
	_, err = s3ClientMgr.GetAppsList(ctx)
	if err == nil {
		t.Errorf("GetAppsList should have returned error as S3 secret has empty s3_secret_key")
	}

	s3SecretKey := []byte{'2'}
	s3Secret.Data = map[string][]byte{"s3_secret_key": s3SecretKey}
	_, err = s3ClientMgr.GetAppsList(ctx)
	if err == nil {
		t.Errorf("GetAppsList should have returned error as S3 secret has empty s3_access_key")
	}

	// Create S3 secret
	s3Secret = spltest.GetMockS3SecretKeys("s3-secret")

	// This should return an error as we have initialized initFn for s3ClientMgr
	// to return a nil client.
	_, err = s3ClientMgr.GetAppsList(ctx)
	if err == nil {
		t.Errorf("GetAppsList should have returned error as we could not get the S3 client")
	}

	s3ClientMgr.initFn = func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
		// To test the error scenario, do no set the Objects member yet
		cl := spltest.MockAWSS3Client{}
		return cl
	}

	s3Resp, err := s3ClientMgr.GetAppsList(ctx)
	if err != nil {
		t.Errorf("GetAppsList should not have returned error since empty appSources are allowed.")
	}
	if len(s3Resp.Objects) != 0 {
		t.Errorf("GetAppsList should return an empty response since we have empty objects in MockAWSS3Client")
	}
}

func TestApplyLicenseManagerDeletion(t *testing.T) {
	ctx := context.TODO()
	lm := enterpriseApi.LicenseManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "LicenseManager",
		},
		Spec: enterpriseApi.LicenseManagerSpec{
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
	configmap := spltest.GetMockPerCRConfigMap("splunk-license-manager-stack1-configmap")
	c.AddObject(&configmap)

	// Create namespace scoped secret
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	lm.ObjectMeta.DeletionTimestamp = &currentTime
	lm.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}

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

	_, err = ApplyLicenseManager(ctx, c, &lm)
	if err != nil {
		t.Errorf("ApplyLicenseManager should not have returned error here.")
	}
}

func TestLicenseManagerList(t *testing.T) {
	ctx := context.TODO()
	lm := enterpriseApi.LicenseManager{}

	listOpts := []client.ListOption{
		client.InNamespace("test"),
	}

	client := spltest.NewMockClient()

	var numOfObjects int
	// Invalid scenario since we haven't added license master to the list yet
	_, err := getLicenseManagerList(ctx, client, &lm, listOpts)
	if err == nil {
		t.Errorf("getNumOfObjects should have returned error as we haven't added standalone to the list yet")
	}

	lmList := &enterpriseApi.LicenseManagerList{}
	lmList.Items = append(lmList.Items, lm)

	client.ListObj = lmList

	objList, err := getLicenseManagerList(ctx, client, &lm, listOpts)
	if err != nil {
		t.Errorf("getNumOfObjects should not have returned error=%v", err)
	}

	numOfObjects = len(objList.Items)
	if numOfObjects != 1 {
		t.Errorf("Got wrong number of LicenseManager objects. Expected=%d, Got=%d", 1, numOfObjects)
	}
}

func TestLicenseManagerWithReadyState(t *testing.T) {

	mclient := &spltest.MockHTTPClient{}
	type Entry1 struct {
		Content splclient.ClusterManagerInfo `json:"content"`
	}

	apiResponse1 := struct {
		Entry []Entry1 `json:"entry"`
	}{
		Entry: []Entry1{
			{
				Content: splclient.ClusterManagerInfo{
					Initialized:     true,
					IndexingReady:   true,
					ServiceReady:    true,
					MaintenanceMode: true,
				},
			},
			{
				Content: splclient.ClusterManagerInfo{
					Initialized:     true,
					IndexingReady:   true,
					ServiceReady:    true,
					MaintenanceMode: true,
				},
			},
		},
	}

	type Entry struct {
		Name    string                           `json:"name"`
		Content splclient.ClusterManagerPeerInfo `json:"content"`
	}

	apiResponse2 := struct {
		Entry []Entry `json:"entry"`
	}{
		Entry: []Entry{
			{
				Name: "testing",
				Content: splclient.ClusterManagerPeerInfo{
					ID:             "testing",
					Status:         "Up",
					ActiveBundleID: "testing",
					BucketCount:    2,
					Searchable:     true,
					Label:          "splunk-test-indexer-0",
				},
			},
		},
	}

	response1, _ := json.Marshal(apiResponse1)
	response2, _ := json.Marshal(apiResponse2)
	wantRequest1, _ := http.NewRequest("GET", "https://splunk-test-cluster-manager-service.default.svc.cluster.local:8089/services/cluster/manager/info?count=0&output_mode=json", nil)
	wantRequest2, _ := http.NewRequest("GET", "https://splunk-test-cluster-manager-service.default.svc.cluster.local:8089/services/cluster/manager/peers?count=0&output_mode=json", nil)
	mclient.AddHandler(wantRequest1, 200, string(response1), nil)
	mclient.AddHandler(wantRequest2, 200, string(response2), nil)

	// mock the verify RF peer funciton
	VerifyRFPeers = func(ctx context.Context, mgr indexerClusterPodManager, client splcommon.ControllerClient) error {
		return nil
	}

	// Mock the addTelApp function for unit tests
	addTelApp = func(ctx context.Context, podExecClient splutil.PodExecClientImpl, replicas int32, cr splcommon.MetaObject) error {
		return nil
	}

	// create directory for app framework
	newpath := filepath.Join("/tmp", "appframework")
	_ = os.MkdirAll(newpath, os.ModePerm)

	// adding getapplist to fix test case
	GetAppsList = func(ctx context.Context, remoteDataClientMgr RemoteDataClientManager) (splclient.RemoteDataListResponse, error) {
		RemoteDataListResponse := splclient.RemoteDataListResponse{}
		return RemoteDataListResponse, nil
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

	// create license manager custom resource

	// create licensemanager custom resource
	licensemanager := &enterpriseApi.LicenseManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: enterpriseApi.LicenseManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				Volumes: []corev1.Volume{},
				MonitoringConsoleRef: corev1.ObjectReference{
					Name: "mcName",
				},
			},
		},
	}

	replicas := int32(1)
	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-license-manager",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: "splunk-test-license-manager-headless",
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
			Name:      "splunk-test-license-manager-headless",
			Namespace: "default",
		},
	}

	// simulate service
	err := c.Create(ctx, service)
	if err != nil {
		t.Errorf("Unexpected create pod failed %v", err)
		debug.PrintStack()
	}

	// simulate create stateful set
	err = c.Create(ctx, statefulset)
	if err != nil {
		t.Errorf("Unexpected create pod failed %v", err)
		debug.PrintStack()
	}

	// simulate create clustermanager instance before reconcilation
	err = c.Create(ctx, licensemanager)
	if err != nil {
		t.Errorf("Unexpected create pod failed %v", err)
		debug.PrintStack()
	}

	_, err = ApplyLicenseManager(ctx, c, licensemanager)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for indexer cluster %v", err)
		debug.PrintStack()
	}

	namespacedName := types.NamespacedName{
		Name:      licensemanager.Name,
		Namespace: licensemanager.Namespace,
	}
	err = c.Get(ctx, namespacedName, licensemanager)
	if err != nil {
		t.Errorf("Unexpected get license manager %v", err)
		debug.PrintStack()
	}
	// simulate Ready state
	licensemanager.Status.Phase = enterpriseApi.PhaseReady
	licensemanager.Spec.ServiceTemplate.Annotations = map[string]string{
		"traffic.sidecar.istio.io/excludeOutboundPorts": "8089,8191,9997",
		"traffic.sidecar.istio.io/includeInboundPorts":  "8000,8088",
	}
	licensemanager.Spec.ServiceTemplate.Labels = map[string]string{
		"app.kubernetes.io/instance":   "splunk-test-license-manager",
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "license-manager",
		"app.kubernetes.io/name":       "license-manager",
		"app.kubernetes.io/part-of":    "splunk-test-license-manager",
	}
	err = c.Status().Update(ctx, licensemanager)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for cluster manager with app framework  %v", err)
		debug.PrintStack()
	}

	err = c.Get(ctx, namespacedName, licensemanager)
	if err != nil {
		t.Errorf("Unexpected get license manager %v", err)
		debug.PrintStack()
	}

	// call reconciliation
	_, err = ApplyLicenseManager(ctx, c, licensemanager)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for cluster manager with app framework  %v", err)
		debug.PrintStack()
	}

	// create pod
	stpod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-license-manager-0",
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
	err = c.Create(ctx, stpod)
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

	// get latest stateful set
	stNamespacedName := types.NamespacedName{
		Name:      "splunk-test-license-manager",
		Namespace: "default",
	}
	err = c.Get(ctx, stNamespacedName, statefulset)
	if err != nil {
		t.Errorf("Unexpected get license manager %v", err)
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

	err = c.Get(ctx, namespacedName, licensemanager)
	if err != nil {
		t.Errorf("Unexpected get license manager %v", err)
		debug.PrintStack()
	}

	// call reconciliation
	_, err = ApplyLicenseManager(ctx, c, licensemanager)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for cluster manager with app framework  %v", err)
		debug.PrintStack()
	}

	licenseObjRef := corev1.ObjectReference{
		Kind:      licensemanager.Kind,
		Name:      licensemanager.Name,
		Namespace: licensemanager.Namespace,
		UID:       licensemanager.UID,
	}

	// create clustermanager custom resource
	clustermanager := &enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				LicenseManagerRef: licenseObjRef,
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				Volumes: []corev1.Volume{},
			},
			AppFrameworkConfig: appFrameworkSpec,
		},
	}

	creplicas := int32(1)
	cstatefulset := &appsv1.StatefulSet{
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
			Replicas: &creplicas,
		},
	}

	// simulate create clustermanager instance before reconcilation
	err = c.Create(ctx, clustermanager)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for cluster manager with app framework  %v", err)
		debug.PrintStack()
	}

	// simulate Ready state
	namespacedName = types.NamespacedName{
		Name:      clustermanager.Name,
		Namespace: clustermanager.Namespace,
	}

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
	_, err = ApplyClusterManager(ctx, c, clustermanager)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for cluster manager with app framework  %v", err)
		debug.PrintStack()
	}

	// create pod
	stpod = &corev1.Pod{
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
	err = c.Create(ctx, stpod)
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

	stNamespacedName = types.NamespacedName{
		Name:      "splunk-test-cluster-manager",
		Namespace: "default",
	}
	err = c.Get(ctx, stNamespacedName, cstatefulset)
	if err != nil {
		t.Errorf("Unexpected get cluster manager %v", err)
		debug.PrintStack()
	}
	// update statefulset
	cstatefulset.Status.ReadyReplicas = 1
	cstatefulset.Status.Replicas = 1
	err = c.Status().Update(ctx, cstatefulset)
	if err != nil {
		t.Errorf("Unexpected update statefulset  %v", err)
		debug.PrintStack()
	}

	err = c.Get(ctx, namespacedName, clustermanager)
	if err != nil {
		t.Errorf("Unexpected get cluster manager %v", err)
		debug.PrintStack()
	}

	// call reconciliation
	_, err = ApplyClusterManager(ctx, c, clustermanager)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for cluster manager with app framework  %v", err)
		debug.PrintStack()
	}

	//create namespace MC statefulset
	current := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-default-license-manager",
			Namespace: "default",
		},
	}
	namespacedName = types.NamespacedName{Namespace: "default", Name: "splunk-default-license-manager"}

	// Create MC statefulset
	err = splutil.CreateResource(ctx, c, &current)
	if err != nil {
		t.Errorf("Failed to create owner reference  %s", current.GetName())
	}

	//setownerReference
	err = splctrl.SetStatefulSetOwnerRef(ctx, c, licensemanager, namespacedName)
	if err != nil {
		t.Errorf("Couldn't set owner ref for resource %s", current.GetName())
	}

	err = c.Get(ctx, namespacedName, &current)
	if err != nil {
		t.Errorf("Couldn't get the statefulset resource %s", current.GetName())
	}

	configmap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-default-license-manager",
			Namespace: "default",
		},
	}

	// Create configmap
	err = splutil.CreateResource(ctx, c, &configmap)
	if err != nil {
		t.Errorf("Failed to create resource  %s", current.GetName())
	}

	// call reconciliation
	_, err = ApplyLicenseManager(ctx, c, licensemanager)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for license manager with app framework  %v", err)
		debug.PrintStack()
	}
}
