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
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
)

var _ = Describe("PostgresCluster Controller", func() {

	var (
		ctx         context.Context
		namespace   string
		clusterName string
		className   string
		reconciler  *PostgresClusterReconciler
		req         reconcile.Request
		cnpg        *cnpgv1.Cluster
	)

	BeforeEach(func() {
		specLine := CurrentSpecReport().LeafNodeLocation.LineNumber
		nameSuffix := fmt.Sprintf("%d-%d-%d", GinkgoParallelProcess(), GinkgoRandomSeed(), specLine)

		ctx = context.Background()
		namespace = "default"
		clusterName = "postgresql-cluster-dev-" + nameSuffix
		className = "postgresql-dev-" + nameSuffix
		cnpg = &cnpgv1.Cluster{}

		// Arrange: class defaults used by getMergedConfig()
		postgresVersion := "15.10"
		instances := int32(2)
		storage := resource.MustParse("1Gi")
		poolerEnabled := false

		class := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: className},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Provisioner: "postgresql.cnpg.io",
				Config: &enterprisev4.PostgresClusterClassConfig{
					Instances:               &instances,
					Storage:                 &storage,
					PostgresVersion:         &postgresVersion,
					ConnectionPoolerEnabled: &poolerEnabled,
				},
			},
		}
		Expect(k8sClient.Create(ctx, class)).To(Succeed())

		pc := &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
			Spec: enterprisev4.PostgresClusterSpec{
				Class:                 className,
				ClusterDeletionPolicy: &[]string{"Delete"}[0],
			},
		}
		Expect(k8sClient.Create(ctx, pc)).To(Succeed())

		reconciler = &PostgresClusterReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
		req = reconcile.Request{NamespacedName: types.NamespacedName{Name: clusterName, Namespace: namespace}}
	})

	JustBeforeEach(func() {
		By("Reconciling the created resource")
		result, err := reconciler.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())
	})

	AfterEach(func() {
		By("Deleting PostgresCluster and letting reconcile run finalizer cleanup")
		key := types.NamespacedName{Name: clusterName, Namespace: namespace}
		pc := &enterprisev4.PostgresCluster{}

		// Best-effort delete (object might already be gone in some specs)
		err := k8sClient.Get(ctx, key, pc)
		if err == nil {
			Expect(k8sClient.Delete(ctx, pc)).To(Succeed())
		} else {
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}

		// Drive delete reconcile path until finalizer is removed and object disappears
		Eventually(func() bool {
			_, recErr := reconciler.Reconcile(ctx, req)
			if recErr != nil {
				// Some envtest runs may not have CNPG CRDs installed in the API server.
				// In that case, remove finalizer directly so fixture teardown remains deterministic.
				if meta.IsNoMatchError(recErr) {
					current := &enterprisev4.PostgresCluster{}
					getErr := k8sClient.Get(ctx, key, current)
					if apierrors.IsNotFound(getErr) {
						return true
					}
					if getErr != nil {
						return false
					}
					controllerutil.RemoveFinalizer(current, core.PostgresClusterFinalizerName)
					if err := k8sClient.Update(ctx, current); err != nil && !apierrors.IsNotFound(err) {
						return false
					}
					if err := k8sClient.Delete(ctx, current); err != nil && !apierrors.IsNotFound(err) {
						return false
					}
				} else {
					return false
				}
			}
			getErr := k8sClient.Get(ctx, key, &enterprisev4.PostgresCluster{})
			return apierrors.IsNotFound(getErr)
		}, "10s", "500ms").Should(BeTrue())

		By("Cleaning up PostgresClusterClass fixture")
		class := &enterprisev4.PostgresClusterClass{}
		classKey := types.NamespacedName{Name: className} // cluster-scoped CR
		err = k8sClient.Get(ctx, classKey, class)
		if err == nil {
			Expect(k8sClient.Delete(ctx, class)).To(Succeed())
		} else {
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}
	})

	Context("Happy path and convergence", func() {
		pc := &enterprisev4.PostgresCluster{}
		It("PC-01 creates managed resources and status refs", func() {
			By("creating CNPG cluster via reconcile and avaiting healthy")
			Eventually(func() error {
				_, err := reconciler.Reconcile(ctx, req)
				if err != nil {
					return err
				}
				if err := k8sClient.Get(ctx, req.NamespacedName, cnpg); err != nil {
					return err
				}
				cnpg.Status.Phase = cnpgv1.PhaseHealthy
				return k8sClient.Status().Update(ctx, cnpg)
			}, "10s", "250ms").Should(Succeed())

			By("reconciling until managed resources are published in status")
			Eventually(func() bool {
				_, err := reconciler.Reconcile(ctx, req)
				if err != nil {
					return false
				}
				current := &enterprisev4.PostgresCluster{}
				if err := k8sClient.Get(ctx, req.NamespacedName, current); err != nil {
					return false
				}
				return current.Status.Resources != nil &&
					current.Status.Resources.SuperUserSecretRef != nil &&
					current.Status.Resources.ConfigMapRef != nil
			}, "20s", "250ms").Should(BeTrue())

			By("asserting finalizer contract")
			pc := &enterprisev4.PostgresCluster{}
			Expect(k8sClient.Get(ctx, req.NamespacedName, pc)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(pc, core.PostgresClusterFinalizerName)).To(BeTrue())

			By("asserting status references are published")
			Expect(pc.Status.Resources).NotTo(BeNil())
			Expect(pc.Status.Resources.SuperUserSecretRef).NotTo(BeNil())
			Expect(pc.Status.Resources.ConfigMapRef).NotTo(BeNil())

			By("asserting Secret ownership and existence")
			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: pc.Status.Resources.SuperUserSecretRef.Name, Namespace: namespace,
			}, secret)).To(Succeed())
			Expect(metav1.IsControlledBy(secret, pc)).To(BeTrue())

			By("asserting CNPG Cluster projection and ownership")
			cnpg := &cnpgv1.Cluster{}
			Expect(k8sClient.Get(ctx, req.NamespacedName, cnpg)).To(Succeed())
			Expect(metav1.IsControlledBy(cnpg, pc)).To(BeTrue())
			Expect(cnpg.Spec.Instances).To(Equal(2))
			Expect(cnpg.Spec.ImageName).To(ContainSubstring("postgresql:15.10"))
			Expect(cnpg.Spec.StorageConfiguration.Size).To(Equal("1Gi"))

			By("asserting ConfigMap contract consumed by clients")
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: pc.Status.Resources.ConfigMapRef.Name, Namespace: namespace,
			}, cm)).To(Succeed())
			Expect(metav1.IsControlledBy(cm, pc)).To(BeTrue())
			Expect(cm.Data).To(HaveKeyWithValue("DEFAULT_CLUSTER_PORT", "5432"))
			Expect(cm.Data).To(HaveKey("SUPER_USER_SECRET_REF"))
			Expect(cm.Data).To(HaveKey("CLUSTER_RW_ENDPOINT"))
		})
		It("PC-02 adds finalizer on reconcile", func() {
			Expect(k8sClient.Get(ctx, req.NamespacedName, pc)).To(Succeed())
			Expect(pc.ObjectMeta.Finalizers).To(ContainElement(core.PostgresClusterFinalizerName))
		})
		It("PC-07 is idempotent across repeated reconciles", func() {})
	})

	Context("Deletion and finalizer", func() {
		It("PC-03 Delete policy removes children and finalizer", func() {})
		It("PC-04 Retain policy preserves children and removes ownerRefs", func() {})
	})

	Context("Failure and drift", func() {
		It("PC-05 fails when PostgresClusterClass is missing", func() {})
		It("PC-06 restores drifted managed spec", func() {})
	})

	Context("Predicates", func() {
		It("PC-08 triggers on generation/finalizer/deletion changes", func() {})
		It("PC-09 ignores no-op updates", func() {})
	})

	// Context("When reconciling a resource", func() {

	// 	It("should successfully reconcile the resource", func() {
	// 		By("Reconciling the created resource")
	// 		// controllerReconciler := &PostgresClusterReconciler{
	// 		// 	Client: k8sClient,
	// 		// 	Scheme: k8sClient.Scheme(),
	// 		// }

	// 		// _, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
	// 		// 	NamespacedName: typeNamespacedName,
	// 		// })
	// 		err := errors.New("test error")
	// 		Expect(err).NotTo(HaveOccurred())

	// 	})
	// })
})
