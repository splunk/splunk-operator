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
*/package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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
		Expect(result.RequeueAfter).To(Equal(time.Second * 60))
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
		Expect(result.RequeueAfter).To(Equal(time.Second * 60))
	})

	// Additional tests for error and success cases can be added here
})

/*
func TestTelemetryController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Telemetry Controller Suite")
}

*/
