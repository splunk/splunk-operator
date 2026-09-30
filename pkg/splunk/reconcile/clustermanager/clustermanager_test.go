// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package clustermanager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"k8s.io/apimachinery/pkg/runtime/schema"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	runtime "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	monitoringconsole "github.com/splunk/splunk-operator/pkg/splunk/reconcile/monitoringconsole"
	upgrade "github.com/splunk/splunk-operator/pkg/splunk/reconcile/upgrade"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/appframework"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/telapp"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"
)

func init() {
	splutil.GetReadinessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../../tools/k8_probes/readinessProbe.sh")
		return fileLocation
	}
	splutil.GetLivenessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../../tools/k8_probes/livenessProbe.sh")
		return fileLocation
	}
	splutil.GetStartupScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../../tools/k8_probes/startupProbe.sh")
		return fileLocation
	}
}

func stubCMMultisiteEnvVars(t *testing.T) {
	t.Helper()
	original := getCMMultisiteEnvVars
	getCMMultisiteEnvVars = func(ctx context.Context, cr *enterpriseApi.ClusterManager, namespaceScopedSecret *corev1.Secret) ([]corev1.EnvVar, error) {
		extraEnv := resources.GetClusterManagerExtraEnv(cr)
		return extraEnv, nil
	}
	t.Cleanup(func() { getCMMultisiteEnvVars = original })
}

func TestApplyClusterManager(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	stubCMMultisiteEnvVars(t)

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
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0], funcCalls[3], funcCalls[4], funcCalls[6], funcCalls[10], funcCalls[5]}, "List": {listmockCall[0]}, "Update": {funcCalls[0]}}
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
	reconcileFn := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyClusterManager(ctx, c, cr.(*enterpriseApi.ClusterManager), nil)
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplyClusterManager", &current, revised, createCalls, updateCalls, reconcileFn, true)

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
		_, err := ApplyClusterManager(ctx, c, cr.(*enterpriseApi.ClusterManager), nil)
		return true, err
	}
	spltest.SplunkDeletionTester(t, revised, deleteFunc)

	// Negative testing: spec validation failure is a terminal condition — returns nil (no requeue)
	current.Spec.CommonSplunkSpec.LivenessProbe = &enterpriseApi.Probe{
		InitialDelaySeconds: -1,
	}
	c := spltest.NewMockClient()
	_ = errors.New(splcommon.Rerr)
	current.Kind = "ClusterManager"
	_, err := ApplyClusterManager(ctx, c, &current, nil)
	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Errorf("stalled spec validation failure should return a terminal error, got %v", err)
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
	_, err = ApplyClusterManager(ctx, c, &current, nil)
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
	_, err = ApplyClusterManager(ctx, c, &current, nil)
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
	_, err = ApplyClusterManager(ctx, c, &current, nil)
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
	_, err = ApplyClusterManager(ctx, c, &current, nil)
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
	_, err = ApplyClusterManager(ctx, c, &current, nil)
	if err == nil {
		t.Errorf("Expected error")
	}
}

func TestValidateClusterManagerSpec(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
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
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
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
		spltest.ConfigTester(t, "getClusterManagerStatefulSet", f, want)
	}
	test(spltest.LoadFixture(t, "statefulset_stack1_cluster_manager_base.json"))

	cr.Spec.LicenseManagerRef.Name = "stack1"
	cr.Spec.LicenseManagerRef.Namespace = "test"
	test(spltest.LoadFixture(t, "statefulset_stack1_cluster_manager_base_1.json"))

	cr.Spec.LicenseManagerRef.Name = ""
	cr.Spec.LicenseURL = "/mnt/splunk.lic"
	test(spltest.LoadFixture(t, "statefulset_stack1_cluster_manager_base_2.json"))

	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	test(spltest.LoadFixture(t, "statefulset_stack1_cluster_manager_with_apps.json"))

	// Create a serviceaccount
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	test(spltest.LoadFixture(t, "statefulset_stack1_cluster_manager_with_service_account.json"))

	// Add extraEnv
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	test(spltest.LoadFixture(t, "statefulset_stack1_cluster_manager_with_service_account_1.json"))

	// Add additional label to cr metadata to transfer to the statefulset
	cr.ObjectMeta.Labels = make(map[string]string)
	cr.ObjectMeta.Labels["app.kubernetes.io/test-extra-label"] = "test-extra-label-value"
	test(spltest.LoadFixture(t, "statefulset_stack1_cluster_manager_with_service_account_2.json"))
}

func TestClusterManagerSpecNotCreatedWithoutGeneralTerms(t *testing.T) {
	// Unset the SPLUNK_GENERAL_TERMS environment variable
	os.Unsetenv("SPLUNK_GENERAL_TERMS")
	ctx := context.TODO()

	// Create a mock cluster manager CR
	cm := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterManager",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	// Create a mock client
	c := spltest.NewMockClient()

	// Attempt to apply the cluster manager spec
	_, err := ApplyClusterManager(ctx, c, &cm, nil)

	// SPLUNK_GENERAL_TERMS unset is a stalled misconfiguration: reconciler returns terminal error (no requeue)
	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Errorf("stalled spec validation failure should return a terminal error, got %v", err)
	}
}

func TestSmartstoreApplyClusterManagerFailsOnInvalidSmartStoreConfig(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	cr := enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "idxCluster",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			SmartStore: enterpriseApi.SmartStoreSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "", Path: "testbucket-rs-london"},
				},

				IndexList: []enterpriseApi.IndexSpec{
					{Name: "salesdata1"},
					{Name: "salesdata2", RemotePath: "salesdata2"},
					{Name: "salesdata3", RemotePath: ""},
				},
			},
		},
	}

	client := spltest.NewMockClient()

	_, err := ApplyClusterManager(context.TODO(), client, &cr, nil)
	// ValidateSplunkSmartstoreSpec is called inside validateClusterManagerSpec — stalled, returns terminal error
	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Errorf("stalled spec validation failure should return a terminal error, got %v", err)
	}
}

func TestSmartStoreConfigDoesNotFailOnClusterManagerCR(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	c := spltest.NewMockClient()
	cr := enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "CM",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			SmartStore: enterpriseApi.SmartStoreSpec{
				VolList: []enterpriseApi.VolumeSpec{
					{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret"},
				},

				IndexList: []enterpriseApi.IndexSpec{
					{Name: "salesdata1", RemotePath: "remotepath1", IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
						VolName: "msos_s2s3_vol"},
					},
					{Name: "salesdata2", RemotePath: "remotepath2", IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
						VolName: "msos_s2s3_vol"},
					},
					{Name: "salesdata3", RemotePath: "remotepath3", IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
						VolName: "msos_s2s3_vol"},
					},
				},
			},
		},
	}

	err := validateClusterManagerSpec(ctx, c, &cr)

	if err != nil {
		t.Errorf("Smartstore configuration should not fail on ClusterManager CR: %v", err)
	}
}

func TestApplyClusterManagerWithSmartstore(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	stubCMMultisiteEnvVars(t)

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
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-cluster-manager-secret-v1"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermanager-smartstore"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-clustermanager-smartstore"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-manager"},
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
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[7], funcCalls[8], funcCalls[12], funcCalls[14]}, "List": {listmockCall[0], listmockCall[0], listmockCall[1]}, "Update": {funcCalls[0], funcCalls[3], funcCalls[15]}}
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
	savedAddTelApp := telapp.AddTelApp
	defer func() { telapp.AddTelApp = savedAddTelApp }()
	telapp.AddTelApp = func(ctx context.Context, podExecClient splutil.PodExecClientImpl, replicas int32, cr splcommon.MetaObject) error {
		return nil
	}

	// Without S3 keys, ApplyClusterManager should fail
	current.Kind = "ClusterManager"
	_, err := ApplyClusterManager(ctx, client, &current, nil)
	if err == nil {
		t.Errorf("ApplyClusterManager should fail without S3 secrets configured")
	}

	// Create namespace scoped secret
	secret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Error(err.Error())
	}

	secret.Data[spltest.S3AccessKey] = []byte("abcdJDckRkxhMEdmSk5FekFRRzBFOXV6bGNldzJSWE9IenhVUy80aa")
	secret.Data[spltest.S3SecretKey] = []byte("g4NVp0a29PTzlPdGczWk1vekVUcVBSa0o4NkhBWWMvR1NadDV4YVEy")
	_, err = k8sops.ApplySecret(ctx, client, secret)
	if err != nil {
		t.Error(err.Error())
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
		_, err := ApplyClusterManager(context.Background(), c, cr.(*enterpriseApi.ClusterManager), nil)
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
	if _, err = ApplyClusterManager(context.Background(), client, &current, nil); err != nil {
		t.Errorf("ApplyClusterManager() should not have returned error")
	}

	current.Spec.CommonSplunkSpec.EtcVolumeStorageConfig.StorageCapacity = "-abcd"
	if _, err := ApplyClusterManager(context.Background(), client, &current, nil); err == nil {
		t.Errorf("ApplyClusterManager() should have returned error")
	}

	var replicas int32 = 3
	current.Spec.CommonSplunkSpec.EtcVolumeStorageConfig.StorageCapacity = ""
	ss.Status.ReadyReplicas = 3
	ss.Spec.Replicas = &replicas
	ss.Spec.Template.Spec.Containers[0].Image = "splunk/splunk"
	client.AddObject(ss)
	if result, err := ApplyClusterManager(context.Background(), client, &current, nil); err == nil && !result.Requeue {
		t.Errorf("ApplyClusterManager() should have returned error or result.requeue should have been false")
	}

	ss.Status.ReadyReplicas = 1
	*ss.Spec.Replicas = ss.Status.ReadyReplicas
	objects := []runtime.Object{ss, pod}
	client.AddObjects(objects)
	current.Spec.CommonSplunkSpec.Mock = false

	if _, err := ApplyClusterManager(context.Background(), client, &current, nil); err == nil {
		t.Errorf("ApplyClusterManager() should have returned error")
	}
}

func TestAppFrameworkApplyClusterManagerShouldNotFail(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	appframework.InitGlobalResourceTracker()

	stubCMMultisiteEnvVars(t)

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
	err := os.MkdirAll(appframework.TmpAppDownloadDir, 0755)
	defer os.RemoveAll(appframework.TmpAppDownloadDir)

	if err != nil {
		t.Errorf("Unable to create download directory for apps :%s", appframework.TmpAppDownloadDir)
	}

	client := spltest.NewMockClient()

	// Create S3 secret
	s3Secret := spltest.GetMockS3SecretKeys("s3-secret")

	client.AddObject(&s3Secret)

	// Create namespace scoped secret
	_, err = splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Error(err.Error())
	}

	cm.Kind = "ClusterManager"
	_, err = ApplyClusterManager(context.Background(), client, &cm, nil)
	if err != nil {
		t.Errorf("ApplyClusterManager should not have returned error here.")
	}
}

func TestApplyClusterManagerDeletion(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	stubCMMultisiteEnvVars(t)

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
		t.Error(err.Error())
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
	err = os.MkdirAll(appframework.TmpAppDownloadDir, 0755)
	defer os.RemoveAll(appframework.TmpAppDownloadDir)

	if err != nil {
		t.Errorf("Unable to create download directory for apps :%s", appframework.TmpAppDownloadDir)
	}
	cm.Kind = "ClusterManager"
	_, err = ApplyClusterManager(ctx, c, &cm, nil)
	if err != nil {
		t.Errorf("ApplyClusterManager should not have returned error here.")
	}
}
func createLicenseManagerStatefulSetForTest(t *testing.T, ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.LicenseManager) {
	t.Helper()
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("splunk-%s-license-manager", cr.GetName()),
			Namespace: cr.GetNamespace(),
		},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "splunk", Image: cr.Spec.Image}}}},
		},
	}
	if err := client.Create(ctx, statefulSet); err != nil {
		t.Fatalf("failed to create LicenseManager StatefulSet: %v", err)
	}
}

func TestChangeClusterManagerAnnotations(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
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

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	builder := spltest.NewFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.LicenseManager{}).
		WithStatusSubresource(&enterpriseApi.ClusterManager{})
	client := builder.Build()

	// Create the instances
	client.Create(ctx, lm)
	createLicenseManagerStatefulSetForTest(t, ctx, client, lm)
	var err error
	namespacedName := types.NamespacedName{
		Name:      lm.Name,
		Namespace: lm.Namespace,
	}
	err = client.Get(ctx, namespacedName, lm)
	if err != nil {
		t.Errorf("changeLicenseManagerAnnotations should not have returned error=%v", err)
	}

	// create pods for license manager
	spltest.CreatePods(t, ctx, client, "license-manager", fmt.Sprintf("splunk-%s-license-manager-0", lm.Name), lm.Namespace, lm.Spec.Image)
	spltest.UpdateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-license-manager", lm.Name), lm.Namespace)
	lm.Status.TelAppInstalled = true
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
	stubCMMultisiteEnvVars(t)
	cm.Kind = "ClusterManager"
	client.Create(ctx, cm)
	_, err = ApplyClusterManager(ctx, client, cm, nil)
	if err != nil {
		t.Errorf("applyClusterManager should not have returned error; err=%v", err)
	}
	err = k8sops.ChangeClusterManagerAnnotations(ctx, client, lm)
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

func TestIsClusterManagerReadyForUpgrade(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	stubCMMultisiteEnvVars(t)

	ctx := context.TODO()

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	builder := spltest.NewFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.LicenseManager{}).
		WithStatusSubresource(&enterpriseApi.ClusterManager{})
	client := builder.Build()

	// Create the ClusterManager first since the LicenseManager's env assembly
	// looks it up via ClusterManagerRef.
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
	createLicenseManagerStatefulSetForTest(t, ctx, client, &lm)
	var err error
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

	// Apply the ClusterManager created above now that its LicenseManager is ready
	_, err = ApplyClusterManager(ctx, client, &cm, nil)
	if err != nil {
		t.Errorf("applyClusterManager should not have returned error; err=%v", err)
	}

	// create pods for license manager
	lm.Status.TelAppInstalled = true
	lm.Spec.Image = "splunk2"
	spltest.CreatePods(t, ctx, client, "license-manager", fmt.Sprintf("splunk-%s-license-manager-0", lm.Name), lm.Namespace, lm.Spec.Image)
	spltest.UpdateStatefulSetsInTest(t, ctx, client, 1, fmt.Sprintf("splunk-%s-license-manager", lm.Name), lm.Namespace)
	// now the statefulset image in spec is updated to splunk2
	statefulSet := &appsv1.StatefulSet{}
	if err := client.Get(ctx, types.NamespacedName{Name: "splunk-test-license-manager", Namespace: "test"}, statefulSet); err != nil {
		t.Fatalf("failed to get LicenseManager StatefulSet: %v", err)
	}
	statefulSet.Spec.Template.Spec.Containers[0].Image = lm.Spec.Image
	if err := client.Update(ctx, statefulSet); err != nil {
		t.Fatalf("failed to update LicenseManager StatefulSet: %v", err)
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
	clusterManager.Spec.Image = "splunk2"
	err = client.Update(ctx, clusterManager)
	if err != nil {
		t.Errorf("update should not have returned error; err=%v", err)
	}

	check, err := upgrade.UpgradePathValidation(ctx, client, clusterManager, clusterManager.Spec.CommonSplunkSpec, nil)

	if err != nil {
		t.Errorf("Unexpected upgradeScenario error %v", err)
	}

	if !check {
		t.Errorf("isClusterManagerReadyForUpgrade: CM should be ready for upgrade")
	}
}

func TestClusterManagerWitReadyState(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	// create directory for app framework
	newpath := filepath.Join("/tmp", "appframework")
	_ = os.MkdirAll(newpath, os.ModePerm)

	// Mock getCMMultisiteEnvVars to avoid 5-second HTTP timeout
	// This function tries to connect to Splunk REST API which doesn't exist in unit tests
	stubCMMultisiteEnvVars(t)

	savedPerformCmBundlePush := performCmBundlePush
	performCmBundlePush = func(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.ClusterManager, podExecClient splutil.PodExecClientImpl) error {
		// Just set the flag to false to simulate successful bundle push
		cr.Status.BundlePushTracker.NeedToPushManagerApps = false
		return nil
	}
	defer func() { performCmBundlePush = savedPerformCmBundlePush }()

	// Mock GetPodExecClient to return a mock client that simulates pod operations locally
	savedGetPodExecClient := splutil.GetPodExecClient
	splutil.GetPodExecClient = func(client splcommon.ControllerClient, cr splcommon.MetaObject, targetPodName string) splutil.PodExecClientImpl {
		mockClient := &spltest.MockPodExecClient{
			Client:        client,
			Cr:            cr,
			TargetPodName: targetPodName,
		}
		// Add mock responses for common commands
		ctx := context.TODO()
		// Mock mkdir command (used by createDirOnSplunkPods)
		mockClient.AddMockPodExecReturnContext(ctx, "", &spltest.MockPodExecReturnContext{
			StdOut: "",
			StdErr: "",
			Err:    nil,
		})
		return mockClient
	}
	defer func() { splutil.GetPodExecClient = savedGetPodExecClient }()

	// adding getapplist to fix test case
	savedGetAppsList := appframework.GetAppsList
	appframework.GetAppsList = func(ctx context.Context, remoteDataClientMgr appframework.RemoteDataClientManager) (splcommon.RemoteDataListResponse, error) {
		remoteDataListResponse := splcommon.RemoteDataListResponse{}
		return remoteDataListResponse, nil
	}
	defer func() { appframework.GetAppsList = savedGetAppsList }()

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	builder := spltest.NewFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.LicenseManager{}).
		WithStatusSubresource(&enterpriseApi.ClusterManager{}).
		WithStatusSubresource(&enterpriseApi.Standalone{}).
		WithStatusSubresource(&enterpriseApi.MonitoringConsole{}).
		WithStatusSubresource(&enterpriseApi.IndexerCluster{}).
		WithStatusSubresource(&enterpriseApi.SearchHeadCluster{})

	c := builder.Build()
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
	// simulate create clustermanager instance before reconciliation
	c.Create(ctx, clustermanager)

	_, err := ApplyClusterManager(ctx, c, clustermanager, nil)
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
	_, err = ApplyClusterManager(ctx, c, clustermanager, nil)
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
	err = k8sops.SetStatefulSetOwnerRef(ctx, c, clustermanager, namespacedName)
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

	// Mock the telapp.AddTelApp function for unit tests
	savedAddTelApp := telapp.AddTelApp
	defer func() { telapp.AddTelApp = savedAddTelApp }()
	telapp.AddTelApp = func(ctx context.Context, podExecClient splutil.PodExecClientImpl, replicas int32, cr splcommon.MetaObject) error {
		return nil
	}

	// call reconciliation
	clustermanager.Kind = "ClusterManager"
	_, err = ApplyClusterManager(ctx, c, clustermanager, nil)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for cluster manager with app framework  %v", err)
		debug.PrintStack()
	}
}

func TestCheckCmRemainingReferences(t *testing.T) {
	ctx := context.TODO()
	cmCr := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{},
	}
	client := spltest.NewMockClient()

	err := checkClusterManagerRemainingReferences(ctx, client, &cmCr)
	if err != nil {
		t.Errorf("Didn't expect error, clean run required %v", err)
	}

	// Add an indexerCluster to the client
	idxc := enterpriseApi.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.IndexerClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				ClusterManagerRef: corev1.ObjectReference{
					Name: "stack1",
				},
			}},
	}
	idxcList := &enterpriseApi.IndexerClusterList{}
	idxcList.Items = append(idxcList.Items, idxc)

	client.ListObj = idxcList
	err = checkClusterManagerRemainingReferences(ctx, client, &cmCr)
	if err == nil {
		t.Errorf("Expected an error for having found a stale IDXC connected to clusterManager %v", err)
	}

	// Add a SHC to the client
	shcClient := spltest.NewMockClient()

	shc := enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				ClusterManagerRef: corev1.ObjectReference{
					Name: "stack1",
				},
			}},
	}
	shcList := &enterpriseApi.SearchHeadClusterList{}
	shcList.Items = append(shcList.Items, shc)

	shcClient.ListObj = shcList
	err = checkClusterManagerRemainingReferences(ctx, shcClient, &cmCr)
	if err == nil {
		t.Errorf("Expected an error for having found a stale SHC connected to clusterManager %v", err)
	}

	// Add a LM to the client
	lmClient := spltest.NewMockClient()

	lm := enterpriseApi.LicenseManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "LicenseManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.LicenseManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				ClusterManagerRef: corev1.ObjectReference{
					Name: "stack1",
				},
			}},
	}
	lmList := &enterpriseApi.LicenseManagerList{}
	lmList.Items = append(lmList.Items, lm)

	lmClient.ListObj = lmList
	err = checkClusterManagerRemainingReferences(ctx, lmClient, &cmCr)
	if err == nil {
		t.Errorf("Expected an error for having found a stale LM connected to clusterManager %v", err)
	}

	// Add a MC to the client
	mcClient := spltest.NewMockClient()

	mc := enterpriseApi.MonitoringConsole{
		TypeMeta: metav1.TypeMeta{
			Kind: "MonitoringConsole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.MonitoringConsoleSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				ClusterManagerRef: corev1.ObjectReference{
					Name: "stack1",
				},
			}},
	}
	mcList := &enterpriseApi.MonitoringConsoleList{}
	mcList.Items = append(mcList.Items, mc)

	mcClient.ListObj = mcList
	err = checkClusterManagerRemainingReferences(ctx, mcClient, &cmCr)
	if err == nil {
		t.Errorf("Expected an error for having found a stale MC connected to clusterManager %v", err)
	}

}

func TestClusterManagerUpdatesMonitoringConsoleAnnotations(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))
	builder := spltest.NewFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.LicenseManager{}).
		WithStatusSubresource(&enterpriseApi.ClusterManager{}).
		WithStatusSubresource(&enterpriseApi.Standalone{}).
		WithStatusSubresource(&enterpriseApi.MonitoringConsole{}).
		WithStatusSubresource(&enterpriseApi.IndexerCluster{}).
		WithStatusSubresource(&enterpriseApi.SearchHeadCluster{})
	client := builder.Build()
	utilruntime.Must(enterpriseApi.AddToScheme(clientgoscheme.Scheme))
	// define CM and MC
	cm := &enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				Volumes: []corev1.Volume{},
			},
		},
	}
	mc := &enterpriseApi.MonitoringConsole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: enterpriseApi.MonitoringConsoleSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				Volumes: []corev1.Volume{},
				ClusterManagerRef: corev1.ObjectReference{
					Name: "test",
				},
			},
		},
	}
	cm.Spec.Image = "splunk/splunk:latest"
	// Create the instances
	if err := client.Create(ctx, cm); err != nil {
		t.Fatalf("failed to create ClusterManager: %v", err)
	}
	_, err := ApplyClusterManager(ctx, client, cm, nil)
	if err != nil {
		t.Errorf("applyClusterManager should not have returned error; err=%v", err)
	}
	namespacedName := types.NamespacedName{
		Name:      cm.Name,
		Namespace: cm.Namespace,
	}
	err = client.Get(ctx, namespacedName, cm)
	if err != nil {
		t.Errorf("changeMonitoringConsoleAnnotations should not have returned error=%v", err)
	}
	cm.Status.Phase = enterpriseApi.PhaseReady
	err = client.Status().Update(ctx, cm)
	if err != nil {
		t.Errorf("Unexpected update pod  %v", err)
		debug.PrintStack()
	}
	if err := client.Create(ctx, mc); err != nil {
		t.Fatalf("failed to create MonitoringConsole: %v", err)
	}
	_, err = monitoringconsole.ApplyMonitoringConsole(ctx, client, mc)
	if err != nil {
		t.Errorf("applyMonitoringConsole should not have returned error; err=%v", err)
	}
	err = changeMonitoringConsoleAnnotations(ctx, client, cm)
	if err != nil {
		t.Errorf("changeMonitoringConsoleAnnotations should not have returned error=%v", err)
	}
	monitoringConsole := &enterpriseApi.MonitoringConsole{}
	namespacedName = types.NamespacedName{
		Name:      cm.Name,
		Namespace: cm.Namespace,
	}
	err = client.Get(ctx, namespacedName, monitoringConsole)
	if err != nil {
		t.Errorf("changeMonitoringConsoleAnnotations should not have returned error=%v", err)
	}
	annotations := monitoringConsole.GetAnnotations()
	if annotations["splunk/image-tag"] != cm.Spec.Image {
		t.Errorf("changeMonitoringConsoleAnnotations should have set the checkUpdateImage annotation field to the current image")
	}
}
