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
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/config"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controllerconfig "sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var _ = Describe("KVService automatic dependency watches", func() {
	BeforeEach(func() {
		for name, value := range map[string]string{
			"WATCH_NAMESPACE": "", "SPLUNK_GENERAL_TERMS": "--accept-sgt-current-at-splunk-com",
		} {
			previous, exists := os.LookupEnv(name)
			Expect(os.Setenv(name, value)).To(Succeed())
			DeferCleanup(func() {
				if exists {
					Expect(os.Setenv(name, previous)).To(Succeed())
				} else {
					Expect(os.Unsetenv(name)).To(Succeed())
				}
			})
		}
		previous := config.DefaultMutableFeatureGate.Enabled(config.PostgresController)
		Expect(config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): true})).To(Succeed())
		DeferCleanup(func() {
			Expect(config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): previous})).To(Succeed())
		})
	})

	It("recovers from invalid Namespace metadata, provisions its tenant, and revokes an opted-out tenant", func(ctx SpecContext) {
		fixture := newKVServiceWatchFixture(ctx, true, false)
		fixture.waitStalled(ctx)
		// No Reconcile call: a terminal result has no polling retry. Changing
		// this Namespace must enqueue the KVService through its real informer.
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(fixture.namespace), fixture.namespace)).To(Succeed())
		fixture.namespace.Labels[splcommon.LabelTenant] = fixture.tenant
		Expect(k8sClient.Update(ctx, fixture.namespace)).To(Succeed())
		fixture.waitKeys(ctx, []string{"watch-original-key"})
		fixture.waitRecovered(ctx)

		database := fixture.waitDatabase(ctx)
		databaseKey := client.ObjectKeyFromObject(database)
		Expect(database.Spec.ClusterRef).To(Equal(fixture.cr.Spec.Postgres.ClusterRef))
		Expect(database.Spec.Databases[0].Name).To(Equal(fixture.tenant))
		Expect(database.Spec.Databases[0].DeletionPolicy).To(Equal("Retain"))
		Expect(database.OwnerReferences).To(BeEmpty())

		fixture.makeReady(ctx, database)
		fixture.waitIdleReady(ctx)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(fixture.namespace), fixture.namespace)).To(Succeed())
		delete(fixture.namespace.Labels, splcommon.LabelKVService)
		Expect(k8sClient.Update(ctx, fixture.namespace)).To(Succeed())
		Eventually(func(g Gomega) {
			deployment := &appsv1.Deployment{}
			g.Expect(k8sClient.Get(ctx, fixture.deploymentKey(), deployment)).To(Succeed())
			g.Expect(deployment.Spec.Replicas).To(HaveValue(BeZero()))
			membership := &corev1.ConfigMap{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: fixture.cr.Namespace, Name: "splunk-" + fixture.cr.Name + "-kvservice-tenants"}, membership)).To(Succeed())
			g.Expect(membership.Data).To(BeEmpty())
			ring := &corev1.Secret{}
			g.Expect(k8sClient.Get(ctx, fixture.keyringKey(), ring)).To(Succeed())
			g.Expect(ring.Data).To(BeEmpty())
		}, 10*time.Second, 50*time.Millisecond).Should(Succeed())
		Expect(k8sClient.Get(ctx, databaseKey, database)).To(Succeed(), "tenant removal must retain the database")
	}, SpecTimeout(60*time.Second))

	It("recovers on a cross-namespace source Secret update and publishes both rotation keys", func(ctx SpecContext) {
		fixture := newKVServiceWatchFixture(ctx, false, true)
		fixture.waitStalled(ctx)
		Expect(fixture.source.Namespace).NotTo(Equal(fixture.cr.Namespace))
		Expect(fixture.source.OwnerReferences).To(BeEmpty(), "the source cannot rely on an owner watch")
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(fixture.source), fixture.source)).To(Succeed())
		fixture.source.Data = map[string][]byte{"pass4SymmKey": []byte("watch-original-key")}
		Expect(k8sClient.Update(ctx, fixture.source)).To(Succeed())
		fixture.waitKeys(ctx, []string{"watch-original-key"})
		fixture.waitRecovered(ctx)

		fixture.makeReady(ctx, fixture.waitDatabase(ctx))
		fixture.waitIdleReady(ctx)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(fixture.source), fixture.source)).To(Succeed())
		fixture.source.Data["pass4SymmKey"] = []byte("watch-rotated-key")
		Expect(k8sClient.Update(ctx, fixture.source)).To(Succeed())
		fixture.waitKeys(ctx, []string{"watch-rotated-key", "watch-original-key"})
		current := &enterpriseApi.KVService{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(fixture.cr), current)).To(Succeed())
		Expect(current.Generation).To(Equal(fixture.cr.Generation), "source events must not require editing the KVService spec")
	}, SpecTimeout(60*time.Second))

	It("observes a database status-only change from an idle Ready service", func(ctx SpecContext) {
		fixture := newKVServiceWatchFixture(ctx, false, false)
		database := fixture.waitDatabase(ctx)
		fixture.makeReady(ctx, database)
		fixture.waitIdleReady(ctx)

		// Only the PostgreSQL status subresource changes. No KVService spec,
		// Namespace, Secret, Deployment, or connection reference is modified.
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(database), database)).To(Succeed())
		generation := database.Generation
		database.Status.Phase = ptr.To("Pending")
		Expect(k8sClient.Status().Update(ctx, database)).To(Succeed())
		Expect(database.Generation).To(Equal(generation))
		Eventually(func(g Gomega) {
			current := &enterpriseApi.KVService{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(fixture.cr), current)).To(Succeed())
			g.Expect(current.Status.Phase).To(Equal(enterpriseApi.PhasePending))
			g.Expect(splcommon.IsStalled(current.Status.Conditions)).To(BeFalse())
		}, 10*time.Second, 50*time.Millisecond).Should(Succeed())
	}, SpecTimeout(60*time.Second))
})

type kvserviceWatchFixture struct {
	cr        *enterpriseApi.KVService
	namespace *corev1.Namespace
	source    *corev1.Secret
	tenant    string
	recorder  *events.FakeRecorder
}

func newKVServiceWatchFixture(ctx context.Context, invalidTenant, missingKey bool) *kvserviceWatchFixture {
	backend := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "kv-watch-backend-"}}
	Expect(k8sClient.Create(ctx, backend)).To(Succeed())
	tenant := "watch" + strings.TrimPrefix(backend.Name, "kv-watch-backend-")
	fixture := &kvserviceWatchFixture{
		tenant: tenant, recorder: events.NewFakeRecorder(1000),
		cr: &enterpriseApi.KVService{
			ObjectMeta: metav1.ObjectMeta{Name: "watch", Namespace: backend.Name},
			Spec: enterpriseApi.KVServiceSpec{
				CommonWorkloadSpec: enterpriseApi.CommonWorkloadSpec{Spec: enterpriseApi.Spec{
					Image: "kvstore/kvservice:watch-test", ImagePullPolicy: "IfNotPresent",
				}},
				Replicas: 1,
				Postgres: enterpriseApi.KVServicePostgresSpec{ClusterRef: corev1.LocalObjectReference{Name: "watch-postgres"}},
			},
		},
		namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			GenerateName: "kv-watch-tenant-",
			Labels:       map[string]string{splcommon.LabelKVService: splcommon.LabelKVServiceEnabled, splcommon.LabelTenant: tenant},
			Annotations: map[string]string{splcommon.AnnotationKVServiceAddr: fmt.Sprintf("%s.%s:%d",
				splcommon.GetSplunkServiceName(splcommon.SplunkKVService, "watch", false), backend.Name, splcommon.KVServicePort)},
		}},
	}
	if invalidTenant {
		fixture.namespace.Labels[splcommon.LabelTenant] = "invalid-tenant"
	}
	Expect(k8sClient.Create(ctx, fixture.namespace)).To(Succeed())
	fixture.source = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: splcommon.GetNamespaceScopedSecretName(fixture.namespace.Name), Namespace: fixture.namespace.Name},
		Data:       map[string][]byte{"pass4SymmKey": []byte("watch-original-key")},
	}
	if missingKey {
		fixture.source.Data = nil
	}
	Expect(k8sClient.Create(ctx, fixture.source)).To(Succeed())
	Expect(k8sClient.Create(ctx, fixture.cr)).To(Succeed())

	// Do not register KVService in the suite's shared manager. Each spec owns
	// a manager and namespace-scoped cache, avoiding interference with manual
	// reconcile specs and previously-created backend resources.
	manager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:  k8sClient.Scheme(),
		Metrics: metricsserver.Options{BindAddress: "0"}, HealthProbeBindAddress: "0",
		Controller: controllerconfig.Controller{SkipNameValidation: ptr.To(true)},
		Cache:      cache.Options{DefaultNamespaces: map[string]cache.Config{backend.Name: {}, fixture.namespace.Name: {}}},
	})
	Expect(err).NotTo(HaveOccurred())
	Expect((&KVServiceReconciler{Client: manager.GetClient(), Scheme: manager.GetScheme(), Recorder: fixture.recorder}).SetupWithManager(manager)).To(Succeed())
	managerCtx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- manager.Start(managerCtx) }()
	DeferCleanup(func() {
		cancel()
		Eventually(finished, 10*time.Second).Should(Receive(Succeed()))
		// Envtest has no namespace/owner garbage collector. These two
		// namespaces belong only to this spec; do not touch shared fixtures.
		Expect(client.IgnoreNotFound(k8sClient.Delete(context.Background(), fixture.namespace))).To(Succeed())
		Expect(client.IgnoreNotFound(k8sClient.Delete(context.Background(), backend))).To(Succeed())
	})
	Expect(manager.GetCache().WaitForCacheSync(ctx)).To(BeTrue())
	return fixture
}

func (f *kvserviceWatchFixture) keyringKey() client.ObjectKey {
	return client.ObjectKey{Namespace: f.cr.Namespace, Name: splcommon.KVServiceTenantVerificationKeysName}
}

func (f *kvserviceWatchFixture) deploymentKey() client.ObjectKey {
	return client.ObjectKey{Namespace: f.cr.Namespace, Name: splutil.GetSplunkDeploymentName(splcommon.SplunkKVService, f.cr.Name)}
}

func (f *kvserviceWatchFixture) waitDatabase(ctx context.Context) *platformApi.PostgresDatabase {
	database := &platformApi.PostgresDatabase{}
	Eventually(func() error {
		return k8sClient.Get(ctx, client.ObjectKey{Namespace: f.cr.Namespace, Name: f.tenant + "-database"}, database)
	}, 10*time.Second, 50*time.Millisecond).Should(Succeed())
	return database
}

func (f *kvserviceWatchFixture) waitIdleReady(ctx context.Context) {
	// Pending provisioning schedules a five-second delayed retry. Ready
	// returns no requeue, but does not cancel work already in that queue.
	// Stay Ready beyond that delay before changing the watched dependency,
	// so an old provisioning poll cannot masquerade as informer delivery.
	Consistently(func(g Gomega) {
		current := &enterpriseApi.KVService{}
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(f.cr), current)).To(Succeed())
		g.Expect(current.Status.Phase).To(Equal(enterpriseApi.PhaseReady))
		g.Expect(splcommon.IsStalled(current.Status.Conditions)).To(BeFalse())
	}, 6*time.Second, 50*time.Millisecond).Should(Succeed())

	// Ready reconciles emit a Ready event. Drain those produced by startup
	// and the delayed retry, then observe a quiet manager before mutation.
	for len(f.recorder.Events) > 0 {
		<-f.recorder.Events
	}
	Consistently(f.recorder.Events, time.Second, 50*time.Millisecond).ShouldNot(Receive(), "Ready must be idle before testing the next watch")
}

func (f *kvserviceWatchFixture) waitStalled(ctx context.Context) {
	Eventually(func(g Gomega) {
		current := &enterpriseApi.KVService{}
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(f.cr), current)).To(Succeed())
		g.Expect(splcommon.IsStalled(current.Status.Conditions)).To(BeTrue())
	}, 10*time.Second, 50*time.Millisecond).Should(Succeed())
	// Let startup events from owned projections and database creation drain.
	// Every terminal reconcile emits Stalled; a quiet recorder establishes an
	// idle controller before the sole recovery stimulus (Namespace or Secret).
	lastStall := time.Now()
	sawStall := false
	Eventually(func() bool {
		for {
			select {
			case message := <-f.recorder.Events:
				if strings.HasPrefix(message, "Warning "+splcommon.EventReasonStalled+" ") {
					sawStall = true
					lastStall = time.Now()
				}
			default:
				return sawStall && time.Since(lastStall) >= time.Second
			}
		}
	}, 10*time.Second, 50*time.Millisecond).Should(BeTrue())
}

func (f *kvserviceWatchFixture) waitKeys(ctx context.Context, expected []string) {
	Eventually(func(g Gomega) {
		ring := &corev1.Secret{}
		g.Expect(k8sClient.Get(ctx, f.keyringKey(), ring)).To(Succeed())
		var keys []string
		g.Expect(json.Unmarshal(ring.Data[f.tenant], &keys)).To(Succeed())
		g.Expect(keys).To(Equal(expected))
	}, 10*time.Second, 50*time.Millisecond).Should(Succeed())
}

func (f *kvserviceWatchFixture) waitRecovered(ctx context.Context) {
	Eventually(func(g Gomega) {
		current := &enterpriseApi.KVService{}
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(f.cr), current)).To(Succeed())
		g.Expect(splcommon.IsStalled(current.Status.Conditions)).To(BeFalse())
	}, 10*time.Second, 50*time.Millisecond).Should(Succeed())
	Eventually(func() bool {
		for {
			select {
			case message := <-f.recorder.Events:
				if strings.HasPrefix(message, "Normal "+splcommon.EventReasonStalledResolved+" ") {
					return true
				}
			default:
				return false
			}
		}
	}, 5*time.Second, 50*time.Millisecond).Should(BeTrue(), "automatic recovery must publish the standard event")
}

func (f *kvserviceWatchFixture) makeReady(ctx context.Context, database *platformApi.PostgresDatabase) {
	// Supply the PostgreSQL controller's connection/status contract. Envtest
	// runs no database or Deployment controller; neither requires a live pod.
	Expect(k8sClient.Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "watch-connection", Namespace: f.cr.Namespace}, Data: map[string]string{
		"CLUSTER_RW_ENDPOINT": "watch-postgres-rw." + f.cr.Namespace, "DEFAULT_CLUSTER_PORT": "5432", "ADMIN_USER_NAME": f.tenant + "_admin",
	}})).To(Succeed())
	Expect(k8sClient.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "watch-owner", Namespace: f.cr.Namespace}, Data: map[string][]byte{"password": []byte("watch-database-password")}})).To(Succeed())
	ready, generation := "Ready", database.Generation
	database.Status = platformApi.PostgresDatabaseStatus{
		Phase: &ready, ObservedGeneration: &generation,
		Databases: []platformApi.DatabaseInfo{{Name: f.tenant, Ready: true,
			ConfigMapRef:       &corev1.LocalObjectReference{Name: "watch-connection"},
			AdminUserSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "watch-owner"}, Key: "password"},
		}},
	}
	Expect(k8sClient.Status().Update(ctx, database)).To(Succeed())
	deployment := &appsv1.Deployment{}
	Eventually(func() error { return k8sClient.Get(ctx, f.deploymentKey(), deployment) }, 10*time.Second, 50*time.Millisecond).Should(Succeed())
	deployment.Status = appsv1.DeploymentStatus{Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1}
	Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())
	Eventually(func(g Gomega) {
		current := &enterpriseApi.KVService{}
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(f.cr), current)).To(Succeed())
		g.Expect(current.Status.Phase).To(Equal(enterpriseApi.PhaseReady))
	}, 15*time.Second, 50*time.Millisecond).Should(Succeed())
}
