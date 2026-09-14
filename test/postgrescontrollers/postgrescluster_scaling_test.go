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
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("postgrescontrollers, integration, postgres-scaling", Label("tier:e2e-full", "cloud:aws", "feature:postgres"), func() {

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

	It("postgrescontrollers, integration, postgres-scaling: resizes storage and returns to Ready",
		Label("tier:e2e-full", "tier:e2e-pr", "sva:s1", "cloud:aws", "feature:postgres"),
		NodeTimeout(testenv.MediumTimeout),
		func(ctx SpecContext) {
			ns := testcaseEnvInst.GetName()
			kubeClient := testcaseEnvInst.GetKubeClient()

			pgClass := createPGClass(ctx, kubeClient, ns)
			pgCluster := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "storage-resize", Namespace: ns},
				Spec: platformv1alpha1.PostgresClusterSpec{
					Class:                 pgClass.Name,
					ClusterDeletionPolicy: ptr.To("Delete"),
				},
			}
			Expect(kubeClient.Create(ctx, pgCluster)).To(Succeed())

			clusterKey := types.NamespacedName{Name: pgCluster.Name, Namespace: ns}

			By("waiting for PostgresCluster to reach Ready")
			Eventually(func(g Gomega) {
				pc := &platformv1alpha1.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				g.Expect(pc.Status.Phase).NotTo(BeNil())
				g.Expect(*pc.Status.Phase).To(Equal("Ready"))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("patching spec.storage upward")
			newStorage := resource.MustParse("51Gi")
			patch := client.MergeFrom(pgCluster.DeepCopy())
			pgCluster.Spec.Storage = &newStorage
			Expect(kubeClient.Patch(ctx, pgCluster, patch)).To(Succeed())

			By("waiting for PostgresCluster to return to Ready with updated storage")
			Eventually(func(g Gomega) {
				pc := &platformv1alpha1.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				g.Expect(pc.Status.Phase).NotTo(BeNil())
				g.Expect(*pc.Status.Phase).To(Equal("Ready"))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("verifying CNPG StorageConfiguration.Size reflects the new quantity")
			Eventually(func(g Gomega) {
				cnpg := &cnpgv1.Cluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, cnpg)).To(Succeed())
				g.Expect(cnpg.Spec.StorageConfiguration.Size).To(Equal(newStorage.String()))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
		},
	)

	It("postgrescontrollers, integration, postgres-scaling: updates CPU/memory resources and returns to Ready",
		Label("tier:e2e-full", "sva:s1", "cloud:aws", "feature:postgres"),
		NodeTimeout(testenv.MediumTimeout),
		func(ctx SpecContext) {
			ns := testcaseEnvInst.GetName()
			kubeClient := testcaseEnvInst.GetKubeClient()

			pgClass := createPGClass(ctx, kubeClient, ns)
			pgCluster := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "resources-change", Namespace: ns},
				Spec: platformv1alpha1.PostgresClusterSpec{
					Class:                 pgClass.Name,
					ClusterDeletionPolicy: ptr.To("Delete"),
				},
			}
			Expect(kubeClient.Create(ctx, pgCluster)).To(Succeed())

			clusterKey := types.NamespacedName{Name: pgCluster.Name, Namespace: ns}

			By("waiting for PostgresCluster to reach Ready")
			Eventually(func(g Gomega) {
				pc := &platformv1alpha1.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				g.Expect(pc.Status.Phase).NotTo(BeNil())
				g.Expect(*pc.Status.Phase).To(Equal("Ready"))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("patching spec.resources")
			newResources := corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("200m"),
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				},
			}
			patch := client.MergeFrom(pgCluster.DeepCopy())
			pgCluster.Spec.Resources = &newResources
			Expect(kubeClient.Patch(ctx, pgCluster, patch)).To(Succeed())

			By("waiting for PostgresCluster to return to Ready")
			Eventually(func(g Gomega) {
				pc := &platformv1alpha1.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				g.Expect(pc.Status.Phase).NotTo(BeNil())
				g.Expect(*pc.Status.Phase).To(Equal("Ready"))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("verifying CNPG Spec.Resources reflects the updated requests")
			Eventually(func(g Gomega) {
				cnpg := &cnpgv1.Cluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, cnpg)).To(Succeed())
				g.Expect(cnpg.Spec.Resources.Requests.Cpu().Cmp(resource.MustParse("200m"))).To(Equal(0))
				g.Expect(cnpg.Spec.Resources.Requests.Memory().Cmp(resource.MustParse("256Mi"))).To(Equal(0))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
		},
	)

	It("postgrescontrollers, integration, postgres-scaling: scales instances 1→3→1 holding Provisioning during transition",
		Label("tier:e2e-full", "sva:s1", "cloud:aws", "feature:postgres"),
		NodeTimeout(testenv.LongTimeout),
		func(ctx SpecContext) {
			ns := testcaseEnvInst.GetName()
			kubeClient := testcaseEnvInst.GetKubeClient()

			pgClass := createPGClass(ctx, kubeClient, ns)
			pgCluster := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "horiz-scale", Namespace: ns},
				Spec: platformv1alpha1.PostgresClusterSpec{
					Class:                 pgClass.Name,
					ClusterDeletionPolicy: ptr.To("Delete"),
					Instances:             ptr.To(int32(1)),
				},
			}
			Expect(kubeClient.Create(ctx, pgCluster)).To(Succeed())

			clusterKey := types.NamespacedName{Name: pgCluster.Name, Namespace: ns}

			By("waiting for single-instance PostgresCluster to reach Ready")
			Eventually(func(g Gomega) {
				pc := &platformv1alpha1.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				g.Expect(pc.Status.Phase).NotTo(BeNil())
				g.Expect(*pc.Status.Phase).To(Equal("Ready"))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("scaling instances to 3")
			patch := client.MergeFrom(pgCluster.DeepCopy())
			pgCluster.Spec.Instances = ptr.To(int32(3))
			Expect(kubeClient.Patch(ctx, pgCluster, patch)).To(Succeed())

			// Phase should hold Provisioning while replicas are being created. Asserting
			// Phase alone is vulnerable to missing a fast transition inside one 5s poll
			// window, so also require ReadyInstances to still be below the new desired
			// count — the two together can't both be satisfied by a stale read.
			By("observing Provisioning phase with ReadyInstances below target during scale-out")
			Eventually(func(g Gomega) {
				pc := &platformv1alpha1.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				g.Expect(pc.Status.Phase).NotTo(BeNil())
				g.Expect(*pc.Status.Phase).To(Equal("Provisioning"))
				g.Expect(pc.Status.ReadyInstances).NotTo(BeNil())
				g.Expect(*pc.Status.ReadyInstances).To(BeNumerically("<", 3))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("waiting for 3-instance PostgresCluster to reach Ready")
			Eventually(func(g Gomega) {
				pc := &platformv1alpha1.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				g.Expect(pc.Status.Phase).NotTo(BeNil())
				g.Expect(*pc.Status.Phase).To(Equal("Ready"))
				g.Expect(pc.Status.ReadyInstances).NotTo(BeNil())
				g.Expect(*pc.Status.ReadyInstances).To(BeEquivalentTo(3))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("verifying CNPG cluster has 3 instances")
			Eventually(func(g Gomega) {
				cnpg := &cnpgv1.Cluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, cnpg)).To(Succeed())
				g.Expect(cnpg.Spec.Instances).To(Equal(3))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("scaling instances back to 1")
			patch = client.MergeFrom(pgCluster.DeepCopy())
			pgCluster.Spec.Instances = ptr.To(int32(1))
			Expect(kubeClient.Patch(ctx, pgCluster, patch)).To(Succeed())

			By("waiting for 1-instance PostgresCluster to reach Ready")
			Eventually(func(g Gomega) {
				pc := &platformv1alpha1.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				g.Expect(pc.Status.Phase).NotTo(BeNil())
				g.Expect(*pc.Status.Phase).To(Equal("Ready"))
				g.Expect(pc.Status.ReadyInstances).NotTo(BeNil())
				g.Expect(*pc.Status.ReadyInstances).To(BeEquivalentTo(1))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
		},
	)
})
