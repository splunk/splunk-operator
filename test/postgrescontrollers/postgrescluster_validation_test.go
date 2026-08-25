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

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/test/testenv"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("postgrescontrollers, integration, postgres-validation", Label("tier:e2e-full", "cloud:aws", "feature:postgres"), func() {

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

	It("postgrescontrollers, integration, postgres-validation: apiserver rejects mutating spec.class (immutable)",
		Label("tier:e2e-full", "sva:s1", "cloud:aws", "feature:postgres"),
		NodeTimeout(testenv.ShortTimeout),
		func(ctx SpecContext) {
			ns := testcaseEnvInst.GetName()
			kubeClient := testcaseEnvInst.GetKubeClient()

			pgClass := createPGClass(ctx, kubeClient, ns)
			pgCluster := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "validate-class", Namespace: ns},
				Spec: platformv1alpha1.PostgresClusterSpec{
					Class:                 pgClass.Name,
					ClusterDeletionPolicy: ptr.To("Delete"),
				},
			}
			Expect(kubeClient.Create(ctx, pgCluster)).To(Succeed())

			By("attempting to mutate spec.class")
			patch := client.MergeFrom(pgCluster.DeepCopy())
			pgCluster.Spec.Class = "some-other-class"
			err := kubeClient.Patch(ctx, pgCluster, patch)
			Expect(err).To(HaveOccurred(), "expected apiserver to reject class mutation")
			Expect(err.Error()).To(ContainSubstring("immutable"))
		},
	)

	It("postgrescontrollers, integration, postgres-validation: apiserver rejects decreasing spec.storage",
		Label("tier:e2e-full", "tier:e2e-pr", "sva:s1", "cloud:aws", "feature:postgres"),
		NodeTimeout(testenv.ShortTimeout),
		func(ctx SpecContext) {
			ns := testcaseEnvInst.GetName()
			kubeClient := testcaseEnvInst.GetKubeClient()

			pgClass := createPGClass(ctx, kubeClient, ns)
			initialStorage := resource.MustParse("10Gi")
			pgCluster := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "validate-storage", Namespace: ns},
				Spec: platformv1alpha1.PostgresClusterSpec{
					Class:                 pgClass.Name,
					ClusterDeletionPolicy: ptr.To("Delete"),
					Storage:               &initialStorage,
				},
			}
			Expect(kubeClient.Create(ctx, pgCluster)).To(Succeed())

			// Re-fetch once the cache has caught up to get the server-assigned resourceVersion.
			Eventually(func(g Gomega) {
				g.Expect(kubeClient.Get(ctx, types.NamespacedName{Name: pgCluster.Name, Namespace: ns}, pgCluster)).To(Succeed())
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("attempting to decrease spec.storage")
			smallerStorage := resource.MustParse("5Gi")
			patch := client.MergeFrom(pgCluster.DeepCopy())
			pgCluster.Spec.Storage = &smallerStorage
			err := kubeClient.Patch(ctx, pgCluster, patch)
			Expect(err).To(HaveOccurred(), "expected apiserver to reject storage decrease")
			Expect(err.Error()).To(ContainSubstring("storage"))
		},
	)

	It("postgrescontrollers, integration, postgres-validation: apiserver rejects downgrading postgresVersion major",
		Label("tier:e2e-full", "sva:s1", "cloud:aws", "feature:postgres"),
		NodeTimeout(testenv.ShortTimeout),
		func(ctx SpecContext) {
			ns := testcaseEnvInst.GetName()
			kubeClient := testcaseEnvInst.GetKubeClient()

			pgClass := createPGClass(ctx, kubeClient, ns)
			pgCluster := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "validate-pgversion", Namespace: ns},
				Spec: platformv1alpha1.PostgresClusterSpec{
					Class:                 pgClass.Name,
					ClusterDeletionPolicy: ptr.To("Delete"),
					PostgresVersion:       ptr.To("18"),
				},
			}
			Expect(kubeClient.Create(ctx, pgCluster)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(kubeClient.Get(ctx, types.NamespacedName{Name: pgCluster.Name, Namespace: ns}, pgCluster)).To(Succeed())
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("attempting to downgrade postgresVersion to a lower major")
			patch := client.MergeFrom(pgCluster.DeepCopy())
			pgCluster.Spec.PostgresVersion = ptr.To("17")
			err := kubeClient.Patch(ctx, pgCluster, patch)
			Expect(err).To(HaveOccurred(), "expected apiserver to reject major version downgrade")
			Expect(err.Error()).To(ContainSubstring("postgresVersion"))
		},
	)

	It("postgrescontrollers, integration, postgres-validation: apiserver rejects enabled backup without a provider",
		Label("tier:e2e-full", "tier:e2e-pr", "sva:s1", "cloud:aws", "feature:postgres"),
		NodeTimeout(testenv.ShortTimeout),
		func(ctx SpecContext) {
			ns := testcaseEnvInst.GetName()
			schedule := "* * * * *"
			pgClass := &platformv1alpha1.PostgresClusterClass{
				ObjectMeta: metav1.ObjectMeta{Name: "postgres-backup-validation-" + ns},
				Spec: platformv1alpha1.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &platformv1alpha1.PostgresClusterClassConfig{
						Instances: ptr.To(int32(1)),
						Backup: &platformv1alpha1.BackupConfig{
							Enabled:  ptr.To(true),
							Schedule: &schedule,
						},
					},
					CNPG: &platformv1alpha1.CNPGConfig{},
				},
			}

			err := testcaseEnvInst.GetKubeClient().Create(ctx, pgClass)
			Expect(err).To(HaveOccurred(), "expected apiserver to reject enabled backup without a provider")
			Expect(err.Error()).To(ContainSubstring(
				"cnpg.backup.volumeSnapshot or cnpg.backup.barmanObjectStore must be set when config.backup.enabled is true",
			))
		},
	)
})
