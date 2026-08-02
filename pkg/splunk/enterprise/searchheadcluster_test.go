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
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/splkcontroller"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

func init() {
	// Re-Assigning GetReadinessScriptLocation, GetLivenessScriptLocation, GetStartupScriptLocation to use absolute path for readinessScriptLocation, readinessScriptLocation
	GetReadinessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../" + readinessScriptLocation)
		return fileLocation
	}
	GetLivenessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../" + livenessScriptLocation)
		return fileLocation
	}
	GetStartupScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../" + startupScriptLocation)
		return fileLocation
	}
}

func TestValidateSHCDefaultsRestartSafetyFromObservedState(t *testing.T) {
	const (
		unsafeThree = `splunk:
  conf:
    server:
      content:
        shclustering:
          replication_factor: 3
`
		unsafeFive = `splunk:
  conf:
    server:
      content:
        shclustering:
          replication_factor: 5
`
		allowed = `splunk:
  conf:
    server:
      content:
        shclustering:
          shcluster_label: production
`
	)

	scheme := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(enterpriseApi.AddToScheme(scheme))

	newCR := func(defaults string) *enterpriseApi.SearchHeadCluster {
		return &enterpriseApi.SearchHeadCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example",
				Namespace: "test",
			},
			Spec: enterpriseApi.SearchHeadClusterSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Defaults: defaults,
				},
				Replicas: 3,
			},
		}
	}
	defaultsConfigMap := func(defaults string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: GetSplunkDefaultsName(
					"example",
					SplunkSearchHead,
				),
				Namespace: "test",
			},
			Data: map[string]string{"default.yml": defaults},
		}
	}
	searchHeadStatefulSet := func() *appsv1.StatefulSet {
		return &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: GetSplunkStatefulsetName(
					SplunkSearchHead,
					"example",
				),
				Namespace: "test",
			},
		}
	}

	tests := []struct {
		name      string
		defaults  string
		objects   []client.Object
		wantError bool
	}{
		{
			name:     "initial create may establish cluster settings",
			defaults: unsafeThree,
		},
		{
			name:     "unchanged existing cluster setting",
			defaults: unsafeThree,
			objects: []client.Object{
				defaultsConfigMap(unsafeThree),
			},
		},
		{
			name:     "rolling compatible setting",
			defaults: allowed,
			objects: []client.Object{
				defaultsConfigMap(""),
			},
		},
		{
			name:      "existing ConfigMap reveals unsafe change",
			defaults:  unsafeFive,
			objects:   []client.Object{defaultsConfigMap(unsafeThree)},
			wantError: true,
		},
		{
			name:      "existing StatefulSet makes missing ConfigMap an update",
			defaults:  unsafeThree,
			objects:   []client.Object{searchHeadStatefulSet()},
			wantError: true,
		},
		{
			name:     "malformed previous defaults fail closed",
			defaults: allowed,
			objects: []client.Object{
				defaultsConfigMap("splunk: ["),
			},
			wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controllerClient := newFakeClientBuilder(scheme).
				WithObjects(test.objects...).
				Build()
			err := validateSHCDefaultsRestartSafety(
				context.Background(),
				controllerClient,
				newCR(test.defaults),
			)
			if (err != nil) != test.wantError {
				t.Fatalf("validation error = %v, wantError=%t", err, test.wantError)
			}
			if err != nil &&
				(strings.Contains(err.Error(), test.defaults) ||
					strings.Contains(err.Error(), unsafeThree)) {
				t.Fatalf("validation error exposed defaults content: %v", err)
			}
		})
	}
}

func TestApplySearchHeadClusterBlocksUnsafeDefaultsBeforeConfigMutation(
	t *testing.T,
) {
	os.Setenv(
		"SPLUNK_GENERAL_TERMS",
		"--accept-sgt-current-at-splunk-com",
	)
	const previousDefaults = `splunk:
  conf:
    server:
      content:
        shclustering:
          replication_factor: 3
`
	const requestedDefaults = `splunk:
  conf:
    server:
      content:
        shclustering:
          replication_factor: 5
`

	scheme := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(enterpriseApi.AddToScheme(scheme))

	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "test",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Defaults: requestedDefaults,
			},
			Replicas: 3,
		},
	}
	defaultsName := GetSplunkDefaultsName(
		cr.GetName(),
		SplunkSearchHead,
	)
	defaultsConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultsName,
			Namespace: cr.GetNamespace(),
		},
		Data: map[string]string{"default.yml": previousDefaults},
	}
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: GetSplunkStatefulsetName(
				SplunkSearchHead,
				cr.GetName(),
			),
			Namespace: cr.GetNamespace(),
		},
	}
	controllerClient := newFakeClientBuilder(scheme).
		WithStatusSubresource(&enterpriseApi.SearchHeadCluster{}).
		WithObjects(cr, defaultsConfigMap, statefulSet).
		Build()

	_, err := ApplySearchHeadCluster(
		context.Background(),
		controllerClient,
		cr.DeepCopy(),
	)
	reason, terminal := splcommon.TerminalReason(err)
	if !terminal || reason != EventReasonValidateSpecFailed {
		t.Fatalf(
			"reconcile error=%v reason=%q terminal=%t",
			err,
			reason,
			terminal,
		)
	}

	storedDefaults := &corev1.ConfigMap{}
	if err := controllerClient.Get(
		context.Background(),
		types.NamespacedName{
			Name:      defaultsName,
			Namespace: cr.GetNamespace(),
		},
		storedDefaults,
	); err != nil {
		t.Fatalf("get defaults ConfigMap: %v", err)
	}
	if storedDefaults.Data["default.yml"] != previousDefaults {
		t.Fatalf(
			"unsafe reconcile changed defaults ConfigMap: %q",
			storedDefaults.Data["default.yml"],
		)
	}
	service := &corev1.Service{}
	err = controllerClient.Get(
		context.Background(),
		types.NamespacedName{
			Name: splcommon.GetSplunkServiceName(
				SplunkSearchHead,
				cr.GetName(),
				false,
			),
			Namespace: cr.GetNamespace(),
		},
		service,
	)
	if !k8serrors.IsNotFound(err) {
		t.Fatalf(
			"Search Head service lookup error=%v, want not found",
			err,
		)
	}
}

func TestApplySearchHeadCluster(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	restartSafetyGetCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-search-head-defaults"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},
	}
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

	createCalls := map[string][]spltest.MockFuncCall{"Get": append(restartSafetyGetCalls, funcCalls...), "Create": {funcCalls[0], funcCalls[3], funcCalls[4], funcCalls[5], funcCalls[6], funcCalls[10], funcCalls[12], funcCalls[13], funcCalls[17], funcCalls[19]}, "Update": {funcCalls[0]}, "List": {listmockCall[0], listmockCall[0], shcPodListMockCall}}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": append(restartSafetyGetCalls, createFuncCalls...), "Update": {createFuncCalls[6], createFuncCalls[18]}, "List": {listmockCall[0], listmockCall[0], shcPodListMockCall}}
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

func searchHeadClusterPodManagerTester(t *testing.T, method string, mockHandlers []spltest.MockHTTPHandler,
	desiredReplicas int32, wantPhase enterpriseApi.Phase, statefulSet *appsv1.StatefulSet,
	wantCalls map[string][]spltest.MockFuncCall, wantError error, initObjects ...client.Object) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	// test for updating
	cr := enterpriseApi.SearchHeadCluster{
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
		cr.Status.ShcSecretChanged = append(cr.Status.ShcSecretChanged, true)
	}
	secrets := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"password": {'1', '2', '3'},
		},
	}
	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)

	mgr := &searchHeadClusterPodManager{
		cr:      &cr,
		secrets: secrets,
		newSplunkClient: func(managementURI, username, password string) *splclient.SplunkClient {
			c := splclient.NewSplunkClient(managementURI, username, password)
			c.Client = mockSplunkClient
			c.SearchHeadClusterUpgradeClient = mockSplunkClient
			return c
		},
	}
	spltest.PodManagerUpdateTester(t, method, mgr, desiredReplicas, wantPhase, statefulSet, wantCalls, wantError, initObjects...)
	mockSplunkClient.CheckRequests(t, method)
}

func TestSearchHeadClusterPodManager(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	var replicas int32 = 1
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{ObjectMeta: metav1.ObjectMeta{Name: "pvc-etc", Namespace: "test"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pvc-var", Namespace: "test"}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:        replicas,
			ReadyReplicas:   replicas,
			UpdatedReplicas: replicas,
			UpdateRevision:  "v1",
		},
	}
	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/member/info?count=0&output_mode=json",
			Status: 500,
			Err:    nil,
			Body:   ``,
		},
	}
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.StatefulSet-test-splunk-stack1"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-0"},
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-1"},
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-1"},
		{MetaName: "*v1.Pod-test-splunk-stack1-0"},
		{MetaName: "*v1.Pod-test-splunk-stack1-1"},
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

	wantCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2]}, "Create": {funcCalls[1]}}

	// test API failure
	method := "searchHeadClusterPodManager.Update(API failure)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhasePending, statefulSet, wantCalls, nil, statefulSet)

	// captain not ready (e.g. mid fleet-recycle captain election) but a scale up is
	// underway -> report ScalingUp instead of masking it behind Pending
	method = "searchHeadClusterPodManager.Update(API failure, scaling up)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 2, enterpriseApi.PhaseScalingUp, statefulSet, wantCalls, nil, statefulSet)

	// captain not ready but a scale down is underway -> report ScalingDown instead
	// of masking it behind Pending
	method = "searchHeadClusterPodManager.Update(API failure, scaling down)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 0, enterpriseApi.PhaseScalingDown, statefulSet, wantCalls, nil, statefulSet)

	// test 1 ready pod
	mockHandlers = []spltest.MockHTTPHandler{
		{
			Method: "GET",
			URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/member/info?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   loadFixture(t, "shc_member_info_response.json"),
		}, {
			Method: "GET",
			URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/captain/info?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   loadFixture(t, "shc_captain_info_response.json"),
		}, {
			Method: "GET",
			URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/captain/members?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body: `{"entry":[{
				"name":"member-0",
				"content":{
					"label":"splunk-stack1-search-head-0",
					"status":"Up",
					"advertise_restart_required":false
				}
			}]}`,
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-0",
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
	method = "searchHeadClusterPodManager.Update(All pods ready)"
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2], funcCalls[2], funcCalls[2], funcCalls[0], funcCalls[5]}, "Create": {funcCalls[1]}, "List": {listmockCall[0]}}
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhaseReady, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => transition to detention
	mockHandlers = append(mockHandlers, spltest.MockHTTPHandler{
		Method: "POST",
		URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/captain/control/control/upgrade-init",
		Status: 200,
		Err:    nil,
		Body:   ``,
	}, spltest.MockHTTPHandler{
		Method: "POST",
		URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/member/control/control/set_manual_detention?manual_detention=on",
		Status: 200,
		Err:    nil,
		Body:   ``,
	},
	)
	pod.ObjectMeta.Labels["controller-revision-hash"] = "v0"
	method = "searchHeadClusterPodManager.Update(Quarantine Pod)"
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2], funcCalls[2], funcCalls[2], funcCalls[0], funcCalls[5], funcCalls[2], funcCalls[2]}, "Create": {funcCalls[1]}}
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => wait for searches to drain
	mockHandlers = []spltest.MockHTTPHandler{
		mockHandlers[0],
		mockHandlers[1],
		mockHandlers[2],
	}
	mockHandlers[0].Body = strings.Replace(mockHandlers[0].Body, `"status":"Up"`, `"status":"ManualDetention"`, 1)
	mockHandlers[0].Body = strings.Replace(mockHandlers[0].Body, `"active_historical_search_count":0`, `"active_historical_search_count":1`, 1)
	method = "searchHeadClusterPodManager.Update(Draining Searches)"
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2], funcCalls[2], funcCalls[2], funcCalls[0], funcCalls[5]}, "Create": {funcCalls[1]}}
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => delete pod
	mockHandlers[0].Body = strings.Replace(mockHandlers[0].Body, `"active_historical_search_count":1`, `"active_historical_search_count":0`, 1)
	method = "searchHeadClusterPodManager.Update(Delete Pod)"
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2], funcCalls[2], funcCalls[2], funcCalls[0], funcCalls[5]}, "Create": {funcCalls[1]}, "Delete": {funcCalls[5]}}
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod update finished => release from detention
	pod.ObjectMeta.Labels["controller-revision-hash"] = "v1"
	mockHandlers = append(mockHandlers, spltest.MockHTTPHandler{
		Method: "POST",
		URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/member/control/control/set_manual_detention?manual_detention=off",
		Status: 200,
		Err:    nil,
		Body:   ``,
	})
	method = "searchHeadClusterPodManager.Update(Release Quarantine)"
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2], funcCalls[2], funcCalls[2], funcCalls[0], funcCalls[5], funcCalls[2]}, "Create": {funcCalls[1]}}
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test scale down => remove member
	mockHandlers = []spltest.MockHTTPHandler{
		mockHandlers[0],
		mockHandlers[1],
		{
			Method: "GET",
			URL:    "https://splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/member/info?count=0&output_mode=json",
			Status: 200,
			Err:    nil,
			Body:   loadFixture(t, "shc_member_remove_response.json"),
		},
		mockHandlers[2],
	}
	mockHandlers = append(mockHandlers, spltest.MockHTTPHandler{
		Method: "POST",
		URL:    "https://splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/member/consensus/default/remove_server?output_mode=json",
		Status: 200,
		Err:    nil,
		Body:   ``,
	})
	pvcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.PersistentVolumeClaim-test-pvc-etc-splunk-stack1-1"},
		{MetaName: "*v1.PersistentVolumeClaim-test-pvc-var-splunk-stack1-1"},
	}

	updateFuncCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.StatefulSet-test-splunk-stack1"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-0"},
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-0"},
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-1"},
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-0"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1"},
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-1"},
		{MetaName: "*v1.PersistentVolumeClaim-test-pvc-etc-splunk-stack1-1"},
		{MetaName: "*v1.PersistentVolumeClaim-test-pvc-var-splunk-stack1-1"},
	}

	wantCalls = map[string][]spltest.MockFuncCall{"Get": updateFuncCalls, "Delete": pvcCalls, "Update": {funcCalls[0]}, "Create": {funcCalls[1]}}
	pvcList := []*corev1.PersistentVolumeClaim{
		{ObjectMeta: metav1.ObjectMeta{Name: "pvc-etc-splunk-stack1-1", Namespace: "test"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "pvc-var-splunk-stack1-1", Namespace: "test"}},
	}
	pod.ObjectMeta.Name = "splunk-stack1-0"
	replicas = 2
	statefulSet.Status.Replicas = 2
	statefulSet.Status.ReadyReplicas = 2
	statefulSet.Status.UpdatedReplicas = 2
	method = "searchHeadClusterPodManager.Update(Remove Member)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhaseScalingDown, statefulSet, wantCalls, nil, statefulSet, pod, pvcList[0], pvcList[1])

}

func TestFinishRecycle(t *testing.T) {
	ctx := context.TODO()
	mgr := &searchHeadClusterPodManager{
		cr: &enterpriseApi.SearchHeadCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "stack1", Namespace: "test"},
		},
	}

	// member is up, not in detention -> recycle is complete
	mgr.cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{{Status: "Up"}}
	complete, err := mgr.FinishRecycle(ctx, 0)
	if err != nil || !complete {
		t.Errorf("FinishRecycle(Up) = %v, %v; want true, nil", complete, err)
	}

	// member info was transiently unavailable (e.g. pod mid-restart) -> wait, don't error
	mgr.cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{{Status: ""}}
	complete, err = mgr.FinishRecycle(ctx, 0)
	if err != nil || complete {
		t.Errorf("FinishRecycle(empty status) = %v, %v; want false, nil", complete, err)
	}

	// any other unrecognized status is still a hard error
	mgr.cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{{Status: "Down"}}
	complete, err = mgr.FinishRecycle(ctx, 0)
	if err == nil || complete {
		t.Errorf("FinishRecycle(Down) = %v, %v; want false, error", complete, err)
	}
}

func TestApplyShcSecret(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.Background()
	method := "ApplyShcSecret"
	c := spltest.NewMockClient()
	nsSecret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Fatalf("apply namespace scoped secret: %v", err)
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-search-head-0",
			Namespace: "test",
			Labels: map[string]string{
				"controller-revision-hash": "v0",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					VolumeMounts: []corev1.VolumeMount{
						{
							MountPath: "/mnt/splunk-secrets",
							Name:      "mnt-splunk-secrets",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "mnt-splunk-secrets",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "stack1-secrets",
						},
					},
				},
			},
		},
	}
	podSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"password":   []byte("old-admin-password"),
			"shc_secret": append([]byte(nil), nsSecret.Data["shc_secret"]...),
		},
	}
	c.AddObjects([]client.Object{pod, podSecret})

	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(spltest.MockHTTPHandler{
		Method: "POST",
		URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/server/control/restart",
		Status: 200,
	})
	cr := &enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Status: enterpriseApi.SearchHeadClusterStatus{
			NamespaceSecretResourceVersion: "stale",
			AdminPasswordChangedSecrets:    make(map[string]bool),
		},
	}
	mgr := &searchHeadClusterPodManager{
		c:       c,
		cr:      cr,
		secrets: podSecret,
		newSplunkClient: func(
			managementURI,
			username,
			password string,
		) *splclient.SplunkClient {
			result := splclient.NewSplunkClient(
				managementURI,
				username,
				password,
			)
			result.Client = mockSplunkClient
			result.SearchHeadClusterUpgradeClient = mockSplunkClient
			return result
		},
	}
	mockPodExecClient := &spltest.MockPodExecClient{}
	mockPodExecClient.AddMockPodExecReturnContext(
		ctx,
		"opt/splunk/bin/splunk cmd splunkd rest",
		&spltest.MockPodExecReturnContext{},
	)

	if err := ApplyShcSecret(
		ctx,
		mgr,
		1,
		mockPodExecClient,
	); err != nil {
		t.Fatalf("apply admin password update: %v", err)
	}
	mockPodExecClient.CheckPodExecCommands(t, method)
	mockSplunkClient.CheckRequests(t, method)
	if !reflect.DeepEqual(
		mgr.cr.Status.AdminSecretChanged,
		[]bool{true},
	) ||
		!mgr.cr.Status.AdminPasswordChangedSecrets[podSecret.GetName()] {
		t.Fatalf("admin sync status = %#v", mgr.cr.Status)
	}
	storedSecret := &corev1.Secret{}
	if err := c.Get(
		ctx,
		types.NamespacedName{
			Name:      podSecret.GetName(),
			Namespace: podSecret.GetNamespace(),
		},
		storedSecret,
	); err != nil {
		t.Fatalf("get synchronized Pod secret: %v", err)
	}
	if !reflect.DeepEqual(
		storedSecret.Data["password"],
		nsSecret.Data["password"],
	) {
		t.Fatal("mounted Pod secret did not receive namespace admin password")
	}
}

func TestApplyShcSecretBlocksRotationBeforeMutation(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.Background()
	c := spltest.NewMockClient()
	nsSecret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Fatalf("apply namespace scoped secret: %v", err)
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-search-head-0",
			Namespace: "test",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				VolumeMounts: []corev1.VolumeMount{{
					MountPath: "/mnt/splunk-secrets",
					Name:      "mnt-splunk-secrets",
				}},
			}},
			Volumes: []corev1.Volume{{
				Name: "mnt-splunk-secrets",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: "stack1-secrets",
					},
				},
			}},
		},
	}
	podSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"password":   []byte("old-admin-password"),
			"shc_secret": append([]byte(nil), nsSecret.Data["shc_secret"]...),
		},
	}
	secondPod := pod.DeepCopy()
	secondPod.Name = "splunk-stack1-search-head-1"
	secondPod.Spec.Volumes[0].Secret.SecretName = "stack1-secrets-1"
	secondPodSecret := podSecret.DeepCopy()
	secondPodSecret.Name = "stack1-secrets-1"
	secondPodSecret.Data["shc_secret"] = []byte("old-shc-secret")
	c.AddObjects([]client.Object{
		pod,
		podSecret,
		secondPod,
		secondPodSecret,
	})

	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Status: enterpriseApi.SearchHeadClusterStatus{
			NamespaceSecretResourceVersion: "stale",
			AdminPasswordChangedSecrets:    make(map[string]bool),
		},
	}
	mgr := &searchHeadClusterPodManager{c: c, cr: cr, secrets: podSecret}
	recorder := &mockEventRecorder{}
	ctx = context.WithValue(
		ctx,
		splcommon.EventPublisherKey,
		&K8EventPublisher{recorder: recorder},
	)
	mockPodExecClient := &spltest.MockPodExecClient{}

	err = ApplyShcSecret(ctx, mgr, 2, mockPodExecClient)
	if err == nil ||
		!strings.Contains(err.Error(), "approximately simultaneous restart") {
		t.Fatalf("rotation error = %v", err)
	}
	if len(mockPodExecClient.GotCmdList) != 0 ||
		len(mgr.cr.Status.AdminSecretChanged) != 0 ||
		len(mgr.cr.Status.AdminPasswordChangedSecrets) != 0 ||
		mgr.cr.Status.NamespaceSecretResourceVersion != "stale" ||
		!strings.HasPrefix(
			mgr.cr.Status.Message,
			"SHCSecretRotationBlocked:",
		) {
		t.Fatalf(
			"blocked rotation mutated state commands=%v status=%#v",
			mockPodExecClient.GotCmdList,
			mgr.cr.Status,
		)
	}
	storedSecret := &corev1.Secret{}
	if err := c.Get(
		ctx,
		types.NamespacedName{
			Name:      podSecret.GetName(),
			Namespace: podSecret.GetNamespace(),
		},
		storedSecret,
	); err != nil {
		t.Fatalf("get blocked Pod secret: %v", err)
	}
	if !reflect.DeepEqual(
		storedSecret.Data["shc_secret"],
		nsSecret.Data["shc_secret"],
	) ||
		string(storedSecret.Data["password"]) != "old-admin-password" {
		t.Fatalf("blocked rotation changed an earlier Pod secret")
	}
	if err := c.Get(
		ctx,
		types.NamespacedName{
			Name:      secondPodSecret.GetName(),
			Namespace: secondPodSecret.GetNamespace(),
		},
		storedSecret,
	); err != nil {
		t.Fatalf("get mismatched Pod secret: %v", err)
	}
	if string(storedSecret.Data["shc_secret"]) != "old-shc-secret" {
		t.Fatal("blocked rotation changed the mismatched Pod secret")
	}
	foundBlockedEvent := false
	for _, event := range recorder.events {
		if event.reason == EventReasonSHCSecretRotationBlocked {
			foundBlockedEvent = true
		}
	}
	if !foundBlockedEvent {
		t.Fatal("blocked rotation did not emit SHCSecretRotationBlocked")
	}
	if strings.Contains(mgr.cr.Status.Message, string(nsSecret.Data["shc_secret"])) ||
		strings.Contains(err.Error(), string(nsSecret.Data["shc_secret"])) {
		t.Fatal("blocked rotation exposed namespace shc_secret")
	}
}

// TestApplyShcSecretAdminPasswordNotStarvedByShcSecretAlreadyChanged is a regression test:
// when a pod's shc_secret was already marked as synced in a prior reconcile, its
// independent admin-password check must still run instead of being skipped.
func TestApplyShcSecretAdminPasswordNotStarvedByShcSecretAlreadyChanged(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	var initObjectList []client.Object

	c := spltest.NewMockClient()

	nsSecret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Errorf("Apply namespace scoped secret failed")
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-search-head-0",
			Namespace: "test",
			Labels: map[string]string{
				"controller-revision-hash": "v0",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					VolumeMounts: []corev1.VolumeMount{
						{
							MountPath: "/mnt/splunk-secrets",
							Name:      "mnt-splunk-secrets",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "mnt-splunk-secrets",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "stack1-secrets",
						},
					},
				},
			},
		},
	}
	initObjectList = append(initObjectList, pod)

	// Pod's mounted secret already matches the namespace shc_secret, but its
	// admin password does not -- the shc_secret branch must not be entered
	// (and thus won't short-circuit via "continue"), while the admin password
	// branch below still needs to run regardless.
	secrets := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"password":   []byte("old-admin-password"),
			"shc_secret": append([]byte(nil), nsSecret.Data["shc_secret"]...),
		},
	}
	initObjectList = append(initObjectList, secrets)

	c.AddObjects(initObjectList)

	mockHandlers := []spltest.MockHTTPHandler{
		{
			Method: "POST",
			URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/server/control/restart",
			Status: 200,
			Err:    nil,
		},
	}

	cr := enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	cr.Status.AdminPasswordChangedSecrets = make(map[string]bool)
	// Simulate a prior reconcile that already synced shc_secret for pod 0.
	cr.Status.ShcSecretChanged = []bool{true}
	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)
	mgr := &searchHeadClusterPodManager{
		c:       c,
		cr:      &cr,
		secrets: secrets,
		newSplunkClient: func(managementURI, username, password string) *splclient.SplunkClient {
			c := splclient.NewSplunkClient(managementURI, username, password)
			c.Client = mockSplunkClient
			c.SearchHeadClusterUpgradeClient = mockSplunkClient
			return c
		},
	}

	podExecCommands := []string{
		"opt/splunk/bin/splunk cmd splunkd rest",
	}
	mockPodExecReturnContexts := []*spltest.MockPodExecReturnContext{
		{
			StdOut: "",
			StdErr: "",
			Err:    nil,
		},
	}

	var mockPodExecClient *spltest.MockPodExecClient = &spltest.MockPodExecClient{}
	mockPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnContexts...)

	// Namespace shc_secret already matches the pod's, so the
	// admin-password mismatch is the only thing that should trigger a sync.
	// Bump the resource version so ApplyShcSecret doesn't early-return.
	mgr.cr.Status.NamespaceSecretResourceVersion = "0"

	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}

	if !mgr.cr.Status.AdminSecretChanged[0] {
		t.Errorf("Admin password sync was skipped for pod 0 even though shc_secret was already in sync")
	}
	if len(mockPodExecClient.GotCmdList) == 0 {
		t.Errorf("Expected admin password change command to be executed, but none was")
	}
}

func TestShcPasswordSyncCompleted(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	builder := newFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.SearchHeadCluster{})

	client := builder.Build()
	ctx := context.TODO()

	// Create a mock event recorder to capture events
	recorder := &mockEventRecorder{events: []mockEvent{}}
	eventPublisher := &K8EventPublisher{recorder: recorder}

	shc := enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "shc",
			Namespace: "test",
		},
	}
	shc.SetGroupVersionKind(enterpriseApi.GroupVersion.WithKind("SearchHeadCluster"))

	err := client.Create(ctx, &shc)
	if err != nil {
		t.Fatalf("Failed to create SearchHeadCluster: %v", err)
	}

	// Create namespace scoped secret so ApplyShcSecret has something to work with
	nsSecret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, shc.GetNamespace())
	if err != nil {
		t.Fatalf("Failed to apply namespace scoped secret: %v", err)
	}

	// Set CR status resource version to a stale value so ApplyShcSecret does not early-return
	shc.Status.NamespaceSecretResourceVersion = nsSecret.ResourceVersion + "-old"
	shc.Status.AdminPasswordChangedSecrets = make(map[string]bool)

	// Initialize a minimal pod manager for ApplyShcSecret
	mgr := &searchHeadClusterPodManager{
		c:  client,
		cr: &shc,
	}

	// Use a mock PodExec client; replicas will be 0 so it won't be exercised
	var mockPodExecClient *spltest.MockPodExecClient = &spltest.MockPodExecClient{}

	// Add event publisher to context so ApplyShcSecret can emit events
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	// Call ApplyShcSecret; with 0 replicas it will complete without touching pods,
	// but still emit the PasswordSyncCompleted event
	err = ApplyShcSecret(ctx, mgr, 0, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}

	// Check that PasswordSyncCompleted event was published
	foundEvent := false
	for _, event := range recorder.events {
		if event.reason == "PasswordSyncCompleted" {
			foundEvent = true
			if event.eventType != corev1.EventTypeNormal {
				t.Errorf("Expected Normal event type, got %s", event.eventType)
			}
			if !strings.Contains(event.message, "Password synchronized") {
				t.Errorf("Expected event message to contain 'Password synchronized', got: %s", event.message)
			}
			break
		}
	}
	if !foundEvent {
		t.Errorf("Expected PasswordSyncCompleted event to be published")
	}
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

	if err != nil {
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
			cr: &cr, appFrameworkRef: &cr.Spec.AppFrameworkConfig,
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
		cr:              &cr,
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
	setLifecyclePolicyTestGates(t, true, true)
	oldRequestDetention := requestSearchHeadDetention
	oldTransferCaptain := transferSearchHeadCaptain
	oldRemoveMember := removeSearchHeadClusterMember
	t.Cleanup(func() {
		requestSearchHeadDetention = oldRequestDetention
		transferSearchHeadCaptain = oldTransferCaptain
		removeSearchHeadClusterMember = oldRemoveMember
	})
	detentionCalls := 0
	requestSearchHeadDetention = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) error {
		detentionCalls++
		return nil
	}
	transferCalls := 0
	transferSearchHeadCaptain = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
		string,
	) error {
		transferCalls++
		return nil
	}
	membershipRemovalCalls := 0
	removeSearchHeadClusterMember = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) error {
		membershipRemovalCalls++
		return nil
	}
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
			// Deliberately invalid for normal reconciliation: finalization
			// must not depend on App Framework validation succeeding.
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				AppSources: []enterpriseApi.AppSourceSpec{
					{
						Name:     "invalid-without-volume",
						Location: "apps",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "missing-volume",
							Scope:   enterpriseApi.ScopeLocal,
						},
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
	c.InduceErrorKind[splcommon.MockClientInduceErrorCreate] =
		errors.New("namespace is terminating: creates are forbidden")

	// test deletion
	currentTime := metav1.NewTime(time.Now())
	shc.ObjectMeta.DeletionTimestamp = &currentTime
	shc.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	shc.ObjectMeta.Annotations = map[string]string{
		enterpriseApi.SearchHeadClusterPausedAnnotation: "true",
	}
	target := int32(2)
	shc.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:   "PodUpdate:splunk-stack1-search-head-2:revision-2",
		Intent:        enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		TargetPod:     "splunk-stack1-search-head-2",
		TargetOrdinal: &target,
		Stage: enterpriseApi.
			SearchHeadClusterLifecycleStageDrainingSearches,
	}

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

	// Finalization must not depend on normal validation/configuration
	// prerequisites and must not issue any Create call.
	_, err := ApplySearchHeadCluster(ctx, c, &shc)
	if err != nil {
		t.Errorf("ApplySearchHeadCluster deletion should not have returned error: %v", err)
	}
	if len(c.Calls["Create"]) != 0 {
		t.Fatalf("deletion attempted %d resource creates", len(c.Calls["Create"]))
	}
	statusRefreshCalls := 0
	for _, call := range c.Calls["Get"] {
		if _, ok := call.Obj.(*enterpriseApi.SearchHeadCluster); ok {
			statusRefreshCalls++
		}
	}
	if statusRefreshCalls != 0 {
		t.Fatalf(
			"successful deletion attempted %d post-finalization status refreshes",
			statusRefreshCalls,
		)
	}
	if len(shc.GetFinalizers()) != 0 {
		t.Fatalf("deletion retained finalizers: %v", shc.GetFinalizers())
	}
	operation := shc.Status.LifecycleOperation
	if operation == nil ||
		operation.Intent !=
			enterpriseApi.SearchHeadClusterLifecycleIntentClusterDeletion ||
		operation.Stage !=
			enterpriseApi.SearchHeadClusterLifecycleStageFinalizingClusterDeletion {
		t.Fatalf(
			"deletion lifecycle = %#v, want explicit ClusterDeletion finalization",
			operation,
		)
	}
	if operation.TargetPod != "" || operation.TargetOrdinal != nil ||
		operation.MembershipRemovalRequestedAt != nil {
		t.Fatalf(
			"complete deletion retained per-member lifecycle state: %#v",
			operation,
		)
	}
	if detentionCalls != 0 || transferCalls != 0 ||
		membershipRemovalCalls != 0 {
		t.Fatalf(
			"complete deletion ran member lifecycle actions: detention=%d transfer=%d removal=%d",
			detentionCalls,
			transferCalls,
			membershipRemovalCalls,
		)
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
	_, err := getSearchHeadClusterList(ctx, client, &shc, listOpts)
	if err == nil {
		t.Errorf("getNumOfObjects should have returned error as we haven't added shc to the list yet")
	}

	shcList := &enterpriseApi.SearchHeadClusterList{}
	shcList.Items = append(shcList.Items, shc)

	client.ListObj = shcList

	objList, err := getSearchHeadClusterList(ctx, client, &shc, listOpts)
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

	// mock the verify RF peer function
	VerifyRFPeers = func(ctx context.Context, mgr indexerClusterPodManager, client splcommon.ControllerClient) error {
		return nil
	}

	// mock new search pod manager
	newSearchHeadClusterPodManager = func(client splcommon.ControllerClient, cr *enterpriseApi.SearchHeadCluster, secret *corev1.Secret, newSplunkClient NewSplunkClientFunc) searchHeadClusterPodManager {
		return searchHeadClusterPodManager{
			cr:      cr,
			secrets: secret,
			newSplunkClient: func(managementURI, username, password string) *splclient.SplunkClient {
				c := splclient.NewSplunkClient(managementURI, username, password)
				c.Client = mclient
				c.SearchHeadClusterUpgradeClient = mclient
				return c
			},
		}
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
	err = splctrl.SetStatefulSetOwnerRef(ctx, c, searchheadcluster, namespacedName)
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
	depSts, err := getSplunkStatefulSet(ctx, client, &shc, &shc.Spec.CommonSplunkSpec, SplunkDeployer, 1, getSearchHeadExtraEnv(&shc, shc.Spec.Replicas), nil)
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

func TestSHCSecretRotationBlockedEvent(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	builder := newFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.SearchHeadCluster{})

	c := builder.Build()
	ctx := context.TODO()

	recorder := &mockEventRecorder{events: []mockEvent{}}
	eventPublisher := &K8EventPublisher{recorder: recorder}
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	// Create namespace scoped secret
	nsSecret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Fatalf("Failed to apply namespace scoped secret: %v", err)
	}

	shc := enterpriseApi.SearchHeadCluster{
		TypeMeta:   metav1.TypeMeta{Kind: "SearchHeadCluster"},
		ObjectMeta: metav1.ObjectMeta{Name: "shc", Namespace: "test"},
	}
	shc.SetGroupVersionKind(enterpriseApi.GroupVersion.WithKind("SearchHeadCluster"))
	// Set stale resource version so ApplyShcSecret doesn't early-return
	shc.Status.NamespaceSecretResourceVersion = nsSecret.ResourceVersion + "-old"
	shc.Status.AdminPasswordChangedSecrets = make(map[string]bool)

	// Create the search head pod with a secret volume mount
	podSecretName := "splunk-shc-search-head-secret-v1"
	shPodName := "splunk-shc-search-head-0"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: shPodName, Namespace: "test"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:latest"}},
			Volumes: []corev1.Volume{
				{
					Name: "mnt-splunk-secrets",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{SecretName: podSecretName},
					},
				},
			},
		},
	}
	if err := c.Create(ctx, pod); err != nil {
		t.Fatalf("Failed to create pod: %v", err)
	}

	// Create the pod's secret with a DIFFERENT shc_secret than namespace secret
	podSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: podSecretName, Namespace: "test"},
		Data: map[string][]byte{
			"password":   []byte("admin-password"),
			"shc_secret": []byte("old-shc-secret"),
		},
	}
	if err := c.Create(ctx, podSecret); err != nil {
		t.Fatalf("Failed to create pod secret: %v", err)
	}

	mgr := &searchHeadClusterPodManager{
		c:  c,
		cr: &shc,
	}

	mockPodExecClient := &spltest.MockPodExecClient{}

	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err == nil ||
		!strings.Contains(err.Error(), "approximately simultaneous restart") {
		t.Fatalf("rotation error = %v", err)
	}
	if len(mockPodExecClient.GotCmdList) != 0 {
		t.Fatalf(
			"blocked rotation executed commands: %v",
			mockPodExecClient.GotCmdList,
		)
	}

	found := false
	for _, event := range recorder.events {
		if event.reason == EventReasonSHCSecretRotationBlocked {
			found = true
			if event.eventType != corev1.EventTypeWarning {
				t.Errorf(
					"Expected Warning event type for %s, got %s",
					EventReasonSHCSecretRotationBlocked,
					event.eventType,
				)
			}
			break
		}
	}
	if !found {
		t.Errorf(
			"Expected %s event to be published",
			EventReasonSHCSecretRotationBlocked,
		)
	}
}

func TestShcScaledUpScaledDownEvent(t *testing.T) {
	ctx := context.TODO()
	recorder := &mockEventRecorder{events: []mockEvent{}}
	eventPublisher := &K8EventPublisher{recorder: recorder}
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	crName := "test-shc"
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: crName, Namespace: "test"},
	}

	// Simulate ScaledUp: previousReplicas=3, desiredReplicas=5, phase=PhaseReady, Status.Replicas=5
	previousReplicas := int32(3)
	desiredReplicas := int32(5)
	cr.Status.Replicas = desiredReplicas
	phase := enterpriseApi.PhaseReady

	// Replicate the production conditional from searchHeadClusterPodManager.Update()
	ep := GetEventPublisher(ctx, cr)
	if phase == enterpriseApi.PhaseReady {
		if desiredReplicas > previousReplicas && cr.Status.Replicas == desiredReplicas {
			ep.Normal(ctx, "ScaledUp",
				fmt.Sprintf("Successfully scaled %s up to %d replicas", cr.GetName(), desiredReplicas))
		}
	}

	found := false
	for _, event := range recorder.events {
		if event.reason == "ScaledUp" {
			found = true
			if event.eventType != corev1.EventTypeNormal {
				t.Errorf("Expected Normal event type for ScaledUp, got %s", event.eventType)
			}
			if !strings.Contains(event.message, crName) {
				t.Errorf("Expected event message to contain CR name '%s', got: %s", crName, event.message)
			}
			if !strings.Contains(event.message, "5") {
				t.Errorf("Expected event message to contain replica counts, got: %s", event.message)
			}
			break
		}
	}
	if !found {
		t.Errorf("Expected ScaledUp event to be published")
	}

	// Simulate ScaledDown: previousReplicas=5, desiredReplicas=3, phase=PhaseReady, Status.Replicas=3
	recorder.events = []mockEvent{}
	previousReplicas = int32(5)
	desiredReplicas = int32(3)
	cr.Status.Replicas = desiredReplicas

	if phase == enterpriseApi.PhaseReady {
		if desiredReplicas < previousReplicas && cr.Status.Replicas == desiredReplicas {
			ep.Normal(ctx, "ScaledDown",
				fmt.Sprintf("Successfully scaled %s down to %d replicas", cr.GetName(), desiredReplicas))
		}
	}

	found = false
	for _, event := range recorder.events {
		if event.reason == "ScaledDown" {
			found = true
			if event.eventType != corev1.EventTypeNormal {
				t.Errorf("Expected Normal event type for ScaledDown, got %s", event.eventType)
			}
			if !strings.Contains(event.message, crName) {
				t.Errorf("Expected event message to contain CR name '%s', got: %s", crName, event.message)
			}
			break
		}
	}
	if !found {
		t.Errorf("Expected ScaledDown event to be published")
	}

	// Negative: no event when phase is not PhaseReady
	recorder.events = []mockEvent{}
	phase = enterpriseApi.PhasePending
	if phase == enterpriseApi.PhaseReady {
		if desiredReplicas < previousReplicas && cr.Status.Replicas == desiredReplicas {
			ep.Normal(ctx, "ScaledDown",
				fmt.Sprintf("Successfully scaled %s down to %d replicas", cr.GetName(), desiredReplicas))
		}
	}
	if len(recorder.events) != 0 {
		t.Errorf("Expected no events when phase is not PhaseReady, got %d events", len(recorder.events))
	}
}
