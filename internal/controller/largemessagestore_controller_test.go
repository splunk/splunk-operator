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

var _ = Describe("LargeMessageStore Controller", func() {
	BeforeEach(func() {
		time.Sleep(2 * time.Second)
	})

	AfterEach(func() {

	})

	Context("LargeMessageStore Management", func() {

		It("Get LargeMessageStore custom resource should fail", func() {
			namespace := "ns-splunk-largemessagestore-1"
			ApplyLargeMessageStore = func(ctx context.Context, client client.Client, instance *enterpriseApi.LargeMessageStore) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}

			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			_, err := GetLargeMessageStore("test", nsSpecs.Name)
			Expect(err.Error()).Should(Equal("largemessagestores.enterprise.splunk.com \"test\" not found"))
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Create LargeMessageStore custom resource with annotations should pause", func() {
			namespace := "ns-splunk-largemessagestore-2"
			annotations := make(map[string]string)
			annotations[enterpriseApi.LargeMessageStorePausedAnnotation] = ""
			ApplyLargeMessageStore = func(ctx context.Context, client client.Client, instance *enterpriseApi.LargeMessageStore) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}

			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			spec := enterpriseApi.LargeMessageStoreSpec{
				Provider: "s3",
				S3: enterpriseApi.S3Spec{
					Endpoint: "https://s3.us-west-2.amazonaws.com",
					Path:     "s3://ingestion/smartbus-test",
				},
			}
			CreateLargeMessageStore("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady, spec)
			icSpec, _ := GetLargeMessageStore("test", nsSpecs.Name)
			annotations = map[string]string{}
			icSpec.Annotations = annotations
			icSpec.Status.Phase = "Ready"
			UpdateLargeMessageStore(icSpec, enterpriseApi.PhaseReady, spec)
			DeleteLargeMessageStore("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Create LargeMessageStore custom resource should succeeded", func() {
			namespace := "ns-splunk-largemessagestore-3"
			ApplyLargeMessageStore = func(ctx context.Context, client client.Client, instance *enterpriseApi.LargeMessageStore) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}

			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			annotations := make(map[string]string)
			spec := enterpriseApi.LargeMessageStoreSpec{
				Provider: "s3",
				S3: enterpriseApi.S3Spec{
					Endpoint: "https://s3.us-west-2.amazonaws.com",
					Path:     "s3://ingestion/smartbus-test",
				},
			}
			CreateLargeMessageStore("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady, spec)
			DeleteLargeMessageStore("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Cover Unused methods", func() {
			namespace := "ns-splunk-largemessagestore-4"
			ApplyLargeMessageStore = func(ctx context.Context, client client.Client, instance *enterpriseApi.LargeMessageStore) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}

			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			ctx := context.TODO()
			builder := fake.NewClientBuilder()
			c := builder.Build()
			instance := LargeMessageStoreReconciler{
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

			spec := enterpriseApi.LargeMessageStoreSpec{
				Provider: "s3",
				S3: enterpriseApi.S3Spec{
					Endpoint: "https://s3.us-west-2.amazonaws.com",
					Path:     "s3://ingestion/smartbus-test",
				},
			}
			lmsSpec := testutils.NewLargeMessageStore("test", namespace, spec)
			Expect(c.Create(ctx, lmsSpec)).Should(Succeed())

			annotations := make(map[string]string)
			annotations[enterpriseApi.LargeMessageStorePausedAnnotation] = ""
			lmsSpec.Annotations = annotations
			Expect(c.Update(ctx, lmsSpec)).Should(Succeed())

			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())

			annotations = map[string]string{}
			lmsSpec.Annotations = annotations
			Expect(c.Update(ctx, lmsSpec)).Should(Succeed())

			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())

			lmsSpec.DeletionTimestamp = &metav1.Time{}
			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
		})

	})
})

func GetLargeMessageStore(name string, namespace string) (*enterpriseApi.LargeMessageStore, error) {
	By("Expecting LargeMessageStore custom resource to be retrieved successfully")

	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	lms := &enterpriseApi.LargeMessageStore{}

	err := k8sClient.Get(context.Background(), key, lms)
	if err != nil {
		return nil, err
	}

	return lms, err
}

func CreateLargeMessageStore(name string, namespace string, annotations map[string]string, status enterpriseApi.Phase, spec enterpriseApi.LargeMessageStoreSpec) *enterpriseApi.LargeMessageStore {
	By("Expecting LargeMessageStore custom resource to be created successfully")

	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	lmsSpec := &enterpriseApi.LargeMessageStore{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: spec,
	}

	Expect(k8sClient.Create(context.Background(), lmsSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	lms := &enterpriseApi.LargeMessageStore{}
	Eventually(func() bool {
		_ = k8sClient.Get(context.Background(), key, lms)
		if status != "" {
			fmt.Printf("status is set to %v", status)
			lms.Status.Phase = status
			Expect(k8sClient.Status().Update(context.Background(), lms)).Should(Succeed())
			time.Sleep(2 * time.Second)
		}
		return true
	}, timeout, interval).Should(BeTrue())

	return lms
}

func UpdateLargeMessageStore(instance *enterpriseApi.LargeMessageStore, status enterpriseApi.Phase, spec enterpriseApi.LargeMessageStoreSpec) *enterpriseApi.LargeMessageStore {
	By("Expecting LargeMessageStore custom resource to be updated successfully")

	key := types.NamespacedName{
		Name:      instance.Name,
		Namespace: instance.Namespace,
	}

	lmsSpec := testutils.NewLargeMessageStore(instance.Name, instance.Namespace, spec)
	lmsSpec.ResourceVersion = instance.ResourceVersion
	Expect(k8sClient.Update(context.Background(), lmsSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	lms := &enterpriseApi.LargeMessageStore{}
	Eventually(func() bool {
		_ = k8sClient.Get(context.Background(), key, lms)
		if status != "" {
			fmt.Printf("status is set to %v", status)
			lms.Status.Phase = status
			Expect(k8sClient.Status().Update(context.Background(), lms)).Should(Succeed())
			time.Sleep(2 * time.Second)
		}
		return true
	}, timeout, interval).Should(BeTrue())

	return lms
}

func DeleteLargeMessageStore(name string, namespace string) {
	By("Expecting LargeMessageStore custom resource to be deleted successfully")

	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	Eventually(func() error {
		lms := &enterpriseApi.LargeMessageStore{}
		_ = k8sClient.Get(context.Background(), key, lms)
		err := k8sClient.Delete(context.Background(), lms)
		return err
	}, timeout, interval).Should(Succeed())
}
