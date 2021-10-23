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

package test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

func init() {
	MockObjectCopiers = append(MockObjectCopiers, coreObjectCopier, appsObjectCopier, enterpriseObjCopier)
}

// MockObjectCopiers is a slice of MockObjectCopier methods that MockClient uses to copy runtime.Objects
var MockObjectCopiers []MockObjectCopier

// MockObjectCopier is a method used to perform the typed copy of a runtime.Object from src to dst.
// It returns true if the runtime.Object was copied, or false if the type is unknown.
type MockObjectCopier func(dst, src *runtime.Object) bool

// enterpriseObjCopier is used to copy enterprise runtime.Objects
func enterpriseObjCopier(dst, src *runtime.Object) bool {
	dstP := *dst
	srcP := *src
	switch srcP.(type) {
	case *enterpriseApi.ClusterMaster:
		*dstP.(*enterpriseApi.ClusterMaster) = *srcP.(*enterpriseApi.ClusterMaster)
	case *enterpriseApi.IndexerCluster:
		*dstP.(*enterpriseApi.IndexerCluster) = *srcP.(*enterpriseApi.IndexerCluster)
	case *enterpriseApi.LicenseMaster:
		*dstP.(*enterpriseApi.LicenseMaster) = *srcP.(*enterpriseApi.LicenseMaster)
	case *enterpriseApi.SearchHeadCluster:
		*dstP.(*enterpriseApi.SearchHeadCluster) = *srcP.(*enterpriseApi.SearchHeadCluster)
	case *enterpriseApi.Standalone:
		*dstP.(*enterpriseApi.Standalone) = *srcP.(*enterpriseApi.Standalone)
	default:
		return false
	}
	return true
}

// coreObjectCopier is used to copy corev1 runtime.Objects
func coreObjectCopier(dst, src *runtime.Object) bool {
	dstP := *dst
	srcP := *src
	if reflect.TypeOf(dstP).String() == reflect.TypeOf((*src).(runtime.Object)).String() {
		switch (srcP).(type) {
		case *corev1.ConfigMap:
			*dstP.(*corev1.ConfigMap) = *srcP.(*corev1.ConfigMap)
		case *corev1.Secret:
			*dstP.(*corev1.Secret) = *srcP.(*corev1.Secret)
		case *corev1.SecretList:
			*dstP.(*corev1.SecretList) = *srcP.(*corev1.SecretList)
		case *corev1.PersistentVolumeClaim:
			*dstP.(*corev1.PersistentVolumeClaim) = *srcP.(*corev1.PersistentVolumeClaim)
		case *corev1.PersistentVolumeClaimList:
			*dstP.(*corev1.PersistentVolumeClaimList) = *srcP.(*corev1.PersistentVolumeClaimList)
		case *corev1.Service:
			*dstP.(*corev1.Service) = *srcP.(*corev1.Service)
		case *corev1.Pod:
			*dstP.(*corev1.Pod) = *srcP.(*corev1.Pod)
		case *corev1.ServiceAccount:
			*dstP.(*corev1.ServiceAccount) = *srcP.(*corev1.ServiceAccount)
		default:
			return false
		}
	}
	return true
}

// appsObjectCopier is used to copy appsv1 runtime.Objects
func appsObjectCopier(dst, src *runtime.Object) bool {
	srcP := *src
	dstP := *dst
	switch srcP.(type) {
	case *appsv1.Deployment:
		*dstP.(*appsv1.Deployment) = *srcP.(*appsv1.Deployment)
	case *appsv1.StatefulSet:
		*dstP.(*appsv1.StatefulSet) = *srcP.(*appsv1.StatefulSet)
	default:
		return false
	}
	return true
}

// copyMockObject uses the global MockObjectCopiers to perform the typed copy of a runtime.Object from src to dst
func copyMockObject(dst, src *runtime.Object) {
	for n := range MockObjectCopiers {
		if MockObjectCopiers[n](dst, src) {
			return
		}
	}
	srcP := *src
	// default if no types match
	*dst = srcP.DeepCopyObject()
}

// MockFuncCall is used to record a function call to MockClient methods
type MockFuncCall struct {
	CTX      context.Context
	Key      client.ObjectKey
	ListOpts []client.ListOption
	Obj      runtime.Object
	MetaName string
}

// MockStatusWriter is used to mock methods for the Kubernetes controller-runtime client
type MockStatusWriter struct {
	// Err is the error code that will be returned by function calls
	Err error

	// Calls is a record of all MockStatusWriter Update() calls
	Calls []MockFuncCall
}

// Update returns status writer's Err field
func (c MockStatusWriter) Update(ctx context.Context, obj runtime.Object, opts ...client.UpdateOption) error {
	c.Calls = append(c.Calls, MockFuncCall{
		CTX: ctx,
		Obj: obj,
	})
	return c.Err
}

// Patch returns status writer's Err field
func (c MockStatusWriter) Patch(ctx context.Context, obj runtime.Object, patch client.Patch, opts ...client.PatchOption) error {
	c.Calls = append(c.Calls, MockFuncCall{
		CTX: ctx,
		Obj: obj,
	})
	return c.Err
}

// blank assignment to verify that MockClient implements client.Client
var _ client.Client = &MockClient{}

// MockClient is used to mock methods for the Kubernetes controller-runtime client
type MockClient struct {
	// StatusWriter is a StatusWriter mock client returned by Status()
	StatusWriter MockStatusWriter

	// ListObj is used to assign obj parameter for List() calls
	ListObj runtime.Object

	// State is used to maintain a simple state of objects in the cluster, where key = <type>-<namespace>-<name>
	State map[string]interface{}

	// Calls is a record of all MockClient function Calls
	Calls map[string][]MockFuncCall

	// error returned when an object is not found
	NotFoundError error
}

// Get returns mock client's Err field
func (c MockClient) Get(ctx context.Context, key client.ObjectKey, obj runtime.Object) error {
	c.Calls["Get"] = append(c.Calls["Get"], MockFuncCall{
		CTX: ctx,
		Key: key,
		Obj: obj,
	})
	getObj := c.State[getStateKeyWithKey(key, obj)]

	if getObj != nil {
		srcObj := getObj.(runtime.Object)
		copyMockObject(&obj, &srcObj)
		return nil
	}
	return c.NotFoundError
}

// List returns mock client's Err field
func (c MockClient) List(ctx context.Context, obj runtime.Object, opts ...client.ListOption) error {
	c.Calls["List"] = append(c.Calls["List"], MockFuncCall{
		CTX:      ctx,
		ListOpts: opts,
		Obj:      obj,
	})
	listObj := c.ListObj
	if listObj != nil {
		srcObj := listObj.(runtime.Object)
		copyMockObject(&obj, &srcObj)
		return nil
	}
	return c.NotFoundError
}

// Create returns mock client's Err field
func (c MockClient) Create(ctx context.Context, obj runtime.Object, opts ...client.CreateOption) error {
	c.Calls["Create"] = append(c.Calls["Create"], MockFuncCall{
		CTX: ctx,
		Obj: obj,
	})
	c.State[getStateKey(obj)] = obj
	return nil
}

// Delete returns mock client's Err field
func (c MockClient) Delete(ctx context.Context, obj runtime.Object, opts ...client.DeleteOption) error {
	c.Calls["Delete"] = append(c.Calls["Delete"], MockFuncCall{
		CTX: ctx,
		Obj: obj,
	})
	c.State[getStateKey(obj)] = nil
	return nil
}

// Update returns mock client's Err field
func (c MockClient) Update(ctx context.Context, obj runtime.Object, opts ...client.UpdateOption) error {
	c.Calls["Update"] = append(c.Calls["Update"], MockFuncCall{
		CTX: ctx,
		Obj: obj,
	})
	c.State[getStateKey(obj)] = obj
	return nil
}

// Patch returns mock client's Err field
func (c MockClient) Patch(ctx context.Context, obj runtime.Object, patch client.Patch, opts ...client.PatchOption) error {
	c.Calls["Patch"] = append(c.Calls["Patch"], MockFuncCall{
		CTX: ctx,
		Obj: obj,
	})
	return nil
}

// DeleteAllOf returns mock client's Err field
func (c MockClient) DeleteAllOf(ctx context.Context, obj runtime.Object, opts ...client.DeleteAllOfOption) error {
	c.Calls["DeleteAllOf"] = append(c.Calls["DeleteAllOf"], MockFuncCall{
		CTX: ctx,
		Obj: obj,
	})
	return nil
}

// Status returns the mock client's StatusWriter
func (c MockClient) Status() client.StatusWriter {
	return c.StatusWriter
}

// ResetCalls resets the function call tracker
func (c *MockClient) ResetCalls() {
	c.Calls = make(map[string][]MockFuncCall)
}

// ResetState resets the state of the MockClient
func (c *MockClient) ResetState() {
	c.State = make(map[string]interface{})
}

// AddObject adds an object to the MockClient's state
func (c *MockClient) AddObject(obj runtime.Object) {
	c.State[getStateKey(obj)] = obj
}

// AddObjects adds multiple objects to the MockClient's state
func (c *MockClient) AddObjects(objects []runtime.Object) {
	for _, obj := range objects {
		c.State[getStateKey(obj)] = obj
	}
}

// CheckCalls verifies that the wanted function calls were performed
func (c *MockClient) CheckCalls(t *testing.T, testname string, wantCalls map[string][]MockFuncCall) {
	notEmptyWantCalls := 0

	for methodName, wantFuncCalls := range wantCalls {
		gotFuncCalls, ok := c.Calls[methodName]

		if len(wantFuncCalls) > 0 {
			if !ok {
				t.Fatalf("%s: MockClient %s() calls = 0; want %d", testname, methodName, len(wantFuncCalls))
			}
			notEmptyWantCalls++
		}

		//t.Fatalf("%s: MockClient %s() calls = %d; want %d, got: %s \n want: %s", testname, methodName, len(gotFuncCalls), len(wantFuncCalls), gotFuncCalls, wantFuncCalls)
		if len(gotFuncCalls) != len(wantFuncCalls) {
			//t.Fatalf("%s: MockClient %s() calls = %d; want %d", testname, methodName, len(gotFuncCalls), len(wantFuncCalls))
			t.Fatalf("%s: MockClient %s() calls = %d; want %d, got: %s \n want: %s", testname, methodName, len(gotFuncCalls), len(wantFuncCalls), gotFuncCalls, wantFuncCalls)
		}

		for n := range wantFuncCalls {
			if methodName == "List" {
				if !reflect.DeepEqual(gotFuncCalls[n].ListOpts, wantFuncCalls[n].ListOpts) {
					t.Errorf("%s: MockClient %s() call #%d = %v; want %v", testname, methodName, n, gotFuncCalls[n].ListOpts, wantFuncCalls[n].ListOpts)
				}
			} else {
				if wantFuncCalls[n].MetaName != "" {
					var got string
					if methodName == "Get" {
						got = getStateKeyWithKey(gotFuncCalls[n].Key, gotFuncCalls[n].Obj)
					} else {
						got = getStateKey(gotFuncCalls[n].Obj)
					}
					if got != wantFuncCalls[n].MetaName {
						t.Errorf("%s: MockClient %s() call #%d = %s; want %s", testname, methodName, n, got, wantFuncCalls[n].MetaName)
					}
				} else {
					if !reflect.DeepEqual(gotFuncCalls[n], wantFuncCalls[n]) {
						t.Errorf("%s: MockClient %s() call #%d = %v; want %v", testname, methodName, n, gotFuncCalls[n], wantFuncCalls[n])
					}
				}
			}
		}
	}

	if notEmptyWantCalls != len(c.Calls) {
		t.Errorf("%s: MockClient functions called = %d; want %d: calls=%v", testname, len(c.Calls), len(wantCalls), c.Calls)
	}
}

// NewMockClient is used to create and initialize a new mock client
func NewMockClient() *MockClient {
	c := &MockClient{
		State:         make(map[string]interface{}),
		Calls:         make(map[string][]MockFuncCall),
		NotFoundError: errors.New("NotFound"),
	}
	return c
}

// getStateKeyFromObject returns a lookup key for the MockClient's state map
func getStateKey(obj runtime.Object) string {
	key := client.ObjectKey{
		Name:      obj.(splcommon.MetaObject).GetName(),
		Namespace: obj.(splcommon.MetaObject).GetNamespace(),
	}
	return getStateKeyWithKey(key, obj)
}

// getStateKey returns a lookup key for the MockClient's state map
func getStateKeyWithKey(key client.ObjectKey, obj runtime.Object) string {
	kind := reflect.TypeOf(obj).String()
	return fmt.Sprintf("%s-%s-%s", kind, key.Namespace, key.Name)
}

// testReconcileForResource is used to test create and update reconcile operations
func testReconcileForResource(t *testing.T, c *MockClient, methodPlus string, resource interface{},
	calls map[string][]MockFuncCall,
	reconcile func(*MockClient, interface{}) error) {

	c.ResetCalls()
	err := reconcile(c, resource)
	if err != nil {
		t.Errorf("%s returned %v; want nil", methodPlus, err)
	}
	c.CheckCalls(t, methodPlus, calls)
}

// ReconcileTester is used to test create and update reconcile operations
func ReconcileTester(t *testing.T, method string,
	current, revised interface{},
	createCalls, updateCalls map[string][]MockFuncCall,
	reconcile func(*MockClient, interface{}) error,
	listInvolved bool,
	initObjects ...runtime.Object) {

	// initialize client
	c := NewMockClient()
	c.AddObjects(initObjects)

	// test create new
	methodPlus := fmt.Sprintf("%s(create)", method)
	testReconcileForResource(t, c, methodPlus, current, createCalls, reconcile)

	// test no updates required for current
	methodPlus = fmt.Sprintf("%s(update-no-change)", method)
	var updateNoChangecalls map[string][]MockFuncCall
	if listInvolved {
		updateNoChangecalls = map[string][]MockFuncCall{"Get": createCalls["Get"], "List": createCalls["List"]}
	} else {
		updateNoChangecalls = map[string][]MockFuncCall{"Get": createCalls["Get"]}
	}
	testReconcileForResource(t, c, methodPlus, current, updateNoChangecalls, reconcile)

	// test updates required
	methodPlus = fmt.Sprintf("%s(update-with-change)", method)
	testReconcileForResource(t, c, methodPlus, revised, updateCalls, reconcile)
}

// ReconcileTesterWithoutRedundantCheck is used to test create and update reconcile operations
func ReconcileTesterWithoutRedundantCheck(t *testing.T, method string,
	current, revised interface{},
	createCalls, updateCalls map[string][]MockFuncCall,
	reconcile func(*MockClient, interface{}) error,
	listInvolved bool,
	initObjects ...runtime.Object) {

	// initialize client
	c := NewMockClient()
	c.AddObjects(initObjects)

	// test create new
	methodPlus := fmt.Sprintf("%s(create)", method)
	testReconcileForResource(t, c, methodPlus, current, createCalls, reconcile)

	// test updates required
	methodPlus = fmt.Sprintf("%s(update-with-change)", method)
	testReconcileForResource(t, c, methodPlus, revised, updateCalls, reconcile)
}

// PodManagerUpdateTester is used to a single reconcile update using a StatefulSetPodManager
func PodManagerUpdateTester(t *testing.T, method string, mgr splcommon.StatefulSetPodManager,
	desiredReplicas int32, wantPhase splcommon.Phase, statefulSet *appsv1.StatefulSet,
	wantCalls map[string][]MockFuncCall, wantError error, initObjects ...runtime.Object) {

	// initialize client
	c := NewMockClient()
	c.AddObjects(initObjects)

	// test update
	gotPhase, err := mgr.Update(c, statefulSet, desiredReplicas)
	if (err == nil && wantError != nil) ||
		(err != nil && wantError == nil) ||
		(err != nil && wantError != nil && err.Error() != wantError.Error()) {
		t.Errorf("%s returned error %v; want %v", method, err, wantError)
	}
	if gotPhase != wantPhase {
		t.Errorf("%s returned phase=%s; want %s", method, gotPhase, wantPhase)
	}

	// check calls
	c.CheckCalls(t, method, wantCalls)
}

// PodManagerTester is used to test StatefulSetPodManager reconcile operations
func PodManagerTester(t *testing.T, method string, mgr splcommon.StatefulSetPodManager) {
	// test create
	funcCalls := []MockFuncCall{
		{MetaName: "*v1.StatefulSet-test-splunk-stack1"},
		{MetaName: "*v1.Pod-test-splunk-stack1-0"},
	}
	createCalls := map[string][]MockFuncCall{"Get": {funcCalls[0]}, "Create": {funcCalls[0]}}
	var replicas int32 = 1
	current := &appsv1.StatefulSet{
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
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-0",
			Namespace: "test",
			Labels: map[string]string{
				"controller-revision-hash": "v0",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Ready: true},
			},
		},
	}

	PodManagerUpdateTester(t, method, mgr, 1, splcommon.PhasePending, current, createCalls, nil)

	// test update
	revised := current.DeepCopy()
	revised.Spec.Template.ObjectMeta.Labels = map[string]string{"one": "two"}
	updateCalls := map[string][]MockFuncCall{"Get": {funcCalls[0]}, "Update": {funcCalls[0]}}
	methodPlus := fmt.Sprintf("%s(%s)", method, "Update StatefulSet")
	PodManagerUpdateTester(t, methodPlus, mgr, 1, splcommon.PhaseUpdating, revised, updateCalls, nil, current)

	// test scale up (zero ready so far; wait for ready)
	revised = current.DeepCopy()
	current.Status.ReadyReplicas = 0
	scaleUpCalls := map[string][]MockFuncCall{"Get": {funcCalls[0]}}
	methodPlus = fmt.Sprintf("%s(%s)", method, "ScalingUp, 0 ready")
	PodManagerUpdateTester(t, methodPlus, mgr, 1, splcommon.PhasePending, revised, scaleUpCalls, nil, current)

	// test scale up (1 ready scaling to 2; wait for ready)
	replicas = 2
	current.Status.Replicas = 2
	current.Status.ReadyReplicas = 1
	methodPlus = fmt.Sprintf("%s(%s)", method, "ScalingUp, 1/2 ready")
	PodManagerUpdateTester(t, methodPlus, mgr, 2, splcommon.PhaseScalingUp, revised, scaleUpCalls, nil, current, pod)

	// test scale up (1 ready scaling to 2)
	replicas = 1
	current.Status.Replicas = 1
	current.Status.ReadyReplicas = 1
	methodPlus = fmt.Sprintf("%s(%s)", method, "ScalingUp, Update Replicas 1=>2")
	PodManagerUpdateTester(t, methodPlus, mgr, 2, splcommon.PhaseScalingUp, revised, updateCalls, nil, current, pod)

	// test scale down (2 ready, 1 desired)
	replicas = 1
	current.Status.Replicas = 1
	current.Status.ReadyReplicas = 2
	delete(scaleUpCalls, "Update")
	methodPlus = fmt.Sprintf("%s(%s)", method, "ScalingDown, Ready > Replicas")
	PodManagerUpdateTester(t, methodPlus, mgr, 1, splcommon.PhaseScalingDown, revised, scaleUpCalls, nil, current, pod)

	// test scale down (2 ready scaling down to 1)
	pvcCalls := []MockFuncCall{
		{MetaName: "*v1.PersistentVolumeClaim-test-pvc-etc-splunk-stack1-1"},
		{MetaName: "*v1.PersistentVolumeClaim-test-pvc-var-splunk-stack1-1"},
	}
	scaleDownCalls := map[string][]MockFuncCall{
		"Get":    {funcCalls[0], pvcCalls[0], pvcCalls[1]},
		"Update": {funcCalls[0]},
		"Delete": pvcCalls,
	}
	pvcList := []*corev1.PersistentVolumeClaim{
		{ObjectMeta: metav1.ObjectMeta{Name: "pvc-etc-splunk-stack1-1", Namespace: "test"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "pvc-var-splunk-stack1-1", Namespace: "test"}},
	}
	replicas = 2
	current.Status.Replicas = 2
	current.Status.ReadyReplicas = 2
	methodPlus = fmt.Sprintf("%s(%s)", method, "ScalingDown, Update Replicas 2=>1")
	PodManagerUpdateTester(t, methodPlus, mgr, 1, splcommon.PhaseScalingDown, revised, scaleDownCalls, nil, current, pod, pvcList[0], pvcList[1])

	// test pod not found
	replicas = 1
	current.Status.Replicas = 1
	current.Status.ReadyReplicas = 1
	podCalls := []MockFuncCall{funcCalls[0], {MetaName: "*v1.Pod-test-splunk-stack1-0"}}
	getPodCalls := map[string][]MockFuncCall{"Get": podCalls}
	methodPlus = fmt.Sprintf("%s(%s)", method, "Pod not found")
	PodManagerUpdateTester(t, methodPlus, mgr, 1, splcommon.PhaseError, revised, getPodCalls, errors.New("NotFound"), current)

	labels := map[string]string{
		"app.kubernetes.io/component":  "versionedSecrets",
		"app.kubernetes.io/managed-by": "splunk-operator",
	}
	listOpts := []client.ListOption{
		client.InNamespace("test"),
		client.MatchingLabels(labels),
	}
	listmockCall := []MockFuncCall{
		{ListOpts: listOpts}}

	delCalls := []MockFuncCall{{MetaName: "*v1.Pod-test-splunk-stack1-0"}}
	updatePodCalls := map[string][]MockFuncCall{"Get": podCalls, "Delete": delCalls}
	methodPlus = fmt.Sprintf("%s(%s)", method, "Recycle pod")
	PodManagerUpdateTester(t, methodPlus, mgr, 1, splcommon.PhaseUpdating, revised, updatePodCalls, nil, current, pod)

	// test all pods ready
	pod.ObjectMeta.Labels["controller-revision-hash"] = "v1"
	methodPlus = fmt.Sprintf("%s(%s)", method, "All pods ready")

	getPodCalls["List"] = []MockFuncCall{listmockCall[0]}
	PodManagerUpdateTester(t, methodPlus, mgr, 1, splcommon.PhaseReady, revised, getPodCalls, nil, current, pod)

	// test pod not ready
	pod.Status.Phase = corev1.PodPending
	delete(getPodCalls, "List")
	methodPlus = fmt.Sprintf("%s(%s)", method, "Pod not ready")
	PodManagerUpdateTester(t, methodPlus, mgr, 1, splcommon.PhaseUpdating, revised, getPodCalls, nil, current, pod)
}
