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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("postgrescontrollers, integration, postgres-database", Label("tier:e2e-full", "cloud:aws", "feature:postgres"), func() {

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

	It("postgrescontrollers, integration, postgres-database: can deploy a PostgresDatabase and reach Ready",
		Label("tier:e2e-full", "tier:e2e-pr", "sva:s1", "cloud:aws", "feature:postgres"),
		NodeTimeout(testenv.ShortTimeout),
		func(ctx SpecContext) {
			ns := testcaseEnvInst.GetName()
			kubeClient := testcaseEnvInst.GetKubeClient()

			// Provision a ready PostgresCluster first.
			pgClass := createPGClass(ctx, kubeClient, ns)

			pgCluster := &enterprisev4.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "db-cluster", Namespace: ns},
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

			// Create the PostgresDatabase.
			pgDB := &enterprisev4.PostgresDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db", Namespace: ns},
				Spec: enterprisev4.PostgresDatabaseSpec{
					ClusterRef: corev1.LocalObjectReference{Name: pgCluster.Name},
					Databases: []enterprisev4.DatabaseDefinition{
						{Name: "appdb", DeletionPolicy: "Delete"},
					},
				},
			}
			Expect(kubeClient.Create(ctx, pgDB)).To(Succeed())

			dbKey := types.NamespacedName{Name: pgDB.Name, Namespace: ns}

			By("waiting for PostgresDatabase to reach Ready")
			Eventually(func(g Gomega) {
				pd := &enterprisev4.PostgresDatabase{}
				g.Expect(kubeClient.Get(ctx, dbKey, pd)).To(Succeed())
				g.Expect(pd.Status.Phase).NotTo(BeNil())
				g.Expect(*pd.Status.Phase).To(Equal("Ready"))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("verifying managed roles for the database are present on the PostgresCluster")
			// The database controller publishes one admin and one rw role per database in its
			// own status (see getDesiredRoles in pkg/postgresql/database/core/database.go),
			// named "<db>_admin" and "<db>_rw"; the cluster controller then claims ownership and
			// reconciles them into Status.ManagedRolesStatus.Reconciled (see
			// pkg/postgresql/cluster/core/managed_roles_model.go).
			Eventually(func(g Gomega) {
				pc := &enterprisev4.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				g.Expect(presentRoleNames(pc)).To(ContainElements("appdb_admin", "appdb_rw"))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
		},
	)

	It("postgrescontrollers, integration, postgres-database: removes child resources when PostgresDatabase is deleted with Delete policy",
		Label("tier:e2e-full", "sva:s1", "cloud:aws", "feature:postgres"),
		NodeTimeout(testenv.ShortTimeout),
		func(ctx SpecContext) {
			ns := testcaseEnvInst.GetName()
			kubeClient := testcaseEnvInst.GetKubeClient()

			pgClass := createPGClass(ctx, kubeClient, ns)

			pgCluster := &enterprisev4.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "db-cluster-del", Namespace: ns},
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

			pgDB := &enterprisev4.PostgresDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "del-db", Namespace: ns},
				Spec: enterprisev4.PostgresDatabaseSpec{
					ClusterRef: corev1.LocalObjectReference{Name: pgCluster.Name},
					Databases: []enterprisev4.DatabaseDefinition{
						{Name: "dropme", DeletionPolicy: "Delete"},
					},
				},
			}
			Expect(kubeClient.Create(ctx, pgDB)).To(Succeed())

			dbKey := types.NamespacedName{Name: pgDB.Name, Namespace: ns}
			By("waiting for PostgresDatabase to reach Ready")
			Eventually(func(g Gomega) {
				pd := &enterprisev4.PostgresDatabase{}
				g.Expect(kubeClient.Get(ctx, dbKey, pd)).To(Succeed())
				g.Expect(pd.Status.Phase).NotTo(BeNil())
				g.Expect(*pd.Status.Phase).To(Equal("Ready"))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			// Naming matches configMapName/roleSecretName in
			// pkg/postgresql/database/core/database.go: "<postgresDB>-<db>-config" and
			// "<postgresDB>-<db>-<role>".
			accessConfigMapKey := types.NamespacedName{Name: "del-db-dropme-config", Namespace: ns}
			roleSecretKeys := []types.NamespacedName{
				{Name: "del-db-dropme-admin", Namespace: ns},
				{Name: "del-db-dropme-rw", Namespace: ns},
			}

			By("verifying the database's access ConfigMap and role Secrets were provisioned")
			Eventually(func(g Gomega) {
				g.Expect(kubeClient.Get(ctx, accessConfigMapKey, &corev1.ConfigMap{})).To(Succeed())

				for _, key := range roleSecretKeys {
					g.Expect(kubeClient.Get(ctx, key, &corev1.Secret{})).To(Succeed())
				}
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("deleting PostgresDatabase")
			Expect(kubeClient.Delete(ctx, pgDB)).To(Succeed())

			By("waiting for PostgresDatabase to be removed")
			Eventually(func() error {
				pd := &enterprisev4.PostgresDatabase{}
				return kubeClient.Get(ctx, dbKey, pd)
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Satisfy(apierrors.IsNotFound))

			By("verifying the database's managed roles are marked absent on the PostgresCluster")
			// cleanupManagedRoles (pkg/postgresql/database/core/database.go) publishes drop
			// intent and holds the finalizer until the cluster confirms the roles are gone from
			// Status.ManagedRolesStatus.Reconciled — by the time the PostgresDatabase CR itself
			// is removed (waited for above), the roles are guaranteed to already be absent here.
			Eventually(func(g Gomega) {
				pc := &enterprisev4.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				g.Expect(presentRoleNames(pc)).NotTo(ContainElement("dropme_admin"))
				g.Expect(presentRoleNames(pc)).NotTo(ContainElement("dropme_rw"))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("verifying the database's access ConfigMap and role Secrets are deleted, not leaked")
			Eventually(func(g Gomega) {
				err := kubeClient.Get(ctx, accessConfigMapKey, &corev1.ConfigMap{})
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected access ConfigMap to be deleted")

				for _, key := range roleSecretKeys {
					err := kubeClient.Get(ctx, key, &corev1.Secret{})
					g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected role Secret %s to be deleted", key.Name)
				}
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
		},
	)
})

var _ = Describe("postgrescontrollers, integration, postgres-database-scenarios", Label("tier:e2e-full", "cloud:aws", "feature:postgres"), func() {

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

	It("postgrescontrollers, integration, postgres-database-scenarios: Retain policy preserves CNPG Database and role on CR deletion",
		Label("tier:e2e-full", "sva:s1", "cloud:aws", "feature:postgres"),
		NodeTimeout(testenv.ShortTimeout),
		func(ctx SpecContext) {
			ns := testcaseEnvInst.GetName()
			kubeClient := testcaseEnvInst.GetKubeClient()

			pgClass := createPGClass(ctx, kubeClient, ns)
			pgCluster := &enterprisev4.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "retain-db-cluster", Namespace: ns},
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

			pgDB := &enterprisev4.PostgresDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "retain-db", Namespace: ns},
				Spec: enterprisev4.PostgresDatabaseSpec{
					ClusterRef: corev1.LocalObjectReference{Name: pgCluster.Name},
					Databases: []enterprisev4.DatabaseDefinition{
						{Name: "keepme", DeletionPolicy: "Retain"},
					},
				},
			}
			Expect(kubeClient.Create(ctx, pgDB)).To(Succeed())

			dbKey := types.NamespacedName{Name: pgDB.Name, Namespace: ns}
			By("waiting for PostgresDatabase to reach Ready")
			Eventually(func(g Gomega) {
				pd := &enterprisev4.PostgresDatabase{}
				g.Expect(kubeClient.Get(ctx, dbKey, pd)).To(Succeed())
				g.Expect(pd.Status.Phase).NotTo(BeNil())
				g.Expect(*pd.Status.Phase).To(Equal("Ready"))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			// cnpgDatabaseName = "<pgDB.Name>-<dbName>"
			cnpgDBKey := types.NamespacedName{Name: pgDB.Name + "-keepme", Namespace: ns}

			By("deleting PostgresDatabase")
			Expect(kubeClient.Delete(ctx, pgDB)).To(Succeed())

			By("waiting for PostgresDatabase CR to be removed")
			Eventually(func() error {
				pd := &enterprisev4.PostgresDatabase{}
				return kubeClient.Get(ctx, dbKey, pd)
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Satisfy(apierrors.IsNotFound))

			By("verifying the CNPG Database object survived (Retain policy)")
			Eventually(func(g Gomega) {
				cnpgDB := &cnpgv1.Database{}
				g.Expect(kubeClient.Get(ctx, cnpgDBKey, cnpgDB)).To(Succeed())
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("verifying the managed roles remain reconciled/present on the PostgresCluster")
			// Consistently blocks for its full duration regardless of outcome, so it must stay
			// well within the spec's NodeTimeout budget. 1m (vs. the default 2s) gives the
			// cluster controller a realistic window to have processed the deletion.
			Consistently(func(g Gomega) {
				pc := &enterprisev4.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				g.Expect(presentRoleNames(pc)).To(ContainElements("keepme_admin", "keepme_rw"))
			}, time.Minute, testenv.ConsistentPollInterval).Should(Succeed())
		},
	)

	It("postgrescontrollers, integration, postgres-database-scenarios: multiple databases in one CR all reach Ready",
		Label("tier:e2e-full", "tier:e2e-pr", "sva:s1", "cloud:aws", "feature:postgres"),
		NodeTimeout(testenv.ShortTimeout),
		func(ctx SpecContext) {
			ns := testcaseEnvInst.GetName()
			kubeClient := testcaseEnvInst.GetKubeClient()

			pgClass := createPGClass(ctx, kubeClient, ns)
			pgCluster := &enterprisev4.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "multi-db-cluster", Namespace: ns},
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

			pgDB := &enterprisev4.PostgresDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "multi-db", Namespace: ns},
				Spec: enterprisev4.PostgresDatabaseSpec{
					ClusterRef: corev1.LocalObjectReference{Name: pgCluster.Name},
					Databases: []enterprisev4.DatabaseDefinition{
						{Name: "alpha", DeletionPolicy: "Delete"},
						{Name: "beta", DeletionPolicy: "Delete"},
						{Name: "gamma", DeletionPolicy: "Delete"},
					},
				},
			}
			Expect(kubeClient.Create(ctx, pgDB)).To(Succeed())

			dbKey := types.NamespacedName{Name: pgDB.Name, Namespace: ns}
			By("waiting for PostgresDatabase to reach Ready")
			Eventually(func(g Gomega) {
				pd := &enterprisev4.PostgresDatabase{}
				g.Expect(kubeClient.Get(ctx, dbKey, pd)).To(Succeed())
				g.Expect(pd.Status.Phase).NotTo(BeNil())
				g.Expect(*pd.Status.Phase).To(Equal("Ready"))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("verifying all role pairs are present on the PostgresCluster")
			Eventually(func(g Gomega) {
				pc := &enterprisev4.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				g.Expect(presentRoleNames(pc)).To(ContainElements(
					"alpha_admin", "alpha_rw",
					"beta_admin", "beta_rw",
					"gamma_admin", "gamma_rw",
				))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("verifying a CNPG Database object exists for each database")
			Eventually(func(g Gomega) {
				for _, dbName := range []string{"alpha", "beta", "gamma"} {
					cnpgDB := &cnpgv1.Database{}
					g.Expect(kubeClient.Get(ctx, types.NamespacedName{
						Name:      pgDB.Name + "-" + dbName,
						Namespace: ns,
					}, cnpgDB)).To(Succeed(), "expected CNPG Database for %s", dbName)
				}
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
		},
	)

	It("postgrescontrollers, integration, postgres-database-scenarios: adding and removing a database updates roles",
		Label("tier:e2e-full", "sva:s1", "cloud:aws", "feature:postgres"),
		NodeTimeout(testenv.ShortTimeout),
		func(ctx SpecContext) {
			ns := testcaseEnvInst.GetName()
			kubeClient := testcaseEnvInst.GetKubeClient()

			pgClass := createPGClass(ctx, kubeClient, ns)
			pgCluster := &enterprisev4.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "addremove-cluster", Namespace: ns},
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

			pgDB := &enterprisev4.PostgresDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "addremove-db", Namespace: ns},
				Spec: enterprisev4.PostgresDatabaseSpec{
					ClusterRef: corev1.LocalObjectReference{Name: pgCluster.Name},
					Databases: []enterprisev4.DatabaseDefinition{
						{Name: "first", DeletionPolicy: "Delete"},
					},
				},
			}
			Expect(kubeClient.Create(ctx, pgDB)).To(Succeed())

			dbKey := types.NamespacedName{Name: pgDB.Name, Namespace: ns}
			By("waiting for PostgresDatabase to reach Ready with first database")
			Eventually(func(g Gomega) {
				pd := &enterprisev4.PostgresDatabase{}
				g.Expect(kubeClient.Get(ctx, dbKey, pd)).To(Succeed())
				g.Expect(pd.Status.Phase).NotTo(BeNil())
				g.Expect(*pd.Status.Phase).To(Equal("Ready"))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("adding a second database")
			Expect(kubeClient.Get(ctx, dbKey, pgDB)).To(Succeed())
			patch := client.MergeFrom(pgDB.DeepCopy())
			pgDB.Spec.Databases = append(pgDB.Spec.Databases, enterprisev4.DatabaseDefinition{
				Name: "second", DeletionPolicy: "Delete",
			})
			Expect(kubeClient.Patch(ctx, pgDB, patch)).To(Succeed())

			By("waiting for PostgresDatabase to return to Ready")
			Eventually(func(g Gomega) {
				pd := &enterprisev4.PostgresDatabase{}
				g.Expect(kubeClient.Get(ctx, dbKey, pd)).To(Succeed())
				g.Expect(pd.Status.Phase).NotTo(BeNil())
				g.Expect(*pd.Status.Phase).To(Equal("Ready"))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("verifying both role pairs are present")
			Eventually(func(g Gomega) {
				pc := &enterprisev4.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				g.Expect(presentRoleNames(pc)).To(ContainElements(
					"first_admin", "first_rw",
					"second_admin", "second_rw",
				))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("removing the first database from spec")
			Expect(kubeClient.Get(ctx, dbKey, pgDB)).To(Succeed())
			patch = client.MergeFrom(pgDB.DeepCopy())
			pgDB.Spec.Databases = []enterprisev4.DatabaseDefinition{
				{Name: "second", DeletionPolicy: "Delete"},
			}
			Expect(kubeClient.Patch(ctx, pgDB, patch)).To(Succeed())

			By("verifying first db roles are flipped absent, second db roles remain present")
			Eventually(func(g Gomega) {
				pc := &enterprisev4.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				roles := presentRoleNames(pc)
				g.Expect(roles).NotTo(ContainElement("first_admin"))
				g.Expect(roles).NotTo(ContainElement("first_rw"))
				g.Expect(roles).To(ContainElements("second_admin", "second_rw"))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
		},
	)

	It("postgrescontrollers, integration, postgres-database-scenarios: holds Pending when cluster not found then recovers to Ready",
		Label("tier:e2e-full", "sva:s1", "cloud:aws", "feature:postgres"),
		NodeTimeout(testenv.MediumTimeout),
		func(ctx SpecContext) {
			ns := testcaseEnvInst.GetName()
			kubeClient := testcaseEnvInst.GetKubeClient()

			By("creating PostgresDatabase referencing a missing cluster")
			pgDB := &enterprisev4.PostgresDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "notfound-db", Namespace: ns},
				Spec: enterprisev4.PostgresDatabaseSpec{
					ClusterRef: corev1.LocalObjectReference{Name: "does-not-exist"},
					Databases: []enterprisev4.DatabaseDefinition{
						{Name: "waitdb", DeletionPolicy: "Delete"},
					},
				},
			}
			Expect(kubeClient.Create(ctx, pgDB)).To(Succeed())

			dbKey := types.NamespacedName{Name: pgDB.Name, Namespace: ns}

			By("asserting PostgresDatabase holds Pending with ClusterNotFound condition")
			Eventually(func(g Gomega) {
				pd := &enterprisev4.PostgresDatabase{}
				g.Expect(kubeClient.Get(ctx, dbKey, pd)).To(Succeed())
				g.Expect(pd.Status.Phase).NotTo(BeNil())
				g.Expect(*pd.Status.Phase).To(Equal("Pending"))
				found := false
				for _, c := range pd.Status.Conditions {
					if c.Type == "ClusterReady" && c.Status == metav1.ConditionFalse && c.Reason == "ClusterNotFound" {
						found = true
					}
				}
				g.Expect(found).To(BeTrue(), "expected ClusterReady=False/ClusterNotFound condition")
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("creating the referenced PostgresCluster")
			pgClass := createPGClass(ctx, kubeClient, ns)
			pgCluster := &enterprisev4.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "does-not-exist", Namespace: ns},
				Spec: enterprisev4.PostgresClusterSpec{
					Class:                 pgClass.Name,
					ClusterDeletionPolicy: ptr.To("Delete"),
				},
			}
			Expect(kubeClient.Create(ctx, pgCluster)).To(Succeed())

			By("waiting for PostgresDatabase to recover to Ready")
			Eventually(func(g Gomega) {
				pd := &enterprisev4.PostgresDatabase{}
				g.Expect(kubeClient.Get(ctx, dbKey, pd)).To(Succeed())
				g.Expect(pd.Status.Phase).NotTo(BeNil())
				g.Expect(*pd.Status.Phase).To(Equal("Ready"))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
		},
	)
})

// presentRoleNames returns the names of managed roles the PostgresCluster
// controller has confirmed are present (see Status.ManagedRolesStatus.Reconciled
// in computeDesiredRoles/syncManagedRolesStatusFromCNPG in
// pkg/postgresql/cluster/core/managed_roles_model.go). A role dropped by its
// owning PostgresDatabase disappears from this list once CNPG confirms removal.
func presentRoleNames(pc *enterprisev4.PostgresCluster) []string {
	if pc.Status.ManagedRolesStatus == nil {
		return nil
	}
	return pc.Status.ManagedRolesStatus.Reconciled
}
