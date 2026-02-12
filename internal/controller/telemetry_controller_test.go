package controller

/*
Copyright (c) 2026 Splunk Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

import (
	"context"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

var _ = Describe("Telemetry Controller", func() {
	var (
		ctx    context.Context
		cmName = "splunk-operator-telemetry"
		ns     = "test-telemetry-ns"
		labels = map[string]string{"name": "splunk-operator"}
	)

	BeforeEach(func() {
		ctx = context.TODO()
	})

	It("Reconcile returns requeue when ConfigMap not found", func() {
		builder := fake.NewClientBuilder().WithScheme(scheme.Scheme)
		c := builder.Build()
		r := &TelemetryReconciler{Client: c, Scheme: scheme.Scheme}
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: cmName, Namespace: ns}}
		result, err := r.Reconcile(ctx, req)
		Expect(err).To(BeNil())
		Expect(result.Requeue).To(BeTrue())
		Expect(result.RequeueAfter).To(Equal(time.Second * 600))
	})

	It("Reconcile returns requeue when ConfigMap has no data", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: ns, Labels: labels},
			Data:       map[string]string{},
		}
		builder := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(cm)
		c := builder.Build()
		r := &TelemetryReconciler{Client: c, Scheme: scheme.Scheme}
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: cmName, Namespace: ns}}
		result, err := r.Reconcile(ctx, req)
		Expect(err).To(BeNil())
		Expect(result.Requeue).To(BeTrue())
		Expect(result.RequeueAfter).To(Equal(time.Second * 600))
	})

	It("Reconcile returns requeue when ConfigMap has data and ApplyTelemetry returns error", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: ns, Labels: labels},
			Data:       map[string]string{"foo": "bar"},
		}
		builder := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(cm)
		c := builder.Build()
		r := &TelemetryReconciler{Client: c, Scheme: scheme.Scheme}
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: cmName, Namespace: ns}}

		// Patch applyTelemetryFn to return error
		origApply := applyTelemetryFn
		defer func() { applyTelemetryFn = origApply }()
		applyTelemetryFn = func(ctx context.Context, client splcommon.ControllerClient, cm *corev1.ConfigMap) (reconcile.Result, error) {
			return reconcile.Result{}, fmt.Errorf("fake error")
		}

		result, err := r.Reconcile(ctx, req)
		Expect(err).To(BeNil())
		Expect(result.Requeue).To(BeTrue())
		Expect(result.RequeueAfter).To(Equal(time.Second * 600))
	})

	It("Reconcile returns result from ApplyTelemetry when ConfigMap has data and ApplyTelemetry returns requeue", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: ns, Labels: labels},
			Data:       map[string]string{"foo": "bar"},
		}
		builder := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(cm)
		c := builder.Build()
		r := &TelemetryReconciler{Client: c, Scheme: scheme.Scheme}
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: cmName, Namespace: ns}}

		// Patch applyTelemetryFn to return a requeue result
		origApply := applyTelemetryFn
		defer func() { applyTelemetryFn = origApply }()
		applyTelemetryFn = func(ctx context.Context, client splcommon.ControllerClient, cm *corev1.ConfigMap) (reconcile.Result, error) {
			return reconcile.Result{Requeue: true, RequeueAfter: time.Second * 600}, nil
		}

		result, err := r.Reconcile(ctx, req)
		Expect(err).To(BeNil())
		Expect(result.Requeue).To(BeTrue())
		Expect(result.RequeueAfter).To(Equal(time.Second * 600))
	})

	It("Reconcile returns success when ApplyTelemetry returns no requeue", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: ns, Labels: labels},
			Data:       map[string]string{"foo": "bar"},
		}
		builder := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(cm)
		c := builder.Build()
		r := &TelemetryReconciler{Client: c, Scheme: scheme.Scheme}
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: cmName, Namespace: ns}}

		origApply := applyTelemetryFn
		defer func() { applyTelemetryFn = origApply }()
		applyTelemetryFn = func(ctx context.Context, client splcommon.ControllerClient, cm *corev1.ConfigMap) (reconcile.Result, error) {
			return reconcile.Result{Requeue: false, RequeueAfter: 0}, nil
		}

		result, err := r.Reconcile(ctx, req)
		Expect(err).To(BeNil())
		Expect(result.Requeue).To(BeFalse())
		Expect(result.RequeueAfter).To(Equal(time.Duration(0)))
	})

	It("Reconcile returns requeue when r.Get returns error (not NotFound)", func() {
		r := &TelemetryReconciler{Client: &errorClient{}, Scheme: scheme.Scheme}
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: cmName, Namespace: ns}}
		result, err := r.Reconcile(ctx, req)
		Expect(err).To(BeNil())
		Expect(result.Requeue).To(BeTrue())
		Expect(result.RequeueAfter).To(Equal(time.Second * 600))
	})

	It("Reconcile recovers from panic in ApplyTelemetry", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: ns, Labels: labels},
			Data:       map[string]string{"foo": "bar"},
		}
		builder := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(cm)
		c := builder.Build()
		r := &TelemetryReconciler{Client: c, Scheme: scheme.Scheme}
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: cmName, Namespace: ns}}

		// Patch applyTelemetryFn to panic
		origApply := applyTelemetryFn
		defer func() { applyTelemetryFn = origApply }()
		applyTelemetryFn = func(ctx context.Context, client splcommon.ControllerClient, cm *corev1.ConfigMap) (reconcile.Result, error) {
			panic("test panic")
		}

		// Should not panic, should recover and return requeue
		Expect(func() {
			_, err := r.Reconcile(ctx, req)
			Expect(err).To(BeNil())
		}).NotTo(Panic())
	})

	It("Reconcile recovers from panic in r.Get", func() {
		r := &TelemetryReconciler{Client: &panicClient{}, Scheme: scheme.Scheme}
		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: cmName, Namespace: ns}}
		Expect(func() {
			_, err := r.Reconcile(ctx, req)
			Expect(err).To(BeNil())
		}).NotTo(Panic())
	})
})

type errorClient struct {
	client.Client
}

func (e *errorClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return fmt.Errorf("some error")
}

func TestTelemetryController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Telemetry Controller Suite")
}

type panicClient struct {
	client.Client
}

func (p *panicClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	panic("test panic in Get")
}
