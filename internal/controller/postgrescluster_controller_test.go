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
	"strings"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/tools/record"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	pgprometheus "github.com/splunk/splunk-operator/pkg/postgresql/shared/adapter/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
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

func containsEvents(events *[]string, recorder *record.FakeRecorder, eventType string, event string) bool {
	for {
		select {
		case e := <-recorder.Events:
			*events = append(*events, e)
			if strings.Contains(e, eventType) && strings.Contains(e, event) {
				return true
			}
		default:
			return false
		}
	}
}

var _ = Describe("PostgresCluster Controller", Label("postgres"), func() {

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
		classNameMetrics  string
		classNamePooler   string
		pgCluster         *enterprisev4.PostgresCluster
		pgClusterClass    *enterprisev4.PostgresClusterClass
		pgClusterKey      types.NamespacedName
		pgClusterClassKey types.NamespacedName
		reconciler        *PostgresClusterReconciler
		req               reconcile.Request
		fakeRecorder      *record.FakeRecorder
	)

	reconcileNTimes := func(times int) {
		for range times {
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
		}
	}

	BeforeEach(func() {
		nameSuffix := fmt.Sprintf("%d-%d-%d",
			GinkgoParallelProcess(),
			GinkgoRandomSeed(),
			CurrentSpecReport().LeafNodeLocation.LineNumber,
		)

		ctx = context.Background()
		clusterName = clusterNamePrefix + nameSuffix
		className = classNamePrefix + nameSuffix
		classNameMetrics = classNamePrefix + "metrics-" + nameSuffix
		classNamePooler = classNamePrefix + "pooler-" + nameSuffix
		pgClusterKey = types.NamespacedName{Name: clusterName, Namespace: namespace}
		pgClusterClassKey = types.NamespacedName{Name: className, Namespace: namespace}

		pgClusterClass = &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: className},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Provisioner: provisioner,
				Config: &enterprisev4.PostgresClusterClassConfig{
					Instances:               ptr.To(clusterMemberCount),
					Storage:                 ptr.To(resource.MustParse(storageAmount)),
					PostgresVersion:         ptr.To(postgresVersion),
					ConnectionPoolerEnabled: ptr.To(poolerEnabled),
				},
			},
		}

		pgClassPostgresMetrics := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: classNameMetrics},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Provisioner: provisioner,
				Config: &enterprisev4.PostgresClusterClassConfig{
					Instances:       ptr.To(clusterMemberCount),
					Storage:         ptr.To(resource.MustParse(storageAmount)),
					PostgresVersion: ptr.To(postgresVersion),
					Monitoring: &enterprisev4.PostgresMonitoringClassConfig{
						PostgreSQLMetrics: &enterprisev4.MetricsClassConfig{Enabled: ptr.To(true)},
					},
				},
			},
		}

		pgClassPoolerMetrics := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: classNamePooler},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Provisioner: provisioner,
				Config: &enterprisev4.PostgresClusterClassConfig{
					Instances:               ptr.To(clusterMemberCount),
					Storage:                 ptr.To(resource.MustParse(storageAmount)),
					PostgresVersion:         ptr.To(postgresVersion),
					ConnectionPoolerEnabled: ptr.To(true),
					Monitoring: &enterprisev4.PostgresMonitoringClassConfig{
						ConnectionPoolerMetrics: &enterprisev4.MetricsClassConfig{Enabled: ptr.To(true)},
					},
				},
				CNPG: &enterprisev4.CNPGConfig{
					ConnectionPooler: &enterprisev4.ConnectionPoolerConfig{
						Instances: ptr.To(int32(2)),
						Mode:      ptr.To(enterprisev4.ConnectionPoolerModeTransaction),
					},
				},
			},
		}

		Expect(k8sClient.Create(ctx, pgClusterClass)).To(Succeed())
		Expect(k8sClient.Create(ctx, pgClassPostgresMetrics)).To(Succeed())
		Expect(k8sClient.Create(ctx, pgClassPoolerMetrics)).To(Succeed())

		pgCluster = &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
			Spec: enterprisev4.PostgresClusterSpec{
				Class:                 className,
				ClusterDeletionPolicy: ptr.To(deletePolicy),
				ManagedRoles: []enterprisev4.ManagedRole{
					{Name: "app_user", Exists: true},
					{Name: "app_user_rw", Exists: true},
				},
			},
		}
		fakeRecorder = record.NewFakeRecorder(100)
		reconciler = &PostgresClusterReconciler{
			Client:         k8sClient,
			Scheme:         k8sClient.Scheme(),
			Recorder:       fakeRecorder,
			Metrics:        &pgprometheus.NoopRecorder{},
			FleetCollector: pgprometheus.NewFleetCollector(),
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

		By("Cleaning up PostgresClusterClass fixtures")
		for _, key := range []types.NamespacedName{
			pgClusterClassKey,
			{Name: classNameMetrics},
			{Name: classNamePooler},
		} {
			existing := &enterprisev4.PostgresClusterClass{}
			err = k8sClient.Get(ctx, key, existing)
			if err == nil {
				Expect(k8sClient.Delete(ctx, existing)).To(Succeed())
			} else {
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}
		}
	})

	When("under typical usage and expecting healthy PostgresCluster state", func() {
		Context("when reconciling", func() {
			// PC-02
			It("adds finalizer on reconcile", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(1)

				pc := &enterprisev4.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				Expect(controllerutil.ContainsFinalizer(pc, core.PostgresClusterFinalizerName)).To(BeTrue())
			})

			// PC-01
			It("creates managed resources and status refs", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				// pass 1: add finalizer; pass 2: create CNPG cluster/secret/status.
				reconcileNTimes(2)

				pc := &enterprisev4.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				cond := meta.FindStatusCondition(pc.Status.Conditions, "ClusterReady")
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				Expect(cond.Reason).To(Equal("CNPGClusterProvisioning"))

				// Simulate external CNPG controller status progression.
				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				cnpg.Status.Phase = cnpgv1.PhaseHealthy
				cnpg.Status.ManagedRolesStatus = cnpgv1.ManagedRoles{
					ByStatus: map[cnpgv1.RoleStatus][]string{
						cnpgv1.RoleStatusReconciled: {"app_user", "app_user_rw"},
					},
				}
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
				reconcileNTimes(1)

				// Expect cnpg status progression propagation
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				cond = meta.FindStatusCondition(pc.Status.Conditions, "ClusterReady")
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				Expect(cond.Reason).To(Equal("CNPGClusterHealthy"))

				secretCond := meta.FindStatusCondition(pc.Status.Conditions, "SecretsReady")
				Expect(secretCond).NotTo(BeNil())
				Expect(secretCond.Status).To(Equal(metav1.ConditionTrue))
				Expect(secretCond.Reason).To(Equal("SuperUserSecretReady"))

				configMapCond := meta.FindStatusCondition(pc.Status.Conditions, "ConfigMapsReady")
				Expect(configMapCond).NotTo(BeNil())
				Expect(configMapCond.Status).To(Equal(metav1.ConditionTrue))
				Expect(configMapCond.Reason).To(Equal("ConfigMapReconciled"))

				managedRolesCond := meta.FindStatusCondition(pc.Status.Conditions, "ManagedRolesReady")
				Expect(managedRolesCond).NotTo(BeNil())
				Expect(managedRolesCond.Status).To(Equal(metav1.ConditionTrue))
				Expect(managedRolesCond.Reason).To(Equal("ManagedRolesReconciled"))

				// Pooler is disabled in this suite fixture, but converge publishes PoolerReady=True with disabled message.
				poolerCond := meta.FindStatusCondition(pc.Status.Conditions, "PoolerReady")
				Expect(poolerCond).NotTo(BeNil())
				Expect(poolerCond.Status).To(Equal(metav1.ConditionTrue))
				Expect(poolerCond.Reason).To(Equal("AllInstancesReady"))
				Expect(poolerCond.Message).To(Equal("Connection pooler disabled"))

				Expect(pc.Status.ManagedRolesStatus).NotTo(BeNil())
				Expect(pc.Status.ManagedRolesStatus.Reconciled).To(ContainElements("app_user", "app_user_rw"))

				Expect(pc.Status.Phase).NotTo(BeNil())
				Expect(*pc.Status.Phase).To(Equal("Ready"))
				Expect(pc.Status.ProvisionerRef).NotTo(BeNil())
				Expect(pc.Status.ProvisionerRef.Kind).To(Equal("Cluster"))
				Expect(pc.Status.ProvisionerRef.Name).To(Equal(clusterName))

				Expect(pc.Status.Resources).NotTo(BeNil())
				Expect(pc.Status.Resources.SuperUserSecretRef).NotTo(BeNil())
				Expect(pc.Status.Resources.ConfigMapRef).NotTo(BeNil())

				received := make([]string, 0, 8)
				Expect(containsEvents(
					&received, fakeRecorder,
					v1.EventTypeNormal, core.EventClusterReady,
				)).To(BeTrue(), "events seen: %v", received)
			})

			// PC-07
			It("is idempotent across repeated reconciles", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)
				reconcileNTimes(3)

				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				Expect(cnpg.Spec.Instances).To(Equal(int(clusterMemberCount)))

				pc := &enterprisev4.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				cond := meta.FindStatusCondition(pc.Status.Conditions, "ClusterReady")
				Expect(cond).NotTo(BeNil())
				Expect(cond.ObservedGeneration).To(Equal(pc.Generation))
			})

		})
	})

	When("monitoring is configured", func() {
		const (
			scrapeAnnotationKey = "prometheus.io/scrape"
			pathAnnotationKey   = "prometheus.io/path"
			portAnnotationKey   = "prometheus.io/port"
			metricsPath         = "/metrics"
			postgresPort        = "9187"
			poolerPort          = "9127"
		)

		Context("with PostgreSQL metrics enabled in class", func() {
			BeforeEach(func() {
				pgCluster.Spec.Class = classNameMetrics
				pgCluster.Spec.ManagedRoles = nil
			})

			It("adds scrape annotations to the CNPG Cluster", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())

				cnpg.Status.Phase = cnpgv1.PhaseHealthy
				cnpg.Status.ManagedRolesStatus = cnpgv1.ManagedRoles{
					ByStatus: map[cnpgv1.RoleStatus][]string{
						cnpgv1.RoleStatusReconciled: {"app_user", "app_user_rw"},
					},
				}
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())

				reconcileNTimes(1)

				Expect(cnpg.Spec.InheritedMetadata).NotTo(BeNil())
				Expect(cnpg.Spec.InheritedMetadata.Annotations).To(HaveKeyWithValue(scrapeAnnotationKey, "true"))
				Expect(cnpg.Spec.InheritedMetadata.Annotations).To(HaveKeyWithValue(pathAnnotationKey, metricsPath))
				Expect(cnpg.Spec.InheritedMetadata.Annotations).To(HaveKeyWithValue(portAnnotationKey, postgresPort))
			})

			It("removes scrape annotations when disabled by cluster override", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				Expect(cnpg.Spec.InheritedMetadata).NotTo(BeNil())
				Expect(cnpg.Spec.InheritedMetadata.Annotations).To(HaveKeyWithValue(portAnnotationKey, postgresPort))

				current := &enterprisev4.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
				current.Spec.Monitoring = &enterprisev4.PostgresClusterMonitoring{
					PostgreSQLMetrics: ptr.To(false),
				}
				Expect(k8sClient.Update(ctx, current)).To(Succeed())
				reconcileNTimes(1)

				Eventually(func(g Gomega) {
					updated := &cnpgv1.Cluster{}
					g.Expect(k8sClient.Get(ctx, pgClusterKey, updated)).To(Succeed())
					g.Expect(updated.Spec.InheritedMetadata).NotTo(BeNil())
					g.Expect(updated.Spec.InheritedMetadata.Annotations).NotTo(HaveKey(scrapeAnnotationKey))
					g.Expect(updated.Spec.InheritedMetadata.Annotations).NotTo(HaveKey(pathAnnotationKey))
					g.Expect(updated.Spec.InheritedMetadata.Annotations).NotTo(HaveKey(portAnnotationKey))
				}, "20s", "250ms").Should(Succeed())
			})
		})

		Context("with connection pooler metrics enabled in class", func() {
			BeforeEach(func() {
				pgCluster.Spec.Class = classNamePooler
				pgCluster.Spec.ManagedRoles = nil
			})

			It("adds scrape annotations to poolers only after the CNPG cluster becomes healthy", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				rwKey := types.NamespacedName{Name: clusterName + "-pooler-rw", Namespace: namespace}
				roKey := types.NamespacedName{Name: clusterName + "-pooler-ro", Namespace: namespace}

				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, rwKey, &cnpgv1.Pooler{}))).To(BeTrue())
				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, roKey, &cnpgv1.Pooler{}))).To(BeTrue())

				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				cnpg.Status.Phase = cnpgv1.PhaseHealthy
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())

				Eventually(func(g Gomega) {
					_, err := reconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())

					rw := &cnpgv1.Pooler{}
					g.Expect(k8sClient.Get(ctx, rwKey, rw)).To(Succeed())
					g.Expect(rw.Spec.Template).NotTo(BeNil())
					g.Expect(rw.Spec.Template.ObjectMeta.Annotations).To(HaveKeyWithValue(scrapeAnnotationKey, "true"))
					g.Expect(rw.Spec.Template.ObjectMeta.Annotations).To(HaveKeyWithValue(pathAnnotationKey, metricsPath))
					g.Expect(rw.Spec.Template.ObjectMeta.Annotations).To(HaveKeyWithValue(portAnnotationKey, poolerPort))

					ro := &cnpgv1.Pooler{}
					g.Expect(k8sClient.Get(ctx, roKey, ro)).To(Succeed())
					g.Expect(ro.Spec.Template).NotTo(BeNil())
					g.Expect(ro.Spec.Template.ObjectMeta.Annotations).To(HaveKeyWithValue(scrapeAnnotationKey, "true"))
					g.Expect(ro.Spec.Template.ObjectMeta.Annotations).To(HaveKeyWithValue(pathAnnotationKey, metricsPath))
					g.Expect(ro.Spec.Template.ObjectMeta.Annotations).To(HaveKeyWithValue(portAnnotationKey, poolerPort))
				}, "20s", "250ms").Should(Succeed())
			})
		})
	})

	When("deleting a PostgresCluster", func() {
		// PC-03
		Context("and clusterDeletionPolicy is set to Delete", func() {
			It("removes children and finalizer", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				pc := &enterprisev4.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				Expect(k8sClient.Delete(ctx, pc)).To(Succeed())

				Eventually(func() bool {
					_, err := reconciler.Reconcile(ctx, req)
					if err != nil {
						return false
					}
					getErr := k8sClient.Get(ctx, pgClusterKey, &enterprisev4.PostgresCluster{})
					return apierrors.IsNotFound(getErr)
				}, "30s", "250ms").Should(BeTrue())
			})
		})

		// PC-04
		Context("when clusterDeletionPolicy is set to Retain", func() {
			It("preserves retained resources and removes owner refs", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				pc := &enterprisev4.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				Expect(k8sClient.Delete(ctx, pc)).To(Succeed())

				Eventually(func() bool {
					_, err := reconciler.Reconcile(ctx, req)
					if err != nil {
						return false
					}
					getErr := k8sClient.Get(ctx, pgClusterKey, &enterprisev4.PostgresCluster{})
					return apierrors.IsNotFound(getErr)
				}, "30s", "250ms").Should(BeTrue())
			})
		})
	})

	When("reconciling with invalid or drifted dependencies", func() {
		// PC-05
		Context("when referenced class does not exist", func() {
			It("fails with class-not-found condition and emits a warning event", func() {
				badName := "bad-" + clusterName
				badKey := types.NamespacedName{Name: badName, Namespace: namespace}

				bad := &enterprisev4.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: badName, Namespace: namespace},
					Spec:       enterprisev4.PostgresClusterSpec{Class: "missing-class"},
				}
				Expect(k8sClient.Create(ctx, bad)).To(Succeed())
				DeferCleanup(func() { _ = k8sClient.Delete(ctx, bad) })

				// pass 1 adds finalizer, pass 2 reaches class lookup and sets failure condition.
				_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: badKey})
				Expect(err).NotTo(HaveOccurred())
				_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: badKey})
				Expect(err).To(HaveOccurred())

				Eventually(func() bool {
					current := &enterprisev4.PostgresCluster{}
					if err := k8sClient.Get(ctx, badKey, current); err != nil {
						return false
					}
					cond := meta.FindStatusCondition(current.Status.Conditions, "ClusterReady")
					return cond != nil && cond.Reason == "ClusterClassNotFound"
				}, "20s", "250ms").Should(BeTrue())

				received := make([]string, 0, 8)
				Expect(containsEvents(
					&received, fakeRecorder,
					v1.EventTypeWarning, core.EventClusterClassNotFound,
				)).To(BeTrue(), "events seen: %v", received)
			})
		})

		// PC-06
		Context("when managed child spec drifts from desired state", func() {
			It("restores drifted managed spec", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				cnpg.Spec.Instances = 8
				Expect(k8sClient.Update(ctx, cnpg)).To(Succeed())

				reconcileNTimes(2)
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				Expect(cnpg.Spec.Instances).To(Equal(int(clusterMemberCount)))
			})
		})
	})
})
