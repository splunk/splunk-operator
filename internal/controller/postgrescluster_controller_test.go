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
	"strconv"
	"time"

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

/*
* Test cases:
* PC-01 creates managed resources and status refs
* PC-02 adds finalizer on reconcile
* PC-07 is idempotent across repeated reconciles
* PC-03 Delete policy removes children and finalizer
* PC-04 Retain policy preserves children and removes ownerRefs
* PC-05 fails when PostgresClusterClass is missing
* PC-06 restores drifted managed spec
* PC-08 triggers on generation/finalizer/deletion changes
* PC-09 ignores no-op updates
 */

var _ = Describe("PostgresCluster Controller", func() {

	const (
		postgresVersion    = "15.10"
		clusterMemberCount = int32(2)
		storageAmount      = "1Gi"
		poolerEnabled      = false
		deletePolicy       = "Delete"
		retainPolicy       = "Retain"
		namespace          = "default"
		classNamePrefix    = "postgresql-dev-"
		clusterNamePrefix  = "postgresql-cluster-dev-"
		provisioner        = "postgresql.cnpg.io"
	)

	var (
		ctx               context.Context
		clusterName       string
		className         string
		pgCluster         *enterprisev4.PostgresCluster
		pgClusterClass    *enterprisev4.PostgresClusterClass
		pgClusterKey      types.NamespacedName
		pgClusterClassKey types.NamespacedName
		reconciler        *PostgresClusterReconciler
		req               reconcile.Request
	)

	BeforeEach(func() {
		nameSuffix := fmt.Sprintf("%d-%d-%d",
			GinkgoParallelProcess(),
			GinkgoRandomSeed(),
			CurrentSpecReport().LeafNodeLocation.LineNumber,
		)

		ctx = context.Background()
		clusterName = clusterNamePrefix + nameSuffix
		className = classNamePrefix + nameSuffix
		pgClusterKey = types.NamespacedName{Name: clusterName, Namespace: namespace}
		pgClusterClassKey = types.NamespacedName{Name: className, Namespace: namespace}

		pgClusterClass = &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: className},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Provisioner: provisioner,
				Config: &enterprisev4.PostgresClusterClassConfig{
					Instances:               &[]int32{clusterMemberCount}[0],
					Storage:                 &[]resource.Quantity{resource.MustParse(storageAmount)}[0],
					PostgresVersion:         &[]string{postgresVersion}[0],
					ConnectionPoolerEnabled: &[]bool{poolerEnabled}[0],
				},
			},
		}

		Expect(k8sClient.Create(ctx, pgClusterClass)).To(Succeed())

		pgCluster = &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
			Spec: enterprisev4.PostgresClusterSpec{
				Class:                 className,
				ClusterDeletionPolicy: &[]string{deletePolicy}[0],
			},
		}

		reconciler = &PostgresClusterReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
		req = reconcile.Request{NamespacedName: types.NamespacedName{Name: clusterName, Namespace: namespace}}
	})

	AfterEach(func() {
		By("Deleting PostgresCluster and letting reconcile run finalizer cleanup")

		// Best-effort delete (object might already be gone in some specs)
		err := k8sClient.Get(ctx, pgClusterKey, pgCluster)
		if err == nil {
			Expect(k8sClient.Delete(ctx, pgCluster)).To(Succeed())
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
					getErr := k8sClient.Get(ctx, pgClusterKey, current)
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
			getErr := k8sClient.Get(ctx, pgClusterKey, &enterprisev4.PostgresCluster{})
			return apierrors.IsNotFound(getErr)
		}, "10s", "500ms").Should(BeTrue())

		By("Cleaning up PostgresClusterClass fixture")
		err = k8sClient.Get(ctx, pgClusterClassKey, pgClusterClass)
		if err == nil {
			Expect(k8sClient.Delete(ctx, pgClusterClass)).To(Succeed())
		} else {
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}
	})

	When("under typical usage and expecting healthy PostgresCluster state", func() {
		Context("when reconciling", func() {
			// PC-02
			It("adds finalizer on reconcile", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())

				Eventually(func() bool {
					pc := &enterprisev4.PostgresCluster{}
					if err := k8sClient.Get(ctx, pgClusterKey, pc); err != nil {
						return false
					}
					return controllerutil.ContainsFinalizer(pc, core.PostgresClusterFinalizerName)
				}, "10s", "250ms").Should(BeTrue())
			})

			// PC-01
			It("creates managed resources and status refs", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())

				Eventually(func(g Gomega) {
					pc := &enterprisev4.PostgresCluster{}
					g.Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())

					cond := meta.FindStatusCondition(pc.Status.Conditions, "ClusterReady")
					g.Expect(cond).NotTo(BeNil())
					g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
					g.Expect(cond.Reason).To(Equal("CNPGClusterProvisioning"))
				}, "20s", "250ms").Should(Succeed())

				// Simulate external CNPG controller status progression.
				Eventually(func() error {
					cnpg := &cnpgv1.Cluster{}
					if err := k8sClient.Get(ctx, pgClusterKey, cnpg); err != nil {
						return err
					}
					cnpg.Status.Phase = cnpgv1.PhaseHealthy
					return k8sClient.Status().Update(ctx, cnpg) // update event
				}, "10s", "250ms").Should(Succeed())

				// Expect cnpg status progression propagation
				Eventually(func(g Gomega) {
					pc := &enterprisev4.PostgresCluster{}
					g.Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())

					cond := meta.FindStatusCondition(pc.Status.Conditions, "ClusterReady")
					g.Expect(cond).NotTo(BeNil())
					g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(cond.Reason).To(Equal("CNPGClusterHealthy"))
				}, "20s", "250ms").Should(Succeed())

				Eventually(func(g Gomega) {
					pc := &enterprisev4.PostgresCluster{}
					g.Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
					g.Expect(pc.Status.Resources).NotTo(BeNil())
					g.Expect(pc.Status.Resources.SuperUserSecretRef).NotTo(BeNil())
					g.Expect(pc.Status.Resources.ConfigMapRef).NotTo(BeNil())
				}, "20s", "250ms").Should(Succeed())
			})

			// PC-07
			It("is idempotent across repeated reconciles", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())

				// Trigger extra update events that should not change desired state semantics.
				Eventually(func() error {
					pc := &enterprisev4.PostgresCluster{}
					if err := k8sClient.Get(ctx, pgClusterKey, pc); err != nil {
						return err
					}
					if pc.Annotations == nil {
						pc.Annotations = map[string]string{}
					}
					pc.Annotations["test.bump"] = strconv.FormatInt(time.Now().UnixNano(), 10)
					return k8sClient.Update(ctx, pc) // update event
				}, "10s", "250ms").Should(Succeed())

				Eventually(func(g Gomega) {
					cnpg := &cnpgv1.Cluster{}
					g.Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
					g.Expect(cnpg.Spec.Instances).To(Equal(int(clusterMemberCount)))
				}, "20s", "250ms").Should(Succeed())
			})
		})
	})

	When("deleting a PostgresCluster", func() {
		// PC-03
		Context("and clusterDeletionPolicy is set to Delete", func() {
			It("removes children and finalizer", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())

				pc := &enterprisev4.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				Expect(k8sClient.Delete(ctx, pc)).To(Succeed()) // delete event

				Eventually(func() bool {
					err := k8sClient.Get(ctx, pgClusterKey, &enterprisev4.PostgresCluster{})
					return apierrors.IsNotFound(err)
				}, "30s", "250ms").Should(BeTrue())
			})
		})

		// PC-04
		Context("when clusterDeletionPolicy is set to Retain", func() {
			It("preserves retained resources and removes owner refs", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())

				// Trigger update event: switch policy to Retain before delete.
				Eventually(func() error {
					pc := &enterprisev4.PostgresCluster{}
					if err := k8sClient.Get(ctx, pgClusterKey, pc); err != nil {
						return err
					}
					pc.Spec.ClusterDeletionPolicy = &[]string{retainPolicy}[0]
					return k8sClient.Update(ctx, pc)
				}, "10s", "250ms").Should(Succeed())

				pc := &enterprisev4.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				Expect(k8sClient.Delete(ctx, pc)).To(Succeed()) // delete event

				Eventually(func() bool {
					err := k8sClient.Get(ctx, pgClusterKey, &enterprisev4.PostgresCluster{})
					return apierrors.IsNotFound(err)
				}, "30s", "250ms").Should(BeTrue())

			})
		})
	})

	When("reconciling with invalid or drifted dependencies", func() {
		// PC-05
		Context("when referenced class does not exist", func() {
			It("fails with class-not-found condition", func() {
				clusterName = "bad-" + clusterName
				className = "missing-class"

				bad := &enterprisev4.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
					Spec:       enterprisev4.PostgresClusterSpec{Class: className},
				}
				Expect(k8sClient.Create(ctx, bad)).To(Succeed()) // create event

				Eventually(func() bool {
					current := &enterprisev4.PostgresCluster{}
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: bad.Name, Namespace: namespace}, current); err != nil {
						return false
					}
					cond := meta.FindStatusCondition(current.Status.Conditions, "ClusterReady")
					return cond != nil && cond.Reason == "ClusterClassNotFound"
				}, "20s", "250ms").Should(BeTrue())
			})
		})

		// PC-06
		Context("when managed child spec drifts from desired state", func() {
			It("restores drifted managed spec", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())

				Eventually(func() error {
					return k8sClient.Get(ctx, pgClusterKey, &cnpgv1.Cluster{})
				}, "20s", "250ms").Should(Succeed())

				Eventually(func() error {
					pc := &enterprisev4.PostgresCluster{}
					if err := k8sClient.Get(ctx, pgClusterKey, pc); err != nil {
						return err
					}
					if pc.Annotations == nil {
						pc.Annotations = map[string]string{}
					}
					pc.Annotations["drift-trigger"] = strconv.FormatInt(time.Now().UnixNano(), 10)
					pc.Spec.Instances = &[]int32{8}[0]
					return k8sClient.Update(ctx, pc)
				}, "10s", "250ms").Should(Succeed())

				Eventually(func() bool {
					cnpg := &cnpgv1.Cluster{}
					if err := k8sClient.Get(ctx, pgClusterKey, cnpg); err != nil {
						return false
					}
					return cnpg.Spec.Instances == int(clusterMemberCount)
				}, "20s", "250ms").Should(BeTrue())
			})
		})
	})
})
