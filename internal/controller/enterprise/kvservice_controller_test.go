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
	"fmt"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/config"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

var _ = Describe("KVService Controller", func() {
	It("routes peer events within the hosting namespace without status loops", func() {
		survivor := &enterprisev4.KVService{ObjectMeta: metav1.ObjectMeta{Name: "survivor", Namespace: "shared"}}
		peer := &enterprisev4.KVService{ObjectMeta: metav1.ObjectMeta{Name: "peer", Namespace: "shared"}}
		elsewhere := &enterprisev4.KVService{ObjectMeta: metav1.ObjectMeta{Name: "elsewhere", Namespace: "other"}}
		c := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).WithObjects(survivor, peer, elsewhere).Build()
		r := &KVServiceReconciler{Client: c}
		Expect(r.mapKVServicePeers(context.Background(), peer)).To(Equal([]reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(survivor)}}))
		updated := peer.DeepCopy()
		updated.Status.Phase = enterprisev4.PhaseError
		Expect(kvServicePeerChangedPredicate().Update(event.UpdateEvent{ObjectOld: peer, ObjectNew: updated})).To(BeFalse())
		Expect(kvServicePeerChangedPredicate().Generic(event.GenericEvent{Object: peer})).To(BeFalse())
		Expect(r.mapKVServicePeers(context.Background(), &corev1.ConfigMap{})).To(BeEmpty())
	})

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
					Postgres: enterprisev4.KVServicePostgresSpec{
						ClusterRef: corev1.LocalObjectReference{Name: "paused-postgres"},
					},
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
		const clusterName = "test-postgres"
		const backendNamespacePrefix = "kvservice-backend-"

		ctx := context.Background()

		var typeNamespacedName types.NamespacedName
		kvservice := &enterprisev4.KVService{}

		BeforeEach(func() {
			Expect(os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")).To(Succeed())
			postgresEnabled := config.DefaultMutableFeatureGate.Enabled(config.PostgresController)
			Expect(config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): true})).To(Succeed())
			DeferCleanup(func() {
				Expect(config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): postgresEnabled})).To(Succeed())
			})
			watchNamespace, watchNamespaceSet := os.LookupEnv("WATCH_NAMESPACE")
			Expect(os.Setenv("WATCH_NAMESPACE", "")).To(Succeed())
			DeferCleanup(func() {
				if watchNamespaceSet {
					Expect(os.Setenv("WATCH_NAMESPACE", watchNamespace)).To(Succeed())
				} else {
					Expect(os.Unsetenv("WATCH_NAMESPACE")).To(Succeed())
				}
			})

			createFixture := func(object client.Object) {
				Expect(k8sClient.Create(ctx, object)).To(Succeed())
				DeferCleanup(func() {
					Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, object))).To(Succeed())
				})
			}
			backendNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: backendNamespacePrefix}}
			createFixture(backendNamespace)
			tenantName := "controller" + backendNamespace.Name[len(backendNamespacePrefix):]
			typeNamespacedName = types.NamespacedName{Name: resourceName, Namespace: backendNamespace.Name}
			createFixture(&corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: "kvservice-test-sa", Namespace: backendNamespace.Name},
			})
			By("creating the KVService with a required PostgreSQL cluster reference")
			kvservice = &enterprisev4.KVService{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: backendNamespace.Name},
				Spec: enterprisev4.KVServiceSpec{
					CommonWorkloadSpec: enterprisev4.CommonWorkloadSpec{
						Spec:                        enterprisev4.Spec{ImagePullPolicy: "IfNotPresent"},
						ServiceAccount:              "kvservice-test-sa",
						LivenessInitialDelaySeconds: 90,
					},
					ReadinessProbe: &enterprisev4.Probe{TimeoutSeconds: 7},
					Replicas:       1,
					Postgres: enterprisev4.KVServicePostgresSpec{
						ClusterRef: corev1.LocalObjectReference{Name: clusterName},
					},
				},
			}
			createFixture(kvservice)

			By("publishing independent ready tenant database connection dependencies")
			tenantNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
				GenerateName: "kvservice-tenant-",
				Labels: map[string]string{
					splcommon.LabelKVService: splcommon.LabelKVServiceEnabled,
					splcommon.LabelTenant:    tenantName,
				},
				Annotations: map[string]string{splcommon.AnnotationKVServiceAddr: fmt.Sprintf("%s.%s:%d",
					splcommon.GetSplunkServiceName(splcommon.SplunkKVService, resourceName, false), backendNamespace.Name, splcommon.KVServicePort)},
			}}
			createFixture(tenantNamespace)
			createFixture(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: splcommon.GetNamespaceScopedSecretName(tenantNamespace.Name), Namespace: tenantNamespace.Name},
				Data:       map[string][]byte{"pass4SymmKey": []byte("controller-tenant-key")},
			})
			createFixture(&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "tenant-connection", Namespace: backendNamespace.Name},
				Data: map[string]string{
					"CLUSTER_RW_ENDPOINT":  "test-postgres-rw." + backendNamespace.Name,
					"DEFAULT_CLUSTER_PORT": "5432",
					"ADMIN_USER_NAME":      tenantName + "_admin",
				},
			})
			createFixture(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "tenant-admin", Namespace: backendNamespace.Name},
				Data:       map[string][]byte{"password": []byte("controller-database-password")},
			})
			// The PostgreSQL controller owns cluster readiness and connection publication.
			// Supply its API contract directly; this suite needs no PostgreSQL server.
			database := &platformv1alpha1.PostgresDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name: tenantName + "-database", Namespace: backendNamespace.Name,
					Annotations: map[string]string{"enterprise.splunk.com/kvservice-owner": backendNamespace.Name + "/" + resourceName},
				},
				Spec: platformv1alpha1.PostgresDatabaseSpec{
					ClusterRef: corev1.LocalObjectReference{Name: clusterName},
					Databases: []platformv1alpha1.DatabaseDefinition{{
						Name: tenantName, DeletionPolicy: "Retain", Extensions: []string{"pg_stat_statements", "pgcrypto"},
					}},
				},
			}
			createFixture(database)
			ready, generation := "Ready", database.Generation
			database.Status = platformv1alpha1.PostgresDatabaseStatus{
				Phase: &ready, ObservedGeneration: &generation,
				Databases: []platformv1alpha1.DatabaseInfo{{
					Name: tenantName, Ready: true,
					ConfigMapRef: &corev1.LocalObjectReference{Name: "tenant-connection"},
					AdminUserSecretRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "tenant-admin"}, Key: "password",
					},
				}},
			}
			Expect(k8sClient.Status().Update(ctx, database)).To(Succeed())
		})

		AfterEach(func() {
			Expect(os.Unsetenv("SPLUNK_GENERAL_TERMS")).To(Succeed())
		})
		It("recovers from bypassed duplicate admission through peer deletion events", func() {
			duplicate := kvservice.DeepCopy()
			duplicate.Name, duplicate.UID, duplicate.ResourceVersion = "duplicate", "", ""
			duplicate.CreationTimestamp = metav1.Time{}
			Expect(k8sClient.Create(ctx, duplicate)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, duplicate))).To(Succeed())
			})
			// Register the real controller and watch handlers. The normal suite
			// uses explicit KVService reconciliation instead of this manager.
			manager, err := ctrl.NewManager(cfg, ctrl.Options{
				Scheme:                 k8sClient.Scheme(),
				Metrics:                metricsserver.Options{BindAddress: "0"},
				HealthProbeBindAddress: "0",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect((&KVServiceReconciler{Client: manager.GetClient(), Scheme: manager.GetScheme()}).SetupWithManager(manager)).To(Succeed())
			managerCtx, cancel := context.WithCancel(ctx)
			done := make(chan error, 1)
			go func() { done <- manager.Start(managerCtx) }()
			DeferCleanup(func() {
				cancel()
				Eventually(done, 30*time.Second).Should(Receive(Succeed()))
			})
			isConflicted := func(key types.NamespacedName) bool {
				current := &enterprisev4.KVService{}
				if err := k8sClient.Get(ctx, key, current); err != nil {
					return false
				}
				stalled := meta.FindStatusCondition(current.Status.Conditions, string(enterprisev4.ConditionStalled))
				return stalled != nil && stalled.Status == metav1.ConditionTrue && strings.Contains(stalled.Message, "multiple KVServices")
			}
			Eventually(func() bool { return isConflicted(typeNamespacedName) }, 20*time.Second).Should(BeTrue())
			Eventually(func() bool { return isConflicted(client.ObjectKeyFromObject(duplicate)) }, 20*time.Second).Should(BeTrue())
			deploymentKey := types.NamespacedName{Namespace: typeNamespacedName.Namespace,
				Name: splutil.GetSplunkDeploymentName(splcommon.SplunkKVService, resourceName)}
			Expect(k8sClient.Get(ctx, deploymentKey, &appsv1.Deployment{})).To(MatchError(ContainSubstring("not found")))
			Expect(k8sClient.Delete(ctx, duplicate)).To(Succeed())
			// No manual reconcile or survivor spec edit: the peer watch must
			// wake the terminal CR and allow provisioning to resume.
			Eventually(func() error { return k8sClient.Get(ctx, deploymentKey, &appsv1.Deployment{}) }, 20*time.Second).Should(Succeed())
			Eventually(func() bool { return isConflicted(typeNamespacedName) }, 20*time.Second).Should(BeFalse())
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
