/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

var _ = Describe("KVService Controller", func() {
	DescribeTable("records paused status with a valid phase",
		func(initialPhase enterprisev4.Phase) {
			ctx := context.Background()
			resource := &enterprisev4.KVService{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "paused-status-",
					Namespace:    "default",
					Annotations:  map[string]string{enterprisev4.KVServicePausedAnnotation: "true"},
				},
				Spec: enterprisev4.KVServiceSpec{
					CommonWorkloadSpec: enterprisev4.CommonWorkloadSpec{
						Spec: enterprisev4.Spec{ImagePullPolicy: "IfNotPresent"},
					},
					Replicas: 1,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			})
			Expect(resource.Status.Phase).To(BeEmpty())
			if initialPhase != "" {
				resource.Status.Phase = initialPhase
				Expect(k8sClient.Status().Update(ctx, resource)).To(Succeed())
			}
			key := types.NamespacedName{Name: resource.Name, Namespace: resource.Namespace}
			reconciler := &KVServiceReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(splcommon.PauseRetryDelay))

			current := &enterprisev4.KVService{}
			Expect(k8sClient.Get(ctx, key, current)).To(Succeed())
			expectedPhase := initialPhase
			if expectedPhase == "" {
				expectedPhase = enterprisev4.PhasePending
			}
			Expect(current.Status.Phase).To(Equal(expectedPhase))
			Expect(current.Status.ObservedGeneration).To(Equal(current.Generation))
			paused := meta.FindStatusCondition(current.Status.Conditions, string(enterprisev4.ConditionPaused))
			Expect(paused).NotTo(BeNil())
			Expect(paused.Status).To(Equal(metav1.ConditionTrue))
			Expect(paused.ObservedGeneration).To(Equal(current.Generation))
		},
		Entry("when created already paused", enterprisev4.Phase("")),
		Entry("without replacing an existing phase", enterprisev4.PhaseReady),
	)

	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		kvservice := &enterprisev4.KVService{}

		BeforeEach(func() {
			Expect(os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")).To(Succeed())
			Expect(k8sClient.Create(ctx, &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: "kvservice-test-sa", Namespace: "default"},
			})).To(Succeed())
			By("creating the custom resource for the Kind KVService")
			err := k8sClient.Get(ctx, typeNamespacedName, kvservice)
			if err != nil && errors.IsNotFound(err) {
				resource := &enterprisev4.KVService{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "enterprise.splunk.com/v4",
						Kind:       "KVService",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: enterprisev4.KVServiceSpec{
						CommonWorkloadSpec: enterprisev4.CommonWorkloadSpec{
							Spec: enterprisev4.Spec{
								ImagePullPolicy: "IfNotPresent",
							},
							ServiceAccount:              "kvservice-test-sa",
							LivenessInitialDelaySeconds: 90,
						},
						ReadinessProbe: &enterprisev4.Probe{TimeoutSeconds: 7},
						Replicas:       1,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &enterprisev4.KVService{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance KVService")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: "kvservice-test-sa", Namespace: "default"},
			})).To(Succeed())
			Expect(os.Unsetenv("SPLUNK_GENERAL_TERMS")).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			Expect(k8sClient.Get(ctx, typeNamespacedName, kvservice)).To(Succeed())
			Expect(kvservice.Spec.LivenessInitialDelaySeconds).To(Equal(int32(90)))
			Expect(kvservice.Spec.LivenessProbe).To(Equal(&enterprisev4.Probe{
				TimeoutSeconds: 1, PeriodSeconds: 5, FailureThreshold: 3,
			}))
			Expect(kvservice.Spec.ReadinessProbe.TimeoutSeconds).To(Equal(int32(7)))
			By("Reconciling the created resource")
			controllerReconciler := &KVServiceReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			deployment := &appsv1.Deployment{}
			deploymentKey := types.NamespacedName{Namespace: typeNamespacedName.Namespace,
				Name: splutil.GetSplunkDeploymentName(splcommon.SplunkKVService, resourceName)}
			Expect(k8sClient.Get(ctx, deploymentKey, deployment)).To(Succeed())
			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.LivenessProbe.InitialDelaySeconds).To(Equal(int32(90)))
			Expect(container.LivenessProbe.TimeoutSeconds).To(Equal(int32(1)))
			Expect(container.ReadinessProbe.InitialDelaySeconds).To(Equal(int32(30)))
			Expect(container.ReadinessProbe.TimeoutSeconds).To(Equal(int32(7)))
			Expect(container.ReadinessProbe.PeriodSeconds).To(Equal(int32(5)))
		})
	})
})
