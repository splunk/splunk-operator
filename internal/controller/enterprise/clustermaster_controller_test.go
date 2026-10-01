// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/internal/controller/testutils"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/reconcile/clustermaster"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
)

var defaultClusterMasterApplyClusterMaster = clustermaster.ApplyClusterMaster

var _ = Describe("ClusterMaster Controller", Label("integration"), func() {

	AfterEach(func() {
		clustermaster.ApplyClusterMaster = defaultClusterMasterApplyClusterMaster
	})

	Context("ClusterMaster Management failed", func() {

		It("Get ClusterMaster custom resource should fail", func() {
			namespace := "ns-splunk-cmaster-1"
			clustermaster.ApplyClusterMaster = func(ctx context.Context, client splcommon.ControllerClient, instance *enterpriseApiV3.ClusterMaster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			// check when resource not found
			_, err := GetClusterMaster("test", nsSpecs.Name)
			Expect(err.Error()).Should(Equal("clustermasters.enterprise.splunk.com \"test\" not found"))
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})
	})

	Context("ClusterMaster Management with annotations", func() {

		It("Create ClusterMaster custom resource with annotations should pause", func() {
			namespace := "ns-splunk-cmaster-2"
			clustermaster.ApplyClusterMaster = func(ctx context.Context, client splcommon.ControllerClient, instance *enterpriseApiV3.ClusterMaster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			annotations := make(map[string]string)
			annotations[enterpriseApiV3.ClusterMasterPausedAnnotation] = "true"
			CreateClusterMaster("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady)
			ssSpec, _ := GetClusterMaster("test", nsSpecs.Name)
			annotations = map[string]string{}
			ssSpec.Annotations = annotations
			ssSpec.Status.Phase = "Ready"
			UpdateClusterMaster(ssSpec, enterpriseApi.PhaseReady)
			DeleteClusterMaster("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})
	})
	Context("ClusterMaster Management", func() {
		It("Create ClusterMaster custom resource should succeeded", func() {
			namespace := "ns-splunk-cmaster-3"
			clustermaster.ApplyClusterMaster = func(ctx context.Context, client splcommon.ControllerClient, instance *enterpriseApiV3.ClusterMaster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			annotations := make(map[string]string)
			CreateClusterMaster("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady)
			DeleteClusterMaster("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Cover Unused methods", func() {
			namespace := "ns-splunk-cmaster-4"
			clustermaster.ApplyClusterMaster = func(ctx context.Context, client splcommon.ControllerClient, instance *enterpriseApiV3.ClusterMaster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			ctx := context.TODO()
			builder := fake.NewClientBuilder()
			c := builder.Build()
			instance := ClusterMasterReconciler{
				Client: c,
				Scheme: scheme.Scheme,
			}
			request := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test",
					Namespace: namespace,
				},
			}
			// reconcile for the first time err is resource not found
			_, err := instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			// create resource first and then reconcile for the first time
			ssSpec := testutils.NewClusterMaster("test", namespace, "image")
			Expect(c.Create(ctx, ssSpec)).Should(Succeed())
			// reconcile with updated annotations for pause
			annotations := make(map[string]string)
			annotations[enterpriseApiV3.ClusterMasterPausedAnnotation] = "true"
			ssSpec.Annotations = annotations
			Expect(c.Update(ctx, ssSpec)).Should(Succeed())
			_, err = instance.Reconcile(ctx, request)
			// reconcile after removing annotations for pause
			annotations = map[string]string{}
			ssSpec.Annotations = annotations
			Expect(c.Update(ctx, ssSpec)).Should(Succeed())
			// reconcile after adding delete timestamp
			Expect(err).ToNot(HaveOccurred())
			ssSpec.DeletionTimestamp = &metav1.Time{}
			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
		})

	})
})

func GetClusterMaster(name string, namespace string) (*enterpriseApiV3.ClusterMaster, error) {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	By("Expecting ClusterMaster custom resource to be created successfully")
	ss := &enterpriseApiV3.ClusterMaster{}
	err := k8sClient.Get(context.Background(), key, ss)
	if err != nil {
		return nil, err
	}
	return ss, err
}

func CreateClusterMaster(name string, namespace string, annotations map[string]string, status enterpriseApi.Phase) *enterpriseApiV3.ClusterMaster {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	ssSpec := testutils.NewClusterMaster(name, namespace, "image")
	Expect(k8sClient.Create(context.Background(), ssSpec)).Should(Succeed())

	By("Expecting ClusterMaster custom resource to be created successfully")
	ss := &enterpriseApiV3.ClusterMaster{}
	Eventually(func() bool {
		return k8sClient.Get(context.Background(), key, ss) == nil
	}, timeout, interval).Should(BeTrue())
	if status != "" {
		ss.Status.Phase = status
		Expect(k8sClient.Status().Update(context.Background(), ss)).Should(Succeed())
	}

	return ss
}

func UpdateClusterMaster(instance *enterpriseApiV3.ClusterMaster, status enterpriseApi.Phase) *enterpriseApiV3.ClusterMaster {
	key := types.NamespacedName{
		Name:      instance.Name,
		Namespace: instance.Namespace,
	}

	Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &enterpriseApiV3.ClusterMaster{}
		if err := k8sClient.Get(context.Background(), key, current); err != nil {
			return err
		}
		ssSpec := testutils.NewClusterMaster(instance.Name, instance.Namespace, "image")
		ssSpec.ResourceVersion = current.ResourceVersion
		return k8sClient.Update(context.Background(), ssSpec)
	})).Should(Succeed())

	By("Expecting ClusterMaster custom resource to be updated successfully")
	ss := &enterpriseApiV3.ClusterMaster{}
	Eventually(func() bool {
		return k8sClient.Get(context.Background(), key, ss) == nil
	}, timeout, interval).Should(BeTrue())
	if status != "" {
		ss.Status.Phase = status
		Expect(k8sClient.Status().Update(context.Background(), ss)).Should(Succeed())
	}

	return ss
}

func DeleteClusterMaster(name string, namespace string) {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	By("Expecting ClusterMaster Deleted successfully")
	Eventually(func() error {
		ssys := &enterpriseApiV3.ClusterMaster{}
		_ = k8sClient.Get(context.Background(), key, ssys)
		err := k8sClient.Delete(context.Background(), ssys)
		return err
	}, timeout, interval).Should(Succeed())
}
