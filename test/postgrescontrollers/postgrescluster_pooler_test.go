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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("postgrescontrollers, integration, postgres-pooler", Label("tier:e2e-full", "cloud:aws", "feature:postgres"), func() {

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

	It("postgrescontrollers, integration, postgres-pooler: enables and disables connection pooler",
		Label("tier:e2e-full", "sva:s1", "cloud:aws", "feature:postgres"),
		NodeTimeout(testenv.LongTimeout),
		func(ctx SpecContext) {
			ns := testcaseEnvInst.GetName()
			kubeClient := testcaseEnvInst.GetKubeClient()

			// Pooler class: 2 instances (RO pooler requires ≥2) with cnpg.connectionPooler
			// set (CRD validation requires it when config.connectionPooler.enabled=true).
			pgClass := createPGClassWithPooler(ctx, kubeClient, ns)

			pgCluster := &enterprisev4.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "pooler-cluster", Namespace: ns},
				Spec: enterprisev4.PostgresClusterSpec{
					Class:                 pgClass.Name,
					ClusterDeletionPolicy: ptr.To("Delete"),
					Instances:             ptr.To(int32(2)),
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

			By("enabling connection pooler (RW + RO)")
			patch := client.MergeFrom(pgCluster.DeepCopy())
			pgCluster.Spec.ConnectionPooler = &enterprisev4.ConnectionPoolerEnableConfig{
				Enabled:   ptr.To(true),
				ReadWrite: ptr.To(true),
				ReadOnly:  ptr.To(true),
			}
			Expect(kubeClient.Patch(ctx, pgCluster, patch)).To(Succeed())

			rwPoolerKey := types.NamespacedName{Name: pgCluster.Name + "-pooler-rw", Namespace: ns}
			roPoolerKey := types.NamespacedName{Name: pgCluster.Name + "-pooler-ro", Namespace: ns}

			By("verifying RW and RO Pooler resources are created")
			Eventually(func(g Gomega) {
				rw := &cnpgv1.Pooler{}
				g.Expect(kubeClient.Get(ctx, rwPoolerKey, rw)).To(Succeed())
				ro := &cnpgv1.Pooler{}
				g.Expect(kubeClient.Get(ctx, roPoolerKey, ro)).To(Succeed())
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("verifying ConnectionPoolerStatus is populated")
			Eventually(func(g Gomega) {
				pc := &enterprisev4.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				g.Expect(pc.Status.ConnectionPoolerStatus).NotTo(BeNil())
				g.Expect(pc.Status.ConnectionPoolerStatus.Enabled).To(BeTrue())
				g.Expect(pc.Status.ConnectionPoolerStatus.ReadWriteEnabled).To(BeTrue())
				g.Expect(pc.Status.ConnectionPoolerStatus.ReadOnlyEnabled).To(BeTrue())
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("disabling connection pooler")
			Expect(kubeClient.Get(ctx, clusterKey, pgCluster)).To(Succeed())
			patch = client.MergeFrom(pgCluster.DeepCopy())
			pgCluster.Spec.ConnectionPooler = &enterprisev4.ConnectionPoolerEnableConfig{
				Enabled: ptr.To(false),
			}
			Expect(kubeClient.Patch(ctx, pgCluster, patch)).To(Succeed())

			By("verifying both Pooler resources are deleted")
			Eventually(func(g Gomega) {
				rw := &cnpgv1.Pooler{}
				g.Expect(kubeClient.Get(ctx, rwPoolerKey, rw)).To(Satisfy(apierrors.IsNotFound))
				ro := &cnpgv1.Pooler{}
				g.Expect(kubeClient.Get(ctx, roPoolerKey, ro)).To(Satisfy(apierrors.IsNotFound))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("verifying ConnectionPoolerStatus reflects disabled pooler")
			Eventually(func(g Gomega) {
				pc := &enterprisev4.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				g.Expect(pc.Status.ConnectionPoolerStatus).To(Or(
					BeNil(),
					HaveField("Enabled", BeFalse()),
				))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
		},
	)
})

// createPGClassWithPooler creates a PostgresClusterClass with 2 instances and connection
// pooler configured (required by CRD validation when connectionPooler.enabled=true).
func createPGClassWithPooler(ctx SpecContext, kubeClient client.Client, ns string) *enterprisev4.PostgresClusterClass {
	pgClass := &enterprisev4.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "postgres-pooler-e2e-" + ns,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "e2e-test",
			},
		},
		Spec: enterprisev4.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &enterprisev4.PostgresClusterClassConfig{
				Instances: ptr.To(int32(2)),
			},
			CNPG: &enterprisev4.CNPGConfig{
				ConnectionPooler: &enterprisev4.ConnectionPoolerConfig{},
			},
		},
	}
	Expect(kubeClient.Create(ctx, pgClass)).To(Succeed())
	DeferCleanup(func(ctx SpecContext) {
		err := kubeClient.Delete(ctx, pgClass)
		if err != nil && !apierrors.IsNotFound(err) {
			Expect(err).To(Succeed(), "failed to clean up pooler PostgresClusterClass")
		}
	})
	return pgClass
}
