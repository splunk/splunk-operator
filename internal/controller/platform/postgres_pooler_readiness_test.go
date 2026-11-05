/*
Copyright 2026.

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
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	pgcnpg "github.com/splunk/splunk-operator/pkg/postgresql/shared/cnpg"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var _ = Describe("PgBouncer upgrade readiness", func() {
	It("waits for current Deployment status and complete availability", func(ctx SpecContext) {
		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "pooler-readiness-"}}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		DeferCleanup(func(cleanupCtx SpecContext) { Expect(k8sClient.Delete(cleanupCtx, namespace)).To(Succeed()) }, NodeTimeout(10*time.Second))
		pooler := &cnpgv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{Name: "green-pooler-rw", Namespace: namespace.Name},
			Spec: cnpgv1.PoolerSpec{Cluster: cnpgv1.LocalObjectReference{Name: "green"}, Type: cnpgv1.PoolerTypeRW,
				Instances: new(int32(2)), PgBouncer: &cnpgv1.PgBouncerSpec{PoolMode: cnpgv1.PgBouncerPoolModeSession}},
		}
		Expect(k8sClient.Create(ctx, pooler)).To(Succeed())
		pooler.Status.Phase = cnpgv1.PoolerPhaseActive
		pooler.Status.Instances = 2
		Expect(k8sClient.Status().Update(ctx, pooler)).To(Succeed())
		selector := map[string]string{"cnpg.io/poolerName": pooler.Name}
		owner := []metav1.OwnerReference{*metav1.NewControllerRef(pooler, cnpgv1.SchemeGroupVersion.WithKind("Pooler"))}
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: pooler.Name, Namespace: namespace.Name, OwnerReferences: owner},
			Spec:       corev1.ServiceSpec{Selector: selector, Ports: []corev1.ServicePort{{Name: "pgbouncer", Port: 5432, TargetPort: intstr.FromString("pgbouncer")}}},
		}
		Expect(k8sClient.Create(ctx, service)).To(Succeed())
		assertReadiness := func(want bool) {
			ready, err := pgcnpg.PoolerDeploymentReady(ctx, k8sClient, pooler, service)
			Expect(err).NotTo(HaveOccurred())
			Expect(ready).To(Equal(want))
		}
		By("rejecting CNPG active status without a Deployment")
		assertReadiness(false)
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: pooler.Name, Namespace: namespace.Name, OwnerReferences: owner},
			Spec: appsv1.DeploymentSpec{Replicas: new(int32(2)), Selector: &metav1.LabelSelector{MatchLabels: selector},
				Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: selector}, Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "pgbouncer", Image: "pgbouncer:test", Ports: []corev1.ContainerPort{{Name: "pgbouncer", ContainerPort: 5432}}}},
				}}},
		}
		Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
		assertReadiness(false)
		By("requiring all desired replicas, even when minimum availability is true")
		deployment.Status = appsv1.DeploymentStatus{ObservedGeneration: deployment.Generation, Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: 2, AvailableReplicas: 1,
			Conditions: []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue}}}
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())
		assertReadiness(false)
		deployment.Status.AvailableReplicas = 2
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())
		assertReadiness(true)
		By("rejecting previously healthy status after a new template generation")
		deployment.Spec.Template.Spec.Containers[0].Image = "pgbouncer:updated"
		Expect(k8sClient.Update(ctx, deployment)).To(Succeed())
		Expect(deployment.Generation).To(BeNumerically(">", deployment.Status.ObservedGeneration))
		assertReadiness(false)
		By("waiting for the latest generation to finish replacing old replicas")
		deployment.Status.ObservedGeneration = deployment.Generation
		deployment.Status.UpdatedReplicas = 1
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())
		assertReadiness(false)
		deployment.Status.UpdatedReplicas = 2
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())
		assertReadiness(true)
	}, NodeTimeout(45*time.Second))

	It("wires the database reader to the API even before the manager cache starts", func(ctx SpecContext) {
		manager, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: clientgoscheme.Scheme, Metrics: metricsserver.Options{BindAddress: "0"}})
		Expect(err).NotTo(HaveOccurred())
		reconciler := &PostgresDatabaseReconciler{Client: manager.GetClient(), Scheme: manager.GetScheme()}
		Expect(reconciler.SetupWithManager(manager)).To(Succeed())
		Expect(reconciler.APIReader).To(BeIdenticalTo(manager.GetAPIReader()))
		configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{GenerateName: "uncached-reader-", Namespace: "default"}}
		Expect(k8sClient.Create(ctx, configMap)).To(Succeed())
		DeferCleanup(func(cleanupCtx SpecContext) { Expect(k8sClient.Delete(cleanupCtx, configMap)).To(Succeed()) }, NodeTimeout(10*time.Second))
		observed := &corev1.ConfigMap{}
		Expect(reconciler.APIReader.Get(ctx, client.ObjectKeyFromObject(configMap), observed)).To(Succeed())
		Expect(observed.UID).To(Equal(configMap.UID))
	}, NodeTimeout(15*time.Second))
})
