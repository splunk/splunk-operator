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
	"io/ioutil"
	"log"
	"os"
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
	default:
		dst = src
	}
}

// mockFuncCall is used to record a function call to mockClient methods
type mockFuncCall struct {
	ctx      context.Context
	key      client.ObjectKey
	opts     *client.ListOptions
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
func (c mockStatusWriter) Update(ctx context.Context, obj runtime.Object) error {
	c.calls = append(c.calls, mockFuncCall{
		ctx: ctx,
		obj: obj,
	})
	return c.err
}

// mockClient is used to mock methods for the Kubernetes controller-runtime client
type mockClient struct {
	// getObj is used to assign obj parameter for Get()
	getObj runtime.Object

	// listObj is used to assign obj parameter for List()
	listObj runtime.Object

	// status is a StatusWriter mock client returned by Status()
	status mockStatusWriter

	// errors keeps track of what is returned by function calls, where key = method name
	errors map[string]error

	// calls is a record of all mockClient function calls
	calls map[string][]mockFuncCall
}

// Get returns mock client's err field
func (c mockClient) Get(ctx context.Context, key client.ObjectKey, obj runtime.Object) error {
	c.calls["Get"] = append(c.calls["Get"], mockFuncCall{
		ctx: ctx,
		key: key,
	})
	if c.errors["Get"] == nil && c.getObj != nil {
		copyResource(obj, c.getObj)
	}
	return c.errors["Get"]
}

// List returns mock client's err field
func (c mockClient) List(ctx context.Context, opts *client.ListOptions, obj runtime.Object) error {
	c.calls["List"] = append(c.calls["List"], mockFuncCall{
		ctx:  ctx,
		opts: opts,
		obj:  obj,
	})
	if c.errors["List"] == nil && c.listObj != nil {
		copyResource(obj, c.listObj)
	}
	return c.errors["List"]
}

// Create returns mock client's err field
func (c mockClient) Create(ctx context.Context, obj runtime.Object) error {
	c.calls["Create"] = append(c.calls["Create"], mockFuncCall{
		ctx: ctx,
		obj: obj,
	})
	return c.errors["Create"]
}

// Delete returns mock client's err field
func (c mockClient) Delete(ctx context.Context, obj runtime.Object, opts ...client.DeleteOptionFunc) error {
	c.calls["Delete"] = append(c.calls["Delete"], mockFuncCall{
		ctx: ctx,
		obj: obj,
	})
	return c.errors["Delete"]
}

// Update returns mock client's err field
func (c mockClient) Update(ctx context.Context, obj runtime.Object) error {
	c.calls["Update"] = append(c.calls["Update"], mockFuncCall{
		ctx: ctx,
		obj: obj,
	})
	return c.errors["Update"]
}

// Status returns the mock client's StatusWriter
func (c mockClient) Status() client.StatusWriter {
	return c.status
}

// newMockClient is used to create and initialize a new mock client
func newMockClient() *mockClient {
	c := &mockClient{
		calls: make(map[string][]mockFuncCall),
		errors: map[string]error{
			"Get":    nil,
			"List":   nil,
			"Create": nil,
			"Delete": nil,
			"Update": nil,
		},
	}
	return c
}

// checkCalls verifies that the wanted function calls were performed
func (c *mockClient) checkCalls(t *testing.T, metaOnly bool, testname string, wantCalls map[string][]mockFuncCall) {
	for methodName, wantFuncCalls := range wantCalls {
		gotFuncCalls, ok := c.calls[methodName]
		if !ok {
			t.Fatalf("%s: MockClient %s() calls = 0; want %d", testname, methodName, len(wantFuncCalls))
		}

		if len(gotFuncCalls) != len(wantFuncCalls) {
			t.Fatalf("%s: MockClient %s() calls = %d; want %d", testname, methodName, len(gotFuncCalls), len(wantFuncCalls))
		}

		for n := range wantFuncCalls {
			if metaOnly {
				switch methodName {
				case "Get":
					if gotFuncCalls[n].key.Name != wantFuncCalls[n].metaName {
						t.Errorf("%s: MockClient %s() call #%d = %s; want %s", testname, methodName, n, gotFuncCalls[n].key.Name, wantFuncCalls[n].metaName)
					}
				case "List":
					if !reflect.DeepEqual(gotFuncCalls[n].opts, wantFuncCalls[n].opts) {
						t.Errorf("%s: MockClient %s() call #%d = %v; want %v", testname, methodName, n, gotFuncCalls[n].opts, wantFuncCalls[n].opts)
					}
				default:
					if gotFuncCalls[n].obj.(ResourceObject).GetObjectMeta().GetName() != wantFuncCalls[n].metaName {
						t.Errorf("%s: MockClient %s() call #%d = %s; want %s", testname, methodName, n,
							gotFuncCalls[n].obj.(ResourceObject).GetObjectMeta().GetName(),
							wantFuncCalls[n].metaName)
					}
				}
			} else {
				if gotFuncCalls[n] != wantFuncCalls[n] {
					t.Errorf("%s: MockClient %s() call #%d = %v; want %v", testname, methodName, n, gotFuncCalls[n], wantFuncCalls[n])
				}
			}
		}
	}

	if len(wantCalls) != len(c.calls) {
		t.Errorf("%s: MockClient functions called = %d; want %d", testname, len(c.calls), len(wantCalls))
	}
}

// TestMain() is called before any test cases; we use it to disable logging
func TestMain(m *testing.M) {
	log.SetOutput(ioutil.Discard)
	os.Exit(m.Run())
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
	c.checkCalls(t, false, "TestCreateResource", map[string][]mockFuncCall{
		"Create": {
			{ctx: context.TODO(), obj: &secret},
		},
	})

	c.errors["Create"] = fmt.Errorf("Unknown")
	err = CreateResource(c, &secret)
	if err != c.errors["Create"] {
		t.Errorf("CreateResource() returned %v; want %v", err, c.errors["Create"])
	}
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
	c.checkCalls(t, false, "TestUpdateResource", map[string][]mockFuncCall{
		"Update": {
			{ctx: context.TODO(), obj: &secret},
		},
	})

	c.errors["Update"] = fmt.Errorf("Unknown")
	err = UpdateResource(c, &secret)
	if err != c.errors["Update"] {
		t.Errorf("UpdateResource() returned %v; want %v", err, c.errors["Update"])
	}
}

func applyTester(t *testing.T, testname string, method string, obj ResourceObject, f func(c *mockClient) error) {
	getKey := types.NamespacedName{
		Name:      obj.GetObjectMeta().GetName(),
		Namespace: obj.GetObjectMeta().GetNamespace(),
	}
	getArgs := mockFuncCall{ctx: context.TODO(), key: getKey}
	createArgs := mockFuncCall{ctx: context.TODO(), obj: obj}

	c := newMockClient()
	err := f(c)
	if err != nil {
		t.Errorf("%s returned %v; want nil", method, err)
	}
	// by default, client.Get() returns nil which indicates it found an existing object
	// in this case, Apply*() does nothing else because updates are not supported
	c.checkCalls(t, false, testname, map[string][]mockFuncCall{
		"Get": {getArgs},
	})

	c = newMockClient()
	c.errors["Get"] = fmt.Errorf("NotFound")
	err = f(c)
	if err != nil {
		t.Errorf("%s returned %v; want nil", method, err)
	}
	// when client.Get() returns an error, Apply*() assumes it was not found
	// and should call client.Create()
	c.checkCalls(t, false, testname, map[string][]mockFuncCall{
		"Get":    {getArgs},
		"Create": {createArgs},
	})

	c = newMockClient()
	c.errors["Get"] = fmt.Errorf("NotFound")
	c.errors["Create"] = fmt.Errorf("Unknown")
	err = f(c)
	// client.Get() and client.Create() should be called, but Apply*() should
	// return an error since client.Create() returns an error
	if err == nil {
		t.Errorf("%s returned nil; want %v", method, c.errors["Create"])
	}
	c.checkCalls(t, false, testname, map[string][]mockFuncCall{
		"Get":    {getArgs},
		"Create": {createArgs},
	})
}

func TestApplyConfigMap(t *testing.T) {
	obj := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	f := func(c *mockClient) error { return ApplyConfigMap(c, &obj) }
	applyTester(t, "ApplyConfigMap()", "TestApplyConfigMap", &obj, f)
}

func TestApplySecret(t *testing.T) {
	obj := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hec_key",
			Namespace: "test",
		},
	}
	f := func(c *mockClient) error { return ApplySecret(c, &obj) }
	applyTester(t, "ApplySecret()", "TestApplySecret", &obj, f)
}

func TestApplyService(t *testing.T) {
	obj := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "test",
		},
	}
	f := func(c *mockClient) error { return ApplyService(c, &obj) }
	applyTester(t, "ApplyService()", "TestApplyService", &obj, f)
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
