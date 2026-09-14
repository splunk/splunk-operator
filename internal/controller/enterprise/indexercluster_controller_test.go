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
	"fmt"

	"github.com/pkg/errors"
	"github.com/splunk/splunk-operator/internal/controller/testutils"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	indexercluster "github.com/splunk/splunk-operator/pkg/splunk/reconcile/indexercluster"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

	"time"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
)

var defaultIndexerClusterApply = indexercluster.Apply
var defaultIndexerClusterApplyManager = indexercluster.ApplyIndexerClusterManager
var defaultIndexerClusterApplyLegacy = indexercluster.ApplyIndexerCluster

var _ = Describe("IndexerCluster Controller", Label("integration"), func() {

	BeforeEach(func() {
		time.Sleep(2 * time.Second)
	})

	AfterEach(func() {
		indexercluster.Apply = defaultIndexerClusterApply
		indexercluster.ApplyIndexerClusterManager = defaultIndexerClusterApplyManager
		indexercluster.ApplyIndexerCluster = defaultIndexerClusterApplyLegacy
	})

	Context("IndexerCluster Management", func() {

		It("Get IndexerCluster custom resource should failed", func() {
			namespace := "ns-splunk-ic-1"
			indexercluster.Apply = func(ctx context.Context, client splcommon.ControllerClient, namespacedName types.NamespacedName, recorder record.EventRecorder) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			// check when resource not found
			_, err := GetIndexerCluster("test", nsSpecs.Name)
			Expect(err.Error()).Should(Equal("indexerclusters.enterprise.splunk.com \"test\" not found"))
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Create IndexerCluster custom resource with annotations should pause", func() {
			namespace := "ns-splunk-ic-2"
			indexercluster.Apply = func(ctx context.Context, client splcommon.ControllerClient, namespacedName types.NamespacedName, recorder record.EventRecorder) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			annotations := make(map[string]string)
			annotations[enterpriseApi.IndexerClusterPausedAnnotation] = "true"
			CreateIndexerCluster("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady)
			ssSpec, _ := GetIndexerCluster("test", nsSpecs.Name)
			annotations = map[string]string{}
			ssSpec.Annotations = annotations
			ssSpec.Status.Phase = "Ready"
			ssSpec.Status.ClusterManagerPhase, ssSpec.Status.ClusterMasterPhase = "Ready", "Ready" //CM* Phase can't be empty
			UpdateIndexerCluster(ssSpec, enterpriseApi.PhaseReady)
			DeleteIndexerCluster("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Create IndexerCluster custom resource should succeeded", func() {
			namespace := "ns-splunk-ic-3"
			indexercluster.Apply = func(ctx context.Context, client splcommon.ControllerClient, namespacedName types.NamespacedName, recorder record.EventRecorder) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			annotations := make(map[string]string)
			CreateIndexerCluster("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady)
			DeleteIndexerCluster("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Cover Unused methods", func() {
			namespace := "ns-splunk-ic-4"
			indexercluster.ApplyIndexerCluster = func(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())
			ctx := context.TODO()
			builder := fake.NewClientBuilder().WithStatusSubresource(&enterpriseApi.IndexerCluster{})
			c := builder.Build()
			instance := IndexerClusterReconciler{
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
			ssSpec := testutils.NewIndexerCluster("test", namespace, "image")
			Expect(c.Create(ctx, ssSpec)).Should(Succeed())
			// reconcile with updated annotations for pause
			annotations := make(map[string]string)
			annotations[enterpriseApi.IndexerClusterPausedAnnotation] = "true"
			ssSpec.Annotations = annotations
			Expect(c.Update(ctx, ssSpec)).Should(Succeed())
			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			// verify Paused=True condition was written
			Expect(c.Get(ctx, request.NamespacedName, ssSpec)).Should(Succeed())
			pausedCond := meta.FindStatusCondition(ssSpec.Status.Conditions, string(enterpriseApi.ConditionPaused))
			Expect(pausedCond).ToNot(BeNil())
			Expect(pausedCond.Status).To(Equal(metav1.ConditionTrue))
			// reconcile after removing annotations for pause
			annotations = map[string]string{}
			ssSpec.Annotations = annotations
			Expect(c.Update(ctx, ssSpec)).Should(Succeed())
			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			// verify Paused=False condition was written
			Expect(c.Get(ctx, request.NamespacedName, ssSpec)).Should(Succeed())
			pausedCond = meta.FindStatusCondition(ssSpec.Status.Conditions, string(enterpriseApi.ConditionPaused))
			Expect(pausedCond).ToNot(BeNil())
			Expect(pausedCond.Status).To(Equal(metav1.ConditionFalse))
			ssSpec.DeletionTimestamp = &metav1.Time{}
			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Reconcile emits Stalled Warning on every terminal failure reconcile", func() {
			namespace := "ns-splunk-ic-stalled"
			ctx := context.TODO()
			builder := fake.NewClientBuilder().WithStatusSubresource(&enterpriseApi.IndexerCluster{})
			c := builder.Build()
			recorder := record.NewFakeRecorder(10)
			reconciler := IndexerClusterReconciler{
				Client:   c,
				Scheme:   scheme.Scheme,
				Recorder: recorder,
			}
			ssSpec := testutils.NewIndexerCluster("test", namespace, "image")
			Expect(c.Create(ctx, ssSpec)).Should(Succeed())

			indexercluster.ApplyIndexerCluster = func(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) (reconcile.Result, error) {
				return reconcile.Result{}, splcommon.NewTerminalError("ValidateSpecFailed", "test terminal failure", fmt.Errorf("missing ClusterManagerRef"))
			}

			request := reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test", Namespace: namespace},
			}

			// First reconcile: Stalled=False → Stalled=True — Stalled event expected
			_, err := reconciler.Reconcile(ctx, request)
			Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeTrue())
			Eventually(recorder.Events).Should(Receive(MatchRegexp(`^Warning ` + splcommon.EventReasonStalled + ` `)))

			// Second reconcile: Stalled=True → Stalled=True — Warning fires on every stalled reconcile
			_, err = reconciler.Reconcile(ctx, request)
			Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeTrue())
			Eventually(recorder.Events).Should(Receive(MatchRegexp(`^Warning ` + splcommon.EventReasonStalled + ` `)))
		})

	})
})

func GetIndexerCluster(name string, namespace string) (*enterpriseApi.IndexerCluster, error) {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	By("Expecting IndexerCluster custom resource to be created successfully")
	ss := &enterpriseApi.IndexerCluster{}
	err := k8sClient.Get(context.Background(), key, ss)
	if err != nil {
		return nil, err
	}
	return ss, err
}

func CreateIndexerCluster(name string, namespace string, annotations map[string]string, status enterpriseApi.Phase) *enterpriseApi.IndexerCluster {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	ssSpec := testutils.NewIndexerCluster(name, namespace, "image")
	Expect(k8sClient.Create(context.Background(), ssSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	By("Expecting IndexerCluster custom resource to be created successfully")
	ss := &enterpriseApi.IndexerCluster{}
	Eventually(func() bool {
		_ = k8sClient.Get(context.Background(), key, ss)
		if status != "" {
			fmt.Printf("status is set to %v", status)
			ss.Status.Phase = status
			ss.Status.ClusterManagerPhase, ss.Status.ClusterMasterPhase = status, status //CM* Phase can't be empty
			Expect(k8sClient.Status().Update(context.Background(), ss)).Should(Succeed())
			time.Sleep(2 * time.Second)
		}
		return true
	}, timeout, interval).Should(BeTrue())

	return ss
}

func UpdateIndexerCluster(instance *enterpriseApi.IndexerCluster, status enterpriseApi.Phase) *enterpriseApi.IndexerCluster {
	key := types.NamespacedName{
		Name:      instance.Name,
		Namespace: instance.Namespace,
	}

	Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &enterpriseApi.IndexerCluster{}
		if err := k8sClient.Get(context.Background(), key, current); err != nil {
			return err
		}
		ssSpec := testutils.NewIndexerCluster(instance.Name, instance.Namespace, "image")
		ssSpec.ResourceVersion = current.ResourceVersion
		return k8sClient.Update(context.Background(), ssSpec)
	})).Should(Succeed())
	time.Sleep(2 * time.Second)

	By("Expecting IndexerCluster custom resource to be created successfully")
	ss := &enterpriseApi.IndexerCluster{}
	Eventually(func() bool {
		_ = k8sClient.Get(context.Background(), key, ss)
		if status != "" {
			fmt.Printf("status is set to %v", status)
			ss.Status.Phase = status
			ss.Status.ClusterManagerPhase, ss.Status.ClusterMasterPhase = status, status //CM* Phase can't be empty
			Expect(k8sClient.Status().Update(context.Background(), ss)).Should(Succeed())
			time.Sleep(2 * time.Second)
		}
		return true
	}, timeout, interval).Should(BeTrue())

	return ss
}

func DeleteIndexerCluster(name string, namespace string) {
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	By("Expecting IndexerCluster Deleted successfully")
	Eventually(func() error {
		ssys := &enterpriseApi.IndexerCluster{}
		_ = k8sClient.Get(context.Background(), key, ssys)
		err := k8sClient.Delete(context.Background(), ssys)
		return err
	}, timeout, interval).Should(Succeed())
}
