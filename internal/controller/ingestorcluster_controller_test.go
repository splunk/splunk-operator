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

var _ = Describe("IngestorCluster Controller", func() {
	BeforeEach(func() {
		time.Sleep(2 * time.Second)
	})

	AfterEach(func() {

	})

	Context("IngestorCluster Management", func() {

		It("Get IngestorCluster custom resource should fail", func() {
			namespace := "ns-splunk-ing-1"
			ApplyIngestorCluster = func(ctx context.Context, client client.Client, instance *enterpriseApi.IngestorCluster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}

			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			_, err := GetIngestorCluster("test", nsSpecs.Name)
			Expect(err.Error()).Should(Equal("ingestorclusters.enterprise.splunk.com \"test\" not found"))

			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Create IngestorCluster custom resource with annotations should pause", func() {
			namespace := "ns-splunk-ing-2"
			annotations := make(map[string]string)
			annotations[enterpriseApi.IngestorClusterPausedAnnotation] = ""
			ApplyIngestorCluster = func(ctx context.Context, client client.Client, instance *enterpriseApi.IngestorCluster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}

			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			CreateIngestorCluster("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady)
			icSpec, _ := GetIngestorCluster("test", nsSpecs.Name)
			annotations = map[string]string{}
			icSpec.Annotations = annotations
			icSpec.Status.Phase = "Ready"
			UpdateIngestorCluster(icSpec, enterpriseApi.PhaseReady)
			DeleteIngestorCluster("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Create IngestorCluster custom resource should succeeded", func() {
			namespace := "ns-splunk-ing-3"
			ApplyIngestorCluster = func(ctx context.Context, client client.Client, instance *enterpriseApi.IngestorCluster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}

			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			annotations := make(map[string]string)
			CreateIngestorCluster("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady)
			DeleteIngestorCluster("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Cover Unused methods", func() {
			namespace := "ns-splunk-ing-4"
			ApplyIngestorCluster = func(ctx context.Context, client client.Client, instance *enterpriseApi.IngestorCluster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}

			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			ctx := context.TODO()
			builder := fake.NewClientBuilder()
			c := builder.Build()
			instance := IngestorClusterReconciler{
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

			icSpec := testutils.NewIngestorCluster("test", namespace, "image")
			Expect(c.Create(ctx, icSpec)).Should(Succeed())

			annotations := make(map[string]string)
			annotations[enterpriseApi.IngestorClusterPausedAnnotation] = ""
			icSpec.Annotations = annotations
			Expect(c.Update(ctx, icSpec)).Should(Succeed())

			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())

			annotations = map[string]string{}
			icSpec.Annotations = annotations
			Expect(c.Update(ctx, icSpec)).Should(Succeed())

			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())

			icSpec.DeletionTimestamp = &metav1.Time{}
			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
		})

	})
})

func GetIngestorCluster(name string, namespace string) (*enterpriseApi.IngestorCluster, error) {
	By("Expecting IngestorCluster custom resource to be retrieved successfully")

	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	ic := &enterpriseApi.IngestorCluster{}

	err := k8sClient.Get(context.Background(), key, ic)
	if err != nil {
		return nil, err
	}

	return ic, err
}

func CreateIngestorCluster(name string, namespace string, annotations map[string]string, status enterpriseApi.Phase) *enterpriseApi.IngestorCluster {
	By("Expecting IngestorCluster custom resource to be created successfully")

	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	ingSpec := &enterpriseApi.IngestorCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: enterpriseApi.IngestorClusterSpec{},
	}

	ingSpec = testutils.NewIngestorCluster(name, namespace, "image")
	Expect(k8sClient.Create(context.Background(), ingSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)
	ic := &enterpriseApi.IngestorCluster{}
	Eventually(func() bool {
		_ = k8sClient.Get(context.Background(), key, ic)
		if status != "" {
			fmt.Printf("status is set to %v", status)
			ic.Status.Phase = status
			Expect(k8sClient.Status().Update(context.Background(), ic)).Should(Succeed())
			time.Sleep(2 * time.Second)
		}
		return true
	}, timeout, interval).Should(BeTrue())

	return ic
}

func UpdateIngestorCluster(instance *enterpriseApi.IngestorCluster, status enterpriseApi.Phase) *enterpriseApi.IngestorCluster {
	By("Expecting IngestorCluster custom resource to be updated successfully")

	key := types.NamespacedName{
		Name:      instance.Name,
		Namespace: instance.Namespace,
	}

	icSpec := testutils.NewIngestorCluster(instance.Name, instance.Namespace, "image")
	icSpec.ResourceVersion = instance.ResourceVersion
	Expect(k8sClient.Update(context.Background(), icSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	ic := &enterpriseApi.IngestorCluster{}
	Eventually(func() bool {
		_ = k8sClient.Get(context.Background(), key, ic)
		if status != "" {
			fmt.Printf("status is set to %v", status)
			ic.Status.Phase = status
			Expect(k8sClient.Status().Update(context.Background(), ic)).Should(Succeed())
			time.Sleep(2 * time.Second)
		}
		return true
	}, timeout, interval).Should(BeTrue())

	return ic
}

func DeleteIngestorCluster(name string, namespace string) {
	By("Expecting IngestorCluster custom resource to be deleted successfully")

	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	Eventually(func() error {
		ic := &enterpriseApi.IngestorCluster{}
		_ = k8sClient.Get(context.Background(), key, ic)
		err := k8sClient.Delete(context.Background(), ic)
		return err
	}, timeout, interval).Should(Succeed())
}
