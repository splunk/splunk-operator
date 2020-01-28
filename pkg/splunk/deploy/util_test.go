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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// mockFuncCall is used to record a function call to mockClient methods
type mockFuncCall struct {
	ctx  context.Context
	key  client.ObjectKey
	opts *client.ListOptions
	obj  runtime.Object
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
	// err is the error code that will be returned by function calls
	err error

	// status is a StatusWriter mock client returned by Status()
	status mockStatusWriter

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
	return c.err
}

// List returns mock client's err field
func (c mockClient) List(ctx context.Context, opts *client.ListOptions, obj runtime.Object) error {
	c.calls["List"] = append(c.calls["List"], mockFuncCall{
		ctx:  ctx,
		opts: opts,
		obj:  obj,
	})
	return c.err
}

// Create returns mock client's err field
func (c mockClient) Create(ctx context.Context, obj runtime.Object) error {
	c.calls["Create"] = append(c.calls["Create"], mockFuncCall{
		ctx: ctx,
		obj: obj,
	})
	return c.err
}

// Delete returns mock client's err field
func (c mockClient) Delete(ctx context.Context, obj runtime.Object, opts ...client.DeleteOptionFunc) error {
	c.calls["Delete"] = append(c.calls["Delete"], mockFuncCall{
		ctx: ctx,
		obj: obj,
	})
	return c.err
}

// Update returns mock client's err field
func (c mockClient) Update(ctx context.Context, obj runtime.Object) error {
	c.calls["Update"] = append(c.calls["Update"], mockFuncCall{
		ctx: ctx,
		obj: obj,
	})
	return c.err
}

// Status returns the mock client's StatusWriter
func (c mockClient) Status() client.StatusWriter {
	return c.status
}

// newMockClient is used to create and initialize a new mock client
func newMockClient() *mockClient {
	c := &mockClient{
		calls: make(map[string][]mockFuncCall),
	}
	return c
}

// checkCalls verifies that the wanted function calls were performed
func (c *mockClient) checkCalls(t *testing.T, wantCalls map[string][]mockFuncCall) {
	for methodName, wantFuncCalls := range wantCalls {
		gotFuncCalls, ok := c.calls[methodName]
		if !ok || len(gotFuncCalls) != len(wantFuncCalls) {
			t.Errorf("MockClient %s() calls = %d; want %d", methodName, len(gotFuncCalls), len(wantCalls))
		}
		for n := range wantFuncCalls {
			if gotFuncCalls[n] != wantFuncCalls[n] {
				t.Errorf("MockClient %s() call #%d = %v; want %v", methodName, n, gotFuncCalls[n], wantFuncCalls[n])
			}
		}
	}

	if len(wantCalls) != len(c.calls) {
		t.Errorf("MockClient functions called = %d; want %d", len(c.calls), len(wantCalls))
	}
}

func TestCreateSecret(t *testing.T) {
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret",
			Namespace: "test",
		},
	}

	c := newMockClient()
	err := CreateResource(c, &secret)

	if err != nil {
		t.Errorf("CreateResource(%v) returned error: %v", secret, err)
	}

	c.checkCalls(t, map[string][]mockFuncCall{
		"Create": {
			{ctx: context.TODO(), obj: &secret},
		},
	})
}
