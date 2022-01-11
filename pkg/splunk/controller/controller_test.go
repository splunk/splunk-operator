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

package controller

import (
	"context"
	"errors"
	"github.com/go-logr/logr"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache/informertest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"testing"
)

// MockControllerState manages state for testing the splunk.controller library
type MockControllerState struct {
	instance        splcommon.MetaObject
	watchTypes      []client.Object
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
func (ctrl MockController) GetWatchTypes() []client.Object {
	return ctrl.state.watchTypes
}

// Reconcile is used to perform an idempotent reconciliation of the custom resource managed by this controller
func (ctrl MockController) Reconcile(ctx context.Context, client client.Client, cr splcommon.MetaObject) (reconcile.Result, error) {
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
		watchTypes: []client.Object{&corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
		}},
	}

	return MockController{state: state}
}

// blank assignment to verify that MockFieldIndexer implements client.FieldIndexer
var _ client.FieldIndexer = &MockFieldIndexer{}

type MockFieldIndexer struct{}

// IndexField for MockFieldIndexer just returns nil
func (idx MockFieldIndexer) IndexField(ctx context.Context, obj client.Object, field string, extractValue client.IndexerFunc) error {
	return nil
}

// blank assignment to verify that MockManager implements manager.Manager
var _ manager.Manager = &MockManager{}

// MockManager is used to test methods that involve the Kubernetes manager
type MockManager struct{}

// Add for MockManager just returns nil
func (mgr MockManager) Add(r manager.Runnable) error {
	return mgr.SetFields(r)
}

// GetControllerOptions returns controller global configuration options.
func (mgr MockManager) GetControllerOptions() v1alpha1.ControllerConfigurationSpec {
	return v1alpha1.ControllerConfigurationSpec{}
}

func (mgr MockManager) AddMetricsExtraHandler(path string, handler http.Handler) error {
	return nil
}

func (mgr MockManager) Elected() <-chan struct{} {
	return nil
}

// SetFields for MockManager just returns nil
func (mgr MockManager) SetFields(i interface{}) error {
	if _, err := inject.InjectorInto(mgr.SetFields, i); err != nil {
		return err
	}
	return nil
}

// AddHealthzCheck for MockManager just returns nil
func (mgr MockManager) AddHealthzCheck(name string, check healthz.Checker) error {
	return nil
}

// AddReadyzCheck for MockManager just returns nil
func (mgr MockManager) AddReadyzCheck(name string, check healthz.Checker) error {
	return nil
}

// Start for MockManager just returns nil
func (mgr MockManager) Start(ctx context.Context) error {
	return nil
}

// GetConfig returns an initialized Config
func (mgr MockManager) GetConfig() *rest.Config {
	return &rest.Config{}
}

// GetLogger returns this manager's logger.
func (mgr MockManager) GetLogger() logr.Logger {
	return log
}

// GetScheme returns an initialized Scheme
func (mgr MockManager) GetScheme() *runtime.Scheme {
	return &runtime.Scheme{}
}

// GetClient returns a MockClient
func (mgr MockManager) GetClient() client.Client {
	return spltest.NewMockClient()
}

// GetFieldIndexer returns a client.FieldIndexer configured with the client
func (mgr MockManager) GetFieldIndexer() client.FieldIndexer {
	return MockFieldIndexer{}
}

// GetCache returns a cache.Cache
func (mgr MockManager) GetCache() cache.Cache {
	return &informertest.FakeInformers{}
}

// GetEventRecorderFor returns a new EventRecorder for the provided name
func (mgr MockManager) GetEventRecorderFor(name string) record.EventRecorder {
	broadcaster := record.NewBroadcaster()
	return broadcaster.NewRecorder(mgr.GetScheme(), corev1.EventSource{})
}

// GetRESTMapper returns a RESTMapper
func (mgr MockManager) GetRESTMapper() meta.RESTMapper {
	return &meta.DefaultRESTMapper{}
}

// GetAPIReader returns a reader that will be configured to use the API server.
func (mgr MockManager) GetAPIReader() client.Reader {
	return mgr.GetClient()
}

// GetWebhookServer returns a webhook.Server
func (mgr MockManager) GetWebhookServer() *webhook.Server {
	s := webhook.Server{}
	mgr.SetFields(&s)
	return &s
}

// NewMockManager returns a new instance of a MockManager
func NewMockManager() manager.Manager {
	return MockManager{}
}

func TestAddToManager(t *testing.T) {
	c := spltest.NewMockClient()
	ctrl := newMockController()
	mgr := NewMockManager()
	err := AddToManager(mgr, ctrl, c)
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

		result, err := reconciler.Reconcile(context.Background(), request)
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
	c.NotFoundError = apierrors.NewNotFound(schema.GroupResource{Group: "v1", Resource: "ConfigMap"}, "defaults")
	test("NotFound", 0, reconcile.Result{Requeue: false, RequeueAfter: 0}, nil)

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
