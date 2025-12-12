/*
Copyright 2025.

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

package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/internal/controller/testutils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("Bus Controller", func() {
	BeforeEach(func() {
		time.Sleep(2 * time.Second)
	})

	AfterEach(func() {

	})

	Context("Bus Management", func() {

		It("Get Bus custom resource should fail", func() {
			namespace := "ns-splunk-bus-1"
			ApplyBus = func(ctx context.Context, client client.Client, instance *enterpriseApi.Bus) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}

			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			_, err := GetBus("test", nsSpecs.Name)
			Expect(err.Error()).Should(Equal("buses.enterprise.splunk.com \"test\" not found"))
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Create Bus custom resource with annotations should pause", func() {
			namespace := "ns-splunk-bus-2"
			annotations := make(map[string]string)
			annotations[enterpriseApi.BusPausedAnnotation] = ""
			ApplyBus = func(ctx context.Context, client client.Client, instance *enterpriseApi.Bus) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}

			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			spec := enterpriseApi.BusSpec{
				Provider: "sqs",
				SQS: enterpriseApi.SQSSpec{
					Name:     "smartbus-queue",
					Region:   "us-west-2",
					DLQ:      "smartbus-dlq",
					Endpoint: "https://sqs.us-west-2.amazonaws.com",
				},
			}
			CreateBus("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady, spec)
			icSpec, _ := GetBus("test", nsSpecs.Name)
			annotations = map[string]string{}
			icSpec.Annotations = annotations
			icSpec.Status.Phase = "Ready"
			UpdateBus(icSpec, enterpriseApi.PhaseReady, spec)
			DeleteBus("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Create Bus custom resource should succeeded", func() {
			namespace := "ns-splunk-bus-3"
			ApplyBus = func(ctx context.Context, client client.Client, instance *enterpriseApi.Bus) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}

			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			annotations := make(map[string]string)
			spec := enterpriseApi.BusSpec{
				Provider: "sqs",
				SQS: enterpriseApi.SQSSpec{
					Name:     "smartbus-queue",
					Region:   "us-west-2",
					DLQ:      "smartbus-dlq",
					Endpoint: "https://sqs.us-west-2.amazonaws.com",
				},
			}
			CreateBus("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady, spec)
			DeleteBus("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Cover Unused methods", func() {
			namespace := "ns-splunk-bus-4"
			ApplyBus = func(ctx context.Context, client client.Client, instance *enterpriseApi.Bus) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}

			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			ctx := context.TODO()
			builder := fake.NewClientBuilder()
			c := builder.Build()
			instance := BusReconciler{
				Client: c,
				Scheme: scheme.Scheme,
			}
			request := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test",
					Namespace: namespace,
				},
			}
			_, err := instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())

			spec := enterpriseApi.BusSpec{
				Provider: "sqs",
				SQS: enterpriseApi.SQSSpec{
					Name:     "smartbus-queue",
					Region:   "us-west-2",
					DLQ:      "smartbus-dlq",
					Endpoint: "https://sqs.us-west-2.amazonaws.com",
				},
			}
			bcSpec := testutils.NewBus("test", namespace, spec)
			Expect(c.Create(ctx, bcSpec)).Should(Succeed())

			annotations := make(map[string]string)
			annotations[enterpriseApi.BusPausedAnnotation] = ""
			bcSpec.Annotations = annotations
			Expect(c.Update(ctx, bcSpec)).Should(Succeed())

			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())

			annotations = map[string]string{}
			bcSpec.Annotations = annotations
			Expect(c.Update(ctx, bcSpec)).Should(Succeed())

			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())

			bcSpec.DeletionTimestamp = &metav1.Time{}
			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
		})

	})
})

func GetBus(name string, namespace string) (*enterpriseApi.Bus, error) {
	By("Expecting Bus custom resource to be retrieved successfully")

	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	b := &enterpriseApi.Bus{}

	err := k8sClient.Get(context.Background(), key, b)
	if err != nil {
		return nil, err
	}

	return b, err
}

func CreateBus(name string, namespace string, annotations map[string]string, status enterpriseApi.Phase, spec enterpriseApi.BusSpec) *enterpriseApi.Bus {
	By("Expecting Bus custom resource to be created successfully")

	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	ingSpec := &enterpriseApi.Bus{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: spec,
	}

	Expect(k8sClient.Create(context.Background(), ingSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	b := &enterpriseApi.Bus{}
	Eventually(func() bool {
		_ = k8sClient.Get(context.Background(), key, b)
		if status != "" {
			fmt.Printf("status is set to %v", status)
			b.Status.Phase = status
			Expect(k8sClient.Status().Update(context.Background(), b)).Should(Succeed())
			time.Sleep(2 * time.Second)
		}
		return true
	}, timeout, interval).Should(BeTrue())

	return b
}

func UpdateBus(instance *enterpriseApi.Bus, status enterpriseApi.Phase, spec enterpriseApi.BusSpec) *enterpriseApi.Bus {
	By("Expecting Bus custom resource to be updated successfully")

	key := types.NamespacedName{
		Name:      instance.Name,
		Namespace: instance.Namespace,
	}

	bSpec := testutils.NewBus(instance.Name, instance.Namespace, spec)
	bSpec.ResourceVersion = instance.ResourceVersion
	Expect(k8sClient.Update(context.Background(), bSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	b := &enterpriseApi.Bus{}
	Eventually(func() bool {
		_ = k8sClient.Get(context.Background(), key, b)
		if status != "" {
			fmt.Printf("status is set to %v", status)
			b.Status.Phase = status
			Expect(k8sClient.Status().Update(context.Background(), b)).Should(Succeed())
			time.Sleep(2 * time.Second)
		}
		return true
	}, timeout, interval).Should(BeTrue())

	return b
}

func DeleteBus(name string, namespace string) {
	By("Expecting Bus custom resource to be deleted successfully")

	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	Eventually(func() error {
		b := &enterpriseApi.Bus{}
		_ = k8sClient.Get(context.Background(), key, b)
		err := k8sClient.Delete(context.Background(), b)
		return err
	}, timeout, interval).Should(Succeed())
}
