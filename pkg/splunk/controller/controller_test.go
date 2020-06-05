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

package controller

import (
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

// MockControllerState manages state for testing the splunk.controller library
type MockControllerState struct {
	instance        splcommon.MetaObject
	watchTypes      []runtime.Object
	reconcileCalls  int
	reconcileError  error
	reconcileResult reconcile.Result
}

// blank assignment to verify that MockController implements SplunkController
var _ SplunkController = &MockController{}

// MockController is used to test the splunk.controller library
type MockController struct {
	state *MockControllerState
}

// GetInstance returns an instance of the custom resource managed by the controller
func (ctrl MockController) GetInstance() splcommon.MetaObject {
	return ctrl.state.instance
}

// GetWatchTypes returns a list of types owned by the controller that it would like to receive watch events for
func (ctrl MockController) GetWatchTypes() []runtime.Object {
	return ctrl.state.watchTypes
}

// Reconcile is used to perform an idempotent reconciliation of the custom resource managed by this controller
func (ctrl MockController) Reconcile(client client.Client, cr splcommon.MetaObject) (reconcile.Result, error) {
	ctrl.state.reconcileCalls++
	return ctrl.state.reconcileResult, ctrl.state.reconcileError
}

// GetCalls returns the number of times Reconcile() was called
func (ctrl MockController) GetCalls() int {
	return ctrl.state.reconcileCalls
}

// ResetCalls resets the number of times Reconcile() was called to zero
func (ctrl MockController) ResetCalls() {
	ctrl.state.reconcileCalls = 0
}

func newMockController() MockController {
	state := &MockControllerState{
		instance: &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
		},
		watchTypes: []runtime.Object{&corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
		}},
	}

	return MockController{state: state}
}

func TestAddToManager(t *testing.T) {
	cfg, err := config.GetConfig()
	if err != nil {
		t.Errorf("TestAddToManager: GetConfig() returned %v; want nil", err)
	}

	mgr, err := manager.New(cfg, manager.Options{Namespace: "test"})
	if err != nil {
		t.Errorf("TestAddToManager: manager.New() returned %v; want nil", err)
	}

	c := spltest.NewMockClient()
	ctrl := newMockController()
	err = AddToManager(mgr, ctrl, c)
	if err != nil {
		t.Errorf("TestAddToManager: AddToManager() returned %v; want nil", err)
	}
}

func TestReconcile(t *testing.T) {
	var request reconcile.Request
	request.Namespace = "test"
	request.Name = "defaults"

	funcCalls := []spltest.MockFuncCall{{MetaName: "*v1.ConfigMap-test-defaults"}}
	getCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls}

	c := spltest.NewMockClient()
	ctrl := newMockController()

	test := func(testname string, wantCalls int, wantResult reconcile.Result, wantError error) {
		c.ResetCalls()
		ctrl.ResetCalls()

		reconciler := splunkReconciler{
			client:  c,
			splctrl: ctrl,
		}

		result, err := reconciler.Reconcile(request)
		if wantError == nil && err != nil || wantError != nil && err == nil || (wantError != nil && err != nil && err.Error() != wantError.Error()) {
			t.Errorf("TestReconcile(%s): Returned %v; want %v", testname, err, wantError)
		}

		if result.Requeue != wantResult.Requeue {
			t.Errorf("TestReconcile(%s): result.Requeue=%t; want %t", testname, result.Requeue, wantResult.Requeue)
		}

		if result.RequeueAfter != wantResult.RequeueAfter {
			t.Errorf("TestReconcile(%s) result.RequeueAfter=%d; want %d", testname, result.RequeueAfter, wantResult.RequeueAfter)
		}

		if ctrl.GetCalls() != wantCalls {
			t.Errorf("TestReconcile(%s) reconcileCalls=%d; want %d", testname, ctrl.GetCalls(), wantCalls)
		}

		c.CheckCalls(t, "TestReconcile", getCalls)
	}

	// test for watch event when not found (deleted)
	test("NotFound", 0, reconcile.Result{Requeue: false, RequeueAfter: 0}, errors.New("NotFound"))

	obj := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	c.AddObject(&obj)

	// test for watch event that triggers Reconcile and is successful
	test("Success", 1, reconcile.Result{Requeue: false, RequeueAfter: 0}, nil)

	// test for watch event that triggers Reconcile, which returns error
	ctrl.state.reconcileError = errors.New("ABadThing")
	ctrl.state.reconcileResult = reconcile.Result{Requeue: true, RequeueAfter: 5}
	test("ReconcileError", 1, ctrl.state.reconcileResult, nil)

	// test for watch event that triggers Reconcile, which returns no error but wants requeue
	ctrl.state.reconcileError = nil
	ctrl.state.reconcileResult = reconcile.Result{Requeue: true, RequeueAfter: 10}
	test("ReconcileError", 1, ctrl.state.reconcileResult, nil)
}
