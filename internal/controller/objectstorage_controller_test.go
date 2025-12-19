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

var _ = Describe("ObjectStorage Controller", func() {
	BeforeEach(func() {
		time.Sleep(2 * time.Second)
	})

	AfterEach(func() {

	})

	Context("ObjectStorage Management", func() {

		It("Get ObjectStorage custom resource should fail", func() {
			namespace := "ns-splunk-objectstorage-1"
			ApplyObjectStorage = func(ctx context.Context, client client.Client, instance *enterpriseApi.ObjectStorage) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}

			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			_, err := GetObjectStorage("test", nsSpecs.Name)
			Expect(err.Error()).Should(Equal("objectstorages.enterprise.splunk.com \"test\" not found"))
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Create ObjectStorage custom resource with annotations should pause", func() {
			namespace := "ns-splunk-objectstorage-2"
			annotations := make(map[string]string)
			annotations[enterpriseApi.ObjectStoragePausedAnnotation] = ""
			ApplyObjectStorage = func(ctx context.Context, client client.Client, instance *enterpriseApi.ObjectStorage) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}

			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			spec := enterpriseApi.ObjectStorageSpec{
				Provider: "s3",
				S3: enterpriseApi.S3Spec{
					Endpoint: "https://s3.us-west-2.amazonaws.com",
					Path:     "s3://ingestion/smartbus-test",
				},
			}
			CreateObjectStorage("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady, spec)
			osSpec, _ := GetObjectStorage("test", nsSpecs.Name)
			annotations = map[string]string{}
			osSpec.Annotations = annotations
			osSpec.Status.Phase = "Ready"
			UpdateObjectStorage(osSpec, enterpriseApi.PhaseReady, spec)
			DeleteObjectStorage("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Create ObjectStorage custom resource should succeeded", func() {
			namespace := "ns-splunk-objectstorage-3"
			ApplyObjectStorage = func(ctx context.Context, client client.Client, instance *enterpriseApi.ObjectStorage) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}

			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			annotations := make(map[string]string)
			spec := enterpriseApi.ObjectStorageSpec{
				Provider: "s3",
				S3: enterpriseApi.S3Spec{
					Endpoint: "https://s3.us-west-2.amazonaws.com",
					Path:     "s3://ingestion/smartbus-test",
				},
			}
			CreateObjectStorage("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady, spec)
			DeleteObjectStorage("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Cover Unused methods", func() {
			namespace := "ns-splunk-objectstorage-4"
			ApplyObjectStorage = func(ctx context.Context, client client.Client, instance *enterpriseApi.ObjectStorage) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}

			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			ctx := context.TODO()
			builder := fake.NewClientBuilder()
			c := builder.Build()
			instance := ObjectStorageReconciler{
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

			spec := enterpriseApi.ObjectStorageSpec{
				Provider: "s3",
				S3: enterpriseApi.S3Spec{
					Endpoint: "https://s3.us-west-2.amazonaws.com",
					Path:     "s3://ingestion/smartbus-test",
				},
			}
			osSpec := testutils.NewObjectStorage("test", namespace, spec)
			Expect(c.Create(ctx, osSpec)).Should(Succeed())

			annotations := make(map[string]string)
			annotations[enterpriseApi.ObjectStoragePausedAnnotation] = ""
			osSpec.Annotations = annotations
			Expect(c.Update(ctx, osSpec)).Should(Succeed())

			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())

			annotations = map[string]string{}
			osSpec.Annotations = annotations
			Expect(c.Update(ctx, osSpec)).Should(Succeed())

			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())

			osSpec.DeletionTimestamp = &metav1.Time{}
			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
		})

	})
})

func GetObjectStorage(name string, namespace string) (*enterpriseApi.ObjectStorage, error) {
	By("Expecting ObjectStorage custom resource to be retrieved successfully")

	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	os := &enterpriseApi.ObjectStorage{}

	err := k8sClient.Get(context.Background(), key, os)
	if err != nil {
		return nil, err
	}

	return os, err
}

func CreateObjectStorage(name string, namespace string, annotations map[string]string, status enterpriseApi.Phase, spec enterpriseApi.ObjectStorageSpec) *enterpriseApi.ObjectStorage {
	By("Expecting ObjectStorage custom resource to be created successfully")
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	osSpec := &enterpriseApi.ObjectStorage{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: spec,
	}

	Expect(k8sClient.Create(context.Background(), osSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	os := &enterpriseApi.ObjectStorage{}
	Eventually(func() bool {
		_ = k8sClient.Get(context.Background(), key, os)
		if status != "" {
			fmt.Printf("status is set to %v", status)
			os.Status.Phase = status
			Expect(k8sClient.Status().Update(context.Background(), os)).Should(Succeed())
			time.Sleep(2 * time.Second)
		}
		return true
	}, timeout, interval).Should(BeTrue())

	return os
}

func UpdateObjectStorage(instance *enterpriseApi.ObjectStorage, status enterpriseApi.Phase, spec enterpriseApi.ObjectStorageSpec) *enterpriseApi.ObjectStorage {
	By("Expecting ObjectStorage custom resource to be updated successfully")
	key := types.NamespacedName{
		Name:      instance.Name,
		Namespace: instance.Namespace,
	}

	osSpec := testutils.NewObjectStorage(instance.Name, instance.Namespace, spec)
	osSpec.ResourceVersion = instance.ResourceVersion
	Expect(k8sClient.Update(context.Background(), osSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	os := &enterpriseApi.ObjectStorage{}
	Eventually(func() bool {
		_ = k8sClient.Get(context.Background(), key, os)
		if status != "" {
			fmt.Printf("status is set to %v", status)
			os.Status.Phase = status
			Expect(k8sClient.Status().Update(context.Background(), os)).Should(Succeed())
			time.Sleep(2 * time.Second)
		}
		return true
	}, timeout, interval).Should(BeTrue())

	return os
}

func DeleteObjectStorage(name string, namespace string) {
	By("Expecting ObjectStorage custom resource to be deleted successfully")
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	Eventually(func() error {
		os := &enterpriseApi.ObjectStorage{}
		_ = k8sClient.Get(context.Background(), key, os)
		err := k8sClient.Delete(context.Background(), os)
		return err
	}, timeout, interval).Should(Succeed())
}
