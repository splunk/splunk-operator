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

package deploy

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// copyResource is used to copy from src to dst object
func copyResource(dst runtime.Object, src runtime.Object) {
	switch src.(type) {
	case *corev1.ConfigMap:
		*dst.(*corev1.ConfigMap) = *src.(*corev1.ConfigMap)
	case *corev1.Secret:
		*dst.(*corev1.Secret) = *src.(*corev1.Secret)
	case *corev1.PersistentVolumeClaimList:
		*dst.(*corev1.PersistentVolumeClaimList) = *src.(*corev1.PersistentVolumeClaimList)
	case *appsv1.Deployment:
		*dst.(*appsv1.Deployment) = *src.(*appsv1.Deployment)
	case *appsv1.StatefulSet:
		*dst.(*appsv1.StatefulSet) = *src.(*appsv1.StatefulSet)
	case *enterprisev1.Indexer:
		*dst.(*enterprisev1.Indexer) = *src.(*enterprisev1.Indexer)
	case *enterprisev1.LicenseMaster:
		*dst.(*enterprisev1.LicenseMaster) = *src.(*enterprisev1.LicenseMaster)
	case *enterprisev1.SearchHead:
		*dst.(*enterprisev1.SearchHead) = *src.(*enterprisev1.SearchHead)
	case *enterprisev1.Spark:
		*dst.(*enterprisev1.Spark) = *src.(*enterprisev1.Spark)
	case *enterprisev1.Standalone:
		*dst.(*enterprisev1.Standalone) = *src.(*enterprisev1.Standalone)
	default:
		dst = src
	}
}

// getStateKeyFromObject returns a lookup key for the mockClient's state map
func getStateKey(obj runtime.Object) string {
	key := client.ObjectKey{
		Name:      obj.(ResourceObject).GetObjectMeta().GetName(),
		Namespace: obj.(ResourceObject).GetObjectMeta().GetNamespace(),
	}
	return getStateKeyWithKey(key, obj)
}

// getStateKey returns a lookup key for the mockClient's state map
func getStateKeyWithKey(key client.ObjectKey, obj runtime.Object) string {
	kind := reflect.TypeOf(obj).String()
	//_, kind := obj.GetObjectKind().GroupVersionKind().ToAPIVersionAndKind()
	return fmt.Sprintf("%s-%s-%s", kind, key.Namespace, key.Name)
}

// mockFuncCall is used to record a function call to mockClient methods
type mockFuncCall struct {
	ctx      context.Context
	key      client.ObjectKey
	listOpts []client.ListOption
	obj      runtime.Object
	metaName string
}

// mockStatusWriter is used to mock methods for the Kubernetes controller-runtime client
type mockStatusWriter struct {
	// err is the error code that will be returned by function calls
	err error

	// calls is a record of all mockStatusWriter Update() calls
	calls []mockFuncCall
}

// Update returns status writer's err field
func (c mockStatusWriter) Update(ctx context.Context, obj runtime.Object, opts ...client.UpdateOption) error {
	c.calls = append(c.calls, mockFuncCall{
		ctx: ctx,
		obj: obj,
	})
	return c.err
}

// Patch returns status writer's err field
func (c mockStatusWriter) Patch(ctx context.Context, obj runtime.Object, patch client.Patch, opts ...client.PatchOption) error {
	c.calls = append(c.calls, mockFuncCall{
		ctx: ctx,
		obj: obj,
	})
	return c.err
}

// mockClient is used to mock methods for the Kubernetes controller-runtime client
type mockClient struct {
	// status is a StatusWriter mock client returned by Status()
	status mockStatusWriter

	// listObj is used to assign obj parameter for List() calls
	listObj runtime.Object

	// state is used to maintain a simple state of objects in the cluster, where key = <type>-<namespace>-<name>
	state map[string]interface{}

	// calls is a record of all mockClient function calls
	calls map[string][]mockFuncCall
}

// Get returns mock client's err field
func (c mockClient) Get(ctx context.Context, key client.ObjectKey, obj runtime.Object) error {
	c.calls["Get"] = append(c.calls["Get"], mockFuncCall{
		ctx: ctx,
		key: key,
		obj: obj,
	})
	getObj := c.state[getStateKeyWithKey(key, obj)]
	if getObj != nil {
		copyResource(obj, getObj.(runtime.Object))
		return nil
	}
	return fmt.Errorf("NotFound")
}

// List returns mock client's err field
func (c mockClient) List(ctx context.Context, obj runtime.Object, opts ...client.ListOption) error {
	c.calls["List"] = append(c.calls["List"], mockFuncCall{
		ctx:      ctx,
		listOpts: opts,
		obj:      obj,
	})
	listObj := c.listObj
	if listObj != nil {
		copyResource(obj, listObj.(runtime.Object))
		return nil
	}
	return fmt.Errorf("NotFound")
}

// Create returns mock client's err field
func (c mockClient) Create(ctx context.Context, obj runtime.Object, opts ...client.CreateOption) error {
	c.calls["Create"] = append(c.calls["Create"], mockFuncCall{
		ctx: ctx,
		obj: obj,
	})
	c.state[getStateKey(obj)] = obj
	return nil
}

// Delete returns mock client's err field
func (c mockClient) Delete(ctx context.Context, obj runtime.Object, opts ...client.DeleteOption) error {
	c.calls["Delete"] = append(c.calls["Delete"], mockFuncCall{
		ctx: ctx,
		obj: obj,
	})
	c.state[getStateKey(obj)] = nil
	return nil
}

// Update returns mock client's err field
func (c mockClient) Update(ctx context.Context, obj runtime.Object, opts ...client.UpdateOption) error {
	c.calls["Update"] = append(c.calls["Update"], mockFuncCall{
		ctx: ctx,
		obj: obj,
	})
	c.state[getStateKey(obj)] = obj
	return nil
}

// Patch returns mock client's err field
func (c mockClient) Patch(ctx context.Context, obj runtime.Object, patch client.Patch, opts ...client.PatchOption) error {
	c.calls["Patch"] = append(c.calls["Patch"], mockFuncCall{
		ctx: ctx,
		obj: obj,
	})
	return nil
}

// DeleteAllOf returns mock client's err field
func (c mockClient) DeleteAllOf(ctx context.Context, obj runtime.Object, opts ...client.DeleteAllOfOption) error {
	c.calls["DeleteAllOf"] = append(c.calls["DeleteAllOf"], mockFuncCall{
		ctx: ctx,
		obj: obj,
	})
	return nil
}

// Status returns the mock client's StatusWriter
func (c mockClient) Status() client.StatusWriter {
	return c.status
}

// resetCalls resets the function call tracker
func (c *mockClient) resetCalls() {
	c.calls = make(map[string][]mockFuncCall)
}

// checkCalls verifies that the wanted function calls were performed
func (c *mockClient) checkCalls(t *testing.T, testname string, wantCalls map[string][]mockFuncCall) {
	notEmptyWantCalls := 0

	for methodName, wantFuncCalls := range wantCalls {
		gotFuncCalls, ok := c.calls[methodName]

		if len(wantFuncCalls) > 0 {
			if !ok {
				t.Fatalf("%s: MockClient %s() calls = 0; want %d", testname, methodName, len(wantFuncCalls))
			}
			notEmptyWantCalls++
		}

		if len(gotFuncCalls) != len(wantFuncCalls) {
			t.Fatalf("%s: MockClient %s() calls = %d; want %d", testname, methodName, len(gotFuncCalls), len(wantFuncCalls))
		}

		for n := range wantFuncCalls {
			if methodName == "List" {
				if !reflect.DeepEqual(gotFuncCalls[n].listOpts, wantFuncCalls[n].listOpts) {
					t.Errorf("%s: MockClient %s() call #%d = %v; want %v", testname, methodName, n, gotFuncCalls[n].listOpts, wantFuncCalls[n].listOpts)
				}
			} else {
				if wantFuncCalls[n].metaName != "" {
					var got string
					if methodName == "Get" {
						got = getStateKeyWithKey(gotFuncCalls[n].key, gotFuncCalls[n].obj)
					} else {
						got = getStateKey(gotFuncCalls[n].obj)
					}
					if got != wantFuncCalls[n].metaName {
						t.Errorf("%s: MockClient %s() call #%d = %s; want %s", testname, methodName, n, got, wantFuncCalls[n].metaName)
					}
				} else {
					if !reflect.DeepEqual(gotFuncCalls[n], wantFuncCalls[n]) {
						t.Errorf("%s: MockClient %s() call #%d = %v; want %v", testname, methodName, n, gotFuncCalls[n], wantFuncCalls[n])
					}
				}
			}
		}
	}

	if notEmptyWantCalls != len(c.calls) {
		t.Errorf("%s: MockClient functions called = %d; want %d", testname, len(c.calls), len(wantCalls))
	}
}

// newMockClient is used to create and initialize a new mock client
func newMockClient() *mockClient {
	c := &mockClient{
		state: make(map[string]interface{}),
		calls: make(map[string][]mockFuncCall),
	}
	return c
}

func reconcileTester(t *testing.T, method string,
	current, revised interface{},
	createCalls, updateCalls map[string][]mockFuncCall,
	reconcile func(*mockClient, interface{}) error) {

	// test create new
	c := newMockClient()
	err := reconcile(c, current)
	if err != nil {
		t.Errorf("%s() returned %v; want nil", method, err)
	}
	c.checkCalls(t, method, createCalls)

	// test no updates required for current
	c.resetCalls()
	err = reconcile(c, current)
	if err != nil {
		t.Errorf("%s() returned %v; want nil", method, err)
	}
	c.checkCalls(t, method, map[string][]mockFuncCall{"Get": createCalls["Get"]})

	// test updates required
	c.resetCalls()
	err = reconcile(c, revised)
	if err != nil {
		t.Errorf("%s() returned %v; want nil", method, err)
	}
	c.checkCalls(t, method, updateCalls)

	// test no updates required for revised
	c.resetCalls()
	err = reconcile(c, revised)
	if err != nil {
		t.Errorf("%s() returned %v; want nil", method, err)
	}
	c.checkCalls(t, method, map[string][]mockFuncCall{"Get": updateCalls["Get"]})
}

func TestCreateResource(t *testing.T) {
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret",
			Namespace: "test",
		},
		Data: map[string][]byte{"one": []byte("value1")},
	}

	c := newMockClient()
	err := CreateResource(c, &secret)
	if err != nil {
		t.Errorf("CreateResource() returned %v; want nil", err)
	}
	c.checkCalls(t, "TestCreateResource", map[string][]mockFuncCall{
		"Create": {
			{ctx: context.TODO(), obj: &secret},
		},
	})
}

func TestUpdateResource(t *testing.T) {
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret",
			Namespace: "test",
		},
		Data: map[string][]byte{"one": []byte("value1")},
	}

	c := newMockClient()
	err := UpdateResource(c, &secret)
	if err != nil {
		t.Errorf("UpdateResource() returned %v; want nil", err)
	}
	c.checkCalls(t, "TestUpdateResource", map[string][]mockFuncCall{
		"Update": {
			{ctx: context.TODO(), obj: &secret},
		},
	})
}

func TestMergePodUpdates(t *testing.T) {
	var current, revised corev1.PodTemplateSpec
	name := "test-pod"
	matcher := func() bool { return false }

	podUpdateTester := func(param string) {
		if !MergePodUpdates(&current, &revised, name) {
			t.Errorf("MergePodUpdates() returned %t; want %t", false, true)
		}
		if !matcher() {
			t.Errorf("MergePodUpdates() to detect change: %s", param)
		}
		if MergePodUpdates(&current, &revised, name) {
			t.Errorf("MergePodUpdates() re-run returned %t; want %t", true, false)
		}

	}

	// should be no updates to merge if they are empty
	if MergePodUpdates(&current, &revised, name) {
		t.Errorf("MergePodUpdates() returned %t; want %t", true, false)
	}

	// check Affinity
	revised.Spec.Affinity = &corev1.Affinity{
		PodAffinity: &corev1.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{Namespaces: []string{"test"}},
			},
		},
	}
	matcher = func() bool { return current.Spec.Affinity == revised.Spec.Affinity }
	podUpdateTester("Affinity")

	// check SchedulerName
	revised.Spec.SchedulerName = "gp2"
	matcher = func() bool { return current.Spec.SchedulerName == revised.Spec.SchedulerName }
	podUpdateTester("SchedulerName")

	// check container different Annotations
	revised.ObjectMeta.Annotations = map[string]string{"one": "two"}
	matcher = func() bool { return reflect.DeepEqual(current.ObjectMeta.Annotations, revised.ObjectMeta.Annotations) }
	podUpdateTester("Annotations")

	// check container different Labels
	revised.ObjectMeta.Labels = map[string]string{"one": "two"}
	matcher = func() bool { return reflect.DeepEqual(current.ObjectMeta.Labels, revised.ObjectMeta.Labels) }
	podUpdateTester("Labels")

	// check new container added
	revised.Spec.Containers = []corev1.Container{{Image: "splunk/splunk"}}
	matcher = func() bool { return reflect.DeepEqual(current.Spec.Containers, revised.Spec.Containers) }
	podUpdateTester("Container added")

	// check container different Image
	revised.Spec.Containers = []corev1.Container{{Image: "splunk/spark"}}
	matcher = func() bool { return reflect.DeepEqual(current.Spec.Containers, revised.Spec.Containers) }
	podUpdateTester("Container Image")

	// check container different Ports
	revised.Spec.Containers[0].Ports = []corev1.ContainerPort{{ContainerPort: 8000}}
	matcher = func() bool { return reflect.DeepEqual(current.Spec.Containers, revised.Spec.Containers) }
	podUpdateTester("Container Ports")

	// check container different VolumeMounts
	revised.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "mnt-spark"}}
	matcher = func() bool { return reflect.DeepEqual(current.Spec.Containers, revised.Spec.Containers) }
	podUpdateTester("Container VolumeMounts")

	// check container different Resources
	revised.Spec.Containers[0].Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("0.25"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
	}
	matcher = func() bool { return reflect.DeepEqual(current.Spec.Containers, revised.Spec.Containers) }
	podUpdateTester("Container Resources")

	// check container removed
	revised.Spec.Containers = []corev1.Container{}
	matcher = func() bool { return reflect.DeepEqual(current.Spec.Containers, revised.Spec.Containers) }
	podUpdateTester("Container removed")
}
