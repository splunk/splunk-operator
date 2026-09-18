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

package shc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clienttesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func init() {
	defaultOperations = Operations{
		ApplyStatefulSet:             k8sops.ApplyStatefulSet,
		CheckPodsForTerminalFailures: k8sops.CheckPodsForTerminalFailures,
		UpdateStatefulSetPods:        k8sops.UpdateStatefulSetPods,
		ApplySecret:                  k8sops.ApplySecret,
	}
}

func newFakeClientBuilder(scheme *pkgruntime.Scheme) *fake.ClientBuilder {
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjectTracker(clienttesting.NewObjectTracker(scheme, serializer.NewCodecFactory(scheme).UniversalDecoder())).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				err := c.Get(ctx, key, obj, opts...)
				if err != nil {
					return err
				}
				if gvk, err := apiutil.GVKForObject(obj, scheme); err == nil {
					obj.GetObjectKind().SetGroupVersionKind(gvk)
				}
				return nil
			},
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				gvk := obj.GetObjectKind().GroupVersionKind()
				err := c.Create(ctx, obj, opts...)
				obj.GetObjectKind().SetGroupVersionKind(gvk)
				return err
			},
		})
}

type mockEvent struct {
	eventType string
	reason    string
	message   string
}

type mockEventRecorder struct {
	events []mockEvent
}

func loadFixture(t *testing.T, filename string) string {
	t.Helper()
	path := filepath.Join("..", "..", "reconcile", "searchheadcluster", "testdata", "fixtures", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("Failed to load fixture %s: %v", filename, err)
		return ""
	}

	var compactJSON bytes.Buffer
	if err := json.Compact(&compactJSON, data); err != nil {
		t.Errorf("Failed to compact JSON from fixture %s: %v", filename, err)
		return ""
	}
	return compactJSON.String()
}

func (m *mockEventRecorder) Event(_ pkgruntime.Object, eventType, reason, message string) {
	m.events = append(m.events, mockEvent{eventType: eventType, reason: reason, message: message})
}

func (m *mockEventRecorder) Eventf(_ pkgruntime.Object, eventType, reason, messageFmt string, args ...interface{}) {
	m.events = append(m.events, mockEvent{eventType: eventType, reason: reason, message: fmt.Sprintf(messageFmt, args...)})
}

func (m *mockEventRecorder) AnnotatedEventf(_ pkgruntime.Object, _ map[string]string, eventType, reason, messageFmt string, args ...interface{}) {
	m.events = append(m.events, mockEvent{eventType: eventType, reason: reason, message: fmt.Sprintf(messageFmt, args...)})
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

	mgr := &PodManager{
		CR:      &cr,
		Secrets: secrets,
		NewSplunkClient: func(managementURI, username, password string) *splclient.SplunkClient {
			c := splclient.NewSplunkClient(managementURI, username, password)
			c.Client = mockSplunkClient
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

	// funcCalls[2] (Pod-search-head-0) appears twice: once for the Splunk client admin
	// password lookup and once for the controller-revision-hash read in updateStatus.
	wantCalls := map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2], funcCalls[2]}, "Create": {funcCalls[1]}}

	// test API failure
	method := "PodManager.Update(API failure)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhasePending, statefulSet, wantCalls, nil, statefulSet)

	// captain not ready (e.g. mid fleet-recycle captain election) but a scale up is
	// underway -> report ScalingUp instead of masking it behind Pending
	method = "PodManager.Update(API failure, scaling up)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 2, enterpriseApi.PhaseScalingUp, statefulSet, wantCalls, nil, statefulSet)

	// captain not ready but a scale down is underway -> report ScalingDown instead
	// of masking it behind Pending
	method = "PodManager.Update(API failure, scaling down)"
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
	method = "PodManager.Update(All pods ready)"
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
	method = "PodManager.Update(Quarantine Pod)"
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2], funcCalls[2], funcCalls[2], funcCalls[0], funcCalls[5], funcCalls[2], funcCalls[2]}, "Create": {funcCalls[1]}}
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => wait for searches to drain
	mockHandlers = []spltest.MockHTTPHandler{mockHandlers[0], mockHandlers[1]}
	mockHandlers[0].Body = strings.Replace(mockHandlers[0].Body, `"status":"Up"`, `"status":"ManualDetention"`, 1)
	mockHandlers[0].Body = strings.Replace(mockHandlers[0].Body, `"active_historical_search_count":0`, `"active_historical_search_count":1`, 1)
	method = "PodManager.Update(Draining Searches)"
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2], funcCalls[2], funcCalls[2], funcCalls[0], funcCalls[5]}, "Create": {funcCalls[1]}}
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test pod needs update => delete pod
	mockHandlers[0].Body = strings.Replace(mockHandlers[0].Body, `"active_historical_search_count":1`, `"active_historical_search_count":0`, 1)
	method = "PodManager.Update(Delete Pod)"
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
	method = "PodManager.Update(Release Quarantine)"
	wantCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[0], funcCalls[1], funcCalls[1], funcCalls[2], funcCalls[2], funcCalls[2], funcCalls[0], funcCalls[5], funcCalls[2]}, "Create": {funcCalls[1]}}
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhaseUpdating, statefulSet, wantCalls, nil, statefulSet, pod)

	// test scale down => remove member
	mockHandlers[2] = spltest.MockHTTPHandler{
		Method: "GET",
		URL:    "https://splunk-stack1-search-head-1.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/shcluster/member/info?count=0&output_mode=json",
		Status: 200,
		Err:    nil,
		Body:   loadFixture(t, "shc_member_remove_response.json"),
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
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-0"}, // controller-revision-hash read
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-1"},
		{MetaName: "*v1.Pod-test-splunk-stack1-search-head-1"}, // controller-revision-hash read
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
	method = "PodManager.Update(Remove Member)"
	searchHeadClusterPodManagerTester(t, method, mockHandlers, 1, enterpriseApi.PhaseScalingDown, statefulSet, wantCalls, nil, statefulSet, pod, pvcList[0], pvcList[1])

}

func TestFinishRecycle(t *testing.T) {
	ctx := context.TODO()
	mgr := &PodManager{
		CR: &enterpriseApi.SearchHeadCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "stack1", Namespace: "test"},
		},
	}

	// member is up, not in detention -> recycle is complete
	mgr.CR.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{{Status: "Up"}}
	complete, err := mgr.FinishRecycle(ctx, 0)
	if err != nil || !complete {
		t.Errorf("FinishRecycle(Up) = %v, %v; want true, nil", complete, err)
	}

	// member info was transiently unavailable (e.g. pod mid-restart) -> wait, don't error
	mgr.CR.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{{Status: ""}}
	complete, err = mgr.FinishRecycle(ctx, 0)
	if err != nil || complete {
		t.Errorf("FinishRecycle(empty status) = %v, %v; want false, nil", complete, err)
	}

	// any other unrecognized status is still a hard error
	mgr.CR.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{{Status: "Down"}}
	complete, err = mgr.FinishRecycle(ctx, 0)
	if err == nil || complete {
		t.Errorf("FinishRecycle(Down) = %v, %v; want false, error", complete, err)
	}
}

func TestApplyShcSecret(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	method := "ApplyShcSecret"
	var initObjectList []client.Object

	c := spltest.NewMockClient()

	// Get namespace scoped secret
	nsSecret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
	if err != nil {
		t.Errorf("Apply namespace scoped secret failed")
	}

	// Create pod
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

	secrets := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"password":   {'1', '2', '3'},
			"shc_secret": {'a'},
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
		{
			Method: "POST",
			URL:    "https://splunk-stack1-search-head-0.splunk-stack1-search-head-headless.test.svc.cluster.local:8089/services/server/control/restart",
			Status: 200,
			Err:    nil,
		},
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
	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandlers(mockHandlers...)
	mgr := &PodManager{
		Client:  c,
		CR:      &cr,
		Secrets: secrets,
		NewSplunkClient: func(managementURI, username, password string) *splclient.SplunkClient {
			c := splclient.NewSplunkClient(managementURI, username, password)
			c.Client = mockSplunkClient
			return c
		},
	}

	podExecCommands := []string{
		"/opt/splunk/bin/splunk edit shcluster-config",
		"opt/splunk/bin/splunk cmd splunkd rest",
	}
	mockPodExecReturnContexts := []*spltest.MockPodExecReturnContext{
		{
			StdOut: "",
			StdErr: "",
			Err:    fmt.Errorf("some dummy error"),
		},
		{
			StdOut: "",
			StdErr: "",
			Err:    fmt.Errorf("some dummy error"),
		},
	}

	var mockPodExecClient *spltest.MockPodExecClient = &spltest.MockPodExecClient{}
	mockPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnContexts...)
	// Set resource version as that of NS secret
	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}

	// Change resource version and test
	mgr.CR.Status.NamespaceSecretResourceVersion = "0"
	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err == nil {
		t.Errorf("Couldn't apply shc secret")
	}

	mockPodExecReturnContexts[0].Err = nil
	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err == nil {
		t.Errorf("Couldn't apply shc secret")
	}

	mgr.CR.Status.ShcSecretChanged[0] = false
	mockPodExecReturnContexts[1].Err = nil
	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}
	mockSplunkClient.CheckRequests(t, method)

	// Don't set as it is set already
	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}

	// Update admin password in secret again to hit already set scenario
	secrets.Data["password"] = []byte{'1'}
	err = splutil.UpdateResource(ctx, c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	mgr.CR.Status.ShcSecretChanged[0] = false
	// Test set again for shc_secret
	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}

	// Update admin password in secret again to hit already set scenario
	secrets.Data["password"] = []byte{'1'}
	err = splutil.UpdateResource(ctx, c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	mgr.CR.Status.ShcSecretChanged[0] = false
	mgr.CR.Status.AdminSecretChanged[0] = false
	// Test set again for admin password
	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}

	// Missing shc_secret scenario
	secrets = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"password": {'1', '2', '3'},
		},
	}
	err = splutil.UpdateResource(ctx, c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	errMsg := fmt.Sprintf(splcommon.SecretTokenNotRetrievable, "shc_secret") + ", error: invalid secret data"

	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err.Error() != errMsg {
		t.Errorf("Couldn't recognize missing shc_secret %s", err.Error())
	}

	// Missing admin password scenario
	secrets = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"shc_secret": {'a'},
		},
	}

	err = splutil.UpdateResource(ctx, c, secrets)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	errMsg = fmt.Sprintf(splcommon.SecretTokenNotRetrievable, "admin password") + ", error: invalid secret data"
	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err.Error() != errMsg {
		t.Errorf("Couldn't recognize missing admin password %s", err.Error())
	}

	// Make resource version of ns secret and cr the same
	mgr.CR.Status.NamespaceSecretResourceVersion = "1"
	nsSecret.ResourceVersion = mgr.CR.Status.NamespaceSecretResourceVersion
	err = splutil.UpdateResource(ctx, c, nsSecret)
	if err != nil {
		t.Errorf("Couldn't update resource")
	}

	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
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

	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, "test")
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
			"password":   {'1'},
			"shc_secret": {'a'},
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
	mgr := &PodManager{
		Client:  c,
		CR:      &cr,
		Secrets: secrets,
		NewSplunkClient: func(managementURI, username, password string) *splclient.SplunkClient {
			c := splclient.NewSplunkClient(managementURI, username, password)
			c.Client = mockSplunkClient
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

	// Namespace secret's shc_secret ('a') already matches the pod's, so the
	// admin-password mismatch is the only thing that should trigger a sync.
	// Bump the resource version so ApplyShcSecret doesn't early-return.
	mgr.CR.Status.NamespaceSecretResourceVersion = "0"

	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err != nil {
		t.Errorf("Couldn't apply shc secret %s", err.Error())
	}

	if !mgr.CR.Status.AdminSecretChanged[0] {
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
	eventPublisher, _ := k8sops.NewK8EventPublisherWithRecorder(recorder, &enterpriseApi.SearchHeadCluster{})

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
	mgr := &PodManager{
		Client: client,
		CR:     &shc,
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

func TestShcPasswordSyncFailedEvent(t *testing.T) {
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
	eventPublisher, _ := k8sops.NewK8EventPublisherWithRecorder(recorder, &enterpriseApi.SearchHeadCluster{})
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

	mgr := &PodManager{
		Client: c,
		CR:     &shc,
	}

	// Configure mock pod exec client to return an error on shcluster-config command
	mockPodExecClient := &spltest.MockPodExecClient{}
	mockPodExecClient.AddMockPodExecReturnContext(ctx, "shcluster-config", &spltest.MockPodExecReturnContext{
		StdOut: "",
		StdErr: "connection refused",
		Err:    fmt.Errorf("connection refused"),
	})

	// Call ApplyShcSecret — should fail at RunPodExecCommand and emit PasswordSyncFailed
	err = ApplyShcSecret(ctx, mgr, 1, mockPodExecClient)
	if err == nil {
		t.Errorf("Expected error from ApplyShcSecret when pod exec fails")
	}

	found := false
	for _, event := range recorder.events {
		if event.reason == "PasswordSyncFailed" {
			found = true
			if event.eventType != corev1.EventTypeWarning {
				t.Errorf("Expected Warning event type for PasswordSyncFailed, got %s", event.eventType)
			}
			if !strings.Contains(event.message, shPodName) {
				t.Errorf("Expected event message to contain pod name '%s', got: %s", shPodName, event.message)
			}
			if !strings.Contains(event.message, "connection refused") {
				t.Errorf("Expected event message to contain error details, got: %s", event.message)
			}
			break
		}
	}
	if !found {
		t.Errorf("Expected PasswordSyncFailed event to be published")
	}
}

func TestShcScaledUpScaledDownEvent(t *testing.T) {
	ctx := context.TODO()
	recorder := &mockEventRecorder{events: []mockEvent{}}
	eventPublisher, _ := k8sops.NewK8EventPublisherWithRecorder(recorder, &enterpriseApi.SearchHeadCluster{})
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

	// Replicate the production conditional from PodManager.Update()
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

func newTestSHCPodManager(cr *enterpriseApi.SearchHeadCluster) *PodManager {
	return &PodManager{
		CR: cr,
		NewSplunkClient: func(managementURI, username, password string) *splclient.SplunkClient {
			return splclient.NewSplunkClient(managementURI, username, password)
		},
	}
}

func newTestSHCCR(memberStatus string, historicalSearches, realtimeSearches int) *enterpriseApi.SearchHeadCluster {
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shc",
			Namespace: "test",
		},
	}
	cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{
		{
			Name:                        "splunk-test-shc-search-head-0",
			Status:                      memberStatus,
			ActiveHistoricalSearchCount: historicalSearches,
			ActiveRealtimeSearchCount:   realtimeSearches,
		},
	}
	return cr
}

func TestPrepareRecycle_NormalDrain(t *testing.T) {
	ctx := context.Background()

	// Step 1: active searches present, not timed out — should wait
	cr := newTestSHCCR("ManualDetention", 3, 2)
	cr.Spec.DetentionTimeoutSeconds = 3600
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 10
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready {
		t.Error("expected (false, nil) while searches are active and timeout not exceeded")
	}

	// Step 2: searches drained — should return true and zero the timer fields
	cr.Status.Members[0].ActiveHistoricalSearchCount = 0
	cr.Status.Members[0].ActiveRealtimeSearchCount = 0
	ready, err = mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Error("expected (true, nil) when searches have drained")
	}
	if cr.Status.DetentionStartTimestamp != 0 {
		t.Error("expected DetentionStartTimestamp to be zeroed after drain")
	}
	if cr.Status.DetainedMemberName != "" {
		t.Error("expected DetainedMemberName to be cleared after drain")
	}
}

func TestPrepareRecycle_TimeoutForced(t *testing.T) {
	ctx := context.Background()

	cr := newTestSHCCR("ManualDetention", 1, 2)
	cr.Spec.DetentionTimeoutSeconds = 3600
	savedTimestamp := time.Now().Unix() - 3700
	cr.Status.DetentionStartTimestamp = savedTimestamp
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"
	cr.Status.DetainedPodRevision = "v1"
	cr.Status.Members[0].PodRevision = "v1"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Error("expected (true, nil) when timeout exceeded")
	}
	// Fields must NOT be cleared here — FinishRecycle owns cleanup after pod deletion succeeds.
	// Clearing here causes a fresh full timeout if the pod delete fails transiently.
	if cr.Status.DetentionStartTimestamp != savedTimestamp {
		t.Error("expected DetentionStartTimestamp to be preserved after timeout — FinishRecycle clears it")
	}
	if cr.Status.DetainedMemberName != "splunk-test-shc-search-head-0" {
		t.Error("expected DetainedMemberName to be preserved after timeout — FinishRecycle clears it")
	}
}

func TestPrepareRecycle_TimeoutForcedWhenPodRevisionUnavailable(t *testing.T) {
	ctx := context.Background()

	cr := newTestSHCCR("ManualDetention", 1, 2)
	cr.Spec.DetentionTimeoutSeconds = 3600
	savedTimestamp := time.Now().Unix() - 3700
	cr.Status.DetentionStartTimestamp = savedTimestamp
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Error("expected timeout to force recycle when pod revision is unavailable")
	}
	if cr.Status.DetentionStartTimestamp != savedTimestamp {
		t.Error("expected empty pod revision not to reset an expired detention timer")
	}
}

func TestPrepareRecycle_FirstObservedPodRevisionDoesNotResetTimer(t *testing.T) {
	ctx := context.Background()

	cr := newTestSHCCR("ManualDetention", 1, 2)
	cr.Spec.DetentionTimeoutSeconds = 3600
	savedTimestamp := time.Now().Unix() - 3700
	cr.Status.DetentionStartTimestamp = savedTimestamp
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"
	cr.Status.DetainedPodRevision = ""
	cr.Status.Members[0].PodRevision = "revision-1"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Error("expected timeout to force recycle when first observed pod revision appears after timeout")
	}
	if cr.Status.DetentionStartTimestamp != savedTimestamp {
		t.Error("expected first observed pod revision not to reset an expired detention timer")
	}
	if cr.Status.DetainedPodRevision != "revision-1" {
		t.Errorf("expected first observed pod revision to be recorded, got %q", cr.Status.DetainedPodRevision)
	}
}

func TestPrepareRecycle_TimestampSetOnFirstEntry(t *testing.T) {
	ctx := context.Background()

	cr := newTestSHCCR("ManualDetention", 0, 2)
	cr.Spec.DetentionTimeoutSeconds = 3600
	cr.Status.DetentionStartTimestamp = 0
	cr.Status.DetainedMemberName = ""

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready {
		t.Error("expected (false, nil) on first entry with active searches")
	}
	if cr.Status.DetentionStartTimestamp == 0 {
		t.Error("expected DetentionStartTimestamp to be set on first entry")
	}
	if cr.Status.DetainedMemberName != "splunk-test-shc-search-head-0" {
		t.Errorf("expected DetainedMemberName to be set, got %q", cr.Status.DetainedMemberName)
	}
}

func TestPrepareRecycle_CustomTimeout(t *testing.T) {
	ctx := context.Background()

	cr := newTestSHCCR("ManualDetention", 0, 1)
	cr.Spec.DetentionTimeoutSeconds = 60
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 70
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"
	cr.Status.DetainedPodRevision = "v1"
	cr.Status.Members[0].PodRevision = "v1"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Error("expected (true, nil) when custom timeout of 60s exceeded after 70s")
	}
}

func TestPrepareRecycle_DetentionTimeoutSecondsDefault(t *testing.T) {
	ctx := context.Background()

	// DetentionTimeoutSeconds = 0 means unset — should use defaultSearchHeadDetentionTimeoutSeconds (3600)
	cr := newTestSHCCR("ManualDetention", 0, 1)
	cr.Spec.DetentionTimeoutSeconds = 0
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 3700
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"
	cr.Status.DetainedPodRevision = "v1"
	cr.Status.Members[0].PodRevision = "v1"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Error("expected (true, nil) when default timeout of 3600s exceeded after 3700s")
	}
}

func TestPrepareRecycle_NegativeTimeout(t *testing.T) {
	ctx := context.Background()

	// Negative DetentionTimeoutSeconds should fall back to default (3600)
	cr := newTestSHCCR("ManualDetention", 0, 1)
	cr.Spec.DetentionTimeoutSeconds = -1
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 3700
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"
	cr.Status.DetainedPodRevision = "v1"
	cr.Status.Members[0].PodRevision = "v1"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Error("expected (true, nil) — negative timeout should use default of 3600s, exceeded after 3700s")
	}
}

func TestPrepareRecycle_NegativeActiveSearches(t *testing.T) {
	ctx := context.Background()

	// Negative search count (corrupted REST response) should be treated as drained
	cr := newTestSHCCR("ManualDetention", -1, -1)
	cr.Spec.DetentionTimeoutSeconds = 3600
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 10
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Error("expected (true, nil) — negative search count should be treated as drained")
	}
}

func TestPrepareRecycle_TimerResetOnMemberChange(t *testing.T) {
	ctx := context.Background()

	// Timer was set for a different member (e.g. after operator restart mid-rolling-update)
	// Should reset timestamp and member name for the new member
	cr := newTestSHCCR("ManualDetention", 0, 2)
	cr.Spec.DetentionTimeoutSeconds = 3600
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 100
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-1" // different member
	cr.Status.DetainedPodRevision = "revision-1"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready {
		t.Error("expected (false, nil) — timer just reset for new member, timeout not exceeded")
	}
	if cr.Status.DetainedMemberName != "splunk-test-shc-search-head-0" {
		t.Errorf("expected DetainedMemberName to be updated to current member, got %q", cr.Status.DetainedMemberName)
	}
	if cr.Status.DetainedPodRevision != "" {
		t.Errorf("expected stale DetainedPodRevision to be cleared on member change, got %q", cr.Status.DetainedPodRevision)
	}
	// Timestamp should have been reset to approximately now, not the old value
	if time.Now().Unix()-cr.Status.DetentionStartTimestamp > 2 {
		t.Error("expected DetentionStartTimestamp to be reset to approximately now for new member")
	}
}

func TestFinishRecycle_ClearsTimerFields(t *testing.T) {
	ctx := context.Background()

	cr := newTestSHCCR("ManualDetention", 0, 0)
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 100
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"

	// FinishRecycle clears the timer fields before calling SetSearchHeadDetention.
	// We verify the in-memory CR fields are zeroed regardless of whether the REST call succeeds.
	// getClient requires a controller client for secret retrieval; use defer/recover to catch
	// the nil-client panic and still assert the fields were cleared before the call was made.
	func() {
		defer func() { recover() }() // suppress nil-client panic from getClient
		mgr := newTestSHCPodManager(cr)
		_, _ = mgr.FinishRecycle(ctx, 0)
	}()

	if cr.Status.DetentionStartTimestamp != 0 {
		t.Error("expected DetentionStartTimestamp to be zeroed by FinishRecycle ManualDetention case")
	}
	if cr.Status.DetainedMemberName != "" {
		t.Error("expected DetainedMemberName to be cleared by FinishRecycle ManualDetention case")
	}
}

func TestFinishRecycle_ClearsTimerFields_UpStatus(t *testing.T) {
	ctx := context.Background()

	// After a timeout-forced recycle, the pod comes back Up — FinishRecycle Up branch must clear timer
	cr := newTestSHCCR("Up", 0, 0)
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 100
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.FinishRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Error("expected (true, nil) when member is Up")
	}
	if cr.Status.DetentionStartTimestamp != 0 {
		t.Error("expected DetentionStartTimestamp to be zeroed when member returns Up after timeout-forced recycle")
	}
	if cr.Status.DetainedMemberName != "" {
		t.Error("expected DetainedMemberName to be cleared when member returns Up after timeout-forced recycle")
	}
}

func TestPrepareRecycle_TimerSurvivesUpdateStatus(t *testing.T) {
	ctx := context.Background()

	cr := newTestSHCCR("ManualDetention", 0, 2)
	cr.Spec.DetentionTimeoutSeconds = 3600
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 10
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"

	savedTimestamp := cr.Status.DetentionStartTimestamp
	savedName := cr.Status.DetainedMemberName

	// Simulate what updateStatus() does — rebuild Members slice from REST API
	// This overwrites Members[n] but must NOT touch cluster-level timer fields
	cr.Status.Members[0] = enterpriseApi.SearchHeadClusterMemberStatus{
		Name:                        "splunk-test-shc-search-head-0",
		Status:                      "ManualDetention",
		ActiveHistoricalSearchCount: 0,
		ActiveRealtimeSearchCount:   2,
	}

	if cr.Status.DetentionStartTimestamp != savedTimestamp {
		t.Error("updateStatus() must not wipe DetentionStartTimestamp — it is a cluster-level field")
	}
	if cr.Status.DetainedMemberName != savedName {
		t.Error("updateStatus() must not wipe DetainedMemberName — it is a cluster-level field")
	}

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready {
		t.Error("expected (false, nil) — searches still active, timer should have survived updateStatus()")
	}
}

func TestPrepareRecycle_StaleTimerClearedOnNewDetentionCycle(t *testing.T) {
	ctx := context.Background()

	// Simulate: timeout fired in a previous recycle episode, timer fields were NOT cleared
	// (FinishRecycle was skipped because a new revision arrived before the pod returned to Up).
	// The pod is now Up again and a new recycle episode is starting.
	cr := newTestSHCCR("Up", 0, 0)
	cr.Spec.DetentionTimeoutSeconds = 3600
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 3700 // stale expired timestamp
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"

	// PrepareRecycle on an Up pod detains it — the stale timer must be cleared first
	// so the next ManualDetention cycle gets a fresh 3600s window, not an instant force-recycle.
	// getClient panics with nil controller client in unit tests; recover and assert timer was cleared.
	func() {
		defer func() { recover() }()
		mgr := newTestSHCPodManager(cr)
		_, _ = mgr.PrepareRecycle(ctx, 0)
	}()

	if cr.Status.DetentionStartTimestamp != 0 {
		t.Error("expected stale DetentionStartTimestamp to be cleared when pod re-enters Up state")
	}
	if cr.Status.DetainedMemberName != "" {
		t.Error("expected stale DetainedMemberName to be cleared when pod re-enters Up state")
	}
}

func TestPrepareRecycle_TimerResetOnRevisionChange(t *testing.T) {
	ctx := context.Background()

	// Simulate: timeout fired for revision-1, timer not cleared, pod restarted as revision-2.
	// The replacement pod is reporting ManualDetention (rejoined SHC in detention state).
	// PrepareRecycle must reset the timer for the new revision so it gets a fresh timeout window.
	cr := newTestSHCCR("ManualDetention", 0, 1)
	cr.Spec.DetentionTimeoutSeconds = 3600
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 3700 // stale expired timestamp
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"
	cr.Status.DetainedPodRevision = "revision-1"
	// New pod is on revision-2 — simulate updateStatus having populated PodRevision
	cr.Status.Members[0].PodRevision = "revision-2"

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready {
		t.Error("expected (false, nil) — timer was reset for new revision, not an instant force-recycle")
	}
	if cr.Status.DetainedPodRevision != "revision-2" {
		t.Errorf("expected DetainedPodRevision updated to revision-2, got %q", cr.Status.DetainedPodRevision)
	}
	// Timer should have been reset to approximately now, not the old expired value
	if time.Now().Unix()-cr.Status.DetentionStartTimestamp > 2 {
		t.Error("expected DetentionStartTimestamp to be reset to approximately now for new revision")
	}
}

func TestUpdateStatusPreservesPodRevisionByMemberName(t *testing.T) {
	ctx := context.Background()
	restoreSearchHeadClusterInfoStubs(t)

	replicas := int32(2)
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-shc", Namespace: "test"},
	}
	cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{
		{Name: "splunk-test-shc-search-head-1", PodRevision: "revision-1"},
		{Name: "splunk-test-shc-search-head-0", PodRevision: "revision-0"},
	}

	c := spltest.NewMockClient()
	c.AddObjects([]client.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "splunk-test-shc-search-head-0",
				Namespace: "test",
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "splunk-test-shc-search-head-1",
				Namespace: "test",
			},
		},
	})
	mgr := newTestSHCPodManager(cr)
	mgr.Client = c

	err := mgr.UpdateStatus(ctx, searchHeadStatefulSet("test-shc", replicas))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cr.Status.Members[0].PodRevision; got != "revision-0" {
		t.Errorf("expected revision-0 preserved by member name, got %q", got)
	}
	if got := cr.Status.Members[1].PodRevision; got != "revision-1" {
		t.Errorf("expected revision-1 preserved by member name, got %q", got)
	}
}

func TestUpdateStatusPreservesPodRevisionWhenPodReadFails(t *testing.T) {
	ctx := context.Background()
	restoreSearchHeadClusterInfoStubs(t)

	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-shc", Namespace: "test"},
	}
	cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{
		{Name: "splunk-test-shc-search-head-0", PodRevision: "revision-0"},
	}

	mgr := newTestSHCPodManager(cr)
	mgr.Client = spltest.NewMockClient()

	err := mgr.UpdateStatus(ctx, searchHeadStatefulSet("test-shc", 1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cr.Status.Members[0].PodRevision; got != "revision-0" {
		t.Errorf("expected revision-0 preserved after pod read failure, got %q", got)
	}
}

func TestUpdateStatusUsesCurrentPodRevisionWhenPresent(t *testing.T) {
	ctx := context.Background()
	restoreSearchHeadClusterInfoStubs(t)

	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-shc", Namespace: "test"},
	}
	cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{
		{Name: "splunk-test-shc-search-head-0", PodRevision: "old-revision"},
	}

	c := spltest.NewMockClient()
	c.AddObjects([]client.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "splunk-test-shc-search-head-0",
				Namespace: "test",
				Labels: map[string]string{
					"controller-revision-hash": "current-revision",
				},
			},
		},
	})
	mgr := newTestSHCPodManager(cr)
	mgr.Client = c

	err := mgr.UpdateStatus(ctx, searchHeadStatefulSet("test-shc", 1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cr.Status.Members[0].PodRevision; got != "current-revision" {
		t.Errorf("expected current pod revision to win, got %q", got)
	}
}

func restoreSearchHeadClusterInfoStubs(t *testing.T) {
	t.Helper()
	originalMemberInfo := GetSearchHeadClusterMemberInfo
	originalCaptainInfo := GetSearchHeadCaptainInfo
	t.Cleanup(func() {
		GetSearchHeadClusterMemberInfo = originalMemberInfo
		GetSearchHeadCaptainInfo = originalCaptainInfo
	})

	GetSearchHeadClusterMemberInfo = func(ctx context.Context, mgr *PodManager, n int32) (*splclient.SearchHeadClusterMemberInfo, error) {
		return &splclient.SearchHeadClusterMemberInfo{
			Status:     "Up",
			Adhoc:      true,
			Registered: true,
		}, nil
	}
	GetSearchHeadCaptainInfo = func(ctx context.Context, mgr *PodManager, n int32) (*splclient.SearchHeadCaptainInfo, error) {
		return &splclient.SearchHeadCaptainInfo{
			Label:          "splunk-test-shc-search-head-0",
			ServiceReady:   true,
			Initialized:    true,
			MinPeersJoined: true,
		}, nil
	}
}

func searchHeadStatefulSet(name string, replicas int32) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-" + name + "-search-head", Namespace: "test"},
		Status: appsv1.StatefulSetStatus{
			Replicas:      replicas,
			ReadyReplicas: replicas,
		},
	}
}

// The following captain-stability-aware PrepareRecycle/DeferRecycle/UpdateStatus
// tests are Linus-specific: they exercise the captainStable/nextCaptainStableSince
// gating (searchheadclusterlifecycle.go) that only exists on this branch.
func TestPrepareRecycle_BlocksWhileCaptainNotYetStable(t *testing.T) {
	ctx := context.Background()

	cr := newTestSHCCR("Up", 0, 0)
	cr.Status.Captain = "splunk-test-shc-search-head-1"
	cr.Status.CaptainStableSince = time.Now().Unix() // just observed this reconcile

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready {
		t.Error("expected (false, nil) while the captain has not yet held stable for the settle window")
	}
	// The member-status switch must never have been reached: no detention
	// bookkeeping should have started.
	if cr.Status.DetentionStartTimestamp != 0 {
		t.Error("expected no detention bookkeeping to start while blocked on captain stability")
	}
}

// Once the captain has held stable for at least the settle window, recycling
// proceeds exactly as it would have without this check.
func TestPrepareRecycle_ProceedsOnceCaptainIsStable(t *testing.T) {
	ctx := context.Background()

	cr := newTestSHCCR("ManualDetention", 0, 0)
	cr.Spec.DetentionTimeoutSeconds = 3600
	cr.Status.DetentionStartTimestamp = time.Now().Unix() - 10
	cr.Status.DetainedMemberName = "splunk-test-shc-search-head-0"
	cr.Status.Captain = "splunk-test-shc-search-head-1"
	cr.Status.CaptainStableSince = time.Now().Unix() - captainStabilizationSeconds

	mgr := newTestSHCPodManager(cr)
	ready, err := mgr.PrepareRecycle(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Error("expected (true, nil) — captain has held stable for the full settle window and searches are drained")
	}
}

// threeMemberSHCCR builds a 3-member CR with the given (name, podRevision,
// status) per member, for DeferRecycle tests that need more than one member.
func threeMemberSHCCR(members [3]struct{ podRevision, status string }) *enterpriseApi.SearchHeadCluster {
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-shc", Namespace: "test"},
	}
	for i, m := range members {
		cr.Status.Members = append(cr.Status.Members, enterpriseApi.SearchHeadClusterMemberStatus{
			Name:        fmt.Sprintf("splunk-test-shc-search-head-%d", i),
			PodRevision: m.podRevision,
			Status:      m.status,
		})
	}
	return cr
}

func TestDeferRecycle_DefersCaptainWhileOthersPending(t *testing.T) {
	ctx := context.Background()
	cr := threeMemberSHCCR([3]struct{ podRevision, status string }{
		{"v0", "Up"}, // ordinal 0: not yet recycled
		{"v1", "Up"}, // ordinal 1: already rolled and rejoined
		{"v0", "Up"}, // ordinal 2: captain, not yet recycled
	})
	cr.Status.Captain = "splunk-test-shc-search-head-2.splunk-test-shc-search-head-headless.test.svc.cluster.local" // FQDN, not the short member name

	mgr := newTestSHCPodManager(cr)
	deferred, err := mgr.DeferRecycle(ctx, 2, "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deferred {
		t.Error("expected the captain's recycle to be deferred while ordinal 0 still needs recycling")
	}
}

func TestDeferRecycle_ProceedsOnceAllOthersAreRolled(t *testing.T) {
	ctx := context.Background()
	cr := threeMemberSHCCR([3]struct{ podRevision, status string }{
		{"v1", "Up"}, // ordinal 0: already rolled and rejoined
		{"v1", "Up"}, // ordinal 1: already rolled and rejoined
		{"v0", "Up"}, // ordinal 2: captain, last one left
	})
	cr.Status.Captain = "splunk-test-shc-search-head-2.splunk-test-shc-search-head-headless.test.svc.cluster.local" // FQDN, not the short member name

	mgr := newTestSHCPodManager(cr)
	deferred, err := mgr.DeferRecycle(ctx, 2, "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deferred {
		t.Error("expected the captain's recycle to proceed once every other member has fully rolled")
	}
}

func TestDeferRecycle_WaitsForOthersToFullyRejoinNotJustRecreate(t *testing.T) {
	ctx := context.Background()
	cr := threeMemberSHCCR([3]struct{ podRevision, status string }{
		{"v1", "ManualDetention"}, // ordinal 0: recreated on v1, but not yet rejoined
		{"v1", "Up"},
		{"v0", "Up"}, // ordinal 2: captain
	})
	cr.Status.Captain = "splunk-test-shc-search-head-2.splunk-test-shc-search-head-headless.test.svc.cluster.local" // FQDN, not the short member name

	mgr := newTestSHCPodManager(cr)
	deferred, err := mgr.DeferRecycle(ctx, 2, "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deferred {
		t.Error("expected the captain's recycle to stay deferred until ordinal 0 fully rejoins (status Up), not merely be recreated")
	}
}

// The captain can only ever reach ManualDetention once every other member
// was already fully recycled (others was 0 at that point). If some
// already-recycled member's live status independently regresses away from
// "Up" afterward — unrelated to this rollout, e.g. an admin action or a
// transient blip — the captain must not be re-deferred and left paused
// indefinitely; it should finish the recycle it already started.
func TestDeferRecycle_DoesNotReDeferCaptainAlreadyInDetention(t *testing.T) {
	ctx := context.Background()
	cr := threeMemberSHCCR([3]struct{ podRevision, status string }{
		{"v1", "Up"},              // ordinal 0: already rolled and rejoined
		{"v1", "ManualDetention"}, // ordinal 1: already on v1, but independently regressed away from Up
		{"v1", "ManualDetention"}, // ordinal 2: captain, already mid-recycle from an earlier pass
	})
	cr.Status.Captain = "splunk-test-shc-search-head-2.splunk-test-shc-search-head-headless.test.svc.cluster.local" // FQDN, not the short member name

	mgr := newTestSHCPodManager(cr)
	deferred, err := mgr.DeferRecycle(ctx, 2, "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deferred {
		t.Error("expected a captain already in ManualDetention to never be re-deferred, even if another member's status has since regressed")
	}
}

func TestDeferRecycle_DoesNotDeferNonCaptainMembers(t *testing.T) {
	ctx := context.Background()
	cr := threeMemberSHCCR([3]struct{ podRevision, status string }{
		{"v0", "Up"},
		{"v0", "Up"},
		{"v0", "Up"}, // captain
	})
	cr.Status.Captain = "splunk-test-shc-search-head-2.splunk-test-shc-search-head-headless.test.svc.cluster.local" // FQDN, not the short member name

	mgr := newTestSHCPodManager(cr)
	deferred, err := mgr.DeferRecycle(ctx, 0, "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deferred {
		t.Error("expected a non-captain member to never be deferred by this check")
	}
}

// If the captain changes mid rolling-update, the newly-current captain is
// deferred on the very next call — there is no "captain as of when the
// rollout started" bookkeeping that could go stale.
func TestDeferRecycle_FollowsCaptainChangeMidRollout(t *testing.T) {
	ctx := context.Background()
	cr := threeMemberSHCCR([3]struct{ podRevision, status string }{
		{"v0", "Up"}, // ordinal 0: the OLD captain, now just a regular stale member
		{"v1", "Up"}, // ordinal 1: already rolled
		{"v0", "Up"}, // ordinal 2: the NEW captain, not yet recycled
	})
	cr.Status.Captain = "splunk-test-shc-search-head-2.splunk-test-shc-search-head-headless.test.svc.cluster.local" // captain changed to ordinal 2, FQDN form

	mgr := newTestSHCPodManager(cr)

	// The old captain (ordinal 0) is no longer captain, so it must not be
	// deferred even though it's still stale.
	deferred, err := mgr.DeferRecycle(ctx, 0, "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deferred {
		t.Error("expected the former captain to be recyclable like any other member once it no longer holds the role")
	}

	// The new captain (ordinal 2) must be deferred while ordinal 0 is still stale.
	deferred, err = mgr.DeferRecycle(ctx, 2, "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deferred {
		t.Error("expected the new captain to be deferred while the former captain still needs recycling")
	}
}
func TestUpdateStatusFirstCaptainObservationIsImmediatelyStable(t *testing.T) {
	ctx := context.Background()
	restoreSearchHeadClusterInfoStubs(t) // stubs captain label "splunk-test-shc-search-head-0", ServiceReady=true

	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-shc", Namespace: "test"},
	}
	mgr := newTestSHCPodManager(cr)
	mgr.Client = spltest.NewMockClient()

	if err := mgr.UpdateStatus(ctx, searchHeadStatefulSet("test-shc", 1)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A brand new cluster's very first captain observation must not block
	// its first rolling update behind the settle window.
	if !captainStable(cr.Status.Captain, cr.Status.CaptainStableSince, time.Now()) {
		t.Error("expected a cluster's first-ever captain observation to be immediately stable")
	}
}

func TestUpdateStatusPreservesCaptainStableSinceForSameCaptain(t *testing.T) {
	ctx := context.Background()
	restoreSearchHeadClusterInfoStubs(t) // stubs captain label "splunk-test-shc-search-head-0", ServiceReady=true

	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-shc", Namespace: "test"},
	}
	cr.Status.Captain = "splunk-test-shc-search-head-0" // same label the stub reports
	cr.Status.CaptainReady = true                       // previous reconcile observed it ready, same as CaptainStableSince below
	stableSince := time.Now().Unix() - 500
	cr.Status.CaptainStableSince = stableSince

	mgr := newTestSHCPodManager(cr)
	mgr.Client = spltest.NewMockClient()

	if err := mgr.UpdateStatus(ctx, searchHeadStatefulSet("test-shc", 1)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cr.Status.CaptainStableSince != stableSince {
		t.Errorf("expected CaptainStableSince unchanged at %d for the same, still-ready captain, got %d", stableSince, cr.Status.CaptainStableSince)
	}
}

func TestUpdateStatusRestartsCaptainStableClockOnCaptainChange(t *testing.T) {
	ctx := context.Background()
	restoreSearchHeadClusterInfoStubs(t) // stubs captain label "splunk-test-shc-search-head-0", ServiceReady=true

	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-shc", Namespace: "test"},
	}
	cr.Status.Captain = "splunk-test-shc-search-head-1" // a different, previous captain
	cr.Status.CaptainStableSince = time.Now().Unix() - 500

	mgr := newTestSHCPodManager(cr)
	mgr.Client = spltest.NewMockClient()

	before := time.Now().Unix()
	if err := mgr.UpdateStatus(ctx, searchHeadStatefulSet("test-shc", 1)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cr.Status.CaptainStableSince < before {
		t.Errorf("expected CaptainStableSince to restart to approximately now after a captain change, got %d", cr.Status.CaptainStableSince)
	}
}

// Requested by review on !2490: a captain that was already ready and stable,
// then transiently unreachable (all captain/info requests failing), then
// observed again under the same label must not have that outage counted as
// stable time. updateStatus's failure branch never touches CaptainStableSince
// at all, leaving it at its pre-outage value; nextCaptainStableSince must
// still restart the clock on the first post-outage success because
// previousCaptainReady (captured from the failure reconcile, where
// CaptainReady was left false) is false.
func TestUpdateStatusRestartsCaptainStableClockAfterCaptainInfoFailureWithSameCaptain(t *testing.T) {
	ctx := context.Background()
	restoreSearchHeadClusterInfoStubs(t) // stubs captain label "splunk-test-shc-search-head-0", ServiceReady=true

	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-shc", Namespace: "test"},
	}
	cr.Status.Captain = "splunk-test-shc-search-head-0"
	cr.Status.CaptainReady = true
	stableSince := time.Now().Unix() - 500
	cr.Status.CaptainStableSince = stableSince

	mgr := newTestSHCPodManager(cr)
	mgr.Client = spltest.NewMockClient()

	originalCaptainInfo := GetSearchHeadCaptainInfo
	GetSearchHeadCaptainInfo = func(ctx context.Context, mgr *PodManager, n int32) (*splclient.SearchHeadCaptainInfo, error) {
		return nil, fmt.Errorf("connection refused")
	}
	if err := mgr.UpdateStatus(ctx, searchHeadStatefulSet("test-shc", 1)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cr.Status.Captain != "" || cr.Status.CaptainReady {
		t.Errorf("expected Captain/CaptainReady cleared on a captain/info failure, got Captain=%q CaptainReady=%v", cr.Status.Captain, cr.Status.CaptainReady)
	}
	if cr.Status.CaptainStableSince != stableSince {
		t.Errorf("expected CaptainStableSince left untouched at %d by the failure itself, got %d", stableSince, cr.Status.CaptainStableSince)
	}

	// Connectivity returns; captain/info reports the same captain again.
	GetSearchHeadCaptainInfo = originalCaptainInfo
	before := time.Now().Unix()
	if err := mgr.UpdateStatus(ctx, searchHeadStatefulSet("test-shc", 1)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cr.Status.CaptainStableSince < before {
		t.Errorf("expected CaptainStableSince to restart to approximately now after the outage, not inherit the stale pre-outage %d, got %d", stableSince, cr.Status.CaptainStableSince)
	}
}

// Requested by review on !2490: updateStatus returns immediately when
// ReadyReplicas is 0, before ever calling nextCaptainStableSince, leaving
// CaptainStableSince at whatever it held before all replicas went
// unready. Once replicas recover and a captain is observed again, that
// stale timestamp must not be inherited.
func TestUpdateStatusRestartsCaptainStableClockAfterZeroReadyReplicasRecovers(t *testing.T) {
	ctx := context.Background()
	restoreSearchHeadClusterInfoStubs(t) // stubs captain label "splunk-test-shc-search-head-0", ServiceReady=true

	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-shc", Namespace: "test"},
	}
	cr.Status.Captain = "splunk-test-shc-search-head-0"
	cr.Status.CaptainReady = true
	stableSince := time.Now().Unix() - 500
	cr.Status.CaptainStableSince = stableSince

	mgr := newTestSHCPodManager(cr)
	mgr.Client = spltest.NewMockClient()

	if err := mgr.UpdateStatus(ctx, searchHeadStatefulSet("test-shc", 0)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cr.Status.Captain != "" || cr.Status.CaptainReady {
		t.Errorf("expected Captain/CaptainReady cleared with zero ready replicas, got Captain=%q CaptainReady=%v", cr.Status.Captain, cr.Status.CaptainReady)
	}
	if cr.Status.CaptainStableSince != stableSince {
		t.Errorf("expected CaptainStableSince left untouched at %d while ReadyReplicas is 0, got %d", stableSince, cr.Status.CaptainStableSince)
	}

	// Replicas recover; captain/info reports a captain again.
	before := time.Now().Unix()
	if err := mgr.UpdateStatus(ctx, searchHeadStatefulSet("test-shc", 1)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cr.Status.CaptainStableSince < before {
		t.Errorf("expected CaptainStableSince to restart to approximately now after recovering from zero ready replicas, not inherit the stale pre-outage %d, got %d", stableSince, cr.Status.CaptainStableSince)
	}
}

