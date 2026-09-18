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

package searchheadcluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	reconcile "sigs.k8s.io/controller-runtime/pkg/reconcile"

	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splstorage "github.com/splunk/splunk-operator/pkg/splunk/client/storage"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	shcworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/shc"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/telapp"
)

func init() {
	// Re-Assigning splutil.GetReadinessScriptLocation, splutil.GetLivenessScriptLocation, splutil.GetStartupScriptLocation to use absolute path for readinessScriptLocation, readinessScriptLocation
	splutil.GetReadinessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../../" + readinessScriptLocation)
		return fileLocation
	}
	splutil.GetLivenessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../../" + livenessScriptLocation)
		return fileLocation
	}
	splutil.GetStartupScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../../" + startupScriptLocation)
		return fileLocation
	}
}

func TestApplySearchHeadCluster(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},

		{MetaName: "*v1.ConfigMap-test-splunk-search-head-stack1-configmap"},

		{MetaName: "*v1.Service-test-splunk-stack1-search-head-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-search-head-service"},

		{MetaName: "*v1.Service-test-splunk-stack1-deployer-service"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-deployer"},

		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},

		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-deployer-secret-v1"},

		{MetaName: "*v1.StatefulSet-test-splunk-stack1-deployer"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},

		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-search-head-secret-v1"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},

		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v4.SearchHeadCluster-test-stack1"},
		{MetaName: "*v4.SearchHeadCluster-test-stack1"},
	}

	createFuncCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.ConfigMap-test-splunk-search-head-stack1-configmap"},
		{MetaName: "*v1.Service-test-splunk-stack1-search-head-headless"},
		{MetaName: "*v1.Service-test-splunk-stack1-search-head-service"},

		{MetaName: "*v1.Service-test-splunk-stack1-deployer-service"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-deployer"},

		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-deployer-secret-v1"},
		//{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-deployer"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-deployer"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},

		{MetaName: "*v1.ConfigMap-test-splunk-test-probe-configmap"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-stack1-search-head-secret-v1"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v4.SearchHeadCluster-test-stack1"},
		{MetaName: "*v4.SearchHeadCluster-test-stack1"},
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

	// CheckPodsForTerminalFailures lists pods using the SHC StatefulSet's selector labels.
	shcPodLabels := map[string]string{
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "search-head",
		"app.kubernetes.io/name":       "search-head",
		"app.kubernetes.io/part-of":    "splunk-stack1-search-head",
		"app.kubernetes.io/instance":   "splunk-stack1-search-head",
	}
	shcPodListOpts := []client.ListOption{
		client.InNamespace("test"),
		client.MatchingLabels(shcPodLabels),
	}
	shcPodListMockCall := spltest.MockFuncCall{ListOpts: shcPodListOpts}

	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[0], funcCalls[3], funcCalls[4], funcCalls[5], funcCalls[6], funcCalls[10], funcCalls[12], funcCalls[13], funcCalls[17], funcCalls[19]}, "Update": {funcCalls[0]}, "List": {listmockCall[0], listmockCall[0], shcPodListMockCall}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": createFuncCalls, "Update": {createFuncCalls[6], createFuncCalls[18]}, "List": {listmockCall[0], listmockCall[0], shcPodListMockCall}}
	statefulSet := enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	// Set shc changed to true for testing
	searchHeads := 3
	for i := 0; i < searchHeads; i++ {
		statefulSet.Status.ShcSecretChanged = append(statefulSet.Status.ShcSecretChanged, true)
	}
	revised := statefulSet.DeepCopy()
	revised.Spec.Image = "splunk/test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplySearchHeadCluster(context.TODO(), c, cr.(*enterpriseApi.SearchHeadCluster))
		return err
	}
	spltest.ReconcileTesterWithoutRedundantCheck(t, "TestApplySearchHeadCluster", &statefulSet, revised, createCalls, updateCalls, reconcile, true)

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	revised.ObjectMeta.DeletionTimestamp = &currentTime
	revised.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	deleteFunc := func(cr splcommon.MetaObject, c splcommon.ControllerClient) (bool, error) {
		_, err := ApplySearchHeadCluster(context.Background(), c, cr.(*enterpriseApi.SearchHeadCluster))
		return true, err
	}
	splunkDeletionTester(t, revised, deleteFunc)
}

func TestGetSearchHeadStatefulSet(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.SearchHeadCluster{
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
			if err := validateSearchHeadClusterSpec(ctx, c, &cr); err != nil {
				t.Errorf("validateSearchHeadClusterSpec() returned error: %v", err)
			}
			return getSearchHeadStatefulSet(ctx, c, &cr)
		}
		configTester(t, fmt.Sprintf("getSearchHeadStatefulSet(Replicas=%d)", cr.Spec.Replicas), f, want)
	}

	cr.Spec.Replicas = 3
	test(loadFixture(t, "statefulset_stack1_search_head_base.json"))

	cr.Spec.Replicas = 4
	test(loadFixture(t, "statefulset_stack1_search_head_base_1.json"))

	cr.Spec.Replicas = 5
	cr.Spec.ClusterManagerRef.Name = "stack1"
	_ = splutil.CreateResource(ctx, c, &enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	})
	test(loadFixture(t, "statefulset_stack1_search_head_base_2.json"))

	cr.Spec.Replicas = 6

	cr.Spec.ClusterManagerRef.Namespace = "test2"
	_ = splutil.CreateResource(ctx, c, &enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test2",
		},
	})
	test(loadFixture(t, "statefulset_stack1_search_head_base_3.json"))

	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	test(loadFixture(t, "statefulset_stack1_search_head_base_4.json"))

	// Define additional service port in CR and verified the statefulset has the new port
	test(loadFixture(t, "statefulset_stack1_search_head_base_5.json"))

	// Create a serviceaccount
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"
	test(loadFixture(t, "statefulset_stack1_search_head_with_service_account.json"))

	// Add extraEnv
	cr.Spec.CommonSplunkSpec.ExtraEnv = []corev1.EnvVar{
		{
			Name:  "TEST_ENV_VAR",
			Value: "test_value",
		},
	}
	test(loadFixture(t, "statefulset_stack1_search_head_with_service_account_1.json"))

	// Add additional label to cr metadata to transfer to the statefulset
	cr.ObjectMeta.Labels = make(map[string]string)
	cr.ObjectMeta.Labels["app.kubernetes.io/test-extra-label"] = "test-extra-label-value"
	test(loadFixture(t, "statefulset_stack1_search_head_with_service_account_2.json"))
}

func TestGetDeployerStatefulSet(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.SearchHeadCluster{
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
			if err := validateSearchHeadClusterSpec(ctx, c, &cr); err != nil {
				t.Errorf("validateSearchHeadClusterSpec() returned error: %v", err)
			}
			return getDeployerStatefulSet(ctx, c, &cr)
		}
		configTester(t, "getDeployerStatefulSet()", f, want)
	}

	cr.Spec.Replicas = 3
	test(loadFixture(t, "statefulset_stack1_deployer_base.json"))

	// Allow installation of apps via DefaultsURLApps on the SHCDeployer
	cr.Spec.DefaultsURLApps = "/mnt/apps/apps.yml"
	test(loadFixture(t, "statefulset_stack1_deployer_with_apps.json"))

	// Create a serviceaccount
	current := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	_ = splutil.CreateResource(ctx, c, &current)
	cr.Spec.ServiceAccount = "defaults"

	test(loadFixture(t, "statefulset_stack1_deployer_with_service_account.json"))
}

func TestSearchHeadSpecNotCreatedWithoutGeneralTerms(t *testing.T) {
	// Unset the SPLUNK_GENERAL_TERMS environment variable
	os.Unsetenv("SPLUNK_GENERAL_TERMS")
	ctx := context.TODO()

	// Create a mock search head CR
	shc := enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			Replicas: 3,
		},
	}

	// Create a mock client
	c := spltest.NewMockClient()

	// Attempt to apply the search head spec
	_, err := ApplySearchHeadCluster(ctx, c, &shc)

	// SPLUNK_GENERAL_TERMS unset is a stalled misconfiguration: reconciler returns terminal error (no requeue)
	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Errorf("stalled spec validation failure should return a terminal error, got %v", err)
	}
}

func TestApplySearchHeadClusterValidationFailure(t *testing.T) {
	ctx := context.TODO()
	shc := &enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "SearchHeadCluster",
			APIVersion: "enterprise.splunk.com/v4",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				LivenessProbe: &enterpriseApi.Probe{
					InitialDelaySeconds: -5, // Invalid value
				},
				Volumes: []corev1.Volume{},
			},
		},
		Status: enterpriseApi.SearchHeadClusterStatus{},
	}

	c := spltest.NewMockClient()

	err := c.Create(ctx, shc)
	if err != nil {
		t.Errorf("shc CR creation failed: %v", err)
	}

	_, err = ApplySearchHeadCluster(ctx, c, shc)
	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Errorf("stalled spec validation failure should return a terminal error, got %v", err)
	}
	if shc.Status.Phase != enterpriseApi.PhaseError {
		t.Errorf("Expected PhaseError, got %v", shc.Status.Phase)
	}
	if shc.Status.DeployerPhase != enterpriseApi.PhaseError {
		t.Errorf("Expected DeployerPhaseError, got %v", shc.Status.DeployerPhase)
	}
}

func TestApplySearchHeadClusterDeployerPodTerminalFailure(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	scheme := pkgruntime.NewScheme()
	utilruntime.Must(enterpriseApi.AddToScheme(scheme))
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	c := newFakeClientBuilder(scheme).
		WithStatusSubresource(&enterpriseApi.SearchHeadCluster{}).
		Build()

	cr := &enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "SearchHeadCluster",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	if err := c.Create(ctx, cr); err != nil {
		t.Fatalf("failed to create SHC CR: %v", err)
	}

	// Pass 1: creates the deployer StatefulSet (PhasePending); no error expected.
	if _, err := ApplySearchHeadCluster(ctx, c, cr); err != nil {
		t.Fatalf("pass 1 unexpectedly failed: %v", err)
	}

	// Inject a deployer pod stuck in ImagePullBackOff so that
	// checkPodsForTerminalFailures returns a TerminalError on the next reconcile.
	deployerPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetPodName(SplunkDeployer, cr.GetName(), 0),
			Namespace: cr.GetNamespace(),
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "splunk-operator",
				"app.kubernetes.io/component":  "search-head",
				"app.kubernetes.io/name":       "deployer",
				"app.kubernetes.io/part-of":    fmt.Sprintf("splunk-%s-search-head", cr.GetName()),
				"app.kubernetes.io/instance":   GetSplunkStatefulsetName(SplunkDeployer, cr.GetName()),
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{
						Reason:  "ImagePullBackOff",
						Message: "Back-off pulling image",
					},
				},
			}},
		},
	}
	if err := c.Create(ctx, deployerPod); err != nil {
		t.Fatalf("failed to create terminal deployer pod: %v", err)
	}

	// Pass 2: deployer STS already exists with ReadyReplicas=0; the terminal pod
	// is detected and the function must return a TerminalError with Stalled=True.
	_, err := ApplySearchHeadCluster(ctx, c, cr)
	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Errorf("expected TerminalError for deployer ImagePullBackOff pod, got %v", err)
	}
}

// TestApplySearchHeadClusterUpgradePathSoftWait verifies that when
// UpgradePathValidation soft-waits on a LicenseManager that is temporarily
// not Ready (returns (false, nil), not an error), ApplySearchHeadCluster
// does not leave the earlier-staged PhaseError as the persisted status on
// either cr.Status.Phase or cr.Status.DeployerPhase (CSPL-5080).
func TestApplySearchHeadClusterUpgradePathSoftWait(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()

	scheme := pkgruntime.NewScheme()
	utilruntime.Must(enterpriseApi.AddToScheme(scheme))
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	c := newFakeClientBuilder(scheme).
		WithStatusSubresource(&enterpriseApi.SearchHeadCluster{}).
		WithStatusSubresource(&enterpriseApi.LicenseManager{}).
		Build()

	lm := &enterpriseApi.LicenseManager{
		TypeMeta: metav1.TypeMeta{
			Kind:       "LicenseManager",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lm1",
			Namespace: "test",
		},
	}
	if err := c.Create(ctx, lm); err != nil {
		t.Fatalf("failed to create LicenseManager CR: %v", err)
	}
	// LicenseManager not yet Ready: this is the benign soft-wait state.
	lm.Status.Phase = enterpriseApi.PhaseUpdating
	if err := c.Status().Update(ctx, lm); err != nil {
		t.Fatalf("failed to set LicenseManager status: %v", err)
	}

	cr := &enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "SearchHeadCluster",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				LicenseManagerRef: corev1.ObjectReference{
					Name: "lm1",
				},
			},
		},
	}
	if err := c.Create(ctx, cr); err != nil {
		t.Fatalf("failed to create SHC CR: %v", err)
	}

	// Pass 1: creates the deployer StatefulSet; no LicenseManager image to
	// compare against yet, so UpgradePathValidation isn't exercised until
	// the StatefulSet already exists (see CSPL-3060 guard).
	if _, err := ApplySearchHeadCluster(ctx, c, cr); err != nil {
		t.Fatalf("pass 1 unexpectedly failed: %v", err)
	}

	// The CSPL-3060 guard checks statefulSet.CreationTimestamp.IsZero(); the
	// fake client doesn't populate CreationTimestamp on Create, so set it
	// explicitly to make pass 2 exercise UpgradePathValidation.
	deployerStatefulSetName := types.NamespacedName{
		Name:      GetSplunkStatefulsetName(SplunkDeployer, cr.GetName()),
		Namespace: cr.GetNamespace(),
	}
	deployerStatefulSet := &appsv1.StatefulSet{}
	if err := c.Get(ctx, deployerStatefulSetName, deployerStatefulSet); err != nil {
		t.Fatalf("failed to fetch deployer StatefulSet: %v", err)
	}
	deployerStatefulSet.CreationTimestamp = metav1.Now()
	if err := c.Update(ctx, deployerStatefulSet); err != nil {
		t.Fatalf("failed to set deployer StatefulSet CreationTimestamp: %v", err)
	}

	// Give the LicenseManager a StatefulSet whose image matches the SHC's
	// so UpgradePathValidation reaches the not-Ready soft-wait branch
	// instead of the hard image-mismatch error branch.
	lmStatefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(SplunkLicenseManager, lm.GetName()),
			Namespace: lm.GetNamespace(),
		},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "splunk", Image: cr.Spec.CommonSplunkSpec.Image},
					},
				},
			},
		},
	}
	if err := c.Create(ctx, lmStatefulSet); err != nil {
		t.Fatalf("failed to create LicenseManager StatefulSet: %v", err)
	}

	// Pass 2: deployer StatefulSet already exists, so UpgradePathValidation
	// runs, finds the LicenseManager image matches but Phase != Ready, and
	// soft-waits by returning (false, nil).
	if _, err := ApplySearchHeadCluster(ctx, c, cr); err != nil {
		t.Fatalf("pass 2 unexpectedly returned an error for a benign soft-wait: %v", err)
	}

	if err := c.Get(ctx, types.NamespacedName{Name: cr.GetName(), Namespace: cr.GetNamespace()}, cr); err != nil {
		t.Fatalf("failed to re-fetch SHC CR: %v", err)
	}
	if cr.Status.Phase != enterpriseApi.PhasePending {
		t.Errorf("expected Phase=Pending during LicenseManager soft-wait, got %v", cr.Status.Phase)
	}
	if cr.Status.DeployerPhase != enterpriseApi.PhasePending {
		t.Errorf("expected DeployerPhase=Pending during LicenseManager soft-wait, got %v", cr.Status.DeployerPhase)
	}
}

func TestAppFrameworkSearchHeadClusterShouldNotFail(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			Replicas: 3,
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
		t.Error(err.Error())
	}

	// Create S3 secret
	s3Secret := spltest.GetMockS3SecretKeys("s3-secret")

	client.AddObject(&s3Secret)

	// to pass the validation stage, add the directory to download apps
	err = os.MkdirAll(splcommon.AppDownloadVolume, 0755)
	defer os.RemoveAll(splcommon.AppDownloadVolume)

	if err != nil && !os.IsPermission(err) {
		t.Errorf("Unable to create download directory for apps :%s", splcommon.AppDownloadVolume)
	}

	_, err = ApplySearchHeadCluster(ctx, client, &cr)
	if err != nil {
		t.Errorf("ApplySearchHeadCluster should be successful")
	}
}

func TestSHCGetAppsListForAWSS3ClientShouldNotFail(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			Replicas: 3,
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
		t.Error(err.Error())
	}

	splstorage.RegisterRemoteDataClient(ctx, "aws")

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

		vol, err = splutil.GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
		if err != nil {
			allSuccess = false
			continue
		}

		// Update the GetRemoteDataClient with our mock call which initializes mock AWS client
		getClientWrapper := splstorage.RemoteDataClientsMap[vol.Provider]
		getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, splstorage.NewMockAWSS3Client)

		remoteDataClientMgr := &RemoteDataClientManager{client: client,
			CR: &cr, appFrameworkRef: &cr.Spec.AppFrameworkConfig,
			vol:      &vol,
			location: appSource.Location,
			initFn: func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
				cl := spltest.MockAWSS3Client{}
				cl.Objects = mockAwsObjects[index].Objects
				return cl
			},
			getRemoteDataClient: func(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec, location string, fn splcommon.GetInitFunc) (splstorage.SplunkRemoteDataClient, error) {
				c, err := GetRemoteStorageClient(ctx, client, cr, appFrameworkRef, vol, location, fn)
				return c, err
			},
		}

		RemoteDataListResponse, err := remoteDataClientMgr.GetAppsList(ctx)
		if err != nil {
			allSuccess = false
			continue
		}

		var mockResponse spltest.MockRemoteDataClient
		mockResponse, err = splstorage.ConvertRemoteDataListResponse(ctx, RemoteDataListResponse)
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

func TestSHCGetAppsListForAWSS3ClientShouldFail(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	cr := enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
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
		t.Error(err.Error())
	}

	splstorage.RegisterRemoteDataClient(ctx, "aws")

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

	appFrameworkRef := cr.Spec.AppFrameworkConfig

	mockAwsHandler.AddObjects(appFrameworkRef, mockAwsObjects...)

	var vol enterpriseApi.VolumeSpec

	appSource := appFrameworkRef.AppSources[0]
	vol, err = splutil.GetAppSrcVolume(ctx, appSource, &appFrameworkRef)
	if err != nil {
		t.Errorf("Unable to get Volume due to error=%s", err)
	}

	// Update the GetRemoteDataClient with our mock call which initializes mock AWS client
	getClientWrapper := splstorage.RemoteDataClientsMap[vol.Provider]
	getClientWrapper.SetRemoteDataClientFuncPtr(ctx, vol.Provider, splstorage.NewMockAWSS3Client)

	remoteDataClientMgr := &RemoteDataClientManager{
		client:          client,
		CR:              &cr,
		appFrameworkRef: &cr.Spec.AppFrameworkConfig,
		vol:             &vol,
		location:        appSource.Location,
		initFn: func(ctx context.Context, region, accessKeyID, secretAccessKey string) interface{} {
			// Purposefully return nil here so that we test the error scenario
			return nil
		},
		getRemoteDataClient: func(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject,
			appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec,
			location string, fn splcommon.GetInitFunc) (splstorage.SplunkRemoteDataClient, error) {
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

	remoteDataClientResponse, err := remoteDataClientMgr.GetAppsList(ctx)
	if err != nil {
		t.Errorf("GetAppsList should not have returned error since empty appSources are allowed.")
	}
	if len(remoteDataClientResponse.Objects) != 0 {
		t.Errorf("GetAppsList should return an empty response since we have empty objects in MockAWSS3Client")
	}
}

func TestApplySearchHeadClusterDeletion(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	shc := enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
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

	// Create namespace scoped secret
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Error(err.Error())
	}

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	shc.ObjectMeta.DeletionTimestamp = &currentTime
	shc.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}

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

	if err != nil && !os.IsPermission(err) {
		t.Errorf("Unable to create download directory for apps :%s", splcommon.AppDownloadVolume)
	}

	_, err = ApplySearchHeadCluster(ctx, c, &shc)
	if err != nil {
		t.Errorf("ApplySearchHeadCluster should not have returned error here.")
	}
}

func TestGetSearchHeadClusterList(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	shc := enterpriseApi.SearchHeadCluster{}

	listOpts := []client.ListOption{
		client.InNamespace("test"),
	}

	client := spltest.NewMockClient()

	var numOfObjects int

	// Invalid scenario since we haven't added shc to the list yet
	_, err := k8sops.GetSearchHeadClusterList(ctx, client, &shc, listOpts)
	if err == nil {
		t.Errorf("getNumOfObjects should have returned error as we haven't added shc to the list yet")
	}

	shcList := &enterpriseApi.SearchHeadClusterList{}
	shcList.Items = append(shcList.Items, shc)

	client.ListObj = shcList

	objList, err := k8sops.GetSearchHeadClusterList(ctx, client, &shc, listOpts)
	if err != nil {
		t.Errorf("getNumOfObjects should not have returned error=%v", err)
	}

	numOfObjects = len(objList.Items)
	if numOfObjects != 1 {
		t.Errorf("Got wrong number of SearchHeadCluster objects. Expected=%d, Got=%d", 1, numOfObjects)
	}
}

func TestSearchHeadClusterWithReadyState(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	mclient := &spltest.MockHTTPClient{}
	type Entry1 struct {
		Content splclient.SearchHeadCaptainInfo `json:"content"`
	}

	apiResponse1 := struct {
		Entry []Entry1 `json:"entry"`
	}{
		Entry: []Entry1{
			{
				Content: splclient.SearchHeadCaptainInfo{
					Initialized:     true,
					ServiceReady:    true,
					MaintenanceMode: true,
				},
			},
			{
				Content: splclient.SearchHeadCaptainInfo{
					Initialized:     true,
					ServiceReady:    true,
					MaintenanceMode: true,
				},
			},
			{
				Content: splclient.SearchHeadCaptainInfo{
					Initialized:     true,
					ServiceReady:    true,
					MaintenanceMode: true,
				},
			},
		},
	}

	type Entry struct {
		Name    string                                `json:"name"`
		Content splclient.SearchHeadClusterMemberInfo `json:"content"`
	}

	apiResponse2 := struct {
		Entry []Entry `json:"entry"`
	}{
		Entry: []Entry{
			{
				Name: "splunk-test-search-head-0",
				Content: splclient.SearchHeadClusterMemberInfo{
					ActiveHistoricalSearchCount: 1,
					ActiveRealtimeSearchCount:   1,
					Adhoc:                       true,
					Registered:                  true,
					LastHeartbeatAttempt:        1,
					PeerLoadStatsGla15m:         1,
					PeerLoadStatsGla1m:          1,
					PeerLoadStatsGla5m:          1,
					RestartState:                "Up",
					Status:                      "Up",
				},
			},
			{
				Name: "splunk-test-search-head-1",
				Content: splclient.SearchHeadClusterMemberInfo{
					ActiveHistoricalSearchCount: 1,
					ActiveRealtimeSearchCount:   1,
					Adhoc:                       true,
					Registered:                  true,
					LastHeartbeatAttempt:        1,
					PeerLoadStatsGla15m:         1,
					PeerLoadStatsGla1m:          1,
					PeerLoadStatsGla5m:          1,
					RestartState:                "Up",
					Status:                      "Up",
				},
			},
			{
				Name: "splunk-test-search-head-2",
				Content: splclient.SearchHeadClusterMemberInfo{
					ActiveHistoricalSearchCount: 1,
					ActiveRealtimeSearchCount:   1,
					Adhoc:                       true,
					Registered:                  true,
					LastHeartbeatAttempt:        1,
					PeerLoadStatsGla15m:         1,
					PeerLoadStatsGla1m:          1,
					PeerLoadStatsGla5m:          1,
					RestartState:                "Up",
					Status:                      "Up",
				},
			},
		},
	}

	type Entry3 struct {
		Content splclient.SearchHeadCaptainInfo `json:"content"`
	}

	apiResponse3 := struct {
		Entry []Entry3 `json:"entry"`
	}{
		Entry: []Entry3{
			{
				Content: splclient.SearchHeadCaptainInfo{
					ServiceReady:    true,
					Identifier:      "1",
					ElectedCaptain:  1,
					Initialized:     true,
					Label:           "splunk-test-search-head-0",
					MinPeersJoined:  true,
					MaintenanceMode: false,
				},
			},
			{
				Content: splclient.SearchHeadCaptainInfo{
					ServiceReady:    true,
					Identifier:      "1",
					ElectedCaptain:  1,
					Initialized:     true,
					Label:           "splunk-test-search-head-1",
					MinPeersJoined:  true,
					MaintenanceMode: false,
				},
			},
			{
				Content: splclient.SearchHeadCaptainInfo{
					ServiceReady:    true,
					Identifier:      "1",
					ElectedCaptain:  1,
					Initialized:     true,
					Label:           "splunk-test-search-head-2",
					MinPeersJoined:  true,
					MaintenanceMode: false,
				},
			},
		},
	}

	// mock search head cluster calls
	response1, _ := json.Marshal(apiResponse1)
	response2, _ := json.Marshal(apiResponse2)
	response3, _ := json.Marshal(apiResponse3)
	wantRequest1, _ := http.NewRequest("GET", "https://splunk-test-search-head-0.splunk-test-search-head-headless.default.svc.cluster.local:8089/services/shcluster/member/info?count=0&output_mode=json", nil)
	wantRequest2, _ := http.NewRequest("GET", "https://splunk-test-search-head-0.splunk-test-search-head-headless.default.svc.cluster.local:8089/services/shcluster/member/peers?count=0&output_mode=json", nil)
	wantRequest3, _ := http.NewRequest("GET", "https://splunk-test-search-head-0.splunk-test-search-head-headless.default.svc.cluster.local:8089/services/shcluster/captain/info?count=0&output_mode=json", nil)

	wantRequest4, _ := http.NewRequest("GET", "https://splunk-test-search-head-1.splunk-test-search-head-headless.default.svc.cluster.local:8089/services/shcluster/member/info?count=0&output_mode=json", nil)
	wantRequest5, _ := http.NewRequest("GET", "https://splunk-test-search-head-1.splunk-test-search-head-headless.default.svc.cluster.local:8089/services/shcluster/member/peers?count=0&output_mode=json", nil)
	wantRequest6, _ := http.NewRequest("GET", "https://splunk-test-search-head-1.splunk-test-search-head-headless.default.svc.cluster.local:8089/services/shcluster/captain/info?count=0&output_mode=json", nil)

	wantRequest7, _ := http.NewRequest("GET", "https://splunk-test-search-head-2.splunk-test-search-head-headless.default.svc.cluster.local:8089/services/shcluster/member/info?count=0&output_mode=json", nil)
	wantRequest8, _ := http.NewRequest("GET", "https://splunk-test-search-head-2.splunk-test-search-head-headless.default.svc.cluster.local:8089/services/shcluster/member/peers?count=0&output_mode=json", nil)
	wantRequest9, _ := http.NewRequest("GET", "https://splunk-test-search-head-2.splunk-test-search-head-headless.default.svc.cluster.local:8089/services/shcluster/captain/info?count=0&output_mode=json", nil)

	mclient.AddHandler(wantRequest1, 200, string(response2), nil)
	mclient.AddHandler(wantRequest2, 200, string(response1), nil)
	mclient.AddHandler(wantRequest3, 200, string(response3), nil)

	mclient.AddHandler(wantRequest4, 200, string(response2), nil)
	mclient.AddHandler(wantRequest5, 200, string(response1), nil)
	mclient.AddHandler(wantRequest6, 200, string(response3), nil)

	mclient.AddHandler(wantRequest7, 200, string(response2), nil)
	mclient.AddHandler(wantRequest8, 200, string(response1), nil)
	mclient.AddHandler(wantRequest9, 200, string(response3), nil)

	// mock new search pod manager
	shcworkflow.NewPodManager = func(client splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster, secret *corev1.Secret, newSplunkClient NewSplunkClientFunc, operations shcworkflow.Operations) shcworkflow.PodManager {
		mgr := shcworkflow.PodManager{
			CR:         cr,
			Secrets:    secret,
			Operations: &operations,
			NewSplunkClient: func(managementURI, username, password string) *splclient.SplunkClient {
				c := splclient.NewSplunkClient(managementURI, username, password)
				c.Client = mclient
				return c
			},
		}
		return mgr
	}

	// create directory for app framework
	newpath := filepath.Join("/tmp", "appframework")
	_ = os.MkdirAll(newpath, os.ModePerm)

	// adding getapplist to fix test case
	GetAppsList = func(ctx context.Context, remoteDataClientMgr RemoteDataClientManager) (splcommon.RemoteDataListResponse, error) {
		RemoteDataListResponse := splcommon.RemoteDataListResponse{}
		return RemoteDataListResponse, nil
	}

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	builder := newFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.LicenseManager{}).
		WithStatusSubresource(&enterpriseApi.ClusterManager{}).
		WithStatusSubresource(&enterpriseApi.Standalone{}).
		WithStatusSubresource(&enterpriseApi.MonitoringConsole{}).
		WithStatusSubresource(&enterpriseApi.IndexerCluster{}).
		WithStatusSubresource(&enterpriseApi.SearchHeadCluster{})
	c := builder.Build()
	ctx := context.TODO()

	// create searchheadcluster custom resource
	searchheadcluster := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				Volumes: []corev1.Volume{},
			},
			Replicas: 3,
		},
	}

	replicas := int32(3)
	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-search-head",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			PodManagementPolicy: "Parallel",
			ServiceName:         "splunk-test-deployer-headless",
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
							LivenessProbe: &corev1.Probe{
								InitialDelaySeconds: 300,
							},
							ReadinessProbe: &corev1.Probe{
								InitialDelaySeconds: 300,
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
			Name:      "splunk-test-deployer-headless",
			Namespace: "default",
		},
	}

	// simulate service
	c.Create(ctx, service)

	// simulate create stateful set
	c.Create(ctx, statefulset)

	// simulate create clustermanager instance before reconciliation
	c.Create(ctx, searchheadcluster)

	_, err := ApplySearchHeadCluster(ctx, c, searchheadcluster)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for searchhead cluster %v", err)
		debug.PrintStack()
	}

	namespacedName := types.NamespacedName{
		Name:      searchheadcluster.Name,
		Namespace: searchheadcluster.Namespace,
	}
	err = c.Get(ctx, namespacedName, searchheadcluster)
	if err != nil {
		t.Errorf("Unexpected get search head cluster. Error=%v", err)
		debug.PrintStack()
	}
	// simulate Ready state
	searchheadcluster.Status.Phase = enterpriseApi.PhaseReady
	searchheadcluster.Spec.ServiceTemplate.Annotations = map[string]string{
		"traffic.sidecar.istio.io/excludeOutboundPorts": "8089,8191,9997",
		"traffic.sidecar.istio.io/includeInboundPorts":  "8000,8088",
	}
	searchheadcluster.Spec.ServiceTemplate.Labels = map[string]string{
		"app.kubernetes.io/instance":   "splunk-test-searchhead-cluster",
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "searchhead-cluster",
		"app.kubernetes.io/name":       "search-cluster",
		"app.kubernetes.io/part-of":    "splunk-test-searchead-cluster",
	}
	err = c.Status().Update(ctx, searchheadcluster)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for searchhead cluster with app framework  %v", err)
		debug.PrintStack()
	}

	err = c.Get(ctx, namespacedName, searchheadcluster)
	if err != nil {
		t.Errorf("Unexpected get search head cluster %v", err)
		debug.PrintStack()
	}

	// call reconciliation
	_, err = ApplySearchHeadCluster(ctx, c, searchheadcluster)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for searchead cluster with app framework  %v", err)
		debug.PrintStack()
	}

	// create pod
	stpod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-search-head-0",
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
					LivenessProbe: &corev1.Probe{
						InitialDelaySeconds: 300,
					},
					ReadinessProbe: &corev1.Probe{
						InitialDelaySeconds: 300,
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

	// create pod
	stpod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-search-head-1",
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
					LivenessProbe: &corev1.Probe{
						InitialDelaySeconds: 300,
					},
					ReadinessProbe: &corev1.Probe{
						InitialDelaySeconds: 300,
					},
				},
			},
		},
	}
	// simulate create stateful set
	c.Create(ctx, stpod1)
	if err != nil {
		t.Errorf("Unexpected create pod failed %v", err)
		debug.PrintStack()
	}

	// create pod
	stpod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-search-head-2",
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
					LivenessProbe: &corev1.Probe{
						InitialDelaySeconds: 300,
					},
					ReadinessProbe: &corev1.Probe{
						InitialDelaySeconds: 300,
					},
				},
			},
		},
	}
	// simulate create stateful set
	c.Create(ctx, stpod2)
	if err != nil {
		t.Errorf("Unexpected create pod failed %v", err)
		debug.PrintStack()
	}

	// create pod
	stpoddeployer := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-deployer-0",
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
					LivenessProbe: &corev1.Probe{
						InitialDelaySeconds: 300,
					},
					ReadinessProbe: &corev1.Probe{
						InitialDelaySeconds: 300,
					},
				},
			},
		},
	}
	// simulate create stateful set
	c.Create(ctx, stpoddeployer)
	if err != nil {
		t.Errorf("Unexpected create pod failed %v", err)
		debug.PrintStack()
	}

	// update stateful set pod
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
		t.Errorf("Unexpected update pod  %v", err)
		debug.PrintStack()
	}

	// update stateful set pod
	stpod1.Status.Phase = corev1.PodRunning
	stpod1.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Image: "splunk/splunk:latest",
			Name:  "splunk",
			Ready: true,
		},
	}
	err = c.Status().Update(ctx, stpod1)
	if err != nil {
		t.Errorf("Unexpected update pod  %v", err)
		debug.PrintStack()
	}

	// update stateful set pod
	stpod2.Status.Phase = corev1.PodRunning
	stpod2.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Image: "splunk/splunk:latest",
			Name:  "splunk",
			Ready: true,
		},
	}
	err = c.Status().Update(ctx, stpod2)
	if err != nil {
		t.Errorf("Unexpected update pod  %v", err)
		debug.PrintStack()
	}

	// update statefulset
	stpoddeployer.Status.Phase = corev1.PodRunning
	stpoddeployer.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Image: "splunk/splunk:latest",
			Name:  "splunk",
			Ready: true,
		},
	}
	err = c.Status().Update(ctx, stpoddeployer)
	if err != nil {
		t.Errorf("Unexpected update statefulset  %v", err)
		debug.PrintStack()
	}

	stNamespacedName := types.NamespacedName{
		Name:      "splunk-test-search-head",
		Namespace: "default",
	}
	err = c.Get(ctx, stNamespacedName, statefulset)
	if err != nil {
		t.Errorf("Unexpected get searchhead cluster %v", err)
		debug.PrintStack()
	}

	// update statefulset
	statefulset.Status.ReadyReplicas = 3
	statefulset.Status.Replicas = 3
	err = c.Status().Update(ctx, statefulset)
	if err != nil {
		t.Errorf("Unexpected update statefulset  %v", err)
		debug.PrintStack()
	}

	// update statefulset for deployer

	stNamespacedName = types.NamespacedName{
		Name:      "splunk-test-deployer",
		Namespace: "default",
	}
	err = c.Get(ctx, stNamespacedName, statefulset)
	if err != nil {
		t.Errorf("Unexpected get searchhead cluster %v", err)
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

	err = c.Get(ctx, namespacedName, searchheadcluster)
	if err != nil {
		t.Errorf("Unexpected get searchhead cluster %v", err)
		debug.PrintStack()
	}

	searchheadcluster.Status.Initialized = true
	searchheadcluster.Status.CaptainReady = true
	searchheadcluster.Status.ReadyReplicas = 3
	searchheadcluster.Status.Replicas = 3

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
	err = k8sops.SetStatefulSetOwnerRef(ctx, c, searchheadcluster, namespacedName)
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
	_, err = ApplySearchHeadCluster(ctx, c, searchheadcluster)
	if err != nil {
		t.Errorf("Unexpected error while running reconciliation for search head cluster with app framework. Error=%v", err)
	}
}

func TestSetDeployerConfig(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	client := spltest.NewMockClient()
	depResSpec := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("4"),
			corev1.ResourceMemory: resource.MustParse("14Gi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2"),
			corev1.ResourceMemory: resource.MustParse("7Gi"),
		},
	}

	shc := enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			DeployerResourceSpec: depResSpec,
			DeployerNodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{},
				},
			},
		},
	}

	nsTerm := corev1.NodeSelectorTerm{
		MatchExpressions: []corev1.NodeSelectorRequirement{
			{
				Key: "node-role.kubernetes.io/master",
			},
		},
	}
	shc.Spec.DeployerNodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = append(shc.Spec.DeployerNodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms, nsTerm)

	// Get deployer STS and set resources
	depSts, err := getSplunkStatefulSet(ctx, client, &shc, &shc.Spec.CommonSplunkSpec, SplunkDeployer, 1, resources.GetSearchHeadExtraEnv(&shc, shc.Spec.Replicas), nil)
	if err != nil {
		t.Errorf("Failed to get deployer statefulset due to error=%s", err)
	}
	setDeployerConfig(ctx, &shc, &depSts.Spec.Template)
	if !reflect.DeepEqual(depResSpec.Limits, depSts.Spec.Template.Spec.Containers[0].Resources.Limits) {
		t.Errorf("Failed to set deployer resources properly, limits are off")
	}

	// Verify deployer resources are set properly
	if !reflect.DeepEqual(depResSpec.Requests, depSts.Spec.Template.Spec.Containers[0].Resources.Requests) {
		t.Errorf("Failed to set deployer resources properly, requests are off")
	}

	// Verify deployer nodeAffinity are set properly
	if !reflect.DeepEqual(shc.Spec.DeployerNodeAffinity, depSts.Spec.Template.Spec.Affinity.NodeAffinity) {
		t.Errorf("Failed to set deployer resources properly, requests are off")
	}
}
