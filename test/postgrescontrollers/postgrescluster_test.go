// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package postgrescontrollers

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("postgrescontrollers, integration, postgres", Label("tier:e2e-full", "cloud:aws", "feature:postgres"), func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment

	BeforeEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		var err error
		testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
		Expect(err).To(Succeed(), "Failed to setup test case environment")
	})

	AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
	})

	It("postgrescontrollers, integration, postgres: can deploy a PostgresCluster and reach Ready",
		Label("tier:e2e-full", "tier:e2e-pr", "sva:s1", "cloud:aws", "feature:postgres"),
		NodeTimeout(testenv.ShortTimeout),
		func(ctx SpecContext) {
			ns := testcaseEnvInst.GetName()
			kubeClient := testcaseEnvInst.GetKubeClient()

			pgClass := createPGClass(ctx, kubeClient, ns)

			pgCluster := &enterprisev4.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: ns},
				Spec: enterprisev4.PostgresClusterSpec{
					Class:                 pgClass.Name,
					ClusterDeletionPolicy: ptr.To("Delete"),
				},
			}
			Expect(kubeClient.Create(ctx, pgCluster)).To(Succeed())

			clusterKey := types.NamespacedName{Name: pgCluster.Name, Namespace: ns}

			By("waiting for PostgresCluster to reach Ready phase")
			Eventually(func(g Gomega) {
				pc := &enterprisev4.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				g.Expect(pc.Status.Phase).NotTo(BeNil())
				g.Expect(*pc.Status.Phase).To(Equal("Ready"))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("verifying CNPG Cluster exists and is healthy")
			Eventually(func(g Gomega) {
				cnpg := &cnpgv1.Cluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, cnpg)).To(Succeed())
				g.Expect(cnpg.Status.Phase).To(Equal(cnpgv1.PhaseHealthy))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
		},
	)

	It("postgrescontrollers, integration, postgres: deletes CNPG Cluster when PostgresCluster is deleted with Delete policy",
		Label("tier:e2e-full", "sva:s1", "cloud:aws", "feature:postgres"),
		NodeTimeout(testenv.ShortTimeout),
		func(ctx SpecContext) {
			ns := testcaseEnvInst.GetName()
			kubeClient := testcaseEnvInst.GetKubeClient()

			pgClass := createPGClass(ctx, kubeClient, ns)

			pgCluster := &enterprisev4.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "delete-cluster", Namespace: ns},
				Spec: enterprisev4.PostgresClusterSpec{
					Class:                 pgClass.Name,
					ClusterDeletionPolicy: ptr.To("Delete"),
				},
			}
			Expect(kubeClient.Create(ctx, pgCluster)).To(Succeed())

			clusterKey := types.NamespacedName{Name: pgCluster.Name, Namespace: ns}

			By("waiting for PostgresCluster to reach Ready")
			Eventually(func(g Gomega) {
				pc := &enterprisev4.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				g.Expect(pc.Status.Phase).NotTo(BeNil())
				g.Expect(*pc.Status.Phase).To(Equal("Ready"))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("deleting PostgresCluster")
			Expect(kubeClient.Delete(ctx, pgCluster)).To(Succeed())

			By("waiting for PostgresCluster to be removed")
			Eventually(func() error {
				pc := &enterprisev4.PostgresCluster{}
				return kubeClient.Get(ctx, clusterKey, pc)
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Satisfy(apierrors.IsNotFound))

			By("verifying underlying CNPG Cluster was also deleted")
			Eventually(func() error {
				cnpg := &cnpgv1.Cluster{}
				return kubeClient.Get(ctx, clusterKey, cnpg)
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Satisfy(apierrors.IsNotFound))
		},
	)

	It("postgrescontrollers, integration, postgres: preserves CNPG Cluster and superuser Secret when deleted with Retain policy",
		Label("tier:e2e-full", "sva:s1", "cloud:aws", "feature:postgres"),
		NodeTimeout(testenv.ShortTimeout),
		func(ctx SpecContext) {
			ns := testcaseEnvInst.GetName()
			kubeClient := testcaseEnvInst.GetKubeClient()

			pgClass := createPGClass(ctx, kubeClient, ns)

			// clusterDeletionPolicy defaults to Retain — omit it to test the default.
			pgCluster := &enterprisev4.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "retain-cluster", Namespace: ns},
				Spec: enterprisev4.PostgresClusterSpec{
					Class: pgClass.Name,
				},
			}
			Expect(kubeClient.Create(ctx, pgCluster)).To(Succeed())

			clusterKey := types.NamespacedName{Name: pgCluster.Name, Namespace: ns}

			By("waiting for PostgresCluster to reach Ready")
			var secretName string
			Eventually(func(g Gomega) {
				pc := &enterprisev4.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				g.Expect(pc.Status.Phase).NotTo(BeNil())
				g.Expect(*pc.Status.Phase).To(Equal("Ready"))
				g.Expect(pc.Status.Resources).NotTo(BeNil())
				g.Expect(pc.Status.Resources.SuperUserSecretRef).NotTo(BeNil())
				secretName = pc.Status.Resources.SuperUserSecretRef.Name
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			// Register DeferCleanup to delete the orphaned CNPG Cluster — Retain policy
			// strips owner references, so it survives the PostgresCluster delete below.
			// This must run before namespace teardown, while the cluster is still reachable.
			DeferCleanup(func(ctx SpecContext) {
				cnpg := &cnpgv1.Cluster{}
				err := kubeClient.Get(ctx, clusterKey, cnpg)
				if apierrors.IsNotFound(err) {
					return
				}
				Expect(err).To(Succeed(), "failed to get orphaned CNPG Cluster for cleanup")
				Expect(kubeClient.Delete(ctx, cnpg)).To(Succeed(), "failed to delete orphaned CNPG Cluster")
			})

			By("deleting PostgresCluster")
			Expect(kubeClient.Delete(ctx, pgCluster)).To(Succeed())

			By("waiting for PostgresCluster to be removed")
			Eventually(func() error {
				pc := &enterprisev4.PostgresCluster{}
				return kubeClient.Get(ctx, clusterKey, pc)
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Satisfy(apierrors.IsNotFound))

			By("verifying CNPG Cluster survived with owner references removed")
			Eventually(func(g Gomega) {
				cnpg := &cnpgv1.Cluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, cnpg)).To(Succeed())
				g.Expect(cnpg.OwnerReferences).To(BeEmpty())
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("verifying superuser Secret survived with owner references removed")
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(kubeClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: ns}, secret)).To(Succeed())
				g.Expect(secret.OwnerReferences).To(BeEmpty())
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
		},
	)
})

// createPGClass creates a minimal PostgresClusterClass for the spec and registers
// a DeferCleanup to delete it. PostgresClusterClass is cluster-scoped, so it is not
// covered by the per-namespace TestCaseEnv teardown — the name is keyed by namespace
// to keep parallel specs isolated.
func createPGClass(ctx SpecContext, kubeClient client.Client, ns string) *enterprisev4.PostgresClusterClass {
	pgClass := &enterprisev4.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "postgres-e2e-" + ns,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "e2e-test",
			},
		},
		Spec: enterprisev4.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &enterprisev4.PostgresClusterClassConfig{
				Instances: ptr.To(int32(1)),
			},
			CNPG: &enterprisev4.CNPGConfig{},
		},
	}
	Expect(kubeClient.Create(ctx, pgClass)).To(Succeed())
	DeferCleanup(func(ctx SpecContext) {
		err := kubeClient.Delete(ctx, pgClass)
		if err != nil && !apierrors.IsNotFound(err) {
			Expect(err).To(Succeed(), "failed to clean up PostgresClusterClass")
		}
	})
	return pgClass
}
