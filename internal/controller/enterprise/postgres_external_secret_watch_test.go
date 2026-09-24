/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	pgprometheus "github.com/splunk/splunk-operator/pkg/postgresql/shared/adapter/prometheus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

/*
intentionally manager-driven — they never call Reconcile()
explicitly. The whole point is to prove that controller-runtime is wiring the
Watches(&corev1.Secret{}, ...) correctly
*/

var _ = Describe("PostgresCluster external Secret watch", Ordered, Label("postgres", "postgres-watch"), func() {
	const (
		namespace       = "default"
		postgresVersion = "15.10"
		instances       = int32(1)
		storageAmount   = "1Gi"

		watchTimeout = 30 * time.Second
		pollInterval = 250 * time.Millisecond
	)

	var (
		ctx           context.Context
		mgrCtx        context.Context
		mgrCancel     context.CancelFunc
		mgrDone       chan struct{}
		watchManager  manager.Manager
		suffix        string
		className     string
		clusterName   string
		secretName    string
		pgClusterKey  types.NamespacedName
		extSecretKey  types.NamespacedName
		pgCluster     *enterprisev4.PostgresCluster
		clusterClass  *enterprisev4.PostgresClusterClass
		externalCreds = map[string][]byte{
			"username": []byte("postgres"),
			"password": []byte("p4ssw0rd-rotated"),
		}
	)

	secretsReady := func(g Gomega) *metav1.Condition {
		pc := &enterprisev4.PostgresCluster{}
		if err := k8sClient.Get(ctx, pgClusterKey, pc); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			g.Expect(err).NotTo(HaveOccurred())
		}
		return meta.FindStatusCondition(pc.Status.Conditions, "SecretsReady")
	}

	BeforeAll(func() {
		ctx = context.Background()

		By("spinning up a dedicated manager for the PostgresCluster reconciler")

		var err error
		watchManager, err = ctrl.NewManager(cfg, ctrl.Options{
			Scheme:  clientgoscheme.Scheme,
			Metrics: metricsserver.Options{BindAddress: "0"},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect((&PostgresClusterReconciler{
			Client:         watchManager.GetClient(),
			Scheme:         watchManager.GetScheme(),
			Recorder:       record.NewFakeRecorder(1024),
			Metrics:        &pgprometheus.NoopRecorder{},
			FleetCollector: pgprometheus.NewFleetCollector(),
		}).SetupWithManager(watchManager)).To(Succeed())

		mgrCtx, mgrCancel = context.WithCancel(context.Background())
		mgrDone = make(chan struct{})
		go func() {
			defer close(mgrDone)

			if startErr := watchManager.Start(mgrCtx); startErr != nil {

				fmt.Fprintf(GinkgoWriter, "watch manager exited: %v\n", startErr)
			}
		}()

		syncCtx, syncCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer syncCancel()
		Expect(watchManager.GetCache().WaitForCacheSync(syncCtx)).To(BeTrue(),
			"watch manager's cache must sync before we trust Eventually()")

		suffix = fmt.Sprintf("%d-%d", GinkgoParallelProcess(), GinkgoRandomSeed())
		className = "pg-watch-class-" + suffix
		clusterName = "pg-watch-cluster-" + suffix
		secretName = "pg-watch-ext-secret-" + suffix
		pgClusterKey = types.NamespacedName{Name: clusterName, Namespace: namespace}
		extSecretKey = types.NamespacedName{Name: secretName, Namespace: namespace}

		clusterClass = &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: className},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Provisioner: "postgresql.cnpg.io",
				Config: &enterprisev4.PostgresClusterClassConfig{
					Instances:        ptr.To(instances),
					Storage:          ptr.To(resource.MustParse(storageAmount)),
					PostgresVersion:  ptr.To(postgresVersion),
					ConnectionPooler: &enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(false)},
				},
				CNPG: &enterprisev4.CNPGConfig{},
			},
		}
		Expect(k8sClient.Create(ctx, clusterClass)).To(Succeed())

		pgCluster = &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
			Spec: enterprisev4.PostgresClusterSpec{
				Class:                 className,
				ClusterDeletionPolicy: ptr.To("Delete"),
				PasswordConfig: &enterprisev4.SuperuserPasswordConfig{
					SuperuserExternalSecretRef: corev1.LocalObjectReference{Name: secretName},
				},
			},
		}
		Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
	})

	AfterAll(func() {

		_ = k8sClient.Delete(ctx, &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
		})
		_ = k8sClient.Delete(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
		})
		_ = k8sClient.Delete(ctx, clusterClass)

		Eventually(func() bool {
			pc := &enterprisev4.PostgresCluster{}
			err := k8sClient.Get(ctx, pgClusterKey, pc)
			return apierrors.IsNotFound(err)
		}, watchTimeout, pollInterval).Should(BeTrue(),
			"PostgresCluster must be garbage-collected after AfterAll")

		By("stopping the dedicated watch manager")
		mgrCancel()
		Eventually(mgrDone, 10*time.Second).Should(BeClosed(),
			"watch manager goroutine must exit when its context is cancelled")
	})

	It("reconciles automatically when the external Secret is created post-CR (CREATE event)", func() {
		By("waiting for the manager to converge SecretsReady=False/ExternalSecretMissing on the missing Secret")
		Eventually(func(g Gomega) {
			cond := secretsReady(g)
			g.Expect(cond).NotTo(BeNil())
			g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(cond.Reason).To(Equal("ExternalSecretMissing"))
		}, watchTimeout, pollInterval).Should(Succeed(),
			"PostgresCluster must report ExternalSecretMissing before the Secret exists")

		By("creating the external Secret with valid credentials but no cnpg.io/reload label")
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Data:       externalCreds,
		})).To(Succeed())

		By("waiting for the create event to surface SecretsReady=False/ExternalSecretMissingReloadLabel")
		Eventually(func(g Gomega) {
			cond := secretsReady(g)
			g.Expect(cond).NotTo(BeNil())
			g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(cond.Reason).To(Equal("ExternalSecretMissingReloadLabel"))
		}, watchTimeout, pollInterval).Should(Succeed(),
			"the operator must require — not stamp — the cnpg.io/reload label")

		By("confirming the operator did not add the label itself")
		s := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, extSecretKey, s)).To(Succeed())
		Expect(s.Labels).NotTo(HaveKey("cnpg.io/reload"))

		By("setting the cnpg.io/reload label as the secret owner would")
		if s.Labels == nil {
			s.Labels = map[string]string{}
		}
		s.Labels["cnpg.io/reload"] = "true"
		Expect(k8sClient.Update(ctx, s)).To(Succeed())

		By("waiting for the label-change event to fire a reconcile and flip SecretsReady=True")
		Eventually(func(g Gomega) {
			cond := secretsReady(g)
			g.Expect(cond).NotTo(BeNil())
			g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(cond.Reason).To(Equal("SuperUserSecretReady"))
		}, watchTimeout, pollInterval).Should(Succeed(),
			"the label-diff predicate must enqueue the cluster when the user adds cnpg.io/reload")
	})

	It("reconciles automatically when the external Secret's data becomes invalid (UPDATE event)", func() {
		By("corrupting the external Secret by dropping the required password key")
		s := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, extSecretKey, s)).To(Succeed())
		s.Data = map[string][]byte{"username": []byte("postgres")}
		Expect(k8sClient.Update(ctx, s)).To(Succeed())

		By("waiting for the data-change event to flip SecretsReady to False")
		Eventually(func(g Gomega) {
			cond := secretsReady(g)
			g.Expect(cond).NotTo(BeNil())
			g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			// The exact reason is owned by the secret model — assert on the
			// invariant: it is no longer the success reason.
			g.Expect(cond.Reason).NotTo(Equal("SuperUserSecretReady"))
		}, watchTimeout, pollInterval).Should(Succeed(),
			"a .data change on the external Secret must enqueue a reconcile via the predicate's data-diff branch")
	})

	It("reconciles automatically when the external Secret is deleted (DELETE event)", func() {
		By("restoring valid Secret data so SecretsReady returns to True")
		s := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, extSecretKey, s)).To(Succeed())
		s.Data = externalCreds
		Expect(k8sClient.Update(ctx, s)).To(Succeed())

		Eventually(func(g Gomega) {
			cond := secretsReady(g)
			g.Expect(cond).NotTo(BeNil())
			g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		}, watchTimeout, pollInterval).Should(Succeed(),
			"baseline: SecretsReady must recover to True before W-03 deletes the Secret")

		By("deleting the external Secret")
		Expect(k8sClient.Delete(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
		})).To(Succeed())

		By("waiting for the delete event to flip SecretsReady to False/ExternalSecretMissing")
		Eventually(func(g Gomega) {
			cond := secretsReady(g)
			g.Expect(cond).NotTo(BeNil())
			g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(cond.Reason).To(Equal("ExternalSecretMissing"))
		}, watchTimeout, pollInterval).Should(Succeed(),
			"the watch's DELETE branch must enqueue the cluster when the external Secret disappears")
	})
})
